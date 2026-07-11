// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"os"
	"sort"
	"strings"
)

// SARIF v2.1.0 — the minimal shape GitHub code scanning ingests.
type sarifLog struct {
	Schema  string     `json:"$schema"`
	Version string     `json:"version"`
	Runs    []sarifRun `json:"runs"`
}

type sarifRun struct {
	Tool    sarifTool     `json:"tool"`
	Results []sarifResult `json:"results"`
}

type sarifTool struct {
	Driver sarifDriver `json:"driver"`
}

type sarifDriver struct {
	Name           string      `json:"name"`
	InformationURI string      `json:"informationUri"`
	Version        string      `json:"version,omitempty"`
	Rules          []sarifRule `json:"rules"`
}

type sarifRule struct {
	ID                   string      `json:"id"`
	Name                 string      `json:"name,omitempty"`
	ShortDescription     sarifText   `json:"shortDescription"`
	DefaultConfiguration sarifConfig `json:"defaultConfiguration"`
	HelpURI              string      `json:"helpUri,omitempty"`
}

type sarifText struct {
	Text string `json:"text"`
}

type sarifConfig struct {
	Level string `json:"level"`
}

type sarifResult struct {
	RuleID    string          `json:"ruleId"`
	Level     string          `json:"level"`
	Message   sarifText       `json:"message"`
	Locations []sarifLocation `json:"locations"`
}

type sarifLocation struct {
	PhysicalLocation sarifPhysical `json:"physicalLocation"`
}

type sarifPhysical struct {
	ArtifactLocation sarifArtifact `json:"artifactLocation"`
	Region           sarifRegion   `json:"region"`
}

type sarifArtifact struct {
	URI string `json:"uri"`
}

type sarifRegion struct {
	StartLine int `json:"startLine"`
}

// writeGateSARIF renders the gate's file-level findings as a SARIF report. A
// finding whose effective enforcement is "block" becomes an "error"; everything
// else is a "warning". Findings carry no line number (they are file-scoped), so
// results anchor to line 1 of the offending file. An empty findings set still
// writes a valid report — that clears any prior code-scanning alerts.
func writeGateSARIF(path, diff string, findings []fileFinding) error {
	rulesByID := map[string]sarifRule{}
	// Non-nil so an empty findings set marshals to "results": [] (SARIF requires
	// an array; a nil slice would emit null and code scanning rejects it).
	results := make([]sarifResult, 0)

	for _, ff := range findings {
		for _, w := range ff.Warnings {
			level := "warning"
			if w.GetEnforcement() == "block" {
				level = "error"
			}
			rid := w.GetRuleId()
			if rid == "" {
				continue
			}
			short := firstNonEmptyStr(w.GetMessage(), rid)
			if _, seen := rulesByID[rid]; !seen {
				rulesByID[rid] = sarifRule{
					ID:                   rid,
					Name:                 w.GetClass(),
					ShortDescription:     sarifText{Text: short},
					DefaultConfiguration: sarifConfig{Level: level},
					HelpURI:              "https://github.com/globulario/sensei",
				}
			}
			msg := firstNonEmptyStr(w.GetMessage(), rid)
			if d := strings.TrimSpace(w.GetDetail()); d != "" {
				msg += " — " + d
			}
			if p := strings.TrimSpace(w.GetProvenance()); p != "" {
				msg += " (" + p + ")"
			}
			results = append(results, sarifResult{
				RuleID:  rid,
				Level:   level,
				Message: sarifText{Text: msg},
				Locations: []sarifLocation{{
					PhysicalLocation: sarifPhysical{
						ArtifactLocation: sarifArtifact{URI: ff.File},
						Region:           sarifRegion{StartLine: 1},
					},
				}},
			})
		}
	}

	rules := make([]sarifRule, 0, len(rulesByID))
	for _, r := range rulesByID {
		rules = append(rules, r)
	}
	sort.Slice(rules, func(i, j int) bool { return rules[i].ID < rules[j].ID })

	log := sarifLog{
		Schema:  "https://json.schemastore.org/sarif-2.1.0.json",
		Version: "2.1.0",
		Runs: []sarifRun{{
			Tool: sarifTool{Driver: sarifDriver{
				Name:           "Sensei",
				InformationURI: "https://github.com/globulario/sensei",
				Version:        Version,
				Rules:          rules,
			}},
			Results: results,
		}},
	}

	data, err := json.MarshalIndent(log, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func firstNonEmptyStr(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
