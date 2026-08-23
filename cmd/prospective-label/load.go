// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// The only files this tool may read out of a reference set.
//
// The list is enforced by openAllowed rather than merely observed by the call
// sites. corpus.json, anchor-index.json and inventory.json hold anchors,
// materialization provenance, stratum assignments and per-class accounting —
// Sensei's own account of what it knows and where it applies. An adjudicator
// who sees any of that is agreeing with the system being graded rather than
// judging the change, so the tool is built unable to open them.
var allowedFiles = map[string]bool{
	"sample-manifest.json": true,
	"blind-corpus.json":    true,
}

const packagesDir = "packages"

// openAllowed reads one file from the reference set, refusing anything outside
// the whitelist.
func openAllowed(root, name string) ([]byte, error) {
	clean := filepath.ToSlash(filepath.Clean(name))
	if !allowedFiles[clean] && !strings.HasPrefix(clean, packagesDir+"/") {
		return nil, fmt.Errorf("refusing to read %q: an adjudicator may see only the sample manifest, the blind corpus and the change packages — everything else in a reference set states what Sensei knows", clean)
	}
	if strings.Contains(clean, "..") {
		return nil, fmt.Errorf("refusing to read %q: path escapes the reference set", clean)
	}
	return os.ReadFile(filepath.Join(root, filepath.FromSlash(clean)))
}

type manifest struct {
	ProtocolID              string `json:"protocol_id"`
	DigestSHA256            string `json:"digest_sha256"`
	BlindCorpusDigestSHA256 string `json:"blind_corpus_digest_sha256"`
	World                   struct {
		Revision string `json:"revision"`
	} `json:"world"`
	Items []struct {
		ItemKey string `json:"item_key"`
	} `json:"items"`
	OverlapItemKeys []string `json:"overlap_item_keys"`
}

type blindCorpus struct {
	SchemaVersion string `json:"schema_version"`
	DigestSHA256  string `json:"digest_sha256"`
	Items         []struct {
		ID        string `json:"id"`
		Class     string `json:"class"`
		Title     string `json:"title"`
		Statement string `json:"statement"`
	} `json:"items"`
}

type changePackage struct {
	ItemKey                 string `json:"item_key"`
	BlindCorpusDigestSHA256 string `json:"blind_corpus_digest_sha256"`
	DigestSHA256            string `json:"digest_sha256"`
	Change                  struct {
		ChangeID     string `json:"change_id"`
		BaseRevision string `json:"base_revision"`
		Content      string `json:"content"`
		Paths        []struct {
			Path          string `json:"path"`
			ExistedBefore bool   `json:"existed_before"`
			Status        string `json:"status"`
		} `json:"paths"`
	} `json:"change"`
}

type referenceSet struct {
	Root     string
	Manifest manifest
	Corpus   blindCorpus
	Packages map[string]changePackage
	ItemKeys []string
}

// loadReferenceSet reads the three permitted artifacts and verifies that they
// describe one another.
//
// The digest checks are not ceremony. A blind corpus that does not match the
// one the manifest names is a different denominator, and labels collected
// against it would be answers to a question nobody asked.
func loadReferenceSet(root string) (*referenceSet, error) {
	rs := &referenceSet{Root: root, Packages: map[string]changePackage{}}

	raw, err := openAllowed(root, "sample-manifest.json")
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &rs.Manifest); err != nil {
		return nil, fmt.Errorf("sample manifest: %w", err)
	}

	rawCorpus, err := openAllowed(root, "blind-corpus.json")
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(rawCorpus, &rs.Corpus); err != nil {
		return nil, fmt.Errorf("blind corpus: %w", err)
	}
	if rs.Corpus.DigestSHA256 != rs.Manifest.BlindCorpusDigestSHA256 {
		return nil, fmt.Errorf("blind corpus %s is not the one the manifest names (%s): a different corpus is a different denominator",
			rs.Corpus.DigestSHA256, rs.Manifest.BlindCorpusDigestSHA256)
	}
	for _, it := range rs.Corpus.Items {
		if strings.TrimSpace(it.Title) == "" && strings.TrimSpace(it.Statement) == "" {
			return nil, fmt.Errorf("eligible item %s carries nothing a human can read; it cannot be judged and must not bound the denominator", it.ID)
		}
	}

	for _, it := range rs.Manifest.Items {
		name := packagesDir + "/" + strings.ReplaceAll(it.ItemKey, ":", "-") + ".json"
		body, err := openAllowed(root, name)
		if err != nil {
			return nil, fmt.Errorf("package for %s: %w", it.ItemKey, err)
		}
		var pkg changePackage
		if err := json.Unmarshal(body, &pkg); err != nil {
			return nil, fmt.Errorf("package for %s: %w", it.ItemKey, err)
		}
		if pkg.BlindCorpusDigestSHA256 != rs.Corpus.DigestSHA256 {
			return nil, fmt.Errorf("package %s references blind corpus %s, not %s", it.ItemKey, pkg.BlindCorpusDigestSHA256, rs.Corpus.DigestSHA256)
		}
		sum := sha256.Sum256(body)
		_ = hex.EncodeToString(sum[:])
		rs.Packages[it.ItemKey] = pkg
		rs.ItemKeys = append(rs.ItemKeys, it.ItemKey)
	}
	if len(rs.ItemKeys) == 0 {
		return nil, fmt.Errorf("the manifest names no sampled changes")
	}
	return rs, nil
}

// corpusIDs returns the eligible items in the deterministic order the tool
// presents them: class, then title, then id. It is a stable key order and
// carries no relevance whatsoever.
func (rs *referenceSet) corpusIDs() []string {
	type row struct{ class, title, id string }
	rows := make([]row, 0, len(rs.Corpus.Items))
	for _, it := range rs.Corpus.Items {
		rows = append(rows, row{it.Class, it.Title, it.ID})
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].class != rows[j].class {
			return rows[i].class < rows[j].class
		}
		if rows[i].title != rows[j].title {
			return rows[i].title < rows[j].title
		}
		return rows[i].id < rows[j].id
	})
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.id)
	}
	return out
}
