// SPDX-License-Identifier: Apache-2.0

package governancepack

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/globulario/sensei/golang/extractor"
	"github.com/globulario/sensei/golang/seedmeta"
	"github.com/globulario/sensei/golang/statedir"
)

const (
	ManifestSchemaV1     = "awg.governance-pack.v1"
	ActiveRecordSchemaV1 = "awg.active-governance.v1"
	TrustStoreSchemaV1   = "awg.trusted-publishers.v1"
)

const (
	FailureSignatureInvalid     = "signature_invalid"
	FailureManifestInvalid      = "manifest_invalid"
	FailurePayloadInvalid       = "payload_invalid"
	FailureCompatibilityBlocked = "compatibility_blocked"
	FailureActivationIncomplete = "activation_incomplete"
	FailureGraphStale           = "graph_stale"
	FailureGraphUnknown         = "graph_unknown"
	FailureGraphDown            = "graph_down"
	FailureActivePackMissing    = "active_pack_missing"
)

var (
	ErrManifestInvalid      = errors.New(FailureManifestInvalid)
	ErrSignatureInvalid     = errors.New(FailureSignatureInvalid)
	ErrPayloadInvalid       = errors.New(FailurePayloadInvalid)
	ErrCompatibilityBlocked = errors.New(FailureCompatibilityBlocked)
	ErrUnknownPublisher     = errors.New("unknown_publisher_key")
	ErrTrustStoreInvalid    = errors.New("trust_store_invalid")
	ErrKeyRevoked           = errors.New("key_revoked")
	ErrKeyNotYetValid       = errors.New("key_not_yet_valid")
	ErrKeyExpired           = errors.New("key_expired")
)

type Manifest struct {
	SchemaVersion string `json:"schema_version"`
	PackID        string `json:"pack_id"`
	PackVersion   string `json:"pack_version"`
	Publisher     struct {
		ID          string `json:"id"`
		DisplayName string `json:"display_name"`
	} `json:"publisher"`
	IssuedAt string `json:"issued_at"`
	Payload  struct {
		Format       string `json:"format"`
		Path         string `json:"path"`
		DigestSHA256 string `json:"digest_sha256"`
		TripleCount  int64  `json:"triple_count"`
		MarkerIRI    string `json:"marker_iri"`
	} `json:"payload"`
	Compatibility struct {
		MinAWGVersion  string   `json:"min_awg_version"`
		MaxAWGVersion  string   `json:"max_awg_version"`
		SchemaVersions []string `json:"schema_versions"`
	} `json:"compatibility"`
	Source struct {
		CorpusDigestSHA256 string `json:"corpus_digest_sha256"`
		PromotionBatchID   string `json:"promotion_batch_id"`
	} `json:"source"`
	Signature struct {
		Algorithm string `json:"algorithm"`
		KeyID     string `json:"key_id"`
		SigPath   string `json:"sig_path"`
	} `json:"signature"`
}

type TrustStore struct {
	SchemaVersion string             `json:"schema_version"`
	Publishers    []TrustedPublisher `json:"publishers"`
}

type TrustedPublisher struct {
	PublisherID string       `json:"publisher_id"`
	DisplayName string       `json:"display_name,omitempty"`
	Keys        []TrustedKey `json:"keys"`
}

type TrustedKey struct {
	KeyID           string `json:"key_id"`
	Algorithm       string `json:"algorithm"`
	PublicKeyBase64 string `json:"public_key_base64"`
	Status          string `json:"status,omitempty"`
	ValidFrom       string `json:"valid_from,omitempty"`
	ValidUntil      string `json:"valid_until,omitempty"`
	Replaces        string `json:"replaces,omitempty"`
}

type BundlePaths struct {
	Dir           string
	ManifestPath  string
	SignaturePath string
	PayloadPath   string
}

type VerifiedPack struct {
	Manifest             Manifest
	Paths                BundlePaths
	ManifestBytes        []byte
	ManifestDigestSHA256 string
	SignatureBytes       []byte
	PayloadBytes         []byte
	PayloadMarker        seedmeta.Marker
	PublisherKey         TrustedKey
	TrustWarning         string
}

func TrustedKeysPath(root string) string {
	return statedir.Path(root, "governance", "trusted-publishers.json")
}

func (m Manifest) Validate() error {
	switch {
	case strings.TrimSpace(m.SchemaVersion) == "":
		return fmt.Errorf("%w: schema_version is required", ErrManifestInvalid)
	case m.SchemaVersion != ManifestSchemaV1:
		return fmt.Errorf("%w: unsupported schema_version %q", ErrManifestInvalid, m.SchemaVersion)
	case strings.TrimSpace(m.PackID) == "":
		return fmt.Errorf("%w: pack_id is required", ErrManifestInvalid)
	case strings.TrimSpace(m.PackVersion) == "":
		return fmt.Errorf("%w: pack_version is required", ErrManifestInvalid)
	case strings.TrimSpace(m.Publisher.ID) == "":
		return fmt.Errorf("%w: publisher.id is required", ErrManifestInvalid)
	case strings.TrimSpace(m.IssuedAt) == "":
		return fmt.Errorf("%w: issued_at is required", ErrManifestInvalid)
	case strings.TrimSpace(m.Payload.Format) == "":
		return fmt.Errorf("%w: payload.format is required", ErrManifestInvalid)
	case strings.TrimSpace(m.Payload.DigestSHA256) == "":
		return fmt.Errorf("%w: payload.digest_sha256 is required", ErrManifestInvalid)
	case m.Payload.TripleCount <= 0:
		return fmt.Errorf("%w: payload.triple_count must be positive", ErrManifestInvalid)
	case strings.TrimSpace(m.Payload.MarkerIRI) == "":
		return fmt.Errorf("%w: payload.marker_iri is required", ErrManifestInvalid)
	case strings.TrimSpace(m.Compatibility.MinAWGVersion) == "":
		return fmt.Errorf("%w: compatibility.min_awg_version is required", ErrManifestInvalid)
	case strings.TrimSpace(m.Signature.Algorithm) == "":
		return fmt.Errorf("%w: signature.algorithm is required", ErrManifestInvalid)
	case strings.TrimSpace(m.Signature.KeyID) == "":
		return fmt.Errorf("%w: signature.key_id is required", ErrManifestInvalid)
	}
	return nil
}

func ResolveBundlePaths(pathOrDir string) (BundlePaths, error) {
	resolved, err := filepath.Abs(strings.TrimSpace(pathOrDir))
	if err != nil {
		return BundlePaths{}, err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return BundlePaths{}, err
	}
	dir := resolved
	manifest := ""
	if info.IsDir() {
		manifest = filepath.Join(resolved, "governance-pack.manifest.json")
	} else if filepath.Base(resolved) == "governance-pack.manifest.json" {
		manifest = resolved
		dir = filepath.Dir(resolved)
	} else {
		return BundlePaths{}, fmt.Errorf("expected pack directory or governance-pack.manifest.json")
	}
	manifestBytes, err := os.ReadFile(manifest)
	if err != nil {
		return BundlePaths{}, err
	}
	var m Manifest
	if err := json.Unmarshal(manifestBytes, &m); err != nil {
		return BundlePaths{}, fmt.Errorf("%w: decode governance pack manifest: %v", ErrManifestInvalid, err)
	}
	if err := m.Validate(); err != nil {
		return BundlePaths{}, err
	}
	payloadPath := filepath.Join(dir, filepath.Clean(m.Payload.Path))
	if !filepath.IsAbs(m.Payload.Path) && !strings.HasPrefix(payloadPath, dir) {
		return BundlePaths{}, fmt.Errorf("%w: payload path escapes pack dir", ErrManifestInvalid)
	}
	sigName := strings.TrimSpace(m.Signature.SigPath)
	if sigName == "" {
		sigName = "governance-pack.manifest.sig"
	}
	sigPath := filepath.Join(dir, filepath.Clean(sigName))
	if !filepath.IsAbs(sigName) && !strings.HasPrefix(sigPath, dir) {
		return BundlePaths{}, fmt.Errorf("%w: signature path escapes pack dir", ErrManifestInvalid)
	}
	return BundlePaths{
		Dir:           dir,
		ManifestPath:  manifest,
		SignaturePath: sigPath,
		PayloadPath:   payloadPath,
	}, nil
}

func ReadManifest(path string) (Manifest, []byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, nil, err
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return Manifest{}, nil, fmt.Errorf("%w: decode governance pack manifest: %v", ErrManifestInvalid, err)
	}
	if err := manifest.Validate(); err != nil {
		return Manifest{}, nil, err
	}
	return manifest, data, nil
}

func LoadTrustStore(path string) (TrustStore, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return TrustStore{}, err
	}
	var store TrustStore
	if err := json.Unmarshal(data, &store); err != nil {
		return TrustStore{}, fmt.Errorf("decode trusted publisher key set: %w", err)
	}
	if err := store.Validate(); err != nil {
		return TrustStore{}, err
	}
	return store, nil
}

func (s TrustStore) Validate() error {
	if strings.TrimSpace(s.SchemaVersion) == "" {
		return fmt.Errorf("%w: schema_version is required", ErrTrustStoreInvalid)
	}
	if strings.TrimSpace(s.SchemaVersion) != TrustStoreSchemaV1 {
		return fmt.Errorf("%w: unsupported schema_version %q", ErrTrustStoreInvalid, s.SchemaVersion)
	}
	for i, p := range s.Publishers {
		if strings.TrimSpace(p.PublisherID) == "" {
			return fmt.Errorf("%w: publishers[%d].publisher_id is required", ErrTrustStoreInvalid, i)
		}
		if len(p.Keys) == 0 {
			return fmt.Errorf("%w: publishers[%d] must contain at least one key", ErrTrustStoreInvalid, i)
		}
		for j, k := range p.Keys {
			if strings.TrimSpace(k.KeyID) == "" {
				return fmt.Errorf("%w: publishers[%d].keys[%d].key_id is required", ErrTrustStoreInvalid, i, j)
			}
			if strings.TrimSpace(k.Algorithm) == "" {
				return fmt.Errorf("%w: publishers[%d].keys[%d].algorithm is required", ErrTrustStoreInvalid, i, j)
			}
			if strings.TrimSpace(k.PublicKeyBase64) == "" {
				return fmt.Errorf("%w: publishers[%d].keys[%d].public_key_base64 is required", ErrTrustStoreInvalid, i, j)
			}
			if status := normalizedKeyStatus(k.Status); status == "" {
				return fmt.Errorf("%w: publishers[%d].keys[%d].status %q is invalid", ErrTrustStoreInvalid, i, j, k.Status)
			}
			if k.ValidFrom != "" {
				if _, err := time.Parse(time.RFC3339, k.ValidFrom); err != nil {
					return fmt.Errorf("%w: publishers[%d].keys[%d].valid_from invalid: %v", ErrTrustStoreInvalid, i, j, err)
				}
			}
			if k.ValidUntil != "" {
				if _, err := time.Parse(time.RFC3339, k.ValidUntil); err != nil {
					return fmt.Errorf("%w: publishers[%d].keys[%d].valid_until invalid: %v", ErrTrustStoreInvalid, i, j, err)
				}
			}
		}
	}
	return nil
}

func VerifyPack(pathOrDir, trustedKeysPath, currentVersion string) (VerifiedPack, error) {
	paths, err := ResolveBundlePaths(pathOrDir)
	if err != nil {
		return VerifiedPack{}, err
	}
	manifest, manifestBytes, err := ReadManifest(paths.ManifestPath)
	if err != nil {
		return VerifiedPack{}, err
	}
	store, err := LoadTrustStore(trustedKeysPath)
	if err != nil {
		return VerifiedPack{}, err
	}
	key, warning, err := lookupTrustedKey(store, manifest.Publisher.ID, manifest.Signature.KeyID, manifest.Signature.Algorithm, time.Now().UTC())
	if err != nil {
		return VerifiedPack{}, err
	}
	sigBytes, err := readSignature(paths.SignaturePath)
	if err != nil {
		return VerifiedPack{}, err
	}
	if err := verifyManifestSignature(manifestBytes, sigBytes, key); err != nil {
		return VerifiedPack{}, err
	}
	if err := verifyCompatibility(manifest, currentVersion); err != nil {
		return VerifiedPack{}, err
	}
	payloadBytes, marker, err := verifyPayload(paths.PayloadPath, manifest)
	if err != nil {
		return VerifiedPack{}, err
	}
	sum := sha256.Sum256(manifestBytes)
	return VerifiedPack{
		Manifest:             manifest,
		Paths:                paths,
		ManifestBytes:        manifestBytes,
		ManifestDigestSHA256: hex.EncodeToString(sum[:]),
		SignatureBytes:       sigBytes,
		PayloadBytes:         payloadBytes,
		PayloadMarker:        marker,
		PublisherKey:         key,
		TrustWarning:         warning,
	}, nil
}

func lookupTrustedKey(store TrustStore, publisherID, keyID, algorithm string, now time.Time) (TrustedKey, string, error) {
	for _, publisher := range store.Publishers {
		if strings.TrimSpace(publisher.PublisherID) != publisherID {
			continue
		}
		for _, key := range publisher.Keys {
			if strings.TrimSpace(key.KeyID) == keyID && strings.EqualFold(strings.TrimSpace(key.Algorithm), algorithm) {
				warning, err := validateTrustedKeyState(key, now)
				if err != nil {
					return TrustedKey{}, "", err
				}
				return key, warning, nil
			}
		}
		return TrustedKey{}, "", fmt.Errorf("%w: publisher %s has no trusted key %s/%s", ErrUnknownPublisher, publisherID, algorithm, keyID)
	}
	return TrustedKey{}, "", fmt.Errorf("%w: publisher %s not trusted", ErrUnknownPublisher, publisherID)
}

func validateTrustedKeyState(key TrustedKey, now time.Time) (string, error) {
	switch normalizedKeyStatus(key.Status) {
	case "revoked":
		return "", fmt.Errorf("%w: key %s is revoked", ErrKeyRevoked, key.KeyID)
	case "future":
		return "", fmt.Errorf("%w: key %s is not yet valid", ErrKeyNotYetValid, key.KeyID)
	}
	if key.ValidFrom != "" {
		ts, _ := time.Parse(time.RFC3339, key.ValidFrom)
		if now.Before(ts) {
			return "", fmt.Errorf("%w: key %s valid_from %s", ErrKeyNotYetValid, key.KeyID, key.ValidFrom)
		}
	}
	if key.ValidUntil != "" {
		ts, _ := time.Parse(time.RFC3339, key.ValidUntil)
		if now.After(ts) {
			return "", fmt.Errorf("%w: key %s expired at %s", ErrKeyExpired, key.KeyID, key.ValidUntil)
		}
	}
	if normalizedKeyStatus(key.Status) == "deprecated" {
		return "trusted by deprecated key", nil
	}
	return "", nil
}

func normalizedKeyStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "", "active":
		return "active"
	case "deprecated":
		return "deprecated"
	case "revoked":
		return "revoked"
	case "future":
		return "future"
	default:
		return ""
	}
}

func NormalizedKeyStatusForDisplay(status string) string {
	return normalizedKeyStatus(status)
}

func readSignature(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	raw := strings.TrimSpace(string(data))
	if raw == "" {
		return nil, fmt.Errorf("%w: empty signature file", ErrSignatureInvalid)
	}
	sig, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: decode signature: %v", ErrSignatureInvalid, err)
	}
	return sig, nil
}

func verifyManifestSignature(manifestBytes, sig []byte, key TrustedKey) error {
	if !strings.EqualFold(strings.TrimSpace(key.Algorithm), "ed25519") {
		return fmt.Errorf("%w: unsupported signature algorithm %q", ErrSignatureInvalid, key.Algorithm)
	}
	pubRaw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(key.PublicKeyBase64))
	if err != nil {
		return fmt.Errorf("%w: decode trusted public key: %v", ErrSignatureInvalid, err)
	}
	pub := ed25519.PublicKey(pubRaw)
	if len(pub) != ed25519.PublicKeySize {
		return fmt.Errorf("%w: trusted public key has invalid size", ErrSignatureInvalid)
	}
	if !ed25519.Verify(pub, manifestBytes, sig) {
		return fmt.Errorf("%w: manifest signature verification failed", ErrSignatureInvalid)
	}
	return nil
}

func verifyCompatibility(manifest Manifest, currentVersion string) error {
	cur := normalizeVersion(currentVersion)
	min := normalizeVersion(manifest.Compatibility.MinAWGVersion)
	max := normalizeVersion(manifest.Compatibility.MaxAWGVersion)
	if min != "" && compareVersion(cur, min) < 0 {
		return fmt.Errorf("%w: current AWG version %s is below min %s", ErrCompatibilityBlocked, currentVersion, manifest.Compatibility.MinAWGVersion)
	}
	if max != "" && compareVersion(cur, max) > 0 {
		return fmt.Errorf("%w: current AWG version %s is above max %s", ErrCompatibilityBlocked, currentVersion, manifest.Compatibility.MaxAWGVersion)
	}
	if len(manifest.Compatibility.SchemaVersions) > 0 {
		ok := false
		for _, v := range manifest.Compatibility.SchemaVersions {
			if strings.TrimSpace(v) == ManifestSchemaV1 {
				ok = true
				break
			}
		}
		if !ok {
			return fmt.Errorf("%w: schema version %s not allowed by manifest compatibility", ErrCompatibilityBlocked, ManifestSchemaV1)
		}
	}
	return nil
}

func verifyPayload(path string, manifest Manifest) ([]byte, seedmeta.Marker, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, seedmeta.Marker{}, err
	}
	if !strings.EqualFold(strings.TrimSpace(manifest.Payload.Format), "ntriples") {
		return nil, seedmeta.Marker{}, fmt.Errorf("%w: unsupported payload format %q", ErrPayloadInvalid, manifest.Payload.Format)
	}
	if errs := extractor.ValidateNTriples(strings.NewReader(string(data))); len(errs) > 0 {
		return nil, seedmeta.Marker{}, fmt.Errorf("%w: payload has %d N-Triples validation error(s)", ErrPayloadInvalid, len(errs))
	}
	marker, ok := seedmeta.ParseMarker(data)
	if !ok {
		return nil, seedmeta.Marker{}, fmt.Errorf("%w: payload missing graph marker", ErrPayloadInvalid)
	}
	if marker.Digest != manifest.Payload.DigestSHA256 {
		return nil, seedmeta.Marker{}, fmt.Errorf("%w: payload digest %s != manifest %s", ErrPayloadInvalid, marker.Digest, manifest.Payload.DigestSHA256)
	}
	if marker.IRI != manifest.Payload.MarkerIRI {
		return nil, seedmeta.Marker{}, fmt.Errorf("%w: payload marker %s != manifest %s", ErrPayloadInvalid, marker.IRI, manifest.Payload.MarkerIRI)
	}
	if marker.TripleCount != manifest.Payload.TripleCount {
		return nil, seedmeta.Marker{}, fmt.Errorf("%w: payload triple count %d != manifest %d", ErrPayloadInvalid, marker.TripleCount, manifest.Payload.TripleCount)
	}
	return data, marker, nil
}

func normalizeVersion(v string) string {
	v = strings.TrimSpace(strings.TrimPrefix(v, "v"))
	if i := strings.IndexByte(v, '-'); i >= 0 {
		v = v[:i]
	}
	return v
}

func compareVersion(a, b string) int {
	if a == b {
		return 0
	}
	as := splitVersion(a)
	bs := splitVersion(b)
	n := len(as)
	if len(bs) > n {
		n = len(bs)
	}
	for i := 0; i < n; i++ {
		var av, bv int
		if i < len(as) {
			av = as[i]
		}
		if i < len(bs) {
			bv = bs[i]
		}
		if av < bv {
			return -1
		}
		if av > bv {
			return 1
		}
	}
	return 0
}

func splitVersion(v string) []int {
	parts := strings.Split(strings.TrimSpace(v), ".")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		n := 0
		for _, ch := range p {
			if ch < '0' || ch > '9' {
				break
			}
			n = n*10 + int(ch-'0')
		}
		out = append(out, n)
	}
	return out
}
