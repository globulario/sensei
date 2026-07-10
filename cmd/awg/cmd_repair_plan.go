// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	awarenesspb "github.com/globulario/awareness-graph/golang/pb"
)

type repairPlanResult struct {
	Task            string                      `json:"task,omitempty"`
	Files           []string                    `json:"files,omitempty"`
	Status          string                      `json:"status,omitempty"`
	RiskClass       string                      `json:"risk_class,omitempty"`
	Confidence      string                      `json:"confidence,omitempty"`
	Authority       *awarenesspb.GraphAuthority `json:"authority,omitempty"`
	FilesToRead     []string                    `json:"files_to_read,omitempty"`
	RequiredActions []string                    `json:"required_actions,omitempty"`
	TestsToRun      []string                    `json:"tests_to_run,omitempty"`
	BlindSpots      []string                    `json:"blind_spots,omitempty"`
	OrderedSteps    []string                    `json:"ordered_steps,omitempty"`
	Proof           proofPlanResult             `json:"proof"`
}

var repairPlanPreflight = fetchRepairPlanPreflight

func runRepairPlan(args []string) int {
	fs := flag.NewFlagSet("awg repair-plan", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	task := fs.String("task", "", "task description")
	addr := fs.String("addr", "localhost:10120", "AWG gRPC server address")
	repoRoot := fs.String("repo-root", ".", "repository root")
	mode := fs.String("mode", "standard", "preflight mode: standard | compact")
	domain := fs.String("domain", "", "domain/repo scope passed through to preflight")
	authorityPath := fs.String("authority", "", "authority surfaces YAML (default: <repo>/docs/awareness/candidates/authority_surface_candidates.yaml)")
	proofPath := fs.String("proof-obligations", "", "proof obligations YAML (default: <repo>/docs/awareness/generated/proof_obligations.yaml)")
	forbiddenPath := fs.String("forbidden-fixes", "", "forbidden fixes YAML (default: <repo>/docs/awareness/architecture/forbidden_fixes.yaml)")
	format := fs.String("format", "text", "output format: text | json")
	asJSON := fs.Bool("json", false, "deprecated alias for --format json")
	var files stringSlice
	fs.Var(&files, "file", "repo-relative file (repeatable)")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: awg repair-plan [--file <path>]... [--task "description"] [flags]

Build a governed repair plan by combining authoritative preflight output from a
current graph with repo-local proof obligations and forbidden moves.

This command fails closed if the server cannot prove the graph is current and
authoritative.

At least one of --file or --task is required.

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
	if len(files) == 0 && strings.TrimSpace(*task) == "" {
		fmt.Fprintln(os.Stderr, "awg repair-plan: provide --file and/or --task")
		return 2
	}

	root, err := filepath.Abs(*repoRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "awg repair-plan: resolve repo root: %v\n", err)
		return 1
	}
	authPath, proofObPath, forbiddenFixPath := defaultProofPlanPaths(root, *authorityPath, *proofPath, *forbiddenPath)
	authorities, err := loadAuthoritySurfaces(authPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "awg repair-plan: load authority surfaces: %v\n", err)
		return 1
	}
	proofDoc, err := loadProofObligationsForCertify(proofObPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "awg repair-plan: load proof obligations: %v\n", err)
		return 1
	}
	forbiddenFixes, err := loadForbiddenFixes(forbiddenFixPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "awg repair-plan: load forbidden fixes: %v\n", err)
		return 1
	}

	pfMode := awarenesspb.PreflightMode_PREFLIGHT_STANDARD
	if strings.EqualFold(*mode, "compact") {
		pfMode = awarenesspb.PreflightMode_PREFLIGHT_COMPACT
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	resp, err := repairPlanPreflight(ctx, *addr, &awarenesspb.PreflightRequest{
		Task:   strings.TrimSpace(*task),
		Files:  files,
		Mode:   pfMode,
		Domain: *domain,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "awg repair-plan: %v\n", err)
		return 1
	}
	if err := requireAuthoritativeGraph(resp.GetAuthority(), "repair-plan"); err != nil {
		fmt.Fprintf(os.Stderr, "awg repair-plan: %v\n", err)
		return 1
	}

	proof, err := buildProofPlanForFiles(root, authorities, proofDoc, forbiddenFixes, files)
	if err != nil {
		fmt.Fprintf(os.Stderr, "awg repair-plan: build proof plan: %v\n", err)
		return 1
	}

	res := buildRepairPlanResult(strings.TrimSpace(*task), files, resp, proof)
	switch *format {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(res)
	default:
		fmt.Print(renderRepairPlanText(res))
	}
	return 0
}

func fetchRepairPlanPreflight(ctx context.Context, addr string, req *awarenesspb.PreflightRequest) (*awarenesspb.PreflightResponse, error) {
	client, err := connectAWG(addr)
	if err != nil {
		return nil, err
	}
	defer client.Close()
	return client.Preflight(ctx, req)
}

func defaultProofPlanPaths(root, authorityPath, proofPath, forbiddenPath string) (string, string, string) {
	if authorityPath == "" {
		authorityPath = filepath.Join(root, "docs", "awareness", "candidates", "authority_surface_candidates.yaml")
	}
	if proofPath == "" {
		proofPath = filepath.Join(root, "docs", "awareness", "generated", "proof_obligations.yaml")
	}
	if forbiddenPath == "" {
		forbiddenPath = filepath.Join(root, "docs", "awareness", "architecture", "forbidden_fixes.yaml")
	}
	return authorityPath, proofPath, forbiddenPath
}

func requireAuthoritativeGraph(authority *awarenesspb.GraphAuthority, surface string) error {
	if authority == nil {
		return fmt.Errorf("%s requires current graph authority: authority metadata unavailable", surface)
	}
	if !authority.GetAuthoritative() {
		return fmt.Errorf("%s requires current graph authority: server returned non-authoritative state (%s)", surface, authorityStateName(authority.GetGraphFreshnessState()))
	}
	if authority.GetGraphFreshnessState() != awarenesspb.GraphFreshnessState_GRAPH_FRESHNESS_STATE_CURRENT {
		return fmt.Errorf("%s requires current graph authority: freshness is %s", surface, authorityStateName(authority.GetGraphFreshnessState()))
	}
	return nil
}

func authorityStateName(state awarenesspb.GraphFreshnessState) string {
	return strings.ToLower(strings.TrimPrefix(state.String(), "GRAPH_FRESHNESS_STATE_"))
}

func buildProofPlanForFiles(root string, authorities []authoritySurfaceCandidate, proofDoc proofObligationsDoc, forbiddenFixes []proofPlanForbiddenMove, files []string) (proofPlanResult, error) {
	selectedAuthorities := map[string]authoritySurfaceCandidate{}
	selectedObligations := map[string]generatedProofObligation{}
	for _, file := range files {
		if strings.TrimSpace(file) == "" {
			continue
		}
		res, err := buildProofPlan(root, authorities, proofDoc, forbiddenFixes, strings.TrimSpace(file), "", "", "")
		if err != nil {
			return proofPlanResult{}, err
		}
		for _, authority := range res.Authority {
			selectedAuthorities[authority.ID] = authority
		}
		for _, obligation := range res.Obligations {
			selectedObligations[obligation.ID] = obligation
		}
	}
	authorityList := sortAuthorityCandidates(selectedAuthorities)
	obligationList := sortProofObligations(selectedObligations)
	return proofPlanResult{
		Subject:        strings.Join(files, ", "),
		Authority:      authorityList,
		Obligations:    obligationList,
		ForbiddenMoves: matchForbiddenMoves(authorityList, forbiddenFixes),
	}, nil
}

func buildRepairPlanResult(task string, files []string, resp *awarenesspb.PreflightResponse, proof proofPlanResult) repairPlanResult {
	return repairPlanResult{
		Task:            task,
		Files:           append([]string(nil), files...),
		Status:          strings.ToLower(strings.TrimPrefix(resp.GetStatus().String(), "PREFLIGHT_STATUS_")),
		RiskClass:       strings.ToLower(strings.TrimPrefix(resp.GetRiskClass().String(), "RISK_CLASS_")),
		Confidence:      strings.ToLower(strings.TrimPrefix(resp.GetConfidence().String(), "CONFIDENCE_")),
		Authority:       resp.GetAuthority(),
		FilesToRead:     append([]string(nil), resp.GetFilesToRead()...),
		RequiredActions: append([]string(nil), resp.GetRequiredActions()...),
		TestsToRun:      append([]string(nil), resp.GetTestsToRun()...),
		BlindSpots:      append([]string(nil), resp.GetBlindSpots()...),
		OrderedSteps:    buildRepairPlanSteps(resp, proof),
		Proof:           proof,
	}
}

func buildRepairPlanSteps(resp *awarenesspb.PreflightResponse, proof proofPlanResult) []string {
	var steps []string
	for _, file := range uniqueSortedStrings(resp.GetFilesToRead()) {
		steps = append(steps, "read governed context: "+file)
	}
	for _, action := range uniqueSortedStrings(resp.GetRequiredActions()) {
		steps = append(steps, "satisfy required action: "+action)
	}
	for _, authority := range proof.Authority {
		steps = append(steps, "preserve authority surface: "+authority.ID)
	}
	for _, obligation := range proof.Obligations {
		steps = append(steps, "prove obligation: "+obligation.ID)
	}
	for _, move := range proof.ForbiddenMoves {
		steps = append(steps, "avoid forbidden move: "+move.ID)
	}
	for _, test := range uniqueSortedStrings(resp.GetTestsToRun()) {
		steps = append(steps, "run test: "+test)
	}
	for _, spot := range uniqueSortedStrings(resp.GetBlindSpots()) {
		steps = append(steps, "cover blind spot: "+spot)
	}
	return steps
}

func uniqueSortedStrings(items []string) []string {
	seen := make(map[string]bool, len(items))
	var out []string
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" || seen[trimmed] {
			continue
		}
		seen[trimmed] = true
		out = append(out, trimmed)
	}
	sort.Strings(out)
	return out
}

func renderRepairPlanText(res repairPlanResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Repair plan\n")
	if res.Task != "" {
		fmt.Fprintf(&b, "task: %s\n", res.Task)
	}
	if len(res.Files) > 0 {
		fmt.Fprintf(&b, "files: %s\n", strings.Join(res.Files, ", "))
	}
	fmt.Fprintf(&b, "status: %s   risk: %s   confidence: %s\n", res.Status, res.RiskClass, res.Confidence)
	if res.Authority != nil {
		state := "non-authoritative"
		if res.Authority.GetAuthoritative() {
			state = "authoritative"
		}
		fmt.Fprintf(&b, "authority: %s (%s)\n", state, authorityStateName(res.Authority.GetGraphFreshnessState()))
		if digest := res.Authority.GetLiveStoreGraphDigestSha256(); digest != "" {
			fmt.Fprintf(&b, "live_digest: %s\n", digest)
		}
		if triples := res.Authority.GetLiveStoreGraphTripleCount(); triples > 0 {
			fmt.Fprintf(&b, "live_triples: %d\n", triples)
		}
	}
	appendSection := func(title string, items []string) {
		if len(items) == 0 {
			return
		}
		fmt.Fprintf(&b, "\n%s:\n", title)
		for _, item := range items {
			fmt.Fprintf(&b, "  - %s\n", item)
		}
	}
	appendSection("Ordered steps", res.OrderedSteps)
	appendSection("Required actions", res.RequiredActions)
	appendSection("Files to read", res.FilesToRead)
	if len(res.Proof.Authority) > 0 {
		fmt.Fprintf(&b, "\nAuthority surfaces:\n")
		for _, authority := range res.Proof.Authority {
			fmt.Fprintf(&b, "  - %s [%s]\n", authority.ID, authority.Kind)
		}
	}
	if len(res.Proof.Obligations) > 0 {
		fmt.Fprintf(&b, "\nProof obligations:\n")
		for _, obligation := range res.Proof.Obligations {
			var slots []string
			for _, slot := range obligation.RequiredSlots {
				if slot.Required {
					slots = append(slots, slot.Kind)
				}
			}
			fmt.Fprintf(&b, "  - %s", obligation.ID)
			if len(slots) > 0 {
				fmt.Fprintf(&b, " (%s)", strings.Join(slots, ", "))
			}
			fmt.Fprintf(&b, "\n")
		}
	}
	appendSection("Forbidden moves", forbiddenMoveIDs(res.Proof.ForbiddenMoves))
	appendSection("Tests to run", res.TestsToRun)
	appendSection("Blind spots", res.BlindSpots)
	return b.String()
}

func forbiddenMoveIDs(moves []proofPlanForbiddenMove) []string {
	out := make([]string, 0, len(moves))
	for _, move := range moves {
		out = append(out, move.ID)
	}
	return out
}
