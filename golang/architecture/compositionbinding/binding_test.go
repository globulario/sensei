// SPDX-License-Identifier: AGPL-3.0-only

package compositionbinding

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/admission"
	"github.com/globulario/sensei/golang/architecture/tasksession"
)

// The fixture prepares a REAL task session through tasksession.Prepare rather
// than hand-writing its files. The digests this package resolves are computed
// by the task machinery, so a hand-built task would let the fixture and the
// code under test agree on a shape neither the real pipeline nor the validator
// would accept — and the test would prove only that two pieces of my own code
// match.
func prepareTask(t *testing.T, domain string) (root, taskDir string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root = t.TempDir()
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	write := func(rel, body string) {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, rel)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, rel), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git := func(args ...string) string {
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=fixture", "GIT_AUTHOR_EMAIL=fixture@example.invalid",
			"GIT_COMMITTER_NAME=fixture", "GIT_COMMITTER_EMAIL=fixture@example.invalid",
			"GIT_AUTHOR_DATE=2026-01-01T00:00:00Z", "GIT_COMMITTER_DATE=2026-01-01T00:00:00Z")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return strings.TrimSpace(string(out))
	}
	write("a.txt", "old\n")
	write(".gitignore", ".sensei/\n")
	git("init", "-q")
	git("add", "-A")
	git("commit", "-q", "-m", "base")
	revision := git("rev-parse", "HEAD")

	graphBytes := []byte("<https://example.com/s> <https://example.com/p> <https://example.com/o> .\n")
	graphPath := filepath.Join(root, ".sensei", "fixture-graph.nt")
	if err := os.MkdirAll(filepath.Dir(graphPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(graphPath, graphBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256Of(graphBytes)

	binding := architecture.ClaimDocumentBinding{
		RepositoryDomain: domain, Revision: revision, RevisionStatus: "resolved",
		GraphDigestSHA256: sum, GraphDigestStatus: "resolved",
	}
	fact := architecture.Fact{
		ID: "fact.binding.1", Kind: "topology", Subject: "a.txt",
		Predicate: "declares_symbol", Object: "Binding",
		Scope:      architecture.Scope{Repository: domain},
		Evidence:   architecture.Evidence{SourceFile: "a.txt", LineStart: 1, LineEnd: 1},
		Confidence: 1, Extractor: "fixture_extractor",
	}
	claims := architecture.ClaimDocument{
		SchemaVersion: "architecture.claims.v1", GeneratedBy: "fixture", Binding: binding,
		FactReceipts: []architecture.ClaimFactReceipt{{Fact: fact, Provenance: architecture.Provenance{
			RepositoryDomain: domain, RepositoryDomainStatus: "resolved",
			Revision: revision, RevisionStatus: "resolved",
			SourceDigest: sum, SourceDigestStatus: "resolved", SourceKind: "source_file",
		}}},
		Claims: []architecture.Claim{{
			ID: "claim.binding.1", Label: "binding claim",
			Statement:              architecture.ClaimStatement{Subject: "a.txt", Predicate: "declares_symbol", Object: "Binding"},
			Scope:                  architecture.ClaimScope{Repository: domain},
			ArchitecturalPlane:     architecture.PlaneObserved,
			AssertionOrigin:        architecture.OriginDerived,
			InferenceRule:          "fixture.rule.v1",
			EpistemicStatus:        architecture.StatusSupported,
			InvalidationConditions: []string{"a.txt no longer declares Binding"},
			PromotionStatus:        architecture.PromotionCandidate,
			HumanReviewRequired:    true,
			PremiseFacts:           []string{"fact.binding.1"},
		}},
	}
	data, err := yaml.Marshal(struct {
		ArchitectureClaims architecture.ClaimDocument `yaml:"architecture_claims"`
	}{claims})
	if err != nil {
		t.Fatal(err)
	}
	claimsPath := filepath.Join(root, ".sensei", "fixture-claims.yaml")
	if err := os.WriteFile(claimsPath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := tasksession.Prepare(tasksession.PrepareOptions{
		RepoRoot: root, RepositoryDomain: domain,
		Description: "composition binding fixture", Mode: admission.ModeModify,
		TaskClass: "implementation", RiskClass: "low_risk", DirectionRequirement: "preserve",
		GraphNT: graphPath, Claims: claimsPath,
		Files:     []tasksession.FileOperation{{Operation: "modify", Path: "a.txt"}},
		SetActive: true,
	})
	if err != nil {
		t.Fatalf("prepare task: %v", err)
	}
	taskDir = res.TaskDir
	if !filepath.IsAbs(taskDir) {
		taskDir = filepath.Join(root, taskDir)
	}
	return root, taskDir
}

// The core of checkpoint C: every digest comes from a governed document, and
// the binding says which one.
func TestResolveProducesEveryDimensionWithANamedOwner(t *testing.T) {
	root, taskDir := prepareTask(t, "example.com/binding")

	doc, err := Resolve(root, taskDir)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(doc.Resolutions) != len(Ordered) {
		t.Fatalf("resolved %d dimension(s), want %d", len(doc.Resolutions), len(Ordered))
	}
	for _, dim := range Ordered {
		d, ok := doc.Digest(dim)
		if !ok {
			t.Fatalf("%s was not resolved", dim)
		}
		if !isSHA256(d) {
			t.Errorf("%s digest is not a sha256: %q", dim, d)
		}
	}
	for _, r := range doc.Resolutions {
		if strings.TrimSpace(r.Producer) == "" {
			t.Errorf("%s names no producer", r.Dimension)
		}
	}
	if doc.RepositoryDomain != "example.com/binding" {
		t.Errorf("repository_domain = %q", doc.RepositoryDomain)
	}
	if doc.TaskID == "" || doc.Generation == "" {
		t.Errorf("binding does not identify its task/generation: %+v", doc)
	}
	// A freshly prepared task has published no generation. That state is
	// NAMED, never blank, so a prepare-time binding cannot be mistaken for one
	// whose generation could not be resolved.
	if doc.Generation != GenerationPrepareTime {
		t.Errorf("generation = %q, want %q for a task that has not advanced", doc.Generation, GenerationPrepareTime)
	}
}

// Review history HAS an owner. An earlier reading of this gap concluded it did
// not, which is why this is asserted explicitly rather than left implied: an
// ArchitectAnswer records who answered, when, and under what governance
// status, and that is the review record this system keeps.
func TestReviewHistoryResolvesFromArchitectAnswers(t *testing.T) {
	root, taskDir := prepareTask(t, "example.com/binding")
	doc, err := Resolve(root, taskDir)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, r := range doc.Resolutions {
		if r.Dimension == DimensionReview {
			found = true
			if !strings.Contains(r.Producer, "architect answers") {
				t.Errorf("review history producer = %q, want the architect-answer record", r.Producer)
			}
		}
	}
	if !found {
		t.Fatal("review history was not resolved")
	}
}

// Two resolutions of an unchanged generation must be identical, or the binding
// could not make composition reproducible — which is its only purpose.
func TestResolveIsDeterministic(t *testing.T) {
	root, taskDir := prepareTask(t, "example.com/binding")
	first, err := Resolve(root, taskDir)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Resolve(root, taskDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, dim := range Ordered {
		a, _ := first.Digest(dim)
		b, _ := second.Digest(dim)
		if a != b {
			t.Fatalf("%s is not deterministic: %s vs %s", dim, a, b)
		}
	}
}

// Different content must produce a different digest, or the binding would be a
// constant that happens to look like proof.
func TestDigestsTrackTheDocumentsTheyDescribe(t *testing.T) {
	root, taskDir := prepareTask(t, "example.com/binding")
	before, err := Resolve(root, taskDir)
	if err != nil {
		t.Fatal(err)
	}
	graphPath := filepath.Join(taskDir, "source", "graph.nt")
	data, err := os.ReadFile(graphPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(graphPath, append(data, []byte("<https://example.com/s2> <https://example.com/p> <https://example.com/o> .\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	after, err := Resolve(root, taskDir)
	if err != nil {
		t.Fatal(err)
	}
	b1, _ := before.Digest(DimensionGraph)
	b2, _ := after.Digest(DimensionGraph)
	if b1 == b2 {
		t.Fatal("the graph digest did not change when the graph did")
	}
}

// Fail closed on a missing task rather than emitting a well-formed binding
// nobody can trace.
func TestResolveRefusesAMissingTask(t *testing.T) {
	root := t.TempDir()
	if _, err := Resolve(root, filepath.Join(root, "nope")); err == nil {
		t.Fatal("a missing task produced a binding")
	}
}

// A partial or forged binding must not validate.
func TestValidateRefusesIncompleteOrUntraceableBindings(t *testing.T) {
	root, taskDir := prepareTask(t, "example.com/binding")
	good, err := Resolve(root, taskDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := Validate(good); err != nil {
		t.Fatalf("a resolved binding does not validate: %v", err)
	}

	for name, mutate := range map[string]func(*Document){
		"missing a dimension": func(d *Document) { d.Resolutions = d.Resolutions[:len(d.Resolutions)-1] },
		"unknown dimension":   func(d *Document) { d.Resolutions[0].Dimension = "vibes" },
		"duplicate dimension": func(d *Document) { d.Resolutions[1].Dimension = d.Resolutions[0].Dimension },
		"digest is not a sha256": func(d *Document) {
			d.Resolutions[0].Digest = "not-a-digest"
		},
		"producer unrecorded": func(d *Document) { d.Resolutions[0].Producer = "" },
		"wrong schema":        func(d *Document) { d.SchemaVersion = "sensei.compositionbinding.v99" },
	} {
		t.Run(name, func(t *testing.T) {
			tampered := good
			tampered.Resolutions = append([]Resolution{}, good.Resolutions...)
			mutate(&tampered)
			if err := Validate(tampered); err == nil {
				t.Fatal("an untraceable binding validated")
			}
		})
	}
}

// The five digests must not be interchangeable: a caller that swapped two
// would otherwise produce a binding that still validated.
func TestDimensionsAreDistinct(t *testing.T) {
	root, taskDir := prepareTask(t, "example.com/binding")
	doc, err := Resolve(root, taskDir)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]string{}
	for _, r := range doc.Resolutions {
		if prior, dup := seen[r.Digest]; dup && r.Dimension != prior {
			// Equal digests across dimensions are possible in principle (two
			// empty collections), so this is a warning-shaped assertion: the
			// PRODUCERS must still differ.
			t.Logf("note: %s and %s share a digest (both may be empty collections)", prior, r.Dimension)
		}
		seen[r.Digest] = r.Dimension
	}
	producers := map[string]bool{}
	for _, r := range doc.Resolutions {
		if producers[r.Producer] && r.Dimension != DimensionQuestions && r.Dimension != DimensionReview {
			t.Errorf("two dimensions claim the same producer: %s", r.Producer)
		}
		producers[r.Producer] = true
	}
}

// A blank generation must not validate: it is the shape that conflates "the
// task has not advanced" with "the generation could not be resolved".
func TestValidateRefusesABlankGeneration(t *testing.T) {
	root, taskDir := prepareTask(t, "example.com/binding")
	doc, err := Resolve(root, taskDir)
	if err != nil {
		t.Fatal(err)
	}
	doc.Generation = ""
	if err := Validate(doc); err == nil {
		t.Fatal("a binding with a blank generation validated")
	}
}
