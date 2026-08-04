// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/binding"
	"github.com/globulario/sensei/golang/architecture/repodomain"
	"github.com/globulario/sensei/golang/architecture/tasksession"
	"github.com/globulario/sensei/golang/architecture/workspacecontract"
	awarenessclient "github.com/globulario/sensei/golang/client"
)

// composeSynthesisRunIdentity resolves every governed fact through its
// existing owner and hands them to workspacecontract.ComposeIdentity, which
// does only deterministic composition -- never a live lookup itself. This is
// a deliberate port of cmd/awareness-mcp/workspace_tools.go's
// composeWorkspaceIdentity: that function is unexported in package main of a
// different binary, so it cannot be imported, only mirrored. Keep the two in
// sync by hand if either changes -- they must resolve identity identically.
//
// Unlike the MCP tool (which treats "task" as an optional caller-supplied
// argument that may be entirely absent), synthesis-run always has a resolved
// task directory by this point in its own flow, so taskDir is passed
// directly rather than re-resolved through resolveWorkspaceTaskIdentity's
// not_requested branch.
func composeSynthesisRunIdentity(ctx context.Context, addr, absRepo, taskDir string) (workspacecontract.Identity, error) {
	var limitations []workspacecontract.Limitation

	domain, domainErr := repodomain.Configured(absRepo)
	if domainErr != nil {
		return workspacecontract.Identity{}, fmt.Errorf("repository domain configuration: %w", domainErr)
	}
	domainSource := workspacecontract.RepositoryDomainUnbound
	if domain != "" {
		domainSource = workspacecontract.RepositoryDomainConfigured
	} else {
		limitations = append(limitations, workspacecontract.Limitation{
			Source: "golang/architecture/repodomain", Scope: "repository_domain",
			Reason: "no repository.domain is configured in .sensei/config.yaml for this checkout", Blocking: true,
		})
	}

	revision, revisionStatus, revLimitations := architecture.ResolveRevision(absRepo, true)
	for _, l := range revLimitations {
		limitations = append(limitations, workspacecontract.Limitation{Source: l.Source, Scope: l.Scope, Reason: l.Reason, Blocking: l.Blocking})
	}

	var treeDigest string
	if revisionStatus == architecture.RevisionResolved {
		td, terr := binding.RepositoryTreeDigestSHA256(absRepo, revision)
		if terr != nil {
			limitations = append(limitations, workspacecontract.Limitation{
				Source: "golang/architecture/binding", Scope: "tree_digest_sha256", Reason: terr.Error(), Blocking: false,
			})
		} else {
			treeDigest = td
		}
	}

	conn, connErr := connectAWG(addr)
	var graphAuthority *workspacecontract.GraphAuthority
	coverageState := "COVERAGE_STATE_UNSPECIFIED"
	if connErr != nil {
		limitations = append(limitations, workspacecontract.Limitation{
			Source: "golang/server Metadata RPC", Scope: "graph_authority", Reason: connErr.Error(), Blocking: true,
		})
	} else {
		defer conn.Close()
		metaResp, metaErr := conn.MetadataScoped(ctx, domain)
		if metaErr != nil {
			limitations = append(limitations, workspacecontract.Limitation{
				Source: "golang/server Metadata RPC", Scope: "graph_authority", Reason: metaErr.Error(), Blocking: true,
			})
		} else {
			mv := awarenessclient.InterpretMetadataAuthority(metaResp)
			graphAuthority = &workspacecontract.GraphAuthority{
				Authoritative:                   mv.Authoritative,
				GraphFreshnessState:             awarenessclient.EffectiveMetadataFreshness(metaResp).String(),
				GraphFreshnessDetail:            metaResp.GetGraphFreshnessDetail(),
				SeedState:                       metaResp.GetSeedState().String(),
				BuildProvenanceState:            metaResp.GetBuildProvenanceState().String(),
				LiveStoreGraphDigestSHA256:      metaResp.GetLiveStoreGraphDigestSha256(),
				LiveStoreGraphTripleCount:       metaResp.GetLiveStoreGraphTripleCount(),
				EmbeddedSeedDigestSHA256:        metaResp.GetEmbeddedSeedDigestSha256(),
				EmbeddedTransactionStampPresent: metaResp.GetEmbeddedTransactionStampPresent(),
				EmbeddedTransactionMatchesSeed:  metaResp.GetEmbeddedTransactionMatchesSeed(),
				CertifiedAwarenessGraphCommit:   metaResp.GetCertifiedAwarenessGraphCommit(),
				CertifiedServicesRepoCommit:     metaResp.GetCertifiedServicesRepoCommit(),
			}
			if cs := metaResp.GetCoverageState().String(); cs != "" {
				coverageState = cs
			}
		}
	}

	taskIdentity, taskLimitations := resolveSynthesisRunTaskIdentity(absRepo, taskDir)
	limitations = append(limitations, taskLimitations...)

	return workspacecontract.ComposeIdentity(workspacecontract.IdentityInputs{
		RepositoryDomainSource: domainSource,
		RepositoryDomain:       domain,
		Revision:               revision,
		RevisionStatus:         workspacecontract.RevisionStatus(revisionStatus),
		TreeDigestSHA256:       treeDigest,
		// GraphDigestSHA256/GraphDigestStatus deliberately stay unset
		// (not_requested/null): that pair carries ClaimDocumentBinding's
		// task/snapshot-scoped local graph.nt identity, a different
		// authority from GraphAuthority.LiveStoreGraphDigestSHA256 above --
		// never borrow the live-store digest into this field, even though
		// both describe "the graph": they are not the same fact.
		GraphDigestSHA256: "",
		GraphDigestStatus: workspacecontract.GraphDigestNotRequested,
		GraphAuthority:    graphAuthority,
		CoverageState:     coverageState,
		TaskIdentity:      taskIdentity,
		Limitations:       limitations,
	}), nil
}

// resolveSynthesisRunTaskIdentity mirrors workspace_tools.go's
// resolveWorkspaceTaskIdentity's resolved/unavailable branches. synthesis-run
// never takes the not_requested branch: by the time this is called, a task
// directory has already been resolved (via an active pointer or an explicit
// --task flag) and its session already loaded, so there is always a task to
// describe.
func resolveSynthesisRunTaskIdentity(absRepo, taskDir string) (workspacecontract.TaskIdentity, []workspacecontract.Limitation) {
	state, _, err := tasksession.ControlStatus(absRepo, strings.TrimSpace(taskDir), false)
	if err != nil {
		return workspacecontract.TaskIdentity{State: workspacecontract.TaskIdentityUnavailable},
			[]workspacecontract.Limitation{{Source: "golang/architecture/tasksession", Scope: "task_identity", Reason: err.Error(), Blocking: false}}
	}
	if strings.TrimSpace(state.TaskID) == "" {
		return workspacecontract.TaskIdentity{State: workspacecontract.TaskIdentityUnavailable},
			[]workspacecontract.Limitation{{Source: "golang/architecture/tasksession", Scope: "task_identity", Reason: "resolved task control state carries no task_id", Blocking: false}}
	}
	resolvedTaskID := state.TaskID
	return workspacecontract.TaskIdentity{State: workspacecontract.TaskIdentityResolved, TaskID: &resolvedTaskID}, nil
}

// identityPartialOnlyForThinCoverage reports whether identity is Partial
// strictly because CoverageState is not sufficient, with every other
// completeness dimension deriveCompositionState checks (revision resolved,
// graph authority reachable and authoritative) otherwise satisfied. It
// recomputes those same conditions directly from Identity's own exported
// fields -- never by scanning Limitations text/scope strings, which are a
// display concern, not the source of truth -- so it can never silently
// drift out of sync with workspacecontract.deriveCompositionState's real
// definition of Partial.
//
// --force-thin-coverage is gated on this returning true: a stale/dirty
// revision or an unreachable/non-authoritative graph must always refuse,
// regardless of the flag -- only "the graph is real and current but does
// not yet know enough about this repository" (the expected, honest state
// of a freshly-onboarded benchmark checkout) is ever overridable.
func identityPartialOnlyForThinCoverage(identity workspacecontract.Identity) bool {
	if identity.CompositionState != workspacecontract.CompositionPartial {
		return false
	}
	if identity.Binding.RevisionStatus != workspacecontract.RevisionResolved {
		return false
	}
	if identity.GraphAuthority == nil || !identity.GraphAuthority.Authoritative {
		return false
	}
	return identity.CoverageState != workspacecontract.CoverageStateSufficient
}
