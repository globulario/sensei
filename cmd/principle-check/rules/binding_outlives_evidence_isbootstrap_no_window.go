// SPDX-License-Identifier: AGPL-3.0-only

//go:build ignore
// +build ignore

// @awareness namespace=globular.awareness_graph
// @awareness component=build.principle_check.rules.binding_outlives_evidence
// @awareness file_role=ruleguard_rules_for_meta_binding_outlives_evidence_until_invalidated
// @awareness enforces=globular.platform:invariant.isbootstrap_consumer_must_check_window

// Ruleguard rules for the per-instance invariant
//
//	isbootstrap_consumer_must_check_window
//
// (parent meta.binding_outlives_evidence_until_invalidated)
//
// The bug shape: code consumes `authCtx.IsBootstrap` to grant relaxed
// permissions, but doesn't itself verify the bootstrap WINDOW is still
// open. If an attacker / stale auth-context carries IsBootstrap=true
// past the 30-min bootstrap window, the binding outlives its evidence.
//
// The interceptor sets IsBootstrap based on loopback + window + allowlist
// (evidence-bound). Per-RPC consumers trust the bool. The principle
// reminds us: if anyone consumes IsBootstrap WITHOUT a fresh evidence
// check, they're relying on a binding whose evidence may have moved.
//
// The rule below catches the simplest bug shape: bare reads of
// `authCtx.IsBootstrap` outside the interceptor. Current consumers
// (rbac role-bindings handlers, audit log) are EXCEPTION sites — the
// interceptor sets the bool per-RPC, so the binding is FRESH for each
// call. Future code that caches an authCtx longer-lived (across multiple
// RPCs or a goroutine) would trip this rule and require explicit
// classification.
//
// Today's sweep: classifies known consumer files as EXCEPTION; any new
// consumer adds DRIFT requiring explicit per-file classification.
package gorules

import "github.com/quasilyte/go-ruleguard/dsl"

// isBootstrapConsumerWithoutWindowCheck catches `authCtx.IsBootstrap`
// reads. The current 4 consumers (rbac_role_bindings.go,
// ServerInterceptors.go, audit_log.go) are per-RPC consumers with
// fresh-evidence binding — classified EXCEPTION. New uses outside
// those files surface as DRIFT.
func isBootstrapConsumerWithoutWindowCheck(m dsl.Matcher) {
	// Match `$_.IsBootstrap` field reads without a type filter — the
	// security.AuthContext type doesn't resolve at rule-compile time
	// (the package isn't a dependency of the rule file's module).
	// The IsBootstrap field name is distinctive enough that false
	// positives are unlikely; the per-instance invariant's
	// actor_writer_dirs further scopes scanning.
	m.Match(`$x.IsBootstrap`).
		Report(`IsBootstrap consumed — the bool is set by the interceptor based on a loopback + 30-min window + allowlist; consuming it without re-checking the binding's freshness lets a stale auth-context carry bootstrap permissions past the window. Verify the auth-context is per-RPC (not cached across calls / goroutines). See meta.binding_outlives_evidence_until_invalidated`)
}
