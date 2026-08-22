// SPDX-License-Identifier: AGPL-3.0-only

// Package prospective builds the frozen sample of
// docs/evaluation/prospective-recall-protocol-v1.md — the steps between
// "protocol merged" and "human applicability labels frozen", bounded by
// section 4 of docs/design/prospective-recall-harness-259.md.
//
// It decides WHICH proposed changes a human will adjudicate, and what they may
// see while deciding. It never decides which known law was applicable, and it
// deliberately holds no applicability vocabulary at all — not even an unused
// one. The protocol's freeze order puts this artifact before labels exist, and
// a generator able to express an answer is a generator able to leak one.
//
// The separation is the whole reason the package exists. Sensei is the system
// being graded on prospective recall, so if Sensei also chose which changes to
// grade, or told the adjudicator which items govern them, the measurement would
// be an opinion about its own output. Selection here is a stable hash over a
// committed seed and a change identity. Nothing about what Sensei surfaced, or
// would surface, can move a change into or out of the sample.
//
// Retrieval is not executed here. Slice 1 records WHICH production surface the
// measurement will use (section 3.1 of the design) so the decision is frozen
// before any score exists; running it belongs to Slice 2, after the labels.
package prospective

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// SchemaVersion identifies the manifest shape. A reader that does not know
// this version must refuse the file rather than interpret fields positionally.
const SchemaVersion = "sensei.prospective_sample_manifest.v1"

// The protocol's strata (section 5). A and B are separate constants and stay
// separate through every function in this package: collapsing them would hide
// the single most actionable distinction the experiment can produce — whether
// the problem is creation-time context or missing file anchors generally.
const (
	// StratumA is a change whose touched paths did not exist before it. The
	// graph holds no facts about them because there was nothing to hold facts
	// about.
	StratumA = "A_new_seam"
	// StratumB is a change to files that already existed and for which the
	// graph still resolves no usable anchors.
	StratumB = "B_unanchored_existing"
	// StratumC is a change to files the graph already holds usable facts about.
	StratumC = "C_anchored"
	// StratumD is one change touching both anchored material and a new or
	// unanchored seam.
	StratumD = "D_mixed"
)

// Strata is the closed, ordered vocabulary. Reports iterate this rather than a
// map, so a stratum can never go missing from a report by being absent from
// the data — an empty stratum is reported as empty.
var Strata = []string{StratumA, StratumB, StratumC, StratumD}

// DefaultTargetPerStratum is section 8.2's per-stratum target for v1.
const DefaultTargetPerStratum = 12

// Stratum statuses. An empty stratum is REPORTED with a reason rather than
// omitted: a stratum that vanishes from the manifest reads as one nobody
// needed, and a denominator cannot tell that from a population that was zero.
const (
	StatusSampled    = "sampled"
	StatusSampledAll = "sampled_all_available"
	StatusAbsent     = "population_empty"
)

// WorldBinding is the exact repository state a change was drawn from.
//
// The local checkout path is deliberately absent: it is not what was measured,
// and it makes two runs of the same world look like different experiments.
type WorldBinding struct {
	World            string `json:"world"`
	RepositoryDomain string `json:"repository_domain"`
	// Revision is the pinned world revision. Section 3.2 of the design makes
	// this an identity rather than a label: a run whose checkout has drifted
	// off it is a different experiment and must refuse rather than report.
	Revision         string `json:"revision"`
	TreeDigestSHA256 string `json:"tree_digest_sha256,omitempty"`
	DigestSHA256     string `json:"binding_digest_sha256"`
}

// Bind computes a world binding's identity.
func Bind(world, domain, revision, treeDigest string) WorldBinding {
	wb := WorldBinding{World: world, RepositoryDomain: domain, Revision: revision, TreeDigestSHA256: treeDigest}
	wb.DigestSHA256 = HashFields("sensei.prospective.world_binding.v1",
		wb.World, wb.RepositoryDomain, wb.Revision, wb.TreeDigestSHA256)
	return wb
}

// VerifyRevision is the fail-closed drift check of design section 3.2.
//
// It exists as a function rather than as a comparison at one call site because
// drift must be refused everywhere a frozen artifact meets a live checkout,
// and a check that lives at one call site is a check the next caller forgets.
// The error names the drift: a run that reported an empty inventory instead
// would look like a world with nothing in it, which is the same output a
// genuinely empty population produces.
func VerifyRevision(wb WorldBinding, checkoutRevision string) error {
	if wb.Revision == "" {
		return fmt.Errorf("world binding pins no revision: prospective recall is a claim about what the graph knew at a moment, so a sample that cannot name that moment cannot be scored")
	}
	if checkoutRevision != wb.Revision {
		return fmt.Errorf("world drift: this sample is bound to %s but the checkout resolves to %s — results may not carry across drift, so this requires a new observation rather than a rerun",
			wb.Revision, checkoutRevision)
	}
	return nil
}

// HashFields hashes a tuple with length prefixes rather than a delimiter.
//
// A separator that can appear inside a field lets two different tuples hash
// identically, and a collision here silently merges two changes into one
// question.
func HashFields(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		fmt.Fprintf(h, "%d:", len(p))
		h.Write([]byte(p))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// DigestOf content-addresses any artifact this package emits.
func DigestOf(v any) (string, error) {
	payload, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("digest: %w", err)
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}
