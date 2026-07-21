// SPDX-License-Identifier: AGPL-3.0-only

// The "Project Awareness" dashboard — an architect's cockpit.
//
// A single webview that answers two questions: is the project under
// architectural control (the Metadata banner), and for any selected concern,
// what is the local causal graph around it (Resolve + Query(related)).
//
// All reads go over gRPC. The corpus area is review-only: it reads candidate
// YAML from disk and shows the guarded CLI to run — it never mutates the graph,
// because promotion is a not-yet-guarded flow and a one-click button could
// silently half-promote.

import * as vscode from 'vscode';
import * as path from 'path';
import * as fs from 'fs';
import * as crypto from 'crypto';
import {
  AwgError,
  MetadataResponse,
  QueryRow,
  ArtifactListFilters,
  metadata,
  queryByClass,
  queryRelated,
  resolveNode,
  getOntologyNavigationDescriptor,
  getArchitectureControlSnapshot,
  listArchitectureArtifacts,
  getArchitectureArtifactState,
  prepareArchitectAnswerDisposition,
  recordArchitectAnswerDisposition,
  ArchitectureDispositionInput,
} from './grpcClient';
import { assessMetadataAuthority } from './graphAuthority';
import {
  chooseAutomaticGraphDomain,
  effectiveDomain,
  effectiveRepositoryIdentity,
} from './projectDomain';
import {
  AwgRunResult,
  LocalOpsDisabledError,
  awarenessDiffStat,
  awgAvailable,
  backupSeed,
  isAwgProject,
  localOpsEnabled,
  rebuildPlan,
  restoreSeed,
  runAwg,
  runAwgReadOnly,
  seedLineCount,
  workspaceRoot,
} from './awgRunner';
import { candidatePromotePlan } from './localOpsPlan';
import { loadActiveTask, mergeTaskStatusJson, taskMatchesGraphDomain } from './taskSession';

// Shared "Awareness Operations" output channel — the full, auditable log of
// every sensei command the dashboard runs (command, cwd, exit, stdout/stderr).
// Created lazily so a read-only session never opens an empty channel.
let opChannel: vscode.OutputChannel | undefined;
function ops(): vscode.OutputChannel {
  if (!opChannel) {
    opChannel = vscode.window.createOutputChannel('Awareness Operations');
  }
  return opChannel;
}

// class → canonical YAML file `sensei promote` appends to (golang promote map).
// Classes outside this set have no promotion target.
const PROMOTE_TARGET: Record<string, string> = {
  invariant: 'invariants.yaml',
  failure_mode: 'failure_modes.yaml',
  incident_pattern: 'incident_patterns.yaml',
  intent: 'intents.yaml',
};

type ReviewDecision = 'approved' | 'rejected';
const reviewKey = (id: string): string => 'sensei.review:' + id;

// sensei ops can rebuild the seed, which takes longer than a query — give promote
// a generous deadline independent of the per-request gRPC timeout.
const AWG_OP_TIMEOUT_MS = 180000;
const AWG_STATUS_TIMEOUT_MS = 30000;

// Shrink guard: if a rebuild leaves the seed below this fraction of its previous
// line count, treat it as a clobber (e.g. an AG-only build overwriting the
// combined seed, which drops to ~10%), restore the previous seed, and refuse to
// reload the live store. Legitimate source edits never lose half the graph.
const SEED_SHRINK_GUARD = 0.5;

// The deterministic codebase scan: echo drafter (no LLM, no API key, no cost),
// grounding architectural-intent proposals against the workspace tree. `--apply`
// is appended when the user chooses to write results to the queue.
const SCAN_ARGS = [
  'intent-mine',
  '--repo',
  '.',
  '--sources',
  'docs,comments,schemas,tests',
  '--drafter',
  'echo',
];

// A candidate entry parsed (dependency-free) from a candidates/*.yaml file.
// This is only for listing + the review card; `sensei promote --dry-run` remains
// the source of truth for what actually lands.
interface CandidateEntry {
  id: string;
  klass?: string;
  confidence?: string;
  label?: string;
  summary?: string;
  evidence?: string;
  files?: string[];
  review_label?: string;
  line?: number; // 0-based line of the `- id:` entry within its file
  // Enrichment added by handleCandidates (not parsed from the entry itself):
  decision?: ReviewDecision; // staged approve/reject from workspaceState
  target?: string; // canonical YAML this class promotes into, if any
}

// Resolve's class whitelist (golang/server/resolve.go) is the lowercase token
// itself — NOT the CamelCase ontology name the proto comment suggests. So we
// pass the token straight through. Classes outside this set (forbidden_fix,
// test) have no resolve endpoint; we surface a soft note rather than an error.
const RESOLVABLE = new Set([
  'invariant',
  'failure_mode',
  'incident_pattern',
  'symbol',
  'source_file',
  'intent',
  'code_symbol',
  'forbidden_fix',
  'test',
  // Architectural spine + pattern + UML classes (server resolves these via
  // resolveIRIForClassAndID / awarenessRelatedID).
  'meta_principle',
  'component',
  'boundary',
  'contract',
  'decision',
  'evidence',
  'design_pattern',
  'implementation_pattern',
  'pattern_misuse',
  'architecture_claim',
  'open_question',
  'architect_answer',
  'evidence_probe',
]);

type ControlKind = 'closure' | 'convergence' | 'admission' | 'verification';
const CONTROL_KEYS: Record<ControlKind, string> = {
  closure: 'sensei.control.closureAssessment',
  convergence: 'sensei.control.convergenceSession',
  admission: 'sensei.control.admissionDecision',
  verification: 'sensei.control.admissionVerification',
};

interface ControlArtifact {
  kind: ControlKind;
  configured?: string;
  path?: string;
  exists: boolean;
  valid: boolean;
  digest?: string;
  modified?: number;
  summary: Record<string, string>;
  error?: string;
}

interface GraphNode {
  id: string; // class-qualified
  token: string; // class token
  label: string;
  severity?: string;
  uml_view?: string; // UML view for client-side view filtering
  level: number; // 0 = center, 1, 2
}

interface GraphEdge {
  from: string;
  to: string;
  relation: string;
}

function splitQualified(qid: string): { token: string; bare: string } {
  const i = qid.indexOf(':');
  if (i < 0) {
    return { token: '', bare: qid };
  }
  return { token: qid.slice(0, i), bare: decodeURIComponent(qid.slice(i + 1)) };
}

function errText(err: unknown): string {
  if (err instanceof Error) {
    return err.message;
  }
  return String(err);
}

function commandFailureMessage(res: AwgRunResult): string {
  return (
    res.spawnError ||
    res.stderr.trim() ||
    res.stdout.trim() ||
    (res.code === null ? 'command failed' : `exit ${res.code}`)
  );
}

function field(content: string, key: string): string {
  const re = new RegExp(`^\\s*${key}:\\s*["']?([^"'\\n#]+)["']?\\s*(?:#.*)?$`, 'm');
  return (re.exec(content)?.[1] || '').trim();
}

function firstField(content: string, keys: string[]): string {
  for (const key of keys) {
    const v = field(content, key);
    if (v) {
      return v;
    }
  }
  return '';
}

function summarizeControlArtifact(kind: ControlKind, content: string): Record<string, string> {
  if (kind === 'closure') {
    return {
      verdict: firstField(content, ['verdict', 'closure_verdict', 'status']),
      waiting_on: firstField(content, ['waiting_on', 'wait_class', 'next_waiting_on']),
      next_action: firstField(content, ['next_action', 'recommended_next_action']),
      graph_digest: firstField(content, ['graph_digest', 'graph_digest_sha256']),
    };
  }
  if (kind === 'convergence') {
    return {
      status: firstField(content, ['status', 'latest_status']),
      iteration: firstField(content, ['iteration', 'latest_iteration']),
      waiting_on: firstField(content, ['waiting_on', 'wait_class']),
      budget: firstField(content, ['budget', 'max_iterations']),
    };
  }
  if (kind === 'admission') {
    return {
      decision: firstField(content, ['decision', 'status', 'admission']),
      inspect: firstField(content, ['inspect', 'inspect_status']),
      modify: firstField(content, ['modify', 'modify_status']),
      waiting_on: firstField(content, ['waiting_on', 'wait_class']),
    };
  }
  return {
    scope: firstField(content, ['scope', 'scope_status', 'verification']),
    correctness: firstField(content, ['correctness', 'correctness_certified']),
    extra_tracked_paths: firstField(content, ['extra_tracked_paths']),
    extra_untracked_paths: firstField(content, ['extra_untracked_paths']),
  };
}

function formatArtifactSummary(artifact: ControlArtifact): string {
  const lines = [`${artifact.kind}: ${artifact.path || artifact.configured || '(none)'}`];
  for (const [k, v] of Object.entries(artifact.summary)) {
    if (v) {
      lines.push(`${k}: ${v}`);
    }
  }
  if (artifact.digest) {
    lines.push(`digest: ${artifact.digest}`);
  }
  return lines.join('\n');
}

async function selectControlArtifactInState(
  state: vscode.Memento,
  kind: ControlKind
): Promise<void> {
  const root = workspaceRoot();
  const choice = await vscode.window.showOpenDialog({
    canSelectFiles: true,
    canSelectFolders: false,
    canSelectMany: false,
    defaultUri: root ? vscode.Uri.file(root) : undefined,
    filters: { 'YAML or JSON': ['yaml', 'yml', 'json'] },
    title: `Select Sensei ${kind} artifact`,
  });
  const file = choice?.[0]?.fsPath;
  if (!file || !root) {
    return;
  }
  const rel = path.relative(root, file);
  if (rel.startsWith('..') || path.isAbsolute(rel)) {
    void vscode.window.showWarningMessage('Sensei: control artifacts must be inside the workspace.');
    return;
  }
  await state.update(CONTROL_KEYS[kind], rel);
}

// Indent multi-line command output for the operations log.
function indentLog(s: string): string {
  return s
    .trimEnd()
    .split('\n')
    .map((l) => '  ' + l)
    .join('\n');
}

const indentOf = (s: string): number => s.length - s.replace(/^\s+/, '').length;
const unquote = (s: string): string => s.trim().replace(/^["']|["']$/g, '').trim();

// Capture a YAML scalar that may be inline (`key: value`) or a block scalar
// (`key: >-` / `|` followed by indented lines). Dependency-free and lossy by
// design: it feeds the review card, while `sensei promote --dry-run` stays the
// source of truth for what actually lands.
function blockScalar(block: string[], key: string, cap = 600): string | undefined {
  const re = new RegExp(`^(\\s*)${key}:\\s*(.*)$`);
  for (let i = 0; i < block.length; i++) {
    const m = re.exec(block[i]);
    if (!m) {
      continue;
    }
    const keyIndent = m[1].length;
    const inline = m[2].trim();
    if (inline && !/^[|>][+-]?$/.test(inline)) {
      return unquote(inline).slice(0, cap);
    }
    const out: string[] = [];
    for (let j = i + 1; j < block.length; j++) {
      const ln = block[j];
      if (ln.trim() === '') {
        continue;
      }
      if (indentOf(ln) <= keyIndent) {
        break;
      }
      out.push(ln.trim());
    }
    return out.join(' ').slice(0, cap) || undefined;
  }
  return undefined;
}

function listUnder(block: string[], parentKey: string, childKey: string): string[] {
  const out: string[] = [];
  const parentRe = new RegExp(`^(\\s*)${parentKey}:\\s*$`);
  for (let i = 0; i < block.length; i++) {
    const pm = parentRe.exec(block[i]);
    if (!pm) {
      continue;
    }
    const childRe = new RegExp(`^(\\s*)${childKey}:\\s*$`);
    for (let j = i + 1; j < block.length; j++) {
      if (indentOf(block[j]) <= pm[1].length && block[j].trim() !== '') {
        return out; // left the parent map
      }
      const cm = childRe.exec(block[j]);
      if (!cm) {
        continue;
      }
      for (let k = j + 1; k < block.length; k++) {
        const item = /^\s*-\s*(.+?)\s*$/.exec(block[k]);
        if (item && indentOf(block[k]) > cm[1].length) {
          out.push(unquote(item[1]));
        } else if (block[k].trim() !== '' && indentOf(block[k]) <= cm[1].length) {
          return out;
        }
      }
      return out;
    }
  }
  return out;
}

// Split a candidates/*.yaml into per-entry review cards. Each entry begins with
// a `- id: <id>` line; we slice the file on those boundaries.
function parseCandidateEntries(content: string): CandidateEntry[] {
  const lines = content.split(/\r?\n/);
  const idRe = /^\s*-\s*id:\s*(.+?)\s*$/;
  const starts: number[] = [];
  for (let i = 0; i < lines.length; i++) {
    if (idRe.test(lines[i])) {
      starts.push(i);
    }
  }
  const entries: CandidateEntry[] = [];
  for (let s = 0; s < starts.length; s++) {
    const block = lines.slice(starts[s], s + 1 < starts.length ? starts[s + 1] : lines.length);
    const id = unquote(idRe.exec(block[0])![1]);
    if (!id) {
      continue;
    }
    entries.push({
      id,
      klass: blockScalar(block, 'class', 60),
      confidence: blockScalar(block, 'confidence', 30),
      label: blockScalar(block, 'label', 200),
      summary: blockScalar(block, 'summary'),
      evidence: blockScalar(block, 'evidence'),
      files: listUnder(block, 'protects', 'files'),
      review_label: blockScalar(block, 'review_label', 60),
      line: starts[s],
    });
  }
  return entries;
}

export class DashboardPanel {
  static current: DashboardPanel | undefined;
  static readonly viewType = 'sensei.dashboard';

  static async selectControlArtifact(
    context: vscode.ExtensionContext,
    kind: ControlKind
  ): Promise<void> {
    await selectControlArtifactInState(context.workspaceState, kind);
    await DashboardPanel.current?.handleControlState();
  }

  static async clearControlSelection(context: vscode.ExtensionContext): Promise<void> {
    for (const key of Object.values(CONTROL_KEYS)) {
      await context.workspaceState.update(key, undefined);
    }
    await DashboardPanel.current?.handleControlState();
  }

  private readonly disposables: vscode.Disposable[] = [];

  static show(context: vscode.ExtensionContext): void {
    if (DashboardPanel.current) {
      DashboardPanel.current.panel.reveal();
      return;
    }
    const panel = vscode.window.createWebviewPanel(
      DashboardPanel.viewType,
      'Project Awareness',
      vscode.ViewColumn.Active,
      {
        enableScripts: true,
        retainContextWhenHidden: true,
        localResourceRoots: [vscode.Uri.joinPath(context.extensionUri, 'media')],
      }
    );
    panel.iconPath = vscode.Uri.joinPath(context.extensionUri, 'media', 'awareness.svg');
    DashboardPanel.current = new DashboardPanel(panel, context.extensionUri, context.workspaceState);
  }

  // Maps a candidate id to where its entry lives, so approve/reject/open/promote
  // can act on it. Rebuilt on every candidate scan.
  private candidateIndex = new Map<string, { file: string; line: number; label: string; klass?: string }>();

  private constructor(
    private readonly panel: vscode.WebviewPanel,
    extensionUri: vscode.Uri,
    private readonly state: vscode.Memento
  ) {
    this.panel.webview.html = this.html(extensionUri, this.panel.webview);
    this.panel.webview.onDidReceiveMessage(
      (m) => this.onMessage(m),
      null,
      this.disposables
    );
    this.panel.onDidDispose(() => this.dispose(), null, this.disposables);
    this.watchActiveTaskFiles();
    vscode.window.onDidChangeActiveTextEditor(
      () => {
        void this.handleControlState();
      },
      null,
      this.disposables
    );
  }

  private decisionFor(id: string): ReviewDecision | undefined {
    return this.state.get<ReviewDecision>(reviewKey(id));
  }
  private async setDecision(id: string, d: ReviewDecision | undefined): Promise<void> {
    await this.state.update(reviewKey(id), d);
  }

  private dispose(): void {
    DashboardPanel.current = undefined;
    this.panel.dispose();
    while (this.disposables.length) {
      this.disposables.pop()?.dispose();
    }
  }

  private cfg(): { addr: string; domain: string; timeout: number } {
    const c = vscode.workspace.getConfiguration('sensei');
    return {
      addr: c.get<string>('serverAddr', 'localhost:10120'),
      domain: c.get<string>('domain', '') || '',
      timeout: c.get<number>('requestTimeoutMs', 10000),
    };
  }

  // The domain the banner + lists are scoped to. `undefined` = not chosen, so
  // default to the current project (setting or git remote); `''` = the user
  // picked "All domains" (graph-wide).
  private selectedDomain: string | undefined;
  private scopedGraphDomain: string | undefined;

  private async activeDomain(): Promise<string> {
    if (this.selectedDomain !== undefined) {
      return this.selectedDomain;
    }
    return (await effectiveDomain(this.cfg().domain)) ?? '';
  }

  private async graphDomain(): Promise<string> {
    if (this.selectedDomain !== undefined) {
      return this.selectedDomain;
    }
    if (this.scopedGraphDomain !== undefined) {
      return this.scopedGraphDomain;
    }
    return (await this.scopedMetadata()).domain;
  }

  private post(msg: unknown): void {
    void this.panel.webview.postMessage(msg);
  }

  private async onMessage(msg: any): Promise<void> {
    try {
      switch (msg?.type) {
        case 'getMetadata':
          return await this.handleMetadata();
        case 'setDomain':
          // '' = All domains (graph-wide); a key = scope to it. Re-pull the
          // banner; the webview re-requests the active list under the new scope.
          this.selectedDomain = typeof msg.domain === 'string' ? msg.domain : undefined;
          await this.handleMetadata();
          await this.handleControlSnapshot();
          return await this.handleControlState();
        case 'refresh':
          return msg.mode === 'rebuild'
            ? await this.handleRefreshRebuild()
            : await this.handleRefreshReload();
        case 'listClass':
          return await this.handleList(msg.cls);
        case 'resolve':
          return await this.handleResolve(msg.id);
        case 'graph':
          return await this.handleGraph(msg.id, msg.label, msg.depth);
        case 'getCandidates':
          return this.handleCandidates();
        case 'getControlState':
          return await this.handleControlState();
        case 'getNavigationDescriptor':
          return await this.handleNavigationDescriptor();
        case 'getControlSnapshot':
          return await this.handleControlSnapshot();
        case 'listArtifacts':
          return await this.handleListArtifacts(msg.filters, msg.cursor);
        case 'getArtifactState':
          return await this.handleArtifactState(msg.nodeIri);
        case 'prepareDisposition':
          return await this.handlePrepareDisposition(msg);
        case 'commitDisposition':
          return await this.handleCommitDisposition(msg);
        case 'selectControlArtifact':
          return await this.handleSelectControlArtifact(msg.kind);
        case 'clearControlSelection':
          return await this.handleClearControlSelection();
        case 'controlStatus':
          return await this.handleControlStatus(msg.kind);
        case 'candidatePreview':
          return await this.handleCandidatePreview(msg.id);
        case 'candidatePromote':
          return await this.handleCandidatePromote(msg.id, msg.label);
        case 'candidateApprove':
          return await this.handleDecision(msg.id, 'approved');
        case 'candidateReject':
          return await this.handleDecision(msg.id, 'rejected');
        case 'candidateUndecide':
          return await this.handleDecision(msg.id, undefined);
        case 'candidateOpen':
          return this.handleCandidateOpen(msg.id);
        case 'promoteApproved':
          return await this.handlePromoteApproved();
        case 'candidateScan':
          return await this.handleCandidateScan();
        case 'candidateScanApply':
          return await this.handleCandidateScanApply();
        case 'showOpLog':
          ops().show(true);
          return;
        case 'openAnchor':
          return this.handleOpenAnchor(msg.file, msg.line);
        case 'copy':
          await vscode.env.clipboard.writeText(String(msg.text ?? ''));
          void vscode.window.showInformationMessage('Awareness: command copied to clipboard.');
          return;
      }
    } catch (err) {
      const e = err instanceof AwgError ? err : new AwgError(String(err));
      this.post({
        type: 'error',
        context: msg?.type ?? 'unknown',
        message: e.message,
        unreachable: e instanceof AwgError ? e.unreachable : false,
      });
    }
  }

  // The local-ops state the webview needs to render the refresh bar correctly:
  // whether writes are enabled, plus the project-aware rebuild plan (so a
  // single-repo workspace, a combined awareness-graph+services workspace, and a
  // misconfigured one each present the right command / disabled state).
  private localOpsPayload(): {
    enabled: boolean;
    rebuild: {
      mode: string;
      command: string;
      cwd?: string;
      servicesRepoPath?: string;
      servicesDetected: boolean;
      reason?: string;
    };
  } {
    const plan = rebuildPlan();
    return {
      enabled: localOpsEnabled(),
      rebuild: {
        mode: plan.mode,
        command: plan.command,
        cwd: plan.cwd,
        servicesRepoPath: plan.servicesRepoPath,
        servicesDetected: plan.servicesDetected,
        reason: plan.reason,
      },
    };
  }

  // Fetch metadata scoped to the effective domain. Defaults the dashboard to THIS
  // project's domain (from the git remote) when the graph carries it; otherwise
  // if the server exposes exactly one domain, prefer that home scope over an
  // ambiguous graph-wide "All domains" view. Re-evaluated every call (not
  // one-time), so it self-corrects after a reseed:
  //   - user explicitly picked a domain (selectedDomain !== undefined) → honour it
  //   - else the project domain, IF the graph actually carries it → scope to it
  //   - else the sole advertised graph domain, if there is one
  //   - else graph-wide (the project isn't a distinct domain in this graph)
  private async scopedMetadata(): Promise<{ domain: string; data: MetadataResponse }> {
    const { addr, timeout } = this.cfg();
    let domain = await this.activeDomain();
    const requestedDomain = domain;
    let requestRejected = false;
    let data: MetadataResponse;
    try {
      data = await metadata(addr, timeout, domain || undefined);
    } catch (err) {
      if (this.selectedDomain !== undefined || !domain || !errText(err).includes('unknown domain scope')) {
        throw err;
      }
      requestRejected = true;
      domain = '';
      data = await metadata(addr, timeout, undefined);
    }
    if (this.selectedDomain === undefined) {
      const available = data.available_domains ?? [];
      const nextDomain = chooseAutomaticGraphDomain(requestedDomain, available, requestRejected);
      if (nextDomain && nextDomain !== domain) {
        domain = nextDomain;
        data = await metadata(addr, timeout, domain);
      } else if (!nextDomain && domain) {
        domain = '';
        data = await metadata(addr, timeout, undefined);
      } else {
        domain = nextDomain;
      }
    }
    this.scopedGraphDomain = domain;
    return { domain, data };
  }

  private async handleMetadata(): Promise<void> {
    const { domain, data } = await this.scopedMetadata();
    this.post({ type: 'metadata', data, activeDomain: domain, localOps: this.localOpsPayload() });
  }

  // ── Phase 9.5 architectural control panel (read-only) ──────────────────────
  // Thin transport: fetch the owner projection and post it verbatim. The host
  // NEVER computes closure/severity/lifecycle/class; the webview renders the
  // typed state. The navigation descriptor is registry-derived and cached.
  private cachedNavDescriptor: unknown;

  private async controlScope(): Promise<{ repo?: string; domain: string; addr: string; timeout: number }> {
    const { addr, timeout } = this.cfg();
    const repo = await effectiveRepositoryIdentity();
    const domain = await this.graphDomain();
    return { repo, domain, addr, timeout };
  }

  private async handleNavigationDescriptor(): Promise<void> {
    if (this.cachedNavDescriptor) {
      this.post({ type: 'navigationDescriptor', descriptor: this.cachedNavDescriptor });
      return;
    }
    const { addr, timeout } = this.cfg();
    const resp = await getOntologyNavigationDescriptor(addr, timeout);
    this.cachedNavDescriptor = resp.descriptor;
    this.post({ type: 'navigationDescriptor', descriptor: resp.descriptor });
  }

  private async handleControlSnapshot(): Promise<void> {
    const { repo, domain, addr, timeout } = await this.controlScope();
    if (!repo) {
      // No logical repository identity → do not call the RPC (the server rejects
      // an empty identity); render an explicit unavailable state, never a zero.
      this.post({ type: 'controlSnapshot', unavailable: true, reason: 'repository_context_unavailable' });
      return;
    }
    const resp = await getArchitectureControlSnapshot(addr, repo, domain, timeout);
    this.post({ type: 'controlSnapshot', snapshot: resp.snapshot });
  }

  private async handleListArtifacts(
    filters: ArtifactListFilters | undefined,
    cursor: string | undefined
  ): Promise<void> {
    const { repo, domain, addr, timeout } = await this.controlScope();
    if (!repo) {
      this.post({ type: 'artifactIndex', unavailable: true, reason: 'repository_context_unavailable' });
      return;
    }
    const resp = await listArchitectureArtifacts(
      addr,
      repo,
      domain,
      100,
      cursor ?? '',
      filters ?? {},
      timeout
    );
    this.post({ type: 'artifactIndex', index: resp.index, filters: filters ?? {} });
  }

  private async handleArtifactState(nodeIri: unknown): Promise<void> {
    const iri = String(nodeIri ?? '');
    const { repo, domain, addr, timeout } = await this.controlScope();
    if (!repo) {
      this.post({ type: 'artifactState', unavailable: true, reason: 'repository_context_unavailable', nodeIri: iri });
      return;
    }
    const resp = await getArchitectureArtifactState(addr, repo, domain, iri, timeout);
    this.post({ type: 'artifactState', state: resp.state, nodeIri: iri });
  }

  // ── Phase 9.5 Checkpoint 5: guarded architect-answer mutation family ────────
  // Painfully literal: prepare (pure) → the webview shows the owner candidate →
  // explicit confirm → commit once → typed receipt/refusal → ALWAYS refresh the
  // artifact state from the owner. The host adds no authority and never chains.

  private dispositionInputFrom(msg: any, repo: string, domain: string): ArchitectureDispositionInput {
    const answer = typeof msg.answerText === 'string' && msg.answerText !== '' ? Buffer.from(msg.answerText, 'utf8') : undefined;
    return {
      repository_identity: repo,
      domain,
      question_id: String(msg.questionId || ''),
      disposition: msg.disposition,
      reusability: msg.reusability || 'ARCHITECTURE_REUSABILITY_NONE',
      rationale: String(msg.rationale || ''),
      answer_id: msg.answerId || undefined,
      answer_bytes: answer,
    };
  }

  private async handlePrepareDisposition(msg: any): Promise<void> {
    const { repo, domain, addr, timeout } = await this.controlScope();
    if (!repo) {
      this.post({ type: 'dispositionPrepared', questionId: msg.questionId, refusal: { reason_code: 'repository_context_unavailable', owner: 'client', mutation_applied: false } });
      return;
    }
    const resp = await prepareArchitectAnswerDisposition(addr, this.dispositionInputFrom(msg, repo, domain), timeout);
    this.post({ type: 'dispositionPrepared', questionId: msg.questionId, candidate: resp.candidate, refusal: resp.refusal });
  }

  private async handleCommitDisposition(msg: any): Promise<void> {
    const { repo, domain, addr, timeout } = await this.controlScope();
    if (!repo) {
      this.post({ type: 'dispositionCommitted', questionId: msg.questionId, refusal: { reason_code: 'repository_context_unavailable', owner: 'client', mutation_applied: false } });
      return;
    }
    const resp = await recordArchitectAnswerDisposition(addr, this.dispositionInputFrom(msg, repo, domain), String(msg.expectedHead || ''), timeout);
    this.post({ type: 'dispositionCommitted', questionId: msg.questionId, receipt: resp.receipt, refusal: resp.refusal });
    // ALWAYS refresh the architectural state from the owner after a commit attempt.
    if (msg.nodeIri) {
      await this.handleArtifactState(msg.nodeIri);
    }
  }

  // Reload: re-pull what the server already serves (Metadata). Cheap, no local
  // op — just a fresh read, so the banner reflects a graph rebuilt out-of-band.
  private async handleRefreshReload(): Promise<void> {
    try {
      const { domain, data } = await this.scopedMetadata();
      const authority = assessMetadataAuthority(data);
      this.post({ type: 'metadata', data, activeDomain: domain, localOps: this.localOpsPayload() });
      this.post({
        type: 'refreshResult',
        mode: 'reload',
        ok: authority.authoritative,
        authoritative: authority.authoritative,
        authority,
        message: authority.authoritative ? '' : authority.detail || authority.summary,
      });
    } catch (err) {
      const e = err instanceof AwgError ? err : new AwgError(String(err));
      this.post({ type: 'refreshResult', mode: 'reload', ok: false, message: e.message, unreachable: e.unreachable });
    }
  }

  // Rebuild: regenerate the seed from source (`sensei rebuild`) in the workspace,
  // then reload Metadata and report before/after counts. A local op — gated,
  // confirmed, logged, and surfaced as a git diff the user commits.
  private async handleRefreshRebuild(): Promise<void> {
    if (!localOpsEnabled()) {
      this.post({ type: 'refreshResult', mode: 'rebuild', ok: false, message: new LocalOpsDisabledError().message });
      return;
    }

    // Project-aware: in the awareness-graph repo the committed seed is the
    // COMBINED graph, so we must pass --services-repo; a plain rebuild would
    // overwrite it with a single-repo seed. If the combined build can't be
    // resolved, block rather than silently shrink the seed.
    const selectedDomain = await this.graphDomain();
    const workspaceDomain = (await effectiveDomain(this.cfg().domain)) ?? '';
    const plan = rebuildPlan(selectedDomain, workspaceDomain);
    if (plan.mode === 'blocked') {
      const msg = plan.reason ?? 'Rebuild is unavailable for this workspace.';
      ops().appendLine('\n=== Rebuild graph — BLOCKED ===\n  ' + msg);
      this.post({ type: 'refreshResult', mode: 'rebuild', ok: false, message: msg, guard: 'blocked' });
      void vscode.window.showErrorMessage('Awareness: ' + msg);
      return;
    }

    const scopeLabel =
      plan.mode === 'combined' ? 'combined (awareness-graph + services)' : 'single-repo';
    const svcLine = plan.servicesRepoPath
      ? `${plan.servicesRepoPath}${plan.servicesDetected ? '  [auto-detected ../services]' : '  [configured]'}`
      : '(none — single-repo build)';
    const detail =
      `Command:       ${plan.command}\n` +
      `Working dir:   ${plan.cwd ?? '(none)'}\n` +
      `Services repo: ${svcLine}\n` +
      `Domain:        ${selectedDomain || 'All domains'}\n` +
      `Graph scope:   ${scopeLabel}\n\n` +
      'The regenerated seed changes your working tree but is NOT committed — you review the git diff and commit.';
    const choice = await vscode.window.showWarningMessage(
      'Rebuild the awareness graph from source?',
      { modal: true, detail },
      'Rebuild'
    );
    if (choice !== 'Rebuild') {
      this.post({ type: 'refreshResult', mode: 'rebuild', ok: false, cancelled: true });
      return;
    }

    const ch = ops();
    ch.appendLine('\n=== Rebuild graph ===');
    ch.appendLine(`$ ${plan.command}`);
    ch.appendLine(`cwd:   ${plan.cwd}`);
    ch.appendLine(`domain: ${selectedDomain || 'All domains'}`);
    ch.appendLine(`scope: ${scopeLabel}`);
    const before = await this.countsSafe();
    const preLines = seedLineCount(plan.seedPath);
    const backup = backupSeed(plan.seedPath);
    if (plan.seedPath) ch.appendLine(`seed:  ${plan.seedPath} (${preLines} lines before)`);

    // Phase 1 — build the seed WITHOUT touching the live store (--no-runtime-
    // reload), so a bad/shrunken result is caught and rolled back before
    // anything is served.
    let res: AwgRunResult;
    try {
      res = await vscode.window.withProgress(
        { location: vscode.ProgressLocation.Notification, title: 'Awareness: rebuilding graph…' },
        () => runAwg([...plan.args, '--no-runtime-reload'], AWG_OP_TIMEOUT_MS)
      );
    } catch (err) {
      ch.appendLine('  ERROR: ' + errText(err));
      this.post({ type: 'refreshResult', mode: 'rebuild', ok: false, message: errText(err) });
      return;
    }
    if (res.stdout.trim()) ch.appendLine(indentLog(res.stdout));
    if (res.stderr.trim()) ch.appendLine(indentLog(res.stderr));
    if (!res.ok) {
      // sensei writes the seed only after validation passes, so a failure usually
      // left it untouched — restore from backup defensively all the same.
      if (restoreSeed(plan.seedPath, backup)) ch.appendLine('  restored previous seed (rebuild failed)');
      ch.appendLine(`  exit ${res.code} — rebuild failed`);
      this.post({ type: 'refreshResult', mode: 'rebuild', ok: false, stdout: res.stdout, stderr: res.stderr, message: commandFailureMessage(res) });
      return;
    }

    // Shrink guard — refuse to serve a seed that collapsed (the AG-only-clobber
    // signature). Restore the previous seed and do NOT reload the live store.
    const postLines = seedLineCount(plan.seedPath);
    if (plan.seedPath && preLines > 0 && postLines < preLines * SEED_SHRINK_GUARD) {
      const restored = restoreSeed(plan.seedPath, backup);
      const msg =
        `Rebuild produced a much smaller seed (${preLines} → ${postLines} lines) — refusing to reload the ` +
        `live store.${restored ? ' Previous seed restored.' : ''} This usually means the combined services ` +
        `graph was not included; set sensei.servicesRepoPath.`;
      ch.appendLine('  SHRINK GUARD TRIPPED: ' + msg);
      this.post({ type: 'refreshResult', mode: 'rebuild', ok: false, message: msg, guard: 'shrink', before: before?.counts });
      void vscode.window.showErrorMessage('Awareness: ' + msg);
      return;
    }

    // Phase 2 — seed is sane: reload the live store. The seed is already current
    // from phase 1, so this is a no-op write + Oxigraph PUT.
    let reloadOk = false;
    let reloadErr = '';
    try {
      const reload = await runAwg(plan.args, AWG_OP_TIMEOUT_MS);
      if (reload.stdout.trim()) ch.appendLine(indentLog(reload.stdout));
      if (reload.stderr.trim()) ch.appendLine(indentLog(reload.stderr));
      reloadOk = reload.ok;
      if (!reload.ok) {
        reloadErr = commandFailureMessage(reload);
        ch.appendLine('  reload WARN: ' + reloadErr);
      }
    } catch (err) {
      reloadErr = errText(err);
      ch.appendLine('  reload WARN: ' + reloadErr);
    }

    const diffStat = await awarenessDiffStat().catch(() => '');
    const after = await this.countsSafe();
    const authority = after ? assessMetadataAuthority(after.meta) : undefined;
    if (after) {
      this.post({ type: 'metadata', data: after.meta, localOps: this.localOpsPayload() });
    }
    ch.appendLine(
      `result: rebuilt (${preLines} → ${postLines} lines)` +
        (before && after ? `, ${before.counts.triples} → ${after.counts.triples} triples` : '')
    );
    this.post({
      type: 'refreshResult',
      mode: 'rebuild',
      ok: reloadOk && !!authority?.authoritative,
      authoritative: !!authority?.authoritative,
      authority,
      before: before?.counts,
      after: after?.counts,
      diffStat,
      reloaded: reloadOk,
      reloadWarning: reloadOk ? '' : reloadErr,
      command: plan.command,
      scope: plan.mode,
      message:
        !reloadOk
          ? reloadErr
          : authority && !authority.authoritative
            ? authority.detail || authority.summary
            : '',
    });
    if (reloadOk) {
      void vscode.window.showInformationMessage(
        'Awareness: graph rebuilt and reloaded. Review the git diff and commit when ready.'
      );
    } else {
      void vscode.window.showWarningMessage(
        'Awareness: graph rebuilt on disk, but live reload failed. The served graph may still be stale.'
      );
    }
  }

  private async handleList(cls: string): Promise<void> {
    const { addr, timeout } = this.cfg();
    const domain = await this.graphDomain();
    const resp = await queryByClass(addr, cls, 100, timeout, domain || undefined);
    this.post({ type: 'list', cls, rows: resp.rows ?? [], authority: resp.authority ?? null });
  }

  private async handleResolve(qid: string): Promise<void> {
    const { addr, timeout } = this.cfg();
    const { token, bare } = splitQualified(qid);
    if (!RESOLVABLE.has(token)) {
      // forbidden_fix / test: no resolve endpoint — not an error, just no detail.
      this.post({ type: 'detail', id: qid, found: false, unsupported: true, klass: token });
      return;
    }
    // Scope node detail to the same validated graph domain used by Metadata and lists.
    const scoped = await this.graphDomain();
    const res = await resolveNode(addr, token, bare, scoped, timeout);
    this.post({ type: 'detail', id: qid, found: !!res.found, node: res.node ?? null, authority: res.authority ?? null });
  }

  private async handleGraph(qid: string, label: string, depth: number): Promise<void> {
    const { addr, timeout } = this.cfg();
    const domain = await this.graphDomain();
    const nodes = new Map<string, GraphNode>();
    const edges: GraphEdge[] = [];
    const seenEdge = new Set<string>();
    let authority: unknown = null;

    const addNode = (id: string, level: number, row?: QueryRow): void => {
      const existing = nodes.get(id);
      if (existing) {
        existing.level = Math.min(existing.level, level);
        return;
      }
      const { token } = splitQualified(id);
      nodes.set(id, {
        id,
        token: row?.class || token,
        label: row?.label || splitQualified(id).bare,
        severity: row?.severity,
        uml_view: row?.uml_view,
        level,
      });
    };

    addNode(qid, 0);
    const center = nodes.get(qid)!;
    if (label) {
      center.label = label;
    }

    const expand = async (id: string, level: number): Promise<string[]> => {
      const resp = await queryRelated(addr, id, 48, timeout, domain || undefined);
      if (level === 1) {
        authority = resp.authority ?? null
      }
      const rows = resp.rows ?? [];
      const out: string[] = [];
      for (const r of rows) {
        if (!r.id) {
          continue;
        }
        addNode(r.id, level, r);
        const key = [id, r.id].sort().join('|');
        if (!seenEdge.has(key)) {
          seenEdge.add(key);
          edges.push({ from: id, to: r.id, relation: r.relation || 'related' });
        }
        out.push(r.id);
      }
      return out;
    };

    const level1 = await expand(qid, 1);
    if (depth >= 2) {
      // Cap fan-out so a depth-2 view stays legible rather than becoming a hairball.
      for (const id of level1.slice(0, 12)) {
        await expand(id, 2);
      }
    }

    this.post({
      type: 'graph',
      center: qid,
      depth,
      nodes: Array.from(nodes.values()),
      edges,
      authority,
    });
  }

  private async handleCandidates(): Promise<void> {
    const folders = vscode.workspace.workspaceFolders ?? [];
    const files: Array<{
      path: string;
      content: string;
      truncated: boolean;
      modified?: number;
      entries: CandidateEntry[];
    }> = [];
    this.candidateIndex.clear();
    for (const f of folders) {
      const dir = path.join(f.uri.fsPath, 'docs', 'awareness', 'candidates');
      let names: string[] = [];
      try {
        names = fs.readdirSync(dir);
      } catch {
        continue;
      }
      for (const name of names.sort()) {
        if (!name.endsWith('.yaml') && !name.endsWith('.yml')) {
          continue;
        }
        const full = path.join(dir, name);
        let content = '';
        let modified: number | undefined;
        try {
          content = fs.readFileSync(full, 'utf8');
          modified = fs.statSync(full).mtimeMs;
        } catch {
          continue;
        }
        const entries = parseCandidateEntries(content);
        for (const e of entries) {
          // Enrich each entry with its staged decision + promotion target, and
          // index where it lives for open/promote.
          e.decision = this.decisionFor(e.id);
          e.target = e.klass ? PROMOTE_TARGET[e.klass] : undefined;
          this.candidateIndex.set(e.id, {
            file: full,
            line: e.line ?? 0,
            label: e.label || e.id,
            klass: e.klass,
          });
        }
        const truncated = content.length > 20000;
        files.push({
          path: vscode.workspace.asRelativePath(full),
          content: truncated ? content.slice(0, 20000) : content,
          truncated,
          modified,
          entries,
        });
      }
    }
    // Guarded, documented promotion flow — shown, never executed by the UI.
    const commands = [
      { label: 'Validate candidates before promoting', cmd: 'sensei corpus validate' },
      { label: 'Promote a reviewed candidate by id', cmd: 'sensei promote <candidate-id>' },
      { label: 'Rebuild the graph after promotion', cmd: 'sensei rebuild' },
      { label: 'Verify the promoted rule actually anchors to files', cmd: 'sensei impact --file <path-it-should-protect>' },
    ];
    const candidateCount = files.reduce((n, f) => n + f.entries.length, 0);
    this.post({
      type: 'candidates',
      files,
      commands,
      localOps: { enabled: localOpsEnabled(), hasWorkspace: !!workspaceRoot() },
      capabilities: {
        hasWorkspace: !!workspaceRoot(),
        isAwgProject: isAwgProject(),
        awgAvailable: localOpsEnabled() ? await awgAvailable() : undefined,
        candidateCount,
      },
    });
  }

  // Preview a promotion without touching anything: `sensei promote <id> --dry-run`
  // validates the candidate and prints the canonical YAML it WOULD append.
  private async handleCandidatePreview(id: string): Promise<void> {
    let res: AwgRunResult;
    try {
      res = await runAwg(['promote', id, '--dry-run'], AWG_OP_TIMEOUT_MS);
    } catch (err) {
      this.post({ type: 'candidatePreview', id, ok: false, message: errText(err) });
      return;
    }
    this.post({
      type: 'candidatePreview',
      id,
      ok: res.ok,
      stdout: res.stdout,
      stderr: res.stderr,
      message: res.spawnError,
    });
  }

  // Promote for real, after an explicit user confirmation. Runs
  // `sensei promote --no-rebuild` in the working tree (validate → append canonical
  // YAML → remove candidate), then rebuilds through the same project-aware
  // rebuild plan the banner uses. This avoids clobbering the combined seed in
  // the awareness-graph repo with a plain single-repo rebuild.
  private async handleCandidatePromote(id: string, label: string): Promise<void> {
    if (!localOpsEnabled()) {
      this.post({ type: 'candidatePromote', id, ok: false, message: new LocalOpsDisabledError().message });
      return;
    }
    const plan = rebuildPlan();
    if (plan.mode === 'blocked') {
      const msg = plan.reason ?? 'Rebuild is unavailable for this workspace.';
      ops().appendLine('\n=== Promote candidate — BLOCKED ===\n  ' + msg);
      this.post({ type: 'candidatePromote', id, ok: false, message: msg });
      void vscode.window.showErrorMessage('Awareness: ' + msg);
      return;
    }
    const steps = candidatePromotePlan(id, plan);
    const choice = await vscode.window.showWarningMessage(
      `Promote candidate "${label || id}" into the graph?\n\n` +
        `This runs "${steps[0].command}", then "${steps[1].command}" in your workspace: ` +
        `it validates the candidate, appends it to the canonical awareness YAML, removes it from ` +
        `the queue, rebuilds the right graph shape for this repo, and reloads the served graph. ` +
        `Your files change but are NOT committed — you review the git diff and commit yourself.`,
      { modal: true },
      'Promote'
    );
    if (choice !== 'Promote') {
      this.post({ type: 'candidatePromote', id, ok: false, cancelled: true });
      return;
    }

    const ch = ops();
    ch.appendLine(`\n=== Promote candidate (${id}) ===`);
    ch.appendLine(`cwd: ${workspaceRoot()}`);
    const before = await this.countsSafe();

    let promote: AwgRunResult;
    try {
      promote = await vscode.window.withProgress(
        { location: vscode.ProgressLocation.Notification, title: `Awareness: promoting ${id}…` },
        async (progress) => {
          progress.report({ message: 'validate + stage candidate' });
          ch.appendLine(`$ ${steps[0].command}`);
          const promoteRes = await runAwg(steps[0].args, AWG_OP_TIMEOUT_MS);
          if (promoteRes.stdout.trim()) ch.appendLine(indentLog(promoteRes.stdout));
          if (promoteRes.stderr.trim()) ch.appendLine(indentLog(promoteRes.stderr));
          if (!promoteRes.ok) {
            return promoteRes;
          }
          progress.report({ message: 'rebuild graph' });
          ch.appendLine('$ ' + steps[1].command);
          const rebuildRes = await runAwg(steps[1].args, AWG_OP_TIMEOUT_MS);
          if (rebuildRes.stdout.trim()) ch.appendLine(indentLog(rebuildRes.stdout));
          if (rebuildRes.stderr.trim()) ch.appendLine(indentLog(rebuildRes.stderr));
          if (!rebuildRes.ok) {
            return {
              ...rebuildRes,
              stdout:
                [promoteRes.stdout.trim(), rebuildRes.stdout.trim()].filter(Boolean).join('\n\n'),
              stderr:
                [promoteRes.stderr.trim(), rebuildRes.stderr.trim()].filter(Boolean).join('\n\n'),
            };
          }
          return {
            ...rebuildRes,
            stdout:
              [promoteRes.stdout.trim(), rebuildRes.stdout.trim()].filter(Boolean).join('\n\n'),
            stderr:
              [promoteRes.stderr.trim(), rebuildRes.stderr.trim()].filter(Boolean).join('\n\n'),
          };
        }
      );
    } catch (err) {
      this.post({ type: 'candidatePromote', id, ok: false, message: errText(err) });
      return;
    }

    if (!promote.ok) {
      ch.appendLine(`  promote/rebuild failed: ${promote.spawnError || `exit ${promote.code}`}`);
      this.post({
        type: 'candidatePromote',
        id,
        ok: false,
        stdout: promote.stdout,
        stderr: promote.stderr,
        message: promote.spawnError,
      });
      return;
    }

    const diffStat = await awarenessDiffStat().catch(() => '');
    // Reload Metadata so the banner/score reflect the just-rebuilt graph.
    let meta: MetadataResponse | null = null;
    let reloadUnavailable = false
    let promoteDomain = ''
    try {
      const { addr, timeout } = this.cfg();
      promoteDomain = await this.graphDomain();
      meta = await metadata(addr, timeout, promoteDomain || undefined);
    } catch {
      // The seed file is rebuilt on disk, but the authority backend is down or
      // unreachable — do not report clean authority.
      reloadUnavailable = true
    }
    const authority = meta ? assessMetadataAuthority(meta) : undefined;
    if (meta) {
      this.post({ type: 'metadata', data: meta, activeDomain: promoteDomain, localOps: this.localOpsPayload() });
    }
    const after = meta
      ? {
          triples: Number(meta.triple_count || 0),
          invariants: Number(meta.invariant_count || 0),
          tests: Number(meta.required_test_count || 0),
          files: Number(meta.source_file_count || 0),
          intents: Number(meta.intent_count || 0),
        }
      : undefined;
    this.post({
      type: 'candidatePromote',
      id,
      ok: !!authority?.authoritative,
      stdout: promote.stdout,
      diffStat,
      before: before?.counts,
      after,
      reloaded: !!meta,
      authoritative: !!authority?.authoritative,
      authority: authority ?? (reloadUnavailable ? { state: 'down', summary: 'authority backend unreachable', detail: 'The seed changed on disk, but the dashboard could not reload graph metadata because the backend was unreachable.' } : undefined),
      unreachable: reloadUnavailable,
      message: reloadUnavailable
        ? 'The seed changed on disk, but the dashboard could not reload graph metadata because the backend was unreachable.'
        : authority
        ? authority.authoritative ? '' : authority.detail || authority.summary
        : 'Seed rebuilt on disk, but the served graph could not be verified.',
    });
    if (authority?.authoritative) {
      void vscode.window.showInformationMessage(
        `Awareness: promoted ${id}. Review the git diff and commit when ready.`
      );
    } else if (reloadUnavailable) {
      void vscode.window.showWarningMessage(
        `Awareness: promoted ${id}, but the authority backend is unreachable.`
      );
    } else {
      void vscode.window.showWarningMessage(
        `Awareness: promoted ${id}, but the served graph is not authoritative yet.`
      );
    }
  }

  // Approve/reject is a local staging decision in workspaceState — it does NOT
  // touch files or the graph. The auditable record is the promotion git diff.
  private async handleDecision(id: string, d: ReviewDecision | undefined): Promise<void> {
    await this.setDecision(id, d);
    await this.handleCandidates(); // re-post so badges + the approved count update
  }

  // Edit = open the candidate file at its entry. No in-dashboard form editor.
  private async handleCandidateOpen(id: string): Promise<void> {
    const entry = this.candidateIndex.get(id);
    if (!entry) {
      void vscode.window.showWarningMessage(`Awareness: candidate "${id}" not found in the queue.`);
      return;
    }
    try {
      const doc = await vscode.workspace.openTextDocument(entry.file);
      const editor = await vscode.window.showTextDocument(doc, vscode.ViewColumn.Beside);
      const pos = new vscode.Position(Math.max(0, entry.line), 0);
      editor.selection = new vscode.Selection(pos, pos);
      editor.revealRange(new vscode.Range(pos, pos), vscode.TextEditorRevealType.InCenter);
    } catch {
      void vscode.window.showWarningMessage(`Awareness: could not open the candidate file for "${id}".`);
    }
  }

  // Batch-promote every approved candidate through the guarded path: each
  // `sensei promote <id> --no-rebuild` (validate → append canonical YAML → remove
  // from queue), then ONE `sensei rebuild`, then reload metadata for before/after
  // counts. Stops on the first validation failure and reports it. The graph
  // only changes through the deterministic rebuild; the user commits the diff.
  private async handlePromoteApproved(): Promise<void> {
    if (!localOpsEnabled()) {
      this.post({ type: 'promoteApproved', ok: false, message: new LocalOpsDisabledError().message });
      return;
    }
    const approved = [...this.candidateIndex.keys()].filter((id) => this.decisionFor(id) === 'approved');
    if (approved.length === 0) {
      void vscode.window.showInformationMessage('Awareness: no approved candidates to promote.');
      this.post({ type: 'promoteApproved', ok: false, message: 'No approved candidates.' });
      return;
    }
    const summary = approved.map((id) => '  • ' + (this.candidateIndex.get(id)?.label || id)).join('\n');
    const choice = await vscode.window.showWarningMessage(
      `Promote ${approved.length} approved candidate(s) into the graph?\n\n${summary}\n\n` +
        `Each runs "sensei promote <id>" (validate → append canonical YAML → remove from queue), then one ` +
        `"sensei rebuild". Files change but are NOT committed — review the git diff and commit. Stops on the ` +
        `first validation failure.`,
      { modal: true },
      'Promote approved'
    );
    if (choice !== 'Promote approved') {
      this.post({ type: 'promoteApproved', ok: false, cancelled: true });
      return;
    }

    // Resolve the project-aware rebuild BEFORE promoting, so we never append to
    // the canonical YAML and then clobber the combined seed (or fail to rebuild
    // at all). Same plan the Rebuild button uses.
    const plan = rebuildPlan();
    if (plan.mode === 'blocked') {
      const msg = plan.reason ?? 'Rebuild is unavailable for this workspace.';
      ops().appendLine('\n=== Promote approved — BLOCKED ===\n  ' + msg);
      this.post({ type: 'promoteApproved', ok: false, message: msg });
      void vscode.window.showErrorMessage('Awareness: ' + msg);
      return;
    }

    const ch = ops();
    ch.show(true);
    ch.appendLine(`\n=== Promote approved (${approved.length}) ===`);
    ch.appendLine(`cwd: ${workspaceRoot()}`);

    const before = await this.countsSafe();
    const promoted: string[] = [];
    let failure: { id: string; output: string } | undefined;

    await vscode.window.withProgress(
      { location: vscode.ProgressLocation.Notification, title: 'Awareness: promoting approved candidates…' },
      async (progress) => {
        for (const id of approved) {
          progress.report({ message: id });
          ch.appendLine(`$ sensei promote ${id} --no-rebuild`);
          let res: AwgRunResult;
          try {
            res = await runAwg(['promote', id, '--no-rebuild'], AWG_OP_TIMEOUT_MS);
          } catch (err) {
            failure = { id, output: errText(err) };
            ch.appendLine('  ERROR: ' + errText(err));
            break;
          }
          if (res.stdout.trim()) ch.appendLine(indentLog(res.stdout));
          if (res.stderr.trim()) ch.appendLine(indentLog(res.stderr));
          if (!res.ok) {
            failure = { id, output: res.stderr || res.spawnError || res.stdout || 'validation failed' };
            ch.appendLine(`  exit ${res.code} — stopping (validation/promote failure)`);
            break;
          }
          promoted.push(id);
          ch.appendLine(`  ✓ promoted ${id}`);
        }
        if (!failure && promoted.length) {
          progress.report({ message: 'rebuild' });
          ch.appendLine('$ ' + plan.command);
          let rb: AwgRunResult;
          try {
            rb = await runAwg(plan.args, AWG_OP_TIMEOUT_MS);
          } catch (err) {
            failure = { id: '(rebuild)', output: errText(err) };
            ch.appendLine('  ERROR: ' + errText(err));
            return;
          }
          if (rb.stdout.trim()) ch.appendLine(indentLog(rb.stdout));
          if (rb.stderr.trim()) ch.appendLine(indentLog(rb.stderr));
          if (!rb.ok) {
            failure = { id: '(rebuild)', output: rb.stderr || rb.spawnError || rb.stdout || 'rebuild failed' };
            ch.appendLine(`  exit ${rb.code} — rebuild failed`);
          }
        }
      }
    );

    // Successfully promoted candidates are gone from the queue — clear their
    // staged decision so a stale "approved" can't linger.
    for (const id of promoted) {
      await this.setDecision(id, undefined);
    }

    const diffStat = await awarenessDiffStat().catch(() => '');
    const after = await this.countsSafe();
    const backendDown = !after && !failure
    const authority = after ? assessMetadataAuthority(after.meta) : undefined;
    if (after) {
      this.post({ type: 'metadata', data: after.meta, localOps: this.localOpsPayload() });
    }
    ch.appendLine(`result: promoted ${promoted.length}/${approved.length}${failure ? `, FAILED at ${failure.id}` : ''}`);

    this.post({
      type: 'promoteApproved',
      ok: !failure && !!authority?.authoritative,
      promoted,
      failedId: failure?.id,
      error: failure?.output,
      before: before?.counts,
      after: after?.counts,
      diffStat,
      reloaded: !!after,
      authoritative: !!authority?.authoritative,
      authority: authority ?? (backendDown ? { state: 'down', summary: 'authority backend unreachable', detail: 'The rebuild finished, but the dashboard could not reload graph metadata because the backend was unreachable.' } : undefined),
      unreachable: backendDown,
      message: failure
        ? failure.output
        : backendDown
          ? 'The rebuild finished, but the dashboard could not reload graph metadata because the backend was unreachable.'
        : authority
          ? authority.authoritative ? '' : authority.detail || authority.summary
          : 'Seed rebuilt on disk, but the served graph could not be verified.',
    });
    await this.handleCandidates(); // queue shrank — refresh

    if (failure) {
      void vscode.window.showErrorMessage(
        `Awareness: promotion stopped at ${failure.id} (${promoted.length} promoted before). See the Awareness Operations log.`
      );
    } else if (backendDown) {
      void vscode.window.showWarningMessage(
        `Awareness: promoted ${promoted.length} candidate(s), but the authority backend is unreachable.`
      );
    } else if (!authority?.authoritative) {
      void vscode.window.showWarningMessage(
        `Awareness: promoted ${promoted.length} candidate(s), but the served graph is not authoritative yet.`
      );
    } else {
      void vscode.window.showInformationMessage(
        `Awareness: promoted ${promoted.length} candidate(s). Review the git diff and commit when ready.`
      );
    }
  }

  // Fetch metadata and reduce it to the counts the operation summary compares.
  // Returns undefined if the server is unreachable (rebuild still happened on disk).
  private async countsSafe(): Promise<{ meta: MetadataResponse; counts: Record<string, number> } | undefined> {
    try {
      const { addr, timeout } = this.cfg();
      const domain = await this.graphDomain();
      const m = await metadata(addr, timeout, domain || undefined);
      return {
        meta: m,
        counts: {
          triples: Number(m.triple_count || 0),
          invariants: Number(m.invariant_count || 0),
          tests: Number(m.required_test_count || 0),
          files: Number(m.source_file_count || 0),
          intents: Number(m.intent_count || 0),
        },
      };
    } catch {
      return undefined;
    }
  }

  // Scan the codebase for extractable knowledge, deterministically (echo
  // drafter, no LLM, no key, no cost). Dry-run: report only, nothing written.
  private async handleCandidateScan(): Promise<void> {
    let res: AwgRunResult;
    try {
      res = await vscode.window.withProgress(
        { location: vscode.ProgressLocation.Notification, title: 'Awareness: scanning codebase…' },
        () => runAwg(SCAN_ARGS, AWG_OP_TIMEOUT_MS)
      );
    } catch (err) {
      this.post({ type: 'candidateScan', ok: false, message: errText(err) });
      return;
    }
    this.post({
      type: 'candidateScan',
      ok: res.ok,
      stdout: res.stdout,
      stderr: res.stderr,
      message: res.spawnError,
    });
  }

  // Apply the scan: same echo extraction, but --apply writes grounded intents
  // (>=0.80 → docs/awareness/intent_*.yaml) and parks the rest under
  // candidates/ for review. Writes the working tree, NOT the graph — surfaced as
  // a git diff the user commits. Then refresh the candidate queue.
  private async handleCandidateScanApply(): Promise<void> {
    if (!localOpsEnabled()) {
      this.post({ type: 'candidateScanApply', ok: false, message: new LocalOpsDisabledError().message });
      return;
    }
    const choice = await vscode.window.showWarningMessage(
      'Apply scan results to the candidate queue?\n\n' +
        'This runs "sensei intent-mine --apply" in your workspace: it writes grounded intents ' +
        '(≥0.80 → docs/awareness/intent_*.yaml) and parks weaker proposals + findings under ' +
        'docs/awareness/candidates/ for review. Your files change but are NOT committed — you ' +
        'review the git diff and commit yourself. Nothing reaches the graph until you rebuild.',
      { modal: true },
      'Apply'
    );
    if (choice !== 'Apply') {
      this.post({ type: 'candidateScanApply', ok: false, cancelled: true });
      return;
    }
    const ch = ops();
    ch.appendLine('\n=== Scan apply (sensei intent-mine --apply) ===');
    ch.appendLine(`cwd: ${workspaceRoot()}`);
    let res: AwgRunResult;
    try {
      res = await vscode.window.withProgress(
        { location: vscode.ProgressLocation.Notification, title: 'Awareness: applying scan results…' },
        () => runAwg([...SCAN_ARGS, '--apply'], AWG_OP_TIMEOUT_MS)
      );
    } catch (err) {
      ch.appendLine('  ERROR: ' + errText(err));
      this.post({ type: 'candidateScanApply', ok: false, message: errText(err) });
      return;
    }
    if (res.stdout.trim()) ch.appendLine(indentLog(res.stdout));
    if (res.stderr.trim()) ch.appendLine(indentLog(res.stderr));
    if (!res.ok) {
      this.post({
        type: 'candidateScanApply',
        ok: false,
        stdout: res.stdout,
        stderr: res.stderr,
        message: res.spawnError,
      });
      return;
    }
    const diffStat = await awarenessDiffStat().catch(() => '');
    this.post({ type: 'candidateScanApply', ok: true, stdout: res.stdout, diffStat });
    void vscode.window.showInformationMessage(
      'Awareness: scan applied. Review the git diff and the candidate queue, then commit when ready.'
    );
  }

  private async handleOpenAnchor(file: string, line: number): Promise<void> {
    const folder =
      vscode.window.activeTextEditor &&
      vscode.workspace.getWorkspaceFolder(vscode.window.activeTextEditor.document.uri);
    const root = folder?.uri ?? vscode.workspace.workspaceFolders?.[0]?.uri;
    if (!root) {
      void vscode.window.showWarningMessage(`Awareness: no workspace to resolve "${file}".`);
      return;
    }
    const target = vscode.Uri.joinPath(root, file);
    try {
      const doc = await vscode.workspace.openTextDocument(target);
      const editor = await vscode.window.showTextDocument(doc, vscode.ViewColumn.Beside);
      const pos = new vscode.Position(Math.max(0, line), 0);
      editor.selection = new vscode.Selection(pos, pos);
      editor.revealRange(new vscode.Range(pos, pos), vscode.TextEditorRevealType.InCenter);
    } catch {
      void vscode.window.showWarningMessage(`Awareness: could not open "${file}".`);
    }
  }

  private configOrState(key: string): string {
    const c = vscode.workspace.getConfiguration('sensei');
    return (this.state.get<string>(key) || c.get<string>(key, '') || '').trim();
  }

  private workspacePath(input: string): string | undefined {
    const root = workspaceRoot();
    if (!root || !input.trim()) {
      return undefined;
    }
    const abs = path.isAbsolute(input) ? input : path.resolve(root, input);
    const rel = path.relative(root, abs);
    if (rel.startsWith('..') || path.isAbsolute(rel)) {
      return undefined;
    }
    return abs;
  }

  private artifact(kind: ControlKind, configured: string): ControlArtifact {
    const file = this.workspacePath(configured);
    if (!configured || !file) {
      return {
        kind,
        configured,
        exists: false,
        valid: false,
        summary: {},
        error: configured ? 'Path is outside the workspace.' : 'No artifact selected.',
      };
    }
    try {
      const content = fs.readFileSync(file, 'utf8');
      const stat = fs.statSync(file);
      return {
        kind,
        configured,
        path: vscode.workspace.asRelativePath(file),
        exists: true,
        valid: true,
        digest: crypto.createHash('sha256').update(content).digest('hex'),
        modified: stat.mtimeMs,
        summary: summarizeControlArtifact(kind, content),
      };
    } catch (err) {
      return {
        kind,
        configured,
        path: vscode.workspace.asRelativePath(file),
        exists: false,
        valid: false,
        summary: {},
        error: errText(err),
      };
    }
  }

  private watchActiveTaskFiles(): void {
    const root = workspaceRoot();
    if (!root) {
      return;
    }
    const watcher = vscode.workspace.createFileSystemWatcher(
      new vscode.RelativePattern(root, path.join('.sensei', 'tasks', '**'))
    );
    const refresh = (): void => {
      void this.handleControlState();
    };
    watcher.onDidChange(refresh, null, this.disposables);
    watcher.onDidCreate(refresh, null, this.disposables);
    watcher.onDidDelete(refresh, null, this.disposables);
    this.disposables.push(watcher);
  }

  private activeFileRelative(): string | undefined {
    const root = workspaceRoot();
    const file = vscode.window.activeTextEditor?.document.uri.fsPath;
    if (!root || !file) {
      return undefined;
    }
    const rel = path.relative(root, file);
    if (rel.startsWith('..') || path.isAbsolute(rel)) {
      return undefined;
    }
    return rel.split(path.sep).join('/');
  }

  private async handleControlState(): Promise<void> {
    const artifacts = Object.fromEntries(
      (Object.keys(CONTROL_KEYS) as ControlKind[]).map((kind) => [
        kind,
        this.artifact(kind, this.configOrState(CONTROL_KEYS[kind])),
      ])
    );
    let activeTask = loadActiveTask(workspaceRoot(), this.activeFileRelative());
    if (activeTask.kind !== 'none') {
      const res = await runAwgReadOnly(['task-status', '--active', '--compact', '--format', 'json'], AWG_STATUS_TIMEOUT_MS);
      activeTask = mergeTaskStatusJson(activeTask, res.ok ? res.stdout : '', res.ok ? '' : commandFailureMessage(res));
    }
    const graphDomain = await this.graphDomain();
    this.post({
      type: 'controlState',
      artifacts,
      activeTask,
      graphDomain,
      taskScopeMatches: taskMatchesGraphDomain(activeTask.repositoryDomain, graphDomain),
      localOps: { enabled: localOpsEnabled(), hasWorkspace: !!workspaceRoot() },
    });
  }

  private async handleSelectControlArtifact(kind: ControlKind): Promise<void> {
    if (!CONTROL_KEYS[kind]) {
      return;
    }
    await selectControlArtifactInState(this.state, kind);
    await this.handleControlState();
  }

  private async handleClearControlSelection(): Promise<void> {
    for (const key of Object.values(CONTROL_KEYS)) {
      await this.state.update(key, undefined);
    }
    await this.handleControlState();
  }

  private async handleControlStatus(kind: ControlKind): Promise<void> {
    const closure = this.artifact('closure', this.configOrState(CONTROL_KEYS.closure));
    const convergence = this.artifact('convergence', this.configOrState(CONTROL_KEYS.convergence));
    const admission = this.artifact('admission', this.configOrState(CONTROL_KEYS.admission));
    const verification = this.artifact('verification', this.configOrState(CONTROL_KEYS.verification));
    const rel = (a: ControlArtifact): string | undefined => a.exists && a.path ? a.path : undefined;
    let args: string[] | undefined;
    if (kind === 'convergence' && rel(convergence)) {
      args = ['convergence-status', '--session', rel(convergence)!, '--format', 'yaml'];
    } else if ((kind === 'admission' || kind === 'verification') && rel(admission)) {
      args = ['admission-status', '--decision', rel(admission)!];
      if (rel(verification)) {
        args.push('--verification', rel(verification)!);
      }
    } else if (kind === 'closure' && rel(closure)) {
      this.post({ type: 'controlStatus', kind, ok: true, stdout: formatArtifactSummary(closure) });
      return;
    }
    if (!args) {
      this.post({ type: 'controlStatus', kind, ok: false, message: 'Required artifact is not selected.' });
      return;
    }
    const res = await runAwgReadOnly(args, AWG_STATUS_TIMEOUT_MS);
    this.post({
      type: 'controlStatus',
      kind,
      ok: res.ok,
      stdout: res.stdout,
      stderr: res.stderr,
      message: commandFailureMessage(res),
    });
  }

  private html(extensionUri: vscode.Uri, webview: vscode.Webview): string {
    const nonce = crypto.randomBytes(16).toString('hex');
    const cssUri = webview.asWebviewUri(
      vscode.Uri.joinPath(extensionUri, 'media', 'dashboard.css')
    );
    const jsUri = webview.asWebviewUri(
      vscode.Uri.joinPath(extensionUri, 'media', 'dashboard.js')
    );
    const cpFmtUri = webview.asWebviewUri(
      vscode.Uri.joinPath(extensionUri, 'media', 'controlPanelFmt.js')
    );
    const cpUri = webview.asWebviewUri(
      vscode.Uri.joinPath(extensionUri, 'media', 'controlPanel.js')
    );
    const cpMutUri = webview.asWebviewUri(
      vscode.Uri.joinPath(extensionUri, 'media', 'controlPanelMutation.js')
    );
    const csp = [
      `default-src 'none'`,
      `img-src ${webview.cspSource} data:`,
      `style-src ${webview.cspSource} 'unsafe-inline'`,
      `script-src 'nonce-${nonce}'`,
    ].join('; ');

    return /* html */ `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8" />
  <meta http-equiv="Content-Security-Policy" content="${csp}" />
  <meta name="viewport" content="width=device-width, initial-scale=1.0" />
  <link href="${cssUri}" rel="stylesheet" />
  <title>Project Awareness</title>
</head>
<body>
  <div class="cp-modebar">
    <strong class="cp-brand">Sensei — architecture control panel</strong>
    <span class="cp-mode-toggle" role="tablist" aria-label="View">
      <button id="cpModeControl" class="cp-modebtn cp-modebtn--on" role="tab" aria-pressed="true">Control panel</button>
      <button id="cpModeLegacy" class="cp-modebtn" role="tab" aria-pressed="false">Legacy explorer</button>
    </span>
  </div>

  <div id="controlPanel" class="cp">
    <div id="cpLive" class="cp-sr-only" role="status" aria-live="polite"></div>
    <div id="cpTopStrip" class="cp-topstrip" aria-label="Repository posture"></div>
    <div class="cp-body">
      <nav id="cpRail" class="cp-rail" aria-label="Ontology navigation"></nav>
      <section class="cp-center">
        <div id="cpChips" class="cp-chips" role="toolbar" aria-label="Attention and artifact filters"></div>
        <div id="cpList" class="cp-list" role="list" aria-live="polite"></div>
        <div id="cpPager" class="cp-pager"></div>
      </section>
      <aside id="cpHeader" class="cp-header-pane" aria-label="Selected artifact header"></aside>
    </div>
  </div>

  <div id="legacyView" hidden>
  <header id="banner" class="banner banner--loading">
    <div class="banner__title">Project Awareness</div>
    <div class="banner__state" id="bannerState">loading…</div>
    <div class="banner__actions" id="bannerActions"></div>
    <div class="banner__stats" id="bannerStats"></div>
    <div class="banner__meta" id="bannerMeta"></div>
  </header>

  <div id="phaseRail" class="phase-rail">
    <button class="phase-btn phase-btn--on" data-phase="awareness">
      <span>Phase 1</span><b>Project Awareness</b>
    </button>
    <button class="phase-btn" data-phase="control">
      <span>Phase 2</span><b>Closure &amp; Control</b>
    </button>
  </div>

  <nav id="nav" class="nav"></nav>

  <div id="viewFilter" class="viewbar" title="Filter by UML view"></div>

  <main class="main">
    <section class="pane pane--list">
      <div class="pane__head">
        <input id="search" class="search" type="text" placeholder="Filter…" />
        <span id="listCount" class="muted"></span>
      </div>
      <div id="list" class="list"></div>
    </section>

    <section class="pane pane--detail">
      <div id="detail" class="detail">
        <p class="muted">Select a concern to inspect its reasoning chain.</p>
      </div>
      <div class="graph-wrap">
        <div class="graph-toolbar">
          <span class="graph-title">Focus graph</span>
          <span class="depth-toggle" id="depthToggle">
            depth <button data-depth="1" class="depth depth--on">1</button><button data-depth="2" class="depth">2</button>
          </span>
          <button id="graphReset" class="btn-mini" title="Reset view">reset</button>
        </div>
        <svg id="graph" class="graph" preserveAspectRatio="xMidYMid meet"></svg>
        <div id="tooltip" class="tooltip" hidden></div>
      </div>
    </section>
  </main>
  </div>

  <footer class="footer">
    This view is a client of the <a href="https://github.com/globulario/sensei">Sensei CLI</a> — it reads the awareness graph that <code>sensei serve</code> hosts.
    Install it: <code>brew install globulario/tap/sensei</code> · <code>winget install Globulario.Sensei</code> · <code>curl -fsSL https://raw.githubusercontent.com/globulario/sensei/main/install.sh | sh</code>, then run <code>sensei serve</code>.
  </footer>

  <script nonce="${nonce}" src="${cpFmtUri}"></script>
  <script nonce="${nonce}" src="${cpMutUri}"></script>
  <script nonce="${nonce}" src="${cpUri}"></script>
  <script nonce="${nonce}" src="${jsUri}"></script>
</body>
</html>`;
  }
}
