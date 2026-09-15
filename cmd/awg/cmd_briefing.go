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
	// EMPTY default, deliberately (law 3). A command that carries a default port owns a
	// graph choice; an empty value means "not named", so resolveGraphReader can tell an
	// operator override from ordinary resolution.
	addr := fs.String("addr", "", "Sensei gRPC server address (default: resolved for the domain)")
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

	resolvedDomain := resolveRepositoryDomain(*repo, *domain)
	if resolvedDomain.Err != nil {
		fmt.Fprintf(os.Stderr, "sensei briefing: %v\n", resolvedDomain.Err)
		return 1
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// LAW 3: the endpoint and the graph identity come from the one owner, for the domain
	// already resolved above. This command used to dial the netcfg default while holding
	// that domain in hand, so in a repository whose config states :10122 it failed
	// against :10120 with the right graph running the whole time.
	//
	// There is no fallback. If the resolved endpoint does not answer, that is the answer:
	// trying a second port would select a graph by liveness.
	// THE PROJECT ROOT, RESOLVED -- not --repo, which means something else.
	//
	// --repo is documented as "repository checkout for --task active" and defaults to ".".
	// Passing it as resolveGraphReader's projectRoot made endpoint resolution read
	// ./.sensei/config.yaml, so the SAME command run from a subdirectory resolved a different
	// endpoint than from the root: the config was simply not found and resolution fell through
	// to the registry or the built-in default. Every other reader resolves the root by walking
	// up, via productionReaderFor.
	//
	// Two different questions wearing one flag: which checkout a task briefing describes, and
	// which project's configuration names the graph endpoint.
	briefingRoot, _ := resolveProjectRoot("")
	reader := resolveGraphReader(fs, briefingRoot, resolvedDomain.Domain, *addr, DefaultDomainRegistryPath())
	if notice := nonCanonicalReaderNotice(reader); notice != "" {
		fmt.Fprintln(os.Stderr, notice)
	}
	conn, err := client.DialConn(reader.Addr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei briefing: connect %s (%s): %v\n", reader.Addr, reader.Source, err)
		return 1
	}
	defer conn.Close()

	client := awarenesspb.NewAwarenessGraphClient(conn)
	resp, err := client.Briefing(ctx, &awarenesspb.BriefingRequest{
		File:   *file,
		Task:   *task,
		Depth:  *depth,
		Domain: resolvedDomain.Domain,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei briefing: %v\n", err)
		return 1
	}

	// The generation that answered must be the one this domain declares ACTIVE. Checked
	// before the briefing is rendered, so findings from an undeclared graph are never
	// read as findings about this domain.
	if err := reader.verifyServed(resp.GetAuthority().GetLiveStoreGraphDigestSha256()); err != nil {
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
