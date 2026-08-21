// SPDX-License-Identifier: AGPL-3.0-only

// Command eval-arms runs the landed evaluation arms of issue #131 section 5
// over the architectural mutant suite and writes their reports.
//
// It exists because the arms were reachable only from Go tests, and #131's
// completion evidence asks for reports that are reproducible and
// content-addressed — which a test that prints to a log is not. This produces
// the artifacts: each arm's report as canonical JSON, its SHA-256, and a
// summary index binding both to the exact revision the run was made at.
//
// It runs ONLY the arms that can run offline today. Arm 3 needs a bound model,
// arm 4 needs a published graph the mutant domain cannot have, and worlds 2
// and 3 need external repositories; each is reported as not-run with its
// reason rather than omitted, because an index that lists only what worked
// reads as a complete protocol.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/globulario/sensei/golang/architecture/evalharness"
)

type armArtifact struct {
	Arm           string `json:"arm"`
	Status        string `json:"status"`
	Reason        string `json:"reason,omitempty"`
	ReportFile    string `json:"report_file,omitempty"`
	ReportDigest  string `json:"report_digest_sha256,omitempty"`
	SiteCoverage  string `json:"site_coverage,omitempty"`
	CandidateRate string `json:"candidate_rate,omitempty"`
}

type index struct {
	SchemaVersion string        `json:"schema_version"`
	GeneratedBy   string        `json:"generated_by"`
	Revision      string        `json:"revision"`
	RevisionState string        `json:"revision_status"`
	CapturedAt    string        `json:"captured_at"`
	Domain        string        `json:"repository_domain"`
	Arms          []armArtifact `json:"arms"`
}

func main() {
	out := flag.String("out", "", "directory to write reports into (required)")
	capturedAt := flag.String("captured-at", "", "explicit RFC3339 capture timestamp (required; a self-stamped report is not reproducible)")
	domain := flag.String("repo-domain", "example.com/eval", "repository domain the synthetic mutants are attributed to")
	flag.Parse()

	if strings.TrimSpace(*out) == "" || strings.TrimSpace(*capturedAt) == "" {
		fmt.Fprintln(os.Stderr, "sensei eval-arms: --out and --captured-at are required")
		os.Exit(2)
	}
	if err := os.MkdirAll(*out, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "sensei eval-arms: %v\n", err)
		os.Exit(1)
	}

	// The suite is materialized under --out so the trees a run measured are
	// kept beside the reports that describe them, rather than in a temp
	// directory the reader cannot inspect afterwards.
	workdir := filepath.Join(*out, "mutants")
	opts := evalharness.Options{
		RepositoryDomain: *domain,
		CapturedAt:       *capturedAt,
		MaterializeInto: func(name string) (string, error) {
			path := filepath.Join(workdir, name)
			return path, os.MkdirAll(path, 0o755)
		},
	}
	idx := index{
		SchemaVersion: "sensei.eval_arms_index.v1",
		GeneratedBy:   "sensei eval-arms",
		Revision:      gitRevision(),
		RevisionState: revisionState(),
		CapturedAt:    *capturedAt,
		Domain:        *domain,
	}

	if report, err := evalharness.RunDeterministicExtraction(opts); err != nil {
		idx.Arms = append(idx.Arms, armArtifact{Arm: evalharness.ArmDeterministicExtraction, Status: "failed", Reason: err.Error()})
	} else {
		covered, total := report.SiteCoverageRate()
		art := writeReport(*out, evalharness.ArmDeterministicExtraction, report)
		art.SiteCoverage = fmt.Sprintf("%d/%d", covered, total)
		idx.Arms = append(idx.Arms, art)
	}

	if report, err := evalharness.RunCompositionArm(opts); err != nil {
		idx.Arms = append(idx.Arms, armArtifact{Arm: evalharness.ArmCompositionModelDisabled, Status: "failed", Reason: err.Error()})
	} else {
		produced, total := report.CandidateRate()
		art := writeReport(*out, evalharness.ArmCompositionModelDisabled, report)
		art.CandidateRate = fmt.Sprintf("%d/%d", produced, total)
		idx.Arms = append(idx.Arms, art)
	}

	// Everything this run could NOT do, named. An index listing only what
	// worked would read as a complete protocol.
	idx.Arms = append(idx.Arms,
		armArtifact{Arm: "phase10_composition_model_bound", Status: "not_run",
			Reason: "arm 3 requires an explicitly bound optional model; none is bound in this environment"},
		armArtifact{Arm: "briefing_and_impact_surfaces", Status: "not_run",
			Reason: "arm 4 requires a PUBLISHED graph: the read surfaces fail closed on graph freshness, and publishing a synthetic mutant domain is refused by registry admission"},
		armArtifact{Arm: "world2_globular", Status: "not_run",
			Reason: "requires an exact Globular checkout, which is not part of this repository"},
		armArtifact{Arm: "world3_independent_calibration", Status: "not_run",
			Reason: "requires an independently understood repository inside the Go observation surface; SQLite is C and yields no observations"},
	)

	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei eval-arms: %v\n", err)
		os.Exit(1)
	}
	data = append(data, '\n')
	if err := os.WriteFile(filepath.Join(*out, "index.json"), data, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "sensei eval-arms: %v\n", err)
		os.Exit(1)
	}

	for _, a := range idx.Arms {
		line := fmt.Sprintf("%-34s %s", a.Arm, a.Status)
		if a.SiteCoverage != "" {
			line += "  site_coverage=" + a.SiteCoverage
		}
		if a.CandidateRate != "" {
			line += "  candidates=" + a.CandidateRate
		}
		if a.ReportDigest != "" {
			line += "  digest=" + a.ReportDigest[:12]
		}
		if a.Reason != "" {
			line += "  (" + a.Reason + ")"
		}
		fmt.Println(line)
	}
	fmt.Printf("\nindex: %s\n", filepath.Join(*out, "index.json"))
}

func writeReport(dir, arm string, report any) armArtifact {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return armArtifact{Arm: arm, Status: "failed", Reason: err.Error()}
	}
	data = append(data, '\n')
	name := arm + ".json"
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
		return armArtifact{Arm: arm, Status: "failed", Reason: err.Error()}
	}
	return armArtifact{Arm: arm, Status: "ran", ReportFile: name, ReportDigest: sha256Hex(data)}
}

// gitRevision and revisionState bind the run to the tree it measured. A report
// that names no revision cannot be compared with the next one, and a dirty tree
// must not claim a commit it was not read from (#216).
func gitRevision() string {
	out, err := exec.Command("git", "rev-parse", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func revisionState() string {
	if gitRevision() == "" {
		return "unavailable"
	}
	out, err := exec.Command("git", "status", "--porcelain").Output()
	if err != nil {
		return "unavailable"
	}
	if strings.TrimSpace(string(out)) != "" {
		return "dirty"
	}
	return "resolved"
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
