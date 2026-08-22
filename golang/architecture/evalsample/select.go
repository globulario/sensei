// SPDX-License-Identifier: AGPL-3.0-only

package evalsample

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/globulario/sensei/golang/architecture"
)

// candidate is one identity eligible for selection, before the seed orders it.
type candidate struct {
	subjectID    string
	provider     string
	multiplicity int
	evidenceIDs  []string
	blind        BlindItem
}

// sampleLane draws one lane of one world.
//
// Every lane returns at least one stratum. A lane whose population is empty
// returns a stratum saying so, because "no contradiction cases were found" and
// "nobody looked for contradiction cases" are different facts and only the
// manifest can distinguish them afterwards.
func sampleLane(w World, wb WorldBinding, lane string, opts Options) ([]Stratum, []Item, []BlindItem, error) {
	switch lane {
	case LanePrecision:
		return samplePrecision(w, wb, opts)
	case LaneRecallUnit:
		return sampleFlat(w, wb, opts.Seed, LaneRecallUnit, recallCandidates(w), opts.RecallUnitsPerWorld,
			"the world supplied no independent recall-unit inventory")
	case LaneContradiction:
		return sampleFlat(w, wb, opts.Seed, LaneContradiction, contradictionCandidates(w), opts.CasesPerWorld,
			contradictionEmptyReason(w))
	case LaneChallenge:
		return sampleFlat(w, wb, opts.Seed, LaneChallenge, challengeCandidates(w), opts.CasesPerWorld,
			"this world's lane produced no counterexample or candidate question")
	}
	return nil, nil, nil, fmt.Errorf("unknown metric lane %q", lane)
}

// samplePrecision draws independently per provider (section 6.1).
//
// Independently is the load-bearing word. The world-1 distribution is skewed
// roughly a thousand to one between the largest and the thinnest provider, so
// a uniform draw over raw observations would measure the largest provider and
// call it a measurement of Sensei.
func samplePrecision(w World, wb WorldBinding, opts Options) ([]Stratum, []Item, []BlindItem, error) {
	byProvider := map[string][]architecture.Fact{}
	for _, o := range w.Observations {
		p := strings.TrimSpace(o.Extractor)
		if p == "" {
			return nil, nil, nil, fmt.Errorf("%s: an observation carries no extractor, so it cannot be placed in a provider stratum", w.Name)
		}
		byProvider[p] = append(byProvider[p], o)
	}
	if len(byProvider) == 0 {
		return []Stratum{{
			World: w.Name, Lane: LanePrecision, Target: opts.PrecisionPerProvider,
			Status: StatusAbsent, Reason: "this world produced no observations",
		}}, nil, nil, nil
	}

	providers := make([]string, 0, len(byProvider))
	for p := range byProvider {
		providers = append(providers, p)
	}
	sort.Strings(providers)

	var strata []Stratum
	var items []Item
	var blinds []BlindItem
	for _, p := range providers {
		cands, emissions := observationCandidates(byProvider[p])
		st, drawn, bl := draw(w, wb, opts.Seed, LanePrecision, p, cands, emissions, opts.PrecisionPerProvider, "")
		strata = append(strata, st)
		items = append(items, drawn...)
		blinds = append(blinds, bl...)
	}
	return strata, items, blinds, nil
}

// sampleFlat draws a lane that has no provider strata.
// contradictionEmptyReason distinguishes "nothing disagreed" from "nobody said
// what disagreement would look like". Collapsing the two would let an
// undeclared ontology read as a clean bill of health.
func contradictionEmptyReason(w World) string {
	if len(w.FunctionalPredicates) == 0 {
		return "no functional-predicate declaration was supplied, so no pair of observations can be shown to disagree rather than to describe a multi-valued relation; this lane is undrawn, not clean"
	}
	return "no observation pair asserts different objects for one subject under a predicate declared functional"
}

func sampleFlat(w World, wb WorldBinding, seed, lane string, cands []candidate, target int, emptyReason string) ([]Stratum, []Item, []BlindItem, error) {
	st, drawn, bl := draw(w, wb, seed, lane, "", cands, len(cands), target, emptyReason)
	return []Stratum{st}, drawn, bl, nil
}

// draw applies section 6.2: stable selection key from the committed seed, sort
// ascending, take the first N.
//
// The key is a hash of (seed, world binding, provider, subject identity) and
// nothing else. It cannot see a claim's text, confidence, or provider ranking,
// so no property of an item's content can pull it into or out of the sample.
func draw(w World, wb WorldBinding, seed, lane, provider string, cands []candidate, emissions, target int, emptyReason string) (Stratum, []Item, []BlindItem) {
	st := Stratum{
		World: w.Name, Lane: lane, ProviderID: provider,
		Emissions: emissions, Population: len(cands), Target: target,
	}
	if len(cands) == 0 {
		st.Status = StatusAbsent
		st.Reason = emptyReason
		return st, nil, nil
	}

	keyed := make([]struct {
		key string
		c   candidate
	}, 0, len(cands))
	for _, c := range cands {
		keyed = append(keyed, struct {
			key string
			c   candidate
		}{selectionKey(seed, wb, provider, c.subjectID), c})
	}
	// Sort by key, then by subject identity. The tie-break matters: two
	// identities hashing to the same key would otherwise be ordered by
	// whatever the map yielded, and the sample would stop being reproducible
	// exactly where it is hardest to notice.
	sort.SliceStable(keyed, func(i, j int) bool {
		if keyed[i].key != keyed[j].key {
			return keyed[i].key < keyed[j].key
		}
		return keyed[i].c.subjectID < keyed[j].c.subjectID
	})

	take := target
	if take >= len(keyed) {
		take = len(keyed)
		st.Status = StatusSampledAll
	} else {
		st.Status = StatusSampled
	}

	var items []Item
	var blinds []BlindItem
	for _, k := range keyed[:take] {
		blind := k.c.blind
		blind.Lane = lane
		key := itemKey(wb, lane, provider, k.c.subjectID)
		blind.ItemKey = key
		payload := digestOf(blind)
		items = append(items, Item{
			ItemKey:                  key,
			World:                    w.Name,
			WorldBindingDigestSHA256: wb.DigestSHA256,
			Lane:                     lane,
			ProviderID:               provider,
			SubjectID:                k.c.subjectID,
			Multiplicity:             k.c.multiplicity,
			SelectionKey:             k.key,
			EvidenceIDs:              k.c.evidenceIDs,
			BlindPayloadDigestSHA256: payload,
		})
		blinds = append(blinds, blind)
	}
	st.Selected = len(items)
	return st, items, blinds
}

// selectionKey orders one stratum. The COMMITTED SEED is the first field and
// is not optional: without it the key is a pure function of identity, so every
// sample of the same world would draw the same items forever and section 6.2's
// "changing the seed creates a new sample version" would be a promise the
// artifact could not keep. A regression test drives exactly that — the seed
// once reached the manifest as a recorded field while never reaching the draw,
// which mints a fresh sample IDENTITY over an unchanged sample.
func selectionKey(seed string, wb WorldBinding, provider, subjectID string) string {
	return hashFields("sensei.eval_sample.selection.v1", seed, wb.DigestSHA256, provider, subjectID)
}

// itemKey is the identity a human label attaches to.
//
// It is scoped to the world binding and the lane, not to content alone. Two
// pinned worlds can produce a byte-identical claim that the evidence supports
// in one and refutes in the other; an unscoped key would let one adjudicated
// verdict migrate to a question it was never asked about — the worst failure
// available to an answer key, because it looks correct.
//
// The scheme tag is not decoration. evalmodel keys the model-acquisition lane
// with its own scope, and the two vocabularies must never be joinable by
// accident just because both are hex.
func itemKey(wb WorldBinding, lane, provider, subjectID string) string {
	return "es1:" + hashFields("sensei.eval_sample.item.v1", wb.DigestSHA256, lane, provider, subjectID)[:32]
}

func hashFields(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		// Length-prefixed rather than delimiter-joined: a separator that can
		// appear inside a field lets two different tuples hash identically,
		// and a collision here silently merges two questions.
		fmt.Fprintf(h, "%d:", len(p))
		h.Write([]byte(p))
	}
	return hex.EncodeToString(h.Sum(nil))
}

func digestOf(v any) string {
	payload, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func digestManifest(m Manifest) string {
	m.DigestSHA256 = ""
	return digestOf(m)
}

func bindingOf(w World) WorldBinding {
	wb := WorldBinding{
		World:            w.Name,
		RepositoryDomain: w.Binding.RepositoryDomain,
		Revision:         w.Binding.Revision,
		RevisionStatus:   string(w.Binding.RevisionStatus),
		TreeDigestSHA256: w.Binding.TreeDigestSHA256,
	}
	wb.DigestSHA256 = hashFields("sensei.eval_sample.world_binding.v1",
		wb.World, wb.RepositoryDomain, wb.Revision, wb.RevisionStatus, wb.TreeDigestSHA256)
	return wb
}
