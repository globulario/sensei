// SPDX-License-Identifier: AGPL-3.0-only

package synthesisdriver

import (
	"errors"
	"fmt"
	"strings"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/synthesis"
)

func NormalizeRunReceipt(receipt RunReceipt) RunReceipt {
	if receipt.O2ReceiptDigestsSHA256 == nil {
		receipt.O2ReceiptDigestsSHA256 = []string{}
	}
	if receipt.RunnerReceiptDigestsSHA256 == nil {
		receipt.RunnerReceiptDigestsSHA256 = []string{}
	}
	if receipt.EvaluationReceiptDigestsSHA256 == nil {
		receipt.EvaluationReceiptDigestsSHA256 = []string{}
	}
	return receipt
}

// RunReceiptDigest excludes observation timestamps and the self field. All
// authority and evidence references remain part of identity.
func RunReceiptDigest(receipt RunReceipt) (string, error) {
	receipt = NormalizeRunReceipt(receipt)
	receipt.StartedAt = ""
	receipt.CompletedAt = ""
	receipt.ReceiptDigestSHA256 = ""
	return closureprotocol.SemanticDigest(receipt)
}

func finalizeRunReceipt(receipt RunReceipt) (RunReceipt, error) {
	receipt = NormalizeRunReceipt(receipt)
	digest, err := RunReceiptDigest(receipt)
	if err != nil {
		return RunReceipt{}, err
	}
	receipt.ReceiptDigestSHA256 = digest
	return receipt, ValidateRunReceipt(receipt)
}

func ValidateRunReceipt(receipt RunReceipt) error {
	receipt = NormalizeRunReceipt(receipt)
	if receipt.SchemaVersion != RunReceiptSchemaVersion {
		return fmt.Errorf("synthesisdriver: receipt schema_version %q", receipt.SchemaVersion)
	}
	if strings.TrimSpace(receipt.ReceiptID) == "" || receipt.GeneratedBy != GeneratedBy {
		return errors.New("synthesisdriver: receipt identity is incomplete")
	}
	if !isSHA256(receipt.SessionDigestSHA256) || !isSHA256(receipt.ReceiptDigestSHA256) {
		return errors.New("synthesisdriver: receipt session or self digest is invalid")
	}
	if receipt.StepCount < 0 {
		return errors.New("synthesisdriver: receipt step_count is negative")
	}
	if _, err := synthesis.ParsePhase(receipt.FinalPhase); err != nil {
		return fmt.Errorf("synthesisdriver: receipt final_phase: %w", err)
	}
	for _, digests := range [][]string{
		receipt.O2ReceiptDigestsSHA256,
		receipt.RunnerReceiptDigestsSHA256,
		receipt.EvaluationReceiptDigestsSHA256,
	} {
		for _, digest := range digests {
			if !isSHA256(digest) {
				return fmt.Errorf("synthesisdriver: invalid evidence digest %q", digest)
			}
	}
	}
	if receipt.SynthesisReceiptDigestSHA256 != nil && !isSHA256(*receipt.SynthesisReceiptDigestSHA256) {
		return errors.New("synthesisdriver: invalid synthesis receipt digest")
	}
	if receipt.CandidateArtifactDigestSHA256 != nil && !isSHA256(*receipt.CandidateArtifactDigestSHA256) {
		return errors.New("synthesisdriver: invalid candidate artifact digest")
	}
	switch receipt.Disposition {
	case DispositionCandidateReady:
		if receipt.FinalPhase != string(synthesis.PhaseSucceeded) || receipt.SynthesisReceiptDigestSHA256 == nil || receipt.CandidateArtifactDigestSHA256 == nil {
			return errors.New("synthesisdriver: candidate-ready receipt lacks succeeded O1/candidate identity")
		}
	case DispositionTerminalFailure:
		if receipt.FinalPhase != string(synthesis.PhaseFailed) || receipt.SynthesisReceiptDigestSHA256 == nil {
			return errors.New("synthesisdriver: terminal-failure receipt lacks failed O1 identity")
		}
	case DispositionProviderStopped, DispositionRunnerStopped, DispositionStepLimitReached:
		if receipt.SynthesisReceiptDigestSHA256 != nil {
			return errors.New("synthesisdriver: nonterminal stop cannot invent an O1 terminal receipt")
		}
	default:
		return fmt.Errorf("synthesisdriver: unknown disposition %q", receipt.Disposition)
	}
	computed, err := RunReceiptDigest(receipt)
	if err != nil {
		return err
	}
	if computed != receipt.ReceiptDigestSHA256 {
		return fmt.Errorf("synthesisdriver: receipt declares digest %q but computed %q", receipt.ReceiptDigestSHA256, computed)
	}
	return nil
}

func isSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, r := range value {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}
