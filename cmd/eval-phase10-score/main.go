// SPDX-License-Identifier: AGPL-3.0-only

// Command eval-phase10-score scores a filled Phase 10.8 reference set against
// docs/evaluation/phase10-reference-protocol-v2.md and renders its section 20
// report.
//
// It reads frozen artifacts only. It never contacts a graph, never runs an
// extractor, and holds no vocabulary for producing a label — the answer key is
// the human's, and a scorer able to write one is a scorer able to grade itself.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/globulario/sensei/golang/architecture/phase10score"
)

func main() {
	set := flag.String("reference-set", "docs/evaluation/phase10-v2-reference-set", "frozen reference set to score")
	secondSet := flag.String("second-adjudicator-set", "", "a second adjudicator's copy of the same reference set; omitted records second_adjudicator_unavailable")
	scoreOut := flag.String("score-out", "", "path to write the score artifact to")
	reportOut := flag.String("report-out", "", "path to write the section 20 report to; omitted writes it to stdout")
	flag.Parse()

	rs, err := phase10score.Load(*set)
	if err != nil {
		fatal(err)
	}
	var second *phase10score.ReferenceSet
	if *secondSet != "" {
		second, err = phase10score.Load(*secondSet)
		if err != nil {
			fatal(fmt.Errorf("second adjudicator set: %w", err))
		}
	}
	score, err := phase10score.Compute(rs, second)
	if err != nil {
		fatal(err)
	}
	if *scoreOut != "" {
		body, err := json.MarshalIndent(score, "", "  ")
		if err != nil {
			fatal(err)
		}
		if err := os.WriteFile(*scoreOut, append(body, '\n'), 0o644); err != nil {
			fatal(err)
		}
		fmt.Fprintf(os.Stderr, "score written: %s\n", *scoreOut)
	}
	report := phase10score.Render(score)
	if *reportOut == "" {
		fmt.Print(report)
		return
	}
	if err := os.WriteFile(*reportOut, []byte(report), 0o644); err != nil {
		fatal(err)
	}
	fmt.Fprintf(os.Stderr, "report written: %s\n", *reportOut)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "eval-phase10-score:", err)
	os.Exit(1)
}
