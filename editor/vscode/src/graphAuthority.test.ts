// SPDX-License-Identifier: AGPL-3.0-only

import test from 'node:test';
import assert from 'node:assert/strict';

import { assessMetadataAuthority, effectiveMetadataFreshness } from './graphAuthority';

test('assessMetadataAuthority marks stamped current metadata authoritative', () => {
  const got = assessMetadataAuthority({
    triple_count: '42',
    build_provenance_state: 'BUILD_PROVENANCE_STATE_STAMPED',
    seed_state: 'SEED_STATE_CURRENT',
    live_store_contains_embedded_seed_marker: true,
  });
  assert.equal(got.authoritative, true);
  assert.equal(got.verdict, 'authoritative');
  assert.equal(got.state, 'current');
});

test('assessMetadataAuthority marks stale metadata non-authoritative', () => {
  const got = assessMetadataAuthority({
    triple_count: '12062',
    build_provenance_state: 'BUILD_PROVENANCE_STATE_STAMPED',
    seed_state: 'SEED_STATE_STALE',
    graph_freshness_state: 'GRAPH_FRESHNESS_STATE_STALE',
    graph_freshness_detail: 'live store digest diverges from expected artifact',
    live_store_contains_embedded_seed_marker: false,
  });
  assert.equal(got.authoritative, false);
  assert.equal(got.verdict, 'stale');
  assert.equal(got.summary, 'Live graph stale — authority disabled');
  assert.equal(got.detail, 'live store digest diverges from expected artifact');
});

test('effectiveMetadataFreshness infers current from stamped fields when raw freshness is unset', () => {
  const got = effectiveMetadataFreshness({
    triple_count: '77',
    build_provenance_state: 'BUILD_PROVENANCE_STATE_STAMPED',
    seed_state: 'SEED_STATE_CURRENT',
    live_store_contains_embedded_seed_marker: true,
  });
  assert.equal(got, 'GRAPH_FRESHNESS_STATE_CURRENT');
});

test('assessMetadataAuthority marks empty graph non-authoritative', () => {
  const got = assessMetadataAuthority({
    triple_count: '0',
    build_provenance_state: 'BUILD_PROVENANCE_STATE_STAMPED',
    seed_state: 'SEED_STATE_CURRENT',
    live_store_contains_embedded_seed_marker: true,
  });
  assert.equal(got.authoritative, false);
  assert.equal(got.verdict, 'empty');
  assert.equal(got.summary, 'Graph empty — authority disabled');
});
