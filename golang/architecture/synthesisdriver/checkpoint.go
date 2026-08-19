// SPDX-License-Identifier: AGPL-3.0-only

package synthesisdriver

import (
	"errors"
	"fmt"
	"strings"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/synthesis"
)

const (
	CheckpointSchemaVersion = "sensei.synthesisdriver.checkpoint.v1"
	CheckpointGeneratedBy   = "sensei-synthesis-driver"
)

// CheckpointSessionState is O7's closed projection of O1's runtime
// synthesis.SessionState.
//
// O1 states plainly that SessionState "is NOT a governed document and has no
// schema or semantic digest of its own"; it carries no JSON tags because
// nothing there is meant to be persisted as canonical truth outside the
// process driving the state machine. A durable checkpoint has exactly the
// opposite requirement: a closed document with stable field names, strict
// schema validation, and a semantic digest.
//
// So O7 authors its own projection rather than marshalling O1's runtime
// struct. This moves no authority: Session and Receipt are embedded as the
// governed documents they already are, every other field is copied verbatim,
// and LoadSessionState rebuilds the exact SessionState back. The projection is
// faithful by construction — ToSessionState/FromSessionState are inverses, and
// a contract test proves it for every field.
type CheckpointSessionState struct {
	Session synthesis.Session `json:"session"`
	Phase   string            `json:"phase"`

	InterpretationDigestSHA256 string `json:"interpretation_digest_sha256"`

	PlanGeneration int `json:"plan_generation"`
	AttemptNumber  int `json:"attempt_number"`

	ExpectedPlanGeneration int `json:"expected_plan_generation"`
	ExpectedAttemptNumber  int `json:"expected_attempt_number"`

	LatestPlanDigestSHA256       string `json:"latest_plan_digest_sha256"`
	LatestAttemptDigestSHA256    string `json:"latest_attempt_digest_sha256"`
	LatestEvaluationDigestSHA256 string `json:"latest_evaluation_digest_sha256"`

	// RemainingRetryBudget / RemainingReplanBudget are carried explicitly and
	// are never recomputed from Session on load. Consumed budget stays
	// derivable as Session.RetryBudget-RemainingRetryBudget, exactly as O1
	// defines it, so a restart cannot refill a budget by reconstructing a
	// fresh session (contract section 7).
	RemainingRetryBudget  int `json:"remaining_retry_budget"`
	RemainingReplanBudget int `json:"remaining_replan_budget"`

	Receipt *synthesis.Receipt `json:"receipt"`
}

// CheckpointTrace carries the accumulated O7 evidence references needed to
// stamp an honest final run receipt after a later process resumes. Only
// digests are carried: RunReceipt itself binds evidence by digest, so a
// resumed run can produce a receipt covering pre-restart evidence without
// rehydrating provider prose, and provider prose cannot re-enter identity
// through the checkpoint.
type CheckpointTrace struct {
	O2ReceiptDigestsSHA256                    []string `json:"o2_receipt_digests_sha256"`
	InterpretationClosureReceiptDigestsSHA256 []string `json:"interpretation_closure_receipt_digests_sha256"`
	RunnerReceiptDigestsSHA256                []string `json:"runner_receipt_digests_sha256"`
	EvaluationReceiptDigestsSHA256            []string `json:"evaluation_receipt_digests_sha256"`
}

// Checkpoint is the durable O7 continuation boundary: a closed, self-digested
// document carrying the exact nonterminal O1 state, the accepted
// Interpretation/Plan, accumulated evidence, the execution-boundary identities
// resume must re-observe, and the consumed step budget.
//
// It is written only at a boundary where completed history is unambiguous
// (see ResumablePhase). Timestamps are observation, not authority, and are
// excluded from CheckpointDigest.
type Checkpoint struct {
	SchemaVersion string `json:"schema_version"`
	CheckpointID  string `json:"checkpoint_id"`
	GeneratedBy   string `json:"generated_by"`

	// Sequence starts at 1 for the first checkpoint of a run and increases by
	// one per durable boundary. PreviousCheckpointDigestSHA256 is nil exactly
	// when Sequence is 1, which is what makes the history a verifiable chain
	// rather than a set of unrelated documents.
	Sequence                       int     `json:"sequence"`
	PreviousCheckpointDigestSHA256 *string `json:"previous_checkpoint_digest_sha256"`

	SessionState CheckpointSessionState `json:"session_state"`

	Interpretation *synthesis.Interpretation `json:"interpretation"`
	Plan           *synthesis.Plan           `json:"plan"`

	CandidateArtifactDigestSHA256 *string `json:"candidate_artifact_digest_sha256"`

	Trace CheckpointTrace `json:"trace"`

	// StepsConsumed is how many O7 steps this session has already spent under
	// the immutable MaxSteps. A resumed driver starts with StepsConsumed
	// already spent and has at most MaxSteps-StepsConsumed left; restart is
	// not a budget refill (contract section 7).
	StepsConsumed int `json:"steps_consumed"`
	MaxSteps      int `json:"max_steps"`

	// --- execution boundary identities, re-observed and compared on resume ---

	RepositoryDomain              string `json:"repository_domain"`
	BaseRevision                  string `json:"base_revision"`
	WorkspaceIdentityDigestSHA256 string `json:"workspace_identity_digest_sha256"`
	GraphAuthorityDigestSHA256    string `json:"graph_authority_digest_sha256"`
	TaskID                        string `json:"task_id"`
	TaskSessionDigestSHA256       string `json:"task_session_digest_sha256"`
	TaskControlStateDigestSHA256  string `json:"task_control_state_digest_sha256"`
	TaskControlGeneration         int    `json:"task_control_generation"`
	ClosureReportDigestSHA256     string `json:"closure_report_digest_sha256"`

	// RunStartedAt is the original fresh-run observation, retained so a
	// receipt stamped after a restart still reports when the session began.
	// Both timestamps are observation only and are excluded from identity.
	RunStartedAt string `json:"run_started_at"`
	ObservedAt   string `json:"observed_at"`

	CheckpointDigestSHA256 string `json:"checkpoint_digest_sha256"`
}

// ResumablePhase reports whether a checkpoint captured at this O1 phase may be
// continued by a later process.
//
// Terminal phases are reloadable and reportable but start no new synthesis
// work. Evaluating is deliberately excluded: the O4 engine resolves
// Evaluating -> {Succeeded|Retry|Replan|Failed} within one owner call, so a
// serialized Evaluating checkpoint would be a half-consumed process-local
// handoff pretending to be durable (contract section 4).
func ResumablePhase(phase synthesis.Phase) bool {
	switch phase {
	case synthesis.PhaseCreated,
		synthesis.PhasePlanning,
		synthesis.PhasePlanned,
		synthesis.PhaseAttempting,
		synthesis.PhaseRetry,
		synthesis.PhaseReplan:
		return true
	default:
		return false
	}
}

// ResumablePhases is the CLOSED vocabulary of durable boundaries, in stable
// order, so a consumer can range over them instead of re-listing them by hand.
func ResumablePhases() []synthesis.Phase {
	return []synthesis.Phase{
		synthesis.PhaseCreated,
		synthesis.PhasePlanning,
		synthesis.PhasePlanned,
		synthesis.PhaseAttempting,
		synthesis.PhaseRetry,
		synthesis.PhaseReplan,
	}
}

// FromSessionState projects O1 runtime state into the closed checkpoint shape.
func FromSessionState(state synthesis.SessionState) CheckpointSessionState {
	return CheckpointSessionState{
		Session:                      state.Session,
		Phase:                        string(state.Phase),
		InterpretationDigestSHA256:   state.InterpretationDigestSHA256,
		PlanGeneration:               state.PlanGeneration,
		AttemptNumber:                state.AttemptNumber,
		ExpectedPlanGeneration:       state.ExpectedPlanGeneration,
		ExpectedAttemptNumber:        state.ExpectedAttemptNumber,
		LatestPlanDigestSHA256:       state.LatestPlanDigestSHA256,
		LatestAttemptDigestSHA256:    state.LatestAttemptDigestSHA256,
		LatestEvaluationDigestSHA256: state.LatestEvaluationDigestSHA256,
		RemainingRetryBudget:         state.RemainingRetryBudget,
		RemainingReplanBudget:        state.RemainingReplanBudget,
		Receipt:                      state.Receipt,
	}
}

// ToSessionState rebuilds the exact O1 runtime state from the projection. It
// is the inverse of FromSessionState and reconstructs nothing: every field is
// carried, so no budget, counter, or accepted digest is re-derived.
func (s CheckpointSessionState) ToSessionState() synthesis.SessionState {
	return synthesis.SessionState{
		Session:                      s.Session,
		Phase:                        synthesis.Phase(s.Phase),
		InterpretationDigestSHA256:   s.InterpretationDigestSHA256,
		PlanGeneration:               s.PlanGeneration,
		AttemptNumber:                s.AttemptNumber,
		ExpectedPlanGeneration:       s.ExpectedPlanGeneration,
		ExpectedAttemptNumber:        s.ExpectedAttemptNumber,
		LatestPlanDigestSHA256:       s.LatestPlanDigestSHA256,
		LatestAttemptDigestSHA256:    s.LatestAttemptDigestSHA256,
		LatestEvaluationDigestSHA256: s.LatestEvaluationDigestSHA256,
		RemainingRetryBudget:         s.RemainingRetryBudget,
		RemainingReplanBudget:        s.RemainingReplanBudget,
		Receipt:                      s.Receipt,
	}
}

func NormalizeCheckpoint(checkpoint Checkpoint) Checkpoint {
	if checkpoint.Trace.O2ReceiptDigestsSHA256 == nil {
		checkpoint.Trace.O2ReceiptDigestsSHA256 = []string{}
	}
	if checkpoint.Trace.InterpretationClosureReceiptDigestsSHA256 == nil {
		checkpoint.Trace.InterpretationClosureReceiptDigestsSHA256 = []string{}
	}
	if checkpoint.Trace.RunnerReceiptDigestsSHA256 == nil {
		checkpoint.Trace.RunnerReceiptDigestsSHA256 = []string{}
	}
	if checkpoint.Trace.EvaluationReceiptDigestsSHA256 == nil {
		checkpoint.Trace.EvaluationReceiptDigestsSHA256 = []string{}
	}
	return checkpoint
}

// CheckpointDigest excludes the observation timestamps and the self field, in
// the same spirit as RunReceiptDigest. Every authority and evidence reference
// — including the previous-checkpoint link and the step accounting — remains
// part of identity, so none of them can change without invalidating the
// checkpoint.
func CheckpointDigest(checkpoint Checkpoint) (string, error) {
	checkpoint = NormalizeCheckpoint(checkpoint)
	checkpoint.RunStartedAt = ""
	checkpoint.ObservedAt = ""
	checkpoint.CheckpointDigestSHA256 = ""
	return closureprotocol.SemanticDigest(checkpoint)
}

// FinalizeCheckpoint stamps the semantic digest and returns the checkpoint
// only if it validates.
func FinalizeCheckpoint(checkpoint Checkpoint) (Checkpoint, error) {
	checkpoint = NormalizeCheckpoint(checkpoint)
	digest, err := CheckpointDigest(checkpoint)
	if err != nil {
		return Checkpoint{}, err
	}
	checkpoint.CheckpointDigestSHA256 = digest
	return checkpoint, ValidateCheckpoint(checkpoint)
}

// ValidateCheckpoint fails closed. A checkpoint is usable only when its
// schema, identity, O1 projection, evidence lineage, chain position, and step
// accounting all hold and its declared digest is the digest of its own
// content.
func ValidateCheckpoint(checkpoint Checkpoint) error {
	checkpoint = NormalizeCheckpoint(checkpoint)
	if err := ValidateCheckpointSchema(checkpoint); err != nil {
		return fmt.Errorf("synthesisdriver: checkpoint schema: %w", err)
	}
	if checkpoint.SchemaVersion != CheckpointSchemaVersion {
		return fmt.Errorf("synthesisdriver: checkpoint schema_version %q", checkpoint.SchemaVersion)
	}
	if strings.TrimSpace(checkpoint.CheckpointID) == "" || checkpoint.GeneratedBy != CheckpointGeneratedBy {
		return errors.New("synthesisdriver: checkpoint identity is incomplete")
	}
	if checkpoint.Sequence < 1 {
		return fmt.Errorf("synthesisdriver: checkpoint sequence %d is not positive", checkpoint.Sequence)
	}

	// The chain link is structural, not advisory: the first checkpoint of a
	// run has no predecessor and every later one must name exactly which
	// checkpoint it continues.
	switch {
	case checkpoint.Sequence == 1 && checkpoint.PreviousCheckpointDigestSHA256 != nil:
		return errors.New("synthesisdriver: first checkpoint cannot reference a previous checkpoint")
	case checkpoint.Sequence > 1 && checkpoint.PreviousCheckpointDigestSHA256 == nil:
		return errors.New("synthesisdriver: checkpoint after the first must reference its previous checkpoint")
	}
	if checkpoint.PreviousCheckpointDigestSHA256 != nil && !isSHA256(*checkpoint.PreviousCheckpointDigestSHA256) {
		return errors.New("synthesisdriver: invalid previous checkpoint digest")
	}

	if err := validateCheckpointSessionState(checkpoint.SessionState); err != nil {
		return err
	}

	// Step accounting is immutable across restart. A checkpoint that claims
	// more consumed steps than its own budget allows would hand a resumed
	// driver a negative remaining budget.
	if checkpoint.MaxSteps <= 0 {
		return errors.New("synthesisdriver: checkpoint max_steps must be positive")
	}
	if checkpoint.StepsConsumed < 0 || checkpoint.StepsConsumed > checkpoint.MaxSteps {
		return fmt.Errorf("synthesisdriver: checkpoint steps_consumed %d is outside max_steps=%d", checkpoint.StepsConsumed, checkpoint.MaxSteps)
	}

	if err := validateCheckpointEvidence(checkpoint); err != nil {
		return err
	}
	if err := validateCheckpointBoundary(checkpoint); err != nil {
		return err
	}

	computed, err := CheckpointDigest(checkpoint)
	if err != nil {
		return err
	}
	if computed != checkpoint.CheckpointDigestSHA256 {
		return fmt.Errorf("synthesisdriver: checkpoint declares digest %q but computed %q", checkpoint.CheckpointDigestSHA256, computed)
	}
	return nil
}

func validateCheckpointSessionState(state CheckpointSessionState) error {
	phase := synthesis.Phase(state.Phase)
	if !validPhase(phase) {
		return fmt.Errorf("synthesisdriver: checkpoint phase %q is outside O1 vocabulary", state.Phase)
	}
	if !isSHA256(state.Session.SessionDigestSHA256) {
		return errors.New("synthesisdriver: checkpoint session digest is invalid")
	}

	// The session document is embedded, so it is verified as the governed
	// document it is rather than trusted because it arrived inside a
	// checkpoint: its declared digest must be the digest O1 recomputes from
	// its content.
	sessionDigest, err := synthesis.SessionDigest(state.Session)
	if err != nil {
		return fmt.Errorf("synthesisdriver: checkpoint session: %w", err)
	}
	if sessionDigest != state.Session.SessionDigestSHA256 {
		return fmt.Errorf("synthesisdriver: checkpoint session declares digest %q but computed %q", state.Session.SessionDigestSHA256, sessionDigest)
	}

	// Budgets are carried, never recomputed. Remaining budget outside
	// [0, granted] would let a restart hand O1 more attempts than the session
	// was ever granted.
	if state.RemainingRetryBudget < 0 || state.RemainingRetryBudget > state.Session.RetryBudget {
		return fmt.Errorf("synthesisdriver: checkpoint remaining_retry_budget %d is outside the session budget %d", state.RemainingRetryBudget, state.Session.RetryBudget)
	}
	if state.RemainingReplanBudget < 0 || state.RemainingReplanBudget > state.Session.ReplanBudget {
		return fmt.Errorf("synthesisdriver: checkpoint remaining_replan_budget %d is outside the session budget %d", state.RemainingReplanBudget, state.Session.ReplanBudget)
	}
	if state.PlanGeneration < 0 || state.AttemptNumber < 0 || state.ExpectedPlanGeneration < 0 || state.ExpectedAttemptNumber < 0 {
		return errors.New("synthesisdriver: checkpoint generation/attempt counters cannot be negative")
	}

	for label, digest := range map[string]string{
		"interpretation":    state.InterpretationDigestSHA256,
		"latest plan":       state.LatestPlanDigestSHA256,
		"latest attempt":    state.LatestAttemptDigestSHA256,
		"latest evaluation": state.LatestEvaluationDigestSHA256,
	} {
		if digest != "" && !isSHA256(digest) {
			return fmt.Errorf("synthesisdriver: checkpoint %s digest is invalid", label)
		}
	}

	// A terminal receipt inside a nonterminal checkpoint (or a nonterminal
	// phase claiming one) is a lineage contradiction, not a cosmetic one.
	if state.Receipt != nil && !phase.Terminal() {
		return fmt.Errorf("synthesisdriver: checkpoint phase %q cannot carry an O1 terminal receipt", state.Phase)
	}
	return nil
}

func validateCheckpointEvidence(checkpoint Checkpoint) error {
	for _, digests := range [][]string{
		checkpoint.Trace.O2ReceiptDigestsSHA256,
		checkpoint.Trace.InterpretationClosureReceiptDigestsSHA256,
		checkpoint.Trace.RunnerReceiptDigestsSHA256,
		checkpoint.Trace.EvaluationReceiptDigestsSHA256,
	} {
		for _, digest := range digests {
			if !isSHA256(digest) {
				return fmt.Errorf("synthesisdriver: checkpoint carries invalid evidence digest %q", digest)
			}
		}
	}
	if checkpoint.CandidateArtifactDigestSHA256 != nil && !isSHA256(*checkpoint.CandidateArtifactDigestSHA256) {
		return errors.New("synthesisdriver: checkpoint candidate artifact digest is invalid")
	}

	// An embedded accepted artifact is verified against the digest O1 already
	// accepted for it. This is what stops a tampered Interpretation or Plan
	// from re-entering a resumed session under an accepted identity.
	if checkpoint.Interpretation != nil {
		digest, err := synthesis.InterpretationDigest(*checkpoint.Interpretation)
		if err != nil {
			return fmt.Errorf("synthesisdriver: checkpoint interpretation: %w", err)
		}
		if digest != checkpoint.Interpretation.InterpretationDigestSHA256 {
			return fmt.Errorf("synthesisdriver: checkpoint interpretation declares digest %q but computed %q", checkpoint.Interpretation.InterpretationDigestSHA256, digest)
		}
		if state := checkpoint.SessionState; state.InterpretationDigestSHA256 != "" && state.InterpretationDigestSHA256 != digest {
			return errors.New("synthesisdriver: checkpoint interpretation does not match the interpretation O1 accepted")
		}
	}
	if checkpoint.Plan != nil {
		digest, err := synthesis.PlanDigest(*checkpoint.Plan)
		if err != nil {
			return fmt.Errorf("synthesisdriver: checkpoint plan: %w", err)
		}
		if digest != checkpoint.Plan.PlanDigestSHA256 {
			return fmt.Errorf("synthesisdriver: checkpoint plan declares digest %q but computed %q", checkpoint.Plan.PlanDigestSHA256, digest)
		}
		if state := checkpoint.SessionState; state.LatestPlanDigestSHA256 != "" && state.LatestPlanDigestSHA256 != digest {
			return errors.New("synthesisdriver: checkpoint plan does not match the plan O1 accepted")
		}
	}

	// O1 records the interpretation exactly once, on Created -> Planning. A
	// checkpoint past that boundary that carries no Interpretation cannot
	// continue: the next owner call would have to reconstruct an accepted
	// premise it does not have.
	if phase := synthesis.Phase(checkpoint.SessionState.Phase); phase != synthesis.PhaseCreated && checkpoint.Interpretation == nil {
		return fmt.Errorf("synthesisdriver: checkpoint at phase %q must carry the accepted interpretation", checkpoint.SessionState.Phase)
	}
	if phase := synthesis.Phase(checkpoint.SessionState.Phase); phase == synthesis.PhaseAttempting && checkpoint.Plan == nil {
		return errors.New("synthesisdriver: checkpoint at phase \"attempting\" must carry the accepted plan")
	}
	return nil
}

func validateCheckpointBoundary(checkpoint Checkpoint) error {
	if strings.TrimSpace(checkpoint.RepositoryDomain) == "" {
		return errors.New("synthesisdriver: checkpoint repository_domain is empty")
	}
	if strings.TrimSpace(checkpoint.BaseRevision) == "" {
		return errors.New("synthesisdriver: checkpoint base_revision is empty")
	}
	if strings.TrimSpace(checkpoint.TaskID) == "" {
		return errors.New("synthesisdriver: checkpoint task_id is empty")
	}
	if checkpoint.TaskControlGeneration < 0 {
		return errors.New("synthesisdriver: checkpoint task_control_generation cannot be negative")
	}
	for label, digest := range map[string]string{
		"workspace_identity_digest_sha256": checkpoint.WorkspaceIdentityDigestSHA256,
		"graph_authority_digest_sha256":    checkpoint.GraphAuthorityDigestSHA256,
		"task_session_digest_sha256":       checkpoint.TaskSessionDigestSHA256,
		"task_control_state_digest_sha256": checkpoint.TaskControlStateDigestSHA256,
		"closure_report_digest_sha256":     checkpoint.ClosureReportDigestSHA256,
	} {
		if !isSHA256(digest) {
			return fmt.Errorf("synthesisdriver: checkpoint %s is invalid", label)
		}
	}

	// The boundary identities are not a second, independent copy of the
	// session binding — they must be the same facts O1 already bound, or the
	// checkpoint would let resume compare current observations against
	// identities the session never had.
	session := checkpoint.SessionState.Session
	if checkpoint.RepositoryDomain != session.RepositoryDomain {
		return errors.New("synthesisdriver: checkpoint repository_domain does not match the session binding")
	}
	if checkpoint.BaseRevision != session.BaseRevision {
		return errors.New("synthesisdriver: checkpoint base_revision does not match the session binding")
	}
	if checkpoint.WorkspaceIdentityDigestSHA256 != session.WorkspaceIdentityDigestSHA256 {
		return errors.New("synthesisdriver: checkpoint workspace identity does not match the session binding")
	}
	if checkpoint.GraphAuthorityDigestSHA256 != session.GraphAuthorityDigestSHA256 {
		return errors.New("synthesisdriver: checkpoint graph authority does not match the session binding")
	}
	if checkpoint.TaskSessionDigestSHA256 != session.TaskSessionDigestSHA256 {
		return errors.New("synthesisdriver: checkpoint task session does not match the session binding")
	}
	if checkpoint.ClosureReportDigestSHA256 != session.ClosureDigestSHA256 {
		return errors.New("synthesisdriver: checkpoint closure report does not match the session binding")
	}
	return nil
}
