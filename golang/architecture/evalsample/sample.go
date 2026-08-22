// SPDX-License-Identifier: AGPL-3.0-only

// Package evalsample builds the frozen sample manifest of section 15 of
// docs/evaluation/phase10-reference-protocol-v1.md — step 9 of the #131
// handoff, the step between "protocol merged" and "human adjudication".
//
// It decides WHICH items a human will adjudicate. It never decides what any of
// them means, and it deliberately holds no verdict vocabulary at all: the
// protocol's freeze order puts this artifact before labels exist, and a
// generator able to express an answer is a generator able to leak one.
//
// The separation is the reason the artifact exists. Sensei produced the
// observations being sampled, so if Sensei also chose which of them to grade,
// the sample would be an opinion about its own output. Selection here is a
// stable hash over a committed seed and an identity. Nothing about a claim's
// content, its confidence, or whether it looks right can move it into or out
// of the sample.
package evalsample

import (
	"fmt"
	"sort"
	"strings"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/investigation"
)

// SchemaVersion identifies the manifest shape. A reader that does not know
// this version must refuse the file rather than interpret fields positionally.
const SchemaVersion = "sensei.eval_sample_manifest.v1"

// Lanes are the protocol's metric lanes (section 15, "metric lane").
const (
	LanePrecision     = "precision"
	LaneRecallUnit    = "recall_unit"
	LaneContradiction = "contradiction"
	LaneChallenge     = "challenge"
)

// Stratum statuses. An empty stratum is REPORTED with a reason rather than
// omitted: a lane that vanishes from the manifest reads as a lane nobody
// needed, and the protocol's denominators cannot tell that from a lane whose
// population happened to be zero.
const (
	StatusSampled    = "sampled"
	StatusSampledAll = "sampled_all_available"
	StatusAbsent     = "population_empty"
)

// Default sampling targets, from sections 6.1 and 7 of the protocol.
const (
	DefaultPrecisionPerProvider = 30
	DefaultRecallUnitsPerWorld  = 12
	// The protocol fixes no number for the contradiction and challenge lanes.
	// They take the recall unit target because they are adjudicated per case
	// rather than per claim, and the cost per item is comparable.
	DefaultCasesPerWorld = 12
)

// World is one pinned evaluation world and everything sampled from it.
//
// The caller supplies the extraction output; this package never runs an
// extractor. That keeps the sample bound to exactly the run whose identity the
// manifest records, instead of to a second run that might differ.
type World struct {
	Name    string
	Binding architecture.ClaimDocumentBinding

	// Observations is the deterministic lane's output for this world.
	Observations []architecture.Fact

	// Counterexamples and CandidateQuestions feed the challenge lane.
	Counterexamples    []investigation.Counterexample
	CandidateQuestions []architecture.OpenQuestion

	// FunctionalPredicates names the predicates that may hold only ONE object
	// for a subject. Only those can produce a contradiction case.
	//
	// It is a caller-supplied declaration, and its absence types the lane away
	// rather than defaulting to something permissive. The first version of this
	// package treated every repeated (subject, predicate) with differing
	// objects as a disagreement and produced 23,641 "contradiction cases" from
	// world 1 — almost all of them ordinary multi-valued relations, because a
	// component depending on many things is not a component contradicting
	// itself. Manufacturing that population would have handed an adjudicator
	// thousands of non-questions and let the lane report a healthy denominator
	// built from nothing.
	//
	// Which predicates are single-valued is a statement about the ontology, not
	// something a sampler may infer from the data it is sampling. So it comes
	// from outside, like the recall inventory, and for the same reason.
	FunctionalPredicates []string

	// RecallInventory is the INDEPENDENTLY defined unit inventory of section 7
	// — packages, components, bounded interactions. It must not be derived
	// from Sensei's output for this world, or recall would be measured over
	// exactly the units Sensei already had something to say about, which is
	// the one sampling error that makes omissions structurally invisible.
	RecallInventory []string
}

// Options are the frozen inputs of a sample: what protocol it serves, what
// seed orders it, and how large each stratum is allowed to be.
type Options struct {
	ProtocolID           string
	ProtocolDigestSHA256 string

	// Seed is committed in the manifest before labels exist (section 6.2).
	// Changing it creates a new sample version rather than a corrected one.
	Seed string

	// GeneratedAt is caller-supplied. A self-stamped artifact is not
	// reproducible, and this one is content-addressed.
	GeneratedAt string

	PrecisionPerProvider int
	RecallUnitsPerWorld  int
	CasesPerWorld        int
}

func (o Options) withDefaults() Options {
	if o.PrecisionPerProvider <= 0 {
		o.PrecisionPerProvider = DefaultPrecisionPerProvider
	}
	if o.RecallUnitsPerWorld <= 0 {
		o.RecallUnitsPerWorld = DefaultRecallUnitsPerWorld
	}
	if o.CasesPerWorld <= 0 {
		o.CasesPerWorld = DefaultCasesPerWorld
	}
	return o
}

// Manifest is the frozen selection. It contains no labels, no scores, and no
// verdict field for one to be written into later.
type Manifest struct {
	SchemaVersion        string `json:"schema_version"`
	ProtocolID           string `json:"protocol_id"`
	ProtocolDigestSHA256 string `json:"protocol_digest_sha256"`
	SelectionSeed        string `json:"selection_seed"`
	GeneratedAt          string `json:"generated_at"`

	Worlds  []WorldBinding `json:"worlds"`
	Strata  []Stratum      `json:"strata"`
	Items   []Item         `json:"items"`
	Targets Targets        `json:"targets"`

	// DigestSHA256 is the identity a reference-set release names as its
	// sample_manifest_digest_sha256. It is computed over every field above.
	DigestSHA256 string `json:"digest_sha256"`
}

// Targets records the sampling rule the run used, so a manifest read years
// later states its own parameters rather than relying on the defaults of
// whatever binary reads it.
type Targets struct {
	PrecisionPerProvider int `json:"precision_per_provider"`
	RecallUnitsPerWorld  int `json:"recall_units_per_world"`
	CasesPerWorld        int `json:"cases_per_world"`
}

// WorldBinding is the exact world an item was drawn from, reduced to its
// identity. The local checkout path is deliberately absent: it is not what was
// measured, and it makes two runs of the same world look different.
type WorldBinding struct {
	World            string `json:"world"`
	RepositoryDomain string `json:"repository_domain"`
	Revision         string `json:"revision,omitempty"`
	RevisionStatus   string `json:"revision_status"`
	TreeDigestSHA256 string `json:"tree_digest_sha256,omitempty"`
	DigestSHA256     string `json:"binding_digest_sha256"`
}

// Stratum is one (world, lane, provider) population and what was drawn from
// it. Both counts are kept because they answer different questions.
type Stratum struct {
	World      string `json:"world"`
	Lane       string `json:"lane"`
	ProviderID string `json:"provider_id,omitempty"`

	// Emissions is how many raw items the lane produced; Population is how
	// many DISTINCT identities they carry.
	//
	// Sampling is over distinct identities. Two byte-identical observations are
	// the same claim about the same anchor, so adjudicating both adds no
	// information while silently doubling that claim's weight in the score.
	// Both numbers are reported so a reader can see the collapse rather than
	// discover it in a denominator.
	Emissions  int `json:"emissions"`
	Population int `json:"population"`

	Target   int    `json:"target"`
	Selected int    `json:"selected"`
	Status   string `json:"status"`
	Reason   string `json:"reason,omitempty"`
}

// Item is one selected unit of adjudication (section 15).
type Item struct {
	ItemKey                  string `json:"item_key"`
	World                    string `json:"world"`
	WorldBindingDigestSHA256 string `json:"world_binding_digest_sha256"`
	Lane                     string `json:"lane"`
	ProviderID               string `json:"provider_id,omitempty"`

	// SubjectID is the sampled item's identity within its world.
	SubjectID string `json:"subject_id"`

	// Multiplicity is how many raw emissions carry this identity.
	Multiplicity int `json:"multiplicity,omitempty"`

	// SelectionKey is the stable hash the ordering used. It is published so
	// the selection can be recomputed and disputed, which is the only thing
	// that makes "deterministic" checkable rather than asserted.
	SelectionKey string `json:"selection_key"`

	// EvidenceIDs are the source identities an adjudicator needs to open.
	EvidenceIDs []string `json:"evidence_ids,omitempty"`

	// BlindPayloadDigestSHA256 binds the blinded view materialized for this
	// item, when one was (section 15). It is what proves the adjudicator saw
	// the payload the manifest describes and not a later edit of it.
	BlindPayloadDigestSHA256 string `json:"blind_payload_digest_sha256,omitempty"`
}

// BlindItem is the adjudicator's view. The provider label is absent by
// construction rather than by being blanked, so it cannot be restored by a
// reader who was not meant to have it (section 12).
//
// The claim and its evidence anchor ARE present: they are what makes support
// judgeable, and blinding is an anti-bias tool, never a reason to withhold the
// evidence the judgement needs.
type BlindItem struct {
	ItemKey     string   `json:"item_key"`
	Lane        string   `json:"lane"`
	Claim       string   `json:"claim,omitempty"`
	Unit        string   `json:"unit,omitempty"`
	EvidenceIDs []string `json:"evidence_ids,omitempty"`
	// Alternatives carries the sides of a contradiction case, so the
	// adjudicator sees a disagreement rather than one claim at a time.
	Alternatives []string `json:"alternatives,omitempty"`
}

// Build produces the manifest and the blinded views, in one pass so the digest
// recorded for a blind payload is the digest of the payload actually emitted.
func Build(worlds []World, opts Options) (Manifest, map[string][]BlindItem, error) {
	opts = opts.withDefaults()
	if strings.TrimSpace(opts.Seed) == "" {
		return Manifest{}, nil, fmt.Errorf("selection seed is required: an unseeded sample cannot be recomputed, and a sample nobody can recompute cannot be audited")
	}
	if strings.TrimSpace(opts.GeneratedAt) == "" {
		return Manifest{}, nil, fmt.Errorf("generated-at timestamp is required: a self-stamped manifest is not reproducible")
	}
	if strings.TrimSpace(opts.ProtocolDigestSHA256) == "" {
		return Manifest{}, nil, fmt.Errorf("protocol digest is required: a sample that does not name the protocol it serves cannot be shown to obey it")
	}

	m := Manifest{
		SchemaVersion:        SchemaVersion,
		ProtocolID:           opts.ProtocolID,
		ProtocolDigestSHA256: opts.ProtocolDigestSHA256,
		SelectionSeed:        opts.Seed,
		GeneratedAt:          opts.GeneratedAt,
		Targets: Targets{
			PrecisionPerProvider: opts.PrecisionPerProvider,
			RecallUnitsPerWorld:  opts.RecallUnitsPerWorld,
			CasesPerWorld:        opts.CasesPerWorld,
		},
	}
	blind := map[string][]BlindItem{}

	ordered := append([]World(nil), worlds...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Name < ordered[j].Name })

	seen := map[string]bool{}
	for _, w := range ordered {
		if strings.TrimSpace(w.Name) == "" {
			return Manifest{}, nil, fmt.Errorf("an evaluation world with no name cannot bind its items")
		}
		// Two worlds under one name keep both sets of items in the manifest
		// while the second world's blinded views overwrite the first's under
		// the same key — leaving manifest items whose adjudication payload no
		// longer exists, and a digest describing a sample that cannot be
		// adjudicated. The CLI rejects duplicates already; Build must not
		// depend on its caller having done so.
		// The blinded views are written to one file per (world, lane), so a
		// world name is a path component. A name carrying a separator or a
		// parent reference would put the adjudicator's payload somewhere the
		// run never said it wrote, which is worse than refusing it.
		if strings.ContainsAny(w.Name, `/\`) || strings.Contains(w.Name, "..") {
			return Manifest{}, nil, fmt.Errorf("evaluation world %q names a path rather than a world; its blinded views would be written outside the run's own output", w.Name)
		}
		if seen[w.Name] {
			return Manifest{}, nil, fmt.Errorf("evaluation world %q was supplied twice; each world's blinded views are keyed by its name, so the second would silently replace the first", w.Name)
		}
		seen[w.Name] = true
		wb := bindingOf(w)
		m.Worlds = append(m.Worlds, wb)

		for _, lane := range []string{LanePrecision, LaneRecallUnit, LaneContradiction, LaneChallenge} {
			strata, items, blinds, err := sampleLane(w, wb, lane, opts)
			if err != nil {
				return Manifest{}, nil, err
			}
			m.Strata = append(m.Strata, strata...)
			m.Items = append(m.Items, items...)
			if len(blinds) > 0 {
				blind[blindKey(w.Name, lane)] = blinds
			}
		}
	}

	sort.SliceStable(m.Items, func(i, j int) bool { return m.Items[i].ItemKey < m.Items[j].ItemKey })
	m.DigestSHA256 = digestManifest(m)
	return m, blind, nil
}

func blindKey(world, lane string) string { return world + "." + lane }
