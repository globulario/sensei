// SPDX-License-Identifier: AGPL-3.0-only

package prospective

// Recall and nuisance, measured together on the real corpus.
//
// A retrieval that returns the whole graph has perfect recall and no value: it
// relocates the reading problem instead of solving it. So neither number is
// reported alone, and the test fails if recall is bought by volume.
//
// THE GROUND TRUTH IS THE CORPUS ITSELF, by leave-one-out. For a file that IS
// governed, hide its direct anchors and ask what prospective retrieval would
// have proposed. If the hidden anchor comes back, the relationship was reachable
// without the direct binding -- which is the capability under test.
//
// WHAT THIS DOES NOT MEASURE. Whether a proposed candidate is SEMANTICALLY
// right for the change. That is the third rung and it is not mechanical. These
// numbers describe reach, not relevance, and the assertions below are bounds on
// reach only.

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// corpusAnchors reads authored anchors and their path scopes out of the YAML
// without a YAML dependency: it needs only `- id:` lines and the `files:` block
// beneath each, which is a stable authored shape.
func corpusAnchors(t *testing.T) []Anchor {
	t.Helper()
	var out []Anchor
	// FAIL, never skip a file. If either authored corpus file is renamed,
	// missing or unreadable, silently measuring the remainder still clears the
	// 20-anchor threshold and both bounds -- leaving CI green while reporting
	// recall and nuisance figures computed over a partial corpus. A number
	// measured over an unknown population is not a smaller measurement.
	for _, name := range []string{"invariants.yaml", "failure_modes.yaml"} {
		b, err := os.ReadFile(filepath.Join("../../docs/awareness", name))
		if err != nil {
			t.Fatalf("authored corpus file %s could not be read, so any recall figure "+
				"would describe an unknown population: %v", name, err)
		}
		var cur *Anchor
		inFiles := false
		for _, line := range strings.Split(string(b), "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "#") {
				continue
			}
			if strings.HasPrefix(trimmed, "- id:") {
				if cur != nil && len(cur.Files) > 0 {
					out = append(out, *cur)
				}
				id := strings.TrimSpace(strings.TrimPrefix(trimmed, "- id:"))
				cur = &Anchor{ID: id, Class: strings.TrimSuffix(name, ".yaml")}
				inFiles = false
				continue
			}
			if cur == nil {
				continue
			}
			if trimmed == "files:" {
				inFiles = true
				continue
			}
			if inFiles {
				if strings.HasPrefix(trimmed, "- ") {
					cur.Files = append(cur.Files, strings.TrimSpace(strings.TrimPrefix(trimmed, "- ")))
					continue
				}
				inFiles = false
			}
		}
		if cur != nil && len(cur.Files) > 0 {
			out = append(out, *cur)
		}
	}
	return out
}

func TestRecallAndNuisanceAreMeasuredTogether(t *testing.T) {
	anchors := corpusAnchors(t)
	if len(anchors) < 20 {
		t.Skipf("only %d authored anchors with path scopes; not enough corpus to measure", len(anchors))
	}

	// Subjects: every file some anchor protects, with the anchors that protect it.
	truth := map[string]map[string]bool{}
	for _, a := range anchors {
		for _, f := range a.Files {
			if strings.HasSuffix(f, "/") || f == "" {
				continue // directory scope, not a file subject
			}
			if truth[f] == nil {
				truth[f] = map[string]bool{}
			}
			truth[f][a.ID] = true
		}
	}
	var files []string
	for f := range truth {
		files = append(files, f)
	}
	sort.Strings(files)

	var subjects, hits, wanted, totalCandidates int
	worst := 0
	for _, f := range files {
		want := truth[f]
		if len(want) == 0 {
			continue
		}
		// LEAVE ONE OUT: strip this file from every anchor, so the direct
		// binding cannot supply the answer.
		var hidden []Anchor
		for _, a := range anchors {
			cp := a
			cp.Files = nil
			for _, af := range a.Files {
				if af != f {
					cp.Files = append(cp.Files, af)
				}
			}
			if len(cp.Files) > 0 {
				hidden = append(hidden, cp)
			}
		}
		got := Retrieve(Subject{Files: []string{f}}, hidden, nil)
		subjects++
		wanted += len(want)
		totalCandidates += len(got)
		if len(got) > worst {
			worst = len(got)
		}
		for _, c := range got {
			if want[c.ID] {
				hits++
			}
		}
	}
	if subjects == 0 {
		t.Fatal("no subjects — the evaluation would report a vacuous number")
	}
	recall := float64(hits) / float64(wanted)
	avg := float64(totalCandidates) / float64(subjects)
	t.Logf("subjects=%d  hidden-anchors=%d  recalled=%d  recall=%.2f  candidates/subject avg=%.1f worst=%d",
		subjects, wanted, hits, recall, avg, worst)

	// WHICH SIGNAL PRODUCED THIS NUMBER, stated so the figure is not read as
	// more than it is. This evaluation passes no subject domains, so the
	// ESTABLISHED signal never fires and every candidate above is resemblance.
	// The measured recall is therefore the same-directory signal ALONE, and the
	// authority-eligible path is unmeasured rather than measured-as-zero.
	var established int
	for _, f := range files[:1] {
		for _, c := range Retrieve(Subject{Files: []string{f}}, anchors, nil) {
			if c.AuthorityEligible() {
				established++
			}
		}
	}
	if established != 0 {
		t.Errorf("this evaluation supplies no domains, so no candidate can be authority-eligible; got %d", established)
	}

	// BOUNDS ON REACH, not on relevance.
	//
	// Recall must be non-trivial: a retrieval that finds nothing is not
	// retrieval. And the nuisance bound is what stops recall being bought by
	// volume -- returning the whole corpus would score 1.0 and be worthless.
	if recall <= 0 {
		t.Errorf("recall is %.2f: no hidden anchor was reachable through any governed relationship", recall)
	}
	if avg > 40 {
		t.Errorf("average %.1f candidates per subject: recall is being bought with volume, "+
			"which relocates the reading problem rather than solving it", avg)
	}
}
