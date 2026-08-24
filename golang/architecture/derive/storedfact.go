// SPDX-License-Identifier: AGPL-3.0-only

package derive

// What durable storage may hold about a derived fact.
//
// # Storage remembers what to CHECK, not what is TRUE
//
// This is the resolution to the problem §8f named. A derived fact is
// proposition + derivation + observation envelope; strip any of those and you
// have a different, stronger proposition nobody established. But a stored
// object cannot carry an envelope's meaning safely, because storage is exactly
// where the scope gets dropped and where a forged entry would enter.
//
// So a StoredFact is not the fact. It is the RECIPE: the typed proposition and
// the derivation that can decide it, plus provenance describing where it was
// first established. Reading one back gives a caller something to re-run, never
// something to believe.
//
//	Derive        -> Established   (the fact, in memory, scoped, now)
//	Admit         -> StoredFact    (the recipe, durable)
//	Revalidate    -> Established   (the fact again, in THIS world)
//
// That has three consequences worth stating.
//
// A forged StoredFact is harmless as a truth claim. Anybody can write the YAML;
// nobody can make the derivation succeed by writing it. The worst a fabricated
// entry achieves is wasting one derivation.
//
// Supersession needs no engine. A stored recipe whose re-derivation stops
// succeeding simply stops producing a fact — there is no cached truth to
// invalidate, because none was cached.
//
// And the ratchet still holds, which is the point. What the project no longer
// has to rediscover is WHICH PROPOSITION IS WORTH CHECKING HERE — the expensive,
// judgment-bearing half that an agent's investigation produced. Recomputing the
// answer is parsing a package.
//
// # Descriptive, not normative
//
// A StoredFact says "this was independently derived as holding, within this
// envelope, at this world". It does not say "this must remain true". Deriving
// that Bus.subs is accessed under Bus.mu does not oblige any future
// implementation to keep that mutex — tomorrow's architecture may correctly
// replace it with ownership, channels or an atomic. Turning a description into
// a preservation requirement is epistemic inflation, and Normative() exists to
// be permanently false rather than to be forgotten.

import (
	"fmt"
	"strings"
	"time"
)

// StoredFact is the durable record: a proposition and the derivation that can
// decide it, with provenance about where it was first established.
//
// Every field is inert. None of them asserts that the proposition holds now.
type StoredFact struct {
	Proposition       Proposition `json:"proposition" yaml:"proposition"`
	DerivationID      string      `json:"derivation_id" yaml:"derivation_id"`
	DerivationVersion string      `json:"derivation_version" yaml:"derivation_version"`

	// FirstEstablished describes the world where a derivation last succeeded.
	// Provenance, not authority: a reader may not conclude anything about the
	// current world from it.
	FirstEstablished Provenance `json:"first_established" yaml:"first_established"`
}

// Provenance is where and how the proposition was established once.
type Provenance struct {
	Repository        string   `json:"repository" yaml:"repository"`
	Commit            string   `json:"pinned_commit" yaml:"pinned_commit"`
	Inputs            []string `json:"observed_inputs" yaml:"observed_inputs"`
	CompletenessScope []string `json:"completeness_scope" yaml:"completeness_scope"`
	Detail            string   `json:"detail" yaml:"detail"`
	At                string   `json:"at" yaml:"at"`
}

// Admit turns a successful receipt into something storage may hold.
//
// The only entry point, and it refuses anything that did not derive. UNKNOWN
// and NOT_DERIVED are not weaker forms of a fact, so there is no path by which
// either becomes durable.
func Admit(r Receipt) (StoredFact, error) {
	if r.Outcome != Derived {
		return StoredFact{}, fmt.Errorf(
			"a %s receipt establishes nothing and may not be admitted; only a successful derivation may", r.Outcome)
	}
	if strings.TrimSpace(r.DerivationID) == "" || strings.TrimSpace(r.Commit) == "" {
		return StoredFact{}, fmt.Errorf("a receipt with no derivation identity or no pinned commit cannot be re-run")
	}
	return StoredFact{
		Proposition:       r.Proposition,
		DerivationID:      r.DerivationID,
		DerivationVersion: r.DerivationVersion,
		FirstEstablished: Provenance{
			Repository: r.Repository, Commit: r.Commit,
			Inputs: r.Inputs, CompletenessScope: r.CompletenessScope,
			Detail: r.Detail, At: r.ProducedAt,
		},
	}, nil
}

// Revalidate re-runs the derivation in a world and returns what holds THERE.
//
// This is how a stored fact is consumed. There is no accessor that reports the
// proposition as true without doing the work, because the stored record does
// not know whether it is true — only which question to ask.
//
// A derivation whose registered version has moved on is reported rather than
// silently accepted: the same proposition decided by different code is a
// different establishment, and pretending otherwise would let a derivation
// change meaning under facts already relying on it.
func (s StoredFact) Revalidate(src PinnedSource, now time.Time) (Receipt, *Established) {
	receipt, est := Derive(src, s.Proposition, now)
	if receipt.DerivationVersion != "" && s.DerivationVersion != "" &&
		receipt.DerivationVersion != s.DerivationVersion {
		receipt.Detail = fmt.Sprintf(
			"re-derived by %s/%s, but this fact was established by %s/%s; a proposition decided by different code "+
				"is a different establishment — %s",
			receipt.DerivationID, receipt.DerivationVersion, s.DerivationID, s.DerivationVersion, receipt.Detail)
	}
	return receipt, est
}

// Normative reports whether a derived fact obliges anything to stay true.
//
// Permanently false, and it is a method rather than a comment so that a caller
// reaching for "may I treat this as a constraint" gets an answer instead of a
// silence it can interpret.
//
// "Bus.subs is accessed under Bus.mu at commit C" is a description. It does not
// oblige a future implementation to keep the mutex: replacing it with ownership,
// a channel or an atomic may be entirely correct, and the derived fact has no
// standing to forbid that. What a description legitimately does is end
// "Sensei knows nothing here" — which is all the coverage question ever asked.
func (StoredFact) Normative() bool { return false }

// String renders the recipe, not a verdict.
func (s StoredFact) String() string {
	return fmt.Sprintf("%s — decidable by %s/%s; last established at %s of %s over %d file(s)",
		s.Proposition, s.DerivationID, s.DerivationVersion,
		shortCommit(s.FirstEstablished.Commit), s.FirstEstablished.Repository, len(s.FirstEstablished.Inputs))
}
