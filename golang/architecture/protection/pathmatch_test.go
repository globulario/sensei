// SPDX-License-Identifier: AGPL-3.0-only

package protection

import "testing"

func TestNormalizePath_ValidCases(t *testing.T) {
	cases := map[string]string{
		"src/auth/login.go":   "src/auth/login.go",
		"./src/auth/login.go": "src/auth/login.go",
		"/src/auth/login.go":  "src/auth/login.go",
		"src\\auth\\login.go": "src/auth/login.go",
		"src/./auth/login.go": "src/auth/login.go",
	}
	for in, want := range cases {
		got, ok := NormalizePath(in)
		if !ok {
			t.Errorf("NormalizePath(%q): expected ok, got not-ok", in)
			continue
		}
		if got != want {
			t.Errorf("NormalizePath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizePath_RejectsEscapes(t *testing.T) {
	bad := []string{
		"",
		"   ",
		"../etc/passwd",
		"../../x",
		"a/../../b",
		`C:\Windows\System32`,
		`\\host\share\file`,
	}
	for _, in := range bad {
		if _, ok := NormalizePath(in); ok {
			t.Errorf("NormalizePath(%q): expected reject, got accepted", in)
		}
	}
}

// contract §3.7: "avoid src/auth/ matching src/authorization/ unless
// explicitly intended" — the exact bug class this package must never
// reintroduce.
func TestInPathScope_SegmentBoundarySafe(t *testing.T) {
	if InPathScope("src/authorization/x.go", "src/auth") {
		t.Fatal("src/auth must not match src/authorization/x.go (segment-boundary violation)")
	}
	if !InPathScope("src/auth/login.go", "src/auth") {
		t.Fatal("src/auth must match src/auth/login.go")
	}
	if !InPathScope("src/auth", "src/auth") {
		t.Fatal("a prefix must match its own exact path")
	}
	if InPathScope("src/auth2/x.go", "src/auth/") {
		t.Fatal("src/auth/ must not match src/auth2/x.go")
	}
}

func TestInPathScope_TrailingSlashAgnostic(t *testing.T) {
	if !InPathScope("golang/server/main.go", "golang/server/") {
		t.Fatal("trailing slash on prefix must still match a descendant")
	}
	if !InPathScope("golang/server/main.go", "golang/server") {
		t.Fatal("no trailing slash on prefix must still match a descendant")
	}
}
