// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// runDraftCandidate implements `sensei draft-candidate` (WB-2): the incident →
// candidate bridge. It turns a runtime observation — a cluster-doctor finding, a
// scar, an incident record — into a structured DRAFT entry in the awareness
// review queue (docs/awareness/candidates/), carrying provenance back to the
// originating incident. It NEVER promotes and NEVER rebuilds: candidates are
// excluded from the live graph build (extractor skips candidates/) until a human
// reviews and runs `sensei promote`.
//
// This closes the open end of the write-back loop: previously a raw incident had
// no path to a drafted candidate — an agent hand-authored every one. The drafter
// is deterministic; it STRUCTURES the incident into a review-ready skeleton of
// the chosen class. The reviewer completes the rule specifics and promotes.
//
// Input is a typed payload (--json file or stdin, or flags):
//
//	{ "class": "forbidden_fix", "title": "...", "description": "...",
//	  "severity": "high", "source_files": ["..."], "evidence": ["..."],
//	  "discovered_from": "doctor-finding:quorum-lost-2026-06-24" }
func runDraftCandidate(args []string) int {
	fs := flag.NewFlagSet("sensei draft-candidate", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	jsonIn := fs.String("json", "", "read the incident payload as JSON from a file or '-' for stdin")
	class := fs.String("class", "", "candidate class: invariant | forbidden_fix | required_test | failure_mode")
	id := fs.String("id", "", "candidate id (default: derived from title)")
	title := fs.String("title", "", "short title")
	description := fs.String("description", "", "what was observed / why it matters")
	severity := fs.String("severity", "", "critical | high | medium | low")
	discoveredFrom := fs.String("discovered-from", "", "provenance: the incident/finding/scar this was drafted from (REQUIRED)")
	var sourceFiles, evidence multiString
	fs.Var(&sourceFiles, "source-file", "implicated source file (repeatable)")
	fs.Var(&evidence, "evidence", "evidence line (repeatable)")
	repoRoot := fs.String("repo-root", "", "repo whose docs/awareness/candidates/ receives the draft (default: services repo)")
	dryRun := fs.Bool("dry-run", false, "print the drafted candidate; do not write")
	asJSON := fs.Bool("json-out", false, "emit the result as JSON")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: sensei draft-candidate [flags]

Draft a runtime incident/finding/scar into a structured candidate in the
awareness review queue (docs/awareness/candidates/). status:candidate — excluded
from the graph build until reviewed and promoted with `+"`sensei promote`"+`.

Flags:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}

	in := draftCandidateInput{
		Class:          *class,
		ID:             *id,
		Title:          *title,
		Description:    *description,
		Severity:       *severity,
		DiscoveredFrom: *discoveredFrom,
		SourceFiles:    []string(sourceFiles),
		Evidence:       []string(evidence),
	}
	if strings.TrimSpace(*jsonIn) != "" {
		raw, err := readJSONInput(*jsonIn)
		if err != nil {
			fmt.Fprintf(os.Stderr, "sensei draft-candidate: %v\n", err)
			return 1
		}
		if err := json.Unmarshal(raw, &in); err != nil {
			fmt.Fprintf(os.Stderr, "sensei draft-candidate: parse json: %v\n", err)
			return 1
		}
	}

	relPath, content, err := draftCandidateDoc(in)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei draft-candidate: %v\n", err)
		return 1
	}

	if *dryRun {
		fmt.Print(string(content))
		return 0
	}

	root := strings.TrimSpace(*repoRoot)
	if root == "" {
		root, _ = resolveServicesRepo("")
	}
	if root == "" {
		fmt.Fprintln(os.Stderr, "sensei draft-candidate: cannot resolve a repo for docs/awareness/candidates/; pass -repo-root or use -dry-run")
		return 1
	}
	outPath := filepath.Join(root, "docs", "awareness", relPath)
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "sensei draft-candidate: %v\n", err)
		return 1
	}
	if err := os.WriteFile(outPath, content, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "sensei draft-candidate: %v\n", err)
		return 1
	}

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(map[string]string{"status": "drafted", "path": relPath, "review_with": "sensei promote " + draftedID(in)})
		return 0
	}
	fmt.Printf("drafted candidate: %s\n", outPath)
	fmt.Printf("  status:candidate — review and `sensei promote %s` to enter the graph\n", draftedID(in))
	return 0
}

// draftCandidateInput is the typed incident payload the drafter accepts.
type draftCandidateInput struct {
	Class             string   `json:"class"`
	ID                string   `json:"id"`
	Title             string   `json:"title"`
	Description       string   `json:"description"`
	Severity          string   `json:"severity"`
	SourceFiles       []string `json:"source_files"`
	Evidence          []string `json:"evidence"`
	RelatedInvariants []string `json:"related_invariants"`
	RelatedFailures   []string `json:"related_failure_modes"`
	DiscoveredFrom    string   `json:"discovered_from"`
}

// draftedCandidate is the ordered YAML shape written to the review queue.
type draftedCandidate struct {
	ID                  string   `yaml:"id"`
	Class               string   `yaml:"class"`
	Status              string   `yaml:"status"`
	Confidence          string   `yaml:"confidence"`
	Title               string   `yaml:"title,omitempty"`
	Description         string   `yaml:"description,omitempty"`
	Severity            string   `yaml:"severity,omitempty"`
	SourceFiles         []string `yaml:"source_files,omitempty"`
	Evidence            []string `yaml:"evidence,omitempty"`
	RelatedInvariants   []string `yaml:"related_invariants,omitempty"`
	RelatedFailureModes []string `yaml:"related_failure_modes,omitempty"`
	DiscoveredFrom      string   `yaml:"discovered_from"`
	ReviewTodo          string   `yaml:"review_todo"`
}

type draftedCandidateFile struct {
	Candidates []draftedCandidate `yaml:"candidates"`
}

// draftCandidateClasses maps the accepted class keyword to its PascalCase node
// class, its candidates/ subdirectory, and the reviewer guidance for promotion.
var draftCandidateClasses = map[string]struct {
	node   string
	subdir string
	todo   string
}{
	"invariant":     {"Invariant", "invariant", "Express the property that must ALWAYS hold; add protects(files/symbols) and required_tests, then promote."},
	"forbidden_fix": {"ForbiddenFix", "forbidden_fix", "State the fix that must NEVER be applied and the invariant it would violate, then promote."},
	"required_test": {"RequiredTest", "required_test", "Name the test that should exist to guard this behavior and the file it lives in, then promote."},
	"failure_mode":  {"FailureMode", "failure_mode", "Describe how this breaks and link the invariant/forbidden_fix that prevents it, then promote."},
}

// draftCandidateDoc is the pure core: it validates the incident and renders the
// review-queue candidate document. Returns the repo-relative path under
// docs/awareness/ and the YAML content. Deterministic — no clock, no I/O.
func draftCandidateDoc(in draftCandidateInput) (relPath string, content []byte, err error) {
	cls := strings.ToLower(strings.TrimSpace(in.Class))
	spec, ok := draftCandidateClasses[cls]
	if !ok {
		valid := make([]string, 0, len(draftCandidateClasses))
		for k := range draftCandidateClasses {
			valid = append(valid, k)
		}
		sort.Strings(valid)
		return "", nil, fmt.Errorf("class must be one of %s, got %q", strings.Join(valid, " | "), in.Class)
	}
	if strings.TrimSpace(in.DiscoveredFrom) == "" {
		return "", nil, fmt.Errorf("discovered_from is required — a candidate must carry provenance back to the incident it was drafted from")
	}
	if strings.TrimSpace(in.Title) == "" && strings.TrimSpace(in.ID) == "" {
		return "", nil, fmt.Errorf("title or id is required")
	}

	id := strings.TrimSpace(in.ID)
	if id == "" {
		id = "candidate." + cls + "." + slugify(in.Title)
	}

	entry := draftedCandidate{
		ID:                  id,
		Class:               spec.node,
		Status:              "candidate",
		Confidence:          "candidate",
		Title:               strings.TrimSpace(in.Title),
		Description:         strings.TrimSpace(in.Description),
		Severity:            strings.ToLower(strings.TrimSpace(in.Severity)),
		SourceFiles:         in.SourceFiles,
		Evidence:            in.Evidence,
		RelatedInvariants:   in.RelatedInvariants,
		RelatedFailureModes: in.RelatedFailures,
		DiscoveredFrom:      strings.TrimSpace(in.DiscoveredFrom),
		ReviewTodo:          spec.todo,
	}

	body, err := yaml.Marshal(draftedCandidateFile{Candidates: []draftedCandidate{entry}})
	if err != nil {
		return "", nil, err
	}
	header := fmt.Sprintf("# DRAFT candidate from incident %q (sensei draft-candidate, WB-2).\n"+
		"# status:candidate — excluded from the graph build until reviewed and\n"+
		"# promoted with `sensei promote %s`.\n", entry.DiscoveredFrom, id)
	relPath = filepath.Join("candidates", spec.subdir, slugify(id)+".yaml")
	return relPath, append([]byte(header), body...), nil
}

func draftedID(in draftCandidateInput) string {
	if id := strings.TrimSpace(in.ID); id != "" {
		return id
	}
	return "candidate." + strings.ToLower(strings.TrimSpace(in.Class)) + "." + slugify(in.Title)
}

// readJSONInput reads a JSON payload from a file path or '-' for stdin.
func readJSONInput(src string) ([]byte, error) {
	if src == "-" {
		return io.ReadAll(os.Stdin)
	}
	return os.ReadFile(src)
}
