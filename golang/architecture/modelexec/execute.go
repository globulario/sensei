// SPDX-License-Identifier: AGPL-3.0-only

package modelexec

import (
	"context"
	"errors"

	"github.com/globulario/sensei/golang/architecture/investigation"
)

// Config is what a CALLER may ask for. Note what it cannot contain: a status.
//
// A caller states which provider and model it wants. It does not get to state
// the outcome. That asymmetry is the whole point of #256 — the terminal binding
// is constructed from what Execute observed, never from what a caller declared.
type Config struct {
	// Requested is false when no model execution was asked for at all.
	Requested bool
	// Disabled turns the capability off regardless of any other field.
	Disabled bool

	ProviderID string
	ModelName  string
}

// Execute runs the optional model lane at most once and returns the terminal
// binding built from observed outcome.
//
// Every return path is a typed outcome. There is deliberately no path that
// silently falls back to "deterministic only" and reports success: the
// deterministic result is still returned by the caller, but this lane always
// records what actually happened to it.
func Execute(ctx context.Context, cfg Config, reg Registry, req Request) Outcome {
	// Nothing may be called.
	if cfg.Disabled {
		return Outcome{Binding: investigation.DisabledModelBinding()}
	}
	if !cfg.Requested {
		return Outcome{Binding: investigation.ModelBinding{
			Status: investigation.ModelStatusNotRequested,
			Reason: investigation.ModelReasonNoModelRequested,
		}}
	}

	// Resolution happens BEFORE invocation, so its failure is unavailable
	// rather than errored. Conflating the two would report an outage we never
	// observed.
	provider, found := reg[cfg.ProviderID]
	if !found || provider == nil {
		return Outcome{Binding: investigation.ModelBinding{
			Status:    investigation.ModelStatusUnavailable,
			Reason:    investigation.ModelReasonProviderUnknown,
			ModelName: cfg.ModelName,
		}}
	}
	identity := provider.Identity()
	if identity.ID == "" || identity.Version == "" {
		return Outcome{Binding: investigation.ModelBinding{
			Status:    investigation.ModelStatusUnavailable,
			Reason:    investigation.ModelReasonProviderUnreachable,
			ModelName: cfg.ModelName,
		}}
	}
	if cfg.ModelName == "" {
		return Outcome{Binding: investigation.ModelBinding{
			Status:   investigation.ModelStatusUnavailable,
			Reason:   investigation.ModelReasonModelUnknown,
			Provider: investigation.ProviderBinding{ID: identity.ID, Version: identity.Version},
		}}
	}

	// The request is finalized and hashed before the call, and this exact
	// digest is what the terminal binding carries.
	req.Provider = identity
	req.Model.Name = cfg.ModelName
	if req.SchemaVersion == "" {
		req.SchemaVersion = ArtifactSchemaVersion
	}
	requestDigest, err := RequestDigest(req)
	if err != nil {
		return Outcome{Binding: investigation.ModelBinding{
			Status:   investigation.ModelStatusErrored,
			Reason:   investigation.ModelReasonExecutionFailed,
			Provider: investigation.ProviderBinding{ID: identity.ID, Version: identity.Version},
		}}
	}

	invoked := investigation.ModelBinding{
		Provider:            investigation.ProviderBinding{ID: identity.ID, Version: identity.Version},
		ModelName:           cfg.ModelName,
		RequestDigestSHA256: requestDigest,
	}

	// Exactly one invocation.
	artifact, execErr := provider.Execute(ctx, req)
	if execErr != nil {
		out := invoked
		// A refusal is an answer. An error is a failure. They are different
		// facts about the provider and must not collapse.
		var refusal *Refusal
		if errors.As(execErr, &refusal) {
			out.Status = investigation.ModelStatusRefused
			out.Reason = investigation.ModelReasonProviderRefused
		} else {
			out.Status = investigation.ModelStatusErrored
			out.Reason = investigation.ModelReasonExecutionFailed
		}
		return Outcome{Binding: out, ProviderCalls: 1}
	}

	if reason, ok := ValidateArtifact(artifact, req); !ok {
		out := invoked
		out.Status = investigation.ModelStatusInvalid
		out.Reason = reason
		// The rejected artifact's digest is deliberately NOT recorded in the
		// binding: only an ACCEPTED artifact has an identity there, and
		// recording a rejection's digest would make it look like a result.
		return Outcome{Binding: out, ProviderCalls: 1}
	}

	artifactDigest, err := ArtifactDigest(artifact)
	if err != nil {
		out := invoked
		out.Status = investigation.ModelStatusInvalid
		out.Reason = investigation.ModelReasonArtifactUnhashable
		return Outcome{Binding: out, ProviderCalls: 1}
	}

	resolved := invoked
	resolved.Status = investigation.ModelStatusResolved
	resolved.ArtifactDigestSHA256 = artifactDigest
	// The provider's own statement about replayability is carried, never
	// invented: claiming determinism this lane cannot deliver would make an
	// unreproducible run look reproducible.
	resolved.NondeterminismDeclaration = artifact.NondeterminismDeclaration
	if req.Model.DigestAbsent {
		resolved.ModelDigestAbsence = investigation.ModelDigestAbsent
	} else {
		resolved.ModelDigestSHA256 = req.Model.DigestSHA256
	}

	// Fail closed on our own output. If the binding this owner just built does
	// not satisfy the contract, the run is invalid — a resolved status that
	// cannot pass validation must never leave this function.
	if errs := investigation.ValidateModelBinding(resolved); len(errs) > 0 {
		out := invoked
		out.Status = investigation.ModelStatusInvalid
		out.Reason = investigation.ModelReasonArtifactMalformed
		return Outcome{Binding: out, ProviderCalls: 1}
	}
	return Outcome{Binding: resolved, Artifact: &artifact, ProviderCalls: 1}
}
