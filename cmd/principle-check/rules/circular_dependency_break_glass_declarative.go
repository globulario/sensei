// SPDX-License-Identifier: AGPL-3.0-only

//go:build ignore
// +build ignore

// @awareness namespace=globular.awareness_graph
// @awareness component=build.principle_check.rules.circular_dependency_break_glass
// @awareness file_role=ruleguard_rules_for_meta_circular_dependency_must_have_break_glass
// @awareness enforces=globular.platform:invariant.deploy_self_install_must_not_be_break_glass

// Ruleguard rules for the per-instance invariant
//
//	deploy_self_install_must_not_be_break_glass
//
// (parent meta.circular_dependency_must_have_break_glass)
//
// The bug shape: a SERVICE service does its own install from a locally-
// built binary (hot-deploy), bypassing the normal CI-verified pipeline
// — when the pipeline IS the broken dependency, this looks like a
// recovery path but actually corrupts the cycle further (no ldflags,
// no checksum match, no signature).
//
// The canonical break-glass procedure is documented as a runbook
// (scp verified binary from healthy peer, manual install, return to
// normal pipeline). The runbook is NOT enforceable as code — but the
// FORBIDDEN shapes are: any local `go build` followed by `cp` into a
// /usr/lib/globular/bin/ path is the hot-deploy anti-pattern.
//
// The rule below catches the simplest manifestation: an exec.Command
// invocation of "go" followed in nearby code by a copy to a globular
// binary path. This is fundamentally a runbook-level concern; the rule
// is a thin marker that this principle exists in the graph and a
// future automation that does "go build && cp /usr/lib/globular/..."
// from production code surfaces as DRIFT.
//
// Today's sweep: 0 findings. The hot-deploy pattern lives in scripts
// (testcluster/, tools/), not in production code; correctly absent.
package gorules

import "github.com/quasilyte/go-ruleguard/dsl"

// goBuildInProductionCode catches `exec.Command("go", "build", ...)`
// invocations in production code. The legitimate uses are in
// test/build tooling (scripts/, testcluster/, tools/) which are out
// of the per-instance invariant's actor_writer_dirs scope.
func goBuildInProductionCode(m dsl.Matcher) {
	m.Match(
		`exec.Command("go", "build", $*_)`,
		`exec.CommandContext($_, "go", "build", $*_)`,
	).Report(`exec.Command("go", "build", ...) in production code — hot-deploying locally-built binaries is a forbidden break-glass pattern (no ldflags, no checksum match, no signature). The canonical break-glass is "scp CI-verified binary from healthy peer + manual install + return to normal pipeline." See meta.circular_dependency_must_have_break_glass`)
}
