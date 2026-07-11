// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	yaml "gopkg.in/yaml.v3"
)

// sensei review-realization — the review docket.
//
// A candidateRealizesContract is a HYPOTHESIS. This command records a human (or
// deterministic) review DECISION about one, so the same weak association is not
// proposed again and so a rejected lead still teaches the corpus what is NOT
// authority. Promotion (candidate -> realizesContract knowledge) is the separate
// `sensei promote-realization`; this command records everything that is not yet
// knowledge:
//
//	rejected             not an obligation — never propose again
//	needs_contract       the architectural contract it should realize is missing
//	needs_test           plausible, but no test proves the obligation yet
//	needs_failure_mode   plausible, but no failure mode shows what breaks
//	needs_human_decision plausible, awaiting an architect's confirmation
//
// Decisions are tracked process-state (an authored YAML file), NOT graph
// authority — a review is a verdict about a hypothesis, not a fact about the
// system, so it adds no predicates and never pollutes the authority graph.

var reviewDecisions = map[string]bool{
	"rejected":             true,
	"needs_contract":       true,
	"needs_test":           true,
	"needs_failure_mode":   true,
	"needs_human_decision": true,
}

type reviewEntry struct {
	Implementation string `yaml:"implementation"`
	Realizes       string `yaml:"realizes"`
	Decision       string `yaml:"decision"`
	Reason         string `yaml:"reason"`
}

type reviewsFile struct {
	ContractRealizationReviews struct {
		Reviews []reviewEntry `yaml:"reviews"`
	} `yaml:"contract_realization_reviews"`
}

func runReviewRealization(args []string) int {
	fs := flag.NewFlagSet("sensei review-realization", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	impl := fs.String("impl", "", "implementation contract id (required)")
	arch := fs.String("arch", "", "architectural contract id (required)")
	decision := fs.String("decision", "", "rejected | needs_contract | needs_test | needs_failure_mode | needs_human_decision (required)")
	reason := fs.String("reason", "", "why this decision (required)")
	svcRepoFlag := fs.String("services-repo", "", "path to services repo (auto-detect)")
	agRepoFlag := fs.String("ag-repo", "", "path to awareness-graph repo (auto-detect)")
	reviewsFlag := fs.String("reviews", "", "reviews file (default: in ag-repo)")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: sensei review-realization --impl <id> --arch <id> --decision <d> --reason "<why>"

Record a review decision on a candidateRealizesContract. The pair is then
excluded from future 'sensei suggest-realizations' runs. Promotion is a separate
command (sensei promote-realization).
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *impl == "" || *arch == "" || *decision == "" || *reason == "" {
		fmt.Fprintln(os.Stderr, "sensei review-realization: --impl, --arch, --decision and --reason are all required")
		return 2
	}
	if !reviewDecisions[*decision] {
		fmt.Fprintf(os.Stderr, "sensei review-realization: invalid --decision %q (want rejected|needs_contract|needs_test|needs_failure_mode|needs_human_decision)\n", *decision)
		return 2
	}

	path := *reviewsFlag
	if path == "" {
		agRepo := *agRepoFlag
		if agRepo == "" {
			svcRepo, _ := resolveServicesRepo(*svcRepoFlag)
			agRepo, _ = resolveAGRepo("", svcRepo)
		}
		path = filepath.Join(agRepo, "docs", "contract_realization_reviews.yaml")
	}

	var f reviewsFile
	if raw, err := os.ReadFile(path); err == nil {
		_ = yaml.Unmarshal(raw, &f)
	}
	updated := recordReview(&f, *impl, *arch, *decision, *reason)
	if err := os.WriteFile(path, renderReviews(&f), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "sensei review-realization: write: %v\n", err)
		return 1
	}
	verb := "recorded"
	if updated {
		verb = "updated"
	}
	fmt.Fprintf(os.Stderr, "review-realization: %s %s ~> %s = %s\n", verb, *impl, *arch, *decision)
	fmt.Fprintln(os.Stderr, "  this pair will no longer be proposed by `sensei suggest-realizations`.")
	return 0
}

// recordReview upserts one (impl,arch) review entry. Returns true if it replaced
// an existing entry. The list is kept stable-sorted for deterministic output.
func recordReview(f *reviewsFile, impl, arch, decision, reason string) (updated bool) {
	for i := range f.ContractRealizationReviews.Reviews {
		r := &f.ContractRealizationReviews.Reviews[i]
		if r.Implementation == impl && r.Realizes == arch {
			r.Decision, r.Reason = decision, reason
			updated = true
		}
	}
	if !updated {
		f.ContractRealizationReviews.Reviews = append(f.ContractRealizationReviews.Reviews,
			reviewEntry{Implementation: impl, Realizes: arch, Decision: decision, Reason: reason})
	}
	sort.SliceStable(f.ContractRealizationReviews.Reviews, func(i, j int) bool {
		a, b := f.ContractRealizationReviews.Reviews[i], f.ContractRealizationReviews.Reviews[j]
		if a.Implementation != b.Implementation {
			return a.Implementation < b.Implementation
		}
		return a.Realizes < b.Realizes
	})
	return updated
}

// decidedPairs returns the set of "impl|arch" pairs that already carry a review
// decision, so the candidate generator can skip them.
func decidedPairs(reviewsBytes []byte) map[string]bool {
	var f reviewsFile
	_ = yaml.Unmarshal(reviewsBytes, &f)
	out := map[string]bool{}
	for _, r := range f.ContractRealizationReviews.Reviews {
		if r.Implementation != "" && r.Realizes != "" {
			out[r.Implementation+"|"+r.Realizes] = true
		}
	}
	return out
}

func renderReviews(f *reviewsFile) []byte {
	if f.ContractRealizationReviews.Reviews == nil {
		f.ContractRealizationReviews.Reviews = []reviewEntry{}
	}
	var buf bytes.Buffer
	buf.WriteString("# Review decisions on candidateRealizesContract proposals.\n")
	buf.WriteString("# A pair recorded here is EXCLUDED from future `sensei suggest-realizations`\n")
	buf.WriteString("# runs — the decision sticks. Promoted decisions live in\n")
	buf.WriteString("# contract_realizations.yaml `realizations:` instead. This is tracked\n")
	buf.WriteString("# review process-state, NOT graph authority (no predicates).\n")
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	_ = enc.Encode(f)
	enc.Close()
	return buf.Bytes()
}
