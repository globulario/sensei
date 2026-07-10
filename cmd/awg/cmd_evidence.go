// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"

	"github.com/globulario/sensei/golang/evidence"
)

// runEvidence aggregates the outcome ledger that `awg gate`/`awg edit-guard`
// append to (when --event-log / $AWG_EVENT_LOG is set) into the headline the
// control claim needs: "caught N drift incidents across M repos". It reads only;
// it never writes.
func runEvidence(args []string) int {
	fs := flag.NewFlagSet("awg evidence", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	path := fs.String("log", os.Getenv("AWG_EVENT_LOG"), "path to the JSONL evidence ledger (default: $AWG_EVENT_LOG)")
	asJSON := fs.Bool("json", false, "output the aggregated summary as JSON")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: awg evidence [--log <path>] [--json]

Aggregate the gate/guard outcome ledger into evidence: how many drift incidents
were caught (enforcement:block findings), across how many repos, by which rules.
Enable logging by running the gate/guard with --event-log <path> (or
$AWG_EVENT_LOG); this command then reads that ledger.

Flags:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *path == "" {
		fmt.Fprintln(os.Stderr, "awg evidence: no ledger path — pass --log <path> or set $AWG_EVENT_LOG")
		return 2
	}

	events, err := evidence.Load(*path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "awg evidence: read ledger: %v\n", err)
		return 1
	}
	sum := evidence.Aggregate(events)

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(sum)
		return 0
	}

	reposWithCatches := 0
	for _, r := range sum.ByRepo {
		if r.Blocks > 0 {
			reposWithCatches++
		}
	}
	fmt.Printf("AWG evidence — %s\n", *path)
	fmt.Printf("  caught %d drift incident(s) (%d enforced) across %d repo(s) with catches\n",
		sum.Blocks, sum.HardBlocks, reposWithCatches)
	fmt.Printf("  %d run(s) total across %d repo(s)", sum.Events, len(sum.Repos))
	if sum.FirstTS != "" {
		fmt.Printf("  ·  %s → %s", sum.FirstTS, sum.LastTS)
	}
	fmt.Printf("\n  outcomes: block/would-block=%d  warn=%d  allow=%d  cannot-verify=%d\n",
		sum.Blocks, sum.Warns, sum.Allows, sum.CannotVerify)

	if len(sum.CatchesByRule) > 0 {
		type rc struct {
			rule string
			n    int
		}
		ranked := make([]rc, 0, len(sum.CatchesByRule))
		for r, n := range sum.CatchesByRule {
			ranked = append(ranked, rc{r, n})
		}
		sort.Slice(ranked, func(i, j int) bool {
			if ranked[i].n != ranked[j].n {
				return ranked[i].n > ranked[j].n
			}
			return ranked[i].rule < ranked[j].rule
		})
		fmt.Println("\n  catches by rule:")
		for _, r := range ranked {
			fmt.Printf("    %4d  %s\n", r.n, r.rule)
		}
	}

	if len(sum.ByRepo) > 0 {
		fmt.Println("\n  by repo:")
		for _, r := range sum.ByRepo {
			fmt.Printf("    %-40s runs=%-4d catches=%d\n", r.Repo, r.Events, r.Blocks)
		}
	}
	return 0
}
