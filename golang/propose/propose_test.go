// SPDX-License-Identifier: Apache-2.0

package propose

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestValidate_ContractFirst(t *testing.T) {
	cases := []struct {
		name    string
		req     Request
		wantErr bool
	}{
		{"missing kind", Request{Title: "x"}, true},
		{"unknown kind", Request{Kind: "nope", Title: "x"}, true},
		{"missing title", Request{Kind: "failure_mode", RelatedInvariants: []string{"i"}, Evidence: []string{"e"}}, true},
		{"failure_mode without contract link", Request{Kind: "failure_mode", Title: "t", Evidence: []string{"e"}}, true},
		{"failure_mode ok", Request{Kind: "failure_mode", Title: "t", RelatedInvariants: []string{"inv.x"}, Evidence: []string{"observed"}}, false},
		{"invariant without source", Request{Kind: "invariant", Title: "t", RelatedFailures: []string{"f"}}, true},
		{"invariant ok", Request{Kind: "invariant", Title: "t", SourceFiles: []string{"a.go"}, RelatedFailures: []string{"fm.x"}}, false},
		{"required_test bad id", Request{Kind: "required_test", Title: "t", ID: "not-a-test", SourceFiles: []string{"a.go"}}, true},
		{"required_test ok", Request{Kind: "required_test", Title: "t", ID: "a_test.go:TestX", RelatedInvariants: []string{"inv.x"}}, false},
		{"forbidden_fix ok", Request{Kind: "forbidden_fix", Title: "t", Description: "why", RelatedInvariants: []string{"inv.x"}}, false},
		{"contract_unknown needs proposed+evidence", Request{Kind: "contract_unknown", Title: "t", Description: "d"}, true},
		{"contract_unknown ok", Request{Kind: "contract_unknown", Title: "t", Description: "d", ProposedContract: "c", Evidence: []string{"e"}}, false},
		{"bad severity", Request{Kind: "failure_mode", Title: "t", Severity: "spicy", RelatedInvariants: []string{"i"}, Evidence: []string{"e"}}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			errs := Validate(tc.req)
			if tc.wantErr && len(errs) == 0 {
				t.Errorf("expected validation errors, got none")
			}
			if !tc.wantErr && len(errs) > 0 {
				t.Errorf("expected valid, got %v", errs)
			}
		})
	}
}

func TestNormalize_TrimsAndDropsEmpties(t *testing.T) {
	r := Request{Kind: "  failure_mode ", Title: " t ", Evidence: []string{" e ", "", "  "}}
	Normalize(&r)
	if r.Kind != "failure_mode" || r.Title != "t" {
		t.Errorf("trim failed: %+v", r)
	}
	if len(r.Evidence) != 1 || r.Evidence[0] != "e" {
		t.Errorf("list clean failed: %v", r.Evidence)
	}
}

func TestRenderCandidate_ParksInProposals(t *testing.T) {
	r := Request{Kind: "failure_mode", Title: "Stale seed served after reload",
		RelatedInvariants: []string{"awareness.x"}, Evidence: []string{"observed stale node"}}
	c, err := RenderCandidate(r)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(c.RelPath, "candidates/proposals/failure_mode.") {
		t.Errorf("candidate path = %q, want candidates/proposals/failure_mode.*", c.RelPath)
	}
	if len(c.NodeIDs) != 1 || !strings.HasPrefix(c.NodeIDs[0], "failure.") {
		t.Errorf("node ids = %v", c.NodeIDs)
	}
	// Content must be valid YAML and mark the entry as awaiting review.
	var doc candidateDoc
	if err := yaml.Unmarshal(c.Content, &doc); err != nil {
		t.Fatalf("candidate is not valid YAML: %v\n%s", err, c.Content)
	}
	if doc.Proposal.Status != "awaiting_review" || doc.Proposal.ProposedBy != "agent" {
		t.Errorf("candidate metadata wrong: %+v", doc.Proposal)
	}
	if doc.Proposal.Kind != "failure_mode" {
		t.Errorf("candidate kind = %q", doc.Proposal.Kind)
	}
}

func TestDeriveID(t *testing.T) {
	if got := DeriveID(Request{Kind: "failure_mode", Title: "Stale Seed!"}); got != "failure.stale_seed" {
		t.Errorf("DeriveID = %q, want failure.stale_seed", got)
	}
	if got := DeriveID(Request{Kind: "invariant", ID: "explicit.id", Title: "x"}); got != "explicit.id" {
		t.Errorf("explicit id not respected: %q", got)
	}
}
