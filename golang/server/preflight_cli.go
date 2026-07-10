// SPDX-License-Identifier: Apache-2.0

// @awareness namespace=globular.awareness_graph
// @awareness component=server.preflight_cli
// @awareness file_role=offline_preflight_mode
// @awareness implements=globular.awareness_graph:intent.awg.graph_is_compiled_context_not_authority
// @awareness risk=low
package main

// preflight_cli.go — the -preflight offline mode. Runs the full Preflight
// pipeline against the EMBEDDED seed (no Oxigraph, no cluster, no deploy) and
// prints the result. This closes the dogfooding gap where a freshly built
// graph was unusable until a pipeline deploy: operators and agents can ask
//
//	awareness-graph -preflight -task "fix X" -file golang/repository/...
//
// against the exact knowledge this binary carries.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	awarenesspb "github.com/globulario/awareness-graph/golang/pb"
)

// stringListFlag accumulates repeated -file flags.
type stringListFlag []string

func (s *stringListFlag) String() string { return strings.Join(*s, ",") }
func (s *stringListFlag) Set(v string) error {
	*s = append(*s, v)
	return nil
}

// runPreflightCLI executes one offline Preflight and writes a human-readable
// report (or JSON) to w. Returns the process exit code.
func runPreflightCLI(w io.Writer, task string, files []string, mode string, asJSON bool) int {
	pfMode := awarenesspb.PreflightMode_PREFLIGHT_STANDARD
	if strings.EqualFold(mode, "compact") {
		pfMode = awarenesspb.PreflightMode_PREFLIGHT_COMPACT
	}

	s := newServer(newEmbeddedSeedStore())
	resp, err := s.Preflight(context.Background(), &awarenesspb.PreflightRequest{
		Task:  task,
		Files: files,
		Mode:  pfMode,
	})
	if err != nil {
		fmt.Fprintf(w, "preflight: %v\n", err)
		return 1
	}

	if asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		if err := enc.Encode(resp); err != nil {
			fmt.Fprintf(w, "preflight: encode: %v\n", err)
			return 1
		}
		return 0
	}

	fmt.Fprintf(w, "Preflight (offline, embedded seed)\n")
	fmt.Fprintf(w, "task:   %s\n", task)
	fmt.Fprintf(w, "files:  %s\n", strings.Join(files, ", "))
	fmt.Fprintf(w, "status: %s   risk: %s   confidence: %s\n",
		resp.GetStatus(), resp.GetRiskClass(), resp.GetConfidence())
	if c := resp.GetCoverage(); c != nil {
		fmt.Fprintf(w, "coverage: sufficient=%v anchors=%d (%s)\n",
			c.GetSufficient(), c.GetDirectAnchorCount(), c.GetNote())
	}
	printList := func(title string, items []string) {
		if len(items) == 0 {
			return
		}
		fmt.Fprintf(w, "\n%s:\n", title)
		for _, it := range items {
			fmt.Fprintf(w, "  - %s\n", it)
		}
	}
	printList("required_actions", resp.GetRequiredActions())
	printList("forbidden_fixes", resp.GetForbiddenFixes())
	printList("tests_to_run", resp.GetTestsToRun())
	printList("files_to_read", resp.GetFilesToRead())
	printList("blind_spots", resp.GetBlindSpots())
	if ps := resp.GetImplementationPatterns(); len(ps) > 0 {
		fmt.Fprintf(w, "\nimplementation_patterns:\n")
		for _, p := range ps {
			fmt.Fprintf(w, "  - %s [%s] %s\n", p.GetId(), p.GetMatchStrength(), p.GetLabel())
		}
	}
	return 0
}
