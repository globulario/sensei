// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/globulario/sensei/golang/client"
	awarenesspb "github.com/globulario/sensei/golang/pb"
)

func runImpact(args []string) int {
	fs := flag.NewFlagSet("sensei impact", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	file := fs.String("file", "", "repo-relative file path (required)")
	domain := fs.String("domain", "", "domain/repo scope (e.g. github.com/caddyserver/caddy); required when the graph hosts >1 domain")
	addr := fs.String("addr", defaultServiceAddr(), "AWG gRPC server address")
	asJSON := fs.Bool("json", false, "output as JSON")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: sensei impact --file <path> [flags]

Returns structured knowledge nodes (invariants, failure modes, etc.)
that touch the given file. More detailed than briefing.

Flags:
`)
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *file == "" {
		fmt.Fprintln(os.Stderr, "sensei impact: --file is required")
		return 2
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := client.DialConn(*addr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei impact: connect %s: %v\n", *addr, err)
		return 1
	}
	defer conn.Close()

	client := awarenesspb.NewAwarenessGraphClient(conn)
	resp, err := client.Impact(ctx, &awarenesspb.ImpactRequest{
		File:   *file,
		Domain: *domain,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei impact: %v\n", err)
		return 1
	}

	if *asJSON {
		return emitProtoJSON(resp)
	}

	// Human-readable output.
	printGraphAuthority(resp.GetAuthority())
	printNodes := func(title string, nodes []*awarenesspb.KnowledgeNode) {
		if len(nodes) == 0 {
			return
		}
		fmt.Printf("\n%s:\n", title)
		for _, n := range nodes {
			sev := ""
			if n.GetSeverity() != "" {
				sev = fmt.Sprintf(" [%s]", n.GetSeverity())
			}
			fmt.Printf("  - %s%s: %s\n", n.GetId(), sev, n.GetLabel())
			if desc := n.GetDescription(); desc != "" {
				fmt.Printf("    %s\n", desc)
			}
		}
	}

	printNodes("Invariants (direct)", resp.GetDirectInvariants())
	printNodes("Failure Modes (direct)", resp.GetDirectFailureModes())
	printNodes("Intents (direct)", resp.GetDirectIntents())
	printNodes("Architecture (direct)", resp.GetDirectArchitecture())
	printNodes("Forbidden fixes", resp.GetForbiddenFixes())
	printNodes("Required tests", resp.GetRequiredTests())
	printNodes("Invariants (inferred)", resp.GetInferredInvariants())
	printNodes("Failure Modes (inferred)", resp.GetInferredFailureModes())

	// Code symbols (from an ingested SCIP index) are structural, not governing
	// knowledge — but a file can have them with no invariants yet, so surface
	// them rather than reporting "nothing found" and hiding the graph's contents.
	syms := resp.GetSymbols()
	if len(syms) > 0 {
		fmt.Printf("\nCode symbols (%d):\n", len(syms))
		for i, s := range syms {
			if i >= 25 {
				fmt.Printf("  … and %d more\n", len(syms)-25)
				break
			}
			line := "  - " + s.GetId()
			if n := len(s.GetReferences()); n > 0 {
				line += fmt.Sprintf("  (references %d)", n)
			}
			fmt.Println(line)
		}
	}

	noKnowledge := len(resp.GetDirectInvariants()) == 0 &&
		len(resp.GetDirectFailureModes()) == 0 &&
		len(resp.GetDirectIntents()) == 0 &&
		len(resp.GetDirectArchitecture()) == 0
	if noKnowledge {
		if len(syms) > 0 {
			// Honest: the file IS in the graph (structure/symbols), it just has
			// no authored governing knowledge yet.
			fmt.Printf("\nNo governing knowledge nodes anchored to this file yet — structure only.\n" +
				"Author invariants / forbidden-fixes (or `sensei propose`) to govern it.\n")
		} else {
			fmt.Println("No knowledge nodes found for this file.")
		}
	}
	return 0
}
