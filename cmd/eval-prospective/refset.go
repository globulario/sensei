// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/globulario/sensei/golang/architecture/prospective"
)

// ReferenceSet is the frozen sample as Slice 2 reads it.
//
// The runner reads the manifest in full — including each change's stratum —
// because it must record results against the partition the sample was frozen
// with. That is the opposite of the labelling tool's permission, which is
// deliberately unable to open anything carrying a stratum or an anchor: an
// adjudicator told "the graph has nothing for this file" has been told
// something about the answer. The runner is not adjudicating and must never
// consult anchors, so it opens the manifest and the packages and stops there.
type ReferenceSet struct {
	Root     string
	Manifest prospective.Manifest
	Corpus   prospective.BlindCorpus
	Packages map[string]prospective.BlindPackage
}

// LoadReferenceSet reads the frozen sample and verifies the artifacts describe
// one another.
func LoadReferenceSet(root string) (*ReferenceSet, error) {
	rs := &ReferenceSet{Root: root, Packages: map[string]prospective.BlindPackage{}}
	if err := readJSON(filepath.Join(root, "sample-manifest.json"), &rs.Manifest); err != nil {
		return nil, fmt.Errorf("sample manifest: %w", err)
	}
	if rs.Manifest.SchemaVersion != prospective.SchemaVersion {
		return nil, fmt.Errorf("sample manifest carries schema %q, not %q", rs.Manifest.SchemaVersion, prospective.SchemaVersion)
	}
	if err := readJSON(filepath.Join(root, prospective.BlindCorpusRef), &rs.Corpus); err != nil {
		return nil, fmt.Errorf("blind corpus: %w", err)
	}
	if rs.Corpus.DigestSHA256 != rs.Manifest.BlindCorpusDigestSHA256 {
		return nil, fmt.Errorf("blind corpus %s is not the one the manifest names (%s): a different corpus is a different denominator",
			rs.Corpus.DigestSHA256, rs.Manifest.BlindCorpusDigestSHA256)
	}
	for _, it := range rs.Manifest.Items {
		name := filepath.Join(root, "packages", strings.ReplaceAll(it.ItemKey, ":", "-")+".json")
		var pkg prospective.BlindPackage
		if err := readJSON(name, &pkg); err != nil {
			return nil, fmt.Errorf("package for %s: %w", it.ItemKey, err)
		}
		if pkg.BlindCorpusDigestSHA256 != rs.Corpus.DigestSHA256 {
			return nil, fmt.Errorf("package %s references blind corpus %s, not %s", it.ItemKey, pkg.BlindCorpusDigestSHA256, rs.Corpus.DigestSHA256)
		}
		rs.Packages[it.ItemKey] = pkg
	}
	if len(rs.Packages) == 0 {
		return nil, fmt.Errorf("the manifest names no sampled changes")
	}
	return rs, nil
}

// EligibleItemIDs is the identity set that bounds the denominator.
func (rs *ReferenceSet) EligibleItemIDs() []string {
	out := make([]string, 0, len(rs.Corpus.Items))
	for _, it := range rs.Corpus.Items {
		out = append(out, it.ID)
	}
	sort.Strings(out)
	return out
}

func readJSON(path string, into any) error {
	body, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, into)
}

func writeSealedJSON(path string, payload any) error {
	body, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(path, append(body, '\n'), 0o644)
}
