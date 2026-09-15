// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/globulario/sensei/golang/client"
	awarenesspb "github.com/globulario/sensei/golang/pb"
)

func runPreflight(args []string) int {
	fs := flag.NewFlagSet("sensei preflight", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	task := fs.String("task", "", "task description")
	addr := fs.String("addr", "", "Sensei gRPC server address")
	asJSON := fs.Bool("json", false, "output as JSON")
	mode := fs.String("mode", "standard", "preflight mode: standard | compact")
	domain := fs.String("domain", "", "domain/repo scope passed through to per-file impact queries")
	repo := fs.String("repo", ".", "repository checkout, used to resolve the domain when --domain is omitted")
	var files stringSlice
	fs.Var(&files, "file", "repo-relative file (repeatable)")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: sensei preflight [--file <path>]... [--task "description"] [flags]

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
		fmt.Fprintln(os.Stderr, "sensei preflight: provide --file and/or --task")
		return 2
	}

	resolvedDomain := resolveRepositoryDomain(*repo, *domain)
	if resolvedDomain.Err != nil {
		fmt.Fprintf(os.Stderr, "sensei preflight: %v\n", resolvedDomain.Err)
		return 1
	}
	// LAW 3 -- ENDPOINT OWNERSHIP, resolved HERE rather than right after Parse because the
	// domain is not settled until above. The owner's answer is per-domain, so a reader resolved
	// from the raw flag would carry the endpoint and declared generation of a different domain
	// -- usually none -- and would look resolved while answering for the wrong one.
	reader := productionReaderFor(fs, *repo, resolvedDomain.Domain, *addr)
	*addr = reader.Addr

	// ENDPOINT AGREEMENT IS EVALUATED AGAINST THE ENDPOINT THE OPERATION WILL ACTUALLY USE.
	//
	// The server whose authority this asserts is decided here, and the project config states one
	// too. Refuse a silent disagreement rather than report a verdict from a server the operator
	// did not name (issue #212).
	//
	// This check used to run BEFORE the owner, on the raw --addr value. That was sound while the
	// raw flag WAS the endpoint dialled, which is how issue #212 first landed it. The
	// endpoint-ownership family then made the endpoint come from productionReaderFor and set the
	// flag's default to empty -- and left the guard reading the flag. It therefore compared the
	// configured address against "" and refused every canonical invocation, in a repository whose
	// configuration the owner was about to honour exactly. A guard that runs before the value it
	// guards exists reports its own position, not a disagreement.
	//
	// The root is graphConfigRoot, the SAME root productionReaderFor resolved the configuration
	// from. resolveProjectRoot does not walk up, so from a subdirectory the guard would have read a
	// different config file than the owner did -- finding none, concluding nothing is configured,
	// and passing. One endpoint decision, one configuration.
	if err := requireServerAddrAgreement(fs, graphConfigRoot(*repo), reader.Addr); err != nil {
		fmt.Fprintf(os.Stderr, "sensei preflight: %v\n", err)
		return 1
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := client.DialConn(*addr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei preflight: connect %s: %v\n", *addr, err)
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
		Domain: resolvedDomain.Domain,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei preflight: %v\n", err)
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
