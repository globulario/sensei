// SPDX-License-Identifier: Apache-2.0

package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/governancepack"
	"github.com/globulario/sensei/golang/seedmeta"
)

func TestGovernanceVerifyPack_DoesNotMutateLocalState(t *testing.T) {
	root := t.TempDir()
	mkdirAll(t, filepath.Join(root, "docs", "awareness"))
	env := writeGovernancePackFixture(t, root)
	if code := runGovernance([]string{"verify-pack", "-project-root", root, "-trusted-keys", env.trustStorePath, env.packDir}); code != 0 {
		t.Fatalf("verify-pack code=%d, want 0", code)
	}
	if _, err := os.Stat(governancepack.ActiveRecordPath(root)); !os.IsNotExist(err) {
		t.Fatalf("active record should not be created, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(governancepack.GovernanceDirPath(root), "packs")); !os.IsNotExist(err) {
		t.Fatalf("packs dir should not be created, stat err=%v", err)
	}
}

func TestGovernanceInitAndTrustAddListShowRotateCheck(t *testing.T) {
	root := t.TempDir()
	if code := runGovernance([]string{"init", "-project-root", root}); code != 0 {
		t.Fatalf("init code=%d, want 0", code)
	}
	if _, err := os.Stat(governancepack.TrustedKeysPath(root)); err != nil {
		t.Fatalf("trust store missing: %v", err)
	}
	env := writeGovernancePackFixture(t, root)
	incoming := filepath.Join(root, "incoming-trust.json")
	src, err := os.ReadFile(env.trustStorePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(incoming, src, 0o644); err != nil {
		t.Fatal(err)
	}
	if code := runGovernance([]string{"trust", "add", "-project-root", root, "--file", incoming}); code != 0 {
		t.Fatalf("trust add code=%d, want 0", code)
	}

	capture := captureGovernanceOutput(t, func() int {
		return runGovernance([]string{"trust", "list", "-project-root", root})
	})
	if capture.code != 0 || !strings.Contains(capture.stdout, "core@globular.io") {
		t.Fatalf("trust list stdout=%q stderr=%q code=%d", capture.stdout, capture.stderr, capture.code)
	}
	capture = captureGovernanceOutput(t, func() int {
		return runGovernance([]string{"trust", "show", "-project-root", root, "core@globular.io"})
	})
	if capture.code != 0 || !strings.Contains(capture.stdout, `"publisher_id": "core@globular.io"`) {
		t.Fatalf("trust show stdout=%q stderr=%q code=%d", capture.stdout, capture.stderr, capture.code)
	}
	capture = captureGovernanceOutput(t, func() int {
		return runGovernance([]string{"trust", "rotate-check", "-project-root", root})
	})
	if capture.code != 0 || !strings.Contains(capture.stdout, "Rotation check:") {
		t.Fatalf("rotate-check stdout=%q stderr=%q code=%d", capture.stdout, capture.stderr, capture.code)
	}
}

func TestGovernanceTrustFetch_FromFileStagesWithoutMutatingLiveTrust(t *testing.T) {
	root := t.TempDir()
	if code := runGovernance([]string{"init", "-project-root", root}); code != 0 {
		t.Fatalf("init code=%d, want 0", code)
	}
	sourceRoot := filepath.Join(root, "source")
	mkdirAll(t, sourceRoot)
	env := writeGovernancePackFixture(t, sourceRoot)
	sourcePath := filepath.Join(root, "source-trusted-publishers.json")
	srcBytes, err := os.ReadFile(env.trustStorePath)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, sourcePath, string(srcBytes))
	if code := runGovernance([]string{"trust", "fetch", "-project-root", root, "--source", sourcePath}); code != 0 {
		t.Fatalf("trust fetch code=%d, want 0", code)
	}
	staged := governancepack.StagedTrustStorePath(root)
	if _, err := os.Stat(staged); err != nil {
		t.Fatalf("staged trust store missing: %v", err)
	}
	rec, err := governancepack.ReadStagedTrustRecord(governancepack.StagedTrustRecordPath(root))
	if err != nil {
		t.Fatalf("ReadStagedTrustRecord: %v", err)
	}
	if rec.Source != sourcePath || rec.PublisherCount != 1 {
		t.Fatalf("staged record=%+v", rec)
	}
	live, err := governancepack.LoadTrustStore(governancepack.TrustedKeysPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if len(live.Publishers) != 0 {
		t.Fatalf("live trust store should remain empty after fetch, got %+v", live)
	}
}

func TestGovernanceTrustFetch_FromHTTPStagesCandidate(t *testing.T) {
	root := t.TempDir()
	env := writeGovernancePackFixture(t, root)
	dir := filepath.Dir(env.trustStorePath)
	ts := httptest.NewServer(http.FileServer(http.Dir(dir)))
	defer ts.Close()
	projectRoot := filepath.Join(root, "project")
	mkdirAll(t, projectRoot)
	if code := runGovernance([]string{"trust", "fetch", "-project-root", projectRoot, "--source", ts.URL}); code != 0 {
		t.Fatalf("trust fetch code=%d, want 0", code)
	}
	staged, err := governancepack.LoadTrustStore(governancepack.StagedTrustStorePath(projectRoot))
	if err != nil {
		t.Fatal(err)
	}
	if len(staged.Publishers) != 1 || staged.Publishers[0].PublisherID != "core@globular.io" {
		t.Fatalf("staged=%+v", staged)
	}
}

func TestGovernanceTrustFetch_InvalidCandidateFailsClosed(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "bad-trust.json")
	writeFile(t, src, `{"schema_version":"awg.trusted-publishers.v1","publishers":[{"publisher_id":"","keys":[]}]}`)
	projectRoot := filepath.Join(root, "project")
	mkdirAll(t, projectRoot)
	if code := runGovernance([]string{"trust", "fetch", "-project-root", projectRoot, "--source", src}); code == 0 {
		t.Fatal("trust fetch should fail for invalid trust store")
	}
	if _, err := os.Stat(governancepack.StagedTrustStorePath(projectRoot)); !os.IsNotExist(err) {
		t.Fatalf("staged trust store should not remain, stat err=%v", err)
	}
}

func TestGovernanceActivate_SuccessWritesActiveRecordAndLog(t *testing.T) {
	root := t.TempDir()
	awarenessDir := filepath.Join(root, "docs", "awareness")
	mkdirAll(t, awarenessDir)
	if err := copyFixtureYAMLs(filepath.Join("..", "..", "golang", "extractor", "testdata"), awarenessDir); err != nil {
		t.Fatal(err)
	}
	env := writeGovernancePackFixture(t, root)

	var loaded []byte
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/store":
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("read store body: %v", err)
			}
			loaded = append([]byte(nil), body...)
			w.WriteHeader(http.StatusNoContent)
		case "/query":
			writeVerificationQuery(t, w, loaded, readQueryBody(t, r))
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	if code := runGovernance([]string{"activate", "-project-root", root, "-trusted-keys", env.trustStorePath, "-store-url", ts.URL + "/store?default", env.packDir}); code != 0 {
		t.Fatalf("activate code=%d, want 0", code)
	}
	active, err := governancepack.ReadActiveRecord(governancepack.ActiveRecordPath(root))
	if err != nil {
		t.Fatalf("ReadActiveRecord: %v", err)
	}
	if active.PackID != "core.meta-principles" || active.PayloadDigestSHA256 != env.marker.Digest {
		t.Fatalf("active=%+v", active)
	}
	graphMarker, err := seedmeta.ReadMarkerFile(seedmeta.RuntimeMarkerPath(root))
	if err != nil {
		t.Fatalf("ReadMarkerFile: %v", err)
	}
	if active.CombinedGraphDigestSHA256 != graphMarker.Digest {
		t.Fatalf("combined digest=%s, want %s", active.CombinedGraphDigestSHA256, graphMarker.Digest)
	}
	logBytes, err := os.ReadFile(governancepack.ActivationLogPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(logBytes), `"result":"success"`) {
		t.Fatalf("activation log=%s", string(logBytes))
	}
}

func TestGovernanceActivate_FailureLeavesPreviousActiveRecordUnchanged(t *testing.T) {
	root := t.TempDir()
	awarenessDir := filepath.Join(root, "docs", "awareness")
	mkdirAll(t, awarenessDir)
	if err := copyFixtureYAMLs(filepath.Join("..", "..", "golang", "extractor", "testdata"), awarenessDir); err != nil {
		t.Fatal(err)
	}
	env := writeGovernancePackFixture(t, root)
	prev := governancepack.ActiveRecord{
		SchemaVersion:             governancepack.ActiveRecordSchemaV1,
		PackID:                    "prev.pack",
		PackVersion:               "1",
		PublisherID:               "core@globular.io",
		PayloadDigestSHA256:       "prevdigest",
		PayloadTripleCount:        10,
		PayloadMarkerIRI:          "https://globular.io/awareness#seed/sha256-prev",
		CombinedGraphDigestSHA256: "prevcombined",
		CombinedGraphTripleCount:  10,
	}
	if err := governancepack.WriteActiveRecord(governancepack.ActiveRecordPath(root), prev); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/store" {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	if code := runGovernance([]string{"activate", "-project-root", root, "-trusted-keys", env.trustStorePath, "-store-url", ts.URL + "/store?default", env.packDir}); code == 0 {
		t.Fatal("activate should fail when upload fails")
	}
	got, err := governancepack.ReadActiveRecord(governancepack.ActiveRecordPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if got.PackID != prev.PackID || got.PayloadDigestSHA256 != prev.PayloadDigestSHA256 {
		t.Fatalf("active record mutated: got=%+v want=%+v", got, prev)
	}
	logBytes, err := os.ReadFile(governancepack.ActivationLogPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(logBytes), `"result":"failure"`) {
		t.Fatalf("activation log=%s", string(logBytes))
	}
}

func TestRunBuild_FailsClosedWhenManagedGovernanceEnabledAndActivePackMissing(t *testing.T) {
	root := t.TempDir()
	awarenessDir := filepath.Join(root, "docs", "awareness")
	mkdirAll(t, awarenessDir)
	if err := copyFixtureYAMLs(filepath.Join("..", "..", "golang", "extractor", "testdata"), awarenessDir); err != nil {
		t.Fatal(err)
	}
	env := writeGovernancePackFixture(t, root)
	_ = env

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(cwd) }()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	if code := runBuild([]string{"-input", awarenessDir, "-output", filepath.Join(root, "out.nt")}); code == 0 {
		t.Fatal("runBuild should fail when managed mode is enabled but active pack is missing")
	}
}

func TestRunRebuild_FailsClosedWhenManagedGovernanceEnabledAndActivePackMissing(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, "docs", "awareness"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := copyFixtureYAMLs(filepath.Join("..", "..", "golang", "extractor", "testdata"), filepath.Join(repo, "docs", "awareness")); err != nil {
		t.Fatal(err)
	}
	writeGovernancePackFixture(t, repo)
	initGitRepo(t, repo)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(cwd) }()
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	if code := runRebuild([]string{"--ag-repo", repo, "--no-runtime-reload"}); code == 0 {
		t.Fatal("runRebuild should fail when managed mode is enabled but active pack is missing")
	}
}

func TestGovernancePublishBuildSignRelease_EndToEnd(t *testing.T) {
	root := t.TempDir()
	inputNT := filepath.Join(root, "canonical.nt")
	writeFile(t, inputNT, "<https://example.test/principle/a> <https://example.test/p> \"x\" .\n")
	signingKeyPath, trustStorePath := writeSigningKeyFixture(t, root, "core@globular.io", "core-2026-q2")
	packDir := filepath.Join(root, "build", "core.meta-principles", "2026.06.21")

	if code := runGovernance([]string{
		"publish", "build",
		"--input-nt", inputNT,
		"--out-dir", packDir,
		"--pack-id", "core.meta-principles",
		"--pack-version", "2026.06.21",
		"--publisher-id", "core@globular.io",
		"--publisher-name", "Globular Core",
		"--issued-at", "2026-06-21T12:00:00Z",
		"--min-awg-version", "0.0.0",
		"--key-id", "core-2026-q2",
	}); code != 0 {
		t.Fatalf("publish build code=%d, want 0", code)
	}
	if _, err := os.Stat(filepath.Join(packDir, "governance-pack.manifest.sig")); !os.IsNotExist(err) {
		t.Fatalf("signature should not exist before sign, stat err=%v", err)
	}
	if code := runGovernance([]string{"publish", "sign", "--signing-key", signingKeyPath, packDir}); code != 0 {
		t.Fatalf("publish sign code=%d, want 0", code)
	}
	if code := runGovernance([]string{"verify-pack", "-project-root", root, "-trusted-keys", trustStorePath, packDir}); code != 0 {
		t.Fatalf("verify-pack code=%d, want 0", code)
	}
	pubRoot := filepath.Join(root, "published")
	if code := runGovernance([]string{
		"publish", "release",
		"--trusted-keys", trustStorePath,
		"--signing-key", signingKeyPath,
		"--publication-root", pubRoot,
		"--channel", "stable",
		packDir,
	}); code != 0 {
		t.Fatalf("publish release code=%d, want 0", code)
	}
	released := filepath.Join(pubRoot, "governance", "packs", "core.meta-principles", "2026.06.21")
	for _, name := range []string{"governance-pack.nt", "governance-pack.manifest.json", "governance-pack.manifest.sig"} {
		if _, err := os.Stat(filepath.Join(released, name)); err != nil {
			t.Fatalf("released artifact %s missing: %v", name, err)
		}
	}
	indexBytes, err := os.ReadFile(filepath.Join(pubRoot, "governance", "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(indexBytes), `"pack_id": "core.meta-principles"`) || !strings.Contains(string(indexBytes), `"channel": "stable"`) {
		t.Fatalf("publication index=%s", string(indexBytes))
	}
	if _, err := os.Stat(filepath.Join(pubRoot, "governance", "index.json.sig")); err != nil {
		t.Fatalf("index signature missing: %v", err)
	}
	publishedTrust, err := governancepack.LoadTrustStore(filepath.Join(pubRoot, "trusted-publishers.json"))
	if err != nil {
		t.Fatalf("published trust root missing or invalid: %v", err)
	}
	if len(publishedTrust.Publishers) != 1 || publishedTrust.Publishers[0].PublisherID != "core@globular.io" {
		t.Fatalf("publishedTrust=%+v", publishedTrust)
	}
}

func TestGovernancePublishGenKey_WritesValidSigningKey(t *testing.T) {
	root := t.TempDir()
	signingKeyPath := filepath.Join(root, "publisher", "signing-key.json")
	if code := runGovernance([]string{
		"publish", "gen-key",
		"--out", signingKeyPath,
		"--publisher-id", "core@globular.io",
		"--key-id", "core-2026-q3",
	}); code != 0 {
		t.Fatalf("publish gen-key code=%d, want 0", code)
	}
	key, priv, err := governancepack.LoadSigningKey(signingKeyPath)
	if err != nil {
		t.Fatal(err)
	}
	if key.SchemaVersion != governancepack.SigningKeySchemaV1 || key.PublisherID != "core@globular.io" || key.KeyID != "core-2026-q3" {
		t.Fatalf("signingKey=%+v", key)
	}
	if len(priv) != ed25519.PrivateKeySize {
		t.Fatalf("private key size=%d, want %d", len(priv), ed25519.PrivateKeySize)
	}
}

func TestGovernancePublishTrustRoot_WritesValidTrustStore(t *testing.T) {
	root := t.TempDir()
	signingKeyPath := filepath.Join(root, "signing-key.json")
	trustStorePath := filepath.Join(root, "published", "trusted-publishers.json")
	if code := runGovernance([]string{
		"publish", "gen-key",
		"--out", signingKeyPath,
		"--publisher-id", "core@globular.io",
		"--key-id", "core-2026-q3",
	}); code != 0 {
		t.Fatalf("publish gen-key code=%d, want 0", code)
	}
	if code := runGovernance([]string{
		"publish", "trust-root",
		"--signing-key", signingKeyPath,
		"--out", trustStorePath,
		"--display-name", "Globular Core",
	}); code != 0 {
		t.Fatalf("publish trust-root code=%d, want 0", code)
	}
	store, err := governancepack.LoadTrustStore(trustStorePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(store.Publishers) != 1 {
		t.Fatalf("publishers=%d, want 1", len(store.Publishers))
	}
	pub := store.Publishers[0]
	if pub.PublisherID != "core@globular.io" || pub.DisplayName != "Globular Core" {
		t.Fatalf("publisher=%+v", pub)
	}
	if len(pub.Keys) != 1 || pub.Keys[0].KeyID != "core-2026-q3" || pub.Keys[0].Status != "active" {
		t.Fatalf("keys=%+v", pub.Keys)
	}
}

func TestGovernancePublishSign_RejectsMismatchedKeyID(t *testing.T) {
	root := t.TempDir()
	inputNT := filepath.Join(root, "canonical.nt")
	writeFile(t, inputNT, "<https://example.test/principle/a> <https://example.test/p> \"x\" .\n")
	packDir := filepath.Join(root, "build", "core.meta-principles", "2026.06.21")
	if code := runGovernance([]string{
		"publish", "build",
		"--input-nt", inputNT,
		"--out-dir", packDir,
		"--pack-id", "core.meta-principles",
		"--pack-version", "2026.06.21",
		"--publisher-id", "core@globular.io",
		"--issued-at", "2026-06-21T12:00:00Z",
		"--min-awg-version", "0.0.0",
		"--key-id", "core-2026-q2",
	}); code != 0 {
		t.Fatalf("publish build code=%d, want 0", code)
	}
	signingKeyPath, _ := writeSigningKeyFixture(t, root, "core@globular.io", "other-key")
	if code := runGovernance([]string{"publish", "sign", "--signing-key", signingKeyPath, packDir}); code == 0 {
		t.Fatal("publish sign should fail when key id does not match manifest")
	}
}

func TestGovernancePublishRelease_FailureDoesNotWriteIndex(t *testing.T) {
	root := t.TempDir()
	inputNT := filepath.Join(root, "canonical.nt")
	writeFile(t, inputNT, "<https://example.test/principle/a> <https://example.test/p> \"x\" .\n")
	signingKeyPath, trustStorePath := writeSigningKeyFixture(t, root, "core@globular.io", "core-2026-q2")
	packDir := filepath.Join(root, "build", "core.meta-principles", "2026.06.21")
	if code := runGovernance([]string{
		"publish", "build",
		"--input-nt", inputNT,
		"--out-dir", packDir,
		"--pack-id", "core.meta-principles",
		"--pack-version", "2026.06.21",
		"--publisher-id", "core@globular.io",
		"--issued-at", "2026-06-21T12:00:00Z",
		"--min-awg-version", "0.0.0",
		"--key-id", "core-2026-q2",
	}); code != 0 {
		t.Fatalf("publish build code=%d, want 0", code)
	}
	if code := runGovernance([]string{"publish", "sign", "--signing-key", signingKeyPath, packDir}); code != 0 {
		t.Fatalf("publish sign code=%d, want 0", code)
	}
	badTrust := filepath.Join(root, "bad-trust.json")
	writeFile(t, badTrust, `{
  "schema_version": "awg.trusted-publishers.v1",
  "publishers": [{
    "publisher_id": "other@globular.io",
    "keys": [{
      "key_id": "core-2026-q2",
      "algorithm": "ed25519",
      "public_key_base64": "AAAA",
      "status": "active"
    }]
  }]
}
`)
	_ = trustStorePath
	pubRoot := filepath.Join(root, "published")
	if code := runGovernance([]string{
		"publish", "release",
		"--trusted-keys", badTrust,
		"--signing-key", signingKeyPath,
		"--publication-root", pubRoot,
		packDir,
	}); code == 0 {
		t.Fatal("publish release should fail when verification fails")
	}
	if _, err := os.Stat(filepath.Join(pubRoot, "governance", "index.json")); !os.IsNotExist(err) {
		t.Fatalf("publication index should not exist, stat err=%v", err)
	}
}

func TestGovernanceFetch_FromLocalPublicationRoot(t *testing.T) {
	root := t.TempDir()
	inputNT := filepath.Join(root, "canonical.nt")
	writeFile(t, inputNT, "<https://example.test/principle/a> <https://example.test/p> \"x\" .\n")
	signingKeyPath, trustStorePath := writeSigningKeyFixture(t, root, "core@globular.io", "core-2026-q2")
	packDir := filepath.Join(root, "build", "core.meta-principles", "2026.06.21")
	if code := runGovernance([]string{
		"publish", "build",
		"--input-nt", inputNT,
		"--out-dir", packDir,
		"--pack-id", "core.meta-principles",
		"--pack-version", "2026.06.21",
		"--publisher-id", "core@globular.io",
		"--issued-at", "2026-06-21T12:00:00Z",
		"--min-awg-version", "0.0.0",
		"--key-id", "core-2026-q2",
	}); code != 0 {
		t.Fatalf("publish build code=%d", code)
	}
	if code := runGovernance([]string{"publish", "sign", "--signing-key", signingKeyPath, packDir}); code != 0 {
		t.Fatalf("publish sign code=%d", code)
	}
	pubRoot := filepath.Join(root, "published")
	if code := runGovernance([]string{
		"publish", "release",
		"--trusted-keys", trustStorePath,
		"--signing-key", signingKeyPath,
		"--publication-root", pubRoot,
		"--channel", "stable",
		packDir,
	}); code != 0 {
		t.Fatalf("publish release code=%d", code)
	}
	projectRoot := filepath.Join(root, "project")
	mkdirAll(t, projectRoot)
	if code := runGovernance([]string{"init", "-project-root", projectRoot}); code != 0 {
		t.Fatalf("governance init code=%d", code)
	}
	if code := runGovernance([]string{"trust", "fetch", "-project-root", projectRoot, "--source", pubRoot}); code != 0 {
		t.Fatalf("trust fetch code=%d", code)
	}
	if code := runGovernance([]string{"trust", "add", "-project-root", projectRoot, "--file", governancepack.StagedTrustStorePath(projectRoot)}); code != 0 {
		t.Fatalf("trust add code=%d", code)
	}
	if code := runGovernance([]string{
		"fetch",
		"-project-root", projectRoot,
		"-source", pubRoot,
		"-pack-id", "core.meta-principles",
		"-channel", "stable",
	}); code != 0 {
		t.Fatalf("fetch code=%d", code)
	}
	fetchedManifest := filepath.Join(projectRoot, ".sensei", "governance", "fetched", "core.meta-principles", "2026.06.21", "governance-pack.manifest.json")
	if _, err := os.Stat(fetchedManifest); err != nil {
		t.Fatalf("fetched manifest missing: %v", err)
	}
	fetchedRecord, err := governancepack.ReadFetchedRecord(filepath.Join(projectRoot, ".sensei", "governance", "fetched", "core.meta-principles", "2026.06.21", "fetch.json"))
	if err != nil {
		t.Fatalf("ReadFetchedRecord: %v", err)
	}
	if fetchedRecord.RequestedChannel != "stable" || fetchedRecord.Source != pubRoot {
		t.Fatalf("fetchedRecord=%+v", fetchedRecord)
	}
}

func TestGovernanceFetch_FromHTTPPublicationRoot(t *testing.T) {
	root := t.TempDir()
	inputNT := filepath.Join(root, "canonical.nt")
	writeFile(t, inputNT, "<https://example.test/principle/a> <https://example.test/p> \"x\" .\n")
	signingKeyPath, trustStorePath := writeSigningKeyFixture(t, root, "core@globular.io", "core-2026-q2")
	packDir := filepath.Join(root, "build", "core.meta-principles", "2026.06.21")
	if code := runGovernance([]string{
		"publish", "build",
		"--input-nt", inputNT,
		"--out-dir", packDir,
		"--pack-id", "core.meta-principles",
		"--pack-version", "2026.06.21",
		"--publisher-id", "core@globular.io",
		"--issued-at", "2026-06-21T12:00:00Z",
		"--min-awg-version", "0.0.0",
		"--key-id", "core-2026-q2",
	}); code != 0 {
		t.Fatalf("publish build code=%d", code)
	}
	if code := runGovernance([]string{"publish", "sign", "--signing-key", signingKeyPath, packDir}); code != 0 {
		t.Fatalf("publish sign code=%d", code)
	}
	pubRoot := filepath.Join(root, "published")
	if code := runGovernance([]string{
		"publish", "release",
		"--trusted-keys", trustStorePath,
		"--signing-key", signingKeyPath,
		"--publication-root", pubRoot,
		"--channel", "stable",
		packDir,
	}); code != 0 {
		t.Fatalf("publish release code=%d", code)
	}
	fileServer := httptest.NewServer(http.FileServer(http.Dir(pubRoot)))
	defer fileServer.Close()
	projectRoot := filepath.Join(root, "project")
	mkdirAll(t, projectRoot)
	if code := runGovernance([]string{
		"fetch",
		"-project-root", projectRoot,
		"-trusted-keys", trustStorePath,
		"-source", fileServer.URL,
		"-pack-id", "core.meta-principles",
		"-channel", "stable",
	}); code != 0 {
		t.Fatalf("fetch code=%d", code)
	}
	fetchedPayload := filepath.Join(projectRoot, ".sensei", "governance", "fetched", "core.meta-principles", "2026.06.21", "governance-pack.nt")
	if _, err := os.Stat(fetchedPayload); err != nil {
		t.Fatalf("fetched payload missing: %v", err)
	}
}

func TestGovernanceFetch_MissingChannelFailsClosed(t *testing.T) {
	root := t.TempDir()
	signingKeyPath, trustStorePath := writeSigningKeyFixture(t, root, "core@globular.io", "core-2026-q2")
	_ = signingKeyPath
	pubRoot := filepath.Join(root, "published")
	index := governancepack.PublicationIndex{
		SchemaVersion: governancepack.PublicationIndexSchemaV1,
		Packs:         []governancepack.PublicationPackIndex{},
	}
	indexBytes, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(pubRoot, "governance", "index.json"), string(append(indexBytes, '\n')))
	projectRoot := filepath.Join(root, "project")
	mkdirAll(t, projectRoot)
	if code := runGovernance([]string{
		"fetch",
		"-project-root", projectRoot,
		"-trusted-keys", trustStorePath,
		"-source", pubRoot,
		"-pack-id", "core.meta-principles",
		"-channel", "stable",
	}); code == 0 {
		t.Fatal("fetch should fail when channel target is missing")
	}
	gotDir := filepath.Join(projectRoot, ".sensei", "governance", "fetched")
	if _, err := os.Stat(gotDir); !os.IsNotExist(err) {
		t.Fatalf("fetched dir should not exist, stat err=%v", err)
	}
}

func TestGovernanceFetch_TamperedIndexFailsClosed(t *testing.T) {
	root := t.TempDir()
	inputNT := filepath.Join(root, "canonical.nt")
	writeFile(t, inputNT, "<https://example.test/principle/a> <https://example.test/p> \"x\" .\n")
	signingKeyPath, trustStorePath := writeSigningKeyFixture(t, root, "core@globular.io", "core-2026-q2")
	packDir := filepath.Join(root, "build", "core.meta-principles", "2026.06.21")
	if code := runGovernance([]string{
		"publish", "build",
		"--input-nt", inputNT,
		"--out-dir", packDir,
		"--pack-id", "core.meta-principles",
		"--pack-version", "2026.06.21",
		"--publisher-id", "core@globular.io",
		"--issued-at", "2026-06-21T12:00:00Z",
		"--min-awg-version", "0.0.0",
		"--key-id", "core-2026-q2",
	}); code != 0 {
		t.Fatalf("publish build code=%d", code)
	}
	if code := runGovernance([]string{"publish", "sign", "--signing-key", signingKeyPath, packDir}); code != 0 {
		t.Fatalf("publish sign code=%d", code)
	}
	pubRoot := filepath.Join(root, "published")
	if code := runGovernance([]string{
		"publish", "release",
		"--trusted-keys", trustStorePath,
		"--signing-key", signingKeyPath,
		"--publication-root", pubRoot,
		"--channel", "stable",
		packDir,
	}); code != 0 {
		t.Fatalf("publish release code=%d", code)
	}
	indexPath := filepath.Join(pubRoot, "governance", "index.json")
	data, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	replaced := strings.Replace(string(data), `"pack_version": "2026.06.21"`, `"pack_version": "9999.99.99"`, 1)
	if replaced == string(data) {
		t.Fatal("expected index mutation")
	}
	writeFile(t, indexPath, replaced)
	projectRoot := filepath.Join(root, "project")
	mkdirAll(t, projectRoot)
	if code := runGovernance([]string{
		"fetch",
		"-project-root", projectRoot,
		"-trusted-keys", trustStorePath,
		"-source", pubRoot,
		"-pack-id", "core.meta-principles",
		"-channel", "stable",
	}); code == 0 {
		t.Fatal("fetch should fail when index signature no longer matches")
	}
}

func TestGovernanceStatus_ShowsFetchedProvenanceAndDetectsFetchedStale(t *testing.T) {
	root := t.TempDir()
	inputNT := filepath.Join(root, "canonical.nt")
	writeFile(t, inputNT, "<https://example.test/principle/a> <https://example.test/p> \"x\" .\n")
	signingKeyPath, trustStorePath := writeSigningKeyFixture(t, root, "core@globular.io", "core-2026-q2")
	packDir := filepath.Join(root, "build", "core.meta-principles", "2026.06.21")
	if code := runGovernance([]string{
		"publish", "build",
		"--input-nt", inputNT,
		"--out-dir", packDir,
		"--pack-id", "core.meta-principles",
		"--pack-version", "2026.06.21",
		"--publisher-id", "core@globular.io",
		"--issued-at", "2026-06-21T12:00:00Z",
		"--min-awg-version", "0.0.0",
		"--key-id", "core-2026-q2",
	}); code != 0 {
		t.Fatalf("publish build code=%d", code)
	}
	if code := runGovernance([]string{"publish", "sign", "--signing-key", signingKeyPath, packDir}); code != 0 {
		t.Fatalf("publish sign code=%d", code)
	}
	pubRoot := filepath.Join(root, "published")
	if code := runGovernance([]string{
		"publish", "release",
		"--trusted-keys", trustStorePath,
		"--signing-key", signingKeyPath,
		"--publication-root", pubRoot,
		"--channel", "stable",
		packDir,
	}); code != 0 {
		t.Fatalf("publish release code=%d", code)
	}
	projectRoot := filepath.Join(root, "project")
	mkdirAll(t, projectRoot)
	trustBytes, err := os.ReadFile(trustStorePath)
	if err != nil {
		t.Fatal(err)
	}
	mkdirAll(t, filepath.Dir(governancepack.TrustedKeysPath(projectRoot)))
	writeFile(t, governancepack.TrustedKeysPath(projectRoot), string(trustBytes))
	if code := runGovernance([]string{
		"fetch",
		"-project-root", projectRoot,
		"-trusted-keys", trustStorePath,
		"-source", pubRoot,
		"-pack-id", "core.meta-principles",
		"-channel", "stable",
	}); code != 0 {
		t.Fatalf("fetch code=%d", code)
	}
	capture := captureGovernanceOutput(t, func() int {
		return runGovernance([]string{"status", "-project-root", projectRoot})
	})
	if capture.code != 0 || !strings.Contains(capture.stdout, "Fetched state:       current") || !strings.Contains(capture.stdout, "Fetched channel:     stable") {
		t.Fatalf("status stdout=%q stderr=%q code=%d", capture.stdout, capture.stderr, capture.code)
	}
	writeFile(t, filepath.Join(projectRoot, ".sensei", "governance", "fetched", "core.meta-principles", "2026.06.21", "governance-pack.nt"), "<https://tampered> <https://p> \"x\" .\n")
	status := governancepack.AssessLocalStatus(projectRoot, Version)
	if status.FetchedState != governancepack.StateStale {
		t.Fatalf("FetchedState=%s want stale detail=%s", status.FetchedState, status.FetchedDetail)
	}
}

func TestGovernanceStatus_ShowsStagedTrustState(t *testing.T) {
	root := t.TempDir()
	if code := runGovernance([]string{"init", "-project-root", root}); code != 0 {
		t.Fatalf("init code=%d, want 0", code)
	}
	sourceRoot := filepath.Join(root, "source")
	mkdirAll(t, sourceRoot)
	env := writeGovernancePackFixture(t, sourceRoot)
	sourcePath := filepath.Join(root, "source-trusted-publishers.json")
	srcBytes, err := os.ReadFile(env.trustStorePath)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, sourcePath, string(srcBytes))
	if code := runGovernance([]string{"trust", "fetch", "-project-root", root, "--source", sourcePath}); code != 0 {
		t.Fatalf("trust fetch code=%d", code)
	}
	capture := captureGovernanceOutput(t, func() int {
		return runGovernance([]string{"status", "-project-root", root})
	})
	if capture.code != 0 || !strings.Contains(capture.stdout, "Staged trust state:  current") || !strings.Contains(capture.stdout, "Staged trust src:    "+sourcePath) {
		t.Fatalf("status stdout=%q stderr=%q code=%d", capture.stdout, capture.stderr, capture.code)
	}
	if code := runGovernance([]string{"trust", "add", "-project-root", root, "--file", governancepack.StagedTrustStorePath(root)}); code != 0 {
		t.Fatalf("trust add code=%d", code)
	}
	capture = captureGovernanceOutput(t, func() int {
		return runGovernance([]string{"status", "-project-root", root})
	})
	if capture.code != 0 || !strings.Contains(capture.stdout, "Staged trust detail: staged trust root matches active trust store") {
		t.Fatalf("status after add stdout=%q stderr=%q code=%d", capture.stdout, capture.stderr, capture.code)
	}
}

type governancePackFixture struct {
	packDir        string
	trustStorePath string
	marker         seedmeta.Marker
}

func writeGovernancePackFixture(t *testing.T, root string) governancePackFixture {
	t.Helper()
	packDir := filepath.Join(root, "incoming-pack")
	mkdirAll(t, packDir)
	payload, marker := seedmeta.AppendMarker([]byte("<https://example.test/shared> <https://example.test/p> \"x\" .\n"))
	writeFile(t, filepath.Join(packDir, "governance-pack.nt"), string(payload))
	manifest := map[string]any{
		"schema_version": governancepack.ManifestSchemaV1,
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
			"schema_versions": []string{governancepack.ManifestSchemaV1},
		},
		"source": map[string]any{
			"corpus_digest_sha256": "corpus",
			"promotion_batch_id":   "",
		},
		"signature": map[string]any{
			"algorithm": "ed25519",
			"key_id":    "core-2026-q2",
			"sig_path":  "governance-pack.manifest.sig",
		},
	}
	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	manifestBytes = append(manifestBytes, '\n')
	writeFile(t, filepath.Join(packDir, "governance-pack.manifest.json"), string(manifestBytes))
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sig := ed25519.Sign(priv, manifestBytes)
	writeFile(t, filepath.Join(packDir, "governance-pack.manifest.sig"), base64.StdEncoding.EncodeToString(sig)+"\n")
	trustStore := governancepack.TrustStore{
		SchemaVersion: governancepack.TrustStoreSchemaV1,
		Publishers: []governancepack.TrustedPublisher{{
			PublisherID: "core@globular.io",
			Keys: []governancepack.TrustedKey{{
				KeyID:           "core-2026-q2",
				Algorithm:       "ed25519",
				PublicKeyBase64: base64.StdEncoding.EncodeToString(pub),
				Status:          "active",
			}},
		}},
	}
	trustBytes, err := json.MarshalIndent(trustStore, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	trustStorePath := governancepack.TrustedKeysPath(root)
	mkdirAll(t, filepath.Dir(trustStorePath))
	writeFile(t, trustStorePath, string(append(trustBytes, '\n')))
	return governancePackFixture{
		packDir:        packDir,
		trustStorePath: trustStorePath,
		marker:         marker,
	}
}

func writeSigningKeyFixture(t *testing.T, root, publisherID, keyID string) (string, string) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signingKey := map[string]any{
		"schema_version":     governancepack.SigningKeySchemaV1,
		"publisher_id":       publisherID,
		"key_id":             keyID,
		"algorithm":          "ed25519",
		"private_key_base64": base64.StdEncoding.EncodeToString(priv),
	}
	signingBytes, err := json.MarshalIndent(signingKey, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	signingKeyPath := filepath.Join(root, "signing-key.json")
	writeFile(t, signingKeyPath, string(append(signingBytes, '\n')))
	trustStore := governancepack.TrustStore{
		SchemaVersion: governancepack.TrustStoreSchemaV1,
		Publishers: []governancepack.TrustedPublisher{{
			PublisherID: publisherID,
			Keys: []governancepack.TrustedKey{{
				KeyID:           keyID,
				Algorithm:       "ed25519",
				PublicKeyBase64: base64.StdEncoding.EncodeToString(pub),
				Status:          "active",
			}},
		}},
	}
	trustBytes, err := json.MarshalIndent(trustStore, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	trustStorePath := filepath.Join(root, "trusted-publishers.json")
	writeFile(t, trustStorePath, string(append(trustBytes, '\n')))
	return signingKeyPath, trustStorePath
}

type capturedOutput struct {
	code   int
	stdout string
	stderr string
}

func captureGovernanceOutput(t *testing.T, fn func() int) capturedOutput {
	t.Helper()
	origStdout := os.Stdout
	origStderr := os.Stderr
	rOut, wOut, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	rErr, wErr, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = wOut
	os.Stderr = wErr
	code := fn()
	_ = wOut.Close()
	_ = wErr.Close()
	os.Stdout = origStdout
	os.Stderr = origStderr
	outBytes, _ := io.ReadAll(rOut)
	errBytes, _ := io.ReadAll(rErr)
	return capturedOutput{code: code, stdout: string(outBytes), stderr: string(errBytes)}
}

func mkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}
