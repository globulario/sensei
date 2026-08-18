// SPDX-License-Identifier: AGPL-3.0-only

package tasksession

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Before a generation exists, the prepare-time request IS the current one.
func TestResolveCurrentAdmissionRequestPathFallsBackToPrepareTime(t *testing.T) {
	taskDir := t.TempDir()
	got, err := ResolveCurrentAdmissionRequestPath(taskDir)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(taskDir, "admission", "request.yaml")
	if got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
}

// Once advance-task publishes a generation, the prepare-time request is no
// longer current: it was computed against a convergence iteration that has
// since advanced. Resolving to it would bind a derived admission scope to an
// iteration no decision would be evaluated against.
func TestResolveCurrentAdmissionRequestPathPrefersTheCurrentGeneration(t *testing.T) {
	taskDir := t.TempDir()
	digest := strings.Repeat("a", 64)
	if err := os.MkdirAll(filepath.Join(taskDir, "control", "generations", digest), 0o755); err != nil {
		t.Fatal(err)
	}
	pointer, err := yaml.Marshal(controlGenerationPointerEnvelope{
		TaskControlGeneration: controlGenerationPointer{
			SchemaVersion: SchemaVersion,
			Generation:    filepath.ToSlash(filepath.Join("generations", digest)),
			DigestSHA256:  digest,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(taskDir, "control", "latest-generation.yaml"), pointer, 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := ResolveCurrentAdmissionRequestPath(taskDir)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(taskDir, "control", "generations", digest, "admission-request.yaml")
	if got != want {
		t.Fatalf("path = %q, want the current generation's own request %q", got, want)
	}
}

// A pointer that does not describe a complete generation is refused rather
// than silently falling back to the prepare-time request, which would answer a
// different question than the caller asked.
func TestResolveCurrentAdmissionRequestPathRefusesAnIncompletePointer(t *testing.T) {
	taskDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(taskDir, "control"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(taskDir, "control", "latest-generation.yaml"),
		[]byte("task_control_generation:\n  schema_version: \""+SchemaVersion+"\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveCurrentAdmissionRequestPath(taskDir); err == nil {
		t.Fatal("an incomplete generation pointer was accepted")
	}
}
