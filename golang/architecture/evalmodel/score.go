// SPDX-License-Identifier: AGPL-3.0-only

package evalmodel

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"

	"github.com/globulario/sensei/golang/architecture/investigation"
)

// ReferenceSet is the INDEPENDENT human answer key. This package can consume
// one and can never author one: every field here is filled in outside the
// system being graded, under docs/evaluation/phase10-reference-protocol-v1.md.
//
// It is identified by digest so a score always names the exact ruler it was
// measured against, and a ruler that moved after the fact is detectable.
type ReferenceSet struct {
	SchemaVersion string `json:"schema_version"`
	ProtocolID    string `json:"protocol_id"`

	// The constituents section 17 of the protocol defines a reference-set
	// release over. They are represented here so the recomputed identity is
	// the identity the protocol specifies, not a reduced local imitation of
	// it: hashing only a protocol name and flattened labels would give two
	// genuinely different rulers — different samples, different adjudicators,
	// different worlds — the same accepted identity whenever their verdicts
	// happened to coincide. It would also make an externally computed release
	// digest fail this check, pushing a release toward being recomputed
	// locally rather than verified.
	ProtocolDigestSHA256           string   `json:"protocol_digest_sha256,omitempty"`
	SampleManifestDigestSHA256     string   `json:"sample_manifest_digest_sha256,omitempty"`
	LabelFileDigestsSHA256         []string `json:"label_file_digests_sha256,omitempty"`
	WorldBindingDigestsSHA256      []string `json:"world_binding_digests_sha256,omitempty"`
	AdjudicatorOverlapDigestSHA256 string   `json:"adjudicator_overlap_digest_sha256,omitempty"`

	DigestSHA256 string           `json:"digest_sha256"`
	Labels       []ReferenceLabel `json:"labels"`
}

// ReferenceLabel is one adjudicated verdict. The vocabulary is the protocol's,
// not this package's.
type ReferenceLabel struct {
	ItemKey string `json:"item_key"`
	Verdict string `json:"verdict"`
}

// Closed verdict vocabulary, mirroring the frozen protocol.
const (
	VerdictSupported    = "supported"
	VerdictUnsupported  = "unsupported"
	VerdictAmbiguous    = "ambiguous"
	VerdictOutsideScope = "outside_scope"
)

// Score is the deterministic result of measuring one frozen acquisition against
// one frozen reference set.
type Score struct {
	SchemaVersion string `json:"schema_version"`

	AcquisitionDigestSHA256 string `json:"acquisition_digest_sha256"`
	ReferenceDigestSHA256   string `json:"reference_digest_sha256,omitempty"`

	// ModelStatus is copied from the acquisition, never recomputed. The scorer
	// has no opinion about whether a model ran.
	ModelStatus string `json:"model_status"`

	// Scored reports the typed state of the measurement itself. A score is only
	// produced when there is an answer key to produce it against.
	Scored bool   `json:"scored"`
	Reason string `json:"reason,omitempty"`

	// Deterministic and model-derived counts stay SEPARATE. Merging them and
	// scoring the union would attribute the deterministic lane's work to the
	// model lane, which is the specific dishonesty the acquisition split exists
	// to prevent.
	DeterministicObservations int            `json:"deterministic_observations"`
	DeterministicCandidates   int            `json:"deterministic_candidates"`
	ModelItemsByKind          map[string]int `json:"model_items_by_kind,omitempty"`

	ModelSupported   int `json:"model_supported,omitempty"`
	ModelUnsupported int `json:"model_unsupported,omitempty"`
	ModelAmbiguous   int `json:"model_ambiguous,omitempty"`
	ModelUnlabelled  int `json:"model_unlabelled,omitempty"`
}

// Typed reasons a measurement produced no score.
const (
	ReasonReferenceSetAbsent     = "reference_set_absent"
	ReasonModelDidNotResolve     = "model_did_not_resolve"
	ReasonAcquisitionAltered     = "acquisition_contents_do_not_match_its_digest"
	ReasonReferenceSetAltered    = "reference_set_contents_do_not_match_its_digest"
	ReasonReferenceSetUnfrozen   = "reference_set_carries_labels_but_no_frozen_identity"
	ReasonReferenceSetConflicted = "reference_set_labels_the_same_item_more_than_once"
)

// ScoreAcquisition measures a FROZEN bundle against a FROZEN reference set.
//
// It must be byte-identical on replay for the same two inputs. Nothing here
// reads a clock, a filesystem, a network, or a map in iteration order.
func ScoreAcquisition(a Acquisition, ref ReferenceSet) Score {
	s := Score{
		SchemaVersion:             ScoreSchemaVersion,
		AcquisitionDigestSHA256:   a.AcquisitionDigestSHA256,
		ReferenceDigestSHA256:     ref.DigestSHA256,
		ModelStatus:               a.Model.Status,
		DeterministicObservations: a.Baseline.ObservationCount,
		DeterministicCandidates:   a.Baseline.CandidateCount,
	}
	if len(a.Items) > 0 {
		s.ModelItemsByKind = map[string]int{}
		for _, item := range a.Items {
			s.ModelItemsByKind[item.Kind]++
		}
	}

	// A digest stored inside a frozen file is a CLAIM that file makes about
	// itself. Both are recomputed from the contents actually loaded, because a
	// file edited without refreshing its digest would otherwise be scored while
	// the score named the identity it no longer has — which is exactly how a
	// moved answer key would go undetected. This is the same lesson already
	// recorded against the verification-record path, in a new place.
	if a.AcquisitionDigestSHA256 != acquisitionDigest(a) {
		s.Reason = ReasonAcquisitionAltered
		return s
	}
	if a.Model.Status != investigation.ModelStatusResolved {
		// A refusal or an error is a real evaluation result. It is reported as
		// itself, never as a zero score that would read like a bad model.
		s.Reason = ReasonModelDidNotResolve
		return s
	}

	// Ordered AFTER the model outcome on purpose: what the run did is a fact
	// about the run, and a refusal must be reported as itself whatever state
	// the ruler is in.
	//
	// A reference set that carries labels MUST carry its own frozen identity.
	// Validating the digest only when one happens to be present let an
	// unfrozen answer key score: labels with an empty digest would skip the
	// check here and then satisfy the "labels exist" test below, producing
	// Scored=true against a ruler nothing pins. An answer key with no identity
	// is not frozen, and an unfrozen ruler cannot support a defensible score.
	if len(ref.Labels) > 0 && strings.TrimSpace(ref.DigestSHA256) == "" {
		s.Reason = ReasonReferenceSetUnfrozen
		return s
	}
	if ref.DigestSHA256 != "" && ref.DigestSHA256 != ReferenceDigest(ref) {
		s.Reason = ReasonReferenceSetAltered
		return s
	}

	if len(ref.Labels) == 0 {
		// No answer key means no score. Producing one anyway — by inferring
		// correctness from the system's own output — is exactly what the
		// protocol forbids.
		s.Reason = ReasonReferenceSetAbsent
		return s
	}

	// Duplicate keys invalidate the set rather than silently resolving to the
	// last one. Combined label files are exactly where two adjudicators'
	// conflicting verdicts on one item meet, and the protocol requires that
	// disagreement to stay explicit: overwriting it would erase the very
	// signal the overlap sample exists to produce.
	labels := map[string]string{}
	for _, l := range ref.Labels {
		if _, seen := labels[l.ItemKey]; seen {
			s.Reason = ReasonReferenceSetConflicted
			return s
		}
		labels[l.ItemKey] = l.Verdict
	}
	for _, item := range a.Items {
		switch labels[ItemKey(item)] {
		case VerdictSupported:
			s.ModelSupported++
		case VerdictUnsupported:
			s.ModelUnsupported++
		case VerdictAmbiguous:
			s.ModelAmbiguous++
		case VerdictOutsideScope:
			// Deliberately counted nowhere: an item the protocol placed outside
			// scope must not silently become a miss.
		default:
			s.ModelUnlabelled++
		}
	}
	s.Scored = true
	return s
}

// ItemKey is the stable identity of one acquired item, so a human label written
// against a frozen sample keeps pointing at the same item.
func ItemKey(item AcquiredItem) string {
	// FilePaths are part of the identity: two items with the same words that
	// attribute the finding to different files are different claims, and a
	// human label written for one must not migrate to the other.
	payload, _ := json.Marshal(struct {
		Kind  string   `json:"kind"`
		Text  string   `json:"text"`
		Cited []string `json:"cited"`
		Files []string `json:"files"`
	}{item.Kind, item.Text, sortedCopy(item.CitedEvidenceIDs), sortedCopy(item.FilePaths)})
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])[:32]
}

// ReferenceDigest content-addresses an answer key, so a score can name the
// exact ruler it used and a later edit is visible.
func ReferenceDigest(ref ReferenceSet) string {
	ref.DigestSHA256 = ""
	sort.SliceStable(ref.Labels, func(i, j int) bool {
		if ref.Labels[i].ItemKey != ref.Labels[j].ItemKey {
			return ref.Labels[i].ItemKey < ref.Labels[j].ItemKey
		}
		return ref.Labels[i].Verdict < ref.Labels[j].Verdict
	})
	ref.LabelFileDigestsSHA256 = sortedCopy(ref.LabelFileDigestsSHA256)
	ref.WorldBindingDigestsSHA256 = sortedCopy(ref.WorldBindingDigestsSHA256)
	data, _ := json.Marshal(ref)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
