package main

// G5: the command that reports canonical graph state must resolve its endpoint the
// same way a production reader does.
//
// Measured 2026-09-13: `sensei metadata` dialled 127.0.0.1:10120 — netcfg's declared
// DefaultServicePort — and failed, because nothing listens there. Two healthy
// awareness services were running at the time, :10121 for the sensei domain and
// :10122 for sensei-code, and a production reader (sensei-code) reaches them from its
// project configuration. So the one command whose job is to report the canonical
// state was the only participant unable to see any graph at all.
//
// The precedence was already established and already documented in
// cmd_edit_brief.go: an explicit flag is the operator naming the endpoint at the
// point of use and always wins; otherwise the project's own configuration decides;
// only then the built-in default. metadata simply did not follow it. This puts the
// rule in one place so a fourth command cannot invent a fourth precedence.

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func addrFlagSet(t *testing.T, passed bool, value string) *flag.FlagSet {
	t.Helper()
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	fs.String("addr", "localhost:10120", "")
	if passed {
		if err := fs.Parse([]string{"-addr", value}); err != nil {
			t.Fatal(err)
		}
	}
	return fs
}

// A project root whose config names an endpoint.
func projectNaming(t *testing.T, addr string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".sensei"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "sources:\n    - docs/awareness\n"
	if addr != "" {
		body += "server:\n    addr: " + addr + "\n"
	}
	if err := os.WriteFile(filepath.Join(root, ".sensei", "config.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestAnExplicitFlagAlwaysWins(t *testing.T) {
	root := projectNaming(t, "localhost:10122")
	got := resolveServiceAddr(addrFlagSet(t, true, "localhost:19999"), root, "localhost:19999")
	if got != "localhost:19999" {
		t.Errorf("resolved %q; an endpoint named on the command line must win", got)
	}
}

func TestTheProjectConfigBeatsTheBuiltInDefault(t *testing.T) {
	root := projectNaming(t, "localhost:10122")
	got := resolveServiceAddr(addrFlagSet(t, false, ""), root, "localhost:10120")
	if got != "localhost:10122" {
		t.Errorf("resolved %q, want the project's configured localhost:10122 — "+
			"the built-in default is netcfg's 10120, which nothing listens on here", got)
	}
}

func TestTheBuiltInDefaultIsUsedOnlyWhenNothingElseSpeaks(t *testing.T) {
	root := projectNaming(t, "")
	got := resolveServiceAddr(addrFlagSet(t, false, ""), root, "localhost:10120")
	if got != "localhost:10120" {
		t.Errorf("resolved %q, want the built-in default when neither flag nor config names one", got)
	}
	// An unreadable project is not a reason to invent an endpoint: fall back, do
	// not fail, and do not guess something else.
	if got := resolveServiceAddr(addrFlagSet(t, false, ""), filepath.Join(t.TempDir(), "absent"), "localhost:10120"); got != "localhost:10120" {
		t.Errorf("resolved %q for an unreadable project root", got)
	}
}

// G5 also requires the report to say WHICH endpoint answered and where that choice
// came from. Without it, two runs from different directories print different numbers
// and the output does not say why — which is the confusion this whole front is about.
func TestTheReportNamesTheEndpointAndWhereItCameFrom(t *testing.T) {
	root := projectNaming(t, "localhost:10122")
	if got := serviceAddrSource(addrFlagSet(t, true, "localhost:19999"), root); got != "named on the command line" {
		t.Errorf("source = %q for an explicit flag", got)
	}
	if got := serviceAddrSource(addrFlagSet(t, false, ""), root); got != "this project's configuration" {
		t.Errorf("source = %q when the project names one", got)
	}
	if got := serviceAddrSource(addrFlagSet(t, false, ""), projectNaming(t, "")); got != "the built-in default" {
		t.Errorf("source = %q when nothing else speaks", got)
	}
}

// Law 10: a marker cannot certify a different generation/store than the one being
// served. Today nothing compares them, so a divergence is invisible — measured on
// 2026-09-13, a disposable generation wrote digest 230a74f6…/35,255 while the live
// marker still said c0b660fc…/35,268, and no command would have said so.
func TestTheReportSaysWhetherTheMarkerAgreesWithTheServedGraph(t *testing.T) {
	agree := markerAgreement("c0b660fc42a5", 35268, "c0b660fc42a5", 35268)
	if !strings.Contains(agree, "agrees") {
		t.Errorf("matching marker and graph reported as %q", agree)
	}

	for name, tc := range map[string]struct {
		md string
		mt int
	}{
		"different digest": {"230a74f68fed", 35268},
		"different count":  {"c0b660fc42a5", 35255},
		"both differ":      {"230a74f68fed", 35255},
	} {
		got := markerAgreement("c0b660fc42a5", 35268, tc.md, tc.mt)
		if !strings.Contains(got, "DOES NOT") {
			t.Errorf("%s: divergence reported as %q", name, got)
		}
		if !strings.Contains(got, "c0b660fc42a5") || !strings.Contains(got, tc.md) {
			t.Errorf("%s: the report does not name both identities: %q", name, got)
		}
	}

	// A marker that states nothing is not agreement. Law 13: a count alone is
	// evidence, never identity, so an absent digest cannot be read as a match.
	if got := markerAgreement("c0b660fc42a5", 35268, "", 35268); !strings.Contains(got, "cannot") {
		t.Errorf("an absent marker digest was read as agreement: %q", got)
	}
}
