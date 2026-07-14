// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture"
)

func TestAssessPlanesRequiresClaims(t *testing.T) {
	if code := runAssessPlanes(nil); code != 2 {
		t.Fatalf("code=%d, want 2", code)
	}
}

func TestAssessPlanesWritesStdoutOnlyByDefault(t *testing.T) {
	claims := writeAssessClaims(t, t.TempDir(), observedAssessDoc(t))
	out := captureStdout(t, func() {
		if code := runAssessPlanes([]string{"--claims", claims}); code != 0 {
			t.Fatalf("code=%d", code)
		}
	})
	if !strings.Contains(out, "architectural_plane_assessment:") {
		t.Fatalf("missing assessment output:\n%s", out)
	}
}

func TestAssessPlanesCheckFresh(t *testing.T) {
	dir := t.TempDir()
	claims := writeAssessClaims(t, dir, observedAssessDoc(t))
	opts := assessPlanesOptions{Claims: claims, GraphDigestStatus: architecture.GraphDigestNotRequested}
	rendered, _, err := buildAssessPlanesOutput(opts)
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "assessment.yaml")
	if err := os.WriteFile(out, rendered, 0o644); err != nil {
		t.Fatal(err)
	}
	if code := runAssessPlanes([]string{"--claims", claims, "--output", out, "--check"}); code != 0 {
		t.Fatalf("code=%d, want 0", code)
	}
}

func TestAssessPlanesCheckStale(t *testing.T) {
	dir := t.TempDir()
	claims := writeAssessClaims(t, dir, observedAssessDoc(t))
	out := filepath.Join(dir, "assessment.yaml")
	if err := os.WriteFile(out, []byte("stale\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := runAssessPlanes([]string{"--claims", claims, "--output", out, "--check"}); code != 1 {
		t.Fatalf("code=%d, want 1", code)
	}
}

func TestAssessPlanesRequireJustifiedPasses(t *testing.T) {
	claims := writeAssessClaims(t, t.TempDir(), observedAssessDoc(t))
	if code := runAssessPlanes([]string{"--claims", claims, "--require-justified"}); code != 0 {
		t.Fatalf("code=%d, want 0", code)
	}
}

func TestAssessPlanesRequireJustifiedFailsOnInvalid(t *testing.T) {
	claims := writeAssessClaims(t, t.TempDir(), invalidDesiredAssessDoc(t))
	if code := runAssessPlanes([]string{"--claims", claims, "--require-justified"}); code != 1 {
		t.Fatalf("code=%d, want 1", code)
	}
}

func TestAssessPlanesRejectsActiveAwarenessOutputPath(t *testing.T) {
	claims := writeAssessClaims(t, t.TempDir(), observedAssessDoc(t))
	if code := runAssessPlanes([]string{"--claims", claims, "--output", filepath.Join("docs", "awareness", "plane_assessment.yaml")}); code != 2 {
		t.Fatalf("code=%d, want 2", code)
	}
}

func TestAssessPlanesAllowsCandidateOutputPath(t *testing.T) {
	claims := writeAssessClaims(t, t.TempDir(), observedAssessDoc(t))
	if code := runAssessPlanes([]string{"--claims", claims, "--output", filepath.Join("docs", "awareness", "candidates", "plane_assessment.yaml"), "--check"}); code != 1 {
		t.Fatalf("code=%d, want stale/missing 1", code)
	}
}

func TestAssessPlanesDoesNotQueryLiveGraph(t *testing.T) {
	claims := writeAssessClaims(t, t.TempDir(), observedAssessDoc(t))
	if code := runAssessPlanes([]string{"--claims", claims}); code != 0 {
		t.Fatalf("code=%d", code)
	}
}

func TestAssessPlanesDoesNotMutateGraph(t *testing.T) {
	root, _ := filepath.Abs(".")
	before := statOptional(t, filepath.Join(root, ".sensei", "graph-authority.json"))
	claims := writeAssessClaims(t, t.TempDir(), observedAssessDoc(t))
	if code := runAssessPlanes([]string{"--claims", claims}); code != 0 {
		t.Fatalf("code=%d", code)
	}
	after := statOptional(t, filepath.Join(root, ".sensei", "graph-authority.json"))
	if before != after {
		t.Fatal("graph marker changed")
	}
}

func TestAssessPlanesDoesNotWriteSeed(t *testing.T) {
	root, _ := filepath.Abs(".")
	before := statOptional(t, filepath.Join(root, "golang", "server", "embeddata", "awareness.nt"))
	claims := writeAssessClaims(t, t.TempDir(), observedAssessDoc(t))
	if code := runAssessPlanes([]string{"--claims", claims}); code != 0 {
		t.Fatalf("code=%d", code)
	}
	after := statOptional(t, filepath.Join(root, "golang", "server", "embeddata", "awareness.nt"))
	if before != after {
		t.Fatal("seed changed")
	}
}

func TestAssessPlanesDoesNotModifyClaims(t *testing.T) {
	claims := writeAssessClaims(t, t.TempDir(), observedAssessDoc(t))
	before, err := os.ReadFile(claims)
	if err != nil {
		t.Fatal(err)
	}
	if code := runAssessPlanes([]string{"--claims", claims}); code != 0 {
		t.Fatalf("code=%d", code)
	}
	after, err := os.ReadFile(claims)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("claims modified")
	}
}

func TestAssessPlanesDoesNotModifyDialogue(t *testing.T) {
	dir := t.TempDir()
	claims := writeAssessClaims(t, dir, observedAssessDoc(t))
	dialogue := filepath.Join(dir, "dialogue.yaml")
	content := `architecture_dialogue:
  schema_version: "1"
  compiled_by: test
  binding:
    repository_domain: github.com/example/repo
    revision: abc123
    revision_status: resolved
    graph_digest_sha256: def456
    graph_digest_status: resolved
  open_questions: []
`
	if err := os.WriteFile(dialogue, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := runAssessPlanes([]string{"--claims", claims, "--dialogue", dialogue}); code != 0 {
		t.Fatalf("code=%d", code)
	}
	after, err := os.ReadFile(dialogue)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != content {
		t.Fatal("dialogue modified")
	}
}

func writeAssessClaims(t *testing.T, dir string, doc architecture.ClaimDocument) string {
	t.Helper()
	data, err := architecture.MarshalCanonicalClaimDocumentYAML(doc)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "claims.yaml")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func observedAssessDoc(t *testing.T) architecture.ClaimDocument {
	return assessClaimDoc(t, architecture.Claim{
		ID:                  "claim.observed",
		Label:               "observed",
		Statement:           architecture.ClaimStatement{Subject: "S", Predicate: "p", Object: "O"},
		Scope:               architecture.ClaimScope{Repository: "github.com/example/repo", Repo: "github.com/example/repo"},
		ArchitecturalPlane:  architecture.PlaneObserved,
		AssertionOrigin:     architecture.OriginDerived,
		EpistemicStatus:     architecture.StatusUnknown,
		InferenceRule:       "rule.test",
		PremiseFacts:        []string{"fact.guard"},
		Unknowns:            []string{"bounded"},
		Confidence:          0.5,
		HumanReviewRequired: true,
		PromotionStatus:     architecture.PromotionCandidate,
	}, architecture.Fact{
		ID:         "fact.guard",
		Kind:       "guard",
		Subject:    "S",
		Predicate:  "refuses_when",
		Object:     "O",
		Confidence: 0.8,
		Extractor:  "test",
		Provenance: &architecture.Provenance{
			RepositoryDomain:       "github.com/example/repo",
			RepositoryDomainStatus: architecture.RepositoryDomainResolved,
			Revision:               "abc123",
			RevisionStatus:         architecture.RevisionResolved,
			SourceDigestStatus:     architecture.SourceDigestUnavailable,
			SourceKind:             "test",
		},
	})
}

func invalidDesiredAssessDoc(t *testing.T) architecture.ClaimDocument {
	doc := observedAssessDoc(t)
	doc.Claims[0].ArchitecturalPlane = architecture.PlaneDesired
	doc.Claims[0].ID = "claim.invalid_desired"
	return assessNormalize(t, doc)
}

func assessClaimDoc(t *testing.T, claim architecture.Claim, fact architecture.Fact) architecture.ClaimDocument {
	p := *fact.Provenance
	return assessNormalize(t, architecture.ClaimDocument{
		SchemaVersion: "1",
		GeneratedBy:   "test",
		Binding: architecture.ClaimDocumentBinding{
			RepositoryDomain:  "github.com/example/repo",
			Revision:          "abc123",
			RevisionStatus:    architecture.RevisionResolved,
			GraphDigestSHA256: "def456",
			GraphDigestStatus: architecture.GraphDigestResolved,
		},
		FactReceipts: []architecture.ClaimFactReceipt{{Fact: fact, Provenance: p}},
		Claims:       []architecture.Claim{claim},
	})
}

func assessNormalize(t *testing.T, doc architecture.ClaimDocument) architecture.ClaimDocument {
	t.Helper()
	doc, err := architecture.NormalizeClaimDocument(doc)
	if err != nil {
		t.Fatal(err)
	}
	return doc
}
