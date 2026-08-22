// SPDX-License-Identifier: AGPL-3.0-only

package modelexec

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"sort"
	"strings"
)

// requestIdentity is the SEMANTIC projection of a Request: everything that can
// change what the provider is asked to decide, and nothing else.
//
// Credentials, hostnames, temporary paths and wall-clock timings are absent by
// construction rather than by filtering. Smuggling them into semantic identity
// would make two identical questions look like different ones, which quietly
// destroys the only thing this digest is for.
type requestIdentity struct {
	SchemaVersion                string             `json:"schema_version"`
	RepositoryDomain             string             `json:"repository_domain"`
	RepositoryRevision           string             `json:"repository_revision"`
	RepositoryTree               string             `json:"repository_tree"`
	GraphDigestSHA256            string             `json:"graph_digest_sha256"`
	GraphStatus                  string             `json:"graph_status"`
	PolicyProfileID              string             `json:"policy_profile_id"`
	HowDocumentDigestSHA256      string             `json:"how_document_digest_sha256"`
	EvidenceSnapshotDigestSHA256 string             `json:"evidence_snapshot_digest_sha256"`
	TargetObservationIDs         []string           `json:"target_observation_ids"`
	TargetEvidenceIDs            []string           `json:"target_evidence_ids"`
	QueryDigestSHA256            string             `json:"query_digest_sha256"`
	SuppliedEvidence             []evidenceIdentity `json:"supplied_evidence"`
	ProviderID                   string             `json:"provider_id"`
	ProviderVersion              string             `json:"provider_version"`
	ModelName                    string             `json:"model_name"`
	ModelDigestSHA256            string             `json:"model_digest_sha256"`
	ModelDigestAbsent            bool               `json:"model_digest_absent"`
	PromptContractDigest         string             `json:"prompt_contract_digest"`
	OutputSchemaVersion          string             `json:"output_schema_version"`
	ToolPolicy                   string             `json:"tool_policy"`
}

// evidenceIdentity binds each supplied excerpt by its ID, the caller's declared
// digest, AND a digest computed over the excerpt bytes themselves.
//
// The computed digest is the load-bearing one. A caller-supplied digest is a
// claim about content, not the content: changing an excerpt while leaving its
// ID and declared digest alone would send the model different material under
// an identical request identity, so the terminal binding would content-address
// a request that was never sent.
type evidenceIdentity struct {
	ID                   string `json:"id"`
	DeclaredDigestSHA256 string `json:"declared_digest_sha256"`
	ExcerptDigestSHA256  string `json:"excerpt_digest_sha256"`
	// FilePath is part of the identity because it is part of what the model is
	// shown AND what its output is allowed to claim: the adapter sends it in
	// the prompt, and artifact scope is checked against it. The same excerpt
	// attributed to a.go and to b.go is a different question with a different
	// permitted answer, so it must not share one request identity.
	FilePath string `json:"file_path"`
}

// RequestDigest content-addresses the exact question. It is computed BEFORE
// invocation and the terminal binding records this value: a digest derived
// later from a reconstruction would describe what we believe we asked rather
// than what we asked.
func RequestDigest(r Request) (string, error) {
	id := requestIdentity{
		SchemaVersion:                r.SchemaVersion,
		RepositoryDomain:             r.RepositoryDomain,
		RepositoryRevision:           r.RepositoryRevision,
		RepositoryTree:               r.RepositoryTree,
		GraphDigestSHA256:            r.GraphDigestSHA256,
		GraphStatus:                  r.GraphStatus,
		PolicyProfileID:              r.PolicyProfileID,
		HowDocumentDigestSHA256:      r.HowDocumentDigestSHA256,
		EvidenceSnapshotDigestSHA256: r.EvidenceSnapshotDigestSHA256,
		TargetObservationIDs:         sortedCopy(r.TargetObservationIDs),
		TargetEvidenceIDs:            sortedCopy(r.TargetEvidenceIDs),
		QueryDigestSHA256:            r.QueryDigestSHA256,
		ProviderID:                   r.Provider.ID,
		ProviderVersion:              r.Provider.Version,
		ModelName:                    r.Model.Name,
		ModelDigestSHA256:            r.Model.DigestSHA256,
		ModelDigestAbsent:            r.Model.DigestAbsent,
		PromptContractDigest:         r.PromptContractDigest,
		OutputSchemaVersion:          r.OutputSchemaVersion,
		ToolPolicy:                   r.ToolPolicy,
	}
	for _, e := range r.SuppliedEvidence {
		sum := sha256.Sum256([]byte(e.Excerpt))
		id.SuppliedEvidence = append(id.SuppliedEvidence, evidenceIdentity{
			ID:                   e.ID,
			DeclaredDigestSHA256: e.DigestSHA256,
			ExcerptDigestSHA256:  hex.EncodeToString(sum[:]),
			FilePath:             filepath.ToSlash(strings.TrimSpace(e.FilePath)),
		})
	}
	sort.Slice(id.SuppliedEvidence, func(i, j int) bool { return id.SuppliedEvidence[i].ID < id.SuppliedEvidence[j].ID })
	data, err := json.Marshal(id)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func sortedCopy(in []string) []string {
	out := append([]string{}, in...)
	sort.Strings(out)
	if out == nil {
		out = []string{}
	}
	return out
}

// suppliedFilePaths is the exact set of files the model was shown. An artifact
// may attribute a claim only to these: a path it was never given is a claim
// about material the model did not see.
func suppliedFilePaths(r Request) map[string]bool {
	out := map[string]bool{}
	for _, e := range r.SuppliedEvidence {
		if p := strings.TrimSpace(e.FilePath); p != "" {
			out[filepath.ToSlash(p)] = true
		}
	}
	return out
}

// suppliedIDs is the exact set the model was permitted to cite.
func suppliedIDs(r Request) map[string]bool {
	out := map[string]bool{}
	for _, e := range r.SuppliedEvidence {
		if id := strings.TrimSpace(e.ID); id != "" {
			out[id] = true
		}
	}
	return out
}
