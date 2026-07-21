// SPDX-License-Identifier: AGPL-3.0-only
//
// Adversarial proofs for issue #93 Layer 3 (coverage / grounding signal). The panel
// derives ratios ONLY from tallies that already exist under one owner and never invents
// a denominator: no denominator → no percentage; unobserved catalog → unavailable;
// observed-zero population ≠ unobserved population; coverage is never correctness and
// never suppresses attention.

import test from 'node:test';
import assert from 'node:assert/strict';
import * as path from 'path';

// eslint-disable-next-line @typescript-eslint/no-var-requires
const CPFmt = require(path.join(__dirname, '..', 'media', 'controlPanelFmt.js'));

test('cpRatio: a percentage requires an observed denominator > 0', () => {
  assert.equal(CPFmt.cpRatio(3, 10).percent, 30, 'observed ratio yields a percentage');
  assert.equal(CPFmt.cpRatio(3, 10).text, '3 of 10');
  assert.equal(CPFmt.cpRatio(5, null).percent, null, 'unobserved denominator → no percentage');
  assert.equal(CPFmt.cpRatio(5, null).text, 'unavailable');
  assert.equal(CPFmt.cpRatio(null, 10).percent, null, 'unobserved numerator → no percentage');
  // Observed zero population is NOT the same as unobserved, and never renders 0%/100%.
  assert.equal(CPFmt.cpRatio(0, 0).percent, null, 'zero population → no percentage (undefined, never 0/100%)');
  assert.match(CPFmt.cpRatio(0, 0).text, /no eligible items/);
});

test('cpKeyedCount / cpKeyedTotal preserve unobserved-vs-observed-zero', () => {
  assert.equal(CPFmt.cpKeyedCount(undefined, 'assessable'), null, 'absent array = unobserved (null)');
  assert.equal(CPFmt.cpKeyedCount([{ key: 'unsupported', count: 2 }], 'assessable'), 0, 'observed collection, key absent = observed 0');
  assert.equal(CPFmt.cpKeyedCount([{ key: 'assessable', count: '4' }], 'assessable'), 4, 'int64-as-string parsed');
  assert.equal(CPFmt.cpKeyedTotal(undefined), null);
  assert.equal(CPFmt.cpKeyedTotal([{ key: 'a', count: 3 }, { key: 'b', count: '2' }]), 5);
});

test('grounding is unavailable (never 0%/100%) when the catalog was not observed', () => {
  const h = CPFmt.cpGroundingSummary({});
  assert.match(h, /coverage unavailable/, 'no catalog tallies → explicit unavailable');
  assert.doesNotMatch(h, /\d+%/, 'no percentage fabricated from a missing denominator');
  assert.doesNotMatch(h, />100%|>0%/);
});

test('grounding renders honest ratios from owned tallies, with a not-correctness caveat', () => {
  const snap = {
    assessment_coverage_counts: [
      { key: 'assessable', count: 6 },
      { key: 'unsupported', count: 2 },
      { key: 'explicitly_not_applicable', count: 2 },
    ],
    closure_counts: [
      { key: 'closed', count: 3 },
      { key: 'open', count: 3 },
    ],
  };
  const h = CPFmt.cpGroundingSummary(snap);
  assert.match(h, /6 of 10 <span class="cp-grounding-pct">\(60%\)/, 'assessable / enumerated = 6/10');
  assert.match(h, /3 of 6 <span class="cp-grounding-pct">\(50%\)/, 'closed / assessable = 3/6');
  assert.match(h, /not correctness/i, 'coverage is explicitly not correctness');
  assert.match(h, /never suppresses attention/i);
});

test('grounding with zero assessable never renders a closure percentage', () => {
  const snap = {
    assessment_coverage_counts: [{ key: 'unsupported', count: 4 }],
    closure_counts: [{ key: 'closed', count: 0 }],
  };
  const h = CPFmt.cpGroundingSummary(snap);
  // 0 assessable / 4 enumerated = an honest 0% (an observed denominator) — that is a real signal.
  assert.match(h, /Assessable \/ enumerated<\/span><span class="cp-grounding-v">0 of 4 <span class="cp-grounding-pct">\(0%\)/);
  // But closed / assessable has a ZERO denominator → undefined, "no eligible items", never a percentage.
  assert.match(h, /Closed \/ assessable<\/span><span class="cp-grounding-v">0 of 0 \(no eligible items\)<\/span>/);
  assert.doesNotMatch(h, /\(100%\)/, 'a zero denominator never becomes 100%');
});
