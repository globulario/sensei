// SPDX-License-Identifier: AGPL-3.0-only

// Package whyinvestigation defines the offline, evidence-only contract for
// Phase 10.3. Providers never create canonical architectural claims.
package whyinvestigation

import "context"

type ProviderBinding struct{ ID, Version string }
type RepositoryBinding struct{ Domain, Revision, TreeDigest string }
type CaptureRequest struct {
	Repository  RepositoryBinding
	QueryDigest string
	Targets     []string
	TimeRange   string
}
type Query struct {
	ID, Statement string
	Targets       []string
}
type Snapshot struct {
	Provider               ProviderBinding
	Digest                 string
	Repository             RepositoryBinding
	QueryDigest, TimeRange string
	RawEvidence            []Evidence
	Coverage               Coverage
}
type Evidence struct{ ID, SourceIdentity, SourceDigest, ContentDigest, Content string }
type Coverage struct {
	State, Reason string
	Limitations   []string
}
type Result struct {
	Evidence       []Evidence
	Coverage       Coverage
	Contradictions []string
	Candidates     []Candidate
}
type Candidate struct {
	ID, Statement string
	EvidenceIDs   []string
	Status        string
}

// Provider captures immutable source evidence, then investigates only that
// snapshot. Valid coverage states are searched, partial, unavailable, and
// not_configured; absence is never inferred from an unsearched snapshot.
type Provider interface {
	Identity() ProviderBinding
	Capture(context.Context, CaptureRequest) (Snapshot, error)
	Investigate(context.Context, Snapshot, Query) Result
}
