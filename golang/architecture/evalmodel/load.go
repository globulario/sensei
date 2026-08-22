// SPDX-License-Identifier: AGPL-3.0-only

package evalmodel

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

// LabelFile is one adjudicator's frozen output. The protocol keeps labels in
// their own files, separate from the release manifest that names them: a
// manifest cannot list the digest of a file it is itself inside, and more
// importantly the labels and the release are produced by different people at
// different times.
type LabelFile struct {
	SchemaVersion string           `json:"schema_version"`
	AdjudicatorID string           `json:"adjudicator_id"`
	Labels        []ReferenceLabel `json:"labels"`
}

// LoadReferenceSet reads a frozen release manifest plus the label files it
// names, verifying both before any score can consume them.
//
// Three checks, and each closes a different way an answer key could be wrong:
//
//   - the release's published identity must be the one its own constituents
//     produce, so an edited release cannot appear under the digest of the one
//     that was frozen;
//   - every label file's BYTES must hash to a digest the release lists, so a
//     caller cannot hand the scorer labels no published release carries;
//   - every label file the release names must actually be supplied, so a
//     release cannot be scored against a subset of its own adjudication.
func LoadReferenceSet(manifestPath string, labelFilePaths []string) (ReferenceSet, error) {
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return ReferenceSet{}, err
	}
	var ref ReferenceSet
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&ref); err != nil {
		return ReferenceSet{}, fmt.Errorf("release manifest is outside the closed schema: %w", err)
	}
	if len(ref.Labels) > 0 {
		return ReferenceSet{}, fmt.Errorf("release manifest carries inline labels; labels belong in the label files the manifest names")
	}
	if strings.TrimSpace(ref.DigestSHA256) == "" {
		return ReferenceSet{}, fmt.Errorf("release carries no published identity; an unfrozen ruler cannot support a defensible score")
	}
	if computed := ReferenceDigest(ref); computed != ref.DigestSHA256 {
		return ReferenceSet{}, fmt.Errorf("release publishes identity %s but its constituents produce %s", short(ref.DigestSHA256), short(computed))
	}

	named := map[string]bool{}
	for _, d := range ref.LabelFileDigestsSHA256 {
		named[strings.TrimSpace(d)] = true
	}
	seen := map[string]bool{}
	for _, path := range labelFilePaths {
		body, err := os.ReadFile(path)
		if err != nil {
			return ReferenceSet{}, err
		}
		sum := sha256.Sum256(body)
		digest := hex.EncodeToString(sum[:])
		if !named[digest] {
			return ReferenceSet{}, fmt.Errorf("label file %s hashes to %s, which the release does not name", path, short(digest))
		}
		if seen[digest] {
			return ReferenceSet{}, fmt.Errorf("label file %s was supplied twice", path)
		}
		seen[digest] = true

		var file LabelFile
		fdec := json.NewDecoder(bytes.NewReader(body))
		fdec.DisallowUnknownFields()
		if err := fdec.Decode(&file); err != nil {
			return ReferenceSet{}, fmt.Errorf("label file %s is outside the closed schema: %w", path, err)
		}
		ref.Labels = append(ref.Labels, file.Labels...)
	}
	// A release scored against only some of its adjudication would report a
	// partial ruler under the full release's identity.
	if len(seen) != len(named) {
		return ReferenceSet{}, fmt.Errorf("release names %d label file(s) but %d were supplied", len(named), len(seen))
	}
	sort.SliceStable(ref.Labels, func(i, j int) bool { return ref.Labels[i].ItemKey < ref.Labels[j].ItemKey })
	return ref, nil
}

func short(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}
