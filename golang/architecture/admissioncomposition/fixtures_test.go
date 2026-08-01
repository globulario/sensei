// SPDX-License-Identifier: AGPL-3.0-only

package admissioncomposition

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/admission"
	"github.com/globulario/sensei/golang/architecture/evaluatorcomposition"
	"github.com/globulario/sensei/golang/architecture/runnercomposition"
	"github.com/globulario/sensei/golang/architecture/synthesis"
)

func validInput(t *testing.T) ComposeInput {
	t.Helper()
	base := []runnercomposition.CandidateManifestEntry{manifestEntry("a.txt", []byte("old\n"), runnercomposition.ModeRegular)}
	final := []runnercomposition.CandidateManifestEntry{manifestEntry("a.txt", []byte("new\n"), runnercomposition.ModeRegular)}
	inputDigest, err := runnercomposition.ManifestDigest(base)
	if err != nil {
		t.Fatal(err)
	}
	finalDigest, err := runnercomposition.ManifestDigest(final)
	if err != nil {
		t.Fatal(err)
	}
	attemptDigest := hex64("attempt")
	evaluationDigest := hex64("evaluation")
	sessionDigest := hex64("session")
	artifact := runnercomposition.CandidateArtifact{
		SchemaVersion:                     runnercomposition.CandidateArtifactSchemaVersion,
		RepositoryDomain:                  "github.com/globulario/sensei",
		BaseRevision:                      "0123456789012345678901234567890123456789",
		WorkspaceIdentityDigestSHA256:     hex64("workspace"),
		SessionDigestSHA256:               sessionDigest,
		PlanDigestSHA256:                  hex64("plan"),
		PlanGeneration:                    1,
		AttemptNumber:                     1,
		InputCandidateDigestSHA256:        inputDigest,
		ProposedChangeDigestSHA256:        hex64("change"),
		FinalCandidateContentDigestSHA256: finalDigest,
		Manifest:                          final,
	}
	artifactDigest, err := runnercomposition.CandidateArtifactDigest(artifact)
	if err != nil {
		t.Fatal(err)
	}
	artifact.CandidateArtifactDigestSHA256 = artifactDigest
	resultDigest := hex64("result")
	o2ReceiptDigest := hex64("o2")
	requestDigest := hex64("request")
	cleanup := true
	runner := runnercomposition.RunnerReceipt{
		SchemaVersion:                     runnercomposition.RunnerReceiptSchemaVersion,
		ReceiptID:                         "runner.receipt",
		RequestDigestSHA256:               requestDigest,
		ResultDigestSHA256:                &resultDigest,
		O2ReceiptDigestSHA256:             &o2ReceiptDigest,
		InputCandidateDigestSHA256:        &inputDigest,
		ProposedChangeDigestSHA256:        &artifact.ProposedChangeDigestSHA256,
		FinalCandidateContentDigestSHA256: &finalDigest,
		CandidateArtifactDigestSHA256:     &artifactDigest,
		Disposition:                       runnercomposition.DispositionVerified,
		CleanupSucceeded:                  &cleanup,
		CompletedAt:                       "2026-08-01T22:00:00Z",
	}
	runnerDigest, err := runnercomposition.RunnerReceiptDigest(runner)
	if err != nil {
		t.Fatal(err)
	}
	runner.RunnerReceiptDigestSHA256 = runnerDigest
	o1 := synthesis.Receipt{
		SchemaVersion:               synthesis.ReceiptSchemaVersion,
		ReceiptID:                   "synthesis.receipt",
		SessionDigestSHA256:         sessionDigest,
		TerminalReason:              synthesis.ReasonCandidateReadyForAdmission,
		FinalAttemptDigestSHA256:    &attemptDigest,
		FinalEvaluationDigestSHA256: &evaluationDigest,
		RetryCount:                  0,
		ReplanCount:                 0,
		Summary:                     "candidate ready for admission",
		Limitations:                 []synthesis.Limitation{},
		CompletedAt:                 "2026-08-01T22:00:00Z",
	}
	o1Digest, err := synthesis.ReceiptDigest(o1)
	if err != nil {
		t.Fatal(err)
	}
	o1.ReceiptDigestSHA256 = o1Digest
	o4 := evaluatorcomposition.EvaluationReceipt{
		SchemaVersion:                 evaluatorcomposition.EvaluationReceiptSchemaVersion,
		ReceiptID:                     "evaluation.receipt",
		SessionDigestSHA256:           sessionDigest,
		AttemptDigestSHA256:           attemptDigest,
		RunnerReceiptDigestSHA256:     runnerDigest,
		RequestDigestSHA256:           requestDigest,
		ResultDigestSHA256:            resultDigest,
		O2ReceiptDigestSHA256:         o2ReceiptDigest,
		PolicyDigestSHA256:            hex64("policy"),
		CandidateArtifactDigestSHA256: artifactDigest,
		CandidateArtifactVerified:     true,
		EvaluatorResultBindings:       []evaluatorcomposition.EvaluatorResultBinding{},
		EvaluationDigestSHA256:        &evaluationDigest,
		O1TerminalReceiptDigestSHA256: &o1Digest,
		Disposition:                   evaluatorcomposition.DispositionEvaluated,
		CleanupSucceeded:              &cleanup,
		CompletedAt:                   "2026-08-01T22:00:00Z",
	}
	o4Digest, err := evaluatorcomposition.EvaluationReceiptDigest(o4)
	if err != nil {
		t.Fatal(err)
	}
	o4.ReceiptDigestSHA256 = o4Digest
	template := admission.Request{
		SchemaVersion: admission.SchemaVersion,
		Binding: architecture.ClaimDocumentBinding{
			RepositoryDomain:  artifact.RepositoryDomain,
			Revision:          artifact.BaseRevision,
			RevisionStatus:    "resolved",
			TreeDigestSHA256:  hex64("tree"),
			GraphDigestSHA256: hex64("graph"),
			GraphDigestStatus: "resolved",
		},
		Convergence: admission.ConvergenceBinding{
			SessionID:                 "session",
			IterationDigestSHA256:     hex64("iteration"),
			SemanticStateDigestSHA256: hex64("semantic"),
		},
		Mode:                 admission.ModeModify,
		TaskClass:            "implementation",
		AcceptedConditionIDs: []string{},
		RequestedBy:          "test",
	}
	return ComposeInput{
		SynthesisReceipt:  o1,
		RunnerReceipt:     runner,
		EvaluationReceipt: o4,
		CandidateArtifact: artifact,
		BaseManifest:      base,
		AdmissionTemplate: template,
	}
}

func canonicalDecision(t *testing.T, req admission.Request, identity string) admission.Decision {
	t.Helper()
	decision := admission.Decision{
		SchemaVersion:        admission.SchemaVersion,
		GeneratedBy:          admission.GeneratedBy,
		AdmissionID:          "admission.test",
		PolicyID:             admission.PolicyStrictID,
		PolicyVersion:        admission.PolicyStrictVersion,
		Decision:             admission.DecisionAdmitted,
		RequestedMode:        admission.ModeModify,
		Binding:              req.Binding,
		RequestReceipt:       admission.RequestReceipt{DigestSHA256: identity, Scope: req.Scope, Mode: req.Mode, TaskClass: req.TaskClass},
		InspectionCapability: admission.CapabilityAdmitted,
		MutationCapability:   admission.CapabilityAdmitted,
		Envelope:             admission.ChangeEnvelope{ModifyPaths: []string{"a.txt"}},
		ScopeOnly:            true,
		CorrectnessCertified: false,
	}
	data, err := admission.MarshalCanonicalDecisionJSON(decision)
	if err != nil {
		t.Fatal(err)
	}
	var env struct {
		ArchitectureAdmissionDecision admission.Decision `json:"architecture_admission_decision"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatal(err)
	}
	return env.ArchitectureAdmissionDecision
}

func canonicalVerification(t *testing.T, decision admission.Decision) admission.Verification {
	t.Helper()
	verification := admission.Verification{
		SchemaVersion:         admission.SchemaVersion,
		GeneratedBy:           admission.GeneratedBy,
		AdmissionID:           decision.AdmissionID,
		DecisionDigestSHA256:  decision.DecisionDigestSHA256,
		Status:                admission.VerificationScopeCompliant,
		Binding:               decision.Binding,
		SessionID:             "session",
		IterationDigestSHA256: hex64("iteration"),
		PatchDigestSHA256:     hex64("patch"),
		ScopeOnly:             true,
		CorrectnessCertified:  false,
	}
	data, err := admission.MarshalCanonicalVerificationJSON(verification)
	if err != nil {
		t.Fatal(err)
	}
	var env struct {
		ArchitectureAdmissionVerification admission.Verification `json:"architecture_admission_verification"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatal(err)
	}
	return env.ArchitectureAdmissionVerification
}

func manifestEntry(path string, content []byte, mode runnercomposition.CandidateFileMode) runnercomposition.CandidateManifestEntry {
	sum := sha256.Sum256(content)
	return runnercomposition.CandidateManifestEntry{
		Path:                path,
		Mode:                mode,
		Content:             append([]byte{}, content...),
		ContentDigestSHA256: hex.EncodeToString(sum[:]),
	}
}

func hex64(seed string) string {
	sum := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(sum[:])
}
