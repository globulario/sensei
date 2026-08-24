// SPDX-License-Identifier: AGPL-3.0-only

// Package derive establishes narrow architectural propositions by computing
// them from pinned project state, rather than by believing a claimant.
//
// # Why this exists
//
// Three candidates were run through `sensei promote` against an isolated graph:
// a true claim, a plausible well-formed FALSE claim about the same file, and a
// claim whose evidence cited only artifacts the same change introduced. All
// three were accepted, because the whole evidential check was that a string was
// non-empty. #298 then made evidence references independently obtainable — the
// bytes come from git, not from the candidate — which kills fabricated
// citations but not the B specimen: real lines, wrong architectural conclusion.
//
// So there is deliberately NO generic transition here:
//
//	EVIDENCE_VERIFIED -> ESTABLISHED
//
// What this package adds is one rung further up: a proposition Sensei computes
// itself, from bytes the claimant did not supply, using a registered derivation
// whose result is reproducible from the same pinned inputs.
//
// # The guarantee is structural, not a flag
//
// Established has no exported fields and no exported constructor. The only
// value of that type anybody can obtain comes back from Derive, and Derive
// returns one only when a registered derivation actually succeeded. There is no
// boolean to flip and no path to widen: a caller outside this package cannot
// construct an Established at all.
//
// That replaces the earlier shape — a function returning constant false with a
// comment explaining why — which was honest but only as durable as the next
// person's willingness to leave it alone.
//
// # Derivability is an output
//
// Nothing here reads a claimant-supplied "this is mechanically derivable"
// label. A proposition is typed; Sensei looks for a registered derivation that
// applies to that type and attempts it. Success establishes it, failure does
// not, and no applicable derivation returns UNKNOWN — which is an answer, and
// must never soften into a weaker admission path (design doc §8d).
package derive

import (
	"fmt"
	"strings"
	"time"
)

// Kind is a proposition family this package knows how to attempt.
//
// One family, chosen because a real coverage gap needed it:
// internal/event/bus.go answered PREFLIGHT_STATUS_EMPTY with "no anchored rules
// apply", and the architectural fact underneath it is a lock discipline.
type Kind string

const (
	// KindFieldAccessUnderLock: every access to a named field of a named struct
	// occurs while a named lock field of the same struct is held.
	//
	// Deliberately factual and deliberately narrow. It says WHETHER the
	// discipline holds. It says nothing about WHY the lock exists, which is a
	// different proposition that no derivation here attempts.
	KindFieldAccessUnderLock Kind = "field_access_under_lock"
)

// Proposition is a claim in a shape a derivation can attempt.
//
// Typed rather than prose, and that is the load-bearing part. "The mutex
// serializes map access" cannot be attempted, argued with, or reproduced; it
// can only be believed. A proposition names entities and a relation, so a
// machine can go and check.
type Proposition struct {
	Kind Kind `json:"kind" yaml:"kind"`
	// Dir is the package directory, repository-relative.
	Dir string `json:"dir" yaml:"dir"`
	// Type is the struct type whose field is claimed to be protected.
	Type string `json:"type" yaml:"type"`
	// Field is the protected field.
	Field string `json:"field" yaml:"field"`
	// Lock is the field holding the mutex.
	Lock string `json:"lock" yaml:"lock"`
}

func (p Proposition) String() string {
	return fmt.Sprintf("every access to %s.%s in %s occurs while %s.%s is held",
		p.Type, p.Field, p.Dir, p.Type, p.Lock)
}

// Outcome is what an attempt produced. Three, and the third is not a failure.
type Outcome string

const (
	// Derived: the derivation ran and the proposition holds over the pinned
	// inputs it read.
	Derived Outcome = "DERIVED"
	// NotDerived: the derivation ran and found a counterexample. This is a
	// stronger answer than UNKNOWN and it refutes the proposition.
	NotDerived Outcome = "NOT_DERIVED"
	// Unknown: no registered derivation applies, or the inputs could not be
	// read. Nothing was established and nothing was refuted.
	//
	// A purpose claim lands here. So does a claim about a family nobody has
	// taught Sensei to compute. Both are honest, and neither may be quietly
	// routed to a weaker admission path.
	Unknown Outcome = "UNKNOWN"
)

// Receipt is what Sensei derived, not what anybody said it derived.
//
// The reproducibility property is the whole value: the same derivation at the
// same version over the same pinned commit reads the same inputs and reaches
// the same outcome. A receipt somebody cannot re-run is a claim about a
// computation rather than a record of one.
type Receipt struct {
	DerivationID      string      `json:"derivation_id" yaml:"derivation_id"`
	DerivationVersion string      `json:"derivation_version" yaml:"derivation_version"`
	Proposition       Proposition `json:"proposition" yaml:"proposition"`
	Repository        string      `json:"repository" yaml:"repository"`
	// Commit pins the world. Without it the outcome describes whatever happened
	// to be checked out, which is a fact about nothing.
	Commit string `json:"pinned_commit" yaml:"pinned_commit"`
	// Inputs are the exact paths read, obtained from git rather than supplied
	// by the proposer.
	Inputs     []string `json:"independently_observed_inputs" yaml:"independently_observed_inputs"`
	Outcome    Outcome  `json:"result" yaml:"result"`
	Detail     string   `json:"detail" yaml:"detail"`
	ProducedAt string   `json:"produced_at" yaml:"produced_at"`
	// Invalidatedby records what would end this fact's authority. Principle 6
	// of the BMG ladder: every promoted rule carries a path by which it can be
	// weakened or revoked, and a fact established at commit C must not silently
	// outlive the world it described.
	InvalidatedBy []string `json:"invalidated_by" yaml:"invalidated_by"`
}

// Established is a proposition Sensei computed for itself.
//
// Unexported fields, no exported constructor, and the only function returning
// one is Derive. A caller cannot build this from verified evidence, from a
// model's agreement, or from a boolean somebody set — those paths do not exist
// in the type system, which is stronger than them existing and being refused.
type Established struct {
	proposition Proposition
	receipt     Receipt
}

// Proposition returns exactly what was established.
func (e Established) Proposition() Proposition { return e.proposition }

// Receipt returns the reproducible record behind it.
func (e Established) Receipt() Receipt { return e.receipt }

// Scope is the narrowest true statement of what this establishes.
//
// Deliberately not generalised. Deriving that a field is accessed under a lock
// at commit C establishes that, at commit C. It does not establish that the
// lock EXISTS for that reason, and it does not bind future implementations —
// those are different propositions, and BMG Principle 3 is that a rule learned
// under a condition may not be applied outside it.
func (e Established) Scope() string {
	return fmt.Sprintf("%s — at %s of %s, over the %d file(s) read; "+
		"establishes that the discipline holds, not why the lock exists, and not that it must hold in future revisions",
		e.proposition, shortCommit(e.receipt.Commit), e.receipt.Repository, len(e.receipt.Inputs))
}

// Deriver computes one proposition family from pinned source.
type Deriver interface {
	ID() string
	Version() string
	Applies(Proposition) bool
	// Derive reads its own inputs. It is handed a reader for the pinned tree
	// rather than any content the proposer supplied.
	Derive(src PinnedSource, p Proposition) (Outcome, []string, string)
}

// PinnedSource reads a repository at one immutable revision.
type PinnedSource interface {
	Repository() string
	Commit() string
	// List returns repo-relative paths under dir at the pinned commit.
	List(dir string) ([]string, error)
	// Read returns the bytes of one path at the pinned commit.
	Read(path string) ([]byte, error)
}

// registry is the set of derivations Sensei can attempt. Adding a family is a
// reviewed change to this list, not something a claimant can request.
var registry = []Deriver{lockDiscipline{}}

// Derive attempts a proposition against pinned project state.
//
// The claimant may choose the proposition and may point at where to look. It
// does not supply the bytes, does not choose which derivation applies, and does
// not decide the outcome.
func Derive(src PinnedSource, p Proposition, now time.Time) (Receipt, *Established) {
	receipt := Receipt{
		Proposition: p,
		Repository:  src.Repository(),
		Commit:      src.Commit(),
		ProducedAt:  now.UTC().Format(time.RFC3339),
		InvalidatedBy: []string{
			"the pinned revision is superseded and the derivation is not re-run",
			"the derivation no longer succeeds over the same inputs",
			"a registered derivation of the same family changes version",
			"contradictory evidence is recorded against the proposition",
		},
	}
	for _, d := range registry {
		if !d.Applies(p) {
			continue
		}
		receipt.DerivationID, receipt.DerivationVersion = d.ID(), d.Version()
		outcome, inputs, detail := d.Derive(src, p)
		receipt.Outcome, receipt.Inputs, receipt.Detail = outcome, inputs, detail
		if outcome != Derived {
			return receipt, nil
		}
		return receipt, &Established{proposition: p, receipt: receipt}
	}
	receipt.Outcome = Unknown
	receipt.Detail = fmt.Sprintf("no registered derivation applies to a %q proposition; "+
		"nothing was established and nothing was refuted", p.Kind)
	return receipt, nil
}

func shortCommit(c string) string {
	if c = strings.TrimSpace(c); len(c) > 12 {
		return c[:12]
	}
	return c
}
