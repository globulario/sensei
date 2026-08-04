// SPDX-License-Identifier: AGPL-3.0-only

package senseireport

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"
)

// candidateStatusAwaitingReview is the one status value counted as
// "awaiting review", matching cmd_promote.go's validateCandidateEntry
// convention (status must equal "candidate" before promotion review).
const candidateStatusAwaitingReview = "candidate"

// maxHighlightedCandidates bounds Memory.Highlighted so the report stays
// concise -- the design doc requires this section show only high-value
// candidates, never the full queue.
const maxHighlightedCandidates = 5

// maxCandidateLimitations bounds how many statusless-file notes are named
// individually before collapsing the remainder into a count, so a large
// corpus can't make the report itself unreadable.
const maxCandidateLimitations = 10

type candidateEntry struct {
	CandidateSummary
	Status string
}

// Candidates walks docs/awareness/candidates/**/*.yaml and summarizes
// behavioral-memory candidates awaiting review. It is shape-agnostic on
// purpose: the corpus holds at least three shapes today -- a wrapper key
// plus a status-bearing list; a flat single-entry file with status at the
// top level; and a contract_unknown wrapper whose entries carry no status
// field at all, predating/bypassing the review convention entirely.
// Rather than hard-coding one shape the way cmd_promote.go's
// findCandidateEntry does (one directory level, one shape, used for
// promoting a single already-known id), this walks the decoded YAML tree
// looking for any mapping that carries a string "status" key, treats that
// mapping as one candidate entry, and does not recurse further into it. A
// file that contributes zero status-bearing entries is never silently
// dropped -- it is named in Memory.Limitations.
func Candidates(repoRoot string) (Memory, error) {
	dir := filepath.Join(repoRoot, "docs", "awareness", "candidates")
	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return Memory{Limitations: []string{"docs/awareness/candidates does not exist"}}, nil
		}
		return Memory{}, fmt.Errorf("stat %s: %w", dir, err)
	}
	if !info.IsDir() {
		return Memory{}, fmt.Errorf("%s is not a directory", dir)
	}

	var entries []candidateEntry
	var statuslessFiles []string

	walkErr := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		ext := filepath.Ext(path)
		if ext != ".yaml" && ext != ".yml" {
			return nil
		}
		rel, relErr := filepath.Rel(repoRoot, path)
		if relErr != nil {
			rel = path
		}
		rel = filepath.ToSlash(rel)

		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return fmt.Errorf("read %s: %w", rel, readErr)
		}
		var doc interface{}
		if unmarshalErr := yaml.Unmarshal(data, &doc); unmarshalErr != nil {
			// A malformed candidate file is real, but this package's job is
			// to summarize what it can, not abort report generation over
			// one bad file -- name it as a limitation instead.
			statuslessFiles = append(statuslessFiles, rel+" (malformed YAML)")
			return nil
		}

		before := len(entries)
		entries = walkCandidateNode(doc, rel, entries)
		if len(entries) == before {
			statuslessFiles = append(statuslessFiles, rel)
		}
		return nil
	})
	if walkErr != nil {
		return Memory{}, walkErr
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Source != entries[j].Source {
			return entries[i].Source < entries[j].Source
		}
		return entries[i].ID < entries[j].ID
	})
	sort.Strings(statuslessFiles)

	m := Memory{}
	var highlighted []CandidateSummary
	for _, e := range entries {
		if e.Status != candidateStatusAwaitingReview {
			continue
		}
		m.CandidatesAwaitingReview++
		if len(highlighted) < maxHighlightedCandidates {
			highlighted = append(highlighted, e.CandidateSummary)
		}
	}
	m.Highlighted = highlighted

	if len(statuslessFiles) > 0 {
		shown := statuslessFiles
		truncated := 0
		if len(shown) > maxCandidateLimitations {
			truncated = len(shown) - maxCandidateLimitations
			shown = shown[:maxCandidateLimitations]
		}
		for _, f := range shown {
			m.Limitations = append(m.Limitations, "no status-bearing entry found (excluded from count): "+f)
		}
		if truncated > 0 {
			m.Limitations = append(m.Limitations, fmt.Sprintf("%d more file(s) with no status-bearing entry, not listed individually", truncated))
		}
	}

	return m, nil
}

// walkCandidateNode recurses through a decoded YAML value looking for any
// mapping that carries a string "status" key. Map key iteration order is
// sorted before recursing so results are deterministic regardless of the
// YAML decoder's own map ordering.
func walkCandidateNode(node interface{}, source string, out []candidateEntry) []candidateEntry {
	switch v := node.(type) {
	case map[string]interface{}:
		if status, ok := v["status"].(string); ok {
			entry := candidateEntry{Status: status}
			entry.Source = source
			if id, ok := v["id"].(string); ok {
				entry.ID = id
			}
			if class, ok := v["class"].(string); ok {
				entry.Class = class
			} else if kind, ok := v["kind"].(string); ok {
				entry.Class = kind
			}
			return append(out, entry)
		}
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			out = walkCandidateNode(v[k], source, out)
		}
		return out
	case []interface{}:
		for _, item := range v {
			out = walkCandidateNode(item, source, out)
		}
		return out
	default:
		return out
	}
}
