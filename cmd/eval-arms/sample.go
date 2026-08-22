// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/globulario/sensei/golang/architecture/evalsample"
)

// protocolPath is the frozen protocol this sample serves. The manifest records
// its digest, so a sample drawn under one protocol version can never be read
// as though it obeyed another.
const protocolPath = "docs/evaluation/phase10-reference-protocol-v1.md"

const protocolID = "phase10-reference-protocol-v1"

// recallUnitInventory is the INDEPENDENT unit inventory of section 7.
//
// It is the repository's own package structure, read from the filesystem. That
// independence is the point and not an implementation convenience: an
// inventory derived from Sensei's extraction could only contain units Sensei
// already had something to say about, so a unit it missed entirely would never
// enter the denominator and its omission would be unmeasurable by
// construction. Recall would then be a measurement of Sensei's output against
// itself.
//
// Directories, not observations. A directory holding Go files exists whether
// or not any extractor ever looked at it.
func recallUnitInventory(root string) ([]string, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	units := map[string]bool{}
	err = filepath.WalkDir(abs, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			// Vendored and hidden trees are not units of this repository's
			// architecture, and testdata is fixture material rather than a
			// component somebody owns.
			if path != abs && (strings.HasPrefix(name, ".") || name == "vendor" || name == "node_modules" || name == "testdata") {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" {
			return nil
		}
		rel, relErr := filepath.Rel(abs, filepath.Dir(path))
		if relErr != nil {
			return relErr
		}
		units[filepath.ToSlash(rel)] = true
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(units) == 0 {
		return nil, fmt.Errorf("no directory under %s holds a Go file, so this world has no unit inventory to draw recall from", root)
	}
	out := make([]string, 0, len(units))
	for u := range units {
		out = append(out, u)
	}
	sort.Strings(out)
	return out, nil
}

// writeSample builds the frozen sample manifest and the blinded adjudication
// views, and writes them under out/sample.
//
// The manifest is written even when a lane is empty. Step 9 of the handoff
// produces the SELECTION; whether a lane had anything to select is one of the
// facts the selection is supposed to record.
func writeSample(out, protocolFile string, worlds []evalsample.World, seed, capturedAt string) armArtifact {
	art := armArtifact{Arm: "frozen_sample_manifest", Subject: subjectPublishedDomain}
	if strings.TrimSpace(seed) == "" {
		// not_run, not failed. A run that only wanted the arms is a legitimate
		// run; what it did not do was draw a sample, and saying so is different
		// from saying the draw was attempted and broke.
		art.Status = statusNotRun
		art.Reason = "no --selection-seed given; the protocol requires the seed to be committed before labels exist, so this run drew no sample"
		return art
	}
	if len(worlds) == 0 {
		art.Status = statusNotRun
		art.Reason = "no evaluation world ran, so there is nothing to sample; supply --world"
		return art
	}
	digest, err := fileDigest(protocolFile)
	if err != nil {
		art.Status = statusFailed
		art.Reason = fmt.Sprintf("cannot read the frozen protocol at %s: %v — a sample that cannot name the protocol it serves cannot be shown to obey it; pass --protocol-file", protocolFile, err)
		return art
	}

	manifest, blind, err := evalsample.Build(worlds, evalsample.Options{
		ProtocolID:           protocolID,
		ProtocolDigestSHA256: digest,
		Seed:                 seed,
		GeneratedAt:          capturedAt,
	})
	if err != nil {
		art.Status = statusFailed
		art.Reason = err.Error()
		return art
	}

	dir := filepath.Join(out, "sample")
	if err := os.MkdirAll(filepath.Join(dir, "blind"), 0o755); err != nil {
		art.Status = statusFailed
		art.Reason = err.Error()
		return art
	}
	if err := writeJSON(filepath.Join(dir, "sample-manifest.json"), manifest); err != nil {
		art.Status = statusFailed
		art.Reason = err.Error()
		return art
	}
	names := make([]string, 0, len(blind))
	for name := range blind {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := writeJSON(filepath.Join(dir, "blind", name+".json"), blind[name]); err != nil {
			art.Status = statusFailed
			art.Reason = err.Error()
			return art
		}
	}

	art.Status = statusRan
	art.ReportFile = filepath.Join("sample", "sample-manifest.json")
	art.ReportDigest = manifest.DigestSHA256
	art.SiteCoverage = fmt.Sprintf("%d item(s) across %d stratum/strata", len(manifest.Items), len(manifest.Strata))
	return art
}

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func fileDigest(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
