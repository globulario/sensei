// SPDX-License-Identifier: Apache-2.0

//go:build ignore
// +build ignore

// @awareness namespace=globular.awareness_graph
// @awareness component=build.principle_check.rules.assertions_scope
// @awareness file_role=ruleguard_rules_for_meta_assertions_must_carry_their_scope
// @awareness enforces=globular.platform:invariant.cluster_event_must_carry_node_or_cluster_scope

// Ruleguard rules for the per-instance invariant
//
//	cluster_event_must_carry_node_or_cluster_scope
//
// (parent meta.assertions_must_carry_their_scope)
//
// The bug shape (INC-2026-0004): cluster events emitted without
// node_id / cluster_id / scope tag. Consumers (doctor, dashboard)
// aggregate the events across nodes assuming each represents a
// cluster-wide truth. Per-node findings get aggregated as cluster-
// wide, producing 100+ false events/min.
//
// The fix: every emitClusterEvent payload must include a scope marker
// (node_id for per-node, cluster_id for cluster-wide, plus a
// "scope" key naming which it is).
//
// The rule below is a placeholder. The structural shape "map literal
// includes a scope key" is hard to express in ruleguard without
// scanning the map's contents. We declare the per-instance invariant
// as a documentation+regression marker — the meta-principle is on
// status: candidate in the parent YAML, so a thin scanner is
// appropriate.
//
// The narrow rule below matches the literal call
// `emitClusterEvent($_, $_)` so future regressions that strip the
// payload signature are noticed. A scope-content check would require
// map-literal inspection which gogrep doesn't do well.
//
// Today's sweep: classifies known emit sites as EXCEPTION (they all
// pass map[string]interface{} payloads with node_id/cluster_id keys
// — verified by inspection 2026-06-05).
package gorules

import "github.com/quasilyte/go-ruleguard/dsl"

// emitClusterEventWithoutPayload catches `emitClusterEvent` calls
// missing a payload argument. The two-argument form (event_name +
// payload-map) is the canonical shape; a one-argument form would
// strip the scope completely. ruleguard can't verify the payload-
// map's contents structurally, so this rule is a thin baseline.
func emitClusterEventWithoutPayload(m dsl.Matcher) {
	m.Match(`$_.emitClusterEvent($name)`).
		Report(`emitClusterEvent called with only an event name — no payload, no scope tag. Per meta.assertions_must_carry_their_scope, every cluster event must include a scope marker (node_id / cluster_id) so doctor/dashboard consumers can attribute the assertion correctly. Pass a map[string]interface{} with at least one of: node_id, cluster_id, "scope"`)
}
