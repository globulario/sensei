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

	// ENDPOINT AGREEMENT IS A VISIBILITY DUTY, NOT A VETO.
	//
	// The operation follows the owner, and reports it when the owner's answer is not the one this
	// repository's configuration names.
	//
	// This was a refusal (issue #212: do not report a verdict from a server the operator did not
	// name). Two measurements retired it. First, the refusal was comparing the configured address
	// against the RAW --addr flag, from before productionReaderFor existed, so with --addr omitted
	// it compared against "" and refused every canonical invocation. Re-pointing it at the owner's
	// answer then exposed the real problem: the only case left in which it could refuse was a
	// registry-declared endpoint the project config does not name -- and the registry outranks the
	// project config precisely so that a repository cannot redirect its own graph. The guard had
	// become able to fire only where it must not.
	//
	// Second, #212's failure is now unreachable rather than merely unobserved: the owner reads this
	// project's configuration itself as precedence 3, and no subject carries an endpoint default of
	// its own. Both limbs are witnessed in issue_212_reachability_test.go, which is what makes this
	// a measured consequence instead of a check dropped because it was inconvenient.
	//
	// The configuration is read from graphConfigRoot(*repo), the root the owner resolved from, so
	// the notice compares the same two things the owner did.
	if cfg, err := loadEndpointConfig(graphConfigRoot(*repo)); err == nil {
		if notice := endpointDisagreementNotice(reader.Addr, reader.Authority, cfg.configuredServerAddr(), reader.Overridden); notice != "" {
			fmt.Fprintln(os.Stderr, notice)
		}
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
