// SPDX-License-Identifier: AGPL-3.0-only

package resultpipeline

import (
	"context"
	"fmt"
	"strings"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/admission"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/governedimpact"
	"github.com/globulario/sensei/golang/architecture/resulttransition"
)

// Stable determinism / ledger error codes.
const (
	CodeTransitionRequiresCommittedResult = "resultpipeline.transition_requires_committed_result"
	CodeExpectedLedgerHeadInvalid         = "resultpipeline.expected_ledger_head_invalid"
	CodeLedgerHeadMismatch                = "resultpipeline.ledger_head_mismatch"
	CodeLedgerChangedDuringPreparation    = "resultpipeline.ledger_changed_during_transition_preparation"
	CodeNonDeterministicResult            = "resultpipeline.non_deterministic_result"
	CodeBuildResultDigestMismatch         = "resultpipeline.build_result_digest_mismatch"
)

// buildIdentity is the explicit canonical identity of a build result. It excludes
// every process-local path (temporary directories, checkout path, task directory,
// native materialization paths, cleanup handles).
type buildIdentity struct {
	TaskID    string `json:"task_id"`
	SessionID string `json:"session_id"`

	BaseBindingDigestSHA256           string `json:"base_binding_digest_sha256"`
	ActorBindingDigestSHA256          string `json:"actor_binding_digest_sha256"`
	AuthorityResolutionDigestSHA256   string `json:"authority_resolution_digest_sha256"`
	AdmissionDecisionDigestSHA256     string `json:"admission_decision_digest_sha256"`
	CapabilityConsumptionDigestSHA256 string `json:"capability_consumption_digest_sha256"`
	ObservedChangeSetDigestSHA256     string `json:"observed_change_set_digest_sha256"`
	ScopeVerificationDigestSHA256     string `json:"scope_verification_digest_sha256"`

	ResultBinding             closureprotocol.ResultBinding `json:"result_binding"`
	ResultBindingDigestSHA256 string                        `json:"result_binding_digest_sha256"`

	Stages []stageIdentity `json:"stages"`

	ClosureReportDigest     string `json:"closure_report_digest"`
	DialogueDigest          string `json:"dialogue_digest"`
	ProofRequirementsDigest string `json:"proof_requirements_digest"`
	ImpactReportDigest      string `json:"impact_report_digest"`

	PipelinePolicyID string   `json:"pipeline_policy_id"`
	EvaluatedAt      string   `json:"evaluated_at"`
	Limitations      []string `json:"limitations"`
}

type stageIdentity struct {
	Stage       string                             `json:"stage"`
	LogicalPath string                             `json:"logical_path"`
	MediaType   string                             `json:"media_type"`
	ByteDigest  string                             `json:"byte_digest"`
	Receipt     closureprotocol.ArtifactReceipt    `json:"receipt"`
	Derivation  closureprotocol.ArtifactDerivation `json:"derivation"`
}

func buildIdentityOf(result BuildResult) buildIdentity {
	b := result.BoundRepositoryResult
	id := buildIdentity{
		TaskID:                            b.Task.ID,
		SessionID:                         b.Task.SessionID,
		BaseBindingDigestSHA256:           b.BaseBindingDigestSHA256,
		ActorBindingDigestSHA256:          b.ActorBindingDigestSHA256,
		AuthorityResolutionDigestSHA256:   b.AuthorityResolutionDigestSHA256,
		AdmissionDecisionDigestSHA256:     b.AdmissionDecisionDigestSHA256,
		CapabilityConsumptionDigestSHA256: b.CapabilityConsumptionDigestSHA256,
		ObservedChangeSetDigestSHA256:     b.ObservedChangeSetDigestSHA256,
		ScopeVerificationDigestSHA256:     b.ScopeVerificationDigestSHA256,
		ResultBinding:                     result.ResultBinding,
		ResultBindingDigestSHA256:         result.ResultBindingDigestSHA256,
		ClosureReportDigest:               closureprotocol.MustSemanticDigest(result.ClosureReport),
		DialogueDigest:                    closureprotocol.MustSemanticDigest(result.Dialogue),
		ProofRequirementsDigest:           closureprotocol.MustSemanticDigest(result.ProofRequirements),
		ImpactReportDigest:                closureprotocol.MustSemanticDigest(result.GovernedKnowledgeImpactReport),
		PipelinePolicyID:                  result.PipelinePolicyID,
		EvaluatedAt:                       result.EvaluatedAt,
		Limitations:                       append([]string(nil), result.Limitations...),
	}
	for _, a := range result.StageArtifacts {
		id.Stages = append(id.Stages, stageIdentity{
			Stage: string(a.Stage), LogicalPath: a.LogicalPath, MediaType: a.MediaType,
			ByteDigest: sha256hex(a.Bytes), Receipt: a.Receipt, Derivation: a.Derivation,
		})
	}
	return id
}

// BuildResultDigest is the canonical identity of a validated build result. It
// validates the result first, then digests an explicit identity structure — never
// reflective equality of the whole Go object, and never any process-local path.
func BuildResultDigest(result BuildResult) (string, error) {
	if err := ValidateBuildResult(result); err != nil {
		return "", err
	}
	return closureprotocol.MustSemanticDigest(buildIdentityOf(result)), nil
}

// CompareBuildResults validates both results and proves they are the identical
// result architecture, naming the first meaningful mismatch on failure.
func CompareBuildResults(first, second BuildResult) error {
	if err := ValidateBuildResult(first); err != nil {
		return err
	}
	if err := ValidateBuildResult(second); err != nil {
		return err
	}
	a := buildIdentityOf(first)
	b := buildIdentityOf(second)
	if closureprotocol.MustSemanticDigest(a) == closureprotocol.MustSemanticDigest(b) {
		return nil
	}
	if surface := firstBuildMismatch(a, b); surface != "" {
		return &ValidationError{Code: CodeNonDeterministicResult, Detail: "builds differ at " + surface}
	}
	return &ValidationError{Code: CodeNonDeterministicResult, Detail: "builds differ"}
}

func firstBuildMismatch(a, b buildIdentity) string {
	if a.ResultBindingDigestSHA256 != b.ResultBindingDigestSHA256 ||
		closureprotocol.MustSemanticDigest(a.ResultBinding) != closureprotocol.MustSemanticDigest(b.ResultBinding) {
		return "result binding"
	}
	if len(a.Stages) != len(b.Stages) {
		return "stage count"
	}
	for i := range a.Stages {
		sa, sb := a.Stages[i], b.Stages[i]
		switch {
		case sa.ByteDigest != sb.ByteDigest:
			return fmt.Sprintf("%s bytes", sa.Stage)
		case sa.Receipt.SemanticDigestSHA256 != sb.Receipt.SemanticDigestSHA256:
			return fmt.Sprintf("%s semantic digest", sa.Stage)
		case closureprotocol.MustSemanticDigest(sa.Receipt) != closureprotocol.MustSemanticDigest(sb.Receipt):
			return fmt.Sprintf("%s receipt", sa.Stage)
		case closureprotocol.MustSemanticDigest(sa.Derivation) != closureprotocol.MustSemanticDigest(sb.Derivation):
			return fmt.Sprintf("%s derivation", sa.Stage)
		}
	}
	if a.ClosureReportDigest != b.ClosureReportDigest {
		return "closure report"
	}
	if a.DialogueDigest != b.DialogueDigest {
		return "dialogue"
	}
	if a.ProofRequirementsDigest != b.ProofRequirementsDigest {
		return "proof requirements document"
	}
	if a.ImpactReportDigest != b.ImpactReportDigest {
		return "governed impact report"
	}
	if a.PipelinePolicyID != b.PipelinePolicyID {
		return "pipeline policy"
	}
	if a.EvaluatedAt != b.EvaluatedAt {
		return "evaluation time"
	}
	if !equalStringsLocal(a.Limitations, b.Limitations) {
		return "limitations"
	}
	return ""
}

func equalStringsLocal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// DeterministicBuildRequest asks for two independent, ledger-anchored builds.
type DeterministicBuildRequest struct {
	BuildRequest                   BuildRequest
	ExpectedLedgerHeadDigestSHA256 string
}

// DeterministicBuild is the proven-deterministic result and its identity.
type DeterministicBuild struct {
	Result                  BuildResult
	BuildResultDigestSHA256 string
	LedgerHeadDigestSHA256  string
}

// preparationDeps is an unexported test seam. The production path always supplies
// the public Build and admission.TaskLedgerHead; it is never a public bypass.
type preparationDeps struct {
	build      func(context.Context, BuildRequest) (BuildResult, error)
	ledgerHead func(string) (string, error)
}

func productionDeps() preparationDeps {
	return preparationDeps{build: Build, ledgerHead: admission.TaskLedgerHead}
}

// BuildDeterministically builds the result twice through the public gated Build,
// proves both builds are identical, and verifies the task ledger head is unmoved
// around both builds. It returns a deep-owned copy of the first build.
func BuildDeterministically(ctx context.Context, req DeterministicBuildRequest) (DeterministicBuild, error) {
	return buildDeterministically(ctx, req, productionDeps())
}

func buildDeterministically(ctx context.Context, req DeterministicBuildRequest, deps preparationDeps) (DeterministicBuild, error) {
	expected := strings.TrimSpace(req.ExpectedLedgerHeadDigestSHA256)
	if !isHex64(expected) {
		return DeterministicBuild{}, &ValidationError{Code: CodeExpectedLedgerHeadInvalid, Detail: "expected ledger head must be a 64-hex sha256"}
	}
	if req.BuildRequest.ResultMode != resulttransition.ResultModeRevision {
		return DeterministicBuild{}, &ValidationError{Code: CodeTransitionRequiresCommittedResult, Detail: "transition preparation requires a committed revision result"}
	}
	taskDir := strings.TrimSpace(req.BuildRequest.TaskDirectory)

	if err := requireHead(deps, taskDir, expected, CodeLedgerHeadMismatch); err != nil {
		return DeterministicBuild{}, err
	}
	first, err := deps.build(ctx, req.BuildRequest)
	if err != nil {
		return DeterministicBuild{}, err
	}
	if err := requireHead(deps, taskDir, expected, CodeLedgerChangedDuringPreparation); err != nil {
		return DeterministicBuild{}, err
	}
	second, err := deps.build(ctx, req.BuildRequest)
	if err != nil {
		return DeterministicBuild{}, err
	}
	if err := requireHead(deps, taskDir, expected, CodeLedgerChangedDuringPreparation); err != nil {
		return DeterministicBuild{}, err
	}
	if err := CompareBuildResults(first, second); err != nil {
		return DeterministicBuild{}, err
	}
	digest, err := BuildResultDigest(first)
	if err != nil {
		return DeterministicBuild{}, err
	}
	if err := requireHead(deps, taskDir, expected, CodeLedgerChangedDuringPreparation); err != nil {
		return DeterministicBuild{}, err
	}
	return DeterministicBuild{
		Result:                  deepCopyBuildResult(first),
		BuildResultDigestSHA256: digest,
		LedgerHeadDigestSHA256:  expected,
	}, nil
}

func requireHead(deps preparationDeps, taskDir, expected, mismatchCode string) error {
	head, err := deps.ledgerHead(taskDir)
	if err != nil {
		return &ValidationError{Code: mismatchCode, Detail: "read ledger head: " + err.Error()}
	}
	if strings.TrimSpace(head) != expected {
		return &ValidationError{Code: mismatchCode, Detail: "observed ledger head differs from expected"}
	}
	return nil
}

// deepCopyBuildResult returns a copy whose load-bearing mutable backing arrays are
// independent of the input, so later mutation of one cannot alter the other. The
// input is a freshly-built result not aliased elsewhere; this copy makes the
// returned candidate defensively independent regardless.
func deepCopyBuildResult(in BuildResult) BuildResult {
	out := in // value copy of scalars and non-slice fields

	out.StageArtifacts = make([]PipelineArtifact, len(in.StageArtifacts))
	for i, a := range in.StageArtifacts {
		c := a
		if a.Bytes != nil {
			c.Bytes = append([]byte(nil), a.Bytes...)
		}
		c.Derivation.InputArtifactReceiptDigestsSHA256 = cloneStrings(a.Derivation.InputArtifactReceiptDigestsSHA256)
		c.Derivation.InputBindingDigestsSHA256 = cloneStrings(a.Derivation.InputBindingDigestsSHA256)
		out.StageArtifacts[i] = c
	}

	out.Limitations = cloneStrings(in.Limitations)
	out.ResultBinding.GeneratedArtifacts = cloneArtifacts(in.ResultBinding.GeneratedArtifacts)

	// Governed impact report slices.
	r := in.GovernedKnowledgeImpactReport
	out.GovernedKnowledgeImpactReport.BaseManifests = copyManifests(r.BaseManifests)
	out.GovernedKnowledgeImpactReport.ResultManifests = copyManifests(r.ResultManifests)
	if r.Impacts != nil {
		out.GovernedKnowledgeImpactReport.Impacts = make([]closureprotocol.GovernedKnowledgeImpact, len(r.Impacts))
		for i, im := range r.Impacts {
			c := im
			c.ChangedRecordIDs = cloneStrings(im.ChangedRecordIDs)
			out.GovernedKnowledgeImpactReport.Impacts[i] = c
		}
	}
	out.GovernedKnowledgeImpactReport.Limitations = cloneStrings(r.Limitations)

	// Dialogue open questions (only clone when non-empty; the value copy already
	// carries an empty/nil header safely).
	if len(in.Dialogue.OpenQuestions) > 0 {
		out.Dialogue.OpenQuestions = append([]architecture.OpenQuestion(nil), in.Dialogue.OpenQuestions...)
	}
	return out
}

// cloneStrings clones a non-empty slice and preserves the nil/empty distinction so
// canonical identity is unchanged.
func cloneStrings(s []string) []string {
	if len(s) == 0 {
		return s
	}
	out := make([]string, len(s))
	copy(out, s)
	return out
}

func cloneArtifacts(s []closureprotocol.ResultArtifact) []closureprotocol.ResultArtifact {
	if len(s) == 0 {
		return s
	}
	out := make([]closureprotocol.ResultArtifact, len(s))
	copy(out, s)
	return out
}

func copyManifests(in []governedimpact.CategoryManifest) []governedimpact.CategoryManifest {
	if in == nil {
		return nil
	}
	out := make([]governedimpact.CategoryManifest, len(in))
	for i, m := range in {
		c := m
		if m.RecordIdentity != nil {
			c.RecordIdentity = append([]governedimpact.RecordIdentity(nil), m.RecordIdentity...)
		}
		out[i] = c
	}
	return out
}
