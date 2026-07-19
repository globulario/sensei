// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

func runIngest(args []string) int {
	fs := flag.NewFlagSet("sensei ingest", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fromFile := fs.String("from-file", "", "YAML file with awareness entries")
	fromScan := fs.Bool("from-scan", false, "re-run annotation scanner + rebuild")
	dryRun := fs.Bool("dry-run", false, "validate only, do not modify files")
	noRebuild := fs.Bool("no-rebuild", false, "skip automatic rebuild")
	svcRepoFlag := fs.String("services-repo", "", "path to services repo (auto-detect)")
	agRepoFlag := fs.String("ag-repo", "", "path to awareness-graph repo (auto-detect)")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: sensei ingest --from-file <path.yaml> | --from-scan [flags]

Feed new knowledge into the awareness graph.

Sources (exactly one required):

  --from-file <path.yaml>
    Read a YAML file containing awareness entries. Entries are appended
    to the matching canonical YAML file, then rebuild is triggered.

  --from-scan
    Re-run the annotation scanner on all services and rebuild.
    Requires the awareness-graph repo.

Flags:
`)
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return 2
	}

	sources := 0
	if *fromFile != "" {
		sources++
	}
	if *fromScan {
		sources++
	}
	if sources != 1 {
		fmt.Fprintln(os.Stderr, "sensei ingest: exactly one of --from-file or --from-scan is required")
		return 2
	}

	svcRepo, _ := resolveServicesRepo(*svcRepoFlag)
	if svcRepo == "" {
		root, _ := resolveProjectRoot("")
		svcRepo = root
	}
	agRepo, _ := resolveAGRepo(*agRepoFlag, svcRepo)

	switch {
	case *fromFile != "":
		return ingestFromFile(*fromFile, svcRepo, agRepo, *dryRun, *noRebuild)
	case *fromScan:
		return ingestFromScan(svcRepo, agRepo, *dryRun)
	}
	return 0
}

// ── from-file ────────────────────────────────────────────────────────────

var ingestClassToFile = map[string]string{
	"invariants": "invariants.yaml", "failure_modes": "failure_modes.yaml",
	"incident_patterns": "incident_patterns.yaml", "forbidden_fixes": "forbidden_fixes.yaml",
	"required_tests": "required_tests.yaml", "candidates": "",
}

func ingestFromFile(inputPath, svcRepo, agRepo string, dryRun, noRebuild bool) int {
	absPath, err := filepath.Abs(inputPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei ingest: %v\n", err)
		return 1
	}
	raw, err := os.ReadFile(absPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei ingest: read: %v\n", err)
		return 1
	}

	var doc map[string]interface{}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		fmt.Fprintf(os.Stderr, "sensei ingest: parse: %v\n", err)
		return 1
	}

	var topKey string
	for k := range doc {
		if _, known := ingestClassToFile[k]; known {
			topKey = k
			break
		}
	}
	if topKey == "" {
		fmt.Fprintf(os.Stderr, "sensei ingest: unrecognized top-level key in %s\n", absPath)
		return 1
	}

	// Candidates: copy to candidates dir.
	if topKey == "candidates" {
		destDir := filepath.Join(svcRepo, "docs", "awareness", "candidates")
		dest := filepath.Join(destDir, filepath.Base(absPath))
		if dryRun {
			fmt.Printf("[dry-run] would copy %s → %s\n", absPath, dest)
			return 0
		}
		_ = os.MkdirAll(destDir, 0o755)
		if err := os.WriteFile(dest, raw, 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "sensei ingest: %v\n", err)
			return 1
		}
		fmt.Printf("  candidate file written: %s\n", dest)
		fmt.Println("  use 'sensei promote <id>' to promote entries")
		return 0
	}

	// Merge into canonical.
	canonicalFile := ingestClassToFile[topKey]
	targetPath := filepath.Join(svcRepo, "docs", "awareness", canonicalFile)

	existingRaw, err := os.ReadFile(targetPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei ingest: read %s: %v\n", canonicalFile, err)
		return 1
	}
	var existing map[string]interface{}
	if err := yaml.Unmarshal(existingRaw, &existing); err != nil {
		fmt.Fprintf(os.Stderr, "sensei ingest: parse %s: %v\n", canonicalFile, err)
		return 1
	}

	newEntries, ok := doc[topKey].([]interface{})
	if !ok || len(newEntries) == 0 {
		fmt.Fprintf(os.Stderr, "sensei ingest: no entries under %q\n", topKey)
		return 1
	}

	existingIDs := collectEntryIDs(existing[topKey])
	added := 0
	for _, entry := range newEntries {
		m, ok := entry.(map[string]interface{})
		if !ok {
			continue
		}
		id, _ := m["id"].(string)
		if id == "" {
			fmt.Fprintf(os.Stderr, "sensei ingest: entry missing 'id'\n")
			return 1
		}
		if existingIDs[id] {
			fmt.Printf("  skipped (duplicate): %s\n", id)
			continue
		}
		existingList, _ := existing[topKey].([]interface{})
		existing[topKey] = append(existingList, entry)
		existingIDs[id] = true
		added++
		fmt.Printf("  added: %s\n", id)
	}

	if added == 0 {
		fmt.Println("  no new entries (all duplicates)")
		return 0
	}
	if dryRun {
		fmt.Printf("[dry-run] would append %d entries to %s\n", added, canonicalFile)
		return 0
	}

	out, err := yaml.Marshal(existing)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei ingest: marshal: %v\n", err)
		return 1
	}
	if err := os.WriteFile(targetPath, out, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "sensei ingest: write: %v\n", err)
		return 1
	}
	fmt.Printf("  wrote %d new entries to %s\n", added, targetPath)

	if noRebuild {
		fmt.Println("  rebuild: skipped (--no-rebuild)")
		return 0
	}
	if err := ensureCrossRepoRebuildPrereqs(agRepo, svcRepo); err != nil {
		fmt.Fprintf(os.Stderr, "sensei ingest: %v\n", err)
		return 1
	}
	fmt.Println("\nTriggering rebuild...")
	rebuildArgs := []string{"--combined"}
	if svcRepo != "" {
		rebuildArgs = append(rebuildArgs, "--services-repo", svcRepo)
	}
	if agRepo != "" {
		rebuildArgs = append(rebuildArgs, "--ag-repo", agRepo)
	}
	return runRebuild(rebuildArgs)
}

func collectEntryIDs(list interface{}) map[string]bool {
	ids := make(map[string]bool)
	entries, ok := list.([]interface{})
	if !ok {
		return ids
	}
	for _, e := range entries {
		m, ok := e.(map[string]interface{})
		if !ok {
			continue
		}
		if id, ok := m["id"].(string); ok {
			ids[id] = true
		}
	}
	return ids
}

// ── from-scan ────────────────────────────────────────────────────────────

func ingestFromScan(svcRepo, agRepo string, dryRun bool) int {
	if agRepo == "" {
		fmt.Fprintln(os.Stderr, "sensei ingest --from-scan: cannot find awareness-graph repo")
		return 1
	}
	if err := ensureCrossRepoRebuildPrereqs(agRepo, svcRepo); err != nil {
		fmt.Fprintf(os.Stderr, "sensei ingest: %v\n", err)
		return 1
	}
	scriptPath := filepath.Join(agRepo, "scripts", "build-awareness-graph.sh")
	if _, err := os.Stat(scriptPath); err != nil {
		fmt.Fprintf(os.Stderr, "sensei ingest: script not found: %s\n", scriptPath)
		return 1
	}
	if dryRun {
		fmt.Printf("[dry-run] would run: %s\n", scriptPath)
		return 0
	}

	fmt.Println("Running annotation scanner + rebuild...")
	cmd := exec.Command("bash", scriptPath)
	cmd.Dir = agRepo
	env := os.Environ()
	if svcRepo != "" {
		env = append(env, "SERVICES_REPO="+svcRepo)
	}
	cmd.Env = env
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "sensei ingest: %v\n", err)
		return 1
	}
	fmt.Println("\nDone.")
	return 0
}

// unused but keeps the interface compatible if we add --from-incident later
var _ = strings.TrimSpace
