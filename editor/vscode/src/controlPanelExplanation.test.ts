// SPDX-License-Identifier: AGPL-3.0-only
//
// Adversarial proofs for issue #93 Layer 1 (actionable incompleteness) + Layer 2
// (distinct non-positive states) in the read-only control panel. The panel renders
// the owner-projected explanation VERBATIM (no client-composed wording, no decision
// table) and each non-positive state is rendered distinctly (glyph + text, never
// color alone, never collapsed into a generic warning).

import test from 'node:test';
import assert from 'node:assert/strict';
import * as path from 'path';
import * as fs from 'fs';

// eslint-disable-next-line @typescript-eslint/no-var-requires
const CPFmt = require(path.join(__dirname, '..', 'media', 'controlPanelFmt.js'));

const RUNTIME_UNAVAILABLE = {
  dimension: 'runtime',
  label: 'Runtime boundary',
  applicable: true,
  required: true,
  state: 'ARCHITECTURE_DIMENSION_STATE_UNKNOWN',
  reason_code: 'required_evidence_absent',
  owner: 'runtimeboundary',
  next_action_owner: 'architect',
  explanation: {
    kind: 'required_evidence_absent',
    known: 'the boundary and its policy are valid and runtime-assessable',
    missing: 'an admissible native crossing observation (caller, callee, crossing binding)',
    why_not_improvable: 'no runtime crossing evidence has been observed, so compliance cannot be established',
    next_evidence: 'a native crossing observation admitted through an explicit binding',
  },
};

test('an owner explanation renders verbatim (known/missing/why/next), with its stable kind', () => {
  const h = CPFmt.cpDimensionRow(RUNTIME_UNAVAILABLE);
  assert.match(h, /data-explain-kind="required_evidence_absent"/, 'stable semantic kind carried for tests');
  assert.match(h, /the boundary and its policy are valid and runtime-assessable/, 'Known verbatim');
  assert.match(h, /admissible native crossing observation/, 'Missing verbatim');
  assert.match(h, /compliance cannot be established/, 'WhyNotImprovable verbatim');
  assert.match(h, /admitted through an explicit binding/, 'NextEvidence verbatim');
  // Honest, not broken: an unknown/unavailable dimension never reads satisfied/compliant/healthy.
  assert.doesNotMatch(h, />Satisfied<|compliant|healthy|complete/i, 'unknown never renders positive');
});

test('cpExplanation is presentation-only: a null/kindless explanation yields nothing', () => {
  assert.equal(CPFmt.cpExplanation(null), '');
  assert.equal(CPFmt.cpExplanation({}), '');
  // Empty fields are omitted, never rendered as blank rows.
  const h = CPFmt.cpExplanation({ kind: 'not_applicable', known: 'owner ruled out of scope' });
  assert.match(h, /owner ruled out of scope/);
  assert.doesNotMatch(h, /Missing<\/span><span class="cp-explain-v"><\/span>/, 'no blank rows');
});

test('a satisfied dimension carries no explanation block', () => {
  const sat = {
    dimension: 'runtime', label: 'Runtime boundary', applicable: true, required: true,
    state: 'ARCHITECTURE_DIMENSION_STATE_SATISFIED', owner: 'runtimeboundary',
  };
  assert.doesNotMatch(CPFmt.cpDimensionRow(sat), /cp-dim-explain/, 'positive state → no incompleteness block');
});

test('explanation text is escaped (no markup injection from owner strings)', () => {
  const h = CPFmt.cpExplanation({ kind: 'x', known: '<img src=x onerror=alert(1)>' });
  assert.doesNotMatch(h, /<img/, 'owner strings are escaped, never live markup');
  assert.match(h, /&lt;img/, 'escaped form present');
});

// Layer 2: distinct non-positive states in the shipped CSS — unknown / not_applicable /
// unavailable / invalid each have their own rule (glyph + border), never collapsed, and never
// color-only. This asserts against the shipped stylesheet source (the panel CSS is not a DOM here).
test('the stylesheet renders each non-positive state distinctly (glyph + not color-only)', () => {
  const css = fs.readFileSync(path.join(__dirname, '..', 'media', 'dashboard.css'), 'utf8');
  // Each state has its own selector line (not a shared collapsed rule).
  assert.match(css, /\.cp-dim-unknown\s*\{/, 'unknown has its own rule');
  assert.match(css, /\.cp-dim-not_applicable\s*\{/, 'not_applicable has its own rule');
  assert.match(css, /\.cp-avail-unavailable\s*[,{]/, 'unavailable has its own rule');
  assert.match(css, /\.cp-avail-invalid\s*[,{]/, 'invalid has its own rule');
  // The old collapsed pairs are gone.
  assert.doesNotMatch(css, /\.cp-dim-unknown,\s*\.cp-dim-not_applicable/, 'unknown+not_applicable no longer collapsed');
  assert.doesNotMatch(css, /\.cp-avail-unavailable,\s*\.cp-avail-invalid/, 'unavailable+invalid no longer collapsed');
  // Glyph (non-color carrier) present for the ambiguous "not grounded" states.
  assert.match(css, /\.cp-dim-unknown::before\s*\{\s*content:/, 'unknown carries a glyph, not color alone');
  assert.match(css, /\.cp-dim-not_applicable::before\s*\{\s*content:/, 'not_applicable carries a glyph');
});
