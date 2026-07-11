// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/globulario/sensei/golang/rdf"
	"github.com/globulario/sensei/golang/store/oxigraph"
)

// runReconcile implements `sensei reconcile` (GC-3): the live-store ↔ authored-YAML
// reconciliation job. It diffs the running Oxigraph store's node set against the
// committed seed (which GC-1 keeps coherent with the authored YAML), and names:
//
//   - store-only subjects → ORPHANS: nodes the live store carries that the
//     authored corpus does not. These arise at runtime from additive loads
//     (`sensei promote`/`propose` POST-merge into the store) that were never
//     reconciled back into the seed/YAML — exactly the orphan subgraph that was
//     previously only found by hand.
//   - seed-only subjects → the live store is LAGGING the committed seed (it was
//     not reloaded after a rebuild, or is stale/empty). Informational, not an
//     orphan; `sensei rebuild` / a store reload fixes it.
//
// Where GC-1's `sensei audit` seed-orphans check polices the COMMITTED artifact,
// this polices the LIVE runtime store, which the committed artifact cannot see.
//
// Exit codes:
//
//	0  reconciled (no store-only orphans), or store unreachable without -require-clean
//	1  store-only orphans found, or -require-clean and the store could not be proven clean
//	2  usage / configuration error
func runReconcile(args []string) int {
	fs := flag.NewFlagSet("sensei reconcile", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	seedPathFlag := fs.String("seed", "", "path to awareness.nt (default: auto-detect embedded seed)")
	oxigraphURL := fs.String("oxigraph-url", "http://localhost:7878/query", "Oxigraph query or store endpoint")
	baselineFlag := fs.String("baseline", "auto", "authored baseline: auto | yaml | seed")
	requireClean := fs.Bool("require-clean", false, "exit 1 unless the live store is proven free of store-only orphans")
	asJSON := fs.Bool("json", false, "output as JSON")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: sensei reconcile [flags]

Reconcile the live Oxigraph store against the authored corpus. Surfaces
store-only orphans — nodes present in the running store but absent from the
authored corpus — and live-store lag.

Baseline (-baseline):
  yaml   Freshly generate the seed from working-tree YAML. The TRUE-orphan
         detector: a node still authored somewhere (e.g. locally rebuilt but
         not yet committed) is NOT flagged — only nodes with no authored origin
         anywhere are orphans. Requires the repos to be present.
  seed   The committed/embedded awareness.nt. The deployed-runtime detector:
         on a node with no repo checkout the shipped seed IS the authored truth.
  auto   yaml when the repos resolve, else seed (default).

Interpreting store-only orphans: hand-authored classes (forbidden_fix,
invariant, failure_mode, intent) are the high-signal drift. Code-scanned classes
(test_symbol, source_file, symbol, code_symbol) may over-report under the yaml
baseline when the live store was built from a wider scan corpus (e.g. a sibling
repo not checked out here) than this baseline reproduces — diagnose, don't bulk-
delete.

Flags:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	switch *baselineFlag {
	case "auto", "yaml", "seed":
	default:
		fmt.Fprintf(os.Stderr, "sensei reconcile: -baseline must be auto|yaml|seed, got %q\n", *baselineFlag)
		return 2
	}

	queryURL, err := normalizeOxigraphQueryURL(*oxigraphURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei reconcile: invalid --oxigraph-url: %v\n", err)
		return 1
	}

	res := reconcileResult{QueryURL: queryURL, RequireClean: *requireClean}
	seedSubjects := resolveAuthoredBaseline(*baselineFlag, *seedPathFlag, &res)
	if seedSubjects == nil {
		// resolveAuthoredBaseline already reported the fatal reason.
		return 1
	}
	res.SeedNodeCount = len(seedSubjects)

	store, err := oxigraph.New(queryURL)
	if err != nil {
		res.StoreState = "down"
		res.StoreDetail = err.Error()
		return printReconcileResult(res, *asJSON)
	}
	defer store.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	liveSubjects, err := store.Subjects(ctx)
	if err != nil {
		if looksLikeStoreDown(err.Error()) {
			res.StoreState = "down"
		} else {
			res.StoreState = "degraded"
		}
		res.StoreDetail = err.Error()
		return printReconcileResult(res, *asJSON)
	}

	live := awarenessSubjectSet(liveSubjects)
	res.LiveNodeCount = len(live)
	if res.LiveNodeCount == 0 {
		res.StoreState = "empty"
		res.StoreDetail = "live store has no awareness subjects"
		return printReconcileResult(res, *asJSON)
	}

	res.StoreOnly, res.SeedOnly = reconcileSubjects(seedSubjects, live)
	if len(res.StoreOnly) == 0 {
		res.StoreState = "reconciled"
	} else {
		res.StoreState = "orphans"
	}
	return printReconcileResult(res, *asJSON)
}

type reconcileResult struct {
	Baseline      string   `json:"baseline"`
	SeedPath      string   `json:"seed_path,omitempty"`
	QueryURL      string   `json:"query_url"`
	StoreState    string   `json:"store_state"`
	StoreDetail   string   `json:"store_detail,omitempty"`
	SeedNodeCount int      `json:"seed_node_count"`
	LiveNodeCount int      `json:"live_node_count"`
	StoreOnly     []string `json:"store_only_orphans,omitempty"`
	SeedOnly      []string `json:"seed_only_lagging,omitempty"`
	RequireClean  bool     `json:"require_clean"`
}

// resolveAuthoredBaseline picks the authored node set to reconcile the live
// store against. With -baseline yaml/auto and the repos present, it freshly
// generates the seed from working-tree YAML so locally-rebuilt-but-uncommitted
// content is NOT mistaken for an orphan (the true-orphan detector). Otherwise it
// falls back to the committed/embedded awareness.nt (the deployed-runtime
// detector). Returns nil only on a fatal error (already reported).
func resolveAuthoredBaseline(baseline, seedPathFlag string, res *reconcileResult) []string {
	if baseline == "auto" || baseline == "yaml" {
		svcRepo, _ := resolveServicesRepo("")
		agRepo, _ := resolveAGRepo("", svcRepo)
		if agRepo != "" {
			inputDirs, intentDir, derr := collectInputDirs(svcRepo, agRepo)
			if derr == nil && len(inputDirs) > 0 {
				generated, _, _, gerr := generateNT(inputDirs, intentDir, svcRepo, agRepo)
				if gerr == nil {
					res.Baseline = "generated-yaml"
					return awarenessSubjectsFromNT(generated)
				}
				if baseline == "yaml" {
					fmt.Fprintf(os.Stderr, "sensei reconcile: generate from YAML: %v\n", gerr)
					return nil
				}
			} else if baseline == "yaml" {
				if derr != nil {
					fmt.Fprintf(os.Stderr, "sensei reconcile: collect input dirs: %v\n", derr)
				} else {
					fmt.Fprintln(os.Stderr, "sensei reconcile: no awareness input dirs found")
				}
				return nil
			}
		} else if baseline == "yaml" {
			fmt.Fprintln(os.Stderr, "sensei reconcile: -baseline yaml requires the awareness-graph repo (not found)")
			return nil
		}
		// auto + generation unavailable → fall through to the committed seed.
	}

	seedPath, err := resolveSeedPath(seedPathFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei reconcile: %v\n", err)
		return nil
	}
	seedBytes, err := os.ReadFile(seedPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei reconcile: read seed: %v\n", err)
		return nil
	}
	res.Baseline = "committed-seed"
	res.SeedPath = seedPath
	return awarenessSubjectsFromNT(seedBytes)
}

// reconcileSubjects computes the set difference between the committed-seed node
// set and the live-store node set. storeOnly are orphans (live − seed); seedOnly
// are lagging nodes (seed − live). Both are sorted for stable output. seed is a
// slice (the seed parse preserves a stable subject list); live is a set.
func reconcileSubjects(seed []string, live map[string]bool) (storeOnly, seedOnly []string) {
	seedSet := make(map[string]bool, len(seed))
	for _, s := range seed {
		seedSet[s] = true
	}
	for s := range live {
		if !seedSet[s] {
			storeOnly = append(storeOnly, s)
		}
	}
	for s := range seedSet {
		if !live[s] {
			seedOnly = append(seedOnly, s)
		}
	}
	sort.Strings(storeOnly)
	sort.Strings(seedOnly)
	return storeOnly, seedOnly
}

// awarenessSubjectsFromNT extracts the distinct awareness-namespace subject IRIs
// from an N-Triples byte stream (bare IRIs, angle brackets stripped). Blank
// nodes and foreign-namespace subjects are excluded so the diff compares like
// with like against the live store's awareness subjects.
func awarenessSubjectsFromNT(b []byte) []string {
	seen := map[string]bool{}
	for s := range ntSubjects(b) {
		iri := strings.TrimSuffix(strings.TrimPrefix(s, "<"), ">")
		if strings.HasPrefix(iri, rdf.AwNS) {
			seen[iri] = true
		}
	}
	out := make([]string, 0, len(seen))
	for s := range seen {
		out = append(out, s)
	}
	return out
}

// awarenessSubjectSet narrows a list of live-store subject IRIs to the awareness
// namespace, as a set.
func awarenessSubjectSet(subjects []string) map[string]bool {
	set := make(map[string]bool, len(subjects))
	for _, s := range subjects {
		if strings.HasPrefix(s, rdf.AwNS) {
			set[s] = true
		}
	}
	return set
}

func reconcileShortID(iri string) string {
	if i := strings.LastIndex(iri, "#"); i >= 0 {
		return iri[i+1:]
	}
	return iri
}

func printReconcileResult(res reconcileResult, asJSON bool) int {
	exit := reconcileExitCode(res)
	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(res)
		return exit
	}
	fmt.Printf("Authored baseline:   %s\n", res.Baseline)
	if res.SeedPath != "" {
		fmt.Printf("Seed file:           %s\n", res.SeedPath)
	}
	fmt.Printf("Oxigraph query URL:  %s\n", res.QueryURL)
	fmt.Printf("Store state:         %s\n", res.StoreState)
	if res.StoreDetail != "" {
		fmt.Printf("  detail:            %s\n", res.StoreDetail)
	}
	fmt.Printf("Seed node count:     %d\n", res.SeedNodeCount)
	fmt.Printf("Live node count:     %d\n", res.LiveNodeCount)
	fmt.Printf("Store-only orphans:  %d\n", len(res.StoreOnly))
	for _, s := range res.StoreOnly {
		fmt.Printf("  orphan: %s\n", reconcileShortID(s))
	}
	fmt.Printf("Seed-only (lagging): %d\n", len(res.SeedOnly))
	for _, s := range res.SeedOnly {
		fmt.Printf("  lagging: %s\n", reconcileShortID(s))
	}
	switch res.StoreState {
	case "reconciled":
		fmt.Println("Status:              live store matches the authored corpus")
	case "orphans":
		fmt.Println("Status:              live store carries nodes absent from the authored corpus")
		fmt.Println("Next step:           promote them into YAML (sensei propose/learn) or reload the store (sensei rebuild)")
	case "down", "degraded":
		fmt.Println("Status:              live store could not be reconciled")
	case "empty":
		fmt.Println("Status:              live store is empty — reload it (sensei rebuild)")
	}
	return exit
}

func reconcileExitCode(res reconcileResult) int {
	if len(res.StoreOnly) > 0 {
		return 1
	}
	// Could not prove cleanliness: under -require-clean, fail closed.
	if res.RequireClean && res.StoreState != "reconciled" {
		return 1
	}
	return 0
}
