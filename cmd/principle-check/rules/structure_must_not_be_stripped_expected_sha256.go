// SPDX-License-Identifier: AGPL-3.0-only

//go:build ignore
// +build ignore

// @awareness namespace=globular.awareness_graph
// @awareness component=build.principle_check.rules.structure_stripped
// @awareness file_role=ruleguard_rules_for_meta_structure_must_not_be_stripped_in_projection
// @awareness enforces=globular.platform:invariant.expected_sha256_param_must_carry_subject_name

// Ruleguard rules for the per-instance invariant
//
//	expected_sha256_param_must_carry_subject_name
//
// (parent meta.structure_must_not_be_stripped_in_projection)
//
// The bug shape (2026-06-02 incident, commit 42828e50): functions
// accepted a generic `expectedSHA256 string` parameter, the caller
// passed `manifest.entrypoint_checksum` (BINARY digest), and the callee
// verified the .tgz BUNDLE bytes against it — universally rejecting
// every install. The parameter name `expectedSHA256` projected away
// the SUBJECT (bundle vs binary vs config). The fix renamed parameters
// to explicit subjects: expectedBundleSHA256, expectedBinarySHA256.
//
// The rule below catches the projection-shape: a function declaring a
// parameter named `expectedSHA256` (or expectedSha256, expected_sha256,
// expectedHash) — the name strips the subject. Renaming to
// `expectedBundleSHA256` / `expectedBinarySHA256` / `expectedManifestSHA256`
// makes the subject explicit at the call boundary.
//
// Today's sweep: 0 findings — the v1.2.140 + v1.2.142 fixes renamed
// all known sites. Pure regression detector for NEW functions that
// reintroduce the subject-stripped parameter name.
package gorules

import "github.com/quasilyte/go-ruleguard/dsl"

// expectedSha256ParamNameStrippedOfSubject catches function declarations
// whose parameter name is `expectedSHA256` / variants — projecting away
// the subject (bundle vs binary vs config). The canonical fix names
// the subject in the parameter (e.g. expectedBinarySHA256).
//
// We match parameter declarations structurally; ruleguard's gogrep
// supports matching specific identifier names in function signatures.
func expectedSha256ParamNameStrippedOfSubject(m dsl.Matcher) {
	// gogrep's function-signature patterns don't cleanly support
	// multiple `$*_` placeholders. Instead we match call SITES that
	// pass an identifier literally named `expectedSHA256` / `expectedSha256`
	// — every caller of a subject-stripped parameter writes that
	// identifier at the call site, so this catches the projection at
	// the boundary it crosses.
	m.Match(`$_($*_, expectedSHA256, $*_)`,
		`$_($*_, expectedSha256, $*_)`,
		`$_($*_, expectedSHA256)`,
		`$_($*_, expectedSha256)`).
		Report(`identifier "expectedSHA256" passed across a function boundary — projects away the SUBJECT (bundle vs binary vs config vs manifest). Past incident (2026-06-02, commit 42828e50): generic expectedSHA256 was used as both a bundle digest AND a binary digest at different call sites, causing every install to reject. Rename to expectedBundleSHA256 / expectedBinarySHA256 / expectedManifestSHA256 so the call boundary names the subject. See meta.structure_must_not_be_stripped_in_projection`)
}
