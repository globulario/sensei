// SPDX-License-Identifier: Apache-2.0

package questionpromotion

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture/admission"
	qd "github.com/globulario/sensei/golang/architecture/questiondisposition"
)

// TestEndToEndIdentityCrosslinks proves the committed receipt binds the exact
// lineage across every store, with distinct authorities and recomputing identities.
func TestEndToEndIdentityCrosslinks(t *testing.T) {
	p := seedPromotable(t)
	res, err := Promote(context.Background(), p.request())
	if err != nil || res.Outcome != OutcomeCommitted {
		t.Fatalf("promote: %v (%s)", err, res.Outcome)
	}
	rc := *res.Receipt

	// Cross-check against the recorded disposition.
	rd, err := qd.LoadRecordedDisposition(p.TaskDir, p.DispositionDigest)
	if err != nil {
		t.Fatal(err)
	}
	d := rd.Receipt
	checks := map[string][2]string{
		"disposition_receipt_digest": {rc.QuestionDispositionReceiptDigestSHA256, p.DispositionDigest},
		"disposition_entry_digest":   {rc.DispositionEntryDigestSHA256, rd.EntryDigestSHA256},
		"question_id":                {rc.QuestionID, d.QuestionID},
		"answer_id":                  {rc.AnswerID, d.AnswerID},
		"answer_bytes_digest":        {rc.AnswerBytesDigestSHA256, d.AnswerBytesDigestSHA256},
		"result_binding_digest":      {rc.ResultBindingDigestSHA256, d.ResultBindingDigestSHA256},
		"task_id":                    {rc.Task.ID, d.Task.ID},
		"session_id":                 {rc.Task.SessionID, d.Task.SessionID},
	}
	for name, pair := range checks {
		if pair[0] != pair[1] || pair[0] == "" {
			t.Errorf("%s does not cross-link: %q vs %q", name, pair[0], pair[1])
		}
	}
	// Distinct disposition vs promotion authority.
	if rc.DispositionAuthorityGrantID == rc.PromotionAuthorityGrantID {
		t.Fatal("disposition and promotion authority grants collapsed")
	}
	if rc.PromotionAuthorityGrantID != GrantGovernedPromotion || rc.DispositionAuthorityGrantID != qd.GrantQuestionDisposition {
		t.Fatalf("wrong grants: promotion %q disposition %q", rc.PromotionAuthorityGrantID, rc.DispositionAuthorityGrantID)
	}
	// Governed node corresponds to kind+id.
	if rc.GovernedNodeIRI != GovernedNodeIRIFor(rc.GovernedTargetKind, rc.CanonicalRecordID) {
		t.Fatal("governed node IRI does not correspond")
	}
	// Lineage + receipt digest + committed identity all recompute.
	if lin, _ := ComputeLineageID(rc); lin != rc.PromotionLineageID || lin != res.PromotionLineageID {
		t.Fatal("lineage id does not recompute")
	}
	if dg, _ := Digest(rc); dg != rc.ReceiptDigestSHA256 || dg != res.ReceiptDigestSHA256 {
		t.Fatal("receipt digest does not recompute")
	}
	if rc.CommittedCausalIdentitySHA256 != res.CommittedCausalIdentitySHA256 || rc.CommittedCausalIdentitySHA256 == "" {
		t.Fatal("committed causal identity missing")
	}
	// Graph identities bound.
	for name, v := range map[string]string{
		"pre_manifest": rc.PreMutationManifestDigestSHA256, "post_manifest": rc.PostMutationManifestDigestSHA256,
		"build_input": rc.GraphBuildInputDigestSHA256, "graph_byte": rc.PersistedGraphByteDigestSHA256,
		"graph_semantic": rc.GraphSemanticDigestSHA256, "marker": rc.MarkerDigestSHA256, "mutation": rc.CanonicalMutationDigestSHA256,
	} {
		if !isHex64(v) {
			t.Errorf("%s identity not bound: %q", name, v)
		}
	}
}

// TestAuthorityNonCollapse proves no single durable artifact establishes reusable
// truth: with any conjunct broken, a replay fails closed rather than returning
// committed/exact_replay.
func TestAuthorityNonCollapse(t *testing.T) {
	cases := map[string]func(t *testing.T, p promotable, res PromoteResult){
		"receipt_file_deleted": func(t *testing.T, p promotable, res PromoteResult) {
			os.Remove(filepath.Join(promotionDirOf(p, res.PromotionLineageID), receiptFileName))
		},
		"governed_source_drift": func(t *testing.T, p promotable, res PromoteResult) {
			// Remove the promoted record from source (graph still has it).
			inv := filepath.Join(p.Repo, "docs", "awareness", "invariants.yaml")
			data, _ := os.ReadFile(inv)
			os.WriteFile(inv, []byte(strings.Split(string(data), "invariants:")[0]+"invariants:\n"), 0o644)
		},
		"marker_tampered": func(t *testing.T, p promotable, res PromoteResult) {
			os.WriteFile(filepath.Join(p.Repo, ".sensei", "graph-authority.json"), []byte(`{"digest_sha256":"x","marker_iri":"y","triple_count":1}`), 0o644)
		},
	}
	for name, breakIt := range cases {
		t.Run(name, func(t *testing.T) {
			p := seedPromotable(t)
			res, err := Promote(context.Background(), p.request())
			if err != nil || res.Outcome != OutcomeCommitted {
				t.Fatalf("first commit: %v (%s)", err, res.Outcome)
			}
			breakIt(t, p, res)
			replay, _ := Promote(context.Background(), p.request())
			if replay.Outcome == OutcomeCommitted || replay.Outcome == OutcomeExactReplay {
				t.Fatalf("%s: broken conjunct still authoritative (%s)", name, replay.Outcome)
			}
		})
	}
}

// TestRefusalZeroSideEffects proves a pre-mutation refusal touches no governed
// source, graph, promotion journal, or task ledger.
func TestRefusalZeroSideEffects(t *testing.T) {
	p := seedPromotable(t)
	taskBefore, _ := admission.TaskLedgerHead(p.TaskDir)
	req := p.request()
	req.IdentityRoot = t.TempDir() // unenrolled → authority refusal
	res, _ := Promote(context.Background(), req)
	if res.Outcome != OutcomeAuthorityRefusal {
		t.Fatalf("outcome = %s, want authority_refusal", res.Outcome)
	}
	assertNoGovernedRecord(t, p)
	if _, err := os.Stat(filepath.Join(p.Repo, ".sensei", "project", "graph.nt")); err == nil {
		t.Fatal("refusal built a graph")
	}
	if _, err := os.Stat(filepath.Join(p.Repo, ".sensei", "project", "promotions")); err == nil {
		t.Fatal("refusal created a promotion journal")
	}
	if after, _ := admission.TaskLedgerHead(p.TaskDir); after != taskBefore {
		t.Fatal("refusal mutated the task ledger")
	}
}

// TestNonReusableDispositionRefused: a task_local disposition is ineligible.
func TestNonReusableDispositionRefused(t *testing.T) {
	env := seedDispositionOnly(t, qd.DispositionAnswered, qd.ReusabilityTaskLocal)
	req := env.request()
	res, _ := Promote(context.Background(), req)
	if res.Outcome != OutcomeIneligibleDisposition {
		t.Fatalf("outcome = %s, want ineligible_disposition", res.Outcome)
	}
	assertNoGovernedRecord(t, env)
}

// TestPromotionWritesNoCertificationOrCompletion proves the promotion leaves task
// certification/completion truth untouched.
func TestPromotionWritesNoCertificationOrCompletion(t *testing.T) {
	p := seedPromotable(t)
	if res, _ := Promote(context.Background(), p.request()); res.Outcome != OutcomeCommitted {
		t.Fatalf("promote: %s", res.Outcome)
	}
	// No certified/completed events on the task ledger.
	store := admission.TaskLedgerHead
	_ = store
	entries, _ := os.ReadDir(filepath.Join(p.TaskDir, "ledger"))
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		data, _ := os.ReadFile(filepath.Join(p.TaskDir, "ledger", e.Name()))
		for _, forbidden := range []string{"certified", "completed", "correctness_certified: true"} {
			if strings.Contains(string(data), forbidden) {
				t.Errorf("task ledger %s contains %q", e.Name(), forbidden)
			}
		}
	}
}
