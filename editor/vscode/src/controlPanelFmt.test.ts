// SPDX-License-Identifier: AGPL-3.0-only
//
// Proofs for the shared control-panel formatters (media/controlPanelFmt.js).
// Maps to Phase 9.5 design proofs: 19 (unknown stays unknown), 22 (color is
// never the sole state carrier — every enum yields a text label), 40 (a source
// critical/invalid condition is never silently downgraded to OK).

import test from 'node:test';
import assert from 'node:assert/strict';
import * as path from 'path';

// The formatter is a plain-JS module shared verbatim with the webview; require
// it at runtime (it lives outside rootDir, so it is loaded, not type-checked).
// eslint-disable-next-line @typescript-eslint/no-var-requires
const CPFmt = require(path.join(__dirname, '..', 'media', 'controlPanelFmt.js'));

test('cpCount preserves unknown-versus-zero', () => {
  assert.equal(CPFmt.cpCount(undefined), null, 'absent optional is unknown, not 0');
  assert.equal(CPFmt.cpCount(null), null);
  assert.equal(CPFmt.cpCount(''), null);
  assert.equal(CPFmt.cpCount('0'), 0, 'an observed zero is a real 0');
  assert.equal(CPFmt.cpCount('5'), 5);
  assert.equal(CPFmt.cpIsUnknownCount(undefined), true);
  assert.equal(CPFmt.cpIsUnknownCount('0'), false);
});

test('cpCountText renders unknown as an em dash and observed zero as 0', () => {
  assert.equal(CPFmt.cpCountText(undefined), '—');
  assert.equal(CPFmt.cpCountText('0'), '0');
  assert.equal(CPFmt.cpCountText('12'), '12');
});

test('cpEnumToken strips the known prefixes to a bare CSS token', () => {
  assert.equal(CPFmt.cpEnumToken('ARCHITECTURE_ARTIFACT_CLOSURE_CLOSED'), 'closed');
  assert.equal(CPFmt.cpEnumToken('ARCHITECTURE_ATTENTION_SEVERITY_CRITICAL'), 'critical');
  assert.equal(CPFmt.cpEnumToken('ARCHITECTURE_LIFECYCLE_STATE_NOT_APPLICABLE'), 'not_applicable');
  assert.equal(CPFmt.cpEnumToken(undefined), '');
});

test('cpEnumLabel gives every badge a human text label (color never sole carrier)', () => {
  assert.equal(CPFmt.cpEnumLabel('ARCHITECTURE_ARTIFACT_CLOSURE_CLOSED'), 'Closed');
  assert.equal(CPFmt.cpEnumLabel('ARCHITECTURE_ARTIFACT_CLOSURE_NOT_APPLICABLE'), 'Not applicable');
  assert.equal(CPFmt.cpEnumLabel('ARCHITECTURE_ATTENTION_SEVERITY_WARNING'), 'Warning');
  assert.equal(CPFmt.cpEnumLabel(undefined), 'Unknown');
});

test('cpIsUnspecified flags missing and *_UNSPECIFIED values as invalid', () => {
  assert.equal(CPFmt.cpIsUnspecified('ARCHITECTURE_AVAILABILITY_UNSPECIFIED'), true);
  assert.equal(CPFmt.cpIsUnspecified('ARCHITECTURE_ARTIFACT_CLOSURE_UNSPECIFIED'), true);
  assert.equal(CPFmt.cpIsUnspecified(undefined), true);
  assert.equal(CPFmt.cpIsUnspecified(''), true);
  // A real value is NOT downgraded to invalid.
  assert.equal(CPFmt.cpIsUnspecified('ARCHITECTURE_AVAILABILITY_AVAILABLE'), false);
  assert.equal(CPFmt.cpIsUnspecified('ARCHITECTURE_ARTIFACT_CLOSURE_UNKNOWN'), false);
});

test('cpBadge renders the enum verbatim: token class + text label always present', () => {
  const closed = CPFmt.cpBadge('closure', 'ARCHITECTURE_ARTIFACT_CLOSURE_CLOSED');
  assert.match(closed, /class="cp-badge cp-closure-closed"/, 'visual class from the enum token');
  assert.match(closed, />Closed</, 'text label present (color never the sole carrier)');

  const open = CPFmt.cpBadge('closure', 'ARCHITECTURE_ARTIFACT_CLOSURE_OPEN');
  assert.match(open, /cp-closure-open/);
  assert.match(open, />Open</);

  const sev = CPFmt.cpBadge('sev', 'ARCHITECTURE_ATTENTION_SEVERITY_CRITICAL');
  assert.match(sev, /cp-sev-critical/);
  assert.match(sev, />Critical</);
});

test('cpBadge renders UNSPECIFIED/missing as an explicit Invalid badge, never neutral/OK', () => {
  const unspec = CPFmt.cpBadge('closure', 'ARCHITECTURE_ARTIFACT_CLOSURE_UNSPECIFIED');
  assert.match(unspec, /cp-closure--invalid/);
  assert.match(unspec, />Invalid</);
  // A missing enum is NOT silently rendered as any real state.
  const missing = CPFmt.cpBadge('avail', undefined);
  assert.match(missing, /cp-avail--invalid/);
  assert.doesNotMatch(missing, /available|Unknown|closed/i);
});

test('cpSeverityCount: unknown when the source was not observed, real 0 when observed', () => {
  const counts = [
    { key: 'critical', count: '2' },
    { key: 'warning', count: '0' },
  ];
  assert.equal(CPFmt.cpSeverityCount(counts, 'critical', true), '2');
  assert.equal(CPFmt.cpSeverityCount(counts, 'warning', true), '0', 'observed zero is a real 0');
  assert.equal(CPFmt.cpSeverityCount(counts, 'attention', true), '0', 'observed-but-absent bucket is 0');
  // Source NOT observed → Unknown, never 0.
  assert.equal(CPFmt.cpSeverityCount(undefined, 'critical', false), 'Unknown');
  assert.equal(CPFmt.cpSeverityCount([], 'critical', false), 'Unknown');
});
