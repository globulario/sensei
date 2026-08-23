// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/globulario/sensei/golang/architecture/epistemic"
)

// runEpistemicAmend adds an alternative to a question already declared.
//
// The alternative set is what somebody could see when the question was
// declared; real work is what reveals the rest. Without this the discovery
// becomes a NEW question, losing the fact that it is the same decision, or gets
// folded into an existing alternative's wording, losing it entirely.
func runEpistemicAmend(args []string) int {
	fs := flag.NewFlagSet("sensei epistemic amend", flag.ContinueOnError)
	ledgerPath := fs.String("ledger", DefaultEpistemicLedger, "epistemic ledger path")
	question := fs.String("question", "", "the design question to widen")
	var alternatives repeatable
	fs.Var(&alternatives, "alternative", "an alternative as id=statement (repeat)")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Add an alternative to a question that was already declared.

Widening is safe in the direction that matters: adding a viable alternative can
only move a disposition toward MORE openness — CONSERVATION can become
EXPLORATION_CANDIDATE or AUTHORITY, never the reverse — so it cannot be used to
manufacture freedom by narrowing. Eliminating an alternative still requires
naming the constraint that killed it.

An alternative may not arrive already eliminated: whoever added it would have
done the eliminating in their head, where nobody can review it.

A question whose answer has been adopted cannot be widened. Reopening a settled
decision is supersession, and supersession is deliberately not built.

`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*question) == "" || len(alternatives) == 0 {
		fmt.Fprintln(os.Stderr, "error: --question and at least one --alternative are required")
		return 2
	}

	l, err := loadLedger(*ledgerPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	before := dispositionOf(l, *question)
	for _, spec := range alternatives {
		id, statement, ok := strings.Cut(spec, "=")
		if !ok {
			fmt.Fprintf(os.Stderr, "error: --alternative %q must be id=statement\n", spec)
			return 2
		}
		alt := epistemic.Alternative{ID: strings.TrimSpace(id), Statement: strings.TrimSpace(statement)}
		if errs := l.AddAlternative(strings.TrimSpace(*question), alt); errs != nil {
			return reportErrs("amendment", errs)
		}
	}
	if err := saveLedger(*ledgerPath, l); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	after := dispositionOf(l, *question)
	fmt.Printf("amended: %s\n", strings.TrimSpace(*question))
	for _, spec := range alternatives {
		fmt.Printf("  + %s\n", spec)
	}
	fmt.Printf("disposition: %s -> %s   (computed, not authored)\n", before, after)
	return 0
}

func dispositionOf(l epistemic.Ledger, questionID string) epistemic.Disposition {
	for _, q := range l.Questions {
		if q.ID == questionID {
			d, _ := epistemic.Dispose(q)
			return d
		}
	}
	return "(absent)"
}
