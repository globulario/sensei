// SPDX-License-Identifier: AGPL-3.0-only

package investigator

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/globulario/sensei/golang/architecture"
)

// ReceiptSemanticDigests contains the exact normalized semantic indexes that a
// run receipt must bind independently of the outer result digest.
type ReceiptSemanticDigests struct {
	CandidateIDsAndDigests       map[string]string
	ChallengeIDsAndDigests       map[string]string
	CounterexampleIDsAndDigests  map[string]string
	EvidenceRequestIDsAndDigests map[string]string
	RankingDigestSHA256          string
}

type candidateSemanticRecord struct {
	Envelope CandidateEnvelope  `json:"envelope"`
	Claim    architecture.Claim `json:"claim"`
}

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

// ComputeReceiptSemanticDigests derives exact semantic indexes from the
// normalized result. Receipt fields are cleared before normalization so the
// indexes cannot depend on, or recursively certify, their own representation.
func ComputeReceiptSemanticDigests(res Result) (ReceiptSemanticDigests, error) {
	payload := res
	payload.Receipt = RunReceipt{}

	normalized, err := Normalize(payload)
	if err != nil {
		return ReceiptSemanticDigests{}, fmt.Errorf("normalize receipt semantic payload failed: %w", err)
	}

	claimsByID := make(map[string]architecture.Claim, len(normalized.Document.CandidateClaims))
	for _, claim := range normalized.Document.CandidateClaims {
		if claim.ID == "" {
			return ReceiptSemanticDigests{}, errors.New("candidate claim ID is required for semantic receipt indexing")
		}
		if _, exists := claimsByID[claim.ID]; exists {
			return ReceiptSemanticDigests{}, fmt.Errorf("duplicate candidate claim ID %q in semantic receipt payload", claim.ID)
		}
		claimsByID[claim.ID] = claim
	}

	out := ReceiptSemanticDigests{
		CandidateIDsAndDigests:       make(map[string]string, len(normalized.Candidates)),
		ChallengeIDsAndDigests:       make(map[string]string, len(normalized.Challenges)),
		CounterexampleIDsAndDigests:  make(map[string]string, len(normalized.Counterexamples)),
		EvidenceRequestIDsAndDigests: make(map[string]string, len(normalized.EvidenceRequests)),
	}

	for _, envelope := range normalized.Candidates {
		claim, ok := claimsByID[envelope.ClaimID]
		if !ok {
			return ReceiptSemanticDigests{}, fmt.Errorf("candidate %q references missing claim %q", envelope.CandidateID, envelope.ClaimID)
		}
		if err := addSemanticDigest(out.CandidateIDsAndDigests, envelope.CandidateID, candidateSemanticRecord{
			Envelope: envelope,
			Claim:    claim,
		}); err != nil {
			return ReceiptSemanticDigests{}, fmt.Errorf("candidate semantic index: %w", err)
		}
	}

	for _, challenge := range normalized.Challenges {
		if err := addSemanticDigest(out.ChallengeIDsAndDigests, challenge.ID, challenge); err != nil {
			return ReceiptSemanticDigests{}, fmt.Errorf("challenge semantic index: %w", err)
		}
	}

	for _, counterexample := range normalized.Counterexamples {
		if err := addSemanticDigest(out.CounterexampleIDsAndDigests, counterexample.Counterexample.ID, counterexample); err != nil {
			return ReceiptSemanticDigests{}, fmt.Errorf("counterexample semantic index: %w", err)
		}
	}

	for _, request := range normalized.EvidenceRequests {
		if err := addSemanticDigest(out.EvidenceRequestIDsAndDigests, request.ID, request); err != nil {
			return ReceiptSemanticDigests{}, fmt.Errorf("evidence request semantic index: %w", err)
		}
	}

	out.RankingDigestSHA256, err = semanticDigest(normalized.Rankings)
	if err != nil {
		return ReceiptSemanticDigests{}, fmt.Errorf("ranking semantic digest: %w", err)
	}

	return out, nil
}

// ResultDigest calculates the SHA256 of the Result document while enforcing
// the complete receipt semantic spine. ExactResultDigestSHA256 is ignored to
// avoid a cyclic digest dependency.
func ResultDigest(res Result) (string, error) {
	expected, err := ComputeReceiptSemanticDigests(res)
	if err != nil {
		return "", err
	}
	if err := validateReceiptSemanticSpine(res, expected); err != nil {
		return "", err
	}

	res.Receipt.ExactResultDigestSHA256 = ""

	bytes, err := json.Marshal(res)
	if err != nil {
		return "", fmt.Errorf("marshal result failed: %w", err)
	}

	return SHA256Bytes(bytes), nil
}

func addSemanticDigest(index map[string]string, id string, value any) error {
	if strings.TrimSpace(id) == "" {
		return errors.New("semantic index ID is required")
	}
	if _, exists := index[id]; exists {
		return fmt.Errorf("duplicate semantic index ID %q", id)
	}

	digest, err := semanticDigest(value)
	if err != nil {
		return err
	}
	index[id] = digest
	return nil
}

func semanticDigest(value any) (string, error) {
	bytes, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("marshal semantic value failed: %w", err)
	}
	return SHA256Bytes(bytes), nil
}

func validateReceiptSemanticSpine(res Result, expected ReceiptSemanticDigests) error {
	var errs []string
	receipt := res.Receipt

	for _, comparison := range []struct {
		name string
		got  string
		want string
	}{
		{name: "schema version", got: receipt.SchemaVersion, want: res.SchemaVersion},
		{name: "generated-by identity", got: receipt.GeneratedBy, want: res.GeneratedBy},
		{name: "generator version", got: receipt.GeneratorVersion, want: res.Binding.GeneratorVersion},
		{name: "ruleset version", got: receipt.RulesetVersion, want: res.Binding.RulesetVersion},
	} {
		if comparison.got == "" || comparison.got != comparison.want {
			errs = append(errs, fmt.Sprintf("receipt %s %q must exactly match %q", comparison.name, comparison.got, comparison.want))
		}
	}

	errs = append(errs, compareDigestIndex("candidate", receipt.CandidateIDsAndDigests, expected.CandidateIDsAndDigests)...)
	errs = append(errs, compareDigestIndex("challenge", receipt.ChallengeIDsAndDigests, expected.ChallengeIDsAndDigests)...)
	errs = append(errs, compareDigestIndex("counterexample", receipt.CounterexampleIDsAndDigests, expected.CounterexampleIDsAndDigests)...)
	errs = append(errs, compareDigestIndex("evidence request", receipt.EvidenceRequestIDsAndDigests, expected.EvidenceRequestIDsAndDigests)...)

	if receipt.RankingDigestSHA256 == "" || receipt.RankingDigestSHA256 != expected.RankingDigestSHA256 {
		errs = append(errs, fmt.Sprintf("receipt ranking digest %q must exactly match %q", receipt.RankingDigestSHA256, expected.RankingDigestSHA256))
	}

	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func compareDigestIndex(name string, got, want map[string]string) []string {
	if got == nil {
		return []string{fmt.Sprintf("receipt %s semantic index is required", name)}
	}

	var errs []string
	wantKeys := sortedMapKeys(want)
	for _, id := range wantKeys {
		gotDigest, exists := got[id]
		if !exists {
			errs = append(errs, fmt.Sprintf("receipt %s semantic index is missing %q", name, id))
			continue
		}
		if gotDigest != want[id] {
			errs = append(errs, fmt.Sprintf("receipt %s semantic digest for %q does not match", name, id))
		}
	}

	for _, id := range sortedMapKeys(got) {
		if _, exists := want[id]; !exists {
			errs = append(errs, fmt.Sprintf("receipt %s semantic index has unexpected %q", name, id))
		}
	}
	return errs
}

func sortedMapKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
