// SPDX-License-Identifier: AGPL-3.0-only

package main

// A tolerated dangling reference must say WHY it is tolerated.
//
// The baseline holds 233 entries and records only that each is allowed. That is
// dangerous in one specific way: a genuinely dead proof edge in THIS repository
// is indistinguishable from a reference that belongs to another domain's corpus
// and simply cannot be built here. The checker exists to catch the first, and
// the baseline hides it among the second.
//
// Measured 2026-09-01: of the 34 bare-name Test entries, 25 are real test
// functions in globulario/services (cited 86 times in that corpus) and 9
// resolve nowhere at all. None of them is a sensei proof edge. So the tolerated
// set is currently honest -- and nothing was enforcing that.
//
// This test classifies every tolerated Test entry and refuses the one class
// that must never be tolerated: a HOME-DOMAIN proof edge whose referent this
// repository can see is missing.
//
// WHAT IT DELIBERATELY DOES NOT DO. It does not decide whether a resolvable
// test PROVES the claim that cites it. That is the third rung and it is not
// mechanical; the classification stops where structure stops.

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// ratchet is the number of tolerated Test entries. It must TRACK the baseline,
// not merely cap it.
//
// As a ceiling it could be gamed without anyone noticing: remove two entries,
// add two others, and the count returns to 72 while the promised explicit
// re-baselining never happens. So the check requires EQUALITY -- a fall must
// lower this constant in the same change, which is the human statement the
// ratchet was supposed to force.
const toleratedTestEntriesRatchet = 72

type baselineEntry struct{ class, id string }

func readBaseline(t *testing.T) []baselineEntry {
	t.Helper()
	// FAIL, never skip. The baseline is a repository-owned fixture, so an
	// unreadable one is a defect. Skipping made both checks report success
	// while classifying nothing -- and it permitted the exact shape recorded
	// as forbidden: delete the baseline to make the check green.
	f, err := os.Open("../../docs/awareness/dangling_refs_baseline.tsv")
	if err != nil {
		t.Fatalf("the governed baseline could not be opened, so nothing was classified: %v", err)
	}
	defer f.Close()
	var out []baselineEntry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "#") || strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) < 2 {
			continue
		}
		out = append(out, baselineEntry{class: parts[0], id: parts[1]})
	}
	return out
}

// homeTestFunctions indexes every test function this repository declares, keyed
// by "file:function".
//
// Keying by function NAME alone accepted the same name from ANY file, so a
// cited test that moved to another file left the exact file:fn proof edge stale
// and the check still passed -- the dead-binding case this exists to prevent,
// surviving because the referent was matched loosely. A proof edge names a
// path and a function, and both must match.
func homeTestFunctions(t *testing.T) map[string]string {
	t.Helper()
	decl := regexp.MustCompile(`func\s+(Test\w+)\s*\(`)
	out := map[string]string{}
	root := "../.."
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		rel := strings.TrimPrefix(filepath.ToSlash(path), "../../")
		for _, m := range decl.FindAllSubmatch(b, -1) {
			out[rel+":"+string(m[1])] = rel
		}
		return nil
	})
	return out
}

// The class that must never be tolerated: a path-form proof edge naming a file
// in THIS repository whose function is missing. That is a dead binding, and it
// is exactly what two critical invariants in globulario/sensei-code turned out
// to be.
func TestNoHomeDomainProofEdgeHidesBehindTheBaseline(t *testing.T) {
	entries := readBaseline(t)
	if len(entries) == 0 {
		t.Fatal("read no baseline entries — this check would pass vacuously")
	}
	home := homeTestFunctions(t)
	if len(home) == 0 {
		t.Fatal("indexed no test functions — this check would pass vacuously")
	}

	var dead []string
	for _, e := range entries {
		if !strings.Contains(e.class, "Test") {
			continue
		}
		file, fn, isPath := strings.Cut(e.id, ":")
		if !isPath || !strings.HasSuffix(file, "_test.go") {
			continue // bare-name form; classified by the test below
		}
		// A HOME-DOMAIN edge is one whose path belongs to this repository's
		// tree, whether or not the file is still there.
		//
		// Testing os.Stat on the file itself let the worst case through: DELETE
		// the cited _test.go entirely and the entry was skipped as foreign,
		// counted as path-form, and the total stayed at 72. The dead-binding
		// case this check exists to catch, defeated by deleting more.
		//
		// Ownership is decided by the top-level directory, which is stable and
		// present regardless of the file: cmd/ and golang/ are this repository,
		// and a path rooted anywhere else belongs to a corpus built elsewhere.
		if !homeDomainPath(file) {
			continue
		}
		if _, ok := home[file+":"+fn]; !ok {
			dead = append(dead, e.id)
		}
	}
	if len(dead) > 0 {
		t.Fatalf("%d tolerated proof edge(s) name a file in this repository whose test is GONE; "+
			"a dead binding may not be baseline-tolerated: %v", len(dead), dead)
	}
}

// Every tolerated Test entry must fall into a named class. An entry that fits
// none of them is unclassified debt, and unclassified is not a licence.
func TestEveryToleratedProofEdgeIsClassified(t *testing.T) {
	entries := readBaseline(t)
	home := homeTestFunctions(t)

	var pathForm, bareForm, unclassified int
	for _, e := range entries {
		if !strings.Contains(e.class, "Test") {
			continue
		}
		switch file, fn, isPath := strings.Cut(e.id, ":"); {
		case isPath && strings.HasSuffix(file, "_test.go"):
			// Path form: resolvable or not, it is checkable, and the test above
			// refuses the dead ones.
			_ = fn
			pathForm++
		case strings.HasPrefix(e.id, "Test"):
			// Bare form: no path, so this repository cannot resolve it. It is
			// a foreign-domain or historical citation, and it is tolerated
			// BECAUSE it is unresolvable here -- not because it was checked.
			if declaredSomewhere(home, e.id) {
				t.Errorf("%q is declared in THIS repository but tolerated as unresolvable; "+
					"a resolvable edge must be bound, not baselined", e.id)
			}
			bareForm++
		default:
			unclassified++
			t.Errorf("tolerated Test entry %q matches no known form", e.id)
		}
	}
	total := pathForm + bareForm + unclassified
	t.Logf("tolerated Test entries: %d (path-form %d, bare-form %d, unclassified %d)",
		total, pathForm, bareForm, unclassified)

	switch {
	case total > toleratedTestEntriesRatchet:
		t.Fatalf("tolerated Test entries rose to %d from a ratchet of %d; "+
			"a new dangling proof edge must be repaired, or re-baselined by a human who states why",
			total, toleratedTestEntriesRatchet)
	case total < toleratedTestEntriesRatchet:
		t.Fatalf("tolerated Test entries fell to %d but the ratchet still reads %d; "+
			"lower the constant in this change, or the freed headroom silently permits %d new ones",
			total, toleratedTestEntriesRatchet, toleratedTestEntriesRatchet-total)
	}
}

// declaredSomewhere answers the LOOSER question deliberately: is this bare name
// declared anywhere in this repository? A bare citation carries no path, so
// name is all there is to match -- and a bare name that resolves here should be
// bound rather than baselined.
func declaredSomewhere(home map[string]string, name string) bool {
	for k := range home {
		if strings.HasSuffix(k, ":"+name) {
			return true
		}
	}
	return false
}

// homeDomainPath reports whether a cited path belongs to THIS repository, by
// PROVENANCE rather than by current existence.
//
// Two earlier answers were both wrong, in opposite directions:
//
//	os.Stat(file)  deleting the cited _test.go made the entry look foreign, so
//	               the worst case -- a dead edge -- bypassed the check.
//	os.Stat(top)   the combined build consumes the services corpus, so a foreign
//	               citation like cmd/foo/foo_test.go read as home merely because
//	               Sensei has a cmd/ directory; and deleting a home-only
//	               top-level directory restored the deletion bypass.
//
// Both used the filesystem AS IT IS NOW to answer a question about ORIGIN.
// Git history answers it directly and is stable against both: a file this
// repository ever tracked is ours, deleted or not; a services path this
// repository never tracked is not ours, whatever directories we happen to
// share.
func homeDomainPath(file string) bool {
	file = filepath.ToSlash(strings.TrimSpace(file))
	if file == "" || !strings.Contains(file, "/") {
		return false
	}
	out, err := exec.Command("git", "-C", "../..", "log", "--all", "--oneline", "-1", "--", file).Output()
	if err != nil {
		// Cannot establish provenance. NOT home -- an unanswerable ownership
		// question must not manufacture an accusation, and the ratchet still
		// counts the entry.
		return false
	}
	return strings.TrimSpace(string(out)) != ""
}

// Ownership is provenance, not current existence — pinned permanently.
//
// Two earlier answers used the filesystem AS IT IS NOW to decide ORIGIN, and
// both had a bypass: `os.Stat(file)` let a DELETED cited test look foreign, and
// `os.Stat(topLevelDir)` let a foreign path look home because we share a `cmd/`
// directory, while deleting a home-only directory restored the first bypass.
//
// The manual experiment that found this — delete a file, run, restore — is not
// a test. Without this, re-adding an existence gate breaks nothing that fails.
func TestOwnershipIsProvenanceNotCurrentExistence(t *testing.T) {
	// A path this repository TRACKED AND DELETED is still ours. That is the
	// case the check exists for, and the one an existence gate silently drops.
	deleted := "golang/publication/oxigraph_roundtrip_test.go"
	if _, err := os.Stat(filepath.Join("../..", deleted)); err == nil {
		t.Skipf("%s exists again; this test needs a path deleted from history", deleted)
	}
	if !homeDomainPath(deleted) {
		t.Errorf("%s was tracked and deleted in this repository and is read as foreign; "+
			"an existence gate would let its dead proof edge bypass the check", deleted)
	}

	// A path this repository never tracked is NOT ours, even when we share its
	// top-level directory. The combined build consumes the services corpus, so
	// cmd/ is not evidence of ownership.
	foreign := "cmd/globular_services_only/never_here_test.go"
	if homeDomainPath(foreign) {
		t.Errorf("%s was never tracked here and is claimed as ours merely because we have a cmd/ "+
			"directory; that accuses another corpus of our defect", foreign)
	}
}
