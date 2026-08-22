// SPDX-License-Identifier: AGPL-3.0-only

// Package modelexec owns the OPTIONAL model execution lane for investigation.
//
// It exists as its own package to keep one direction of authority. Deterministic
// extraction and the deterministic WHY evidence providers must never learn to
// call a model: their contracts promise reproducible capture, and a package
// that can reach a network cannot promise that. Orchestration depends on this
// package; this package depends on investigation; nothing depends back.
//
// The serialized contract stays investigation.ModelBinding. This package
// produces one, it does not define a second identity for the same execution.
//
// What this package may produce is derived evidence: candidates, questions,
// challenges, limitations. What it may never produce is authority. A model
// result travels the pre-existing governed candidate/review path or it does not
// travel at all.
package modelexec

import (
	"context"

	"github.com/globulario/sensei/golang/architecture/investigation"
)

// ProviderIdentity names WHO ran, precisely enough to tell two behaviours
// apart. A bare provider name is not enough: the same name can front different
// versions, and a replay needs to know which one answered.
type ProviderIdentity struct {
	ID      string
	Version string
}

// ModelIdentity names WHAT ran. Some providers expose a stable model digest and
// some genuinely cannot; DigestAbsent records the second case as a statement
// rather than leaving the field silently empty.
type ModelIdentity struct {
	Name         string
	DigestSHA256 string
	DigestAbsent bool
}

// Request is the bounded question put to a provider.
//
// It is the ONLY thing a provider is given. The model does not roam the
// repository or the network: if material is not in this request, an artifact
// citing it is ungrounded by construction, which is what makes the grounding
// check in ValidateArtifact decidable rather than a matter of trust.
type Request struct {
	SchemaVersion string

	// The exact world the question is about.
	RepositoryDomain   string
	RepositoryRevision string
	RepositoryTree     string
	GraphDigestSHA256  string
	GraphStatus        string
	PolicyProfileID    string

	// The deterministic work this question is downstream of.
	HowDocumentDigestSHA256      string
	EvidenceSnapshotDigestSHA256 string

	// The bounded target.
	TargetObservationIDs []string
	TargetEvidenceIDs    []string
	QueryDigestSHA256    string

	// Exactly what the model may read. Anything cited outside this set is
	// ungrounded.
	SuppliedEvidence []SuppliedEvidence

	// Who is being asked, and under what output contract.
	Provider             ProviderIdentity
	Model                ModelIdentity
	PromptContractDigest string
	OutputSchemaVersion  string
	ToolPolicy           string
}

// SuppliedEvidence is one excerpt the model is permitted to see and cite.
//
// FilePath records where the excerpt came from, so an artifact attributing a
// claim to a file can be checked against the files the model was actually
// shown. Without it, "in scope" could only be checked at repository
// granularity, and a model could attribute a finding to any file in the repo.
type SuppliedEvidence struct {
	ID           string
	DigestSHA256 string
	FilePath     string
	Excerpt      string
}

// Artifact is the provider's structured answer, BEFORE Sensei accepts anything
// in it. It is deliberately not an architecture object: deserializing provider
// output straight into canonical types and calling that acceptance is the
// error this indirection exists to prevent.
type Artifact struct {
	SchemaVersion string
	// Items are proposals, never conclusions.
	Items []ArtifactItem
	// NondeterminismDeclaration is the provider's own statement about what may
	// differ on replay. An executor never invents one.
	NondeterminismDeclaration string
}

// ArtifactItem is one proposed derived finding.
type ArtifactItem struct {
	Kind string
	Text string
	// CitedEvidenceIDs must be a subset of what the request supplied.
	CitedEvidenceIDs []string
	// Scope the item claims to be about; must stay inside the bound request.
	RepositoryDomain string
	FilePaths        []string
	// Authority-shaped fields a model is NOT permitted to set. They exist in
	// the struct precisely so an attempt is DETECTABLE and can be refused,
	// rather than being silently dropped by a parser that has no field for it.
	//
	// A model that tries to promote a candidate or declare an invariant is
	// making a bid for authority; that bid must be visible in order to be
	// rejected and recorded.
	ClaimsCanonical bool
	ClaimsPromotion bool
	ClaimsAdmission bool
}

// Provider is the narrow port. Selection is explicit: an executor is handed a
// provider or a registry, never an ambient global discovered from credentials.
type Provider interface {
	Identity() ProviderIdentity
	// Execute is invoked at most once per run by the executor.
	//
	// A provider may report a structured refusal or a structured execution
	// failure. It cannot declare itself resolved: only the executor writes
	// Sensei's record, from what it observed.
	Execute(context.Context, Request) (Artifact, error)
}

// Refusal is a provider's explicit "no". It is an ANSWER, not an outage, and
// collapsing it into an error would erase the difference.
type Refusal struct{ Reason string }

func (r *Refusal) Error() string { return "provider refused the request: " + r.Reason }

// Registry resolves a provider ID to a provider. Resolution failure happens
// BEFORE any invocation and is therefore unavailable, never errored.
type Registry map[string]Provider

// Outcome is what one execution attempt produced: the terminal binding, the
// accepted artifact when there is one, and how many times the provider was
// actually called — which the proof matrix asserts on directly.
type Outcome struct {
	Binding       investigation.ModelBinding
	Artifact      *Artifact
	ProviderCalls int
}
