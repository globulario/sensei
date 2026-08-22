// SPDX-License-Identifier: AGPL-3.0-only

package prospective

import (
	"fmt"
	"sort"
	"strings"
)

// RetrievalSurface is the production authoring-time path the measurement will
// use (design section 3.1), frozen here before any score exists.
//
// Slice 1 does not run it. Recording the decision now is what stops it from
// being chosen later, after somebody has seen which surface scores better —
// which would make the choice of instrument a function of the result.
type RetrievalSurface struct {
	// ID is the closed identity of the chosen surface.
	ID string `json:"id"`
	// Invocation is the exact production command shape, recorded verbatim.
	Invocation string `json:"invocation"`
	// Rationale states why this surface answers the protocol's question
	// rather than BY_FILE.
	Rationale string `json:"rationale"`
	// NoChannelStatus is the status Slice 2 must record when production
	// exposes no prospective channel for a path that does not yet exist.
	//
	// It is named here, in the frozen manifest, rather than invented by the
	// runner later. A runner that had to coin this status while looking at a
	// disappointing stratum A could just as easily coin BY_FILE as a fallback
	// instead, and the substitution would never appear in the numbers.
	NoChannelStatus string `json:"no_channel_status"`
}

// StatusNoProspectiveChannel is what Slice 2 records for a change whose seam
// production retrieval has no prospective query for. It is a reported outcome,
// never a reason to drop the change from a denominator.
const StatusNoProspectiveChannel = "no_prospective_channel"

// Stratum is one population and what was drawn from it.
type Stratum struct {
	Stratum string `json:"stratum"`
	// InventoryDigestSHA256 is this stratum's frozen population identity
	// (protocol section 8.1). A recall figure computed over an inventory whose
	// digest is not published is not reproducible.
	InventoryDigestSHA256 string `json:"inventory_digest_sha256"`
	Population            int    `json:"population"`
	Target                int    `json:"target"`
	Selected              int    `json:"selected"`
	Status                string `json:"status"`
	Reason                string `json:"reason,omitempty"`
}

// Item is one selected change, as the sample names it.
type Item struct {
	ItemKey                  string `json:"item_key"`
	ChangeID                 string `json:"change_id"`
	Stratum                  string `json:"stratum"`
	WorldBindingDigestSHA256 string `json:"world_binding_digest_sha256"`
	// SelectionKey is published so the draw can be recomputed and disputed,
	// which is the only thing that makes "deterministic" checkable rather than
	// asserted.
	SelectionKey string `json:"selection_key"`
	// BlindPayloadDigestSHA256 binds the exact package the adjudicator saw.
	BlindPayloadDigestSHA256 string `json:"blind_payload_digest_sha256"`
}

// Options are the frozen inputs of a sample.
type Options struct {
	ProtocolID           string
	ProtocolDigestSHA256 string
	// Seed is committed in the manifest before labels exist (section 8.2).
	Seed string
	// GeneratedAt is caller-supplied: a self-stamped artifact is not
	// reproducible, and this one is content-addressed.
	GeneratedAt      string
	TargetPerStratum int
	RetrievalSurface RetrievalSurface
	// OverlapFraction is the second-adjudicator subset of section 10.
	OverlapFraction float64
}

// Manifest is the frozen selection. It contains no labels, no scores, and no
// field for one to be written into later.
type Manifest struct {
	SchemaVersion        string `json:"schema_version"`
	ProtocolID           string `json:"protocol_id"`
	ProtocolDigestSHA256 string `json:"protocol_digest_sha256"`
	SelectionSeed        string `json:"selection_seed"`
	GeneratedAt          string `json:"generated_at"`

	World WorldBinding `json:"world"`

	ClassificationRuleID          string `json:"classification_rule_id"`
	ClassificationRuleDescription string `json:"classification_rule_description"`
	AnchorIndexDigestSHA256       string `json:"anchor_index_digest_sha256"`
	CorpusDigestSHA256            string `json:"corpus_digest_sha256"`

	RetrievalSurface RetrievalSurface `json:"retrieval_surface"`

	TargetPerStratum int              `json:"target_per_stratum"`
	Strata           []Stratum        `json:"strata"`
	Exclusions       []ExclusionCount `json:"exclusions"`
	Items            []Item           `json:"items"`

	// OverlapItemKeys is the deterministic second-adjudicator subset, fixed
	// before any label is compared (section 10).
	OverlapItemKeys []string `json:"overlap_item_keys"`

	DigestSHA256 string `json:"digest_sha256"`
}

// Build draws the sample and returns the manifest together with the blind
// packages, in one pass so the digest recorded for a package is the digest of
// the package actually emitted.
func Build(inv Inventory, corpus Corpus, opts Options, content ContentLookup) (Manifest, []BlindPackage, error) {
	if strings.TrimSpace(opts.Seed) == "" {
		return Manifest{}, nil, fmt.Errorf("selection seed is required: an unseeded sample cannot be recomputed, and a sample nobody can recompute cannot be audited")
	}
	if strings.TrimSpace(opts.GeneratedAt) == "" {
		return Manifest{}, nil, fmt.Errorf("generated-at timestamp is required: a self-stamped manifest is not reproducible")
	}
	if strings.TrimSpace(opts.ProtocolDigestSHA256) == "" {
		return Manifest{}, nil, fmt.Errorf("protocol digest is required: a sample that does not name the protocol it serves cannot be shown to obey it")
	}
	if strings.TrimSpace(opts.RetrievalSurface.ID) == "" {
		return Manifest{}, nil, fmt.Errorf("retrieval surface is required: section 3.1 is resolved before the sample is frozen, not after a score exists")
	}
	if corpus.DigestSHA256 == "" {
		return Manifest{}, nil, fmt.Errorf("eligible corpus is not content-addressed: it bounds what an adjudicator could mark applicable, so a sample that does not name it describes a different denominator")
	}
	target := opts.TargetPerStratum
	if target <= 0 {
		target = DefaultTargetPerStratum
	}

	m := Manifest{
		SchemaVersion:                 SchemaVersion,
		ProtocolID:                    opts.ProtocolID,
		ProtocolDigestSHA256:          opts.ProtocolDigestSHA256,
		SelectionSeed:                 opts.Seed,
		GeneratedAt:                   opts.GeneratedAt,
		World:                         inv.World,
		ClassificationRuleID:          inv.ClassificationRuleID,
		ClassificationRuleDescription: ClassificationRuleDescription,
		AnchorIndexDigestSHA256:       inv.AnchorIndexDigestSHA256,
		CorpusDigestSHA256:            corpus.DigestSHA256,
		RetrievalSurface:              opts.RetrievalSurface,
		TargetPerStratum:              target,
		Exclusions:                    inv.ExclusionCounts(),
	}

	var packages []BlindPackage
	for _, s := range Strata {
		pop := inv.InStratum(s)
		st := Stratum{
			Stratum:               s,
			InventoryDigestSHA256: inv.StratumDigests[s],
			Population:            len(pop),
			Target:                target,
		}
		if len(pop) == 0 {
			// Reported, never omitted. A stratum with nothing in it is a
			// finding under this protocol — section 6 of the design makes a
			// documented near-zero stratum A an acceptable outcome — and a
			// finding cannot be reported by a row that is missing.
			st.Status = StatusAbsent
			st.Reason = "no change in the frozen population was classified into this stratum"
			m.Strata = append(m.Strata, st)
			continue
		}
		drawn := draw(opts.Seed, inv.World, s, pop, target)
		if len(drawn) >= len(pop) {
			st.Status = StatusSampledAll
			// Section 8.2: a stratum with fewer than the target reports the
			// smaller denominator rather than borrowing from another stratum.
			st.Reason = fmt.Sprintf("population %d is below the target %d; the smaller denominator is reported rather than topped up from another stratum", len(pop), target)
		} else {
			st.Status = StatusSampled
		}
		for _, cl := range drawn {
			pkg, err := attachContent(buildBlindPackage(inv.World, corpus, cl), content)
			if err != nil {
				return Manifest{}, nil, err
			}
			d, err := DigestOf(pkg)
			if err != nil {
				return Manifest{}, nil, err
			}
			pkg.DigestSHA256 = d
			packages = append(packages, pkg)
			m.Items = append(m.Items, Item{
				ItemKey:                  pkg.ItemKey,
				ChangeID:                 cl.ChangeID,
				Stratum:                  s,
				WorldBindingDigestSHA256: inv.World.DigestSHA256,
				SelectionKey:             selectionKey(opts.Seed, inv.World, s, cl.ChangeID),
				BlindPayloadDigestSHA256: d,
			})
		}
		st.Selected = len(drawn)
		m.Strata = append(m.Strata, st)
	}

	m.OverlapItemKeys = overlapSubset(opts.Seed, m.Items, opts.OverlapFraction)

	m.DigestSHA256 = ""
	d, err := DigestOf(m)
	if err != nil {
		return Manifest{}, nil, err
	}
	m.DigestSHA256 = d
	return m, packages, nil
}

// draw applies section 8.2: a stable key from the committed seed and the
// change identity, sorted ascending, take the first N.
//
// The key can see the seed, the world and the change identity, and nothing
// else. It cannot see what the change did, whether it looks interesting, or
// what Sensei would surface for it — sampling changes because they look
// interesting is how a calibration run becomes an anecdote.
func draw(seed string, wb WorldBinding, stratum string, pop []Classification, target int) []Classification {
	keyed := make([]struct {
		key string
		cl  Classification
	}, 0, len(pop))
	for _, cl := range pop {
		keyed = append(keyed, struct {
			key string
			cl  Classification
		}{selectionKey(seed, wb, stratum, cl.ChangeID), cl})
	}
	sort.SliceStable(keyed, func(i, j int) bool {
		if keyed[i].key != keyed[j].key {
			return keyed[i].key < keyed[j].key
		}
		// Two identities hashing alike would otherwise be ordered by whatever
		// the slice happened to hold, and the sample would stop being
		// reproducible exactly where it is hardest to notice.
		return keyed[i].cl.ChangeID < keyed[j].cl.ChangeID
	})
	if target > len(keyed) {
		target = len(keyed)
	}
	out := make([]Classification, 0, target)
	for _, k := range keyed[:target] {
		out = append(out, k.cl)
	}
	return out
}

func selectionKey(seed string, wb WorldBinding, stratum, changeID string) string {
	return HashFields("sensei.prospective.selection.v1", seed, wb.DigestSHA256, stratum, changeID)
}

// itemKey is the identity a human label attaches to.
//
// It is scoped to the world binding and the stratum, not to the change alone:
// the same change under a different pinned world is a different question, and
// an unscoped key would let one adjudicated verdict migrate to a question it
// was never asked about.
func itemKey(wb WorldBinding, stratum, changeID string) string {
	return "pr1:" + HashFields("sensei.prospective.item.v1", wb.DigestSHA256, stratum, changeID)[:32]
}

// overlapSubset picks the second-adjudicator overlap deterministically, before
// either adjudicator's labels exist (section 10).
//
// Selecting it afterwards would let the overlap be chosen where the two humans
// happened to agree, and an agreement rate measured on a subset picked for
// agreement is not an agreement rate.
func overlapSubset(seed string, items []Item, fraction float64) []string {
	if len(items) == 0 || fraction <= 0 {
		return nil
	}
	keyed := make([]struct{ key, item string }, 0, len(items))
	for _, it := range items {
		keyed = append(keyed, struct{ key, item string }{
			HashFields("sensei.prospective.overlap.v1", seed, it.ItemKey), it.ItemKey,
		})
	}
	sort.SliceStable(keyed, func(i, j int) bool {
		if keyed[i].key != keyed[j].key {
			return keyed[i].key < keyed[j].key
		}
		return keyed[i].item < keyed[j].item
	})
	// Round up: 20% of a small sample rounds to zero under truncation, which
	// would type the overlap away exactly when the sample is small enough for
	// one adjudicator's idiosyncrasy to carry the whole result.
	take := int(float64(len(keyed))*fraction + 0.999999)
	if take > len(keyed) {
		take = len(keyed)
	}
	out := make([]string, 0, take)
	for _, k := range keyed[:take] {
		out = append(out, k.item)
	}
	sort.Strings(out)
	return out
}
