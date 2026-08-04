// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/globulario/sensei/golang/architecture/evaluatorcomposition"
	"github.com/globulario/sensei/golang/architecture/runnercomposition"
	"github.com/globulario/sensei/golang/architecture/synthesis"
	"github.com/globulario/sensei/golang/architecture/synthesisdriver"
)

// newTestCandidateStore constructs a real runnercomposition.CandidateArtifactStore
// rooted at a fresh temp dir, matching what runSynthesisRun actually
// constructs and passes to persistAdmissionLineage.
func newTestCandidateStore(t *testing.T) (runnercomposition.CandidateArtifactStore, string) {
	t.Helper()
	dir := t.TempDir()
	store, err := runnercomposition.NewFSCandidateArtifactStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	return store, dir
}

// TestPersistAdmissionLineage_NonCandidateReadyReturnsEmpty covers every
// disposition without a sealed candidate: there is nothing to persist, and
// persistAdmissionLineage must not error or write a file.
func TestPersistAdmissionLineage_NonCandidateReadyReturnsEmpty(t *testing.T) {
	store, dir := newTestCandidateStore(t)
	for _, d := range []synthesisdriver.Disposition{
		synthesisdriver.DispositionTerminalFailure,
		synthesisdriver.DispositionProviderStopped,
		synthesisdriver.DispositionRunnerStopped,
		synthesisdriver.DispositionStepLimitReached,
	} {
		result := synthesisdriver.Result{Receipt: synthesisdriver.RunReceipt{Disposition: d}}
		path, err := persistAdmissionLineage(context.Background(), result, store, dir)
		if err != nil {
			t.Fatalf("disposition %q: unexpected error: %v", d, err)
		}
		if path != "" {
			t.Fatalf("disposition %q: expected no lineage path, got %q", d, path)
		}
	}
}

// TestPersistAdmissionLineage_CandidateReadyWritesCompleteBundle is the
// real regression test for a live review finding: a candidate-ready run's
// full SynthesisReceipt/RunnerReceipt/EvaluationReceipt documents (not
// merely their digests) must be durably persisted somewhere
// admissioncomposition.ComposeInput can actually be constructed from --
// they previously existed only in-memory and vanished when the process
// exited.
func TestPersistAdmissionLineage_CandidateReadyWritesCompleteBundle(t *testing.T) {
	const candidateDigest = "cand0000000000000000000000000000000000000000000000000000000000"

	result := synthesisdriver.Result{
		Receipt: synthesisdriver.RunReceipt{
			Disposition:                   synthesisdriver.DispositionCandidateReady,
			CandidateArtifactDigestSHA256: strPtr(candidateDigest),
		},
		SessionState: synthesis.SessionState{
			Receipt: &synthesis.Receipt{
				SchemaVersion: synthesis.ReceiptSchemaVersion,
				ReceiptID:     "receipt.test",
			},
		},
		Trace: synthesisdriver.Trace{
			GenerationHandoffs: []runnercomposition.VerifiedGenerationHandoff{
				{
					RunnerReceipt: runnercomposition.RunnerReceipt{
						SchemaVersion:                 "sensei.runnercomposition.runnerreceipt.v1",
						ReceiptID:                     "runnerreceipt.test",
						Disposition:                   runnercomposition.DispositionVerified,
						CandidateArtifactDigestSHA256: strPtr(candidateDigest),
						RunnerReceiptDigestSHA256:     "runner-digest",
					},
				},
			},
			EvaluationResults: []evaluatorcomposition.Result{
				{
					Receipt: &evaluatorcomposition.EvaluationReceipt{
						SchemaVersion:                 "sensei.evaluatorcomposition.evaluationreceipt.v1",
						ReceiptID:                     "evaluationreceipt.test",
						CandidateArtifactDigestSHA256: candidateDigest,
						Disposition:                   evaluatorcomposition.DispositionEvaluated,
						ReceiptDigestSHA256:           "evaluation-digest",
					},
				},
			},
		},
	}

	store, storeDir := newTestCandidateStore(t)
	path, err := persistAdmissionLineage(context.Background(), result, store, storeDir)
	if err != nil {
		t.Fatalf("persistAdmissionLineage: %v", err)
	}
	wantPath := filepath.Join(storeDir, candidateDigest+".lineage.json")
	if path != wantPath {
		t.Fatalf("path = %q, want %q", path, wantPath)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read lineage file: %v", err)
	}
	var got synthesisRunLineage
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal lineage file: %v", err)
	}
	if got.SchemaVersion != synthesisRunLineageSchemaVersion {
		t.Fatalf("schema_version = %q, want %q", got.SchemaVersion, synthesisRunLineageSchemaVersion)
	}
	if got.CandidateArtifactDigestSHA256 != candidateDigest {
		t.Fatalf("candidate_artifact_digest_sha256 = %q, want %q", got.CandidateArtifactDigestSHA256, candidateDigest)
	}
	if got.CandidateArtifactPath != filepath.Join(storeDir, candidateDigest+".json") {
		t.Fatalf("candidate_artifact_path = %q", got.CandidateArtifactPath)
	}
	if got.SynthesisReceipt.ReceiptID != "receipt.test" {
		t.Fatalf("synthesis_receipt not persisted: %+v", got.SynthesisReceipt)
	}
	if got.RunnerReceipt.ReceiptID != "runnerreceipt.test" {
		t.Fatalf("runner_receipt not persisted: %+v", got.RunnerReceipt)
	}
	if got.EvaluationReceipt.ReceiptID != "evaluationreceipt.test" {
		t.Fatalf("evaluation_receipt not persisted: %+v", got.EvaluationReceipt)
	}
}

// TestPersistAdmissionLineage_MissingSynthesisReceiptErrors covers a
// defensive check: a candidate-ready disposition is only ever reachable
// with SessionState.Receipt populated in real synthesisdriver.Run output
// (Receipt is set once Phase.Terminal() is true), but persistAdmissionLineage
// must fail loudly rather than persist an incomplete bundle if that
// invariant is ever violated.
func TestPersistAdmissionLineage_MissingSynthesisReceiptErrors(t *testing.T) {
	const candidateDigest = "cand0000000000000000000000000000000000000000000000000000000000"
	result := synthesisdriver.Result{
		Receipt: synthesisdriver.RunReceipt{
			Disposition:                   synthesisdriver.DispositionCandidateReady,
			CandidateArtifactDigestSHA256: strPtr(candidateDigest),
		},
	}
	store, dir := newTestCandidateStore(t)
	if _, err := persistAdmissionLineage(context.Background(), result, store, dir); err == nil {
		t.Fatal("expected an error when SessionState.Receipt is nil")
	}
}

// TestPersistAdmissionLineage_NoMatchingRunnerReceiptErrors covers a
// trace whose generation handoffs don't include one matching the sealed
// candidate's digest -- persistAdmissionLineage must refuse rather than
// silently persist an unrelated (or absent) runner receipt.
func TestPersistAdmissionLineage_NoMatchingRunnerReceiptErrors(t *testing.T) {
	const candidateDigest = "cand0000000000000000000000000000000000000000000000000000000000"
	result := synthesisdriver.Result{
		Receipt: synthesisdriver.RunReceipt{
			Disposition:                   synthesisdriver.DispositionCandidateReady,
			CandidateArtifactDigestSHA256: strPtr(candidateDigest),
		},
		SessionState: synthesis.SessionState{
			Receipt: &synthesis.Receipt{SchemaVersion: synthesis.ReceiptSchemaVersion, ReceiptID: "receipt.test"},
		},
		// No matching GenerationHandoffs entry.
	}
	store, dir := newTestCandidateStore(t)
	if _, err := persistAdmissionLineage(context.Background(), result, store, dir); err == nil {
		t.Fatal("expected an error when no runner receipt matches the candidate digest")
	}
}
