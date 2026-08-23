// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/globulario/sensei/golang/architecture/epistemic"
)

// runEpistemicAdopt records the event that turns a supported design into
// architecture.
//
// Every other route was eliminated before this verb existed: promotion by
// implementation is architecture by sediment, promotion on SUPPORTED is an
// automatic status transition, and implicit promotion leaves architecture with
// no evidential basis. So the record is required rather than offered, and
// `sensei epistemic scope` reports anything that reached canonical architecture
// by a different path.
func runEpistemicAdopt(args []string) int {
	fs := flag.NewFlagSet("sensei epistemic adopt", flag.ContinueOnError)
	ledgerPath := fs.String("ledger", DefaultEpistemicLedger, "epistemic ledger path")
	id := fs.String("id", "", "stable adoption id")
	question := fs.String("question", "", "the design question this resolves")
	design := fs.String("design", "", "the alternative id being adopted")
	uncertainty := fs.String("remaining-uncertainty", "", `what is still not known ("none identified" is an acceptable answer)`)
	authority := fs.String("authority", "", "the authority this was adopted under (required when the question reached AUTHORITY)")
	adoptedBy := fs.String("adopted-by", defaultActor(), "who adopted it")
	notes := fs.String("notes", "", "free notes")
	var hypotheses, scope repeatable
	fs.Var(&hypotheses, "evidence", "a SUPPORTED hypothesis this rests on (repeat; required unless the question is CONSERVATION)")
	fs.Var(&scope, "scope", "path that becomes established architecture (repeat; defaults to the hypotheses' experimental scope)")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Adopt a supported design as architecture.

  SUPPORTED is not ESTABLISHED. This is the event between them.

Reaching SUPPORTED earns a design the right to be adopted; it does not adopt it.
Until this record exists the code stays experimental, and any canonical
architecture already citing it is reported by "sensei epistemic scope".

Adoption is not a synonym for human approval. When the question carries only
reversible consequences, the agent that ran the experiments may adopt. What
matters is not who typed the command but that the record carries what was
adopted, why, from what evidence, under what remaining uncertainty, and under
whose authority. When the question reached AUTHORITY -- an irreversible
consequence -- --authority must be given.

A CONSERVATION question needs no --evidence: its answer came from the
constraints rather than from an experiment, and demanding a hypothesis would
force a fake one to confirm what was already decided.

Caveat: nothing here VERIFIES that a named authority is a person, or that a
person agreed. It records an attribution.

`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}

	a := epistemic.Adoption{
		ID: strings.TrimSpace(*id), Question: strings.TrimSpace(*question),
		Alternative: strings.TrimSpace(*design), Hypotheses: hypotheses,
		RemainingUncertainty: strings.TrimSpace(*uncertainty),
		Authority:            strings.TrimSpace(*authority),
		Scope:                scope,
		AdoptedBy:            strings.TrimSpace(*adoptedBy),
		AdoptedAt:            time.Now().UTC().Format(time.RFC3339),
		Notes:                strings.TrimSpace(*notes),
	}
	l, err := loadLedger(*ledgerPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	if errs := l.AddAdoption(a, time.Now().UTC()); errs != nil {
		return reportErrs("adoption", errs)
	}
	if err := saveLedger(*ledgerPath, l); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	fmt.Printf("adopted:       %s / %s\n", a.Question, a.Alternative)
	if len(a.Hypotheses) > 0 {
		fmt.Printf("evidence:      %s\n", strings.Join(a.Hypotheses, ", "))
	} else {
		fmt.Println("evidence:      the question's constraints (CONSERVATION — no experiment was needed)")
	}
	fmt.Printf("still unknown: %s\n", a.RemainingUncertainty)
	fmt.Println("\nThis design is now something the project relies on rather than something it is trying.")
	return 0
}
