// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The A/B/C specimens, re-executed rather than remembered.
//
// The finding they encode was first produced by hand: three candidates run
// through `sensei promote --dry-run` against an isolated graph, all three
// accepted. That observation was recorded by the same actor who predicted it,
// which is weak on its own — so the strengthening is not another opinion, it is
// independent EXECUTION. This test re-runs the same inputs through the same
// validator on every CI run, and it fails if the behaviour changes without
// anybody saying so.
//
// What it pins today is uncomfortable on purpose: B and C PASS. That is the
// current, honest behaviour of a structural gate, and the test says so in the
// assertion rather than in a comment somebody can skip.
func loadSpecimens(t *testing.T) []map[string]interface{} {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "promote_specimens", "specimens.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Candidates []map[string]interface{} `yaml:"candidates"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Candidates) != 3 {
		t.Fatalf("expected 3 specimens, got %d", len(doc.Candidates))
	}
	return doc.Candidates
}

func TestPromoteValidatesStructureNotEvidence(t *testing.T) {
	dir := t.TempDir()
	for _, c := range loadSpecimens(t) {
		name, _ := c["specimen"].(string)
		id, _ := c["id"].(string)
		t.Run(name, func(t *testing.T) {
			err := validateCandidateEntry(c, "invariants.yaml", dir)
			if err != nil {
				t.Fatalf("%s (%s) was rejected: %v\n\nIf this is now rejected on EVIDENTIAL grounds, that is a "+
					"real advance and this test should be rewritten to demand it. If it is rejected on a "+
					"format technicality, the specimen has rotted and should be repaired, not deleted.", name, id, err)
			}
		})
	}
}

// The two that must eventually fail, and the reason each exists.
//
// Neither is a gate today. They are the acceptance criteria for whatever
// answers dq.closure_knowledge_admission: a mechanism that cannot reject these
// has established nothing about truth, whatever it prints.
func TestTheSpecimensThatAFutureAdmissionBoundaryMustReject(t *testing.T) {
	specimens := loadSpecimens(t)
	byName := map[string]map[string]interface{}{}
	for _, c := range specimens {
		n, _ := c["specimen"].(string)
		byName[n] = c
	}

	// B is partially factual, which is what makes it dangerous. The mutex DOES
	// serialize map access; that is not what it is for. A deliberately absurd
	// false claim would test nothing.
	b := byName["B-plausible-false"]
	if b == nil {
		t.Fatal("the B specimen is missing; it is the semantic-discrimination case")
	}
	if statement, _ := b["statement"].(string); !strings.Contains(strings.ToLower(statement), "serialize") {
		t.Fatal("the B specimen no longer makes the partially-factual claim it exists to make")
	}

	// C authorizes itself: its evidence cites only artifacts introduced by the
	// same change, and promoting it removes the gap blocking the run.
	c := byName["C-self-supporting"]
	if c == nil {
		t.Fatal("the C specimen is missing; it is the anti-self-certification case")
	}
	if from, _ := c["discovered_from"].(string); !strings.Contains(from, "same change") {
		t.Fatal("the C specimen no longer describes its own evidence as self-introduced")
	}
}

// The whole evidential check, stated as a test so it cannot quietly grow a
// reputation it has not earned: a non-empty string passes, and a blank one does
// not. Nothing opens a file.
func TestEvidenceCheckIsAStringPresenceCheck(t *testing.T) {
	dir := t.TempDir()
	base := loadSpecimens(t)[0]

	blank := map[string]interface{}{}
	for k, v := range base {
		blank[k] = v
	}
	blank["evidence"] = "   "
	if err := validateCandidateEntry(blank, "invariants.yaml", dir); err == nil {
		t.Fatal("blank evidence was accepted")
	}

	// Any non-empty string satisfies it, including one that establishes
	// nothing. This is the finding, not a defect being introduced here.
	nonsense := map[string]interface{}{}
	for k, v := range base {
		nonsense[k] = v
	}
	nonsense["evidence"] = "trust me"
	if err := validateCandidateEntry(nonsense, "invariants.yaml", dir); err != nil {
		t.Fatalf("the evidence check reads more than presence now: %v — if that is deliberate, this test "+
			"should be rewritten to describe what it reads", err)
	}
}
