// SPDX-License-Identifier: Apache-2.0

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
// selector. It carries NO filesystem root (the root is the immutable startup-owned context).
type feedbackBriefingScope struct {
	taskOnly        bool   // natural-language task briefing — no canonical file/task scope
	effectiveDomain string // the exact domain the graph briefing already resolved
	file            string // repository-relative file (file-scoped only)
}

// briefingFeedback selects exactly ONE feedback path and returns the canonical projection. It
// is a thin consumer: it never rediscovers promotions, reimplements verification, calculates
// availability, or reinterprets findings — it either invokes the canonical owner
// (briefingfeedback.Build) for an exact file-scoped request against the matching configured
// repository, or returns a canonical typed-unavailable projection for every other case WITHOUT
// touching the filesystem or invoking the owner.
func (s *server) briefingFeedback(ctx context.Context, scope feedbackBriefingScope) (briefingfeedback.Projection, error) {
	// Repository context absent — no filesystem access, no owner invocation.
	if s.briefingRepo == nil {
		return briefingfeedback.BuildUnavailable(feedbackScopeIdentity(scope, ""), briefingfeedback.RepositoryContextAbsent)
	}
	repo := s.briefingRepo

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
	// configured root + domain and the exact repository-relative file. No task binding: the
	// server never discovers a task for the file.
	proj, err := briefingfeedback.Build(ctx, briefingfeedback.Request{
		RepositoryRoot:     repo.Root,
		RepositoryIdentity: repo.Domain,
		RequestedDomain:    repo.Domain,
		RequestedFiles:     []string{scope.file},
	})
	if err != nil {
		// The owner's exceptional Go error stays server-side; the wire/prose get a typed
		// unavailable projection, never the raw error.
		s.logger.Printf("awareness-graph: briefing feedback owner error (sanitized): %v", err)
		return briefingfeedback.BuildUnavailable(feedbackScopeIdentity(scope, repo.Domain), briefingfeedback.FeedbackProjectionInternalUnavailable)
	}
	return proj, nil
}

// resolveBriefingFeedback returns the feedback projection for a scope, or nil to OMIT feedback
// entirely. Feedback is an opt-in configured feature: when no startup-owned repository context
// is configured, the feedback leg is DISABLED and the response is byte-for-byte the pre-9.6
// graph briefing (no field 7, no prose, no status change) — "the graph briefing remains
// usable." nil is also returned in the impossible last-resort case that not even a typed
// unavailable projection can be constructed, so the graph briefing is never aborted.
func (s *server) resolveBriefingFeedback(ctx context.Context, scope feedbackBriefingScope) *briefingfeedback.Projection {
	if s.briefingRepo == nil {
		return nil // feedback disabled — graph briefing unchanged
	}
	proj, err := s.briefingFeedback(ctx, scope)
	if err != nil {
		s.logger.Printf("awareness-graph: briefing feedback projection unavailable (sanitized): %v", err)
		return nil
	}
	return &proj
}

// feedbackWire maps a resolved projection to its additive wire message, or nil (a mapping
// failure is logged and never aborts the graph briefing).
func (s *server) feedbackWire(proj *briefingfeedback.Projection) *awarenesspb.BriefingFeedbackProjection {
	if proj == nil {
		return nil
	}
	wire, err := briefingFeedbackToProto(*proj)
	if err != nil {
		s.logger.Printf("awareness-graph: briefing feedback wire mapping failed (sanitized): %v", err)
		return nil
	}
	return wire
}

// feedbackScopeIdentity builds the public identity for a typed-unavailable projection. It never
// carries a filesystem root.
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
