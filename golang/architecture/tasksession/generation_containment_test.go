// SPDX-License-Identifier: AGPL-3.0-only

package tasksession

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// writeGenerationPointer writes control/latest-generation.yaml naming `generation`.
func writeGenerationPointer(t *testing.T, taskDir, generation, digest string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(taskDir, "control"), 0o755); err != nil {
		t.Fatal(err)
	}
	b, err := yaml.Marshal(controlGenerationPointerEnvelope{
		TaskControlGeneration: controlGenerationPointer{
			SchemaVersion: SchemaVersion, Generation: generation, DigestSHA256: digest,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(taskDir, "control", "latest-generation.yaml"), b, 0o600); err != nil {
		t.Fatal(err)
	}
}

// A GENERATION POINTER MAY NOT NAME A DIRECTORY OUTSIDE ITS TASK.
//
// The guard was `filepath.Base(root) != ptr.DigestSHA256`. filepath.Join CLEANS ".."
// segments, so a pointer whose generation is "../../../../elsewhere/<digest>" resolves to a
// root outside the task directory while its basename still equals the recorded hex -- the
// check passes on a path that has left the task.
//
// The resolved generation selects which admission-decision.yaml is authoritative; the
// inspection capability and Envelope.ReadPaths are taken from it and spliced into
// Permission.ExactScope. So a pointer is an authority-selecting operand, and it must be
// confined to the task whose authority it selects.
//
// This file performs the containment test correctly for the TASK DIRECTORY itself roughly
// 180 lines earlier (filepath.Rel plus a ".." prefix test). Two rules for one property in
// one file is how the weaker one survives.
func TestAGenerationPointerOutsideTheTaskDirectoryIsRefused(t *testing.T) {
	const digest = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	taskDir := filepath.Join(t.TempDir(), "001")
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatal(err)
	}

	escapes := []string{
		"../../../../elsewhere/" + digest,
		"../" + digest,
		"generations/../../" + digest,
	}
	for _, generation := range escapes {
		writeGenerationPointer(t, taskDir, generation, digest)
		paths, gen, err := currentControlPaths(taskDir)
		if err == nil {
			rel, _ := filepath.Rel(taskDir, paths.Session)
			t.Errorf("generation %q was ACCEPTED (gen=%q); resolved Session is %q, relative to the "+
				"task directory %q -- the pointer selected an authority outside its own task",
				generation, gen, paths.Session, rel)
			continue
		}
		if !strings.Contains(err.Error(), ReasonIncompleteGeneration) {
			t.Errorf("generation %q was refused with %v, not the typed %s",
				generation, err, ReasonIncompleteGeneration)
		}
	}
}

// THE NEGATIVE CONTROL. An ordinary in-task generation still resolves, or the refusal above
// would be satisfied by a check that refuses everything.
func TestAnOrdinaryGenerationPointerStillResolves(t *testing.T) {
	const digest = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	taskDir := filepath.Join(t.TempDir(), "001")
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeGenerationPointer(t, taskDir, filepath.ToSlash(filepath.Join("generations", digest)), digest)

	paths, gen, err := currentControlPaths(taskDir)
	if err != nil {
		t.Fatalf("an ordinary generation pointer was refused: %v", err)
	}
	if gen != digest {
		t.Errorf("resolved generation = %q, want %q", gen, digest)
	}
	want := filepath.Join(taskDir, "control", "generations", digest, "convergence", "session.yaml")
	if paths.Session != want {
		t.Errorf("Session = %q, want %q", paths.Session, want)
	}
}

// THE POINTER'S DIGEST MUST IDENTIFY THE DIRECTORY IT NAMES.
//
// Containment and identity are two conjuncts, and the escape witnesses above prove only the
// first: they are satisfied by a check that tests containment alone. This drives the second.
//
// A generation directory is named BY its digest, so a pointer that records digest X while
// naming a directory called something else has not identified a generation -- it has selected
// a directory and then asserted an unrelated digest for it. Both remain authority-selecting,
// so both must refuse.
func TestAGenerationPointerWhoseDirectoryIsNotNamedByItsDigestIsRefused(t *testing.T) {
	const digest = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	taskDir := filepath.Join(t.TempDir(), "001")
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Inside control/ -- containment holds -- but the leaf is not the recorded digest.
	for _, generation := range []string{
		"generations/some-other-name",
		"generations/" + digest + "/nested",
		"generations",
	} {
		writeGenerationPointer(t, taskDir, generation, digest)
		if _, _, err := currentControlPaths(taskDir); err == nil {
			t.Errorf("generation %q was accepted although the directory it names is not %q",
				generation, digest)
		} else if !strings.Contains(err.Error(), ReasonIncompleteGeneration) {
			t.Errorf("generation %q refused with %v, not the typed %s",
				generation, err, ReasonIncompleteGeneration)
		}
	}
}
