// SPDX-License-Identifier: AGPL-3.0-only

package questiongen

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/closure"
	"github.com/globulario/sensei/golang/architecture/graphsnapshot"
	"github.com/globulario/sensei/golang/architecture/maintenance"
	"github.com/globulario/sensei/golang/architecture/plane"
)

// R-CLOSURE-01. The deadlock observed on github.com/davecourtois/persona: a
// derived observed claim pinned to a source digest; the file is then edited in
// the working tree (uncommitted). Maintenance correctly evaluates the claim as
// stale (premise.source_digest_changed). Closure then demanded an OpenQuestion
// and evidence for it, while question generation declined both as
// "self-evidenced" — about a source that had changed since it was read. No
// blocker had a path out.
//
// The pipeline below is the real one, stage by stage: git fixture → claim
// document → source edit → maintenance.Evaluate → plane.Assess →
// closure.Evaluate → Generate. Nothing is hand-assembled after the claim
// document, so the test fails for the same reason the live task failed.

const livenessGraph = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// livenessFixture commits state.go and returns a claim document whose single
// derived observed claim is pinned to the committed content.
func livenessFixture(t *testing.T) (string, architecture.ClaimDocument) {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "state.go"), []byte("package fixture\n\ntype Cache struct{ path string }\n\nfunc New(p string) *Cache { return &Cache{path: p} }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "test@example.com")
	runGit(t, root, "config", "user.name", "Test User")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "initial")
	rev := runGit(t, root, "rev-parse", "HEAD")
	digest, err := architecture.SourceDigestSHA256(root, "state.go")
	if err != nil {
		t.Fatal(err)
	}
	fact := architecture.Fact{
		ID: "fact.writer", Kind: "write", Subject: "fixture.New", Predicate: "writes", Object: "fixture.Cache.path",
		Scope:    architecture.Scope{Repository: "github.com/example/project", Files: []string{"state.go"}, Symbols: []string{"fixture.New"}},
		Evidence: architecture.Evidence{SourceFile: "state.go", LineStart: 5, LineEnd: 5}, Confidence: 0.6, Extractor: "test",
	}
	claim := architecture.Claim{
		ID: "claim.observed.writer", Label: "observed writer set",
		Statement:          architecture.ClaimStatement{Subject: "fixture.Cache.path", Predicate: "has_observed_writer_set", Object: "fixture.New"},
		Scope:              architecture.ClaimScope{Repository: "github.com/example/project", Files: []string{"state.go"}, Symbols: []string{"fixture.New"}},
		ArchitecturalPlane: architecture.PlaneObserved, AssertionOrigin: architecture.OriginDerived,
		EpistemicStatus: architecture.StatusSupported, InferenceRule: "rule.observed_writer_set.v1",
		PremiseFacts: []string{fact.ID}, InvalidationConditions: []string{"A source digest changes."},
		Confidence: 0.55, Freshness: "current", HumanReviewRequired: true, PromotionStatus: architecture.PromotionCandidate,
	}
	doc := architecture.ClaimDocument{
		SchemaVersion: "1", GeneratedBy: "test", Binding: livenessBinding(rev),
		FactReceipts: []architecture.ClaimFactReceipt{{Fact: fact, Provenance: architecture.Provenance{
			RepositoryDomain: "github.com/example/project", RepositoryDomainStatus: architecture.RepositoryDomainResolved,
			Revision: rev, RevisionStatus: architecture.RevisionResolved,
			SourceDigest: digest, SourceDigestStatus: architecture.SourceDigestResolved, SourceKind: "source_file",
		}}},
		Claims: []architecture.Claim{claim},
	}
	return root, doc
}

func livenessBinding(rev string) architecture.ClaimDocumentBinding {
	return architecture.ClaimDocumentBinding{
		RepositoryDomain: "github.com/example/project", Revision: rev, RevisionStatus: architecture.RevisionResolved,
		GraphDigestSHA256: livenessGraph, GraphDigestStatus: architecture.GraphDigestResolved,
	}
}

type livenessRun struct {
	maintenance maintenance.Result
	closure     closure.Report
	questions   Report
}

// assessAndAsk runs maintenance, plane, closure and question generation on the
// fixture exactly as a task session does.
func assessAndAsk(t *testing.T, root string, doc architecture.ClaimDocument) livenessRun {
	t.Helper()
	b := doc.Binding
	dialogue := &architecture.DialogueDocument{SchemaVersion: "1", CompiledBy: "test", Binding: b, OpenQuestions: []architecture.OpenQuestion{}}
	evidence := &maintenance.EvidenceStateDocument{SchemaVersion: "1", GeneratedBy: "test", Binding: b, Evidence: []maintenance.EvidenceState{}}
	m, err := maintenance.Evaluate(maintenance.Context{RepositoryRoot: root, Current: doc, Dialogue: dialogue, Evidence: evidence, ObservedBinding: b})
	if err != nil {
		t.Fatal(err)
	}
	p, err := plane.Assess(plane.Context{Claims: m.Document, Maintenance: &m.Report, Evidence: evidence, Dialogue: dialogue,
		GraphDigest: livenessGraph, GraphDigestStatus: architecture.GraphDigestResolved, GraphDigestVerified: true})
	if err != nil {
		t.Fatal(err)
	}
	req := closure.Request{SchemaVersion: closure.SchemaVersion, Binding: b, Scope: closure.Scope{
		Domain: "repository", TaskClass: "feature", RiskClass: closure.RiskArchitectureSensitive,
		AccessMode: closure.AccessWrite, DirectionRequirement: closure.DirectionNotApplicable, Files: []string{"state.go"},
	}}
	rep, err := closure.Evaluate(closure.Context{
		Request: req, Claims: m.Document, Maintenance: &m.Report, Plane: &p, Dialogue: dialogue, Evidence: evidence,
		GraphReceipt:   graphsnapshot.Receipt{Status: architecture.GraphDigestResolved, DigestSHA256: livenessGraph, Verified: true},
		RepositoryRoot: root, RepositoryRev: b.Revision, RepositoryStatus: architecture.RevisionResolved,
	})
	if err != nil {
		t.Fatal(err)
	}
	gen, err := Generate(Context{Closure: rep, Claims: m.Document, CreatedAt: "2026-11-26T00:00:00Z",
		ClosureAssessmentDigestSHA256: strings.Repeat("d", 64)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return livenessRun{maintenance: m, closure: rep, questions: gen.Report}
}

// questionDemand: actions only a question (or a human supplying evidence that
// a question asks for) can discharge.
var questionDemand = map[string]bool{"create_open_question": true, "answer_open_question": true, "add_evidence": true}

// undischargeable returns every claim blocker whose only way out is a
// question that question generation declined to create. That is the deadlock:
// an obligation with no transition able to satisfy it.
func undischargeable(run livenessRun) []string {
	asked := map[string]bool{}
	for _, g := range run.questions.Generated {
		asked[g.BlockerID] = true
	}
	for _, e := range run.questions.ExistingCoverage {
		asked[e.BlockerID] = true
	}
	declined := map[string]string{}
	for _, s := range run.questions.Skipped {
		declined[s.BlockerID] = s.Disposition
	}
	var out []string
	for _, bl := range run.closure.Blockers {
		if len(bl.ClaimIDs) == 0 || !questionDemand[bl.RequiredNextAction] || asked[bl.ID] {
			continue
		}
		if d, ok := declined[bl.ID]; ok {
			out = append(out, bl.Code+" ("+bl.RequiredNextAction+") declined as "+d)
		}
	}
	return out
}

// C1: the reproduction. It fails on the code that produced the PERSONA
// deadlock and must pass once every claim blocker has a dischargeable path.
func TestStaleSourceClaimNeverLeavesAnUndischargeableBlocker(t *testing.T) {
	root, doc := livenessFixture(t)
	if err := os.WriteFile(filepath.Join(root, "state.go"), []byte("package fixture\n\ntype Cache struct{ path string }\n\nfunc New(p string) *Cache { return &Cache{path: p} }\n\nfunc (c *Cache) Reset() { c.path = \"\" }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run := assessAndAsk(t, root, doc)
	var stale bool
	for _, ev := range run.maintenance.Report.ClaimEvaluations {
		stale = stale || ev.EvaluatedStatus == architecture.StatusStale
	}
	if !stale {
		t.Fatalf("fixture did not reproduce staleness: %+v", run.maintenance.Report.ClaimEvaluations)
	}
	if dead := undischargeable(run); len(dead) > 0 {
		t.Fatalf("closure left %d blocker(s) with no path out (INV-CLOSURE-LIVENESS):\n  %s", len(dead), strings.Join(dead, "\n  "))
	}
}
