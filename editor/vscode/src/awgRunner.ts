// SPDX-License-Identifier: Apache-2.0

// Local `sensei` CLI runner — the extension's only write channel.
//
// The dashboard is a pure gRPC *read* client. Write operations (promoting a
// reviewed candidate) are inherently local: they mutate the workspace's
// awareness YAML and rebuild the embedded seed, which the remote daemon cannot
// do because it does not have the user's checkout. So those run the local
// `sensei` binary in the workspace — and only when the user has explicitly opted
// in via `sensei.enableLocalOperations` (default off).
//
// We never bypass `sensei`'s guards: `sensei promote` validates naming/status/
// confidence/evidence, appends to the canonical YAML, removes the candidate,
// and rebuilds. The GUI just drives it (with `--dry-run` for preview) and shows
// the resulting git diff. Nothing lands invisibly; the user still commits.

import * as vscode from 'vscode';
import * as path from 'path';
import * as fs from 'fs';
import { execFile } from 'child_process';
import {
  resolveRebuildPlan,
  resolveSenseiBinaryCandidates,
  validateReadOnlySenseiArgs,
  type RebuildPlan,
} from './localOpsPlan';

export interface AwgRunResult {
  ok: boolean; // process exited 0
  code: number | null;
  stdout: string;
  stderr: string;
  /** Set when the process could not be started (e.g. sensei not found). */
  spawnError?: string;
}

/** Raised when a caller asks for a local op but the feature is disabled. */
export class LocalOpsDisabledError extends Error {
  constructor() {
    super(
      'Local operations are disabled. Enable "sensei.enableLocalOperations" ' +
        'to let the dashboard run sensei in your workspace.'
    );
    this.name = 'LocalOpsDisabledError';
  }
}

export function localOpsEnabled(): boolean {
  return vscode.workspace
    .getConfiguration('sensei')
    .get<boolean>('enableLocalOperations', false);
}

function senseiPath(): string {
  return (
    vscode.workspace.getConfiguration('sensei').get<string>('senseiPath', 'sensei') || 'sensei'
  );
}

// Ordered binary candidates. After the awg→sensei rename, a bare `sensei` may not
// be on PATH even though the project builds `./bin/sensei` (or a legacy `awg`
// binary is still around). Try, in order: an explicit sensei.senseiPath setting,
// the workspace-built bin/sensei, `sensei` on PATH, then the legacy bin/awg / awg.
function candidateBins(cwd: string): string[] {
  const configured = (
    vscode.workspace.getConfiguration('sensei').get<string>('senseiPath', '') || ''
  ).trim();
  return resolveSenseiBinaryCandidates(cwd, configured);
}

// A path-shaped candidate (absolute or containing a separator) must exist on disk;
// a bare command name is left for the OS PATH lookup at spawn time.
function candidateUsable(bin: string): boolean {
  if (path.isAbsolute(bin) || bin.includes(path.sep)) {
    return fs.existsSync(bin);
  }
  return true;
}

// The last binary that spawned successfully, reused to avoid re-probing every call.
let resolvedBin: string | undefined;

// The workspace root to run sensei from. Prefer a folder that actually holds an
// awareness candidate tree (so sensei auto-detects the right repos); otherwise the
// first workspace folder.
export function workspaceRoot(): string | undefined {
  const folders = vscode.workspace.workspaceFolders ?? [];
  for (const f of folders) {
    if (fs.existsSync(path.join(f.uri.fsPath, 'docs', 'awareness', 'candidates'))) {
      return f.uri.fsPath;
    }
  }
  return folders[0]?.uri.fsPath;
}

// Run a binary, capturing stdout/stderr. A non-zero exit is a *result*, not a
// thrown error — callers decide what a failure means (e.g. validation failed).
function run(
  bin: string,
  args: string[],
  cwd: string,
  timeoutMs: number
): Promise<AwgRunResult> {
  return new Promise((resolve) => {
    execFile(
      bin,
      args,
      { cwd, timeout: timeoutMs, maxBuffer: 8 * 1024 * 1024, windowsHide: true },
      (err, stdout, stderr) => {
        if (err && (err as NodeJS.ErrnoException).code === 'ENOENT') {
          resolve({ ok: false, code: null, stdout: '', stderr: '', spawnError: `not found: ${bin}` });
          return;
        }
        const code = err && typeof (err as any).code === 'number' ? (err as any).code : err ? 1 : 0;
        resolve({ ok: code === 0, code, stdout: String(stdout ?? ''), stderr: String(stderr ?? '') });
      }
    );
  });
}

/** Run `sensei <args>` in the workspace. Throws if local ops are disabled or there is no workspace. */
export async function runAwg(args: string[], timeoutMs: number): Promise<AwgRunResult> {
  if (!localOpsEnabled()) {
    return Promise.reject(new LocalOpsDisabledError());
  }
  const cwd = workspaceRoot();
  if (!cwd) {
    return Promise.reject(new Error('No workspace folder open to run sensei in.'));
  }

  // Fast path: reuse the previously-resolved binary while it is still usable.
  if (resolvedBin && candidateUsable(resolvedBin)) {
    const res = await run(resolvedBin, args, cwd, timeoutMs);
    if (!res.spawnError) {
      return res;
    }
    resolvedBin = undefined; // it disappeared — fall through and re-resolve
  }

  const cands = candidateBins(cwd);
  let lastErr: AwgRunResult | undefined;
  for (const bin of cands) {
    if (!candidateUsable(bin)) {
      continue;
    }
    const res = await run(bin, args, cwd, timeoutMs);
    if (!res.spawnError) {
      resolvedBin = bin; // first binary that actually spawned wins
      return res;
    }
    lastErr = res; // ENOENT — try the next candidate
  }
  return (
    lastErr ?? {
      ok: false,
      code: null,
      stdout: '',
      stderr: '',
      spawnError: `sensei binary not found (tried: ${cands.join(', ')}). Set "sensei.senseiPath" in settings.`,
    }
  );
}

/** Run an allowlisted read-only `sensei` inspection command in the workspace.
 * This remains available when local write operations are disabled. */
export async function runAwgReadOnly(args: string[], timeoutMs: number): Promise<AwgRunResult> {
  const invalid = validateReadOnlySenseiArgs(args);
  if (invalid) {
    return { ok: false, code: null, stdout: '', stderr: invalid };
  }
  const cwd = workspaceRoot();
  if (!cwd) {
    return Promise.reject(new Error('No workspace folder open to run sensei in.'));
  }

  if (resolvedBin && candidateUsable(resolvedBin)) {
    const res = await run(resolvedBin, args, cwd, timeoutMs);
    if (!res.spawnError) {
      return res;
    }
    resolvedBin = undefined;
  }

  const cands = candidateBins(cwd);
  let lastErr: AwgRunResult | undefined;
  for (const bin of cands) {
    if (!candidateUsable(bin)) {
      continue;
    }
    const res = await run(bin, args, cwd, timeoutMs);
    if (!res.spawnError) {
      resolvedBin = bin;
      return res;
    }
    lastErr = res;
  }
  return (
    lastErr ?? {
      ok: false,
      code: null,
      stdout: '',
      stderr: '',
      spawnError: `sensei binary not found (tried: ${cands.join(', ')}). Set "sensei.senseiPath" in settings.`,
    }
  );
}

/** `git diff --stat` over the awareness tree, so the user sees exactly what a promote changed. */
export async function awarenessDiffStat(timeoutMs = 10000): Promise<string> {
  const cwd = workspaceRoot();
  if (!cwd) {
    return '';
  }
  const res = await run(
    'git',
    ['diff', '--stat', '--', 'docs/awareness', 'golang/server/embeddata/awareness.nt'],
    cwd,
    timeoutMs
  );
  return res.stdout.trim();
}

// ---- capability detection (informational; never gated) --------------------

let awgAvailableCache: boolean | undefined;

/** True when the configured `sensei` binary can be spawned in the workspace.
 * Independent of enableLocalOperations — capability detection must work even
 * when local ops are off, so the UI can tell the user what they're missing. */
export async function awgAvailable(timeoutMs = 5000): Promise<boolean> {
  if (awgAvailableCache !== undefined) {
    return awgAvailableCache;
  }
  const cwd = workspaceRoot();
  if (!cwd) {
    return false;
  }
  // Probe the same candidates runAwg would use; the first that spawns (a non-zero
  // exit still means it spawned — only ENOENT means "not found") is available and
  // is cached for reuse.
  for (const bin of candidateBins(cwd)) {
    if (!candidateUsable(bin)) {
      continue;
    }
    const res = await run(bin, ['--help'], cwd, timeoutMs);
    if (!res.spawnError) {
      resolvedBin = bin;
      awgAvailableCache = true;
      return true;
    }
  }
  awgAvailableCache = false;
  return false;
}

/** Reset cached binary resolution — call when sensei.* settings change. */
export function resetAwgBinaryCache(): void {
  resolvedBin = undefined;
  awgAvailableCache = undefined;
}

/** True when the workspace root looks like an Sensei-enabled project. */
export function isAwgProject(): boolean {
  const root = workspaceRoot();
  return !!root && fs.existsSync(path.join(root, 'docs', 'awareness'));
}

function servicesRepoSetting(): string {
  return (
    vscode.workspace.getConfiguration('sensei').get<string>('servicesRepoPath', '') || ''
  ).trim();
}

/** Work out the correct rebuild command for the current workspace. */
export function rebuildPlan(selectedDomain = '', workspaceDomain = ''): RebuildPlan {
  return resolveRebuildPlan(senseiPath(), workspaceRoot(), servicesRepoSetting(), {
    selectedDomain,
    workspaceDomain,
  });
}

/** Lines in the committed seed (0 if missing) — the cheap, deterministic metric
 *  the Rebuild shrink-guard compares before/after. */
export function seedLineCount(seedPath: string | undefined): number {
  if (!seedPath) {
    return 0;
  }
  try {
    return fs.readFileSync(seedPath, 'utf8').split('\n').length;
  } catch {
    return 0;
  }
}

/** Snapshot the seed so a suspicious rebuild can be rolled back. */
export function backupSeed(seedPath: string | undefined): Buffer | undefined {
  if (!seedPath) {
    return undefined;
  }
  try {
    return fs.readFileSync(seedPath);
  } catch {
    return undefined;
  }
}

/** Restore a seed snapshot. Returns true if it was written. */
export function restoreSeed(seedPath: string | undefined, backup: Buffer | undefined): boolean {
  if (!seedPath || !backup) {
    return false;
  }
  try {
    fs.writeFileSync(seedPath, backup);
    return true;
  } catch {
    return false;
  }
}
