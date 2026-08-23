// SPDX-License-Identifier: AGPL-3.0-only

// Package phase10score scores a filled Phase 10.8 reference set against
// docs/evaluation/phase10-reference-protocol-v2.md.
//
// It is the missing half of #131. The reference set is frozen, its blinded
// views are committed, and its nine label containers are empty — but nothing
// read those containers, so the metrics section 20 requires had no
// implementation. This package is written while every container is still
// empty, which is the same discipline the sample manifest was frozen under: a
// scorer authored after seeing labels is a scorer that can be shaped by them.
//
// Three properties are structural:
//
//   - Only `supported` and `unsupported` enter the primary precision
//     denominator. `ambiguous`, `outside_scope` and `cannot_adjudicate` are
//     counted and reported, never folded into either side.
//   - The headline is the macro average across provider strata, so a
//     high-volume provider cannot mathematically erase a weak thin one. Micro
//     precision is reported beside it, never instead of it.
//   - A metric the label containers cannot express is reported as
//     NOT COMPUTABLE with the reason, never as zero and never omitted. Section
//     20 exists to prevent flattering aggregation, and a silently missing
//     metric flatters by omission.
package phase10score

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Precision labels (protocol section 5.1).
const (
	LabelSupported        = "supported"
	LabelUnsupported      = "unsupported"
	LabelAmbiguous        = "ambiguous"
	LabelOutsideScope     = "outside_scope"
	LabelCannotAdjudicate = "cannot_adjudicate"
)

// PrecisionLabels is the closed vocabulary, in report order.
var PrecisionLabels = []string{LabelSupported, LabelUnsupported, LabelAmbiguous, LabelOutsideScope, LabelCannotAdjudicate}

// Expected-fact states for the recall lane (protocol section 5.2).
const (
	ExpectedSupported    = "expected_supported"
	ExpectedAmbiguous    = "expected_ambiguous"
	ExpectedOutsideScope = "expected_outside_scope"
)

// ExpectedStates is the closed recall vocabulary.
var ExpectedStates = []string{ExpectedSupported, ExpectedAmbiguous, ExpectedOutsideScope}

// Lanes as the sample manifest names them.
const (
	LanePrecision  = "precision"
	LaneRecallUnit = "recall_unit"
	LaneChallenge  = "challenge"
)

// LabelRecord is one human decision, as the committed containers hold it.
//
// The optional fields at the bottom do not exist in the v2 containers as
// generated. They are read when present so a reference set whose containers
// were extended BEFORE its first label can report the metrics that need them,
// and their absence is reported rather than silently scored as zero.
type LabelRecord struct {
	ItemKey                   string   `json:"item_key"`
	World                     string   `json:"world"`
	Lane                      string   `json:"lane"`
	AdjudicatorID             *string  `json:"adjudicator_id"`
	Label                     *string  `json:"label"`
	EvidenceIDsInspected      []string `json:"evidence_ids_inspected"`
	Rationale                 *string  `json:"rationale"`
	AdjudicatedAt             *string  `json:"adjudicated_at"`
	AdjudicatedAtSource       *string  `json:"adjudicated_at_source"`
	BlindedAtDecisionTime     *bool    `json:"blinded_at_decision_time"`
	DisagreementResolutionRef *string  `json:"disagreement_resolution_ref"`

	// ActionTaken is section 10's required "whether the challenge caused"
	// field; RequiredCorrection is section 11's correction count input; and
	// ExpectedFacts is section 5.2's frozen expected set, without which
	// primary recall has no denominator. None of the three exists in the v2
	// containers.
	ActionTaken        *string        `json:"action_taken,omitempty"`
	RequiredCorrection *bool          `json:"required_correction,omitempty"`
	ExpectedFacts      []ExpectedFact `json:"expected_facts,omitempty"`
	ActiveSeconds      *float64       `json:"active_seconds,omitempty"`
}

// ExpectedFact is one architectural fact a human wrote from the pinned
// evidence before seeing Sensei's extraction for that unit.
type ExpectedFact struct {
	ID      string `json:"id"`
	State   string `json:"state"`
	Matched *bool  `json:"matched"`
}

// LabelFile is one container.
type LabelFile struct {
	SchemaVersion              string        `json:"schema_version"`
	ProtocolID                 string        `json:"protocol_id"`
	ProtocolDigestSHA256       string        `json:"protocol_digest_sha256"`
	SampleManifestDigestSHA256 string        `json:"sample_manifest_digest_sha256"`
	World                      string        `json:"world"`
	Lane                       string        `json:"lane"`
	ItemCount                  int           `json:"item_count"`
	LabelledCount              int           `json:"labelled_count"`
	Labels                     []LabelRecord `json:"labels"`

	// Path and FileDigestSHA256 are filled by the loader. Section 17
	// content-addresses a reference-set release over the label FILE digests,
	// so the bytes each container was read from are part of the identity.
	Path             string `json:"-"`
	FileDigestSHA256 string `json:"-"`
}

// SampleItem is the manifest's account of one sampled item.
type SampleItem struct {
	ItemKey                  string   `json:"item_key"`
	World                    string   `json:"world"`
	WorldBindingDigestSHA256 string   `json:"world_binding_digest_sha256"`
	Lane                     string   `json:"lane"`
	ProviderID               string   `json:"provider_id"`
	SubjectID                string   `json:"subject_id"`
	Multiplicity             int      `json:"multiplicity"`
	SelectionKey             string   `json:"selection_key"`
	EvidenceIDs              []string `json:"evidence_ids"`
}

// Stratum is one sampling stratum.
type Stratum struct {
	World      string `json:"world"`
	Lane       string `json:"lane"`
	ProviderID string `json:"provider_id"`
	Emissions  int    `json:"emissions"`
	Population int    `json:"population"`
	Target     int    `json:"target"`
	Selected   int    `json:"selected"`
	Status     string `json:"status"`
}

// SampleManifest is the frozen sample.
type SampleManifest struct {
	SchemaVersion        string       `json:"schema_version"`
	ProtocolID           string       `json:"protocol_id"`
	ProtocolDigestSHA256 string       `json:"protocol_digest_sha256"`
	SelectionSeed        string       `json:"selection_seed"`
	GeneratedAt          string       `json:"generated_at"`
	Worlds               []World      `json:"worlds"`
	Strata               []Stratum    `json:"strata"`
	Items                []SampleItem `json:"items"`
	DigestSHA256         string       `json:"digest_sha256"`
}

// World is one evaluation world binding.
type World struct {
	World                    string `json:"world"`
	RepositoryDomain         string `json:"repository_domain"`
	Revision                 string `json:"revision"`
	WorldBindingDigestSHA256 string `json:"world_binding_digest_sha256"`
}

// Overlap is the second-adjudicator subset.
type Overlap struct {
	SchemaVersion              string `json:"schema_version"`
	ProtocolID                 string `json:"protocol_id"`
	ProtocolDigestSHA256       string `json:"protocol_digest_sha256"`
	SampleManifestDigestSHA256 string `json:"sample_manifest_digest_sha256"`
	SelectionSeed              string `json:"selection_seed"`
	TotalOverlapItems          int    `json:"total_overlap_items"`
	// Worlds is keyed by world name, as the committed artifact writes it.
	Worlds map[string]struct {
		ItemKeys []string `json:"item_keys"`
	} `json:"worlds"`
	FileDigestSHA256 string `json:"-"`
}

// ReferenceSet is a loaded Phase 10.8 reference set.
type ReferenceSet struct {
	Root     string
	Manifest SampleManifest
	// ManifestFileDigestSHA256 is the sha256 of the bytes on disk, which is
	// NOT the manifest's declared identity. The two are different facts and
	// the README says so; keeping both means a report can never quote one
	// where the other was meant.
	ManifestFileDigestSHA256 string
	Labels                   []LabelFile
	Overlap                  Overlap
}

// Load reads a reference set from disk.
func Load(root string) (*ReferenceSet, error) {
	rs := &ReferenceSet{Root: root}
	manifestPath := filepath.Join(root, "sample", "sample-manifest.json")
	digest, err := readJSONWithDigest(manifestPath, &rs.Manifest)
	if err != nil {
		return nil, fmt.Errorf("sample manifest: %w", err)
	}
	rs.ManifestFileDigestSHA256 = digest
	if strings.TrimSpace(rs.Manifest.DigestSHA256) == "" {
		return nil, fmt.Errorf("sample manifest declares no identity: a score that cannot name the sample it consumed is not replayable")
	}

	overlapPath := filepath.Join(root, "adjudicator-overlap.json")
	if d, err := readJSONWithDigest(overlapPath, &rs.Overlap); err == nil {
		rs.Overlap.FileDigestSHA256 = d
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("adjudicator overlap: %w", err)
	}

	entries, err := os.ReadDir(filepath.Join(root, "labels"))
	if err != nil {
		return nil, fmt.Errorf("labels: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".labels.json") {
			continue
		}
		path := filepath.Join(root, "labels", e.Name())
		var lf LabelFile
		d, err := readJSONWithDigest(path, &lf)
		if err != nil {
			return nil, fmt.Errorf("label container %s: %w", e.Name(), err)
		}
		lf.Path = e.Name()
		lf.FileDigestSHA256 = d
		if lf.SampleManifestDigestSHA256 != rs.Manifest.DigestSHA256 {
			return nil, fmt.Errorf("label container %s answers sample manifest %s, not %s: labels bound to a different sample are answers to a question nobody asked",
				e.Name(), lf.SampleManifestDigestSHA256, rs.Manifest.DigestSHA256)
		}
		if lf.ProtocolDigestSHA256 != rs.Manifest.ProtocolDigestSHA256 {
			return nil, fmt.Errorf("label container %s was generated under protocol digest %s, not %s",
				e.Name(), lf.ProtocolDigestSHA256, rs.Manifest.ProtocolDigestSHA256)
		}
		rs.Labels = append(rs.Labels, lf)
	}
	if len(rs.Labels) == 0 {
		return nil, fmt.Errorf("the reference set holds no label containers")
	}
	sort.Slice(rs.Labels, func(i, j int) bool { return rs.Labels[i].Path < rs.Labels[j].Path })
	return rs, nil
}

// ItemsByKey indexes the manifest.
func (rs *ReferenceSet) ItemsByKey() map[string]SampleItem {
	out := make(map[string]SampleItem, len(rs.Manifest.Items))
	for _, it := range rs.Manifest.Items {
		out[it.ItemKey] = it
	}
	return out
}
