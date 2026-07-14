// SPDX-License-Identifier: AGPL-3.0-only

// Package knowledgeadoption evaluates repository-local knowledge candidates
// under closed, code-registered policies. It may stage, machine-adopt, or
// reject; it never creates governed knowledge.
package knowledgeadoption

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/globulario/sensei/golang/architecture/graphsnapshot"
	"github.com/globulario/sensei/golang/rdf"
	"gopkg.in/yaml.v3"
)

const (
	OutcomeStaged         = "staged"
	OutcomeMachineAdopted = "machine_adopted"
	OutcomeRejected       = "rejected"

	ClassInvariant    = "Invariant"
	ClassFailureMode  = "FailureMode"
	ClassForbiddenFix = "ForbiddenFix"
	ClassBoundary     = "Boundary"
	ClassDecision     = "Decision"
	ClassContract     = "Contract"
	ClassIncident     = "Incident"
)

var policyByClass = map[string]string{
	ClassInvariant:    "adoption.invariant.corroborated.v1",
	ClassFailureMode:  "adoption.failure_mode.repeated_surface.v1",
	ClassForbiddenFix: "adoption.forbidden_fix.explicit_rejection.v1",
	ClassBoundary:     "adoption.boundary.compiler_enforced.v1",
	ClassDecision:     "adoption.decision.explicit_rationale.v1",
	ClassContract:     "adoption.contract.structurally_complete.v1",
	ClassIncident:     "adoption.incident.explicit_event.v1",
}

type Options struct {
	RepositoryRoot    string
	RepositoryDomain  string
	CandidatesDir     string
	OutputDir         string
	Revision          string
	GraphDigest       string
	DecisionTimestamp string
	ProvisionalGraph  []byte
}

type Binding struct {
	RepositoryDomain  string `yaml:"repository_domain"`
	Revision          string `yaml:"revision"`
	GraphDigest       string `yaml:"graph_digest_sha256"`
	DecisionTimestamp string `yaml:"decision_timestamp"`
}

type ClassSummary struct {
	Class          string `yaml:"class"`
	Drafted        int    `yaml:"drafted"`
	Candidates     int    `yaml:"candidates"`
	Eligible       int    `yaml:"eligible"`
	Staged         int    `yaml:"staged"`
	MachineAdopted int    `yaml:"machine_adopted"`
	Rejected       int    `yaml:"rejected"`
	Contested      int    `yaml:"contested"`
	Governed       int    `yaml:"governed"`
}

type CandidateDecision struct {
	CandidateID        string   `yaml:"candidate_id"`
	CandidateClass     string   `yaml:"candidate_class"`
	CandidatePath      string   `yaml:"candidate_path"`
	CandidateSource    string   `yaml:"candidate_source"`
	Outcome            string   `yaml:"outcome"`
	KnowledgeID        string   `yaml:"knowledge_id,omitempty"`
	PolicyID           string   `yaml:"policy_id"`
	Reasons            []string `yaml:"reasons,omitempty"`
	MissingFields      []string `yaml:"missing_fields,omitempty"`
	SourceReceipts     []string `yaml:"source_receipts,omitempty"`
	CorroborationKinds []string `yaml:"corroboration_kinds,omitempty"`
}

type Report struct {
	SchemaVersion string              `yaml:"schema_version"`
	GeneratedBy   string              `yaml:"generated_by"`
	Binding       Binding             `yaml:"binding"`
	Classes       []ClassSummary      `yaml:"classes"`
	Decisions     []CandidateDecision `yaml:"decisions"`
	Limitations   []string            `yaml:"limitations,omitempty"`
}

type Result struct {
	Report Report
	Paths  map[string]string
}

type candidate struct {
	ID                     string
	Class                  string
	Path                   string
	Source                 string
	Theme                  string
	Title                  string
	Statement              string
	Reason                 string
	Kind                   string
	Confidence             string
	ConfidenceScore        int
	Severity               string
	SourceReceipts         []string
	Files                  []string
	Tests                  []string
	Components             []string
	Forbids                []string
	Contradictions         []string
	InvalidationConditions []string
	Alternatives           []string
	RequestedPlane         string
	CurrentApplicability   []string
	SupersededBy           string
	EventTimeOrRange       string
	ObservedConsequence    string
	LinkedFailureMode      string
	Resolution             string
	IntentID               string
	IntentMachineAdopted   bool
	ProviderComponents     []string
	PublicConsumerCategory string
	Interaction            string
	ReadOrWrite            string
	Stability              string
}

type evaluation struct {
	candidate          candidate
	outcome            string
	knowledgeID        string
	policyID           string
	reasons            []string
	sourceReceipts     []string
	corroborationKinds []string
	files              []string
	tests              []string
	plane              string
	severity           string
	missingFields      []string
	eligible           bool
	contested          bool
}

// Run evaluates all supported candidates and atomically replaces the project
// knowledge bundle. The same inputs and explicit timestamp produce identical
// bytes.
func Run(opts Options) (Result, error) {
	opts.RepositoryRoot = filepath.Clean(opts.RepositoryRoot)
	if opts.CandidatesDir == "" {
		opts.CandidatesDir = filepath.Join(opts.RepositoryRoot, "docs", "awareness", "candidates")
	}
	if opts.OutputDir == "" {
		opts.OutputDir = filepath.Join(opts.RepositoryRoot, ".sensei", "project", "knowledge")
	}
	candidates, err := loadCandidates(opts)
	if err != nil {
		return Result{}, err
	}
	graphSubjects, graphComponents, err := indexGraph(opts.ProvisionalGraph)
	if err != nil {
		return Result{}, fmt.Errorf("index provisional graph: %w", err)
	}

	bindingComplete := strings.TrimSpace(opts.Revision) != "" && strings.TrimSpace(opts.GraphDigest) != "" && strings.TrimSpace(opts.DecisionTimestamp) != ""
	evaluations := make([]evaluation, 0, len(candidates))
	seen := map[string]string{}
	for _, c := range candidates {
		ev := evaluateCandidate(opts, c, graphSubjects, graphComponents)
		if !bindingComplete && ev.outcome == OutcomeMachineAdopted {
			ev.outcome = OutcomeStaged
			ev.reasons = append(ev.reasons, "machine adoption requires revision, graph digest, and explicit decision timestamp")
		}
		fingerprint := candidateFingerprint(c)
		if prior, ok := seen[ev.knowledgeID]; ok && prior != fingerprint {
			ev.outcome = OutcomeRejected
			ev.reasons = append(ev.reasons, "stable knowledge identity collides with a different proposition")
		} else {
			seen[ev.knowledgeID] = fingerprint
		}
		evaluations = append(evaluations, ev)
	}
	sort.Slice(evaluations, func(i, j int) bool {
		if evaluations[i].candidate.Class != evaluations[j].candidate.Class {
			return evaluations[i].candidate.Class < evaluations[j].candidate.Class
		}
		return evaluations[i].candidate.ID < evaluations[j].candidate.ID
	})

	result, err := writeBundle(opts, evaluations)
	if err != nil {
		return Result{}, err
	}
	if !bindingComplete && len(candidates) > 0 {
		result.Report.Limitations = append(result.Report.Limitations, "adoption binding incomplete; otherwise eligible candidates remained staged")
		if err := writeYAML(result.Paths["adoption_report"], result.Report); err != nil {
			return Result{}, err
		}
	}
	return result, nil
}

func loadCandidates(opts Options) ([]candidate, error) {
	var paths []string
	if err := filepath.WalkDir(opts.CandidatesDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !entry.IsDir() && (strings.HasSuffix(entry.Name(), ".yaml") || strings.HasSuffix(entry.Name(), ".yml")) {
			paths = append(paths, path)
		}
		return nil
	}); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	sort.Strings(paths)
	var out []candidate
	for _, path := range paths {
		base := filepath.Base(path)
		var loaded []candidate
		var err error
		switch base {
		case "invariant_candidates.yaml", "invariant_candidates.yml":
			loaded, err = loadMechanicalInvariants(opts, path)
		case "boundary_candidates.yaml", "boundary_candidates.yml":
			loaded, err = loadBoundaries(opts, path)
		default:
			loaded, err = loadHistoryCandidate(opts, path)
		}
		if err != nil {
			return nil, fmt.Errorf("load %s: %w", path, err)
		}
		out = append(out, loaded...)
	}
	contracts, err := loadContractCandidates(opts)
	if err != nil {
		return nil, err
	}
	out = append(out, contracts...)
	return out, nil
}

type historyCandidate struct {
	ID                     string   `yaml:"id"`
	Class                  string   `yaml:"class"`
	Theme                  string   `yaml:"theme"`
	Title                  string   `yaml:"title"`
	Statement              string   `yaml:"statement"`
	Reason                 string   `yaml:"reason"`
	Confidence             string   `yaml:"confidence"`
	Severity               string   `yaml:"severity"`
	SourcePaths            []string `yaml:"source_paths"`
	InvalidationConditions []string `yaml:"invalidation_conditions"`
	Contradictions         []string `yaml:"contradictions"`
	Choice                 string   `yaml:"choice"`
	SelectedDirection      string   `yaml:"selected_direction"`
	Rationale              string   `yaml:"rationale"`
	AlternativesRejected   []string `yaml:"alternatives_rejected"`
	ArchitecturalPlane     string   `yaml:"architectural_plane"`
	CurrentApplicability   []string `yaml:"current_applicability_evidence"`
	SupersededBy           string   `yaml:"superseded_by"`
	EventTimeOrRange       string   `yaml:"event_time_or_revision_range"`
	ObservedConsequence    string   `yaml:"observed_consequence"`
	LinkedFailureMode      string   `yaml:"linked_failure_mode"`
	Resolution             string   `yaml:"resolution"`
}

func loadHistoryCandidate(opts Options, path string) ([]candidate, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc historyCandidate
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	class := strings.TrimSuffix(strings.TrimSpace(doc.Class), "Candidate")
	if _, ok := policyByClass[class]; !ok || doc.ID == "" {
		return nil, nil
	}
	statement := strings.TrimSpace(doc.Statement)
	if statement == "" {
		statement = strings.TrimSpace(coalesceString(doc.Choice, doc.SelectedDirection))
	}
	if class == ClassInvariant && statement == "" {
		statement = deriveInvariantStatement(doc.Reason)
		if statement != "" && len(doc.InvalidationConditions) == 0 {
			doc.InvalidationConditions = []string{"cited current implementation or Tests no longer support the proposition"}
		}
	}
	base := candidate{
		ID: doc.ID, Class: class, Path: relative(opts.RepositoryRoot, path), Source: "history", Theme: doc.Theme,
		Title: doc.Title, Statement: statement, Reason: coalesceString(doc.Rationale, doc.Reason), Confidence: strings.ToLower(doc.Confidence),
		Severity: doc.Severity, SourceReceipts: doc.SourcePaths, Contradictions: doc.Contradictions,
		InvalidationConditions: doc.InvalidationConditions,
		Alternatives:           doc.AlternativesRejected, RequestedPlane: doc.ArchitecturalPlane,
		CurrentApplicability: doc.CurrentApplicability, SupersededBy: doc.SupersededBy,
		EventTimeOrRange: doc.EventTimeOrRange, ObservedConsequence: doc.ObservedConsequence,
		LinkedFailureMode: doc.LinkedFailureMode, Resolution: doc.Resolution,
	}
	out := []candidate{base}
	if derived, ok := deriveDecisionCandidate(base); ok {
		out = append(out, derived)
	}
	return out, nil
}

type mechanicalInvariantDocument struct {
	Invariants []struct {
		ID         string `yaml:"id"`
		Statement  string `yaml:"statement"`
		Kind       string `yaml:"kind"`
		Status     string `yaml:"status"`
		Confidence struct {
			Level string `yaml:"level"`
			Score int    `yaml:"score"`
		} `yaml:"confidence"`
		Scope struct {
			Files []string `yaml:"files"`
		} `yaml:"scope"`
		Evidence struct {
			Tests   []string `yaml:"tests"`
			Commits []string `yaml:"commits"`
		} `yaml:"evidence"`
		Contradictions   []string `yaml:"contradictions"`
		ProofObligations []string `yaml:"proof_obligations"`
	} `yaml:"invariants"`
}

func loadMechanicalInvariants(opts Options, path string) ([]candidate, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc mechanicalInvariantDocument
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	out := make([]candidate, 0, len(doc.Invariants))
	for _, item := range doc.Invariants {
		receipts := append([]string(nil), item.Scope.Files...)
		for _, file := range item.Scope.Files {
			receipts = append(receipts, "file:"+file)
		}
		for _, test := range item.Evidence.Tests {
			receipts = append(receipts, "test:"+test)
		}
		for _, commit := range item.Evidence.Commits {
			receipts = append(receipts, "commit:"+commit)
		}
		out = append(out, candidate{
			ID: item.ID, Class: ClassInvariant, Path: relative(opts.RepositoryRoot, path), Source: "deterministic", Statement: item.Statement,
			Kind: item.Kind, Confidence: strings.ToLower(item.Confidence.Level), ConfidenceScore: item.Confidence.Score,
			SourceReceipts: receipts, Files: item.Scope.Files, Tests: item.Evidence.Tests,
			Contradictions: item.Contradictions,
		})
	}
	return out, nil
}

type boundaryDocument struct {
	Boundaries []struct {
		ID          string   `yaml:"id"`
		Name        string   `yaml:"name"`
		Kind        string   `yaml:"kind"`
		Description string   `yaml:"description"`
		Separates   []string `yaml:"separates"`
		Forbids     []string `yaml:"forbids"`
		SourceFiles []string `yaml:"source_files"`
	} `yaml:"boundaries"`
}

func loadBoundaries(opts Options, path string) ([]candidate, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc boundaryDocument
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	out := make([]candidate, 0, len(doc.Boundaries))
	for _, item := range doc.Boundaries {
		receipts := make([]string, 0, len(item.SourceFiles))
		for _, file := range item.SourceFiles {
			receipts = append(receipts, "file:"+file)
		}
		out = append(out, candidate{
			ID: item.ID, Class: ClassBoundary, Path: relative(opts.RepositoryRoot, path), Source: "deterministic", Theme: item.Name,
			Title: item.Name, Statement: item.Description, Reason: item.Description, Kind: item.Kind,
			SourceReceipts: receipts, Files: item.SourceFiles, Components: item.Separates, Forbids: item.Forbids,
		})
	}
	return out, nil
}

func evaluateCandidate(opts Options, c candidate, graphSubjects, graphComponents map[string]bool) evaluation {
	ev := evaluation{candidate: c, outcome: OutcomeStaged, policyID: policyByClass[c.Class], knowledgeID: stableKnowledgeID(c)}
	ev.sourceReceipts, ev.corroborationKinds, ev.files, ev.tests, ev.reasons = validateSources(opts.RepositoryRoot, c)
	if c.ID == "" || !rdf.IsStableID(c.ID) {
		ev.outcome = OutcomeRejected
		ev.reasons = append(ev.reasons, "candidate identity is empty or unstable")
		return finalizeEvaluation(ev)
	}
	if graphSubjects[strings.Trim(rdf.MintIRI(classIRI(c.Class), ev.knowledgeID), "<>")] {
		ev.reasons = append(ev.reasons, "project graph already defines this stable knowledge identity")
		return finalizeEvaluation(ev)
	}
	if len(c.Contradictions) > 0 || containsAny(strings.ToLower(c.Reason+" "+c.Statement), "contested", "conflicting evidence") {
		ev.contested = true
		ev.reasons = append(ev.reasons, "candidate carries unresolved contradictory evidence")
		return finalizeEvaluation(ev)
	}

	switch c.Class {
	case ClassInvariant:
		evaluateInvariant(&ev)
	case ClassFailureMode:
		evaluateFailureMode(&ev)
	case ClassForbiddenFix:
		evaluateForbiddenFix(&ev)
	case ClassBoundary:
		evaluateBoundary(&ev, graphComponents)
	case ClassDecision:
		evaluateDecision(&ev)
	case ClassIncident:
		evaluateIncident(&ev)
	case ClassContract:
		evaluateContract(&ev)
	default:
		ev.outcome = OutcomeRejected
		ev.reasons = append(ev.reasons, "no registered adoption policy for candidate class")
	}
	if ev.outcome == OutcomeMachineAdopted {
		ev.eligible = true
	}
	return finalizeEvaluation(ev)
}

func evaluateContract(ev *evaluation) {
	c := ev.candidate
	var missing []string
	if !c.IntentMachineAdopted {
		missing = append(missing, "machine_adopted_intent_receipt")
	}
	if len(c.ProviderComponents) != 1 {
		missing = append(missing, "provider_component")
	}
	if !supportedPublicConsumer(c.PublicConsumerCategory) {
		missing = append(missing, "consumer_component_or_public_category")
	}
	if strings.TrimSpace(c.Interaction) == "" {
		missing = append(missing, "interaction_boundary")
	}
	if strings.TrimSpace(c.Statement) == "" {
		missing = append(missing, "behavioral_guarantee")
	}
	if c.ReadOrWrite != "read" && c.ReadOrWrite != "write" && c.ReadOrWrite != "read_write" {
		missing = append(missing, "read_write_semantics")
	}
	if strings.TrimSpace(c.Stability) == "" {
		missing = append(missing, "stability")
	}
	if len(ev.files) == 0 {
		missing = append(missing, "source_anchors")
	}
	if len(ev.tests) == 0 {
		missing = append(missing, "required_test_or_evidence")
	}
	if len(missing) > 0 {
		ev.missingFields = normalized(missing)
		ev.reasons = append(ev.reasons, "contract-like Intent remains staged because load-bearing structure is incomplete")
		return
	}
	ev.outcome = OutcomeMachineAdopted
	ev.plane = "intended"
	ev.reasons = append(ev.reasons, "machine-adopted contract Intent resolved to a structurally complete public boundary")
}

func supportedPublicConsumer(value string) bool {
	switch strings.TrimSpace(value) {
	case "external Go caller", "net/http integration", "middleware implementation", "Render implementation", "Binding implementation":
		return true
	default:
		return false
	}
}

func evaluateDecision(ev *evaluation) {
	c := ev.candidate
	if strings.TrimSpace(c.Statement) == "" || strings.TrimSpace(c.Reason) == "" || len(c.Alternatives) == 0 {
		ev.reasons = append(ev.reasons, "decision requires an explicit choice, rationale, and rejected alternative")
		return
	}
	if c.SupersededBy != "" {
		ev.reasons = append(ev.reasons, "decision is superseded by "+c.SupersededBy)
		return
	}
	if len(ev.files) == 0 && independentHistoryCount(ev.sourceReceipts) == 0 {
		ev.reasons = append(ev.reasons, "decision scope and repository-local authority source are unresolved")
		return
	}
	plane := strings.ToLower(strings.TrimSpace(c.RequestedPlane))
	if plane == "intended" && len(c.CurrentApplicability) == 0 {
		ev.reasons = append(ev.reasons, "current intended decision lacks current applicability evidence")
		return
	}
	if plane == "" {
		plane = "historical"
	}
	if plane != "historical" && plane != "intended" {
		ev.reasons = append(ev.reasons, "decision plane must be historical or intended")
		return
	}
	ev.outcome = OutcomeMachineAdopted
	ev.plane = plane
	ev.reasons = append(ev.reasons, "explicit repository-local choice preserves rationale and rejected alternative")
}

func evaluateIncident(ev *evaluation) {
	c := ev.candidate
	text := strings.ToLower(c.Statement + " " + c.Reason + " " + c.ObservedConsequence)
	if !containsAny(text, "production regression", "security incident", "outage", "release rollback", "data corruption", "compatibility break", "panic affecting users") {
		ev.reasons = append(ev.reasons, "ordinary bug evidence does not identify an explicit incident event")
		return
	}
	if c.EventTimeOrRange == "" || c.ObservedConsequence == "" || c.LinkedFailureMode == "" || c.Resolution == "" || len(ev.files) == 0 {
		ev.reasons = append(ev.reasons, "incident requires time/revision range, consequence, scope, linked FailureMode, and resolution state")
		return
	}
	ev.outcome = OutcomeMachineAdopted
	ev.plane = "historical"
	ev.severity = strings.ToLower(strings.TrimSpace(c.Severity))
	if ev.severity == "" {
		ev.severity = "unknown"
	}
	ev.reasons = append(ev.reasons, "repository-local evidence explicitly identifies a bounded incident event")
}

func evaluateInvariant(ev *evaluation) {
	c := ev.candidate
	if strings.TrimSpace(c.Statement) == "" {
		ev.reasons = append(ev.reasons, "candidate lacks a standalone precise invariant proposition")
		return
	}
	if c.ConfidenceScore > 0 && c.ConfidenceScore < 80 {
		ev.reasons = append(ev.reasons, "mechanical confidence score is below the corroborated adoption threshold")
		return
	}
	if len(c.InvalidationConditions) == 0 {
		ev.reasons = append(ev.reasons, "invalidation conditions are not explicit")
		return
	}
	kinds := stringSet(ev.corroborationKinds)
	currentPair := kinds["source_file"] && kinds["test"]
	historyCount := independentHistoryCount(ev.sourceReceipts)
	if !currentPair && historyCount < 2 {
		ev.reasons = append(ev.reasons, "invariant lacks current Test plus implementation support or two independent history events")
		return
	}
	if len(ev.files) == 0 {
		ev.reasons = append(ev.reasons, "invariant has no resolving current source anchor")
		return
	}
	ev.outcome = OutcomeMachineAdopted
	if currentPair {
		ev.plane = "enforced"
	} else {
		ev.plane = "historical"
	}
	ev.reasons = append(ev.reasons, "precise invariant met corroboration and invalidation policy")
}

func evaluateFailureMode(ev *evaluation) {
	text := strings.ToLower(ev.candidate.Statement + " " + ev.candidate.Reason)
	if !containsAny(text, "panic", "regression", "failure mode", "revert", "break", "incorrect", "bug", "error") {
		ev.reasons = append(ev.reasons, "candidate describes a vague bug theme rather than a concrete failure")
		return
	}
	if len(ev.files) == 0 {
		ev.reasons = append(ev.reasons, "failure mode has no resolving current source scope")
		return
	}
	if independentHistoryCount(ev.sourceReceipts) < 2 {
		ev.reasons = append(ev.reasons, "failure mode lacks two independent repository history events")
		return
	}
	severity := strings.ToLower(strings.TrimSpace(ev.candidate.Severity))
	if severity == "" {
		switch {
		case containsAny(text, "security", "panic", "data corruption"):
			severity = "high"
		case containsAny(text, "regression", "revert", "break", "incorrect"):
			severity = "medium"
		default:
			ev.reasons = append(ev.reasons, "failure severity is not supported by the candidate evidence")
			return
		}
	}
	ev.outcome = OutcomeMachineAdopted
	ev.plane = "historical"
	ev.severity = severity
	ev.reasons = append(ev.reasons, "concrete failure surface recurs across independent repository history")
}

func evaluateForbiddenFix(ev *evaluation) {
	text := strings.ToLower(ev.candidate.Statement + " " + ev.candidate.Reason)
	if !containsAny(text, "revert", "explicitly reject", "forbidden fix", "known-rejected", "does not close", "don't update", "do not add") {
		ev.reasons = append(ev.reasons, "candidate lacks an explicit revert, rejection, or demonstrated ineffective fix")
		return
	}
	if len(ev.files) == 0 && len(ev.tests) == 0 {
		ev.reasons = append(ev.reasons, "forbidden fix has no resolving current scope")
		return
	}
	if independentHistoryCount(ev.sourceReceipts) == 0 {
		ev.reasons = append(ev.reasons, "forbidden fix has no repository-local rejection event")
		return
	}
	ev.outcome = OutcomeMachineAdopted
	ev.plane = "historical"
	ev.reasons = append(ev.reasons, "explicit repository-local rejection identifies the bad move and its scope")
}

func evaluateBoundary(ev *evaluation, graphComponents map[string]bool) {
	c := ev.candidate
	if c.Kind != "visibility" || !strings.Contains(strings.ToLower(c.Statement), "go internal/") {
		ev.reasons = append(ev.reasons, "boundary is not compiler-enforced Go internal visibility")
		return
	}
	for _, component := range c.Components {
		iri := strings.Trim(rdf.MintIRI(rdf.ClassComponent, component), "<>")
		if !graphComponents[iri] {
			ev.outcome = OutcomeRejected
			ev.reasons = append(ev.reasons, "boundary references missing component "+component)
			return
		}
	}
	if len(c.Components) == 0 || len(ev.files) == 0 {
		ev.outcome = OutcomeRejected
		ev.reasons = append(ev.reasons, "compiler boundary lacks a component or resolving source file")
		return
	}
	ev.outcome = OutcomeMachineAdopted
	ev.plane = "enforced"
	ev.reasons = append(ev.reasons, "Go internal visibility is compiler-enforced and all graph references resolve")
}

func finalizeEvaluation(ev evaluation) evaluation {
	ev.reasons = normalized(ev.reasons)
	ev.sourceReceipts = normalized(ev.sourceReceipts)
	ev.corroborationKinds = normalized(ev.corroborationKinds)
	ev.files = normalized(ev.files)
	ev.tests = normalized(ev.tests)
	ev.missingFields = normalized(ev.missingFields)
	return ev
}

func validateSources(root string, c candidate) (receipts, kinds, files, tests, reasons []string) {
	receipts = normalized(c.SourceReceipts)
	for _, receipt := range receipts {
		kind, value := splitReceipt(receipt)
		switch kind {
		case "file", "source_file":
			path := stripLine(value)
			if regularFile(filepath.Join(root, filepath.FromSlash(path))) {
				files = append(files, path)
				if strings.HasSuffix(path, "_test.go") {
					kinds = append(kinds, "test")
					tests = append(tests, path)
				} else {
					kinds = append(kinds, "source_file")
				}
			} else {
				reasons = append(reasons, "source receipt does not resolve: "+receipt)
			}
		case "test":
			path := stripTestSymbol(value)
			if regularFile(filepath.Join(root, filepath.FromSlash(path))) {
				kinds = append(kinds, "test")
				tests = append(tests, value)
			} else {
				reasons = append(reasons, "test receipt does not resolve: "+receipt)
			}
		case "commit":
			if validCommit(root, value) {
				kinds = append(kinds, "commit")
			} else {
				reasons = append(reasons, "commit receipt does not resolve: "+receipt)
			}
		case "pr", "pull_request_review":
			parts := strings.Split(value, ":")
			if len(parts) >= 1 && numeric(parts[0]) {
				kinds = append(kinds, "pull_request_review")
			} else {
				reasons = append(reasons, "pull request receipt is malformed: "+receipt)
			}
		case "revert", "issue", "documentation", "runtime_evidence", "model_draft", "deterministic_rule":
			kinds = append(kinds, kind)
		default:
			if regularFile(filepath.Join(root, filepath.FromSlash(stripLine(receipt)))) {
				files = append(files, stripLine(receipt))
				kinds = append(kinds, "source_file")
			}
		}
	}
	return
}

func independentHistoryCount(receipts []string) int {
	seen := map[string]bool{}
	for _, receipt := range receipts {
		kind, value := splitReceipt(receipt)
		switch kind {
		case "commit", "revert", "issue":
			seen[kind+":"+value] = true
		case "pr", "pull_request_review":
			parts := strings.Split(value, ":")
			if len(parts) > 0 {
				seen["pr:"+parts[0]] = true
			}
		}
	}
	return len(seen)
}

func indexGraph(raw []byte) (subjects, components map[string]bool, err error) {
	subjects = map[string]bool{}
	components = map[string]bool{}
	if len(bytes.TrimSpace(raw)) == 0 {
		return subjects, components, nil
	}
	triples, err := graphsnapshot.Read(bytes.NewReader(raw))
	if err != nil {
		return nil, nil, err
	}
	for _, triple := range triples {
		subjects[triple.Subject] = true
		if triple.Predicate == rdf.PropType && triple.ObjectIsIRI && triple.Object == rdf.ClassComponent {
			components[triple.Subject] = true
		}
	}
	return subjects, components, nil
}

func stableKnowledgeID(c candidate) string {
	id := strings.TrimPrefix(strings.TrimSpace(c.ID), "candidate.")
	switch c.Class {
	case ClassInvariant:
		if strings.HasPrefix(id, "invariant.") {
			return id
		}
		return "invariant.history." + id
	case ClassFailureMode:
		if strings.HasPrefix(id, "failure_mode.") {
			return id
		}
		return "failure_mode.history." + id
	case ClassForbiddenFix:
		if strings.HasPrefix(id, "forbidden_fix.") {
			return id
		}
		return "forbidden_fix.history." + id
	case ClassBoundary:
		return id
	case ClassDecision:
		if strings.HasPrefix(id, "decision.") {
			return id
		}
		return "decision.history." + id
	case ClassIncident:
		if strings.HasPrefix(id, "incident.") {
			return id
		}
		return "incident.history." + id
	case ClassContract:
		if strings.HasPrefix(id, "contract.") {
			return id
		}
		return "contract.materialized." + id
	default:
		return id
	}
}

func candidateFingerprint(c candidate) string {
	canonical := strings.Join([]string{c.Class, c.ID, c.Statement, c.Reason, strings.Join(normalized(c.Files), "\x00")}, "\x1f")
	sum := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(sum[:])
}

func classIRI(class string) string {
	switch class {
	case ClassInvariant:
		return rdf.ClassInvariant
	case ClassFailureMode:
		return rdf.ClassFailureMode
	case ClassForbiddenFix:
		return rdf.ClassForbiddenFix
	case ClassBoundary:
		return rdf.ClassBoundary
	case ClassDecision:
		return rdf.ClassDecision
	case ClassContract:
		return rdf.ClassContract
	case ClassIncident:
		return rdf.ClassIncident
	default:
		return rdf.AwNS + class
	}
}

func deriveInvariantStatement(reason string) string {
	lower := strings.ToLower(reason)
	markers := []string{"the invariant is that ", "ground the invariant that ", "this is an invariant on "}
	for _, marker := range markers {
		idx := strings.Index(lower, marker)
		if idx < 0 {
			continue
		}
		statement := strings.TrimSpace(reason[idx+len(marker):])
		if colon := strings.Index(statement, ":"); marker == "this is an invariant on " && colon >= 0 {
			statement = strings.TrimSpace(statement[colon+1:])
		}
		for _, stop := range []string{" Grounded in", " Together these", " This is architectural", " Confidence is"} {
			if end := strings.Index(statement, stop); end >= 0 {
				statement = statement[:end]
			}
		}
		if end := strings.Index(statement, ". "); end >= 0 {
			statement = statement[:end+1]
		}
		return strings.TrimSpace(statement)
	}
	return ""
}

func deriveDecisionCandidate(source candidate) (candidate, bool) {
	if source.Class != ClassForbiddenFix {
		return candidate{}, false
	}
	lower := strings.ToLower(source.Reason)
	choiceMarker := "decision to "
	choiceAt := strings.Index(lower, choiceMarker)
	rejectMarker := "explicitly rejects "
	rejectAt := strings.Index(lower, rejectMarker)
	if choiceAt < 0 || rejectAt < 0 {
		return candidate{}, false
	}
	choice := strings.TrimSpace(source.Reason[choiceAt+len(choiceMarker):])
	if end := strings.Index(strings.ToLower(choice), " rather than"); end >= 0 {
		choice = strings.TrimSpace(choice[:end])
	} else if end := strings.Index(choice, "."); end >= 0 {
		choice = strings.TrimSpace(choice[:end])
	}
	rejected := strings.TrimSpace(source.Reason[rejectAt+len(rejectMarker):])
	for _, stop := range []string{", reasoning", ", because", "."} {
		if end := strings.Index(strings.ToLower(rejected), stop); end >= 0 {
			rejected = strings.TrimSpace(rejected[:end])
			break
		}
	}
	if choice == "" || rejected == "" || normalizedProposition(choice) == normalizedProposition(rejected) {
		return candidate{}, false
	}
	return candidate{
		ID: "candidate.decision_choice." + strings.TrimPrefix(source.ID, "candidate."), Class: ClassDecision,
		Path: source.Path, Source: "history", Theme: source.Theme, Title: "Historical decision for " + source.Theme,
		Statement: choice, Reason: source.Reason, Alternatives: []string{rejected}, RequestedPlane: "historical",
		Confidence: source.Confidence, SourceReceipts: source.SourceReceipts, Files: source.Files,
	}, true
}

func normalizedProposition(value string) string {
	return strings.Join(strings.Fields(strings.ToLower(value)), " ")
}

func coalesceString(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func splitReceipt(receipt string) (string, string) {
	kind, value, ok := strings.Cut(strings.TrimSpace(receipt), ":")
	if !ok {
		return "", strings.TrimSpace(receipt)
	}
	return strings.ToLower(strings.TrimSpace(kind)), strings.TrimSpace(value)
}

func stripLine(path string) string {
	path = strings.TrimSpace(path)
	if idx := strings.LastIndex(path, ":"); idx >= 0 {
		if _, err := strconv.Atoi(path[idx+1:]); err == nil {
			return path[:idx]
		}
	}
	return path
}

func stripTestSymbol(value string) string {
	if idx := strings.Index(value, ":"); idx >= 0 {
		return value[:idx]
	}
	return value
}

func validCommit(root, revision string) bool {
	revision = strings.TrimSpace(revision)
	if revision == "" {
		return false
	}
	return exec.Command("git", "-C", root, "cat-file", "-e", revision+"^{commit}").Run() == nil
}

func numeric(value string) bool {
	_, err := strconv.Atoi(value)
	return err == nil
}

func regularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func containsAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}

func normalized(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, raw := range in {
		value := strings.TrimSpace(raw)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func stringSet(in []string) map[string]bool {
	out := make(map[string]bool, len(in))
	for _, value := range in {
		out[value] = true
	}
	return out
}

func relative(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}
