// SPDX-License-Identifier: Apache-2.0

// Package propose holds the pure, I/O-free core of the awareness feedback
// write-path: the typed request, contract-first validation, normalization, and
// rendering of a review-queue candidate. It is shared by the `awg propose` CLI
// and the server's Propose RPC so there is exactly ONE validator — a second,
// drifting copy is precisely the kind of dishonesty AWG exists to prevent.
//
// This package never touches the filesystem, git, or the graph. Callers decide
// where a rendered candidate is written and when (if ever) it is promoted into
// the live corpus. The server RPC writes candidates only; promotion into the
// canonical YAML + rebuild stays a human/CI-gated step.
package propose

import (
	"fmt"
	"path"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// Request is one proposed feedback entry. Field tags match the CLI's JSON/YAML
// shape so a request can round-trip between the CLI, the wire, and a candidate
// file unchanged.
type Request struct {
	Kind        string `json:"kind" yaml:"kind"`
	ID          string `json:"id,omitempty" yaml:"id,omitempty"`
	Title       string `json:"title" yaml:"title"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
	Severity    string `json:"severity,omitempty" yaml:"severity,omitempty"`

	SourceFiles       []string `json:"source_files,omitempty" yaml:"source_files,omitempty"`
	RelatedInvariants []string `json:"related_invariants,omitempty" yaml:"related_invariants,omitempty"`
	RelatedFailures   []string `json:"related_failures,omitempty" yaml:"related_failures,omitempty"`
	RequiredTests     []string `json:"required_tests,omitempty" yaml:"required_tests,omitempty"`
	ForbiddenFixes    []string `json:"forbidden_fixes,omitempty" yaml:"forbidden_fixes,omitempty"`
	Evidence          []string `json:"evidence,omitempty" yaml:"evidence,omitempty"`

	Repo   string `json:"repo,omitempty" yaml:"repo,omitempty"`
	Domain string `json:"domain,omitempty" yaml:"domain,omitempty"`

	Contract         string `json:"contract,omitempty" yaml:"contract,omitempty"`
	ProposedContract string `json:"proposed_contract,omitempty" yaml:"proposed_contract,omitempty"`
	RevisionRequest  string `json:"revision_request,omitempty" yaml:"revision_request,omitempty"`
}

// Kinds returns the accepted entry kinds.
func Kinds() []string {
	return []string{"failure_mode", "invariant", "required_test", "forbidden_fix", "contract_unknown"}
}

var validKinds = map[string]bool{
	"failure_mode": true, "invariant": true, "required_test": true,
	"forbidden_fix": true, "contract_unknown": true,
}

var validSeverities = map[string]bool{"critical": true, "high": true, "warning": true}

// Normalize trims whitespace and drops empty list entries in place.
func Normalize(r *Request) {
	r.Kind = strings.TrimSpace(r.Kind)
	r.ID = strings.TrimSpace(r.ID)
	r.Title = strings.TrimSpace(r.Title)
	r.Description = strings.TrimSpace(r.Description)
	r.Severity = strings.ToLower(strings.TrimSpace(r.Severity))
	r.Repo = strings.TrimSpace(r.Repo)
	r.Domain = strings.TrimSpace(r.Domain)
	r.Contract = strings.TrimSpace(r.Contract)
	r.ProposedContract = strings.TrimSpace(r.ProposedContract)
	r.RevisionRequest = strings.TrimSpace(r.RevisionRequest)
	r.SourceFiles = cleanList(r.SourceFiles)
	r.RelatedInvariants = cleanList(r.RelatedInvariants)
	r.RelatedFailures = cleanList(r.RelatedFailures)
	r.RequiredTests = cleanList(r.RequiredTests)
	r.ForbiddenFixes = cleanList(r.ForbiddenFixes)
	r.Evidence = cleanList(r.Evidence)
}

// Validate enforces the contract-first rules. An empty slice means the request
// is acceptable; otherwise every problem is named. It is pure — no I/O.
func Validate(r Request) []string {
	var errs []string

	switch {
	case r.Kind == "":
		return []string{"kind is required (failure_mode | invariant | required_test | forbidden_fix | contract_unknown)"}
	case !validKinds[r.Kind]:
		return []string{fmt.Sprintf("unknown kind %q", r.Kind)}
	}

	if r.Title == "" {
		errs = append(errs, "title is required")
	}
	if r.Severity != "" && !validSeverities[r.Severity] {
		errs = append(errs, fmt.Sprintf("severity %q is not one of critical|high|warning", r.Severity))
	}

	if r.Kind == "contract_unknown" {
		if r.Description == "" {
			errs = append(errs, "contract_unknown requires a description of what was observed")
		}
		if r.ProposedContract == "" && r.RevisionRequest == "" {
			errs = append(errs, "contract_unknown requires a proposed_contract or revision_request (no vague notes)")
		}
		if len(r.Evidence) == 0 {
			errs = append(errs, "contract_unknown requires at least one evidence line (the observed failure)")
		}
		return errs
	}

	// Contract-first: every canonical entry must connect to a contract.
	if len(r.RelatedInvariants) == 0 && len(r.RelatedFailures) == 0 && r.Contract == "" {
		errs = append(errs, "contract-first: link a related_invariant or related_failure, or set contract (what contract was violated or clarified?)")
	}

	switch r.Kind {
	case "failure_mode":
		if len(r.RelatedInvariants) == 0 && r.Contract == "" {
			errs = append(errs, "failure_mode: name the invariant it violates via related_invariant (or contract)")
		}
		if len(r.Evidence) == 0 && len(r.RequiredTests) == 0 {
			errs = append(errs, "failure_mode: provide evidence (what we observed) or required_test (what proves it now)")
		}
	case "invariant":
		if len(r.SourceFiles) == 0 {
			errs = append(errs, "invariant: anchor it with at least one source_file (the files it protects)")
		}
		if len(r.RelatedFailures) == 0 && len(r.ForbiddenFixes) == 0 && len(r.RequiredTests) == 0 {
			errs = append(errs, "invariant: connect a related_failure, forbidden_fix, or required_test")
		}
	case "required_test":
		if r.ID == "" {
			errs = append(errs, "required_test: id is required and must be file.go:TestName")
		} else if !looksLikeTestID(r.ID) {
			errs = append(errs, fmt.Sprintf("required_test: id %q must look like path/to/file_test.go:TestName", r.ID))
		}
		if len(r.RelatedInvariants) == 0 && len(r.RelatedFailures) == 0 && len(r.SourceFiles) == 0 {
			errs = append(errs, "required_test: it must protect something — related_invariant, related_failure, or source_file")
		}
	case "forbidden_fix":
		if len(r.RelatedInvariants) == 0 && r.Contract == "" {
			errs = append(errs, "forbidden_fix: name the invariant it protects via related_invariant (or contract)")
		}
		if r.Description == "" {
			errs = append(errs, "forbidden_fix: description must state why the fix is forbidden")
		}
	}
	return errs
}

// Candidate is a rendered review-queue entry: the repo-relative path it should
// be written to, the file content, and the node ids it declares.
type Candidate struct {
	RelPath string
	Content []byte
	NodeIDs []string
}

// candidateDoc is the on-disk shape of an agent proposal awaiting review.
type candidateDoc struct {
	Proposal candidateEntry `yaml:"proposal"`
}

type candidateEntry struct {
	Status     string           `yaml:"status"`      // always "awaiting_review"
	ProposedBy string           `yaml:"proposed_by"` // "agent"
	Request    `yaml:",inline"` // kind, id, title, … at the same level
}

// RenderCandidate produces the review-queue file for a (validated) request. All
// kinds render as a candidate — an agent proposal never lands directly in the
// live corpus; a human/CI step promotes it. Deterministic output.
func RenderCandidate(r Request) (Candidate, error) {
	id := DeriveID(r)
	r.ID = id // stamp the resolved id so the entry is self-describing
	doc := candidateDoc{Proposal: candidateEntry{
		Status:     "awaiting_review",
		ProposedBy: "agent",
		Request:    r,
	}}
	body, err := yaml.Marshal(doc)
	if err != nil {
		return Candidate{}, err
	}
	header := "# Agent-proposed awareness entry — AWAITING REVIEW.\n" +
		"# Written by the awareness feedback write-path (Propose RPC / awg onboard); NOT a\n" +
		"# live graph node. Promote by moving the entry into the canonical corpus file\n" +
		"# after verifying the contract.\n"
	relPath := path.Join("candidates", "proposals", r.Kind+"."+slugify(id)+".yaml")
	return Candidate{RelPath: relPath, Content: append([]byte(header), body...), NodeIDs: []string{id}}, nil
}

// DeriveID mirrors the CLI's id derivation so a proposal has a stable id.
func DeriveID(r Request) string {
	if r.ID != "" {
		return r.ID
	}
	prefix := idPrefixByKind[r.Kind]
	if prefix == "" {
		prefix = "feedback"
	}
	if hint := domainHint(r); hint != "" {
		prefix = prefix + "." + hint
	}
	return prefix + "." + slugify(r.Title)
}

var idPrefixByKind = map[string]string{
	"failure_mode":     "failure",
	"invariant":        "invariant",
	"forbidden_fix":    "forbidden_fix",
	"contract_unknown": "contract_unknown",
}

var testIDPattern = regexp.MustCompile(`(?i)\.go:Test[A-Za-z0-9_]+`)

func looksLikeTestID(id string) bool { return testIDPattern.MatchString(id) }

var nonSlugRun = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = nonSlugRun.ReplaceAllString(s, "_")
	s = strings.Trim(s, "_")
	if len(s) > 60 {
		s = strings.Trim(s[:60], "_")
	}
	if s == "" {
		s = "entry"
	}
	return s
}

func domainHint(r Request) string {
	src := r.Domain
	if src == "" {
		src = r.Repo
	}
	if src == "" {
		return ""
	}
	parts := strings.Split(strings.Trim(src, "/"), "/")
	return slugify(parts[len(parts)-1])
}

func cleanList(in []string) []string {
	var out []string
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}
