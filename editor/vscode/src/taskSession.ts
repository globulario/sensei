// SPDX-License-Identifier: AGPL-3.0-only

import * as crypto from 'crypto';
import * as fs from 'fs';
import * as path from 'path';

export type ActiveTaskKind = 'none' | 'active' | 'stale' | 'invalid';
export type EnvelopeState = 'none' | 'modify' | 'read' | 'outside' | 'untrusted';

export interface ActiveTaskNextAction {
  action: string;
  reference?: string;
  summary?: string;
}

export interface ActiveTaskPrimaryBlocker {
  id: string;
  groupId?: string;
  statement: string;
  consequence?: string;
}

export interface ActiveTaskPrimaryQuestion {
  id: string;
  questionText: string;
  resolutionClass?: string;
}

export interface ActiveTaskEvidenceProgress {
  total: number;
  eligible: number;
  completed: number;
  inconclusive: number;
  failed: number;
  rejected: number;
  unavailable: number;
}

export interface ActiveTaskArtifactRefs {
  task_request?: string;
  closure_request?: string;
  claims?: string;
  dialogue?: string;
  evidence_state?: string;
  graph_snapshot?: string;
  graph_receipt?: string;
  convergence_bundle?: string;
  convergence_session?: string;
  admission_request?: string;
  admission_decision?: string;
  admission_verification?: string;
  prepare_receipt?: string;
  status_receipt?: string;
}

export interface ActiveTaskCounts {
  claims: number;
  questions: number;
  answers: number;
  probes: number;
}

export interface ActiveFileAdmission {
  relativePath?: string;
  state: EnvelopeState;
  label: string;
}

export interface ActiveTaskState {
  kind: ActiveTaskKind;
  taskId?: string;
  taskDir?: string;
  sessionPath?: string;
  sessionDigest?: string;
  pointerDigest?: string;
  pointerPath?: string;
  description?: string;
  repositoryDomain?: string;
  revision?: string;
  graphDigest?: string;
  bindingHealth?: string;
  phase?: string;
  status?: string;
  closure?: string;
  convergence?: string;
  admission?: string;
  inspect?: string;
  modify?: string;
  waitingOn: string[];
  readEnvelope: string[];
  modifyEnvelope: string[];
  next?: ActiveTaskNextAction;
  primaryBlocker?: ActiveTaskPrimaryBlocker;
  primaryQuestion?: ActiveTaskPrimaryQuestion;
  automaticEvidence?: ActiveTaskEvidenceProgress;
  timelineReceiptIds: string[];
  artifacts: ActiveTaskArtifactRefs;
  artifactHealth: Array<{ name: string; path: string; exists: boolean; digest?: string; error?: string }>;
  counts: ActiveTaskCounts;
  activeFile: ActiveFileAdmission;
  verified?: boolean;
  verifyErrors: string[];
  errors: string[];
}

const EMPTY_COUNTS: ActiveTaskCounts = { claims: 0, questions: 0, answers: 0, probes: 0 };

function emptyState(kind: ActiveTaskKind, label: string): ActiveTaskState {
  return {
    kind,
    waitingOn: [],
    readEnvelope: [],
    modifyEnvelope: [],
    artifacts: {},
    artifactHealth: [],
    counts: { ...EMPTY_COUNTS },
    activeFile: { state: kind === 'none' ? 'none' : 'untrusted', label },
    timelineReceiptIds: [],
    verifyErrors: [],
    errors: kind === 'none' ? [] : [label],
  };
}

function readText(file: string): string {
  return fs.readFileSync(file, 'utf8');
}

function scalar(content: string, key: string): string {
  const re = new RegExp(`^\\s*${key}:\\s*(.*?)\\s*$`, 'm');
  const raw = re.exec(content)?.[1] ?? '';
  if (!raw || raw === 'null') {
    return '';
  }
  return raw.replace(/\s+#.*$/, '').replace(/^["']|["']$/g, '').trim();
}

function mapScalars(content: string, key: string): Record<string, string> {
  const lines = content.split(/\r?\n/);
  const start = lines.findIndex((line) => new RegExp(`^(\\s*)${key}:\\s*$`).test(line));
  if (start < 0) {
    return {};
  }
  const parentIndent = lines[start].length - lines[start].replace(/^\s+/, '').length;
  const out: Record<string, string> = {};
  for (let i = start + 1; i < lines.length; i++) {
    const line = lines[i];
    if (!line.trim()) {
      continue;
    }
    const indent = line.length - line.replace(/^\s+/, '').length;
    if (indent <= parentIndent) {
      break;
    }
    const m = /^\s*([A-Za-z0-9_]+):\s*(.*?)\s*$/.exec(line);
    if (m) {
      out[m[1]] = m[2].replace(/\s+#.*$/, '').replace(/^["']|["']$/g, '').trim();
    }
  }
  return out;
}

function sequenceScalars(content: string, key: string): string[] {
  const lines = content.split(/\r?\n/);
  const start = lines.findIndex((line) => new RegExp(`^(\\s*)${key}:\\s*$`).test(line));
  if (start < 0) {
    return [];
  }
  const parentIndent = lines[start].length - lines[start].replace(/^\s+/, '').length;
  const out: string[] = [];
  for (let i = start + 1; i < lines.length; i++) {
    const line = lines[i];
    if (!line.trim()) {
      continue;
    }
    const indent = line.length - line.replace(/^\s+/, '').length;
    if (indent <= parentIndent) {
      break;
    }
    const m = /^\s*-\s*(.*?)\s*$/.exec(line);
    if (m) {
      const v = m[1].replace(/\s+#.*$/, '').replace(/^["']|["']$/g, '').trim();
      if (v) {
        out.push(v);
      }
    }
  }
  return out;
}

function firstMapInSequence(content: string, key: string): ActiveTaskNextAction | undefined {
  const lines = content.split(/\r?\n/);
  const start = lines.findIndex((line) => new RegExp(`^(\\s*)${key}:\\s*$`).test(line));
  if (start < 0) {
    return undefined;
  }
  const parentIndent = lines[start].length - lines[start].replace(/^\s+/, '').length;
  const out: Record<string, string> = {};
  let inside = false;
  for (let i = start + 1; i < lines.length; i++) {
    const line = lines[i];
    if (!line.trim()) {
      continue;
    }
    const indent = line.length - line.replace(/^\s+/, '').length;
    if (indent <= parentIndent) {
      break;
    }
    const first = /^\s*-\s*([A-Za-z0-9_]+):\s*(.*?)\s*$/.exec(line);
    if (first) {
      if (inside) {
        break;
      }
      inside = true;
      out[first[1]] = unquote(first[2]);
      continue;
    }
    if (inside) {
      const m = /^\s*([A-Za-z0-9_]+):\s*(.*?)\s*$/.exec(line);
      if (m) {
        out[m[1]] = unquote(m[2]);
      }
    }
  }
  return out.action ? { action: out.action, reference: out.reference, summary: out.summary } : undefined;
}

function unquote(v: string): string {
  return v.replace(/\s+#.*$/, '').replace(/^["']|["']$/g, '').trim();
}

function resolveInside(root: string, rel: string): string | undefined {
  const abs = path.resolve(root, rel);
  const back = path.relative(root, abs);
  if (back.startsWith('..') || path.isAbsolute(back)) {
    return undefined;
  }
  return abs;
}

function sha256(data: string | Buffer): string {
  return crypto.createHash('sha256').update(data).digest('hex');
}

function countItemsInSection(content: string, key: string): number {
  const lines = content.split(/\r?\n/);
  const start = lines.findIndex((line) => new RegExp(`^(\\s*)${key}:\\s*$`).test(line));
  if (start < 0) {
    return 0;
  }
  const parentIndent = lines[start].length - lines[start].replace(/^\s+/, '').length;
  const itemIndents: number[] = [];
  for (let i = start + 1; i < lines.length; i++) {
    const line = lines[i];
    if (!line.trim()) {
      continue;
    }
    const indent = line.length - line.replace(/^\s+/, '').length;
    if (indent <= parentIndent) {
      break;
    }
    if (/^\s*-\s+id:\s*/.test(line)) {
      itemIndents.push(indent);
    }
  }
  if (!itemIndents.length) {
    return 0;
  }
  const directItemIndent = Math.min(...itemIndents);
  return itemIndents.filter((indent) => indent === directItemIndent).length;
}

function countArtifacts(root: string, taskDir: string, artifacts: ActiveTaskArtifactRefs): ActiveTaskCounts {
  const readArtifact = (rel?: string): string => {
    if (!rel) {
      return '';
    }
    const abs = resolveInside(root, path.join(taskDir, rel));
    if (!abs || !fs.existsSync(abs)) {
      return '';
    }
    return readText(abs);
  };
  const claims = readArtifact(artifacts.claims);
  const latestDialoguePath = path.join(taskDir, 'convergence', 'latest', 'dialogue.yaml');
  const latestDialogueAbs = resolveInside(root, latestDialoguePath);
  const dialogue = latestDialogueAbs && fs.existsSync(latestDialogueAbs)
    ? readText(latestDialogueAbs)
    : readArtifact(artifacts.dialogue);
  const probePath = path.join(taskDir, 'convergence', 'latest', 'probes.yaml');
  const probesAbs = resolveInside(root, probePath);
  const probes = probesAbs && fs.existsSync(probesAbs) ? readText(probesAbs) : '';
  return {
    claims: countItemsInSection(claims, 'claims'),
    questions: countItemsInSection(dialogue, 'open_questions'),
    answers: countItemsInSection(dialogue, 'architect_answers'),
    probes: countItemsInSection(probes, 'probes') || countItemsInSection(probes, 'evidence_probes'),
  };
}

export function taskMatchesGraphDomain(taskDomain: string | undefined, graphDomain: string): boolean {
  return !graphDomain || !taskDomain || taskDomain === graphDomain;
}

function artifactHealth(root: string, taskDir: string, refs: ActiveTaskArtifactRefs): ActiveTaskState['artifactHealth'] {
  const out: ActiveTaskState['artifactHealth'] = [];
  for (const [name, rel] of Object.entries(refs)) {
    if (!rel) {
      continue;
    }
    const joined = path.join(taskDir, rel);
    const abs = resolveInside(root, joined);
    if (!abs) {
      out.push({ name, path: joined, exists: false, error: 'outside workspace' });
      continue;
    }
    try {
      const data = fs.readFileSync(abs);
      out.push({ name, path: joined, exists: true, digest: sha256(data).slice(0, 12) });
    } catch (err) {
      out.push({ name, path: joined, exists: false, error: err instanceof Error ? err.message : String(err) });
    }
  }
  return out;
}

function activeFileAdmission(
  activeFileRel: string | undefined,
  readEnvelope: string[],
  modifyEnvelope: string[],
  trusted: boolean
): ActiveFileAdmission {
  if (!trusted) {
    return { relativePath: activeFileRel, state: 'untrusted', label: 'Task stale or untrusted' };
  }
  if (!activeFileRel) {
    return { state: 'none', label: 'No active editor file' };
  }
  if (modifyEnvelope.includes(activeFileRel)) {
    return { relativePath: activeFileRel, state: 'modify', label: 'Admitted for modification' };
  }
  if (readEnvelope.includes(activeFileRel)) {
    return { relativePath: activeFileRel, state: 'read', label: 'Readable only' };
  }
  return { relativePath: activeFileRel, state: 'outside', label: 'Outside current envelope' };
}

export function loadActiveTask(root: string | undefined, activeFileRel?: string): ActiveTaskState {
  if (!root) {
    return emptyState('none', 'No workspace folder open.');
  }
  const activePath = path.join(root, '.sensei', 'tasks', 'active.yaml');
  if (!fs.existsSync(activePath)) {
    return emptyState('none', 'No active architectural task');
  }
  try {
    const pointerText = readText(activePath);
    const sessionRel = scalar(pointerText, 'session_path');
    const taskId = scalar(pointerText, 'task_id');
    const pointerDigest = scalar(pointerText, 'session_digest_sha256');
    if (!sessionRel) {
      return emptyState('invalid', 'Active task pointer has no session_path.');
    }
    const sessionPath = resolveInside(root, sessionRel);
    if (!sessionPath) {
      return emptyState('invalid', 'Active task session path is outside the workspace.');
    }
    const sessionText = readText(sessionPath);
    const sessionDigest = scalar(sessionText, 'session_digest_sha256');
    const sessionTaskId = scalar(sessionText, 'task_id');
    const artifacts = mapScalars(sessionText, 'artifacts') as ActiveTaskArtifactRefs;
    const taskDir = path.dirname(sessionRel).split(path.sep).join('/');
    const errors: string[] = [];
    if (taskId && sessionTaskId && taskId !== sessionTaskId) {
      errors.push('active pointer task_id does not match session task_id');
    }
    if (pointerDigest && sessionDigest && pointerDigest !== sessionDigest) {
      errors.push('active pointer digest does not match session digest');
    }
    const kind: ActiveTaskKind = errors.length ? 'stale' : 'active';
    const readEnvelope = sequenceScalars(sessionText, 'read_envelope');
    const modifyEnvelope = sequenceScalars(sessionText, 'modify_envelope');
    return {
      kind,
      taskId: sessionTaskId || taskId,
      taskDir,
      sessionPath: sessionRel,
      sessionDigest,
      pointerDigest,
      pointerPath: '.sensei/tasks/active.yaml',
      description: scalar(sessionText, 'description'),
      repositoryDomain: scalar(sessionText, 'repository_domain'),
      revision: scalar(sessionText, 'revision'),
      graphDigest: scalar(sessionText, 'graph_digest_sha256'),
      bindingHealth: kind === 'active' ? 'unchecked' : 'stale',
      phase: scalar(sessionText, 'workflow_phase'),
      status: scalar(sessionText, 'operational_status'),
      closure: scalar(sessionText, 'closure_verdict'),
      convergence: scalar(sessionText, 'convergence_status'),
      admission: scalar(sessionText, 'admission_decision'),
      inspect: scalar(sessionText, 'inspection_capability'),
      modify: scalar(sessionText, 'mutation_capability'),
      waitingOn: sequenceScalars(sessionText, 'waiting_on'),
      readEnvelope,
      modifyEnvelope,
      next: firstMapInSequence(sessionText, 'next_actions'),
      timelineReceiptIds: [],
      artifacts,
      artifactHealth: artifactHealth(root, taskDir, artifacts),
      counts: countArtifacts(root, taskDir, artifacts),
      activeFile: activeFileAdmission(activeFileRel, readEnvelope, modifyEnvelope, kind === 'active'),
      verified: kind === 'active' ? undefined : false,
      verifyErrors: [...errors],
      errors,
    };
  } catch (err) {
    const st = emptyState('invalid', err instanceof Error ? err.message : String(err));
    st.pointerPath = '.sensei/tasks/active.yaml';
    return st;
  }
}

export function mergeTaskStatusJson(state: ActiveTaskState, stdout: string, stderr: string): ActiveTaskState {
  const next: ActiveTaskState = {
    ...state,
    waitingOn: [...state.waitingOn],
    readEnvelope: [...state.readEnvelope],
    modifyEnvelope: [...state.modifyEnvelope],
    artifacts: { ...state.artifacts },
    artifactHealth: [...state.artifactHealth],
    counts: { ...state.counts },
    activeFile: { ...state.activeFile },
    timelineReceiptIds: [...state.timelineReceiptIds],
    verifyErrors: [...state.verifyErrors],
    errors: [...state.errors],
  };
  if (stderr.trim()) {
    next.verified = false;
    next.verifyErrors.push(stderr.trim());
    next.kind = next.kind === 'none' ? 'none' : 'stale';
    next.activeFile = { ...next.activeFile, state: 'untrusted', label: 'Task stale or untrusted' };
    return next;
  }
  try {
    const parsed = JSON.parse(stdout) as {
      task_control?: {
        binding_health?: string;
        permission?: { inspect?: string; modify?: string; exact_scope?: string[] };
        primary_blocker?: { id?: string; group_id?: string; statement?: string; consequence?: string };
        primary_question?: { id?: string; question_text?: string; resolution_class?: string };
        automatic_evidence?: ActiveTaskEvidenceProgress;
        next_action?: { kind?: string; target_id?: string; summary?: string; command_hint?: string };
        limitations?: string[];
        receipts?: string[];
        receipt_digest_sha256?: string;
      };
      architecture_task_status?: {
        phase?: string;
        status?: string;
        closure?: string;
        convergence?: string;
        admission?: string;
        verified?: boolean;
        verify_errors?: string[];
        next?: ActiveTaskNextAction;
      };
    };
    const control = parsed.task_control;
    if (control) {
      next.bindingHealth = control.binding_health || next.bindingHealth;
      next.inspect = control.permission?.inspect || next.inspect;
      next.modify = control.permission?.modify || next.modify;
      if (control.primary_blocker?.id && control.primary_blocker.statement) {
        next.primaryBlocker = {
          id: control.primary_blocker.id,
          groupId: control.primary_blocker.group_id,
          statement: control.primary_blocker.statement,
          consequence: control.primary_blocker.consequence,
        };
      }
      if (control.primary_question?.id && control.primary_question.question_text) {
        next.primaryQuestion = {
          id: control.primary_question.id,
          questionText: control.primary_question.question_text,
          resolutionClass: control.primary_question.resolution_class,
        };
      }
      next.automaticEvidence = control.automatic_evidence;
      if (control.next_action?.kind) {
        next.next = {
          action: control.next_action.kind,
          reference: control.next_action.target_id,
          summary: control.next_action.summary,
        };
      }
      next.timelineReceiptIds = [...(control.receipts || [])];
      if (control.receipt_digest_sha256) {
        next.timelineReceiptIds.push(`task_control:${control.receipt_digest_sha256}`);
      }
      next.verifyErrors = control.limitations || [];
      next.verified = control.binding_health === 'current';
      next.kind = next.verified ? 'active' : 'stale';
      if (!next.verified) {
        next.activeFile = { ...next.activeFile, state: 'untrusted', label: 'Task stale or untrusted' };
      }
      return next;
    }
    const st = parsed.architecture_task_status;
    if (!st) {
      throw new Error('missing architecture_task_status');
    }
    next.phase = st.phase || next.phase;
    next.status = st.status || next.status;
    next.closure = st.closure || next.closure;
    next.convergence = st.convergence || next.convergence;
    next.admission = st.admission || next.admission;
    next.next = st.next || next.next;
    next.verified = !!st.verified;
    next.verifyErrors = st.verify_errors || [];
    if (!next.verified) {
      next.kind = 'stale';
      next.activeFile = { ...next.activeFile, state: 'untrusted', label: 'Task stale or untrusted' };
    }
    return next;
  } catch (err) {
    next.verified = false;
    next.verifyErrors.push(err instanceof Error ? err.message : String(err));
    return next;
  }
}
