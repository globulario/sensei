// SPDX-License-Identifier: Apache-2.0

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
	w := cloneNotCompleted(t)
	p := w.project(t)
	if p.TerminalState != TerminalNotCompleted || p.ClosureVerdict != ClosureNotCompleted || p.AuthoritativeCompletion {
		t.Fatalf("not-completed projection = %+v", p)
	}
}

// 2 + 10: authoritative completion only from the full conjunction; governed drift
// keeps it authoritative with the flag set.
func TestProjectionAuthoritativeAndDrift(t *testing.T) {
	w := cloneCommitted(t)
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
			w, head := cloneReady(t)
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
	w := cloneCommitted(t)
	deleteProjections(t, w.TaskDir)
	p := w.project(t)
	if !p.AuthoritativeCompletion || p.ClosureVerdict != ClosureAuthoritativeCompletion {
		t.Fatalf("projection loss erased authoritative completion: %+v", p)
	}
}

// 11: tampering any bound owner breaks the closure verdict surfaced by the projection.
func TestProjectionTamperingBreaks(t *testing.T) {
	w := cloneCommitted(t)
	tamperCurrentCorrectness(t, w.TaskDir)
	p := w.project(t)
	if p.AuthoritativeCompletion || p.ClosureVerdict == ClosureAuthoritativeCompletion {
		t.Fatal("tampering must break the surfaced verdict")
	}
}

// 12: unchanged evidence yields a byte-identical projection digest.
func TestProjectionDeterministic(t *testing.T) {
	w := cloneCommitted(t)
	if w.project(t).DigestSHA256 != w.project(t).DigestSHA256 {
		t.Fatal("projection is not deterministic")
	}
}

// 13: building the projection performs zero mutation.
func TestProjectionZeroMutation(t *testing.T) {
	w := cloneCommitted(t)
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
	w := cloneCommitted(t)
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
	w2 := cloneNotCompleted(t)
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

	// A dedicated unavailable constructor fabricates no terminal state and is
	// deterministic and valid.
	u1 := UnavailableTaskDirectoryEnvelope("no active task pointer")
	u2 := UnavailableTaskDirectoryEnvelope("no active task pointer")
	if u1.Availability != CompletionUnavailable || u1.Projection != nil || !u1.NonAuthoritativeProjection {
		t.Fatalf("unavailable envelope malformed: %+v", u1)
	}
	if u1.DigestSHA256 == "" || u1.DigestSHA256 != u2.DigestSHA256 {
		t.Fatal("unavailable envelope must be stamped and deterministic")
	}
	if env.DigestSHA256 == u1.DigestSHA256 {
		t.Fatal("available and unavailable must have distinct identities")
	}
	if err := ValidateCompletionEnvelope(u1); err != nil {
		t.Fatalf("dedicated constructor produced an invalid envelope: %v", err)
	}
}

// Correction: validation GOVERNS construction and rendering — an arbitrary class or a
// malformed conjunction cannot be stamped or rendered as canonical.
func TestCompletionEnvelopeValidationEnforced(t *testing.T) {
	w := cloneCommitted(t)
	// The available builder and both dedicated unavailable constructors are valid,
	// stamped, and deterministic.
	available := BuildCompletionProjectionEnvelope(context.Background(), Request{RepositoryRoot: w.Repo, TaskDirectory: w.TaskDir})
	for _, e := range []CompletionProjectionEnvelope{available, UnavailableTaskDirectoryEnvelope("x"), UnavailableProjectionOwnerEnvelope("y")} {
		if err := ValidateCompletionEnvelope(e); err != nil {
			t.Fatalf("constructor produced invalid envelope: %v", err)
		}
		if e.DigestSHA256 == "" {
			t.Fatal("valid constructor output must be stamped")
		}
	}

	// An explicitly cast arbitrary class cannot be stamped (no digest) and cannot
	// render as canonical (Summary reports it invalid).
	arbitrary := stampEnvelope(CompletionProjectionEnvelope{Availability: CompletionUnavailable, UnavailableClass: CompletionUnavailableClass("owner_failure"), UnavailableDetail: "synonym"})
	if arbitrary.DigestSHA256 != "" {
		t.Fatal("an arbitrary unavailable class must never be stamped")
	}
	if ValidateCompletionEnvelope(arbitrary) == nil {
		t.Fatal("an arbitrary unavailable class must be rejected")
	}
	if !strings.Contains(arbitrary.Summary(), "invalid completion projection envelope") {
		t.Fatalf("arbitrary class must render as invalid, got %q", arbitrary.Summary())
	}

	// Malformed available/unavailable conjunctions are never stamped and render as
	// invalid — in particular `available` with a nil projection is NOT reinterpreted as
	// unavailable.
	malformed := []CompletionProjectionEnvelope{
		{Availability: CompletionAvailable, Projection: nil},
		{Availability: CompletionAvailable, Projection: available.Projection, UnavailableClass: UnavailableProjectionOwnerError},
		{Availability: CompletionUnavailable, Projection: available.Projection, UnavailableClass: UnavailableProjectionOwnerError},
		{Availability: CompletionUnavailable, UnavailableClass: ""},
		{Availability: "maybe"},
	}
	for i, m := range malformed {
		stamped := stampEnvelope(m)
		if stamped.DigestSHA256 != "" {
			t.Fatalf("malformed envelope %d must never be stamped", i)
		}
		if ValidateCompletionEnvelope(stamped) == nil {
			t.Fatalf("malformed envelope %d must be rejected", i)
		}
		if !strings.Contains(stamped.Summary(), "invalid completion projection envelope") {
			t.Fatalf("malformed envelope %d must render as invalid, got %q", i, stamped.Summary())
		}
	}
	// The available-with-nil case must not be mapped to the unavailable rendering.
	availNil := CompletionProjectionEnvelope{Availability: CompletionAvailable}
	if strings.Contains(availNil.Summary(), "unavailable (") {
		t.Fatalf("available+nil must not render as unavailable: %q", availNil.Summary())
	}
}

// Correction: canonical PUBLICATION validity is re-verified at the render/publish
// boundary, not only at construction. Closing the constructor door stops invalid
// construction, but a stamped envelope can be altered between the workshop and the
// display case: its fields keep satisfying the structural conjunction while its digest
// no longer represents its content. Publication must reject it — detectable invalidity
// is not enforced validity.
func TestCompletionEnvelopeCanonicalPublication(t *testing.T) {
	w := cloneCommitted(t)
	available := BuildCompletionProjectionEnvelope(context.Background(), Request{RepositoryRoot: w.Repo, TaskDirectory: w.TaskDir})

	// Freshly stamped envelopes are canonically valid and publish as a canonical union
	// carrying the verbatim envelope under the stable publication schema.
	for _, e := range []CompletionProjectionEnvelope{available, UnavailableProjectionOwnerEnvelope("y"), UnavailableTaskDirectoryEnvelope("z")} {
		if err := ValidateCanonicalCompletionEnvelope(e); err != nil {
			t.Fatalf("stamped envelope must be canonically valid: %v", err)
		}
		pub := e.PublicationView()
		if pub.SchemaVersion != "completion.projection_publication/v1" {
			t.Fatalf("publication schema = %q, want the stable union schema", pub.SchemaVersion)
		}
		if !pub.Canonical || pub.Envelope == nil || pub.InvalidClass != "" {
			t.Fatalf("a canonical envelope must publish verbatim under canonical:true, got %#v", pub)
		}
		if err := ValidateCompletionPublication(pub); err != nil {
			t.Fatalf("canonical publication must re-validate: %v", err)
		}
	}

	// Mutate an unavailable envelope's detail AFTER stamping. Structure still passes; the
	// digest no longer matches, so canonical publication fails and both publish surfaces
	// report invalid rather than rendering the changed content.
	tampered := UnavailableProjectionOwnerEnvelope("original")
	tampered.UnavailableDetail = "changed after stamping"
	if err := ValidateCompletionEnvelope(tampered); err != nil {
		t.Fatalf("structural validation should still pass on a field-consistent mutation: %v", err)
	}
	if ValidateCanonicalCompletionEnvelope(tampered) == nil {
		t.Fatal("post-stamp mutation must fail canonical publication validation")
	}
	if !strings.Contains(tampered.Summary(), "invalid completion projection envelope") {
		t.Fatalf("tampered envelope must render invalid, got %q", tampered.Summary())
	}
	if strings.Contains(tampered.Summary(), "changed after stamping") {
		t.Fatalf("tampered content must not be rendered: %q", tampered.Summary())
	}
	// The tampered envelope publishes as the SAME typed union under the SAME schema — the
	// non-canonical path — with a recognized class and no envelope, not a foreign shape
	// mislabeled with the envelope schema.
	tpub := tampered.PublicationView()
	if tpub.SchemaVersion != "completion.projection_publication/v1" {
		t.Fatalf("both paths must share the publication schema, got %q", tpub.SchemaVersion)
	}
	if tpub.Canonical || tpub.Envelope != nil {
		t.Fatalf("tampered envelope must publish as canonical:false with no envelope, got %#v", tpub)
	}
	if tpub.InvalidClass != PublicationInvalidDigestMismatch {
		t.Fatalf("tampered envelope class = %q, want digest_mismatch", tpub.InvalidClass)
	}
	if err := ValidateCompletionPublication(tpub); err != nil {
		t.Fatalf("non-canonical publication must re-validate as a coherent union: %v", err)
	}

	// Mutate the nested available projection AFTER stamping — the envelope digest covers
	// it, so canonical publication catches this too.
	tam2 := BuildCompletionProjectionEnvelope(context.Background(), Request{RepositoryRoot: w.Repo, TaskDirectory: w.TaskDir})
	tam2.Projection.Detail = tam2.Projection.Detail + " forged"
	if ValidateCanonicalCompletionEnvelope(tam2) == nil {
		t.Fatal("mutating the nested projection must fail canonical publication validation")
	}

	// One layer below the envelope: an internally consistent but IMPOSSIBLE projection —
	// verdict=not_completed with authoritative_completion=true — re-digested at BOTH the
	// projection and envelope layers so every digest verifies, must still be rejected
	// because the nested projection violates its own canonical contract.
	impossible := BuildCompletionProjectionEnvelope(context.Background(), Request{RepositoryRoot: w.Repo, TaskDirectory: w.TaskDir})
	impossible.Projection.ClosureVerdict = ClosureNotCompleted
	impossible.Projection.AuthoritativeCompletion = true
	pd, perr := recomputeProjectionDigest(*impossible.Projection)
	if perr != nil {
		t.Fatalf("recompute projection digest: %v", perr)
	}
	impossible.Projection.DigestSHA256 = pd
	ed, eerr := recomputeEnvelopeDigest(impossible)
	if eerr != nil {
		t.Fatalf("recompute envelope digest: %v", eerr)
	}
	impossible.DigestSHA256 = ed
	// The envelope digest verifies, but the projection contract does not.
	if perr := ValidateCanonicalCompletionProjection(*impossible.Projection); perr == nil {
		t.Fatal("a projection with authoritative=true but verdict=not_completed must fail its contract")
	}
	if ValidateCanonicalCompletionEnvelope(impossible) == nil {
		t.Fatal("an available envelope wrapping a contradictory projection must fail canonical publication")
	}
	ipub := impossible.PublicationView()
	if ipub.Canonical || ipub.InvalidClass != PublicationInvalidProjection {
		t.Fatalf("impossible nested projection must publish canonical:false class=projection, got %#v", ipub)
	}
	if err := ValidateCompletionPublication(ipub); err != nil {
		t.Fatalf("the non-canonical publication must re-validate as a coherent union: %v", err)
	}

	// An off-schema version and a stripped digest each fail canonical publication while
	// leaving the structural conjunction intact.
	badSchema := UnavailableProjectionOwnerEnvelope("s")
	badSchema.SchemaVersion = "completion.projection_envelope/v2"
	if ValidateCompletionEnvelope(badSchema) != nil || ValidateCanonicalCompletionEnvelope(badSchema) == nil {
		t.Fatal("off-schema envelope must pass structure but fail canonical publication")
	}
	if badSchema.PublicationView().InvalidClass != PublicationInvalidSchema {
		t.Fatalf("off-schema class = %q, want schema", badSchema.PublicationView().InvalidClass)
	}
	noDigest := UnavailableProjectionOwnerEnvelope("d")
	noDigest.DigestSHA256 = ""
	if ValidateCompletionEnvelope(noDigest) != nil || ValidateCanonicalCompletionEnvelope(noDigest) == nil {
		t.Fatal("digest-less envelope must pass structure but fail canonical publication")
	}
	if noDigest.PublicationView().InvalidClass != PublicationInvalidDigestMalformed {
		t.Fatalf("digest-less class = %q, want digest_malformed", noDigest.PublicationView().InvalidClass)
	}
}

// 15: the projection explicitly disclaims terminal authority and repository-wide
// perfection.
func TestProjectionDisclaimsAuthority(t *testing.T) {
	w := cloneCommitted(t)
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
