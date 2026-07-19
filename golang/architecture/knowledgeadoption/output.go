// SPDX-License-Identifier: Apache-2.0

package knowledgeadoption

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/globulario/sensei/golang/architecture/adoption"
	"gopkg.in/yaml.v3"
)

type outputScope struct {
	Repo      string `yaml:"repo"`
	Domain    string `yaml:"domain"`
	SourceSet string `yaml:"source_set"`
	Origin    string `yaml:"origin"`
}

type outputProtects struct {
	Files []string `yaml:"files,omitempty"`
}

type invariantOutput struct {
	ID               string `yaml:"id"`
	Title            string `yaml:"title"`
	Severity         string `yaml:"severity,omitempty"`
	adoption.Receipt `yaml:",inline"`
	Protects         outputProtects `yaml:"protects"`
	RequiredTests    []string       `yaml:"required_tests,omitempty"`
	outputScope      `yaml:",inline"`
}

type failureModeOutput struct {
	ID               string `yaml:"id"`
	Title            string `yaml:"title"`
	Severity         string `yaml:"severity,omitempty"`
	adoption.Receipt `yaml:",inline"`
	Protects         outputProtects `yaml:"protects"`
	RequiredTests    []string       `yaml:"required_tests,omitempty"`
	outputScope      `yaml:",inline"`
}

type forbiddenFixOutput struct {
	ID               string `yaml:"id"`
	Title            string `yaml:"title"`
	Reason           string `yaml:"reason"`
	adoption.Receipt `yaml:",inline"`
	Protects         outputProtects `yaml:"protects"`
	outputScope      `yaml:",inline"`
}

type boundaryOutput struct {
	ID               string `yaml:"id"`
	Name             string `yaml:"name"`
	Description      string `yaml:"description"`
	Kind             string `yaml:"kind"`
	Assertion        string `yaml:"assertion"`
	adoption.Receipt `yaml:",inline"`
	Separates        []string `yaml:"separates"`
	SourceFiles      []string `yaml:"source_files"`
	outputScope      `yaml:",inline"`
}

type decisionOutput struct {
	ID                   string   `yaml:"id"`
	Title                string   `yaml:"title"`
	Rationale            string   `yaml:"rationale"`
	AlternativesRejected []string `yaml:"alternatives_rejected"`
	Assertion            string   `yaml:"assertion"`
	adoption.Receipt     `yaml:",inline"`
	SourceFiles          []string `yaml:"source_files,omitempty"`
	outputScope          `yaml:",inline"`
}

type incidentOutput struct {
	IncidentID          string `yaml:"incident_id"`
	Title               string `yaml:"title"`
	Severity            string `yaml:"severity"`
	EventTimeOrRange    string `yaml:"event_time_or_revision_range"`
	ObservedConsequence string `yaml:"observed_consequence"`
	LinkedFailureMode   string `yaml:"linked_failure_mode"`
	Resolution          string `yaml:"resolution"`
	adoption.Receipt    `yaml:",inline"`
	RelatedFiles        []string `yaml:"related_files,omitempty"`
	outputScope         `yaml:",inline"`
}

type contractOutput struct {
	ID                     string `yaml:"id"`
	Name                   string `yaml:"name"`
	Description            string `yaml:"description"`
	Kind                   string `yaml:"kind"`
	Stability              string `yaml:"stability"`
	ReadOrWrite            string `yaml:"read_or_write"`
	PublicConsumerCategory string `yaml:"public_consumer_category"`
	Assertion              string `yaml:"assertion"`
	adoption.Receipt       `yaml:",inline"`
	ExposedBy              []string `yaml:"exposed_by"`
	Tests                  []string `yaml:"tests"`
	SourceFiles            []string `yaml:"source_files"`
	outputScope            `yaml:",inline"`
}

type contractCandidateOutput struct {
	ID                     string   `yaml:"id"`
	IntentID               string   `yaml:"intent_id"`
	Title                  string   `yaml:"title"`
	Status                 string   `yaml:"status"`
	MissingFields          []string `yaml:"missing_fields"`
	ProviderComponents     []string `yaml:"provider_components,omitempty"`
	PublicConsumerCategory string   `yaml:"public_consumer_category,omitempty"`
	SourceReceipts         []string `yaml:"source_receipts,omitempty"`
}

type invariantFile struct {
	Invariants []invariantOutput `yaml:"invariants"`
}
type failureModeFile struct {
	FailureModes []failureModeOutput `yaml:"failure_modes"`
}
type forbiddenFixFile struct {
	ForbiddenFixes []forbiddenFixOutput `yaml:"forbidden_fixes"`
}
type boundaryFile struct {
	Boundaries []boundaryOutput `yaml:"boundaries"`
}
type decisionFile struct {
	Decisions []decisionOutput `yaml:"decisions"`
}
type contractFile struct {
	Contracts []contractOutput `yaml:"contracts"`
}
type contractCandidateFile struct {
	ContractCandidates []contractCandidateOutput `yaml:"contract_candidates"`
}
type incidentFile struct {
	Incidents []incidentOutput `yaml:"incidents"`
}

func writeBundle(opts Options, evaluations []evaluation) (Result, error) {
	if err := os.MkdirAll(opts.OutputDir, 0o755); err != nil {
		return Result{}, err
	}
	paths := map[string]string{
		"invariants":          filepath.Join(opts.OutputDir, "invariants.yaml"),
		"failure_modes":       filepath.Join(opts.OutputDir, "failure_modes.yaml"),
		"forbidden_fixes":     filepath.Join(opts.OutputDir, "forbidden_fixes.yaml"),
		"boundaries":          filepath.Join(opts.OutputDir, "boundaries.yaml"),
		"decisions":           filepath.Join(opts.OutputDir, "decisions.yaml"),
		"contracts":           filepath.Join(opts.OutputDir, "contracts.yaml"),
		"contract_candidates": filepath.Join(opts.OutputDir, "contract-candidates.yaml"),
		"incidents":           filepath.Join(opts.OutputDir, "incidents.yaml"),
		"adoption_report":     filepath.Join(opts.OutputDir, "adoption-report.yaml"),
	}

	invDoc := invariantFile{Invariants: []invariantOutput{}}
	fmDoc := failureModeFile{FailureModes: []failureModeOutput{}}
	ffDoc := forbiddenFixFile{ForbiddenFixes: []forbiddenFixOutput{}}
	boundaryDoc := boundaryFile{Boundaries: []boundaryOutput{}}
	decisionDoc := decisionFile{Decisions: []decisionOutput{}}
	incidentDoc := incidentFile{Incidents: []incidentOutput{}}
	contractDoc := contractFile{Contracts: []contractOutput{}}
	contractCandidateDoc := contractCandidateFile{ContractCandidates: []contractCandidateOutput{}}
	report := Report{
		SchemaVersion: "1",
		GeneratedBy:   "sensei project reconstruction knowledge adoption",
		Binding: Binding{
			RepositoryDomain:  opts.RepositoryDomain,
			Revision:          opts.Revision,
			GraphDigest:       opts.GraphDigest,
			DecisionTimestamp: opts.DecisionTimestamp,
		},
		Decisions: []CandidateDecision{},
	}
	summaries := map[string]*ClassSummary{}
	for class := range policyByClass {
		summaries[class] = &ClassSummary{Class: class}
	}

	for _, ev := range evaluations {
		summary := summaries[ev.candidate.Class]
		summary.Drafted++
		summary.Candidates++
		if ev.eligible {
			summary.Eligible++
		}
		if ev.contested {
			summary.Contested++
		}
		switch ev.outcome {
		case OutcomeMachineAdopted:
			summary.MachineAdopted++
		case OutcomeRejected:
			summary.Rejected++
		default:
			summary.Staged++
		}
		report.Decisions = append(report.Decisions, CandidateDecision{
			CandidateID: ev.candidate.ID, CandidateClass: ev.candidate.Class + "Candidate",
			CandidatePath: ev.candidate.Path, CandidateSource: ev.candidate.Source, Outcome: ev.outcome, KnowledgeID: ev.knowledgeID,
			PolicyID: ev.policyID, Reasons: ev.reasons, SourceReceipts: ev.sourceReceipts,
			CorroborationKinds: ev.corroborationKinds, MissingFields: ev.missingFields,
		})
		if ev.candidate.Class == ClassContract && ev.outcome != OutcomeMachineAdopted {
			contractCandidateDoc.ContractCandidates = append(contractCandidateDoc.ContractCandidates, contractCandidateOutput{
				ID: ev.candidate.ID, IntentID: ev.candidate.IntentID, Title: outputTitle(ev.candidate),
				Status: ev.outcome, MissingFields: ev.missingFields, ProviderComponents: normalized(ev.candidate.ProviderComponents),
				PublicConsumerCategory: ev.candidate.PublicConsumerCategory, SourceReceipts: ev.sourceReceipts,
			})
		}
		if ev.outcome != OutcomeMachineAdopted {
			continue
		}
		receipt := receiptFor(opts, ev)
		scope := outputScope{Repo: opts.RepositoryDomain, Domain: "repo", SourceSet: "project_reconstruction", Origin: "machine_adoption"}
		title := outputTitle(ev.candidate)
		switch ev.candidate.Class {
		case ClassInvariant:
			invDoc.Invariants = append(invDoc.Invariants, invariantOutput{
				ID: ev.knowledgeID, Title: title, Severity: ev.severity, Receipt: receipt,
				Protects: outputProtects{Files: ev.files}, RequiredTests: ev.tests, outputScope: scope,
			})
		case ClassFailureMode:
			fmDoc.FailureModes = append(fmDoc.FailureModes, failureModeOutput{
				ID: ev.knowledgeID, Title: title, Severity: ev.severity, Receipt: receipt,
				Protects: outputProtects{Files: ev.files}, RequiredTests: ev.tests, outputScope: scope,
			})
		case ClassForbiddenFix:
			ffDoc.ForbiddenFixes = append(ffDoc.ForbiddenFixes, forbiddenFixOutput{
				ID: ev.knowledgeID, Title: title,
				Reason:  "Repository history explicitly rejects this scoped change; consult the adoption report and source receipts before modifying it.",
				Receipt: receipt, Protects: outputProtects{Files: ev.files}, outputScope: scope,
			})
		case ClassBoundary:
			boundaryDoc.Boundaries = append(boundaryDoc.Boundaries, boundaryOutput{
				ID: ev.knowledgeID, Name: title, Description: ev.candidate.Statement, Kind: ev.candidate.Kind,
				Assertion: "inferred", Receipt: receipt, Separates: normalized(ev.candidate.Components),
				SourceFiles: ev.files, outputScope: scope,
			})
		case ClassDecision:
			decisionDoc.Decisions = append(decisionDoc.Decisions, decisionOutput{
				ID: ev.knowledgeID, Title: title, Rationale: ev.candidate.Reason,
				AlternativesRejected: normalized(ev.candidate.Alternatives), Assertion: "history_inferred",
				Receipt: receipt, SourceFiles: ev.files, outputScope: scope,
			})
		case ClassIncident:
			incidentDoc.Incidents = append(incidentDoc.Incidents, incidentOutput{
				IncidentID: ev.knowledgeID, Title: title, Severity: ev.severity,
				EventTimeOrRange: ev.candidate.EventTimeOrRange, ObservedConsequence: ev.candidate.ObservedConsequence,
				LinkedFailureMode: ev.candidate.LinkedFailureMode, Resolution: ev.candidate.Resolution,
				Receipt: receipt, RelatedFiles: ev.files, outputScope: scope,
			})
		case ClassContract:
			contractDoc.Contracts = append(contractDoc.Contracts, contractOutput{
				ID: ev.knowledgeID, Name: title, Description: ev.candidate.Statement,
				Kind: ev.candidate.Interaction, Stability: ev.candidate.Stability, ReadOrWrite: ev.candidate.ReadOrWrite,
				PublicConsumerCategory: ev.candidate.PublicConsumerCategory, Assertion: "deterministic_inference",
				Receipt: receipt, ExposedBy: normalized(ev.candidate.ProviderComponents), Tests: ev.tests,
				SourceFiles: ev.files, outputScope: scope,
			})
		}
	}

	classes := make([]string, 0, len(summaries))
	for class := range summaries {
		classes = append(classes, class)
	}
	sort.Strings(classes)
	for _, class := range classes {
		report.Classes = append(report.Classes, *summaries[class])
	}

	for path, document := range map[string]any{
		paths["invariants"]: invDoc, paths["failure_modes"]: fmDoc,
		paths["forbidden_fixes"]: ffDoc, paths["boundaries"]: boundaryDoc,
		paths["decisions"]:           decisionDoc,
		paths["contracts"]:           contractDoc,
		paths["contract_candidates"]: contractCandidateDoc,
		paths["incidents"]:           incidentDoc,
		paths["adoption_report"]:     report,
	} {
		if err := writeYAML(path, document); err != nil {
			return Result{}, err
		}
	}
	return Result{Report: report, Paths: paths}, nil
}

func receiptFor(opts Options, ev evaluation) adoption.Receipt {
	receipt := adoption.Normalize(adoption.Receipt{
		Status: adoption.PromotionMachineAdopted, PromotionStatus: adoption.PromotionMachineAdopted,
		AssertionOrigin: assertionOrigin(ev.candidate), EpistemicStatus: "supported", ArchitecturalPlane: ev.plane,
		DecisionActor: "sensei.knowledge_adoption", DecisionContext: "project_reconstruction",
		DecisionPolicy: ev.policyID, DecisionTimestamp: opts.DecisionTimestamp,
		ValidForRevision: opts.Revision, ValidForGraphDigest: opts.GraphDigest,
		ReviewStatus: adoption.ReviewNotHumanReviewed, AdoptionBasis: ev.reasons,
		SourceReceipts: ev.sourceReceipts, CorroborationKinds: ev.corroborationKinds,
		RevocationConditions: revocationConditions(ev),
		Limitations:          []string{"machine-adopted knowledge is not human-governed authority"},
	})
	if err := adoption.ValidateMachineAdoption(receipt); err != nil {
		panic(fmt.Sprintf("policy emitted an invalid adoption receipt for %s: %v", ev.candidate.ID, err))
	}
	return receipt
}

func assertionOrigin(c candidate) string {
	if c.Class == ClassBoundary || c.Class == ClassContract {
		return "deterministic_inference"
	}
	return "history_inferred"
}

func revocationConditions(ev evaluation) []string {
	out := append([]string(nil), ev.candidate.InvalidationConditions...)
	out = append(out,
		"repository revision no longer matches valid_for_revision",
		"project reconstruction graph no longer matches valid_for_graph_digest",
		"later governed knowledge contradicts or supersedes this proposition",
	)
	return normalized(out)
}

func outputTitle(c candidate) string {
	if title := strings.TrimSpace(c.Title); title != "" {
		return title
	}
	base := strings.NewReplacer("_", " ", ".", " ", "-", " ").Replace(strings.TrimSpace(c.Theme))
	if base == "" {
		base = strings.TrimPrefix(strings.TrimSpace(c.ID), "candidate.")
	}
	words := strings.Fields(base)
	for i := range words {
		if words[i] != "" {
			words[i] = strings.ToUpper(words[i][:1]) + words[i][1:]
		}
	}
	suffix := map[string]string{
		ClassInvariant: " invariant", ClassFailureMode: " recurring failure mode",
		ClassForbiddenFix: " rejected fix", ClassBoundary: " boundary",
		ClassDecision: " decision", ClassContract: " contract", ClassIncident: " incident",
	}[c.Class]
	return strings.Join(words, " ") + suffix
}

func writeYAML(path string, document any) error {
	raw, err := yaml.Marshal(document)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
