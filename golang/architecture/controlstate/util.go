// SPDX-License-Identifier: AGPL-3.0-only

package controlstate

import "sort"

// sortedUnique returns the input as a sorted, de-duplicated slice (nil-safe, deterministic).
func sortedUnique(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
