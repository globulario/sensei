// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/globulario/sensei/golang/architecture/certification"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

// runCertifyChange is the thin adapter for the architectural-closure
// certification engine. It carries NO business rules: verification, record
// resolution, lane recomputation, verdicts, and ledger appends all live in
// golang/architecture/certification. This is the only command that can
// produce a closureprotocol.CertificationReceipt or append a `certified`
// task-ledger event; the legacy `sensei certify --event` benchmark adapter
// can do neither.
func runCertifyChange(args []string) int {
	fs := flag.NewFlagSet("sensei certify-change", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var taskDir, expectedHead, requestPath, format string
	fs.StringVar(&taskDir, "task-dir", "", "task directory (.sensei/tasks/task.<id>)")
	fs.StringVar(&expectedHead, "expected-head", "", "expected task-ledger head digest")
	fs.StringVar(&requestPath, "request", "", "typed certification request (default <task-dir>/"+certification.RequestFileName+")")
	fs.StringVar(&format, "format", "text", "output format: text|json")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: sensei certify-change --task-dir <dir> --expected-head <digest> [flags]

Runs the Phase 6 architectural-closure certification engine over a verified
task ledger: every referenced record is loaded content-addressed and
digest-verified, the four lanes (scope, authority, proof, evidence) are
recomputed from typed records, and a frozen CertificationReceipt is emitted.
On a certifying verdict the frozen 'certified' ledger event is appended; a
blocked evaluation reports deterministically and leaves the ledger untouched.
This command never appends a 'completed' event (Phase 8 owns completion).
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(taskDir) == "" || strings.TrimSpace(expectedHead) == "" {
		fmt.Fprintln(os.Stderr, "sensei certify-change: --task-dir and --expected-head are required")
		return 2
	}
	if format != "text" && format != "json" {
		fmt.Fprintln(os.Stderr, "sensei certify-change: --format must be text|json")
		return 2
	}

	res, err := certification.CertifyTask(certification.TaskCertifyOptions{
		TaskDir:                  taskDir,
		ExpectedHeadDigestSHA256: expectedHead,
		RequestPath:              requestPath,
		ProducedAt:               time.Now().UTC(),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei certify-change: %v\n", err)
		return 1
	}

	switch format {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(res); err != nil {
			fmt.Fprintf(os.Stderr, "sensei certify-change: %v\n", err)
			return 1
		}
	default:
		fmt.Print(renderCertifyChangeText(res))
	}

	switch res.Result.Receipt.CertificationVerdict {
	case closureprotocol.Certified, closureprotocol.CertifiedWithConditions:
		return 0
	default:
		return 1
	}
}

func renderCertifyChangeText(res certification.TaskCertifyResult) string {
	var b strings.Builder
	receipt := res.Result.Receipt
	fmt.Fprintf(&b, "Certification verdict: %s\n", receipt.CertificationVerdict)
	fmt.Fprintf(&b, "Policy: %s\n", receipt.CertificationPolicy)
	fmt.Fprintf(&b, "Receipt digest: %s\n", receipt.DigestSHA256)
	fmt.Fprintf(&b, "Lanes:\n")
	for _, lane := range res.Result.Lanes {
		fmt.Fprintf(&b, "  - %s: %s\n", lane.Lane, lane.Status)
		for _, reason := range lane.ReasonCodes {
			fmt.Fprintf(&b, "    reason: %s\n", reason)
		}
		for _, limitation := range lane.Limitations {
			fmt.Fprintf(&b, "    limitation: %s\n", limitation)
		}
	}
	if len(receipt.ForbiddenMoves) > 0 {
		fmt.Fprintf(&b, "Forbidden moves:\n")
		for _, move := range receipt.ForbiddenMoves {
			fmt.Fprintf(&b, "  - %s\n", move)
		}
	}
	if len(receipt.UnresolvedContradictions) > 0 {
		fmt.Fprintf(&b, "Unresolved contradictions:\n")
		for _, id := range receipt.UnresolvedContradictions {
			fmt.Fprintf(&b, "  - %s\n", id)
		}
	}
	if res.Appended {
		fmt.Fprintf(&b, "Ledger: certified event appended (head %s)\n", res.Head.EntryDigestSHA256)
	} else {
		fmt.Fprintf(&b, "Ledger: unchanged (no certified event)\n")
	}
	fmt.Fprintf(&b, "Next: %s\n", res.Result.NextAction)
	return b.String()
}
