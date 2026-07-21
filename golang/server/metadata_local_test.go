// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/globulario/sensei/golang/seedmeta"
)

func TestCollectLocalMetadataFromRoot_CandidatesAndBenchmarkSummary(t *testing.T) {
	root := t.TempDir()
	mkdirAll(t, filepath.Join(root, "docs", "awareness", "candidates"))
	mkdirAll(t, filepath.Join(root, "eval", "multi-swe-bench", "contracts"))
	mkdirAll(t, filepath.Join(root, "eval", "multi-swe-bench", "notes", "learning_events"))

	writeFile(t, filepath.Join(root, "docs", "awareness", "candidates", "one.yaml"), "- id: candidate.one\n  class: invariant\n- id: candidate.two\n  class: intent\n")
	writeFile(t, filepath.Join(root, "eval", "multi-swe-bench", "contracts", "task-a.yaml"), "contract_set_version: 1\n")
	writeFile(t, filepath.Join(root, "eval", "multi-swe-bench", "contracts", "task-b.yaml"), "contract_set_version: 1\n")
	writeFile(t, filepath.Join(root, "eval", "multi-swe-bench", "notes", "learning_events", "cli__cli-1388-20260619T054223Z.yaml"), `learning_event:
  task: cli__cli-1388
  certification_status: certified_clean_repair
  current:
    score: 100
`)
	writeFile(t, filepath.Join(root, "eval", "multi-swe-bench", "notes", "learning_events", "cli__cli-1388-latest.yaml"), "ignored latest link placeholder\n")

	got := collectLocalMetadataFromRoot(root)
	if got.candidateState.String() != "CANDIDATE_QUEUE_STATE_PRESENT" {
		t.Fatalf("candidate state=%s", got.candidateState)
	}
	if got.candidateFileCount != 1 || got.candidateEntryCount != 2 {
		t.Fatalf("candidate counts=(%d,%d), want (1,2)", got.candidateFileCount, got.candidateEntryCount)
	}
	if got.benchmarkState.String() != "BENCHMARK_STATE_PRESENT" {
		t.Fatalf("benchmark state=%s", got.benchmarkState)
	}
	if got.benchmarkContracts != 2 || got.benchmarkEvents != 1 {
		t.Fatalf("benchmark counts=(%d,%d), want (2,1)", got.benchmarkContracts, got.benchmarkEvents)
	}
	if got.benchmarkLatestTask != "cli__cli-1388" || got.benchmarkLatestScore != 100 || got.benchmarkLatestCert != "certified_clean_repair" {
		t.Fatalf("latest benchmark summary=%+v", got)
	}
	wantTime := time.Date(2026, 6, 19, 5, 42, 23, 0, time.UTC).Unix()
	if got.benchmarkLatestUnix != wantTime {
		t.Fatalf("latest unix=%d, want %d", got.benchmarkLatestUnix, wantTime)
	}
}

func TestCollectLocalMetadataFromRoot_SingleDocumentCandidateCountsAsPresent(t *testing.T) {
	root := t.TempDir()
	mkdirAll(t, filepath.Join(root, "docs", "awareness", "candidates", "implementation_pattern"))

	writeFile(t, filepath.Join(root, "docs", "awareness", "candidates", "implementation_pattern", "one.yaml"), `id: candidate.implementation_pattern.awareness_graph_read_only
class: ImplementationPattern
label: AwarenessGraph exposes a read-only gRPC API (candidate)
status: candidate
confidence: candidate
description: Bootstrap-derived candidate.
source_files:
  - proto/awareness_graph.proto
`)

	got := collectLocalMetadataFromRoot(root)
	if got.candidateState.String() != "CANDIDATE_QUEUE_STATE_PRESENT" {
		t.Fatalf("candidate state=%s", got.candidateState)
	}
	if got.candidateFileCount != 1 || got.candidateEntryCount != 1 {
		t.Fatalf("candidate counts=(%d,%d), want (1,1)", got.candidateFileCount, got.candidateEntryCount)
	}
}

func TestCollectLocalMetadataFromRoot_NoLocalSurfaces(t *testing.T) {
	root := t.TempDir()
	mkdirAll(t, filepath.Join(root, "docs", "awareness"))
	got := collectLocalMetadataFromRoot(root)
	if got.candidateState.String() != "CANDIDATE_QUEUE_STATE_NOT_DETECTED" {
		t.Fatalf("candidate state=%s", got.candidateState)
	}
	if got.benchmarkState.String() != "BENCHMARK_STATE_NOT_DETECTED" {
		t.Fatalf("benchmark state=%s", got.benchmarkState)
	}
	if got.governancePackState.String() != "GOVERNANCE_PACK_STATE_NOT_DETECTED" {
		t.Fatalf("governance pack state=%s", got.governancePackState)
	}
}

func TestCollectLocalMetadataFromRoot_GovernancePackNone(t *testing.T) {
	root := t.TempDir()
	mkdirAll(t, filepath.Join(root, ".awg", "governance"))

	got := collectLocalMetadataFromRoot(root)
	if got.governancePackState.String() != "GOVERNANCE_PACK_STATE_NONE" {
		t.Fatalf("governance pack state=%s", got.governancePackState)
	}
}

func TestCollectLocalMetadataFromRoot_GovernancePackActiveUnverified(t *testing.T) {
	root := t.TempDir()
	mkdirAll(t, filepath.Join(root, ".awg", "governance"))
	writeFile(t, filepath.Join(root, ".awg", "governance", "active.json"), `{
  "schema_version": "awg.active-governance.v1",
  "pack_id": "core.meta-principles",
  "pack_version": "2026.06.21",
  "publisher_id": "core@globular.io",
  "payload_digest_sha256": "abc123",
  "payload_triple_count": 42,
  "payload_marker_iri": "https://globular.io/awareness#seed/sha256-abc123"
}`)

	got := collectLocalMetadataFromRoot(root)
	if got.governancePackState.String() != "GOVERNANCE_PACK_STATE_UNVERIFIED" {
		t.Fatalf("governance pack state=%s", got.governancePackState)
	}
	if got.governancePackID != "core.meta-principles" || got.governancePackVer != "2026.06.21" || got.governancePackDigest != "abc123" || got.governancePublisher != "core@globular.io" {
		t.Fatalf("governance summary=%+v", got)
	}
}

func TestCollectLocalMetadataFromRoot_GovernancePackCurrentWhenRecordsAlign(t *testing.T) {
	root := t.TempDir()
	mkdirAll(t, filepath.Join(root, ".awg", "governance", "packs", "core.meta-principles", "2026.06.21"))
	writeFile(t, filepath.Join(root, ".awg", "governance", "trusted-publishers.json"), `{
  "schema_version": "awg.trusted-publishers.v1",
  "publishers": [{
    "publisher_id": "core@globular.io",
    "keys": [{
      "key_id": "core-2026-q2",
      "algorithm": "ed25519",
      "public_key_base64": "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
    }]
  }]
}`)
	writeFile(t, filepath.Join(root, ".awg", "governance", "packs", "core.meta-principles", "2026.06.21", "governance-pack.nt"), string([]byte("<https://globular.io/awareness#seedBuild/sha256-abc123> <https://globular.io/awareness#seedDigestSha256> \"abc123\" .\n<https://globular.io/awareness#seedBuild/sha256-abc123> <https://globular.io/awareness#seedTripleCount> \"1\" .\n")))
	writeFile(t, filepath.Join(root, ".awg", "governance", "packs", "core.meta-principles", "2026.06.21", "governance-pack.manifest.json"), `{
  "schema_version": "awg.governance-pack.v1",
  "pack_id": "core.meta-principles",
  "pack_version": "2026.06.21",
  "publisher": {"id": "core@globular.io", "display_name": "Globular Core"},
  "issued_at": "2026-06-21T12:00:00Z",
  "payload": {
    "format": "ntriples",
    "path": "governance-pack.nt",
    "digest_sha256": "abc123",
    "triple_count": 1,
    "marker_iri": "https://globular.io/awareness#seedBuild/sha256-abc123"
  },
  "compatibility": {"min_awg_version": "0.0.0", "schema_versions": ["awg.governance-pack.v1"]},
  "signature": {"algorithm": "ed25519", "key_id": "core-2026-q2", "sig_path": "governance-pack.manifest.sig"}
}`)
	writeFile(t, filepath.Join(root, ".awg", "governance", "packs", "core.meta-principles", "2026.06.21", "governance-pack.manifest.sig"), "AAAA\n")
	writeFile(t, filepath.Join(root, ".awg", "governance", "active.json"), `{
  "schema_version": "awg.active-governance.v1",
  "pack_id": "core.meta-principles",
  "pack_version": "2026.06.21",
  "publisher_id": "core@globular.io",
  "payload_digest_sha256": "abc123",
  "payload_triple_count": 1,
  "payload_marker_iri": "https://globular.io/awareness#seedBuild/sha256-abc123",
  "manifest_path": ".awg/governance/packs/core.meta-principles/2026.06.21/governance-pack.manifest.json",
  "combined_graph_digest_sha256": "combined",
  "combined_graph_triple_count": 9
}`)
	if err := seedmeta.WriteMarkerFile(filepath.Join(root, ".awg", "graph-authority.json"), seedmeta.Marker{
		Digest:      "combined",
		IRI:         "https://globular.io/awareness#seedBuild/sha256-combined",
		TripleCount: 9,
	}); err != nil {
		t.Fatal(err)
	}

	got := collectLocalMetadataFromRoot(root)
	if got.governancePackState.String() != "GOVERNANCE_PACK_STATE_UNVERIFIED" && got.governancePackState.String() != "GOVERNANCE_PACK_STATE_CURRENT" {
		t.Fatalf("governance pack state=%s", got.governancePackState)
	}
}

func mkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
