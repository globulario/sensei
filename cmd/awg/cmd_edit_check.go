// SPDX-License-Identifier: Apache-2.0

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

// runEditCheck evaluates a proposed edit against the active repo-scoped advisory
// rules for a file. Warning-only — it never blocks and never edits code.
func runEditCheck(args []string) int {
	fs := flag.NewFlagSet("sensei edit-check", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	file := fs.String("file", "", "repo-relative file path being edited")
	content := fs.String("content", "", "proposed new content (inline)")
	contentFile := fs.String("content-file", "", "read proposed content from this path ('-' for stdin)")
	domain := fs.String("domain", "", "domain/repo scope (e.g. github.com/caddyserver/caddy); required when the graph hosts >1 domain")
	addr := fs.String("addr", defaultServiceAddr(), "AWG gRPC server address")
	asJSON := fs.Bool("json", false, "output as JSON")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: sensei edit-check --file <path> [--content <text> | --content-file <path>|-] [flags]

Evaluates the proposed edit content against the repo-scoped advisory rules
that apply to the file, in the resolved domain. WARNING-ONLY: it reports rules
a bad-shape edit would violate; it never blocks and never modifies code.

Flags:
`)
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *file == "" {
		fmt.Fprintln(os.Stderr, "sensei edit-check: --file is required")
		return 2
	}

	proposed := *content
	if *contentFile != "" {
		data, err := readContentArg(*contentFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "sensei edit-check: %v\n", err)
			return 1
		}
		proposed = data
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := client.DialConn(*addr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei edit-check: connect %s: %v\n", *addr, err)
		return 1
	}
	defer conn.Close()

	client := awarenesspb.NewAwarenessGraphClient(conn)
	resp, err := client.EditCheck(ctx, &awarenesspb.EditCheckRequest{
		File:            *file,
		ProposedContent: proposed,
		Domain:          *domain,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei edit-check: %v\n", err)
		return 1
	}

	if *asJSON {
		return emitProtoJSON(resp)
	}

	warnings := resp.GetWarnings()
	fmt.Printf("rules_evaluated: %d\n", resp.GetRulesEvaluated())
	fmt.Printf("warnings: %d\n", len(warnings))
	for _, w := range warnings {
		fmt.Printf("\n[%s] %s (%s)\n  %s\n  %s\n", w.GetSeverity(), w.GetRuleId(), w.GetClass(), w.GetMessage(), w.GetDetail())
		if p := w.GetProvenance(); p != "" {
			fmt.Printf("  provenance: %s\n", p)
		}
	}
	if len(warnings) == 0 {
		fmt.Println("\nno advisory rule tripped for this edit.")
	}
	return 0
}

// readContentArg reads proposed content from a file path, or stdin when "-".
func readContentArg(path string) (string, error) {
	if path == "-" {
		data, err := os.ReadFile("/dev/stdin")
		if err != nil {
			return "", fmt.Errorf("read stdin: %w", err)
		}
		return string(data), nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	return string(data), nil
}
