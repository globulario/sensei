// SPDX-License-Identifier: Apache-2.0

// @awareness namespace=globular.awareness_graph
// @awareness component=cmd.awg.runtime_evidence
// @awareness file_role=runtime_evidence_schema_and_manifest_validation_phase1

// Runtime evidence lane — Phase 1: schema + manifest validation.
//
// AWG is a GENERIC repair-governance sidecar. It must not hard-link to any
// platform's runtime services (no Globular RPCs, protobufs, or service names in
// core). Instead AWG core owns a normalized runtime-evidence vocabulary — a set
// of LANES with freshness + authority metadata — and a runtime ADAPTER manifest
// that a platform supplies to map its own surfaces onto those lanes. AWG core
// validates the shapes; a platform adapter (Globular first, out of core) fills
// them in.
//
//	platform → adapter → runtime-evidence snapshot → AWG diagnosis/repair → gate
//
// This file is Phase 1 of that lane: the generic schema (runtime-evidence/v1,
// runtime-adapter/v1) and structural validation of both, with NO platform
// hard-link. Diagnosis verdicts (Phase 3), repair reports (Phase 4), gates
// (Phase 5), and the Globular adapter (Phase 2) are deliberately NOT here.
package main

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	runtimeAdapterSchemaVersion  = "runtime-adapter/v1"
	runtimeEvidenceSchemaVersion = "runtime-evidence/v1"
)

// canonicalLanes is the generic runtime-evidence vocabulary AWG core owns. A
// platform adapter maps its surfaces onto these; AWG never learns the platform's
// service names. (desired_state..performance are evidence lanes; freshness and
// authority are also accepted as lane keys though they are usually per-lane
// metadata — both forms appear in the spec.)
var canonicalLanes = map[string]bool{
	"desired_state":    true,
	"observed_state":   true,
	"runtime_identity": true,
	"diagnosis":        true,
	"health":           true,
	"topology":         true,
	"quorum":           true,
	"release_state":    true,
	"action_trace":     true,
	"performance":      true,
	"freshness":        true,
	"authority":        true,
}

// authorityLevels are the only authority levels a lane may declare.
var authorityLevels = map[string]bool{
	"owner": true, "diagnostic": true, "derived": true,
	"advisory": true, "cache": true, "unknown": true,
}

// freshnessModes are the only freshness states a snapshot lane may report.
var freshnessModes = map[string]bool{
	"fresh": true, "stale": true, "cache_only": true,
	"unknown": true, "unavailable": true,
}

// finding is one structural validation result. Error blocks; Warning is advisory.
type finding struct {
	Severity string // "error" | "warning"
	Where    string // lane or field
	Message  string
}

func sortFindings(fs []finding) {
	sort.Slice(fs, func(i, j int) bool {
		if fs[i].Severity != fs[j].Severity {
			return fs[i].Severity < fs[j].Severity // error < warning
		}
		return fs[i].Where < fs[j].Where
	})
}

func hasErrors(fs []finding) bool {
	for _, f := range fs {
		if f.Severity == "error" {
			return true
		}
	}
	return false
}

// ── runtime-adapter/v1 manifest ──────────────────────────────────────────────

type runtimeAdapterManifest struct {
	SchemaVersion string `yaml:"schema_version"`
	Adapter       struct {
		Name     string `yaml:"name"`
		Platform string `yaml:"platform"`
	} `yaml:"adapter"`
	Lanes map[string]runtimeAdapterLane `yaml:"lanes"`
}

type runtimeAdapterLane struct {
	Provider          string      `yaml:"provider"`
	Source            string      `yaml:"source"`
	Authority         string      `yaml:"authority"`
	FreshnessRequired interface{} `yaml:"freshness_required"` // bool OR "when_*" condition string
}

// validateRuntimeAdapterManifest checks the manifest's SHAPE only — never the
// platform semantics. A platform adapter is well-formed when every declared lane
// is a known AWG lane mapped to a provider+source with a valid authority level
// and a freshness_required declaration. AWG core stays platform-agnostic: it
// does not care WHAT the provider is, only that the mapping is declared.
func validateRuntimeAdapterManifest(m runtimeAdapterManifest) []finding {
	var fs []finding
	if m.SchemaVersion != runtimeAdapterSchemaVersion {
		fs = append(fs, finding{"error", "schema_version",
			fmt.Sprintf("schema_version=%q, want %q", m.SchemaVersion, runtimeAdapterSchemaVersion)})
	}
	if strings.TrimSpace(m.Adapter.Name) == "" {
		fs = append(fs, finding{"error", "adapter.name", "adapter.name is required"})
	}
	if strings.TrimSpace(m.Adapter.Platform) == "" {
		fs = append(fs, finding{"error", "adapter.platform", "adapter.platform is required"})
	}
	if len(m.Lanes) == 0 {
		fs = append(fs, finding{"error", "lanes", "at least one evidence lane must be declared"})
	}
	for name, lane := range m.Lanes {
		where := "lanes." + name
		if !canonicalLanes[name] {
			fs = append(fs, finding{"error", where,
				fmt.Sprintf("%q is not a recognized AWG evidence lane (a platform may not invent lanes)", name)})
		}
		if strings.TrimSpace(lane.Provider) == "" {
			fs = append(fs, finding{"error", where + ".provider", "provider is required (which platform surface supplies this lane)"})
		}
		if strings.TrimSpace(lane.Source) == "" {
			fs = append(fs, finding{"error", where + ".source", "source is required (the surface/RPC the adapter calls)"})
		}
		if !authorityLevels[lane.Authority] {
			fs = append(fs, finding{"error", where + ".authority",
				fmt.Sprintf("authority=%q is not a valid level (owner|diagnostic|derived|advisory|cache|unknown)", lane.Authority)})
		}
		if !validFreshnessRequired(lane.FreshnessRequired) {
			fs = append(fs, finding{"error", where + ".freshness_required",
				"freshness_required must be true, false, or a \"when_*\" condition string"})
		}
	}
	sortFindings(fs)
	return fs
}

// validFreshnessRequired accepts a bool, or a string "true"/"false"/"when_*".
func validFreshnessRequired(v interface{}) bool {
	switch t := v.(type) {
	case bool:
		return true
	case string:
		s := strings.TrimSpace(t)
		return s == "true" || s == "false" || strings.HasPrefix(s, "when_")
	default:
		return false
	}
}

// ── runtime-evidence/v1 snapshot ─────────────────────────────────────────────

type runtimeEvidenceSnapshot struct {
	SchemaVersion string `yaml:"schema_version"`
	Platform      string `yaml:"platform"`
	GeneratedAt   string `yaml:"generated_at"`
	Subject       struct {
		Type string `yaml:"type"`
		ID   string `yaml:"id"`
		Node string `yaml:"node"`
	} `yaml:"subject"`
	Lanes         map[string]runtimeEvidenceLane `yaml:"lanes"`
	VerdictInputs struct {
		RequiredLanes []string `yaml:"required_lanes"`
		MissingLanes  []string `yaml:"missing_lanes"`
		StaleLanes    []string `yaml:"stale_lanes"`
	} `yaml:"verdict_inputs"`
}

type runtimeEvidenceLane struct {
	Status     string                 `yaml:"status"`
	Freshness  string                 `yaml:"freshness"`
	Owner      string                 `yaml:"owner"`
	Source     string                 `yaml:"source"`
	ObservedAt string                 `yaml:"observed_at"`
	Facts      map[string]interface{} `yaml:"facts"`    // lane payload (Phase 3 reads these)
	Findings   []laneFinding          `yaml:"findings"` // diagnosis-lane structured findings
}

type laneFinding struct {
	ID       string `yaml:"id"`
	Severity string `yaml:"severity"`
	Summary  string `yaml:"summary"`
}

// validateRuntimeSnapshot checks a normalized snapshot's SHAPE. Verdict rules
// (stale-cannot-prove-repair, unknown-must-not-green, missing-owner-blocks) are
// Phase 3+; Phase 1 only enforces that the evidence is well-formed and labels
// its freshness and authority honestly.
func validateRuntimeSnapshot(s runtimeEvidenceSnapshot) []finding {
	var fs []finding
	if s.SchemaVersion != runtimeEvidenceSchemaVersion {
		fs = append(fs, finding{"error", "schema_version",
			fmt.Sprintf("schema_version=%q, want %q", s.SchemaVersion, runtimeEvidenceSchemaVersion)})
	}
	if strings.TrimSpace(s.Platform) == "" {
		fs = append(fs, finding{"error", "platform", "platform is required"})
	}
	if strings.TrimSpace(s.GeneratedAt) == "" {
		fs = append(fs, finding{"error", "generated_at", "generated_at is required"})
	}
	if strings.TrimSpace(s.Subject.ID) == "" {
		fs = append(fs, finding{"error", "subject.id", "subject.id is required"})
	}
	if len(s.Lanes) == 0 {
		fs = append(fs, finding{"error", "lanes", "a snapshot must carry at least one evidence lane"})
	}
	for name, lane := range s.Lanes {
		where := "lanes." + name
		if !canonicalLanes[name] {
			fs = append(fs, finding{"error", where, fmt.Sprintf("%q is not a recognized AWG evidence lane", name)})
		}
		if !freshnessModes[lane.Freshness] {
			fs = append(fs, finding{"error", where + ".freshness",
				fmt.Sprintf("freshness=%q is not valid (fresh|stale|cache_only|unknown|unavailable)", lane.Freshness)})
		}
		// Authority-bearing evidence must name its owner; unknown freshness must
		// not masquerade as proof later, so an owner is required to anchor it.
		if strings.TrimSpace(lane.Owner) == "" {
			fs = append(fs, finding{"error", where + ".owner",
				"owner is required (authority anchor); a lane with no owner cannot support an authority-sensitive verdict"})
		}
		if strings.TrimSpace(lane.ObservedAt) == "" {
			fs = append(fs, finding{"warning", where + ".observed_at",
				"observed_at missing — freshness cannot be re-derived; treat as unknown"})
		}
	}
	sortFindings(fs)
	return fs
}

// ── command surface ──────────────────────────────────────────────────────────

func runRuntimeAdapter(args []string) int {
	if len(args) >= 1 && args[0] == "validate" {
		return runRuntimeValidate(args[1:], "runtime-adapter validate", "manifest", func(b []byte) ([]finding, error) {
			var m runtimeAdapterManifest
			if err := yaml.Unmarshal(b, &m); err != nil {
				return nil, err
			}
			return validateRuntimeAdapterManifest(m), nil
		})
	}
	fmt.Fprintln(os.Stderr, "usage: awg runtime-adapter validate --manifest <file.yaml> [--json]")
	return 2
}

func runRuntimeSnapshot(args []string) int {
	if len(args) >= 1 && args[0] == "validate" {
		return runRuntimeValidate(args[1:], "runtime-snapshot validate", "in", func(b []byte) ([]finding, error) {
			var s runtimeEvidenceSnapshot
			if err := yaml.Unmarshal(b, &s); err != nil {
				return nil, err
			}
			return validateRuntimeSnapshot(s), nil
		})
	}
	fmt.Fprintln(os.Stderr, "usage: awg runtime-snapshot validate --in <snapshot.yaml> [--json]\n"+
		"  (Phase 1: schema validation only; collection from a platform adapter is Phase 2.)")
	return 2
}

// runRuntimeValidate is the shared validate-a-file flow for both schema kinds.
func runRuntimeValidate(args []string, cmdName, pathFlag string, validate func([]byte) ([]finding, error)) int {
	fs := flag.NewFlagSet("awg "+cmdName, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	path := fs.String(pathFlag, "", "path to the YAML file to validate (required)")
	asJSON := fs.Bool("json", false, "machine-readable summary")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *path == "" {
		fmt.Fprintf(os.Stderr, "awg %s: --%s <file> is required\n", cmdName, pathFlag)
		return 2
	}
	raw, err := os.ReadFile(*path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "awg %s: read %s: %v\n", cmdName, *path, err)
		return 1
	}
	findings, err := validate(raw)
	if err != nil {
		fmt.Fprintf(os.Stderr, "awg %s: parse %s: %v\n", cmdName, *path, err)
		return 1
	}
	errs := 0
	warns := 0
	for _, f := range findings {
		if f.Severity == "error" {
			errs++
		} else {
			warns++
		}
	}
	if *asJSON {
		fmt.Printf("{\"file\":%q,\"errors\":%d,\"warnings\":%d,\"valid\":%t}\n", *path, errs, warns, errs == 0)
	} else {
		for _, f := range findings {
			fmt.Printf("  %-7s %s: %s\n", strings.ToUpper(f.Severity), f.Where, f.Message)
		}
		if errs == 0 {
			fmt.Printf("awg %s: %s is a valid %s (%d warning(s))\n", cmdName, *path, runtimeSchemaLabel(cmdName), warns)
		} else {
			fmt.Printf("awg %s: %s is INVALID — %d error(s), %d warning(s)\n", cmdName, *path, errs, warns)
		}
	}
	if errs > 0 {
		return 1
	}
	return 0
}

func runtimeSchemaLabel(cmdName string) string {
	if strings.HasPrefix(cmdName, "runtime-adapter") {
		return runtimeAdapterSchemaVersion + " adapter manifest"
	}
	return runtimeEvidenceSchemaVersion + " evidence snapshot"
}
