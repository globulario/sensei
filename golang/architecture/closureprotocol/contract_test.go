// SPDX-License-Identifier: Apache-2.0

package closureprotocol

import (
	"path/filepath"
	"strings"
	"testing"
)

// loadValidTransition loads the canonical valid result-transition fixture and
// returns its embedded receipt.
func loadValidTransition(t *testing.T) ResultTransitionReceipt {
	t.Helper()
	root := filepath.Join("..", "..", "..", "docs", "fixtures", "architectural-closure", "v1")
	fix := loadFixture(t, filepath.Join(root, "result-transition", "bundle.yaml"))
	if fix.Records.ResultTransitionReceipt == nil {
		t.Fatal("valid result-transition fixture has no receipt")
	}
	return *fix.Records.ResultTransitionReceipt
}

func contractOf(r ResultTransitionReceipt) ResultPipelineContract {
	return ResultPipelineContract{
		BaseBindingDigestSHA256:       r.BaseBindingDigestSHA256,
		ObservedChangeSetDigestSHA256: r.ObservedChangeSetDigestSHA256,
		ResultBinding:                 r.ResultBinding,
		ResultBindingDigestSHA256:     r.ResultBindingDigestSHA256,
		OperationalArtifactReceipts:   r.OperationalArtifactReceipts,
		Derivations:                   r.Derivations,
	}
}

// The shared contract validator accepts exactly the topology a valid transition
// receipt carries — proving the extracted entry point is usable stand-alone, not
// only via the receipt validator.
func TestValidateResultPipelineContractAcceptsValid(t *testing.T) {
	if err := ValidateResultPipelineContract(contractOf(loadValidTransition(t))); err != nil {
		t.Fatalf("valid contract rejected: %v", err)
	}
}

func TestValidateResultPipelineContractRejectsForgedBindingDigest(t *testing.T) {
	c := contractOf(loadValidTransition(t))
	c.ResultBindingDigestSHA256 = strings.Repeat("a", 64)
	if err := ValidateResultPipelineContract(c); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("expected binding-digest mismatch, got %v", err)
	}
}

func TestValidateResultPipelineContractRejectsCrossResultArtifact(t *testing.T) {
	c := contractOf(loadValidTransition(t))
	if len(c.OperationalArtifactReceipts) == 0 {
		t.Skip("fixture carries no operational artifacts")
	}
	dup := append([]ArtifactReceipt(nil), c.OperationalArtifactReceipts...)
	dup[0].ResultBindingDigestSHA256 = strings.Repeat("b", 64)
	c.OperationalArtifactReceipts = dup
	if err := ValidateResultPipelineContract(c); err == nil || !strings.Contains(err.Error(), "different result binding") {
		t.Fatalf("expected cross-result rejection, got %v", err)
	}
}
