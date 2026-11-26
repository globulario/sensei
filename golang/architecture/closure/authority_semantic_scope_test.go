// SPDX-License-Identifier: AGPL-3.0-only

package closure

import (
	"testing"

	"github.com/globulario/sensei/golang/rdf"
)

// INV-AUTHORITY-SEMANTIC-SCOPE. An authority conflict is judged against the
// governed truth, not against a shared file path. These tests model the shape of
// Sensei's own corpus after the reconciliation: one repository authority over
// the source tree, and four task authorities each scoped to a distinct task
// truth, implemented in code under that tree.

// taskAuthority is one task-truth authority: it owns a ledger truth and names
// the code implementing it, but covers no source path.
func taskAuthority(id, state, implementedIn, owner, mutation string) GraphIndex {
	g := authorityDomainGraph(id, authorityDomainOptions{
		OwnsStates:    []string{state},
		OwnerServices: []string{owner},
		MayWrite:      []string{"role.repository_repair_agent"},
		MayRead:       []string{"role.repository_maintainer"},
		MustMutateVia: []string{mutation},
		MustReadVia:   []string{"observation_path.repository_read"},
		TruthLayers:   []string{"task"},
	})
	iri := classIRI("authority_domain", id)
	return mergeGraphs(g, BuildGraphIndex(mustTriplesForPanic(nt(
		triple(iri, rdf.PropAuthorityImplementedIn, implementedIn, false),
	))))
}

func reconciledAuthorityGraph() GraphIndex {
	return mergeGraphs(
		authorityDomainGraph("authority.repo", authorityDomainOptions{
			CoversPaths:   []string{"golang/architecture/"},
			OwnerServices: []string{"service.repository_governance"},
			MayWrite:      []string{"role.repository_repair_agent"},
			MayRead:       []string{"role.repository_maintainer"},
			MustMutateVia: []string{"mutation_path.repository_edit"},
			MustReadVia:   []string{"observation_path.repository_read"},
			TruthLayers:   []string{"repository"},
		}),
		taskAuthority("authority.disposition", "task_ledger.event.question_disposition_recorded",
			"golang/architecture/questiondisposition/", "service.question_disposition", "mutation_path.question_disposition"),
		taskAuthority("authority.certification", "task_artifact.question_resolution_certificate",
			"golang/architecture/questionresolution/", "service.question_resolution", "mutation_path.question_resolution_certification"),
		// Completion and abandonment are implemented by the SAME package and share
		// an owner service; they still govern different truths.
		taskAuthority("authority.completion", "task_ledger.event.completed",
			"golang/architecture/completion/", "service.completion", "mutation_path.terminal_completion"),
		taskAuthority("authority.abandonment", "task_ledger.event.abandoned",
			"golang/architecture/completion/", "service.completion", "mutation_path.terminal_abandonment"),
	)
}

// Acceptance: the original self-admission shape (edits under the source tree,
// including the code implementing every task authority) no longer produces an
// artificial contradiction, and the edit is governed by the repository
// authority alone.
func TestEditingAuthorityImplementationBindsOnlyRepositoryMutation(t *testing.T) {
	files := []string{
		"golang/architecture/closure/model.go",
		"golang/architecture/questiondisposition/record.go",
		"golang/architecture/questionresolution/model.go",
		"golang/architecture/completion/abandon.go",
	}
	for _, file := range files {
		t.Run(file, func(t *testing.T) {
			root := authoredFileRoot(t, file)
			graph := mergeGraphs(sourceFileGraph(file), reconciledAuthorityGraph())
			req := validRequest()
			req.Scope.Files = []string{file}
			req.Scope.AccessMode = AccessWrite
			report, err := Evaluate(validContext(t, root, req, graph))
			if err != nil {
				t.Fatalf("Evaluate: %v", err)
			}
			assertNoAuthorityBlocker(t, report, "closure.authority.applicable_records_contradict")
			var bound []string
			for _, b := range report.AuthorityBindings {
				if b.TargetFile == file {
					bound = append(bound, b.AuthorityDomainID)
				}
			}
			// The task session resolves the edit's mutation authority from exactly
			// these bindings: a code edit must never be authorized against a task
			// authority (before the reconciliation it resolved against the
			// disposition authority, first in id order).
			if len(bound) != 1 || bound[0] != "authority.repo" {
				t.Fatalf("%s bound to %v, want only the repository authority", file, bound)
			}
		})
	}
}

// Acceptance: a genuine conflict still blocks. Two authorities with different
// owners and mutation paths claiming the SAME task truth contradict, even though
// neither covers a path.
func TestTwoAuthoritiesClaimingTheSameTruthStillContradict(t *testing.T) {
	file := "golang/architecture/completion/complete.go"
	root := authoredFileRoot(t, file)
	graph := mergeGraphs(
		sourceFileGraph(file),
		componentStateGraph("component.completion", file, "task_ledger.event.completed"),
		taskAuthority("authority.completion", "task_ledger.event.completed",
			"golang/architecture/completion/", "service.completion", "mutation_path.terminal_completion"),
		taskAuthority("authority.rival", "task_ledger.event.completed",
			"golang/architecture/completion/", "service.rival", "mutation_path.rival"),
	)
	req := validRequest()
	req.Scope.Files = []string{file}
	req.Scope.AccessMode = AccessWrite
	report, err := Evaluate(validContext(t, root, req, graph))
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	assertAuthorityBlocker(t, report, "closure.authority.applicable_records_contradict")
}

// Same ledger, same implementing package, same truth layer, same task: none of
// these alone is a contradiction. Code that writes both terminal truths binds
// each authority to its own truth, with no conflict between them.
func TestDistinctTruthsInOneLedgerAndOneFileDoNotContradict(t *testing.T) {
	file := "golang/architecture/completion/terminal.go"
	root := authoredFileRoot(t, file)
	graph := mergeGraphs(
		sourceFileGraph(file),
		componentStateGraph("component.terminal", file, "task_ledger.event.completed", "task_ledger.event.abandoned"),
		reconciledAuthorityGraph(),
	)
	req := validRequest()
	req.Scope.Files = []string{file}
	req.Scope.AccessMode = AccessWrite
	report, err := Evaluate(validContext(t, root, req, graph))
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	assertNoAuthorityBlocker(t, report, "closure.authority.applicable_records_contradict")
	assertNoAuthorityBlocker(t, report, "closure.authority.state_unmapped")
	byState := map[string]string{}
	for _, b := range report.AuthorityBindings {
		if b.TargetState != "" {
			byState[b.TargetState] = b.AuthorityDomainID
		}
	}
	if byState["task_ledger.event.completed"] != "authority.completion" || byState["task_ledger.event.abandoned"] != "authority.abandonment" {
		t.Fatalf("each truth must bind its own authority: %v", byState)
	}
}
