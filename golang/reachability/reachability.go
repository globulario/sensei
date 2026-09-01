// SPDX-License-Identifier: AGPL-3.0-only

// Package reachability answers one question a governed decision could not
// previously ask: is the knowledge I am deciding from the knowledge that has
// been admitted?
//
// The graph already states which source revision produced it
// (GraphBuildCommit). Nothing compared that to the authored corpus, so a
// briefing reported "authoritative (current)" while the live store was ELEVEN
// DAYS behind the corpus -- because "current" was measured against the
// published artifact's own marker, and an artifact always matches itself.
//
// Every law admitted in that window was authored, reviewed, merged and
// unreachable, and no surface said so.
//
// WHAT THIS PACKAGE REFUSES TO DO. It does not publish, rebuild, or mutate
// anything. Reachability is a REPORT. A stale generation is reported as stale
// knowledge and never as absence of law: "the graph has no rule about this" and
// "the graph that would have the rule is eleven days old" are different
// answers, and collapsing them is the failure this exists to prevent.
package reachability

import (
	"fmt"
	"strings"
)

// State is a CLOSED set read by membership. `Unknown` is a member, not a
// fallback: a reachability question that cannot be answered is answered
// `Unknown`, never silently answered `Current`.
type State string

const (
	// StateCurrent: the published generation contains every authored change.
	StateCurrent State = "current"
	// StateStale: the authored corpus has moved past the published generation.
	// The knowledge exists and is admitted; the decision surface cannot see it.
	StateStale State = "stale"
	// StateUnknown: the relationship could not be established. NOT current.
	StateUnknown State = "unknown"
)

// Inputs is what an assessment needs. Every field may be empty, and an empty
// field produces Unknown rather than an assumption.
type Inputs struct {
	// PublishedCommit is the source revision the serving graph states it was
	// built from.
	PublishedCommit string
	// CorpusCommit is the revision of the authored corpus the caller holds.
	CorpusCommit string
	// CommitsAhead is how many corpus-touching commits the caller has that the
	// published generation does not. Meaningful only when Contains is true.
	CommitsAhead int
	// Contains reports whether the caller's history contains PublishedCommit.
	// False means the two cannot be ordered, which is Unknown -- not stale and
	// certainly not current.
	Contains bool
	// AheadKnown reports whether CommitsAhead was actually MEASURED.
	//
	// Without it the zero value is indistinguishable from a measured zero, so a
	// failed count read as "0 changes behind" and therefore as CURRENT -- a
	// fail-open in the very package written to stop one. An unmeasured distance
	// is Unknown.
	AheadKnown bool
}

// Assessment is the typed answer.
type Assessment struct {
	State           State
	PublishedCommit string
	CorpusCommit    string
	CommitsAhead    int
	// Detail names the reason in words that do not overstate it.
	Detail string
}

// Reachable reports whether newly admitted knowledge is visible to the surface
// that produced this assessment. Only one state may say yes.
func (a Assessment) Reachable() bool { return a.State == StateCurrent }

// AssertsAbsence is always false, and exists to be read.
//
// No reachability state licenses "the graph has nothing to say about this".
// That is a statement about the CORPUS; this package only measures the distance
// between a corpus and a generation.
func (a Assessment) AssertsAbsence() bool { return false }

// Assess is pure: no git, no filesystem, no network.
func Assess(in Inputs) Assessment {
	out := Assessment{
		PublishedCommit: strings.TrimSpace(in.PublishedCommit),
		CorpusCommit:    strings.TrimSpace(in.CorpusCommit),
		CommitsAhead:    in.CommitsAhead,
	}
	switch {
	case out.PublishedCommit == "":
		out.State = StateUnknown
		out.Detail = "the serving graph does not state which revision produced it"
	case out.CorpusCommit == "":
		out.State = StateUnknown
		out.Detail = "the authored corpus revision could not be resolved from this caller"
	case !in.Contains:
		// The published generation came from a history this caller does not
		// have. It may be newer, older or unrelated. Reporting `stale` would
		// invent an ordering; reporting `current` would invent agreement.
		out.State = StateUnknown
		out.Detail = fmt.Sprintf("the serving graph was built from %s, which this checkout does not contain", short(out.PublishedCommit))
	case sameRevision(out.PublishedCommit, out.CorpusCommit):
		out.State = StateCurrent
		out.Detail = "the serving graph was built from the authored corpus revision this caller holds"
	case !in.AheadKnown:
		out.State = StateUnknown
		out.Detail = "the distance between the corpus and the serving graph could not be measured"
	case in.CommitsAhead == 0:
		out.State = StateCurrent
		out.Detail = "the serving graph contains every authored corpus change this caller can see"
	default:
		out.State = StateStale
		out.Detail = fmt.Sprintf("%d authored corpus change(s) are admitted but NOT reachable by the serving graph, built from %s",
			in.CommitsAhead, short(out.PublishedCommit))
	}
	return out
}

// sameRevision compares two git revisions that may be abbreviated to different
// lengths. Prefix comparison is correct here and equality is not: the graph
// reports a 12-character commit while a caller resolves a 40-character one.
func sameRevision(a, b string) bool {
	a, b = strings.ToLower(a), strings.ToLower(b)
	if a == "" || b == "" {
		return false
	}
	if len(a) > len(b) {
		a, b = b, a
	}
	return strings.HasPrefix(b, a)
}

func short(rev string) string {
	if len(rev) > 12 {
		return rev[:12]
	}
	return rev
}

// Line renders the assessment for a human-readable authority block.
func (a Assessment) Line() string {
	switch a.State {
	case StateCurrent:
		return "Reachability: current — admitted knowledge is visible to this graph"
	case StateStale:
		return fmt.Sprintf("Reachability: STALE — %d admitted corpus change(s) are NOT reachable here; "+
			"a missing rule may be unpublished rather than absent", a.CommitsAhead)
	default:
		return "Reachability: UNKNOWN — " + a.Detail + "; treat a missing rule as unestablished, not as absent"
	}
}
