// SPDX-License-Identifier: Apache-2.0

package graphbuild

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

// GraphInputSnapshotSchemaVersion identifies the immutable graph-input snapshot
// shape.
const GraphInputSnapshotSchemaVersion = "graphbuild.graph-input-snapshot/v1"

// SupplementalGraphArtifactKeyPrefix is the closed namespace every supplemental
// graph's content-addressed task-artifact key lives under, so a supplemental
// reference can never collide with a core task artifact (closure_request,
// graph_input_snapshot, session, ...).
const SupplementalGraphArtifactKeyPrefix = "supplemental_graph."

// supplementalIDRE is the closed, deterministic supplemental-id grammar. It is
// lowercase so two ids can never differ only by case and then collide onto one
// artifact key.
var supplementalIDRE = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

// SupplementalGraphArtifactKey returns the content-addressed task-artifact key
// for a supplemental graph id. It refuses any id outside the closed grammar
// (uppercase, path separators, whitespace, control characters, "..", empty), so
// no two distinct ids are silently normalized to one key.
func SupplementalGraphArtifactKey(id string) (string, error) {
	id = strings.TrimSpace(id)
	if !supplementalIDRE.MatchString(id) || strings.Contains(id, "..") {
		return "", fmt.Errorf("graphbuild: supplemental graph id %q is not a valid lowercase id", id)
	}
	return SupplementalGraphArtifactKeyPrefix + id, nil
}

// LogicalSourceRoot is one governed source root named by a repository-relative
// logical path. It never carries an absolute path, a mutable active-pointer path,
// or a timestamp: it is resolved against a materialized base or result root by the
// graph-input policy.
type LogicalSourceRoot struct {
	LogicalPath         string `json:"logical_path" yaml:"logical_path"`
	DefaultDomain       string `json:"default_domain,omitempty" yaml:"default_domain,omitempty"`
	DefaultSourceSet    string `json:"default_source_set,omitempty" yaml:"default_source_set,omitempty"`
	SkipNestedGenerated bool   `json:"skip_nested_generated,omitempty" yaml:"skip_nested_generated,omitempty"`
}

// SupplementalGraphBinding names a verified, already-marked supplemental graph
// (e.g. a governance pack) by immutable identity: its id, version, semantic
// digest, and the content-addressed task-artifact key under which its exact bytes
// are stored. It is never resolved through a current active-pointer.
type SupplementalGraphBinding struct {
	ID                   string `json:"id" yaml:"id"`
	Version              string `json:"version" yaml:"version"`
	SemanticDigestSHA256 string `json:"semantic_digest_sha256" yaml:"semantic_digest_sha256"`
	ArtifactKey          string `json:"artifact_key" yaml:"artifact_key"`
}

// GraphInputSnapshot is the immutable, task-bound record of exactly which governed
// graph inputs admission observed: the source-root policy, the logical source
// roots, and the supplemental graphs (by immutable identity). Recomputing only
// the repository tree is not enough to reproduce the architecture graph; this
// snapshot pins the remaining ingredients. It carries no absolute path and no
// timestamp, and its identity is a self-excluding semantic digest.
type GraphInputSnapshot struct {
	SchemaVersion    string `json:"schema_version" yaml:"schema_version"`
	PolicyID         string `json:"policy_id" yaml:"policy_id"`
	RepositoryDomain string `json:"repository_domain" yaml:"repository_domain"`

	SourceRoots        []LogicalSourceRoot        `json:"source_roots" yaml:"source_roots"`
	SupplementalGraphs []SupplementalGraphBinding `json:"supplemental_graphs" yaml:"supplemental_graphs"`

	SnapshotDigestSHA256 string `json:"snapshot_digest_sha256,omitempty" yaml:"snapshot_digest_sha256,omitempty"`
}

// CanonicalizeGraphInputSnapshot trims and deterministically orders the snapshot's
// collections (source roots by logical path, supplemental graphs by id) and fails
// on a duplicate logical root or supplemental id, so identity is order- and
// whitespace-independent.
func CanonicalizeGraphInputSnapshot(in GraphInputSnapshot) (GraphInputSnapshot, error) {
	out := in
	out.SchemaVersion = strings.TrimSpace(out.SchemaVersion)
	out.PolicyID = strings.TrimSpace(out.PolicyID)
	out.RepositoryDomain = strings.TrimSpace(out.RepositoryDomain)
	out.SnapshotDigestSHA256 = ""

	roots := make([]LogicalSourceRoot, 0, len(in.SourceRoots))
	seenRoot := map[string]bool{}
	for _, r := range in.SourceRoots {
		r.LogicalPath = strings.TrimSpace(r.LogicalPath)
		r.DefaultDomain = strings.TrimSpace(r.DefaultDomain)
		r.DefaultSourceSet = strings.TrimSpace(r.DefaultSourceSet)
		if r.LogicalPath == "" {
			return GraphInputSnapshot{}, fmt.Errorf("graphbuild: graph-input snapshot has an empty source-root logical path")
		}
		if seenRoot[r.LogicalPath] {
			return GraphInputSnapshot{}, fmt.Errorf("graphbuild: duplicate graph-input source root %q", r.LogicalPath)
		}
		seenRoot[r.LogicalPath] = true
		roots = append(roots, r)
	}
	sort.Slice(roots, func(i, j int) bool { return roots[i].LogicalPath < roots[j].LogicalPath })
	out.SourceRoots = roots

	sups := make([]SupplementalGraphBinding, 0, len(in.SupplementalGraphs))
	seenSup := map[string]bool{}
	seenKey := map[string]bool{}
	for _, s := range in.SupplementalGraphs {
		s.ID = strings.TrimSpace(s.ID)
		s.Version = strings.TrimSpace(s.Version)
		s.SemanticDigestSHA256 = strings.TrimSpace(s.SemanticDigestSHA256)
		s.ArtifactKey = strings.TrimSpace(s.ArtifactKey)
		wantKey, err := SupplementalGraphArtifactKey(s.ID)
		if err != nil {
			return GraphInputSnapshot{}, err
		}
		if s.ArtifactKey == "" {
			s.ArtifactKey = wantKey
		}
		if s.ArtifactKey != wantKey {
			return GraphInputSnapshot{}, fmt.Errorf("graphbuild: supplemental graph %q artifact key %q is not %q", s.ID, s.ArtifactKey, wantKey)
		}
		if seenSup[s.ID] {
			return GraphInputSnapshot{}, fmt.Errorf("graphbuild: duplicate supplemental graph id %q", s.ID)
		}
		if seenKey[s.ArtifactKey] {
			return GraphInputSnapshot{}, fmt.Errorf("graphbuild: duplicate supplemental graph artifact key %q", s.ArtifactKey)
		}
		seenSup[s.ID] = true
		seenKey[s.ArtifactKey] = true
		sups = append(sups, s)
	}
	sort.Slice(sups, func(i, j int) bool {
		if sups[i].ID != sups[j].ID {
			return sups[i].ID < sups[j].ID
		}
		return sups[i].ArtifactKey < sups[j].ArtifactKey
	})
	out.SupplementalGraphs = sups
	return out, nil
}

// ValidateBoundGraphInputSnapshot is the strict check for a snapshot that has
// already been stamped and recorded: it requires a nonempty snapshot digest,
// passes the full structural validation, and requires the recorded digest to
// recompute exactly. The looser ValidateGraphInputSnapshot stays available for a
// caller constructing a snapshot before stamping it.
func ValidateBoundGraphInputSnapshot(snapshot GraphInputSnapshot) error {
	if strings.TrimSpace(snapshot.SnapshotDigestSHA256) == "" {
		return fmt.Errorf("graphbuild: bound graph-input snapshot requires a snapshot digest")
	}
	if err := ValidateGraphInputSnapshot(snapshot); err != nil {
		return err
	}
	want, err := GraphInputSnapshotDigest(snapshot)
	if err != nil {
		return err
	}
	if want != strings.TrimSpace(snapshot.SnapshotDigestSHA256) {
		return fmt.Errorf("graphbuild: bound graph-input snapshot digest does not recompute")
	}
	return nil
}

// GraphInputSnapshotDigest is the self-excluding semantic identity of a snapshot.
func GraphInputSnapshotDigest(in GraphInputSnapshot) (string, error) {
	canon, err := CanonicalizeGraphInputSnapshot(in)
	if err != nil {
		return "", err
	}
	canon.SnapshotDigestSHA256 = ""
	return closureprotocol.SemanticDigest(canon)
}

// ValidateGraphInputSnapshot fails closed on a structurally unsound snapshot: a
// missing schema/policy/domain, an absolute or escaping logical path, a missing
// supplemental digest/key, or a self-digest that does not recompute. It does not
// resolve the policy id against the registry — that is the policy layer's job.
func ValidateGraphInputSnapshot(in GraphInputSnapshot) error {
	canon, err := CanonicalizeGraphInputSnapshot(in)
	if err != nil {
		return err
	}
	if canon.SchemaVersion != GraphInputSnapshotSchemaVersion {
		return fmt.Errorf("graphbuild: graph-input snapshot schema %q is not %q", canon.SchemaVersion, GraphInputSnapshotSchemaVersion)
	}
	if canon.PolicyID == "" {
		return fmt.Errorf("graphbuild: graph-input snapshot requires a policy id")
	}
	if canon.RepositoryDomain == "" {
		return fmt.Errorf("graphbuild: graph-input snapshot requires a repository domain")
	}
	if len(canon.SourceRoots) == 0 {
		return fmt.Errorf("graphbuild: graph-input snapshot requires at least one source root")
	}
	for _, r := range canon.SourceRoots {
		if escapesRoot(r.LogicalPath) || strings.HasPrefix(r.LogicalPath, "/") {
			return fmt.Errorf("graphbuild: graph-input source root %q must be a repository-relative, non-escaping logical path", r.LogicalPath)
		}
	}
	for _, s := range canon.SupplementalGraphs {
		if s.Version == "" {
			return fmt.Errorf("graphbuild: supplemental graph %q requires a version", s.ID)
		}
		if !isHexSHA256(s.SemanticDigestSHA256) {
			return fmt.Errorf("graphbuild: supplemental graph %q semantic digest must be a 64-hex sha256", s.ID)
		}
		if s.ArtifactKey == "" {
			return fmt.Errorf("graphbuild: supplemental graph %q requires a content-addressed artifact key", s.ID)
		}
	}
	if strings.TrimSpace(in.SnapshotDigestSHA256) != "" {
		want, err := GraphInputSnapshotDigest(in)
		if err != nil {
			return err
		}
		if want != strings.TrimSpace(in.SnapshotDigestSHA256) {
			return fmt.Errorf("graphbuild: graph-input snapshot digest does not recompute")
		}
	}
	return nil
}

func isHexSHA256(s string) bool {
	if len(s) != 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
