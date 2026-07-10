// SPDX-License-Identifier: AGPL-3.0-only

// @awareness namespace=globular.awareness_graph
// @awareness component=extractor
// @awareness file_role=canonical_ntriples_dedup
// @awareness enforces=globular.awareness_graph:invariant.awareness.rdf.yaml_is_source_of_truth
package extractor

import "bytes"

// DedupNTriples removes duplicate non-empty, non-comment lines from an
// N-Triples byte buffer while preserving first-occurrence order. Returns the
// deduplicated bytes, the unique-triple count, and the number of duplicates
// suppressed. Empty lines and comment lines pass through unchanged.
//
// This is the ONE canonical dedup for generated graph output. Both yaml2nt
// (the canonical seed pipeline) and awg (rebuild/audit) MUST use it: the
// 2026-06-12 audit drift — committed seed 20,976 lines vs awg-generated
// 30,050 — happened because awg generated without deduping while yaml2nt
// deduped, so the freshness check compared two different computations of
// the same truth (meta.identity_computation_must_be_invariant).
func DedupNTriples(in []byte) (out []byte, uniqueCount, dupCount int) {
	seen := make(map[string]struct{}, len(in)/100)
	buf := make([]byte, 0, len(in))
	start := 0
	for i := 0; i <= len(in); i++ {
		if i == len(in) || in[i] == '\n' {
			line := in[start:i]
			trimmed := bytes.TrimSpace(line)
			if len(trimmed) == 0 || trimmed[0] == '#' {
				// structural — keep
				buf = append(buf, line...)
				if i < len(in) {
					buf = append(buf, '\n')
				}
			} else {
				key := string(trimmed)
				if _, ok := seen[key]; ok {
					dupCount++
				} else {
					seen[key] = struct{}{}
					uniqueCount++
					buf = append(buf, line...)
					if i < len(in) {
						buf = append(buf, '\n')
					}
				}
			}
			start = i + 1
		}
	}
	return buf, uniqueCount, dupCount
}
