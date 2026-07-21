// SPDX-License-Identifier: AGPL-3.0-only

//go:build ignore
// +build ignore

// @awareness namespace=globular.awareness_graph
// @awareness component=build.principle_check.rules.defer_in_for_loop
// @awareness file_role=ruleguard_rules_for_meta_write_creates_completion_obligation

// Ruleguard rules for the per-instance invariant
//
//	defer_in_for_loop_must_not_accumulate
//
// (parent meta.write_creates_completion_obligation)
//
// The bug shape: `defer` inside a `for` loop. Go defers run at
// FUNCTION exit, not loop-iteration exit. A defer added each
// iteration accumulates on the function's defer stack until the
// outer function returns. In a tight loop (per-row scan, per-event
// iteration), this is a real resource leak: file handles, etcd
// leases, Scylla sessions, mutex locks all pile up.
//
// Canonical good shape — wrap the body in a closure so each iteration
// has its own defer-scope:
//
//	for _, item := range items {
//	    func() {
//	        f, err := os.Open(item.path)
//	        if err != nil { return }
//	        defer f.Close()
//	        // ... use f ...
//	    }()
//	}
//
// Or call a helper that takes the per-item work and isolates the
// defers there.
//
// Bug shape:
//
//	for _, item := range items {
//	    f, err := os.Open(item.path)
//	    if err != nil { continue }
//	    defer f.Close()                  // ← accumulates
//	    // ... use f ...
//	}
package gorules

import "github.com/quasilyte/go-ruleguard/dsl"

// deferInsideForLoop catches `defer` statements that are direct
// children of a `for` block. Defers nested deeper inside a closure
// (the canonical fix shape) are NOT direct children and don't match.
func deferInsideForLoop(m dsl.Matcher) {
	m.Match(
		`for { $*_; defer $f($*_); $*_ }`,
		`for $_, $_ := range $_ { $*_; defer $f($*_); $*_ }`,
		`for $_ := range $_ { $*_; defer $f($*_); $*_ }`,
		`for $_; $_; $_ { $*_; defer $f($*_); $*_ }`,
	).Report(`defer inside a for-loop body — defers run at FUNCTION exit, not loop-iteration exit. Each iteration accumulates on the function's defer stack; in tight loops this leaks resources (file handles, etcd leases, mutex locks). Wrap the body in a closure (func() { defer ...; ... }()) or extract a helper so the defer fires per-iteration. See meta.write_creates_completion_obligation`)
}
