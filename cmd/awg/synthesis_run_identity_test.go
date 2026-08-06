// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"testing"

	"github.com/globulario/sensei/golang/architecture/workspacecontract"
)

func completeIdentityFixture() workspacecontract.Identity {
	revision := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	return workspacecontract.Identity{
		CompositionState: workspacecontract.CompositionComplete,
		Binding: workspacecontract.Binding{
			RepositoryDomain: "github.com/example/tinyrepo",
			RevisionStatus:   workspacecontract.RevisionResolved,
			Revision:         &revision,
		},
		GraphAuthority: &workspacecontract.GraphAuthority{Authoritative: true},
		CoverageState:  workspacecontract.CoverageStateSufficient,
	}
}

func TestIdentityPartialOnlyForThinCoverage_TrueForThinCoverageAlone(t *testing.T) {
	id := completeIdentityFixture()
	id.CoverageState = "COVERAGE_STATE_THIN"
	id.CompositionState = workspacecontract.CompositionPartial
	if !identityPartialOnlyForThinCoverage(id) {
		t.Fatal("expected true: revision resolved, graph authoritative, only coverage insufficient")
	}
}

func TestIdentityPartialOnlyForThinCoverage_FalseWhenComplete(t *testing.T) {
	id := completeIdentityFixture()
	if identityPartialOnlyForThinCoverage(id) {
		t.Fatal("expected false: a complete identity is never eligible for the override")
	}
}

func TestIdentityPartialOnlyForThinCoverage_FalseWhenRevisionUnresolved(t *testing.T) {
	id := completeIdentityFixture()
	id.CoverageState = "COVERAGE_STATE_THIN"
	id.CompositionState = workspacecontract.CompositionPartial
	id.Binding.RevisionStatus = workspacecontract.RevisionUnavailable
	id.Binding.Revision = nil
	if identityPartialOnlyForThinCoverage(id) {
		t.Fatal("expected false: an unresolved revision must never be overridable by --force-thin-coverage")
	}
}

func TestIdentityPartialOnlyForThinCoverage_FalseWhenGraphNotAuthoritative(t *testing.T) {
	id := completeIdentityFixture()
	id.CoverageState = "COVERAGE_STATE_THIN"
	id.CompositionState = workspacecontract.CompositionPartial
	id.GraphAuthority.Authoritative = false
	if identityPartialOnlyForThinCoverage(id) {
		t.Fatal("expected false: a non-authoritative graph must never be overridable by --force-thin-coverage")
	}
}

func TestIdentityPartialOnlyForThinCoverage_FalseWhenGraphAuthorityNil(t *testing.T) {
	id := completeIdentityFixture()
	id.CoverageState = "COVERAGE_STATE_THIN"
	id.CompositionState = workspacecontract.CompositionPartial
	id.GraphAuthority = nil
	if identityPartialOnlyForThinCoverage(id) {
		t.Fatal("expected false: an unreachable graph authority must never be overridable by --force-thin-coverage")
	}
}
