// SPDX-License-Identifier: AGPL-3.0-only

package prospective

import (
	"fmt"
	"sort"
	"strings"
)

// CorpusItem is one piece of already-known architectural law eligible for
// adjudication.
//
// Anchors are the paths the graph attaches this item to. They are held here
// because the corpus is frozen with them — the eligible corpus is part of the
// ruler (protocol section 6.2) and must be reproducible — and they are the one
// field that never reaches an adjudicator. See BlindCorpusItem.
type CorpusItem struct {
	ID        string   `json:"id"`
	Class     string   `json:"class"`
	Title     string   `json:"title,omitempty"`
	Statement string   `json:"statement,omitempty"`
	Anchors   []string `json:"anchors,omitempty"`

	// Materialization records how the item's readable form was obtained, so a
	// reader can tell a node that stated its own meaning from one whose
	// meaning was composed from the law next door.
	Materialization string `json:"materialization,omitempty"`
}

// How a corpus item's readable form was obtained.
const (
	MaterializedFromNode    = "node_label_and_facts"
	MaterializedFromRelated = "composed_from_related_governing_nodes"
)

// Corpus is the frozen set of items an adjudicator may mark applicable.
//
// It is frozen by digest before labels exist because it bounds what could have
// been marked applicable: a recall denominator computed against a different
// corpus is a different experiment. Growing it later — even by adding
// genuinely relevant law — creates a new reference-set version rather than
// correcting the old one.
type Corpus struct {
	RepositoryDomain  string       `json:"repository_domain"`
	GraphDigestSHA256 string       `json:"graph_digest_sha256"`
	ProducedBy        string       `json:"produced_by"`
	Items             []CorpusItem `json:"items"`

	// Excluded names every eligible-class item that could NOT be made
	// adjudicable, with a stable reason.
	//
	// An earlier version kept such items in the corpus and counted them
	// separately. That was worse than it looked: 305 of 423 rows reached the
	// adjudication package as bare identifiers, so the package advertised a
	// 423-item denominator while only 118 rows could actually be judged. An
	// item a human cannot read cannot be marked applicable, so it does not
	// belong in the set that bounds the denominator — but it must not vanish
	// either, or the shortfall becomes invisible.
	Excluded []CorpusExclusion `json:"excluded,omitempty"`

	// Accounting reconciles what the graph holds against what this corpus
	// could enumerate and materialize, per class.
	Accounting []ClassAccounting `json:"accounting,omitempty"`

	// QueryRowCap is the production row cap the enumeration ran under. It is
	// recorded because it, not the graph, is what bounds the enumeration.
	QueryRowCap int `json:"query_row_cap,omitempty"`

	DigestSHA256 string `json:"digest_sha256"`
}

// Corpus exclusion reasons. They are constants because a reason a report
// groups by must mean the same thing in every run.
const (
	// CorpusExcludedUnresolvable is an item the pinned world could not resolve
	// to a node at all.
	CorpusExcludedUnresolvable = "unresolvable_from_pinned_world"
	// CorpusExcludedNoStatement is an item that resolved but carries nothing a
	// human could read and judge.
	CorpusExcludedNoStatement = "no_human_readable_statement"
	// CorpusNotEnumerable is counted rather than listed, because the ids are
	// exactly what the row cap withheld. It is the difference between what the
	// graph reports holding and what a capped enumeration could see.
	CorpusNotEnumerable = "not_enumerable_within_query_row_cap"
)

// CorpusExclusion is one item that could not be made adjudicable.
type CorpusExclusion struct {
	ID     string `json:"id"`
	Class  string `json:"class"`
	Reason string `json:"reason"`
	Detail string `json:"detail,omitempty"`
}

// ClassAccounting reconciles one class's numbers so the effective denominator
// is unambiguous.
//
// GraphTotal comes from the graph's own metadata rather than from the
// enumeration, which is the whole point: without an independent total, a
// capped enumeration reports its cap as if it were the population.
type ClassAccounting struct {
	Class      string `json:"class"`
	GraphTotal int    `json:"graph_total"`
	Enumerated int    `json:"enumerated"`
	// NotEnumerable is GraphTotal minus Enumerated: items the row cap withheld.
	NotEnumerable int `json:"not_enumerable_within_query_row_cap"`
	// Materialized is what reached the eligible corpus, and is the only number
	// that bounds the recall denominator.
	Materialized int `json:"materialized"`
	Excluded     int `json:"excluded"`
}

// Adjudicable reports the effective eligible-corpus denominator: every item in
// Items is human-adjudicable by construction, so this is simply their count.
// It exists as a named method so a report cannot accidentally quote a total
// that includes rows nobody could judge.
func (c Corpus) Adjudicable() int { return len(c.Items) }

// NormalizeCorpus sorts, deduplicates by ID and content-addresses the corpus.
func NormalizeCorpus(c Corpus) (Corpus, error) {
	if strings.TrimSpace(c.ProducedBy) == "" {
		return Corpus{}, fmt.Errorf("corpus names no producing command: an eligible corpus nobody can reproduce cannot be defended as the denominator's bound")
	}
	seen := map[string]bool{}
	out := make([]CorpusItem, 0, len(c.Items))
	for _, it := range c.Items {
		id := strings.TrimSpace(it.ID)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		anchors := append([]string(nil), it.Anchors...)
		sort.Strings(anchors)
		it.ID, it.Anchors = id, anchors
		out = append(out, it)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	c.Items = out
	for _, it := range c.Items {
		// Enforced here rather than trusted from the caller: this is the one
		// invariant that makes the denominator mean what a report says it
		// means, and it must hold for every path that builds a corpus.
		if strings.TrimSpace(it.Title) == "" && strings.TrimSpace(it.Statement) == "" {
			return Corpus{}, fmt.Errorf("eligible item %s carries neither a title nor a statement: a human cannot judge whether it governs a change, so it may not bound the denominator — exclude it with reason %s instead",
				it.ID, CorpusExcludedNoStatement)
		}
	}
	excluded := append([]CorpusExclusion(nil), c.Excluded...)
	sort.SliceStable(excluded, func(i, j int) bool {
		if excluded[i].Reason != excluded[j].Reason {
			return excluded[i].Reason < excluded[j].Reason
		}
		return excluded[i].ID < excluded[j].ID
	})
	c.Excluded = excluded
	acc := append([]ClassAccounting(nil), c.Accounting...)
	sort.SliceStable(acc, func(i, j int) bool { return acc[i].Class < acc[j].Class })
	for _, a := range acc {
		// Accounting that does not add up is worse than none: it looks like a
		// reconciliation while hiding whichever rows it fails to mention.
		if a.Materialized+a.Excluded != a.Enumerated {
			return Corpus{}, fmt.Errorf("class %s: %d materialized plus %d excluded does not account for the %d rows enumerated",
				a.Class, a.Materialized, a.Excluded, a.Enumerated)
		}
		want := a.GraphTotal - a.Enumerated
		if want < 0 {
			want = 0
		}
		if a.NotEnumerable != want {
			return Corpus{}, fmt.Errorf("class %s: the graph reports %d items and %d were enumerated, so %d were beyond the row cap, not %d",
				a.Class, a.GraphTotal, a.Enumerated, want, a.NotEnumerable)
		}
	}
	c.Accounting = acc
	c.DigestSHA256 = ""
	d, err := DigestOf(c)
	if err != nil {
		return Corpus{}, err
	}
	c.DigestSHA256 = d
	return c, nil
}

// BlindCorpusItem is one eligible item as the adjudicator sees it.
//
// The anchors are absent BY CONSTRUCTION rather than blanked, so they cannot
// be restored by a reader who was not meant to have them. This is the single
// most load-bearing omission in the package: the anchors are Sensei's own
// account of which files an item governs, so showing them would hand the
// adjudicator the answer key and turn applicability adjudication into
// agreement with the system being graded.
type BlindCorpusItem struct {
	ID        string `json:"id"`
	Class     string `json:"class"`
	Title     string `json:"title,omitempty"`
	Statement string `json:"statement,omitempty"`
}

// BlindChange is the change as the adjudicator sees it: what was proposed, and
// what the world looked like before it.
//
// The stratum is absent, and so is the anchored/unanchored partition that
// produced it. Both are statements about what Sensei already knows, and an
// adjudicator told "the graph has nothing for this file" has been told
// something about the answer before deciding the question.
type BlindChange struct {
	ChangeID             string       `json:"change_id"`
	BaseRevision         string       `json:"base_revision"`
	BaseTreeDigestSHA256 string       `json:"base_tree_digest_sha256"`
	Paths                []PathChange `json:"paths"`
	Content              string       `json:"content"`
	ContentDigestSHA256  string       `json:"content_digest_sha256"`
}

// BlindCorpusSchemaVersion identifies the shared blind corpus shape.
const BlindCorpusSchemaVersion = "sensei.prospective_blind_corpus.v1"

// BlindCorpus is the eligible corpus as every adjudicator sees it, stored once
// and content-addressed on its own.
//
// It is a separate artifact from the frozen Corpus, not a view of it, and the
// separation is the point rather than a saving. Corpus carries anchors,
// materialization provenance and per-class accounting — all of which are
// Sensei's own account of what it knows and where it applies, and all of which
// are withheld from an adjudicator. A package that referenced the full corpus
// file would hand that over by reference instead of by value, which is the
// same disclosure with an extra step.
//
// Storing it once also removes a real hazard: with the corpus embedded per
// package, 48 copies could drift apart under a partial regeneration, and
// nothing in the artifact would show which copy an adjudicator actually read.
type BlindCorpus struct {
	SchemaVersion string            `json:"schema_version"`
	Items         []BlindCorpusItem `json:"items"`
	DigestSHA256  string            `json:"digest_sha256"`
}

// NewBlindCorpus derives the shared blind corpus and content-addresses it.
func NewBlindCorpus(c Corpus) (BlindCorpus, error) {
	bc := BlindCorpus{SchemaVersion: BlindCorpusSchemaVersion, Items: blindCorpus(c)}
	d, err := DigestOf(bc)
	if err != nil {
		return BlindCorpus{}, err
	}
	bc.DigestSHA256 = d
	return bc, nil
}

// BlindCorpusRef is the file an adjudication package points at. It is a
// relative name so a reference set can be moved or published without
// rewriting every package.
const BlindCorpusRef = "blind-corpus.json"

// BlindPackage is one adjudication unit: one change, its world binding, and a
// reference to the shared blind corpus.
//
// It carries no Sensei retrieval output of any kind, and there is no field in
// this type for one — a package that could hold a surfaced set is a package
// that will eventually be emitted holding one.
type BlindPackage struct {
	ItemKey string       `json:"item_key"`
	World   WorldBinding `json:"world"`
	Change  BlindChange  `json:"change"`

	// CorpusDigestSHA256 binds the frozen Corpus this blind corpus was derived
	// from. It is a provenance binding, not somewhere to read from: the full
	// corpus holds material withheld from the adjudicator, and the package
	// names its digest only so a later reader can prove which corpus the blind
	// view came from.
	CorpusDigestSHA256 string `json:"corpus_digest_sha256"`

	// BlindCorpusDigestSHA256 and BlindCorpusRef are what an adjudicator
	// actually opens.
	BlindCorpusDigestSHA256 string `json:"blind_corpus_digest_sha256"`
	BlindCorpusRef          string `json:"blind_corpus_ref"`

	DigestSHA256 string `json:"digest_sha256,omitempty"`
}

// ContentLookup returns the exact diff or new-file contents of a change.
//
// It is a parameter rather than a fetch inside this package so that Build
// stays deterministic given its inputs, and so a test can drive it without a
// repository.
type ContentLookup func(changeID string) (string, error)

func buildBlindPackage(wb WorldBinding, corpus Corpus, blind BlindCorpus, cl Classification) BlindPackage {
	return BlindPackage{
		ItemKey: itemKey(wb, cl.Stratum, cl.ChangeID),
		World:   wb,
		Change: BlindChange{
			ChangeID:             cl.ChangeID,
			BaseRevision:         cl.Change.BaseRevision,
			BaseTreeDigestSHA256: cl.Change.BaseTreeDigestSHA256,
			Paths:                cl.Change.Paths,
			ContentDigestSHA256:  cl.Change.ContentDigestSHA256,
		},
		CorpusDigestSHA256:      corpus.DigestSHA256,
		BlindCorpusDigestSHA256: blind.DigestSHA256,
		BlindCorpusRef:          BlindCorpusRef,
	}
}

func blindCorpus(c Corpus) []BlindCorpusItem {
	out := make([]BlindCorpusItem, 0, len(c.Items))
	for _, it := range c.Items {
		out = append(out, BlindCorpusItem{ID: it.ID, Class: it.Class, Title: it.Title, Statement: it.Statement})
	}
	return out
}

// attachContent fills in the change contents and refuses any that do not
// match the digest the inventory froze.
//
// The refusal matters more than the fill. The inventory pins each change by
// content digest; if the checkout now yields different bytes, the package
// would show an adjudicator one change while the sample names another, and
// every label collected against it would be attached to the wrong question.
//
// It runs BEFORE the package is digested, and that order is load-bearing: a
// package digested empty and filled afterwards would have the manifest binding
// a payload the adjudicator never saw.
func attachContent(p BlindPackage, lookup ContentLookup) (BlindPackage, error) {
	if lookup == nil {
		return BlindPackage{}, fmt.Errorf("no content lookup supplied: an adjudication package without the change contents asks a human to judge a change they cannot read")
	}
	content, err := lookup(p.Change.ChangeID)
	if err != nil {
		return BlindPackage{}, fmt.Errorf("change %s: %w", p.Change.ChangeID, err)
	}
	got, err := DigestOf(content)
	if err != nil {
		return BlindPackage{}, err
	}
	if p.Change.ContentDigestSHA256 != "" && got != p.Change.ContentDigestSHA256 {
		return BlindPackage{}, fmt.Errorf("change %s: contents digest %s does not match the %s the inventory froze — the sample and the package would describe different changes",
			p.Change.ChangeID, got, p.Change.ContentDigestSHA256)
	}
	p.Change.Content = content
	p.Change.ContentDigestSHA256 = got
	return p, nil
}

// AnchorIndexFromCorpus derives the anchored-path set from the frozen eligible
// corpus.
//
// An earlier rule probed `sensei impact --file <path>` per path and treated an
// empty answer as "not anchored". On this graph that probe returns nothing even
// for paths the corpus itself names as anchors, so the whole world classified
// as unanchored and strata C and D came out empty — a confident empty result
// laundering a query's blind spot into a fact about the repository, which is
// the failure protocol section 7.3 exists to separate from an honest miss.
//
// Deriving from the corpus removes the second query and, with it, the chance
// of the two disagreeing: an item is eligible exactly when it is in the corpus,
// and a path is anchored exactly when some eligible item governs it. The
// anchored set and the denominator's bound then describe the same graph state
// rather than two reads of it taken by different means.
func AnchorIndexFromCorpus(c Corpus) (AnchorIndex, error) {
	if c.DigestSHA256 == "" {
		return AnchorIndex{}, fmt.Errorf("corpus is not content-addressed: an anchor index derived from it would name evidence nobody can reproduce")
	}
	var paths []string
	for _, it := range c.Items {
		paths = append(paths, it.Anchors...)
	}
	return NormalizeAnchorIndex(AnchorIndex{
		RepositoryDomain:  c.RepositoryDomain,
		GraphDigestSHA256: c.GraphDigestSHA256,
		ProducedBy: "derived from the frozen eligible corpus " + c.DigestSHA256 +
			": a path is anchored when at least one eligible item names it as a file anchor. Produced by: " + c.ProducedBy,
		AnchoredPaths: paths,
	})
}

// VerifyAnchorsReachThePopulation refuses an index whose paths never meet the
// population it is supposed to stratify.
//
// A corpus that carries anchors while not one of them matches an inventory path
// is almost always a path-form mismatch — one side repo-relative, the other
// prefixed or absolute. Left unchecked it classifies every change as unanchored
// and reports empty anchored strata as a finding about the repository.
func VerifyAnchorsReachThePopulation(idx AnchorIndex, inventoryPaths []string) error {
	if len(idx.AnchoredPaths) == 0 {
		return nil
	}
	in := map[string]bool{}
	for _, p := range inventoryPaths {
		in[p] = true
	}
	for _, a := range idx.AnchoredPaths {
		if in[a] {
			return nil
		}
	}
	return fmt.Errorf("the anchor index names %d anchored paths and not one of them appears among the %d paths in the population — this is a path-form mismatch, not an unanchored world, and classifying on it would report empty anchored strata as a finding",
		len(idx.AnchoredPaths), len(inventoryPaths))
}
