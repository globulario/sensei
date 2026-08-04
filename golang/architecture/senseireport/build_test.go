// SPDX-License-Identifier: AGPL-3.0-only

package senseireport

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/admission"
	"github.com/globulario/sensei/golang/architecture/closure"
	"github.com/globulario/sensei/golang/architecture/tasksession"
	"github.com/globulario/sensei/golang/rdf"
)

func writeModuleFixture(t *testing.T, root string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module github.com/example/project\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestBuildNoActiveTask(t *testing.T) {
	root := t.TempDir()
	writeModuleFixture(t, root)
	gitInitRepo(t, root)
	gitCommitAll(t, root, "initial")

	r, err := Build(root)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if r.CurrentWork.Active {
		t.Fatalf("expected no active task, got %+v", r.CurrentWork)
	}
	if r.CurrentWork.Note != "no active task" {
		t.Fatalf("expected literal note, got %q", r.CurrentWork.Note)
	}
	if r.CurrentWork.TaskID != "" || r.CurrentWork.Disposition != "" {
		t.Fatalf("expected no fabricated task id/disposition, got %+v", r.CurrentWork)
	}
	if r.Verification.RepositoryWideVerification != RepositoryWideVerificationNotRun {
		t.Fatalf("expected repository-wide verification to be NOT_RUN, got %q", r.Verification.RepositoryWideVerification)
	}
	if r.Identity.Repository.Key != "github.com/example/project" {
		t.Fatalf("expected module key from go.mod, got %q", r.Identity.Repository.Key)
	}
	if errs := Validate(r); len(errs) != 0 {
		t.Fatalf("Build produced an invalid report: %+v", errs)
	}
}

func gitTest(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func fileDigestHex(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func ntTriple(s, p, o string, iri bool) string {
	obj := `"` + strings.ReplaceAll(strings.ReplaceAll(o, `\`, `\\`), `"`, `\"`) + `"`
	if iri {
		obj = "<" + o + ">"
	}
	return "<" + s + "> <" + p + "> " + obj + " ."
}

// prepareActiveTaskFixture creates a real, governed active task via
// tasksession.Prepare -- the same production code path a real `sensei
// prepare-change` invocation uses -- so buildCurrentWork exercises real
// ControlStatus/AssessReadiness behavior rather than a hand-rolled shape
// that could silently drift from what those functions actually require.
func prepareActiveTaskFixture(t *testing.T) (root string, taskID string) {
	t.Helper()
	root = t.TempDir()
	writeModuleFixture(t, root)
	if err := os.WriteFile(filepath.Join(root, "gin.go"), []byte("package gin\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitInitRepo(t, root)
	gitTest(t, root, "add", ".")
	gitTest(t, root, "commit", "-m", "initial")

	graph := strings.Join([]string{
		ntTriple("https://globular.io/awareness#sourceFile/gin.go", rdf.PropType, rdf.ClassSourceFile, true),
		ntTriple("https://globular.io/awareness#sourceFile/gin.go", rdf.PropSourcePath, "gin.go", false),
		"",
	}, "\n")
	graphPath := filepath.Join(root, "graph.nt")
	if err := os.WriteFile(graphPath, []byte(graph), 0o644); err != nil {
		t.Fatal(err)
	}

	revision := strings.TrimSpace(gitHeadFor(t, root))
	domain := "github.com/example/project"
	provenance := architecture.Provenance{
		RepositoryDomain:       domain,
		RepositoryDomainStatus: architecture.RepositoryDomainResolved,
		Revision:               revision,
		RevisionStatus:         architecture.RevisionResolved,
		SourceDigest:           fileDigestHex(t, filepath.Join(root, "gin.go")),
		SourceDigestStatus:     architecture.SourceDigestResolved,
		SourceKind:             "source_file",
	}
	fact := architecture.Fact{
		ID:        "fact.senseireport-test",
		Kind:      "guard",
		Subject:   "gin.Engine",
		Predicate: "refuses_when",
		Object:    "route state is invalid",
		Scope: architecture.Scope{
			Repository: domain,
			Files:      []string{"gin.go"},
			Symbols:    []string{"gin.Engine"},
		},
		Evidence:   architecture.Evidence{SourceFile: "gin.go", LineStart: 1, LineEnd: 1},
		Confidence: 0.6,
		Extractor:  "senseireport_test",
		Provenance: &provenance,
	}
	claim := architecture.Claim{
		ID:                     "claim.senseireport-test",
		Label:                  "Engine rejects invalid route state",
		Statement:              architecture.ClaimStatement{Subject: "gin.Engine", Predicate: "refuses_when", Object: "route state is invalid"},
		Scope:                  architecture.ClaimScope{Repository: domain, Repo: domain, Files: []string{"gin.go"}, Symbols: []string{"gin.Engine"}},
		ArchitecturalPlane:     architecture.PlaneObserved,
		AssertionOrigin:        architecture.OriginDerived,
		EpistemicStatus:        architecture.StatusSupported,
		InferenceRule:          "rule.senseireport_test.v1",
		PremiseFacts:           []string{fact.ID},
		InvalidationConditions: []string{"The premise fact changes."},
		Confidence:             0.6,
		HumanReviewRequired:    true,
		PromotionStatus:        architecture.PromotionCandidate,
	}
	doc := architecture.ClaimDocument{
		SchemaVersion: "1",
		GeneratedBy:   "senseireport test",
		Binding: architecture.ClaimDocumentBinding{
			RepositoryDomain:  domain,
			Revision:          revision,
			RevisionStatus:    architecture.RevisionResolved,
			GraphDigestSHA256: fileDigestHex(t, graphPath),
			GraphDigestStatus: architecture.GraphDigestResolved,
		},
		FactReceipts: []architecture.ClaimFactReceipt{{Fact: fact, Provenance: provenance}},
		Claims:       []architecture.Claim{claim},
	}
	claimsData, err := architecture.MarshalCanonicalClaimDocumentYAML(doc)
	if err != nil {
		t.Fatalf("marshal project claims: %v", err)
	}
	claimsPath := filepath.Join(root, ".sensei", "project", "claims.yaml")
	if err := os.MkdirAll(filepath.Dir(claimsPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(claimsPath, claimsData, 0o644); err != nil {
		t.Fatalf("write project claims: %v", err)
	}

	res, err := tasksession.Prepare(tasksession.PrepareOptions{
		RepoRoot:             root,
		RepositoryDomain:     domain,
		Description:          "Ensure literal colon routes resolve consistently.",
		Mode:                 admission.ModeModify,
		TaskClass:            "literal_colon_route_consistency",
		RiskClass:            closure.RiskArchitectureSensitive,
		DirectionRequirement: closure.DirectionPreserve,
		Files: []tasksession.FileOperation{
			{Path: "gin.go", Operation: admission.OperationModify},
		},
		GraphNT:   graphPath,
		SetActive: true,
	})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	return root, res.TaskID
}

func gitHeadFor(t *testing.T, root string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

func TestBuildActiveTask(t *testing.T) {
	root, taskID := prepareActiveTaskFixture(t)

	r, err := Build(root)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if !r.CurrentWork.Active {
		t.Fatalf("expected an active task, got %+v", r.CurrentWork)
	}
	if r.CurrentWork.TaskID != taskID {
		t.Fatalf("expected task id %q, got %q", taskID, r.CurrentWork.TaskID)
	}
	if r.CurrentWork.Title != "Ensure literal colon routes resolve consistently." {
		t.Fatalf("expected task title from task-request.yaml, got %q", r.CurrentWork.Title)
	}
	switch r.CurrentWork.Disposition {
	case DispositionVerified, DispositionBlocked, DispositionUnverified, DispositionIncomplete:
	default:
		t.Fatalf("unexpected disposition %q", r.CurrentWork.Disposition)
	}
	if r.Verification.RepositoryWideVerification != RepositoryWideVerificationNotRun {
		t.Fatalf("expected repository-wide verification to remain NOT_RUN, got %q", r.Verification.RepositoryWideVerification)
	}
	if errs := Validate(r); len(errs) != 0 {
		t.Fatalf("Build produced an invalid report: %+v", errs)
	}
}
