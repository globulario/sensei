// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"os"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/runtimedescriptor"
)

// withDescriptorHome isolates runtimedescriptor's ~/.local/share/sensei/runtime
// resolution to a temp HOME for the duration of the test.
func withDescriptorHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
}

func TestCheckOxigraphCompatibility_UnidentifiedOccupantHardFails(t *testing.T) {
	withDescriptorHome(t)
	ok, err := checkOxigraphCompatibility("127.0.0.1:7878", "/data")
	if ok {
		t.Fatal("expected an occupied address with no descriptor to be incompatible")
	}
	if err == nil || !strings.Contains(err.Error(), "no compatible runtime descriptor") {
		t.Fatalf("expected a no-descriptor diagnostic, got %v", err)
	}
}

func TestCheckOxigraphCompatibility_ExactDataDirMatchRequired(t *testing.T) {
	withDescriptorHome(t)
	if err := runtimedescriptor.Write(runtimedescriptor.Descriptor{
		Kind: runtimedescriptor.KindOxigraph, PID: os.Getpid(), ListenAddr: "127.0.0.1:7878", DataDir: "/data/a",
	}); err != nil {
		t.Fatal(err)
	}

	ok, err := checkOxigraphCompatibility("127.0.0.1:7878", "/data/a")
	if !ok || err != nil {
		t.Fatalf("expected an exact data-dir match to be compatible, got ok=%v err=%v", ok, err)
	}

	ok, err = checkOxigraphCompatibility("127.0.0.1:7878", "/data/b")
	if ok {
		t.Fatal("expected a mismatched data directory to be incompatible")
	}
	if err == nil || !strings.Contains(err.Error(), "data directory") ||
		!strings.Contains(err.Error(), "/data/a") || !strings.Contains(err.Error(), "/data/b") {
		t.Fatalf("expected a diagnostic naming both data directories, got %v", err)
	}
}

func TestCheckAwarenessCompatibility_UnidentifiedOccupantHardFails(t *testing.T) {
	withDescriptorHome(t)
	ok, err := checkAwarenessCompatibility(":10120", "http://localhost:7878/query", "/repo/.sensei/graph-authority.json", "/repo", "github.com/owner/repo")
	if ok {
		t.Fatal("expected an occupied address with no descriptor to be incompatible")
	}
	if err == nil || !strings.Contains(err.Error(), "no compatible runtime descriptor") {
		t.Fatalf("expected a no-descriptor diagnostic, got %v", err)
	}
}

func TestCheckAwarenessCompatibility_ExactMatchRequired(t *testing.T) {
	base := runtimedescriptor.Descriptor{
		Kind:             runtimedescriptor.KindAwarenessGraph,
		PID:              os.Getpid(),
		ListenAddr:       ":10120",
		OxigraphQueryURL: "http://localhost:7878/query",
		GraphMarkerFile:  "/repo/.sensei/graph-authority.json",
		RepoRoot:         "/repo",
		RepoDomain:       "github.com/owner/repo",
	}

	cases := []struct {
		name       string
		mutate     func(d runtimedescriptor.Descriptor) runtimedescriptor.Descriptor
		wantField  string
		compatible bool
	}{
		{"exact match", func(d runtimedescriptor.Descriptor) runtimedescriptor.Descriptor { return d }, "", true},
		{"mismatched oxigraph url", func(d runtimedescriptor.Descriptor) runtimedescriptor.Descriptor {
			d.OxigraphQueryURL = "http://localhost:7878/other"
			return d
		}, "oxigraph query url", false},
		{"mismatched marker file", func(d runtimedescriptor.Descriptor) runtimedescriptor.Descriptor {
			d.GraphMarkerFile = "/other/.sensei/graph-authority.json"
			return d
		}, "graph marker file", false},
		{"mismatched repo root", func(d runtimedescriptor.Descriptor) runtimedescriptor.Descriptor {
			d.RepoRoot = "/other"
			return d
		}, "repo root", false},
		{"mismatched repo domain", func(d runtimedescriptor.Descriptor) runtimedescriptor.Descriptor {
			d.RepoDomain = "github.com/owner/other"
			return d
		}, "repo domain", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withDescriptorHome(t)
			running := tc.mutate(base)
			if err := runtimedescriptor.Write(running); err != nil {
				t.Fatal(err)
			}
			ok, err := checkAwarenessCompatibility(base.ListenAddr, base.OxigraphQueryURL, base.GraphMarkerFile, base.RepoRoot, base.RepoDomain)
			if ok != tc.compatible {
				t.Fatalf("got ok=%v, want %v (err=%v)", ok, tc.compatible, err)
			}
			if !tc.compatible {
				if err == nil || !strings.Contains(err.Error(), tc.wantField) {
					t.Fatalf("expected diagnostic naming %q, got %v", tc.wantField, err)
				}
			}
		})
	}
}

// contract §3.7/#6: two checkouts differing only in repo-root/repo-domain
// must never be treated as the same execution habitat, even when every
// other field (oxigraph URL, marker file) happens to coincide.
func TestCheckAwarenessCompatibility_RejectsForeignCheckout(t *testing.T) {
	withDescriptorHome(t)
	checkoutA := runtimedescriptor.Descriptor{
		Kind:             runtimedescriptor.KindAwarenessGraph,
		PID:              os.Getpid(),
		ListenAddr:       ":10120",
		OxigraphQueryURL: "http://localhost:7878/query",
		GraphMarkerFile:  "/checkout-a/.sensei/graph-authority.json",
		RepoRoot:         "/checkout-a",
		RepoDomain:       "github.com/globulario/checkout-a",
	}
	if err := runtimedescriptor.Write(checkoutA); err != nil {
		t.Fatal(err)
	}

	ok, err := checkAwarenessCompatibility(
		":10120",
		"http://localhost:7878/query",
		"/checkout-b/.sensei/graph-authority.json",
		"/checkout-b",
		"github.com/globulario/checkout-b",
	)
	if ok {
		t.Fatal("expected checkout B to be rejected as incompatible with checkout A's running service")
	}
	if err == nil ||
		!strings.Contains(err.Error(), "/checkout-a") ||
		!strings.Contains(err.Error(), "/checkout-b") {
		t.Fatalf("expected the diagnostic to name both checkouts' marker/repo values, got %v", err)
	}
}

func TestFormatIncompatibleReuseError_NamesAddrAndPID(t *testing.T) {
	got := runtimedescriptor.Descriptor{PID: 4242, DataDir: "/data/a"}
	want := runtimedescriptor.Descriptor{DataDir: "/data/b"}
	err := formatIncompatibleReuseError(runtimedescriptor.KindOxigraph, "127.0.0.1:7878", got, want)
	for _, want := range []string{"127.0.0.1:7878", "4242", "/data/a", "/data/b"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("expected diagnostic to contain %q, got %q", want, err.Error())
		}
	}
}
