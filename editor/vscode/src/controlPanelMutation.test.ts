// SPDX-License-Identifier: AGPL-3.0-only
//
// Adversarial proofs for the guarded architect-answer workflow state machine
// (media/controlPanelMutation.js). The UI must be painfully literal and never
// optimistic: no inferred "accepted" state, no second click while a commit is in
// flight, the server owns replay, and the displayed architectural lifecycle
// ALWAYS comes from the refreshed owner projection — never from the receipt.

import test from 'node:test';
import assert from 'node:assert/strict';
import * as path from 'path';

// eslint-disable-next-line @typescript-eslint/no-var-requires
const M = require(path.join(__dirname, '..', 'media', 'controlPanelMutation.js'));

function toCandidate(s: any) {
  return M.cpGuardedReduce(M.cpGuardedReduce(s, { type: 'PREPARE_START' }),
    { type: 'PREPARE_RESULT', candidate: { expected_ledger_head_digest_sha256: 'head1' } });
}

test('commit requires an explicit confirmation and happens exactly once', () => {
  let s = toCandidate(M.cpGuardedInitial());
  // A commit BEFORE confirm is refused (no transition).
  const noConfirm = M.cpGuardedReduce(s, { type: 'COMMIT_START' });
  assert.equal(noConfirm.phase, 'prepared', 'commit before confirm must not start');

  s = M.cpGuardedReduce(s, { type: 'CONFIRM' });
  assert.equal(M.cpCanSubmit(s), true, 'submit is allowed once confirmed');
  s = M.cpGuardedReduce(s, { type: 'COMMIT_START' });
  assert.equal(s.inFlight, true);

  // A SECOND commit while one is in flight is refused (no second click).
  const second = M.cpGuardedReduce(s, { type: 'COMMIT_START' });
  assert.equal(second.phase, 'committing', 'a second commit in-flight must not start');
  assert.equal(M.cpCanSubmit(s), false, 'no submit while a commit is in flight');
});

test('a receipt NEVER produces an optimistic lifecycle — only the owner refresh does', () => {
  let s = toCandidate(M.cpGuardedInitial());
  s = M.cpGuardedReduce(s, { type: 'CONFIRM' });
  s = M.cpGuardedReduce(s, { type: 'COMMIT_START' });
  s = M.cpGuardedReduce(s, { type: 'COMMIT_RESULT', receipt: { mutation_applied: true, outcome: 'RECORDED' } });

  // The server says the mutation was applied…
  assert.equal(M.cpReceiptApplied(s), true, 'receipt applied flag is the server truth');
  // …but the architectural display is still empty until the owner is refreshed.
  assert.equal(M.cpDisplayedLifecycle(s), null, 'no optimistic lifecycle before the owner refresh');
});

test('the displayed lifecycle comes from the refreshed owner projection', () => {
  let s = toCandidate(M.cpGuardedInitial());
  s = M.cpGuardedReduce(s, { type: 'CONFIRM' });
  s = M.cpGuardedReduce(s, { type: 'COMMIT_START' });
  s = M.cpGuardedReduce(s, { type: 'COMMIT_RESULT', receipt: { mutation_applied: true } });
  s = M.cpGuardedReduce(s, { type: 'REFRESH_RESULT', ownerState: { lifecycle: 'ARCHITECTURE_LIFECYCLE_STATE_ACTIVE' } });
  const disp = M.cpDisplayedLifecycle(s);
  assert.equal(disp.source, 'owner');
  assert.equal(disp.lifecycle, 'ARCHITECTURE_LIFECYCLE_STATE_ACTIVE');
});

// The extra proof: a successful commit followed by a STALE/UNAVAILABLE refresh
// must NOT preserve an optimistic-looking accepted state. The receipt may say
// applied; the display must reflect the refreshed (unavailable) owner state.
test('a stale/unavailable owner refresh after a successful commit is shown honestly, not optimistically', () => {
  let s = toCandidate(M.cpGuardedInitial());
  s = M.cpGuardedReduce(s, { type: 'CONFIRM' });
  s = M.cpGuardedReduce(s, { type: 'COMMIT_START' });
  s = M.cpGuardedReduce(s, { type: 'COMMIT_RESULT', receipt: { mutation_applied: true, outcome: 'RECORDED' } });
  s = M.cpGuardedReduce(s, { type: 'REFRESH_RESULT', ownerState: { unavailable: true, reason: 'graph_authority_unobserved' } });

  assert.equal(M.cpReceiptApplied(s), true, 'the receipt still records the applied mutation');
  const disp = M.cpDisplayedLifecycle(s);
  assert.equal(disp.unavailable, true, 'the display must reflect the refreshed unavailable owner state');
  assert.equal(disp.reason, 'graph_authority_unobserved');
  assert.notEqual(disp.lifecycle, 'ARCHITECTURE_LIFECYCLE_STATE_ACTIVE', 'no invented accepted/active lifecycle');
});

test('a refusal never yields a committed state and is never optimistic', () => {
  let s = toCandidate(M.cpGuardedInitial());
  s = M.cpGuardedReduce(s, { type: 'CONFIRM' });
  s = M.cpGuardedReduce(s, { type: 'COMMIT_START' });
  s = M.cpGuardedReduce(s, { type: 'COMMIT_RESULT', refusal: { reason_code: 'stale_expected_head', owner: 'questiondisposition' } });
  assert.equal(s.phase, 'refused');
  assert.equal(s.receipt, null, 'a refusal carries no receipt');
  assert.equal(s.inFlight, false, 'a refusal releases the in-flight guard');
  assert.equal(M.cpDisplayedLifecycle(s), null, 'a refusal shows no lifecycle');
});

test('exact replay is the server truth (mutation_applied=false), reflected verbatim', () => {
  let s = toCandidate(M.cpGuardedInitial());
  s = M.cpGuardedReduce(s, { type: 'CONFIRM' });
  s = M.cpGuardedReduce(s, { type: 'COMMIT_START' });
  s = M.cpGuardedReduce(s, { type: 'COMMIT_RESULT', receipt: { mutation_applied: false, outcome: 'REPLAYED' } });
  assert.equal(M.cpReceiptApplied(s), false, 'exact replay applied nothing new — shown verbatim');
});
