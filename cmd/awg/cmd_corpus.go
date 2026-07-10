// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/globulario/awareness-graph/golang/extractor/coldsource"
	"github.com/globulario/awareness-graph/golang/extractor/corpus"
)

// runCorpus dispatches the human-gated corpus-integration subcommands
// (docs/corpus-integration-design.md §9). None of them promote, mutate a graph,
// touch the seed, or use an LLM. `materialize` writes ONLY status:candidate YAML
// under a candidates/ tree; promotion is a separate human/PR step.
func runCorpus(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: awg corpus <plan|materialize|validate> [flags]")
		return 2
	}
	switch args[0] {
	case "plan":
		return runCorpusPlan(args[1:])
	case "materialize":
		return runCorpusMaterialize(args[1:])
	case "validate":
		return runCorpusValidate(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "awg corpus: unknown subcommand %q (plan|materialize|validate)\n", args[0])
		return 2
	}
}

// runCorpusPlan: read-only. Classify a findings report into integrate/hold/never.
func runCorpusPlan(args []string) int {
	fs := flag.NewFlagSet("awg corpus plan", flag.ContinueOnError)
	from := fs.String("from", "", "findings report YAML (findings: [...])")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *from == "" {
		fmt.Fprintln(os.Stderr, "error: --from <report.yaml> is required")
		return 2
	}
	r, err := corpus.LoadReport(*from)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: load report: %v\n", err)
		return 1
	}
	verdicts := corpus.Plan(r)
	integrate, hold, never := 0, 0, 0
	fmt.Printf("\nAWG corpus plan — %d finding(s)\n", len(verdicts))
	fmt.Printf("%-10s %-18s %-12s %s\n", "action", "entry_type", "max_status", "id")
	fmt.Printf("%s\n", strings.Repeat("-", 92))
	for _, v := range verdicts {
		switch v.Action {
		case corpus.ActionIntegrate:
			integrate++
		case corpus.ActionHold:
			hold++
		case corpus.ActionNever:
			never++
		}
		fmt.Printf("%-10s %-18s %-12s %s\n", v.Action, dash(v.EntryType), dash(v.MaxStatus), v.Finding.ID)
		fmt.Printf("           ↳ %s\n", v.Reason)
	}
	fmt.Printf("\nintegrate: %d   hold: %d   never: %d\n", integrate, hold, never)
	fmt.Printf("Reports are automatic; corpus truth is not. `materialize` writes status:candidate only, for human-selected ids.\n")
	return 0
}

// runCorpusMaterialize: write candidate entries for SELECTED, integrate-eligible
// findings. Always status:candidate, always under a candidates/ tree.
func runCorpusMaterialize(args []string) int {
	fs := flag.NewFlagSet("awg corpus materialize", flag.ContinueOnError)
	from := fs.String("from", "", "findings report YAML")
	selected := fs.String("selected", "", "comma list of finding ids to materialize (required)")
	out := fs.String("out", "", "output directory under a candidates/ tree (required)")
	status := fs.String("status", corpus.StatusCandidate, "only 'candidate' is allowed")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *from == "" || *selected == "" || *out == "" {
		fmt.Fprintln(os.Stderr, "error: --from, --selected, and --out are required")
		return 2
	}
	if *status != corpus.StatusCandidate {
		fmt.Fprintf(os.Stderr, "error: materialize only writes status:candidate (promotion is a separate human step), got %q\n", *status)
		return 2
	}
	r, err := corpus.LoadReport(*from)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: load report: %v\n", err)
		return 1
	}
	want := map[string]bool{}
	for _, id := range strings.Split(*selected, ",") {
		if id = strings.TrimSpace(id); id != "" {
			want[id] = true
		}
	}
	byID := map[string]corpus.Verdict{}
	for _, v := range corpus.Plan(r) {
		byID[v.Finding.ID] = v
	}
	written, skipped := 0, 0
	for id := range want {
		v, ok := byID[id]
		if !ok {
			fmt.Fprintf(os.Stderr, "skip %s: not in report\n", id)
			skipped++
			continue
		}
		e, merr := corpus.Materialize(v)
		if merr != nil {
			fmt.Fprintf(os.Stderr, "skip %s: %v\n", id, merr)
			skipped++
			continue
		}
		path, werr := corpus.WriteEntry(*out, e)
		if werr != nil {
			fmt.Fprintf(os.Stderr, "error: write %s: %v\n", id, werr)
			return 1
		}
		fmt.Printf("wrote %s (type=%s status=candidate)\n", path, e.Type)
		written++
	}
	fmt.Printf("\nmaterialized %d candidate entrie(s), skipped %d. Nothing promoted; nothing in any graph.\n", written, skipped)
	fmt.Printf("Next (human): review, run `awg build` to extract minimal owned triples, open a PR.\n")
	return 0
}

// runCorpusValidate: check candidate entry files against the §4/§5 rules.
func runCorpusValidate(args []string) int {
	fs := flag.NewFlagSet("awg corpus validate", flag.ContinueOnError)
	entry := fs.String("entry", "", "a candidate entry YAML (or use --dir)")
	dir := fs.String("dir", "", "a directory of candidate entry YAMLs")
	repo := fs.String("repo", ".", "repo for citation resolution (active entries)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	var files []string
	switch {
	case *entry != "":
		files = []string{*entry}
	case *dir != "":
		matches, _ := filepath.Glob(filepath.Join(*dir, "*.yaml"))
		files = matches
	default:
		fmt.Fprintln(os.Stderr, "error: --entry <file> or --dir <dir> required")
		return 2
	}
	git := coldsource.NewGitVerifier(*repo)
	bad := 0
	for _, f := range files {
		e, err := corpus.LoadEntry(f)
		if err != nil {
			fmt.Printf("FAIL %s: %v\n", f, err)
			bad++
			continue
		}
		if violations := corpus.ValidateEntry(e, *repo, git); len(violations) > 0 {
			fmt.Printf("FAIL %s:\n", f)
			for _, vv := range violations {
				fmt.Printf("  - %s\n", vv)
			}
			bad++
			continue
		}
		fmt.Printf("OK   %s (type=%s status=%s tier=%s)\n", f, e.Type, e.Status, e.Grounding.Tier)
	}
	if bad > 0 {
		fmt.Printf("\n%d entrie(s) failed validation\n", bad)
		return 1
	}
	fmt.Printf("\nall %d entrie(s) valid\n", len(files))
	return 0
}

func dash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}
