// SPDX-License-Identifier: AGPL-3.0-only

package investigator

import (
	"context"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/investigation"
)

// Options holds configuration and inputs for candidate composition and challenge runs.
type Options struct {
	Root           string
	CapturedAt     string
	ResourceLimits map[string]string
}

// Input represents the holistic set of architectural facts, adopted claims,
// dialogue history, and evidence compiled for candidate synthesis.
type Input struct {
	// The current codebase snapshot/observations
	Document investigation.Document

	// Current adopted claims in the repository
	AdoptedClaims []architecture.Claim

	// Active dialogue/architect questions
	Dialogue []architecture.OpenQuestion

	// Historical review blockers/outcomes
	HistoricalBlockers []string
}

// CandidateIdentity holds fields required for candidate identity binding (Section 8.2).
type CandidateIdentity struct {
	Repository             string
	Proposition            string
	Scope                  architecture.ClaimScope
	CandidateKind          string
	GeneratorVersion       string
	InputGraphDigest       string
	EvidenceSnapshotDigest string
}

// Synthesizer performs candidate claim and candidate question generation.
type Synthesizer interface {
	Synthesize(ctx context.Context, input Input, opts Options) ([]architecture.Claim, []architecture.OpenQuestion, error)
}

// Challenger runs skeptic logic to identify minimal counterexamples and adversarial challenges.
type Challenger interface {
	Challenge(ctx context.Context, input Input, claims []architecture.Claim, opts Options) ([]investigation.Counterexample, []architecture.OpenQuestion, error)
}

// Ranker ranks candidate claims and questions by blast radius, incident recurrence, etc.
type Ranker interface {
	RankClaims(ctx context.Context, claims []architecture.Claim, input Input) []architecture.Claim
	RankQuestions(ctx context.Context, questions []architecture.OpenQuestion, input Input) []architecture.OpenQuestion
}

// PostProcessor performs deterministic grounding validation (Section 8.1).
type PostProcessor interface {
	ValidateGrounding(ctx context.Context, doc investigation.Document, root string) error
}

// Engine orchestrates the end-to-end candidate composition, adversarial challenge,
// ranking, and post-processing pipeline.
type Engine interface {
	Compose(ctx context.Context, input Input, opts Options) (investigation.Document, error)
}
