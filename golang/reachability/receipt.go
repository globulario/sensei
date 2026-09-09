// SPDX-License-Identifier: AGPL-3.0-only

package reachability

import (
	"encoding/json"
	"github.com/globulario/sensei/golang/gitobject"
	"os"
	"path/filepath"
	"strings"
)

// ReceiptPath is where a publication receipt sits, beside the graph marker.
const ReceiptPath = ".sensei/graph-publication-receipt.json"

// publicationReceipt is the subset of the receipt this package reads.
type publicationReceipt struct {
	Receipt struct {
		Revision string `json:"Revision"`
		Domain   string `json:"Domain"`
		State    string `json:"State"`
	} `json:"receipt"`
	GraphGeneration string `json:"graph_generation"`
}

// PublishedCorpusRevision resolves the CORPUS revision the live generation was
// built from, and it is the value this package's comparison needs.
//
// WHY NOT GraphBuildCommit. That field is the awareness-graph BINARY's own vcs
// revision — a commit in the tool's repository. Measured 2026-09-08 on two live
// instances, it equalled `go version -m <serving binary>` exactly in both:
//
//	governed repo != tool repo   the two commits are from different
//	                             repositories and can never be ordered, so the
//	                             verdict was permanently Unknown
//	governed repo == tool repo   they order, and the check reported a specific
//	                             count that was the distance from the BINARY to
//	                             the corpus. Rebuilding the store from a
//	                             CLEAN_EXACT corpus left the verdict
//	                             byte-identical; rebuilding the BINARY alone
//	                             would have reported `current` with the store
//	                             untouched
//
// The second is the direction that fails open, and it is the failure named in
// this package's own header: an artifact always matches itself. Replacing the
// marker with the binary's revision did not fix that, it moved it.
//
// The receipt is the honest source. `sensei build` writes it beside the marker
// when it loads a generation, and it records the revision of the corpus that
// was compiled — a commit in the GOVERNED repository, the same history the
// admitted base is resolved from.
//
// GENERATION-BOUND, and that binding is the whole guard. A receipt describes
// one generation; if the live store has since been replaced, the receipt
// describes something else and must not be read as describing what is being
// served. Mismatch resolves NOTHING rather than the stale value, so the caller
// reports Unknown — "not recorded" — instead of a confident wrong answer.
func PublishedCorpusRevision(repoRoot, liveGeneration string) (string, bool) {
	liveGeneration = strings.TrimSpace(liveGeneration)
	if strings.TrimSpace(repoRoot) == "" || liveGeneration == "" {
		return "", false
	}
	blob, err := os.ReadFile(filepath.Join(repoRoot, ReceiptPath))
	if err != nil {
		return "", false
	}
	var r publicationReceipt
	if err := json.Unmarshal(blob, &r); err != nil {
		return "", false
	}
	// The receipt must describe the generation actually being served.
	if !strings.EqualFold(strings.TrimSpace(r.GraphGeneration), liveGeneration) {
		return "", false
	}
	// VALIDATE, then normalize. gitobject.IsObjectID accepts both of Git's
	// object formats, so a SHA-256 repository's receipt is readable here. This
	// accepted only 40 hex before, which rejected every id such a repository
	// produces and resolved nothing -- a FALSE Unknown, in the mechanism built
	// to stop false verdicts.
	rev := strings.TrimSpace(r.Receipt.Revision)
	if !gitobject.IsObjectID(rev) {
		return "", false
	}
	return strings.ToLower(rev), true
}

// MarkerPath is the verified live graph identity written after a store load.
const MarkerPath = ".sensei/graph-authority.json"

type graphMarker struct {
	DigestSHA256 string `json:"digest_sha256"`
}

// PublishedCorpusRevisionFromRepo resolves the published corpus revision using
// only the checkout, by binding the receipt to the marker written beside it.
//
// Both files are written by the same successful store load, so requiring them
// to agree is what makes a LEFTOVER receipt unusable: a receipt describing a
// generation the marker no longer names resolves nothing, and the caller
// reports Unknown rather than a confident revision for a store that has since
// been replaced.
//
// This deliberately takes no value from the served message. The one field the
// server offers for this — graph_build_commit — is the tool binary's revision,
// which is the defect being repaired; reading it here would reintroduce it.
func PublishedCorpusRevisionFromRepo(repoRoot string) (string, bool) {
	if strings.TrimSpace(repoRoot) == "" {
		return "", false
	}
	blob, err := os.ReadFile(filepath.Join(repoRoot, MarkerPath))
	if err != nil {
		return "", false
	}
	var m graphMarker
	if err := json.Unmarshal(blob, &m); err != nil {
		return "", false
	}
	return PublishedCorpusRevision(repoRoot, m.DigestSHA256)
}
