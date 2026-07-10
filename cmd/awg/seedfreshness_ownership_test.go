// SPDX-License-Identifier: Apache-2.0

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
