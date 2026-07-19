// SPDX-License-Identifier: Apache-2.0

package inference

import "sort"

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		if k != "" {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

func dedupeStrings(in []string) []string {
	seen := map[string]bool{}
	for _, s := range in {
		if s != "" {
			seen[s] = true
		}
	}
	return sortedKeys(seen)
}
