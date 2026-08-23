// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/globulario/sensei/golang/architecture/epistemic"
)

// DefaultEpistemicLedger is where the uncertain-design-belief lane lives.
//
// Under docs/awareness/ because it is governed repository material, in its own
// directory because it is NOT part of the awareness corpus: yaml2nt does not
// read it, no seed contains it, and no routing surface can consult it. A
// DesignQuestion is not law and is never promoted into one.
const DefaultEpistemicLedger = "docs/awareness/epistemic/ledger.yaml"

// runEpistemic dispatches the epistemic lane (globulario/sensei#288 slice 1).
//
// Nothing here grants authority, promotes anything, or transitions a status by
// itself. `status` computes and prints; the three writers append one validated
// record each. That is the whole surface.
func runEpistemic(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: sensei epistemic <declare|hypothesize|observe|adopt|status|scope> [flags]")
		return 2
	}
	switch args[0] {
	case "declare":
		return runEpistemicDeclare(args[1:])
	case "hypothesize":
		return runEpistemicHypothesize(args[1:])
	case "observe":
		return runEpistemicObserve(args[1:])
	case "status":
		return runEpistemicStatus(args[1:])
	case "scope":
		return runEpistemicScope(args[1:])
	case "adopt":
		return runEpistemicAdopt(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "sensei epistemic: unknown subcommand %q (declare|hypothesize|observe|adopt|status|scope)\n", args[0])
		return 2
	}
}

// repeatable collects a flag that may appear more than once.
type repeatable []string

func (r *repeatable) String() string     { return strings.Join(*r, ", ") }
func (r *repeatable) Set(v string) error { *r = append(*r, strings.TrimSpace(v)); return nil }

func defaultActor() string {
	for _, k := range []string{"SENSEI_ACTOR", "USER", "USERNAME"} {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	return ""
}

func loadLedger(path string) (epistemic.Ledger, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return epistemic.Decode(nil)
		}
		return epistemic.Ledger{}, err
	}
	return epistemic.Decode(b)
}

func saveLedger(path string, l epistemic.Ledger) error {
	b, err := epistemic.Encode(l)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// reportErrs prints every validation problem at once. Fixing them one at a time
// is how a reviewer's attention gets spent on the form instead of the content.
func reportErrs(what string, errs []string) int {
	fmt.Fprintf(os.Stderr, "sensei epistemic: %s rejected\n", what)
	for _, e := range errs {
		fmt.Fprintf(os.Stderr, "  - %s\n", e)
	}
	return 1
}

// -----------------------------------------------------------------------------
// declare
// -----------------------------------------------------------------------------

func runEpistemicDeclare(args []string) int {
	fs := flag.NewFlagSet("sensei epistemic declare", flag.ContinueOnError)
	ledgerPath := fs.String("ledger", DefaultEpistemicLedger, "epistemic ledger path")
	id := fs.String("id", "", "stable question id, e.g. dq.graph_integrity_evidence")
	question := fs.String("question", "", "what is actually being decided")
	objective := fs.String("objective", "", "the objective this question arose under")
	declaredBy := fs.String("declared-by", defaultActor(), "who declared this uncertainty")
	notes := fs.String("notes", "", "free notes")
	var alternatives, eliminated, constraints, reversible, irreversible repeatable
	fs.Var(&alternatives, "alternative", "an alternative as id=statement (repeat; at least 2 required)")
	fs.Var(&eliminated, "eliminated", "mark an alternative dead as altid=constraint_id (repeat)")
	fs.Var(&constraints, "constraint", "established knowledge id bound to this question (repeat)")
	fs.Var(&reversible, "consequence", "a REVERSIBLE consequence of experimenting here (repeat)")
	fs.Var(&irreversible, "irreversible-consequence", "an IRREVERSIBLE consequence of experimenting here (repeat)")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Declare a design question — uncertainty stated positively, so it can be resolved.

A question needs at least two materially distinct alternatives that are still
viable when it is declared. One alternative is a decision already made; none is
a topic heading, and a topic heading confers nothing.

You do not write a disposition. It is computed from the structure you declare:
constraints that leave one alternative give CONSERVATION, two or more viable
alternatives with reversible consequences give EXPLORATION_CANDIDATE, and an
irreversible consequence gives AUTHORITY. AUTHORITY is reached by consequence,
never because a question is technically hard.

Consequences are about the world, not version control. A branch is trivially
revertible; the experiment that runs on it may mutate a database, spend quota,
publish an artifact or send something outward.

`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}

	q := epistemic.DesignQuestion{
		ID: strings.TrimSpace(*id), Question: strings.TrimSpace(*question),
		Objective: strings.TrimSpace(*objective), DeclaredBy: strings.TrimSpace(*declaredBy),
		DeclaredAt: time.Now().UTC().Format(time.RFC3339), Notes: strings.TrimSpace(*notes),
		Constraints: constraints,
	}
	byID := map[string]int{}
	for _, spec := range alternatives {
		altID, statement, ok := strings.Cut(spec, "=")
		if !ok {
			fmt.Fprintf(os.Stderr, "error: --alternative %q must be id=statement\n", spec)
			return 2
		}
		altID = strings.TrimSpace(altID)
		byID[altID] = len(q.Alternatives)
		q.Alternatives = append(q.Alternatives, epistemic.Alternative{ID: altID, Statement: strings.TrimSpace(statement)})
	}
	for _, spec := range eliminated {
		altID, by, ok := strings.Cut(spec, "=")
		if !ok {
			fmt.Fprintf(os.Stderr, "error: --eliminated %q must be altid=constraint_id\n", spec)
			return 2
		}
		i, known := byID[strings.TrimSpace(altID)]
		if !known {
			fmt.Fprintf(os.Stderr, "error: --eliminated names alternative %q, which was not declared\n", strings.TrimSpace(altID))
			return 2
		}
		q.Alternatives[i].EliminatedBy = strings.TrimSpace(by)
	}
	for _, e := range reversible {
		q.Consequences = append(q.Consequences, epistemic.Consequence{Effect: e, Reversible: true})
	}
	for _, e := range irreversible {
		q.Consequences = append(q.Consequences, epistemic.Consequence{Effect: e, Reversible: false})
	}

	l, err := loadLedger(*ledgerPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	if errs := l.AddQuestion(q); errs != nil {
		return reportErrs("design question", errs)
	}
	if err := saveLedger(*ledgerPath, l); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	disposition, why := epistemic.Dispose(q)
	fmt.Printf("declared:    %s\n", q.ID)
	fmt.Printf("disposition: %s   (computed, not authored)\n", disposition)
	fmt.Printf("because:     %s\n", why)
	fmt.Printf("ledger:      %s\n", *ledgerPath)
	switch disposition {
	case epistemic.DispositionAuthority:
		fmt.Println("\nThis one is not yours to settle by experiment: a named consequence is irreversible.")
	case epistemic.DispositionExploration:
		fmt.Println("\nNext: `sensei epistemic hypothesize` — a prediction, a falsifier, and a horizon.")
	}
	return 0
}

// -----------------------------------------------------------------------------
// hypothesize
// -----------------------------------------------------------------------------

func runEpistemicHypothesize(args []string) int {
	fs := flag.NewFlagSet("sensei epistemic hypothesize", flag.ContinueOnError)
	ledgerPath := fs.String("ledger", DefaultEpistemicLedger, "epistemic ledger path")
	id := fs.String("id", "", "stable hypothesis id")
	question := fs.String("question", "", "the design question id this would settle")
	alternative := fs.String("alternative", "", "the alternative id this predicts about")
	prediction := fs.String("prediction", "", "an OBSERVABLE prediction")
	falsifier := fs.String("falsifier", "", "an observation that would show the prediction wrong")
	due := fs.String("due", "", "observation horizon, RFC3339 (e.g. 2026-12-01T00:00:00Z)")
	condition := fs.String("condition", "", "the real trigger, e.g. 'after 100 reconciliation cycles' (the date is the backstop)")
	declaredBy := fs.String("declared-by", defaultActor(), "who holds this belief")
	notes := fs.String("notes", "", "free notes")
	var scope repeatable
	fs.Var(&scope, "scope", "a repository path that exists ONLY to test this hypothesis (repeat)")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Record a falsifiable belief about one alternative.

Mandatory: an observable prediction, an observation horizon, and a falsifier.
Optional: everything else. Rationale prose is the easy thing to write and the
impossible thing to check, so only the fields reality can disagree with are
enforced.

The falsifier must name an observation that could occur WHILE EVERY EXISTING
GATE STILL PASSES. "The tests fail" restates the merge gate and says nothing
about the architectural claim; that shape is refused.

--due is required even when --condition is the real trigger. A horizon nothing
can detect can never be reported overdue, and an undetectable horizon is
decoration.

--scope names code that exists ONLY to test this belief. It does not remove
governance: the established envelope still holds, and what it says is that the
design inside that envelope is provisional. "sensei epistemic scope" reports
when canonical architecture starts defending it anyway.

`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}

	h := epistemic.Hypothesis{
		ID: strings.TrimSpace(*id), Question: strings.TrimSpace(*question),
		Alternative: strings.TrimSpace(*alternative), Prediction: strings.TrimSpace(*prediction),
		Falsifier:  strings.TrimSpace(*falsifier),
		Horizon:    epistemic.Horizon{DueAt: strings.TrimSpace(*due), Condition: strings.TrimSpace(*condition)},
		DeclaredBy: strings.TrimSpace(*declaredBy), DeclaredAt: time.Now().UTC().Format(time.RFC3339),
		ExperimentalScope: scope, Notes: strings.TrimSpace(*notes),
	}
	l, err := loadLedger(*ledgerPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	if errs := l.AddHypothesis(h); errs != nil {
		return reportErrs("hypothesis", errs)
	}
	if err := saveLedger(*ledgerPath, l); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	fmt.Printf("recorded: %s  (about %s / %s)\n", h.ID, h.Question, h.Alternative)
	fmt.Printf("due:      %s\n", h.Horizon.DueAt)
	if h.Horizon.Condition != "" {
		fmt.Printf("trigger:  %s\n", h.Horizon.Condition)
	}
	fmt.Println("\nUntil an observation is recorded this is AWAITING_HORIZON, and after the")
	fmt.Println("horizon with nothing observed it is OVERDUE. It is never SUPPORTED by silence.")
	return 0
}

// -----------------------------------------------------------------------------
// observe
// -----------------------------------------------------------------------------

func runEpistemicObserve(args []string) int {
	fs := flag.NewFlagSet("sensei epistemic observe", flag.ContinueOnError)
	ledgerPath := fs.String("ledger", DefaultEpistemicLedger, "epistemic ledger path")
	id := fs.String("id", "", "stable observation id")
	hypothesis := fs.String("hypothesis", "", "the hypothesis id this moves")
	what := fs.String("what", "", "what actually happened")
	outcome := fs.String("outcome", "", "refutes | supports | inconclusive")
	observedBy := fs.String("observed-by", defaultActor(), "who observed it")
	viableFor := fs.String("still-viable-for", "", "where a refuted design may still apply (optional)")
	var evidence, conditions repeatable
	fs.Var(&evidence, "evidence", "where someone else can go and check (repeat; at least one)")
	fs.Var(&conditions, "conditions", "the circumstances the prediction failed under (repeat; required on --outcome refutes)")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Record what actually happened, and let it move the belief.

This is the back-edge that closes the loop. A failure gained a causal past when
scars learned to name the change that introduced them; this is the forward arc,
and without it a hypothesis table is a filing cabinet.

Evidence is required. An observation nobody can go and check is a claim, and
keeping those two apart is the whole point of this lane.

A refutation must carry --conditions. "Design B is bad" is almost never what was
observed: B failed under partition plus leader turnover, or above some write
volume. Dropping the condition turns one experiment into a universal
prohibition nobody tested, which would make failed designs a second kind of
frozen dogma. --still-viable-for records what survives.

`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}

	o := epistemic.Observation{
		ID: strings.TrimSpace(*id), Hypothesis: strings.TrimSpace(*hypothesis),
		ObservedAt: time.Now().UTC().Format(time.RFC3339), What: strings.TrimSpace(*what),
		Outcome: epistemic.Outcome(strings.TrimSpace(*outcome)), Evidence: evidence,
		ObservedBy:        strings.TrimSpace(*observedBy),
		FailureConditions: conditions, RemainingApplicability: strings.TrimSpace(*viableFor),
	}
	l, err := loadLedger(*ledgerPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	if errs := l.AddObservation(o); errs != nil {
		return reportErrs("observation", errs)
	}
	if err := saveLedger(*ledgerPath, l); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	for _, h := range l.Hypotheses {
		if h.ID != o.Hypothesis {
			continue
		}
		fmt.Printf("recorded: %s\n", o.ID)
		fmt.Printf("%s is now %s\n", h.ID, epistemic.StateOf(h, l.Observations, time.Now().UTC()))
	}
	return 0
}

// -----------------------------------------------------------------------------
// status — and the §6 overdue tripwire
// -----------------------------------------------------------------------------

func runEpistemicStatus(args []string) int {
	fs := flag.NewFlagSet("sensei epistemic status", flag.ContinueOnError)
	ledgerPath := fs.String("ledger", DefaultEpistemicLedger, "epistemic ledger path")
	asJSON := fs.Bool("json", false, "machine-readable output")
	tripwire := fs.Bool("tripwire", false, "exit 1 when any hypothesis is past due with nothing observed")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Report computed dispositions, hypothesis states, and liveness.

A growing hypothesis table looks like learning. The overdue count is the only
thing distinguishing a learning loop from a filing cabinet, which is why
--tripwire exists and why it ships with the primitive rather than after it.

`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	l, err := loadLedger(*ledgerPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	now := time.Now().UTC()
	live := epistemic.Measure(l.Hypotheses, l.Observations, now)

	if *asJSON {
		type qrow struct {
			ID          string `json:"id"`
			Disposition string `json:"disposition"`
			Because     string `json:"because"`
		}
		type hrow struct {
			ID    string `json:"id"`
			State string `json:"state"`
			DueAt string `json:"due_at"`
		}
		out := struct {
			Questions  []qrow             `json:"design_questions"`
			Hypotheses []hrow             `json:"hypotheses"`
			Liveness   epistemic.Liveness `json:"liveness"`
		}{Liveness: live}
		for _, q := range l.Questions {
			d, why := epistemic.Dispose(q)
			out.Questions = append(out.Questions, qrow{q.ID, string(d), why})
		}
		for _, h := range l.Hypotheses {
			out.Hypotheses = append(out.Hypotheses, hrow{h.ID, string(epistemic.StateOf(h, l.Observations, now)), h.Horizon.DueAt})
		}
		b, _ := json.MarshalIndent(out, "", "  ")
		fmt.Println(string(b))
	} else {
		printEpistemicStatus(l, live, now)
	}

	if *tripwire && live.PastDue > 0 {
		fmt.Fprintf(os.Stderr, "\nsensei epistemic: %d hypothesis(es) past their horizon with nothing observed.\n", live.PastDue)
		fmt.Fprintln(os.Stderr, "Record an observation, or move the horizon and say why. Neither is done by letting the date pass.")
		return 1
	}
	return 0
}

func printEpistemicStatus(l epistemic.Ledger, live epistemic.Liveness, now time.Time) {
	fmt.Printf("\nEpistemic lane — %d question(s), %d hypothesis(es), %d observation(s), %d adoption(s)\n",
		len(l.Questions), len(l.Hypotheses), len(l.Observations), len(l.Adoptions))
	fmt.Println("Not canonical knowledge; nothing here is established until an adoption says so.")

	if len(l.Questions) > 0 {
		fmt.Printf("\n%-40s %-22s %-9s %s\n", "design question", "disposition", "viable", "adopted")
		fmt.Println(strings.Repeat("-", 92))
		qs := append([]epistemic.DesignQuestion(nil), l.Questions...)
		sort.SliceStable(qs, func(i, j int) bool { return qs[i].ID < qs[j].ID })
		for _, q := range qs {
			d, _ := epistemic.Dispose(q)
			viable := 0
			for _, a := range q.Alternatives {
				if a.Viable() {
					viable++
				}
			}
			adopted := "—"
			if alt := adoptedAlt(l, q.ID); alt != "" {
				adopted = alt
			}
			fmt.Printf("%-40s %-22s %d of %-5d %s\n", truncate(q.ID, 40), d, viable, len(q.Alternatives), adopted)
		}
	}

	if len(l.Hypotheses) > 0 {
		fmt.Printf("\n%-40s %-18s %-22s %s\n", "hypothesis", "state", "due", "observations")
		fmt.Println(strings.Repeat("-", 92))
		hs := append([]epistemic.Hypothesis(nil), l.Hypotheses...)
		sort.SliceStable(hs, func(i, j int) bool { return hs[i].ID < hs[j].ID })
		for _, h := range hs {
			n := 0
			for _, o := range l.Observations {
				if o.Hypothesis == h.ID {
					n++
				}
			}
			fmt.Printf("%-40s %-18s %-22s %d\n", truncate(h.ID, 40),
				epistemic.StateOf(h, l.Observations, now), h.Horizon.DueAt, n)
		}
	}

	fmt.Printf("\nliveness: total=%d active=%d past_due=%d awaiting_horizon=%d supported=%d refuted=%d inconclusive=%d\n",
		live.Total, live.Active, live.PastDue, live.AwaitingHorizon, live.Supported, live.Refuted, live.Inconclusive)
	if live.Ratio == nil {
		// Absent, never 0.000. A rate with no denominator would report an empty
		// table as a total failure.
		fmt.Println("past_due / active: absent (no active hypotheses)")
	} else {
		fmt.Printf("past_due / active: %.3f\n", *live.Ratio)
	}

	// The shape of a fake reasoning loop, counted rather than gated. It is also
	// the shape of an honest one-person project, which is why this reports and
	// never fails: a number nobody can see is how the shape becomes normal.
	if live.SelfConfirmedRatio == nil {
		fmt.Println("self-confirmed:    absent (nothing supported yet)")
	} else {
		fmt.Printf("self-confirmed:    %d of %d supported (%.3f) — supported only by the actor that declared the belief\n",
			live.SelfConfirmed, live.Supported, *live.SelfConfirmedRatio)
		if live.SelfConfirmed == live.Supported {
			fmt.Println("                   reasoning has not yet escaped the reasoner here.")
		}
	}
}

// -----------------------------------------------------------------------------
// scope — the anti-sediment check
// -----------------------------------------------------------------------------

// runEpistemicScope reports experimental code that canonical architecture has
// begun to defend, and experimental code whose hypothesis was refuted.
//
// Code written to test a belief must not become governing architecture merely
// because it exists. Otherwise the loop closes on itself: an agent guesses B,
// implements B, extraction records that B exists, B becomes architecture, and
// the agent can no longer replace its own guess.
func runEpistemicScope(args []string) int {
	fs := flag.NewFlagSet("sensei epistemic scope", flag.ContinueOnError)
	ledgerPath := fs.String("ledger", DefaultEpistemicLedger, "epistemic ledger path")
	repoRoot := fs.String("repo-root", ".", "repository root holding docs/awareness/")
	asJSON := fs.Bool("json", false, "machine-readable output")
	tripwire := fs.Bool("tripwire", false, "exit 1 when any finding is reported")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Report experimental code that established architecture has started to defend.

Naming an experimental scope does NOT remove governance. The established
envelope still holds — surrounding invariants, contracts and forbidden fixes
apply exactly as before. What it says is narrower: the design inside that
envelope is provisional and may be rewritten while the question is open.

  Conserve the envelope. Explore inside it.

ARCHITECTURE_BY_SEDIMENT  canonical architecture cites a path that exists only
                          to test a hypothesis that is still open
ORPHANED_EXPERIMENT       the hypothesis was refuted; the code written to test
                          it is still declared as its scope

`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	l, err := loadLedger(*ledgerPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	established, err := establishedAnchors(*repoRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	findings := l.CheckSediment(established, time.Now().UTC())

	if *asJSON {
		b, _ := json.MarshalIndent(struct {
			AnchoredPaths int                         `json:"anchored_paths"`
			Findings      []epistemic.SedimentFinding `json:"findings"`
		}{len(established), findings}, "", "  ")
		fmt.Println(string(b))
	} else {
		fmt.Printf("\nExperimental scope — %d hypothesis(es), %d canonical anchor path(s) read\n",
			len(l.Hypotheses), len(established))
		if len(findings) == 0 {
			fmt.Println("No experimental code is being defended as established architecture.")
		}
		for _, f := range findings {
			fmt.Printf("\n%s\n  path:       %s\n  hypothesis: %s (%s)\n", f.Kind, f.Path, f.Hypothesis, f.State)
			if len(f.CitedBy) > 0 {
				fmt.Printf("  cited by:   %s\n", strings.Join(f.CitedBy, ", "))
			}
			fmt.Printf("  %s\n", f.Detail)
		}
	}
	if *tripwire && len(findings) > 0 {
		return 1
	}
	return 0
}

// establishedAnchors reads the canonical corpus for paths it treats as
// established architecture, mapped to the entries citing them.
//
// Deliberately narrow: an invariant's protects.files and the high-risk list.
// Those are the two surfaces that say "this path is load-bearing", which is
// exactly the claim experimental code must not acquire by existing. Widening
// this later is additive; guessing at more surfaces now would report findings
// nobody can act on.
func establishedAnchors(repoRoot string) (map[string][]string, error) {
	out := map[string][]string{}

	var inv struct {
		Invariants []struct {
			ID       string `yaml:"id"`
			Protects struct {
				Files []string `yaml:"files"`
			} `yaml:"protects"`
		} `yaml:"invariants"`
	}
	if err := readYAML(filepath.Join(repoRoot, "docs", "awareness", "invariants.yaml"), &inv); err != nil {
		return nil, err
	}
	for _, i := range inv.Invariants {
		for _, f := range i.Protects.Files {
			out[f] = append(out[f], "invariant:"+i.ID)
		}
	}

	var hrf struct {
		Files []string `yaml:"files"`
	}
	if err := readYAML(filepath.Join(repoRoot, "docs", "awareness", "high_risk_files.yaml"), &hrf); err != nil {
		return nil, err
	}
	for _, f := range hrf.Files {
		out[f] = append(out[f], "high_risk_files")
	}
	return out, nil
}

func readYAML(path string, into any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// A repository without this surface simply anchors nothing. That is
			// not an error, and reporting it as one would make the check
			// unusable everywhere except this repository.
			return nil
		}
		return err
	}
	return yaml.Unmarshal(b, into)
}

// adoptedAlt returns the alternative adopted for a question, if any.
func adoptedAlt(l epistemic.Ledger, question string) string {
	for _, a := range l.Adoptions {
		if a.Question == question {
			return a.Alternative
		}
	}
	return ""
}
