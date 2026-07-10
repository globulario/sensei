// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/globulario/sensei/golang/governancepack"
	"github.com/globulario/sensei/golang/seedmeta"
)

func generateGovernanceSigningKey(outPath, publisherID, keyID string) (governancepack.SigningKey, error) {
	if strings.TrimSpace(outPath) == "" {
		return governancepack.SigningKey{}, fmt.Errorf("output path is required")
	}
	if strings.TrimSpace(publisherID) == "" {
		return governancepack.SigningKey{}, fmt.Errorf("publisher id is required")
	}
	if strings.TrimSpace(keyID) == "" {
		return governancepack.SigningKey{}, fmt.Errorf("key id is required")
	}
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return governancepack.SigningKey{}, err
	}
	key := governancepack.SigningKey{
		SchemaVersion: governancepack.SigningKeySchemaV1,
		PublisherID:   strings.TrimSpace(publisherID),
		KeyID:         strings.TrimSpace(keyID),
		Algorithm:     "ed25519",
		PrivateKeyB64: base64.StdEncoding.EncodeToString(priv),
	}
	if err := key.Validate(); err != nil {
		return governancepack.SigningKey{}, err
	}
	data, err := json.MarshalIndent(key, "", "  ")
	if err != nil {
		return governancepack.SigningKey{}, err
	}
	if err := writeFileAtomic(outPath, append(data, '\n')); err != nil {
		return governancepack.SigningKey{}, err
	}
	return key, nil
}

func buildGovernanceTrustRoot(signingKeyPath, outPath, displayName, status string) (governancepack.TrustStore, error) {
	if strings.TrimSpace(outPath) == "" {
		return governancepack.TrustStore{}, fmt.Errorf("output path is required")
	}
	key, priv, err := governancepack.LoadSigningKey(signingKeyPath)
	if err != nil {
		return governancepack.TrustStore{}, err
	}
	if strings.TrimSpace(key.PublisherID) == "" {
		return governancepack.TrustStore{}, fmt.Errorf("signing key publisher_id is required")
	}
	pub := priv.Public().(ed25519.PublicKey)
	store := governancepack.TrustStore{
		SchemaVersion: governancepack.TrustStoreSchemaV1,
		Publishers: []governancepack.TrustedPublisher{{
			PublisherID: strings.TrimSpace(key.PublisherID),
			DisplayName: strings.TrimSpace(displayName),
			Keys: []governancepack.TrustedKey{{
				KeyID:           strings.TrimSpace(key.KeyID),
				Algorithm:       "ed25519",
				PublicKeyBase64: base64.StdEncoding.EncodeToString(pub),
				Status:          strings.TrimSpace(status),
			}},
		}},
	}
	if err := store.Validate(); err != nil {
		return governancepack.TrustStore{}, err
	}
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return governancepack.TrustStore{}, err
	}
	if err := writeFileAtomic(outPath, append(data, '\n')); err != nil {
		return governancepack.TrustStore{}, err
	}
	return store, nil
}

type governancePublishBuildOptions struct {
	PackID             string
	PackVersion        string
	PublisherID        string
	PublisherName      string
	IssuedAt           string
	MinAWGVersion      string
	MaxAWGVersion      string
	KeyID              string
	CorpusDigestSHA256 string
	PromotionBatchID   string
	OutputDir          string
}

func buildGovernancePackFromBytes(payloadInput []byte, opts governancePublishBuildOptions) (seedmeta.Marker, error) {
	if err := validateGovernancePublishBuildOptions(opts); err != nil {
		return seedmeta.Marker{}, err
	}
	payload, marker, _, _ := finalizeBuildArtifact(payloadInput)
	if errs := extractorValidate(payload); len(errs) > 0 {
		return seedmeta.Marker{}, fmt.Errorf("payload validation failed: %d error(s)", len(errs))
	}
	if err := os.MkdirAll(opts.OutputDir, 0o755); err != nil {
		return seedmeta.Marker{}, err
	}
	manifest := governancepack.Manifest{
		SchemaVersion: governancepack.ManifestSchemaV1,
		PackID:        strings.TrimSpace(opts.PackID),
		PackVersion:   strings.TrimSpace(opts.PackVersion),
		IssuedAt:      strings.TrimSpace(opts.IssuedAt),
	}
	manifest.Publisher.ID = strings.TrimSpace(opts.PublisherID)
	manifest.Publisher.DisplayName = strings.TrimSpace(opts.PublisherName)
	manifest.Payload.Format = "ntriples"
	manifest.Payload.Path = "governance-pack.nt"
	manifest.Payload.DigestSHA256 = marker.Digest
	manifest.Payload.TripleCount = marker.TripleCount
	manifest.Payload.MarkerIRI = marker.IRI
	manifest.Compatibility.MinAWGVersion = strings.TrimSpace(opts.MinAWGVersion)
	manifest.Compatibility.MaxAWGVersion = strings.TrimSpace(opts.MaxAWGVersion)
	manifest.Compatibility.SchemaVersions = []string{governancepack.ManifestSchemaV1}
	manifest.Source.CorpusDigestSHA256 = strings.TrimSpace(opts.CorpusDigestSHA256)
	manifest.Source.PromotionBatchID = strings.TrimSpace(opts.PromotionBatchID)
	manifest.Signature.Algorithm = "ed25519"
	manifest.Signature.KeyID = strings.TrimSpace(opts.KeyID)
	manifest.Signature.SigPath = "governance-pack.manifest.sig"
	if err := manifest.Validate(); err != nil {
		return seedmeta.Marker{}, err
	}
	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return seedmeta.Marker{}, err
	}
	manifestBytes = append(manifestBytes, '\n')
	if err := writeFileAtomic(filepath.Join(opts.OutputDir, "governance-pack.nt"), payload); err != nil {
		return seedmeta.Marker{}, err
	}
	if err := writeFileAtomic(filepath.Join(opts.OutputDir, "governance-pack.manifest.json"), manifestBytes); err != nil {
		return seedmeta.Marker{}, err
	}
	return marker, nil
}

func validateGovernancePublishBuildOptions(opts governancePublishBuildOptions) error {
	switch {
	case strings.TrimSpace(opts.PackID) == "":
		return fmt.Errorf("pack id is required")
	case strings.TrimSpace(opts.PackVersion) == "":
		return fmt.Errorf("pack version is required")
	case strings.TrimSpace(opts.PublisherID) == "":
		return fmt.Errorf("publisher id is required")
	case strings.TrimSpace(opts.IssuedAt) == "":
		return fmt.Errorf("issued_at is required")
	case strings.TrimSpace(opts.MinAWGVersion) == "":
		return fmt.Errorf("min_awg_version is required")
	case strings.TrimSpace(opts.KeyID) == "":
		return fmt.Errorf("key id is required")
	case strings.TrimSpace(opts.OutputDir) == "":
		return fmt.Errorf("output dir is required")
	}
	if _, err := time.Parse(time.RFC3339, opts.IssuedAt); err != nil {
		return fmt.Errorf("issued_at invalid: %w", err)
	}
	return nil
}

func signGovernancePack(pathOrDir, signingKeyPath string) (governancepack.Manifest, []byte, error) {
	paths, err := governancepack.ResolveBundlePaths(pathOrDir)
	if err != nil {
		return governancepack.Manifest{}, nil, err
	}
	manifest, manifestBytes, err := governancepack.ReadManifest(paths.ManifestPath)
	if err != nil {
		return governancepack.Manifest{}, nil, err
	}
	key, priv, err := governancepack.LoadSigningKey(signingKeyPath)
	if err != nil {
		return governancepack.Manifest{}, nil, err
	}
	if !strings.EqualFold(strings.TrimSpace(key.Algorithm), strings.TrimSpace(manifest.Signature.Algorithm)) {
		return governancepack.Manifest{}, nil, fmt.Errorf("signing key algorithm %s does not match manifest %s", key.Algorithm, manifest.Signature.Algorithm)
	}
	if strings.TrimSpace(key.KeyID) != strings.TrimSpace(manifest.Signature.KeyID) {
		return governancepack.Manifest{}, nil, fmt.Errorf("signing key id %s does not match manifest %s", key.KeyID, manifest.Signature.KeyID)
	}
	if strings.TrimSpace(key.PublisherID) != "" && strings.TrimSpace(key.PublisherID) != strings.TrimSpace(manifest.Publisher.ID) {
		return governancepack.Manifest{}, nil, fmt.Errorf("signing key publisher %s does not match manifest %s", key.PublisherID, manifest.Publisher.ID)
	}
	sig := ed25519.Sign(priv, manifestBytes)
	if err := writeFileAtomic(paths.SignaturePath, []byte(base64.StdEncoding.EncodeToString(sig)+"\n")); err != nil {
		return governancepack.Manifest{}, nil, err
	}
	return manifest, sig, nil
}

func releaseGovernancePack(pathOrDir, trustedKeysPath, publicationRoot, signingKeyPath string, channels []string) (governancepack.VerifiedPack, string, error) {
	verified, err := governancepack.VerifyPack(pathOrDir, trustedKeysPath, Version)
	if err != nil {
		return governancepack.VerifiedPack{}, "", err
	}
	trustStore, err := governancepack.LoadTrustStore(trustedKeysPath)
	if err != nil {
		return governancepack.VerifiedPack{}, "", err
	}
	stageDir := filepath.Join(governancepack.PublicationRoot(publicationRoot), "packs", verified.Manifest.PackID, verified.Manifest.PackVersion)
	for _, item := range []struct {
		path string
		data []byte
	}{
		{filepath.Join(stageDir, "governance-pack.nt"), verified.PayloadBytes},
		{filepath.Join(stageDir, "governance-pack.manifest.json"), verified.ManifestBytes},
		{filepath.Join(stageDir, "governance-pack.manifest.sig"), []byte(base64.StdEncoding.EncodeToString(verified.SignatureBytes) + "\n")},
	} {
		if err := writeFileAtomic(item.path, item.data); err != nil {
			return governancepack.VerifiedPack{}, "", err
		}
	}
	if err := governancepack.WriteTrustStore(filepath.Join(publicationRoot, "trusted-publishers.json"), trustStore); err != nil {
		return governancepack.VerifiedPack{}, "", err
	}
	indexChannels, err := mergePublicationChannels(governancepack.PublicationIndexPath(publicationRoot), verified.Manifest.PackID, verified.Manifest.PackVersion, channels)
	if err != nil {
		return governancepack.VerifiedPack{}, "", err
	}
	index, err := governancepack.BuildPublicationIndex(publicationRoot, trustedKeysPath, Version, indexChannels)
	if err != nil {
		return governancepack.VerifiedPack{}, "", err
	}
	indexPath := governancepack.PublicationIndexPath(publicationRoot)
	signingKey, priv, err := governancepack.LoadSigningKey(signingKeyPath)
	if err != nil {
		return governancepack.VerifiedPack{}, "", err
	}
	if strings.TrimSpace(signingKey.PublisherID) != "" && strings.TrimSpace(signingKey.PublisherID) != strings.TrimSpace(verified.Manifest.Publisher.ID) {
		return governancepack.VerifiedPack{}, "", fmt.Errorf("signing key publisher %s does not match pack publisher %s", signingKey.PublisherID, verified.Manifest.Publisher.ID)
	}
	index.Publisher.ID = verified.Manifest.Publisher.ID
	index.Publisher.DisplayName = verified.Manifest.Publisher.DisplayName
	index.Signature.KeyID = signingKey.KeyID
	index.Signature.Algorithm = signingKey.Algorithm
	index.Signature.SigPath = "index.json.sig"
	if err := governancepack.SignPublicationIndex(indexPath, index, signingKey, priv); err != nil {
		return governancepack.VerifiedPack{}, "", err
	}
	return verified, indexPath, nil
}

func mergePublicationChannels(indexPath, packID, packVersion string, channels []string) ([]governancepack.PublicationChannelRef, error) {
	var existing []governancepack.PublicationChannelRef
	data, err := os.ReadFile(indexPath)
	if err == nil {
		var index governancepack.PublicationIndex
		if err := json.Unmarshal(data, &index); err != nil {
			return nil, fmt.Errorf("decode publication index: %w", err)
		}
		existing = append(existing, index.Channels...)
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	for _, channel := range channels {
		channel = strings.TrimSpace(channel)
		if channel == "" {
			continue
		}
		replaced := false
		for i := range existing {
			if existing[i].Channel == channel && existing[i].PackID == packID {
				existing[i].PackVersion = packVersion
				replaced = true
				break
			}
		}
		if !replaced {
			existing = append(existing, governancepack.PublicationChannelRef{
				Channel:     channel,
				PackID:      packID,
				PackVersion: packVersion,
			})
		}
	}
	return existing, nil
}

func readPackBuildInput(inputNT string, inputDirs []string, strict bool) ([]byte, error) {
	if strings.TrimSpace(inputNT) != "" {
		data, err := os.ReadFile(strings.TrimSpace(inputNT))
		if err != nil {
			return nil, err
		}
		return bytes.TrimSpace(data), nil
	}
	if len(inputDirs) == 0 {
		return nil, fmt.Errorf("one of --input-nt or --input is required")
	}
	raw, _, err := compileAwarenessInputs(inputDirs, "", "", "", strict)
	if err != nil {
		return nil, err
	}
	return raw, nil
}

func fetchGovernancePack(source, trustedKeysPath, root, packID, packVersion, channel string) (governancepack.VerifiedPack, governancepack.BundlePaths, error) {
	index, target, base, err := resolvePublicationFetchTarget(source, trustedKeysPath, packID, packVersion, channel)
	if err != nil {
		return governancepack.VerifiedPack{}, governancepack.BundlePaths{}, err
	}
	_ = index
	destDir := filepath.Join(governancepack.GovernanceDirPath(root), "fetched", target.PackID, target.PackVersion)
	tmpDir := destDir + ".tmp"
	_ = os.RemoveAll(tmpDir)
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return governancepack.VerifiedPack{}, governancepack.BundlePaths{}, err
	}
	for _, item := range []struct {
		rel string
		dst string
	}{
		{target.ManifestPath, filepath.Join(tmpDir, "governance-pack.manifest.json")},
		{target.SignaturePath, filepath.Join(tmpDir, "governance-pack.manifest.sig")},
		{target.PayloadPath, filepath.Join(tmpDir, "governance-pack.nt")},
	} {
		if err := fetchPublicationFile(base, item.rel, item.dst); err != nil {
			_ = os.RemoveAll(tmpDir)
			return governancepack.VerifiedPack{}, governancepack.BundlePaths{}, err
		}
	}
	verified, err := governancepack.VerifyPack(tmpDir, trustedKeysPath, Version)
	if err != nil {
		_ = os.RemoveAll(tmpDir)
		return governancepack.VerifiedPack{}, governancepack.BundlePaths{}, err
	}
	_ = os.RemoveAll(destDir)
	if err := os.Rename(tmpDir, destDir); err != nil {
		_ = os.RemoveAll(tmpDir)
		return governancepack.VerifiedPack{}, governancepack.BundlePaths{}, err
	}
	rec := governancepack.FetchedRecord{
		SchemaVersion:        governancepack.FetchedRecordSchemaV1,
		PackID:               verified.Manifest.PackID,
		PackVersion:          verified.Manifest.PackVersion,
		PublisherID:          verified.Manifest.Publisher.ID,
		PayloadDigestSHA256:  verified.PayloadMarker.Digest,
		PayloadTripleCount:   verified.PayloadMarker.TripleCount,
		PayloadMarkerIRI:     verified.PayloadMarker.IRI,
		FetchedAt:            time.Now().UTC().Format(time.RFC3339),
		Source:               strings.TrimSpace(source),
		RequestedChannel:     strings.TrimSpace(channel),
		RequestedPackVersion: strings.TrimSpace(packVersion),
		ManifestPath:         filepath.ToSlash(filepath.Join(".awg", "governance", "fetched", verified.Manifest.PackID, verified.Manifest.PackVersion, "governance-pack.manifest.json")),
	}
	if err := governancepack.WriteFetchedRecord(filepath.Join(destDir, "fetch.json"), rec); err != nil {
		return governancepack.VerifiedPack{}, governancepack.BundlePaths{}, err
	}
	return verified, governancepack.BundlePaths{
		Dir:           destDir,
		ManifestPath:  filepath.Join(destDir, "governance-pack.manifest.json"),
		SignaturePath: filepath.Join(destDir, "governance-pack.manifest.sig"),
		PayloadPath:   filepath.Join(destDir, "governance-pack.nt"),
	}, nil
}

func resolvePublicationFetchTarget(source, trustedKeysPath, packID, packVersion, channel string) (governancepack.PublicationIndex, governancepack.PublicationPackVersion, string, error) {
	base := strings.TrimSpace(source)
	if base == "" {
		return governancepack.PublicationIndex{}, governancepack.PublicationPackVersion{}, "", fmt.Errorf("publication source is required")
	}
	index, err := readPublicationIndexFromSource(base, trustedKeysPath)
	if err != nil {
		return governancepack.PublicationIndex{}, governancepack.PublicationPackVersion{}, "", err
	}
	target, err := governancepack.ResolvePublicationTarget(index, packID, packVersion, channel)
	if err != nil {
		return governancepack.PublicationIndex{}, governancepack.PublicationPackVersion{}, "", err
	}
	return index, target, base, nil
}

func readPublicationIndexFromSource(source, trustedKeysPath string) (governancepack.PublicationIndex, error) {
	if isHTTPSource(source) {
		u, err := url.Parse(source)
		if err != nil {
			return governancepack.PublicationIndex{}, err
		}
		u.Path = strings.TrimRight(u.Path, "/") + "/governance/index.json"
		u.RawQuery = ""
		req, err := http.NewRequest(http.MethodGet, u.String(), nil)
		if err != nil {
			return governancepack.PublicationIndex{}, err
		}
		resp, err := httpDefaultClient().Do(req)
		if err != nil {
			return governancepack.PublicationIndex{}, err
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
			return governancepack.PublicationIndex{}, fmt.Errorf("fetch publication index: %s: %s", resp.Status, strings.TrimSpace(string(body)))
		}
		data, err := io.ReadAll(resp.Body)
		if err != nil {
			return governancepack.PublicationIndex{}, err
		}
		tmp := filepath.Join(os.TempDir(), fmt.Sprintf("awg-pub-index-%d.json", time.Now().UnixNano()))
		if err := os.WriteFile(tmp, data, 0o600); err != nil {
			return governancepack.PublicationIndex{}, err
		}
		defer os.Remove(tmp)
		rawIndex, err := governancepack.ReadPublicationIndex(tmp)
		if err != nil {
			return governancepack.PublicationIndex{}, err
		}
		sigURL := *u
		sigURL.Path = strings.TrimRight(sigURL.Path, "/") + ".sig"
		req, err = http.NewRequest(http.MethodGet, sigURL.String(), nil)
		if err != nil {
			return governancepack.PublicationIndex{}, err
		}
		resp, err = httpDefaultClient().Do(req)
		if err != nil {
			return governancepack.PublicationIndex{}, err
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
			return governancepack.PublicationIndex{}, fmt.Errorf("fetch publication index signature: %s: %s", resp.Status, strings.TrimSpace(string(body)))
		}
		sigData, err := io.ReadAll(resp.Body)
		if err != nil {
			return governancepack.PublicationIndex{}, err
		}
		sigPath := filepath.Join(filepath.Dir(tmp), strings.TrimSpace(rawIndex.Signature.SigPath))
		if err := os.WriteFile(sigPath, sigData, 0o600); err != nil {
			return governancepack.PublicationIndex{}, err
		}
		defer os.Remove(sigPath)
		index, _, err := governancepack.VerifyPublicationIndex(tmp, trustedKeysPath)
		return index, err
	}
	indexPath := governancepack.PublicationIndexPath(source)
	index, _, err := governancepack.VerifyPublicationIndex(indexPath, trustedKeysPath)
	return index, err
}

func fetchPublicationFile(source, relPath, dst string) error {
	if isHTTPSource(source) {
		u, err := url.Parse(source)
		if err != nil {
			return err
		}
		u.Path = strings.TrimRight(u.Path, "/") + "/" + strings.TrimLeft(filepath.ToSlash(relPath), "/")
		u.RawQuery = ""
		req, err := http.NewRequest(http.MethodGet, u.String(), nil)
		if err != nil {
			return err
		}
		resp, err := httpDefaultClient().Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
			return fmt.Errorf("fetch %s: %s: %s", relPath, resp.Status, strings.TrimSpace(string(body)))
		}
		return governancepack.CopyFile(dst, resp.Body)
	}
	src := filepath.Join(source, filepath.FromSlash(relPath))
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	return governancepack.CopyFile(dst, f)
}

func isHTTPSource(source string) bool {
	return strings.HasPrefix(strings.TrimSpace(source), "http://") || strings.HasPrefix(strings.TrimSpace(source), "https://")
}

func fetchTrustStoreCandidate(source, root string) (governancepack.TrustStore, string, error) {
	source = strings.TrimSpace(source)
	if source == "" {
		return governancepack.TrustStore{}, "", fmt.Errorf("trust source is required")
	}
	stagePath := governancepack.StagedTrustStorePath(root)
	if isHTTPSource(source) {
		u, err := url.Parse(source)
		if err != nil {
			return governancepack.TrustStore{}, "", err
		}
		u.Path = strings.TrimRight(u.Path, "/") + "/trusted-publishers.json"
		u.RawQuery = ""
		req, err := http.NewRequest(http.MethodGet, u.String(), nil)
		if err != nil {
			return governancepack.TrustStore{}, "", err
		}
		resp, err := httpDefaultClient().Do(req)
		if err != nil {
			return governancepack.TrustStore{}, "", err
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
			return governancepack.TrustStore{}, "", fmt.Errorf("fetch trust store: %s: %s", resp.Status, strings.TrimSpace(string(body)))
		}
		if err := governancepack.CopyFile(stagePath, resp.Body); err != nil {
			return governancepack.TrustStore{}, "", err
		}
	} else {
		path := source
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			path = filepath.Join(path, "trusted-publishers.json")
		} else if err != nil {
			return governancepack.TrustStore{}, "", err
		}
		f, err := os.Open(path)
		if err != nil {
			return governancepack.TrustStore{}, "", err
		}
		defer f.Close()
		if err := governancepack.CopyFile(stagePath, f); err != nil {
			return governancepack.TrustStore{}, "", err
		}
	}
	store, err := governancepack.LoadTrustStore(stagePath)
	if err != nil {
		_ = os.Remove(stagePath)
		return governancepack.TrustStore{}, "", err
	}
	if err := governancepack.WriteStagedTrustRecord(governancepack.StagedTrustRecordPath(root), governancepack.StagedTrustRecord{
		SchemaVersion:  governancepack.StagedTrustRecordSchemaV1,
		Source:         source,
		FetchedAt:      time.Now().UTC().Format(time.RFC3339),
		PublisherCount: len(store.Publishers),
	}); err != nil {
		_ = os.Remove(stagePath)
		return governancepack.TrustStore{}, "", err
	}
	return store, stagePath, nil
}
