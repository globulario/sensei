// SPDX-License-Identifier: AGPL-3.0-only

package tasksession

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// writeGenerationPointerRaw writes control/latest-generation.yaml with the
// generation path exactly as given, including a traversal one. The existing
// writeLatestGenerationPointer helper derives the generation from the digest
// and so cannot express a pointer whose generation and digest disagree about
// where the generation lives -- which is the whole shape under test here.
func writeGenerationPointerRaw(t *testing.T, taskDir, generation, digest string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(taskDir, "control"), 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := yaml.Marshal(controlGenerationPointerEnvelope{TaskControlGeneration: controlGenerationPointer{
		SchemaVersion: SchemaVersion,
		Generation:    generation,
		DigestSHA256:  digest,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(taskDir, "control", "latest-generation.yaml"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// A generation pointer names the generation whose admission-decision.yaml is
// authoritative, and whose inspection capability and read paths are spliced
// into Permission.ExactScope. A basename equal to the recorded digest is a
// real property but not a sufficient one: filepath.Join cleans the ".."
// segments away, so "../../../../elsewhere/<hex>" keeps the matching basename
// while the resolved directory has left the task directory entirely. A task
// must not be able to name an authority that lives outside itself.
func TestCurrentControlPathsRefusesGenerationOutsideTheTaskDirectory(t *testing.T) {
	taskDir := t.TempDir()
	digest := strings.Repeat("b", 64)
	writeGenerationPointerRaw(t, taskDir, "../../../../elsewhere/"+digest, digest)

	paths, gotDigest, err := currentControlPaths(taskDir)
	if err == nil {
		escaped := filepath.Dir(filepath.Dir(filepath.Dir(paths.Claims)))
		back, relErr := filepath.Rel(taskDir, escaped)
		t.Fatalf("escaping generation accepted: digest %q resolved to %q, which is %q relative to task directory %q (rel err %v)",
			gotDigest, escaped, back, taskDir, relErr)
	}
	if err.Error() != ReasonIncompleteGeneration {
		t.Fatalf("refusal reason = %q, want %q", err.Error(), ReasonIncompleteGeneration)
	}
}

// The containment requirement must not refuse the ordinary case: a pointer
// at generations/<digest> inside the task directory still resolves, and the
// resolved paths still hang off that generation.
func TestCurrentControlPathsResolvesAnInDirectoryGeneration(t *testing.T) {
	taskDir := t.TempDir()
	digest := strings.Repeat("c", 64)
	if err := os.MkdirAll(filepath.Join(taskDir, "control", "generations", digest), 0o755); err != nil {
		t.Fatal(err)
	}
	writeGenerationPointerRaw(t, taskDir, filepath.ToSlash(filepath.Join("generations", digest)), digest)

	paths, gotDigest, err := currentControlPaths(taskDir)
	if err != nil {
		t.Fatalf("legitimate in-directory generation refused: %v", err)
	}
	if gotDigest != digest {
		t.Fatalf("digest = %q, want %q", gotDigest, digest)
	}
	want := filepath.Join(taskDir, "control", "generations", digest, "evidence-state.yaml")
	if paths.Evidence != want {
		t.Fatalf("evidence path = %q, want %q", paths.Evidence, want)
	}
}
