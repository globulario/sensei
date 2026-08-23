// SPDX-License-Identifier: AGPL-3.0-only

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
	Kind         string `json:"kind" yaml:"kind"`
	ID           string `json:"id,omitempty" yaml:"id,omitempty"`
	Title        string `json:"title" yaml:"title"`
	Description  string `json:"description,omitempty" yaml:"description,omitempty"`
	Severity     string `json:"severity,omitempty" yaml:"severity,omitempty"`
	Status       string `json:"status,omitempty" yaml:"status,omitempty"`
	Context      string `json:"context,omitempty" yaml:"context,omitempty"`
	Consequences string `json:"consequences,omitempty" yaml:"consequences,omitempty"`

	ArchitecturalPlane string `json:"architectural_plane,omitempty" yaml:"architectural_plane,omitempty"`

	SourceFiles       []string `json:"source_files,omitempty" yaml:"source_files,omitempty"`
	RelatedInvariants []string `json:"related_invariants,omitempty" yaml:"related_invariants,omitempty"`
	RelatedFailures   []string `json:"related_failures,omitempty" yaml:"related_failures,omitempty"`
	RequiredTests     []string `json:"required_tests,omitempty" yaml:"required_tests,omitempty"`
	ForbiddenFixes    []string `json:"forbidden_fixes,omitempty" yaml:"forbidden_fixes,omitempty"`
	Evidence          []string `json:"evidence,omitempty" yaml:"evidence,omitempty"`
	DefinesBoundaries []string `json:"defines_boundaries,omitempty" yaml:"defines_boundaries,omitempty"`
	DefinesContracts  []string `json:"defines_contracts,omitempty" yaml:"defines_contracts,omitempty"`
	AffectsComponents []string `json:"affects_components,omitempty" yaml:"affects_components,omitempty"`
	SupportedEvidence []string `json:"supported_by_evidence,omitempty" yaml:"supported_by_evidence,omitempty"`

	// SurvivalEvidence is what shows the repair HELD — the thing that separates a
	// repair from an anecdote about one. Required for applied_repair.
	SurvivalEvidence []string `json:"survival_evidence,omitempty" yaml:"survival_evidence,omitempty"`

	// IntroducedBy is what the filer explicitly attributes this failure's
	// introduction to. It is a list because real failures have compound
	// ancestry — one change introduces unsafe state, another removes the check
	// that compensated for it — and a one-scar-one-commit model would force
	// somebody to pick a favourite and drop the rest.
	IntroducedBy []Attribution `json:"introduced_by,omitempty" yaml:"introduced_by,omitempty"`

	Repo   string `json:"repo,omitempty" yaml:"repo,omitempty"`
	Domain string `json:"domain,omitempty" yaml:"domain,omitempty"`

	Contract         string `json:"contract,omitempty" yaml:"contract,omitempty"`
	ProposedContract string `json:"proposed_contract,omitempty" yaml:"proposed_contract,omitempty"`
	RevisionRequest  string `json:"revision_request,omitempty" yaml:"revision_request,omitempty"`
}

// Attribution names one change a failure is explicitly attributed to.
//
// The identity is repository plus immutable commit SHA, never the SHA alone.
// The same abbreviated hash occurs in more than one repository, and a causal
// relation that cannot say which one is not evidence. The CLI may accept only
// the SHA because repository context is already known; what gets PERSISTED
// carries both.
type Attribution struct {
	Repo   string `json:"repo" yaml:"repo"`
	Commit string `json:"commit" yaml:"commit"`
}

// commitSHA is what may be recorded as an immutable change identity.
//
// Branch names, tags and HEAD are refused deliberately: all three move, and an
// attribution that silently repoints later is worse than none, because it
// still looks like a record of what happened.
var commitSHA = regexp.MustCompile(`^[0-9a-f]{7,40}$`)

// NormalizeAttributions trims, lowercases, defaults the repository and drops
// duplicates, preserving order.
func NormalizeAttributions(in []Attribution, defaultRepo string) []Attribution {
	seen := map[string]bool{}
	var out []Attribution
	for _, a := range in {
		a.Commit = strings.ToLower(strings.TrimSpace(a.Commit))
		a.Repo = strings.TrimSpace(a.Repo)
		if a.Repo == "" {
			a.Repo = strings.TrimSpace(defaultRepo)
		}
		if a.Commit == "" && a.Repo == "" {
			continue
		}
		key := a.Repo + "@" + a.Commit
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, a)
	}
	return out
}

// Kinds returns the accepted entry kinds.
func Kinds() []string {
	return []string{"failure_mode", "invariant", "required_test", "forbidden_fix", "applied_repair", "decision", "contract_unknown"}
}

var validKinds = map[string]bool{
	"failure_mode": true, "invariant": true, "required_test": true,
	"forbidden_fix": true, "applied_repair": true, "decision": true, "contract_unknown": true,
}

var validSeverities = map[string]bool{"critical": true, "high": true, "warning": true}
var validArchitecturalPlanes = map[string]bool{"desired": true, "intended": true, "historical": true}

// Normalize trims whitespace and drops empty list entries in place.
func Normalize(r *Request) {
	r.Kind = strings.TrimSpace(r.Kind)
	r.ID = strings.TrimSpace(r.ID)
	r.Title = strings.TrimSpace(r.Title)
	r.Description = strings.TrimSpace(r.Description)
	r.Severity = strings.ToLower(strings.TrimSpace(r.Severity))
	r.Status = strings.ToLower(strings.TrimSpace(r.Status))
	r.Context = strings.TrimSpace(r.Context)
	r.Consequences = strings.TrimSpace(r.Consequences)
	r.ArchitecturalPlane = strings.ToLower(strings.TrimSpace(r.ArchitecturalPlane))
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
	r.IntroducedBy = NormalizeAttributions(r.IntroducedBy, r.Repo)
	r.DefinesBoundaries = cleanList(r.DefinesBoundaries)
	r.DefinesContracts = cleanList(r.DefinesContracts)
	r.AffectsComponents = cleanList(r.AffectsComponents)
	r.SupportedEvidence = cleanList(r.SupportedEvidence)
	r.SurvivalEvidence = cleanList(r.SurvivalEvidence)
}

// Validate enforces the contract-first rules. An empty slice means the request
// is acceptable; otherwise every problem is named. It is pure — no I/O.
func Validate(r Request) []string {
	var errs []string

	switch {
	case r.Kind == "":
		return []string{"kind is required (failure_mode | invariant | required_test | forbidden_fix | applied_repair | decision | contract_unknown)"}
	case !validKinds[r.Kind]:
		return []string{fmt.Sprintf("unknown kind %q", r.Kind)}
	}

	if r.Kind != "failure_mode" && len(r.IntroducedBy) != 0 {
		// Narrow on purpose. introduced_by asserts that a change introduced a
		// FAILURE; what it would mean on an invariant or a required_test has
		// not been decided, and inventing a meaning here would put edges in
		// the graph that nobody defined.
		errs = append(errs, "introduced_by is only accepted on failure_mode; what it asserts for "+quote(r.Kind)+" is undefined")
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
		for _, a := range r.IntroducedBy {
			if !commitSHA.MatchString(a.Commit) {
				errs = append(errs, "introduced_by: "+quote(a.Commit)+" is not a commit SHA (7-40 hex); a branch, tag or HEAD moves, and an attribution that repoints later still looks like a record of what happened")
			}
			if strings.TrimSpace(a.Repo) == "" {
				errs = append(errs, "introduced_by: "+quote(a.Commit)+" names no repository; the identity is repository plus commit, because the same short hash occurs in more than one repository")
			}
		}
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
	case "applied_repair":
		// The positive counterpart to forbidden_fix. Every scar previously taught
		// the project one negative lesson and discarded the positive one, so the
		// next agent facing the same failure_mode re-derived the repair from
		// scratch — and derived it slightly differently, which is how two
		// correct-looking fixes for one problem end up in the same codebase.
		//
		// Each requirement below exists to keep this from becoming a changelog:
		// a repair with no failure is an anecdote, a repair with no test is
		// unproven, and a repair with no files is not reproducible, because
		// repairs are context-bound and the context IS the files.
		if len(r.RelatedFailures) == 0 {
			errs = append(errs, "applied_repair: name the failure it repaired via related_failure (a repair with no failure is an anecdote)")
		}
		if len(r.RequiredTests) == 0 {
			errs = append(errs, "applied_repair: cite the required_test that proves it (a repair with no test is unproven)")
		}
		if len(r.SourceFiles) == 0 {
			errs = append(errs, "applied_repair: anchor it with at least one source_file (repairs are context-bound; the context is the files)")
		}
		if r.Description == "" {
			errs = append(errs, "applied_repair: description must state what the repair actually did")
		}
		if len(r.SurvivalEvidence) == 0 {
			errs = append(errs, "applied_repair: provide survival_evidence (what shows the repair HELD, not merely that it was applied)")
		}
	case "decision":
		if r.Severity != "" {
			errs = append(errs, "decision: severity is not supported")
		}
		if r.Contract != "" || r.ProposedContract != "" || r.RevisionRequest != "" {
			errs = append(errs, "decision: contract and contract_unknown fields are not supported")
		}
		if len(r.RequiredTests) != 0 {
			errs = append(errs, "decision: required_test links are not supported directly; define evidence or separate required_test records")
		}
		if r.Description == "" {
			errs = append(errs, "decision: description is required and becomes rationale")
		}
		if r.ArchitecturalPlane != "" && !validArchitecturalPlanes[r.ArchitecturalPlane] {
			errs = append(errs, fmt.Sprintf("decision: architectural_plane %q is not one of desired|intended|historical", r.ArchitecturalPlane))
		}
		if len(r.RelatedInvariants) == 0 && len(r.RelatedFailures) == 0 &&
			len(r.ForbiddenFixes) == 0 && len(r.SourceFiles) == 0 &&
			len(r.DefinesBoundaries) == 0 && len(r.DefinesContracts) == 0 &&
			len(r.AffectsComponents) == 0 && len(r.SupportedEvidence) == 0 {
			errs = append(errs, "decision: connect the record to at least one invariant, failure, forbidden fix, source file, boundary, contract, component, or supporting evidence")
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

type candidateRequest struct {
	Kind               string        `yaml:"kind,omitempty"`
	ID                 string        `yaml:"id,omitempty"`
	Title              string        `yaml:"title,omitempty"`
	Description        string        `yaml:"description,omitempty"`
	Severity           string        `yaml:"severity,omitempty"`
	RecordStatus       string        `yaml:"record_status,omitempty"`
	Context            string        `yaml:"context,omitempty"`
	Consequences       string        `yaml:"consequences,omitempty"`
	ArchitecturalPlane string        `yaml:"architectural_plane,omitempty"`
	SourceFiles        []string      `yaml:"source_files,omitempty"`
	RelatedInvariants  []string      `yaml:"related_invariants,omitempty"`
	RelatedFailures    []string      `yaml:"related_failures,omitempty"`
	RequiredTests      []string      `yaml:"required_tests,omitempty"`
	ForbiddenFixes     []string      `yaml:"forbidden_fixes,omitempty"`
	Evidence           []string      `yaml:"evidence,omitempty"`
	DefinesBoundaries  []string      `yaml:"defines_boundaries,omitempty"`
	DefinesContracts   []string      `yaml:"defines_contracts,omitempty"`
	AffectsComponents  []string      `yaml:"affects_components,omitempty"`
	SupportedEvidence  []string      `yaml:"supported_by_evidence,omitempty"`
	SurvivalEvidence   []string      `yaml:"survival_evidence,omitempty"`
	IntroducedBy       []Attribution `yaml:"introduced_by,omitempty"`
	Repo               string        `yaml:"repo,omitempty"`
	Domain             string        `yaml:"domain,omitempty"`
	Contract           string        `yaml:"contract,omitempty"`
	ProposedContract   string        `yaml:"proposed_contract,omitempty"`
	RevisionRequest    string        `yaml:"revision_request,omitempty"`
}

type candidateEntry struct {
	Status           string           `yaml:"status"`      // always "awaiting_review"
	ProposedBy       string           `yaml:"proposed_by"` // "agent"
	candidateRequest `yaml:",inline"` // kind, id, title, … at the same level
}

// RenderCandidate produces the review-queue file for a (validated) request. All
// kinds render as a candidate — an agent proposal never lands directly in the
// live corpus; a human/CI step promotes it. Deterministic output.
func RenderCandidate(r Request) (Candidate, error) {
	id := DeriveID(r)
	r.ID = id // stamp the resolved id so the entry is self-describing
	doc := candidateDoc{Proposal: candidateEntry{
		Status:           "awaiting_review",
		ProposedBy:       "agent",
		candidateRequest: candidateRequestFromRequest(r),
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

func candidateRequestFromRequest(r Request) candidateRequest {
	return candidateRequest{
		Kind:               r.Kind,
		ID:                 r.ID,
		Title:              r.Title,
		Description:        r.Description,
		Severity:           r.Severity,
		RecordStatus:       r.Status,
		Context:            r.Context,
		Consequences:       r.Consequences,
		ArchitecturalPlane: r.ArchitecturalPlane,
		SourceFiles:        r.SourceFiles,
		RelatedInvariants:  r.RelatedInvariants,
		RelatedFailures:    r.RelatedFailures,
		RequiredTests:      r.RequiredTests,
		ForbiddenFixes:     r.ForbiddenFixes,
		Evidence:           r.Evidence,
		DefinesBoundaries:  r.DefinesBoundaries,
		DefinesContracts:   r.DefinesContracts,
		AffectsComponents:  r.AffectsComponents,
		SupportedEvidence:  r.SupportedEvidence,
		SurvivalEvidence:   r.SurvivalEvidence,
		IntroducedBy:       r.IntroducedBy,
		Repo:               r.Repo,
		Domain:             r.Domain,
		Contract:           r.Contract,
		ProposedContract:   r.ProposedContract,
		RevisionRequest:    r.RevisionRequest,
	}
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
	"decision":         "decision",
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

// quote renders a value inside an error without pulling in fmt at each site.
func quote(s string) string { return "\"" + s + "\"" }
