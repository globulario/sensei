// SPDX-License-Identifier: AGPL-3.0-only

package inference

import (
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture"
)

// boundContext is a binding that is itself fully resolved, so any unknown this
// test observes is attributable to the premise fact rather than to the context.
func boundContext() Context {
	return Context{Binding: architecture.ClaimDocumentBinding{
		RepositoryDomain: "example.test/repo",
		Revision:         strings.Repeat("a", 40), RevisionStatus: architecture.RevisionResolved,
		GraphDigestSHA256: strings.Repeat("b", 64), GraphDigestStatus: architecture.GraphDigestResolved,
	}}
}

func premiseWithDigest(digest string) architecture.Fact {
	return architecture.Fact{
		ID: "fact.1", Kind: "read", Subject: "a.B", Predicate: "reads", Object: "x",
		Scope:      architecture.Scope{Repository: "example.test/repo", Files: []string{"a.go"}},
		Evidence:   architecture.Evidence{SourceFile: "a.go", LineStart: 1, LineEnd: 1},
		Confidence: 0.9, Extractor: "probe",
		Provenance: &architecture.Provenance{
			RepositoryDomain: "example.test/repo", RepositoryDomainStatus: "resolved",
			// Not committed: the ONLY thing that could bind this premise to an
			// exact tree state is the per-file source digest.
			RevisionStatus:     architecture.RevisionUnavailable,
			SourceKind:         "source_file",
			SourceDigest:       digest,
			SourceDigestStatus: architecture.SourceDigestResolved,
		},
	}
}

// The status is not the evidence.
//
// A premise whose provenance claims a resolved source digest while carrying no
// digest is pinned to nothing. architecture.ValidateFact accepts that shape, and
// maintenance.VerifySourceReceipt — which reads the digest — calls the same fact
// unknown. This reader must not disagree with it and promote the claim to
// supported on the strength of a status string.
func TestUncommittedPremiseWithoutARealDigestIsNotSupported(t *testing.T) {
	status, unknowns := statusForPremises(boundContext(), []architecture.Fact{premiseWithDigest("")})
	if status != architecture.StatusUnknown {
		t.Fatalf("status = %s, want %s: an empty digest pins nothing", status, architecture.StatusUnknown)
	}
	if len(unknowns) == 0 || !strings.Contains(strings.Join(unknowns, " "), "digest") {
		t.Fatalf("the unknown does not name the missing digest: %v", unknowns)
	}
}

// A malformed digest is not a digest either: accepting any non-empty string
// would trade one unverifiable claim for another.
func TestUncommittedPremiseWithAMalformedDigestIsNotSupported(t *testing.T) {
	for _, bad := range []string{"not-a-digest", strings.Repeat("a", 63), strings.Repeat("A", 64), strings.Repeat("z", 64)} {
		status, _ := statusForPremises(boundContext(), []architecture.Fact{premiseWithDigest(bad)})
		if status != architecture.StatusUnknown {
			t.Fatalf("digest %q was accepted as pinning the file content", bad)
		}
	}
}

// The baseline: a real digest on an uncommitted tree IS a binding, and must
// still support the claim — otherwise this check would simply refuse everything
// and pass for the wrong reason.
func TestUncommittedPremiseWithARealDigestRemainsSupported(t *testing.T) {
	status, unknowns := statusForPremises(boundContext(), []architecture.Fact{premiseWithDigest(strings.Repeat("a", 64))})
	if status != architecture.StatusSupported {
		t.Fatalf("status = %s (%v), want supported: a resolved digest pins the content", status, unknowns)
	}
}

// And a committed revision still binds without any source digest at all.
func TestCommittedPremiseRemainsSupportedWithoutASourceDigest(t *testing.T) {
	f := premiseWithDigest("")
	f.Provenance.RevisionStatus = architecture.RevisionResolved
	f.Provenance.SourceKind = "revision"
	status, unknowns := statusForPremises(boundContext(), []architecture.Fact{f})
	if status != architecture.StatusSupported {
		t.Fatalf("status = %s (%v), want supported: a committed revision binds on its own", status, unknowns)
	}
}
