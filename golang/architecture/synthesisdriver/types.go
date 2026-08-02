// SPDX-License-Identifier: AGPL-3.0-only

// Package synthesisdriver implements O7's deterministic phase dispatcher over
// the existing O1, O2, O3, and O4 owners. It creates no architecture,
// admission, repository-application, or GitHub authority.
package synthesisdriver

import (
	"context"
	"time"

	"github.com/globulario/sensei/golang/architecture/evaluatorcomposition"
	"github.com/globulario/sensei/golang/architecture/providerport"
	"github.com/globulario/sensei/golang/architecture/runnercomposition"
	"github.com/globulario/sensei/golang/architecture/synthesis"
	"github.com/globulario/sensei/golang/architecture/workspacecontract"
)

const (
	RunReceiptSchemaVersion = "sensei.synthesisdriver.receipt.v1"
	GeneratedBy             = "sensei-synthesis-driver"
)

type Disposition string

const (
	DispositionCandidateReady   Disposition = "candidate-ready"
	DispositionTerminalFailure  Disposition = "terminal-failure"
	DispositionProviderStopped  Disposition = "provider-stopped"
	DispositionRunnerStopped    Disposition = "runner-stopped"
	DispositionStepLimitReached Disposition = "step-limit-reached"
)

// ProviderPolicy is the exact O2 execution budget for one operation.
type ProviderPolicy struct {
	DeadlineAt          string
	MaxObservationCount int
	MaxObservationBytes int
}

// ProviderExecution preserves the exact O2 evidence for one interpretation or
// planning call. Generation evidence remains on O3's handoff.
type ProviderExecution struct {
	Request          providerport.Request
	Result           providerport.Result
	ObservationBatch providerport.ObservationBatch
	Receipt          providerport.Receipt
}

// EvaluationEngine is O7's typed call into O4. The default implementation in
// engine.go composes the real O4 owners. Tests may supply another implementation
// only when exercising driver phase behavior in isolation.
type EvaluationEngine interface {
	Evaluate(ctx context.Context, state synthesis.SessionState, handoff runnercomposition.VerifiedGenerationHandoff) (evaluatorcomposition.Result, error)
}

// Config is the complete immutable capability set for one synchronous run.
type Config struct {
	WorkspaceIdentity workspacecontract.Identity
	RepositoryRoot    string
	CandidateStore    runnercomposition.CandidateArtifactStore

	InterpretationProvider providerport.Provider
	PlanningProvider       providerport.Provider
	GenerationFactory      runnercomposition.GenerationProviderFactory
	EvaluationEngine       EvaluationEngine

	InterpretationPolicy ProviderPolicy
	PlanningPolicy       ProviderPolicy
	GenerationPolicy     runnercomposition.RequestPolicy

	MaxSteps int
	Now      func() time.Time
}

// Trace preserves every accepted owner document produced while driving.
type Trace struct {
	ProviderExecutions []ProviderExecution
	GenerationHandoffs []runnercomposition.VerifiedGenerationHandoff
	EvaluationResults  []evaluatorcomposition.Result
	Events             []synthesis.Event
}

// Result is the complete O7 return value. Interpretation and Plan are the
// latest accepted O1 artifacts needed for inspection or a later resumable
// checkpoint. Candidate is populated once O3 sealed one.
type Result struct {
	SessionState   synthesis.SessionState
	Interpretation *synthesis.Interpretation
	Plan           *synthesis.Plan
	Candidate      *runnercomposition.CandidateArtifact
	Trace          Trace
	Receipt        RunReceipt
}

// RunReceipt binds the ordered O2/O3/O4 evidence to the final O1 state. The
// observation timestamps are excluded from its semantic identity.
type RunReceipt struct {
	SchemaVersion string `json:"schema_version"`
	ReceiptID     string `json:"receipt_id"`
	GeneratedBy   string `json:"generated_by"`

	SessionDigestSHA256 string      `json:"session_digest_sha256"`
	FinalPhase          string      `json:"final_phase"`
	Disposition         Disposition `json:"disposition"`
	StepCount           int         `json:"step_count"`

	O2ReceiptDigestsSHA256         []string `json:"o2_receipt_digests_sha256"`
	RunnerReceiptDigestsSHA256     []string `json:"runner_receipt_digests_sha256"`
	EvaluationReceiptDigestsSHA256 []string `json:"evaluation_receipt_digests_sha256"`

	SynthesisReceiptDigestSHA256  *string `json:"synthesis_receipt_digest_sha256"`
	CandidateArtifactDigestSHA256 *string `json:"candidate_artifact_digest_sha256"`

	Detail      string `json:"detail"`
	StartedAt   string `json:"started_at"`
	CompletedAt string `json:"completed_at"`

	ReceiptDigestSHA256 string `json:"receipt_digest_sha256"`
}
