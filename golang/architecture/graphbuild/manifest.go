// SPDX-License-Identifier: AGPL-3.0-only

package graphbuild

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/extractor"
	"github.com/globulario/sensei/golang/seedmeta"
)

// ImportReport and FileReport are the extractor's per-file dispositions, surfaced
// unchanged so callers see exactly which sources were imported, ignored, or
// refused.
type ImportReport = extractor.ImportReport

// FileReport re-exports the extractor per-file disposition record.
type FileReport = extractor.FileReport

// Closed exclusion-rule strings recorded in every manifest.
const (
	exclusionCandidates      = "candidate directories (candidates/) excluded from graph authority"
	exclusionNonGoverned     = "non-governed file extensions excluded from graph import"
	exclusionNestedGenerated = "nested generated/ directories skipped"
)

var baseExclusions = []string{exclusionCandidates, exclusionNonGoverned}

// SourceManifestEntry is one governed source file's deterministic identity.
type SourceManifestEntry struct {
	RepositoryDomain string `json:"repository_domain,omitempty" yaml:"repository_domain,omitempty"`
	LogicalPath      string `json:"logical_path" yaml:"logical_path"`
	ByteDigestSHA256 string `json:"byte_digest_sha256" yaml:"byte_digest_sha256"`
	Disposition      string `json:"disposition" yaml:"disposition"`
	Schema           string `json:"schema,omitempty" yaml:"schema,omitempty"`
	TripleCount      int    `json:"triple_count" yaml:"triple_count"`
}

// SupplementalManifestEntry records a composed supplemental graph by identity.
type SupplementalManifestEntry struct {
	ID           string `json:"id" yaml:"id"`
	Version      string `json:"version,omitempty" yaml:"version,omitempty"`
	DigestSHA256 string `json:"digest_sha256" yaml:"digest_sha256"`
}

// SourceManifest is the deterministic record of every governed source, the
// exclusion policy applied, and any composed supplemental graphs.
type SourceManifest struct {
	SchemaVersion      string                      `json:"schema_version" yaml:"schema_version"`
	ImportPolicy       string                      `json:"import_policy" yaml:"import_policy"`
	Sources            []SourceManifestEntry       `json:"sources" yaml:"sources"`
	SupplementalGraphs []SupplementalManifestEntry `json:"supplemental_graphs,omitempty" yaml:"supplemental_graphs,omitempty"`
	Exclusions         []string                    `json:"exclusions" yaml:"exclusions"`
	DigestSHA256       string                      `json:"digest_sha256,omitempty" yaml:"digest_sha256,omitempty"`
}

// confineSourceRoot rejects a source root that escapes its identity root, and —
// under a policy that requires it — any source symlink that resolves outside the
// identity root.
func confineSourceRoot(root SourceRoot, policy ValidationPolicy) error {
	id := strings.TrimSpace(root.IdentityRoot)
	if id == "" {
		return errors.New("graphbuild: source root identity root is required")
	}
	if strings.TrimSpace(root.FilesystemPath) == "" {
		return errors.New("graphbuild: source root filesystem path is required")
	}
	rel, err := filepath.Rel(id, root.FilesystemPath)
	if err != nil || escapesRoot(rel) {
		return fmt.Errorf("graphbuild: source root %q escapes identity root %q", root.FilesystemPath, id)
	}
	if policy.RejectExternalSymlinks {
		return rejectEscapingSymlinks(root.FilesystemPath, id)
	}
	return nil
}

func escapesRoot(rel string) bool {
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return true
	}
	return filepath.IsAbs(rel)
}

// rejectEscapingSymlinks refuses any symlink under dir whose target resolves
// outside identityRoot.
func rejectEscapingSymlinks(dir, identityRoot string) error {
	realRoot, err := filepath.EvalSymlinks(identityRoot)
	if err != nil {
		realRoot = identityRoot
	}
	return filepath.WalkDir(dir, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.Type()&fs.ModeSymlink == 0 {
			return nil
		}
		real, err := filepath.EvalSymlinks(p)
		if err != nil {
			return fmt.Errorf("graphbuild: symlink_escape: cannot resolve %q: %w", p, err)
		}
		rel, err := filepath.Rel(realRoot, real)
		if err != nil || escapesRoot(rel) {
			return fmt.Errorf("graphbuild: symlink_escape: %q resolves outside identity root", p)
		}
		return nil
	})
}

// manifestEntriesForRoot builds one manifest entry per reported file, with a
// stable repository-relative logical path and the file's byte digest.
func manifestEntriesForRoot(root SourceRoot, rep *extractor.ImportReport) ([]SourceManifestEntry, error) {
	var out []SourceManifestEntry
	for _, fr := range rep.Files {
		logical, err := logicalPath(root.IdentityRoot, fr.Path)
		if err != nil {
			return nil, err
		}
		digest, err := fileByteDigest(fr.Path)
		if err != nil {
			return nil, err
		}
		out = append(out, SourceManifestEntry{
			RepositoryDomain: root.RepositoryDomain,
			LogicalPath:      logical,
			ByteDigestSHA256: digest,
			Disposition:      string(fr.Status),
			Schema:           fr.Schema,
			TripleCount:      fr.Count,
		})
	}
	return out, nil
}

// logicalPath makes a filesystem path repository-relative to identityRoot and
// refuses any traversal or absolute result.
func logicalPath(identityRoot, p string) (string, error) {
	rel, err := filepath.Rel(identityRoot, p)
	if err != nil || escapesRoot(rel) {
		return "", fmt.Errorf("graphbuild: source %q escapes identity root %q", p, identityRoot)
	}
	return filepath.ToSlash(rel), nil
}

func fileByteDigest(p string) (string, error) {
	data, err := os.ReadFile(p)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// buildManifest assembles a deterministic manifest: entries are sorted by stable
// identity before the digest is computed, so filesystem walk order never affects
// identity.
func buildManifest(policy ValidationPolicy, entries []SourceManifestEntry, exclusions []string, supplemental []SupplementalManifestEntry) SourceManifest {
	sorted := append([]SourceManifestEntry{}, entries...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].RepositoryDomain != sorted[j].RepositoryDomain {
			return sorted[i].RepositoryDomain < sorted[j].RepositoryDomain
		}
		if sorted[i].LogicalPath != sorted[j].LogicalPath {
			return sorted[i].LogicalPath < sorted[j].LogicalPath
		}
		return sorted[i].ByteDigestSHA256 < sorted[j].ByteDigestSHA256
	})
	manifest := SourceManifest{
		SchemaVersion:      ManifestSchemaVersion,
		ImportPolicy:       policy.label(),
		Sources:            sorted,
		SupplementalGraphs: supplemental,
		Exclusions:         exclusions,
	}
	if d, err := sourceManifestDigest(manifest); err == nil {
		manifest.DigestSHA256 = d
	}
	return manifest
}

// sourceManifestDigest is the semantic digest of the manifest with its own digest
// field excluded, using the shared closure canonicalizer.
func sourceManifestDigest(m SourceManifest) (string, error) {
	m.DigestSHA256 = ""
	m.SupplementalGraphs = sortSupplemental(m.SupplementalGraphs)
	return closureprotocol.SemanticDigest(m)
}

func sortSupplemental(in []SupplementalManifestEntry) []SupplementalManifestEntry {
	if len(in) == 0 {
		return in
	}
	out := append([]SupplementalManifestEntry{}, in...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].ID != out[j].ID {
			return out[i].ID < out[j].ID
		}
		return out[i].DigestSHA256 < out[j].DigestSHA256
	})
	return out
}

// enforcePolicy fails closed when the report contains a disposition the policy
// rejects. Ignored (deliberate non-authority config) is always allowed.
func enforcePolicy(report ImportReport, policy ValidationPolicy) error {
	for _, f := range report.Files {
		switch f.Status {
		case extractor.StatusUnknownSchema:
			if policy.RejectUnknownSchemas {
				return fmt.Errorf("graphbuild: unrecognized schema in %s (%s)", f.Path, f.Status)
			}
		case extractor.StatusInvalid:
			if policy.RejectInvalidFiles {
				return fmt.Errorf("graphbuild: invalid governed source %s: %s", f.Path, f.Reason)
			}
		case extractor.StatusKnownUnsupported:
			if policy.RejectUnsupportedFiles {
				return fmt.Errorf("graphbuild: unsupported governed source %s (%s)", f.Path, f.Status)
			}
		}
	}
	return nil
}

// stripMarkerLines removes a graph's seed-marker triples, returning the
// marker-free body. It mirrors the canonical marker shape (all six triples share
// the marker's content-hash IRI subject).
func stripMarkerLines(nt []byte) []byte {
	marker, ok := seedmeta.ParseMarker(nt)
	if !ok {
		return bytes.TrimSpace(nt)
	}
	needle := "<" + marker.IRI + "> "
	var kept []string
	for _, raw := range strings.Split(string(nt), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, needle) {
			continue
		}
		kept = append(kept, line)
	}
	if len(kept) == 0 {
		return nil
	}
	return append([]byte(strings.Join(kept, "\n")), '\n')
}
