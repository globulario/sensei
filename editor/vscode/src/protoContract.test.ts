// SPDX-License-Identifier: AGPL-3.0-only

import test from 'node:test';
import assert from 'node:assert/strict';
import * as fs from 'fs';
import * as path from 'path';

const root = path.resolve(__dirname, '..');
const repoRoot = path.resolve(root, '..', '..');

test('vendored proto matches canonical proto', () => {
  const canonical = fs.readFileSync(path.join(repoRoot, 'proto', 'awareness_graph.proto'), 'utf8');
  const vendored = fs.readFileSync(path.join(root, 'proto', 'awareness_graph.proto'), 'utf8');
  assert.equal(vendored, canonical);
});

test('vendored proto includes Phase 2 query classes and metadata counts', () => {
  const vendored = fs.readFileSync(path.join(root, 'proto', 'awareness_graph.proto'), 'utf8');
  for (const needle of [
    'QUERY_CLASS_ARCHITECTURE_CLAIM',
    'QUERY_CLASS_OPEN_QUESTION',
    'QUERY_CLASS_ARCHITECT_ANSWER',
    'QUERY_CLASS_EVIDENCE_PROBE',
    'architecture_claim_count',
    'open_question_count',
    'architect_answer_count',
    'evidence_probe_count',
  ]) {
    assert.match(vendored, new RegExp(needle));
  }
});
