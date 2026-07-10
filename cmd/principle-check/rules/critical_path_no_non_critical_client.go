// SPDX-License-Identifier: Apache-2.0

//go:build ignore
// +build ignore

// @awareness namespace=globular.awareness_graph
// @awareness component=build.principle_check.rules.critical_path_dep
// @awareness file_role=ruleguard_rules_for_meta_critical_path_no_non_critical_dependency
// @awareness enforces=globular.platform:invariant.heartbeat_must_not_take_non_critical_dependencies

// Ruleguard rules for the per-instance invariant
//
//	heartbeat_must_not_take_non_critical_dependencies
//
// (parent meta.critical_path_no_non_critical_dependency)
//
// The bug shape: heartbeat / leader-liveness / connection-establishment
// code picks up a dependency on a non-critical service (repository,
// event bus, analytics, monitoring). When the non-critical thing fails,
// the critical path gets stuck — not by crashing, but by blocking,
// flooding, or silencing. Past incident: power-outage scenario where
// heartbeat Phase 3 called repository.ListArtifacts; ScyllaDB was down
// so the repository call hung; nodes were unreachable for 45 minutes.
//
// The fix (already in heartbeat.go): wrap the non-critical call in a
// goroutine + timeout-context + select pattern so the heartbeat
// continues even when the dependency is down.
//
// The rule below catches DIRECT uses of repository_client (and event_client,
// monitoring_client) symbols. The current bounded call in heartbeat.go is
// classified as EXCEPTION in the per-instance invariant — the wrapping
// goroutine+select makes the call safe. A future regression that adds
// an unbounded call surfaces as DRIFT.
//
// Scope is intentionally narrow: only the critical-path files
// (heartbeat.go, leader_election.go, leader_liveness.go) — these are
// the files where the bug class lives.
package gorules

import "github.com/quasilyte/go-ruleguard/dsl"

// nonCriticalClientInCriticalPath catches symbol uses from packages
// that should not appear in heartbeat / leader-liveness paths. The
// scope is enforced by the per-instance invariant's actor_writer_dirs;
// the rule itself fires on any of these imports.
func nonCriticalClientInCriticalPath(m dsl.Matcher) {
	m.Match(
		`repository_client.$_($*_)`,
		`event_client.$_($*_)`,
		`monitoring_client.$_($*_)`,
	).Report(`non-critical client used in a critical-path file — heartbeat / leader-liveness / connection-establishment must NOT depend on repository/event/monitoring services synchronously. If the call is bounded (goroutine + timeout + select), classify the file under exception_files in the per-instance invariant with the wrapping pattern documented. See meta.critical_path_no_non_critical_dependency`)
}
