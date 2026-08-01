// SPDX-License-Identifier: AGPL-3.0-only

package evaluatorcomposition

import (
	"context"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture/providerport"
	"github.com/globulario/sensei/golang/architecture/runnercomposition"
	"github.com/globulario/sensei/golang/architecture/synthesis"
)

const alternateLineageDigest = "2222222222222222222222222222222222222222222222222222222222222222"

// finishTamperedO2Receipt makes an intentionally altered O2 receipt and its
// enclosing O3 RunnerReceipt independently self-consistent. A rejection
// after this helper therefore proves O4 cross-bound the receipt to the exact
// Request/Result values rather than merely checking each document's own
// digest in isolation.
func finishTamperedO2Receipt(t *testing.T, handoff runnercomposition.VerifiedGenerationHandoff) runnercomposition.VerifiedGenerationHandoff {
	t.Helper()

	handoff.O2Receipt = providerport.NormalizeReceipt(handoff.O2Receipt)
	o2Digest, err := providerport.ReceiptDigest(handoff.O2Receipt)
	if err != nil {
		t.Fatal(err)
	}
	handoff.O2Receipt.ReceiptDigestSHA256 = o2Digest

	handoff.RunnerReceipt.O2ReceiptDigestSHA256 = &o2Digest
	handoff.RunnerReceipt = runnercomposition.NormalizeRunnerReceipt(handoff.RunnerReceipt)
	runnerDigest, err := runnercomposition.RunnerReceiptDigest(handoff.RunnerReceipt)
	if err != nil {
		t.Fatal(err)
	}
	handoff.RunnerReceipt.RunnerReceiptDigestSHA256 = runnerDigest

	if err := providerport.ValidateReceiptSchema(mustJSON(t, handoff.O2Receipt)); err != nil {
		t.Fatalf("test fixture bug: tampered O2 receipt is not schema-valid: %v", err)
	}
	if err := runnercomposition.ValidateRunnerReceipt(handoff.RunnerReceipt); err != nil {
		t.Fatalf("test fixture bug: RunnerReceipt no longer validates after rebinding the tampered O2 receipt: %v", err)
	}
	return handoff
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := jsonMarshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// jsonMarshal is a small indirection so the lineage fixtures remain focused
// on their semantic mutation rather than repeating error handling.
var jsonMarshal = func(value any) ([]byte, error) {
	return json.Marshal(value)
}

func TestRunRejectsSelfConsistentO2ReceiptLineageMismatches(t *testing.T) {
	tests := []struct {
		name       string
		mutate     func(*providerport.Receipt)
		wantDetail string
	}{
		{
			name: "request digest",
			mutate: func(receipt *providerport.Receipt) {
				receipt.RequestDigestSHA256 = alternateLineageDigest
			},
			wantDetail: "o2 receipt references request digest",
		},
		{
			name: "result digest",
			mutate: func(receipt *providerport.Receipt) {
				receipt.ResultDigestSHA256 = alternateLineageDigest
			},
			wantDetail: "o2 receipt references result digest",
		},
		{
			name: "terminal outcome",
			mutate: func(receipt *providerport.Receipt) {
				receipt.TerminalOutcome = providerport.OutcomeUnavailable
				receipt.PayloadDigestSHA256 = nil
			},
			wantDetail: "o2 receipt terminal_outcome",
		},
		{
			name: "payload digest",
			mutate: func(receipt *providerport.Receipt) {
				digest := alternateLineageDigest
				receipt.PayloadDigestSHA256 = &digest
			},
			wantDetail: "o2 receipt payload_digest_sha256",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handoff, sessionState, store := verifiedHandoffFixture(t, synthesis.ProviderStatusCompleted)
			policy := fixturePolicyForHandoff(t, sessionState, handoff)

			tampered := handoff
			test.mutate(&tampered.O2Receipt)
			tampered = finishTamperedO2Receipt(t, tampered)

			if _, err := Run(context.Background(), sessionState, tampered, policy, store, runFixedNow); err == nil {
				t.Fatalf("self-consistent O2 receipt with mismatched %s was wrongly accepted", test.name)
			} else if !strings.Contains(err.Error(), test.wantDetail) {
				t.Fatalf("wrong rejection for mismatched %s: %v", test.name, err)
			}
		})
	}
}

func TestRunCandidateLoadFailureCommandBindsO4ReceiptIdentityAndDisposition(t *testing.T) {
	handoff, sessionState, store := verifiedHandoffFixture(t, synthesis.ProviderStatusCompleted)
	policy := fixturePolicyForHandoff(t, sessionState, handoff)

	missingDigest := "1111111111111111111111111111111111111111111111111111111111111111"
	handoff.RunnerReceipt.CandidateArtifactDigestSHA256 = &missingDigest
	handoff.RunnerReceipt = runnercomposition.NormalizeRunnerReceipt(handoff.RunnerReceipt)
	runnerDigest, err := runnercomposition.RunnerReceiptDigest(handoff.RunnerReceipt)
	if err != nil {
		t.Fatal(err)
	}
	handoff.RunnerReceipt.RunnerReceiptDigestSHA256 = runnerDigest

	policy.CandidateArtifactDigestSHA256 = missingDigest
	policy, err = finishPolicyFixture(t, policy)
	if err != nil {
		t.Fatal(err)
	}

	result, err := Run(context.Background(), sessionState, handoff, policy, store, runFixedNow)
	if err != nil {
		t.Fatalf("Run returned an error rather than a governed candidate-load-failure: %v", err)
	}
	if result.Receipt == nil || result.SessionState.Receipt == nil {
		t.Fatal("candidate-load-failure must produce both an O4 receipt and an O1 terminal receipt")
	}

	summary := result.SessionState.Receipt.Summary
	for _, want := range []string{
		"o4_receipt_id=\"" + result.Receipt.ReceiptID + "\"",
		"disposition=\"" + string(DispositionCandidateLoadFailure) + "\"",
		"failure_detail=\"" + result.Receipt.FailureDetail + "\"",
	} {
		if !strings.Contains(summary, want) {
			t.Errorf("O1 terminal summary %q does not bind %q", summary, want)
		}
	}
}
