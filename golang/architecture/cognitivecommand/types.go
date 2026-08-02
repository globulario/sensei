// SPDX-License-Identifier: AGPL-3.0-only

// Package cognitivecommand maps one bounded StructuredAgent response into the
// existing O2 interpretation or planning result. The external command proposes
// semantic fields only; Go owns every identity, binding, provider observation,
// digest, and result envelope.
package cognitivecommand

import (
	"github.com/globulario/sensei/golang/architecture/agentcommand"
	"github.com/globulario/sensei/golang/architecture/providerport"
	"github.com/globulario/sensei/golang/architecture/synthesis"
)

const (
	InterpretationProposalSchemaVersion = "sensei.cognitivecommand.interpretationproposal.v1"
	PlanProposalSchemaVersion           = "sensei.cognitivecommand.planproposal.v1"
)

// InterpretationProposal contains only provider-proposed semantic fields. The
// objective, session identity, source references, and digest are not writable
// by the command.
type InterpretationProposal struct {
	SchemaVersion string `json:"schema_version"`

	ApplicableIntent         []string `json:"applicable_intent"`
	BindingInvariants        []string `json:"binding_invariants"`
	RelevantContracts        []string `json:"relevant_contracts"`
	AuthorityBoundaries      []string `json:"authority_boundaries"`
	KnownFailureModes        []string `json:"known_failure_modes"`
	ForbiddenFixes           []string `json:"forbidden_fixes"`
	RequiredProofObligations []string `json:"required_proof_obligations"`
	Assumptions              []string `json:"assumptions"`
	UnresolvedQuestions      []string `json:"unresolved_questions"`
	Limitations              []synthesis.Limitation `json:"limitations"`
}

// PlanProposal contains only the semantic planning fields. Interpretation
// identity, plan generation, provider observation, and digest remain Go-owned.
type PlanProposal struct {
	SchemaVersion string `json:"schema_version"`
	Steps          []synthesis.PlanStep `json:"steps"`
	Assumptions    []string `json:"assumptions"`
	Risks          []string `json:"risks"`
	StopConditions []string `json:"stop_conditions"`
}

// Config freezes one O2 cognitive provider's command and identity capability.
type Config struct {
	Agent           agentcommand.StructuredAgent
	ProviderID      string
	ProviderKind    string
	ModelIdentifier string
	ObservedAt      string

	SupportedOperations []providerport.Operation
}
