// SPDX-License-Identifier: Apache-2.0

package closureprotocol

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
)

var setLikeKeys = map[string]bool{
	"roles": true, "authority_domain_ids": true, "grant_ids": true, "delegation_ids": true,
	"evidence_receipt_ids": true, "artifact_digests": true, "reason_codes": true, "limitations": true,
	"conflicts_with": true, "waiver_digests": true, "proof_discharge_digests": true,
	"required_proof_slots": true, "required_evidence_profiles": true, "required_result_rebuilds": true,
	"consumed_operation_ids": true, "triggering_evidence": true, "applies_to": true,
	"legal_mechanisms": true, "mapped_evidence": true, "missing_slots": true,
	"incompatible_receipts": true, "forbidden_moves": true, "unresolved_contradictions": true,
	"revocation_conditions": true,
}

var omitIfEmptyKeys = map[string]bool{
	"path": true,
}

func CanonicalJSON(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		return nil, err
	}
	canonical := normalizeValue("", value)
	return json.Marshal(canonical)
}

func SemanticDigest(v any) (string, error) {
	b, err := CanonicalJSON(v)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

func normalizeValue(key string, value any) any {
	switch v := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out := make(map[string]any, len(keys))
		for _, k := range keys {
			norm := normalizeValue(k, v[k])
			if shouldOmit(k, norm) {
				continue
			}
			out[k] = norm
		}
		return out
	case []any:
		out := make([]any, 0, len(v))
		for _, item := range v {
			out = append(out, normalizeValue("", item))
		}
		if setLikeKeys[key] {
			out = dedupeAndSortAny(out)
		}
		return out
	default:
		return v
	}
}

func shouldOmit(key string, value any) bool {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v) == ""
	case []any:
		return len(v) == 0
	case map[string]any:
		return len(v) == 0
	default:
		return false
	}
}

func dedupeAndSortAny(items []any) []any {
	type pair struct {
		key string
		val any
	}
	seen := map[string]pair{}
	for _, item := range items {
		b, _ := json.Marshal(item)
		seen[string(b)] = pair{key: string(b), val: item}
	}
	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]any, 0, len(keys))
	for _, k := range keys {
		out = append(out, seen[k].val)
	}
	return out
}

func CompletionReceiptDigest(in CompletionReceipt) (string, error) {
	copy := in
	copy.ReceiptDigestSHA256 = ""
	return SemanticDigest(copy)
}

func CertificationReceiptDigest(in CertificationReceipt) (string, error) {
	copy := in
	copy.DigestSHA256 = ""
	return SemanticDigest(copy)
}

func AuthorityResolutionDigest(in AuthorityResolution) (string, error) {
	copy := in
	copy.ResolutionDigestSHA256 = ""
	return SemanticDigest(copy)
}

func ProofDischargeDigest(in ProofDischarge) (string, error) {
	copy := in
	copy.DischargeDigestSHA256 = ""
	return SemanticDigest(copy)
}

func LedgerEntryDigest(in LedgerEntry) (string, error) {
	copy := in
	copy.EntryDigestSHA256 = ""
	return SemanticDigest(copy)
}

func NormalizeSet(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

func NormalizeOrdered(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func MustSemanticDigest(v any) string {
	d, err := SemanticDigest(v)
	if err != nil {
		panic(fmt.Sprintf("semantic digest failed: %v", err))
	}
	return d
}
