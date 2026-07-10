// SPDX-License-Identifier: Apache-2.0

package main

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestDraftCandidateDoc_ForbiddenFixFromDoctorFinding(t *testing.T) {
	in := draftCandidateInput{
		Class:          "forbidden_fix",
		Title:          "rebuild full cluster to recover quorum",
		Description:    "Doctor saw quorum loss; remediation must never rebuild the whole cluster without a backup.",
		Severity:       "critical",
		SourceFiles:    []string{"golang/cluster_doctor/cluster_doctor_server/rules/etcd_infra.go"},
		Evidence:       []string{"finding:etcd.quorum_lost", "node:globule-ryzen"},
		DiscoveredFrom: "doctor-finding:etcd.quorum_lost@2026-06-24",
	}
	relPath, content, err := draftCandidateDoc(in)
	if err != nil {
		t.Fatalf("draftCandidateDoc: %v", err)
	}
	if !strings.HasPrefix(relPath, "candidates/forbidden_fix/") || !strings.HasSuffix(relPath, ".yaml") {
		t.Errorf("relPath = %q, want candidates/forbidden_fix/*.yaml", relPath)
	}

	// Header carries provenance + the promote hint.
	if !strings.Contains(string(content), "doctor-finding:etcd.quorum_lost@2026-06-24") {
		t.Errorf("header missing provenance: %s", content)
	}

	var doc draftedCandidateFile
	body := content[strings.Index(string(content), "candidates:"):]
	if err := yaml.Unmarshal(body, &doc); err != nil {
		t.Fatalf("rendered candidate is not valid YAML: %v", err)
	}
	if len(doc.Candidates) != 1 {
		t.Fatalf("want 1 candidate, got %d", len(doc.Candidates))
	}
	c := doc.Candidates[0]
	if c.Status != "candidate" || c.Confidence != "candidate" {
		t.Errorf("candidate must be status+confidence candidate, got %q/%q", c.Status, c.Confidence)
	}
	if c.Class != "ForbiddenFix" {
		t.Errorf("class = %q, want ForbiddenFix", c.Class)
	}
	if c.DiscoveredFrom != in.DiscoveredFrom {
		t.Errorf("discovered_from not preserved: %q", c.DiscoveredFrom)
	}
	if c.ReviewTodo == "" {
		t.Errorf("review_todo must guide the promoter")
	}
	if c.ID == "" || !strings.HasPrefix(c.ID, "candidate.forbidden_fix.") {
		t.Errorf("id should be derived from class+title slug, got %q", c.ID)
	}
}

func TestDraftCandidateDoc_RequiresProvenance(t *testing.T) {
	_, _, err := draftCandidateDoc(draftCandidateInput{Class: "invariant", Title: "x"})
	if err == nil || !strings.Contains(err.Error(), "discovered_from") {
		t.Fatalf("expected discovered_from-required error, got %v", err)
	}
}

func TestDraftCandidateDoc_RejectsUnknownClass(t *testing.T) {
	_, _, err := draftCandidateDoc(draftCandidateInput{Class: "policy", Title: "x", DiscoveredFrom: "scar:1"})
	if err == nil || !strings.Contains(err.Error(), "class must be one of") {
		t.Fatalf("expected class validation error, got %v", err)
	}
}

func TestDraftCandidateDoc_RequiresTitleOrID(t *testing.T) {
	_, _, err := draftCandidateDoc(draftCandidateInput{Class: "invariant", DiscoveredFrom: "scar:1"})
	if err == nil || !strings.Contains(err.Error(), "title or id") {
		t.Fatalf("expected title-or-id error, got %v", err)
	}
}

func TestDraftCandidateDoc_DeterministicAndClassMapping(t *testing.T) {
	classes := map[string]string{
		"invariant":     "Invariant",
		"forbidden_fix": "ForbiddenFix",
		"required_test": "RequiredTest",
		"failure_mode":  "FailureMode",
	}
	for kw, node := range classes {
		in := draftCandidateInput{Class: kw, ID: "candidate." + kw + ".fixed", DiscoveredFrom: "scar:42"}
		_, c1, err := draftCandidateDoc(in)
		if err != nil {
			t.Fatalf("%s: %v", kw, err)
		}
		_, c2, _ := draftCandidateDoc(in)
		if string(c1) != string(c2) {
			t.Errorf("%s: drafting is not deterministic", kw)
		}
		if !strings.Contains(string(c1), "class: "+node) {
			t.Errorf("%s: expected class %q in output", kw, node)
		}
	}
}
