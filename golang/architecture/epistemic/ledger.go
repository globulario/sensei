// SPDX-License-Identifier: AGPL-3.0-only

package epistemic

import (
	"fmt"
	"sort"

	"gopkg.in/yaml.v3"
)

// Ledger is the on-disk shape of the epistemic lane: the declared questions,
// the hypotheses about them, and the observations that move those beliefs.
//
// It is deliberately NOT part of the awareness corpus. Nothing here is
// canonical knowledge, none of it is compiled into the seed by yaml2nt, and no
// class of it reaches the graph in this slice. A DesignQuestion is not law and
// is never promoted into one; putting it in the same files as invariants would
// re-create the masquerade this lane exists to end, and putting it in the
// candidates/ review queue would say it is awaiting promotion, which it is not.
type Ledger struct {
	Version      int              `yaml:"version" json:"version"`
	Questions    []DesignQuestion `yaml:"design_questions" json:"design_questions"`
	Hypotheses   []Hypothesis     `yaml:"hypotheses" json:"hypotheses"`
	Observations []Observation    `yaml:"observations" json:"observations"`
}

// LedgerVersion is the current on-disk version.
const LedgerVersion = 1

// Decode parses a ledger. An empty input is an empty ledger rather than an
// error: the lane starting out with nothing in it is the normal first state,
// and treating that as a failure would push callers into creating the file
// before they have anything to say.
func Decode(b []byte) (Ledger, error) {
	var l Ledger
	if len(b) == 0 {
		l.Version = LedgerVersion
		return l, nil
	}
	if err := yaml.Unmarshal(b, &l); err != nil {
		return Ledger{}, fmt.Errorf("parse epistemic ledger: %w", err)
	}
	if l.Version == 0 {
		l.Version = LedgerVersion
	}
	if l.Version != LedgerVersion {
		// Refused rather than read positionally. A ledger written by a version
		// this binary does not know is not a ledger it may interpret, and
		// guessing would silently mis-assign fields that decide dispositions.
		return Ledger{}, fmt.Errorf("epistemic ledger version %d is not supported (this binary knows version %d)", l.Version, LedgerVersion)
	}
	return l, nil
}

// Encode renders a ledger deterministically: entries sorted by id so a diff
// shows what changed rather than where it landed.
func Encode(l Ledger) ([]byte, error) {
	l.Version = LedgerVersion
	sort.SliceStable(l.Questions, func(i, j int) bool { return l.Questions[i].ID < l.Questions[j].ID })
	sort.SliceStable(l.Hypotheses, func(i, j int) bool { return l.Hypotheses[i].ID < l.Hypotheses[j].ID })
	sort.SliceStable(l.Observations, func(i, j int) bool { return l.Observations[i].ID < l.Observations[j].ID })
	b, err := yaml.Marshal(l)
	if err != nil {
		return nil, fmt.Errorf("render epistemic ledger: %w", err)
	}
	header := "# Epistemic lane — uncertain design belief (globulario/sensei#288).\n" +
		"#\n" +
		"# NOT canonical knowledge. Nothing here is an invariant, a contract or a\n" +
		"# decision, nothing here is compiled into the awareness seed, and no routing\n" +
		"# surface reads it. A DesignQuestion records what must be resolved; a\n" +
		"# Hypothesis records what is believed and what would refute it; an\n" +
		"# Observation records what actually happened.\n" +
		"#\n" +
		"# Dispositions are COMPUTED (`sensei epistemic status`), never authored.\n" +
		"# Write with `sensei epistemic declare|hypothesize|observe`, which validate.\n"
	return append([]byte(header), b...), nil
}

// AddQuestion validates and appends, refusing a duplicate id.
func (l *Ledger) AddQuestion(q DesignQuestion) []string {
	if errs := ValidateQuestion(q); len(errs) > 0 {
		return errs
	}
	for _, existing := range l.Questions {
		if existing.ID == q.ID {
			return []string{fmt.Sprintf("design question %q already exists; a question is declared once and answered by observation, not redeclared", q.ID)}
		}
	}
	l.Questions = append(l.Questions, q)
	return nil
}

// AddHypothesis validates, refuses a duplicate id, and requires that the
// question and the alternative it names both exist.
//
// The referential check is not bookkeeping: a hypothesis about an alternative
// nobody declared cannot settle anything, and one about an alternative that was
// already eliminated is proposing to spend an experiment on a decision
// established knowledge already made.
func (l *Ledger) AddHypothesis(h Hypothesis) []string {
	if errs := ValidateHypothesis(h); len(errs) > 0 {
		return errs
	}
	for _, existing := range l.Hypotheses {
		if existing.ID == h.ID {
			return []string{fmt.Sprintf("hypothesis %q already exists", h.ID)}
		}
	}
	var q *DesignQuestion
	for i := range l.Questions {
		if l.Questions[i].ID == h.Question {
			q = &l.Questions[i]
			break
		}
	}
	if q == nil {
		return []string{fmt.Sprintf("no design question %q; declare the question before predicting about it", h.Question)}
	}
	for _, a := range q.Alternatives {
		if a.ID != h.Alternative {
			continue
		}
		if !a.Viable() {
			return []string{fmt.Sprintf("alternative %q of %q was eliminated by %q; an experiment on it would spend evidence on a decision established knowledge already made",
				h.Alternative, h.Question, a.EliminatedBy)}
		}
		l.Hypotheses = append(l.Hypotheses, h)
		return nil
	}
	return []string{fmt.Sprintf("design question %q declares no alternative %q", h.Question, h.Alternative)}
}

// AddObservation validates, refuses a duplicate id, and requires the hypothesis
// it moves to exist.
func (l *Ledger) AddObservation(o Observation) []string {
	if errs := ValidateObservation(o); len(errs) > 0 {
		return errs
	}
	for _, existing := range l.Observations {
		if existing.ID == o.ID {
			return []string{fmt.Sprintf("observation %q already exists; observations are appended, never rewritten", o.ID)}
		}
	}
	for _, h := range l.Hypotheses {
		if h.ID == o.Hypothesis {
			l.Observations = append(l.Observations, o)
			return nil
		}
	}
	return []string{fmt.Sprintf("no hypothesis %q to observe against", o.Hypothesis)}
}
