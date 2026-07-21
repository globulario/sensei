// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/globulario/sensei/golang/architecture/convergence"
	"gopkg.in/yaml.v3"
)

type advanceConvergenceOptions struct {
	ClosureRequest    string
	Claims            string
	Dialogue          string
	EvidenceState     string
	GraphNT           string
	RepositoryRoot    string
	QuestionCreatedAt string
	OutputDir         string
	Session           string
	ExistingProbes    string
	Policy            string
	Format            string
	Check             bool
	RequireClosed     bool
	RequireTerminal   bool
}

func runAdvanceConvergence(args []string) int {
	fs := flag.NewFlagSet("sensei advance-convergence", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	opts := advanceConvergenceOptions{}
	fs.StringVar(&opts.ClosureRequest, "closure-request", "", "architecture_closure_request YAML document")
	fs.StringVar(&opts.Claims, "claims", "", "architecture_claims YAML document")
	fs.StringVar(&opts.Dialogue, "dialogue", "", "architecture_dialogue YAML document")
	fs.StringVar(&opts.EvidenceState, "evidence-state", "", "architecture_evidence_state YAML document")
	fs.StringVar(&opts.GraphNT, "graph-nt", "", "compiled graph N-Triples snapshot")
	fs.StringVar(&opts.RepositoryRoot, "repo", "", "repository checkout fixed to the closure request revision")
	fs.StringVar(&opts.QuestionCreatedAt, "question-created-at", "", "explicit RFC3339 timestamp for newly generated questions")
	fs.StringVar(&opts.OutputDir, "output-dir", "", "write deterministic convergence bundle here")
	fs.StringVar(&opts.Session, "session", "", "optional existing architecture_convergence_session YAML")
	fs.StringVar(&opts.ExistingProbes, "existing-probes", "", "optional existing architecture_evidence_probes YAML document")
	fs.StringVar(&opts.Policy, "policy", convergence.PolicyStrictV1, "convergence policy ID")
	fs.StringVar(&opts.Format, "format", "text", "status output format: text | yaml | json")
	fs.BoolVar(&opts.Check, "check", false, "compare expected bundle with --output-dir and write nothing")
	fs.BoolVar(&opts.RequireClosed, "require-closed", false, "exit 1 unless latest status is closed")
	fs.BoolVar(&opts.RequireTerminal, "require-terminal", false, "exit 1 unless latest status is terminal")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: sensei advance-convergence --closure-request <request.yaml> --claims <claims.yaml> --dialogue <dialogue.yaml> --evidence-state <state.yaml> --graph-nt <graph.nt> --repo <checkout> --question-created-at <RFC3339> --output-dir <dir> [flags]

Advance one deterministic offline convergence iteration. The command composes
existing Go package APIs only; it does not execute probes, run tests, query or
mutate the live graph, update Evidence state, or promote knowledge.

Flags:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if opts.ClosureRequest == "" || opts.Claims == "" || opts.Dialogue == "" || opts.EvidenceState == "" || opts.GraphNT == "" || opts.RepositoryRoot == "" || opts.QuestionCreatedAt == "" || opts.OutputDir == "" {
		fmt.Fprintln(os.Stderr, "sensei advance-convergence: --closure-request, --claims, --dialogue, --evidence-state, --graph-nt, --repo, --question-created-at, and --output-dir are required")
		return 2
	}
	var existing *convergence.Session
	if opts.Session != "" {
		s, err := convergence.LoadSession(opts.Session)
		if err != nil {
			fmt.Fprintf(os.Stderr, "sensei advance-convergence: load session: %v\n", err)
			return 1
		}
		existing = &s
	}
	res, err := convergence.Advance(convergence.Options{
		Paths: convergence.InputPaths{
			ClosureRequest: opts.ClosureRequest,
			Claims:         opts.Claims,
			Dialogue:       opts.Dialogue,
			EvidenceState:  opts.EvidenceState,
			GraphNT:        opts.GraphNT,
			RepositoryRoot: opts.RepositoryRoot,
			ExistingProbes: opts.ExistingProbes,
		},
		QuestionCreatedAt: opts.QuestionCreatedAt,
		PolicyID:          opts.Policy,
		Session:           existing,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei advance-convergence: %v\n", err)
		return 1
	}
	if opts.Check {
		if err := checkConvergenceBundle(opts.OutputDir, res.Bundle); err != nil {
			fmt.Fprintf(os.Stderr, "advance-convergence: STALE - %v\n", err)
			return 1
		}
	} else if res.Disposition != convergence.DispositionReplay && res.Disposition != convergence.DispositionBudgetExhausted {
		if err := convergence.WriteBundle(opts.OutputDir, res.Bundle); err != nil {
			fmt.Fprintf(os.Stderr, "sensei advance-convergence: write bundle: %v\n", err)
			return 1
		}
	}
	if res.Disposition == convergence.DispositionReplay {
		fmt.Println(convergence.DispositionReplay)
	}
	if res.Disposition == convergence.DispositionBudgetExhausted {
		fmt.Println(convergence.DispositionBudgetExhausted)
	}
	if err := printConvergenceStatus(res.Report, opts.Format); err != nil {
		fmt.Fprintf(os.Stderr, "sensei advance-convergence: %v\n", err)
		return 2
	}
	if opts.RequireClosed && res.Report.Status != convergence.StatusClosed {
		return 1
	}
	if opts.RequireTerminal && !convergenceTerminal(res.Report.Status) {
		return 1
	}
	return 0
}

type convergenceStatusOptions struct {
	Session      string
	Format       string
	VerifyBundle string
}

func runConvergenceStatus(args []string) int {
	fs := flag.NewFlagSet("sensei convergence-status", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	opts := convergenceStatusOptions{}
	fs.StringVar(&opts.Session, "session", "", "architecture_convergence_session YAML")
	fs.StringVar(&opts.Format, "format", "text", "output format: text | yaml | json")
	fs.StringVar(&opts.VerifyBundle, "verify-bundle", "", "optional bundle directory to verify")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if opts.Session == "" {
		fmt.Fprintln(os.Stderr, "sensei convergence-status: --session is required")
		return 2
	}
	session, err := convergence.LoadSession(opts.Session)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei convergence-status: %v\n", err)
		return 1
	}
	if opts.VerifyBundle != "" {
		if err := convergence.VerifyBundle(opts.VerifyBundle, session); err != nil {
			fmt.Fprintf(os.Stderr, "sensei convergence-status: bundle verification failed: %v\n", err)
			return 1
		}
	}
	report, err := convergence.Status(session)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei convergence-status: %v\n", err)
		return 1
	}
	if err := printConvergenceStatus(report, opts.Format); err != nil {
		fmt.Fprintf(os.Stderr, "sensei convergence-status: %v\n", err)
		return 2
	}
	return 0
}

func checkConvergenceBundle(dir string, bundle convergence.Bundle) error {
	for rel, want := range bundle.Files {
		got, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			return fmt.Errorf("missing %s", rel)
		}
		if !bytes.Equal(bytes.TrimSpace(got), bytes.TrimSpace(want)) {
			return fmt.Errorf("stale %s", rel)
		}
	}
	var extra []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if _, ok := bundle.Files[rel]; !ok {
			extra = append(extra, rel)
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	sort.Strings(extra)
	if len(extra) > 0 {
		return fmt.Errorf("extra files: %s", strings.Join(extra, ", "))
	}
	return nil
}

func printConvergenceStatus(report convergence.StatusReport, format string) error {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", "text":
		fmt.Printf("Session: %s\n", report.SessionID)
		fmt.Printf("Iteration: %d/%d\n", report.Iteration, report.MaxIterations)
		fmt.Printf("Status: %s\n", report.Status)
		fmt.Printf("Closure: %s\n", report.ClosureVerdict)
		fmt.Printf("Progress: %s\n", report.ProgressStatus)
		if len(report.WaitClasses) > 0 {
			fmt.Printf("Waiting on: %s\n", strings.Join(report.WaitClasses, ", "))
		}
		fmt.Printf("Critical blockers: %d\n", report.CriticalBlockers)
		fmt.Printf("Repeated blockers: %d\n", report.RepeatedBlockers)
		if len(report.NextActions) > 0 {
			fmt.Println("Next actions:")
			for _, a := range report.NextActions {
				fmt.Printf("  - %s %s\n", a.Class, a.Reference)
			}
		}
		return nil
	case "yaml", "yml":
		data, err := yaml.Marshal(map[string]convergence.StatusReport{"architecture_convergence_status": report})
		if err != nil {
			return err
		}
		fmt.Print(string(data))
		return nil
	case "json":
		data, err := json.MarshalIndent(map[string]convergence.StatusReport{"architecture_convergence_status": report}, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	default:
		return fmt.Errorf("--format must be text, yaml, or json")
	}
}

func convergenceTerminal(status string) bool {
	switch status {
	case convergence.StatusClosed, convergence.StatusConditionallyClosed, convergence.StatusStalled, convergence.StatusOscillating, convergence.StatusBudgetExhausted, convergence.StatusUncertifiable:
		return true
	default:
		return false
	}
}
