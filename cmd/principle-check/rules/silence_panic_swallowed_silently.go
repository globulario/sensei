// SPDX-License-Identifier: AGPL-3.0-only

//go:build ignore
// +build ignore

// @awareness namespace=globular.awareness_graph
// @awareness component=build.principle_check.rules.silence_panic_swallowed
// @awareness file_role=ruleguard_rules_for_meta_silence_is_not_valid_for_unexpected
// @awareness enforces=globular.platform:invariant.panic_recovery_must_not_be_silent

// Ruleguard rules for the per-instance invariant
//
//	panic_recovery_must_not_be_silent
//
// (parent meta.silence_is_not_valid_for_unexpected)
//
// The bug shape: a defer-recover block that catches a panic but does
// NOTHING with it — no log, no metric, no error propagation. A future
// crash inside the protected goroutine then becomes invisible: the
// goroutine dies, the deferred recover swallows the panic, and the
// system continues as if nothing happened. The next observable
// symptom is "X stopped working" with no breadcrumb to root cause.
//
// Canonical good shape (cluster_controller_server/server.go::safeGo):
//
//	defer func() {
//	    if r := recover(); r != nil {
//	        log.Printf("panic in %s: %v\n%s", tag, r, debug.Stack())
//	        h.SetState(globular_service.SubsystemFailed)
//	        h.SetError(fmt.Sprintf("panic: %v", r))
//	    }
//	}()
//
// Bug shapes (all silent):
//
//	defer func() { _ = recover() }()
//	defer func() { recover() }()
//	defer func() { if r := recover(); r != nil { /* empty */ } }()
//
// Today's sweep: 0 findings. Pure regression detector.
package gorules

import "github.com/quasilyte/go-ruleguard/dsl"

// bareRecoverNoLog catches the three common silent-recover idioms.
func bareRecoverNoLog(m dsl.Matcher) {
	m.Match(
		`defer func() { _ = recover() }()`,
		`defer func() { recover() }()`,
		`defer func() { if $r := recover(); $r != nil { } }()`,
	).Report(`panic silently recovered — meta.silence_is_not_valid_for_unexpected. A panic caught with no log/metric/state-mutation produces an invisible failure: the goroutine dies but no breadcrumb survives. At minimum, log.Printf("panic in <tag>: %v\n%s", r, debug.Stack()) — see safeGo in cluster_controller_server/server.go for the canonical pattern.`)
}
