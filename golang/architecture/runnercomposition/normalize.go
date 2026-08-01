// SPDX-License-Identifier: AGPL-3.0-only

package runnercomposition

import "sort"

// NormalizeCandidateArtifact returns a copy of a with Manifest defaulted to
// a non-nil empty slice and sorted by Path, and each entry's Content
// defaulted to a non-nil empty slice, so two semantically identical
// artifacts always encode identically before digesting, and Content never
// marshals to JSON null (see CandidateManifestEntry's "never nil" contract
// in types.go). It performs simple structural normalization only -- it does
// not validate mode/content consistency or reject duplicate paths;
// CanonicalizeManifest (called by ManifestDigest) does that.
func NormalizeCandidateArtifact(a CandidateArtifact) CandidateArtifact {
	sorted := make([]CandidateManifestEntry, len(a.Manifest))
	copy(sorted, a.Manifest)
	for i, e := range sorted {
		if e.Content == nil {
			sorted[i].Content = []byte{}
		}
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Path < sorted[j].Path })
	a.Manifest = sorted
	return a
}

// NormalizeRunnerReceipt returns r unchanged. Defined for symmetry with
// every other document's Normalize function in this codebase -- RunnerReceipt
// has no slice or map field whose encoding order is ambiguous today.
func NormalizeRunnerReceipt(r RunnerReceipt) RunnerReceipt {
	return r
}
