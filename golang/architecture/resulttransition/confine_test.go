// SPDX-License-Identifier: Apache-2.0

package resulttransition

import "testing"

func TestConfinedPathRejectsTraversalAndAbsolute(t *testing.T) {
	bad := []string{"../escape.go", "src/../../etc/passwd", "/abs/path.go", "\\\\host\\share", "C:\\x", "a/../../b"}
	for _, p := range bad {
		if err := confinedPath(p); err == nil {
			t.Errorf("confinedPath(%q) = nil, want rejection", p)
		}
	}
	good := []string{"src/model.go", "a/b/c.txt", "deep/nested/dir/file"}
	for _, p := range good {
		if err := confinedPath(p); err != nil {
			t.Errorf("confinedPath(%q) = %v, want nil", p, err)
		}
	}
}

func TestSymlinkEscapeDetection(t *testing.T) {
	cases := []struct {
		link, target string
		escapes      bool
	}{
		{"src/link", "model.go", false},         // sibling inside repo
		{"src/link", "../docs/x.md", false},     // up one, still inside repo
		{"src/link", "../../etc/passwd", true},  // climbs above root
		{"link", "../outside", true},            // top-level up-escape
		{"a/b/link", "../../../x", true},        // deep escape
		{"src/link", "/etc/passwd", true},       // absolute
		{"src/link", "sub/deep/../file", false}, // internal traversal that stays inside
		{"link", "", false},                     // empty target
	}
	for _, c := range cases {
		if got := symlinkEscapes(c.link, c.target); got != c.escapes {
			t.Errorf("symlinkEscapes(%q,%q) = %v, want %v", c.link, c.target, got, c.escapes)
		}
	}
}
