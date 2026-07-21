// SPDX-License-Identifier: AGPL-3.0-only

package governedmutation

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

// GovernedSourceDir is the canonical governed-source directory, repo-relative.
const GovernedSourceDir = "docs/awareness"

// candidatesDir is excluded from the governed manifest: contract_unknown entries
// are a human-review queue, not governed truth.
const candidatesDir = "candidates"

// GovernedManifestDigest is the deterministic compare-and-swap token over the
// whole governed-source set: a semantic digest of the sorted map of every
// governed YAML file's repo-relative path to its content byte digest. The
// candidates/ queue is excluded. It spans the whole set (not one file) because a
// downstream graph rebuild re-reads the entire governed-source tree from disk.
func GovernedManifestDigest(root string) (string, error) {
	base := filepath.Join(root, filepath.FromSlash(GovernedSourceDir))
	files := map[string]string{}
	walkErr := filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			if d.Name() == candidatesDir {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".yaml") && !strings.HasSuffix(d.Name(), ".yml") {
			return nil
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return rerr
		}
		sum := sha256.Sum256(data)
		files[filepath.ToSlash(rel)] = hex.EncodeToString(sum[:])
		return nil
	})
	if walkErr != nil {
		return "", walkErr
	}
	// Deterministic ordered manifest.
	type entry struct {
		Path       string `json:"path"`
		ByteDigest string `json:"byte_digest_sha256"`
	}
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	manifest := make([]entry, 0, len(paths))
	for _, p := range paths {
		manifest = append(manifest, entry{Path: p, ByteDigest: files[p]})
	}
	return closureprotocol.SemanticDigest(struct {
		Kind    string  `json:"kind"`
		Entries []entry `json:"entries"`
	}{Kind: "governed_source_manifest/v1", Entries: manifest})
}
