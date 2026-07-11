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
	"text/tabwriter"
	"time"

	awarenesspb "github.com/globulario/sensei/golang/pb"
)

func runPatternCheck(args []string) int {
	fs := flag.NewFlagSet("sensei pattern-check", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	addr := fs.String("addr", "localhost:10120", "AWG gRPC server address")
	format := fs.String("format", "table", "output format: table | json")
	failOnViolation := fs.Bool("fail-on-violation", true, "exit non-zero on violation")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: sensei pattern-check <file>... [flags]

Text-scans each file against ImplementationPattern recipes returned by
the awareness-graph briefing. Reports missing required calls and present
forbidden calls.

Exit code: 0 when all files satisfy patterns, 1 on violation.

Flags:
`)
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "sensei pattern-check: requires at least one file argument")
		return 2
	}

	c, err := connectAWG(*addr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei pattern-check: connect: %v\n", err)
		return 1
	}
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var results []pcFileResult
	totalViolations := 0
	for _, file := range fs.Args() {
		fr := pcCheckOneFile(ctx, c.Stub(), file)
		totalViolations += fr.violationCount()
		results = append(results, fr)
	}

	switch *format {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(map[string]interface{}{"results": results})
	default:
		pcPrintTable(results)
	}

	if *failOnViolation && totalViolations > 0 {
		return 1
	}
	return 0
}

// ── types ────────────────────────────────────────────────────────────────

type pcFileResult struct {
	File     string            `json:"file"`
	Error    string            `json:"error,omitempty"`
	Patterns []pcPatternResult `json:"patterns,omitempty"`
}

type pcPatternResult struct {
	PatternID       string   `json:"pattern_id"`
	PatternLabel    string   `json:"pattern_label,omitempty"`
	MatchStrength   string   `json:"match_strength,omitempty"`
	MissingRequired []string `json:"missing_required,omitempty"`
	ForbiddenFound  []string `json:"forbidden_found,omitempty"`
	ReferenceFiles  []string `json:"reference_files,omitempty"`
	Status          string   `json:"status"`
}

func (r pcFileResult) violationCount() int {
	n := 0
	for _, p := range r.Patterns {
		if p.Status == "violation" {
			n++
		}
	}
	return n
}

// ── core ─────────────────────────────────────────────────────────────────

func pcCheckOneFile(ctx context.Context, stub awarenesspb.AwarenessGraphClient, file string) pcFileResult {
	out := pcFileResult{File: file}

	content, err := os.ReadFile(file)
	if err != nil {
		out.Error = "read: " + err.Error()
		return out
	}

	task := pcDeriveTask(file)
	resp, err := stub.Briefing(ctx, &awarenesspb.BriefingRequest{
		File: file, Task: task, Depth: "compact",
	})
	if err != nil {
		out.Error = "briefing: " + err.Error()
		return out
	}

	patterns := resp.GetImplementationPatterns()
	if len(patterns) == 0 {
		return out
	}

	contentStr := string(content)
	for _, p := range patterns {
		one := pcPatternResult{
			PatternID:      pcTrimIDPrefix(p.GetId()),
			PatternLabel:   p.GetLabel(),
			MatchStrength:  p.GetMatchStrength(),
			ReferenceFiles: p.GetReferenceFiles(),
			Status:         "pass",
		}
		for _, req := range p.GetRequiredCalls() {
			if req != "" && !pcCallPresent(contentStr, req) {
				one.MissingRequired = append(one.MissingRequired, req)
			}
		}
		for _, forb := range p.GetForbiddenCalls() {
			if forb != "" && pcCallPresent(contentStr, forb) {
				one.ForbiddenFound = append(one.ForbiddenFound, forb)
			}
		}
		if len(one.MissingRequired) > 0 || len(one.ForbiddenFound) > 0 {
			one.Status = "violation"
		}
		out.Patterns = append(out.Patterns, one)
	}
	return out
}

func pcCallPresent(content, call string) bool {
	for _, v := range pcCallVariants(call) {
		if strings.Contains(content, v) {
			return true
		}
	}
	return false
}

func pcCallVariants(call string) []string {
	variants := []string{call}
	switch {
	case strings.HasPrefix(call, "globular."):
		variants = append(variants, "globular_client."+call[len("globular."):])
	case strings.HasPrefix(call, "globular_client."):
		variants = append(variants, "globular."+call[len("globular_client."):])
	}
	return variants
}

func pcDeriveTask(file string) string {
	base := filepath.Base(file)
	if dot := strings.LastIndexByte(base, '.'); dot > 0 {
		base = base[:dot]
	}
	return "service " + strings.ReplaceAll(base, "_", " ")
}

func pcTrimIDPrefix(id string) string {
	const p = "implementation_pattern:"
	if strings.HasPrefix(id, p) {
		return id[len(p):]
	}
	return id
}

// ── output ───────────────────────────────────────────────────────────────

func pcPrintTable(results []pcFileResult) {
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	defer tw.Flush()
	fmt.Fprintln(tw, "FILE\tPATTERN\tSTATUS\tDETAIL")

	for _, fr := range results {
		if fr.Error != "" {
			fmt.Fprintf(tw, "%s\t-\tERROR\t%s\n", fr.File, fr.Error)
			continue
		}
		if len(fr.Patterns) == 0 {
			fmt.Fprintf(tw, "%s\t-\tno_pattern\t(no pattern matched)\n", fr.File)
			continue
		}
		for _, p := range fr.Patterns {
			detail := "ok"
			if p.Status == "violation" {
				var parts []string
				if len(p.MissingRequired) > 0 {
					parts = append(parts, "missing: "+strings.Join(p.MissingRequired, ","))
				}
				if len(p.ForbiddenFound) > 0 {
					parts = append(parts, "forbidden: "+strings.Join(p.ForbiddenFound, ","))
				}
				detail = strings.Join(parts, "; ")
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", fr.File, p.PatternID, strings.ToUpper(p.Status), detail)
		}
	}

	for _, fr := range results {
		for _, p := range fr.Patterns {
			if p.Status == "violation" && len(p.ReferenceFiles) > 0 {
				fmt.Println()
				fmt.Printf("%s — pattern %s recommends consulting:\n", fr.File, p.PatternID)
				for _, ref := range p.ReferenceFiles {
					fmt.Printf("  - %s\n", ref)
				}
			}
		}
	}
}
