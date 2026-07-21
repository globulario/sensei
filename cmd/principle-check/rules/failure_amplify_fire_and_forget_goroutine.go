// SPDX-License-Identifier: AGPL-3.0-only

//go:build ignore
// +build ignore

// @awareness namespace=globular.awareness_graph
// @awareness component=build.principle_check.rules.failure_amplify_goroutine
// @awareness file_role=ruleguard_rules_for_meta_failure_response_must_contract_not_amplify
// @awareness enforces=globular.platform:invariant.error_path.no_unbounded_fire_and_forget_goroutine

// Ruleguard rules for the per-instance invariant
//
//	error_path.no_unbounded_fire_and_forget_goroutine
//
// (parent meta.failure_response_must_contract_not_amplify)
//
// The bug shape this catches comes from the zombie-leader incident:
// when an event disconnect was treated as a transient error, the
// handler spawned `go func() { ... }()` to retry/recover inline. Under
// sustained outage, hundreds of those goroutines accumulated, each
// blocked on retries — CPU starvation, heartbeats froze, the leader
// went zombie because its own liveness loop couldn't get scheduled.
//
// The structural pattern:
//
//	if err != nil {
//	    go func() { ... }()        // ← unbounded amplification
//	}
//
// The fix is one of:
//   - Bounded worker pool with a buffered channel
//   - Singleflight gate so at most one in-flight reaction exists
//   - Synchronous handling (let the caller see the error)
//   - Errgroup with a context cancel for clean teardown
//
// Naked `go func()` inside an err-handling block has no upper bound
// on concurrent reactions. Each error multiplies the resource pressure
// rather than backing off.
package gorules

import "github.com/quasilyte/go-ruleguard/dsl"

// fireAndForgetGoroutineOnErrorPath catches `go func() { ... }()`
// invoked as a direct child of an `if err != nil` block. Under
// sustained failure, every error spawns a new goroutine — the
// amplification shape from the zombie-leader incident.
//
// The rule deliberately matches direct children only. A wrapped
// form (e.g. `if err != nil { workerPool.Submit(func() {...}) }`)
// uses a bounded primitive and is the correct pattern; it does not
// match this rule.
func fireAndForgetGoroutineOnErrorPath(m dsl.Matcher) {
	// Constrain $err to look like a Go err variable (named "err",
	// "*Err*", or assigned via an :=) so the rule doesn't fire on
	// `if srv.foo != nil { go bar() }` shapes — that's a nil-check
	// on an optional service, not an error path. The Where filter
	// uses Text matching to keep the rule lightweight; we add the
	// inline-assign shape separately for `if err := X(); err != nil`.
	m.Match(
		`if $err != nil { $*_; go func() { $*_ }(); $*_ }`,
		`if $err != nil { $*_; go $f($*_); $*_ }`,
	).Where(
		m["err"].Text.Matches(`^(err|.*Err)$`),
	).Report(`fire-and-forget goroutine spawned inside if-err block — unbounded amplification under sustained failure (zombie-leader bug class). Replace with a bounded worker pool, singleflight, errgroup, or synchronous handling. See meta.failure_response_must_contract_not_amplify`)

	// Inline-assignment form: `if err := f(); err != nil { go ... }`
	m.Match(
		`if $err := $_; $err != nil { $*_; go func() { $*_ }(); $*_ }`,
		`if $err := $_; $err != nil { $*_; go $f($*_); $*_ }`,
	).Where(
		m["err"].Text.Matches(`^(err|.*Err)$`),
	).Report(`fire-and-forget goroutine spawned inside if-err (inline-assign) block — same amplification class as above`)
}
