// SPDX-License-Identifier: AGPL-3.0-only

// Package subjectstate is the single owner of the question every governed
// decision starts from: what does the graph know about this subject?
//
// WHY IT EXISTS. Preflight, change-impact and briefing each rebuilt that answer
// out of raw slices, and every rebuild was a fresh chance to filter in the
// wrong order. Measured 2026-09-01, the lifecycle predicate alone had FOUR call
// sites and THREE different semantics. The twenty-odd cap/lifecycle/applicability
// defects repaired in #317 and #318 were not twenty bugs; they were one question
// with four owners.
//
// THE ORDER IS THE POINT:
//
//	complete knowledge -> lifecycle filtering -> governance decision -> presentation
//
// never
//
//	knowledge -> presentation projection -> decision
//
// A State is built once from the complete raw material, and every decision
// reads the State. Caps, sorting and truncation belong to whatever renders a
// response, strictly afterwards, and this package deliberately offers no way to
// cap anything.
package subjectstate

import (
	"sort"
	"strings"
)

// Class is the CLOSED set of governed anchor classes. It is read by membership.
//
// Reading it by naming some of its members is the specific mistake that
// produced a response saying "no governing rule applies" while the same
// response let a contract decide authority: coverage knew three classes,
// examination knew five.
type Class string

const (
	ClassInvariant    Class = "invariant"
	ClassFailureMode  Class = "failure_mode"
	ClassIntent       Class = "intent"
	ClassForbiddenFix Class = "forbidden_fix"
	ClassContract     Class = "contract"
)

// AllClasses is the membership list. A decision that is class-complete iterates
// this; a decision that is deliberately narrower must name the classes it uses
// AND say why at the point of use.
func AllClasses() []Class {
	return []Class{ClassInvariant, ClassFailureMode, ClassIntent, ClassForbiddenFix, ClassContract}
}

// Node is the minimum this package needs. It is an interface so the package
// carries no protobuf dependency and can be exercised without one.
type Node interface {
	GetId() string
	GetIri() string
	GetStatus() string
	GetSeverity() string
}

// Examination has THREE states, and collapsing the middle one into either
// neighbour is how a withdrawal came to read as coverage.
type Examination string

const (
	// ExaminedGoverned: live governed anchors exist. The graph looked and
	// something governs this subject now.
	ExaminedGoverned Examination = "examined_governed"
	// ExaminedWithdrawn: anchors exist and NONE are live. The graph looked, and
	// every rule it learned has since been retired. This is a DETERMINED
	// NEGATIVE, not missing information, and no fallback may overturn it.
	ExaminedWithdrawn Examination = "examined_withdrawn"
	// ExaminedUnknown: no governed anchors at all. Only here may a secondary
	// source (a source-file index) be consulted.
	ExaminedUnknown Examination = "unknown"
)

// Determined reports whether this examination is an ANSWER rather than an
// absence. Both governed and withdrawn are answers; only unknown is not.
func (e Examination) Determined() bool { return e != ExaminedUnknown }

// MayConsultIndex reports whether a secondary source may be asked. Exactly one
// state permits it, and that is the whole rule.
func (e Examination) MayConsultIndex() bool { return e == ExaminedUnknown }

// snapshot is a node's VALUES at the moment Build read them.
//
// Build receives pointer-backed nodes -- planChangeImpact passes
// *awarenesspb.KnowledgeNode -- and copying the []Node backing array only
// copies the pointers. A caller retaining the raw protobuf could then change an
// ID or a status afterwards, and LiveIDs would report the new ID while the node
// sat in the live partition computed from the old status: the canonical state
// contradicting itself, which is the one thing it exists to prevent.
//
// Values are copied once, at the boundary. Nothing a caller does later can move
// a node between partitions or rename it.
type snapshot struct{ id, iri, status, severity string }

func (s snapshot) GetId() string       { return s.id }
func (s snapshot) GetIri() string      { return s.iri }
func (s snapshot) GetStatus() string   { return s.status }
func (s snapshot) GetSeverity() string { return s.severity }

// Raw is everything the graph returned about a subject, before any filtering.
// The caller supplies it per class so no class can be forgotten silently: a
// missing key is visible here, whereas a forgotten append was not.
type Raw map[Class][]Node

// State is the canonical answer. Its fields are unexported so a consumer cannot
// reach past the accessors into a partially-filtered view -- which is exactly
// how the competing interpretations arose.
type State struct {
	raw     map[Class][]Node
	live    map[Class][]Node
	retired map[Class][]Node
}

// Build applies lifecycle filtering ONCE, for every class, with ONE predicate.
//
// isPrimary is supplied by the caller because resolving a promotion status may
// require I/O and this package stays pure. It is called exactly once per node.
func Build(raw Raw, isPrimary func(Node) bool) State {
	s := State{
		raw:     map[Class][]Node{},
		live:    map[Class][]Node{},
		retired: map[Class][]Node{},
	}
	for _, c := range AllClasses() {
		for _, n := range raw[c] {
			if n == nil {
				continue
			}
			// Snapshot BEFORE classifying, so the partition and the values it
			// was computed from can never disagree.
			v := snapshot{id: n.GetId(), iri: n.GetIri(), status: n.GetStatus(), severity: n.GetSeverity()}
			s.raw[c] = append(s.raw[c], v)
			if isPrimary != nil && isPrimary(n) {
				s.live[c] = append(s.live[c], v)
			} else {
				s.retired[c] = append(s.retired[c], v)
			}
		}
	}
	return s
}

// Live returns the lifecycle-filtered anchors of one class. Uncapped, always.
func (s State) Live(c Class) []Node { return append([]Node(nil), s.live[c]...) }

// Retired returns what was withdrawn. It is not discarded: retired knowledge
// remains guidance and travels in a briefing; it simply does not govern.
func (s State) Retired(c Class) []Node { return append([]Node(nil), s.retired[c]...) }

// AllLive is the COMPLETE live set across every governed class. This is what a
// class-complete decision reads.
func (s State) AllLive() []Node {
	var out []Node
	for _, c := range AllClasses() {
		out = append(out, s.live[c]...)
	}
	return out
}

// LiveIn is for a decision that is deliberately narrower than class-complete.
// It requires the caller to NAME the classes, so a narrowing is visible in the
// call rather than implied by which fields someone remembered to merge.
func (s State) LiveIn(classes ...Class) []Node {
	var out []Node
	for _, c := range classes {
		out = append(out, s.live[c]...)
	}
	return out
}

// CountRaw and CountLive answer the two halves of the examination question.
func (s State) CountRaw() int  { return countAll(s.raw) }
func (s State) CountLive() int { return countAll(s.live) }

func countAll(m map[Class][]Node) int {
	n := 0
	for _, c := range AllClasses() {
		n += len(m[c])
	}
	return n
}

// Examination is derived, never set. There is no way to assert a state that
// contradicts the anchors, which is what made the three-state model repairable
// in one place instead of three.
func (s State) Examination() Examination {
	switch {
	case s.CountLive() > 0:
		return ExaminedGoverned
	case s.CountRaw() > 0:
		return ExaminedWithdrawn
	default:
		return ExaminedUnknown
	}
}

// LiveIDs returns the identities of the live anchors of a class, sorted, for
// applicability comparisons.
func (s State) LiveIDs(c Class) []string {
	out := make([]string, 0, len(s.live[c]))
	for _, n := range s.live[c] {
		if id := strings.TrimSpace(n.GetId()); id != "" {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

// HasLiveAnchors is the "is this subject anchored at all" signal. It is
// class-complete: a subject anchored only by a live contract is anchored.
func (s State) HasLiveAnchors() bool { return s.CountLive() > 0 }
