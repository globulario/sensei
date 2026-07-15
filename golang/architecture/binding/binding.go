// SPDX-License-Identifier: Apache-2.0

package binding

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"gopkg.in/yaml.v3"
)

const DefaultGraphSchemaVersion = "awareness-ontology/0.2"

type Base = closureprotocol.BaseBinding
type Result = closureprotocol.ResultBinding
type RuntimeTarget = closureprotocol.RuntimeTarget
type PolicyBinding = closureprotocol.PolicyBinding

type ResolveBaseOptions struct {
	RepoRoot           string
	RepositoryDomain   string
	GraphPath          string
	TaskID             string
	SessionID          string
	IterationDigest    string
	Policies           closureprotocol.PolicyBinding
	GraphSchemaVersion string
}

func ResolveBase(opts ResolveBaseOptions) (closureprotocol.BaseBinding, error) {
	root, err := filepath.Abs(strings.TrimSpace(opts.RepoRoot))
	if err != nil {
		return closureprotocol.BaseBinding{}, fmt.Errorf("resolve repo root: %w", err)
	}
	domain := strings.TrimSpace(opts.RepositoryDomain)
	if domain == "" {
		return closureprotocol.BaseBinding{}, fmt.Errorf("repository domain is required")
	}
	revision, revisionStatus, _ := architecture.ResolveRevision(root, true)
	if revisionStatus != architecture.RevisionResolved || strings.TrimSpace(revision) == "" {
		return closureprotocol.BaseBinding{}, fmt.Errorf("repository revision must be resolved: %s", revisionStatus)
	}
	treeDigest, err := RepositoryTreeDigestSHA256(root, revision)
	if err != nil {
		return closureprotocol.BaseBinding{}, err
	}
	graphDigest, err := GraphDigestSHA256(opts.GraphPath)
	if err != nil {
		return closureprotocol.BaseBinding{}, err
	}
	graphSchemaVersion := strings.TrimSpace(opts.GraphSchemaVersion)
	if graphSchemaVersion == "" {
		graphSchemaVersion = DefaultGraphSchemaVersion
	}
	base := closureprotocol.BaseBinding{
		Repository: closureprotocol.RepositorySnapshot{
			Domain:           domain,
			Revision:         revision,
			RevisionStatus:   architecture.RevisionResolved,
			TreeDigestSHA256: treeDigest,
		},
		Graph: closureprotocol.GraphSnapshot{
			DigestSHA256:  graphDigest,
			DigestStatus:  architecture.GraphDigestResolved,
			SchemaVersion: graphSchemaVersion,
		},
		Task: closureprotocol.TaskBinding{
			ID:                    strings.TrimSpace(opts.TaskID),
			SessionID:             strings.TrimSpace(opts.SessionID),
			IterationDigestSHA256: strings.TrimSpace(opts.IterationDigest),
		},
		Policies: canonicalPolicyBinding(opts.Policies),
	}
	if err := ValidateBase(base); err != nil {
		return closureprotocol.BaseBinding{}, err
	}
	return base, nil
}

func RepositoryTreeDigestSHA256(root, revision string) (string, error) {
	if strings.TrimSpace(root) == "" || strings.TrimSpace(revision) == "" {
		return "", fmt.Errorf("repository root and revision are required")
	}
	cmd := exec.Command("git", "-C", root, "ls-tree", "-r", "-z", "--full-tree", revision)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git ls-tree %s: %w", revision, err)
	}
	sum := sha256.Sum256(out)
	return hex.EncodeToString(sum[:]), nil
}

func GraphDigestSHA256(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("graph path is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read graph snapshot: %w", err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func ValidateBase(in closureprotocol.BaseBinding) error {
	in.Policies = canonicalPolicyBinding(in.Policies)
	if err := closureprotocol.ValidateBaseBinding(in); err != nil {
		return err
	}
	if strings.TrimSpace(in.Repository.TreeDigestSHA256) == "" {
		return fmt.Errorf("repository tree_digest_sha256 is required")
	}
	if strings.TrimSpace(in.Graph.SchemaVersion) == "" {
		return fmt.Errorf("graph schema_version is required")
	}
	if strings.TrimSpace(in.Policies.Admission) == "" ||
		strings.TrimSpace(in.Policies.Certification) == "" ||
		strings.TrimSpace(in.Policies.Completion) == "" ||
		strings.TrimSpace(in.Policies.Revocation) == "" ||
		strings.TrimSpace(in.Policies.Ledger) == "" ||
		strings.TrimSpace(in.Policies.Canonicalization) == "" {
		return fmt.Errorf("policy binding is incomplete")
	}
	return nil
}

func ValidateResult(in closureprotocol.ResultBinding) error {
	if strings.TrimSpace(in.BaseRevision) == "" {
		return fmt.Errorf("base_revision is required")
	}
	if strings.TrimSpace(in.PatchDigestSHA256) == "" {
		return fmt.Errorf("patch_digest_sha256 is required")
	}
	if strings.TrimSpace(in.ResultTreeDigestSHA256) == "" {
		return fmt.Errorf("result_tree_digest_sha256 is required")
	}
	if strings.TrimSpace(in.GraphDigestSHA256) == "" {
		return fmt.Errorf("graph_digest_sha256 is required")
	}
	for _, artifact := range in.GeneratedArtifacts {
		path := filepath.ToSlash(strings.TrimSpace(artifact.Path))
		if path == "" {
			return fmt.Errorf("generated artifact path is required")
		}
		if strings.HasPrefix(path, "/") || path == "." || path == ".." || strings.HasPrefix(path, "../") || strings.Contains(path, "/../") {
			return fmt.Errorf("generated artifact path must be repository-relative: %s", artifact.Path)
		}
		if strings.TrimSpace(artifact.DigestSHA256) == "" {
			return fmt.Errorf("generated artifact digest is required for %s", artifact.Path)
		}
	}
	return nil
}

func CompareBaseAndResult(base closureprotocol.BaseBinding, result closureprotocol.ResultBinding) error {
	if err := ValidateBase(base); err != nil {
		return err
	}
	if err := ValidateResult(result); err != nil {
		return err
	}
	if base.Repository.Revision != "" && result.BaseRevision != base.Repository.Revision {
		return fmt.Errorf("result base_revision %q does not match base binding revision %q", result.BaseRevision, base.Repository.Revision)
	}
	return nil
}

func SemanticDigestBase(in closureprotocol.BaseBinding) (string, error) {
	if err := ValidateBase(in); err != nil {
		return "", err
	}
	return closureprotocol.SemanticDigest(in)
}

func SemanticDigestResult(in closureprotocol.ResultBinding) (string, error) {
	if err := ValidateResult(in); err != nil {
		return "", err
	}
	return closureprotocol.SemanticDigest(in)
}

func ToClaimDocumentBinding(base closureprotocol.BaseBinding) architecture.ClaimDocumentBinding {
	return architecture.ClaimDocumentBinding{
		RepositoryDomain:  base.Repository.Domain,
		Revision:          base.Repository.Revision,
		RevisionStatus:    base.Repository.RevisionStatus,
		GraphDigestSHA256: base.Graph.DigestSHA256,
		GraphDigestStatus: base.Graph.DigestStatus,
	}
}

func FromClaimDocumentBinding(in architecture.ClaimDocumentBinding, taskID, sessionID, iterationDigest string, policies closureprotocol.PolicyBinding) (closureprotocol.BaseBinding, error) {
	base := closureprotocol.BaseBinding{
		Repository: closureprotocol.RepositorySnapshot{
			Domain:         strings.TrimSpace(in.RepositoryDomain),
			Revision:       strings.TrimSpace(in.Revision),
			RevisionStatus: strings.TrimSpace(in.RevisionStatus),
		},
		Graph: closureprotocol.GraphSnapshot{
			DigestSHA256:  strings.TrimSpace(in.GraphDigestSHA256),
			DigestStatus:  strings.TrimSpace(in.GraphDigestStatus),
			SchemaVersion: DefaultGraphSchemaVersion,
		},
		Task: closureprotocol.TaskBinding{
			ID:                    strings.TrimSpace(taskID),
			SessionID:             strings.TrimSpace(sessionID),
			IterationDigestSHA256: strings.TrimSpace(iterationDigest),
		},
		Policies: canonicalPolicyBinding(policies),
	}
	if err := closureprotocol.ValidateBaseBinding(base); err != nil {
		return closureprotocol.BaseBinding{}, err
	}
	return base, nil
}

func canonicalPolicyBinding(in closureprotocol.PolicyBinding) closureprotocol.PolicyBinding {
	in.Admission = strings.TrimSpace(in.Admission)
	in.Certification = strings.TrimSpace(in.Certification)
	in.Completion = strings.TrimSpace(in.Completion)
	in.Revocation = strings.TrimSpace(in.Revocation)
	in.Ledger = strings.TrimSpace(in.Ledger)
	in.Canonicalization = strings.TrimSpace(in.Canonicalization)
	return in
}

func CanonicalYAML(v any) ([]byte, error) {
	jsonData, err := closureprotocol.CanonicalJSON(v)
	if err != nil {
		return nil, err
	}
	var value any
	if err := yaml.Unmarshal(jsonData, &value); err != nil {
		return nil, err
	}
	data, err := yaml.Marshal(value)
	if err != nil {
		return nil, err
	}
	return bytes.TrimLeft(data, "\n"), nil
}
