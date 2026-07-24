// SPDX-License-Identifier: AGPL-3.0-only

package investigationsurface

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/globulario/sensei/golang/architecture/investigation"
)

const EvidenceSnapshotSchemaVersion = "investigation.evidence-snapshot.v1"

type EvidenceSnapshotEntry struct {
	Path         string `json:"path" yaml:"path"`
	DigestSHA256 string `json:"digest_sha256" yaml:"digest_sha256"`
	Content      []byte `json:"content" yaml:"content"`
}

type EvidenceSnapshot struct {
	SchemaVersion        string                         `json:"schema_version" yaml:"schema_version"`
	GeneratedBy          string                         `json:"generated_by" yaml:"generated_by"`
	Category             investigation.EvidenceCategory `json:"category" yaml:"category"`
	ProviderID           string                         `json:"provider_id" yaml:"provider_id"`
	ProviderVersion      string                         `json:"provider_version" yaml:"provider_version"`
	SourceRoot           string                         `json:"source_root" yaml:"source_root"`
	CapturedAt           string                         `json:"captured_at" yaml:"captured_at"`
	TimestampSource      string                         `json:"timestamp_source" yaml:"timestamp_source"`
	Entries              []EvidenceSnapshotEntry        `json:"entries" yaml:"entries"`
	SnapshotDigestSHA256 string                         `json:"snapshot_digest_sha256" yaml:"snapshot_digest_sha256"`
}

type EvidenceImportReceipt struct {
	SchemaVersion        string   `json:"schema_version" yaml:"schema_version"`
	ImportedBy           string   `json:"imported_by" yaml:"imported_by"`
	SnapshotDigestSHA256 string   `json:"snapshot_digest_sha256" yaml:"snapshot_digest_sha256"`
	RepositoryRoot       string   `json:"repository_root" yaml:"repository_root"`
	ImportedPaths        []string `json:"imported_paths" yaml:"imported_paths"`
	ManifestPath         string   `json:"manifest_path" yaml:"manifest_path"`
}

func CaptureEvidence(source, capturedAt string) (EvidenceSnapshot, error) {
	if _, err := time.Parse(time.RFC3339, strings.TrimSpace(capturedAt)); err != nil {
		return EvidenceSnapshot{}, fmt.Errorf("captured_at must be explicit RFC3339: %w", err)
	}
	root, err := filepath.Abs(strings.TrimSpace(source))
	if err != nil {
		return EvidenceSnapshot{}, err
	}
	info, err := os.Stat(root)
	if err != nil {
		return EvidenceSnapshot{}, err
	}
	base := root
	if !info.IsDir() {
		base = filepath.Dir(root)
	}
	var entries []EvidenceSnapshotEntry
	walk := func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(base, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if escapingPath(rel) {
			return fmt.Errorf("evidence path escapes source root: %s", rel)
		}
		entries = append(entries, EvidenceSnapshotEntry{Path: rel, DigestSHA256: investigation.SHA256Bytes(content), Content: content})
		return nil
	}
	if info.IsDir() {
		err = filepath.WalkDir(root, walk)
	} else {
		err = walk(root, fileEntry{info}, nil)
	}
	if err != nil {
		return EvidenceSnapshot{}, err
	}
	if len(entries) == 0 {
		return EvidenceSnapshot{}, errors.New("evidence source contains no files")
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	snapshot := EvidenceSnapshot{SchemaVersion: EvidenceSnapshotSchemaVersion, GeneratedBy: "sensei evidence snapshot", Category: investigation.EvidenceRuntime, ProviderID: "imported_evidence_provider", ProviderVersion: "1.0", SourceRoot: root, CapturedAt: strings.TrimSpace(capturedAt), TimestampSource: "caller_supplied", Entries: entries}
	digest, err := evidenceSnapshotDigest(snapshot)
	if err != nil {
		return EvidenceSnapshot{}, err
	}
	snapshot.SnapshotDigestSHA256 = digest
	return snapshot, ValidateEvidenceSnapshot(snapshot)
}

type fileEntry struct{ os.FileInfo }

func (f fileEntry) Type() fs.FileMode          { return f.Mode().Type() }
func (f fileEntry) Info() (fs.FileInfo, error) { return f.FileInfo, nil }

func ValidateEvidenceSnapshot(snapshot EvidenceSnapshot) error {
	var errs []string
	if snapshot.SchemaVersion != EvidenceSnapshotSchemaVersion {
		errs = append(errs, "unsupported evidence snapshot schema")
	}
	if snapshot.GeneratedBy != "sensei evidence snapshot" {
		errs = append(errs, "unexpected evidence snapshot generator")
	}
	if snapshot.Category != investigation.EvidenceRuntime {
		errs = append(errs, "imported evidence snapshots must use runtime_observability category")
	}
	if snapshot.ProviderID != "imported_evidence_provider" || snapshot.ProviderVersion != "1.0" {
		errs = append(errs, "snapshot provider must match imported_evidence_provider@1.0")
	}
	if _, err := time.Parse(time.RFC3339, snapshot.CapturedAt); err != nil {
		errs = append(errs, "captured_at must be RFC3339")
	}
	if snapshot.TimestampSource != "caller_supplied" {
		errs = append(errs, "timestamp_source must be caller_supplied")
	}
	seen := map[string]bool{}
	for _, entry := range snapshot.Entries {
		path := filepath.ToSlash(strings.TrimSpace(entry.Path))
		if path == "" || escapingPath(path) {
			errs = append(errs, "snapshot entry path must be non-empty, relative, and non-escaping")
			continue
		}
		if seen[path] {
			errs = append(errs, "duplicate snapshot entry path "+path)
		}
		seen[path] = true
		if investigation.SHA256Bytes(entry.Content) != entry.DigestSHA256 {
			errs = append(errs, "snapshot entry digest mismatch for "+path)
		}
	}
	if len(snapshot.Entries) == 0 {
		errs = append(errs, "snapshot entries are required")
	}
	digest, err := evidenceSnapshotDigest(snapshot)
	if err != nil {
		errs = append(errs, err.Error())
	} else if snapshot.SnapshotDigestSHA256 != digest {
		errs = append(errs, "snapshot digest mismatch")
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func evidenceSnapshotDigest(snapshot EvidenceSnapshot) (string, error) {
	copy := snapshot
	copy.SourceRoot = ""
	copy.SnapshotDigestSHA256 = ""
	sort.Slice(copy.Entries, func(i, j int) bool { return copy.Entries[i].Path < copy.Entries[j].Path })
	data, err := json.Marshal(copy)
	if err != nil {
		return "", err
	}
	return investigation.SHA256Bytes(data), nil
}

func ImportEvidence(snapshot EvidenceSnapshot, repositoryRoot string) (EvidenceImportReceipt, error) {
	if err := ValidateEvidenceSnapshot(snapshot); err != nil {
		return EvidenceImportReceipt{}, err
	}
	root, err := filepath.Abs(strings.TrimSpace(repositoryRoot))
	if err != nil {
		return EvidenceImportReceipt{}, err
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return EvidenceImportReceipt{}, errors.New("repository root must be an existing directory")
	}
	importRoot := filepath.Join(root, ".sensei", "evidence", "imported", snapshot.SnapshotDigestSHA256)
	var imported []string
	for _, entry := range snapshot.Entries {
		target := filepath.Join(importRoot, filepath.FromSlash(entry.Path))
		clean := filepath.Clean(target)
		if clean != importRoot && !strings.HasPrefix(clean, importRoot+string(filepath.Separator)) {
			return EvidenceImportReceipt{}, fmt.Errorf("import path escapes evidence root: %s", entry.Path)
		}
		if err := os.MkdirAll(filepath.Dir(clean), 0o755); err != nil {
			return EvidenceImportReceipt{}, err
		}
		if err := os.WriteFile(clean, entry.Content, 0o644); err != nil {
			return EvidenceImportReceipt{}, err
		}
		rel, _ := filepath.Rel(root, clean)
		imported = append(imported, filepath.ToSlash(rel))
	}
	sort.Strings(imported)
	manifestDir := filepath.Join(root, ".sensei", "evidence-manifests")
	if err := os.MkdirAll(manifestDir, 0o755); err != nil {
		return EvidenceImportReceipt{}, err
	}
	receipt := EvidenceImportReceipt{SchemaVersion: "investigation.evidence-import.v1", ImportedBy: "sensei evidence import", SnapshotDigestSHA256: snapshot.SnapshotDigestSHA256, RepositoryRoot: root, ImportedPaths: imported, ManifestPath: filepath.ToSlash(filepath.Join(".sensei", "evidence-manifests", snapshot.SnapshotDigestSHA256+".json"))}
	data, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return EvidenceImportReceipt{}, err
	}
	data = append(data, '\n')
	if err := os.WriteFile(filepath.Join(manifestDir, snapshot.SnapshotDigestSHA256+".json"), data, 0o644); err != nil {
		return EvidenceImportReceipt{}, err
	}
	return receipt, nil
}

func escapingPath(path string) bool {
	return filepath.IsAbs(path) || path == ".." || strings.HasPrefix(path, "../") || strings.Contains(path, "/../")
}
