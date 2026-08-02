// SPDX-License-Identifier: AGPL-3.0-only

// Package cognitivecommand maps one bounded StructuredAgent response into an
// existing O2 planning result. The external command proposes semantic planning
// fields only; Go owns every identity, binding, provider observation, digest,
// and result envelope.
//
// Interpretation is deliberately not provided by this package. An O1
// Interpretation must already have been produced from governed, digest-bound
// source evidence before a cognitive command may plan from it.
package cognitivecommand

import (
	"github.com/globulario/sensei/golang/architecture/agentcommand"
	"github.com/globulario/sensei/golang/architecture/providerport"
	"github.com/globulario/sensei/golang/architecture/synthesis"
)

const PlanProposalSchemaVersion = "sensei.cognitivecommand.planproposal.v1"

// PlanProposal contains only the semantic planning fields. Interpretation
// identity, plan generation, provider observation, and digest remain Go-owned.
type PlanProposal struct {
	SchemaVersion  string               `json:"schema_version"`
	Steps          []synthesis.PlanStep `json:"steps"`
	Assumptions    []string             `json:"assumptions"`
	Risks          []string             `json:"risks"`
	StopConditions []string             `json:"stop_conditions"`
}

// Config freezes one O2 cognitive planning provider's command and identity
// capability.
type Config struct {
	Agent           agentcommand.StructuredAgent
	ProviderID      string
	ProviderKind    string
	ModelIdentifier string
	ObservedAt      string

	SupportedOperations []providerport.Operation
}
