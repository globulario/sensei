// SPDX-License-Identifier: AGPL-3.0-only

package gitobject

import (
	"strings"
	"testing"
)

const (
	sha1ID   = "5f999ba5354c2c9dcac57fba58dd841f61726cca"
	sha256ID = "fc45da2905b4f41d982b1d2c20b954b090713bc274be4be426210389004c5121"
)

// Both of Git's object formats, and nothing else.
//
// The 40-only reading is what this package exists to end: `git init
// --object-format=sha256` is documented and supported, and its `git rev-parse
// HEAD` returns 64 hex. Refusing that is refusing a valid identity.
//
// The other direction matters just as much: anything shorter is an
// ABBREVIATION, which names a commit probabilistically and cannot be compared
// for equality with one written out in full. A predicate that accepted "hex of
// any length" would admit both, which is why the widths are enumerated rather
// than bounded.
func TestOnlyGitsTwoObjectFormatsAreObjectIDs(t *testing.T) {
	for _, c := range []struct {
		name string
		in   string
		want bool
	}{
		{"sha-1, 40 hex", sha1ID, true},
		{"sha-256, 64 hex", sha256ID, true},
		{"upper case names the same object", strings.ToUpper(sha1ID), true},
		{"mixed case", "5F999ba5354c2c9dcac57FBA58dd841f61726CCA", true},
		{"surrounded by whitespace", "  " + sha256ID + "\n", true},

		{"abbreviation", "5f999ba5354c", false},
		{"39 hex, one short of sha-1", sha1ID[:39], false},
		{"41 hex, one past sha-1", sha1ID + "a", false},
		{"63 hex, one short of sha-256", sha256ID[:63], false},
		{"65 hex, one past sha-256", sha256ID + "a", false},
		{"52 hex, between the two widths", strings.Repeat("a", 52), false},
		{"empty", "", false},
		{"whitespace only", "   ", false},
		{"not hex", strings.Repeat("z", 40), false},
		{"hex with a non-hex tail", sha1ID[:39] + "g", false},
		{"a branch name", "refs/heads/main", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := IsObjectID(c.in); got != c.want {
				t.Errorf("IsObjectID(%q) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}
