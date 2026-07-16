// SPDX-License-Identifier: Apache-2.0

package prereview

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// fixtureDir is the on-disk fixture root, relative to this package directory.
const fixtureDir = "../../../docs/fixtures/architectural-pre-review/v1"

// baseDraft returns a minimal, structurally valid advisory draft. Finalizing it
// yields a ready_for_human_review report.
func baseDraft() PreReviewReport {
	return PreReviewReport{
		Binding: ReviewBinding{
			RepositoryDomain:     "github.com/example/project",
			BaseRevision:         "0000000000000000000000000000000000000001",
			BaseTreeDigestSHA256: "basetree0000000000000000000000000000000000000000000000000000aaaa",
			HeadRevision:         "0000000000000000000000000000000000000002",
			HeadTreeDigestSHA256: "headtree0000000000000000000000000000000000000000000000000000bbbb",
			DiffDigestSHA256:     "diff00000000000000000000000000000000000000000000000000000000cccc",
			PolicyIDs:            []string{"gate.default.v1"},
		},
		Coverage: CoverageSummary{Level: CoverageAdvisory, Available: []string{"diff", "graph_briefing"}},
		Summary:  ExecutiveSummary{Purpose: "Add a new HTTP route to the gateway."},
		Impact: ArchitecturalImpact{
			RiskClass: "architecture_sensitive",
			AffectedComponents: []ImpactItem{
				{ID: "component.gateway", Title: "HTTP gateway", Epistemic: EpistemicGoverned, EvidenceRefs: []string{"graph:component.gateway"}},
			},
		},
		Change: ChangeSummary{FilesModified: []string{"gin.go"}},
	}
}

// governedDraft extends baseDraft with a resolved, admitted, scope-verified
// governance surface at governed coverage.
func governedDraft() PreReviewReport {
	d := baseDraft()
	d.Coverage = CoverageSummary{Level: CoverageGoverned, Available: []string{"diff", "authority", "admission", "scope"}}
	d.Binding.TaskID = "task.example"
	d.Binding.LedgerHeadDigestSHA256 = "ledger000000000000000000000000000000000000000000000000000000dddd"
	d.Governance = GovernanceSummary{
		Actor:            "agent.sensei.local",
		VerifiedRoles:    []string{"role.repository_repair_agent"},
		AuthorityStatus:  "resolved",
		AdmissionStatus:  "admitted",
		CapabilityStatus: "consumed",
		ScopeStatus:      "scope_verified",
	}
	return d
}

// positiveBuilders builds each canonical positive fixture. Each returns a
// finalized, valid report.
func positiveBuilders(t *testing.T) map[string]PreReviewReport {
	t.Helper()
	out := map[string]PreReviewReport{}

	out["advisory-clean"] = mustFinalize(t, baseDraft())

	blocked := baseDraft()
	blocked.ReviewerAttention = []ReviewerAttentionItem{{
		ID: "attn.direction", Category: AttentionUnknownDirection,
		Question:     "Is this new writer a permanent ownership path or a temporary migration exception?",
		WhyItMatters: "It determines whether a new authority domain is introduced.",
		Blocking:     true, Severity: SeverityHigh, Epistemic: EpistemicUnknown,
		RelatedFiles: []string{"gin.go"}, ArchitecturalReach: 3, TaskRelevance: 3,
		AllowedAnswers: []string{"permanent", "temporary"}, ResolutionPath: "record architect decision",
	}}
	out["advisory-blocked"] = mustFinalize(t, blocked)

	out["governed-scope-verified"] = mustFinalize(t, governedDraft())

	gov := governedDraft()
	gov.Governance.AuthorityStatus = "unresolved"
	gov.Governance.AdmissionStatus = "waiting_governance"
	gov.Governance.CapabilityStatus = ""
	gov.Governance.ScopeStatus = ""
	out["governance-required"] = mustFinalize(t, gov)

	ev := governedDraft()
	ev.Proof = ProofSummary{
		RequiredSlots:   []string{"proof.behavior.route_ownership"},
		UnresolvedSlots: []string{"proof.behavior.route_ownership"},
		RequiredObligations: []ProofSlot{
			{ID: "proof.behavior.route_ownership", Title: "Route ownership preserved", Status: "unresolved", Epistemic: EpistemicUnknown},
		},
	}
	out["evidence-required"] = mustFinalize(t, ev)

	cert := governedDraft()
	cert.Coverage = CoverageSummary{Level: CoverageProofBound, Available: []string{"evidence", "proof", "certification"}}
	cert.Proof = ProofSummary{
		RequiredSlots:   []string{"proof.behavior.route_ownership"},
		DischargedSlots: []string{"proof.behavior.route_ownership"},
		RequiredObligations: []ProofSlot{
			{ID: "proof.behavior.route_ownership", Title: "Route ownership preserved", Status: "discharged", Epistemic: EpistemicObserved, EvidenceRefs: []string{"receipt:test.route"}},
		},
		Certification: &CertificationView{Verdict: "certified", ReceiptDigestSHA256: "cert0000000000000000000000000000000000000000000000000000000eeee", VerifiedAt: "2026-07-16T00:00:00Z"},
	}
	out["certified"] = mustFinalize(t, cert)

	term := governedDraft()
	term.Coverage = CoverageSummary{Level: CoverageTerminal, Available: []string{"evidence", "proof", "certification", "completion"}}
	term.Proof = ProofSummary{
		Certification: &CertificationView{Verdict: "certified", ReceiptDigestSHA256: "cert0000000000000000000000000000000000000000000000000000000eeee", VerifiedAt: "2026-07-16T00:00:00Z"},
	}
	term.Result = ResultArchitectureSummary{
		Available:               true,
		BaseGraphDigestSHA256:   "basegraph00000000000000000000000000000000000000000000000000ffff",
		ResultGraphDigestSHA256: "resgraph000000000000000000000000000000000000000000000000000f1f1",
		Completion:              &CompletionView{ReceiptDigestSHA256: "done00000000000000000000000000000000000000000000000000000000f2f2", CompletedAt: "2026-07-16T01:00:00Z"},
	}
	out["terminally-closed"] = mustFinalize(t, term)

	cv := governedDraft()
	cv.Epistemic = EpistemicSummary{
		Contradicted: []Statement{{ID: "stmt.owner", Claim: "Two authorities both claim to own gin.go."}},
	}
	out["cannot-verify"] = mustFinalize(t, cv)

	neural := baseDraft()
	neural.Epistemic = EpistemicSummary{
		ModelCandidates: []Statement{{ID: "cand.boundary", Claim: "Likely boundary change near the gateway."}},
	}
	neural.ReviewerAttention = []ReviewerAttentionItem{{
		ID: "attn.candidate", Category: AttentionModelCandidate,
		Question: "Possible ownership shift near the gateway — worth an architect glance?",
		Blocking: false, Severity: SeverityLow, Epistemic: EpistemicModelCandidate,
		ArchitecturalReach: 2, TaskRelevance: 1, ResolutionPath: "architect glance",
	}}
	out["neural-candidates"] = mustFinalize(t, neural)

	return out
}

// invalidBuilders builds intentionally-invalid reports. Each must fail Validate.
func invalidBuilders(t *testing.T) map[string]PreReviewReport {
	t.Helper()
	out := map[string]PreReviewReport{}

	forged := positiveBuilders(t)["certified"]
	forged.Proof.Certification.ReceiptDigestSHA256 = "" // certified verdict, no receipt
	out["caller-forged-certification"] = forged

	noComplete := positiveBuilders(t)["terminally-closed"]
	noComplete.Result.Completion.ReceiptDigestSHA256 = "" // terminally_closed, no completion receipt
	out["completion-without-receipt"] = noComplete

	wrongDiff := positiveBuilders(t)["advisory-clean"]
	wrongDiff.Binding.DiffDigestSHA256 = "tampereddiff00000000000000000000000000000000000000000000000dead" // ID no longer matches
	out["report-bound-to-wrong-diff"] = wrongDiff

	staleLedger := positiveBuilders(t)["governed-scope-verified"]
	staleLedger.Binding.LedgerHeadDigestSHA256 = "" // task-backed but unbound
	out["stale-ledger-head"] = staleLedger

	resultMismatch := positiveBuilders(t)["advisory-clean"]
	resultMismatch.Result = ResultArchitectureSummary{Available: true} // available but no graph digests
	out["result-graph-mismatch"] = resultMismatch

	passNoEvidence := positiveBuilders(t)["advisory-clean"]
	passNoEvidence.Protection.Invariants = []ProtectionItem{
		{ID: "inv.owner", Title: "Single owner", Severity: SeverityHigh, Status: "pass", Epistemic: EpistemicGoverned},
	}
	out["unresolved-marked-pass"] = passNoEvidence

	neuralGoverned := positiveBuilders(t)["advisory-clean"]
	neuralGoverned.Protection.Invariants = []ProtectionItem{
		{ID: "inv.candidate", Title: "Predicted invariant", Severity: SeverityMedium, Status: "at_risk", Epistemic: EpistemicModelCandidate, EvidenceRefs: []string{"model:x"}},
	}
	out["neural-candidate-as-governed"] = neuralGoverned

	dup := positiveBuilders(t)["advisory-clean"]
	dupItem := ReviewerAttentionItem{
		ID: "attn.a", Category: AttentionArchitectQuestion, Question: "Is this a permanent writer?",
		Blocking: false, Severity: SeverityMedium, Epistemic: EpistemicUnknown,
	}
	dupItem2 := dupItem
	dupItem2.ID = "attn.b"
	dup.ReviewerAttention = []ReviewerAttentionItem{dupItem, dupItem2}
	dup.Summary.ReviewerAttentionCount = 2
	out["duplicate-reviewer-questions"] = dup

	tempPath := positiveBuilders(t)["advisory-clean"]
	tempPath.ReportDigestSHA256 = "0000000000000000000000000000000000000000000000000000000000000000" // as if a volatile value leaked into the digest
	out["temporary-path-in-digest"] = tempPath

	badVocab := positiveBuilders(t)["advisory-clean"]
	badVocab.Coverage.Level = "bogus_level"
	out["invalid-unknown-vocabulary"] = badVocab

	return out
}

func mustFinalize(t *testing.T, d PreReviewReport) PreReviewReport {
	t.Helper()
	r, err := Finalize(d)
	if err != nil {
		t.Fatalf("finalize: %v", err)
	}
	return r
}

// loadFixture reads and decodes an on-disk fixture report.
func loadFixture(t *testing.T, name string) PreReviewReport {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(fixtureDir, name, "report.json"))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	var r PreReviewReport
	if err := json.Unmarshal(raw, &r); err != nil {
		t.Fatalf("decode fixture %s: %v", name, err)
	}
	return r
}

// TestWriteFixtures regenerates the on-disk fixtures. It is gated so ordinary
// test runs never write; run with PREREVIEW_WRITE_FIXTURES=1 to materialize.
func TestWriteFixtures(t *testing.T) {
	if os.Getenv("PREREVIEW_WRITE_FIXTURES") == "" {
		t.Skip("set PREREVIEW_WRITE_FIXTURES=1 to (re)write fixtures")
	}
	write := func(name string, r PreReviewReport) {
		dir := filepath.Join(fixtureDir, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", name, err)
		}
		raw, err := RenderJSON(r)
		if err != nil {
			t.Fatalf("render %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(dir, "report.json"), raw, 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	for name, r := range positiveBuilders(t) {
		write(name, r)
	}
	for name, r := range invalidBuilders(t) {
		write(filepath.Join("invalid", name), r)
	}
}
