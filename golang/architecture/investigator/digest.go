// SPDX-License-Identifier: AGPL-3.0-only

package investigator

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
)

// SHA256String returns the SHA256 hex string of a string.
func SHA256String(s string) string {
	return SHA256Bytes([]byte(s))
}

// SHA256Bytes returns the SHA256 hex string of bytes.
func SHA256Bytes(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// GroundingSnapshotDigest normalizes, sorts, and hashes the GroundingSnapshot.
func GroundingSnapshotDigest(snap GroundingSnapshot) (string, error) {
	// Normalize/sort lists to ensure order independence
	files := append([]string(nil), snap.Files...)
	sort.Strings(files)
	symbols := append([]string(nil), snap.Symbols...)
	sort.Strings(symbols)
	nodes := append([]string(nil), snap.GraphNodeIDs...)
	sort.Strings(nodes)
	claims := append([]string(nil), snap.ClaimIDs...)
	sort.Strings(claims)
	obs := append([]string(nil), snap.ObservationIDs...)
	sort.Strings(obs)
	evs := append([]string(nil), snap.EvidenceReceiptIDs...)
	sort.Strings(evs)
	questions := append([]string(nil), snap.ExistingQuestionIDs...)
	sort.Strings(questions)

	normalized := GroundingSnapshot{
		Files:               files,
		Symbols:             symbols,
		GraphNodeIDs:        nodes,
		ClaimIDs:            claims,
		ObservationIDs:      obs,
		EvidenceReceiptIDs:  evs,
		ExistingQuestionIDs: questions,
	}

	bytes, err := json.Marshal(normalized)
	if err != nil {
		return "", fmt.Errorf("marshal grounding snapshot failed: %w", err)
	}

	return SHA256Bytes(bytes), nil
}

// ResultDigest calculates the SHA256 of the Result document (ignoring Receipt.ExactResultDigestSHA256).
func ResultDigest(res Result) (string, error) {
	// Zero out the ExactResultDigestSHA256 to avoid cyclic dependency
	res.Receipt.ExactResultDigestSHA256 = ""

	bytes, err := json.Marshal(res)
	if err != nil {
		return "", fmt.Errorf("marshal result failed: %w", err)
	}

	return SHA256Bytes(bytes), nil
}
