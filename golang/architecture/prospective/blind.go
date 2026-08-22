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
}

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
	DigestSHA256      string       `json:"digest_sha256"`
}

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

// BlindPackage is one adjudication unit: one change, its world binding, and
// the eligible corpus. It carries no Sensei retrieval output of any kind, and
// there is no field in this type for one — a package that could hold a
// surfaced set is a package that will eventually be emitted holding one.
type BlindPackage struct {
	ItemKey            string            `json:"item_key"`
	World              WorldBinding      `json:"world"`
	Change             BlindChange       `json:"change"`
	CorpusDigestSHA256 string            `json:"corpus_digest_sha256"`
	EligibleCorpus     []BlindCorpusItem `json:"eligible_corpus"`
	DigestSHA256       string            `json:"digest_sha256,omitempty"`
}

// ContentLookup returns the exact diff or new-file contents of a change.
//
// It is a parameter rather than a fetch inside this package so that Build
// stays deterministic given its inputs, and so a test can drive it without a
// repository.
type ContentLookup func(changeID string) (string, error)

func buildBlindPackage(wb WorldBinding, corpus Corpus, cl Classification) BlindPackage {
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
		CorpusDigestSHA256: corpus.DigestSHA256,
		EligibleCorpus:     blindCorpus(corpus),
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
