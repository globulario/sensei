// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/globulario/sensei/golang/architecture/tasksession"
	"github.com/globulario/sensei/golang/client"
	awarenesspb "github.com/globulario/sensei/golang/pb"
)

func runBriefing(args []string) int {
	fs := flag.NewFlagSet("sensei briefing", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	file := fs.String("file", "", "repo-relative file path")
	task := fs.String("task", "", "task description")
	depth := fs.String("depth", "standard", "briefing depth: agent_compact | compact | standard | deep")
	domain := fs.String("domain", "", "domain/repo scope (e.g. github.com/caddyserver/caddy); required when the graph hosts >1 domain")
	addr := fs.String("addr", defaultServiceAddr(), "Sensei gRPC server address")
	asJSON := fs.Bool("json", false, "output as JSON")
	repo := fs.String("repo", ".", "repository checkout for --task active")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: sensei briefing [--file <path>] [--task "description"] [flags]

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
		fmt.Fprintln(os.Stderr, "sensei briefing: provide --file and/or --task")
		return 2
	}
	if strings.TrimSpace(*task) == "active" {
		if strings.TrimSpace(*file) == "" {
			fmt.Fprintln(os.Stderr, "sensei briefing: --task active requires --file")
			return 2
		}
		brief, err := tasksession.BuildTaskBriefing(*repo, "", *file, true)
		if err != nil {
			fmt.Fprintf(os.Stderr, "sensei briefing: %v\n", err)
			return 1
		}
		format := "text"
		if *asJSON {
			format = "json"
		}
		if err := printTaskBriefing(brief, format); err != nil {
			fmt.Fprintf(os.Stderr, "sensei briefing: %v\n", err)
			return 2
		}
		return 0
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := client.DialConn(*addr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei briefing: connect %s: %v\n", *addr, err)
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
		fmt.Fprintf(os.Stderr, "sensei briefing: %v\n", err)
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
