// SPDX-License-Identifier: AGPL-3.0-only
//
// Anti-duplication proof (design proof 41), made aggressive per the CP3 ruling:
// the new control-panel surface must NOT re-introduce any client-maintained
// semantic table. It fails if ASPECTS, semantic class membership, severity
// ordering, closure ordering, resolver capabilities, or ontology-family tables
// reappear in the control-panel modules. The panel gets ALL of that from the
// owner projections (navigation descriptor / snapshot / index) at runtime.

import test from 'node:test';
import assert from 'node:assert/strict';
import * as fs from 'fs';
import * as path from 'path';

// __dirname is out/ at test time; the source tree is a sibling.
const media = path.resolve(__dirname, '..', 'media');
const src = path.resolve(__dirname, '..', 'src');
const read = (p: string) => fs.readFileSync(p, 'utf8');

// The NEW control-panel surface (must be free of semantic tables).
const CONTROL_PANEL_FILES = ['controlPanel.js', 'controlPanelFmt.js'].map((f) => path.join(media, f));

function panelSource(): string {
  return CONTROL_PANEL_FILES.map(read).join('\n');
}

test('the control panel re-introduces no legacy semantic table', () => {
  const s = panelSource();
  for (const forbidden of [
    'ASPECTS', // the hardcoded ontology navigation array
    'CLASS_COLOR', // class -> color semantic table
    'SEV_COLOR', // severity -> color semantic table
    'RESOLVABLE', // legacy resolver-capability whitelist
    'PROMOTE_TARGET', // legacy promotion-capability table
    'severityRank', // any client severity ordering
    'severityForClass', // any client severity derivation
  ]) {
    assert.ok(!s.includes(forbidden), `control panel must not contain ${forbidden}`);
  }
});

test('the control panel maintains no ontology-family table (families come from the descriptor)', () => {
  const s = panelSource();
  // The family LABELS only co-occur in a hardcoded family table; at runtime the
  // panel reads them from descriptor.families, so the source must not embed them.
  for (const familyLabel of ['Knowledge', 'Realization', 'Dialogue & closure']) {
    assert.ok(!s.includes(familyLabel), `family label ${familyLabel} must not be hardcoded`);
  }
});

test('the control panel imposes no client severity or closure ordering', () => {
  const s = panelSource();
  // Server order is preserved exactly: the panel never sorts, and defines no
  // rank/order helper over severity or closure. (Keyed filter maps are allowed;
  // a RANKING is not.)
  assert.ok(!/\.sort\s*\(/.test(s), 'the panel never re-sorts owner-ordered output');
  assert.ok(!/(severity|closure)(Rank|Order)\b/.test(s), 'no severity/closure ranking helper');
});

test('the control panel navigation is descriptor-driven, and state is server-sourced', () => {
  const panel = read(path.join(media, 'controlPanel.js'));
  // Left rail from the descriptor; queue/list/pagination from the owner.
  assert.match(panel, /descriptor\.families/, 'left rail iterates descriptor.families');
  assert.match(panel, /top_attention/, 'attention queue from snapshot.top_attention');
  assert.match(panel, /next_cursor/, 'pagination uses the opaque next_cursor');
  // Capabilities/visibility are READ off the descriptor class, never a client table.
  assert.match(panel, /\.default_visible/, 'visibility read from the descriptor');
});

test('legacy semantic tables remain quarantined to the legacy dashboard module', () => {
  // CP3 keeps the legacy Phase-2 explorer working, so its tables are retained
  // there ON PURPOSE — but ONLY there, never in the new control-panel surface.
  const legacy = read(path.join(media, 'dashboard.js'));
  assert.ok(legacy.includes('ASPECTS'), 'legacy ASPECTS retained in dashboard.js (documented)');
  const panel = panelSource();
  assert.ok(!panel.includes('acquireVsCodeApi();\n'), 'panel shares the single VS Code API handle');
});

test('Phase 9.4 authority path is unchanged (existing consumers compatible)', () => {
  // The cockpit stops deriving authority, but the Phase-9.4 "This File" tree
  // still uses graphAuthority.ts — CP3 must not remove that surface (proofs 24/25).
  const ga = read(path.join(src, 'graphAuthority.ts'));
  assert.match(ga, /export function assessMetadataAuthority/);
  assert.match(ga, /export function assessGraphAuthority/);
  const tree = read(path.join(src, 'awarenessProvider.ts'));
  assert.match(tree, /graphAuthority/, 'the file tree still consumes graphAuthority.ts');
});

test('the control panel is read-only EXCEPT the single guarded mutation family (CP5)', () => {
  const panel = read(path.join(media, 'controlPanel.js'));
  // Capture every posted message type (direct or Object.assign-wrapped).
  const posted = [...panel.matchAll(/postMessage\(\s*(?:Object\.assign\(\s*)?\{\s*type:\s*'([^']+)'/g)].map((m) => m[1]);
  const reads = new Set(['getNavigationDescriptor', 'getControlSnapshot', 'listArtifacts', 'getArtifactState']);
  const guarded = new Set(['prepareDisposition', 'commitDisposition']);
  assert.ok(posted.length > 0, 'the panel posts requests');
  for (const t of posted) {
    assert.ok(reads.has(t) || guarded.has(t), `unexpected posted message type ${t} — only reads + the guarded family are allowed`);
  }
  // The guarded family IS wired; but NO out-of-family mutation exists.
  assert.ok(posted.includes('prepareDisposition') && posted.includes('commitDisposition'), 'the guarded disposition flow is wired');
  for (const forbidden of ['completeTask', 'certify', 'proposeFeedback', 'answerDirect', 'adjudicate']) {
    assert.ok(!posted.includes(forbidden), `no ${forbidden} message (out of the guarded family)`);
  }
  // Relationships / focus graph go through the honest unavailable builder, never
  // a client-side graph (no SVG graph, no legacy color/direction tables).
  assert.match(panel, /cpUnavailableSection\(/, 'relationships/focus graph render unavailable');
  assert.ok(!/<svg|<circle|CLASS_COLOR|\bDIR\b/.test(panel), 'no client-side relationship graph in the panel');
});

test('the guarded workflow is literal: prepare then a SEPARATE confirmed commit, no auto-chaining', () => {
  const panel = read(path.join(media, 'controlPanel.js'));
  // commit is posted only from an explicit confirm handler that first CONFIRMs
  // then COMMIT_STARTs the pure state machine — never automatically after prepare.
  assert.match(panel, /cpGuardedReduce\(mutation\.gs,\s*\{\s*type:\s*'CONFIRM'\s*\}\)/, 'commit requires an explicit CONFIRM');
  assert.match(panel, /if \(!mutation\.gs\.inFlight\)/, 'commit only fires when the in-flight guard is set');
  // The displayed lifecycle comes from the refreshed owner, never the receipt.
  assert.match(panel, /cpDisplayedLifecycle/, 'display lifecycle comes from the owner-refresh machine');
  assert.match(panel, /REFRESH_RESULT/, 'a commit is followed by an owner refresh feeding the machine');
});

test('the inspector renders owner ArtifactState fields, applicable-only, with no client inference', () => {
  const panel = read(path.join(media, 'controlPanel.js'));
  assert.match(panel, /applicable !== false/, 'dimensions filtered to applicable');
  assert.match(panel, /cpDimensionRow/, 'dimension rows via the shared owner builder');
  assert.match(panel, /cpFeedbackProvenance/, 'feedback rendered as provenance');
  // No local closure/lifecycle/severity/next-action synthesis: the panel never sorts.
  assert.ok(!/\.sort\s*\(/.test(panel));
});
