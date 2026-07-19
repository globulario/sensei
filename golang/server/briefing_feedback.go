// SPDX-License-Identifier: AGPL-3.0-only

// @awareness namespace=globular.awareness_graph
// @awareness component=server.briefing
// @awareness file_role=grpc_rpc_handler
// @awareness implements=globular.awareness_graph:invariant.closure.briefing_feedback_prose_is_rendered_only_from_the_typed_projection
// @awareness implements=globular.awareness_graph:invariant.closure.briefing_feedback_unavailability_never_erases_base_graph_briefing
package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/globulario/sensei/golang/architecture/briefingfeedback"
	awarenesspb "github.com/globulario/sensei/golang/pb"
)

// feedbackBriefingScope is the exact, already-resolved scope the server hands to the feedback
// selector. It carries NO filesystem root. rawFile/rawDomain are the UNTRIMMED request values —
// the feedback leg checks their canonicality itself so a padded identity the graph briefing
// silently trimmed can never be repaired into feedback authority.
type feedbackBriefingScope struct {
	taskOnly        bool   // natural-language task briefing — no canonical file/task scope
	effectiveDomain string // the exact domain the graph briefing already resolved
	file            string // canonical repository-relative file (file-scoped only)
	rawFile         string // untrimmed BriefingRequest.file
	rawDomain       string // untrimmed BriefingRequest.domain
}

// resolvedBriefingFeedback is the ONE atomic result: a valid projection AND its exact wire
// mapping. The RPC uses this single pair for field 7, prose, referenced ids, and status, so the
// structured section, the prose, and the status can never diverge or reference different
// projections.
type resolvedBriefingFeedback struct {
	Projection briefingfeedback.Projection
	Wire       *awarenesspb.BriefingFeedbackProjection
}

// briefingFeedback selects exactly ONE feedback path and returns the canonical projection. It
// is a thin consumer: it never rediscovers promotions, reimplements verification, calculates
// availability, or reinterprets findings.
//
// Precedence: repository context absent (feature not configured) → repository_context_absent
// (no filesystem access, no owner invocation); then noncanonical RAW identity → feedback_invalid
// (no owner invocation); then task-only → canonical_task_scope_not_established; then foreign
// resolved domain → repository_context_domain_mismatch; then the exact file-scoped owner call.
func (s *server) briefingFeedback(ctx context.Context, scope feedbackBriefingScope) (briefingfeedback.Projection, error) {
	// Repository context absent — feedback is unavailable but explicit on the wire. No
	// filesystem access, no owner invocation, no task identity.
	if s.briefingRepo == nil {
		return briefingfeedback.BuildUnavailable(feedbackScopeIdentity(scope, ""), briefingfeedback.RepositoryContextAbsent)
	}
	repo := s.briefingRepo

	// Noncanonical RAW identity is never repaired into feedback authority. The domain is checked
	// in both modes; the file only in file-scoped mode. No promotion discovery/verification runs.
	if !feedbackDomainCanonical(scope.rawDomain) {
		return briefingfeedback.BuildInvalid(feedbackScopeIdentity(scope, repo.Domain), briefingfeedback.RequestedDomainNoncanonical)
	}
	if !scope.taskOnly && !feedbackFileCanonical(scope.rawFile) {
		return briefingfeedback.BuildInvalid(feedbackScopeIdentity(scope, repo.Domain), briefingfeedback.RequestedFileNoncanonical)
	}

	// Task-only natural-language briefing — task text is not canonical task identity.
	if scope.taskOnly {
		return briefingfeedback.BuildUnavailable(feedbackScopeIdentity(scope, repo.Domain), briefingfeedback.CanonicalTaskScopeNotEstablished)
	}

	// Foreign-domain request — the graph briefing stays usable, but the configured filesystem
	// context cannot be consumed for a domain that is not the configured repository domain.
	if scope.effectiveDomain != repo.Domain {
		return briefingfeedback.BuildUnavailable(feedbackScopeIdentity(scope, repo.Domain), briefingfeedback.RepositoryContextDomainMismatch)
	}

	// File-scoped, matching repository — invoke the canonical owner with the IMMUTABLE
	// configured root + domain and the exact repository-relative file. No task binding.
	proj, err := briefingfeedback.Build(ctx, briefingfeedback.Request{
		RepositoryRoot:     repo.Root,
		RepositoryIdentity: repo.Domain,
		RequestedDomain:    repo.Domain,
		RequestedFiles:     []string{scope.file},
	})
	if err != nil {
		// The owner's exceptional Go error stays server-side; the caller gets a typed
		// unavailable projection, never the raw error.
		s.logger.Printf("awareness-graph: briefing feedback owner error (sanitized): %v", err)
		return briefingfeedback.BuildUnavailable(feedbackScopeIdentity(scope, repo.Domain), briefingfeedback.FeedbackProjectionInternalUnavailable)
	}
	return proj, nil
}

// resolveBriefingFeedback resolves the projection AND its wire mapping as one atomic pair. On
// the (expected-unreachable) failure to map a valid primary projection, it falls back to a
// canonical feedback_projection_internal_unavailable projection and maps THAT, using it
// consistently. If even the fallback cannot map, it returns an error so the RPC fails with a
// typed gRPC internal — never a response where prose/status/references and field 7 diverge.
func (s *server) resolveBriefingFeedback(ctx context.Context, scope feedbackBriefingScope) (resolvedBriefingFeedback, error) {
	proj, err := s.briefingFeedback(ctx, scope)
	if err != nil {
		return resolvedBriefingFeedback{}, fmt.Errorf("build briefing feedback projection: %w", err)
	}
	mapper := s.feedbackMapper
	if mapper == nil {
		mapper = briefingFeedbackToProto
	}
	if wire, werr := mapper(proj); werr == nil {
		return resolvedBriefingFeedback{Projection: proj, Wire: wire}, nil
	} else {
		s.logger.Printf("awareness-graph: briefing feedback wire mapping failed (sanitized): %v", werr)
	}
	// Adapter-failure fallback: one canonical internal-unavailable projection, mapped once.
	domain := ""
	if s.briefingRepo != nil {
		domain = s.briefingRepo.Domain
	}
	fb, ferr := briefingfeedback.BuildUnavailable(feedbackScopeIdentity(scope, domain), briefingfeedback.FeedbackProjectionInternalUnavailable)
	if ferr != nil {
		return resolvedBriefingFeedback{}, fmt.Errorf("build feedback fallback: %w", ferr)
	}
	fwire, fwerr := mapper(fb)
	if fwerr != nil {
		return resolvedBriefingFeedback{}, fmt.Errorf("map feedback fallback: %w", fwerr)
	}
	return resolvedBriefingFeedback{Projection: fb, Wire: fwire}, nil
}

// feedbackDomainCanonical reports whether a RAW requested domain is canonical: unpadded and
// free of embedded whitespace. Empty is permitted (the graph domain-resolution contract governs
// it). A trimmed/case-folded value is never used as proof of repository match.
func feedbackDomainCanonical(raw string) bool {
	return raw == strings.TrimSpace(raw) && !strings.ContainsAny(raw, " \t\r\n")
}

// feedbackFileCanonical reports whether a RAW requested file is canonical: unpadded, non-empty,
// and a safe repository-relative path (no leading/trailing whitespace repaired, no unsafe path
// normalized into acceptance). Backslash→slash parity is permitted.
func feedbackFileCanonical(raw string) bool {
	if raw != strings.TrimSpace(raw) || raw == "" {
		return false
	}
	s := strings.ReplaceAll(raw, "\\", "/")
	if strings.HasPrefix(s, "/") {
		return false
	}
	if len(s) >= 2 && s[1] == ':' {
		return false
	}
	for _, seg := range strings.Split(s, "/") {
		if seg == "" || seg == "." || seg == ".." || seg != strings.TrimSpace(seg) {
			return false
		}
	}
	return true
}

// feedbackScopeIdentity builds the public identity for a typed unavailable/invalid projection.
// It never carries a filesystem root.
func feedbackScopeIdentity(scope feedbackBriefingScope, repoDomain string) briefingfeedback.Scope {
	sc := briefingfeedback.Scope{RepositoryIdentity: repoDomain, RequestedDomain: scope.effectiveDomain}
	if !scope.taskOnly && scope.file != "" {
		sc.RequestedFiles = []string{scope.file}
	}
	return sc
}

// feedbackReferencedIDs returns the class-qualified governed record identities
// (<governed-kind>:<canonical-record-id>) to append to BriefingResponse.referenced_ids. Only
// verified governed records qualify; promotion-lineage/question/answer/receipt/task/session
// identities remain structured feedback provenance and never enter the generic list.
func feedbackReferencedIDs(p briefingfeedback.Projection) []string {
	var out []string
	for _, r := range p.Records {
		if r.GovernedKind == "" || r.CanonicalRecordID == "" {
			continue
		}
		out = append(out, r.GovernedKind+":"+r.CanonicalRecordID)
	}
	return out
}

// combineBriefingStatus composes base ⊕ feedback by the frozen table. Feedback never converts a
// degraded state into OK; a degraded/unavailable/invalid feedback yields DEGRADED; only an
// available feedback can lift a base EMPTY to OK.
func combineBriefingStatus(base awarenesspb.BriefingStatus, avail briefingfeedback.Availability) awarenesspb.BriefingStatus {
	switch avail {
	case briefingfeedback.FeedbackAvailable:
		if base == awarenesspb.BriefingStatus_BRIEFING_STATUS_EMPTY {
			return awarenesspb.BriefingStatus_BRIEFING_STATUS_OK
		}
		return base
	case briefingfeedback.FeedbackEmpty:
		return base
	default: // degraded, unavailable, invalid
		return awarenesspb.BriefingStatus_BRIEFING_STATUS_DEGRADED
	}
}

// briefingFeedbackProse renders the feedback section ONLY from the validated canonical
// projection. It renders governed-record identity (kind, canonical record id, source document)
// as truth; findings are rendered as typed diagnostics, never as governed records; raw answer
// text and provenance identities are never rendered.
func briefingFeedbackProse(p briefingfeedback.Projection) string {
	switch p.Availability {
	case briefingfeedback.FeedbackEmpty:
		return "" // concise: no section, no warning
	case briefingfeedback.FeedbackUnavailable:
		return "\n\nGoverned briefing feedback unavailable\nReason: " + firstFindingReason(p)
	case briefingfeedback.FeedbackInvalid:
		return "\n\nGoverned briefing feedback INVALID\nReason: " + firstFindingReason(p)
	}
	var b strings.Builder
	if p.Availability == briefingfeedback.FeedbackDegraded {
		b.WriteString("\n\nGoverned briefing feedback (incomplete — some verified feedback is unavailable):")
		for _, f := range p.Findings {
			fmt.Fprintf(&b, "\n- [%s] %s", f.Class, f.ReasonCode)
		}
	} else {
		b.WriteString("\n\nGoverned briefing feedback:")
	}
	for _, r := range p.Records {
		fmt.Fprintf(&b, "\n- %s:%s", r.GovernedKind, r.CanonicalRecordID)
		if r.SourceDocument != "" {
			fmt.Fprintf(&b, "\n  Source: %s", r.SourceDocument)
		}
	}
	return b.String()
}

func firstFindingReason(p briefingfeedback.Projection) string {
	if len(p.Findings) > 0 && p.Findings[0].ReasonCode != "" {
		return p.Findings[0].ReasonCode
	}
	return "unknown"
}
