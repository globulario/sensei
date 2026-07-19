// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type architectureExtractionReport struct {
	GeneratedBy                     string                       `json:"generated_by" yaml:"generated_by"`
	GeneratedAt                     string                       `json:"generated_at" yaml:"generated_at"`
	RepoRoot                        string                       `json:"repo_root" yaml:"repo_root"`
	RepoDomain                      string                       `json:"repo_domain,omitempty" yaml:"repo_domain,omitempty"`
	EvidencePolicy                  []string                     `json:"evidence_policy" yaml:"evidence_policy"`
	ArchitecturalInventory          architectureInventory        `json:"architectural_inventory" yaml:"architectural_inventory"`
	ObservedContractSet             []architectureContractRecord `json:"observed_contract_set" yaml:"observed_contract_set"`
	InferredContractSet             []architectureContractRecord `json:"inferred_contract_set" yaml:"inferred_contract_set"`
	GovernedContractSet             []architectureContractRecord `json:"governed_contract_set" yaml:"governed_contract_set"`
	HistoricalMigrationMap          []architectureMigration      `json:"historical_migration_map" yaml:"historical_migration_map"`
	AuthoritySplitReport            []architectureAuthoritySplit `json:"authority_split_report" yaml:"authority_split_report"`
	ArchitecturalDirectionReport    []architectureDirection      `json:"architectural_direction_report" yaml:"architectural_direction_report"`
	UnknownsAndIrrecoverableIntent  []string                     `json:"unknowns_and_irrecoverable_intent" yaml:"unknowns_and_irrecoverable_intent"`
	RecommendedPromotionCandidates  []architecturePromotion      `json:"recommended_promotion_candidates" yaml:"recommended_promotion_candidates"`
	ProofObligationsForFutureAgents []architectureProof          `json:"proof_obligations_for_future_agents" yaml:"proof_obligations_for_future_agents"`
}

type architectureInventory struct {
	Components          []architectureInventoryItem `json:"components" yaml:"components"`
	Owners              []architectureInventoryItem `json:"owners" yaml:"owners"`
	StateDomains        []architectureInventoryItem `json:"state_domains" yaml:"state_domains"`
	APIs                []architectureInventoryItem `json:"apis" yaml:"apis"`
	Stores              []architectureInventoryItem `json:"stores" yaml:"stores"`
	Workflows           []architectureInventoryItem `json:"workflows" yaml:"workflows"`
	GeneratedArtifacts  []architectureInventoryItem `json:"generated_artifacts" yaml:"generated_artifacts"`
	EnforcementSurfaces []architectureInventoryItem `json:"enforcement_surfaces" yaml:"enforcement_surfaces"`
}

type architectureInventoryItem struct {
	ID       string   `json:"id" yaml:"id"`
	Kind     string   `json:"kind" yaml:"kind"`
	Evidence []string `json:"evidence" yaml:"evidence"`
}

type architectureContractRecord struct {
	ID                  string                      `json:"id" yaml:"id"`
	Title               string                      `json:"title" yaml:"title"`
	Statement           string                      `json:"statement" yaml:"statement"`
	Classification      architectureClassification  `json:"classification" yaml:"classification"`
	Scope               architectureScope           `json:"scope" yaml:"scope"`
	Authority           architectureAuthority       `json:"authority" yaml:"authority"`
	Evidence            architectureEvidence        `json:"evidence" yaml:"evidence"`
	Enforcement         architectureEnforcement     `json:"enforcement" yaml:"enforcement"`
	History             architectureContractHistory `json:"history" yaml:"history"`
	Contradictions      []string                    `json:"contradictions" yaml:"contradictions"`
	Alternatives        []string                    `json:"alternatives" yaml:"alternatives"`
	UnresolvedQuestions []string                    `json:"unresolved_questions" yaml:"unresolved_questions"`
	ProofObligations    []string                    `json:"proof_obligations" yaml:"proof_obligations"`
}

type architectureClassification struct {
	Layer      string `json:"layer" yaml:"layer"`
	Kind       string `json:"kind" yaml:"kind"`
	Confidence string `json:"confidence" yaml:"confidence"`
}

type architectureScope struct {
	Repositories []string `json:"repositories" yaml:"repositories"`
	Components   []string `json:"components" yaml:"components"`
	Files        []string `json:"files" yaml:"files"`
	Symbols      []string `json:"symbols" yaml:"symbols"`
	Workflows    []string `json:"workflows" yaml:"workflows"`
}

type architectureAuthority struct {
	Owner            string   `json:"owner" yaml:"owner"`
	AllowedWriters   []string `json:"allowed_writers" yaml:"allowed_writers"`
	DerivedConsumers []string `json:"derived_consumers" yaml:"derived_consumers"`
	ForbiddenWriters []string `json:"forbidden_writers" yaml:"forbidden_writers"`
}

type architectureEvidence struct {
	Tests         []string `json:"tests" yaml:"tests"`
	Gates         []string `json:"gates" yaml:"gates"`
	Schemas       []string `json:"schemas" yaml:"schemas"`
	CodePaths     []string `json:"code_paths" yaml:"code_paths"`
	Commits       []string `json:"commits" yaml:"commits"`
	PullRequests  []string `json:"pull_requests" yaml:"pull_requests"`
	Incidents     []string `json:"incidents" yaml:"incidents"`
	Documentation []string `json:"documentation" yaml:"documentation"`
}

type architectureEnforcement struct {
	Current     string `json:"current" yaml:"current"`
	FailureMode string `json:"failure_mode" yaml:"failure_mode"`
	Severity    string `json:"severity" yaml:"severity"`
}

type architectureContractHistory struct {
	PreviousPattern   string   `json:"previous_pattern" yaml:"previous_pattern"`
	MigrationSequence []string `json:"migration_sequence" yaml:"migration_sequence"`
	CurrentDirection  string   `json:"current_direction" yaml:"current_direction"`
}

type architectureMigration struct {
	Domain             string   `json:"domain" yaml:"domain"`
	OldPattern         string   `json:"old_pattern" yaml:"old_pattern"`
	CorrectivePressure string   `json:"corrective_pressure" yaml:"corrective_pressure"`
	Migration          []string `json:"migration" yaml:"migration"`
	CurrentDirection   string   `json:"current_direction" yaml:"current_direction"`
	Evidence           []string `json:"evidence" yaml:"evidence"`
	Confidence         string   `json:"confidence" yaml:"confidence"`
}

type architectureAuthoritySplit struct {
	Subject        string   `json:"subject" yaml:"subject"`
	Classification string   `json:"classification" yaml:"classification"`
	Evidence       []string `json:"evidence" yaml:"evidence"`
	Risk           string   `json:"risk" yaml:"risk"`
}

type architectureDirection struct {
	Domain                    string   `json:"domain" yaml:"domain"`
	ObservedCondition         string   `json:"observed_condition" yaml:"observed_condition"`
	HistoricalDirection       string   `json:"historical_direction" yaml:"historical_direction"`
	BindingContract           string   `json:"binding_contract" yaml:"binding_contract"`
	InferredContract          string   `json:"inferred_contract" yaml:"inferred_contract"`
	ContradictionOrRisk       string   `json:"contradiction_or_risk" yaml:"contradiction_or_risk"`
	LikelyCoherentMove        string   `json:"likely_coherent_move" yaml:"likely_coherent_move"`
	ForbiddenOrRegressiveMove string   `json:"forbidden_or_regressive_move" yaml:"forbidden_or_regressive_move"`
	ProofObligations          []string `json:"proof_obligations" yaml:"proof_obligations"`
	Evidence                  []string `json:"evidence" yaml:"evidence"`
}

type architecturePromotion struct {
	ID           string   `json:"id" yaml:"id"`
	Reason       string   `json:"reason" yaml:"reason"`
	Evidence     []string `json:"evidence" yaml:"evidence"`
	ReviewNeeded []string `json:"review_needed" yaml:"review_needed"`
}

type architectureProof struct {
	Scope  string   `json:"scope" yaml:"scope"`
	Checks []string `json:"checks" yaml:"checks"`
}

type architectureDoc struct {
	Path       string
	Candidate  bool
	Generated  bool
	TopKeys    []string
	Entries    []architectureDocEntry
	ParseError string
}

type architectureDocEntry struct {
	Key         string
	ID          string
	Title       string
	Statement   string
	Kind        string
	Status      string
	Stability   string
	SourceFiles []string
	Components  []string
	Tests       []string
	Refs        []string
}

func runArchitectureExtract(args []string) int {
	fs := flag.NewFlagSet("sensei architecture-extract", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	repo := fs.String("repo", ".", "repository root whose extracted evidence should be layered")
	domain := fs.String("domain", "", "repository domain key (default: derive from git origin when available)")
	format := fs.String("format", "markdown", "output format: markdown | json | yaml")
	outPath := fs.String("out", "", "write report to this file instead of stdout")
	historyLimit := fs.Int("history-limit", 40, "number of recent commits to inspect for migration hints (0 disables history)")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: sensei architecture-extract --repo <checkout> [flags]

Build a read-only architecture contract extraction report from evidence already
present in a repository. The report separates observed, inferred, and governed
layers and never promotes candidate contracts into active authority.

Flags:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if fs.NArg() != 0 {
		fs.Usage()
		return 2
	}
	root, err := filepath.Abs(strings.TrimSpace(*repo))
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei architecture-extract: resolve repo: %v\n", err)
		return 1
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		fmt.Fprintf(os.Stderr, "sensei architecture-extract: --repo must be an existing directory: %s\n", root)
		return 2
	}
	dom := strings.TrimSpace(*domain)
	if dom == "" {
		dom = gitRemoteDomain(root)
	}

	report, err := buildArchitectureExtractionReport(root, dom, *historyLimit)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei architecture-extract: %v\n", err)
		return 1
	}
	rendered, err := renderArchitectureExtractionReport(report, strings.ToLower(strings.TrimSpace(*format)))
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei architecture-extract: %v\n", err)
		return 2
	}
	if strings.TrimSpace(*outPath) != "" {
		if err := os.MkdirAll(filepath.Dir(*outPath), 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "sensei architecture-extract: mkdir: %v\n", err)
			return 1
		}
		if err := os.WriteFile(*outPath, rendered, 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "sensei architecture-extract: write: %v\n", err)
			return 1
		}
		fmt.Fprintf(os.Stderr, "architecture-extract: wrote %s\n", *outPath)
		return 0
	}
	fmt.Print(string(rendered))
	return 0
}

func buildArchitectureExtractionReport(root, domain string, historyLimit int) (architectureExtractionReport, error) {
	docs, err := collectArchitectureDocs(root)
	if err != nil {
		return architectureExtractionReport{}, err
	}
	workflows := collectArchitectureWorkflowFiles(root)
	generated := collectArchitectureGeneratedArtifacts(root)
	commits := collectArchitectureCommits(root, historyLimit)
	duplicates := architectureDuplicateIDs(docs)
	missingRefs := architectureMissingSourceRefs(root, docs)

	report := architectureExtractionReport{
		GeneratedBy: "sensei architecture-extract",
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		RepoRoot:    root,
		RepoDomain:  domain,
		EvidencePolicy: []string{
			"Observed facts are mechanically visible in the repository.",
			"Inferred contracts come from candidates, repeated evidence, or history hints and are review-only.",
			"Governed contracts are active authored awareness entries or enforced CI/admission surfaces.",
			"Documentation and comments are evidence, not automatic authority.",
		},
	}
	report.ArchitecturalInventory = buildArchitectureInventory(root, docs, workflows, generated)
	report.ObservedContractSet = buildObservedArchitectureContracts(root, domain, docs, generated)
	report.GovernedContractSet = buildGovernedArchitectureContracts(root, domain, docs, workflows)
	report.InferredContractSet = buildInferredArchitectureContracts(root, domain, docs, commits)
	report.AuthoritySplitReport = buildArchitectureAuthoritySplits(duplicates, missingRefs)
	report.HistoricalMigrationMap = buildArchitectureMigrations(commits, docs, workflows)
	report.ArchitecturalDirectionReport = buildArchitectureDirections(report, commits)
	report.UnknownsAndIrrecoverableIntent = buildArchitectureUnknowns(root, docs, commits)
	report.RecommendedPromotionCandidates = buildArchitecturePromotions(report.InferredContractSet)
	report.ProofObligationsForFutureAgents = buildArchitectureProofs(report.GovernedContractSet, workflows)
	return report, nil
}

func collectArchitectureDocs(root string) ([]architectureDoc, error) {
	var docs []architectureDoc
	for _, relRoot := range []string{filepath.Join("docs", "awareness"), filepath.Join("docs", "intent")} {
		base := filepath.Join(root, relRoot)
		if _, err := os.Stat(base); err != nil {
			continue
		}
		if err := filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			ext := strings.ToLower(filepath.Ext(path))
			if ext != ".yaml" && ext != ".yml" {
				return nil
			}
			doc := parseArchitectureDoc(root, path)
			docs = append(docs, doc)
			return nil
		}); err != nil {
			return nil, err
		}
	}
	sort.Slice(docs, func(i, j int) bool { return docs[i].Path < docs[j].Path })
	return docs, nil
}

func parseArchitectureDoc(root, path string) architectureDoc {
	rel := architectureRel(root, path)
	doc := architectureDoc{
		Path:      rel,
		Candidate: strings.Contains(rel, "/candidates/"),
		Generated: strings.Contains(rel, "/generated/"),
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		doc.ParseError = err.Error()
		return doc
	}
	var node yaml.Node
	if err := yaml.Unmarshal(raw, &node); err != nil {
		doc.ParseError = err.Error()
		return doc
	}
	if len(node.Content) == 0 || node.Content[0].Kind != yaml.MappingNode {
		return doc
	}
	mapping := node.Content[0]
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		key := mapping.Content[i].Value
		value := mapping.Content[i+1]
		doc.TopKeys = append(doc.TopKeys, key)
		switch key {
		case "contracts", "invariants", "failure_modes", "forbidden_fixes", "required_tests",
			"components", "boundaries", "decisions", "evidence", "proof_obligations":
			doc.Entries = append(doc.Entries, architectureEntriesFromSequence(key, value)...)
		case "authority_surface_candidates":
			doc.Entries = append(doc.Entries, architectureNestedCandidates(key, value)...)
		case "candidates":
			doc.Entries = append(doc.Entries, architectureEntriesFromSequence(key, value)...)
		case "proposal":
			if value.Kind == yaml.MappingNode {
				entry := architectureEntryFromMapping(key, value)
				if entry.ID != "" || entry.Title != "" {
					doc.Entries = append(doc.Entries, entry)
				}
			}
		}
	}
	if len(doc.Entries) == 0 {
		entry := architectureEntryFromMapping("candidate", mapping)
		if entry.ID != "" && (doc.Candidate || strings.Contains(strings.ToLower(entry.Status), "candidate")) {
			doc.Entries = append(doc.Entries, entry)
		}
	}
	sort.Strings(doc.TopKeys)
	return doc
}

func architectureNestedCandidates(key string, value *yaml.Node) []architectureDocEntry {
	if value == nil || value.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(value.Content); i += 2 {
		if value.Content[i].Value == "candidates" {
			return architectureEntriesFromSequence(key, value.Content[i+1])
		}
	}
	return nil
}

func architectureEntriesFromSequence(key string, value *yaml.Node) []architectureDocEntry {
	if value == nil || value.Kind != yaml.SequenceNode {
		return nil
	}
	var out []architectureDocEntry
	for _, item := range value.Content {
		if item.Kind != yaml.MappingNode {
			continue
		}
		e := architectureEntryFromMapping(key, item)
		if e.ID != "" || e.Title != "" {
			out = append(out, e)
		}
	}
	return out
}

func architectureEntryFromMapping(key string, item *yaml.Node) architectureDocEntry {
	e := architectureDocEntry{Key: key}
	if item == nil || item.Kind != yaml.MappingNode {
		return e
	}
	for i := 0; i+1 < len(item.Content); i += 2 {
		k := item.Content[i].Value
		v := item.Content[i+1]
		switch k {
		case "id":
			e.ID = scalarString(v)
		case "name", "title":
			if e.Title == "" {
				e.Title = scalarString(v)
			}
		case "description", "statement", "intent", "summary", "contract", "rationale", "reason":
			if e.Statement == "" {
				e.Statement = scalarString(v)
			}
		case "kind", "class":
			if e.Kind == "" {
				e.Kind = scalarString(v)
			}
		case "status":
			e.Status = scalarString(v)
		case "stability":
			e.Stability = scalarString(v)
		case "source_files", "source_paths", "files", "paths", "applies_to_authority_surfaces":
			e.SourceFiles = append(e.SourceFiles, scalarStringSlice(v)...)
		case "components", "exposed_by":
			e.Components = append(e.Components, scalarStringSlice(v)...)
		case "required_tests":
			e.Tests = append(e.Tests, scalarStringSlice(v)...)
		case "constrained_by_invariants", "protects", "related", "evidence", "incidents", "forbidden_fixes":
			e.Refs = append(e.Refs, scalarStringSlice(v)...)
		}
	}
	e.SourceFiles = dedupeSorted(e.SourceFiles)
	e.Components = dedupeSorted(e.Components)
	e.Tests = dedupeSorted(e.Tests)
	e.Refs = dedupeSorted(e.Refs)
	return e
}

func scalarString(n *yaml.Node) string {
	if n == nil {
		return ""
	}
	switch n.Kind {
	case yaml.ScalarNode:
		return strings.TrimSpace(n.Value)
	case yaml.SequenceNode:
		return strings.Join(scalarStringSlice(n), ", ")
	default:
		return ""
	}
}

func scalarStringSlice(n *yaml.Node) []string {
	if n == nil {
		return nil
	}
	if n.Kind == yaml.ScalarNode {
		if s := strings.TrimSpace(n.Value); s != "" {
			return []string{s}
		}
		return nil
	}
	if n.Kind != yaml.SequenceNode {
		return nil
	}
	var out []string
	for _, item := range n.Content {
		switch item.Kind {
		case yaml.ScalarNode:
			if s := strings.TrimSpace(item.Value); s != "" {
				out = append(out, s)
			}
		case yaml.MappingNode:
			for i := 0; i+1 < len(item.Content); i += 2 {
				key := strings.TrimSpace(item.Content[i].Value)
				value := scalarString(item.Content[i+1])
				if value == "" {
					continue
				}
				if key == "file" || key == "path" {
					out = append(out, value)
				}
			}
		}
	}
	return dedupeSorted(out)
}

func collectArchitectureWorkflowFiles(root string) []string {
	var out []string
	base := filepath.Join(root, ".github", "workflows")
	_ = filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext == ".yml" || ext == ".yaml" {
			out = append(out, architectureRel(root, path))
		}
		return nil
	})
	sort.Strings(out)
	return out
}

func collectArchitectureGeneratedArtifacts(root string) []string {
	var out []string
	for _, base := range []string{
		filepath.Join(root, "docs", "awareness", "generated"),
		filepath.Join(root, "golang", "server", "embeddata"),
	} {
		_ = filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			out = append(out, architectureRel(root, path))
			return nil
		})
	}
	sort.Strings(out)
	return out
}

func collectArchitectureCommits(root string, limit int) []string {
	if limit <= 0 {
		return nil
	}
	cmd := exec.Command("git", "-C", root, "log", "--oneline", "-n", fmt.Sprintf("%d", limit))
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	var commits []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			commits = append(commits, line)
		}
	}
	return commits
}

func buildArchitectureInventory(root string, docs []architectureDoc, workflows, generated []string) architectureInventory {
	var inv architectureInventory
	seenComponents := map[string]bool{}
	seenAPIs := map[string]bool{}
	for _, doc := range docs {
		for _, e := range doc.Entries {
			switch e.Key {
			case "components":
				id := firstNonEmpty(e.ID, e.Title)
				if id != "" && !seenComponents[id] {
					seenComponents[id] = true
					inv.Components = append(inv.Components, architectureInventoryItem{ID: id, Kind: "component", Evidence: []string{doc.Path}})
				}
			case "contracts":
				id := firstNonEmpty(e.ID, e.Title)
				if id != "" && !seenAPIs[id] {
					seenAPIs[id] = true
					inv.APIs = append(inv.APIs, architectureInventoryItem{ID: id, Kind: firstNonEmpty(e.Kind, "contract"), Evidence: []string{doc.Path}})
				}
			}
			for _, c := range e.Components {
				if c != "" && !seenComponents[c] {
					seenComponents[c] = true
					inv.Components = append(inv.Components, architectureInventoryItem{ID: c, Kind: "referenced_component", Evidence: []string{doc.Path}})
				}
			}
		}
	}
	for _, rel := range workflows {
		inv.Workflows = append(inv.Workflows, architectureInventoryItem{ID: rel, Kind: "github_workflow", Evidence: []string{rel}})
		if architectureFileContains(root, rel, "gate --enforce") || architectureFileContains(root, rel, "awg gate") || architectureFileContains(root, rel, "sensei gate") {
			inv.EnforcementSurfaces = append(inv.EnforcementSurfaces, architectureInventoryItem{ID: rel, Kind: "ci_gate", Evidence: []string{rel}})
		}
	}
	for _, rel := range generated {
		inv.GeneratedArtifacts = append(inv.GeneratedArtifacts, architectureInventoryItem{ID: rel, Kind: "generated_artifact", Evidence: []string{rel}})
	}
	for _, rel := range []string{"docs/awareness", "docs/awareness/generated", "docs/intent", ".awg", ".sensei"} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err == nil {
			inv.Stores = append(inv.Stores, architectureInventoryItem{ID: rel, Kind: "repository_state", Evidence: []string{rel}})
		}
	}
	for _, rel := range []string{"docs/awareness/high_risk_files.yaml", "docs/awareness/forbidden_fixes.yaml", "docs/awareness/required_tests.yaml"} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err == nil {
			inv.EnforcementSurfaces = append(inv.EnforcementSurfaces, architectureInventoryItem{ID: rel, Kind: "awareness_policy", Evidence: []string{rel}})
		}
	}
	for _, state := range []string{"desired_state", "runtime_state", "installed_state", "configuration_state", "identity_state", "generated_graph_state"} {
		if architectureDocsMention(docs, state) {
			inv.StateDomains = append(inv.StateDomains, architectureInventoryItem{ID: state, Kind: "mentioned_state_domain", Evidence: architectureDocsMentioning(docs, state, 3)})
		}
	}
	sortInventory(&inv)
	return inv
}

func buildObservedArchitectureContracts(root, domain string, docs []architectureDoc, generated []string) []architectureContractRecord {
	var out []architectureContractRecord
	for _, rel := range generated {
		out = append(out, architectureRecord("observed.generated."+slugify(rel),
			"Generated artifact exists",
			fmt.Sprintf("Repository contains generated artifact %s; treat it as derived evidence unless an authored source declares otherwise.", rel),
			"observed", "positive", "proven", domain, []string{rel}, architectureEvidence{CodePaths: []string{rel}},
			architectureEnforcement{Current: "none", FailureMode: "Derived artifact may be stale unless checked against source.", Severity: "advisory"}))
	}
	for _, doc := range docs {
		if doc.ParseError != "" {
			out = append(out, architectureRecord("observed.parse_error."+slugify(doc.Path),
				"Awareness YAML parse error observed",
				fmt.Sprintf("%s could not be parsed as YAML: %s", doc.Path, doc.ParseError),
				"observed", "safety", "proven", domain, []string{doc.Path}, architectureEvidence{Documentation: []string{doc.Path}},
				architectureEnforcement{Current: "none", FailureMode: "Extraction cannot rely on this document.", Severity: "warning"}))
		}
	}
	return capRecords(out, 80)
}

func buildGovernedArchitectureContracts(root, domain string, docs []architectureDoc, workflows []string) []architectureContractRecord {
	var out []architectureContractRecord
	for _, doc := range docs {
		if doc.Candidate || doc.Generated {
			continue
		}
		for _, e := range doc.Entries {
			switch e.Key {
			case "contracts", "invariants", "failure_modes", "forbidden_fixes", "required_tests":
				id := firstNonEmpty(e.ID, "governed."+slugify(doc.Path)+"."+slugify(e.Title))
				kind := architectureKindForKey(e.Key)
				if e.Key == "forbidden_fixes" {
					kind = "negative"
				}
				statement := firstNonEmpty(e.Statement, e.Title, "Active authored awareness entry exists.")
				rec := architectureRecord(id, firstNonEmpty(e.Title, id), statement,
					"governed", kind, "proven", domain, append([]string{doc.Path}, e.SourceFiles...),
					architectureEvidence{
						Tests:         e.Tests,
						Schemas:       []string{doc.Path + ":" + e.Key},
						CodePaths:     e.SourceFiles,
						Incidents:     filterRefs(e.Refs, "failure", "incident"),
						Documentation: []string{doc.Path},
					},
					architectureEnforcement{Current: governedEnforcement(e.Key), FailureMode: governedFailureMode(e.Key), Severity: governedSeverity(e.Key)})
				rec.Scope.Components = e.Components
				rec.Authority.Owner = "authored awareness corpus"
				rec.Authority.AllowedWriters = []string{"human-reviewed docs/awareness edit", "sensei promote"}
				rec.Authority.DerivedConsumers = []string{"sensei build", "sensei briefing", "sensei preflight", "sensei gate"}
				rec.Authority.ForbiddenWriters = []string{"candidate queue without promotion", "generated extractor output claiming authority"}
				rec.ProofObligations = append(rec.ProofObligations, "Keep the authored awareness entry parseable and included in the graph build.")
				for _, test := range e.Tests {
					rec.ProofObligations = append(rec.ProofObligations, "Run required test "+test)
				}
				out = append(out, rec)
			}
		}
	}
	for _, wf := range workflows {
		if architectureFileContains(root, wf, "gate --enforce") || architectureFileContains(root, wf, "awg gate") || architectureFileContains(root, wf, "sensei gate") {
			rec := architectureRecord("governed.ci."+slugify(wf), "Architectural gate is enforced in CI",
				"Pull-request changes are subject to the repository's configured Sensei/AWG gate before merge.",
				"governed", "safety", "proven", domain, []string{wf},
				architectureEvidence{Gates: []string{wf}, Documentation: []string{wf}},
				architectureEnforcement{Current: "ci_gate", FailureMode: "Gate failure blocks or fails the workflow according to CI configuration.", Severity: "blocking"})
			rec.Scope.Workflows = []string{wf}
			rec.Authority.Owner = "CI workflow"
			rec.Authority.AllowedWriters = []string{"workflow maintainers"}
			rec.Authority.DerivedConsumers = []string{"pull requests", "future coding agents"}
			out = append(out, rec)
		}
	}
	return capRecords(out, 120)
}

func buildInferredArchitectureContracts(root, domain string, docs []architectureDoc, commits []string) []architectureContractRecord {
	var out []architectureContractRecord
	for _, doc := range docs {
		if !doc.Candidate {
			continue
		}
		for _, e := range doc.Entries {
			id := firstNonEmpty(e.ID, "candidate."+slugify(doc.Path)+"."+slugify(e.Title))
			statement := firstNonEmpty(e.Statement, e.Title, "Candidate awareness entry exists but is not active authority.")
			rec := architectureRecord(id, firstNonEmpty(e.Title, id), statement,
				"inferred", architectureKindForKey(e.Key), "medium", domain, append([]string{doc.Path}, e.SourceFiles...),
				architectureEvidence{
					Tests:         e.Tests,
					Schemas:       []string{doc.Path + ":" + e.Key},
					CodePaths:     e.SourceFiles,
					Documentation: []string{doc.Path},
				},
				architectureEnforcement{Current: "convention", FailureMode: "Candidate is review-only; treating it as binding would overstate authority.", Severity: "advisory"})
			rec.Scope.Components = e.Components
			rec.Authority.Owner = "review queue"
			rec.Authority.AllowedWriters = []string{"candidate generator", "human reviewer"}
			rec.Authority.ForbiddenWriters = []string{"automatic promotion"}
			rec.Contradictions = []string{"Candidate status means this is not governed until reviewed and promoted."}
			rec.UnresolvedQuestions = []string{"Has an authorized reviewer accepted this contract and its scope?"}
			rec.ProofObligations = []string{"Review contradictions and source evidence before promotion.", "Add or identify tests that prove the behavior."}
			out = append(out, rec)
		}
	}
	for _, commit := range commits {
		lower := strings.ToLower(commit)
		if strings.Contains(lower, "fix") || strings.Contains(lower, "invariant") || strings.Contains(lower, "contract") || strings.Contains(lower, "gate") {
			rec := architectureRecord("inferred.history."+slugify(commit), "History hints at architectural pressure",
				"Recent commit history contains a repair, invariant, contract, or gate signal. This is directional evidence only, not a binding contract.",
				"inferred", "lifecycle", "low", domain, nil,
				architectureEvidence{Commits: []string{commit}},
				architectureEnforcement{Current: "none", FailureMode: "Commit subjects alone can misrepresent intent.", Severity: "advisory"})
			rec.UnresolvedQuestions = []string{"Do linked PRs, tests, or incidents confirm this as an architectural migration?"}
			out = append(out, rec)
		}
	}
	return capRecords(out, 120)
}

func architectureRecord(id, title, statement, layer, kind, confidence, domain string, files []string, ev architectureEvidence, enf architectureEnforcement) architectureContractRecord {
	return architectureContractRecord{
		ID:        id,
		Title:     title,
		Statement: statement,
		Classification: architectureClassification{
			Layer:      layer,
			Kind:       kind,
			Confidence: confidence,
		},
		Scope: architectureScope{
			Repositories: nonEmptySlice(domain),
			Files:        dedupeSorted(files),
		},
		Authority: architectureAuthority{
			Owner:            "",
			AllowedWriters:   []string{},
			DerivedConsumers: []string{},
			ForbiddenWriters: []string{},
		},
		Evidence:    ev,
		Enforcement: enf,
		History: architectureContractHistory{
			PreviousPattern:   "",
			MigrationSequence: []string{},
			CurrentDirection:  "",
		},
		Contradictions:      []string{},
		Alternatives:        []string{},
		UnresolvedQuestions: []string{},
		ProofObligations:    []string{},
	}
}

func buildArchitectureAuthoritySplits(duplicates map[string][]string, missingRefs []string) []architectureAuthoritySplit {
	var out []architectureAuthoritySplit
	for id, paths := range duplicates {
		out = append(out, architectureAuthoritySplit{
			Subject:        id,
			Classification: "unresolved_authority_split",
			Evidence:       paths,
			Risk:           "The same architectural id appears in multiple files; determine whether this is intentional compatibility, active migration, or duplicate authority.",
		})
	}
	for _, ref := range missingRefs {
		out = append(out, architectureAuthoritySplit{
			Subject:        ref,
			Classification: "likely_defect",
			Evidence:       []string{ref},
			Risk:           "A governed or candidate entry references a source file that is absent from the checkout.",
		})
	}
	if len(out) == 0 {
		out = append(out, architectureAuthoritySplit{
			Subject:        "no mechanically detected duplicate IDs or missing source references",
			Classification: "insufficient_evidence",
			Evidence:       []string{},
			Risk:           "This scan does not prove there are no authority splits; it only found none in parsed awareness YAML.",
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Subject < out[j].Subject })
	return out
}

func buildArchitectureMigrations(commits []string, docs []architectureDoc, workflows []string) []architectureMigration {
	var out []architectureMigration
	if len(workflows) > 0 && docsContainTopKey(docs, "invariants") {
		out = append(out, architectureMigration{
			Domain:             "repository governance",
			OldPattern:         "Architectural rules were only implicit in code, tests, or documentation.",
			CorrectivePressure: "The repository now carries authored awareness YAML and workflow enforcement surfaces.",
			Migration:          matchingCommits(commits, "awareness", "awg", "sensei", "gate", "invariant", "contract"),
			CurrentDirection:   "The repository appears to be moving toward explicit reviewable architectural memory plus gates.",
			Evidence:           append(workflows, docsWithTopKey(docs, "invariants", 3)...),
			Confidence:         confidenceFromEvidence(len(workflows) + len(docsWithTopKey(docs, "invariants", 3))),
		})
	}
	if docsContainTopKey(docs, "failure_modes") || len(matchingCommits(commits, "fix", "regression", "leak", "dropped")) > 1 {
		out = append(out, architectureMigration{
			Domain:             "failure-born rules",
			OldPattern:         "Repairs existed as isolated fixes.",
			CorrectivePressure: "Failure-mode files or repeated repair commits indicate durable lessons are being captured.",
			Migration:          matchingCommits(commits, "fix", "regression", "leak", "dropped", "failure"),
			CurrentDirection:   "The repository appears to be converting repeated failures into named reviewable rules.",
			Evidence:           docsWithTopKey(docs, "failure_modes", 3),
			Confidence:         confidenceFromEvidence(len(docsWithTopKey(docs, "failure_modes", 3)) + len(matchingCommits(commits, "fix", "regression", "leak", "dropped", "failure"))),
		})
	}
	if len(out) == 0 {
		out = append(out, architectureMigration{
			Domain:             "unknown",
			OldPattern:         "",
			CorrectivePressure: "",
			Migration:          []string{},
			CurrentDirection:   "Insufficient evidence to infer a historical migration sequence.",
			Evidence:           []string{},
			Confidence:         "low",
		})
	}
	return out
}

func buildArchitectureDirections(report architectureExtractionReport, commits []string) []architectureDirection {
	var out []architectureDirection
	if len(report.GovernedContractSet) > 0 {
		out = append(out, architectureDirection{
			Domain:                    "contract governance",
			ObservedCondition:         fmt.Sprintf("%d governed awareness/gate records were found.", len(report.GovernedContractSet)),
			HistoricalDirection:       "Recent evidence favors explicit awareness entries and gates over unstated architectural convention.",
			BindingContract:           "Active docs/awareness entries and enforced workflows are binding only within their explicit scope.",
			InferredContract:          "New architectural rules should be proposed as candidates first, then reviewed and promoted.",
			ContradictionOrRisk:       "Authored files may be stale relative to source; this command does not prove freshness.",
			LikelyCoherentMove:        "Extend existing awareness files, required tests, and gates instead of creating an unreviewed parallel authority.",
			ForbiddenOrRegressiveMove: "Treat generated or candidate output as active authority without promotion.",
			ProofObligations:          []string{"Run repository tests and configured Sensei/AWG gates.", "Verify source references and required tests for changed contracts."},
			Evidence:                  firstRecordEvidence(report.GovernedContractSet, 5),
		})
	}
	if len(report.InferredContractSet) > 0 {
		out = append(out, architectureDirection{
			Domain:                    "candidate handling",
			ObservedCondition:         fmt.Sprintf("%d inferred/candidate records were found.", len(report.InferredContractSet)),
			HistoricalDirection:       "Candidate queues preserve uncertain findings without silently making them law.",
			BindingContract:           "No candidate is binding unless promoted into active awareness authority.",
			InferredContract:          "Future extraction should improve citations and tests before promotion.",
			ContradictionOrRisk:       "Some candidates may be stale, duplicated, or contradicted by source.",
			LikelyCoherentMove:        "Review high-confidence candidates with owner approval.",
			ForbiddenOrRegressiveMove: "Auto-promote extracted candidates to governed contracts.",
			ProofObligations:          []string{"Resolve contradictions.", "Identify owner and scope.", "Add or cite behavior-proving tests."},
			Evidence:                  firstRecordEvidence(report.InferredContractSet, 5),
		})
	}
	if len(out) == 0 {
		out = append(out, architectureDirection{
			Domain:                    "unknown",
			ObservedCondition:         "No governed or inferred contracts were extracted.",
			HistoricalDirection:       "Insufficient evidence.",
			BindingContract:           "",
			InferredContract:          "",
			ContradictionOrRisk:       "The repository may not have been imported or may use unsupported evidence formats.",
			LikelyCoherentMove:        "Run sensei import or bootstrap first, then re-run architecture-extract.",
			ForbiddenOrRegressiveMove: "Invent contracts from sparse evidence.",
			ProofObligations:          []string{"Collect authored, generated, test, CI, and history evidence."},
			Evidence:                  []string{},
		})
	}
	return out
}

func buildArchitectureUnknowns(root string, docs []architectureDoc, commits []string) []string {
	unknowns := []string{
		"Pull-request and issue discussions are not inspected by this local command.",
		"Runtime behavior, deployed state, and production incidents are not observed unless already committed as evidence.",
		"Commit subjects are directional hints, not proof of architectural intent.",
		"Documentation is reported as evidence but is not treated as authoritative by itself.",
	}
	if len(docs) == 0 {
		unknowns = append(unknowns, "No docs/awareness or docs/intent YAML was found; governed contract extraction is necessarily sparse.")
	}
	if len(commits) == 0 {
		unknowns = append(unknowns, "Git history was unavailable or disabled; historical migration sequences are incomplete.")
	}
	if _, err := os.Stat(filepath.Join(root, "docs", "awareness", "generated")); err != nil {
		unknowns = append(unknowns, "No docs/awareness/generated directory was found; generated structural evidence may be missing.")
	}
	return unknowns
}

func buildArchitecturePromotions(records []architectureContractRecord) []architecturePromotion {
	var out []architecturePromotion
	for _, rec := range records {
		if rec.Classification.Layer != "inferred" || rec.Evidence.Documentation == nil {
			continue
		}
		if len(rec.Evidence.CodePaths) == 0 && len(rec.Evidence.Tests) == 0 {
			continue
		}
		out = append(out, architecturePromotion{
			ID:       rec.ID,
			Reason:   "Candidate has explicit source or test evidence and may be worth human review.",
			Evidence: append(append([]string{}, rec.Evidence.CodePaths...), rec.Evidence.Tests...),
			ReviewNeeded: []string{
				"Confirm owner and scope.",
				"Check for contradictory governed contracts.",
				"Approve promotion explicitly; do not auto-promote.",
			},
		})
	}
	return capPromotions(out, 30)
}

func buildArchitectureProofs(records []architectureContractRecord, workflows []string) []architectureProof {
	var out []architectureProof
	for _, rec := range records {
		checks := []string{"Keep evidence references resolvable.", "Do not weaken the governed statement without reviewer approval."}
		checks = append(checks, rec.ProofObligations...)
		if len(rec.Evidence.Tests) > 0 {
			checks = append(checks, "Run cited tests: "+strings.Join(rec.Evidence.Tests, ", "))
		}
		out = append(out, architectureProof{Scope: rec.ID, Checks: dedupeSorted(checks)})
	}
	if len(workflows) > 0 {
		out = append(out, architectureProof{Scope: "ci", Checks: []string{"Run the configured CI/Sensei/AWG gate for the modified diff.", "Treat cannot-verify outcomes as blocking when the workflow is enforcing."}})
	}
	return capProofs(out, 80)
}

func renderArchitectureExtractionReport(report architectureExtractionReport, format string) ([]byte, error) {
	switch format {
	case "", "markdown", "md":
		return renderArchitectureExtractionMarkdown(report), nil
	case "json":
		var b bytes.Buffer
		enc := json.NewEncoder(&b)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			return nil, err
		}
		return b.Bytes(), nil
	case "yaml", "yml":
		return yaml.Marshal(report)
	default:
		return nil, fmt.Errorf("--format must be markdown, json, or yaml")
	}
}

func renderArchitectureExtractionMarkdown(report architectureExtractionReport) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "# Architectural Contract Extraction Report\n\n")
	fmt.Fprintf(&b, "- repo: `%s`\n", report.RepoRoot)
	if report.RepoDomain != "" {
		fmt.Fprintf(&b, "- domain: `%s`\n", report.RepoDomain)
	}
	fmt.Fprintf(&b, "- generated_by: `%s`\n- generated_at: `%s`\n\n", report.GeneratedBy, report.GeneratedAt)
	fmt.Fprintf(&b, "This report separates observed facts, inferred candidates, and governed contracts. It does not promote inferred contracts.\n\n")

	fmt.Fprintf(&b, "## 1. Architectural Inventory\n\n")
	renderInventoryItems(&b, "Components", report.ArchitecturalInventory.Components, 30)
	renderInventoryItems(&b, "APIs / Contracts", report.ArchitecturalInventory.APIs, 30)
	renderInventoryItems(&b, "Stores", report.ArchitecturalInventory.Stores, 20)
	renderInventoryItems(&b, "Workflows", report.ArchitecturalInventory.Workflows, 20)
	renderInventoryItems(&b, "Generated Artifacts", report.ArchitecturalInventory.GeneratedArtifacts, 20)
	renderInventoryItems(&b, "Enforcement Surfaces", report.ArchitecturalInventory.EnforcementSurfaces, 20)

	renderRecordSection(&b, "2. Observed Contract Set", report.ObservedContractSet, 25)
	renderRecordSection(&b, "3. Inferred Contract Set", report.InferredContractSet, 25)
	renderRecordSection(&b, "4. Governed Contract Set", report.GovernedContractSet, 25)

	fmt.Fprintf(&b, "## 5. Historical Migration Map\n\n")
	for _, m := range report.HistoricalMigrationMap {
		fmt.Fprintf(&b, "- **%s** (%s): %s -> %s -> %s\n", m.Domain, m.Confidence, m.OldPattern, m.CorrectivePressure, m.CurrentDirection)
		for _, ev := range capStrings(m.Evidence, 5) {
			fmt.Fprintf(&b, "  - evidence: `%s`\n", ev)
		}
	}
	fmt.Fprintf(&b, "\n## 6. Authority-Split Report\n\n")
	for _, split := range report.AuthoritySplitReport {
		fmt.Fprintf(&b, "- **%s** [%s]: %s\n", split.Subject, split.Classification, split.Risk)
		for _, ev := range capStrings(split.Evidence, 5) {
			fmt.Fprintf(&b, "  - evidence: `%s`\n", ev)
		}
	}
	fmt.Fprintf(&b, "\n## 7. Architectural Direction Report\n\n")
	for _, d := range report.ArchitecturalDirectionReport {
		fmt.Fprintf(&b, "### %s\n\n", d.Domain)
		fmt.Fprintf(&b, "Observed condition: %s\n\n", d.ObservedCondition)
		fmt.Fprintf(&b, "Historical direction: %s\n\n", d.HistoricalDirection)
		fmt.Fprintf(&b, "Binding contract: %s\n\n", d.BindingContract)
		fmt.Fprintf(&b, "Inferred contract: %s\n\n", d.InferredContract)
		fmt.Fprintf(&b, "Contradiction or risk: %s\n\n", d.ContradictionOrRisk)
		fmt.Fprintf(&b, "Likely coherent move: %s\n\n", d.LikelyCoherentMove)
		fmt.Fprintf(&b, "Forbidden or regressive move: %s\n\n", d.ForbiddenOrRegressiveMove)
		fmt.Fprintf(&b, "Proof obligations: %s\n\n", strings.Join(d.ProofObligations, "; "))
	}
	fmt.Fprintf(&b, "## 8. Unknowns and Irrecoverable Intent\n\n")
	for _, u := range report.UnknownsAndIrrecoverableIntent {
		fmt.Fprintf(&b, "- %s\n", u)
	}
	fmt.Fprintf(&b, "\n## 9. Recommended Promotion Candidates\n\n")
	if len(report.RecommendedPromotionCandidates) == 0 {
		fmt.Fprintf(&b, "- none found by this conservative scan\n")
	} else {
		for _, p := range report.RecommendedPromotionCandidates {
			fmt.Fprintf(&b, "- **%s**: %s\n", p.ID, p.Reason)
		}
	}
	fmt.Fprintf(&b, "\n## 10. Proof Obligations for Future Agents\n\n")
	for _, p := range capProofs(report.ProofObligationsForFutureAgents, 30) {
		fmt.Fprintf(&b, "- **%s**: %s\n", p.Scope, strings.Join(p.Checks, "; "))
	}
	return []byte(b.String())
}

func renderInventoryItems(b *strings.Builder, title string, items []architectureInventoryItem, limit int) {
	fmt.Fprintf(b, "### %s\n\n", title)
	if len(items) == 0 {
		fmt.Fprintf(b, "- none detected\n\n")
		return
	}
	for _, item := range capInventory(items, limit) {
		fmt.Fprintf(b, "- `%s` (%s)", item.ID, item.Kind)
		if len(item.Evidence) > 0 {
			fmt.Fprintf(b, " — evidence: `%s`", strings.Join(capStrings(item.Evidence, 3), "`, `"))
		}
		fmt.Fprintf(b, "\n")
	}
	if len(items) > limit {
		fmt.Fprintf(b, "- ... %d more\n", len(items)-limit)
	}
	fmt.Fprintf(b, "\n")
}

func renderRecordSection(b *strings.Builder, title string, records []architectureContractRecord, limit int) {
	fmt.Fprintf(b, "## %s\n\n", title)
	if len(records) == 0 {
		fmt.Fprintf(b, "- none detected\n\n")
		return
	}
	for _, rec := range capRecords(records, limit) {
		fmt.Fprintf(b, "- **%s** [%s/%s/%s]\n", rec.ID, rec.Classification.Layer, rec.Classification.Kind, rec.Classification.Confidence)
		fmt.Fprintf(b, "  - statement: %s\n", rec.Statement)
		ev := firstRecordEvidence([]architectureContractRecord{rec}, 5)
		if len(ev) > 0 {
			fmt.Fprintf(b, "  - evidence: `%s`\n", strings.Join(ev, "`, `"))
		}
		if len(rec.Contradictions) > 0 {
			fmt.Fprintf(b, "  - contradictions: %s\n", strings.Join(rec.Contradictions, "; "))
		}
	}
	if len(records) > limit {
		fmt.Fprintf(b, "- ... %d more\n", len(records)-limit)
	}
	fmt.Fprintf(b, "\n")
}

func architectureDuplicateIDs(docs []architectureDoc) map[string][]string {
	ids := map[string][]string{}
	for _, doc := range docs {
		for _, e := range doc.Entries {
			if e.ID != "" {
				ids[e.ID] = append(ids[e.ID], doc.Path)
			}
		}
	}
	out := map[string][]string{}
	for id, paths := range ids {
		paths = dedupeSorted(paths)
		if len(paths) > 1 {
			out[id] = paths
		}
	}
	return out
}

func architectureMissingSourceRefs(root string, docs []architectureDoc) []string {
	var out []string
	for _, doc := range docs {
		for _, e := range doc.Entries {
			for _, src := range e.SourceFiles {
				if src == "" || strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://") {
					continue
				}
				if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(src))); err != nil {
					out = append(out, fmt.Sprintf("%s references missing source %s", doc.Path, src))
				}
			}
		}
	}
	return dedupeSorted(out)
}

func architectureRel(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}

func architectureFileContains(root, rel, substr string) bool {
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(string(raw)), strings.ToLower(substr))
}

func architectureDocsMention(docs []architectureDoc, text string) bool {
	return len(architectureDocsMentioning(docs, text, 1)) > 0
}

func architectureDocsMentioning(docs []architectureDoc, text string, limit int) []string {
	var out []string
	text = strings.ToLower(text)
	for _, doc := range docs {
		for _, e := range doc.Entries {
			if strings.Contains(strings.ToLower(e.Statement+" "+e.Title+" "+strings.Join(e.Refs, " ")), text) {
				out = append(out, doc.Path)
				break
			}
		}
		if len(out) >= limit {
			break
		}
	}
	return dedupeSorted(out)
}

func docsContainTopKey(docs []architectureDoc, key string) bool {
	return len(docsWithTopKey(docs, key, 1)) > 0
}

func docsWithTopKey(docs []architectureDoc, key string, limit int) []string {
	var out []string
	for _, doc := range docs {
		for _, k := range doc.TopKeys {
			if k == key {
				out = append(out, doc.Path)
				break
			}
		}
		if len(out) >= limit {
			break
		}
	}
	return out
}

func matchingCommits(commits []string, terms ...string) []string {
	var out []string
	for _, commit := range commits {
		lower := strings.ToLower(commit)
		for _, term := range terms {
			if strings.Contains(lower, strings.ToLower(term)) {
				out = append(out, commit)
				break
			}
		}
		if len(out) >= 8 {
			break
		}
	}
	return out
}

func confidenceFromEvidence(n int) string {
	switch {
	case n >= 4:
		return "high"
	case n >= 2:
		return "medium"
	default:
		return "low"
	}
}

func architectureKindForKey(key string) string {
	switch key {
	case "forbidden_fixes":
		return "negative"
	case "required_tests":
		return "safety"
	case "failure_modes":
		return "lifecycle"
	case "contracts":
		return "positive"
	default:
		return "positive"
	}
}

func governedEnforcement(key string) string {
	switch key {
	case "required_tests":
		return "test"
	case "forbidden_fixes":
		return "scanner"
	default:
		return "convention"
	}
}

func governedFailureMode(key string) string {
	switch key {
	case "forbidden_fixes":
		return "Known-bad repair is reintroduced."
	case "required_tests":
		return "Required proof is skipped."
	default:
		return "Authored governed rule is bypassed or weakened."
	}
}

func governedSeverity(key string) string {
	switch key {
	case "forbidden_fixes", "required_tests":
		return "blocking"
	default:
		return "warning"
	}
}

func filterRefs(refs []string, prefixes ...string) []string {
	var out []string
	for _, ref := range refs {
		lower := strings.ToLower(ref)
		for _, p := range prefixes {
			if strings.Contains(lower, p) {
				out = append(out, ref)
				break
			}
		}
	}
	return dedupeSorted(out)
}

func firstRecordEvidence(records []architectureContractRecord, limit int) []string {
	var out []string
	for _, rec := range records {
		out = append(out, rec.Evidence.Tests...)
		out = append(out, rec.Evidence.Gates...)
		out = append(out, rec.Evidence.Schemas...)
		out = append(out, rec.Evidence.CodePaths...)
		out = append(out, rec.Evidence.Commits...)
		out = append(out, rec.Evidence.Documentation...)
		if len(out) >= limit {
			break
		}
	}
	return capStrings(dedupeSorted(out), limit)
}

func sortInventory(inv *architectureInventory) {
	sort.Slice(inv.Components, func(i, j int) bool { return inv.Components[i].ID < inv.Components[j].ID })
	sort.Slice(inv.APIs, func(i, j int) bool { return inv.APIs[i].ID < inv.APIs[j].ID })
	sort.Slice(inv.Stores, func(i, j int) bool { return inv.Stores[i].ID < inv.Stores[j].ID })
	sort.Slice(inv.Workflows, func(i, j int) bool { return inv.Workflows[i].ID < inv.Workflows[j].ID })
	sort.Slice(inv.GeneratedArtifacts, func(i, j int) bool { return inv.GeneratedArtifacts[i].ID < inv.GeneratedArtifacts[j].ID })
	sort.Slice(inv.EnforcementSurfaces, func(i, j int) bool { return inv.EnforcementSurfaces[i].ID < inv.EnforcementSurfaces[j].ID })
}

func capStrings(in []string, limit int) []string {
	if limit <= 0 || len(in) <= limit {
		return in
	}
	return in[:limit]
}

func capRecords(in []architectureContractRecord, limit int) []architectureContractRecord {
	if limit <= 0 || len(in) <= limit {
		return in
	}
	return in[:limit]
}

func capInventory(in []architectureInventoryItem, limit int) []architectureInventoryItem {
	if limit <= 0 || len(in) <= limit {
		return in
	}
	return in[:limit]
}

func capPromotions(in []architecturePromotion, limit int) []architecturePromotion {
	if limit <= 0 || len(in) <= limit {
		return in
	}
	return in[:limit]
}

func capProofs(in []architectureProof, limit int) []architectureProof {
	if limit <= 0 || len(in) <= limit {
		return in
	}
	return in[:limit]
}

func nonEmptySlice(s string) []string {
	if strings.TrimSpace(s) == "" {
		return []string{}
	}
	return []string{s}
}

func dedupeSorted(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, value := range in {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
