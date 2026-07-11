// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"fmt"
	"sort"
	"strings"

	awarenesspb "github.com/globulario/sensei/golang/pb"
)

// The completeness check is the one thing a PERSISTENT graph gives that
// stateless review cannot: it knows every site that shares a reference, so it
// can say "you changed 3 of the 9 call-sites of X — here are the 6 you missed".
// It is ADVISORY (a hint, not a block): a partial change is often correct, so
// this never affects the gate's exit code. It is built on the SCIP aw:references
// edges surfaced by `sensei scip-ingest`; with no ingested symbols it finds nothing
// and stays silent.

// completenessFinding is one reference family the diff touched incompletely.
type completenessFinding struct {
	Target       string   `json:"target"`        // referenced symbol id (file:symbol)
	TargetLabel  string   `json:"target_label"`  // short name for humans
	TotalSites   int      `json:"total_sites"`   // all symbols referencing Target
	TouchedSites int      `json:"touched_sites"` // referencing symbols in changed files
	MissedFiles  []string `json:"missed_files"`  // files with untouched referencing sites
}

// symbolFileOf returns the source-file portion of a "file:symbol" code-symbol
// id. Repo-relative paths carry no ':' so the symbol is whatever follows the
// LAST ':'. Returns the whole id if it has no ':' (defensive).
func symbolFileOf(id string) string {
	if i := strings.LastIndex(id, ":"); i > 0 {
		return id[:i]
	}
	return id
}

// analyzeReferenceFamily classifies one target's referencing sites against the
// changed-file set: how many sites there are in total, how many sit in changed
// files, and the distinct files of the sites the diff left untouched. Pure so it
// is exhaustively unit-tested. missedFiles is sorted and de-duplicated.
func analyzeReferenceFamily(siteIDs []string, changedFiles map[string]bool) (total, touched int, missedFiles []string) {
	missed := map[string]bool{}
	for _, s := range siteIDs {
		total++
		f := symbolFileOf(s)
		if changedFiles[f] {
			touched++
		} else {
			missed[f] = true
		}
	}
	for f := range missed {
		missedFiles = append(missedFiles, f)
	}
	sort.Strings(missedFiles)
	return total, touched, missedFiles
}

// shouldReportFamily decides whether a family is worth flagging. The signal is
// only meaningful when the diff touched at least one site (so it is an
// INCOMPLETE change, not an untouched family), left at least one site
// unchanged, and the family is small enough to be a convention rather than a
// shared utility (bounded by maxFanout).
func shouldReportFamily(total, touched int, missedFiles []string, maxFanout int) bool {
	return touched >= 1 && len(missedFiles) >= 1 && total >= 2 && total <= maxFanout
}

// collectInternalTargets gathers the internal (in-repo) symbols the changed
// files reference — the candidate targets to check for completeness. External
// symbols ("external:<name>") are excluded: you cannot be "incomplete" about
// editing every caller of fmt.Sprintf.
func collectInternalTargets(symbols []*awarenesspb.CodeSymbolNode) []string {
	set := map[string]bool{}
	for _, sym := range symbols {
		for _, ref := range sym.GetReferences() {
			if ref == "" || strings.HasPrefix(ref, "external:") {
				continue
			}
			set[ref] = true
		}
	}
	out := make([]string, 0, len(set))
	for t := range set {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// runCompleteness computes the advisory sibling-site findings for a diff. It
// asks the graph, per changed file, which internal symbols the file references
// (Impact), then asks which OTHER sites reference those same symbols
// (ReferenceSites), and flags the families the diff touched incompletely.
//
// It NEVER fails the gate: any backend error is folded into the returned note
// and an empty finding set. The graph having no ingested symbols is normal and
// simply yields no findings.
func runCompleteness(ctx context.Context, client awarenesspb.AwarenessGraphClient, changedFiles map[string]bool, domain string, maxFanout int) ([]completenessFinding, string) {
	files := make([]string, 0, len(changedFiles))
	for f := range changedFiles {
		files = append(files, f)
	}
	sort.Strings(files)

	targetSet := map[string]bool{}
	for _, f := range files {
		resp, err := client.Impact(ctx, &awarenesspb.ImpactRequest{File: f, Domain: domain})
		if err != nil {
			// Advisory: a scope/backend error on one file just means we cannot
			// judge completeness from it. Skip it, keep going.
			continue
		}
		for _, t := range collectInternalTargets(resp.GetSymbols()) {
			targetSet[t] = true
		}
	}
	if len(targetSet) == 0 {
		return nil, ""
	}
	targets := make([]string, 0, len(targetSet))
	for t := range targetSet {
		targets = append(targets, t)
	}
	sort.Strings(targets)

	resp, err := client.ReferenceSites(ctx, &awarenesspb.ReferenceSitesRequest{
		SymbolIds: targets,
		Domain:    domain,
	})
	if err != nil {
		return nil, fmt.Sprintf("completeness check unavailable: %v", err)
	}

	var findings []completenessFinding
	for _, fam := range resp.GetFamilies() {
		total, touched, missed := analyzeReferenceFamily(fam.GetSiteIds(), changedFiles)
		if !shouldReportFamily(total, touched, missed, maxFanout) {
			continue
		}
		findings = append(findings, completenessFinding{
			Target:       fam.GetSymbolId(),
			TargetLabel:  labelOfSymbol(fam.GetSymbolId()),
			TotalSites:   total,
			TouchedSites: touched,
			MissedFiles:  missed,
		})
	}
	// Most-relevant first (more missed sites = more likely a real omission),
	// tie-break by target for determinism.
	sort.Slice(findings, func(i, j int) bool {
		if len(findings[i].MissedFiles) != len(findings[j].MissedFiles) {
			return len(findings[i].MissedFiles) > len(findings[j].MissedFiles)
		}
		return findings[i].Target < findings[j].Target
	})
	// Anti-flood: discovery is file-level, so a busy file can touch many small
	// families. Cap what we print, but NEVER silently — name how many were
	// dropped so the report doesn't read as "these are the only ones".
	note := ""
	if len(findings) > maxCompletenessFindings {
		note = fmt.Sprintf("showing %d of %d incompletely-touched families (most-missed first)", maxCompletenessFindings, len(findings))
		findings = findings[:maxCompletenessFindings]
	}
	return findings, note
}

// maxCompletenessFindings bounds the advisory output so a single busy file
// cannot flood the report.
const maxCompletenessFindings = 10

// labelOfSymbol returns the short symbol name from a "file:symbol" id.
func labelOfSymbol(id string) string {
	if i := strings.LastIndex(id, ":"); i >= 0 && i+1 < len(id) {
		return id[i+1:]
	}
	return id
}

// printCompleteness renders the advisory completeness section. Silent when there
// is nothing to say (no findings and no note) so a clean diff stays quiet.
func printCompleteness(findings []completenessFinding, note string) {
	if len(findings) == 0 && note == "" {
		return
	}
	fmt.Printf("\nCompleteness (advisory — sibling call-sites, does not block):\n")
	if note != "" {
		fmt.Printf("  %s\n", note)
	}
	for _, f := range findings {
		fmt.Printf("  %s: %d call-site(s) reference it, your diff touched %d — %d file(s) left unchanged:\n",
			f.TargetLabel, f.TotalSites, f.TouchedSites, len(f.MissedFiles))
		for _, m := range f.MissedFiles {
			fmt.Printf("    - %s\n", m)
		}
	}
	if len(findings) > 0 {
		fmt.Printf("  (if this change alters a shared contract, the unchanged sites may need the same update)\n")
	}
}
