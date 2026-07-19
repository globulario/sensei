// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/globulario/sensei/golang/architecture/completion"
)

// runGateCompletion is Phase 9.4a: the ADVISORY completion gate. It resolves an EXPLICIT
// task directory, consumes the canonical typed availability envelope
// (completion.BuildCompletionProjectionEnvelope), validates its canonical publication
// form, and reports the availability, the closure verdict, and the three distinctions in
// text / JSON / SARIF. It is advisory only: it reads completion truth read-only, mutates
// no authoritative state, applies no enforcement policy, and exits 0 on every outcome.
// Enforcement, per-domain policy, the change-to-task binding, the CI action, and audit
// emission are out of scope for 9.4a (locked to 9.4b/9.4c).
func runGateCompletion(repoRoot, taskDir string, asJSON bool, sarifPath string) int {
	td := strings.TrimSpace(taskDir)
	if td == "" {
		fmt.Fprintln(os.Stderr, "sensei gate --completion: --task-dir is required (9.4a takes an explicit task directory)")
		return 2
	}
	absRepo, err := filepath.Abs(strings.TrimSpace(repoRoot))
	if err != nil {
		fmt.Fprintln(os.Stderr, "sensei gate --completion:", err)
		return 2
	}
	absTask, err := filepath.Abs(td)
	if err != nil {
		fmt.Fprintln(os.Stderr, "sensei gate --completion:", err)
		return 2
	}

	// Consume the typed availability envelope and its validated canonical publication
	// form — never the bare projection, never raw ledger, never Go-error interpretation.
	env := completion.BuildCompletionProjectionEnvelope(context.Background(), completion.Request{RepositoryRoot: absRepo, TaskDirectory: absTask})
	pub := env.PublicationView()
	pubInvalid := completion.ValidateCompletionPublication(pub) != nil

	if sarifPath != "" {
		if werr := writeCompletionGateSARIF(sarifPath, absTask, pub, pubInvalid); werr != nil {
			fmt.Fprintf(os.Stderr, "sensei gate --completion: %v\n", werr)
			return 2
		}
	}
	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if eerr := enc.Encode(pub); eerr != nil {
			fmt.Fprintf(os.Stderr, "sensei gate --completion: %v\n", eerr)
			return 2
		}
	} else {
		fmt.Print(renderCompletionGateText(absTask, pub, pubInvalid))
	}
	// Advisory: the gate reports and blocks nothing. Enforcement is 9.4b.
	return 0
}

// completionGateReport reduces the validated publication to a headline availability/verdict
// line and the reported detail lines (the three distinctions when available). It reads only
// the typed envelope; it re-derives no verdict.
func completionGateReport(pub completion.CompletionProjectionPublication, pubInvalid bool) (availability, headline string, detail []string) {
	if pubInvalid || !pub.Canonical || pub.Envelope == nil {
		return "invalid_publication", "invalid completion publication (" + string(pub.InvalidClass) + ")", nonEmpty(pub.InvalidReason)
	}
	e := pub.Envelope
	if e.Availability == completion.CompletionUnavailable {
		return "unavailable", "projection unavailable (" + string(e.UnavailableClass) + ")", nonEmpty(e.UnavailableDetail)
	}
	if e.Projection == nil {
		return "unavailable", "projection unavailable (missing projection)", nil
	}
	p := e.Projection
	head := fmt.Sprintf("verdict=%s authoritative=%v terminal_state=%s", p.ClosureVerdict, p.AuthoritativeCompletion, p.TerminalState)
	return "available", head, p.Distinctions
}

func nonEmpty(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return []string{s}
}

func renderCompletionGateText(taskDir string, pub completion.CompletionProjectionPublication, pubInvalid bool) string {
	avail, head, detail := completionGateReport(pub, pubInvalid)
	var b strings.Builder
	fmt.Fprintf(&b, "Completion gate (advisory) — task %s\n", taskDir)
	fmt.Fprintf(&b, "Availability: %s\n", avail)
	fmt.Fprintf(&b, "Completion: %s\n", head)
	if avail == "available" {
		fmt.Fprintf(&b, "Distinctions:\n")
	}
	for _, d := range detail {
		fmt.Fprintf(&b, "  - %s\n", d)
	}
	fmt.Fprintf(&b, "advisory: read-only completion report; this gate blocks nothing (enforcement is Phase 9.4b).\n")
	return b.String()
}

// writeCompletionGateSARIF renders the advisory completion report as a single SARIF
// result at severity "note" — advisory never surfaces as an error/warning alert. It
// anchors to the task directory. It reuses the gate's SARIF vocabulary.
func writeCompletionGateSARIF(path, taskDir string, pub completion.CompletionProjectionPublication, pubInvalid bool) error {
	avail, head, _ := completionGateReport(pub, pubInvalid)
	rule := sarifRule{
		ID:                   "sensei.completion_gate.advisory",
		Name:                 "SenseiCompletionGateAdvisory",
		ShortDescription:     sarifText{Text: "Advisory report of a task's Phase-8 completion closure verdict."},
		DefaultConfiguration: sarifConfig{Level: "note"},
	}
	result := sarifResult{
		RuleID:  rule.ID,
		Level:   "note",
		Message: sarifText{Text: fmt.Sprintf("completion gate (advisory): availability=%s %s", avail, head)},
		Locations: []sarifLocation{{PhysicalLocation: sarifPhysical{
			ArtifactLocation: sarifArtifact{URI: filepath.ToSlash(taskDir)},
			Region:           sarifRegion{StartLine: 1},
		}}},
	}
	log := sarifLog{
		Schema:  "https://json.schemastore.org/sarif-2.1.0.json",
		Version: "2.1.0",
		Runs: []sarifRun{{
			Tool:    sarifTool{Driver: sarifDriver{Name: "sensei-completion-gate", InformationURI: "https://github.com/globulario/sensei", Rules: []sarifRule{rule}}},
			Results: []sarifResult{result},
		}},
	}
	data, err := json.MarshalIndent(log, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}
