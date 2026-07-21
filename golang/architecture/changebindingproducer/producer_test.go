// SPDX-License-Identifier: AGPL-3.0-only

package changebindingproducer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture/changebinding"
	"gopkg.in/yaml.v3"
)

func hexn(c byte, n int) string { return strings.Repeat(string(c), n) }

func validInput() ProducerInput {
	head, base := hexn('a', 40), hexn('b', 40)
	return ProducerInput{
		EventSource:                  EventPullRequest,
		RepositoryProvider:           "github",
		RepositoryIdentity:           "github.com/globulario/sensei",
		ChangeProvider:               "github",
		ChangeID:                     "81",
		BaseSHA:                      base,
		HeadSHA:                      head,
		CheckoutRepositoryIdentity:   "github.com/globulario/sensei",
		CheckoutHeadSHA:              head,
		TaskDirectory:                ".sensei/tasks/task.x",
		TaskID:                       "task.x",
		TaskSessionID:                "session.x",
		CompletionResultDigestSHA256: hexn('c', 64),
		CompletionResultTaskID:       "task.x",
		CompletionResultSessionID:    "session.x",
		Issuer:                       "sensei.ci",
		Checkout:                     "actions_checkout_v4",
		Tool:                         "sensei",
		ToolVersion:                  "1.1.0",
	}
}

func auth() GitHubAuthority { return DefaultGitHubAuthority() }

func TestProduce_HappyPathIsAuthoritative(t *testing.T) {
	r := Produce(validInput(), auth())
	if !r.OK() || r.Failure != FailNone {
		t.Fatalf("valid input must produce, got failure %q", r.Failure)
	}
	// 36 + 39: the produced binding validates as authoritative through the SAME ckpt1
	// validator, using the shared digest implementation.
	vr := changebinding.ValidateBinding(*r.Binding, changebinding.ExpectedSubject{
		Repository: r.Binding.Repository, Change: r.Binding.Change, Task: &r.Binding.Task,
	}, changebinding.ProvenanceVerified)
	if !vr.IsAuthoritative() {
		t.Fatalf("produced binding must validate authoritative, got %s", vr.Validity)
	}
	if r.Audit.Outcome != "produced" || !r.Audit.SelfValidated || r.Audit.BindingDigestSHA256 != r.Binding.DigestSHA256 {
		t.Fatalf("audit must reflect a self-validated production: %+v", r.Audit)
	}
}

// Staged producer failures, each to its own typed reason. Every mutation is one step off
// the valid input.
func TestProduce_StagedFailures(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*ProducerInput)
		want ProducerFailure
	}{
		// event authority
		{"unsupported_event", func(in *ProducerInput) { in.EventSource = "github_push" }, FailUnsupportedEvent},
		{"missing_repo", func(in *ProducerInput) { in.RepositoryIdentity = "" }, FailMissingEventIdentity},
		{"missing_change", func(in *ProducerInput) { in.ChangeID = "" }, FailMissingEventIdentity},
		{"missing_base", func(in *ProducerInput) { in.BaseSHA = "" }, FailMissingEventIdentity},
		{"missing_head", func(in *ProducerInput) { in.HeadSHA = "" }, FailMissingEventIdentity},
		{"malformed_repo", func(in *ProducerInput) { in.RepositoryIdentity = "bad repo" }, FailMalformedRepositoryIdentity},
		{"malformed_change", func(in *ProducerInput) { in.ChangeID = "8 1" }, FailMalformedChangeIdentity},
		{"malformed_base", func(in *ProducerInput) { in.BaseSHA = "short" }, FailMalformedBaseSHA},
		{"malformed_head", func(in *ProducerInput) { in.HeadSHA = strings.ToUpper(hexn('a', 40)) }, FailMalformedHeadSHA},
		// checkout
		{"checkout_repo_mismatch", func(in *ProducerInput) { in.CheckoutRepositoryIdentity = "github.com/other/repo" }, FailCheckoutRepositoryMismatch},
		{"checkout_head_mismatch", func(in *ProducerInput) { in.CheckoutHeadSHA = hexn('9', 40) }, FailCheckoutHeadMismatch},
		{"checkout_head_shortened", func(in *ProducerInput) { in.CheckoutHeadSHA = "aaaaaaa" }, FailCheckoutHeadMismatch},
		// task
		{"task_absent", func(in *ProducerInput) { in.TaskID = "" }, FailTaskInputAbsent},
		{"task_noncanonical", func(in *ProducerInput) { in.TaskID = " task.x" }, FailTaskIdentityInvalid},
		// completion result
		{"completion_absent", func(in *ProducerInput) { in.CompletionResultDigestSHA256 = "" }, FailCompletionDigestAbsent},
		{"completion_malformed", func(in *ProducerInput) { in.CompletionResultDigestSHA256 = "nothex" }, FailCompletionDigestMalformed},
		{"completion_other_task", func(in *ProducerInput) { in.CompletionResultTaskID = "task.other" }, FailCompletionSubjectMismatch},
		{"completion_other_session", func(in *ProducerInput) { in.CompletionResultSessionID = "session.other" }, FailTaskSessionMismatch},
		// wrong task selection (explicit task ≠ the completion result's subject)
		{"case_varied_task", func(in *ProducerInput) { in.TaskID = "Task.X" }, FailCompletionSubjectMismatch},
		{"prefix_task", func(in *ProducerInput) { in.TaskID = "task" }, FailCompletionSubjectMismatch},
		// provenance: populated but unknown issuer → unverifiable
		{"unknown_issuer", func(in *ProducerInput) { in.Issuer = "attacker" }, FailProvenanceUnverifiable},
		{"unknown_tool", func(in *ProducerInput) { in.Tool = "rogue" }, FailProvenanceUnverifiable},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := validInput()
			c.mut(&in)
			r := Produce(in, auth())
			if r.Failure != c.want {
				t.Fatalf("got %q, want %q", r.Failure, c.want)
			}
			if r.OK() {
				t.Fatal("a failing production must not return OK")
			}
			// Producer failures are their OWN vocabulary — never a 9.4b runtime/degraded reason.
			if strings.Contains(string(r.Failure), "runtime") || strings.Contains(string(r.Failure), "degrad") {
				t.Fatalf("producer failure %q must never be a runtime/degraded reason", r.Failure)
			}
		})
	}
}

// 30: a completion digest cannot be moved to another change without a matching checkout —
// the checkout gate blocks it before construction.
func TestProduce_CannotMoveCompletionToAnotherChangeWithoutCheckout(t *testing.T) {
	in := validInput()
	in.HeadSHA = hexn('d', 40) // "another change" head, but the checkout still names the old head
	if r := Produce(in, auth()); r.Failure != FailCheckoutHeadMismatch {
		t.Fatalf("moving a completion onto another change without a matching checkout must fail checkout, got %q", r.Failure)
	}
}

// 37: altering any bound field after construction breaks self-validation via the digest.
func TestProduce_AlteredFieldFailsDigest(t *testing.T) {
	r := Produce(validInput(), auth())
	b := *r.Binding
	b.Change.HeadSHA = hexn('9', 40) // tamper without restamping
	vr := changebinding.ValidateBinding(b, changebinding.ExpectedSubject{Repository: b.Repository, Change: b.Change, Task: &b.Task}, changebinding.ProvenanceVerified)
	if vr.Validity != changebinding.BindingPublicationInvalid {
		t.Fatalf("altered field must fail digest validation, got %s", vr.Validity)
	}
}

// 38 + 46 + 41: reruns for the same subject are reproducible and identical; the artifact is
// strictly parseable and one binding.
func TestProduce_DeterministicAndPublishable(t *testing.T) {
	r1 := Produce(validInput(), auth())
	r2 := Produce(validInput(), auth())
	if *r1.Binding != *r2.Binding {
		t.Fatal("reruns for the same subject must be identical")
	}
	if r1.Binding.Publication.ID != r2.Binding.Publication.ID {
		t.Fatal("publication identity must be deterministic across reruns")
	}
	data, _ := yaml.Marshal(*r1.Binding)
	if _, v := changebinding.ParseBinding(data); v != "" {
		t.Fatalf("published artifact must strictly parse, got %s", v)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "binding.yaml")
	if f := Publish(*r1.Binding, path); f != FailNone {
		t.Fatalf("publish must succeed, got %q", f)
	}
	// Idempotent rerun (identical bytes).
	if f := Publish(*r2.Binding, path); f != FailNone {
		t.Fatalf("idempotent republish must succeed, got %q", f)
	}
	// 44: the published artifact matches the produced binding.
	published, _ := changebinding.ParseBinding(mustRead(t, path))
	if published != *r1.Binding {
		t.Fatal("published artifact must equal the produced binding")
	}
}

// 42 + 14: a contradictory existing publication (e.g. a force-pushed new head) fails closed;
// never silently overwritten.
func TestPublish_ContradictoryExistingFailsClosed(t *testing.T) {
	r := Produce(validInput(), auth())
	dir := t.TempDir()
	path := filepath.Join(dir, "binding.yaml")
	if f := Publish(*r.Binding, path); f != FailNone {
		t.Fatal("first publish must succeed")
	}
	// A new head → a different binding at the same path.
	in := validInput()
	in.HeadSHA = hexn('e', 40)
	in.CheckoutHeadSHA = hexn('e', 40)
	r2 := Produce(in, auth())
	if !r2.OK() {
		t.Fatalf("new-head production must succeed on its own: %q", r2.Failure)
	}
	if f := Publish(*r2.Binding, path); f != FailContradictoryPublication {
		t.Fatalf("a contradictory existing publication must fail closed, got %q", f)
	}
}

// 45: the audit carries no secret/token/raw-event data (only typed identity + stage fields).
func TestAudit_NoSecrets(t *testing.T) {
	r := Produce(validInput(), auth())
	data, _ := yaml.Marshal(r.Audit)
	low := strings.ToLower(string(data))
	for _, banned := range []string{"token", "authorization", "secret", "password", "bearer", "ghp_", "event_payload"} {
		if strings.Contains(low, banned) {
			t.Fatalf("audit must contain no secret material; found %q", banned)
		}
	}
}

// 48: deterministic under repetition (race is exercised by `go test -race`).
func TestProduce_RepeatDeterministic(t *testing.T) {
	first := Produce(validInput(), auth())
	for i := 0; i < 50; i++ {
		r := Produce(validInput(), auth())
		if *r.Binding != *first.Binding || r.Failure != first.Failure {
			t.Fatal("production must be deterministic")
		}
	}
}

func mustRead(t *testing.T, p string) []byte {
	t.Helper()
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
