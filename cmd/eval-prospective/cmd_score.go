// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/globulario/sensei/golang/architecture/prospectivelabel"
	"github.com/globulario/sensei/golang/architecture/prospectivescore"
)

// runScore compares a frozen answer key with a recorded run.
//
// It touches no graph and no retrieval surface. Scoring that could consult the
// system being graded is scoring that can be talked into a better number.
type scoreFlags struct{ set, labels, run, out *string }

func defineScoreFlags(fs *flag.FlagSet) scoreFlags {
	return scoreFlags{
		set:    fs.String("reference-set", "docs/evaluation/prospective-v1-reference-set", "frozen reference set"),
		labels: fs.String("labels", "", "frozen applicability labels (required)"),
		run:    fs.String("run", "", "retrieval run artifact (required)"),
		out:    fs.String("out", "", "path to write the score artifact to (required)"),
	}
}

func runScore(args []string) error {
	fs := flag.NewFlagSet("score", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	f := defineScoreFlags(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	set, labelsPath, runPath, out := f.set, f.labels, f.run, f.out
	for name, v := range map[string]string{"labels": *labelsPath, "run": *runPath, "out": *out} {
		if strings.TrimSpace(v) == "" {
			return fmt.Errorf("--%s is required", name)
		}
	}
	rs, labels, run, err := loadScoringInputs(*set, *labelsPath, *runPath)
	if err != nil {
		return err
	}
	score, err := prospectivescore.Compute(prospectivescore.Input{
		Manifest:        rs.Manifest,
		Labels:          labels,
		Run:             run,
		EligibleItemIDs: rs.EligibleItemIDs(),
	})
	if err != nil {
		return err
	}
	if err := writeSealedJSON(*out, score); err != nil {
		return err
	}
	fmt.Printf("score written: %s\n  digest: %s\n", *out, score.DigestSHA256)
	// Per-stratum first, and never a single number. A negative result is
	// emitted here exactly as it was computed.
	for _, st := range score.Strata {
		fmt.Printf("  %-24s changes=%-3d recall=%-18s primary_nuisance=%-18s unresolved=%-18s conservative=%s\n",
			st.Stratum, st.ChangeCount,
			renderRateCLI(st.Recall), renderRateCLI(st.PrimaryNuisance),
			renderRateCLI(st.UnresolvedSurfacedRate), renderRateCLI(st.ConservativeNuisance))
	}
	fmt.Println("\nNext: eval-prospective report --reference-set ... --score ...")
	return nil
}

// runReport renders the protocol section 12 report from a computed score.
type reportFlags struct{ set, score, out *string }

func defineReportFlags(fs *flag.FlagSet) reportFlags {
	return reportFlags{
		set:   fs.String("reference-set", "docs/evaluation/prospective-v1-reference-set", "frozen reference set"),
		score: fs.String("score", "", "score artifact (required)"),
		out:   fs.String("out", "", "path to write the report to; omitted writes to stdout"),
	}
}

func runReport(args []string) error {
	fs := flag.NewFlagSet("report", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	f := defineReportFlags(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	set, scorePath, out := f.set, f.score, f.out
	if strings.TrimSpace(*scorePath) == "" {
		return fmt.Errorf("--score is required")
	}
	rs, err := LoadReferenceSet(*set)
	if err != nil {
		return err
	}
	var score prospectivescore.Score
	if err := readJSON(*scorePath, &score); err != nil {
		return fmt.Errorf("score: %w", err)
	}
	if score.SampleManifestDigestSHA256 != rs.Manifest.DigestSHA256 {
		return fmt.Errorf("score answers sample manifest %s, not %s", score.SampleManifestDigestSHA256, rs.Manifest.DigestSHA256)
	}
	body := prospectivescore.Render(score, rs.Manifest)
	if strings.TrimSpace(*out) == "" {
		fmt.Print(body)
		return nil
	}
	if err := os.WriteFile(*out, []byte(body), 0o644); err != nil {
		return err
	}
	fmt.Printf("report written: %s\n", *out)
	return nil
}

func loadScoringInputs(set, labelsPath, runPath string) (*ReferenceSet, prospectivelabel.LabelSet, prospectivescore.Run, error) {
	rs, err := LoadReferenceSet(set)
	if err != nil {
		return nil, prospectivelabel.LabelSet{}, prospectivescore.Run{}, err
	}
	labels, err := prospectivelabel.LoadLabelSet(labelsPath)
	if err != nil {
		return nil, prospectivelabel.LabelSet{}, prospectivescore.Run{}, fmt.Errorf("frozen labels: %w", err)
	}
	var run prospectivescore.Run
	if err := readJSON(runPath, &run); err != nil {
		return nil, prospectivelabel.LabelSet{}, prospectivescore.Run{}, fmt.Errorf("run: %w", err)
	}
	return rs, labels, run, nil
}

func renderRateCLI(r prospectivescore.Rate) string {
	if r.Value == nil {
		return fmt.Sprintf("absent(0/%d)", r.Denominator)
	}
	return fmt.Sprintf("%.3f(%d/%d)", *r.Value, r.Numerator, r.Denominator)
}
