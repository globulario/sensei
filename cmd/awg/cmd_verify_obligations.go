// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/globulario/sensei/golang/architecture/testobligation"
	"github.com/globulario/sensei/golang/client"
	awarenesspb "github.com/globulario/sensei/golang/pb"
)

// runVerifyObligations answers "did the required tests actually prove
// anything", which `go test` cannot: it prints ok for a package whose tests all
// skipped, so an agent reading the exit code reports proof it never got.
//
// The graph supplies the obligations (preflight's tests-to-run) and a
// `go test -json` stream supplies the observations. Sensei does not run the
// tests: executing arbitrary commands in a target repository is authority this
// tool does not need, and the structured stream already carries pass/fail/skip
// without parsing console wording.
//
// @awareness namespace=globular.awareness_graph
// @awareness component=command.verify_obligations
// @awareness enforces=globular.awareness_graph:invariant.awareness.missing_evidence_produces_unknown
func runVerifyObligations(args []string) int {
	fs := flag.NewFlagSet("sensei verify-obligations", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	task := fs.String("task", "", "task description, passed through to preflight")
	results := fs.String("results", "", "file containing `go test -json` output (- for stdin)")
	addr := fs.String("addr", defaultServiceAddr(), "Sensei gRPC server address")
	asJSON := fs.Bool("json", false, "emit the obligation report as JSON")
	domain := fs.String("domain", "", "domain/repo scope passed through to preflight")
	repo := fs.String("repo", ".", "repository checkout, used to resolve the domain when --domain is omitted")
	module := fs.String("module", "", "Go module path to strip from package names (default: read go.mod in --repo)")
	complete := fs.Bool("assert-discovery-complete", false, "caller assertion that the run was exhaustive (no -run filter) over the packages it reported.\n\t\tRecorded as caller-attested: it is reported, but it does NOT authorize a MISSING_IMPLEMENTATION\n\t\tfinding, because nothing here can verify it")
	var files stringSlice
	fs.Var(&files, "file", "repo-relative file (repeatable)")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: sensei verify-obligations --results <file> [--file <path>]... [flags]

Decides whether the tests the graph requires for a change actually ran.

`+"`go test`"+` exits 0 for a package whose tests were all skipped, so a passing
exit code is not proof. This command takes the required tests from preflight,
matches them against a `+"`go test -json`"+` stream, and refuses to certify when a
required test was skipped, was never selected, or belongs to a language this
run cannot speak for.

  go test ./... -json > results.json
  sensei verify-obligations --file golang/server/impact.go --results results.json

Exit codes:
  0  PASS           every required test executed and passed
  1  FAIL           a required test failed
  3  INDETERMINATE  a required test was skipped, unavailable, or missing — not proved
  2  usage or connection error

Absent results are UNAVAILABLE. A run that was not declared complete cannot
prove a test does not exist, and a completeness claim from the caller cannot
be verified here either — a -run-filtered run is indistinguishable from an
exhaustive one in the output stream. Only a trusted discovery runner that
produces its own coverage declaration may turn an absent result into a
MISSING_IMPLEMENTATION finding; no such producer exists yet.

Flags:
`)
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *results == "" {
		fmt.Fprintln(os.Stderr, "sensei verify-obligations: --results is required")
		return 2
	}
	if len(files) == 0 && *task == "" {
		fmt.Fprintln(os.Stderr, "sensei verify-obligations: provide --file and/or --task")
		return 2
	}

	resolvedDomain := resolveRepositoryDomain(*repo, *domain)
	if resolvedDomain.Err != nil {
		fmt.Fprintf(os.Stderr, "sensei verify-obligations: %v\n", resolvedDomain.Err)
		return 2
	}

	modulePath := strings.TrimSpace(*module)
	if modulePath == "" {
		modulePath = readModulePath(*repo)
	}

	anchors, rc := preflightRequiredTests(*addr, *task, files, resolvedDomain.Domain)
	if rc != 0 {
		return rc
	}

	observed, rc := parseResultsFile(*results, modulePath)
	if rc != 0 {
		return rc
	}

	// Deliberately CoverageFromRun, never RunnerProvenCoverage: a flag is a
	// caller assertion, and letting it authorize an accusation would rebuild
	// the "not observed" -> "does not exist" conflation this command exists to
	// prevent.
	coverage := testobligation.CoverageFromRun(observed, *complete)
	report := testobligation.Certify(testobligation.ResolveGoObligations(anchors, observed, coverage))
	if *asJSON {
		return emitObligationJSON(report, coverage)
	}
	printObligationReport(report, coverage)
	return report.Verdict.ExitCode()
}

// preflightRequiredTests asks the graph which tests this change must pass.
func preflightRequiredTests(addr, task string, files []string, domain string) ([]string, int) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := client.DialConn(addr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei verify-obligations: connect %s: %v\n", addr, err)
		return nil, 2
	}
	defer conn.Close()

	resp, err := awarenesspb.NewAwarenessGraphClient(conn).Preflight(ctx, &awarenesspb.PreflightRequest{
		Task:   task,
		Files:  files,
		Mode:   awarenesspb.PreflightMode_PREFLIGHT_STANDARD,
		Domain: domain,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei verify-obligations: %v\n", err)
		return nil, 2
	}
	return resp.GetTestsToRun(), 0
}

func parseResultsFile(path, modulePath string) (map[string]testobligation.GoTestResult, int) {
	in := os.Stdin
	if path != "-" {
		f, err := os.Open(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "sensei verify-obligations: %v\n", err)
			return nil, 2
		}
		defer f.Close()
		in = f
	}
	observed, err := testobligation.ParseGoTestJSON(in, modulePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei verify-obligations: %v\n", err)
		return nil, 2
	}
	return observed, 0
}

// readModulePath reads the module path from go.mod so package names in the
// results stream can be reduced to repo-relative directories. An unreadable
// go.mod is not fatal: matching simply falls back to full package paths, which
// will surface as unavailable obligations rather than as false passes.
func readModulePath(repo string) string {
	raw, err := os.ReadFile(repo + "/go.mod")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "module "); ok {
			return strings.TrimSpace(rest)
		}
	}
	return ""
}

func printObligationReport(r testobligation.Report, c testobligation.DiscoveryCoverage) {
	fmt.Printf("Obligations: %d\n", len(r.Obligations))
	fmt.Printf("Discovery completeness: %s\n\n", coverageNote(c))
	for _, o := range r.Obligations {
		tag := o.Outcome.String()
		if !o.Required {
			tag += " (optional)"
		}
		fmt.Printf("  %-22s %s\n", tag, o.Anchor)
		if o.Reason != "" {
			fmt.Printf("  %-22s reason: %s\n", "", o.Reason)
		}
		if o.CandidateHint != "" {
			// A lead for a human, not a substitute proof: the obligation names
			// an exact anchor and this candidate does not satisfy it.
			fmt.Printf("  %-22s candidate (not authoritative): %s\n", "", o.CandidateHint)
		}
	}

	fmt.Printf("\nVerdict: %s\n", r.Verdict)
	if r.Verdict.Certifies() {
		return
	}
	if blocking := r.Blocking(); len(blocking) > 0 {
		fmt.Println("\nNot certified — these required obligations did not prove anything:")
		for _, o := range blocking {
			fmt.Printf("  - [%s] %s\n", o.Outcome, o.Anchor)
		}
	} else {
		fmt.Println("\nNot certified — no required obligation was observed for this change.")
	}
}

// coverageNote states what is known about discovery completeness, so a reader
// can see whether an UNAVAILABLE means "nobody looked" or "someone says they
// looked and we cannot check".
func coverageNote(c testobligation.DiscoveryCoverage) string {
	if !c.Complete {
		return "not declared (absent results prove nothing)"
	}
	if c.Provenance == testobligation.CoverageRunnerProven {
		return "proven by " + c.Producer
	}
	return c.Provenance.String() + " by " + c.Producer + " — unverifiable here, so absent results stay UNAVAILABLE"
}

// obligationJSON is an explicit wire shape rather than a struct-tagged reuse of
// the domain type: the machine output is a contract for CI, and it should not
// change silently because an internal field was renamed.
type obligationJSON struct {
	Anchor        string `json:"anchor"`
	Required      bool   `json:"required"`
	Outcome       string `json:"outcome"`
	Reason        string `json:"reason,omitempty"`
	CandidateHint string `json:"candidate_hint,omitempty"`
}

type reportJSON struct {
	DiscoveryCompleteness string           `json:"discovery_completeness"`
	Verdict               string           `json:"verdict"`
	Certifies             bool             `json:"certifies"`
	ExitCode              int              `json:"exit_code"`
	Obligations           []obligationJSON `json:"obligations"`
	Blocking              []obligationJSON `json:"blocking,omitempty"`
}

func emitObligationJSON(r testobligation.Report, c testobligation.DiscoveryCoverage) int {
	toJSON := func(in []testobligation.Obligation) []obligationJSON {
		out := make([]obligationJSON, 0, len(in))
		for _, o := range in {
			out = append(out, obligationJSON{
				Anchor:        o.Anchor,
				Required:      o.Required,
				Outcome:       o.Outcome.String(),
				Reason:        o.Reason,
				CandidateHint: o.CandidateHint,
			})
		}
		return out
	}
	payload := reportJSON{
		DiscoveryCompleteness: coverageNote(c),
		Verdict:               r.Verdict.String(),
		Certifies:             r.Verdict.Certifies(),
		ExitCode:              r.Verdict.ExitCode(),
		Obligations:           toJSON(r.Obligations),
		Blocking:              toJSON(r.Blocking()),
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(payload); err != nil {
		fmt.Fprintf(os.Stderr, "sensei verify-obligations: %v\n", err)
		return 2
	}
	return r.Verdict.ExitCode()
}
