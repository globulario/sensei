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

// protocolID is the DEFAULT identity, paired with protocolPath above. It is
// overridable for the same reason the file is: a manifest that named one
// protocol while carrying another's digest would corrupt the very identity a
// reference-set release is supposed to pin.
const protocolID = "phase10-reference-protocol-v1"

// defaultProtocolDigest is the SHA-256 of the document at protocolPath.
//
// Compiled in because protocolPath is relative: an installed binary run from
// outside the repository cannot read it, and the earlier path-comparison
// fallback then misclassified a caller who passed the REAL v1 document by
// absolute path as using a custom protocol — rejecting a valid default pair
// before any arm ran. A digest travels with the binary and needs no working
// directory.
//
// TestTheCompiledProtocolDigestMatchesTheDocument fails if the document
// changes without this constant, so the two cannot drift apart silently.
const defaultProtocolDigest = "e686245ebfeb0885113046d9dfbefcfbab43c2457b1850fa4058ac1ac2f2288c"

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
func writeSample(out, protocolFile, protocolIDArg, protocolDigest string, protocolErr error, isDefaultProtocol bool, missingWorlds []string, worlds []evalsample.World, seed, capturedAt string) armArtifact {
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
		// A seed means the draw was REQUESTED, so "nothing to sample" is a
		// failure of what was asked for, not a quiet absence. The earlier
		// failure guard below sat after this branch and was therefore
		// unreachable here: a direct caller passing --selection-seed with no
		// --world got exit 0 and no manifest.
		art.Status = statusNotRun
		if strings.TrimSpace(seed) != "" {
			art.Status = statusFailed
		}
		art.Reason = "no evaluation world ran, so there is nothing to sample; supply --world"
		return art
	}
	// Refused, not merely noted. The default protocol consumes every world in
	// requiredWorlds, so a sample drawn from a subset would carry the v1
	// identity while following a reduced world definition — the same false
	// claim as substituting a world, arrived at by omission rather than
	// replacement, which is why it looked harmless.
	if isDefaultProtocol && len(missingWorlds) > 0 {
		// statusFailed, not statusNotRun. The caller asked for a draw by
		// supplying a seed; refusing it is a failure of what was requested, and
		// main's exit code counts only failures. Reporting not_run here let
		// automation see a successful command that produced no manifest —
		// silence indistinguishable from success, which is the shape of defect
		// this whole file exists to refuse.
		art.Status = statusFailed
		art.Reason = fmt.Sprintf("refusing to draw under the default protocol while %s did not run: the manifest would claim an identity whose world definition this run did not follow. Run the missing world(s), or bind a protocol that defines the reduced set with --protocol-file and --protocol-id.",
			strings.Join(missingWorlds, ", "))
		return art
	}
	if strings.TrimSpace(protocolIDArg) == "" {
		art.Status = statusFailed
		art.Reason = "no protocol id given; a manifest that does not name the protocol it obeys cannot be checked against one"
		return art
	}
	// The digest is the one validated at startup, not a fresh read. Re-reading
	// here would leave a window across the arms' runtime in which the file could
	// change, so the pair checked at startup and the digest recorded in the
	// manifest could describe different documents.
	if protocolErr != nil {
		art.Status = statusFailed
		art.Reason = fmt.Sprintf("cannot read the frozen protocol at %s: %v — a sample that cannot name the protocol it serves cannot be shown to obey it; pass --protocol-file", protocolFile, protocolErr)
		return art
	}
	digest := protocolDigest

	manifest, blind, err := evalsample.Build(worlds, evalsample.Options{
		ProtocolID:           protocolIDArg,
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
	// The index's report_digest is the digest of the BYTES ON DISK, exactly as
	// it is for every other arm. The manifest's own DigestSHA256 is a different
	// thing — a self-excluding identity over the selection, which a
	// reference-set release names as its sample_manifest_digest_sha256 — and it
	// cannot equal the file hash because the file contains it.
	//
	// Reporting the self-excluding identity here would make a verifier that
	// re-hashes the file, as it does for every other artifact, declare this one
	// altered on every single run.
	manifestPath := filepath.Join(dir, "sample-manifest.json")
	fileDigestHex, err := writeJSON(manifestPath, manifest)
	if err != nil {
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
		if _, err := writeJSON(filepath.Join(dir, "blind", name+".json"), blind[name]); err != nil {
			art.Status = statusFailed
			art.Reason = err.Error()
			return art
		}
	}

	art.Status = statusRan
	art.ReportFile = filepath.Join("sample", "sample-manifest.json")
	art.ReportDigest = fileDigestHex
	art.SampleManifestDigest = manifest.DigestSHA256
	art.SiteCoverage = fmt.Sprintf("%d item(s) across %d stratum/strata", len(manifest.Items), len(manifest.Strata))
	return art
}

// writeJSON writes the artifact and returns the digest of the bytes it wrote,
// so a caller never has to re-derive the hash of a file from a value it hopes
// serializes the same way twice.
func writeJSON(path string, v any) (string, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", err
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func fileDigest(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
