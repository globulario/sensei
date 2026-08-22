// SPDX-License-Identifier: AGPL-3.0-only

package whyinvestigation

import (
	"context"
	"testing"

	"github.com/globulario/sensei/golang/architecture/investigation"
	"github.com/globulario/sensei/golang/architecture/modelexec"
)

// countingProvider is hermetic: no network, no credentials, no vendor, and it
// counts invocations so "zero calls" is an assertion rather than an assumption.
type countingProvider struct{ calls int }

func (p *countingProvider) Identity() modelexec.ProviderIdentity {
	return modelexec.ProviderIdentity{ID: "fake", Version: "v1"}
}

func (p *countingProvider) Execute(_ context.Context, req modelexec.Request) (modelexec.Artifact, error) {
	p.calls++
	return modelexec.Artifact{
		SchemaVersion:             modelexec.ArtifactSchemaVersion,
		NondeterminismDeclaration: "model_response_not_replayable",
		Items: []modelexec.ArtifactItem{{
			Kind: modelexec.ItemKindQuestion,
			Text: "why does this boundary exist?",
		}},
	}, nil
}

// whyFixture is a deterministic WHY run: a real git fixture and a fixed plan,
// so the composed document digest is a stable thing to compare against.
func whyFixture(t *testing.T) (string, CaptureRequest, Plan) {
	t.Helper()
	root, req, first, second := gitFixture(t)
	req.Range = GitRange{Start: first + "~1", End: second}
	return root, req, Plan{
		ID:                   "plan.why.model_lane.v1",
		Description:          "deterministic WHY plan used to prove the model lane changes nothing",
		RequestedProviderIDs: []string{"git_history_provider"},
	}
}

// TestDeterministicWhyIsUnchangedByInstallingModelCapability is #256's central
// regression rule.
//
// Adding a model lane must not perturb the deterministic result merely because
// model-capable code is now installed. The comparison is on the composed
// document's own digest, so provider ordering, evidence capture and normalized
// facts are all covered rather than spot-checked.
func TestDeterministicWhyIsUnchangedByInstallingModelCapability(t *testing.T) {
	root, req, plan := whyFixture(t)

	viaDeterministicEntry, err := Orchestrate(context.Background(), root, req, plan)
	if err != nil {
		t.Fatalf("deterministic orchestrate: %v", err)
	}

	// The model-capable entry point, with a provider REGISTERED but the lane
	// off, must produce the same document — not merely an equivalent one.
	// Registering the provider is the point: it proves that having the
	// capability installed changes nothing until it is engaged.
	p := &countingProvider{}
	viaModelEntry, outcome, err := OrchestrateWithModel(context.Background(), root, req, plan, ModelLane{
		Config:   modelexec.Config{Disabled: true},
		Registry: modelexec.Registry{"fake": p},
	})
	if err != nil {
		t.Fatalf("model-capable orchestrate: %v", err)
	}
	if p.calls != 0 {
		t.Errorf("provider called %d time(s) with the lane disabled", p.calls)
	}
	if outcome.Binding.Status != investigation.ModelStatusDisabled {
		t.Errorf("status = %q, want %q", outcome.Binding.Status, investigation.ModelStatusDisabled)
	}

	// not_requested is a DIFFERENT fact from disabled, and must not be
	// flattened into it: one says the capability is off, the other says nobody
	// asked. Their documents differ because their records differ.
	_, notRequested, err := OrchestrateWithModel(context.Background(), root, req, plan, ModelLane{})
	if err != nil {
		t.Fatal(err)
	}
	if notRequested.Binding.Status != investigation.ModelStatusNotRequested {
		t.Errorf("status = %q, want %q", notRequested.Binding.Status, investigation.ModelStatusNotRequested)
	}

	a := viaDeterministicEntry.Receipt.OutputDocumentDigestSHA256
	b := viaModelEntry.Receipt.OutputDocumentDigestSHA256
	if a == "" {
		t.Fatal("deterministic document carries no digest to compare")
	}
	if a != b {
		t.Errorf("installing model capability changed the deterministic document: %s != %s", a, b)
	}
	if viaModelEntry.Receipt.NondeterminismDeclaration != "deterministic_only" {
		t.Errorf("a run with no model declared %q; it is still deterministic",
			viaModelEntry.Receipt.NondeterminismDeclaration)
	}
}

// TestDisabledModelLaneCallsNothingAndStaysDeterministic covers the other
// zero-call row of the proof matrix at the orchestration seam.
func TestDisabledModelLaneCallsNothingAndStaysDeterministic(t *testing.T) {
	root, req, plan := whyFixture(t)
	baseline, err := Orchestrate(context.Background(), root, req, plan)
	if err != nil {
		t.Fatal(err)
	}

	p := &countingProvider{}
	doc, outcome, err := OrchestrateWithModel(context.Background(), root, req, plan, ModelLane{
		Config:   modelexec.Config{Disabled: true, Requested: true, ProviderID: "fake", ModelName: "m"},
		Registry: modelexec.Registry{"fake": p},
	})
	if err != nil {
		t.Fatal(err)
	}
	if p.calls != 0 {
		t.Errorf("provider called %d time(s) while disabled", p.calls)
	}
	if outcome.Binding.Status != investigation.ModelStatusDisabled {
		t.Errorf("status = %q, want %q", outcome.Binding.Status, investigation.ModelStatusDisabled)
	}
	if doc.Receipt.OutputDocumentDigestSHA256 != baseline.Receipt.OutputDocumentDigestSHA256 {
		t.Error("a disabled model lane changed the deterministic document")
	}
}

// TestModelAssistedRunRecordsItsOwnNondeterminism: once a model has genuinely
// run, the receipt must stop claiming deterministic replay it cannot deliver.
func TestModelAssistedRunRecordsItsOwnNondeterminism(t *testing.T) {
	root, req, plan := whyFixture(t)
	p := &countingProvider{}
	doc, outcome, err := OrchestrateWithModel(context.Background(), root, req, plan, ModelLane{
		Config:   modelexec.Config{Requested: true, ProviderID: "fake", ModelName: "fake-model"},
		Registry: modelexec.Registry{"fake": p},
		Request: modelexec.Request{
			Model: modelexec.ModelIdentity{Name: "fake-model", DigestAbsent: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if p.calls != 1 {
		t.Fatalf("provider called %d time(s), want exactly 1", p.calls)
	}
	if outcome.Binding.Status != investigation.ModelStatusResolved {
		t.Fatalf("status = %q (reason %q), want %q", outcome.Binding.Status, outcome.Binding.Reason, investigation.ModelStatusResolved)
	}
	if doc.Receipt.NondeterminismDeclaration == "deterministic_only" {
		t.Error("a model-assisted run still claimed deterministic_only")
	}
	// Binding and receipt must tell one story.
	if errs := investigation.ValidateModelExecutionAgreement(doc.Binding.Model, doc.Receipt); len(errs) != 0 {
		t.Errorf("binding and receipt disagree about the same execution: %v", errs)
	}
	// A provider with no model digest says so, rather than leaving it empty.
	if doc.Binding.Model.ModelDigestAbsence != investigation.ModelDigestAbsent {
		t.Errorf("model digest absence = %q, want the typed absence", doc.Binding.Model.ModelDigestAbsence)
	}
}
