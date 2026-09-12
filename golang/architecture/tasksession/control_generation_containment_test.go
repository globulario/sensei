package tasksession

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// writeRawGenerationPointer writes a control generation pointer with an exact,
// uninterpreted Generation field, so a test can express a pointer that walks
// out of the task directory. It is deliberately NOT writeLatestGenerationPointer:
// that helper composes "generations/<digest>", which cannot express the
// traversal this test is about.
func writeRawGenerationPointer(t *testing.T, taskDir, generation, digest string) {
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
	if err := writeFileAtomic(filepath.Join(taskDir, "control", "latest-generation.yaml"), data); err != nil {
		t.Fatal(err)
	}
}

// TestCurrentControlPathsRefusesGenerationOutsideTaskDirectory is the witness
// for the escape: filepath.Join cleans the ".." segments, so a generation of
// "../../../../elsewhere/<hex>" resolves to a directory OUTSIDE the task while
// its basename still equals the recorded digest. The basename check alone
// therefore admits it, and the generation it selects decides which
// admission-decision.yaml is authoritative and what is spliced into
// Permission.ExactScope -- so the refusal must come from a containment test,
// not from the name.
func TestCurrentControlPathsRefusesGenerationOutsideTaskDirectory(t *testing.T) {
	taskDir := filepath.Join(t.TempDir(), "repo", "tasks", "task-1")
	const digest = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	writeRawGenerationPointer(t, taskDir, "../../../../elsewhere/"+digest, digest)

	paths, generation, err := currentControlPaths(taskDir)
	if err == nil {
		t.Fatalf("currentControlPaths admitted a generation outside the task directory: generation %q resolved to %s (task directory is %s)",
			generation, filepath.Dir(paths.Evidence), taskDir)
	}
	if err.Error() != ReasonIncompleteGeneration {
		t.Fatalf("refusal reason = %q, want %q", err.Error(), ReasonIncompleteGeneration)
	}
}

// TestCurrentControlPathsAcceptsGenerationInsideTaskDirectory keeps the repair
// honest in the other direction: the containment test must not refuse the
// ordinary pointer advance-task itself writes.
func TestCurrentControlPathsAcceptsGenerationInsideTaskDirectory(t *testing.T) {
	taskDir := filepath.Join(t.TempDir(), "repo", "tasks", "task-1")
	const digest = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	writeRawGenerationPointer(t, taskDir, "generations/"+digest, digest)

	paths, generation, err := currentControlPaths(taskDir)
	if err != nil {
		t.Fatalf("currentControlPaths refused a legitimate in-directory generation: %v", err)
	}
	if generation != digest {
		t.Fatalf("generation digest = %q, want %q", generation, digest)
	}
	want := filepath.Join(taskDir, "control", "generations", digest)
	if !strings.HasPrefix(paths.Evidence, want+string(filepath.Separator)) {
		t.Fatalf("evidence path %q is not inside the resolved generation %q", paths.Evidence, want)
	}
}
