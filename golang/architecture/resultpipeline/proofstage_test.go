// SPDX-License-Identifier: Apache-2.0

package resultpipeline

import (
	"context"
	"testing"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/closure"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/generatedartifact"
	"github.com/globulario/sensei/golang/architecture/proofrequirements"
	"github.com/globulario/sensei/golang/architecture/resulttransition"
)

// TestComposeProofInputReachesComplete proves the Stage-9 adapter maps the
// pipeline's upstream truth into the composer correctly and that a normal run
// (all sources consulted, non-uncertifiable closure, no orphaned admission
// requirements) reaches extraction_completeness=complete — the property the
// bundled uncertifiable e2e seed cannot itself exercise.
func TestComposeProofInputReachesComplete(t *testing.T) {
	res := closureprotocol.AuthorityResolution{
		ActorBindingDigestSHA256: "a", BaseBindingDigestSHA256: "b",
		ClosureAssessmentDigestSHA256: "c", OperationSetDigestSHA256: "d",
		PolicyID: "authority.v2", EvaluatedAt: "2026-07-15T00:00:00Z",
		Status: closureprotocol.ReceiptValid,
		OperationResults: []closureprotocol.AuthorityResolutionOperation{{
			OperationID: "op.1", Status: closureprotocol.ReceiptValid,
			SelectedMechanism:           closureprotocol.MechanismRepositoryEdit,
			RequiredRuntimeMechanismIDs: []string{"mech.repository_edit"},
		}},
	}
	authDigest, err := closureprotocol.AuthorityResolutionDigest(res)
	if err != nil {
		t.Fatal(err)
	}
	dec := closureprotocol.AdmissionDecision{
		DecisionID: "dec.1", PolicyID: "admission.strict.v2", CapabilityID: "cap.1",
		CompletionPolicyID: "completion.v1",
	}
	bound := resulttransition.BoundRepositoryResult{
		AuthorityResolution:             res,
		AuthorityResolutionDigestSHA256: authDigest,
		AdmissionDecision:               dec,
		AdmissionDecisionDigestSHA256:   closureprotocol.MustSemanticDigest(dec),
	}
	gen := generatedartifact.VerificationResult{
		Manifest: generatedartifact.VerificationManifest{AllRequiredMatched: true},
		ExpectedOutputs: []generatedartifact.Output{{
			Path: generatedartifact.ProofObligationsPath, MediaType: "text/yaml",
			Bytes:                []byte("proof_obligations: []\n"),
			SemanticDigestSHA256: "sem", ByteDigestSHA256: "byte",
		}},
	}
	graph := closure.GraphIndex{Nodes: map[string]closure.Node{}, NodesByID: map[string]string{}}
	rep := closure.Report{Verdict: closure.VerdictConditionallyClosed}
	questions := ArchitectQuestionsBundle{
		Dialogue:                     architecture.DialogueDocument{},
		AllBlockersAccountedFor:      true,
		ArchitectQuestionsActionable: true,
	}

	in := composeProofInput("rb-digest", "graph-sem-digest", bound, gen, graph, rep, questions)
	// The adapter must carry the exact upstream truth, not a re-read.
	if in.ExpectedAdmissionDecisionDigest != bound.AdmissionDecisionDigestSHA256 {
		t.Fatal("adapter dropped the carried admission decision digest")
	}
	if _, ok := gen.ExpectedOutputByPath(generatedartifact.ProofObligationsPath); !ok {
		t.Fatal("stage 2 proof-obligations output missing")
	}

	doc, err := proofrequirements.Compose(context.Background(), in)
	if err != nil {
		t.Fatalf("Compose: %v", err)
	}
	if doc.ExtractionCompleteness != proofrequirements.ExtractionComplete {
		t.Fatalf("completeness = %q, want complete; coverage=%+v", doc.ExtractionCompleteness, doc.SourceCoverage)
	}
	if doc.ProvingDisposition != proofrequirements.ProvingReady {
		t.Fatalf("disposition = %q, want ready", doc.ProvingDisposition)
	}
}
