// SPDX-License-Identifier: Apache-2.0

package briefingfeedback

import (
	"fmt"
	"sort"
)

// UnavailableReason is the CLOSED vocabulary of typed reasons a consumer (today the server)
// cannot obtain a real feedback projection for a scope. The zero value fails closed.
type UnavailableReason string

const (
	// RepositoryContextAbsent: no startup-owned repository context is configured.
	RepositoryContextAbsent UnavailableReason = "repository_context_absent"
	// RepositoryContextDomainMismatch: the request's resolved domain is not the configured
	// repository domain, so the configured filesystem context cannot be consumed.
	RepositoryContextDomainMismatch UnavailableReason = "repository_context_domain_mismatch"
	// CanonicalTaskScopeNotEstablished: natural-language task text carries no canonical task
	// scope, so no task-scoped feedback can be established.
	CanonicalTaskScopeNotEstablished UnavailableReason = "canonical_task_scope_not_established"
	// FeedbackProjectionInternalUnavailable: the canonical owner returned its exceptional Go
	// error; the raw error stays server-side and never reaches the wire or prose.
	FeedbackProjectionInternalUnavailable UnavailableReason = "feedback_projection_internal_unavailable"
)

func validUnavailableReason(r UnavailableReason) bool {
	switch r {
	case RepositoryContextAbsent, RepositoryContextDomainMismatch,
		CanonicalTaskScopeNotEstablished, FeedbackProjectionInternalUnavailable:
		return true
	}
	return false
}

// Scope is the PUBLIC projection identity a consumer supplies to BuildUnavailable. It carries
// NO filesystem root — the constructor never touches the filesystem. RequestedDomain may be
// empty; a non-empty domain incoherent with the identity is sanitized to blank (never repaired
// or trusted). Task is honored only when it is an exact, coherent binding.
type Scope struct {
	RepositoryIdentity string
	RequestedDomain    string
	RequestedFiles     []string
	Task               *TaskBinding
}

// BuildUnavailable produces a valid, deterministic feedback_unavailable projection carrying one
// typed unavailable finding for a typed reason. It never instantiates a bespoke projection at a
// call site, never touches the filesystem, embeds no raw error, no absolute path, and no
// dialogue text. An unknown reason fails closed. The reason rides an existing closed finding
// class (promotion_discovery_unavailable) so the vocabulary is not expanded to express context
// unavailability.
func BuildUnavailable(scope Scope, reason UnavailableReason) (Projection, error) {
	if !validUnavailableReason(reason) {
		return Projection{}, fmt.Errorf("unknown unavailable reason %q", reason)
	}
	identity := canonicalOrBlank(scope.RepositoryIdentity)
	files := canonicalFiles(scope.RequestedFiles)
	finding := Finding{
		Class:          PromotionDiscoveryUnavailable,
		ReasonCode:     string(reason),
		Disposition:    DispositionUnavailable,
		AffectedDomain: canonicalOrBlank(scope.RequestedDomain),
		AffectedFiles:  files,
	}
	p := Projection{
		SchemaVersion:              SchemaVersion,
		ProducerName:               ProducerName,
		ProducerVersion:            ProducerVersion,
		RepositoryIdentity:         identity,
		RequestedDomain:            canonicalDomainOrBlank(scope.RequestedDomain, identity),
		RequestedFiles:             files,
		Availability:               FeedbackUnavailable,
		Findings:                   []Finding{finding},
		NonAuthoritativeProjection: true,
		Bound:                      BoundStatement,
	}
	// Honor a task binding only when it is exact and coherent; never fabricate identity.
	if scope.Task != nil && scope.Task.TaskID != "" && scope.Task.SessionID != "" && scope.Task.RepositoryDomain == identity {
		p.TaskID = scope.Task.TaskID
		p.SessionID = scope.Task.SessionID
	}
	dig, err := ComputeDigest(p)
	if err != nil {
		return Projection{}, err
	}
	p.DigestSHA256 = dig
	if err := ValidateProjection(p); err != nil {
		return Projection{}, err
	}
	return p, nil
}

// canonicalFiles canonicalizes, deduplicates, and sorts a file list, dropping any entry that is
// not repository-relative canonical (a fail-closed carrier never surfaces an unsafe path).
func canonicalFiles(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, f := range in {
		c, ok := canonicalRelFile(f)
		if !ok || seen[c] {
			continue
		}
		seen[c] = true
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}
