// SPDX-License-Identifier: AGPL-3.0-only

package synthesisdriver

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/synthesis"
)

const (
	ResumeAssessmentSchemaVersion = "sensei.synthesisdriver.resume-assessment.v1"
)

// ResumeDisposition is the closed outcome vocabulary of an assessment.
type ResumeDisposition string

const (
	ResumeAllowed ResumeDisposition = "allowed"
	ResumeRefused ResumeDisposition = "refused"
)

// ResumeRefusalReason is the exact, typed reason a resume was refused.
//
// These are never collapsed into a generic resolution failure in
// machine-readable output: an operator whose graph moved and an operator whose
// task control generation advanced have to take different actions, and a
// caller that cannot tell them apart cannot report either honestly.
type ResumeRefusalReason string

const (
	RefusalCheckpointInvalid      ResumeRefusalReason = "checkpoint-invalid"
	RefusalCheckpointNotResumable ResumeRefusalReason = "checkpoint-not-resumable"
	RefusalRepositoryDomainDrift  ResumeRefusalReason = "repository-domain-drift"
	RefusalBaseRevisionDrift      ResumeRefusalReason = "base-revision-drift"
	RefusalWorkspaceIdentityDrift ResumeRefusalReason = "workspace-identity-drift"
	RefusalGraphAuthorityDrift    ResumeRefusalReason = "graph-authority-drift"
	RefusalTaskIdentityDrift      ResumeRefusalReason = "task-identity-drift"
	RefusalTaskControlDrift       ResumeRefusalReason = "task-control-drift"
	RefusalClosureDrift           ResumeRefusalReason = "closure-drift"
	RefusalEvidenceLineageInvalid ResumeRefusalReason = "evidence-lineage-invalid"
	RefusalStepBudgetExhausted    ResumeRefusalReason = "step-budget-exhausted"
)

// ResumeRefusalReasons is the CLOSED vocabulary, in stable order, so a
// consumer can range over every reason instead of re-listing them by hand. A
// reason added here without a consumer being updated fails the test that
// ranges over it.
func ResumeRefusalReasons() []ResumeRefusalReason {
	return []ResumeRefusalReason{
		RefusalCheckpointInvalid,
		RefusalCheckpointNotResumable,
		RefusalRepositoryDomainDrift,
		RefusalBaseRevisionDrift,
		RefusalWorkspaceIdentityDrift,
		RefusalGraphAuthorityDrift,
		RefusalTaskIdentityDrift,
		RefusalTaskControlDrift,
		RefusalClosureDrift,
		RefusalEvidenceLineageInvalid,
		RefusalStepBudgetExhausted,
	}
}

// ResumeIdentitySet is the mutable execution boundary: the identities whose
// continuity is required to claim that this is still the same synthesis
// session. Both the checkpoint's expectation and the current observation are
// recorded in this shape so an assessment shows what changed, not merely that
// something did.
type ResumeIdentitySet struct {
	RepositoryDomain              string `json:"repository_domain"`
	BaseRevision                  string `json:"base_revision"`
	WorkspaceIdentityDigestSHA256 string `json:"workspace_identity_digest_sha256"`
	GraphAuthorityDigestSHA256    string `json:"graph_authority_digest_sha256"`
	TaskID                        string `json:"task_id"`
	TaskSessionDigestSHA256       string `json:"task_session_digest_sha256"`
	TaskControlStateDigestSHA256  string `json:"task_control_state_digest_sha256"`
	TaskControlGeneration         int    `json:"task_control_generation"`
	ClosureReportDigestSHA256     string `json:"closure_report_digest_sha256"`
}

// ResumeBinding is the CURRENT observation, composed by the caller through the
// existing owners — Metadata authority for workspace/graph identity, the task
// owner for task and control generation, the closure owner for the report
// resolved at that generation, and Git for the base revision.
//
// O7 does not observe any of this itself: the architecture package stays free
// of gRPC, CLI parsing, and repository path discovery. It only compares what
// it was handed against what the checkpoint recorded.
type ResumeBinding struct {
	Current ResumeIdentitySet
}

// ResumeAssessment is immutable evidence of one resume decision, produced
// whether or not the resume is allowed. A refusal has to be auditable, not
// just an error string that scrolls past.
type ResumeAssessment struct {
	SchemaVersion string `json:"schema_version"`
	AssessmentID  string `json:"assessment_id"`
	GeneratedBy   string `json:"generated_by"`

	CheckpointDigestSHA256 string `json:"checkpoint_digest_sha256"`
	SessionDigestSHA256    string `json:"session_digest_sha256"`
	CheckpointPhase        string `json:"checkpoint_phase"`
	CheckpointSequence     int    `json:"checkpoint_sequence"`

	StepsConsumed int `json:"steps_consumed"`
	MaxSteps      int `json:"max_steps"`

	Expected ResumeIdentitySet `json:"expected"`
	Observed ResumeIdentitySet `json:"observed"`

	Disposition   ResumeDisposition    `json:"disposition"`
	RefusalReason *ResumeRefusalReason `json:"refusal_reason"`
	Detail        string               `json:"detail"`

	ObservedAt string `json:"observed_at"`

	AssessmentDigestSHA256 string `json:"assessment_digest_sha256"`
}

// Allowed reports whether the assessment permits a resumed owner call. Callers
// gate on this rather than on the absence of an error: an assessment that
// refuses is a successful assessment of an impermissible resume.
func (a ResumeAssessment) Allowed() bool {
	return a.Disposition == ResumeAllowed && a.RefusalReason == nil
}

// checkpointIdentitySet projects the identities the checkpoint recorded.
func checkpointIdentitySet(checkpoint Checkpoint) ResumeIdentitySet {
	return ResumeIdentitySet{
		RepositoryDomain:              checkpoint.RepositoryDomain,
		BaseRevision:                  checkpoint.BaseRevision,
		WorkspaceIdentityDigestSHA256: checkpoint.WorkspaceIdentityDigestSHA256,
		GraphAuthorityDigestSHA256:    checkpoint.GraphAuthorityDigestSHA256,
		TaskID:                        checkpoint.TaskID,
		TaskSessionDigestSHA256:       checkpoint.TaskSessionDigestSHA256,
		TaskControlStateDigestSHA256:  checkpoint.TaskControlStateDigestSHA256,
		TaskControlGeneration:         checkpoint.TaskControlGeneration,
		ClosureReportDigestSHA256:     checkpoint.ClosureReportDigestSHA256,
	}
}

// AssessResume decides whether the same governed session may continue from
// this checkpoint under the currently observed boundary.
//
// It calls no provider, runner, or evaluator, and performs no O1 transition —
// it only compares. The order is deliberate: a checkpoint that is not itself
// trustworthy is refused before its recorded identities are believed enough to
// compare anything against, and lineage is checked before drift so a
// self-contradictory checkpoint is never reported as a clean environment
// change.
//
// An improved graph is drift exactly like a degraded one. Resume claims this
// is the same execution; a better premise is still a different premise, and
// wanting one is a reason to start a new session rather than silently change
// the premises of the old one.
func AssessResume(checkpoint Checkpoint, binding ResumeBinding, now func() time.Time) (ResumeAssessment, error) {
	if now == nil {
		return ResumeAssessment{}, errors.New("synthesisdriver: clock is required")
	}
	checkpoint = NormalizeCheckpoint(checkpoint)

	assessment := ResumeAssessment{
		SchemaVersion:          ResumeAssessmentSchemaVersion,
		GeneratedBy:            GeneratedBy,
		CheckpointDigestSHA256: checkpoint.CheckpointDigestSHA256,
		SessionDigestSHA256:    checkpoint.SessionState.Session.SessionDigestSHA256,
		CheckpointPhase:        checkpoint.SessionState.Phase,
		CheckpointSequence:     checkpoint.Sequence,
		StepsConsumed:          checkpoint.StepsConsumed,
		MaxSteps:               checkpoint.MaxSteps,
		Expected:               checkpointIdentitySet(checkpoint),
		Observed:               binding.Current,
		ObservedAt:             now().UTC().Format(time.RFC3339),
	}
	assessment.AssessmentID = fmt.Sprintf("o7.resume.%s.%d", shortDigest(checkpoint.CheckpointDigestSHA256), checkpoint.Sequence)

	if reason, detail := resumeRefusal(checkpoint, binding); reason != nil {
		assessment.Disposition = ResumeRefused
		assessment.RefusalReason = reason
		assessment.Detail = detail
	} else {
		assessment.Disposition = ResumeAllowed
		assessment.Detail = fmt.Sprintf("checkpoint %d at phase %q may continue with %d of %d steps remaining",
			checkpoint.Sequence, checkpoint.SessionState.Phase, checkpoint.MaxSteps-checkpoint.StepsConsumed, checkpoint.MaxSteps)
	}
	return finalizeResumeAssessment(assessment)
}

// resumeRefusal returns the first typed reason this resume cannot proceed, or
// nil when it may.
func resumeRefusal(checkpoint Checkpoint, binding ResumeBinding) (*ResumeRefusalReason, string) {
	if err := ValidateCheckpoint(checkpoint); err != nil {
		return refusal(RefusalCheckpointInvalid, err.Error())
	}
	if !ResumablePhase(synthesis.Phase(checkpoint.SessionState.Phase)) {
		return refusal(RefusalCheckpointNotResumable,
			fmt.Sprintf("phase %q is not a durable boundary", checkpoint.SessionState.Phase))
	}
	if detail := lineageContradiction(checkpoint); detail != "" {
		return refusal(RefusalEvidenceLineageInvalid, detail)
	}

	expected := checkpointIdentitySet(checkpoint)
	observed := binding.Current

	for _, check := range []struct {
		reason ResumeRefusalReason
		label  string
		want   string
		got    string
	}{
		{RefusalRepositoryDomainDrift, "repository domain", expected.RepositoryDomain, observed.RepositoryDomain},
		{RefusalBaseRevisionDrift, "base revision", expected.BaseRevision, observed.BaseRevision},
		{RefusalWorkspaceIdentityDrift, "workspace identity", expected.WorkspaceIdentityDigestSHA256, observed.WorkspaceIdentityDigestSHA256},
		{RefusalGraphAuthorityDrift, "graph authority", expected.GraphAuthorityDigestSHA256, observed.GraphAuthorityDigestSHA256},
		{RefusalTaskIdentityDrift, "task id", expected.TaskID, observed.TaskID},
		{RefusalTaskIdentityDrift, "task session", expected.TaskSessionDigestSHA256, observed.TaskSessionDigestSHA256},
		{RefusalTaskControlDrift, "task control state", expected.TaskControlStateDigestSHA256, observed.TaskControlStateDigestSHA256},
		{RefusalClosureDrift, "closure report", expected.ClosureReportDigestSHA256, observed.ClosureReportDigestSHA256},
	} {
		if check.want != check.got {
			return refusal(check.reason, fmt.Sprintf("%s changed: checkpoint %q, observed %q", check.label, check.want, check.got))
		}
	}

	// The control generation is compared separately from its digest: a
	// generation that moved while the digest happened to match is still a
	// different control state to resolve closure against.
	if expected.TaskControlGeneration != observed.TaskControlGeneration {
		return refusal(RefusalTaskControlDrift, fmt.Sprintf("task control generation changed: checkpoint %d, observed %d",
			expected.TaskControlGeneration, observed.TaskControlGeneration))
	}

	// Restart is not a budget refill. An exhausted budget refuses here, before
	// any external call, rather than starting a step that cannot complete.
	if checkpoint.StepsConsumed >= checkpoint.MaxSteps {
		return refusal(RefusalStepBudgetExhausted, fmt.Sprintf("all %d steps are already consumed", checkpoint.MaxSteps))
	}
	return nil, ""
}

// lineageContradiction reports a checkpoint whose carried evidence disagrees
// with the phase it claims. These are contradictions ValidateCheckpoint does
// not catch because each field is individually well-formed; only their
// combination is impossible.
func lineageContradiction(checkpoint Checkpoint) string {
	phase := synthesis.Phase(checkpoint.SessionState.Phase)

	// A candidate is sealed by O3 during an attempt. A checkpoint that has
	// never reached an attempt cannot already reference one.
	if checkpoint.CandidateArtifactDigestSHA256 != nil {
		switch phase {
		case synthesis.PhaseCreated, synthesis.PhasePlanning, synthesis.PhasePlanned:
			return fmt.Sprintf("phase %q carries a sealed candidate digest, which only an attempt can produce", phase)
		}
	}
	// Evaluation receipts are produced by O4 after an attempt was generated.
	if len(checkpoint.Trace.EvaluationReceiptDigestsSHA256) > 0 && checkpoint.SessionState.AttemptNumber == 0 {
		return "evaluation evidence is present but O1 has recorded no attempt"
	}
	// O1 records the interpretation exactly once, crossing created -> planning,
	// and that transition requires a governing closure receipt. Past that
	// boundary the receipt must be in the trace.
	if phase != synthesis.PhaseCreated && len(checkpoint.Trace.InterpretationClosureReceiptDigestsSHA256) == 0 {
		return fmt.Sprintf("phase %q was reached without an interpretation closure receipt", phase)
	}
	// A plan digest recorded by O1 with no plan carried cannot be continued:
	// the next attempt would have no accepted plan to generate from.
	if checkpoint.SessionState.LatestPlanDigestSHA256 != "" && checkpoint.Plan == nil {
		return "O1 recorded a plan but the checkpoint carries none"
	}
	return ""
}

func refusal(reason ResumeRefusalReason, detail string) (*ResumeRefusalReason, string) {
	value := reason
	return &value, detail
}

func shortDigest(digest string) string {
	if len(digest) < 16 {
		return digest
	}
	return digest[:16]
}

func NormalizeResumeAssessment(assessment ResumeAssessment) ResumeAssessment {
	assessment.Detail = strings.TrimSpace(assessment.Detail)
	return assessment
}

// ResumeAssessmentDigest excludes the observation timestamp and the self
// field. Expected and observed identities, the disposition, and the refusal
// reason all remain part of identity — the assessment IS the record of that
// comparison.
func ResumeAssessmentDigest(assessment ResumeAssessment) (string, error) {
	assessment = NormalizeResumeAssessment(assessment)
	assessment.ObservedAt = ""
	assessment.AssessmentDigestSHA256 = ""
	return closureprotocol.SemanticDigest(assessment)
}

func finalizeResumeAssessment(assessment ResumeAssessment) (ResumeAssessment, error) {
	assessment = NormalizeResumeAssessment(assessment)
	digest, err := ResumeAssessmentDigest(assessment)
	if err != nil {
		return ResumeAssessment{}, err
	}
	assessment.AssessmentDigestSHA256 = digest
	return assessment, ValidateResumeAssessment(assessment)
}

func ValidateResumeAssessment(assessment ResumeAssessment) error {
	assessment = NormalizeResumeAssessment(assessment)
	if err := ValidateResumeAssessmentSchema(assessment); err != nil {
		return fmt.Errorf("synthesisdriver: resume assessment schema: %w", err)
	}
	if assessment.SchemaVersion != ResumeAssessmentSchemaVersion {
		return fmt.Errorf("synthesisdriver: resume assessment schema_version %q", assessment.SchemaVersion)
	}
	if strings.TrimSpace(assessment.AssessmentID) == "" || assessment.GeneratedBy != GeneratedBy {
		return errors.New("synthesisdriver: resume assessment identity is incomplete")
	}
	if !isSHA256(assessment.CheckpointDigestSHA256) || !isSHA256(assessment.SessionDigestSHA256) || !isSHA256(assessment.AssessmentDigestSHA256) {
		return errors.New("synthesisdriver: resume assessment carries an invalid digest")
	}
	if assessment.CheckpointSequence < 0 || assessment.StepsConsumed < 0 || assessment.MaxSteps < 0 {
		return errors.New("synthesisdriver: resume assessment cannot record negative accounting")
	}

	switch assessment.Disposition {
	case ResumeAllowed:
		// An allowed assessment naming a reason would be unreadable evidence:
		// the disposition and the reason would contradict each other.
		if assessment.RefusalReason != nil {
			return errors.New("synthesisdriver: an allowed resume cannot carry a refusal reason")
		}

		// Strict constraints apply to what an ALLOWED assessment claims,
		// because that document is the permission slip for a resumed owner
		// call. A REFUSED assessment must instead be able to record whatever
		// the checkpoint actually claimed — including impossible accounting or
		// a phase outside the O1 vocabulary, which is frequently the very
		// reason it was refused. Enforcing the strict form on both is what
		// made an invalid checkpoint unrecordable, leaving the refusal with
		// nowhere to be written down.
		if !validPhase(synthesis.Phase(assessment.CheckpointPhase)) {
			return fmt.Errorf("synthesisdriver: resume assessment phase %q is outside O1 vocabulary", assessment.CheckpointPhase)
		}
		if assessment.CheckpointSequence < 1 {
			return errors.New("synthesisdriver: an allowed resume assessment requires a positive checkpoint sequence")
		}
		if assessment.MaxSteps <= 0 || assessment.StepsConsumed >= assessment.MaxSteps {
			return errors.New("synthesisdriver: an allowed resume assessment must leave at least one step")
		}
	case ResumeRefused:
		if assessment.RefusalReason == nil {
			return errors.New("synthesisdriver: a refused resume must name its typed reason")
		}
		if !validRefusalReason(*assessment.RefusalReason) {
			return fmt.Errorf("synthesisdriver: unknown resume refusal reason %q", *assessment.RefusalReason)
		}
	default:
		return fmt.Errorf("synthesisdriver: unknown resume disposition %q", assessment.Disposition)
	}

	computed, err := ResumeAssessmentDigest(assessment)
	if err != nil {
		return err
	}
	if computed != assessment.AssessmentDigestSHA256 {
		return fmt.Errorf("synthesisdriver: resume assessment declares digest %q but computed %q", assessment.AssessmentDigestSHA256, computed)
	}
	return nil
}

func validRefusalReason(reason ResumeRefusalReason) bool {
	for _, known := range ResumeRefusalReasons() {
		if reason == known {
			return true
		}
	}
	return false
}
