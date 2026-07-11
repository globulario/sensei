// SPDX-License-Identifier: Apache-2.0
//
// Resolve the "current project" domain for domain-scoped queries. Sensei's
// domain keys look like `github.com/owner/repo`; a workspace's git remote maps
// to exactly that, so we can scope the file-level views to the project the
// developer is in without them configuring anything. An explicit `sensei.domain`
// setting always wins; the derived value is a best-effort default.

import { execFile } from 'node:child_process';
import type * as vscodeType from 'vscode';

// Lazy access to the vscode API. Importing 'vscode' at module top would break
// headless unit tests of the pure helpers (the module is loaded outside the
// extension host); the API is only touched at runtime, inside the host.
function vscodeApi(): typeof vscodeType {
  // eslint-disable-next-line @typescript-eslint/no-require-imports
  return require('vscode');
}

/**
 * Map a git remote URL to a Sensei domain key (`host/owner/repo`, no `.git`).
 * Handles https, ssh://, and scp-like `git@host:owner/repo` forms. Pure —
 * unit-tested.
 */
export function domainFromRemoteUrl(url: string): string | undefined {
  const u = (url || '').trim().replace(/\.git\/?$/i, '');
  if (!u) {
    return undefined;
  }
  // scheme://[user@]host[:port]/path   (https, ssh, git)
  let m = u.match(/^[a-z][a-z0-9+.-]*:\/\/(?:[^@/]+@)?([^/:]+)(?::\d+)?\/(.+)$/i);
  if (!m) {
    // scp-like: [user@]host:path
    m = u.match(/^(?:[^@]+@)?([^/:]+):(.+)$/);
  }
  if (!m) {
    return undefined;
  }
  const host = m[1].toLowerCase();
  const path = m[2].replace(/^\/+|\/+$/g, '');
  if (!host || !path) {
    return undefined;
  }
  return `${host}/${path}`;
}

let cache: { root: string; domain: string } | undefined;

/** Clear the cached derivation (call on workspace/config change). */
export function resetProjectDomainCache(): void {
  cache = undefined;
}

function workspaceRoot(): string | undefined {
  return vscodeApi().workspace.workspaceFolders?.[0]?.uri.fsPath;
}

function deriveFromGit(root: string): Promise<string | undefined> {
  return new Promise((resolve) => {
    execFile(
      'git',
      ['-C', root, 'config', '--get', 'remote.origin.url'],
      { timeout: 3000 },
      (err, stdout) => resolve(err ? undefined : domainFromRemoteUrl(String(stdout))),
    );
  });
}

/**
 * The effective domain for a query: the explicit `sensei.domain` setting if set,
 * otherwise the workspace's git-remote-derived domain (cached per root).
 * Returns undefined when neither is available (single-domain graphs resolve
 * trivially with no domain, so that stays correct).
 */
export async function effectiveDomain(explicit: string | undefined): Promise<string | undefined> {
  const set = (explicit || '').trim();
  if (set) {
    return set;
  }
  const root = workspaceRoot();
  if (!root) {
    return undefined;
  }
  if (cache && cache.root === root) {
    return cache.domain || undefined;
  }
  const domain = await deriveFromGit(root);
  cache = { root, domain: domain || '' };
  return domain;
}
