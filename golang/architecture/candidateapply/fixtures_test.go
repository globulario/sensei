// SPDX-License-Identifier: AGPL-3.0-only

package candidateapply

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/admission"
	"github.com/globulario/sensei/golang/architecture/admissioncomposition"
	"github.com/globulario/sensei/golang/architecture/runnercomposition"
)

type applyFixture struct {
	input ApplyInput
	head  string
}

func newApplyFixture(t *testing.T) applyFixture {
	t.Helper()
	root := t.TempDir()
	runGit(t, root, "init", "-q")
	runGit(t, root, "config", "user.name", "Sensei Test")
	runGit(t, root, "config", "user.email", "sensei-test@example.invalid")
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "a.txt")
	runGit(t, root, "commit", "-qm", "base")
	head := runGit(t, root, "rev-parse", "HEAD")

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
	artifact := runnercomposition.CandidateArtifact{
		SchemaVersion:                     runnercomposition.CandidateArtifactSchemaVersion,
		RepositoryDomain:                  "github.com/globulario/sensei",
		BaseRevision:                      head,
		WorkspaceIdentityDigestSHA256:     hex64("workspace"),
		SessionDigestSHA256:               hex64("session"),
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

	scope := admission.ChangeScope{
		Files:           []admission.FileOperation{{Path: "a.txt", Operation: admission.OperationModify}},
		Symbols:         []string{},
		Components:      []string{},
		ClaimIDs:        []string{},
		PropositionKeys: []string{},
	}
	identity := hex64("admission-request-identity")
	requestDocument := hex64("admission-request-document")
	o5aRequest := admissioncomposition.NormalizeRequest(admissioncomposition.Request{
		SchemaVersion:                        admissioncomposition.RequestSchemaVersion,
		RequestID:                            "o5a.test",
		GeneratedBy:                          admissioncomposition.GeneratedBy,
		SynthesisReceiptDigestSHA256:         hex64("o1"),
		RunnerReceiptDigestSHA256:            hex64("o3"),
		EvaluationReceiptDigestSHA256:        hex64("o4"),
		CandidateArtifactDigestSHA256:        artifactDigest,
		RepositoryDomain:                     artifact.RepositoryDomain,
		BaseRevision:                         head,
		DerivedScope:                         scope,
		UnsupportedOperations:                []admissioncomposition.UnsupportedOperation{},
		AdmissionEligible:                    true,
		AdmissionRequestDigestSHA256:         &requestDocument,
		AdmissionRequestIdentityDigestSHA256: &identity,
	})
	o5aRequestDigest, err := admissioncomposition.RequestDigest(o5aRequest)
	if err != nil {
		t.Fatal(err)
	}
	o5aRequest.RequestDigestSHA256 = o5aRequestDigest
	if err := admissioncomposition.ValidateRequest(o5aRequest); err != nil {
		t.Fatal(err)
	}

	decision := canonicalDecisionFixture(t, head, scope, identity, admission.DecisionAdmitted)
	decisionValue := decision.Decision
	decisionDigest := decision.DecisionDigestSHA256
	o5aReceipt := admissioncomposition.Receipt{
		SchemaVersion:                 admissioncomposition.ReceiptSchemaVersion,
		ReceiptID:                     "o5a-receipt.test",
		GeneratedBy:                   admissioncomposition.GeneratedBy,
		RequestDigestSHA256:           o5aRequestDigest,
		SynthesisReceiptDigestSHA256:  o5aRequest.SynthesisReceiptDigestSHA256,
		CandidateArtifactDigestSHA256: artifactDigest,
		AdmissionDecision:             &decisionValue,
		AdmissionDecisionDigestSHA256: &decisionDigest,
		Disposition:                   admissioncomposition.DispositionAdmissionDecided,
		CompletedAt:                   "2026-08-01T23:50:00Z",
	}
	o5aReceiptDigest, err := admissioncomposition.ReceiptDigest(o5aReceipt)
	if err != nil {
		t.Fatal(err)
	}
	o5aReceipt.ReceiptDigestSHA256 = o5aReceiptDigest
	if err := admissioncomposition.ValidateReceipt(o5aReceipt); err != nil {
		t.Fatal(err)
	}

	return applyFixture{
		head: head,
		input: ApplyInput{
			AdmissionRequest:  o5aRequest,
			AdmissionReceipt:  o5aReceipt,
			Decision:          decision,
			CandidateArtifact: artifact,
			BaseManifest:      base,
			TargetRoot:        root,
		},
	}
}

func canonicalDecisionFixture(t *testing.T, revision string, scope admission.ChangeScope, identity, outcome string) admission.Decision {
	t.Helper()
	mutation := admission.CapabilityAdmitted
	inspection := admission.CapabilityAdmitted
	if outcome != admission.DecisionAdmitted && outcome != admission.DecisionAdmittedWithConditions {
		mutation = admission.CapabilityRefused
		inspection = admission.CapabilityRefused
	}
	decision := admission.Decision{
		SchemaVersion: admission.SchemaVersion,
		GeneratedBy:   admission.GeneratedBy,
		AdmissionID:   "admission.o5b.test",
		PolicyID:      admission.PolicyStrictID,
		PolicyVersion: admission.PolicyStrictVersion,
		Decision:      outcome,
		RequestedMode: admission.ModeModify,
		Binding: architecture.ClaimDocumentBinding{
			RepositoryDomain:  "github.com/globulario/sensei",
			Revision:          revision,
			RevisionStatus:    "resolved",
			TreeDigestSHA256:  hex64("tree"),
			GraphDigestSHA256: hex64("graph"),
			GraphDigestStatus: "resolved",
		},
		RequestReceipt:       admission.RequestReceipt{DigestSHA256: identity, Scope: scope, Mode: admission.ModeModify, TaskClass: "implementation"},
		InspectionCapability: inspection,
		MutationCapability:   mutation,
		Envelope:             admission.ChangeEnvelope{ModifyPaths: []string{"a.txt"}},
		ScopeOnly:            true,
		CorrectnessCertified: false,
	}
	data, err := admission.MarshalCanonicalDecisionJSON(decision)
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		ArchitectureAdmissionDecision admission.Decision `json:"architecture_admission_decision"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatal(err)
	}
	return envelope.ArchitectureAdmissionDecision
}

func canonicalVerificationFixture(t *testing.T, decision admission.Decision, patchDigest, status string) admission.Verification {
	t.Helper()
	verification := admission.Verification{
		SchemaVersion:         admission.SchemaVersion,
		GeneratedBy:           admission.GeneratedBy,
		AdmissionID:           decision.AdmissionID,
		DecisionDigestSHA256:  decision.DecisionDigestSHA256,
		Status:                status,
		Binding:               decision.Binding,
		SessionID:             "session",
		IterationDigestSHA256: hex64("iteration"),
		PatchDigestSHA256:     patchDigest,
		ScopeOnly:             true,
		CorrectnessCertified:  false,
	}
	data, err := admission.MarshalCanonicalVerificationJSON(verification)
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		ArchitectureAdmissionVerification admission.Verification `json:"architecture_admission_verification"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatal(err)
	}
	return envelope.ArchitectureAdmissionVerification
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

func runGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	data, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, data)
	}
	return string(bytesTrimSpace(data))
}

func bytesTrimSpace(data []byte) []byte {
	start, end := 0, len(data)
	for start < end && (data[start] == ' ' || data[start] == '\n' || data[start] == '\r' || data[start] == '\t') {
		start++
	}
	for end > start && (data[end-1] == ' ' || data[end-1] == '\n' || data[end-1] == '\r' || data[end-1] == '\t') {
		end--
	}
	return data[start:end]
}
