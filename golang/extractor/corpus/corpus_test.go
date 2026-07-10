// SPDX-License-Identifier: Apache-2.0

package corpus

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/globulario/sensei/golang/extractor/coldsource"
)

func TestClassify(t *testing.T) {
	cases := []struct {
		name      string
		f         Finding
		action    Action
		entryType string
		maxStatus string
	}{
		{"strong+landed", Finding{OutputClass: "strong_intent", GroundingTier: "landed_commit"}, ActionIntegrate, "intent", StatusActive},
		{"strong+docs", Finding{OutputClass: "strong_intent", GroundingTier: "docs_only"}, ActionIntegrate, "intent", StatusCandidate},
		// bug #2: intent-mine tier names must be recognized as grounded
		{"strong+executable_truth", Finding{OutputClass: "strong_intent", GroundingTier: "executable_truth"}, ActionIntegrate, "intent", StatusActive},
		{"hidden+test", Finding{OutputClass: "hidden_intent", GroundingTier: "test_encoded"}, ActionIntegrate, "invariant", StatusActive},
		{"hidden+landed_behavior", Finding{OutputClass: "hidden_intent", GroundingTier: "landed_behavior"}, ActionIntegrate, "invariant", StatusActive},
		// bug #1: divergence/gap findings are inherently unresolved — they must
		// integrate as candidate-level evidence, NOT be refused as "never".
		{"missing+unresolved", Finding{OutputClass: "missing_invariant", GroundingTier: "unresolved"}, ActionIntegrate, "candidate_principle", StatusCandidate},
		{"missing+weak", Finding{OutputClass: "missing_invariant", GroundingTier: "weak_hint"}, ActionIntegrate, "candidate_principle", StatusCandidate},
		{"stale+docs", Finding{OutputClass: "stale_intent", GroundingTier: "docs_only"}, ActionIntegrate, "drift_warning", StatusCandidate},
		{"stale+unresolved", Finding{OutputClass: "stale_intent", GroundingTier: "unresolved"}, ActionIntegrate, "drift_warning", StatusCandidate},
		{"ambiguous+landed", Finding{OutputClass: "ambiguous_owner", GroundingTier: "landed_commit"}, ActionIntegrate, "drift_warning", StatusCandidate},
		{"ambiguous+unresolved", Finding{OutputClass: "ambiguous_owner", GroundingTier: "unresolved"}, ActionIntegrate, "drift_warning", StatusCandidate},
		{"ungrounded", Finding{OutputClass: "ungrounded_claim", GroundingTier: "docs_only"}, ActionNever, "", ""},
		{"strong+unresolved", Finding{OutputClass: "strong_intent", GroundingTier: "unresolved"}, ActionNever, "", ""},
		{"cs-loadbearing", Finding{Provenance: "coldsource", ReviewLabel: "load-bearing", GroundingTier: "landed_commit"}, ActionIntegrate, "invariant", StatusActive},
		{"cs-review-only", Finding{Provenance: "coldsource", GroundingTier: "review_suggestion"}, ActionNever, "", ""},
		{"cs-not-loadbearing", Finding{Provenance: "coldsource", GroundingTier: "landed_commit", ReviewLabel: "shallow"}, ActionHold, "", StatusCandidate},
	}
	for _, c := range cases {
		v := Classify(c.f)
		if v.Action != c.action || v.EntryType != c.entryType || v.MaxStatus != c.maxStatus {
			t.Errorf("%s: got action=%s type=%q max=%q, want %s/%q/%q (%s)",
				c.name, v.Action, v.EntryType, v.MaxStatus, c.action, c.entryType, c.maxStatus, v.Reason)
		}
	}
}

func TestMaterialize_ForcesCandidate_RefusesNever(t *testing.T) {
	// active-eligible verdict still materializes at candidate
	v := Classify(Finding{ID: "x.strong", OutputClass: "strong_intent", GroundingTier: "landed_commit", Domain: "d", Provenance: "intent_mine", EvidenceCitations: []string{"file:a.go:1"}})
	e, err := Materialize(v)
	if err != nil {
		t.Fatal(err)
	}
	if e.Status != StatusCandidate {
		t.Errorf("materialize must force candidate, got %s", e.Status)
	}
	// never verdict refused
	if _, err := Materialize(Classify(Finding{ID: "y", OutputClass: "ungrounded_claim"})); err == nil {
		t.Error("materializing a 'never' finding must error")
	}
}

func TestWriteEntry_CageAndStatus(t *testing.T) {
	dir := t.TempDir()
	e := Entry{ID: "e1", Type: "invariant", Domain: "d", Status: StatusCandidate, Provenance: "intent_mine", EvidenceCitations: []string{"file:a.go:1"}}
	// outside candidates/ → refused
	if _, err := WriteEntry(filepath.Join(dir, "active"), e); err == nil {
		t.Error("writing outside candidates/ must be refused")
	}
	// under candidates/ → ok
	cdir := filepath.Join(dir, "pilot", "candidates")
	p, err := WriteEntry(cdir, e)
	if err != nil {
		t.Fatalf("write under candidates/: %v", err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Errorf("entry not written: %v", err)
	}
	// non-candidate status → refused
	e.Status = StatusActive
	if _, err := WriteEntry(cdir, e); err == nil {
		t.Error("WriteEntry must refuse a non-candidate status")
	}
}

func TestValidateEntry(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\nvar x = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git := coldsource.NewGitVerifier(dir)

	valid := Entry{ID: "e", Type: "invariant", Domain: "d", Status: StatusCandidate, Provenance: "intent_mine", EvidenceCitations: []string{"file:a.go:1"}}
	valid.Grounding.Tier = "landed_commit"
	if v := ValidateEntry(valid, dir, git); len(v) != 0 {
		t.Errorf("valid candidate rejected: %v", v)
	}

	// missing domain
	noDom := valid
	noDom.Domain = ""
	if v := ValidateEntry(noDom, dir, git); len(v) == 0 {
		t.Error("missing domain must fail")
	}

	// active drift_warning must fail
	driftActive := valid
	driftActive.Type = "drift_warning"
	driftActive.Status = StatusActive
	if v := ValidateEntry(driftActive, dir, git); len(v) == 0 {
		t.Error("active drift_warning must fail")
	}

	// active with unresolved tier must fail
	activeUnres := valid
	activeUnres.Status = StatusActive
	activeUnres.Grounding.Tier = "docs_only"
	if v := ValidateEntry(activeUnres, dir, git); len(v) == 0 {
		t.Error("active below landed_commit must fail")
	}

	// active, grounded, resolving citation → ok
	activeOK := valid
	activeOK.Status = StatusActive // tier landed_commit, file:a.go:1 resolves
	if v := ValidateEntry(activeOK, dir, git); len(v) != 0 {
		t.Errorf("active grounded+resolving entry rejected: %v", v)
	}
}
