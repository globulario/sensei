// SPDX-License-Identifier: Apache-2.0

package governedmutation

import (
	"bytes"
	"strings"

	yaml "gopkg.in/yaml.v3"

	"github.com/globulario/sensei/golang/propose"
)

// ── canonical governed-record write shapes (ordered, omitempty) ─────────────

type recordFiles struct {
	Files []string `yaml:"files,omitempty"`
}

type failureModeRecord struct {
	ID                string       `yaml:"id"`
	Title             string       `yaml:"title"`
	Severity          string       `yaml:"severity,omitempty"`
	Description       string       `yaml:"description,omitempty"`
	Protects          *recordFiles `yaml:"protects,omitempty"`
	RelatedInvariants []string     `yaml:"related_invariants,omitempty"`
	RequiredTests     []string     `yaml:"required_tests,omitempty"`
	ForbiddenFix      []string     `yaml:"forbidden_fix,omitempty"`
	Evidence          []string     `yaml:"evidence,omitempty"`
	Contract          string       `yaml:"contract,omitempty"`
}

type invariantRecord struct {
	ID                  string       `yaml:"id"`
	Title               string       `yaml:"title"`
	Severity            string       `yaml:"severity,omitempty"`
	Status              string       `yaml:"status"`
	Description         string       `yaml:"description,omitempty"`
	Protects            *recordFiles `yaml:"protects,omitempty"`
	RelatedFailureModes []string     `yaml:"related_failure_modes,omitempty"`
	RelatedInvariants   []string     `yaml:"related_invariants,omitempty"`
	ForbiddenFixes      []string     `yaml:"forbidden_fixes,omitempty"`
	RequiredTests       []string     `yaml:"required_tests,omitempty"`
	Evidence            []string     `yaml:"evidence,omitempty"`
	Contract            string       `yaml:"contract,omitempty"`
}

type requiredTestProtects struct {
	Invariants   []string `yaml:"invariants,omitempty"`
	FailureModes []string `yaml:"failure_modes,omitempty"`
	Files        []string `yaml:"files,omitempty"`
}

type requiredTestRecord struct {
	ID       string               `yaml:"id"`
	Title    string               `yaml:"title"`
	Protects requiredTestProtects `yaml:"protects"`
}

type forbiddenFixRecord struct {
	ID                string       `yaml:"id"`
	Title             string       `yaml:"title"`
	Summary           string       `yaml:"summary,omitempty"`
	Protects          *recordFiles `yaml:"protects,omitempty"`
	RelatedInvariants []string     `yaml:"related_invariants,omitempty"`
	Reason            string       `yaml:"reason,omitempty"`
	Evidence          []string     `yaml:"evidence,omitempty"`
	Contract          string       `yaml:"contract,omitempty"`
}

type contractUnknownRecord struct {
	ID               string   `yaml:"id"`
	Kind             string   `yaml:"kind"`
	Title            string   `yaml:"title"`
	Description      string   `yaml:"description,omitempty"`
	ProposedContract string   `yaml:"proposed_contract,omitempty"`
	RevisionRequest  string   `yaml:"revision_request,omitempty"`
	SourceFiles      []string `yaml:"source_files,omitempty"`
	Evidence         []string `yaml:"evidence,omitempty"`
	Domain           string   `yaml:"domain,omitempty"`
}

type decisionRecord struct {
	ID                 string   `yaml:"id"`
	Title              string   `yaml:"title"`
	Status             string   `yaml:"status"`
	ArchitecturalPlane string   `yaml:"architectural_plane,omitempty"`
	Rationale          string   `yaml:"rationale,omitempty"`
	Context            string   `yaml:"context,omitempty"`
	Consequences       string   `yaml:"consequences,omitempty"`
	RelatedInvariants  []string `yaml:"related_invariants,omitempty"`
	DefinesBoundaries  []string `yaml:"defines_boundaries,omitempty"`
	DefinesContracts   []string `yaml:"defines_contracts,omitempty"`
	AffectsComponents  []string `yaml:"affects_components,omitempty"`
	Mitigates          []string `yaml:"mitigates,omitempty"`
	Rejects            []string `yaml:"rejects,omitempty"`
	SupportedEvidence  []string `yaml:"supported_by_evidence,omitempty"`
	SourceFiles        []string `yaml:"source_files,omitempty"`
}

// buildCanonicalItem builds the canonical write shape for a governed kind (or a
// contract_unknown candidate). Returns nil for an unknown kind.
func buildCanonicalItem(p propose.Request, id string) interface{} {
	files := protectsOrNil(p.SourceFiles)
	switch p.Kind {
	case "failure_mode":
		return failureModeRecord{
			ID: id, Title: p.Title, Severity: p.Severity, Description: p.Description,
			Protects: files, RelatedInvariants: p.RelatedInvariants, RequiredTests: p.RequiredTests,
			ForbiddenFix: p.ForbiddenFixes, Evidence: p.Evidence, Contract: p.Contract,
		}
	case "invariant":
		return invariantRecord{
			ID: id, Title: p.Title, Severity: p.Severity, Status: "active", Description: p.Description,
			Protects: files, RelatedFailureModes: p.RelatedFailures, RelatedInvariants: p.RelatedInvariants,
			ForbiddenFixes: p.ForbiddenFixes, RequiredTests: p.RequiredTests, Evidence: p.Evidence, Contract: p.Contract,
		}
	case "required_test":
		return requiredTestRecord{
			ID: id, Title: p.Title,
			Protects: requiredTestProtects{Invariants: p.RelatedInvariants, FailureModes: p.RelatedFailures, Files: p.SourceFiles},
		}
	case "forbidden_fix":
		return forbiddenFixRecord{
			ID: id, Title: p.Title, Summary: p.Description, Protects: files,
			RelatedInvariants: p.RelatedInvariants, Reason: firstNonEmpty(p.Contract, p.Description),
			Evidence: p.Evidence, Contract: p.Contract,
		}
	case "decision":
		status := p.Status
		if strings.TrimSpace(status) == "" {
			status = "accepted"
		}
		return decisionRecord{
			ID: id, Title: p.Title, Status: status, ArchitecturalPlane: p.ArchitecturalPlane,
			Rationale: p.Description, Context: p.Context, Consequences: p.Consequences,
			RelatedInvariants: p.RelatedInvariants, DefinesBoundaries: p.DefinesBoundaries,
			DefinesContracts: p.DefinesContracts, AffectsComponents: p.AffectsComponents,
			Mitigates: p.RelatedFailures, Rejects: p.ForbiddenFixes,
			SupportedEvidence: p.SupportedEvidence, SourceFiles: p.SourceFiles,
		}
	case "contract_unknown":
		return contractUnknownRecord{
			ID: id, Kind: "contract_unknown", Title: p.Title, Description: p.Description,
			ProposedContract: p.ProposedContract, RevisionRequest: p.RevisionRequest,
			SourceFiles: p.SourceFiles, Evidence: p.Evidence, Domain: p.Domain,
		}
	}
	return nil
}

func protectsOrNil(files []string) *recordFiles {
	if len(files) == 0 {
		return nil
	}
	return &recordFiles{Files: files}
}

// renderItem marshals one record as a 2-space-indented YAML list item, producing
// minimal, human-reviewable diffs.
func renderItem(item interface{}) (string, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(item); err != nil {
		return "", err
	}
	_ = enc.Close()
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	var b strings.Builder
	for i, ln := range lines {
		if i == 0 {
			b.WriteString("  - " + ln + "\n")
		} else {
			b.WriteString("    " + ln + "\n")
		}
	}
	return b.String(), nil
}

// itemAsMap renders a record to canonical YAML then decodes it to a map, so its
// body can be semantically compared to an on-disk entry regardless of key order.
func itemAsMap(item interface{}) (map[string]any, error) {
	raw, err := yaml.Marshal(item)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := yaml.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	return m, nil
}
