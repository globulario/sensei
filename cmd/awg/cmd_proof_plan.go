// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type forbiddenFixDoc struct {
	ForbiddenFixes []proofPlanForbiddenMove `yaml:"forbidden_fixes"`
}

type proofPlanForbiddenMove struct {
	ID       string `yaml:"id" json:"id"`
	Title    string `yaml:"title" json:"title,omitempty"`
	Summary  string `yaml:"summary" json:"summary,omitempty"`
	Reason   string `yaml:"reason" json:"reason,omitempty"`
	Protects struct {
		Files []string `yaml:"files" json:"files,omitempty"`
	} `yaml:"protects" json:"protects,omitempty"`
}

type proofPlanResult struct {
	Subject        string                      `json:"subject"`
	Authority      []authoritySurfaceCandidate `json:"authority_surfaces,omitempty"`
	Obligations    []generatedProofObligation  `json:"proof_obligations,omitempty"`
	ForbiddenMoves []proofPlanForbiddenMove    `json:"forbidden_moves,omitempty"`
}

func runProofPlan(args []string) int {
	fs := flag.NewFlagSet("awg proof-plan", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	repoRoot := fs.String("repo-root", ".", "repository root")
	fileArg := fs.String("file", "", "repo-relative file path to inspect")
	authorityID := fs.String("authority-surface-id", "", "authority surface id to inspect")
	proofID := fs.String("proof-obligation-id", "", "proof obligation id to inspect")
	repairClaimPath := fs.String("repair-claim", "", "learning event YAML/JSON file; resolves repair_claim authority/proof inputs")
	authorityPath := fs.String("authority", "", "authority surfaces YAML (default: <repo>/docs/awareness/candidates/authority_surface_candidates.yaml)")
	proofPath := fs.String("proof-obligations", "", "proof obligations YAML (default: <repo>/docs/awareness/generated/proof_obligations.yaml)")
	forbiddenPath := fs.String("forbidden-fixes", "", "forbidden fixes YAML (default: <repo>/docs/awareness/architecture/forbidden_fixes.yaml)")
	format := fs.String("format", "text", "output format: text | json")
	asJSON := fs.Bool("json", false, "deprecated alias for --format json")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: awg proof-plan [flags]

Resolve the governance proof plan for a file, authority surface, proof
obligation, or authored repair claim. Read-only: this command explains what
must be proven before a repair can be promoted; it never certifies or mutates.

Exactly one selector is required:
  --file <repo-relative/path>
  --authority-surface-id <id>
  --proof-obligation-id <id>
  --repair-claim <learning_event.yaml>

Flags:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *asJSON {
		*format = "json"
	}
	selectorCount := 0
	for _, v := range []string{strings.TrimSpace(*fileArg), strings.TrimSpace(*authorityID), strings.TrimSpace(*proofID), strings.TrimSpace(*repairClaimPath)} {
		if v != "" {
			selectorCount++
		}
	}
	if selectorCount != 1 {
		fmt.Fprintln(os.Stderr, "awg proof-plan: exactly one of --file, --authority-surface-id, --proof-obligation-id, or --repair-claim is required")
		return 2
	}
	root, err := filepath.Abs(*repoRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "awg proof-plan: resolve repo root: %v\n", err)
		return 1
	}
	authPath := *authorityPath
	if authPath == "" {
		authPath = filepath.Join(root, "docs", "awareness", "candidates", "authority_surface_candidates.yaml")
	}
	proofObPath := *proofPath
	if proofObPath == "" {
		proofObPath = filepath.Join(root, "docs", "awareness", "generated", "proof_obligations.yaml")
	}
	forbiddenFixPath := *forbiddenPath
	if forbiddenFixPath == "" {
		forbiddenFixPath = filepath.Join(root, "docs", "awareness", "architecture", "forbidden_fixes.yaml")
	}
	authorities, err := loadAuthoritySurfaces(authPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "awg proof-plan: load authority surfaces: %v\n", err)
		return 1
	}
	proofDoc, err := loadProofObligationsForCertify(proofObPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "awg proof-plan: load proof obligations: %v\n", err)
		return 1
	}
	forbiddenFixes, err := loadForbiddenFixes(forbiddenFixPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "awg proof-plan: load forbidden fixes: %v\n", err)
		return 1
	}

	res, err := buildProofPlan(root, authorities, proofDoc, forbiddenFixes, strings.TrimSpace(*fileArg), strings.TrimSpace(*authorityID), strings.TrimSpace(*proofID), strings.TrimSpace(*repairClaimPath))
	if err != nil {
		fmt.Fprintf(os.Stderr, "awg proof-plan: %v\n", err)
		return 1
	}

	switch *format {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(res)
	default:
		fmt.Print(renderProofPlanText(res))
	}
	return 0
}

func buildProofPlan(root string, authorities []authoritySurfaceCandidate, proofDoc proofObligationsDoc, forbiddenFixes []proofPlanForbiddenMove, fileArg, authorityID, proofID, repairClaimPath string) (proofPlanResult, error) {
	selectedAuthorities := map[string]authoritySurfaceCandidate{}
	selectedObligations := map[string]generatedProofObligation{}
	subject := ""

	switch {
	case fileArg != "":
		subject = fileArg
		for _, a := range authorities {
			if containsStringExact(a.SourceFiles, fileArg) {
				selectedAuthorities[a.ID] = a
			}
		}
	case authorityID != "":
		subject = authorityID
		for _, a := range authorities {
			if a.ID == authorityID {
				selectedAuthorities[a.ID] = a
				break
			}
		}
		if len(selectedAuthorities) == 0 {
			return proofPlanResult{}, fmt.Errorf("authority surface %q not found", authorityID)
		}
	case proofID != "":
		subject = proofID
		for _, ob := range proofDoc.ProofObligations {
			if ob.ID == proofID {
				selectedObligations[ob.ID] = ob
				for _, aid := range ob.AppliesToAuthoritySurfaces {
					for _, a := range authorities {
						if a.ID == aid {
							selectedAuthorities[a.ID] = a
						}
					}
				}
				if ob.DerivedFromAuthoritySurface != "" {
					for _, a := range authorities {
						if a.ID == ob.DerivedFromAuthoritySurface {
							selectedAuthorities[a.ID] = a
						}
					}
				}
				break
			}
		}
		if len(selectedObligations) == 0 {
			return proofPlanResult{}, fmt.Errorf("proof obligation %q not found", proofID)
		}
	case repairClaimPath != "":
		doc, err := loadBenchmarkRetryDoc(repairClaimPath)
		if err != nil {
			return proofPlanResult{}, fmt.Errorf("load repair claim: %w", err)
		}
		event := benchmarkRetryUnwrapEvent(doc)
		claim := parseRepairClaim(event)
		subject = firstNonEmpty(claim.ID, repairClaimPath)
		for _, id := range claim.AuthoritySurfaceIDs {
			for _, a := range authorities {
				if a.ID == id {
					selectedAuthorities[a.ID] = a
				}
			}
		}
		for _, id := range claim.ProofObligationIDs {
			for _, ob := range proofDoc.ProofObligations {
				if ob.ID == id {
					selectedObligations[ob.ID] = ob
				}
			}
		}
		if len(selectedAuthorities) == 0 && len(selectedObligations) == 0 {
			return proofPlanResult{}, fmt.Errorf("repair claim did not resolve any authority surfaces or proof obligations")
		}
	}

	for _, ob := range proofDoc.ProofObligations {
		for _, a := range selectedAuthorities {
			if ob.DerivedFromAuthoritySurface == a.ID || containsStringExact(ob.AppliesToAuthoritySurfaces, a.ID) {
				selectedObligations[ob.ID] = ob
			}
		}
	}

	authorityList := sortAuthorityCandidates(selectedAuthorities)
	obligationList := sortProofObligations(selectedObligations)
	forbiddenList := matchForbiddenMoves(authorityList, forbiddenFixes)
	return proofPlanResult{
		Subject:        subject,
		Authority:      authorityList,
		Obligations:    obligationList,
		ForbiddenMoves: forbiddenList,
	}, nil
}

func loadForbiddenFixes(path string) ([]proofPlanForbiddenMove, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc forbiddenFixDoc
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	return doc.ForbiddenFixes, nil
}

func sortAuthorityCandidates(in map[string]authoritySurfaceCandidate) []authoritySurfaceCandidate {
	out := make([]authoritySurfaceCandidate, 0, len(in))
	for _, a := range in {
		out = append(out, a)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func sortProofObligations(in map[string]generatedProofObligation) []generatedProofObligation {
	out := make([]generatedProofObligation, 0, len(in))
	for _, ob := range in {
		out = append(out, ob)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func matchForbiddenMoves(authorities []authoritySurfaceCandidate, forbiddenFixes []proofPlanForbiddenMove) []proofPlanForbiddenMove {
	protectedFiles := map[string]bool{}
	for _, a := range authorities {
		for _, f := range a.SourceFiles {
			protectedFiles[f] = true
		}
	}
	var out []proofPlanForbiddenMove
	for _, move := range forbiddenFixes {
		for _, f := range move.Protects.Files {
			if protectedFiles[f] {
				out = append(out, move)
				break
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func containsStringExact(items []string, want string) bool {
	for _, item := range items {
		if strings.TrimSpace(item) == strings.TrimSpace(want) {
			return true
		}
	}
	return false
}

func renderProofPlanText(res proofPlanResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Proof plan: %s\n", res.Subject)
	if len(res.Authority) > 0 {
		fmt.Fprintf(&b, "\nAuthority surfaces:\n")
		for _, a := range res.Authority {
			fmt.Fprintf(&b, "  - %s\n", a.ID)
			fmt.Fprintf(&b, "    kind: %s\n", a.Kind)
			if len(a.SourceFiles) > 0 {
				fmt.Fprintf(&b, "    source_files: %s\n", strings.Join(a.SourceFiles, ", "))
			}
		}
	}
	if len(res.Obligations) > 0 {
		fmt.Fprintf(&b, "\nProof obligations:\n")
		for _, ob := range res.Obligations {
			fmt.Fprintf(&b, "  - %s\n", ob.ID)
			if ob.EvidenceLane != "" {
				fmt.Fprintf(&b, "    evidence_lane: %s\n", ob.EvidenceLane)
			}
			if len(ob.RequiredSlots) > 0 {
				var slots []string
				for _, slot := range ob.RequiredSlots {
					if slot.Required {
						slots = append(slots, slot.Kind)
					}
				}
				fmt.Fprintf(&b, "    required_slots: %s\n", strings.Join(slots, ", "))
			}
			if ob.Notes != "" {
				fmt.Fprintf(&b, "    notes: %s\n", ob.Notes)
			}
		}
	}
	if len(res.ForbiddenMoves) > 0 {
		fmt.Fprintf(&b, "\nForbidden moves:\n")
		for _, move := range res.ForbiddenMoves {
			fmt.Fprintf(&b, "  - %s\n", move.ID)
			if move.Title != "" {
				fmt.Fprintf(&b, "    title: %s\n", move.Title)
			}
			if move.Reason != "" {
				fmt.Fprintf(&b, "    reason: %s\n", strings.TrimSpace(move.Reason))
			}
		}
	}
	fmt.Fprintf(&b, "\nPromotion requires:\n")
	fmt.Fprintf(&b, "  - all required slots satisfied\n")
	fmt.Fprintf(&b, "  - no forbidden moves detected\n")
	return b.String()
}
