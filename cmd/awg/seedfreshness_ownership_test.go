// SPDX-License-Identifier: AGPL-3.0-only

package main

// B (#141): seed-ownership classification must key an invariant-object edge by the
// FULL invariant id, not the collapsed "invariant" family. Otherwise a SERVICES
// invariant edge on a source file that an AG-OWNED invariant also protects collides
// (subject+predicate+family) and is mis-classified as AG-owned drift, deadlocking
// the services PR (globulario/services#151 is the regression fixture).
//
// The matrix guards BOTH misclassification directions: AG-owned edges must still
// hard-fail on drift (cases 1, 4) while services-owned edges on shared subjects are
// tolerated (cases 2, 3, 5), and the literal-value owned-drift guarantee is
// preserved (case 6).

import "testing"

const (
	bSrc    = `<https://globular.io/awareness#sourceFile/golang%2Fcluster_controller%2Fcluster_controller_server%2Frelease_runtime_convergence.go>`
	bImpl   = `<https://globular.io/awareness#implements>`
	bAGInv  = `<https://globular.io/awareness#invariant/meta.identity_computation_must_be_invariant>` // AG-owned
	bSvcInv = `<https://globular.io/awareness#invariant/convergence.identity_is_build_id>`            // services-owned
)

func bTriple(s, p, o string) string { return s + " " + p + " " + o + " ." }

// Case 1 — AG-owned invariant edge on a services-file subject: drift stays OWNED.
func TestOwnership_AGInvariantEdge_StaysOwned(t *testing.T) {
	agEdge := bTriple(bSrc, bImpl, bAGInv)
	agOnly := nt(agEdge)
	committed := nt()       // seed missing the AG edge
	generated := nt(agEdge) // regen has it → drift
	owned, external := classifySeedDiff(committed, generated, agOnly)
	if len(owned) != 1 || len(external) != 0 {
		t.Fatalf("AG-owned invariant edge drift must be owned; got owned=%v external=%v", owned, external)
	}
}

// Case 2 — THE #151 case. A services-owned invariant edge on a source file that an
// AG-owned invariant also protects must be EXTERNAL (tolerated), not owned drift.
func TestOwnership_ServicesInvariantEdge_OnSharedSourceFile_IsExternal(t *testing.T) {
	agEdge := bTriple(bSrc, bImpl, bAGInv)   // AG invariant already protects this file
	svcEdge := bTriple(bSrc, bImpl, bSvcInv) // services invariant now also protects it
	agOnly := nt(agEdge)
	committed := nt(agEdge)
	generated := nt(agEdge, svcEdge) // svcEdge is the new drift
	owned, external := classifySeedDiff(committed, generated, agOnly)
	if len(owned) != 0 {
		t.Fatalf("services-owned invariant edge must NOT be owned (the #151 deadlock); got owned=%v", owned)
	}
	if len(external) != 1 || external[0] != svcEdge {
		t.Fatalf("services-owned invariant edge must be external; got %v", external)
	}
}

// Case 3 — a source file implementing BOTH an AG and a services invariant:
// classify per-edge (AG owned, services external).
func TestOwnership_FileImplementsBothInvariants_PerEdge(t *testing.T) {
	agEdge := bTriple(bSrc, bImpl, bAGInv)
	svcEdge := bTriple(bSrc, bImpl, bSvcInv)
	agOnly := nt(agEdge)
	committed := nt() // both are new drift
	generated := nt(agEdge, svcEdge)
	owned, external := classifySeedDiff(committed, generated, agOnly)
	if len(owned) != 1 || owned[0] != agEdge {
		t.Fatalf("AG edge must be owned; got owned=%v", owned)
	}
	if len(external) != 1 || external[0] != svcEdge {
		t.Fatalf("services edge must be external; got external=%v", external)
	}
}

// Case 4 — stale AG seed drift (an AG-owned edge removed) must still HARD-FAIL (owned).
func TestOwnership_AGEdgeRemoved_StillOwned(t *testing.T) {
	agEdge := bTriple(bSrc, bImpl, bAGInv)
	agOnly := nt(agEdge)
	committed := nt(agEdge) // seed has it
	generated := nt()       // regen dropped it → drift
	owned, external := classifySeedDiff(committed, generated, agOnly)
	if len(owned) != 1 || len(external) != 0 {
		t.Fatalf("removed AG-owned edge must stay owned (hard-fail); got owned=%v external=%v", owned, external)
	}
}

// Case 5 — legitimate cross-repo external drift (a services edge on a subject the
// AG corpus does not own) stays external (unchanged by B).
func TestOwnership_ServicesOnlyEdge_IsExternal(t *testing.T) {
	svcEdge := bTriple(bSrc, bImpl, bSvcInv)
	agOnly := nt() // AG corpus owns nothing about this subject
	committed := nt()
	generated := nt(svcEdge)
	owned, external := classifySeedDiff(committed, generated, agOnly)
	if len(owned) != 0 || len(external) != 1 {
		t.Fatalf("services-only edge must be external; got owned=%v external=%v", owned, external)
	}
}

// Case 6 — regression guard: a changed LITERAL value on an AG-owned subject+predicate
// must still be owned drift (B must NOT touch literal collapse).
func TestOwnership_LiteralValueChange_StaysOwned(t *testing.T) {
	label := `<http://www.w3.org/2000/01/rdf-schema#label>`
	oldLabel := bSrc + " " + label + ` "old" .`
	newLabel := bSrc + " " + label + ` "new" .`
	agOnly := nt(newLabel)
	committed := nt(oldLabel)
	generated := nt(newLabel)
	owned, external := classifySeedDiff(committed, generated, agOnly)
	if len(owned) != 2 || len(external) != 0 {
		t.Fatalf("changed literal value on owned edge must stay owned; got owned=%v external=%v", owned, external)
	}
}

// ── seed-marker exclusion (globulario/services#218, #219) ────────────────────
//
// The SeedBuild marker names the sha256 and triple count of the artifact it is
// stamped on. The awareness-graph repo commits a SELF-ONLY seed; paired-repo CI
// regenerates a COMBINED one. The two markers therefore differ by construction,
// and because agOnly IS the self-only build, the committed marker's subject is
// reproduced exactly by the ownership probe — so it classified as AG-owned drift
// and hard-failed embeddata-freshness on services master and every services PR
// alike. No commit on either side could reconcile it.
//
// Freshness must ignore the marker in BOTH directions. Marker integrity is
// seedmeta's job (digest + count must match the stamped body), not freshness's.

// markerLines returns the 6 self-describing triples for a synthetic digest.
func markerLines(digest, count string) []string {
	s := "<https://globular.io/awareness#seedBuild/sha256-" + digest + ">"
	return []string{
		bTriple(s, "<http://www.w3.org/1999/02/22-rdf-syntax-ns#type>", "<https://globular.io/awareness#SeedBuild>"),
		bTriple(s, "<http://www.w3.org/2000/01/rdf-schema#label>", `"Embedded seed sha256 `+digest[:12]+`"`),
		bTriple(s, "<https://globular.io/awareness#seedDigestSha256>", `"`+digest+`"`),
		bTriple(s, "<https://globular.io/awareness#seedTripleCount>", `"`+count+`"`),
		bTriple(s, "<https://globular.io/awareness#seedMarkerVersion>", `"v2"`),
		bTriple(s, "<https://globular.io/awareness#authoredIn>", `"generated:seed_marker"`),
	}
}

// Case 7 — the #218/#219 deadlock. Committed carries the self-only marker,
// generated carries the combined marker, and agOnly reproduces the self-only one.
// Neither marker may be reported as drift.
func TestOwnership_SeedMarker_DiffersAcrossBuildModes_IsNotDrift(t *testing.T) {
	agEdge := bTriple(bSrc, bImpl, bAGInv)
	selfOnly := markerLines("eb97beb98c8e09bee6b4c59434a2eaabaa111f1c350f6a0861330819c7bad826", "7865")
	combined := markerLines("9e9a2dd0000000000000000000000000000000000000000000000000000000ff", "185774")

	agOnly := nt(append([]string{agEdge}, selfOnly...)...)
	committed := nt(append([]string{agEdge}, selfOnly...)...)
	generated := nt(append([]string{agEdge}, combined...)...)

	owned, external := classifySeedDiff(committed, generated, agOnly)
	if len(owned) != 0 {
		t.Fatalf("seed marker must never be owned drift (the #218/#219 deadlock); got owned=%v", owned)
	}
	if len(external) != 0 {
		t.Fatalf("seed marker must not be reported as external context either; got external=%v", external)
	}
}

// Case 8 — excluding the marker must not blind the gate. A real owned drift
// sitting alongside a differing marker still fails.
func TestOwnership_SeedMarkerExclusion_DoesNotHideRealDrift(t *testing.T) {
	agEdge := bTriple(bSrc, bImpl, bAGInv)
	selfOnly := markerLines("eb97beb98c8e09bee6b4c59434a2eaabaa111f1c350f6a0861330819c7bad826", "7865")
	combined := markerLines("9e9a2dd0000000000000000000000000000000000000000000000000000000ff", "185774")

	agOnly := nt(append([]string{agEdge}, selfOnly...)...)
	committed := nt(selfOnly...) // agEdge genuinely missing → real drift
	generated := nt(append([]string{agEdge}, combined...)...)

	owned, _ := classifySeedDiff(committed, generated, agOnly)
	if len(owned) != 1 || owned[0] != agEdge {
		t.Fatalf("real AG-owned drift must survive marker exclusion; got owned=%v", owned)
	}
}

// Case 9 — the colliding-guardrail-id case. Both repos author
// docs/awareness/activation_rules.yaml and the ids are minted without a repo
// namespace, so guardrail/activation_empty_policy.low_risk is one IRI with two
// rdfs:comment values. The services value must be EXTERNAL: AG's regeneration
// never emits it, so no AG commit could ever resolve it as staleness.
func TestOwnership_CollidingGuardrailID_ServicesLiteralIsExternal(t *testing.T) {
	subj := `<https://globular.io/awareness#guardrail/activation_empty_policy.low_risk>`
	comment := `<http://www.w3.org/2000/01/rdf-schema#comment>`
	agComment := bTriple(subj, comment, `"Typo fix, formatting, comment, import reorder"`)
	svcComment := bTriple(subj, comment, `"Edit changes no behavior (formatting, typos, comments, import reorder, dependency bump with no API change)."`)

	agOnly := nt(agComment)
	committed := nt(agComment)             // AG's self-only seed, current
	generated := nt(agComment, svcComment) // combined build carries both values

	owned, external := classifySeedDiff(committed, generated, agOnly)
	if len(owned) != 0 {
		t.Fatalf("services literal on a colliding guardrail id must not be owned drift (#218/#219); got owned=%v", owned)
	}
	if len(external) != 1 || external[0] != svcComment {
		t.Fatalf("services literal must be external context; got external=%v", external)
	}
}

// Case 10 — the exact-line rule must not blind ADDED owned drift. A genuinely
// stale seed (AG emits a line the committed seed lacks) still hard-fails, even
// when the paired repo also mints the same subject+predicate.
func TestOwnership_StaleSeed_OnCollidingGuardrailID_StaysOwned(t *testing.T) {
	subj := `<https://globular.io/awareness#guardrail/activation_empty_policy.low_risk>`
	comment := `<http://www.w3.org/2000/01/rdf-schema#comment>`
	agNew := bTriple(subj, comment, `"AG rewrote this tier"`)
	svcComment := bTriple(subj, comment, `"services text"`)

	agOnly := nt(agNew)
	committed := nt() // seed predates AG's rewrite → genuinely stale
	generated := nt(agNew, svcComment)

	owned, external := classifySeedDiff(committed, generated, agOnly)
	if len(owned) != 1 || owned[0] != agNew {
		t.Fatalf("genuine AG staleness must stay owned even on a colliding id; got owned=%v", owned)
	}
	if len(external) != 1 || external[0] != svcComment {
		t.Fatalf("services literal must stay external; got external=%v", external)
	}
}
