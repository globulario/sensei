// SPDX-License-Identifier: AGPL-3.0-only

package changebindingproducer

import "github.com/globulario/sensei/golang/architecture/changebinding"

// GitHubAuthority is the real GitHub provenance verifier (the Checkpoint-1 seam). Positive
// authority requires ALL contractually required evidence — a supported event source, a
// known producer issuer, and a known producer tool. A populated issuer string alone is
// NEVER sufficient. It claims no cryptographic guarantee: there is no signature here, so it
// verifies structural + known-producer authority only, and says so.
//
// It distinguishes: verified (supported event + known issuer + known tool), unverifiable
// (structurally valid but issuer/tool not a known producer), and invalid (missing required
// provenance fields or an unsupported event source). It never reads message text.
type GitHubAuthority struct {
	SupportedEventSources map[string]bool
	KnownIssuers          map[string]bool
	KnownTools            map[string]bool
}

// DefaultGitHubAuthority is the frozen v1 authority: the pull_request event, the sensei CI
// issuer, and the sensei producer tool.
func DefaultGitHubAuthority() GitHubAuthority {
	return GitHubAuthority{
		SupportedEventSources: map[string]bool{string(EventPullRequest): true},
		KnownIssuers:          map[string]bool{"sensei.ci": true},
		KnownTools:            map[string]bool{"sensei": true},
	}
}

// VerifyProvenance implements changebinding.ProvenanceVerifier.
func (a GitHubAuthority) VerifyProvenance(b changebinding.ChangeTaskBinding) changebinding.ProvenanceVerification {
	// Structurally-required provenance fields must be present, else invalid.
	if b.Issuer == "" || b.Provenance.EventSource == "" || b.Provenance.Tool == "" || b.Provenance.ToolVersion == "" {
		return changebinding.ProvenanceInvalid
	}
	// An unsupported event source is not an authoritative origin — invalid, never verified.
	if !a.SupportedEventSources[b.Provenance.EventSource] {
		return changebinding.ProvenanceInvalid
	}
	// Structurally valid but not a known producer → unverifiable (never verified on a
	// populated-but-unknown issuer/tool).
	if !a.KnownIssuers[b.Issuer] || !a.KnownTools[b.Provenance.Tool] {
		return changebinding.ProvenanceUnverifiable
	}
	return changebinding.ProvenanceVerified
}
