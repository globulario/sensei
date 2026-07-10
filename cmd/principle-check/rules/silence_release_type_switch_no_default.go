// SPDX-License-Identifier: Apache-2.0

//go:build ignore
// +build ignore

// @awareness namespace=globular.awareness_graph
// @awareness component=build.principle_check.rules.silence_release_type_switch
// @awareness file_role=ruleguard_rules_for_meta_silence_is_not_valid_for_unexpected
// @awareness enforces=globular.platform:invariant.release_type_switch_must_have_default

// Ruleguard rules for the per-instance invariant
//
//	release_type_switch_must_have_default
//
// (parent meta.silence_is_not_valid_for_unexpected)
//
// The bug shape: a type switch over the three Globular release types
// (ServiceRelease | InfrastructureRelease | ApplicationRelease) with
// no default clause. If a future release type is added (e.g.
// ComputeRelease — already a planned subsystem), the new type's items
// silently fall through the switch without being inspected. Callers
// that depend on the switch's output (resolved checksums, identity
// lookups, dispatch decisions) get "" / nil / zero-value instead of
// a loud "I don't know how to handle this type" signal.
//
// Concrete past violation: lookupResolvedEntrypointChecksum in
// release_runtime_convergence.go had no default; a ComputeRelease
// item would silently produce an empty checksum string, the convergence
// classifier would see "installed checksum doesn't match resolved
// checksum (empty)," and the release would be permanently flagged as
// drift with no operator-visible hint of the missing case.
//
// The fix added a default that logs the unknown type.
//
// The rule below matches a 3-arm release-type switch without a
// default clause. Adding a default clause extends the switch's case
// list to 4 arms and the structural pattern no longer matches.
package gorules

import "github.com/quasilyte/go-ruleguard/dsl"

// releaseTypeSwitchMissingDefault catches the 3-arm type switch over
// (Service|Infrastructure|Application)Release with no default case.
// The case order in the rule mirrors the canonical order in
// release_runtime_convergence.go; other orderings are NOT matched by
// this pattern but follow the same principle and should be fixed
// uniformly when found.
func releaseTypeSwitchMissingDefault(m dsl.Matcher) {
	m.Match(
		`switch $v := $obj.(type) {
		case *cluster_controllerpb.ServiceRelease:
			$*_
		case *cluster_controllerpb.InfrastructureRelease:
			$*_
		case *cluster_controllerpb.ApplicationRelease:
			$*_
		}`,
	).Report(`type switch over release types is missing a default case — a future release type (ComputeRelease, etc.) would be silently skipped, producing zero-value answers from this switch with no operator-visible signal. Add ` + "`default: log.Printf(\"unknown release item type %T\", v)`" + `. See meta.silence_is_not_valid_for_unexpected`)
}
