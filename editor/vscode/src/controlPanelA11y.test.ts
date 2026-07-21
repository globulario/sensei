// SPDX-License-Identifier: AGPL-3.0-only
//
// Checkpoint-6 accessibility closure proofs.
//   proof 21 — keyboard-only operation works: every div[role=button] row honors
//     BOTH Enter and Space; native <button> affordances (chips, rail, guarded
//     panel) are keyboard-operable by definition; a fresh selection moves focus
//     to the inspector; guarded receipts/refusals reach a persistent aria-live
//     region so they are announced without a focus jump.
//   proof 22 — color is never the sole state carrier: every badge carries a text
//     label; tri/auth/tag states carry glyphs or words, not colour alone.
//
// These are structural proofs over the shipped source (the panel is an IIFE that
// wires a live DOM, so — matching controlPanelTables.test.ts — we assert the
// keyboard/focus/aria contract in source) plus functional proofs over the pure
// formatter module.

import test from 'node:test';
import assert from 'node:assert/strict';
import * as fs from 'fs';
import * as path from 'path';

const media = path.resolve(__dirname, '..', 'media');
const srcDir = path.resolve(__dirname, '..', 'src');
const read = (p: string) => fs.readFileSync(p, 'utf8');

const panel = read(path.join(media, 'controlPanel.js'));
const shell = read(path.join(srcDir, 'dashboardPanel.ts'));
const css = read(path.join(media, 'dashboard.css'));
// eslint-disable-next-line @typescript-eslint/no-var-requires
const CPFmt = require(path.join(media, 'controlPanelFmt.js'));

// ---- proof 21: keyboard-only operation ------------------------------------

test('proof 21 — the keyboard helper activates on BOTH Enter and Space', () => {
  // A div[role=button] is not a native button, so it must handle Space too.
  assert.match(panel, /const activateKey = \(e, fn\) =>/, 'activateKey helper present');
  const helper = panel.slice(panel.indexOf('const activateKey'));
  assert.match(helper, /e\.key === 'Enter'/, 'handles Enter');
  assert.match(helper, /e\.key === ' '/, 'handles Space');
  assert.match(helper, /preventDefault\(\)/, 'Space scroll is prevented');
});

test('proof 21 — every selectable row wires keydown through activateKey (no Enter-only rows)', () => {
  // Both the attention queue and the artifact index rows (div[role=button]) route
  // keydown through the Enter+Space helper.
  const rowHandlers = (panel.match(/r\.addEventListener\('keydown'[^\n]*\)/g) || []);
  assert.ok(rowHandlers.length >= 2, `both row lists wire keydown (found ${rowHandlers.length})`);
  for (const w of rowHandlers) {
    assert.match(w, /activateKey/, `row keydown must use activateKey (Enter+Space): ${w}`);
  }
  // The pre-CP6 Enter-only row pattern must be gone entirely.
  assert.doesNotMatch(panel, /keydown'[^\n]*e\.key === 'Enter'\) selectArtifact/, 'no bare Enter-only row handler survives');
});

test('proof 21 — the interactive chips and rail classes are native <button> (keyboard-operable)', () => {
  assert.match(panel, /`<button class="cp-chip/, 'filter chips are native buttons');
  assert.match(panel, /`<button class="cp-cls/, 'rail classes are native buttons');
  assert.match(panel, /class="cp-qanswer/, 'the answer affordance exists');
  assert.match(panel, /<button class="cp-qanswer/, 'the answer affordance is a native button');
});

test('proof 21 — a fresh selection moves focus to the inspector heading', () => {
  assert.match(panel, /pendingInspectorFocus = true/, 'selection arms an inspector focus');
  assert.match(panel, /function focusInspectorIfPending/, 'a one-shot focus mover exists');
  assert.match(panel, /querySelector\('\.cp-header-id'\)[\s\S]*?\.focus\(\)/, 'it focuses the heading');
  // The heading must be focusable via tabindex=-1 in BOTH the full and the
  // unavailable inspector renders.
  const headingDecls = panel.match(/class="cp-header-id"[^>]*/g) || [];
  assert.ok(headingDecls.length >= 2, 'both inspector branches render a heading');
  for (const d of headingDecls) {
    assert.match(d, /tabindex="-1"/, `heading must be programmatically focusable: ${d}`);
  }
});

test('proof 21 — a persistent aria-live region announces guarded results without a focus jump', () => {
  assert.match(shell, /id="cpLive"/, 'the shell hosts a live region');
  assert.match(shell, /aria-live="polite"/, 'it is a polite live region');
  assert.match(shell, /role="status"/, 'exposed as a status region');
  assert.match(css, /\.cp-sr-only\s*\{[^}]*position:\s*absolute/, 'the region is visually hidden, not display:none');
  assert.doesNotMatch(css, /\.cp-sr-only\s*\{[^}]*display:\s*none/, 'display:none would hide it from AT too');
  // The panel writes to it on prepare / commit / refusal / refresh / transport error.
  assert.match(panel, /const announce = \(msg\) =>/, 'an announce helper exists');
  assert.match(panel, /\$\('cpLive'\)/, 'announce targets the cpLive region');
  const announcements = (panel.match(/announce\(/g) || []).length;
  // prepare + commit + refusal-in-error + owner-refresh transitions each announce.
  assert.ok(announcements >= 4, `guarded transitions announce (found ${announcements})`);
});

// ---- proof 22: colour is never the sole state carrier ----------------------

test('proof 22 — every badge kind carries a human text label, not colour alone', () => {
  const cases: Array<[string, string, string]> = [
    ['sev', 'ARCHITECTURE_ATTENTION_SEVERITY_CRITICAL', 'Critical'],
    ['closure', 'ARCHITECTURE_ARTIFACT_CLOSURE_OPEN', 'Open'],
    ['closure', 'ARCHITECTURE_ARTIFACT_CLOSURE_CLOSED', 'Closed'],
    ['lifecycle', 'ARCHITECTURE_LIFECYCLE_STATE_ACTIVE', 'Active'],
    ['avail', 'ARCHITECTURE_AVAILABILITY_UNAVAILABLE', 'Unavailable'],
    ['coverage', 'ARCHITECTURE_ASSESSMENT_COVERAGE_ASSESSABLE', 'Assessable'],
    ['dim', 'ARCHITECTURE_DIMENSION_STATE_OPEN', 'Open'],
  ];
  for (const [kind, en, label] of cases) {
    const html = CPFmt.cpBadge(kind, en);
    assert.match(html, new RegExp('>' + label + '<'), `${kind} badge shows the text "${label}"`);
  }
});

test('proof 22 — an invalid/unspecified value shows the word "Invalid", never a neutral colour', () => {
  const html = CPFmt.cpBadge('closure', 'ARCHITECTURE_ARTIFACT_CLOSURE_UNSPECIFIED');
  assert.match(html, />Invalid</, 'text carries the invalid meaning');
});

test('proof 22 — tri booleans and tags carry glyphs/words alongside colour', () => {
  // Graph-authority tri-state uses ✓/✗ glyphs + a text label, not colour alone.
  assert.match(panel, /\$\{v \? '✓' : '✗'\}/, 'tri renders a ✓/✗ glyph');
  assert.match(panel, /observed<\/span>/, 'graph authority carries the word "observed"');
  assert.match(panel, /not observed/, 'and "not observed"');
  assert.match(panel, />blocking</, 'the blocking tag carries a word');
  assert.match(panel, />architect input</, 'the architect-input tag carries a word');
});
