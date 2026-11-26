// SPDX-License-Identifier: AGPL-3.0-only

package factextract

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestPythonExtractionObservesSyntaxWithoutExecutingCode(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "src", "state.py"), `raise RuntimeError("must never execute the repository")
class Store:
    def apply(self, value):
        if value is None:
            # A direct refusal, with a comment before it.
            raise ValueError("required")
        self.value = value
        self.count += 1
        self.left, self.right = value, value
        local = value
    def reset(self):
        self.value = None
class Other:
    def apply(self, value):
        self.value = value
def nested(value, inner):
    text = "if value: raise ValueError()"
    # if value: raise ValueError()
    if value:
        if inner:
            raise ValueError()
    if inner:
        def fail():
            raise ValueError()
`)
	opts := Options{IncludeTests: true, MinimumConfidence: "low"}
	report, err := Extract(root, opts)
	if err != nil {
		t.Fatal(err)
	}
	guards, writes := 0, map[string][]string{}
	for _, f := range report.Facts {
		if f.Extractor != "python_ast_extractor" {
			continue
		}
		if f.Provenance == nil || !f.Provenance.SourceDigestPinsContent() || f.Evidence.LineStart <= 0 {
			t.Fatalf("Python fact lacks a source receipt: %+v", f)
		}
		switch f.Predicate {
		case "refuses_when":
			guards++
			if f.Object != "value is None" && f.Object != "inner" {
				t.Fatalf("non-refusing branch claimed as a guard: %+v", f)
			}
		case "writes":
			writes[f.Object] = append(writes[f.Object], f.Subject)
		}
	}
	if guards != 2 {
		t.Fatalf("guards=%d, want only the two direct raise branches", guards)
	}
	if len(writes) != 5 || len(writes["src/state.py::Store::self.value"]) != 2 || len(writes["src/state.py::Other::self.value"]) != 1 {
		t.Fatalf("attribute writers lost receiver identity: %+v", writes)
	}
	for _, lim := range report.Limitations {
		if lim.Blocking || lim.Source == "go_semantic_extractor" {
			t.Fatalf("supported Python repository reported as unreadable: %+v", lim)
		}
	}
	again, err := Extract(root, opts)
	if err != nil || !reflect.DeepEqual(report, again) {
		t.Fatalf("Python extraction is not deterministic: %v", err)
	}
}

func TestPythonExtractionExcludesDependenciesAndOptionalTests(t *testing.T) {
	root := t.TempDir()
	guard := "def apply(value):\n    if value is None:\n        raise ValueError()\n"
	for _, rel := range []string{"src/state.py", "tests/test_state.py", ".venv/dependency.py", ".agents/hook.py", "venv/dependency.py", "__pycache__/dependency.py", "generated_pb2.py"} {
		writeFile(t, filepath.Join(root, rel), guard)
	}
	for _, include := range []bool{false, true} {
		report, err := Extract(root, Options{IncludeTests: include})
		if err != nil {
			t.Fatal(err)
		}
		want := 1
		if include {
			want = 2
		}
		if len(report.Facts) != want {
			t.Fatalf("IncludeTests=%v: facts=%d, want %d: %+v", include, len(report.Facts), want, report.Facts)
		}
	}
}

func TestPythonParseFailureRemainsBlocking(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "broken.py"), "def broken(:\n    if True:\n        raise ValueError()\n")
	writeFile(t, filepath.Join(root, "valid.py"), "def apply(value):\n    if value is None:\n        raise ValueError()\n")
	report, err := Extract(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, lim := range report.Limitations {
		if lim.Source == "broken.py" && lim.Blocking {
			if len(report.Facts) != 1 || report.Facts[0].Evidence.SourceFile != "valid.py" {
				t.Fatalf("malformed syntax produced facts or hid valid facts: %+v", report.Facts)
			}
			return
		}
	}
	t.Fatalf("parse failure was hidden: %+v", report.Limitations)
}

func TestPythonExtractionReportsCancellation(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "state.py"), "def apply(value):\n    if value is None:\n        raise ValueError()\n")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	facts, _, limitations, err := extractPythonArchitecture(ctx, resolveInvariantRepositoryIdentity(root), Options{})
	if err != nil || len(facts) != 0 {
		t.Fatalf("cancelled extraction: facts=%d err=%v", len(facts), err)
	}
	for _, lim := range limitations {
		if lim.Blocking && strings.Contains(lim.Reason, "Python syntax extraction") {
			return
		}
	}
	t.Fatalf("cancellation did not report incomplete extraction: %+v", limitations)
}

func TestPythonExtractionHonorsGitIgnoreAndKeepsTrackedSources(t *testing.T) {
	root := t.TempDir()
	guard := "def apply(value):\n    if value is None:\n        raise ValueError()\n"
	writeFile(t, filepath.Join(root, ".gitignore"), "runs/\ntracked.py\n")
	for _, rel := range []string{"src/state.py", "src/tracked.py", "runs/experiment_source/copy.py"} {
		writeFile(t, filepath.Join(root, rel), guard)
	}
	run := func(args ...string) {
		t.Helper()
		out, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("init", "-q")
	run("add", ".gitignore", "src/state.py")
	run("add", "-f", "src/tracked.py")
	writeFile(t, filepath.Join(root, "src/new.py"), guard)
	report, err := Extract(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Facts) != 3 {
		t.Fatalf("facts=%d, want tracked and unignored sources only: %+v", len(report.Facts), report.Facts)
	}
	for _, f := range report.Facts {
		if strings.HasPrefix(f.Evidence.SourceFile, "runs/") {
			t.Fatalf("ignored runtime snapshot became a source fact: %+v", f)
		}
	}
}

// #386 review: containment must hold for symlinked PARENT directories, not only
// for a symlinked file. A directory inside the repository that links outside it
// must never contribute Python sources — in a Git checkout or a plain directory.
func TestPythonSourcesNeverEscapeThroughASymlinkedParentDirectory(t *testing.T) {
	for _, mode := range []string{"git", "plain"} {
		t.Run(mode, func(t *testing.T) {
			outside := t.TempDir()
			writeFile(t, filepath.Join(outside, "secret", "leak.py"), "class Leak:\n    def f(self):\n        self.x = 1\n")
			root := t.TempDir()
			writeFile(t, filepath.Join(root, "src", "ok.py"), "class Ok:\n    def f(self):\n        self.y = 1\n")
			if err := os.Symlink(filepath.Join(outside, "secret"), filepath.Join(root, "src", "linked")); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, filepath.Join(root, "vendorish")); err != nil {
				t.Fatal(err)
			}
			if mode == "git" {
				for _, args := range [][]string{{"init"}, {"config", "user.email", "t@example.com"}, {"config", "user.name", "T"}, {"add", "."}, {"commit", "-m", "init"}} {
					cmd := exec.Command("git", args...)
					cmd.Dir = root
					if out, err := cmd.CombinedOutput(); err != nil {
						t.Fatalf("git %v: %v %s", args, err, out)
					}
				}
			}
			files, err := PythonSourceFiles(context.Background(), root, true)
			if err != nil && !strings.Contains(err.Error(), "refused") && !strings.Contains(err.Error(), "escapes") {
				t.Fatalf("unexpected error: %v", err)
			}
			realOutside, _ := filepath.EvalSymlinks(outside)
			for _, f := range files {
				real, rerr := filepath.EvalSymlinks(f)
				if rerr != nil {
					t.Fatalf("selected an unresolvable path %s: %v", f, rerr)
				}
				if strings.HasPrefix(real, realOutside+string(filepath.Separator)) || strings.Contains(f, "leak.py") {
					t.Fatalf("a source outside the repository was selected through a symlinked parent: %s -> %s", f, real)
				}
			}
			if err == nil {
				found := false
				for _, f := range files {
					found = found || strings.HasSuffix(f, filepath.Join("src", "ok.py"))
				}
				if !found {
					t.Fatalf("the legitimate source was lost: %v", files)
				}
			}
		})
	}
}

// The explicit guard, tested directly: a path through a symlinked parent that
// leaves the repository is refused; one that stays inside is accepted.
func TestContainedSourceRefusesEscapeThroughSymlinkedParent(t *testing.T) {
	outside := t.TempDir()
	writeFile(t, filepath.Join(outside, "pkg", "leak.py"), "x = 1\n")
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "src", "ok.py"), "y = 1\n")
	if err := os.Symlink(filepath.Join(outside, "pkg"), filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "src"), filepath.Join(root, "alias")); err != nil {
		t.Fatal(err)
	}
	if err := containedSource(root, filepath.Join(root, "linked", "leak.py")); err == nil || !strings.Contains(err.Error(), "escapes root") {
		t.Fatalf("escape through a symlinked parent was not refused: %v", err)
	}
	if err := containedSource(root, filepath.Join(root, "src", "ok.py")); err != nil {
		t.Fatalf("a contained source was refused: %v", err)
	}
	if err := containedSource(root, filepath.Join(root, "alias", "ok.py")); err != nil {
		t.Fatalf("an in-repository alias was refused: %v", err)
	}
}
