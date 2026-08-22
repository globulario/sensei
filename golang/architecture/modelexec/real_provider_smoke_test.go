// SPDX-License-Identifier: AGPL-3.0-only

package modelexec

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/globulario/sensei/golang/architecture/investigation"
)

// TestRealProviderSmoke invokes an ACTUAL model through the command adapter.
//
// It is supplementary and skips unless SENSEI_MODEL_BRIDGE names an executable
// and a credential is present in the environment. CI must never depend on it:
// the hermetic fake-provider matrix is the merge proof, and an external service
// must not be able to decide whether Sensei's authority contract holds.
//
// What it proves is narrow and worth stating: that the capability crosses a
// real process and network boundary into a real model, and that what comes back
// still has to earn resolved through the same validation everything else does.
// It proves reachability, not model correctness — correctness belongs to #131.
//
// Nothing secret is asserted on, printed, or returned. The evidence this test
// produces is a status and two digests.
func TestRealProviderSmoke(t *testing.T) {
	bridge := os.Getenv("SENSEI_MODEL_BRIDGE")
	if bridge == "" {
		t.Skip("SENSEI_MODEL_BRIDGE not set; the real-provider smoke is supplementary and never required in CI")
	}
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		t.Skip("no credential in the environment; skipping the supplementary real-provider smoke")
	}
	abs, err := filepath.Abs(bridge)
	if err != nil {
		t.Fatal(err)
	}

	provider := &CommandProvider{
		ProviderID:      "anthropic-bridge",
		ProviderVersion: "example-v1",
		Path:            abs,
	}
	req := Request{
		SchemaVersion:    ArtifactSchemaVersion,
		RepositoryDomain: "github.com/globulario/sensei",
		SuppliedEvidence: []SuppliedEvidence{{
			ID:           "ev-modelexec-execute",
			DigestSHA256: sha,
			FilePath:     "golang/architecture/modelexec/execute.go",
			Excerpt: "// Execute runs the optional model lane at most once and returns the terminal\n" +
				"// binding built from observed outcome.\n" +
				"func Execute(ctx context.Context, cfg Config, reg Registry, req Request) Outcome {\n" +
				"\tif cfg.Disabled {\n\t\treturn Outcome{Binding: investigation.DisabledModelBinding()}\n\t}\n}",
		}},
		TargetObservationIDs: []string{"obs.modelexec.execute"},
		Model:                ModelIdentity{Name: "claude-opus-5", DigestAbsent: true},
		PromptContractDigest: "examples/model-bridge/anthropic-bridge.sh@v1",
		OutputSchemaVersion:  ArtifactSchemaVersion,
		ToolPolicy:           "none",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	out := Execute(ctx, Config{Requested: true, ProviderID: "anthropic-bridge", ModelName: "claude-opus-5"},
		Registry{"anthropic-bridge": provider}, req)

	// Every terminal state is a legitimate smoke result except a resolved one
	// that fails its own contract. A refusal or an error still proves the
	// boundary was crossed and typed correctly.
	t.Logf("real-provider smoke: status=%s reason=%s provider=%s/%s request=%s artifact=%s",
		out.Binding.Status, out.Binding.Reason, out.Binding.ProviderID, out.Binding.ProviderVersion,
		short(out.Binding.RequestDigestSHA256), short(out.Binding.ArtifactDigestSHA256))

	if errs := investigation.ValidateModelBinding(out.Binding); len(errs) != 0 {
		t.Fatalf("a binding produced by a real provider fails the contract: %v", errs)
	}
	if out.Binding.Status == investigation.ModelStatusResolved {
		if out.Artifact == nil || len(out.Artifact.Items) == 0 {
			t.Error("resolved without an accepted artifact")
		}
		if out.ProviderCalls != 1 {
			t.Errorf("provider calls = %d, want exactly 1", out.ProviderCalls)
		}
	}
}

func short(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}
