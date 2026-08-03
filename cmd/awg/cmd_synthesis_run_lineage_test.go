// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/globulario/sensei/golang/architecture/evaluatorcomposition"
	"github.com/globulario/sensei/golang/architecture/runnercomposition"
	"github.com/globulario/sensei/golang/architecture/synthesis"
	"github.com/globulario/sensei/golang/architecture/synthesisdriver"
)

// TestWriteRegularFileAtomicNoFollow_WritesAndReplaces covers the ordinary
// case (no existing entry) and the legitimate replace case (an existing
// regular file, e.g. a second run reproducing the same candidate digest
// with fresh receipt content) -- both must succeed.
func TestWriteRegularFileAtomicNoFollow_WritesAndReplaces(t *testing.T) {
	dir := t.TempDir()
	if err := writeRegularFileAtomicNoFollow(dir, "x.json", []byte("first")); err != nil {
		t.Fatalf("first write: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "x.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "first" {
		t.Fatalf("content = %q, want %q", got, "first")
	}
	if err := writeRegularFileAtomicNoFollow(dir, "x.json", []byte("second")); err != nil {
		t.Fatalf("replace write: %v", err)
	}
	got, err = os.ReadFile(filepath.Join(dir, "x.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "second" {
		t.Fatalf("content after replace = %q, want %q", got, "second")
	}
	// No leftover staging files.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "x.json" {
		t.Fatalf("directory contains unexpected entries: %v", entries)
	}
}

// TestWriteRegularFileAtomicNoFollow_RefusesExistingSymlink is the direct
// regression test for the live review finding: an existing symlink at the
// target name (e.g. planted by another process, or a pre-existing
// attacker-controlled entry) must be refused, never followed or
// transparently overwritten by a plain path-based write.
func TestWriteRegularFileAtomicNoFollow_RefusesExistingSymlink(t *testing.T) {
	dir := t.TempDir()
	outsideTarget := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outsideTarget, []byte("do not touch"), 0o644); err != nil {
		t.Fatal(err)
	}
	linkPath := filepath.Join(dir, "x.json")
	if err := os.Symlink(outsideTarget, linkPath); err != nil {
		t.Skipf("symlinks unsupported in this environment: %v", err)
	}

	if err := writeRegularFileAtomicNoFollow(dir, "x.json", []byte("attempted overwrite")); err == nil {
		t.Fatal("expected an error writing through an existing symlink")
	}

	// The symlink itself, and its target, must be untouched.
	info, err := os.Lstat(linkPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("the symlink at the target name was replaced instead of refused")
	}
	outsideContent, err := os.ReadFile(outsideTarget)
	if err != nil {
		t.Fatal(err)
	}
	if string(outsideContent) != "do not touch" {
		t.Fatalf("symlink target was written through: %s", outsideContent)
	}
}

// TestPersistAdmissionLineage_NonCandidateReadyReturnsEmpty covers every
// disposition without a sealed candidate: there is nothing to persist, and
// persistAdmissionLineage must not error or write a file.
func TestPersistAdmissionLineage_NonCandidateReadyReturnsEmpty(t *testing.T) {
	for _, d := range []synthesisdriver.Disposition{
		synthesisdriver.DispositionTerminalFailure,
		synthesisdriver.DispositionProviderStopped,
		synthesisdriver.DispositionRunnerStopped,
		synthesisdriver.DispositionStepLimitReached,
	} {
		result := synthesisdriver.Result{Receipt: synthesisdriver.RunReceipt{Disposition: d}}
		path, err := persistAdmissionLineage(result, t.TempDir())
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

	storeDir := t.TempDir()
	path, err := persistAdmissionLineage(result, storeDir)
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
	if _, err := persistAdmissionLineage(result, t.TempDir()); err == nil {
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
	if _, err := persistAdmissionLineage(result, t.TempDir()); err == nil {
		t.Fatal("expected an error when no runner receipt matches the candidate digest")
	}
}
