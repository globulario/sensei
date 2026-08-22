// SPDX-License-Identifier: AGPL-3.0-only

// Package evalmodel separates a nondeterministic model ACQUISITION from the
// deterministic SCORING of what it produced.
//
// A live model call may legitimately answer differently every time. Pretending
// otherwise would be fiction, and a benchmark built on that fiction would report
// replay failures that are really just the model being a model.
//
//	LIVE ACQUISITION            (nondeterministic, content-addressed once)
//	        |
//	FROZEN ACQUISITION BUNDLE   (immutable input)
//	        |
//	DETERMINISTIC SCORING       (must replay byte-identically)
//
// So the rule is split in two: a re-acquisition that returns a different
// artifact gets a DIFFERENT acquisition identity — which is a new measurement,
// not a failed replay — while the scorer over one frozen bundle and one frozen
// reference set must produce identical bytes forever.
package evalmodel

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"

	"github.com/globulario/sensei/golang/architecture/investigation"
	"github.com/globulario/sensei/golang/architecture/modelexec"
)

const (
	AcquisitionSchemaVersion = "sensei.eval_model_acquisition.v1"
	ScoreSchemaVersion       = "sensei.eval_model_score.v1"
)

// DeterministicBaseline is what the run recovered WITHOUT the model. It is
// carried alongside the model result and never merged into it: a reader must
// always be able to tell which lane produced which item.
// BaselineItem is one claim the DETERMINISTIC lane produced.
//
// The counts alone cannot be scored: a human label attaches to a claim, so
// without the claims themselves the scorer can report model-item counts and
// nothing else — no deterministic-lane score, and therefore no model delta,
// which is the comparison the whole arm exists to make.
type BaselineItem struct {
	Kind             string   `json:"kind"`
	Text             string   `json:"text"`
	CitedEvidenceIDs []string `json:"cited_evidence_ids,omitempty"`
	FilePaths        []string `json:"file_paths,omitempty"`
}

type DeterministicBaseline struct {
	// DocumentDigestSHA256 is the upstream HOW document. It identifies the
	// input the deterministic lane started from.
	DocumentDigestSHA256 string `json:"document_digest_sha256"`
	// ComposedResultDigestSHA256 identifies what the deterministic lane
	// actually PRODUCED. Counts are not an identity: composition can change
	// which candidates it produced without changing how many, and a baseline
	// described only by an upstream digest plus counts would then reuse one
	// identity for two different results.
	ComposedResultDigestSHA256 string `json:"composed_result_digest_sha256,omitempty"`
	ObservationCount           int    `json:"observation_count"`
	CandidateCount             int    `json:"candidate_count"`
	// Candidates are the scoreable deterministic claims, carried so the
	// deterministic lane can be measured against the same reference set as the
	// model lane.
	Candidates []BaselineItem `json:"candidates,omitempty"`
}

// AcquiredItem is one model-derived proposal, recorded with its provenance.
type AcquiredItem struct {
	Kind             string   `json:"kind"`
	Text             string   `json:"text"`
	CitedEvidenceIDs []string `json:"cited_evidence_ids,omitempty"`
	FilePaths        []string `json:"file_paths,omitempty"`
}

// Acquisition is the frozen record of one live model call.
type Acquisition struct {
	SchemaVersion string                `json:"schema_version"`
	CapturedAt    string                `json:"captured_at"`
	Baseline      DeterministicBaseline `json:"deterministic_baseline"`

	// Model is the terminal binding EXACTLY as modelexec produced it. The
	// evaluator copies it; it never reinterprets a status or supplies one.
	Model investigation.ModelBinding `json:"model"`

	// Items are the accepted model-derived proposals, empty for every
	// non-resolved outcome. A refusal or an error is a result worth freezing.
	Items []AcquiredItem `json:"items,omitempty"`

	AcquisitionDigestSHA256 string `json:"acquisition_digest_sha256"`
}

// NewAcquisition freezes one measurement and content-addresses it.
//
// The identity covers the deterministic baseline, the model binding (which
// carries provider, model, request and artifact identity) and the accepted
// items. Two live calls that answered differently therefore differ here, which
// is the honest outcome: a new acquisition, not a broken replay.
func NewAcquisition(capturedAt string, baseline DeterministicBaseline, outcome modelexec.Outcome) Acquisition {
	a := Acquisition{
		SchemaVersion: AcquisitionSchemaVersion,
		CapturedAt:    capturedAt,
		Baseline:      baseline,
		Model:         outcome.Binding,
	}
	if outcome.Binding.Status == investigation.ModelStatusResolved && outcome.Artifact != nil {
		for _, item := range outcome.Artifact.Items {
			a.Items = append(a.Items, AcquiredItem{
				Kind:             item.Kind,
				Text:             item.Text,
				CitedEvidenceIDs: sortedCopy(item.CitedEvidenceIDs),
				FilePaths:        sortedCopy(item.FilePaths),
			})
		}
		// A TOTAL order. Sorting on kind and text alone leaves items that differ
		// only in citations or file paths in the provider's arrival order, so
		// swapping two otherwise-identical set members would change the
		// marshalled bytes and mint a new acquisition identity — a reordered
		// answer reported as a new measurement. The full item key breaks every
		// remaining tie.
		sort.SliceStable(a.Items, func(i, j int) bool {
			if a.Items[i].Kind != a.Items[j].Kind {
				return a.Items[i].Kind < a.Items[j].Kind
			}
			if a.Items[i].Text != a.Items[j].Text {
				return a.Items[i].Text < a.Items[j].Text
			}
			// Content-only tie-break: the scoped key depends on the
			// acquisition being built, so using it here would be circular.
			if ac, bc := strings.Join(a.Items[i].CitedEvidenceIDs, "\x00"), strings.Join(a.Items[j].CitedEvidenceIDs, "\x00"); ac != bc {
				return ac < bc
			}
			return strings.Join(a.Items[i].FilePaths, "\x00") < strings.Join(a.Items[j].FilePaths, "\x00")
		})
	}
	a.Baseline.Candidates = sortedBaselineItems(a.Baseline.Candidates)
	a.AcquisitionDigestSHA256 = acquisitionDigest(a)
	return a
}

func acquisitionDigest(a Acquisition) string {
	a.AcquisitionDigestSHA256 = ""
	// CapturedAt is excluded: when the same model returns the same answer about
	// the same baseline, that is the same measurement regardless of the clock.
	// Letting the clock into the identity is the mistake already recorded
	// against the verification-record path.
	a.CapturedAt = ""
	data, _ := json.Marshal(a)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func sortedCopy(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := append([]string{}, in...)
	sort.Strings(out)
	return out
}

// sortedBaselineItems imposes the same total order on the deterministic lane
// that the model lane gets, so a reordered identical baseline is not a new
// measurement either.
func sortedBaselineItems(in []BaselineItem) []BaselineItem {
	out := append([]BaselineItem{}, in...)
	for i := range out {
		out[i].CitedEvidenceIDs = sortedCopy(out[i].CitedEvidenceIDs)
		out[i].FilePaths = sortedCopy(out[i].FilePaths)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		if out[i].Text != out[j].Text {
			return out[i].Text < out[j].Text
		}
		if a, b := strings.Join(out[i].CitedEvidenceIDs, "\x00"), strings.Join(out[j].CitedEvidenceIDs, "\x00"); a != b {
			return a < b
		}
		return strings.Join(out[i].FilePaths, "\x00") < strings.Join(out[j].FilePaths, "\x00")
	})
	if len(out) == 0 {
		return nil
	}
	return out
}
