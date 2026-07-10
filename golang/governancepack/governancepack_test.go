// SPDX-License-Identifier: Apache-2.0

package governancepack

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/seedmeta"
)

func TestActiveRecordValidateAndRead(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "active.json")
	if err := os.WriteFile(path, []byte(`{
  "schema_version": "awg.active-governance.v1",
  "pack_id": "core.meta-principles",
  "pack_version": "2026.06.21",
  "publisher_id": "core@globular.io",
  "payload_digest_sha256": "abc123",
  "payload_triple_count": 42,
  "payload_marker_iri": "https://globular.io/awareness#seed/sha256-abc123"
}`), 0o644); err != nil {
		t.Fatal(err)
	}
	rec, err := ReadActiveRecord(path)
	if err != nil {
		t.Fatalf("ReadActiveRecord: %v", err)
	}
	if rec.PackID != "core.meta-principles" || rec.PayloadTripleCount != 42 {
		t.Fatalf("record=%+v", rec)
	}
}

func TestVerifyPack_Valid(t *testing.T) {
	dir := t.TempDir()
	env := writeSignedPackFixture(t, dir, packFixtureOptions{})
	verified, err := VerifyPack(env.packDir, env.trustStorePath, "0.0.1-dev")
	if err != nil {
		t.Fatalf("VerifyPack: %v", err)
	}
	if verified.Manifest.PackID != "core.meta-principles" {
		t.Fatalf("pack id=%q", verified.Manifest.PackID)
	}
	if verified.PayloadMarker.Digest != env.marker.Digest {
		t.Fatalf("payload digest=%s, want %s", verified.PayloadMarker.Digest, env.marker.Digest)
	}
}

func TestVerifyPack_MissingRequiredField(t *testing.T) {
	dir := t.TempDir()
	env := writeSignedPackFixture(t, dir, packFixtureOptions{
		mutateManifest: func(m map[string]any) {
			delete(m, "pack_id")
		},
	})
	_, err := VerifyPack(env.packDir, env.trustStorePath, "0.0.1-dev")
	if err == nil || !errors.Is(err, ErrManifestInvalid) {
		t.Fatalf("VerifyPack error=%v, want manifest invalid", err)
	}
}

func TestVerifyPack_InvalidSignature(t *testing.T) {
	dir := t.TempDir()
	env := writeSignedPackFixture(t, dir, packFixtureOptions{corruptSignature: true})
	_, err := VerifyPack(env.packDir, env.trustStorePath, "0.0.1-dev")
	if err == nil || !errors.Is(err, ErrSignatureInvalid) {
		t.Fatalf("VerifyPack error=%v, want signature invalid", err)
	}
}

func TestVerifyPack_UnknownPublisherKey(t *testing.T) {
	dir := t.TempDir()
	env := writeSignedPackFixture(t, dir, packFixtureOptions{unknownPublisher: true})
	_, err := VerifyPack(env.packDir, env.trustStorePath, "0.0.1-dev")
	if err == nil || !errors.Is(err, ErrUnknownPublisher) {
		t.Fatalf("VerifyPack error=%v, want unknown publisher", err)
	}
}

func TestVerifyPack_PayloadDigestMismatch(t *testing.T) {
	dir := t.TempDir()
	env := writeSignedPackFixture(t, dir, packFixtureOptions{
		mutateManifest: func(m map[string]any) {
			payload := m["payload"].(map[string]any)
			payload["digest_sha256"] = "bad-digest"
		},
	})
	_, err := VerifyPack(env.packDir, env.trustStorePath, "0.0.1-dev")
	if err == nil || !errors.Is(err, ErrPayloadInvalid) {
		t.Fatalf("VerifyPack error=%v, want payload invalid", err)
	}
}

func TestVerifyPack_PayloadTripleCountMismatch(t *testing.T) {
	dir := t.TempDir()
	env := writeSignedPackFixture(t, dir, packFixtureOptions{
		mutateManifest: func(m map[string]any) {
			payload := m["payload"].(map[string]any)
			payload["triple_count"] = float64(1)
		},
	})
	_, err := VerifyPack(env.packDir, env.trustStorePath, "0.0.1-dev")
	if err == nil || !errors.Is(err, ErrPayloadInvalid) {
		t.Fatalf("VerifyPack error=%v, want payload invalid", err)
	}
}

func TestVerifyPack_PayloadMarkerMismatch(t *testing.T) {
	dir := t.TempDir()
	env := writeSignedPackFixture(t, dir, packFixtureOptions{
		mutateManifest: func(m map[string]any) {
			payload := m["payload"].(map[string]any)
			payload["marker_iri"] = "https://globular.io/awareness#seed/sha256-wrong"
		},
	})
	_, err := VerifyPack(env.packDir, env.trustStorePath, "0.0.1-dev")
	if err == nil || !errors.Is(err, ErrPayloadInvalid) {
		t.Fatalf("VerifyPack error=%v, want payload invalid", err)
	}
}

func TestVerifyPack_UnsupportedSchemaVersion(t *testing.T) {
	dir := t.TempDir()
	env := writeSignedPackFixture(t, dir, packFixtureOptions{
		mutateManifest: func(m map[string]any) {
			m["schema_version"] = "awg.governance-pack.v0"
		},
	})
	_, err := VerifyPack(env.packDir, env.trustStorePath, "0.0.1-dev")
	if err == nil || !errors.Is(err, ErrManifestInvalid) {
		t.Fatalf("VerifyPack error=%v, want manifest invalid", err)
	}
}

func TestVerifyPack_CompatibilityBlocked(t *testing.T) {
	dir := t.TempDir()
	env := writeSignedPackFixture(t, dir, packFixtureOptions{
		mutateManifest: func(m map[string]any) {
			compat := m["compatibility"].(map[string]any)
			compat["min_awg_version"] = "99.0.0"
		},
	})
	_, err := VerifyPack(env.packDir, env.trustStorePath, "0.0.1-dev")
	if err == nil || !errors.Is(err, ErrCompatibilityBlocked) {
		t.Fatalf("VerifyPack error=%v, want compatibility blocked", err)
	}
}

func TestLoadTrustStore_RejectsInvalidStatus(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "trusted-publishers.json")
	if err := os.WriteFile(path, []byte(`{
  "schema_version": "awg.trusted-publishers.v1",
  "publishers": [{
    "publisher_id": "core@globular.io",
    "keys": [{
      "key_id": "core-2026-q2",
      "algorithm": "ed25519",
      "public_key_base64": "AAAA",
      "status": "bogus"
    }]
  }]
}`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadTrustStore(path)
	if err == nil || !errors.Is(err, ErrTrustStoreInvalid) {
		t.Fatalf("LoadTrustStore error=%v, want trust store invalid", err)
	}
}

func TestVerifyPack_RevokedKeyBlocked(t *testing.T) {
	dir := t.TempDir()
	env := writeSignedPackFixture(t, dir, packFixtureOptions{keyStatus: "revoked"})
	_, err := VerifyPack(env.packDir, env.trustStorePath, "0.0.1-dev")
	if err == nil || !errors.Is(err, ErrKeyRevoked) {
		t.Fatalf("VerifyPack error=%v, want key revoked", err)
	}
}

func TestVerifyPack_FutureKeyBlocked(t *testing.T) {
	dir := t.TempDir()
	env := writeSignedPackFixture(t, dir, packFixtureOptions{keyStatus: "future", validFrom: "2999-01-01T00:00:00Z"})
	_, err := VerifyPack(env.packDir, env.trustStorePath, "0.0.1-dev")
	if err == nil || !errors.Is(err, ErrKeyNotYetValid) {
		t.Fatalf("VerifyPack error=%v, want key not yet valid", err)
	}
}

func TestAssessLocalStatus_Current(t *testing.T) {
	root := t.TempDir()
	env := writeSignedPackFixture(t, filepath.Join(root, "incoming"), packFixtureOptions{})
	stageDir := filepath.Join(root, ".awg", "governance", "packs", "core.meta-principles", "2026.06.21")
	if err := os.MkdirAll(stageDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"governance-pack.nt", "governance-pack.manifest.json", "governance-pack.manifest.sig"} {
		data, err := os.ReadFile(filepath.Join(env.packDir, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(stageDir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Dir(TrustedKeysPath(root)), 0o755); err != nil {
		t.Fatal(err)
	}
	trustBytes, err := os.ReadFile(env.trustStorePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(TrustedKeysPath(root), trustBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	active := ActiveRecord{
		SchemaVersion:             ActiveRecordSchemaV1,
		PackID:                    "core.meta-principles",
		PackVersion:               "2026.06.21",
		PublisherID:               "core@globular.io",
		PayloadDigestSHA256:       env.marker.Digest,
		PayloadTripleCount:        env.marker.TripleCount,
		PayloadMarkerIRI:          env.marker.IRI,
		ManifestPath:              filepath.ToSlash(filepath.Join(".awg", "governance", "packs", "core.meta-principles", "2026.06.21", "governance-pack.manifest.json")),
		CombinedGraphDigestSHA256: "combined",
		CombinedGraphTripleCount:  77,
	}
	if err := WriteActiveRecord(ActiveRecordPath(root), active); err != nil {
		t.Fatal(err)
	}
	if err := seedmeta.WriteMarkerFile(seedmeta.RuntimeMarkerPath(root), seedmeta.Marker{
		Digest:      "combined",
		IRI:         seedmeta.NamespaceIRI + "seedBuild/sha256-combined",
		TripleCount: 77,
	}); err != nil {
		t.Fatal(err)
	}
	status := AssessLocalStatus(root, "0.0.1-dev")
	if status.State != StateCurrent {
		t.Fatalf("state=%s detail=%s", status.State, status.Detail)
	}
}

func TestFetchedRecordReadWriteAndList(t *testing.T) {
	root := t.TempDir()
	rec := FetchedRecord{
		SchemaVersion:       FetchedRecordSchemaV1,
		PackID:              "core.meta-principles",
		PackVersion:         "2026.06.21",
		PublisherID:         "core@globular.io",
		PayloadDigestSHA256: "abc123",
		PayloadTripleCount:  7,
		PayloadMarkerIRI:    "https://globular.io/awareness#seed/sha256-abc123",
		FetchedAt:           "2026-06-21T12:00:00Z",
		Source:              "/tmp/published",
		RequestedChannel:    "stable",
		ManifestPath:        ".awg/governance/fetched/core.meta-principles/2026.06.21/governance-pack.manifest.json",
	}
	path := FetchedRecordPath(root, rec.PackID, rec.PackVersion)
	if err := WriteFetchedRecord(path, rec); err != nil {
		t.Fatalf("WriteFetchedRecord: %v", err)
	}
	got, err := ReadFetchedRecord(path)
	if err != nil {
		t.Fatalf("ReadFetchedRecord: %v", err)
	}
	if got.RequestedChannel != rec.RequestedChannel || got.Source != rec.Source {
		t.Fatalf("got=%+v want=%+v", got, rec)
	}
	list, err := ListFetchedRecords(root)
	if err != nil {
		t.Fatalf("ListFetchedRecords: %v", err)
	}
	if len(list) != 1 || list[0].PackID != rec.PackID {
		t.Fatalf("list=%+v", list)
	}
}

type packFixtureOptions struct {
	mutateManifest   func(map[string]any)
	corruptSignature bool
	unknownPublisher bool
	keyStatus        string
	validFrom        string
	validUntil       string
}

type packFixtureEnv struct {
	packDir        string
	trustStorePath string
	marker         seedmeta.Marker
}

func writeSignedPackFixture(t *testing.T, dir string, opts packFixtureOptions) packFixtureEnv {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	packDir := filepath.Join(dir, "pack")
	if err := os.MkdirAll(packDir, 0o755); err != nil {
		t.Fatal(err)
	}
	payload, marker := seedmeta.AppendMarker([]byte("<https://example.test/meta> <https://example.test/p> \"x\" .\n"))
	if err := os.WriteFile(filepath.Join(packDir, "governance-pack.nt"), payload, 0o644); err != nil {
		t.Fatal(err)
	}
	manifest := map[string]any{
		"schema_version": ManifestSchemaV1,
		"pack_id":        "core.meta-principles",
		"pack_version":   "2026.06.21",
		"publisher": map[string]any{
			"id":           "core@globular.io",
			"display_name": "Globular Core",
		},
		"issued_at": "2026-06-21T12:00:00Z",
		"payload": map[string]any{
			"format":        "ntriples",
			"path":          "governance-pack.nt",
			"digest_sha256": marker.Digest,
			"triple_count":  marker.TripleCount,
			"marker_iri":    marker.IRI,
		},
		"compatibility": map[string]any{
			"min_awg_version": "0.0.0",
			"max_awg_version": "",
			"schema_versions": []string{ManifestSchemaV1},
		},
		"source": map[string]any{
			"corpus_digest_sha256": "source",
			"promotion_batch_id":   "",
		},
		"signature": map[string]any{
			"algorithm": "ed25519",
			"key_id":    "core-2026-q2",
			"sig_path":  "governance-pack.manifest.sig",
		},
	}
	if opts.mutateManifest != nil {
		opts.mutateManifest(manifest)
	}
	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	manifestBytes = append(manifestBytes, '\n')
	if err := os.WriteFile(filepath.Join(packDir, "governance-pack.manifest.json"), manifestBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sig := ed25519.Sign(priv, manifestBytes)
	if opts.corruptSignature && len(sig) > 0 {
		sig[0] ^= 0xff
	}
	if err := os.WriteFile(filepath.Join(packDir, "governance-pack.manifest.sig"), []byte(base64.StdEncoding.EncodeToString(sig)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	trustStore := TrustStore{
		SchemaVersion: "awg.trusted-publishers.v1",
		Publishers: []TrustedPublisher{{
			PublisherID: "core@globular.io",
			Keys: []TrustedKey{{
				KeyID:           "core-2026-q2",
				Algorithm:       "ed25519",
				PublicKeyBase64: base64.StdEncoding.EncodeToString(pub),
				Status:          opts.keyStatus,
				ValidFrom:       opts.validFrom,
				ValidUntil:      opts.validUntil,
			}},
		}},
	}
	if opts.unknownPublisher {
		trustStore.Publishers[0].Keys[0].KeyID = "wrong"
	}
	trustStorePath := filepath.Join(dir, "trusted-publishers.json")
	data, err := json.MarshalIndent(trustStore, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(trustStorePath, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	return packFixtureEnv{packDir: packDir, trustStorePath: trustStorePath, marker: marker}
}

func TestAppendActivationLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "activation-log.jsonl")
	entry := ActivationLogEntry{Result: "success"}
	if err := AppendActivationLog(path, entry); err != nil {
		t.Fatalf("AppendActivationLog: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"result":"success"`) {
		t.Fatalf("log=%s", string(data))
	}
}
