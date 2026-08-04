// SPDX-License-Identifier: AGPL-3.0-only

package senseireport

import (
	"fmt"
	"strings"
)

// RenderMarkdown renders r deterministically to the SENSEI.md content.
// It performs no derivation of its own -- every value comes from the
// already-built Report -- so it can be called any number of times on the
// same value and always produce byte-identical output. Tone follows the
// design doc literally: facts ("Blocking findings: 0"), never adjectives
// ("Architecture health: excellent").
func RenderMarkdown(r Report) []byte {
	var b strings.Builder

	b.WriteString("# Sensei Report\n\n")
	fmt.Fprintf(&b, "Repository: %s\n", r.Identity.Repository.DisplayName)
	fmt.Fprintf(&b, "Evaluated commit: %s (%s)\n", displayOrUnknown(r.Identity.EvaluatedCommit), r.Identity.EvaluatedCommitStatus)
	fmt.Fprintf(&b, "Evaluated content digest: %s\n", r.Identity.EvaluatedContentDigestSHA256)
	fmt.Fprintf(&b, "Report schema: %s\n", r.SchemaVersion)
	fmt.Fprintf(&b, "Report freshness: %s\n\n", r.Verification.ReportFreshness)

	b.WriteString("## Summary\n\n")
	fmt.Fprintf(&b, "- Blocking findings: %d\n", r.Summary.BlockingFindings)
	fmt.Fprintf(&b, "- Advisory findings: %d\n", r.Summary.AdvisoryFindings)
	fmt.Fprintf(&b, "- Memory candidates awaiting review: %d\n\n", r.Summary.CandidatesAwaitingReview)

	b.WriteString("## Current Work\n\n")
	if !r.CurrentWork.Active {
		fmt.Fprintf(&b, "%s\n\n", displayOrUnknown(r.CurrentWork.Note))
	} else {
		fmt.Fprintf(&b, "Task: %s\n", r.CurrentWork.TaskID)
		if r.CurrentWork.Title != "" {
			fmt.Fprintf(&b, "Title: %s\n", r.CurrentWork.Title)
		}
		fmt.Fprintf(&b, "Disposition: %s\n", r.CurrentWork.Disposition)
		if len(r.CurrentWork.Scope) > 0 {
			fmt.Fprintf(&b, "Scope: %d file(s)\n", len(r.CurrentWork.Scope))
		} else {
			b.WriteString("Scope: unspecified\n")
		}
		fmt.Fprintf(&b, "Authority: %s\n", r.CurrentWork.Authority)
		fmt.Fprintf(&b, "Remaining blockers: %d\n", r.CurrentWork.RemainingBlockers)
		if r.CurrentWork.PrimaryBlocker != "" {
			fmt.Fprintf(&b, "Primary blocker: %s\n", r.CurrentWork.PrimaryBlocker)
		}
		b.WriteString("\n")
	}

	b.WriteString("## Important Findings\n\n")
	if len(r.Findings) == 0 {
		b.WriteString("None.\n\n")
	} else {
		for _, f := range r.Findings {
			fmt.Fprintf(&b, "- [%s] %s\n", f.Kind, f.Statement)
		}
		b.WriteString("\n")
	}

	b.WriteString("## Verification\n\n")
	if r.CurrentWork.Active {
		fmt.Fprintf(&b, "- Latest governed task: %s — %s\n", r.CurrentWork.TaskID, r.CurrentWork.Disposition)
		if r.Verification.TaskReadiness != "" {
			fmt.Fprintf(&b, "- Task readiness: %s\n", r.Verification.TaskReadiness)
		}
		if r.Verification.ObligationsTotal > 0 {
			fmt.Fprintf(&b, "- Obligations satisfied: %d/%d\n", r.Verification.ObligationsSatisfied, r.Verification.ObligationsTotal)
		}
	} else {
		b.WriteString("- Latest governed task: none\n")
	}
	fmt.Fprintf(&b, "- Report freshness: %s\n", r.Verification.ReportFreshness)
	fmt.Fprintf(&b, "- Repository-wide verification: %s\n\n", displayNotRun(r.Verification.RepositoryWideVerification))

	b.WriteString("## Behavioral Memory\n\n")
	fmt.Fprintf(&b, "- Candidates awaiting review: %d\n", r.Memory.CandidatesAwaitingReview)
	for _, c := range r.Memory.Highlighted {
		if c.Class != "" {
			fmt.Fprintf(&b, "  - %s (%s) — %s\n", c.ID, c.Class, c.Source)
		} else {
			fmt.Fprintf(&b, "  - %s — %s\n", c.ID, c.Source)
		}
	}
	b.WriteString("\n")

	b.WriteString("## Reproduce\n\n")
	b.WriteString("```sh\n")
	for _, cmd := range r.Reproduction.Commands {
		b.WriteString(cmd)
		b.WriteString("\n")
	}
	b.WriteString("```\n")

	if len(r.Limitations) > 0 {
		b.WriteString("\n## Limitations\n\n")
		for _, l := range r.Limitations {
			fmt.Fprintf(&b, "- %s\n", l)
		}
	}

	return []byte(b.String())
}

func displayOrUnknown(s string) string {
	if strings.TrimSpace(s) == "" {
		return "unknown"
	}
	return s
}

func displayNotRun(s string) string {
	if s == RepositoryWideVerificationNotRun {
		return "NOT RUN"
	}
	return s
}
