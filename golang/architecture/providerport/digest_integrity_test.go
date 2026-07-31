// SPDX-License-Identifier: AGPL-3.0-only

// digest_integrity_test.go proves the O2 digest-integrity property this
// checkpoint establishes for all five governed documents: each fixture's
// own declared digest field equals what its digest function independently
// recomputes (the happy path), and that equality is content-sensitive --
// tampering a fixture's substantive content after its digest was set
// causes the declared and recomputed digests to diverge. This is the raw
// material a later O2 checkpoint's accept path will check once it exists
// (docs/design/provider-neutral-execution-port-o2.md hard law 5, mirroring
// golang/architecture/synthesis's
// invariant.synthesis.declared_digest_must_equal_computed_content_digest);
// this checkpoint proves the property is real and mechanically detectable
// for every document, ahead of that accept path existing.
package providerport

import "testing"

func TestDeclaredDigestEqualsComputedDigestForValidFixtures(t *testing.T) {
	capabilities := fixtureCapabilities(t)
	if got, err := CapabilitiesDigest(capabilities); err != nil || got != capabilities.CapabilitiesDigestSHA256 {
		t.Errorf("capabilities: declared %q, computed %q (err %v)", capabilities.CapabilitiesDigestSHA256, got, err)
	}

	request := fixturePlanningRequest(t, zeroDigest, zeroDigest)
	if got, err := RequestDigest(request); err != nil || got != request.RequestDigestSHA256 {
		t.Errorf("request: declared %q, computed %q (err %v)", request.RequestDigestSHA256, got, err)
	}

	result := fixtureResult(t, request.RequestDigestSHA256, OperationPlanning, OutcomeCompleted, "planning complete")
	if got, err := ResultDigest(result); err != nil || got != result.ResultDigestSHA256 {
		t.Errorf("result: declared %q, computed %q (err %v)", result.ResultDigestSHA256, got, err)
	}

	batch := fixtureObservationBatch(t, request.RequestDigestSHA256)
	if got, err := ObservationBatchDigest(batch); err != nil || got != batch.ObservationBatchDigestSHA256 {
		t.Errorf("observation batch: declared %q, computed %q (err %v)", batch.ObservationBatchDigestSHA256, got, err)
	}

	receipt := fixtureReceiptCompleted(t, request.RequestDigestSHA256, capabilities.CapabilitiesDigestSHA256, result.ResultDigestSHA256, batch.ObservationBatchDigestSHA256)
	if got, err := ReceiptDigest(receipt); err != nil || got != receipt.ReceiptDigestSHA256 {
		t.Errorf("receipt: declared %q, computed %q (err %v)", receipt.ReceiptDigestSHA256, got, err)
	}
}

func TestDeclaredDigestDivergesFromComputedDigestAfterContentTampering(t *testing.T) {
	capabilities := fixtureCapabilities(t)
	tamperedCapabilities := capabilities
	tamperedCapabilities.SupportedOperations = []Operation{OperationInterpretation}
	if got, err := CapabilitiesDigest(tamperedCapabilities); err != nil {
		t.Fatal(err)
	} else if got == capabilities.CapabilitiesDigestSHA256 {
		t.Error("capabilities: tampering supported_operations did not change the computed digest")
	}

	request := fixturePlanningRequest(t, zeroDigest, zeroDigest)
	tamperedRequest := request
	tamperedRequest.DeadlineAt = "2099-12-31T23:59:59Z"
	if got, err := RequestDigest(tamperedRequest); err != nil {
		t.Fatal(err)
	} else if got == request.RequestDigestSHA256 {
		t.Error("request: tampering deadline_at did not change the computed digest")
	}

	result := fixtureResult(t, request.RequestDigestSHA256, OperationPlanning, OutcomeCompleted, "planning complete")
	tamperedResult := result
	tamperedResult.TerminalOutcome = OutcomeCancelled
	if got, err := ResultDigest(tamperedResult); err != nil {
		t.Fatal(err)
	} else if got == result.ResultDigestSHA256 {
		t.Error("result: tampering terminal_outcome did not change the computed digest")
	}

	batch := fixtureObservationBatch(t, request.RequestDigestSHA256)
	tamperedBatch := batch
	tamperedBatch.Observations = append([]Observation{}, batch.Observations...)
	tamperedBatch.Observations[0].Detail = "tampered"
	if got, err := ObservationBatchDigest(tamperedBatch); err != nil {
		t.Fatal(err)
	} else if got == batch.ObservationBatchDigestSHA256 {
		t.Error("observation batch: tampering an observation's detail did not change the computed digest")
	}

	receipt := fixtureReceiptCompleted(t, request.RequestDigestSHA256, capabilities.CapabilitiesDigestSHA256, result.ResultDigestSHA256, batch.ObservationBatchDigestSHA256)
	tamperedReceipt := receipt
	tamperedReceipt.TerminalOutcome = OutcomeCancelled
	if got, err := ReceiptDigest(tamperedReceipt); err != nil {
		t.Fatal(err)
	} else if got == receipt.ReceiptDigestSHA256 {
		t.Error("receipt: tampering terminal_outcome did not change the computed digest")
	}
}
