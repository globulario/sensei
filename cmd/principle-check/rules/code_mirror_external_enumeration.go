// SPDX-License-Identifier: AGPL-3.0-only

//go:build ignore
// +build ignore

// @awareness namespace=globular.awareness_graph
// @awareness component=build.principle_check.rules.code_mirror_external_enumeration
// @awareness file_role=ruleguard_rules_for_meta_code_must_not_mirror_external_enumerations
// @awareness enforces=globular.platform:invariant.hardcoded_set_must_derive_from_source

// Ruleguard rules for the per-instance invariant
//
//	hardcoded_set_must_derive_from_source
//
// (parent meta.code_must_not_mirror_external_enumerations)
//
// The bug shape this catches: a `map[string]bool` composite literal —
// Go's idiomatic hand-authored set. When this pattern appears in
// service code, it almost always mirrors an external enumeration
// (directory listing, proto enum, etcd prefix, package registry,
// Scylla keyspace set) as a local classification table. The moment
// the external source adds a member, the mirror diverges. No compile
// error, no test failure (the tests often share the same mirror),
// just silent runtime drift.
//
// Concrete incidents this would have surfaced:
//
//   - infraNames in artifact_handlers.go:1881 — 13 hardcoded infra
//     service names; source of truth is packages/specs/*.yaml with
//     metadata.kind=infrastructure. Comment confesses: "Source of
//     truth: packages/specs/*.yaml"
//   - criticalScyllaKeyspaces — 7 keyspace names; codebase creates 10.
//     3 keyspaces silently running at RF=1. (2026-06-06)
//   - commandPackages + skipSystemdUnits — mirrored from
//     packages/specs/*_cmd.yaml. Phantom "cli"; missing entries.
//
// The rule is intentionally broad within its scoped directories —
// it flags ALL map[string]bool literals because each one is a
// potential mirror. Sites that have companion exhaustiveness tests
// (TestCommandAndSkipUnitListsMatchSpecs, TestCriticalScylla-
// KeyspacesMatchSourceCreateStatements, etc.) are classified as
// EXCEPTION in the invariant's exception_files list.
package gorules

import "github.com/quasilyte/go-ruleguard/dsl"

// mapStringBoolSetLiteral catches any `map[string]bool{...}` composite
// literal — Go's idiomatic hand-authored string set. The match is on
// the literal expression itself (not the assignment), so it fires for
// both `x := map[string]bool{...}` and `var x = map[string]bool{...}`
// and `return map[string]bool{...}`.
func mapStringBoolSetLiteral(m dsl.Matcher) {
	m.Match(`map[string]bool{$*_}`).
		Report(`map[string]bool literal used as a hand-authored set — if this mirrors an external enumeration (directory listing, proto enum, registry, config file, Scylla keyspace set), the mirror will drift silently when the source gains entries. Derive the set from the source at startup/test time, or pair with a CI exhaustiveness test that walks the source and fails on drift. See meta.code_must_not_mirror_external_enumerations`)
}
