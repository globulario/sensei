// SPDX-License-Identifier: AGPL-3.0-only
//
// Checkpoint-6 closure proofs for completion / feedback display honesty and the
// deliberately-deferred promotion path:
//   proof 4  — completion is a workflow terminal state, NEVER correctness;
//   proof 12 — an accepted answer is never shown as promoted;
//   proof 13 — promoted knowledge shows Phase-9.6 lineage (exact-scope);
//   proof 18 — degraded feedback does not erase base architecture state.
// Plus the CP6 ruling that governed promotion stays VISIBLY unavailable (no
// fact-writing affordance is painted onto the control panel).

import test from 'node:test';
import assert from 'node:assert/strict';
import * as fs from 'fs';
import * as path from 'path';

const media = path.resolve(__dirname, '..', 'media');
const read = (p: string) => fs.readFileSync(p, 'utf8');
const panel = read(path.join(media, 'controlPanel.js'));
// eslint-disable-next-line @typescript-eslint/no-var-requires
const CPFmt = require(path.join(media, 'controlPanelFmt.js'));

// ---- proof 4: completion is not correctness --------------------------------

test('proof 4 — completionText never claims certification/correctness', () => {
  const fn = panel.slice(panel.indexOf('function completionText'));
  const body = fn.slice(0, fn.indexOf('\n  }') + 4);
  assert.doesNotMatch(body, /certified|correctness verified|is correct|verified correct/i,
    'completion display must not assert correctness');
  assert.match(body, /not correctness/i, 'the "(authoritative)" qualifier is explicitly scoped to completion');
  assert.match(body, /Phase 6/i, 'a comment records that Phase 6 alone certifies');
});

test('proof 4 — the top strip keeps Completion and Active task as separate cells', () => {
  assert.match(panel, /cell\('Active task'/, 'active task is its own cell');
  assert.match(panel, /cell\('Completion'/, 'completion is its own cell — never merged into a correctness verdict');
});

// ---- proof 12 + CP6 ruling: promotion stays visibly unavailable -------------

test('proof 12 — the control panel offers no promotion affordance (no fact-writing button)', () => {
  assert.doesNotMatch(panel, /promoteArchitectAnswer|data-action="promote"|id="cpMutPromote"/,
    'the guarded family is disposition-only; promotion is not wired');
});

test('CP6 ruling — governed promotion is labelled deferred, not silently absent', () => {
  assert.match(panel, /promotion is deferred/i, 'the receipt states promotion is deferred');
  assert.match(panel, /reusable candidate/i, 'and that an accepted answer stays a reusable candidate');
});

test('proof 12 — an accepted disposition outcome is rendered verbatim, never relabelled "promoted"', () => {
  // The receipt shows the owner outcome string directly; it never fabricates a
  // "promoted" state client-side.
  assert.match(panel, /row\('Outcome', esc\(r\.outcome \|\| ''\)\)/, 'outcome is the owner string, verbatim');
  assert.doesNotMatch(panel, /outcome[^\n]*=\s*['"]promoted['"]/i, 'no client-synthesized promoted outcome');
});

// ---- proof 13: promoted knowledge shows Phase-9.6 lineage (exact-scope) -----

test('proof 13 — exact-scope feedback provenance renders verified records + lineage', () => {
  const ref = {
    scope_identity: 'aw:artifact#x',
    availability: 'feedback_available',
    verified_record_ids: ['aw:record#r1'],
    lineage_ids: ['aw:promotion#p1'],
    limitations: [],
  };
  const h = CPFmt.cpFeedbackProvenance(ref);
  assert.match(h, /aw:record#r1/, 'verified record shown');
  assert.match(h, /aw:promotion#p1/, 'Phase-9.6 lineage shown');
  assert.match(h, /not a repo-wide scan, not authority/i, 'labelled as provenance, not authority');
});

test('proof 13/18 — absent feedback is honest ("no exact-scope feedback"), never fabricated', () => {
  const h = CPFmt.cpFeedbackProvenance(null);
  assert.match(h, /no exact-scope feedback/i, 'absence is stated, not filled in');
  assert.doesNotMatch(h, /available|promoted/i, 'no synthesized feedback state');
});

// ---- proof 18: degraded feedback does not erase base state ------------------

test('proof 18 — the degradation banner is additive; base posture cells render first', () => {
  // renderTopStrip pushes Repository/Domain/Graph-authority cells, THEN appends
  // any degradation banner with `html +=` — degradation never replaces the base
  // cells (which would erase architecture state).
  const fn = panel.slice(panel.indexOf('function renderTopStrip'));
  const repoAt = fn.indexOf("cell('Repository'");
  const bannerAt = fn.indexOf('cp-strip-degraded');
  assert.ok(repoAt !== -1 && bannerAt !== -1, 'both base cells and the degraded banner exist');
  assert.ok(repoAt < bannerAt, 'base posture cells are built before the degradation banner');
  assert.match(fn.slice(bannerAt - 200, bannerAt), /html \+=/, 'the banner is appended, not substituted');
});
