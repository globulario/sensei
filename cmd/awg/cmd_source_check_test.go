// SPDX-License-Identifier: Apache-2.0

package main

import (
	"regexp"
	"strings"
	"testing"
)

func TestEnclosingMethod(t *testing.T) {
	src := []string{
		"class Foo extends HTMLElement {", // 0
		"  connectedCallback() {",         // 1
		"    this.innerHTML = `<div>`",    // 2
		"  }",                             // 3
		"  refresh() {",                   // 4
		"    this.innerHTML = ''",         // 5
		"  }",                             // 6
		"}",                               // 7
	}
	cases := map[int]string{2: "connectedCallback", 5: "refresh"}
	for idx, want := range cases {
		if got := enclosingMethod(src, idx); got != want {
			t.Errorf("enclosingMethod(line %d) = %q, want %q", idx+1, got, want)
		}
	}
	// TypeScript return-type annotation must not defeat method detection.
	ts := []string{"  connectedCallback(): void {", "    this.innerHTML = `x`"}
	if got := enclosingMethod(ts, 1); got != "connectedCallback" {
		t.Errorf("TS return-type: enclosingMethod = %q, want connectedCallback", got)
	}
	async := []string{"  async _load(): Promise<void> {", "    this.innerHTML = ''"}
	if got := enclosingMethod(async, 1); got != "_load" {
		t.Errorf("async+return-type: enclosingMethod = %q, want _load", got)
	}
	// control-flow keywords are not methods
	ctrl := []string{"  if (x) {", "    this.innerHTML = ''"}
	if got := enclosingMethod(ctrl, 1); got == "if" {
		t.Errorf("enclosingMethod treated `if` as a method")
	}
}

// TestMatchViolations_MethodScope: scope:method excepts by ENCLOSING method —
// innerHTML in connectedCallback is fine; the same in render() is flagged.
func TestMatchViolations_MethodScope(t *testing.T) {
	cp := compiledPattern{
		sourcePattern: sourcePattern{ID: "sp.x", Scope: "method", Message: "m"},
		re:            regexp.MustCompile(`this\.innerHTML\s*=`),
		excepts:       []*regexp.Regexp{regexp.MustCompile(`connectedCallback`), regexp.MustCompile(`_buildShell`)},
	}
	lines := []string{
		"  connectedCallback() {",      // 1
		"    this.innerHTML = `<div>`", // 2  excepted
		"  }",                          // 3
		"  render() {",                 // 4
		"    this.innerHTML = ''",      // 5  flagged
		"  }",                          // 6
	}
	vs := matchViolations(cp, "f.js", lines, strings.Join(lines, "\n"))
	if len(vs) != 1 {
		t.Fatalf("got %d violations, want 1: %+v", len(vs), vs)
	}
	if vs[0].Line != 5 {
		t.Errorf("violation at line %d, want 5 (render); connectedCallback build must be excepted", vs[0].Line)
	}
}

// TestMatchViolations_ClassScope preserves the existing file-level AND semantics.
func TestMatchViolations_ClassScope(t *testing.T) {
	cp := compiledPattern{
		sourcePattern: sourcePattern{ID: "sp.timer", Scope: "class", Message: "m"},
		re:            regexp.MustCompile(`setInterval`),
		excepts:       []*regexp.Regexp{regexp.MustCompile(`disconnectedCallback`)},
	}
	// has setInterval but no disconnectedCallback → violation
	bad := []string{"setInterval(fn, 1000)"}
	if vs := matchViolations(cp, "a.js", bad, strings.Join(bad, "\n")); len(vs) != 1 {
		t.Errorf("class scope: want 1 violation, got %d", len(vs))
	}
	// has both → no violation
	ok := []string{"setInterval(fn, 1000)", "disconnectedCallback() {}"}
	if vs := matchViolations(cp, "b.js", ok, strings.Join(ok, "\n")); len(vs) != 0 {
		t.Errorf("class scope: want 0 violations (guard present), got %d", len(vs))
	}
}
