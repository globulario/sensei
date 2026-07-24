// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/globulario/sensei/golang/architecture/protection"
)

// runProtectionStatus implements `sensei protection-status`: reports the
// closed coverage status, effective protected-path count, manual/derived
// counts, snapshot state, and gaps for one repository (contract §6).
func runProtectionStatus(args []string) int {
	fs := flag.NewFlagSet("sensei protection-status", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	path := fs.String("path", ".", "repository root to evaluate")
	asJSON := fs.Bool("json", false, "output as JSON")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: sensei protection-status [--path <repo>] [--json]

Reports the effective file-protection coverage for a repository: the
deterministic union of manual registry entries, governed-source protection,
governed relations, structural contract signals, and candidate-derived
provisional caution. Never requires a running Sensei server.

Flags:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}

	cov, err := protection.Derive(*path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei protection-status: %v\n", err)
		return 1
	}
	_, snapshotExists, snapErr := protection.LoadSnapshot(*path)

	if *asJSON {
		out := protectionStatusJSON(cov, snapshotExists, snapErr)
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return exitOnEncodeErr(enc.Encode(out))
	}

	fmt.Printf("coverage status: %s\n", cov.Status)
	fmt.Printf("effective protected paths: %d (manual=%d derived=%d provisional=%d)\n",
		len(cov.ProtectedPaths), cov.ManualCount, cov.DerivedCount, cov.ProvisionalCount)
	if snapErr != nil {
		fmt.Printf("snapshot: error reading existing snapshot: %v\n", snapErr)
	} else if !snapshotExists {
		fmt.Println("snapshot: none published yet (run `sensei bootstrap` or `sensei init`)")
	} else {
		fmt.Println("snapshot: present")
	}
	if len(cov.Gaps) > 0 {
		fmt.Println("gaps:")
		for _, g := range cov.Gaps {
			fmt.Printf("  - %s\n", g)
		}
	}
	if cov.Status == protection.CoverageEmpty {
		fmt.Println("\nNo protection signal was observed. This is an awareness gap, not a low-risk verdict.")
	}
	return 0
}

// runProtectionCheck implements `sensei protection-check`: whether one exact
// file is protected, its reasons, and whether briefing is required (contract
// §6).
func runProtectionCheck(args []string) int {
	fs := flag.NewFlagSet("sensei protection-check", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	path := fs.String("path", ".", "repository root to evaluate")
	file := fs.String("file", "", "repo-relative file to check (required)")
	asJSON := fs.Bool("json", false, "output as JSON")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: sensei protection-check --path <repo> --file <repo-relative-file> [--json]

Reports whether one exact file is protected under the repository's current
effective protection coverage, and why. Never requires a running Sensei
server.

Flags:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *file == "" {
		fmt.Fprintln(os.Stderr, "sensei protection-check: --file is required")
		return 2
	}

	cov, err := protection.Derive(*path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei protection-check: %v\n", err)
		return 1
	}
	fc, ok := protection.ClassifyFile(cov, *file)
	if !ok {
		if *asJSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return exitOnEncodeErr(enc.Encode(map[string]any{
				"error": "file is outside the repository or could not be normalized",
				"file":  *file,
			}))
		}
		fmt.Fprintf(os.Stderr, "sensei protection-check: %q is outside the repository or could not be normalized\n", *file)
		return 1
	}

	briefingRequired := fc.Protected // provisional protection also requires briefing (contract §9.3).

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return exitOnEncodeErr(enc.Encode(protectionCheckJSON(fc, briefingRequired, cov.Status)))
	}

	if !fc.Protected {
		fmt.Printf("%s: not classified as protected by current coverage (status=%s)\n", fc.Path, cov.Status)
		if cov.Status == protection.CoverageEmpty || cov.Status == protection.CoverageDegraded {
			fmt.Println("Coverage itself is " + string(cov.Status) + " — this is not a safety guarantee about this file.")
		}
		return 0
	}
	kind := "protected"
	if fc.Provisional {
		kind = "provisionally protected (candidate-derived caution, not governed authority)"
	}
	fmt.Printf("%s: %s\n", fc.Path, kind)
	for _, r := range fc.Reasons {
		ref := ""
		if r.KnowledgeRef != "" {
			ref = " (" + r.KnowledgeRef + ")"
		}
		fmt.Printf("  - %s/%s from %s%s\n", r.Origin, r.Kind, r.Source, ref)
	}
	fmt.Println("briefing required: true")
	return 0
}

func protectionStatusJSON(cov protection.ProtectionCoverage, snapshotExists bool, snapErr error) map[string]any {
	m := map[string]any{
		"status":              string(cov.Status),
		"protected_count":     len(cov.ProtectedPaths),
		"manual_count":        cov.ManualCount,
		"derived_count":       cov.DerivedCount,
		"provisional_count":   cov.ProvisionalCount,
		"gaps":                cov.Gaps,
		"generation_identity": cov.GenerationIdentity,
		"snapshot_present":    snapshotExists,
	}
	if snapErr != nil {
		m["snapshot_error"] = snapErr.Error()
	}
	return m
}

func protectionCheckJSON(fc protection.FileClassification, briefingRequired bool, coverageStatus protection.ProtectionCoverageStatus) map[string]any {
	reasons := make([]map[string]any, 0, len(fc.Reasons))
	for _, r := range fc.Reasons {
		reasons = append(reasons, map[string]any{
			"origin":        string(r.Origin),
			"kind":          r.Kind,
			"source":        r.Source,
			"knowledge_ref": r.KnowledgeRef,
			"provisional":   r.Provisional,
		})
	}
	return map[string]any{
		"path":              fc.Path,
		"protected":         fc.Protected,
		"provisional":       fc.Provisional,
		"reasons":           reasons,
		"briefing_required": briefingRequired,
		"coverage_status":   string(coverageStatus),
	}
}

func exitOnEncodeErr(err error) int {
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei: encode output: %v\n", err)
		return 1
	}
	return 0
}
