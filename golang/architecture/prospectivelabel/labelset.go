// SPDX-License-Identifier: AGPL-3.0-only

package prospectivelabel

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/globulario/sensei/golang/architecture/prospective"
)

// LabelSetSchemaVersion identifies the frozen answer key's shape.
const LabelSetSchemaVersion = "sensei.prospective_labels.v1"

// SecondAdjudicatorUnavailable is protocol section 10's typed absence. It is
// recorded, never substituted, and no AI may stand in for one.
const SecondAdjudicatorUnavailable = "second_adjudicator_unavailable"

// LabelSet is the frozen answer key. It records how every label came to exist,
// so a reader can weigh the denominator rather than be told a number.
//
// It lives here rather than in the labelling tool because two things now read
// it — the tool that writes it and the scorer that is graded against it — and
// two structs describing one answer key is how a field silently stops being
// carried across.
type LabelSet struct {
	SchemaVersion              string         `json:"schema_version"`
	ProtocolID                 string         `json:"protocol_id"`
	SampleManifestDigestSHA256 string         `json:"sample_manifest_digest_sha256"`
	BlindCorpusDigestSHA256    string         `json:"blind_corpus_digest_sha256"`
	WorldRevision              string         `json:"world_revision"`
	Adjudicator                string         `json:"adjudicator"`
	SecondAdjudicator          string         `json:"second_adjudicator,omitempty"`
	SecondAdjudicatorStatus    string         `json:"second_adjudicator_status"`
	FrozenAt                   string         `json:"frozen_at"`
	Labels                     []Label        `json:"labels"`
	Coverage                   []Coverage     `json:"coverage"`
	Totals                     map[string]int `json:"totals"`
	DigestSHA256               string         `json:"digest_sha256"`
}

// Seal content-addresses a label set. The digest is computed over the set with
// the digest field empty, so it can be recomputed by any reader.
func (ls LabelSet) Seal() (LabelSet, error) {
	ls.DigestSHA256 = ""
	d, err := prospective.DigestOf(ls)
	if err != nil {
		return LabelSet{}, err
	}
	ls.DigestSHA256 = d
	return ls, nil
}

// LoadLabelSet reads a frozen answer key and verifies it is internally
// consistent before anybody scores against it.
func LoadLabelSet(path string) (LabelSet, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return LabelSet{}, err
	}
	var ls LabelSet
	if err := json.Unmarshal(body, &ls); err != nil {
		return LabelSet{}, fmt.Errorf("frozen labels: %w", err)
	}
	if ls.SchemaVersion != LabelSetSchemaVersion {
		return LabelSet{}, fmt.Errorf("frozen labels carry schema %q, not %q: a reader that does not know a version must refuse rather than interpret fields positionally",
			ls.SchemaVersion, LabelSetSchemaVersion)
	}
	if strings.TrimSpace(ls.FrozenAt) == "" {
		return LabelSet{}, fmt.Errorf("frozen labels carry no frozen-at timestamp: an answer key that cannot say when it was fixed cannot be shown to predate the retrieval it grades")
	}
	if strings.TrimSpace(ls.Adjudicator) == "" {
		return LabelSet{}, fmt.Errorf("frozen labels name no adjudicator: an answer key nobody is named on cannot be checked against a second one")
	}
	if len(ls.Labels) == 0 {
		return LabelSet{}, fmt.Errorf("frozen labels contain no decisions")
	}
	for _, l := range ls.Labels {
		if !knownLabel(l.Label) {
			return LabelSet{}, fmt.Errorf("frozen labels contain %q, which is not one of the five labels; the vocabulary is closed", l.Label)
		}
		if l.AssignmentMode != ModeIndividual && l.AssignmentMode != ModeBulkSweep {
			return LabelSet{}, fmt.Errorf("label for (%s, %s) carries the unknown assignment mode %q", l.ItemKey, l.CorpusItemID, l.AssignmentMode)
		}
	}
	sealed, err := ls.Seal()
	if err != nil {
		return LabelSet{}, err
	}
	if ls.DigestSHA256 != "" && sealed.DigestSHA256 != ls.DigestSHA256 {
		return LabelSet{}, fmt.Errorf("frozen labels do not hash to the digest they carry (%s vs recomputed %s): the answer key has been edited since it was frozen",
			ls.DigestSHA256, sealed.DigestSHA256)
	}
	if ls.DigestSHA256 == "" {
		return LabelSet{}, fmt.Errorf("frozen labels carry no digest: an answer key that is not content-addressed cannot be shown to be the one a score was computed against")
	}
	return ls, nil
}

// VerifyBinding is the protocol's fifth arrow, enforced mechanically.
//
// Labels answer one manifest and one blind corpus. Scored against a different
// sample or a different eligible corpus, they are answers to a question nobody
// asked — and the mismatch would be invisible in the resulting number.
func (ls LabelSet) VerifyBinding(manifestDigest, blindCorpusDigest string) error {
	if ls.SampleManifestDigestSHA256 != manifestDigest {
		return fmt.Errorf("frozen labels answer sample manifest %s, but this reference set is %s: a different sample is a different experiment",
			ls.SampleManifestDigestSHA256, manifestDigest)
	}
	if ls.BlindCorpusDigestSHA256 != blindCorpusDigest {
		return fmt.Errorf("frozen labels answer blind corpus %s, but this reference set is %s: the corpus bounds what could be marked applicable, so this is a different denominator",
			ls.BlindCorpusDigestSHA256, blindCorpusDigest)
	}
	return nil
}

// LabelFor returns the human decision for one pair, and whether one exists.
func (ls LabelSet) LabelFor(itemKey, corpusItemID string) (Label, bool) {
	for _, l := range ls.Labels {
		if l.ItemKey == itemKey && l.CorpusItemID == corpusItemID {
			return l, true
		}
	}
	return Label{}, false
}
