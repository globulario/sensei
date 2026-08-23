// SPDX-License-Identifier: AGPL-3.0-only

package epistemic

import (
	"fmt"
	"strings"
	"time"
)

// Adoption is the event that turns a supported design into architecture.
//
// It exists because SUPPORTED is not ESTABLISHED, and nothing may quietly
// convert one into the other:
//
//	promotion by implementation  → architecture by sediment
//	promotion on SUPPORTED       → an automatic status transition, refused by §9
//	implicit promotion           → architecture with no evidential basis
//
// Every path that could have promoted a design without somebody saying so has
// already been eliminated, which is why this record is required rather than
// offered. Before adoption a design is something the project is TRYING; after
// it, something the project RELIES on, and Sensei must treat those differently.
//
// Adoption is not a synonym for human approval. When the question it resolves
// carries only reversible consequences, the agent that ran the experiments may
// adopt: the property that matters is not who typed the command but that the
// record carries what was adopted, why, from what evidence, under what
// remaining uncertainty, and under whose authority. When the question reached
// AUTHORITY -- an irreversible consequence -- Authority must be stated
// explicitly.
//
// A caveat this type cannot fix: nothing here VERIFIES that a named authority
// is a person, or that a person agreed. It records an attribution. Treating the
// field as proof would be exactly the too-strong claim this lane exists to
// avoid.
type Adoption struct {
	ID string `yaml:"id" json:"id"`
	// Question and Alternative name the design being adopted. An alternative of
	// a declared question IS the candidate design; a separate object would add
	// a name for something that already has one.
	Question    string `yaml:"resolves_question" json:"resolves_question"`
	Alternative string `yaml:"design" json:"design"`
	// Hypotheses are the supported beliefs this rests on. Required: an adoption
	// with no hypothesis has no evidential basis, which is the third eliminated
	// path above wearing a record.
	Hypotheses []string `yaml:"evidence_hypotheses" json:"evidence_hypotheses"`
	// RemainingUncertainty is what is still not known about the adopted design.
	//
	// Required, and "none identified" is an acceptable answer. Forcing the
	// sentence to be written is the point: an adoption that silently implies
	// certainty is how SUPPORTED quietly becomes PROVEN six months later, when
	// nobody remembers which one it was.
	RemainingUncertainty string `yaml:"remaining_uncertainty" json:"remaining_uncertainty"`
	// Authority is the basis on which this was adopted. Required only when the
	// question's disposition is AUTHORITY.
	Authority string `yaml:"authority,omitempty" json:"authority,omitempty"`
	// Scope are the paths that become established architecture. Empty is legal
	// -- a design decision need not have code -- and establishes nothing.
	Scope     []string `yaml:"scope,omitempty" json:"scope,omitempty"`
	AdoptedBy string   `yaml:"adopted_by" json:"adopted_by"`
	AdoptedAt string   `yaml:"adopted_at" json:"adopted_at"`
	Notes     string   `yaml:"notes,omitempty" json:"notes,omitempty"`
}

// ValidateAdoption checks the fields that need no cross-reference.
// Ledger.AddAdoption does the rest, because the interesting rules are all
// relational.
func ValidateAdoption(a Adoption) []string {
	var errs []string
	if !idPattern.MatchString(a.ID) {
		errs = append(errs, fmt.Sprintf("id %q must be 3-120 chars of [a-z0-9._-] starting alphanumeric", a.ID))
	}
	if strings.TrimSpace(a.Question) == "" {
		errs = append(errs, "resolves_question is required: an adoption that answers no declared question is architecture appearing from nowhere")
	}
	if strings.TrimSpace(a.Alternative) == "" {
		errs = append(errs, "design is required: name the alternative being adopted")
	}
	if strings.TrimSpace(a.RemainingUncertainty) == "" {
		errs = append(errs, "remaining_uncertainty is required (\"none identified\" is an acceptable answer): an adoption that silently implies certainty is how SUPPORTED becomes PROVEN later, when nobody remembers which it was")
	}
	if strings.TrimSpace(a.AdoptedBy) == "" {
		errs = append(errs, "adopted_by is required")
	}
	if err := requireRFC3339(a.AdoptedAt, "adopted_at"); err != "" {
		errs = append(errs, err)
	}
	return errs
}

// AdoptionFor returns the adoption of a given question and alternative, if any.
func (l *Ledger) AdoptionFor(question, alternative string) *Adoption {
	for i := range l.Adoptions {
		if l.Adoptions[i].Question == question && l.Adoptions[i].Alternative == alternative {
			return &l.Adoptions[i]
		}
	}
	return nil
}

// AddAdoption validates the record against the rest of the ledger and appends
// it.
//
// The relational rules, and why each exists:
//
//   - the alternative must be viable. Adopting one that established knowledge
//     already eliminated would let an experiment overrule a constraint.
//   - a CONSERVATION question may be adopted with no hypothesis at all. Its
//     answer came from the constraints, not from an experiment, and requiring
//     one would force a fake experiment to confirm what was already decided.
//   - every cited hypothesis must be about this exact design, and must be
//     SUPPORTED. Adopting on an AWAITING_HORIZON belief is promotion before the
//     clock matured; on an OVERDUE one it is promotion in place of observing;
//     on a REFUTED one it is promotion against the evidence.
//   - a question gets at most one adopted alternative. They are competing
//     answers to one question, and adopting two says the question was never
//     really a question.
//   - when the question's disposition is AUTHORITY, Authority must be named.
func (l *Ledger) AddAdoption(a Adoption, now time.Time) []string {
	if errs := ValidateAdoption(a); len(errs) > 0 {
		return errs
	}
	for _, existing := range l.Adoptions {
		if existing.ID == a.ID {
			return []string{fmt.Sprintf("adoption %q already exists", a.ID)}
		}
	}

	var q *DesignQuestion
	for i := range l.Questions {
		if l.Questions[i].ID == a.Question {
			q = &l.Questions[i]
			break
		}
	}
	if q == nil {
		return []string{fmt.Sprintf("no design question %q; a design becomes architecture by answering a declared question, not by appearing", a.Question)}
	}

	var alt *Alternative
	for i := range q.Alternatives {
		if q.Alternatives[i].ID == a.Alternative {
			alt = &q.Alternatives[i]
			break
		}
	}
	if alt == nil {
		return []string{fmt.Sprintf("design question %q declares no alternative %q", a.Question, a.Alternative)}
	}
	if !alt.Viable() {
		return []string{fmt.Sprintf("alternative %q was eliminated by %q; adopting it would let an experiment overrule established knowledge",
			a.Alternative, alt.EliminatedBy)}
	}

	if prior := l.adoptedAlternative(a.Question); prior != "" && prior != a.Alternative {
		return []string{fmt.Sprintf("design question %q already adopted alternative %q; these are competing answers to one question, and adopting two says it was never really a question",
			a.Question, prior)}
	}

	disposition, why := Dispose(*q)

	// A CONSERVATION question was settled by established knowledge: constraints
	// eliminated every alternative but one, and no experiment was ever needed.
	// Demanding a SUPPORTED hypothesis there would force a fake experiment to
	// confirm something the constraints already decided -- and refusing to
	// adopt it at all would leave its code permanently unadoptable, which is
	// sediment by a different route. So the evidence basis is the constraints
	// themselves, and the adopted alternative must be the one that survived
	// them.
	if len(a.Hypotheses) == 0 {
		if disposition != DispositionConservation {
			return []string{fmt.Sprintf("evidence_hypotheses is required: %q is %s, and an adoption with no supported belief behind it has no evidential basis",
				a.Question, disposition)}
		}
		if !alt.Viable() {
			return []string{fmt.Sprintf("alternative %q is not the one the constraints left standing", a.Alternative)}
		}
	}

	var errs []string
	for _, hid := range a.Hypotheses {
		h := l.hypothesis(hid)
		if h == nil {
			errs = append(errs, fmt.Sprintf("no hypothesis %q", hid))
			continue
		}
		if h.Question != a.Question || h.Alternative != a.Alternative {
			errs = append(errs, fmt.Sprintf("hypothesis %q is about %s/%s, not the design being adopted", hid, h.Question, h.Alternative))
			continue
		}
		if state := StateOf(*h, l.Observations, now); state != StateSupported {
			errs = append(errs, fmt.Sprintf("hypothesis %q is %s, not SUPPORTED: %s", hid, state, whyNotAdoptable(state)))
		}
	}
	if len(errs) > 0 {
		return errs
	}

	if disposition == DispositionAuthority && strings.TrimSpace(a.Authority) == "" {
		return []string{fmt.Sprintf("design question %q is %s (%s); adoption must name the authority it was made under",
			a.Question, disposition, why)}
	}

	l.Adoptions = append(l.Adoptions, a)
	return nil
}

func whyNotAdoptable(s State) string {
	switch s {
	case StateAwaitingHorizon:
		return "adopting before the clock matured is promotion on silence"
	case StateOverdue:
		return "adopting an unobserved belief is promotion in place of observing"
	case StateRefuted:
		return "adopting a refuted belief is promotion against the evidence"
	default:
		return "only a supported belief can carry an adoption"
	}
}

func (l *Ledger) adoptedAlternative(question string) string {
	for _, a := range l.Adoptions {
		if a.Question == question {
			return a.Alternative
		}
	}
	return ""
}

func (l *Ledger) hypothesis(id string) *Hypothesis {
	for i := range l.Hypotheses {
		if l.Hypotheses[i].ID == id {
			return &l.Hypotheses[i]
		}
	}
	return nil
}

// adoptedPaths is every path an adoption established, for the sediment check.
//
// When an adoption names no explicit scope it inherits the experimental scope
// of the hypotheses it rests on: those are precisely the paths that existed to
// test the belief now being adopted, so adopting the belief without adopting
// the code that embodies it would leave the code permanently unadoptable.
func (l *Ledger) adoptedPaths() map[string]string {
	out := map[string]string{}
	for _, a := range l.Adoptions {
		scope := a.Scope
		if len(scope) == 0 {
			for _, hid := range a.Hypotheses {
				if h := l.hypothesis(hid); h != nil {
					scope = append(scope, h.ExperimentalScope...)
				}
			}
		}
		for _, p := range scope {
			if p = normalizePath(p); p != "" {
				out[p] = a.ID
			}
		}
	}
	return out
}
