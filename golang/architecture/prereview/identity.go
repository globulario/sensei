// SPDX-License-Identifier: Apache-2.0

package prereview

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// canonicalJSON encodes v deterministically: struct field order is fixed and
// the model contains no map fields, so the output is stable across runs. HTML
// escaping is disabled and the trailing newline the encoder appends is trimmed
// so the bytes are a stable digest input.
func canonicalJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// bindingIdentity is the identity-bearing projection of a review binding. It
// deliberately excludes display metadata (PR number, URL, branch, checkout
// path) so those never affect a report's identity.
type bindingIdentity struct {
	SchemaVersion           string   `json:"schema_version"`
	RepositoryDomain        string   `json:"repository_domain"`
	BaseRevision            string   `json:"base_revision"`
	BaseTreeDigestSHA256    string   `json:"base_tree_digest_sha256"`
	HeadRevision            string   `json:"head_revision"`
	HeadTreeDigestSHA256    string   `json:"head_tree_digest_sha256"`
	DiffDigestSHA256        string   `json:"diff_digest_sha256"`
	TaskID                  string   `json:"task_id"`
	LedgerHeadDigestSHA256  string   `json:"ledger_head_digest_sha256"`
	BaseGraphDigestSHA256   string   `json:"base_graph_digest_sha256"`
	ResultGraphDigestSHA256 string   `json:"result_graph_digest_sha256"`
	PolicyIDs               []string `json:"policy_ids"`
}

// ComputeReportID derives the stable report identifier from the binding. Two
// reports over the same repository, diff, ledger head, graphs, and policies
// share a report ID regardless of display metadata or render time.
func ComputeReportID(b ReviewBinding) (string, error) {
	id := bindingIdentity{
		SchemaVersion:           SchemaVersion,
		RepositoryDomain:        b.RepositoryDomain,
		BaseRevision:            b.BaseRevision,
		BaseTreeDigestSHA256:    b.BaseTreeDigestSHA256,
		HeadRevision:            b.HeadRevision,
		HeadTreeDigestSHA256:    b.HeadTreeDigestSHA256,
		DiffDigestSHA256:        b.DiffDigestSHA256,
		TaskID:                  b.TaskID,
		LedgerHeadDigestSHA256:  b.LedgerHeadDigestSHA256,
		BaseGraphDigestSHA256:   b.BaseGraphDigestSHA256,
		ResultGraphDigestSHA256: b.ResultGraphDigestSHA256,
		PolicyIDs:               sortedUnique(b.PolicyIDs),
	}
	raw, err := canonicalJSON(id)
	if err != nil {
		return "", err
	}
	return "prereview." + sha256Hex(raw)[:24], nil
}

// ComputeReportDigest computes the canonical semantic digest of a report. The
// digest excludes display metadata, the optional narrative, and the digest
// field itself, so it is invariant to presentation and stable across runs.
func ComputeReportDigest(r PreReviewReport) (string, error) {
	raw, err := semanticBytes(r)
	if err != nil {
		return "", err
	}
	return sha256Hex(raw), nil
}

// semanticBytes returns the canonical bytes a report's identity is computed
// over: display metadata, narrative, and the stored digest are cleared first.
func semanticBytes(r PreReviewReport) ([]byte, error) {
	c := r
	c.Display = nil
	c.Narrative = nil
	c.ReportDigestSHA256 = ""
	return canonicalJSON(c)
}
