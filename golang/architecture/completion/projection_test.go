// SPDX-License-Identifier: AGPL-3.0-only

package completion

import (
	"context"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

func (w world) project(t *testing.T) CompletionProjection {
	t.Helper()
	p, err := BuildCompletionProjection(context.Background(), Request{RepositoryRoot: w.Repo, TaskDirectory: w.TaskDir})
	if err != nil {
		t.Fatalf("build projection: %v", err)
	}
	if !p.NonAuthoritativeProjection {
		t.Fatal("projection must always be marked non-authoritative")
	}
	return p
}

// 1: not completed is represented exactly.
func TestProjectionNotCompleted(t *testing.T) {
	w := seedWorld(t)
	p := w.project(t)
	if p.TerminalState != TerminalNotCompleted || p.ClosureVerdict != ClosureNotCompleted || p.AuthoritativeCompletion {
		t.Fatalf("not-completed projection = %+v", p)
	}
}

// 2 + 10: authoritative completion only from the full conjunction; governed drift
// keeps it authoritative with the flag set.
func TestProjectionAuthoritativeAndDrift(t *testing.T) {
	w := seedWorld(t)
	head := w.ready(t)
	if w.complete(t, head).Outcome != OutcomeCommitted {
		t.Fatal("completion failed")
	}
	p := w.project(t)
	if p.TerminalState != TerminalCommitted || p.ClosureVerdict != ClosureAuthoritativeCompletion || !p.AuthoritativeCompletion {
		t.Fatalf("authoritative projection = %+v", p)
	}
	if p.GovernedDriftAfterCompletion {
		t.Fatal("no drift expected yet")
	}
	changeGoverned(t, w.Repo)
	p2 := w.project(t)
	if !p2.AuthoritativeCompletion || !p2.GovernedDriftAfterCompletion {
		t.Fatalf("drift projection = %+v", p2)
	}
}

// 3-8: residue/broken/duplicate/revoked/wrong-result/missing-result are surfaced
// exactly and never as completed.
func TestProjectionNonAuthoritativeWorlds(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(t *testing.T, w world, head string)
		state   TerminalState
		verdict ClosureVerdict
	}{
		{"receipt_residue", func(t *testing.T, w world, head string) { seedOrphanReceipt(t, w.TaskDir) }, TerminalReceiptWithoutEvent, ClosureBroken},
		{"broken_pair", func(t *testing.T, w world, head string) {
			w.complete(t, head)
			deleteReceiptArtifact(t, w.TaskDir)
		}, TerminalEventWithoutValidReceipt, ClosureBroken},
		{"duplicate", func(t *testing.T, w world, head string) {
			w.complete(t, head)
			seedCompletedEvent(t, w.TaskDir)
		}, TerminalContradictoryHistory, ClosureContradictory},
		{"revoked", func(t *testing.T, w world, head string) {
			w.complete(t, head)
			w.appendRevoked(t)
		}, TerminalContradictoryHistory, ClosureContradictory},
		{"wrong_result", func(t *testing.T, w world, head string) {
			w.complete(t, head)
			rb := currentResultBinding(t, w.TaskDir)
			rb.ResultTreeDigestSHA256 = "9999999999999999999999999999999999999999999999999999999999999999"
			w.appendResultTransition(t, rb)
		}, TerminalWrongBinding, ClosureBroken},
		{"missing_result", func(t *testing.T, w world, head string) {
			w.complete(t, head)
			w.appendEmptyResultTransition(t)
		}, TerminalUnsupported, ClosureUnsupported},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := seedWorld(t)
			head := w.ready(t)
			c.mutate(t, w, head)
			p := w.project(t)
			if p.TerminalState != c.state {
				t.Fatalf("state = %s, want %s", p.TerminalState, c.state)
			}
			if p.ClosureVerdict != c.verdict {
				t.Fatalf("verdict = %s, want %s", p.ClosureVerdict, c.verdict)
			}
			if p.AuthoritativeCompletion {
				t.Fatal("must not be shown as authoritative completion")
			}
		})
	}
}

// 9: projection loss does not erase valid authoritative completion.
func TestProjectionSurvivesProjectionLoss(t *testing.T) {
	w := seedWorld(t)
	head := w.ready(t)
	if w.complete(t, head).Outcome != OutcomeCommitted {
		t.Fatal("completion failed")
	}
	deleteProjections(t, w.TaskDir)
	p := w.project(t)
	if !p.AuthoritativeCompletion || p.ClosureVerdict != ClosureAuthoritativeCompletion {
		t.Fatalf("projection loss erased authoritative completion: %+v", p)
	}
}

// 11: tampering any bound owner breaks the closure verdict surfaced by the projection.
func TestProjectionTamperingBreaks(t *testing.T) {
	w := seedWorld(t)
	head := w.ready(t)
	if w.complete(t, head).Outcome != OutcomeCommitted {
		t.Fatal("completion failed")
	}
	tamperCurrentCorrectness(t, w.TaskDir)
	p := w.project(t)
	if p.AuthoritativeCompletion || p.ClosureVerdict == ClosureAuthoritativeCompletion {
		t.Fatal("tampering must break the surfaced verdict")
	}
}

// 12: unchanged evidence yields a byte-identical projection digest.
func TestProjectionDeterministic(t *testing.T) {
	w := seedWorld(t)
	head := w.ready(t)
	if w.complete(t, head).Outcome != OutcomeCommitted {
		t.Fatal("completion failed")
	}
	if w.project(t).DigestSHA256 != w.project(t).DigestSHA256 {
		t.Fatal("projection is not deterministic")
	}
}

// 13: building the projection performs zero mutation.
func TestProjectionZeroMutation(t *testing.T) {
	w := seedWorld(t)
	head := w.ready(t)
	if w.complete(t, head).Outcome != OutcomeCommitted {
		t.Fatal("completion failed")
	}
	before := treeDigest(t, w.Repo)
	entries := ledgerEntryCount(t, w.TaskDir)
	_ = w.project(t)
	_ = w.project(t)
	if treeDigest(t, w.Repo) != before || ledgerEntryCount(t, w.TaskDir) != entries {
		t.Fatal("building the projection mutated the repository")
	}
}

// 14: the single canonical rendering preserves every terminal state and every
// closure verdict — no fallback collapse into a generic incomplete state.
func TestProjectionSummaryPreservesAllStatesAndVerdicts(t *testing.T) {
	for _, st := range AssessmentBoundStates() {
		p := CompletionProjection{TerminalState: st, ClosureVerdict: ClosureBroken}
		if !strings.Contains(p.Summary(), string(st)) {
			t.Fatalf("summary dropped terminal state %s: %q", st, p.Summary())
		}
	}
	for _, v := range []ClosureVerdict{ClosureAuthoritativeCompletion, ClosureNotCompleted, ClosureBroken, ClosureContradictory, ClosureUnsupported} {
		p := CompletionProjection{TerminalState: TerminalCommitted, ClosureVerdict: v}
		if !strings.Contains(p.Summary(), string(v)) {
			t.Fatalf("summary dropped closure verdict %s: %q", v, p.Summary())
		}
	}
}

// Correction: projection availability is explicit and typed at the surface boundary —
// never omitted, never a fabricated terminal state.
func TestCompletionProjectionEnvelopeAvailability(t *testing.T) {
	// A valid completion is available and carries the real projection.
	w := seedWorld(t)
	head := w.ready(t)
	if w.complete(t, head).Outcome != OutcomeCommitted {
		t.Fatal("completion failed")
	}
	env := BuildCompletionProjectionEnvelope(context.Background(), Request{RepositoryRoot: w.Repo, TaskDirectory: w.TaskDir})
	if env.Availability != CompletionAvailable || env.Projection == nil {
		t.Fatalf("valid completion must be available with a projection: %+v", env)
	}
	if env.Projection.TerminalState != TerminalCommitted {
		t.Fatalf("available projection unchanged expected committed, got %s", env.Projection.TerminalState)
	}
	if !strings.Contains(env.Summary(), "committed") {
		t.Fatalf("envelope summary dropped state: %q", env.Summary())
	}

	// A not-completed task still surfaces as an actual projection, not absence.
	w2 := seedWorld(t)
	env2 := BuildCompletionProjectionEnvelope(context.Background(), Request{RepositoryRoot: w2.Repo, TaskDirectory: w2.TaskDir})
	if env2.Availability != CompletionAvailable || env2.Projection == nil || env2.Projection.TerminalState != TerminalNotCompleted {
		t.Fatalf("not_completed must be an actual projection, not absence: %+v", env2)
	}

	// A projection-owner error (empty task dir) becomes an explicit typed unavailable
	// envelope — visible, not silent, and never a fabricated terminal state.
	envErr := BuildCompletionProjectionEnvelope(context.Background(), Request{RepositoryRoot: w.Repo, TaskDirectory: ""})
	if envErr.Availability != CompletionUnavailable || envErr.Projection != nil {
		t.Fatalf("owner error must be unavailable with no projection: %+v", envErr)
	}
	if envErr.UnavailableClass != "projection_owner_error" {
		t.Fatalf("unavailable class = %q, want projection_owner_error", envErr.UnavailableClass)
	}
	if !strings.Contains(envErr.Summary(), "unavailable") {
		t.Fatalf("unavailable summary must say so: %q", envErr.Summary())
	}

	// A typed unavailable envelope fabricates no terminal state and stays deterministic.
	u1 := UnavailableCompletionEnvelope("task_directory_unresolved", "no active task pointer")
	u2 := UnavailableCompletionEnvelope("task_directory_unresolved", "no active task pointer")
	if u1.Availability != CompletionUnavailable || u1.Projection != nil || !u1.NonAuthoritativeProjection {
		t.Fatalf("unavailable envelope malformed: %+v", u1)
	}
	if u1.DigestSHA256 != u2.DigestSHA256 {
		t.Fatal("unavailable envelope is not deterministic")
	}
	if env.DigestSHA256 == u1.DigestSHA256 {
		t.Fatal("available and unavailable must have distinct identities")
	}
}

// 15: the projection explicitly disclaims terminal authority and repository-wide
// perfection.
func TestProjectionDisclaimsAuthority(t *testing.T) {
	w := seedWorld(t)
	head := w.ready(t)
	if w.complete(t, head).Outcome != OutcomeCommitted {
		t.Fatal("completion failed")
	}
	p := w.project(t)
	if !p.NonAuthoritativeProjection {
		t.Fatal("must be non-authoritative")
	}
	joinedDist := strings.Join(p.Distinctions, " ")
	if !strings.Contains(joinedDist, "not repository-wide perfection") || !strings.Contains(joinedDist, "ONE task") {
		t.Fatalf("distinctions must disclaim repo perfection and scope to one task: %v", p.Distinctions)
	}
	joinedBound := strings.Join(p.Bound, " ")
	if !strings.Contains(joinedBound, "non-authoritative") || !strings.Contains(joinedBound, "sole terminal truth") {
		t.Fatalf("bound must disclaim authority: %v", p.Bound)
	}
	// authoritative_completion must be derived only from the closure verdict.
	if p.AuthoritativeCompletion != (p.ClosureVerdict == ClosureAuthoritativeCompletion) {
		t.Fatal("authoritative_completion must derive only from ClosureAuthoritativeCompletion")
	}
	_ = closureprotocol.TerminalCompleted
}
