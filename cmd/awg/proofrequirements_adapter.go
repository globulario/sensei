// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/globulario/sensei/golang/architecture/proofrequirements"
)

// Proof-obligation generation now lives in the reusable
// golang/architecture/proofrequirements package (shared with the Phase 7 result
// pipeline). These aliases and wrappers keep the CLI's existing call sites and
// the extract-proof-obligations command working as thin adapters.

type proofObligationsDoc = proofrequirements.ObligationsDoc
type generatedProofObligation = proofrequirements.Obligation
type generatedProofSlot = proofrequirements.Slot
type proofTemplate = proofrequirements.Template

func loadAuthoritySurfaces(path string) ([]authoritySurfaceCandidate, error) {
	return proofrequirements.LoadAuthoritySurfaces(path)
}

func buildProofObligations(surfaces []authoritySurfaceCandidate) proofObligationsDoc {
	return proofrequirements.BuildObligations(surfaces)
}

func renderProofObligations(doc proofObligationsDoc) ([]byte, error) {
	return proofrequirements.RenderObligations(doc)
}

func renderProofObligationSummary(doc proofObligationsDoc, target string, check bool) string {
	return proofrequirements.RenderObligationSummary(doc, target, check)
}

func runExtractProofObligations(args []string) int {
	fs := flag.NewFlagSet("sensei extract-proof-obligations", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	repoRoot := fs.String("repo-root", ".", "repository root")
	authorityPath := fs.String("authority", "", "authority surfaces YAML (default: <repo>/docs/awareness/candidates/authority_surface_candidates.yaml)")
	output := fs.String("output", "", "proof obligations YAML (default: <repo>/docs/awareness/generated/proof_obligations.yaml)")
	check := fs.Bool("check", false, "compare committed proof-obligations file to a fresh run; exit 1 if stale")
	format := fs.String("format", "text", "output format: text | json")
	asJSON := fs.Bool("json", false, "output as JSON (deprecated: same as --format json)")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: sensei extract-proof-obligations [flags]

Generate deterministic proof-obligation YAML from authority surfaces. This is a
governance-layer extractor: it derives expected proof slots by authority kind,
but does not evaluate any repair event.

Flags:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *asJSON {
		*format = "json"
	}
	root, err := filepath.Abs(*repoRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei extract-proof-obligations: resolve repo root: %v\n", err)
		return 1
	}
	authPath := *authorityPath
	if authPath == "" {
		authPath = filepath.Join(root, "docs", "awareness", "candidates", "authority_surface_candidates.yaml")
	}
	surfaces, err := proofrequirements.LoadAuthoritySurfaces(authPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei extract-proof-obligations: load authority surfaces: %v\n", err)
		return 1
	}
	doc := proofrequirements.BuildObligations(surfaces)
	out, err := proofrequirements.RenderObligations(doc)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei extract-proof-obligations: render: %v\n", err)
		return 1
	}
	target := *output
	if target == "" {
		target = filepath.Join(root, "docs", "awareness", "generated", "proof_obligations.yaml")
	}
	if *check {
		committed, _ := os.ReadFile(target)
		if !bytes.Equal(bytes.TrimSpace(committed), bytes.TrimSpace(out)) {
			fmt.Fprintf(os.Stderr, "extract-proof-obligations: STALE — %s differs from a fresh run; rerun to regenerate\n", target)
			return 1
		}
	}
	if !*check {
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "sensei extract-proof-obligations: mkdir: %v\n", err)
			return 1
		}
		if err := os.WriteFile(target, out, 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "sensei extract-proof-obligations: write: %v\n", err)
			return 1
		}
	}

	switch *format {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(doc)
	default:
		fmt.Print(proofrequirements.RenderObligationSummary(doc, target, *check))
	}
	return 0
}
