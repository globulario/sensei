// SPDX-License-Identifier: AGPL-3.0-only

package knowledgeadmission

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// AdmissionCorpusDigestForManifest derives the authored-corpus digest for the
// stable identities a manifest claims as governed.
//
// It deliberately does not establish authority and does not require a valid
// signature. Freezing needs this operation BEFORE a manifest can be signed.
// Verification authenticates the exact manifest bytes independently and then
// compares their recorded binding with the same digest.
//
// Both paths end in AdmissionCorpusDigest. There is no freezer-specific walker,
// normalization rule or path interpretation.
func AdmissionCorpusDigestForManifest(repoRoot string, manifestBytes []byte) (string, error) {
	var m Manifest
	if err := yaml.Unmarshal(manifestBytes, &m); err != nil {
		return "", fmt.Errorf("admission manifest: %w", err)
	}
	ids := governedIdentityClaims(m)
	if len(ids) == 0 {
		return "", fmt.Errorf("admission manifest has no governed identities")
	}
	return AdmissionCorpusDigest(repoRoot, ids)
}

// VerifyManifestCorpusBinding checks that every governed record in a manifest
// binds to the canonical authored corpus it names. It is intended as the
// pre-sign freeze guard. It authenticates nothing; signing and VerifySigned are
// the provenance boundary.
//
// The old valid_for_graph_digest field is accepted only as a migration spelling
// for a CORPUS digest while the v1 baseline is re-frozen. Its value is never
// interpreted as the digest of awareness.nt.
func VerifyManifestCorpusBinding(repoRoot string, manifestBytes []byte) (string, error) {
	var m Manifest
	if err := yaml.Unmarshal(manifestBytes, &m); err != nil {
		return "", fmt.Errorf("admission manifest: %w", err)
	}
	if strings.TrimSpace(m.SchemaVersion) != SchemaVersion {
		return "", fmt.Errorf("admission manifest schema_version %q is not supported for signing (want %s)", m.SchemaVersion, SchemaVersion)
	}
	digest, err := AdmissionCorpusDigest(repoRoot, governedIdentityClaims(m))
	if err != nil {
		return "", err
	}
	for _, raw := range m.Records {
		if Disposition(strings.ToLower(strings.TrimSpace(string(raw.Disposition)))) != DispositionGoverned {
			continue
		}
		r := raw
		r.Identity = strings.TrimSpace(r.Identity)
		r.Receipt.ValidForCorpusDigest = strings.ToLower(strings.TrimSpace(r.Receipt.ValidForCorpusDigest))
		r.Receipt.ValidForGraphDigest = strings.ToLower(strings.TrimSpace(r.Receipt.ValidForGraphDigest))
		if err := verifyGovernedRecord(r, digest); err != nil {
			return "", err
		}
	}
	return digest, nil
}
