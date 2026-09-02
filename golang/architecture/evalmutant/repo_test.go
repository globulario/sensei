// SPDX-License-Identifier: AGPL-3.0-only

package evalmutant

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const testCommittedAt = "2026-01-01T00:00:00Z"

func materializeRepo(t *testing.T, m Mutant) (root, baseRev, defectRev string) {
	t.Helper()
	root = filepath.Join(t.TempDir(), "repo")
	baseRev, defectRev, err := MaterializeRepo(root, m, RepoOptions{CommittedAt: testCommittedAt})
	if err != nil {
		t.Fatalf("materialize repo %s: %v", m.Defect, err)
	}
	return root, baseRev, defectRev
}

func gitIn(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	cmd.Env = gitEnv(RepoOptions{CommittedAt: testCommittedAt})
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// THE reason this exists. The mutant's commit message must actually BE the
// defect commit's message, or the misleading-commit-message class is witnessed
// by proxy — its own witness says "the message lives outside the tree" — and
// the suite measures whether an evaluator noticed a rename rather than whether
// it caught a message describing work the diff does not contain.
func TestDefectCommitCarriesTheMutantsOwnMessage(t *testing.T) {
	for _, d := range Defects() {
		t.Run(string(d), func(t *testing.T) {
			m, err := Build(d)
			if err != nil {
				t.Fatal(err)
			}
			root, _, _ := materializeRepo(t, m)
			got := gitIn(t, root, "log", "-1", "--format=%B")
			if strings.TrimSpace(got) != strings.TrimSpace(m.CommitMessage) {
				t.Errorf("defect commit message = %q, want the mutant's own %q", got, m.CommitMessage)
			}
		})
	}
}

// The defect commit's diff must be exactly the mutation: that is what lets an
// arm ask "what did this commit claim, and what did it change?" A diff
// containing baseline noise would make every such comparison unreliable.
func TestDefectCommitDiffIsExactlyTheMutation(t *testing.T) {
	for _, d := range Defects() {
		t.Run(string(d), func(t *testing.T) {
			m, err := Build(d)
			if err != nil {
				t.Fatal(err)
			}
			root, baseRev, defectRev := materializeRepo(t, m)
			if baseRev == defectRev {
				t.Fatal("the defect produced no commit of its own")
			}
			changed := strings.Fields(gitIn(t, root, "diff", "--name-only", baseRev, defectRev))
			want := map[string]bool{}
			for _, p := range m.TouchedPaths {
				want[strings.TrimSuffix(p, " (removed)")] = true
			}
			if len(changed) != len(want) {
				t.Errorf("commit changed %v, but the mutant reports touching %v", changed, m.TouchedPaths)
			}
			for _, p := range changed {
				if !want[filepath.ToSlash(p)] {
					t.Errorf("commit changed %s, which is not one of the defect's paths", p)
				}
			}
		})
	}
}

// A removal defect must appear as a deletion in history. Writing only the
// mutant's files would leave the baseline copy in place, so the removal would
// be invisible and the commit would misdescribe its own mutation.
func TestRemovedFilesAreDeletedInTheDefectCommit(t *testing.T) {
	var checked int
	for _, d := range Defects() {
		m, err := Build(d)
		if err != nil {
			t.Fatal(err)
		}
		var removed []string
		for _, p := range m.TouchedPaths {
			if strings.HasSuffix(p, " (removed)") {
				removed = append(removed, strings.TrimSuffix(p, " (removed)"))
			}
		}
		if len(removed) == 0 {
			continue
		}
		checked++
		root, baseRev, defectRev := materializeRepo(t, m)
		status := gitIn(t, root, "diff", "--name-status", baseRev, defectRev)
		for _, p := range removed {
			if !strings.Contains(status, "D\t"+p) {
				t.Errorf("%s: %s is not recorded as deleted:\n%s", d, p, status)
			}
		}
	}
	if checked == 0 {
		// The "at least one" shape: skipping a defect that removes no file is
		// right per defect, but checking NONE means the deletion-recording
		// assertion never ran and the package still reports ok.
		t.Fatal("no defect in the suite removes a file, so deletion recording was never " +
			"exercised; the suite is defined in this repository, so this is a defect")
	}
}

// The history must be reproducible, or two runs of the same mutant bind to
// different revisions and every digest derived from them stops comparing.
func TestRepoHistoryIsReproducible(t *testing.T) {
	m, err := Build(DefectAuthoritySplit)
	if err != nil {
		t.Fatal(err)
	}
	_, firstBase, firstDefect := materializeRepo(t, m)
	_, secondBase, secondDefect := materializeRepo(t, m)
	if firstBase != secondBase {
		t.Errorf("baseline revision is not reproducible: %s vs %s", firstBase, secondBase)
	}
	if firstDefect != secondDefect {
		t.Errorf("defect revision is not reproducible: %s vs %s", firstDefect, secondDefect)
	}
}

// The control has no defect, so it must have no defect commit — giving it one
// would hand the control a history shape no mutant has.
func TestControlHasBaselineHistoryOnly(t *testing.T) {
	root, baseRev, defectRev := materializeRepo(t, Baseline())
	if baseRev != defectRev {
		t.Errorf("the control grew a second commit: %s -> %s", baseRev, defectRev)
	}
	if n := gitIn(t, root, "rev-list", "--count", "HEAD"); n != "1" {
		t.Errorf("control history has %s commits, want 1", n)
	}
}

// An unstamped history would not be reproducible, so the timestamp is required
// rather than defaulted to the wall clock.
//
// The offset cases matter as much as the empty one: git accepts a
// timezone-less value and reads it in the MACHINE's local zone, so the same
// mutant and the same string produce different revisions on a laptop and in
// CI. Both runs succeed and only the hashes disagree, which is the quiet shape
// of an unreproducible suite.
func TestMaterializeRepoRequiresAnOffsetBearingTimestamp(t *testing.T) {
	for name, stamp := range map[string]string{
		"empty":        "",
		"blank":        "   ",
		"no timezone":  "2026-01-01T00:00:00",
		"date only":    "2026-01-01",
		"git relative": "yesterday",
		"unix seconds": "1767225600",
		"not a time":   "sometime",
	} {
		t.Run(name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "repo")
			if _, _, err := MaterializeRepo(root, Baseline(), RepoOptions{CommittedAt: stamp}); err == nil {
				t.Fatalf("%q was accepted; it does not name one instant on every machine", stamp)
			}
		})
	}
	// An explicit offset is accepted, including a non-UTC one.
	for _, ok := range []string{"2026-01-01T00:00:00Z", "2026-01-01T00:00:00+02:00"} {
		root := filepath.Join(t.TempDir(), "repo")
		if _, _, err := MaterializeRepo(root, Baseline(), RepoOptions{CommittedAt: ok}); err != nil {
			t.Errorf("%q was refused: %v", ok, err)
		}
	}
}

// The revision must not depend on the MACHINE, which is what requiring an
// offset buys. It deliberately does NOT claim that two spellings of the same
// instant agree: git records the offset inside the commit object, so
// "...T02:00:00Z" and "...T04:00:00+02:00" are different commits by design.
// The property that matters is that ONE string names one commit everywhere,
// which an offset-less value cannot promise.
func TestRevisionDoesNotDependOnTheAmbientTimezone(t *testing.T) {
	m, err := Build(DefectAuthoritySplit)
	if err != nil {
		t.Fatal(err)
	}
	const stamp = "2026-01-01T02:00:00Z"
	rev := func() string {
		root := filepath.Join(t.TempDir(), "repo")
		_, defectRev, err := MaterializeRepo(root, m, RepoOptions{CommittedAt: stamp})
		if err != nil {
			t.Fatalf("materialize: %v", err)
		}
		return defectRev
	}
	t.Setenv("TZ", "UTC")
	inUTC := rev()
	t.Setenv("TZ", "Pacific/Kiritimati") // UTC+14, the furthest offset there is
	shifted := rev()
	if inUTC != shifted {
		t.Errorf("the same stamp produced %s under TZ=UTC and %s under TZ=Pacific/Kiritimati; the history is machine-dependent", inUTC, shifted)
	}
}

// The witness must still find the defect in a git-materialized tree: if
// committing changed what is on disk, the suite would measure a different tree
// than the one it certifies.
func TestWitnessesStillHoldInAGitMaterializedTree(t *testing.T) {
	for _, d := range Defects() {
		t.Run(string(d), func(t *testing.T) {
			m, err := Build(d)
			if err != nil {
				t.Fatal(err)
			}
			root, _, _ := materializeRepo(t, m)
			witness, err := WitnessFor(d)
			if err != nil {
				t.Fatal(err)
			}
			present, detail, err := witness(root)
			if err != nil {
				t.Fatalf("witness: %v", err)
			}
			if !present {
				t.Errorf("%s is absent from its git-materialized tree (%s)", d, detail)
			}
		})
	}
}
