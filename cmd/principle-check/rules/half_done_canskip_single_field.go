// SPDX-License-Identifier: Apache-2.0

//go:build ignore
// +build ignore

// @awareness namespace=globular.awareness_graph
// @awareness component=build.principle_check.rules.canskip_single_field
// @awareness file_role=ruleguard_rules_for_meta_half_done_must_not_look_done
// @awareness enforces=globular.platform:invariant.canskip_predicates_must_check_multiple_fields

// Ruleguard rules for the per-instance invariant
//
//	canskip_predicates_must_check_multiple_fields
//
// (parent meta.half_done_must_not_look_done)
//
// The bug shape (INC-2026-0012): canSkipDueToExistingState checked
// artifact_state but not manifest_json. A partial write (state stamped
// PUBLISHED, manifest_json still NULL from an interrupted sync)
// satisfied the single-field skip predicate, and the controller saw
// "proto: syntax error" on lookup forever.
//
// The principle: multi-step completion requires multi-field check. If a
// predicate decides "is this done?" by reading a single boolean / enum
// / state field, an interruption between writing that field and writing
// its related fields creates a permanent skip.
//
// The rule below catches the SHAPE that indicates a bug-prone canSkip:
// a function named `canSkip*` whose entire body is a single return
// expression with one comparison. The canonical post-fix shape has at
// least one intermediate check (if/switch/early-return) before the
// final return.
//
// Today's sweep: 0 findings — the 2 known canSkip* sites in the
// codebase both have multi-check bodies (canSkipInstallPackage has 7+
// checks; canSkipDueToExistingState has a switch + manifestJSONPresent
// guard post-INC-2026-0012). Pure regression detector for NEW
// canSkip* functions written too simply.
package gorules

import "github.com/quasilyte/go-ruleguard/dsl"

// canSkipSingleFieldReturn catches `func ... canSkip*(...) bool { return $a == $b }`
// — a one-line return-equality body. Any function with more than one
// statement (intermediate check, switch, early-return on a different
// condition) does not match this pattern.
func canSkipSingleFieldReturn(m dsl.Matcher) {
	m.Match(
		`func ($_ $_) $name($*_) bool { return $a == $b }`,
		`func $name($*_) bool { return $a == $b }`,
	).
		Where(m["name"].Text.Matches(`^(canSkip|isReady|isComplete|hasCompleted)`)).
		Report(`single-field return body in a completion predicate named "$name" — multi-step writes need multi-field checks. A partial write that touches the compared field but not its peers satisfies this skip and creates permanent half-done state (see INC-2026-0012). Add intermediate guards (manifest presence, peer-field non-empty, version+build+checksum) before the final return. See meta.half_done_must_not_look_done`)
}
