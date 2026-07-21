// SPDX-License-Identifier: AGPL-3.0-only

//go:build ignore
// +build ignore

// @awareness namespace=globular.awareness_graph
// @awareness component=build.principle_check.rules.timeout_is_a_decision_not_a_truth
// @awareness file_role=ruleguard_rules_for_meta_timeout_is_a_decision_not_a_truth

// Ruleguard rules for the per-instance invariant
//
//	deadline_exceeded_must_not_drive_definitive_node_state
//
// (parent meta.timeout_is_a_decision_not_a_truth)
//
// The bug shape (INC-2026-0004): a collector or doctor rule observes
// context.DeadlineExceeded and IMMEDIATELY classifies the remote
// node as "down" / "unreachable" / "absent." The principle says a
// timeout is the failure of an OBSERVATION, not of the observed
// actor — the node could be slow, blocked, or unreachable via that
// particular path while alive on another.
//
// The canonical good shape: log + emit-finding + best-effort retry
// or cross-check. The principle explicitly allows acting under
// uncertainty when forced, but the action must be REVERSIBLE and
// the audit trail must name the uncertainty.
//
// Today's sweep: the cluster_controller's lane-timeout handlers
// already record metrics-only and set lane phase to "TIMEOUT" rather
// than mutating cluster state. cluster_doctor's node_reachable rule
// surfaces a finding without auto-evicting. INC-2026-0004's specific
// gap (collector partial-snapshot misclassified absent nodes as
// known-down) was fixed and is the canonical shape this rule guards.
//
// Bug shape:
//
//	if errors.Is(err, context.DeadlineExceeded) {
//	    node.Status = "down"             // ← treats timeout as known-down
//	}
//
//	if err == context.DeadlineExceeded {
//	    removeMember(nodeID)              // ← timeout as proof of failure
//	}
//
// Canonical good shape:
//
//	if errors.Is(err, context.DeadlineExceeded) {
//	    slog.Warn("collector timed out", "node", nodeID, "err", err)
//	    snap.addError(source, op, err)    // observation failure, not node failure
//	    continue
//	}
package gorules

import "github.com/quasilyte/go-ruleguard/dsl"

// timeoutFollowedByDownClassification catches if-blocks that detect
// context.DeadlineExceeded and immediately set a state to "down" /
// "unreachable" / "failed". The rule matches the literal string
// values to keep the pattern narrow — assignments to fields with
// these specific string values are the bug class.
func timeoutFollowedByDownClassification(m dsl.Matcher) {
	m.Match(
		`if $err == context.DeadlineExceeded { $*_; $x = $val; $*_ }`,
		`if errors.Is($err, context.DeadlineExceeded) { $*_; $x = $val; $*_ }`,
		`if $err == context.DeadlineExceeded { $*_; $x.$f = $val; $*_ }`,
		`if errors.Is($err, context.DeadlineExceeded) { $*_; $x.$f = $val; $*_ }`,
	).
		Where(
			m["val"].Const &&
				m["val"].Type.Is("string") &&
				m["val"].Text.Matches(`"(down|unreachable|failed|absent|dead|gone|removed)"`),
		).
		Report(`assignment of status="$val" inside a DeadlineExceeded branch — a timeout is an OBSERVATION failure, not proof of remote failure. Use addError/log instead, OR cross-check with a second observer before mutating state. See meta.timeout_is_a_decision_not_a_truth`)
}

// timeoutFollowedByRemove catches the destructive variant — a
// DeadlineExceeded branch that calls something named *Remove* /
// *Evict* / *Drop* on a node identifier. Same principle.
func timeoutFollowedByRemove(m dsl.Matcher) {
	m.Match(
		`if $err == context.DeadlineExceeded { $*_; $f($*_) }`,
		`if errors.Is($err, context.DeadlineExceeded) { $*_; $f($*_) }`,
		`if $err == context.DeadlineExceeded { $*_; $obj.$f($*_) }`,
		`if errors.Is($err, context.DeadlineExceeded) { $*_; $obj.$f($*_) }`,
	).
		Where(
			m["f"].Text.Matches(`^(remove|evict|drop|delete|fence|markDown|markUnreachable)`),
		).
		Report(`destructive call $f() inside a DeadlineExceeded branch — a timeout cannot justify removing/fencing a node by itself. Cross-check with a second observer or escalate to a workflow with operator approval. See meta.timeout_is_a_decision_not_a_truth`)
}
