// SPDX-License-Identifier: AGPL-3.0-only

// Package epistemic holds the pure, I/O-free core of the uncertain-design-belief
// lifecycle: DesignQuestion, Hypothesis and Observation, the disposition
// computed from a question's structure, the state computed from a hypothesis's
// horizon and its observations, and the liveness counters that make an
// unobserved belief visible.
//
// It implements the first slice of globulario/sensei#288, from
// docs/design/experimental-engineering-epistemology.md §3-§7 and §11.
//
// # What this package is FOR
//
// Sensei already has rich classes for established architectural knowledge --
// invariants, contracts, failure modes, forbidden fixes, decisions, patterns.
// What was missing is a lifecycle for a claim that is believed, testable and
// NOT established. Such a claim previously had to masquerade as law or remain
// unrecorded, and neither is honest.
//
// What this package is NOT
//
//   - It is not a routing input. Nothing here is projected into the awareness
//     graph in this slice, so decideRoute cannot consult it even by accident.
//     That is a structural guarantee rather than a promise not to.
//   - It does not grant authority. Failure to retrieve knowledge is still not
//     permission to experiment (§2), and nothing here reads an EMPTY retrieval.
//   - It does not promote anything, transition any status automatically, or
//     learn (§9).
//
// The two rules that shape every type below
//
//   - A disposition is COMPUTED from the decision structure, never authored.
//     There is deliberately no input field an agent could set to reach
//     EXPLORATION, and no field expressing that a question is technically hard
//     -- AUTHORITY is reached by consequence, never by difficulty (§11.7).
//   - unrefuted is not supported, and supported is never derivable from "the
//     tests passed" (§6). A hypothesis whose clock has not matured is
//     AWAITING_HORIZON; one whose clock passed with nothing observed is
//     OVERDUE. Neither is SUPPORTED.
package epistemic

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// -----------------------------------------------------------------------------
// DesignQuestion
// -----------------------------------------------------------------------------

// Alternative is one way the question could be answered.
//
// EliminatedBy names the established constraint that ruled it out, and it is
// the only way an alternative stops being viable. An agent cannot mark an
// alternative dead by opinion: it has to name what killed it, and that name is
// reviewable.
type Alternative struct {
	ID           string `yaml:"id" json:"id"`
	Statement    string `yaml:"statement" json:"statement"`
	EliminatedBy string `yaml:"eliminated_by,omitempty" json:"eliminated_by,omitempty"`
}

// Viable reports whether the alternative survived constraint binding.
func (a Alternative) Viable() bool { return strings.TrimSpace(a.EliminatedBy) == "" }

// Consequence is one effect running the experiment would actually have.
//
// Reversible refers to CONSEQUENCES, not to version control (§7). A branch is
// trivially revertible; the experiment that ran on it may have mutated a
// database, spent quota, published an artifact or sent something outward. So
// "I can revert the commit" is not an answer to this field, and the effect has
// to be named rather than summarised as a boolean alone.
type Consequence struct {
	Effect     string `yaml:"effect" json:"effect"`
	Reversible bool   `yaml:"reversible" json:"reversible"`
}

// DesignQuestion is a positively declared piece of uncertainty.
//
// It is declared by whoever is doing the engineering, to make explicit what
// they must resolve -- not to hand the decision to somebody else. A question
// naming no alternatives is a topic heading and confers nothing (§4).
//
// There is no Disposition field. That is deliberate: see Dispose.
type DesignQuestion struct {
	ID           string        `yaml:"id" json:"id"`
	Objective    string        `yaml:"objective,omitempty" json:"objective,omitempty"`
	Question     string        `yaml:"question" json:"question"`
	DeclaredBy   string        `yaml:"declared_by" json:"declared_by"`
	DeclaredAt   string        `yaml:"declared_at" json:"declared_at"`
	Alternatives []Alternative `yaml:"alternatives" json:"alternatives"`
	// Constraints are ids of established knowledge bound to this question --
	// invariants, failure modes, forbidden fixes, contracts. They are what an
	// alternative's EliminatedBy points at.
	Constraints []string `yaml:"constraints,omitempty" json:"constraints,omitempty"`
	// Consequences are the effects of EXPERIMENTING here, not of shipping the
	// eventual answer. An empty list is refused: "no consequences" is a claim,
	// and an unstated one cannot be reviewed.
	Consequences []Consequence `yaml:"consequences" json:"consequences"`
	Notes        string        `yaml:"notes,omitempty" json:"notes,omitempty"`
}

// Disposition is what a question's own structure says about who resolves it.
type Disposition string

const (
	// DispositionConservation: constraints eliminated all but one alternative.
	// There is no freedom here; established knowledge already decided.
	DispositionConservation Disposition = "CONSERVATION"

	// DispositionExploration: two or more alternatives survive and every named
	// consequence is reversible. This is a CANDIDATE for exploration -- it says
	// the question may be settled by evidence, not that any particular
	// experiment is safe.
	DispositionExploration Disposition = "EXPLORATION_CANDIDATE"

	// DispositionAuthority: two or more alternatives survive and at least one
	// consequence is irreversible. Reached by CONSEQUENCE, never by technical
	// difficulty -- there is no field on DesignQuestion that could express
	// difficulty, and that absence is the enforcement.
	DispositionAuthority Disposition = "AUTHORITY"

	// DispositionOverConstrained: the constraints eliminated every alternative.
	// Not silently folded into CONSERVATION: it means either the constraints
	// conflict or the question was posed wrongly, and both are findings worth
	// surfacing rather than a decision that made itself.
	DispositionOverConstrained Disposition = "OVER_CONSTRAINED"
)

// Dispose computes the disposition and the reason for it.
//
// This function is the reason DesignQuestion carries no disposition field. An
// agent never writes "regime: exploration"; it exposes the decision structure
// that would justify one, and the disposition follows from that structure (§4).
// Anything else is an escape hatch with extra steps.
//
// Dispose assumes a valid question. Validate first; on an invalid one the
// answer is meaningless rather than wrong.
func Dispose(q DesignQuestion) (Disposition, string) {
	viable := 0
	for _, a := range q.Alternatives {
		if a.Viable() {
			viable++
		}
	}
	switch {
	case viable == 0:
		return DispositionOverConstrained,
			fmt.Sprintf("every one of the %d declared alternative(s) names a constraint that eliminated it; either the constraints conflict or the question was posed wrongly", len(q.Alternatives))
	case viable == 1:
		return DispositionConservation,
			"established constraints eliminated all but one alternative; there is no degree of freedom here to explore"
	}
	if irreversible := irreversibleEffects(q.Consequences); len(irreversible) > 0 {
		return DispositionAuthority,
			fmt.Sprintf("%d alternatives remain viable, and experimenting crosses an irreversible consequence: %s", viable, strings.Join(irreversible, "; "))
	}
	return DispositionExploration,
		fmt.Sprintf("%d alternatives remain viable and every named consequence is reversible; this question may be settled by evidence", viable)
}

func irreversibleEffects(cs []Consequence) []string {
	var out []string
	for _, c := range cs {
		if !c.Reversible {
			out = append(out, strings.TrimSpace(c.Effect))
		}
	}
	return out
}

// -----------------------------------------------------------------------------
// Hypothesis
// -----------------------------------------------------------------------------

// Horizon is when a hypothesis becomes due for observation.
//
// DueAt is required and Condition is optional, which is the opposite of how §6
// phrases it ("at its horizon or on a stated condition"). The reason is §6's
// own requirement that overdue hypotheses be DETECTABLE: a condition-only
// horizon such as "after 100 successful reconciliation cycles" never comes due
// on its own, so nothing can report it late, and a horizon nothing can detect
// is the decoration §6 exists to prevent. Condition still travels with it,
// because "100 cycles" is the real trigger and the date is the backstop.
type Horizon struct {
	DueAt     string `yaml:"due_at" json:"due_at"`
	Condition string `yaml:"condition,omitempty" json:"condition,omitempty"`
}

// Hypothesis is what is currently believed about one alternative, in a form
// reality can disagree with.
//
// Mandatory: an observable prediction, an observation horizon, and a falsifier.
// Optional: everything else. Rationale and alternatives prose are the easy
// fields to write and the impossible ones to check; a model will always produce
// plausible architectural oatmeal, so only the fields reality can contradict
// are enforced (§5).
type Hypothesis struct {
	ID       string `yaml:"id" json:"id"`
	Question string `yaml:"question" json:"question"`
	// Alternative is the DesignQuestion alternative this predicts about. A
	// hypothesis floating free of the question it would settle cannot advance
	// anything.
	Alternative string `yaml:"alternative" json:"alternative"`
	Prediction  string `yaml:"prediction" json:"prediction"`
	// Falsifier must name an observation that could occur WHILE EVERY EXISTING
	// GATE STILL PASSES. A falsifier of "the tests fail" restates the merge
	// gate and says nothing about the architectural claim (§5).
	Falsifier  string  `yaml:"falsifier" json:"falsifier"`
	Horizon    Horizon `yaml:"horizon" json:"horizon"`
	DeclaredBy string  `yaml:"declared_by" json:"declared_by"`
	DeclaredAt string  `yaml:"declared_at" json:"declared_at"`
	// ExperimentalScope names repository paths that exist ONLY to test this
	// hypothesis.
	//
	// It is what keeps a guess from becoming law by sediment. Code written to
	// test a belief must not become governing architecture merely because it
	// exists -- otherwise the loop closes on itself: an agent guesses B,
	// implements B, extraction observes B, B becomes architecture, and the
	// agent can no longer replace its own guess. Promotion to architecture is
	// an epistemic event, not a side effect of implementation.
	//
	// Naming the scope does NOT remove governance. The established envelope
	// still holds: the surrounding invariants, contracts and forbidden fixes
	// apply exactly as before. What it says is narrower and specific -- the
	// design INSIDE that envelope is provisional and may be rewritten freely
	// while the question is open. Conserve the envelope, explore inside it.
	ExperimentalScope []string `yaml:"experimental_scope,omitempty" json:"experimental_scope,omitempty"`
	Notes             string   `yaml:"notes,omitempty" json:"notes,omitempty"`
}

// -----------------------------------------------------------------------------
// Observation
// -----------------------------------------------------------------------------

// Outcome is what an observation did to the belief.
type Outcome string

const (
	OutcomeRefutes      Outcome = "refutes"
	OutcomeSupports     Outcome = "supports"
	OutcomeInconclusive Outcome = "inconclusive"
)

var validOutcomes = map[Outcome]bool{
	OutcomeRefutes: true, OutcomeSupports: true, OutcomeInconclusive: true,
}

// Observation is what actually happened: the evidence that moves a belief.
//
// It is the back-edge that closes the loop. A failure gained a causal past when
// scars learned to name the change that introduced them; this is the forward
// arc, and without it a hypothesis table is a filing cabinet.
type Observation struct {
	ID         string   `yaml:"id" json:"id"`
	Hypothesis string   `yaml:"hypothesis" json:"hypothesis"`
	ObservedAt string   `yaml:"observed_at" json:"observed_at"`
	What       string   `yaml:"what" json:"what"`
	Outcome    Outcome  `yaml:"outcome" json:"outcome"`
	Evidence   []string `yaml:"evidence" json:"evidence"`
	ObservedBy string   `yaml:"observed_by" json:"observed_by"`
	// FailureConditions are the circumstances under which the prediction
	// failed. Required on a refutation, because "design B is bad" is almost
	// never what was observed: B failed under partition plus leader turnover,
	// or above some write volume, and a record that drops the condition turns
	// one experiment into a universal prohibition nobody tested.
	FailureConditions []string `yaml:"failure_conditions,omitempty" json:"failure_conditions,omitempty"`
	// RemainingApplicability is where the refuted design may still be viable.
	//
	// Optional, and left blank when nothing survives. Its purpose is to stop a
	// failed design becoming a second kind of frozen dogma: a refuted
	// hypothesis is evidence against one claim under stated conditions, not a
	// standing ban on a mechanism.
	RemainingApplicability string `yaml:"remaining_applicability,omitempty" json:"remaining_applicability,omitempty"`
}

// -----------------------------------------------------------------------------
// Hypothesis state
// -----------------------------------------------------------------------------

// State is what a hypothesis's horizon and observations say about it.
type State string

const (
	// StateRefuted: an observation contradicted the prediction. This is the one
	// state a single observation can reach on its own, and deliberately so --
	// refutation is cheap and support is not.
	StateRefuted State = "REFUTED"

	// StateAwaitingHorizon: the clock has not matured. Never SUPPORTED, however
	// many green runs have accumulated (§6).
	StateAwaitingHorizon State = "AWAITING_HORIZON"

	// StateOverdue: the horizon passed and nothing was observed. This is the
	// tripwire. A growing hypothesis table looks like learning; the overdue
	// count is the only thing distinguishing a learning loop from a filing
	// cabinet.
	StateOverdue State = "OVERDUE"

	// StateSupported: the horizon matured and an observation supported the
	// prediction. Reachable ONLY through a recorded observation -- never from
	// the absence of a refutation, and never from a passing test suite.
	StateSupported State = "SUPPORTED"

	// StateInconclusive: the horizon matured and what was observed did not
	// decide it. An honest and common answer.
	StateInconclusive State = "INCONCLUSIVE"
)

// StateOf computes a hypothesis's state from its horizon and the observations
// recorded against it.
//
// Order matters and encodes §6:
//
//  1. A refutation wins outright, whatever the clock says. A belief contradicted
//     before its horizon is still contradicted.
//  2. Before the horizon, the answer is AWAITING_HORIZON. There is no path from
//     here to SUPPORTED, which is the horizon leak this ordering exists to
//     block -- the most common epistemic move in software engineering is
//     "nothing broke, therefore it works".
//  3. After the horizon with nothing observed, OVERDUE. Not "unrefuted", not
//     "fine so far".
//  4. After the horizon, SUPPORTED requires a supporting observation, and
//     anything else is INCONCLUSIVE.
//
// obs may contain observations for other hypotheses; they are ignored.
func StateOf(h Hypothesis, obs []Observation, now time.Time) State {
	var mine []Observation
	for _, o := range obs {
		if o.Hypothesis == h.ID {
			mine = append(mine, o)
		}
	}
	for _, o := range mine {
		if o.Outcome == OutcomeRefutes {
			return StateRefuted
		}
	}
	due, err := time.Parse(time.RFC3339, strings.TrimSpace(h.Horizon.DueAt))
	if err != nil {
		// An unparseable horizon cannot be reported as not-yet-due: that would
		// let a malformed date buy indefinite silence. Validate rejects this
		// shape; if one reaches here anyway it is treated as already due, so it
		// surfaces in the overdue count rather than hiding in it.
		due = time.Time{}
	}
	if now.Before(due) {
		return StateAwaitingHorizon
	}
	if len(mine) == 0 {
		return StateOverdue
	}
	for _, o := range mine {
		if o.Outcome == OutcomeSupports {
			return StateSupported
		}
	}
	return StateInconclusive
}

// -----------------------------------------------------------------------------
// Liveness
// -----------------------------------------------------------------------------

// Liveness is the §6 counter set: the raw counts, plus the overdue ratio when
// there is a denominator for one.
//
// Ratio is a pointer because an empty denominator has no rate. Printing 0.000
// for "no active hypotheses" would describe a healthy empty table as a total
// failure, and the same discipline already governs the evaluation scorers.
type Liveness struct {
	Total           int      `json:"total"`
	Active          int      `json:"active"`
	PastDue         int      `json:"past_due"`
	AwaitingHorizon int      `json:"awaiting_horizon"`
	Supported       int      `json:"supported"`
	Refuted         int      `json:"refuted"`
	Inconclusive    int      `json:"inconclusive"`
	Ratio           *float64 `json:"past_due_over_active,omitempty"`

	// SelfConfirmed counts SUPPORTED hypotheses where every supporting
	// observation was recorded by the actor that declared the belief.
	//
	// This does not detect dishonesty and does not try to. It counts the shape
	// a fake reasoning loop has: declare a question, predict an answer, observe
	// that the answer was right, and call the result evidence -- one actor
	// congratulating itself at four stages. That shape is also what an honest
	// single-operator project looks like, which is exactly why the number is
	// reported rather than gated. A count nobody can see is how the shape
	// becomes normal.
	SelfConfirmed int `json:"self_confirmed"`
	// SelfConfirmedRatio is SelfConfirmed over Supported, absent when nothing
	// is supported yet. Read it beside SelfConfirmed: 1 of 1 and 40 of 40 are
	// the same ratio and not the same situation.
	SelfConfirmedRatio *float64 `json:"self_confirmed_over_supported,omitempty"`
}

// Measure computes liveness over a hypothesis set.
//
// "Active" is every hypothesis reality has not yet finished with: awaiting its
// horizon, overdue, or matured-but-inconclusive. A refuted or supported
// hypothesis is settled and leaves the denominator, because keeping settled
// beliefs in it would dilute the overdue ratio with work that is done -- the
// exact way this metric would be made to look good without observing anything.
func Measure(hs []Hypothesis, obs []Observation, now time.Time) Liveness {
	var l Liveness
	l.Total = len(hs)
	for _, h := range hs {
		switch StateOf(h, obs, now) {
		case StateRefuted:
			l.Refuted++
		case StateSupported:
			l.Supported++
			if supportedOnlyByItsOwnAuthor(h, obs) {
				l.SelfConfirmed++
			}
		case StateAwaitingHorizon:
			l.AwaitingHorizon++
			l.Active++
		case StateOverdue:
			l.PastDue++
			l.Active++
		case StateInconclusive:
			l.Inconclusive++
			l.Active++
		}
	}
	if l.Active > 0 {
		r := float64(l.PastDue) / float64(l.Active)
		l.Ratio = &r
	}
	if l.Supported > 0 {
		r := float64(l.SelfConfirmed) / float64(l.Supported)
		l.SelfConfirmedRatio = &r
	}
	return l
}

// supportedOnlyByItsOwnAuthor reports whether every observation that supported
// this hypothesis was recorded by whoever declared it.
//
// One independent supporting observation is enough to clear it. The bar is
// deliberately that low: this measures whether anything outside the believer
// ever agreed, not how much did.
func supportedOnlyByItsOwnAuthor(h Hypothesis, obs []Observation) bool {
	author := normalize(h.DeclaredBy)
	for _, o := range obs {
		if o.Hypothesis != h.ID || o.Outcome != OutcomeSupports {
			continue
		}
		if normalize(o.ObservedBy) != author {
			return false
		}
	}
	return true
}

// -----------------------------------------------------------------------------
// Validation
// -----------------------------------------------------------------------------

var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{2,119}$`)

// altIDPattern is looser than idPattern on purpose. An alternative's id is
// local to its question -- "a", "b", "digest" -- and forcing a globally unique
// shape on it would buy nothing and read worse in the one place these are
// actually compared side by side.
var altIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// gateRestatements are falsifier phrasings that restate a gate the project
// already runs.
//
// This list catches the OBVIOUS restatements and nothing more. Whether a
// falsifier names an observation that could occur while every gate still passes
// is a judgment under evidence, and no deterministic check deserves
// architectural authority over it -- the same admission §4 makes about
// "materially distinct". Saying so here matters more than the list being long:
// a validator that implied it had settled the question would be making exactly
// the too-strong claim this lane exists to record honestly.
var gateRestatements = []string{
	"test fails", "tests fail", "the test fails", "the tests fail",
	"test failure", "tests failing", "a test fails", "any test fails",
	"tests pass", "test passes", "the tests pass", "all tests pass",
	"ci fails", "ci is red", "ci goes red", "ci breaks", "ci turns red",
	"ci passes", "ci is green", "ci stays green",
	"the build fails", "build fails", "the build breaks", "build breaks",
	"the suite fails", "suite fails",
	"go test fails", "go vet fails", "gofmt fails",
	"the linter fails", "lint fails",
}

var spaces = regexp.MustCompile(`\s+`)

func normalize(s string) string {
	return spaces.ReplaceAllString(strings.ToLower(strings.TrimSpace(s)), " ")
}

// ValidateQuestion enforces §4. An empty slice means acceptable; otherwise
// every problem is named at once, because fixing one at a time is how a
// reviewer's attention gets spent on the form instead of the content.
func ValidateQuestion(q DesignQuestion) []string {
	var errs []string
	if !idPattern.MatchString(q.ID) {
		errs = append(errs, fmt.Sprintf("id %q must be 3-120 chars of [a-z0-9._-] starting alphanumeric", q.ID))
	}
	if strings.TrimSpace(q.Question) == "" {
		errs = append(errs, "question is required")
	}
	if strings.TrimSpace(q.DeclaredBy) == "" {
		errs = append(errs, "declared_by is required (who declared this uncertainty)")
	}
	if err := requireRFC3339(q.DeclaredAt, "declared_at"); err != "" {
		errs = append(errs, err)
	}

	// Two, because one alternative is a decision already made.
	if len(q.Alternatives) < 2 {
		errs = append(errs, fmt.Sprintf("a design question needs at least 2 alternatives, got %d; one alternative is a decision already made, and none is a topic heading", len(q.Alternatives)))
	}
	seenID := map[string]bool{}
	seenStatement := map[string]bool{}
	for i, a := range q.Alternatives {
		if !altIDPattern.MatchString(a.ID) {
			errs = append(errs, fmt.Sprintf("alternative %d: id %q must be 1-64 chars of [a-z0-9._-] starting alphanumeric", i, a.ID))
		}
		if seenID[a.ID] {
			errs = append(errs, fmt.Sprintf("alternative %d: duplicate id %q", i, a.ID))
		}
		seenID[a.ID] = true
		st := normalize(a.Statement)
		if st == "" {
			errs = append(errs, fmt.Sprintf("alternative %d (%s): statement is required", i, a.ID))
			continue
		}
		// This catches only VERBATIM duplication. "Materially distinct" is the
		// part no deterministic validator can settle (§4), and pretending
		// otherwise here would be the manufactured-alternatives failure with a
		// green check next to it.
		if seenStatement[st] {
			errs = append(errs, fmt.Sprintf("alternative %d (%s): its statement repeats an earlier alternative verbatim; two alternatives must be materially distinct, and this check catches only exact repetition", i, a.ID))
		}
		seenStatement[st] = true
		if !a.Viable() && !containsFold(q.Constraints, a.EliminatedBy) {
			errs = append(errs, fmt.Sprintf("alternative %d (%s): eliminated_by %q is not one of the question's bound constraints; an alternative may only be ruled out by named established knowledge", i, a.ID, a.EliminatedBy))
		}
	}

	// "No consequences" is a claim about the world, and an unstated claim
	// cannot be reviewed. §7 makes the consequence boundary the thing that
	// licenses experimenting at all, so it is not an optional field.
	if len(q.Consequences) == 0 {
		errs = append(errs, "consequences is required: name what experimenting here would actually do (§7 is about consequences, not version control — 'I can revert the commit' is not an answer)")
	}
	for i, c := range q.Consequences {
		if strings.TrimSpace(c.Effect) == "" {
			errs = append(errs, fmt.Sprintf("consequence %d: effect is required", i))
		}
	}
	return errs
}

// ValidateHypothesis enforces §5.
func ValidateHypothesis(h Hypothesis) []string {
	var errs []string
	if !idPattern.MatchString(h.ID) {
		errs = append(errs, fmt.Sprintf("id %q must be 3-120 chars of [a-z0-9._-] starting alphanumeric", h.ID))
	}
	if strings.TrimSpace(h.Question) == "" {
		errs = append(errs, "question is required: a hypothesis floating free of the question it would settle advances nothing")
	}
	if strings.TrimSpace(h.Alternative) == "" {
		errs = append(errs, "alternative is required: name which alternative this predicts about")
	}
	if strings.TrimSpace(h.Prediction) == "" {
		errs = append(errs, "prediction is required and must be observable")
	}
	if strings.TrimSpace(h.DeclaredBy) == "" {
		errs = append(errs, "declared_by is required")
	}
	if err := requireRFC3339(h.DeclaredAt, "declared_at"); err != "" {
		errs = append(errs, err)
	}

	f := normalize(h.Falsifier)
	switch {
	case f == "":
		errs = append(errs, "falsifier is required: name an observation that would show the prediction wrong")
	default:
		for _, bad := range gateRestatements {
			if strings.Contains(f, bad) {
				errs = append(errs, fmt.Sprintf("falsifier restates an existing gate (%q): it must name an observation that could occur WHILE EVERY EXISTING GATE STILL PASSES, or it says nothing about the architectural claim", bad))
				break
			}
		}
	}

	if err := requireRFC3339(h.Horizon.DueAt, "horizon.due_at"); err != "" {
		errs = append(errs, err+"; a horizon nothing can detect cannot be reported overdue, and an undetectable horizon is the decoration §6 exists to prevent")
	}
	return errs
}

// ValidateObservation enforces §3's observation shape.
func ValidateObservation(o Observation) []string {
	var errs []string
	if !idPattern.MatchString(o.ID) {
		errs = append(errs, fmt.Sprintf("id %q must be 3-120 chars of [a-z0-9._-] starting alphanumeric", o.ID))
	}
	if strings.TrimSpace(o.Hypothesis) == "" {
		errs = append(errs, "hypothesis is required: an observation that moves no belief is a log line")
	}
	if strings.TrimSpace(o.What) == "" {
		errs = append(errs, "what is required: state what actually happened")
	}
	if strings.TrimSpace(o.ObservedBy) == "" {
		errs = append(errs, "observed_by is required")
	}
	if err := requireRFC3339(o.ObservedAt, "observed_at"); err != "" {
		errs = append(errs, err)
	}
	if !validOutcomes[o.Outcome] {
		errs = append(errs, fmt.Sprintf("outcome %q is not one of refutes|supports|inconclusive", o.Outcome))
	}
	if len(o.Evidence) == 0 {
		errs = append(errs, "evidence is required: an observation nobody can go and check is a claim, which is the thing this lane exists to keep separate from evidence")
	}
	if o.Outcome == OutcomeRefutes && len(o.FailureConditions) == 0 {
		// A refutation without its conditions is how one experiment becomes a
		// universal prohibition. The next agent meets "design B failed" with no
		// way to tell whether their use has the same failure condition.
		errs = append(errs, "a refutation requires failure_conditions: state the circumstances the prediction failed under, or one experiment becomes a standing ban on a mechanism nobody tested that broadly")
	}
	return errs
}

func requireRFC3339(s, field string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return field + " is required (RFC3339)"
	}
	if _, err := time.Parse(time.RFC3339, s); err != nil {
		return fmt.Sprintf("%s %q is not RFC3339", field, s)
	}
	return ""
}

func containsFold(list []string, want string) bool {
	want = normalize(want)
	for _, s := range list {
		if normalize(s) == want {
			return true
		}
	}
	return false
}
