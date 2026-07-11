// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strings"

	yaml "gopkg.in/yaml.v3"

	"github.com/globulario/sensei/golang/extractor/protoscan"
)

// Minimum, conservative candidate extractors for `sensei bootstrap` — pattern and
// misuse candidates. They are deliberately low-noise and grounded in signals we
// already have (proto API shape, storage-driver imports), emitted as
// status: candidate / confidence: candidate under docs/awareness/candidates/.
// Nothing is auto-promoted; these are suggestions for human review.

type candidateDoc struct {
	ID          string   `yaml:"id"`
	Class       string   `yaml:"class"`
	Label       string   `yaml:"label"`
	Status      string   `yaml:"status"`
	Confidence  string   `yaml:"confidence"`
	Description string   `yaml:"description"`
	SourceFiles []string `yaml:"source_files,omitempty"`
	DetectedIn  []string `yaml:"detected_in,omitempty"`
}

// candidateFile is a rendered candidate ready to write under candidates/.
type candidateFile struct {
	classDir string // "implementation_pattern" | "pattern_misuse"
	doc      candidateDoc
}

func renderCandidate(c candidateDoc) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString("# GENERATED candidate by `sensei bootstrap` — status: candidate.\n")
	buf.WriteString("# A suggestion for human review; never auto-promoted. To accept,\n")
	buf.WriteString("# move it under docs/awareness/architecture/ and set status accordingly.\n")
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(c); err != nil {
		return nil, err
	}
	enc.Close()
	return buf.Bytes(), nil
}

// sanitizeFileName turns a candidate id into a safe filename segment.
func sanitizeFileName(id string) string {
	var b strings.Builder
	for _, r := range id {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}

// ── pattern candidates (from proto API shape) ──────────────────────────────

// extractPatternCandidates suggests an ImplementationPattern per gRPC service
// based on its read/write shape: all-read → ReadOnlyProjection; has writes →
// GuardedMutationFlow. Grounded in the proto, one per service, zero guesswork.
func extractPatternCandidates(contracts []protoscan.Contract) []candidateFile {
	var out []candidateFile
	for _, c := range contracts {
		// Service-level contracts only (uml.kind Interface), not RPCs.
		if c.Uml == nil || c.Uml.Kind != "Interface" {
			continue
		}
		svcSlug := strings.TrimPrefix(c.ID, "contract.")
		var suffix, suggested, label, desc string
		switch c.ReadOrWrite {
		case "read":
			suffix, suggested = "read_only", "pattern.read_only_projection"
			label = c.Name + " exposes a read-only gRPC API (candidate)"
			desc = "All RPCs on " + c.Name + " are reads (read_or_write=read). This API shape looks like a ReadOnlyProjection — review and, if correct, declare an ImplementationPattern realizing " + suggested + "."
		case "write", "read_write":
			suffix, suggested = "guarded_mutation", "pattern.guarded_mutation_flow"
			label = c.Name + " exposes a mutation surface (candidate)"
			desc = "" + c.Name + " has write RPCs (read_or_write=" + c.ReadOrWrite + "). A mutation surface should be validated/auditable — review whether it follows " + suggested + " (a GuardedMutationFlow)."
		default:
			continue // unknown shape → don't guess
		}
		out = append(out, candidateFile{
			classDir: "implementation_pattern",
			doc: candidateDoc{
				ID:          "candidate.implementation_pattern." + svcSlug + "_" + suffix,
				Class:       "ImplementationPattern",
				Label:       label,
				Status:      "candidate",
				Confidence:  "candidate",
				Description: desc,
				SourceFiles: c.SourceFiles,
			},
		})
	}
	return out
}

// ── misuse candidates (direct storage access) ──────────────────────────────

// storageDriverImports are import paths that indicate a component reads/writes a
// store directly (rather than through an owner service's port).
var storageDriverImports = []string{
	`"database/sql"`,
	`"go.etcd.io/etcd`,
	`"go.mongodb.org/mongo-driver`,
	`"github.com/jackc/pgx`,
	`"github.com/lib/pq"`,
	`"github.com/go-redis/redis`,
	`"github.com/redis/go-redis`,
	`"gorm.io/gorm"`,
	`"github.com/dgraph-io/badger`,
	`"go.etcd.io/bbolt"`,
}

// storageOperationSignals are broad operation markers that suggest a file is
// doing storage work directly rather than merely wiring a client type.
var storageOperationSignals = []string{
	".Query(",
	".QueryRow(",
	".Exec(",
	".Prepare(",
	".Begin(",
	".Get(",
	".Put(",
	".Delete(",
	".Watch(",
	".Txn(",
	".Find(",
	".First(",
	".Create(",
	".Save(",
	".Update(",
	".Insert(",
	".Aggregate(",
}

func containsStorageDriverImport(content string) bool {
	for _, imp := range storageDriverImports {
		if strings.Contains(content, imp) {
			return true
		}
	}
	return false
}

func containsStorageOperation(content string) bool {
	for _, sig := range storageOperationSignals {
		if strings.Contains(content, sig) {
			return true
		}
	}
	return false
}

func isExplicitSelfOwnedStorageException(content string) bool {
	return strings.Contains(content, "file_role=xds_self_owned_legacy_etcd_ingress_config_parser") ||
		strings.Contains(content, "OWNER of /globular/xds/v1/*")
}

// extractMisuseCandidates emits ONE conservative PatternMisuse candidate when
// non-test Go files both import a storage driver directly and perform
// recognizable storage operations — a prompt to verify those reads go through
// the owning service's port (the direct_storage_read misuse), not a guess that
// they are wrong. Files with an explicit self-owned storage exception are
// skipped. Returns nil when there are no hits.
func extractMisuseCandidates(root string) []candidateFile {
	const cap = 25
	var hits []string
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p != root && bootstrapExcludedDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if len(hits) >= cap || !strings.HasSuffix(d.Name(), ".go") || isTestFile(d.Name()) {
			return nil
		}
		data, rerr := os.ReadFile(p)
		if rerr != nil {
			return nil
		}
		content := string(data)
		if !containsStorageDriverImport(content) || !containsStorageOperation(content) || isExplicitSelfOwnedStorageException(content) {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		hits = append(hits, filepath.ToSlash(rel))
		return nil
	})
	if len(hits) == 0 {
		return nil
	}
	sort.Strings(hits)
	return []candidateFile{{
		classDir: "pattern_misuse",
		doc: candidateDoc{
			ID:          "candidate.pattern_misuse.direct_storage_access",
			Class:       "PatternMisuse",
			Label:       "Direct storage-driver access — verify it goes through an owner port (candidate)",
			Status:      "candidate",
			Confidence:  "candidate",
			Description: "These files import a storage driver directly. Verify each access goes through the owning service's port (the truth_read_via_owner_rpc_not_direct_storage principle), not a direct read that steals authority. Review per file — a storage component legitimately owns its driver.",
			DetectedIn:  hits,
		},
	}}
}

// writeCandidateFiles renders + writes (or, in dry-run/check, skips) the
// candidate files under candidatesDir. Returns the count written. The
// coldsource cage applies: everything lands under a candidates/ tree.
func writeCandidateFiles(candidatesDir string, cands []candidateFile, write bool) (int, error) {
	n := 0
	for _, c := range cands {
		data, err := renderCandidate(c.doc)
		if err != nil {
			return n, err
		}
		if !write {
			n++
			continue
		}
		dir := filepath.Join(candidatesDir, c.classDir)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return n, err
		}
		path := filepath.Join(dir, sanitizeFileName(c.doc.ID)+".yaml")
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}
