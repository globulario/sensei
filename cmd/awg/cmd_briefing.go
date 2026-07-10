// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/globulario/awareness-graph/golang/client"
	awarenesspb "github.com/globulario/awareness-graph/golang/pb"
)

func runBriefing(args []string) int {
	fs := flag.NewFlagSet("awg briefing", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	file := fs.String("file", "", "repo-relative file path")
	task := fs.String("task", "", "task description")
	depth := fs.String("depth", "standard", "briefing depth: agent_compact | compact | standard | deep")
	domain := fs.String("domain", "", "domain/repo scope (e.g. github.com/caddyserver/caddy); required when the graph hosts >1 domain")
	addr := fs.String("addr", "localhost:10120", "AWG gRPC server address")
	asJSON := fs.Bool("json", false, "output as JSON")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: awg briefing [--file <path>] [--task "description"] [flags]

Queries the awareness graph for context relevant to a file or task.
Returns invariants, forbidden fixes, required tests, and failure modes.

At least one of --file or --task is required.

Flags:
`)
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *file == "" && *task == "" {
		fmt.Fprintln(os.Stderr, "awg briefing: provide --file and/or --task")
		return 2
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := client.DialConn(*addr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "awg briefing: connect %s: %v\n", *addr, err)
		return 1
	}
	defer conn.Close()

	client := awarenesspb.NewAwarenessGraphClient(conn)
	resp, err := client.Briefing(ctx, &awarenesspb.BriefingRequest{
		File:   *file,
		Task:   *task,
		Depth:  *depth,
		Domain: *domain,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "awg briefing: %v\n", err)
		return 1
	}

	if *asJSON {
		return emitProtoJSON(resp)
	}

	// Human-readable output.
	fmt.Printf("Status: %s\n", resp.GetStatus())
	printGraphAuthority(resp.GetAuthority())
	if prose := resp.GetProse(); prose != "" {
		fmt.Printf("\n%s\n", prose)
	}
	if refs := resp.GetReferencedIds(); len(refs) > 0 {
		fmt.Printf("\nReferenced IDs:\n")
		for _, ref := range refs {
			fmt.Printf("  - %s\n", ref)
		}
	}
	if ms := resp.GetGeneratedInMs(); ms > 0 {
		fmt.Printf("\n(generated in %dms)\n", ms)
	}
	return 0
}
