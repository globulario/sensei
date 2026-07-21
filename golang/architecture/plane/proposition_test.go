// SPDX-License-Identifier: AGPL-3.0-only

package plane

import (
	"testing"

	"github.com/globulario/sensei/golang/architecture"
)

func TestPropositionKeyIsDeterministic(t *testing.T) {
	c := claim("claim.one", architecture.PlaneObserved, []string{"fact.one"}, nil, nil)
	if PropositionKey(c) != PropositionKey(c) {
		t.Fatal("proposition key changed")
	}
}

func TestPropositionKeyIgnoresPlane(t *testing.T) {
	a := claim("claim.one", architecture.PlaneObserved, []string{"fact.one"}, nil, nil)
	b := a
	b.ArchitecturalPlane = architecture.PlaneDesired
	if PropositionKey(a) != PropositionKey(b) {
		t.Fatal("plane affected proposition key")
	}
}

func TestPropositionKeyIgnoresOrigin(t *testing.T) {
	a := claim("claim.one", architecture.PlaneObserved, []string{"fact.one"}, nil, nil)
	b := a
	b.AssertionOrigin = architecture.OriginObserved
	if PropositionKey(a) != PropositionKey(b) {
		t.Fatal("origin affected proposition key")
	}
}

func TestPropositionKeyIgnoresStatus(t *testing.T) {
	a := claim("claim.one", architecture.PlaneObserved, []string{"fact.one"}, nil, nil)
	b := a
	b.EpistemicStatus = architecture.StatusSupported
	if PropositionKey(a) != PropositionKey(b) {
		t.Fatal("status affected proposition key")
	}
}

func TestPropositionKeyIgnoresRule(t *testing.T) {
	a := claim("claim.one", architecture.PlaneObserved, []string{"fact.one"}, nil, nil)
	b := a
	b.InferenceRule = "rule.other"
	if PropositionKey(a) != PropositionKey(b) {
		t.Fatal("rule affected proposition key")
	}
}

func TestPropositionKeyChangesWhenStatementChanges(t *testing.T) {
	a := claim("claim.one", architecture.PlaneObserved, []string{"fact.one"}, nil, nil)
	b := a
	b.Statement.Predicate = "other"
	if PropositionKey(a) == PropositionKey(b) {
		t.Fatal("statement change did not affect proposition key")
	}
}

func TestPropositionKeyChangesWhenRepositoryScopeChanges(t *testing.T) {
	a := claim("claim.one", architecture.PlaneObserved, []string{"fact.one"}, nil, nil)
	b := a
	b.Scope.Repository = "github.com/example/other"
	b.Scope.Repo = b.Scope.Repository
	if PropositionKey(a) == PropositionKey(b) {
		t.Fatal("repo change did not affect proposition key")
	}
}
