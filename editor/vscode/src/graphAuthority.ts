// SPDX-License-Identifier: Apache-2.0

import type { GraphAuthority, MetadataResponse } from './grpcClient';

export type AuthorityVerdict =
  | 'authoritative'
  | 'stale'
  | 'unknown'
  | 'empty'
  | 'degraded';

export interface AuthorityAssessment {
  verdict: AuthorityVerdict;
  authoritative: boolean;
  state: string;
  summary: string;
  detail?: string;
}

export function assessMetadataAuthority(meta?: MetadataResponse): AuthorityAssessment {
  if (!meta) {
    return {
      verdict: 'unknown',
      authoritative: false,
      state: 'unknown',
      summary: 'Graph metadata unavailable',
      detail: 'The dashboard could not verify whether the served graph is authoritative.',
    };
  }
  const freshness = effectiveMetadataFreshness(meta);
  const state = freshnessLabel(freshness);
  if (isCurrentMetadataAuthority(meta, freshness)) {
    return {
      verdict: 'authoritative',
      authoritative: true,
      state,
      summary: 'Graph authority current',
    };
  }
  return {
    verdict: freshnessVerdict(freshness),
    authoritative: false,
    state,
    summary: metadataSummary(meta, freshness),
    detail: metadataDetail(meta, freshness),
  };
}

export function assessGraphAuthority(authority?: GraphAuthority): AuthorityAssessment {
  if (!authority) {
    return {
      verdict: 'unknown',
      authoritative: false,
      state: 'unknown',
      summary: 'Authority metadata unavailable',
      detail: 'This response did not carry graph authority metadata.',
    };
  }
  const freshness = authority.graph_freshness_state || 'GRAPH_FRESHNESS_STATE_UNSPECIFIED';
  const state = freshnessLabel(freshness);
  if (authority.authoritative && freshness === 'GRAPH_FRESHNESS_STATE_CURRENT') {
    return {
      verdict: 'authoritative',
      authoritative: true,
      state,
      summary: 'Graph authority current',
    };
  }
  return {
    verdict: freshnessVerdict(freshness),
    authoritative: false,
    state,
    summary: `Graph authority ${state}`,
    detail:
      authority.graph_freshness_detail ||
      'This graph-backed answer is not authoritative.',
  };
}

export function effectiveMetadataFreshness(meta?: MetadataResponse): string {
  if (!meta) {
    return 'GRAPH_FRESHNESS_STATE_UNKNOWN';
  }
  const raw = meta.graph_freshness_state || 'GRAPH_FRESHNESS_STATE_UNSPECIFIED';
  if (raw !== 'GRAPH_FRESHNESS_STATE_UNSPECIFIED') {
    return raw;
  }
  const triples = Number(meta.triple_count || 0);
  if (triples === 0) {
    return 'GRAPH_FRESHNESS_STATE_EMPTY';
  }
  if (
    meta.build_provenance_state === 'BUILD_PROVENANCE_STATE_STAMPED' &&
    meta.seed_state === 'SEED_STATE_CURRENT' &&
    meta.live_store_contains_embedded_seed_marker
  ) {
    return 'GRAPH_FRESHNESS_STATE_CURRENT';
  }
  if (
    meta.seed_state === 'SEED_STATE_STALE' ||
    meta.live_store_contains_embedded_seed_marker === false
  ) {
    return 'GRAPH_FRESHNESS_STATE_STALE';
  }
  if (meta.build_provenance_state === 'BUILD_PROVENANCE_STATE_INCOMPLETE') {
    return 'GRAPH_FRESHNESS_STATE_CHECK_ERROR';
  }
  return 'GRAPH_FRESHNESS_STATE_UNKNOWN';
}

function isCurrentMetadataAuthority(meta: MetadataResponse, freshness: string): boolean {
  return (
    freshness === 'GRAPH_FRESHNESS_STATE_CURRENT' &&
    meta.build_provenance_state === 'BUILD_PROVENANCE_STATE_STAMPED' &&
    meta.seed_state === 'SEED_STATE_CURRENT' &&
    meta.live_store_contains_embedded_seed_marker === true &&
    Number(meta.triple_count || 0) > 0
  );
}

function metadataSummary(meta: MetadataResponse, freshness: string): string {
  switch (freshness) {
    case 'GRAPH_FRESHNESS_STATE_STALE':
      return 'Live graph stale — authority disabled';
    case 'GRAPH_FRESHNESS_STATE_UNKNOWN':
      return 'Graph freshness unknown — authority disabled';
    case 'GRAPH_FRESHNESS_STATE_EMPTY':
      return 'Graph empty — authority disabled';
    case 'GRAPH_FRESHNESS_STATE_CHECK_ERROR':
      return 'Graph check error — freshness unverified';
    default:
      if (meta.build_provenance_state === 'BUILD_PROVENANCE_STATE_DEV' || !meta.graph_build_commit) {
        return 'Dev build — provenance unstamped';
      }
      return `Graph authority ${freshnessLabel(freshness)}`;
  }
}

function metadataDetail(meta: MetadataResponse, freshness: string): string {
  if (meta.graph_freshness_detail) {
    return meta.graph_freshness_detail;
  }
  if (freshness === 'GRAPH_FRESHNESS_STATE_EMPTY' || Number(meta.triple_count || 0) === 0) {
    return 'The live store is empty and cannot serve graph-backed authority.';
  }
  if (meta.live_store_contains_embedded_seed_marker === false) {
    return 'The live store does not contain the expected embedded seed marker.';
  }
  if (meta.seed_state && meta.seed_state !== 'SEED_STATE_CURRENT') {
    return `Embedded seed state is ${freshnessLabel(meta.seed_state)}.`;
  }
  if (meta.build_provenance_state && meta.build_provenance_state !== 'BUILD_PROVENANCE_STATE_STAMPED') {
    return `Build provenance is ${freshnessLabel(meta.build_provenance_state)}.`;
  }
  return 'The dashboard cannot prove the served graph matches the current validated artifact.';
}

function freshnessVerdict(freshness: string): AuthorityVerdict {
  switch (freshness) {
    case 'GRAPH_FRESHNESS_STATE_CURRENT':
      return 'authoritative';
    case 'GRAPH_FRESHNESS_STATE_STALE':
      return 'stale';
    case 'GRAPH_FRESHNESS_STATE_EMPTY':
      return 'empty';
    case 'GRAPH_FRESHNESS_STATE_CHECK_ERROR':
      return 'degraded';
    default:
      return 'unknown';
  }
}

function freshnessLabel(value: string): string {
  return value
    .replace(/^GRAPH_FRESHNESS_STATE_/, '')
    .replace(/^BUILD_PROVENANCE_STATE_/, '')
    .replace(/^SEED_STATE_/, '')
    .toLowerCase();
}
