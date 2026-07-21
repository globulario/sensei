// SPDX-License-Identifier: AGPL-3.0-only

package governancepack

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	SigningKeySchemaV1       = "awg.signing-key.v1"
	PublicationIndexSchemaV1 = "awg.governance-pack-index.v1"
)

type SigningKey struct {
	SchemaVersion string `json:"schema_version"`
	PublisherID   string `json:"publisher_id,omitempty"`
	KeyID         string `json:"key_id"`
	Algorithm     string `json:"algorithm"`
	PrivateKeyB64 string `json:"private_key_base64"`
}

type PublicationIndex struct {
	SchemaVersion string                    `json:"schema_version"`
	GeneratedAt   string                    `json:"generated_at"`
	Publisher     PublicationIndexPublisher `json:"publisher"`
	Signature     PublicationIndexSignature `json:"signature"`
	Packs         []PublicationPackIndex    `json:"packs"`
	Channels      []PublicationChannelRef   `json:"channels,omitempty"`
}

type PublicationIndexPublisher struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name,omitempty"`
}

type PublicationIndexSignature struct {
	Algorithm string `json:"algorithm"`
	KeyID     string `json:"key_id"`
	SigPath   string `json:"sig_path"`
}

type PublicationPackIndex struct {
	PackID   string                   `json:"pack_id"`
	Versions []PublicationPackVersion `json:"versions"`
}

type PublicationPackVersion struct {
	PackID               string `json:"pack_id,omitempty"`
	PackVersion          string `json:"pack_version"`
	PublisherID          string `json:"publisher_id"`
	PublisherDisplayName string `json:"publisher_display_name,omitempty"`
	IssuedAt             string `json:"issued_at"`
	ManifestDigestSHA256 string `json:"manifest_digest_sha256"`
	PayloadDigestSHA256  string `json:"payload_digest_sha256"`
	PayloadTripleCount   int64  `json:"payload_triple_count"`
	PayloadMarkerIRI     string `json:"payload_marker_iri"`
	MinAWGVersion        string `json:"min_awg_version"`
	MaxAWGVersion        string `json:"max_awg_version,omitempty"`
	ManifestPath         string `json:"manifest_path"`
	SignaturePath        string `json:"signature_path"`
	PayloadPath          string `json:"payload_path"`
}

type PublicationChannelRef struct {
	Channel     string `json:"channel"`
	PackID      string `json:"pack_id"`
	PackVersion string `json:"pack_version"`
}

func LoadSigningKey(path string) (SigningKey, ed25519.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return SigningKey{}, nil, err
	}
	var key SigningKey
	if err := json.Unmarshal(data, &key); err != nil {
		return SigningKey{}, nil, fmt.Errorf("decode signing key: %w", err)
	}
	if err := key.Validate(); err != nil {
		return SigningKey{}, nil, err
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(key.PrivateKeyB64))
	if err != nil {
		return SigningKey{}, nil, fmt.Errorf("decode signing key: %w", err)
	}
	priv := ed25519.PrivateKey(raw)
	if len(priv) != ed25519.PrivateKeySize {
		return SigningKey{}, nil, fmt.Errorf("signing key has invalid size")
	}
	return key, priv, nil
}

func (k SigningKey) Validate() error {
	switch {
	case strings.TrimSpace(k.SchemaVersion) == "":
		return fmt.Errorf("signing key schema_version is required")
	case strings.TrimSpace(k.SchemaVersion) != SigningKeySchemaV1:
		return fmt.Errorf("unsupported signing key schema_version %q", k.SchemaVersion)
	case strings.TrimSpace(k.KeyID) == "":
		return fmt.Errorf("signing key key_id is required")
	case !strings.EqualFold(strings.TrimSpace(k.Algorithm), "ed25519"):
		return fmt.Errorf("unsupported signing key algorithm %q", k.Algorithm)
	case strings.TrimSpace(k.PrivateKeyB64) == "":
		return fmt.Errorf("signing key private_key_base64 is required")
	}
	return nil
}

func PublicationRoot(root string) string {
	return filepath.Join(root, "governance")
}

func PublicationIndexPath(root string) string {
	return filepath.Join(PublicationRoot(root), "index.json")
}

func PublicationIndexSignaturePath(root string) string {
	return filepath.Join(PublicationRoot(root), "index.json.sig")
}

func ReadPublicationIndex(path string) (PublicationIndex, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return PublicationIndex{}, err
	}
	var index PublicationIndex
	if err := json.Unmarshal(data, &index); err != nil {
		return PublicationIndex{}, fmt.Errorf("decode publication index: %w", err)
	}
	if strings.TrimSpace(index.SchemaVersion) == "" {
		return PublicationIndex{}, fmt.Errorf("publication index schema_version is required")
	}
	if strings.TrimSpace(index.SchemaVersion) != PublicationIndexSchemaV1 {
		return PublicationIndex{}, fmt.Errorf("unsupported publication index schema_version %q", index.SchemaVersion)
	}
	if strings.TrimSpace(index.Publisher.ID) == "" {
		return PublicationIndex{}, fmt.Errorf("publication index publisher.id is required")
	}
	if strings.TrimSpace(index.Signature.Algorithm) == "" {
		return PublicationIndex{}, fmt.Errorf("publication index signature.algorithm is required")
	}
	if strings.TrimSpace(index.Signature.KeyID) == "" {
		return PublicationIndex{}, fmt.Errorf("publication index signature.key_id is required")
	}
	return index, nil
}

func ResolvePublicationTarget(index PublicationIndex, packID, packVersion, channel string) (PublicationPackVersion, error) {
	packID = strings.TrimSpace(packID)
	packVersion = strings.TrimSpace(packVersion)
	channel = strings.TrimSpace(channel)
	if packID == "" {
		return PublicationPackVersion{}, fmt.Errorf("pack id is required")
	}
	if packVersion == "" && channel == "" {
		return PublicationPackVersion{}, fmt.Errorf("one of pack version or channel is required")
	}
	if packVersion == "" {
		found := false
		for _, ref := range index.Channels {
			if ref.PackID == packID && ref.Channel == channel {
				packVersion = ref.PackVersion
				found = true
				break
			}
		}
		if !found {
			return PublicationPackVersion{}, fmt.Errorf("channel %s for pack %s not found", channel, packID)
		}
	}
	for _, pack := range index.Packs {
		if pack.PackID != packID {
			continue
		}
		for _, version := range pack.Versions {
			if version.PackVersion == packVersion {
				return version, nil
			}
		}
		return PublicationPackVersion{}, fmt.Errorf("pack %s version %s not found", packID, packVersion)
	}
	return PublicationPackVersion{}, fmt.Errorf("pack %s not found", packID)
}

func CopyFile(dst string, src io.Reader) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp := dst + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, src); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}

func BuildPublicationIndex(publishedRoot, trustedKeysPath, currentVersion string, channels []PublicationChannelRef) (PublicationIndex, error) {
	packsRoot := filepath.Join(PublicationRoot(publishedRoot), "packs")
	entries, err := os.ReadDir(packsRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return PublicationIndex{
				SchemaVersion: PublicationIndexSchemaV1,
				GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
				Publisher:     PublicationIndexPublisher{},
				Signature:     PublicationIndexSignature{Algorithm: "ed25519", SigPath: "index.json.sig"},
				Packs:         []PublicationPackIndex{},
				Channels:      normalizePublicationChannels(channels),
			}, nil
		}
		return PublicationIndex{}, err
	}
	var packs []PublicationPackIndex
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		packID := entry.Name()
		versionEntries, err := os.ReadDir(filepath.Join(packsRoot, packID))
		if err != nil {
			return PublicationIndex{}, err
		}
		var versions []PublicationPackVersion
		for _, versionEntry := range versionEntries {
			if !versionEntry.IsDir() {
				continue
			}
			packDir := filepath.Join(packsRoot, packID, versionEntry.Name())
			verified, err := VerifyPack(packDir, trustedKeysPath, currentVersion)
			if err != nil {
				return PublicationIndex{}, fmt.Errorf("verify published pack %s/%s: %w", packID, versionEntry.Name(), err)
			}
			versions = append(versions, PublicationPackVersion{
				PackID:               verified.Manifest.PackID,
				PackVersion:          verified.Manifest.PackVersion,
				PublisherID:          verified.Manifest.Publisher.ID,
				PublisherDisplayName: verified.Manifest.Publisher.DisplayName,
				IssuedAt:             verified.Manifest.IssuedAt,
				ManifestDigestSHA256: verified.ManifestDigestSHA256,
				PayloadDigestSHA256:  verified.PayloadMarker.Digest,
				PayloadTripleCount:   verified.PayloadMarker.TripleCount,
				PayloadMarkerIRI:     verified.PayloadMarker.IRI,
				MinAWGVersion:        verified.Manifest.Compatibility.MinAWGVersion,
				MaxAWGVersion:        verified.Manifest.Compatibility.MaxAWGVersion,
				ManifestPath:         filepath.ToSlash(filepath.Join("governance", "packs", packID, verified.Manifest.PackVersion, "governance-pack.manifest.json")),
				SignaturePath:        filepath.ToSlash(filepath.Join("governance", "packs", packID, verified.Manifest.PackVersion, "governance-pack.manifest.sig")),
				PayloadPath:          filepath.ToSlash(filepath.Join("governance", "packs", packID, verified.Manifest.PackVersion, "governance-pack.nt")),
			})
		}
		sort.Slice(versions, func(i, j int) bool {
			return versions[i].PackVersion < versions[j].PackVersion
		})
		packs = append(packs, PublicationPackIndex{
			PackID:   packID,
			Versions: versions,
		})
	}
	sort.Slice(packs, func(i, j int) bool {
		return packs[i].PackID < packs[j].PackID
	})
	return PublicationIndex{
		SchemaVersion: PublicationIndexSchemaV1,
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
		Publisher:     PublicationIndexPublisher{},
		Signature:     PublicationIndexSignature{Algorithm: "ed25519", SigPath: "index.json.sig"},
		Packs:         packs,
		Channels:      normalizePublicationChannels(channels),
	}, nil
}

func WritePublicationIndex(path string, index PublicationIndex) error {
	if strings.TrimSpace(index.SchemaVersion) == "" {
		index.SchemaVersion = PublicationIndexSchemaV1
	}
	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp := path + ".tmp"
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func SignPublicationIndex(indexPath string, index PublicationIndex, signingKey SigningKey, priv ed25519.PrivateKey) error {
	index.Publisher.ID = strings.TrimSpace(signingKey.PublisherID)
	index.Signature.Algorithm = "ed25519"
	index.Signature.KeyID = strings.TrimSpace(signingKey.KeyID)
	if strings.TrimSpace(index.Signature.SigPath) == "" {
		index.Signature.SigPath = "index.json.sig"
	}
	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.WriteFile(indexPath, data, 0o644); err != nil {
		return err
	}
	sig := ed25519.Sign(priv, data)
	return os.WriteFile(filepath.Join(filepath.Dir(indexPath), index.Signature.SigPath), []byte(base64.StdEncoding.EncodeToString(sig)+"\n"), 0o644)
}

func VerifyPublicationIndex(indexPath, trustedKeysPath string) (PublicationIndex, []byte, error) {
	index, err := ReadPublicationIndex(indexPath)
	if err != nil {
		return PublicationIndex{}, nil, err
	}
	data, err := os.ReadFile(indexPath)
	if err != nil {
		return PublicationIndex{}, nil, err
	}
	store, err := LoadTrustStore(trustedKeysPath)
	if err != nil {
		return PublicationIndex{}, nil, err
	}
	key, _, err := lookupTrustedKey(store, index.Publisher.ID, index.Signature.KeyID, index.Signature.Algorithm, time.Now().UTC())
	if err != nil {
		return PublicationIndex{}, nil, err
	}
	sigPath := filepath.Join(filepath.Dir(indexPath), strings.TrimSpace(index.Signature.SigPath))
	sig, err := readSignature(sigPath)
	if err != nil {
		return PublicationIndex{}, nil, err
	}
	if err := verifyManifestSignature(data, sig, key); err != nil {
		return PublicationIndex{}, nil, err
	}
	return index, data, nil
}

func normalizePublicationChannels(channels []PublicationChannelRef) []PublicationChannelRef {
	if len(channels) == 0 {
		return nil
	}
	out := make([]PublicationChannelRef, 0, len(channels))
	seen := map[string]PublicationChannelRef{}
	for _, channel := range channels {
		name := strings.TrimSpace(channel.Channel)
		if name == "" {
			continue
		}
		channel.Channel = name
		seen[name+"|"+channel.PackID] = channel
	}
	for _, channel := range seen {
		out = append(out, channel)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Channel == out[j].Channel {
			return out[i].PackID < out[j].PackID
		}
		return out[i].Channel < out[j].Channel
	})
	return out
}
