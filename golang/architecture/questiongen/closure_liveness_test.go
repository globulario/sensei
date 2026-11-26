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
	writeState(t, root, stateV1)
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "test@example.com")
	runGit(t, root, "config", "user.name", "Test User")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "initial")
	return root, extractClaims(t, root, runGit(t, root, "rev-parse", "HEAD"))
}

const (
	stateV1 = "package fixture\n\ntype Cache struct{ path string }\n\nfunc New(p string) *Cache { return &Cache{path: p} }\n"
	stateV2 = stateV1 + "\nfunc (c *Cache) Reset() { c.path = \"\" }\n"
	stateV3 = stateV2 + "\nfunc (c *Cache) Path() string { return c.path }\n"
)

func writeState(t *testing.T, root, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, "state.go"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// extractClaims stands in for the extractor: it pins the claim's premise to
// the file's CURRENT content, which is what a re-extraction produces.
func extractClaims(t *testing.T, root, rev string) architecture.ClaimDocument {
	t.Helper()
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
	return architecture.ClaimDocument{
		SchemaVersion: "1", GeneratedBy: "test", Binding: livenessBinding(rev),
		FactReceipts: []architecture.ClaimFactReceipt{{Fact: fact, Provenance: architecture.Provenance{
			RepositoryDomain: "github.com/example/project", RepositoryDomainStatus: architecture.RepositoryDomainResolved,
			Revision: rev, RevisionStatus: architecture.RevisionResolved,
			SourceDigest: digest, SourceDigestStatus: architecture.SourceDigestResolved, SourceKind: "source_file",
		}}},
		Claims: []architecture.Claim{claim},
	}
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
	attestation closure.ResolutionAttestation
	claims      architecture.ClaimDocument
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
	rep, attestation, err := closure.EvaluateAttested(closure.Context{
		Request: req, Claims: m.Document, Maintenance: &m.Report, Plane: &p, Dialogue: dialogue, Evidence: evidence,
		GraphReceipt:   graphsnapshot.Receipt{Status: architecture.GraphDigestResolved, DigestSHA256: livenessGraph, Verified: true},
		RepositoryRoot: root, RepositoryRev: b.Revision, RepositoryStatus: architecture.RevisionResolved,
	})
	if err != nil {
		t.Fatal(err)
	}
	gen, err := Generate(Context{Closure: rep, Claims: m.Document, CreatedAt: "2026-11-26T00:00:00Z",
		ClosureAssessmentDigestSHA256: strings.Repeat("d", 64), ResolutionAttestation: attestation}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return livenessRun{maintenance: m, closure: rep, attestation: attestation, claims: m.Document, questions: gen.Report}
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
	writeState(t, root, stateV2) // uncommitted edit, as in the PERSONA working tree
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

func claimBlockers(run livenessRun) []closure.Blocker {
	var out []closure.Blocker
	for _, bl := range run.closure.Blockers {
		if len(bl.ClaimIDs) > 0 {
			out = append(out, bl)
		}
	}
	return out
}

func questionsFor(run livenessRun, blockerID string) int {
	n := 0
	for _, g := range run.questions.Generated {
		if g.BlockerID == blockerID {
			n++
		}
	}
	return n
}

// C2: a stale source-derived claim is routed to re-extraction — by Sensei —
// with no question manufactured and nothing addressed to a participant.
func TestStaleSourceClaimIsRoutedToReextractionWithoutAQuestion(t *testing.T) {
	root, doc := livenessFixture(t)
	writeState(t, root, stateV2)
	run := assessAndAsk(t, root, doc)
	bls := claimBlockers(run)
	if len(bls) == 0 {
		t.Fatal("a stale claim must still block: refreshing is required before closure")
	}
	for _, bl := range bls {
		if bl.ClaimResolution != string(closure.ResolutionStaleSource) {
			t.Fatalf("%s disposition=%q, want stale_source", bl.Code, bl.ClaimResolution)
		}
		if bl.RequiredNextAction != closure.ActionRefreshExtraction || closure.DischargePaths[bl.RequiredNextAction] != closure.PartySensei {
			t.Fatalf("%s routed to %q (%s); want refresh_extraction owned by Sensei", bl.Code, bl.RequiredNextAction, closure.DischargePaths[bl.RequiredNextAction])
		}
		if bl.Code == "closure.question.missing_artifact" {
			t.Fatalf("closure still demands a question for a stale source claim: %+v", bl)
		}
		if questionsFor(run, bl.ID) > 0 {
			t.Fatalf("a question was manufactured for %s", bl.ID)
		}
	}
	if len(run.closure.EvidencedClaims) != 0 {
		t.Fatalf("stale evidence was recorded as sufficient: %+v", run.closure.EvidencedClaims)
	}
}

// C3: after re-extraction pins the claim to the current content, it closes on
// recorded, traceable evidence — the new digest, not the old one.
func TestReextractedClaimClosesOnTraceableCurrentEvidence(t *testing.T) {
	root, doc := livenessFixture(t)
	old := doc.FactReceipts[0].Provenance.SourceDigest
	writeState(t, root, stateV2)
	refreshed := extractClaims(t, root, doc.Binding.Revision)
	run := assessAndAsk(t, root, refreshed)
	if bls := claimBlockers(run); len(bls) != 0 {
		t.Fatalf("a current source-derived claim still blocks: %+v", bls)
	}
	if len(run.closure.EvidencedClaims) != 1 {
		t.Fatalf("evidenced claims=%+v", run.closure.EvidencedClaims)
	}
	ec := run.closure.EvidencedClaims[0]
	if ec.ClaimID != "claim.observed.writer" || len(ec.SourceDigests) != 1 || ec.SourceDigests[0] == old ||
		ec.SourceDigests[0] != refreshed.FactReceipts[0].Provenance.SourceDigest {
		t.Fatalf("evidence is not the current digest: %+v (old %s)", ec, old)
	}
}

// C4: the generic path — a claim never edited closes on current evidence
// without a question. Nothing here names a particular file or claim.
func TestCurrentSourceDerivedClaimClosesWithoutAQuestion(t *testing.T) {
	root, doc := livenessFixture(t)
	run := assessAndAsk(t, root, doc)
	if bls := claimBlockers(run); len(bls) != 0 {
		t.Fatalf("blockers=%+v", bls)
	}
	if len(run.closure.EvidencedClaims) != 1 {
		t.Fatalf("the closing evidence was not recorded: %+v", run.closure.EvidencedClaims)
	}
}

// C5: genuinely unresolved claims still block, and are still asked.
func TestGenuinelyUnresolvedClaimsStillBlockAndAreAsked(t *testing.T) {
	cases := map[string]func(*architecture.ClaimDocument){
		// (An authored claim cannot appear in a generated claim document; the
		// authored case is covered at unit level in self_evidenced_test.go.)
		"intended claim": func(d *architecture.ClaimDocument) {
			d.Claims[0].ArchitecturalPlane = architecture.PlaneIntended
			d.Claims[0].EpistemicStatus = architecture.StatusUnknown
			d.Claims[0].Unknowns = []string{"Whether this writer set is the intended ownership."}
		},

		"premise with no resolved digest": func(d *architecture.ClaimDocument) {
			d.FactReceipts[0].Provenance.SourceDigest = ""
			d.FactReceipts[0].Provenance.SourceDigestStatus = architecture.SourceDigestUnavailable
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			root, doc := livenessFixture(t)
			mutate(&doc)
			run := assessAndAsk(t, root, doc)
			bls := claimBlockers(run)
			if len(bls) == 0 {
				t.Fatal("an unresolved claim stopped blocking")
			}
			asked := 0
			for _, bl := range bls {
				if bl.ClaimResolution == string(closure.ResolutionEvidencedCurrent) || bl.ClaimResolution == string(closure.ResolutionStaleSource) {
					t.Fatalf("%s mis-disposed as %s", bl.Code, bl.ClaimResolution)
				}
				asked += questionsFor(run, bl.ID)
			}
			if asked == 0 {
				t.Fatalf("no question was asked for an unresolved claim: %+v / %+v", bls, run.questions.Skipped)
			}
			if run.closure.Verdict == closure.VerdictClosed {
				t.Fatal("closure closed over an unresolved claim")
			}
		})
	}
}

// Extra (architect): the source changes AGAIN between re-extraction and
// closure. The refreshed claim is stale again; nothing closes silently.
func TestSourceChangingAgainAfterReextractionDoesNotCloseSilently(t *testing.T) {
	root, doc := livenessFixture(t)
	writeState(t, root, stateV2)
	refreshed := extractClaims(t, root, doc.Binding.Revision)
	writeState(t, root, stateV3)
	run := assessAndAsk(t, root, refreshed)
	if len(run.closure.EvidencedClaims) != 0 {
		t.Fatalf("closed on evidence that changed again: %+v", run.closure.EvidencedClaims)
	}
	bls := claimBlockers(run)
	if len(bls) == 0 || bls[0].RequiredNextAction != closure.ActionRefreshExtraction {
		t.Fatalf("want another refresh, got %+v", bls)
	}
	if dead := undischargeable(run); len(dead) > 0 {
		t.Fatalf("undischargeable: %v", dead)
	}
}

// Extra (architect): re-extraction fails (the claim document is unchanged).
// The assessment is deterministic — the same blockers with the same IDs — so
// convergence's no-effect rule (NoEffectInputLimit) ends it as stalled instead
// of an infinite refresh loop. And re-extraction that yields an unresolved
// claim falls back to an ordinary question, never to silent closure.
func TestFailedOrInconclusiveReextractionNeitherLoopsNorClosesSilently(t *testing.T) {
	root, doc := livenessFixture(t)
	writeState(t, root, stateV2)
	first := assessAndAsk(t, root, doc)
	second := assessAndAsk(t, root, doc) // "refresh" produced nothing new
	ids := func(r livenessRun) string {
		var out []string
		for _, b := range r.closure.Blockers {
			out = append(out, b.ID+"/"+b.RequiredNextAction)
		}
		return strings.Join(out, ",")
	}
	if ids(first) != ids(second) || ids(first) == "" {
		t.Fatalf("a failed refresh must reproduce the identical blocker set:\n%s\n%s", ids(first), ids(second))
	}

	inconclusive := extractClaims(t, root, doc.Binding.Revision)
	inconclusive.FactReceipts[0].Provenance.SourceDigest = ""
	inconclusive.FactReceipts[0].Provenance.SourceDigestStatus = architecture.SourceDigestUnavailable
	run := assessAndAsk(t, root, inconclusive)
	if len(run.closure.EvidencedClaims) != 0 || len(claimBlockers(run)) == 0 {
		t.Fatalf("an inconclusive re-extraction closed: evidenced=%+v", run.closure.EvidencedClaims)
	}
	if dead := undischargeable(run); len(dead) > 0 {
		t.Fatalf("undischargeable: %v", dead)
	}
}

// A claim whose premise is not a pinned source fact is never closed BY SOURCE
// EVIDENCE, whatever else maintenance concludes about it: the disposition is
// not widened past source-backed observations (forbidden fix
// widen_self_evidenced_suppression...).
func TestNonSourcePremiseIsNeverRecordedAsSourceEvidence(t *testing.T) {
	root, doc := livenessFixture(t)
	doc.FactReceipts[0].Provenance.SourceKind = "git_history"
	run := assessAndAsk(t, root, doc)
	if len(run.closure.EvidencedClaims) != 0 {
		t.Fatalf("a non-source premise was recorded as source evidence: %+v", run.closure.EvidencedClaims)
	}
	for _, bl := range claimBlockers(run) {
		if bl.ClaimResolution == string(closure.ResolutionEvidencedCurrent) {
			t.Fatalf("mis-disposed: %+v", bl)
		}
	}
}

// #387 review, finding A: a report whose provenance is not established — loaded
// from a file, or altered after evaluation — cannot suppress a question through
// its stamped fields. The authority of the decision is verified, never
// recomputed.
func TestUnattestedOrAlteredReportCannotSuppressQuestions(t *testing.T) {
	root, doc := livenessFixture(t)
	writeState(t, root, stateV2)
	run := assessAndAsk(t, root, doc)
	ask := func(rep closure.Report, att closure.ResolutionAttestation) Report {
		gen, err := Generate(Context{Closure: rep, Claims: run.claims, CreatedAt: "2026-11-26T00:00:00Z",
			ClosureAssessmentDigestSHA256: strings.Repeat("d", 64), ResolutionAttestation: att}, nil)
		if err != nil {
			t.Fatal(err)
		}
		return gen.Report
	}
	if !run.attestation.Covers(run.closure) {
		t.Fatal("the attestation does not cover the report it was issued for")
	}
	for name, rep := range map[string]Report{
		"no attestation (e.g. generate-questions --closure <file>)": ask(run.closure, closure.ResolutionAttestation{}),
		"report altered after evaluation": func() Report {
			altered := run.closure
			altered.Blockers = append([]closure.Blocker{}, run.closure.Blockers...)
			altered.Blockers[0].Summary += " (edited)"
			if run.attestation.Covers(altered) {
				t.Fatal("an altered report is still covered")
			}
			return ask(altered, run.attestation)
		}(),
	} {
		for _, s := range rep.Skipped {
			if s.ReasonCode == "questiongen.self_evidenced" || strings.Contains(s.Detail, "attested claim resolution") {
				t.Fatalf("%s: a stamped field suppressed a question: %+v", name, s)
			}
		}
	}
}
