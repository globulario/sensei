// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	awarenesspb "github.com/globulario/sensei/golang/pb"
)

const (
	exportTestDomain = "github.com/globulario/sensei-code"
	exportTestGen    = "c0b660fc42a50c4be4741592de178dfff3216c9b873d455c37cc864e27f01705"
	exportTestNT     = "<https://example.org/a> <https://example.org/p> \"A\" .\n"
)

// exportHarness points the command at a temp HOME (so the registry it reads is
// the fixture's, never the operator's) and substitutes the transport.
//
// The transport is substituted; what the command DECIDES about the answer is
// not. That is the whole point of these tests: the registry comparison lives in
// this command, so it has to be exercised here.
func exportHarness(t *testing.T, registry string, served *awarenesspb.GetDomainGraphResponse, rpcErr error) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	if registry != "" {
		if err := os.MkdirAll(filepath.Join(home, ".sensei"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(home, ".sensei", "domains.yaml"), []byte(registry), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	prev := getDomainGraphRPC
	getDomainGraphRPC = func(context.Context, string, string) (*awarenesspb.GetDomainGraphResponse, error) {
		return served, rpcErr
	}
	t.Cleanup(func() { getDomainGraphRPC = prev })
	return t.TempDir()
}

func servedResponse() *awarenesspb.GetDomainGraphResponse {
	return &awarenesspb.GetDomainGraphResponse{
		Domain:      exportTestDomain,
		Generation:  exportTestGen,
		GraphDigest: exportTestGen,
		TripleCount: 1,
		Format:      "ntriples",
		Ntriples:    []byte(exportTestNT),
	}
}

func registryDeclaring(generation string) string {
	return "domains:\n  " + exportTestDomain + ":\n    active_generation: \"" + generation + "\"\n"
}

// The whole point: the served graph IS the declared ACTIVE generation, so the
// snapshot is written with provenance naming exactly what was proven.
func TestGraphExportWritesTheServedActiveGraphWithItsProvenance(t *testing.T) {
	out := filepath.Join(exportHarness(t, registryDeclaring(exportTestGen), servedResponse(), nil), "graph.nt")

	code, stdout, stderr := captureStdoutStderr(t, func() int {
		return runGraphExport([]string{"--domain", exportTestDomain, "--out", out})
	})
	if code != 0 {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	nt, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if string(nt) != exportTestNT {
		t.Fatalf("snapshot bytes = %q, want the served graph verbatim", nt)
	}

	blob, err := os.ReadFile(out + graphExportMetaSuffix)
	if err != nil {
		t.Fatalf("provenance: %v", err)
	}
	var meta snapshotProvenance
	if err := json.Unmarshal(blob, &meta); err != nil {
		t.Fatalf("provenance is not readable: %v", err)
	}
	if meta.Domain != exportTestDomain || meta.Generation != exportTestGen || meta.GraphDigest != exportTestGen {
		t.Fatalf("provenance does not name the exported graph: %+v", meta)
	}
	if meta.Source != "active" || meta.Format != "ntriples" || meta.TripleCount != 1 {
		t.Fatalf("provenance does not state what was proven: %+v", meta)
	}
	if !strings.Contains(stdout, exportTestGen) {
		t.Fatalf("stdout does not name the generation: %q", stdout)
	}
}

// Every way of failing to establish that the served graph is the ACTIVE one
// refuses, and writes NOTHING. A snapshot on disk is evidence; a snapshot
// written beside a refusal is a lie with a timestamp.
func TestGraphExportRefusesWhenTheServedGraphIsNotProvenActive(t *testing.T) {
	otherGen := strings.Repeat("0", 64)

	for name, c := range map[string]struct {
		registry string
		served   *awarenesspb.GetDomainGraphResponse
		want     string
	}{
		"the registry declares a different generation": {
			registry: registryDeclaring(otherGen),
			served:   servedResponse(),
			want:     "ambiguous graph identity",
		},
		"no ACTIVE generation is declared": {
			registry: "domains:\n  " + exportTestDomain + ":\n    endpoint: \"localhost:10122\"\n",
			served:   servedResponse(),
			want:     "no ACTIVE generation is declared",
		},
		"no registry at all": {
			registry: "",
			served:   servedResponse(),
			want:     "no ACTIVE generation is declared",
		},
		"the server states no generation": {
			registry: registryDeclaring(exportTestGen),
			served: &awarenesspb.GetDomainGraphResponse{
				Domain: exportTestDomain, Format: "ntriples", Ntriples: []byte(exportTestNT),
			},
			want: "unverifiable",
		},
	} {
		t.Run(name, func(t *testing.T) {
			out := filepath.Join(exportHarness(t, c.registry, c.served, nil), "graph.nt")

			code, _, stderr := captureStdoutStderr(t, func() int {
				return runGraphExport([]string{"--domain", exportTestDomain, "--out", out})
			})
			if code == 0 {
				t.Fatalf("the export succeeded; stderr=%q", stderr)
			}
			if !strings.Contains(stderr, c.want) {
				t.Fatalf("stderr=%q, want it to say %q", stderr, c.want)
			}
			for _, p := range []string{out, out + graphExportMetaSuffix} {
				if _, err := os.Stat(p); err == nil {
					t.Fatalf("a refused export wrote %s", p)
				}
			}
		})
	}
}

// A snapshot of an unnamed domain names no repository, so the command refuses
// before it asks anything.
func TestGraphExportRequiresADomain(t *testing.T) {
	asked := false
	home := t.TempDir()
	t.Setenv("HOME", home)
	prev := getDomainGraphRPC
	getDomainGraphRPC = func(context.Context, string, string) (*awarenesspb.GetDomainGraphResponse, error) {
		asked = true
		return servedResponse(), nil
	}
	defer func() { getDomainGraphRPC = prev }()

	code, _, stderr := captureStdoutStderr(t, func() int { return runGraphExport(nil) })
	if code == 0 {
		t.Fatal("an export with no domain succeeded")
	}
	if asked {
		t.Fatal("the command asked the service before establishing which domain it meant")
	}
	if !strings.Contains(stderr, "--domain is required") {
		t.Fatalf("stderr=%q", stderr)
	}
}
