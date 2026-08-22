// SPDX-License-Identifier: AGPL-3.0-only

package evalsample

import (
	"fmt"
	"sort"
	"strings"

	"github.com/globulario/sensei/golang/architecture"
)

// observationCandidates turns one provider's observations into selectable
// identities, collapsing repeated emissions of the same claim.
//
// The extractors assign no ID, so identity is derived from the claim and the
// anchor it is made about: same subject, predicate, object, file and line range
// is the same claim. Collapsing is not deduplication for tidiness — an
// adjudicator asked the same question twice returns the same answer, which
// would double that claim's weight in a precision denominator without adding
// any evidence.
//
// The count survives as Multiplicity, so nothing is hidden by the collapse.
func observationCandidates(facts []architecture.Fact) ([]candidate, int) {
	byID := map[string]*candidate{}
	for _, f := range facts {
		id := observationIdentity(f)
		c, ok := byID[id]
		if !ok {
			c = &candidate{
				subjectID:   id,
				provider:    f.Extractor,
				evidenceIDs: evidenceIDs(f),
				blind: BlindItem{
					Claim:       claimText(f),
					EvidenceIDs: evidenceIDs(f),
				},
			}
			byID[id] = c
		}
		c.multiplicity++
	}
	out := make([]candidate, 0, len(byID))
	for _, c := range byID {
		out = append(out, *c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].subjectID < out[j].subjectID })
	return out, len(facts)
}

// observationIdentity is the content identity of one claim about one anchor.
func observationIdentity(f architecture.Fact) string {
	return hashFields("sensei.eval_sample.observation.v1",
		f.Extractor, f.Kind, f.Subject, f.Predicate, f.Object,
		f.Evidence.SourceFile, fmt.Sprintf("%d-%d", f.Evidence.LineStart, f.Evidence.LineEnd))[:32]
}

// claimText is what the adjudicator reads. It is the extractor's own triple,
// rendered, and nothing is added to it: a rephrasing would be this package
// interpreting a claim it is only supposed to be selecting.
func claimText(f architecture.Fact) string {
	return strings.TrimSpace(fmt.Sprintf("%s %s %s", f.Subject, f.Predicate, f.Object))
}

// evidenceIDs are the anchors an adjudicator must open to judge support.
func evidenceIDs(f architecture.Fact) []string {
	var out []string
	if file := strings.TrimSpace(f.Evidence.SourceFile); file != "" {
		if f.Evidence.LineStart > 0 {
			out = append(out, fmt.Sprintf("%s:%d-%d", file, f.Evidence.LineStart, f.Evidence.LineEnd))
		} else {
			out = append(out, file)
		}
	}
	if test := strings.TrimSpace(f.Evidence.TestName); test != "" {
		out = append(out, "test:"+test)
	}
	if commit := strings.TrimSpace(f.Evidence.Commit); commit != "" {
		out = append(out, "commit:"+commit)
	}
	return out
}

// recallCandidates are the world's independently defined units (section 7).
//
// This function does no discovery on purpose. The inventory is supplied by the
// caller from outside Sensei's output, because a recall denominator built from
// what Sensei already produced can only measure units Sensei had something to
// say about — and an omission in a unit that never enters the denominator is
// unmeasurable by construction.
func recallCandidates(w World) []candidate {
	seen := map[string]bool{}
	var out []candidate
	for _, unit := range w.RecallInventory {
		unit = strings.TrimSpace(unit)
		if unit == "" || seen[unit] {
			continue
		}
		seen[unit] = true
		out = append(out, candidate{
			subjectID:    unit,
			multiplicity: 1,
			blind:        BlindItem{Unit: unit},
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].subjectID < out[j].subjectID })
	return out
}

// contradictionCandidates finds where the world's output disagrees with
// itself: a FUNCTIONAL predicate — one the caller has declared may hold only a
// single object — carrying more than one distinct object for one subject, each
// side anchored (section 8).
//
// The functional restriction is what makes the population real. Without it the
// rule fires on every multi-valued relation in the repository, which is not
// disagreement at all: world 1 produced 23,641 such pairs, essentially all of
// them components legitimately depending on many things.
//
// This SELECTS a case. It does not decide the case. Whether the two sides
// genuinely disagree, whether one supersedes the other, or whether the evidence
// settles nothing is exactly what the human adjudicator is for, and section 8
// gives them three states rather than a winner. A mechanical rule that picked a
// side here would be the system grading its own contradictions.
func contradictionCandidates(w World) []candidate {
	functional := map[string]bool{}
	for _, p := range w.FunctionalPredicates {
		if p = strings.TrimSpace(p); p != "" {
			functional[p] = true
		}
	}
	if len(functional) == 0 {
		return nil
	}
	type side struct {
		object   string
		evidence []string
	}
	bySubject := map[string][]side{}
	order := []string{}
	for _, f := range w.Observations {
		subject := strings.TrimSpace(f.Subject)
		predicate := strings.TrimSpace(f.Predicate)
		object := strings.TrimSpace(f.Object)
		if subject == "" || predicate == "" || object == "" {
			continue
		}
		if !functional[predicate] {
			continue
		}
		// An anchorless side cannot be one of the "two pinned evidence items"
		// section 8 requires, so it cannot open a case.
		ev := evidenceIDs(f)
		if len(ev) == 0 {
			continue
		}
		key := subject + "\x00" + predicate
		if _, ok := bySubject[key]; !ok {
			order = append(order, key)
		}
		bySubject[key] = append(bySubject[key], side{object: object, evidence: ev})
	}

	var out []candidate
	for _, key := range order {
		sides := bySubject[key]
		distinct := map[string][]string{}
		for _, s := range sides {
			distinct[s.object] = append(distinct[s.object], s.evidence...)
		}
		if len(distinct) < 2 {
			continue
		}
		objects := make([]string, 0, len(distinct))
		for o := range distinct {
			objects = append(objects, o)
		}
		sort.Strings(objects)

		parts := strings.SplitN(key, "\x00", 2)
		var alternatives, evidence []string
		for _, o := range objects {
			alternatives = append(alternatives, fmt.Sprintf("%s %s %s", parts[0], parts[1], o))
			evidence = append(evidence, dedupe(distinct[o])...)
		}
		evidence = dedupe(evidence)
		out = append(out, candidate{
			subjectID:    hashFields("sensei.eval_sample.contradiction.v1", parts[0], parts[1])[:32],
			multiplicity: len(sides),
			evidenceIDs:  evidence,
			blind: BlindItem{
				Claim:        fmt.Sprintf("%s %s — %d distinct objects asserted", parts[0], parts[1], len(objects)),
				Alternatives: alternatives,
				EvidenceIDs:  evidence,
			},
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].subjectID < out[j].subjectID })
	return out
}

// challengeCandidates are the counterexamples and candidate questions the
// world's lane produced (section 10).
func challengeCandidates(w World) []candidate {
	var out []candidate
	for _, ce := range w.Counterexamples {
		id := strings.TrimSpace(ce.ID)
		if id == "" {
			id = hashFields("sensei.eval_sample.counterexample.v1", ce.ClaimID, ce.Description)[:32]
		}
		out = append(out, candidate{
			subjectID:    "counterexample:" + id,
			multiplicity: 1,
			evidenceIDs:  ce.EvidenceRefIDs,
			blind: BlindItem{
				Claim:       strings.TrimSpace(ce.Description),
				EvidenceIDs: ce.EvidenceRefIDs,
			},
		})
	}
	for _, q := range w.CandidateQuestions {
		id := strings.TrimSpace(q.ID)
		if id == "" {
			id = hashFields("sensei.eval_sample.question.v1", q.QuestionText)[:32]
		}
		out = append(out, candidate{
			subjectID:    "question:" + id,
			multiplicity: 1,
			blind:        BlindItem{Claim: strings.TrimSpace(q.QuestionText)},
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].subjectID < out[j].subjectID })
	return out
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, v := range in {
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}
