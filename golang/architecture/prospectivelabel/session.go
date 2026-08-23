// SPDX-License-Identifier: AGPL-3.0-only

// Package prospectivelabel holds the adjudication policy for establishing the
// human applicability reference of #259.
//
// It is bookkeeping, not judgement. Nothing here decides, suggests, ranks or
// orders items by anything except a deterministic key, and the package cannot
// reach a graph, a retrieval surface or the full corpus even if asked. Its only
// job is to make 41,568 (change, item) pairs survivable for a human without
// letting fatigue or a filter quietly become the measurement.
//
// Two rules carry the whole design.
//
// Nothing has a default label. A pair begins UNSET, because a default of
// not_applicable means that merely opening a package has manufactured 866
// negative answers, and absence of action would masquerade as judgement.
//
// Presented is never the same fact as individually reviewed. A bulk negative
// sweep is a legitimate, explicit act of judgement over a remainder — but it is
// recorded as one, so a later reader can weigh the denominator instead of being
// told 866 items were reviewed one by one.
package prospectivelabel

import (
	"fmt"
	"sort"
	"strings"
)

// The protocol's applicability labels (section 6.1).
const (
	LabelApplicable       = "applicable"
	LabelNotApplicable    = "not_applicable"
	LabelAmbiguous        = "ambiguous"
	LabelOutsideScope     = "outside_scope"
	LabelCannotAdjudicate = "cannot_adjudicate"
)

// Labels is the closed vocabulary, in report order.
var Labels = []string{LabelApplicable, LabelNotApplicable, LabelAmbiguous, LabelOutsideScope, LabelCannotAdjudicate}

// Assignment modes record HOW a label came to exist, so the provenance of the
// answer key is visible in the answer key.
const (
	ModeIndividual = "individual"
	ModeBulkSweep  = "bulk_sweep"
)

func knownLabel(l string) bool {
	for _, k := range Labels {
		if k == l {
			return true
		}
	}
	return false
}

// Label is one human decision about one (change, eligible item) pair.
type Label struct {
	ItemKey        string `json:"item_key"`
	CorpusItemID   string `json:"corpus_item_id"`
	Label          string `json:"label"`
	AssignmentMode string `json:"assignment_mode"`
}

// Coverage separates facts that are routinely conflated.
//
// Presented and IndividuallyAssigned are different claims about what a human
// did, and a report that prints one as the other is overstating its own
// denominator.
type Coverage struct {
	ItemKey                string `json:"item_key"`
	EligibleItems          int    `json:"eligible_items"`
	Presented              int    `json:"presented"`
	IndividuallyAssigned   int    `json:"individually_assigned"`
	BulkSweptNotApplicable int    `json:"bulk_swept_not_applicable"`
	Unresolved             int    `json:"unresolved"`
	Unlabelled             int    `json:"unlabelled"`

	// AdjudicationCoverageComplete means every pair carries a label.
	AdjudicationCoverageComplete bool `json:"adjudication_coverage_complete"`
	// IndividualReviewComplete means every label was decided one at a time. It
	// is reported separately, and is false whenever a sweep was used.
	IndividualReviewComplete bool `json:"individual_review_complete"`
}

// Session is one adjudicator's work over one frozen sample.
type Session struct {
	ManifestDigestSHA256    string
	BlindCorpusDigestSHA256 string
	Adjudicator             string

	// ItemKeys are the sampled changes; CorpusIDs the eligible items. Both are
	// held in the deterministic order the reference set fixed.
	ItemKeys  []string
	CorpusIDs []string

	assigned  map[pair]Label
	presented map[string]map[string]bool
}

type pair struct {
	item   string
	corpus string
}

// New starts a session over one frozen sample.
func New(manifestDigest, blindCorpusDigest, adjudicator string, itemKeys, corpusIDs []string) (*Session, error) {
	if strings.TrimSpace(adjudicator) == "" {
		return nil, fmt.Errorf("an adjudicator name is required: an answer key nobody is named on cannot be checked against a second one")
	}
	if manifestDigest == "" || blindCorpusDigest == "" {
		return nil, fmt.Errorf("the session must bind the sample manifest and the blind corpus it answers")
	}
	if len(itemKeys) == 0 || len(corpusIDs) == 0 {
		return nil, fmt.Errorf("the session needs both changes and eligible items")
	}
	return &Session{
		ManifestDigestSHA256:    manifestDigest,
		BlindCorpusDigestSHA256: blindCorpusDigest,
		Adjudicator:             adjudicator,
		ItemKeys:                append([]string(nil), itemKeys...),
		CorpusIDs:               append([]string(nil), corpusIDs...),
		assigned:                map[pair]Label{},
		presented:               map[string]map[string]bool{},
	}, nil
}

// Present records that items entered the presentation stream for one change.
//
// It is what the sweep gate checks. It cannot prove a human read anything — no
// interface can — but it does prove the software never silently withheld part
// of the corpus, which is the failure that would shrink a denominator without
// anybody noticing.
func (s *Session) Present(itemKey string, corpusIDs ...string) error {
	if !s.knownItem(itemKey) {
		return fmt.Errorf("change %s is not in this sample", itemKey)
	}
	set := s.presented[itemKey]
	if set == nil {
		set = map[string]bool{}
		s.presented[itemKey] = set
	}
	for _, id := range corpusIDs {
		if !s.knownCorpus(id) {
			return fmt.Errorf("eligible item %s is not in the blind corpus", id)
		}
		set[id] = true
	}
	return nil
}

// Assign records one individual decision.
func (s *Session) Assign(itemKey, corpusID, label string) error {
	if !s.knownItem(itemKey) {
		return fmt.Errorf("change %s is not in this sample", itemKey)
	}
	if !s.knownCorpus(corpusID) {
		return fmt.Errorf("eligible item %s is not in the blind corpus", corpusID)
	}
	if !knownLabel(label) {
		return fmt.Errorf("%q is not one of the five labels; the vocabulary is closed", label)
	}
	s.assigned[pair{itemKey, corpusID}] = Label{
		ItemKey: itemKey, CorpusItemID: corpusID, Label: label, AssignmentMode: ModeIndividual,
	}
	return nil
}

// Unset removes a decision, returning the pair to UNSET.
func (s *Session) Unset(itemKey, corpusID string) {
	delete(s.assigned, pair{itemKey, corpusID})
}

// Sweep marks every still-UNSET item for one change as not_applicable.
//
// It is gated on the complete corpus having been presented for that change.
// Without the gate an adjudicator could filter to twelve items, sweep, and
// produce 854 negative labels for items the software never showed them —
// which is indistinguishable, in the output, from having considered them.
func (s *Session) Sweep(itemKey string) (int, error) {
	if !s.knownItem(itemKey) {
		return 0, fmt.Errorf("change %s is not in this sample", itemKey)
	}
	if missing := s.NotPresented(itemKey); len(missing) > 0 {
		return 0, fmt.Errorf("%d of %d eligible items have not been presented for this change, so a negative sweep would cover items the software never showed; present the whole corpus first",
			len(missing), len(s.CorpusIDs))
	}
	n := 0
	for _, id := range s.CorpusIDs {
		k := pair{itemKey, id}
		if _, ok := s.assigned[k]; ok {
			continue
		}
		s.assigned[k] = Label{
			ItemKey: itemKey, CorpusItemID: id, Label: LabelNotApplicable, AssignmentMode: ModeBulkSweep,
		}
		n++
	}
	return n, nil
}

// NotPresented lists eligible items that have not entered the presentation
// stream for a change, in deterministic order.
func (s *Session) NotPresented(itemKey string) []string {
	set := s.presented[itemKey]
	var out []string
	for _, id := range s.CorpusIDs {
		if !set[id] {
			out = append(out, id)
		}
	}
	return out
}

// Coverage reports one change's numbers.
func (s *Session) Coverage(itemKey string) Coverage {
	c := Coverage{ItemKey: itemKey, EligibleItems: len(s.CorpusIDs), Presented: len(s.presented[itemKey])}
	for _, id := range s.CorpusIDs {
		l, ok := s.assigned[pair{itemKey, id}]
		if !ok {
			c.Unlabelled++
			continue
		}
		switch l.AssignmentMode {
		case ModeBulkSweep:
			c.BulkSweptNotApplicable++
		default:
			c.IndividuallyAssigned++
		}
		if l.Label == LabelAmbiguous || l.Label == LabelCannotAdjudicate || l.Label == LabelOutsideScope {
			c.Unresolved++
		}
	}
	c.AdjudicationCoverageComplete = c.Unlabelled == 0 && c.Presented == c.EligibleItems
	c.IndividualReviewComplete = c.Unlabelled == 0 && c.BulkSweptNotApplicable == 0
	return c
}

// Complete reports whether a change may be finalized: every pair labelled and
// every eligible item presented.
func (s *Session) Complete(itemKey string) bool {
	return s.Coverage(itemKey).AdjudicationCoverageComplete
}

// Labels returns every decision in deterministic order.
func (s *Session) Labels() []Label {
	out := make([]Label, 0, len(s.assigned))
	for _, l := range s.assigned {
		out = append(out, l)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].ItemKey != out[j].ItemKey {
			return out[i].ItemKey < out[j].ItemKey
		}
		return out[i].CorpusItemID < out[j].CorpusItemID
	})
	return out
}

// PresentedIDs returns what has been presented for a change, deterministically.
func (s *Session) PresentedIDs(itemKey string) []string {
	set := s.presented[itemKey]
	var out []string
	for _, id := range s.CorpusIDs {
		if set[id] {
			out = append(out, id)
		}
	}
	return out
}

// Restore rebuilds a session's state from persisted rows, so a human can stop
// and come back without the work being re-done or silently lost.
func (s *Session) Restore(labels []Label, presented map[string][]string) error {
	for _, l := range labels {
		if !s.knownItem(l.ItemKey) || !s.knownCorpus(l.CorpusItemID) || !knownLabel(l.Label) {
			return fmt.Errorf("restored label references something outside this sample: %+v", l)
		}
		if l.AssignmentMode != ModeIndividual && l.AssignmentMode != ModeBulkSweep {
			return fmt.Errorf("restored label carries the unknown assignment mode %q", l.AssignmentMode)
		}
		s.assigned[pair{l.ItemKey, l.CorpusItemID}] = l
	}
	for item, ids := range presented {
		if err := s.Present(item, ids...); err != nil {
			return err
		}
	}
	return nil
}

func (s *Session) knownItem(k string) bool    { return contains(s.ItemKeys, k) }
func (s *Session) knownCorpus(id string) bool { return contains(s.CorpusIDs, id) }

func contains(hay []string, needle string) bool {
	for _, h := range hay {
		if h == needle {
			return true
		}
	}
	return false
}
