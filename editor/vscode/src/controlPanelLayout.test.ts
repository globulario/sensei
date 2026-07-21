// SPDX-License-Identifier: AGPL-3.0-only
//
// Checkpoint-6 proof 23 — the panel remains usable at constrained VS Code widths.
// Below a narrow threshold the three-pane grid (rail | center | inspector)
// collapses to a single readable column that scrolls VERTICALLY; the body never
// scrolls horizontally and no section is hidden. Asserted structurally over the
// shipped CSS (node --test has no layout engine, matching the repo's source-parse
// idiom for the panel's non-pure surface).

import test from 'node:test';
import assert from 'node:assert/strict';
import * as fs from 'fs';
import * as path from 'path';

const media = path.resolve(__dirname, '..', 'media');
const css = fs.readFileSync(path.join(media, 'dashboard.css'), 'utf8');

// Extract the body of the `@media (max-width: ...)` block that governs the
// control panel (there are other narrow-width queries for the legacy view).
function balancedBlockFrom(openBraceAt: number): string {
  let depth = 0;
  for (let i = openBraceAt; i < css.length; i++) {
    if (css[i] === '{') depth++;
    else if (css[i] === '}') {
      depth--;
      if (depth === 0) return css.slice(openBraceAt + 1, i);
    }
  }
  throw new Error('unbalanced @media block');
}

function narrowBlock(): string {
  const re = /@media\s*\(max-width:\s*\d+px\)\s*\{/g;
  let m: RegExpExecArray | null;
  while ((m = re.exec(css)) !== null) {
    const openBrace = css.indexOf('{', m.index);
    const body = balancedBlockFrom(openBrace);
    if (body.includes('.cp-body')) return body;
  }
  assert.fail('no narrow-width @media query governs .cp-body (proof 23)');
}

test('proof 23 — a narrow @media collapses the three-pane grid to a single column', () => {
  const block = narrowBlock();
  assert.match(block, /\.cp-body\s*\{[^}]*flex-direction:\s*column/, '.cp-body stacks vertically when narrow');
});

test('proof 23 — the narrow layout scrolls vertically and never horizontally', () => {
  const block = narrowBlock();
  assert.match(block, /\.cp-body\s*\{[^}]*overflow-y:\s*auto/, 'the stacked body scrolls vertically');
  assert.match(block, /overflow-x:\s*hidden/, 'horizontal overflow is suppressed');
});

test('proof 23 — no inspector/rail section is hidden at narrow width (they restack, not disappear)', () => {
  const block = narrowBlock();
  assert.doesNotMatch(block, /\.cp-rail\s*\{[^}]*display:\s*none/, 'the rail is never display:none');
  assert.doesNotMatch(block, /\.cp-header-pane\s*\{[^}]*display:\s*none/, 'the inspector is never display:none');
  assert.doesNotMatch(block, /\.cp-list\s*\{[^}]*display:\s*none/, 'the attention list stays visible');
});

test('proof 23 — the wide default is fluid (a 1fr center), and the center pane can shrink', () => {
  // The default three-column grid must include a fluid 1fr center so text reflows
  // rather than forcing a fixed minimum that overflows.
  assert.match(css, /\.cp-body\s*\{[^}]*grid-template-columns:[^;]*1fr/, 'the default center column is fluid (1fr)');
  assert.match(css, /\.cp-center\s*\{[^}]*min-width:\s*0/, 'the center pane can shrink below content width');
});
