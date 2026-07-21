// SPDX-License-Identifier: AGPL-3.0-only
//
// Adversarial proofs for the Checkpoint-4 read-only artifact inspector builders
// (media/controlPanelFmt.js). The inspector renders owner-derived ArtifactState
// verbatim and infers nothing. Maps to design proofs 5/6 (applicable-only),
// 7/8/40 (no client state computation; UNSPECIFIED is Invalid, never neutral),
// 19 (unknown stays unknown), 37 (exact-scope feedback provenance only), and the
// CP4 read-only ruling (no action markup).

import test from 'node:test';
import assert from 'node:assert/strict';
import * as path from 'path';

// eslint-disable-next-line @typescript-eslint/no-var-requires
const CPFmt = require(path.join(__dirname, '..', 'media', 'controlPanelFmt.js'));

const APPLICABLE_DIM = {
  dimension: 'enforcement',
  label: 'Enforcement',
  applicable: true,
  required: true,
  state: 'ARCHITECTURE_DIMENSION_STATE_OPEN',
  reason_code: 'enforcement_missing',
  blockers: ['blocker:one'],
  evidence: ['evidence:e1'],
  questions: ['question:q1'],
  owner: 'controlstate.enforcement',
  next_action_owner: 'architect',
};

test('a non-applicable dimension is NEVER rendered (applicable-only)', () => {
  const na = { ...APPLICABLE_DIM, applicable: false };
  assert.equal(CPFmt.cpDimensionRow(na), '', 'applicable=false yields no row — never shown as open');
  assert.equal(CPFmt.cpDimensionRow(null), '');
});

test('an applicable dimension renders owner fields verbatim, state from the enum', () => {
  const h = CPFmt.cpDimensionRow(APPLICABLE_DIM);
  assert.match(h, />Enforcement</, 'label');
  assert.match(h, /cp-dim-open/, 'state badge from the enum token (no client computation)');
  assert.match(h, />Open</, 'state text label present');
  assert.match(h, /cp-dim-req/, 'required tag');
  assert.match(h, /blocker:one/, 'blockers verbatim');
  assert.match(h, /evidence:e1/, 'evidence verbatim');
  assert.match(h, /question:q1/, 'questions verbatim');
  assert.match(h, /controlstate\.enforcement/, 'owner verbatim');
  // Read-only: no actionable control on a dimension.
  assert.doesNotMatch(h, /<button|onclick|data-action|answer|promote|dispos/i, 'no action affordance');
});

test('a dimension with an UNSPECIFIED/missing state renders Invalid, never neutral/OK', () => {
  const bad = { ...APPLICABLE_DIM, state: 'ARCHITECTURE_DIMENSION_STATE_UNSPECIFIED' };
  const h = CPFmt.cpDimensionRow(bad);
  assert.match(h, /cp-dim--invalid/);
  assert.match(h, />Invalid</);
  assert.doesNotMatch(h, />Satisfied</i);
});

test('cpIdList renders ids verbatim; empty is "none observed", never a synthesized value', () => {
  assert.match(CPFmt.cpIdList('Evidence', ['a:1', 'b:2']), /a:1[\s\S]*b:2/);
  assert.match(CPFmt.cpIdList('Evidence', []), /none observed/);
  assert.match(CPFmt.cpIdList('Evidence', undefined), /none observed/);
});

test('feedback renders EXACT-SCOPE provenance only, never a repo-wide scan or authority', () => {
  const ref = {
    scope_identity: 'scope:file/x',
    availability: 'feedback_available',
    verified_record_ids: ['rec:1'],
    lineage_ids: ['lin:1'],
    limitations: ['lim:1'],
  };
  const h = CPFmt.cpFeedbackProvenance(ref);
  assert.match(h, /scope:file\/x/);
  assert.match(h, /feedback_available/);
  assert.match(h, /rec:1/);
  assert.match(h, /lin:1/);
  assert.match(h, /Provenance only/i);
  assert.doesNotMatch(h, /repository-wide|authoritative truth|certif/i);
  // Null feedback is explicit absence, not an invented value.
  assert.match(CPFmt.cpFeedbackProvenance(null), /no exact-scope feedback/);
});

test('relationships / focus graph render an honest unavailable placeholder (no legacy graph)', () => {
  const h = CPFmt.cpUnavailableSection('Relationships', 'no owner-projected relationship data');
  assert.match(h, /Relationships/);
  assert.match(h, /no owner-projected relationship data/);
  // Never a fabricated relationship or a legacy colored node.
  assert.doesNotMatch(h, /CLASS_COLOR|<svg|<circle|depends|neighbou?r/i);
});
