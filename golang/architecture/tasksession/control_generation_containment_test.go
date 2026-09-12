// SPDX-License-Identifier: AGPL-3.0-only

package tasksession

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// writeGenerationPointerRaw writes control/latest-generation.yaml with an
// arbitrary generation string, unlike writeLatestGenerationPointer which
// always composes a well-formed "generations/<digest>". The escape this
// covers lives precisely in the generation string, so the test must be
// able to write one production code would never publish.
func writeGenerationPointerRaw(t *testing.T, taskDir, generation, digest string) {
	t.Helper()
	data, err := yaml.Marshal(controlGenerationPointerEnvelope{TaskControlGeneration: controlGenerationPointer{
		SchemaVersion: SchemaVersion,
		Generation:    generation,
		DigestSHA256:  digest,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(taskDir, "control"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(taskDir, "control", "latest-generation.yaml"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

const containmentTestDigest = "0f2c1a7b3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8"

// TestCurrentControlPathsRefusesGenerationOutsideTaskDirectory is the
// witness for the escape: filepath.Join cleans the ".." segments away, so
// a generation of "../../../../elsewhere/<hex>" still has a basename equal
// to the recorded digest while the resolved generation root has left the
// task directory entirely. The resolved generation decides which
// admission-decision.yaml is authoritative and what is spliced into
// Permission.ExactScope, so accepting one from outside the task directory
// hands authority to a directory the task does not own.
func TestCurrentControlPathsRefusesGenerationOutsideTaskDirectory(t *testing.T) {
	taskDir := filepath.Join(t.TempDir(), "task")
	writeGenerationPointerRaw(t, taskDir,
		filepath.ToSlash(filepath.Join("..", "..", "..", "..", "elsewhere", containmentTestDigest)),
		containmentTestDigest)

	paths, _, err := currentControlPaths(taskDir)
	if err == nil {
		t.Fatalf("currentControlPaths accepted a generation outside the task directory; it resolved claims to %s (task directory is %s)",
			paths.Claims, taskDir)
	}
	if !strings.Contains(err.Error(), ReasonIncompleteGeneration) {
		t.Fatalf("expected the refusal to carry %s, got %v", ReasonIncompleteGeneration, err)
	}
}

// TestCurrentControlPathsResolvesGenerationInsideTaskDirectory guards the
// other direction: the containment check must not refuse the ordinary
// pointer advance-task publishes.
func TestCurrentControlPathsResolvesGenerationInsideTaskDirectory(t *testing.T) {
	taskDir := filepath.Join(t.TempDir(), "task")
	writeGenerationPointerRaw(t, taskDir,
		filepath.ToSlash(filepath.Join("generations", containmentTestDigest)),
		containmentTestDigest)

	paths, digest, err := currentControlPaths(taskDir)
	if err != nil {
		t.Fatalf("currentControlPaths refused a legitimate in-directory generation: %v", err)
	}
	if digest != containmentTestDigest {
		t.Fatalf("digest = %q, want %q", digest, containmentTestDigest)
	}
	wantRoot := filepath.Join(taskDir, "control", "generations", containmentTestDigest)
	if !strings.HasPrefix(paths.Claims, wantRoot+string(filepath.Separator)) {
		t.Fatalf("claims path %q is not under the generation root %q", paths.Claims, wantRoot)
	}
}
