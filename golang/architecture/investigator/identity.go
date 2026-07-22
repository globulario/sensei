// SPDX-License-Identifier: AGPL-3.0-only

package investigator

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/globulario/sensei/golang/architecture"
)

const candidateIDDigestPrefixLength = 24

// CandidateIdentityDescriptor defines the exact inputs for candidate ID generation.
type CandidateIdentityDescriptor struct {
	SchemaVersion          string                  `json:"schema_version"`
	RepositoryDomain       string                  `json:"repository_domain"`
	Revision               string                  `json:"revision"`
	Proposition            string                  `json:"proposition"`
	Scope                  architecture.ClaimScope `json:"scope"`
	CandidateKind          CandidateKind           `json:"candidate_kind"`
	GeneratorVersion       string                  `json:"generator_version"`
	GraphDigest            string                  `json:"graph_digest"`
	EvidenceSnapshotDigest string                  `json:"evidence_snapshot_digest"`
}

// ComputeCandidateID returns the stable ID and its full SHA256 digest.
func ComputeCandidateID(schemaVersion string, binding Binding, proposition string, scope architecture.ClaimScope, kind CandidateKind, genVersion string) (string, string, error) {
	sortedFiles := append([]string(nil), scope.Files...)
	sort.Strings(sortedFiles)
	sortedSymbols := append([]string(nil), scope.Symbols...)
	sort.Strings(sortedSymbols)
	sortedComponents := append([]string(nil), scope.Components...)
	sort.Strings(sortedComponents)

	normalizedScope := architecture.ClaimScope{
		Repository: strings.TrimSpace(scope.Repository),
		Repo:       strings.TrimSpace(scope.Repo),
		Domain:     strings.TrimSpace(scope.Domain),
		SourceSet:  strings.TrimSpace(scope.SourceSet),
		Files:      sortedFiles,
		Symbols:    sortedSymbols,
		Components: sortedComponents,
	}

	descriptor := CandidateIdentityDescriptor{
		SchemaVersion:          strings.TrimSpace(schemaVersion),
		RepositoryDomain:       strings.TrimSpace(binding.Repository.RepositoryDomain),
		Revision:               strings.TrimSpace(binding.Repository.Revision),
		Proposition:            strings.TrimSpace(proposition),
		Scope:                  normalizedScope,
		CandidateKind:          kind,
		GeneratorVersion:       strings.TrimSpace(genVersion),
		GraphDigest:            strings.TrimSpace(binding.GraphDigestSHA256),
		EvidenceSnapshotDigest: strings.TrimSpace(binding.EvidenceSnapshotDigestSHA256),
	}

	bytes, err := json.Marshal(descriptor)
	if err != nil {
		return "", "", fmt.Errorf("marshal candidate descriptor failed: %w", err)
	}

	hash := sha256.Sum256(bytes)
	digest := hex.EncodeToString(hash[:])
	id := fmt.Sprintf("candidate_%s_%s", kind, digest[:candidateIDDigestPrefixLength])

	return id, digest, nil
}
