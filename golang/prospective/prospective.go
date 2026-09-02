// SPDX-License-Identifier: AGPL-3.0-only

// Package prospective answers the query Sensei could not previously answer:
//
//	I may know nothing about this exact file yet.
//	What established knowledge COULD govern the change being proposed here?
//
// The central capability gap is not that Sensei lacks the law. It is that the
// law does not reach the change that needs it. Every defect repaired in #317,
// #318, #319 and #320 was found by a person reading code, while the graph held
// laws that described the very shapes being written.
//
// # matched != applicable
//
// This package exists inside a hard constraint, and the constraint is the
// design rather than a caveat on it. Resemblance may produce GUIDANCE or a
// QUESTION. It may never produce AUTHORITY.
//
// So a Candidate carries its Basis, and the two bases are not ordered on one
// scale of confidence: they answer different questions.
//
//	BasisEstablished  a governed relationship exists between the subject and
//	                  the knowledge -- shared authority domain, declared
//	                  component, an authored path scope. This MAY be considered
//	                  for authority by a caller that already has the right to.
//	BasisResemblance  the subject looks like something governed -- same
//	                  directory, similar name, matching task words. This is
//	                  guidance, and no accumulation of it becomes authority.
//
// A caller cannot reach past this: AuthorityEligible is a method on the basis,
// there is no numeric score to threshold, and Candidates carries no ranking
// that would let "enough resemblance" stand in for a relationship.
//
// # Recall is not bought by surfacing everything
//
// A retrieval that returns the whole graph has perfect recall and no value; it
// relocates the reading problem rather than solving it. Both numbers are
// measured together in prospective_eval_test.go, and neither is reported alone.
package prospective

import (
	"path"
	"sort"
	"strings"
)

// Basis is a CLOSED set. It is read by membership, and it is not a confidence
// scale: the two members answer different questions, so no amount of one
// becomes the other.
type Basis string

const (
	BasisEstablished Basis = "established_relationship"
	BasisResemblance Basis = "resemblance"
)

// AuthorityEligible reports whether knowledge reached by this basis may be
// considered for an authority decision by a caller entitled to make one.
//
// Exactly one basis qualifies. This is the constitutional line of the package,
// and it is a method rather than a field so a caller cannot construct a
// Candidate that lies about it.
func (b Basis) AuthorityEligible() bool { return b == BasisEstablished }

// Signal names WHY a candidate surfaced, so a reader can judge the reach rather
// than trusting a number.
type Signal string

const (
	SignalAuthorityDomain Signal = "shared_authority_domain"
	SignalSameDirectory   Signal = "same_directory"
	SignalDeclaredScope   Signal = "authored_path_scope"
)

// Candidate is one piece of knowledge that COULD govern the subject.
//
// THE BASIS IS UNEXPORTED AND THAT IS THE WHOLE DESIGN. With an exported,
// string-backed field a caller could write Candidate{Basis: BasisEstablished}
// -- or deserialize one -- and AuthorityEligibleOnly would accept it with no
// governed relationship behind it. The constitutional line of this package was
// a struct literal away from being forged, which is not a line.
//
// Only Retrieve sets it, and it sets it from a relationship the graph holds.
type Candidate struct {
	ID     string
	Class  string
	Signal Signal
	// Why states the relationship in words a reviewer can check.
	Why string

	basis Basis
}

// Basis reports how this candidate was reached. Read-only by construction.
func (c Candidate) Basis() Basis { return c.basis }

// AuthorityEligible delegates to the basis. A candidate cannot override it,
// and cannot be constructed claiming one it did not earn.
func (c Candidate) AuthorityEligible() bool { return c.basis.AuthorityEligible() }

// Subject is what the caller proposes to change.
type Subject struct {
	Files  []string
	Domain string
}

// Anchor is a governed node with the relationships already in the graph.
type Anchor struct {
	ID    string
	Class string
	// Files are the paths the anchor is authored to protect.
	Files []string
	// Domains are the authority domains it belongs to.
	Domains []string
}

// Retrieve proposes knowledge that could govern the subject.
//
// It NEVER returns knowledge already directly anchored to the subject's files:
// that is not prospective retrieval, it is the direct lookup the caller already
// did, and counting it would inflate recall with answers the caller had.
func Retrieve(s Subject, anchors []Anchor, subjectDomains []string) []Candidate {
	direct := map[string]bool{}
	for _, a := range anchors {
		for _, f := range a.Files {
			for _, sf := range s.Files {
				if f == sf {
					direct[a.ID] = true
				}
			}
		}
	}
	dom := map[string]bool{}
	for _, d := range subjectDomains {
		if d = strings.TrimSpace(d); d != "" {
			dom[d] = true
		}
	}
	dirs := map[string]bool{}
	for _, f := range s.Files {
		dirs[path.Dir(f)] = true
	}

	seen := map[string]bool{}
	var out []Candidate
	add := func(c Candidate) {
		if seen[c.ID] || direct[c.ID] {
			return
		}
		seen[c.ID] = true
		out = append(out, c)
	}

	// The direct-anchor guard lives ONLY in add(), which is the single point
	// every candidate passes through. A second guard in this loop was
	// redundant: mutation showed either one alone sufficed, so removing one
	// survived and the report read as a hole when it was duplication. One
	// invariant, one choke point.
	for _, a := range anchors {
		// ESTABLISHED: the subject and the anchor share an authority domain.
		// That is a relationship the graph already holds, not a similarity.
		matchedDomain := ""
		for _, d := range a.Domains {
			if dom[strings.TrimSpace(d)] {
				matchedDomain = d
				break
			}
		}
		if matchedDomain != "" {
			add(Candidate{ID: a.ID, Class: a.Class, basis: BasisEstablished, Signal: SignalAuthorityDomain,
				Why: "subject and knowledge share authority domain " + matchedDomain})
			continue
		}
		// RESEMBLANCE: the anchor governs a sibling file in the same directory.
		// Useful, and not a relationship to this file.
		for _, f := range a.Files {
			if dirs[path.Dir(f)] {
				add(Candidate{ID: a.ID, Class: a.Class, basis: BasisResemblance, Signal: SignalSameDirectory,
					Why: "knowledge governs " + f + ", a sibling in the same directory"})
				break
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].basis != out[j].basis {
			return out[i].basis == BasisEstablished
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// AuthorityEligibleOnly is the filter a caller must apply before letting any of
// this near an authority decision. Offered here so the rule is applied once,
// in the package that states it, rather than reimplemented by each consumer --
// which is how the same rule came to have four owners elsewhere.
func AuthorityEligibleOnly(in []Candidate) []Candidate {
	var out []Candidate
	for _, c := range in {
		if c.AuthorityEligible() {
			out = append(out, c)
		}
	}
	return out
}
