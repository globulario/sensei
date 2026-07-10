// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"reflect"
	"testing"

	awarenesspb "github.com/globulario/sensei/golang/pb"
)

func TestSymbolFileOf(t *testing.T) {
	cases := map[string]string{
		"golang/server/server.go:Impact": "golang/server/server.go",
		"cmd/awg/main.go:runGate":        "cmd/awg/main.go",
		"noColon":                        "noColon",
		"external:fmt.Sprintf":           "external", // externals are filtered upstream
	}
	for in, want := range cases {
		if got := symbolFileOf(in); got != want {
			t.Errorf("symbolFileOf(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLabelOfSymbol(t *testing.T) {
	if got := labelOfSymbol("golang/server/server.go:Impact"); got != "Impact" {
		t.Errorf("labelOfSymbol = %q, want Impact", got)
	}
	if got := labelOfSymbol("bare"); got != "bare" {
		t.Errorf("labelOfSymbol(bare) = %q, want bare", got)
	}
}

func TestAnalyzeReferenceFamily(t *testing.T) {
	// 9 sibling call-sites of one target across 9 files; the diff changed 3.
	changed := map[string]bool{"a.go": true, "b.go": true, "c.go": true}
	sites := []string{
		"a.go:f1", "b.go:f2", "c.go:f3", // touched
		"d.go:f4", "e.go:f5", "f.go:f6", "g.go:f7", "h.go:f8", "i.go:f9", // missed
	}
	total, touched, missed := analyzeReferenceFamily(sites, changed)
	if total != 9 {
		t.Errorf("total = %d, want 9", total)
	}
	if touched != 3 {
		t.Errorf("touched = %d, want 3", touched)
	}
	wantMissed := []string{"d.go", "e.go", "f.go", "g.go", "h.go", "i.go"}
	if !reflect.DeepEqual(missed, wantMissed) {
		t.Errorf("missed = %v, want %v", missed, wantMissed)
	}
}

func TestAnalyzeReferenceFamily_TwoSitesOneFile(t *testing.T) {
	// Two referencing symbols in the SAME unchanged file collapse to one missed
	// file; two in a changed file both count as touched.
	changed := map[string]bool{"a.go": true}
	sites := []string{"a.go:f1", "a.go:f2", "b.go:g1", "b.go:g2"}
	total, touched, missed := analyzeReferenceFamily(sites, changed)
	if total != 4 || touched != 2 {
		t.Errorf("total=%d touched=%d, want 4/2", total, touched)
	}
	if !reflect.DeepEqual(missed, []string{"b.go"}) {
		t.Errorf("missed = %v, want [b.go]", missed)
	}
}

func TestShouldReportFamily(t *testing.T) {
	cases := []struct {
		name           string
		total, touched int
		missed         []string
		maxFanout      int
		want           bool
	}{
		{"partial small family flags", 9, 3, []string{"d.go"}, 15, true},
		{"nothing touched: not our signal", 9, 0, []string{"d.go"}, 15, false},
		{"nothing missed: complete", 3, 3, nil, 15, false},
		{"single site: no siblings", 1, 1, nil, 15, false},
		{"too big: shared utility", 40, 3, []string{"d.go"}, 15, false},
		{"exactly at fanout cap flags", 15, 1, []string{"d.go"}, 15, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldReportFamily(tc.total, tc.touched, tc.missed, tc.maxFanout); got != tc.want {
				t.Errorf("shouldReportFamily(%d,%d,%v,%d) = %v, want %v",
					tc.total, tc.touched, tc.missed, tc.maxFanout, got, tc.want)
			}
		})
	}
}

func TestCollectInternalTargets(t *testing.T) {
	syms := []*awarenesspb.CodeSymbolNode{
		{Id: "a.go:f1", References: []string{"pkg/x.go:Foo", "external:fmt.Sprintf", ""}},
		{Id: "a.go:f2", References: []string{"pkg/x.go:Foo", "pkg/y.go:Bar"}}, // Foo dedups
	}
	got := collectInternalTargets(syms)
	want := []string{"pkg/x.go:Foo", "pkg/y.go:Bar"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("collectInternalTargets = %v, want %v (externals/empties excluded, deduped, sorted)", got, want)
	}
}
