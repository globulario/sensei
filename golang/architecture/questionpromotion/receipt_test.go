// SPDX-License-Identifier: Apache-2.0

package questionpromotion_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	qp "github.com/globulario/sensei/golang/architecture/questionpromotion"
	"github.com/globulario/sensei/golang/architecture/repograph"
	"github.com/globulario/sensei/golang/rdf"
)

func hexN(c byte) string { return strings.Repeat(string(c), 64) }

func validReceipt() qp.QuestionPromotionReceipt {
	r := qp.QuestionPromotionReceipt{
		SchemaVersion:                          qp.SchemaVersion,
		Task:                                   closureprotocol.TaskBinding{ID: "task.1", SessionID: "session.1"},
		ResultBindingDigestSHA256:              hexN('1'),
		ResultTransitionReceiptDigestSHA256:    hexN('2'),
		DispositionEntryDigestSHA256:           hexN('3'),
		QuestionDispositionReceiptDigestSHA256: hexN('4'),
		QuestionID:                             "question.abc",
		AnswerID:                               "answer.1",
		AnswerBytesDigestSHA256:                hexN('5'),
		DispositionActorBindingDigestSHA256:    hexN('6'),
		DispositionAuthorityGrantID:            "grant.sensei.question_disposition",
		DispositionAuthorityRoleID:             "role.repository_repair_agent",
		PromotionActorBindingDigestSHA256:      hexN('6'), // same enrolled identity is allowed
		PromotionAuthorityGrantID:              "grant.sensei.governed_promotion",
		PromotionAuthorityRoleID:               "role.repository_repair_agent",
		GovernedTargetKind:                     "invariant",
		CanonicalRecordID:                      "invariant.reload_validates",
		SourceDocument:                         "docs/awareness/invariants.yaml",
		TopLevelKey:                            "invariants",
		GovernedNodeIRI:                        qp.GovernedNodeIRIFor("invariant", "invariant.reload_validates"),
		CanonicalMutationDigestSHA256:          hexN('7'),
		PreMutationManifestDigestSHA256:        hexN('8'),
		PostMutationManifestDigestSHA256:       hexN('9'),
		GraphBuildInputDigestSHA256:            hexN('a'),
		PersistedGraphByteDigestSHA256:         hexN('b'),
		GraphSemanticDigestSHA256:              hexN('c'),
		MarkerDigestSHA256:                     hexN('d'),
		MarkerIRI:                              "https://globular.io/awareness#seedBuild/sha256-" + hexN('c'),
		ProjectionProducerID:                   repograph.ProducerID,
		PromotionAttemptID:                     "attempt.1",
		CombinedSeedObligationOutstanding:      true,
		Producer:                               qp.GeneratedBy,
		PromotedAt:                             "2026-07-16T00:00:00Z",
	}
	d, err := qp.Digest(r)
	if err != nil {
		panic(err)
	}
	r.ReceiptDigestSHA256 = d
	return r
}

func TestValidReceiptValidates(t *testing.T) {
	if err := qp.Validate(validReceipt()); err != nil {
		t.Fatalf("valid receipt rejected: %v", err)
	}
}

// 1 + 2 — deterministic self-excluding digest; exact replay bytes → same digest.
func TestDigestDeterministicAndSelfExcluding(t *testing.T) {
	r := validReceipt()
	d1, _ := qp.Digest(r)
	// The digest field itself does not change the digest (self-exclusion).
	r.ReceiptDigestSHA256 = "whatever"
	d2, _ := qp.Digest(r)
	if d1 != d2 {
		t.Fatal("receipt digest is not self-excluding")
	}
	// The reserved committed-causal identity is also excluded (stable across commit).
	r.CommittedCausalIdentitySHA256 = hexN('e')
	d3, _ := qp.Digest(r)
	if d1 != d3 {
		t.Fatal("committed causal identity must not change the receipt digest")
	}
	// Exact replay bytes produce the same digest.
	if d4, _ := qp.Digest(validReceipt()); d4 != d1 {
		t.Fatal("replay produced a different digest")
	}
}

// 3 + 6 — mutation of any load-bearing lineage/authority/mutation/graph field is detected.
func TestLoadBearingFieldMutationDetected(t *testing.T) {
	base, _ := qp.Digest(validReceipt())
	mutations := map[string]func(*qp.QuestionPromotionReceipt){
		"task":                func(r *qp.QuestionPromotionReceipt) { r.Task.ID = "task.2" },
		"result_binding":      func(r *qp.QuestionPromotionReceipt) { r.ResultBindingDigestSHA256 = hexN('f') },
		"disposition_receipt": func(r *qp.QuestionPromotionReceipt) { r.QuestionDispositionReceiptDigestSHA256 = hexN('f') },
		"question":            func(r *qp.QuestionPromotionReceipt) { r.QuestionID = "question.other" },
		"answer_bytes":        func(r *qp.QuestionPromotionReceipt) { r.AnswerBytesDigestSHA256 = hexN('f') },
		"promotion_grant":     func(r *qp.QuestionPromotionReceipt) { r.PromotionAuthorityGrantID = "grant.other" },
		"mutation_digest":     func(r *qp.QuestionPromotionReceipt) { r.CanonicalMutationDigestSHA256 = hexN('f') },
		"pre_manifest":        func(r *qp.QuestionPromotionReceipt) { r.PreMutationManifestDigestSHA256 = hexN('f') },
		"graph_semantic":      func(r *qp.QuestionPromotionReceipt) { r.GraphSemanticDigestSHA256 = hexN('f') },
		"build_input":         func(r *qp.QuestionPromotionReceipt) { r.GraphBuildInputDigestSHA256 = hexN('f') },
		"marker":              func(r *qp.QuestionPromotionReceipt) { r.MarkerDigestSHA256 = hexN('f') },
		"governed_node": func(r *qp.QuestionPromotionReceipt) {
			r.CanonicalRecordID = "invariant.other"
			r.GovernedNodeIRI = qp.GovernedNodeIRIFor("invariant", "invariant.other")
		},
	}
	for name, mut := range mutations {
		r := validReceipt()
		mut(&r)
		d, _ := qp.Digest(r)
		if d == base {
			t.Errorf("mutating %s did not change the receipt digest", name)
		}
	}
}

// 4 — disposition and promotion authorities cannot be collapsed.
func TestDispositionAndPromotionAuthorityCannotCollapse(t *testing.T) {
	r := validReceipt()
	r.PromotionAuthorityGrantID = r.DispositionAuthorityGrantID
	if err := qp.Validate(r); err == nil {
		t.Fatal("collapsing disposition and promotion grants must be rejected")
	}
}

// 5 — target kind/ID/node mismatch is rejected.
func TestGovernedNodeMismatchRejected(t *testing.T) {
	r := validReceipt()
	r.GovernedNodeIRI = "https://globular.io/awareness#invariant/wrong"
	if err := qp.Validate(r); err == nil {
		t.Fatal("mismatched governed node IRI must be rejected")
	}
	r2 := validReceipt()
	r2.GovernedTargetKind = "not_a_kind"
	if err := qp.Validate(r2); err == nil {
		t.Fatal("unknown governed target kind must be rejected")
	}
}

// 7 — the receipt can never claim the combined seed converged.
func TestCombinedSeedConvergenceCannotBeClaimed(t *testing.T) {
	r := validReceipt()
	r.CombinedSeedObligationOutstanding = false
	if err := qp.Validate(r); err == nil {
		t.Fatal("a receipt claiming combined-seed convergence must be rejected")
	}
}

// 8 — the provenance vocabulary supports the complete chain shape, traversable
// through the accepted repograph store adapter, with no cert/completion semantics.
func TestProvenanceChainTraversableAndLineageOnly(t *testing.T) {
	r := validReceipt()
	nt := qp.ProvenanceTriples(r)

	// No certification/completion/phase/correctness meaning in the lineage.
	for _, forbidden := range []string{"certif", "complet", "correctness", "taskPhase", "phase#"} {
		if bytes.Contains(bytes.ToLower(nt), []byte(strings.ToLower(forbidden))) {
			t.Fatalf("provenance triples contain forbidden semantics %q", forbidden)
		}
	}

	g, err := repograph.ReadGraph(bytes.NewReader(nt))
	if err != nil {
		t.Fatalf("provenance triples do not parse: %v", err)
	}
	ctx := context.Background()
	// Walk the full outbound chain.
	step := func(from, pred, wantTo string) {
		t.Helper()
		got, _ := g.Describe(ctx, from)
		for _, tr := range got {
			if tr.Predicate == pred && tr.ObjectIsIRI && tr.Object == wantTo {
				return
			}
		}
		t.Fatalf("missing edge %s --%s--> %s (got %+v)", from, pred, wantTo, got)
	}
	node := r.GovernedNodeIRI
	receipt := qp.ReceiptNodeIRI(r)
	disposition := qp.DispositionNodeIRI(r)
	answer := qp.AnswerNodeIRI(r)
	question := qp.QuestionNodeIRI(r)
	task := qp.TaskNodeIRI(r)
	session := qp.SessionNodeIRI(r)
	result := qp.ResultNodeIRI(r)

	step(node, rdf.PropPromotedVia, receipt)
	step(receipt, rdf.PropRecordsDisposition, disposition)
	step(disposition, rdf.PropResolvesAnswer, answer)
	step(answer, rdf.PropAnswersQuestion, question)
	step(question, rdf.PropRaisedForTask, task)
	step(task, rdf.PropInSession, session)
	step(receipt, rdf.PropForResult, result)

	// Inbound traversal works too (the governed node is reachable from the receipt).
	in, _ := g.DescribeInbound(ctx, receipt)
	found := false
	for _, it := range in {
		if it.Subject == node && it.Predicate == rdf.PropPromotedVia {
			found = true
		}
	}
	if !found {
		t.Fatal("inbound traversal receipt <- governed node missing")
	}
}

// 9 — the receipt package writes nothing (no CLI, YAML, graph, journal, correctness).
func TestReceiptPackageHasNoSideEffectsOrCLIImport(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	dir := filepath.Dir(file)
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		data, _ := os.ReadFile(filepath.Join(dir, e.Name()))
		src := string(data)
		if strings.Contains(src, "globulario/sensei/cmd/awg") {
			t.Errorf("%s imports cmd/awg", e.Name())
		}
		for _, forbidden := range []string{"os.WriteFile", "os.Create", "CorrectnessCertified", "ledger.NewStore"} {
			if strings.Contains(src, forbidden) {
				t.Errorf("%s performs a forbidden side effect %q", e.Name(), forbidden)
			}
		}
	}
}
