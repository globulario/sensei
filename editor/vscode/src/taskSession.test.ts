// SPDX-License-Identifier: Apache-2.0

import test from 'node:test';
import assert from 'node:assert/strict';
import * as fs from 'fs';
import * as os from 'os';
import * as path from 'path';

import { loadActiveTask, mergeTaskStatusJson, taskMatchesGraphDomain } from './taskSession';

function tempDir(t: test.TestContext): string {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'sensei-vscode-task-'));
  t.after(() => fs.rmSync(dir, { recursive: true, force: true }));
  return dir;
}

function write(root: string, rel: string, content: string): void {
  const full = path.join(root, rel);
  fs.mkdirSync(path.dirname(full), { recursive: true });
  fs.writeFileSync(full, content);
}

function writeTask(root: string, pointerDigest = 'digest.one'): void {
  write(root, '.sensei/tasks/active.yaml', `architecture_active_task:
  schema_version: sensei.architecture.task_session.v1
  task_id: task.literal
  session_path: .sensei/tasks/task.literal/session.yaml
  session_digest_sha256: ${pointerDigest}
`);
  write(root, '.sensei/tasks/task.literal/session.yaml', `architecture_task_session:
  schema_version: sensei.architecture.task_session.v1
  task_id: task.literal
  workflow_phase: closure
  operational_status: waiting_on_architect
  binding:
    repository_domain: github.com/example/project
    revision: 1111222233334444555566667777888899990000
    graph_digest_sha256: aaaabbbbccccddddeeeeffff1111222233334444555566667777888899990000
  task_request:
    description: Ensure literal colon routes resolve consistently.
  artifacts:
    claims: source/claims.yaml
    dialogue: source/dialogue.yaml
    admission_decision: admission/decision.yaml
  closure_verdict: open
  convergence_status: waiting_on_architect
  admission_decision: waiting
  inspection_capability: allowed
  mutation_capability: waiting
  waiting_on:
    - architect_answer
  read_envelope:
    - gin/read.go
  modify_envelope:
    - gin.go
    - gin_test.go
  next_actions:
    - action: answer_question
      reference: question.route_tree_shared_state
      summary: Explain route tree shared state.
  session_digest_sha256: digest.one
`);
  write(root, '.sensei/tasks/task.literal/source/claims.yaml', `architecture_claims:
  claims:
    - id: claim.one
    - id: claim.two
`);
  write(root, '.sensei/tasks/task.literal/source/dialogue.yaml', `architecture_dialogue:
  open_questions:
    - id: question.one
  architect_answers:
    - id: answer.one
    - id: answer.two
`);
  write(root, '.sensei/tasks/task.literal/admission/decision.yaml', 'decision: waiting\n');
}

test('loadActiveTask reports no active architectural task', (t) => {
  const root = tempDir(t);
  const state = loadActiveTask(root);
  assert.equal(state.kind, 'none');
  assert.equal(state.activeFile.label, 'No active architectural task');
});

test('loadActiveTask reads active session, counts task artifacts, and uses exact modify envelope', (t) => {
  const root = tempDir(t);
  writeTask(root);

  const state = loadActiveTask(root, 'gin.go');

  assert.equal(state.kind, 'active');
  assert.equal(state.taskId, 'task.literal');
  assert.equal(state.description, 'Ensure literal colon routes resolve consistently.');
  assert.equal(state.repositoryDomain, 'github.com/example/project');
  assert.equal(state.closure, 'open');
  assert.equal(state.next?.reference, 'question.route_tree_shared_state');
  assert.deepEqual(state.counts, { claims: 2, questions: 1, answers: 2, probes: 0 });
  assert.equal(state.activeFile.state, 'modify');
  assert.equal(state.activeFile.label, 'Admitted for modification');
});

test('loadActiveTask counts the latest convergence dialogue when it exists', (t) => {
  const root = tempDir(t);
  writeTask(root);
  write(root, '.sensei/tasks/task.literal/convergence/latest/dialogue.yaml', `architecture_dialogue:
  open_questions:
    - id: question.latest.one
      alternatives:
        - id: alternative.nested
    - id: question.latest.two
  architect_answers:
    - id: answer.latest
`);
  write(root, '.sensei/tasks/task.literal/convergence/latest/probes.yaml', `architecture_evidence_probes:
  probes:
    - id: probe.latest
      evidence:
        - id: evidence.nested
`);

  assert.deepEqual(loadActiveTask(root).counts, { claims: 2, questions: 2, answers: 1, probes: 1 });
});

test('taskMatchesGraphDomain rejects a foreign workspace task for a selected graph', () => {
  assert.equal(taskMatchesGraphDomain('github.com/gin-gonic/gin', 'github.com/gin-gonic/gin'), true);
  assert.equal(taskMatchesGraphDomain('github.com/globulario/sensei', 'github.com/gin-gonic/gin'), false);
  assert.equal(taskMatchesGraphDomain('github.com/globulario/sensei', ''), true);
});

test('loadActiveTask requires exact envelope path matches', (t) => {
  const root = tempDir(t);
  writeTask(root);

  assert.equal(loadActiveTask(root, 'gin/read.go').activeFile.state, 'read');
  assert.equal(loadActiveTask(root, 'gin').activeFile.state, 'outside');
  assert.equal(loadActiveTask(root, 'gin.go/sub').activeFile.state, 'outside');
});

test('loadActiveTask marks digest mismatches stale', (t) => {
  const root = tempDir(t);
  writeTask(root, 'digest.two');

  const state = loadActiveTask(root, 'gin.go');

  assert.equal(state.kind, 'stale');
  assert.equal(state.verified, false);
  assert.equal(state.activeFile.state, 'untrusted');
  assert.match(state.verifyErrors.join('; '), /digest/);
});

test('mergeTaskStatusJson applies verified status result', (t) => {
  const root = tempDir(t);
  writeTask(root);
  const state = loadActiveTask(root, 'gin.go');

  const merged = mergeTaskStatusJson(
    state,
    JSON.stringify({
      architecture_task_status: {
        phase: 'admission',
        status: 'mutation_admitted',
        closure: 'closed',
        convergence: 'closed',
        admission: 'admitted',
        verified: true,
        next: { action: 'modify', reference: 'gin.go' },
      },
    }),
    ''
  );

  assert.equal(merged.kind, 'active');
  assert.equal(merged.verified, true);
  assert.equal(merged.admission, 'admitted');
  assert.equal(merged.next?.action, 'modify');
});

test('mergeTaskStatusJson consumes the stable task-control backend contract', (t) => {
  const root = tempDir(t);
  writeTask(root);
  const state = loadActiveTask(root, 'gin.go');
  const merged = mergeTaskStatusJson(state, JSON.stringify({
    task_control: {
      binding_health: 'current',
      permission: { inspect: 'admitted', modify: 'waiting' },
      primary_blocker: { id: 'blocker.one', group_id: 'group.one', statement: 'Equivalence is unproven.', consequence: 'mutation waiting' },
      automatic_evidence: { total: 4, eligible: 1, completed: 3, inconclusive: 0, failed: 0, rejected: 0, unavailable: 0 },
      primary_question: { id: 'question.one', question_text: 'Which behavior is intended?', resolution_class: 'architect_judgement_required' },
      next_action: { kind: 'answer_architect_question', target_id: 'question.one', summary: 'Answer one question.' },
      receipts: ['closure:abc'],
      receipt_digest_sha256: 'def',
    },
  }), '');

  assert.equal(merged.bindingHealth, 'current');
  assert.equal(merged.inspect, 'admitted');
  assert.equal(merged.modify, 'waiting');
  assert.equal(merged.primaryBlocker?.groupId, 'group.one');
  assert.equal(merged.automaticEvidence?.completed, 3);
  assert.equal(merged.primaryQuestion?.id, 'question.one');
  assert.equal(merged.next?.action, 'answer_architect_question');
  assert.deepEqual(merged.timelineReceiptIds, ['closure:abc', 'task_control:def']);
});
