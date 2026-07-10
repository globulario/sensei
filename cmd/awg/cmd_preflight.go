// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/globulario/awareness-graph/golang/client"
	awarenesspb "github.com/globulario/awareness-graph/golang/pb"
)

func runPreflight(args []string) int {
	fs := flag.NewFlagSet("awg preflight", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	task := fs.String("task", "", "task description")
	addr := fs.String("addr", "localhost:10120", "AWG gRPC server address")
	asJSON := fs.Bool("json", false, "output as JSON")
	mode := fs.String("mode", "standard", "preflight mode: standard | compact")
	domain := fs.String("domain", "", "domain/repo scope passed through to per-file impact queries")
	var files stringSlice
	fs.Var(&files, "file", "repo-relative file (repeatable)")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: awg preflight [--file <path>]... [--task "description"] [flags]

Risk classification for a planned edit. Returns risk class, required
actions, forbidden fixes, and tests to run.

At least one of --file or --task is required.

Flags:
`)
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return 2
	}
	if len(files) == 0 && *task == "" {
		fmt.Fprintln(os.Stderr, "awg preflight: provide --file and/or --task")
		return 2
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := client.DialConn(*addr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "awg preflight: connect %s: %v\n", *addr, err)
		return 1
	}
	defer conn.Close()

	pfMode := awarenesspb.PreflightMode_PREFLIGHT_STANDARD
	if strings.EqualFold(*mode, "compact") {
		pfMode = awarenesspb.PreflightMode_PREFLIGHT_COMPACT
	}

	client := awarenesspb.NewAwarenessGraphClient(conn)
	resp, err := client.Preflight(ctx, &awarenesspb.PreflightRequest{
		Task:   *task,
		Files:  files,
		Mode:   pfMode,
		Domain: *domain,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "awg preflight: %v\n", err)
		return 1
	}

	if *asJSON {
		return emitProtoJSON(resp)
	}

	// Human-readable output.
	fmt.Printf("Status: %s   Risk: %s   Confidence: %s\n",
		resp.GetStatus(), resp.GetRiskClass(), resp.GetConfidence())
	printGraphAuthority(resp.GetAuthority())

	if c := resp.GetCoverage(); c != nil {
		fmt.Printf("Coverage: sufficient=%v anchors=%d (%s)\n",
			c.GetSufficient(), c.GetDirectAnchorCount(), c.GetNote())
	}

	printList := func(title string, items []string) {
		if len(items) == 0 {
			return
		}
		fmt.Printf("\n%s:\n", title)
		for _, it := range items {
			fmt.Printf("  - %s\n", it)
		}
	}
	printList("Required actions", resp.GetRequiredActions())
	printList("Forbidden fixes", resp.GetForbiddenFixes())
	printList("Tests to run", resp.GetTestsToRun())
	printList("Files to read", resp.GetFilesToRead())
	printList("Blind spots", resp.GetBlindSpots())

	return 0
}
