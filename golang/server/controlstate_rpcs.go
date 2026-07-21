// SPDX-License-Identifier: AGPL-3.0-only

// @awareness namespace=globular.awareness_graph
// @awareness component=server.controlstate
// @awareness file_role=grpc_rpc_handler
// @awareness implements=globular.awareness_graph:invariant.controlstate.server_read_handler_must_consume_canonical_projection
// @awareness implements=globular.awareness_graph:invariant.controlstate.semantic_unavailability_must_remain_response_data
// @awareness implements=globular.awareness_graph:invariant.controlstate.read_surfaces_must_be_structurally_non_mutating
package main

import (
	"context"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/globulario/sensei/golang/architecture/controlstate"
	awarenesspb "github.com/globulario/sensei/golang/pb"
	"github.com/globulario/sensei/golang/server/controlstateproto"
	"github.com/globulario/sensei/golang/store"
)

// The four Phase 9.5 control-panel read RPCs. Each handler performs ONLY:
//
//	validate request shape → acquire typed inputs via the provider seam →
//	call the canonical controlstate builder → map losslessly → return.
//
// Handlers never change closure, assign severity, select lifecycle, change applicability, fill
// missing counts with zero, reorder semantic lists, inspect reason prose, choose a canonical
// class, recompute digests, or write repository/dialogue state.
//
// Error law (design §25): a semantically partial/unavailable/degraded/unknown projection is a
// SUCCESSFUL response carrying its typed state. Transport codes are reserved: InvalidArgument
// (malformed request/filter/page/cursor), FailedPrecondition (expected authority/registry
// mismatch, ambiguous domain), NotFound (absence proven by an available authoritative source),
// Unavailable (infrastructure could not execute), Internal (impossible mapper/validator
// contradiction). Client-facing messages carry typed reasons and logical identities only —
// never raw internal errors or filesystem paths.

// validateControlRepositoryIdentity is the PURELY LOCAL repository-identity check (no store
// access, no domain resolution). Repository identity is a logical identifier — never a
// filesystem path; repository/scope authority stays startup/store-owned.
func validateControlRepositoryIdentity(repositoryIdentity string) error {
	if repositoryIdentity == "" || repositoryIdentity != strings.TrimSpace(repositoryIdentity) {
		return status.Error(codes.InvalidArgument, "repository_identity is required and must be exact (unpadded)")
	}
	if strings.HasPrefix(repositoryIdentity, "/") || strings.HasPrefix(repositoryIdentity, `\`) ||
		(len(repositoryIdentity) >= 3 && repositoryIdentity[1] == ':' && (repositoryIdentity[2] == '/' || repositoryIdentity[2] == '\\')) {
		return status.Error(codes.InvalidArgument, "repository_identity is a logical identity, not a filesystem path")
	}
	return nil
}

// resolveControlScope validates the LOGICAL repository identity locally, then resolves ONE
// canonical effective domain BEFORE any provider acquisition. An empty requested domain
// resolves through the existing single-domain/ambiguity rules — an unresolved empty string is
// never compared as scope authority downstream. Callers that carry additional LOCAL request
// fields (e.g. node_iri) must validate them BEFORE this call: domain resolution may query the
// store, and a malformed request must never reach it.
func (s *server) resolveControlScope(ctx context.Context, repositoryIdentity, domain string) (string, error) {
	if err := validateControlRepositoryIdentity(repositoryIdentity); err != nil {
		return "", err
	}
	if s.store == nil {
		return "", status.Error(codes.Unavailable, "store is unavailable")
	}
	if err := s.requireDomainWhenAmbiguous(ctx, domain); err != nil {
		return "", err
	}
	if domain != "" {
		return domain, nil
	}
	// Empty request: resolve the canonical single-domain scope when the graph names exactly one.
	available, ok, err := s.selectableDomains(ctx)
	if err != nil {
		return "", status.Error(codes.Unavailable, "domain list query failed")
	}
	if ok && len(available) == 1 {
		return available[0], nil
	}
	return "", nil // a graph without domain capability stays unscoped
}

// GetArchitectureControlSnapshot returns architecture.control_snapshot/v1.
//
// @awareness namespace=globular.awareness_graph
// @awareness component=server.controlstate
// @awareness implements=globular.awareness_graph:invariant.controlstate.server_read_handler_must_consume_canonical_projection
// @awareness enforces=globular.awareness_graph:invariant.controlstate.semantic_unavailability_must_remain_response_data
// @awareness tested_by=golang/server/controlstate_rpcs_test.go:TestControlSnapshotRPC_PartialIsSuccess
// @awareness risk=high
func (s *server) GetArchitectureControlSnapshot(ctx context.Context, req *awarenesspb.GetArchitectureControlSnapshotRequest) (*awarenesspb.GetArchitectureControlSnapshotResponse, error) {
	effective, err := s.resolveControlScope(ctx, req.GetRepositoryIdentity(), strings.TrimSpace(req.GetDomain()))
	if err != nil {
		return nil, err
	}
	provider := s.controlStateProvider()
	in, err := provider.ControlSnapshotInput(ctx, req.GetRepositoryIdentity(), effective)
	if err != nil {
		return nil, status.Error(codes.Unavailable, "control-snapshot sources could not be acquired")
	}
	snap, err := controlstate.BuildControlSnapshot(controlstate.DefaultRegistry(), in)
	if err != nil {
		s.logControlf("controlstate: snapshot composition rejected typed inputs: %v", err)
		return nil, status.Error(codes.Internal, "control snapshot composition failed validation")
	}
	wire, err := controlstateproto.ToProtoControlSnapshot(snap)
	if err != nil {
		s.logControlf("controlstate: snapshot mapping failed: %v", err)
		return nil, status.Error(codes.Internal, "control snapshot mapping failed")
	}
	return &awarenesspb.GetArchitectureControlSnapshotResponse{Snapshot: wire}, nil
}

// ListArchitectureArtifacts returns one architecture.artifact_index/v1 page.
//
// @awareness namespace=globular.awareness_graph
// @awareness component=server.controlstate
// @awareness implements=globular.awareness_graph:invariant.controlstate.cursor_must_remain_opaque_and_owner_validated
// @awareness tested_by=golang/server/controlstate_rpcs_test.go:TestListArtifactsRPC_CursorOpaque
// @awareness risk=high
func (s *server) ListArchitectureArtifacts(ctx context.Context, req *awarenesspb.ListArchitectureArtifactsRequest) (*awarenesspb.ListArchitectureArtifactsResponse, error) {
	effective, err := s.resolveControlScope(ctx, req.GetRepositoryIdentity(), strings.TrimSpace(req.GetDomain()))
	if err != nil {
		return nil, err
	}
	// A PRESENT filter enum must be a real vocabulary value; UNSPECIFIED never means "no filter".
	var closureFilter controlstate.ArtifactClosure
	if req.ClosureFilter != nil {
		cf, err := closureFilterFromProto(req.GetClosureFilter())
		if err != nil {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		closureFilter = cf
	}
	var severityFilter controlstate.AttentionSeverity
	if req.SeverityFilter != nil {
		sf, err := severityFilterFromProto(req.GetSeverityFilter())
		if err != nil {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		severityFilter = sf
	}

	provider := s.controlStateProvider()
	catalog, err := provider.CatalogSnapshot(ctx, req.GetRepositoryIdentity(), effective)
	if err != nil {
		return nil, status.Error(codes.Unavailable, "artifact catalog sources could not be acquired")
	}
	reg := controlstate.DefaultRegistry()
	idxReq := controlstate.ArtifactIndexRequest{
		RepositoryIdentity: req.GetRepositoryIdentity(),
		Domain:             effective,
		PageSize:           int(req.GetPageSize()),
		Cursor:             req.GetCursor(), // opaque: owner-validated, never parsed here
		FamilyFilter:       req.GetFamilyFilter(),
		ClassFilter:        req.GetClassFilter(),
		ClosureFilter:      closureFilter,
		SeverityFilter:     severityFilter,
	}
	idx, err := controlstate.BuildArtifactIndex(reg, idxReq, catalog)
	if err != nil {
		// The catalog was provider-built and self-validated; a build rejection here is a
		// request-shaped condition (page size, filter, cursor binding).
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	pageSize := idxReq.PageSize
	wire, err := controlstateproto.ToProtoArtifactIndex(reg, idx, pageSize)
	if err != nil {
		s.logControlf("controlstate: index mapping failed: %v", err)
		return nil, status.Error(codes.Internal, "artifact index mapping failed")
	}
	return &awarenesspb.ListArchitectureArtifactsResponse{Index: wire}, nil
}

// GetArchitectureArtifactState returns architecture.artifact_state/v1 for ONE exact node IRI.
//
// @awareness namespace=globular.awareness_graph
// @awareness component=server.controlstate
// @awareness implements=globular.awareness_graph:invariant.controlstate.artifact_class_must_not_be_caller_selected
// @awareness tested_by=golang/server/controlstate_rpcs_test.go:TestArtifactStateRPC_PreconditionsAndNotFound
// @awareness risk=high
func (s *server) GetArchitectureArtifactState(ctx context.Context, req *awarenesspb.GetArchitectureArtifactStateRequest) (*awarenesspb.GetArchitectureArtifactStateResponse, error) {
	// LOCAL validation first — repository identity, then the ONE shared lexical IRI validator —
	// BEFORE domain resolution or ANY store-dependent operation (the store validates the IRI
	// again as defense in depth). A rejected request performs zero provider, store, and
	// domain-resolution calls.
	if err := validateControlRepositoryIdentity(req.GetRepositoryIdentity()); err != nil {
		return nil, err
	}
	nodeIRI := req.GetNodeIri()
	if verr := store.ValidateQueryIRI(nodeIRI); verr != nil {
		return nil, status.Error(codes.InvalidArgument, "node_iri is not a valid node IRI")
	}
	effective, err := s.resolveControlScope(ctx, req.GetRepositoryIdentity(), strings.TrimSpace(req.GetDomain()))
	if err != nil {
		return nil, err
	}

	provider := s.controlStateProvider()
	reg := controlstate.DefaultRegistry()

	// Preconditions, never alternate authorities: a mismatch fails the RPC, it never rebinds it.
	if want := req.GetExpectedRegistryDigest(); want != "" {
		have, err := reg.Digest()
		if err != nil {
			return nil, status.Error(codes.Internal, "registry digest computation failed")
		}
		if want != have {
			return nil, status.Error(codes.FailedPrecondition, "expected_registry_digest does not match the server registry")
		}
	}
	id, res, bundle, observed, err := provider.ArtifactSourceBundle(ctx, req.GetRepositoryIdentity(), effective, nodeIRI)
	if err != nil {
		// An unobserved graph authority cannot bind an artifact identity at all — and the
		// expected seed digest is never substituted. Closed transport law: Unavailable.
		if err == errGraphAuthorityUnobserved {
			return nil, status.Error(codes.Unavailable, "graph authority is unobserved; artifact state cannot be constructed")
		}
		return nil, status.Error(codes.Unavailable, "artifact sources could not be acquired")
	}
	if want := req.GetExpectedGraphAuthorityIdentity(); want != "" && want != id.GraphAuthorityIdentity {
		return nil, status.Error(codes.FailedPrecondition, "expected_graph_authority_identity does not match the graph authority")
	}
	// Authoritative absence → NotFound ONLY when the exact scoped lookup completed AND the
	// authority is observed AND current AND integrity-verified. Stale or integrity-failed
	// authority leaves absence UNPROVEN: the artifact stays visible as an unknown-class state
	// whose degraded/invalid authority source is response data.
	if !observed && bundle.GraphAuthority.Observed && bundle.GraphAuthority.Current && bundle.GraphAuthority.Integrity {
		return nil, status.Error(codes.NotFound, "artifact is absent from the current authoritative graph scope")
	}

	st, err := controlstate.BuildArtifactState(reg, id, res, bundle)
	if err != nil {
		s.logControlf("controlstate: artifact-state composition rejected typed inputs: %v", err)
		return nil, status.Error(codes.Internal, "artifact state composition failed validation")
	}
	wire, err := controlstateproto.ToProtoArtifactState(st)
	if err != nil {
		s.logControlf("controlstate: artifact-state mapping failed: %v", err)
		return nil, status.Error(codes.Internal, "artifact state mapping failed")
	}
	return &awarenesspb.GetArchitectureArtifactStateResponse{State: wire}, nil
}

// GetOntologyNavigationDescriptor returns ontology.navigation_descriptor/v1. It is
// registry-derived: no store, no repository context, no filesystem — available even when
// repository context is unconfigured.
//
// @awareness namespace=globular.awareness_graph
// @awareness component=server.controlstate
// @awareness implements=globular.awareness_graph:invariant.controlstate.navigation_descriptor_is_derived_from_the_canonical_registry
// @awareness tested_by=golang/server/controlstate_rpcs_test.go:TestNavigationDescriptorRPC_NoRepositoryContextNeeded
// @awareness risk=medium
func (s *server) GetOntologyNavigationDescriptor(ctx context.Context, req *awarenesspb.GetOntologyNavigationDescriptorRequest) (*awarenesspb.GetOntologyNavigationDescriptorResponse, error) {
	d, err := controlstate.BuildNavigationDescriptor(controlstate.DefaultRegistry())
	if err != nil {
		s.logControlf("controlstate: navigation descriptor construction failed: %v", err)
		return nil, status.Error(codes.Internal, "navigation descriptor construction failed")
	}
	wire, err := controlstateproto.ToProtoNavigationDescriptor(d)
	if err != nil {
		s.logControlf("controlstate: navigation descriptor mapping failed: %v", err)
		return nil, status.Error(codes.Internal, "navigation descriptor mapping failed")
	}
	return &awarenesspb.GetOntologyNavigationDescriptorResponse{Descriptor_: wire}, nil
}

// closureFilterFromProto maps a PRESENT wire filter to the closed vocabulary (UNSPECIFIED and
// off-vocabulary values are invalid; absence is handled by the caller, never by UNSPECIFIED).
func closureFilterFromProto(c awarenesspb.ArchitectureArtifactClosure) (controlstate.ArtifactClosure, error) {
	switch c {
	case awarenesspb.ArchitectureArtifactClosure_ARCHITECTURE_ARTIFACT_CLOSURE_CLOSED:
		return controlstate.ClosureClosed, nil
	case awarenesspb.ArchitectureArtifactClosure_ARCHITECTURE_ARTIFACT_CLOSURE_OPEN:
		return controlstate.ClosureOpen, nil
	case awarenesspb.ArchitectureArtifactClosure_ARCHITECTURE_ARTIFACT_CLOSURE_DEGRADED:
		return controlstate.ClosureDegraded, nil
	case awarenesspb.ArchitectureArtifactClosure_ARCHITECTURE_ARTIFACT_CLOSURE_UNKNOWN:
		return controlstate.ClosureUnknown, nil
	case awarenesspb.ArchitectureArtifactClosure_ARCHITECTURE_ARTIFACT_CLOSURE_NOT_APPLICABLE:
		return controlstate.ClosureNotApplicable, nil
	}
	return "", statusErrString("closure_filter is present but not a closed-vocabulary value")
}

// severityFilterFromProto maps a PRESENT wire severity filter to the closed vocabulary.
func severityFilterFromProto(v awarenesspb.ArchitectureAttentionSeverity) (controlstate.AttentionSeverity, error) {
	switch v {
	case awarenesspb.ArchitectureAttentionSeverity_ARCHITECTURE_ATTENTION_SEVERITY_INFORMATIONAL:
		return controlstate.SeverityInformational, nil
	case awarenesspb.ArchitectureAttentionSeverity_ARCHITECTURE_ATTENTION_SEVERITY_ATTENTION:
		return controlstate.SeverityAttention, nil
	case awarenesspb.ArchitectureAttentionSeverity_ARCHITECTURE_ATTENTION_SEVERITY_WARNING:
		return controlstate.SeverityWarning, nil
	case awarenesspb.ArchitectureAttentionSeverity_ARCHITECTURE_ATTENTION_SEVERITY_CRITICAL:
		return controlstate.SeverityCritical, nil
	}
	return "", statusErrString("severity_filter is present but not a closed-vocabulary value")
}

// statusErrString is a plain error for filter validation (converted to InvalidArgument above).
type statusErrString string

func (e statusErrString) Error() string { return string(e) }

// logControlf nil-guards the logger (tests construct bare servers without one).
func (s *server) logControlf(format string, args ...any) {
	if s.logger == nil {
		return
	}
	s.logger.Printf(format, args...)
}
