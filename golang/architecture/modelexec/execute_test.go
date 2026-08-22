// SPDX-License-Identifier: AGPL-3.0-only

package modelexec

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture/investigation"
)

// fakeProvider is the hermetic stand-in the #256 contract requires: no network,
// no credentials, no vendor. It counts invocations, because several rows of the
// proof matrix are assertions about how many times the provider was CALLED, not
// about what it returned.
type fakeProvider struct {
	id       string
	version  string
	calls    int
	artifact Artifact
	err      error
}

func (f *fakeProvider) Identity() ProviderIdentity {
	return ProviderIdentity{ID: f.id, Version: f.version}
}

func (f *fakeProvider) Execute(_ context.Context, _ Request) (Artifact, error) {
	f.calls++
	return f.artifact, f.err
}

const sha = "4a8e63db7cc5173b82bd3ba6019d30ce9e22db84d852bd3ba6019d30ce922db8"

func testRequest() Request {
	return Request{
		SchemaVersion:      ArtifactSchemaVersion,
		RepositoryDomain:   "github.com/example/repo",
		RepositoryRevision: sha,
		SuppliedEvidence: []SuppliedEvidence{
			{ID: "ev-1", DigestSHA256: sha, Excerpt: "func A() {}"},
			{ID: "ev-2", DigestSHA256: sha, Excerpt: "func B() {}"},
		},
		Model: ModelIdentity{Name: "fake-model", DigestSHA256: sha},
	}
}

func goodArtifact() Artifact {
	return Artifact{
		SchemaVersion:             ArtifactSchemaVersion,
		NondeterminismDeclaration: "model_response_not_replayable",
		Items: []ArtifactItem{{
			Kind:             ItemKindCandidateClaim,
			Text:             "A calls B",
			CitedEvidenceIDs: []string{"ev-1"},
		}},
	}
}

func registryWith(p *fakeProvider) Registry { return Registry{p.id: p} }

func requested(model string) Config {
	return Config{Requested: true, ProviderID: "fake", ModelName: model}
}

// --- proof matrix -----------------------------------------------------------

func TestModelDisabledCallsNothing(t *testing.T) {
	p := &fakeProvider{id: "fake", version: "v1", artifact: goodArtifact()}
	out := Execute(context.Background(), Config{Disabled: true, Requested: true, ProviderID: "fake", ModelName: "m"}, registryWith(p), testRequest())
	assertOutcome(t, out, p, investigation.ModelStatusDisabled, 0)
}

func TestModelNotRequestedCallsNothing(t *testing.T) {
	p := &fakeProvider{id: "fake", version: "v1", artifact: goodArtifact()}
	out := Execute(context.Background(), Config{}, registryWith(p), testRequest())
	assertOutcome(t, out, p, investigation.ModelStatusNotRequested, 0)
}

// An unresolvable provider fails BEFORE invocation, so it is unavailable and
// never errored: reporting an outage we never observed would be a lie about
// where the failure was.
func TestUnknownProviderIsUnavailableAndNeverInvoked(t *testing.T) {
	p := &fakeProvider{id: "fake", version: "v1", artifact: goodArtifact()}
	out := Execute(context.Background(), Config{Requested: true, ProviderID: "nobody", ModelName: "m"}, registryWith(p), testRequest())
	assertOutcome(t, out, p, investigation.ModelStatusUnavailable, 0)
	if out.Binding.Reason != investigation.ModelReasonProviderUnknown {
		t.Errorf("reason = %q, want %q", out.Binding.Reason, investigation.ModelReasonProviderUnknown)
	}
}

// A refusal is an ANSWER. Collapsing it into errored would erase the difference
// between a provider that declined and one that broke.
func TestProviderRefusalIsRefusedNotErrored(t *testing.T) {
	p := &fakeProvider{id: "fake", version: "v1", err: &Refusal{Reason: "policy"}}
	out := Execute(context.Background(), requested("m"), registryWith(p), testRequest())
	assertOutcome(t, out, p, investigation.ModelStatusRefused, 1)
	if out.Binding.RequestDigestSHA256 == "" {
		t.Error("an invoked provider must record the exact request it was sent")
	}
}

func TestProviderErrorIsErroredNotRefused(t *testing.T) {
	p := &fakeProvider{id: "fake", version: "v1", err: errors.New("transport exploded")}
	out := Execute(context.Background(), requested("m"), registryWith(p), testRequest())
	assertOutcome(t, out, p, investigation.ModelStatusErrored, 1)
}

func TestMalformedArtifactIsInvalid(t *testing.T) {
	bad := goodArtifact()
	bad.SchemaVersion = "something-else"
	p := &fakeProvider{id: "fake", version: "v1", artifact: bad}
	out := Execute(context.Background(), requested("m"), registryWith(p), testRequest())
	assertOutcome(t, out, p, investigation.ModelStatusInvalid, 1)
	if out.Binding.ArtifactDigestSHA256 != "" {
		t.Error("a rejected artifact was given an accepted-artifact identity")
	}
}

// Grounding is decidable because the request is the ONLY material the model was
// given: a citation outside it refers to something the model never received.
func TestArtifactCitingUnsuppliedEvidenceIsInvalid(t *testing.T) {
	bad := goodArtifact()
	bad.Items[0].CitedEvidenceIDs = []string{"ev-never-supplied"}
	p := &fakeProvider{id: "fake", version: "v1", artifact: bad}
	out := Execute(context.Background(), requested("m"), registryWith(p), testRequest())
	assertOutcome(t, out, p, investigation.ModelStatusInvalid, 1)
	if out.Binding.Reason != investigation.ModelReasonArtifactUngrounded {
		t.Errorf("reason = %q, want %q", out.Binding.Reason, investigation.ModelReasonArtifactUngrounded)
	}
	if out.Artifact != nil {
		t.Error("an ungrounded artifact reached composition")
	}
}

// A bid for authority is REFUSED and recorded, not silently stripped: stripping
// would let a model keep asking with no evidence that it ever did.
func TestArtifactClaimingAuthorityIsRejected(t *testing.T) {
	for _, tc := range []struct {
		name  string
		apply func(*ArtifactItem)
	}{
		{"canonical", func(i *ArtifactItem) { i.ClaimsCanonical = true }},
		{"promotion", func(i *ArtifactItem) { i.ClaimsPromotion = true }},
		{"admission", func(i *ArtifactItem) { i.ClaimsAdmission = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bad := goodArtifact()
			tc.apply(&bad.Items[0])
			p := &fakeProvider{id: "fake", version: "v1", artifact: bad}
			out := Execute(context.Background(), requested("m"), registryWith(p), testRequest())
			assertOutcome(t, out, p, investigation.ModelStatusInvalid, 1)
			if out.Binding.Reason != investigation.ModelReasonArtifactAuthority {
				t.Errorf("reason = %q, want %q", out.Binding.Reason, investigation.ModelReasonArtifactAuthority)
			}
			if out.Artifact != nil {
				t.Error("an artifact bidding for authority reached composition")
			}
		})
	}
}

func TestValidArtifactResolvesWithFullIdentity(t *testing.T) {
	p := &fakeProvider{id: "fake", version: "v1", artifact: goodArtifact()}
	out := Execute(context.Background(), requested("fake-model"), registryWith(p), testRequest())
	assertOutcome(t, out, p, investigation.ModelStatusResolved, 1)

	b := out.Binding
	if b.Provider.ID != "fake" || b.Provider.Version != "v1" {
		t.Errorf("provider identity = %+v, want fake/v1", b.Provider)
	}
	if b.RequestDigestSHA256 == "" || b.ArtifactDigestSHA256 == "" {
		t.Error("resolved binding is missing request or artifact identity")
	}
	if b.NondeterminismDeclaration != "model_response_not_replayable" {
		t.Errorf("nondeterminism declaration = %q; it must be the provider's own statement", b.NondeterminismDeclaration)
	}
	if errs := investigation.ValidateModelBinding(b); len(errs) != 0 {
		t.Errorf("a resolved binding this owner produced fails its own contract: %v", errs)
	}
	if out.Artifact == nil {
		t.Error("resolved outcome carries no accepted artifact")
	}

	// The recorded request digest must be the request that was actually sent.
	want, err := RequestDigest(func() Request {
		r := testRequest()
		r.Provider = ProviderIdentity{ID: "fake", Version: "v1"}
		r.Model.Name = "fake-model"
		return r
	}())
	if err != nil {
		t.Fatal(err)
	}
	if b.RequestDigestSHA256 != want {
		t.Error("the recorded request digest is not the digest of the request that was sent")
	}
}

// Configuration cannot manufacture success. Config has no status field at all,
// so the strongest form of this proof is structural — and the outcome for a
// caller who asks hardest for success is still driven by what happened.
func TestCallerCannotPredeclareResolved(t *testing.T) {
	p := &fakeProvider{id: "fake", version: "v1", err: &Refusal{Reason: "no"}}
	out := Execute(context.Background(), requested("fake-model"), registryWith(p), testRequest())
	if out.Binding.Status == investigation.ModelStatusResolved {
		t.Fatal("a caller obtained resolved from a provider that refused")
	}
	if out.Binding.Status != investigation.ModelStatusRefused {
		t.Errorf("status = %q, want %q", out.Binding.Status, investigation.ModelStatusRefused)
	}
	// And a hand-built "resolved" that never ran cannot pass the contract.
	forged := investigation.ModelBinding{Status: investigation.ModelStatusResolved, ModelName: "fake-model"}
	if errs := investigation.ValidateModelBinding(forged); len(errs) == 0 {
		t.Error("a hand-declared resolved binding passed validation")
	}
}

// Determinism of the identity envelope: the same question hashes the same, and
// a changed question does not.
func TestRequestDigestIsStableAndSensitive(t *testing.T) {
	a, err := RequestDigest(testRequest())
	if err != nil {
		t.Fatal(err)
	}
	b, err := RequestDigest(testRequest())
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatal("the same request hashed differently")
	}
	changed := testRequest()
	changed.SuppliedEvidence[0].DigestSHA256 = strings.Repeat("b", 64)
	c, err := RequestDigest(changed)
	if err != nil {
		t.Fatal(err)
	}
	if c == a {
		t.Error("changing supplied evidence did not change the request identity")
	}
}

func assertOutcome(t *testing.T, out Outcome, p *fakeProvider, wantStatus string, wantCalls int) {
	t.Helper()
	if out.Binding.Status != wantStatus {
		t.Fatalf("status = %q (reason %q), want %q", out.Binding.Status, out.Binding.Reason, wantStatus)
	}
	if p.calls != wantCalls {
		t.Errorf("provider called %d time(s), want %d", p.calls, wantCalls)
	}
	if out.ProviderCalls != wantCalls {
		t.Errorf("outcome reports %d call(s), want %d", out.ProviderCalls, wantCalls)
	}
	if wantStatus != investigation.ModelStatusResolved && out.Binding.ArtifactDigestSHA256 != "" {
		t.Error("a non-resolved outcome carries an accepted artifact identity")
	}
	if errs := investigation.ValidateModelBinding(out.Binding); len(errs) != 0 {
		t.Errorf("outcome binding fails the serialized contract: %v", errs)
	}
}
