// SPDX-License-Identifier: Apache-2.0

package changebindingconsumer

import (
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture/changebinding"
)

func hexn(c byte, n int) string { return strings.Repeat(string(c), n) }

type fakeVerifier struct {
	r changebinding.ProvenanceVerification
}

func (f fakeVerifier) VerifyProvenance(changebinding.ChangeTaskBinding) changebinding.ProvenanceVerification {
	return f.r
}

var verified = fakeVerifier{changebinding.ProvenanceVerified}

func validBinding(t *testing.T) changebinding.ChangeTaskBinding {
	t.Helper()
	b := changebinding.ChangeTaskBinding{
		SchemaVersion:                changebinding.SchemaVersion,
		Repository:                   changebinding.RepositoryIdentity{Provider: "github", Identity: "github.com/globulario/sensei"},
		Change:                       changebinding.ChangeIdentity{Provider: "github", ID: "81", HeadSHA: hexn('a', 40), BaseSHA: hexn('b', 40)},
		Task:                         changebinding.TaskIdentity{Directory: ".sensei/tasks/task.x", ID: "task.x", SessionID: "session.x"},
		CompletionResultDigestSHA256: hexn('c', 64),
		Issuer:                       "sensei.ci",
		Publication:                  changebinding.PublicationIdentity{ID: "pub.deadbeef"},
		Provenance:                   changebinding.Provenance{EventSource: "github_pull_request", Checkout: "actions_checkout_v4", Tool: "sensei", ToolVersion: "1.1.0"},
	}
	d, err := changebinding.BindingDigest(b)
	if err != nil {
		t.Fatal(err)
	}
	b.DigestSHA256 = d
	return b
}

func validSubject() CurrentSubject {
	return CurrentSubject{
		RepositoryProvider: "github", RepositoryIdentity: "github.com/globulario/sensei",
		ChangeProvider: "github", ChangeID: "81", BaseSHA: hexn('b', 40), HeadSHA: hexn('a', 40),
		CheckoutRepositoryIdentity: "github.com/globulario/sensei", CheckoutHeadSHA: hexn('a', 40),
		TaskDirectory: ".sensei/tasks/task.x", TaskID: "task.x", TaskSessionID: "session.x",
		CompletionResultDigestSHA256: hexn('c', 64),
		ExpectedIssuer:               "sensei.ci", ExpectedTool: "sensei",
	}
}

func restamp(t *testing.T, b changebinding.ChangeTaskBinding) changebinding.ChangeTaskBinding {
	t.Helper()
	d, err := changebinding.BindingDigest(b)
	if err != nil {
		t.Fatal(err)
	}
	b.DigestSHA256 = d
	return b
}

func TestConsume_HappyPathAccepted(t *testing.T) {
	if g := Consume(true, []changebinding.ChangeTaskBinding{validBinding(t)}, validSubject(), verified); g.Validity != BindingAccepted {
		t.Fatalf("valid binding must be accepted, got %s (%s)", g.Validity, g.Detail)
	}
}

// Passport-swap, repository/change, and current-context correspondence attacks — each
// blocks with its own typed reason; a binding never self-authorizes.
func TestConsume_CorrespondenceAttacksBlock(t *testing.T) {
	cases := []struct {
		name string
		mutB func(*changebinding.ChangeTaskBinding) // mutate the (re-stamped) binding
		mutS func(*CurrentSubject)                  // or mutate the current subject
		want BindingGateValidity
	}{
		// passport swap: binding names another task than the current subject.
		{"binding_names_other_task", func(b *changebinding.ChangeTaskBinding) { b.Task.ID = "task.other" }, nil, BindingGateTaskMismatch},
		{"binding_other_session", func(b *changebinding.ChangeTaskBinding) { b.Task.SessionID = "session.other" }, nil, BindingGateTaskMismatch},
		// completion-result from another task: subject digest differs from the binding's.
		{"completion_result_swap", nil, func(s *CurrentSubject) { s.CompletionResultDigestSHA256 = hexn('9', 64) }, BindingGateCompletionResultMismatch},
		// repository / change / range / head.
		{"other_repository", nil, func(s *CurrentSubject) {
			s.RepositoryIdentity = "github.com/globulario/sensei"
			s.CheckoutRepositoryIdentity = s.RepositoryIdentity
			_ = s
		}, BindingAccepted}, // control: same repo still accepts
		{"repo_lookalike", func(b *changebinding.ChangeTaskBinding) { b.Repository.Identity = "github.com/globulario/sensei-extra" }, nil, BindingGateRepositoryMismatch},
		{"repo_case", func(b *changebinding.ChangeTaskBinding) { b.Repository.Identity = "github.com/globulario/SENSEI" }, nil, BindingGateRepositoryMismatch},
		{"wrong_change_id", func(b *changebinding.ChangeTaskBinding) { b.Change.ID = "999" }, nil, BindingGateChangeRangeMismatch},
		{"wrong_base", func(b *changebinding.ChangeTaskBinding) { b.Change.BaseSHA = hexn('9', 40) }, nil, BindingGateChangeRangeMismatch},
		{"stale_head", func(b *changebinding.ChangeTaskBinding) { b.Change.HeadSHA = hexn('9', 40) }, nil, BindingGateStaleHead},
		// current-context inconsistency: checkout disagrees with the event.
		{"checkout_repo_mismatch", nil, func(s *CurrentSubject) { s.CheckoutRepositoryIdentity = "github.com/other/repo" }, BindingGateCheckoutMismatch},
		{"checkout_head_mismatch", nil, func(s *CurrentSubject) { s.CheckoutHeadSHA = hexn('7', 40) }, BindingGateCheckoutMismatch},
		// producer identity not the expected producer.
		{"producer_issuer", func(b *changebinding.ChangeTaskBinding) { b.Issuer = "sensei.ci"; _ = b }, func(s *CurrentSubject) { s.ExpectedIssuer = "someone.else" }, BindingGateProducerMismatch},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := validBinding(t)
			if c.mutB != nil {
				c.mutB(&b)
				b = restamp(t, b) // keep the binding self-consistent so the failure is a MISMATCH, not a digest error
			}
			s := validSubject()
			if c.mutS != nil {
				c.mutS(&s)
			}
			g := Consume(true, []changebinding.ChangeTaskBinding{b}, s, verified)
			if g.Validity != c.want {
				t.Fatalf("got %s, want %s", g.Validity, c.want)
			}
		})
	}
}

// Publication-set + provenance attacks.
func TestConsume_PublicationAndProvenanceAttacks(t *testing.T) {
	b := validBinding(t)
	s := validSubject()

	if g := Consume(true, nil, s, verified); g.Validity != BindingGateAbsent {
		t.Fatalf("absent required binding must block, got %s", g.Validity)
	}
	if g := Consume(true, []changebinding.ChangeTaskBinding{b, b}, s, verified); g.Validity != BindingGateContradictory {
		t.Fatalf("two publications must be contradictory, got %s", g.Validity)
	}
	// Digest-invalid publication (tampered without restamp).
	tampered := b
	tampered.Issuer = "attacker"
	if g := Consume(true, []changebinding.ChangeTaskBinding{tampered}, s, verified); g.Validity != BindingGatePublicationInvalid {
		t.Fatalf("digest-invalid publication must block, got %s", g.Validity)
	}
	// Valid digest but unverifiable provenance → block (never accepted on issuer string alone).
	if g := Consume(true, []changebinding.ChangeTaskBinding{b}, s, fakeVerifier{changebinding.ProvenanceUnverifiable}); g.Validity != BindingGateUnverifiableProvenance {
		t.Fatalf("unverifiable provenance must block, got %s", g.Validity)
	}
	if g := Consume(true, []changebinding.ChangeTaskBinding{b}, s, fakeVerifier{changebinding.ProvenanceInvalid}); g.Validity != BindingGateUnverifiableProvenance {
		t.Fatalf("invalid provenance must block, got %s", g.Validity)
	}
	// Verified provenance but invalid digest → still block (digest wins).
	if g := Consume(true, []changebinding.ChangeTaskBinding{tampered}, s, verified); g.Validity != BindingGatePublicationInvalid {
		t.Fatalf("verified provenance must not compensate a bad digest, got %s", g.Validity)
	}
	// Unsupported version.
	badver := restamp(t, func() changebinding.ChangeTaskBinding {
		x := b
		x.SchemaVersion = "completion.change_task_binding/v2"
		return x
	}())
	if g := Consume(true, []changebinding.ChangeTaskBinding{badver}, s, verified); g.Validity != BindingGateUnsupportedVersion {
		t.Fatalf("unsupported version must block, got %s", g.Validity)
	}
}

// Compose ordering: the completion thunk (owner invocation) runs ONLY after the binding is
// accepted; any binding failure blocks before completion and never calls the thunk.
func TestCompose_OwnerNeverInvokedBeforeBindingAcceptance(t *testing.T) {
	invoked := 0
	thunk := func() CompletionOutcome {
		invoked++
		return CompletionOutcome{Result: "degraded_pass", Reason: "runtime_unavailable_degraded"}
	}

	// Accepted → thunk runs; a genuine runtime degradation is allowed through.
	f := Compose(gate(BindingAccepted, ""), thunk)
	if invoked != 1 || f.Result != "degraded_pass" || f.Stage != "completion" {
		t.Fatalf("accepted binding must invoke completion; got invoked=%d %+v", invoked, f)
	}
	// Not required → thunk runs (existing behavior preserved).
	Compose(gate(BindingNotRequired, ""), thunk)
	if invoked != 2 {
		t.Fatal("not-required must invoke completion")
	}
	// Every binding failure blocks WITHOUT invoking completion — proven by the spy.
	for _, v := range []BindingGateValidity{
		BindingGateAbsent, BindingGateMalformed, BindingGateStaleHead, BindingGateRepositoryMismatch,
		BindingGateTaskMismatch, BindingGateChangeRangeMismatch, BindingGateContradictory,
		BindingGateUnsupportedVersion, BindingGateUnverifiableProvenance, BindingGatePublicationInvalid,
		BindingGateCheckoutMismatch, BindingGateCompletionResultMismatch, BindingGateTaskSessionMismatch,
		BindingGateProducerMismatch, BindingGateUnsupportedExecution, BindingGateValidity("unknown_zero"),
		BindingGateValidity(""),
	} {
		before := invoked
		f := Compose(gate(v, ""), thunk)
		if invoked != before {
			t.Fatalf("binding failure %q must NOT invoke completion", v)
		}
		if f.Result != "block" || f.Stage != "binding" || f.Reason != string(v) {
			t.Fatalf("binding failure %q must block at the binding stage, got %+v", v, f)
		}
		if f.Completion != nil {
			t.Fatalf("no completion outcome must be recorded for a binding failure %q", v)
		}
	}
}

// Invalid binding + genuine runtime failure blocks (the runtime lane is unreachable); an
// authoritative binding + runtime failure may degrade — proving the ordering is the gate.
func TestCompose_InvalidBindingPlusRuntimeBlocks(t *testing.T) {
	runtime := func() CompletionOutcome {
		return CompletionOutcome{Result: "degraded_pass", Reason: "runtime_unavailable_degraded"}
	}
	if f := Compose(gate(BindingGateStaleHead, ""), runtime); f.Result != "block" || f.Stage != "binding" {
		t.Fatalf("invalid binding + runtime must block, got %+v", f)
	}
	if f := Compose(gate(BindingAccepted, ""), runtime); f.Result != "degraded_pass" {
		t.Fatalf("authoritative binding + runtime may degrade, got %+v", f)
	}
}

// Determinism + isolation: not-required preserves the completion decision verbatim; repeated
// evaluation is identical; the binding for one subject never accepts another.
func TestConsume_DeterminismAndIsolation(t *testing.T) {
	b := validBinding(t)
	s := validSubject()
	first := Consume(true, []changebinding.ChangeTaskBinding{b}, s, verified)
	for i := 0; i < 20; i++ {
		if Consume(true, []changebinding.ChangeTaskBinding{b}, s, verified) != first {
			t.Fatal("consume must be deterministic")
		}
	}
	// The same binding evaluated for a DIFFERENT PR/repo is not accepted.
	for _, mut := range []func(*CurrentSubject){
		func(s *CurrentSubject) { s.ChangeID = "82" },
		func(s *CurrentSubject) {
			s.RepositoryIdentity = "github.com/globulario/other"
			s.CheckoutRepositoryIdentity = s.RepositoryIdentity
		},
	} {
		s2 := validSubject()
		mut(&s2)
		if g := Consume(true, []changebinding.ChangeTaskBinding{b}, s2, verified); g.Validity == BindingAccepted {
			t.Fatal("a binding for one subject must not authorize another")
		}
	}
	// not-required passes the completion outcome through unchanged.
	co := CompletionOutcome{Result: "pass", Reason: "authoritative_completion"}
	if f := Compose(gate(BindingNotRequired, ""), func() CompletionOutcome { return co }); f.Result != "pass" || f.Reason != "authoritative_completion" {
		t.Fatalf("not-required must preserve the completion decision, got %+v", f)
	}
}
