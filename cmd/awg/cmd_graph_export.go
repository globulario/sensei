// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	awarenesspb "github.com/globulario/sensei/golang/pb"
)

// `sensei graph-export` writes the graph the service serves to a file.
//
// It exists because a graph snapshot was an input nothing could produce. Task
// preparation consumes `.sensei/project/graph.nt` and hashes whatever file it
// is handed, so the CALLER effectively chose part of a task's identity, and the
// only producer of that path was a promotion transaction -- a mutation, run to
// obtain something a reader needed. A read-only prerequisite that can change the
// authority it is supposed to observe is the wrong ownership boundary.
//
// This command is deliberately boring, and that is the design:
//
//	resolve the endpoint (the one G2 owner)
//	ask the service to export the domain it serves
//	compare what it served with what the registry declares ACTIVE
//	write the bytes and their provenance
//
// It never queries the store, never assembles triples, and never decides which
// generation is current. The service owns the bytes; the registry owns ACTIVE;
// this command transports and records. The caller may transport a snapshot; it
// must not choose that snapshot's authority.
const graphExportMetaSuffix = ".meta.json"

// snapshotProvenance is written beside the exported graph.
//
// A `.nt` alone is an anonymous pile of triples: nothing in it says which domain
// it belongs to, which generation produced it, or that it came from a served
// graph rather than a local rebuild. A consumer would then have to infer its
// authority, which is the inference this whole surface removes.
type snapshotProvenance struct {
	Domain      string `json:"domain"`
	Generation  string `json:"generation"`
	GraphDigest string `json:"graph_digest"`
	TripleCount int64  `json:"triple_count"`
	// Source is how this snapshot was obtained, as a closed vocabulary. "active"
	// means: exported from the served graph AND equal to the generation the
	// registry declares ACTIVE. Anything weaker gets its own value rather than
	// this one, so a reader never has to guess what was actually proven.
	Source     string `json:"source"`
	Format     string `json:"format"`
	Endpoint   string `json:"endpoint"`
	ExportedAt string `json:"exported_at"`
}

func runGraphExport(args []string) int {
	fs := flag.NewFlagSet("sensei graph-export", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	addr := fs.String("addr", "", "Sensei gRPC server address")
	domain := fs.String("domain", "", "publication domain to export (e.g. github.com/globulario/sensei-code)")
	out := fs.String("out", "", "output path for the N-Triples snapshot (default: <project>/.sensei/project/graph.nt)")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: sensei graph-export --domain <domain> [flags]

Materialize, read-only, the exact graph the service serves for one domain, with
its identity, and write it plus a provenance sidecar.

Exporting changes nothing: no promotion, no marker write, no generation change.
The snapshot is refused rather than written when the served graph and its
authority marker disagree, or when the served generation is not the one the
domain registry declares ACTIVE.

Flags:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*domain) == "" {
		fmt.Fprintln(os.Stderr, "sensei graph-export: --domain is required; a snapshot of an unnamed domain names no repository")
		return 2
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	root, rootErr := resolveProjectRoot("")
	registryPath := DefaultDomainRegistryPath()
	reader := resolveGraphReader(fs, root, *domain, *addr, registryPath)

	resp, err := getDomainGraphRPC(ctx, reader.Addr, *domain)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei graph-export: %s\n", formatReadSurfaceError("graph-export", err))
		return 1
	}

	// THE REGISTRY COMPARISON LIVES HERE, and only here.
	//
	// The service cannot make it: the registry is operator-owned and outside any
	// published repository, precisely so a repository cannot declare its own
	// graph active. So the service states what it SERVES, and this is where that
	// is checked against what an operator DECLARED. Neither record vouches for
	// itself, which is the property the check exists to preserve.
	declared := declaredActiveGeneration(registryPath, *domain)
	if strings.TrimSpace(declared) == "" {
		fmt.Fprintf(os.Stderr, "sensei graph-export: no ACTIVE generation is declared for %s in %s, "+
			"so nothing here proves the served graph is the intended one; the snapshot is not written\n",
			*domain, registryPathOrNone(registryPath))
		return 1
	}
	if err := verifyActiveGeneration(*domain, declared, resp.GetGeneration()); err != nil {
		fmt.Fprintf(os.Stderr, "sensei graph-export: %v\n", err)
		return 1
	}

	target := strings.TrimSpace(*out)
	if target == "" {
		if rootErr != nil {
			fmt.Fprintf(os.Stderr, "sensei graph-export: no project root could be resolved (%v), so the default "+
				"output path would mean something different from each directory; pass --out\n", rootErr)
			return 1
		}
		target = filepath.Join(root, ".sensei", "project", "graph.nt")
	}
	if err := writeFileAtomic(target, resp.GetNtriples()); err != nil {
		fmt.Fprintf(os.Stderr, "sensei graph-export: %v\n", err)
		return 1
	}
	meta := snapshotProvenance{
		Domain:      resp.GetDomain(),
		Generation:  resp.GetGeneration(),
		GraphDigest: resp.GetGraphDigest(),
		TripleCount: resp.GetTripleCount(),
		Source:      "active",
		Format:      resp.GetFormat(),
		Endpoint:    reader.Addr,
		ExportedAt:  time.Now().UTC().Format(time.RFC3339),
	}
	blob, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei graph-export: %v\n", err)
		return 1
	}
	if err := writeFileAtomic(target+graphExportMetaSuffix, append(blob, '\n')); err != nil {
		fmt.Fprintf(os.Stderr, "sensei graph-export: %v\n", err)
		return 1
	}

	fmt.Printf("Exported the served ACTIVE graph.\n")
	fmt.Printf("  domain       %s\n", meta.Domain)
	fmt.Printf("  generation   %s\n", meta.Generation)
	fmt.Printf("  triples      %d\n", meta.TripleCount)
	fmt.Printf("  endpoint     %s\n", meta.Endpoint)
	fmt.Printf("  snapshot     %s\n", target)
	fmt.Printf("  provenance   %s\n", target+graphExportMetaSuffix)
	return 0
}

// getDomainGraphRPC is a variable for the same reason metadataRPC is: a test can
// substitute the transport without substituting what this command DECIDES about
// the answer.
var getDomainGraphRPC = func(ctx context.Context, addr, domain string) (*awarenesspb.GetDomainGraphResponse, error) {
	c, err := connectAWG(addr)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	return c.GetDomainGraph(ctx, domain)
}
