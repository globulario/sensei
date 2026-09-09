// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"strings"
	"testing"
)

// namesRepository was the ONE validator of six that had the widths right, and
// it must keep them after being delegated to the shared predicate.
//
// This is the behaviour-preservation half of that change. It is asserted
// directly on namesRepository rather than only on gitobject.IsObjectID, because
// what a caller depends on is this function, and "the helper is right" and
// "this function still calls the helper" are two different claims.
func TestNamesRepositoryAcceptsBothObjectFormats(t *testing.T) {
	const (
		sha1   = "5f999ba5354c2c9dcac57fba58dd841f61726cca"
		sha256 = "fc45da2905b4f41d982b1d2c20b954b090713bc274be4be426210389004c5121"
	)
	for _, c := range []struct {
		name string
		in   string
		want bool
	}{
		{"sha-1", sha1, true},
		{"sha-256", sha256, true},
		{"upper case, as before", strings.ToUpper(sha1), true},
		{"mixed case, as before", "5F999ba5354c2c9dcac57FBA58dd841f61726CCA", true},

		{"abbreviation", "5f999ba5354c", false},
		{"39 hex", sha1[:39], false},
		{"41 hex", sha1 + "a", false},
		{"63 hex", sha256[:63], false},
		{"65 hex", sha256 + "a", false},
		{"not hex", strings.Repeat("z", 40), false},
		{"empty", "", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := namesRepository(c.in); got != c.want {
				t.Errorf("namesRepository(%q) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}
