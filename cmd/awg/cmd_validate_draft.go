// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/globulario/sensei/golang/extractor/coldsource"
	"gopkg.in/yaml.v3"
)

// runValidateDraft is the standalone cage oracle: given one exported bundle and
// one candidate draft, it runs the SAME validation as the cold-bootstrap import
// path (status:candidate enforcement, known-class, citation membership, citation
// resolution, shallow filter) and prints pass/fail with reasons. It writes
// nothing, promotes nothing, and never mutates the active graph.
//
// This trusts the provided --bundle file (it is a debug/iteration tool). The
// authoritative import path is `cold-bootstrap --drafter stdin`, which
// re-extracts the bundle from the live repo. validate-draft still enforces
// bundle binding: if the draft declares a bundle_id that doesn't match the
// provided bundle's content hash, it fails as stale/drifted.
func runValidateDraft(args []string) int {
	fs := flag.NewFlagSet("sensei validate-draft", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	bundleFile := fs.String("bundle", "", "exported bundle JSON (a single BundleExport object)")
	draftFile := fs.String("draft", "", "candidate draft file (JSON or YAML)")
	repo := fs.String("repo", ".", "repo working tree for citation resolution")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: sensei validate-draft --bundle <bundle.json> --draft <candidate.{json,yaml}> [--repo <path>]

Validate one externally-drafted candidate against one exported bundle, through
the same cage as the cold-bootstrap import path. Prints PASS or FAIL+reasons.
Writes nothing; never promotes; never mutates the active graph.

Flags:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *bundleFile == "" || *draftFile == "" {
		fs.Usage()
		return 2
	}

	bdata, err := os.ReadFile(*bundleFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: read bundle: %v\n", err)
		return 2
	}
	var be coldsource.BundleExport
	if err := json.Unmarshal(bdata, &be); err != nil {
		fmt.Fprintf(os.Stderr, "error: parse bundle JSON: %v\n", err)
		return 2
	}

	ddata, err := os.ReadFile(*draftFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: read draft: %v\n", err)
		return 2
	}
	var draft coldsource.SubmittedDraft
	if err := yaml.Unmarshal(ddata, &draft); err != nil { // yaml.v3 also parses JSON
		fmt.Fprintf(os.Stderr, "error: parse draft (expected JSON or YAML): %v\n", err)
		return 2
	}

	bundle := coldsource.BundleFromExport(be)
	liveID := bundle.BundleID()
	fmt.Printf("validate-draft — bundle %s (theme %s)\n", liveID, bundle.ThemeKey)

	// Binding check: a draft declaring a different bundle_id is stale/drifted.
	if draft.BundleID != "" && draft.BundleID != liveID {
		fmt.Printf("FAIL: bundle_id mismatch — draft cites %q but bundle hashes to %q (stale/drifted bundle)\n", draft.BundleID, liveID)
		return 1
	}
	draft.BundleID = liveID

	// Map the draft via the same StdinDrafter used by the import path (forces
	// status:candidate, rejects unknown class).
	p, derr := coldsource.NewStdinDrafter([]coldsource.SubmittedDraft{draft}).Draft(context.Background(), bundle)
	if derr != nil {
		fmt.Printf("FAIL: %v\n", derr)
		return 1
	}

	var reasons []string
	reasons = append(reasons, coldsource.ValidateDraft(p, bundle)...)
	if shallow, why := coldsource.IsShallow(p, bundle); shallow {
		reasons = append(reasons, "shallow: "+why)
	}
	if ok, results := coldsource.CheckCitations(p.SourcePaths, *repo, coldsource.NewGitVerifier(*repo)); !ok {
		for _, r := range results {
			if !r.OK {
				reasons = append(reasons, "citation unresolved: "+r.Reason)
			}
		}
	}

	if len(reasons) > 0 {
		fmt.Printf("FAIL (%d):\n", len(reasons))
		for _, r := range reasons {
			fmt.Printf("  - %s\n", r)
		}
		return 1
	}
	fmt.Printf("PASS: class=%s confidence=%s citations=%d (status forced to candidate)\n",
		p.CandidateClass, p.Confidence, len(p.SourcePaths))
	return 0
}
