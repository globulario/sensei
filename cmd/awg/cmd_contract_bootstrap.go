// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	awarenesspb "github.com/globulario/sensei/golang/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gopkg.in/yaml.v3"
)

type bootstrapTask struct {
	InstanceID string   `json:"instance_id"`
	Domain     string   `json:"domain"`
	Issue      string   `json:"issue"`
	F2PTests   []string `json:"f2p_tests"`
}

type bootstrapEvidence struct {
	File string `json:"file"`
	Line int    `json:"line"`
	Text string `json:"text"`
}

type bootstrapAWGFile struct {
	File          string   `json:"file"`
	Architecture  []string `json:"architecture,omitempty"`
	Invariants    []string `json:"invariants,omitempty"`
	FailureModes  []string `json:"failure_modes,omitempty"`
	Intents       []string `json:"intents,omitempty"`
	RequiredTests []string `json:"required_tests,omitempty"`
}

type bootstrapProofProvenance struct {
	Source     string `json:"source"`
	Confidence string `json:"confidence"`
	Evidence   string `json:"evidence"`
}

type bootstrapContractScaffold struct {
	ContractSetVersion int                         `json:"contract_set_version" yaml:"contract_set_version"`
	TaskID             string                      `json:"task_id,omitempty" yaml:"task_id,omitempty"`
	Repo               string                      `json:"repo,omitempty" yaml:"repo,omitempty"`
	Contracts          []bootstrapScaffoldContract `json:"contracts" yaml:"contracts"`
}

type bootstrapScaffoldContract struct {
	ID                  string                     `json:"id" yaml:"id"`
	Kind                string                     `json:"kind,omitempty" yaml:"kind,omitempty"`
	Confidence          string                     `json:"confidence,omitempty" yaml:"confidence,omitempty"`
	Statement           string                     `json:"statement,omitempty" yaml:"statement,omitempty"`
	RequiredScope       bootstrapScaffoldScope     `json:"required_scope,omitempty" yaml:"required_scope,omitempty"`
	AllowedRelatedScope bootstrapScaffoldScope     `json:"allowed_related_scope,omitempty" yaml:"allowed_related_scope,omitempty"`
	Governs             bootstrapScaffoldGoverns   `json:"governs,omitempty" yaml:"governs,omitempty"`
	AWGAnchors          []string                   `json:"awg_anchors,omitempty" yaml:"awg_anchors,omitempty"`
	Invariants          []string                   `json:"invariants,omitempty" yaml:"invariants,omitempty"`
	FailureModes        []string                   `json:"failure_modes,omitempty" yaml:"failure_modes,omitempty"`
	Intents             []string                   `json:"intents,omitempty" yaml:"intents,omitempty"`
	RequiredTests       []string                   `json:"required_tests,omitempty" yaml:"required_tests,omitempty"`
	Components          []string                   `json:"components,omitempty" yaml:"components,omitempty"`
	ProofRequired       bool                       `json:"proof_required,omitempty" yaml:"proof_required,omitempty"`
	RequiredTestPaths   []string                   `json:"required_test_paths,omitempty" yaml:"required_test_paths,omitempty"`
	RequiredTestSymbols []string                   `json:"required_test_symbols,omitempty" yaml:"required_test_symbols,omitempty"`
	PromotionRequired   bool                       `json:"promotion_required,omitempty" yaml:"promotion_required,omitempty"`
	ProofProvenance     []bootstrapProofProvenance `json:"proof_provenance,omitempty" yaml:"proof_provenance,omitempty"`
}

type bootstrapScaffoldScope struct {
	Files []string `json:"files,omitempty" yaml:"files,omitempty"`
}

type bootstrapScaffoldGoverns struct {
	Files         []string `json:"files,omitempty" yaml:"files,omitempty"`
	Symbols       []string `json:"symbols,omitempty" yaml:"symbols,omitempty"`
	Invariants    []string `json:"invariants,omitempty" yaml:"invariants,omitempty"`
	FailureModes  []string `json:"failure_modes,omitempty" yaml:"failure_modes,omitempty"`
	Intents       []string `json:"intents,omitempty" yaml:"intents,omitempty"`
	RequiredTests []string `json:"required_tests,omitempty" yaml:"required_tests,omitempty"`
	Components    []string `json:"components,omitempty" yaml:"components,omitempty"`
}

type bootstrapResult struct {
	AWGStatus                   string                     `json:"awg_status"`
	ContractStatus              string                     `json:"contract_status"`
	ProofStatus                 string                     `json:"proof_status"`
	TaskID                      string                     `json:"task_id,omitempty"`
	Domain                      string                     `json:"domain,omitempty"`
	IssueSource                 string                     `json:"issue_source,omitempty"`
	LikelyImplementationFiles   []string                   `json:"likely_implementation_files"`
	LikelyProvingTests          []string                   `json:"likely_proving_tests"`
	CandidateFiles              []string                   `json:"candidate_files"`
	MechanicalEvidence          []bootstrapEvidence        `json:"mechanical_evidence,omitempty"`
	AWGFiles                    []bootstrapAWGFile         `json:"awg_files,omitempty"`
	AWGAnchors                  []string                   `json:"awg_anchors,omitempty"`
	RequiredActions             []string                   `json:"required_actions,omitempty"`
	ForbiddenFixes              []string                   `json:"forbidden_fixes,omitempty"`
	TestsToRun                  []string                   `json:"tests_to_run,omitempty"`
	FilesToRead                 []string                   `json:"files_to_read,omitempty"`
	BlindSpots                  []string                   `json:"blind_spots,omitempty"`
	ProofRequiredProposed       bool                       `json:"proof_required_proposed"`
	RequiredTestPathsProposed   []string                   `json:"required_test_paths_proposed,omitempty"`
	RequiredTestSymbolsProposed []string                   `json:"required_test_symbols_proposed,omitempty"`
	ProofProvenance             []bootstrapProofProvenance `json:"proof_provenance,omitempty"`
	PromotionRequired           bool                       `json:"promotion_required"`
	ContractScaffold            bootstrapContractScaffold  `json:"contract_scaffold"`
}

type bootstrapAWGClient interface {
	Preflight(context.Context, *awarenesspb.PreflightRequest, ...grpc.CallOption) (*awarenesspb.PreflightResponse, error)
	Impact(context.Context, *awarenesspb.ImpactRequest, ...grpc.CallOption) (*awarenesspb.ImpactResponse, error)
}

var contractBootstrapConnectAWG = connectAWG

func runContractBootstrap(args []string) int {
	fs := flag.NewFlagSet("awg contract-bootstrap", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	repoRoot := fs.String("repo-root", ".", "repository root to analyze")
	taskFile := fs.String("task-file", "", "task JSON containing issue/domain/f2p_tests")
	issue := fs.String("issue", "", "issue text when not using --task-file")
	domain := fs.String("domain", "", "optional repo/domain scope for AWG cross-reference")
	addr := fs.String("addr", "localhost:10120", "AWG gRPC server address")
	format := fs.String("format", "text", "output format: text | json | prompt | scaffold")
	asJSON := fs.Bool("json", false, "output as JSON (deprecated: same as --format json)")
	var tests stringSlice
	fs.Var(&tests, "f2p-test", "fail-to-pass test name (repeatable)")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: awg contract-bootstrap [flags]

Build a proposed repair-contract bootstrap from issue text, fail-to-pass tests,
repo surfaces, and optional AWG cross-references.

This command does NOT mutate the graph, write contracts, or claim authority. It
produces a proposed contract scaffold to verify before patching.

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

	task, source, err := loadBootstrapTask(*taskFile, *issue, tests)
	if err != nil {
		fmt.Fprintf(os.Stderr, "awg contract-bootstrap: %v\n", err)
		return 2
	}
	root, err := filepath.Abs(*repoRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "awg contract-bootstrap: resolve repo root: %v\n", err)
		return 1
	}
	if strings.TrimSpace(*domain) == "" {
		*domain = strings.TrimSpace(task.Domain)
	}

	result, err := buildContractBootstrap(root, *addr, *domain, task, source)
	if err != nil {
		fmt.Fprintf(os.Stderr, "awg contract-bootstrap: %v\n", err)
		return 1
	}

	switch *format {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(result)
	case "scaffold":
		data, err := yaml.Marshal(result.ContractScaffold)
		if err != nil {
			fmt.Fprintf(os.Stderr, "awg contract-bootstrap: render scaffold: %v\n", err)
			return 1
		}
		fmt.Print(string(data))
	case "prompt":
		fmt.Print(renderContractBootstrapPrompt(result))
	default:
		fmt.Print(renderContractBootstrapText(result))
	}
	return 0
}

func loadBootstrapTask(taskFile, issue string, tests []string) (bootstrapTask, string, error) {
	if strings.TrimSpace(taskFile) != "" {
		data, err := os.ReadFile(taskFile)
		if err != nil {
			return bootstrapTask{}, "", err
		}
		var task bootstrapTask
		if err := json.Unmarshal(data, &task); err != nil {
			return bootstrapTask{}, "", fmt.Errorf("parse task file: %w", err)
		}
		return task, taskFile, nil
	}
	if strings.TrimSpace(issue) == "" {
		return bootstrapTask{}, "", fmt.Errorf("provide --task-file or --issue")
	}
	task := bootstrapTask{
		Issue:    issue,
		F2PTests: tests,
	}
	return task, "flags", nil
}

func buildContractBootstrap(repoRoot, addr, domain string, task bootstrapTask, source string) (bootstrapResult, error) {
	testFiles, evidence, err := collectTestAnchors(repoRoot, task.F2PTests)
	if err != nil {
		return bootstrapResult{}, err
	}
	implScores, implFiles, err := implementationCandidatesFromTests(repoRoot, testFiles)
	if err != nil {
		return bootstrapResult{}, err
	}
	candidates, err := candidateFiles(repoRoot, task.Issue, implScores)
	if err != nil {
		return bootstrapResult{}, err
	}
	likelyImpl := rankImplementationFiles(candidates, implFiles)
	res := bootstrapResult{
		AWGStatus:                   "AWG-unavailable",
		ContractStatus:              "proposed",
		ProofStatus:                 "proposed",
		TaskID:                      task.InstanceID,
		Domain:                      domain,
		IssueSource:                 source,
		LikelyImplementationFiles:   likelyImpl,
		LikelyProvingTests:          testFiles,
		CandidateFiles:              candidates,
		MechanicalEvidence:          evidence,
		RequiredTestPathsProposed:   dedupeStrings(append([]string{}, testFiles...)),
		RequiredTestSymbolsProposed: proposedTestSymbols(task.F2PTests),
		PromotionRequired:           true,
	}
	res.ProofProvenance = proposedProofProvenance(task.F2PTests, res.RequiredTestPathsProposed)
	res.ProofRequiredProposed = len(res.RequiredTestPathsProposed) > 0 || len(res.RequiredTestSymbolsProposed) > 0
	if strings.TrimSpace(addr) == "" {
		res.ContractScaffold = buildBootstrapContractScaffold(task, res)
		return res, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := contractBootstrapConnectAWG(addr)
	if err != nil {
		res.AWGStatus = "AWG-down"
		res.BlindSpots = dedupeStrings(append(res.BlindSpots, bootstrapAWGDownMessage(err)))
		return res, nil
	}
	defer c.Close()
	res.AWGStatus = "AWG-check-error"
	if err := enrichBootstrapWithAWG(ctx, c.Stub(), task, domain, likelyImpl, candidates, &res); err != nil {
		res.BlindSpots = dedupeStrings(append(res.BlindSpots, err.Error()))
	}
	res.ContractScaffold = buildBootstrapContractScaffold(task, res)
	return res, nil
}

func enrichBootstrapWithAWG(ctx context.Context, client bootstrapAWGClient, task bootstrapTask, domain string, likelyImpl, candidates []string, res *bootstrapResult) error {
	pf, err := client.Preflight(ctx, &awarenesspb.PreflightRequest{
		Task:   task.Issue,
		Files:  dedupeStrings(append(append([]string{}, likelyImpl...), candidates...)),
		Mode:   awarenesspb.PreflightMode_PREFLIGHT_COMPACT,
		Domain: domain,
	})
	if err != nil {
		res.AWGStatus = bootstrapAWGErrorStatus(err)
		return fmt.Errorf("contract-bootstrap requires current graph authority: preflight check failed: %s", bootstrapAWGErrorDetail(err))
	}
	if err := requireAuthoritativeGraph(pf.GetAuthority(), "contract-bootstrap preflight"); err != nil {
		res.AWGStatus = "AWG-non-authoritative"
		return err
	}
	res.AWGStatus = "AWG-authoritative"
	res.RequiredActions = dedupeStrings(pf.GetRequiredActions())
	res.ForbiddenFixes = dedupeStrings(pf.GetForbiddenFixes())
	res.TestsToRun = dedupeStrings(pf.GetTestsToRun())
	res.FilesToRead = dedupeStrings(pf.GetFilesToRead())
	res.BlindSpots = dedupeStrings(pf.GetBlindSpots())
	for _, file := range candidates {
		resp, err := client.Impact(ctx, &awarenesspb.ImpactRequest{File: file, Domain: domain})
		if err != nil {
			res.AWGStatus = bootstrapAWGErrorStatus(err)
			return fmt.Errorf("contract-bootstrap requires current graph authority: impact check failed for %s: %s", file, bootstrapAWGErrorDetail(err))
		}
		if err := requireAuthoritativeGraph(resp.GetAuthority(), "contract-bootstrap impact"); err != nil {
			res.AWGStatus = "AWG-non-authoritative"
			return fmt.Errorf("%w (file: %s)", err, file)
		}
		entry := bootstrapAWGFile{File: file}
		entry.Architecture = collectNodeIDs(resp.GetDirectArchitecture())
		entry.Invariants = collectNodeIDs(resp.GetDirectInvariants())
		entry.FailureModes = collectNodeIDs(resp.GetDirectFailureModes())
		entry.Intents = collectNodeIDs(resp.GetDirectIntents())
		entry.RequiredTests = collectNodeIDs(resp.GetRequiredTests())
		if len(entry.Architecture)+len(entry.Invariants)+len(entry.FailureModes)+len(entry.Intents)+len(entry.RequiredTests) == 0 {
			continue
		}
		res.AWGFiles = append(res.AWGFiles, entry)
		res.AWGAnchors = append(res.AWGAnchors, prefixedIDs("component", entry.Architecture)...)
		res.AWGAnchors = append(res.AWGAnchors, prefixedIDs("invariant", entry.Invariants)...)
		res.AWGAnchors = append(res.AWGAnchors, prefixedIDs("failure_mode", entry.FailureModes)...)
		res.AWGAnchors = append(res.AWGAnchors, prefixedIDs("intent", entry.Intents)...)
		res.AWGAnchors = append(res.AWGAnchors, prefixedIDs("test", entry.RequiredTests)...)
	}
	res.AWGAnchors = dedupeStrings(res.AWGAnchors)
	return nil
}

func bootstrapAWGErrorStatus(err error) string {
	if bootstrapAWGBackendDown(err) {
		return "AWG-down"
	}
	return "AWG-check-error"
}

func bootstrapAWGErrorDetail(err error) string {
	if bootstrapAWGBackendDown(err) {
		return bootstrapAWGDownMessage(err)
	}
	return err.Error()
}

func bootstrapAWGDownMessage(err error) string {
	return fmt.Sprintf("awareness-graph backend unreachable; AWG cross-reference was not obtained and this is not a no-guidance result: %v", err)
}

func bootstrapAWGBackendDown(err error) bool {
	if err == nil {
		return false
	}
	if st, ok := status.FromError(err); ok {
		switch st.Code() {
		case codes.Unavailable, codes.DeadlineExceeded:
			return true
		}
	}
	detail := strings.ToLower(err.Error())
	return strings.Contains(detail, "connection refused") ||
		strings.Contains(detail, "deadline exceeded") ||
		strings.Contains(detail, "backend unreachable")
}

func collectNodeIDs(nodes []*awarenesspb.KnowledgeNode) []string {
	out := make([]string, 0, len(nodes))
	for _, n := range nodes {
		if n == nil || strings.TrimSpace(n.GetId()) == "" {
			continue
		}
		out = append(out, n.GetId())
	}
	return dedupeStrings(out)
}

func prefixedIDs(prefix string, ids []string) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, prefix+":"+id)
	}
	return out
}

func collectTestAnchors(repoRoot string, tests []string) ([]string, []bootstrapEvidence, error) {
	type scored struct {
		file string
		line int
		text string
	}
	byFile := map[string]int{}
	var evidence []bootstrapEvidence
	err := filepath.WalkDir(repoRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, _ := filepath.Rel(repoRoot, path)
		lines, ferr := os.ReadFile(path)
		if ferr != nil {
			return nil
		}
		content := string(lines)
		for _, test := range tests {
			base := baseTestName(test)
			if base == "" {
				continue
			}
			re := regexp.MustCompile(`func\s+` + regexp.QuoteMeta(base) + `\b`)
			loc := re.FindStringIndex(content)
			if loc == nil {
				continue
			}
			line := 1 + strings.Count(content[:loc[0]], "\n")
			text := readLineAt(content, line)
			byFile[filepath.ToSlash(rel)]++
			evidence = append(evidence, bootstrapEvidence{
				File: filepath.ToSlash(rel),
				Line: line,
				Text: strings.TrimSpace(text),
			})
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	files := make([]string, 0, len(byFile))
	for file := range byFile {
		files = append(files, file)
	}
	sort.Slice(files, func(i, j int) bool {
		if byFile[files[i]] != byFile[files[j]] {
			return byFile[files[i]] > byFile[files[j]]
		}
		return files[i] < files[j]
	})
	return files, dedupeEvidence(evidence), nil
}

func implementationCandidatesFromTests(repoRoot string, testFiles []string) (map[string]int, []string, error) {
	scores := map[string]int{}
	var impls []string
	for _, testFile := range testFiles {
		dir := filepath.Dir(testFile)
		base := strings.TrimSuffix(filepath.Base(testFile), "_test.go")
		sibling := filepath.ToSlash(filepath.Join(dir, base+".go"))
		if fileExists(filepath.Join(repoRoot, sibling)) {
			scores[sibling] += 3
			impls = append(impls, sibling)
			continue
		}
		entries, err := os.ReadDir(filepath.Join(repoRoot, dir))
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			rel := filepath.ToSlash(filepath.Join(dir, name))
			scores[rel]++
			impls = append(impls, rel)
		}
	}
	return scores, dedupeStrings(impls), nil
}

func candidateFiles(repoRoot, issue string, implScores map[string]int) ([]string, error) {
	phrases := issuePhrases(issue)
	issueTokens := issuePathTokens(issue)
	scores := map[string]int{}
	for file, score := range implScores {
		scores[file] += score
	}
	err := filepath.WalkDir(repoRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, _ := filepath.Rel(repoRoot, path)
		rel = filepath.ToSlash(rel)
		data, ferr := os.ReadFile(path)
		if ferr != nil {
			return nil
		}
		content := strings.ToLower(string(data))
		pathScore := tokenPathScore(rel, issueTokens)
		textScore := 0
		for _, phrase := range phrases {
			if strings.Contains(content, phrase) {
				textScore += 4
				continue
			}
			words := strings.Fields(phrase)
			for n := min(8, len(words)); n >= 4; n-- {
				prefix := strings.Join(words[:n], " ")
				if len(prefix) < 12 {
					continue
				}
				if strings.Contains(content, prefix) {
					textScore += 2
					break
				}
			}
		}
		if pathScore == 0 && textScore == 0 && scores[rel] == 0 {
			return nil
		}
		scores[rel] += pathScore + textScore
		return nil
	})
	if err != nil {
		return nil, err
	}
	type scored struct {
		file  string
		score int
	}
	var ranked []scored
	for file, score := range scores {
		if score <= 0 {
			continue
		}
		ranked = append(ranked, scored{file: file, score: score})
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].score != ranked[j].score {
			return ranked[i].score > ranked[j].score
		}
		return ranked[i].file < ranked[j].file
	})
	out := make([]string, 0, min(10, len(ranked)))
	for i := 0; i < len(ranked) && i < 10; i++ {
		out = append(out, ranked[i].file)
	}
	return out, nil
}

func rankImplementationFiles(candidates, implFiles []string) []string {
	implSet := map[string]bool{}
	for _, file := range implFiles {
		implSet[file] = true
	}
	var out []string
	for _, file := range candidates {
		if implSet[file] {
			out = append(out, file)
		}
	}
	if len(out) > 0 {
		return out
	}
	return implFiles
}

func issuePhrases(issue string) []string {
	var phrases []string
	codeBlock := false
	sc := bufio.NewScanner(strings.NewReader(issue))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "```") {
			codeBlock = !codeBlock
			continue
		}
		if codeBlock {
			phrases = append(phrases, line)
		}
	}
	re := regexp.MustCompile("`[^`]+`|\"[^\"]+\"")
	for _, match := range re.FindAllString(issue, -1) {
		phrases = append(phrases, strings.Trim(match, "`\""))
	}
	var out []string
	for _, phrase := range phrases {
		phrase = strings.ToLower(strings.TrimSpace(phrase))
		phrase = regexp.MustCompile(`<[^>]+>`).ReplaceAllString(phrase, "")
		if len(phrase) < 12 || len(phrase) > 160 {
			continue
		}
		out = append(out, phrase)
	}
	return dedupeStrings(out)
}

func issuePathTokens(issue string) []string {
	re := regexp.MustCompile(`[A-Za-z][A-Za-z0-9_/-]{2,}`)
	raw := re.FindAllString(strings.ToLower(issue), -1)
	stop := map[string]bool{
		"when": true, "using": true, "select": true, "feature": true, "problem": true,
		"would": true, "solve": true, "proposed": true, "solution": true, "additional": true,
		"context": true, "with": true, "from": true, "this": true, "that": true, "have": true,
	}
	var out []string
	for _, tok := range raw {
		for _, piece := range strings.FieldsFunc(tok, func(r rune) bool { return r == '/' || r == '-' || r == '_' }) {
			if len(piece) < 3 || stop[piece] {
				continue
			}
			out = append(out, piece)
		}
	}
	return dedupeStrings(out)
}

func tokenPathScore(path string, tokens []string) int {
	path = strings.ToLower(path)
	score := 0
	for _, tok := range tokens {
		if strings.Contains(path, tok) {
			score++
		}
	}
	return score
}

func baseTestName(test string) string {
	if i := strings.IndexByte(test, '/'); i >= 0 {
		return test[:i]
	}
	return test
}

func readLineAt(content string, line int) string {
	if line <= 0 {
		return ""
	}
	sc := bufio.NewScanner(strings.NewReader(content))
	cur := 0
	for sc.Scan() {
		cur++
		if cur == line {
			return sc.Text()
		}
	}
	return ""
}

func dedupeStrings(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, item := range in {
		item = strings.TrimSpace(item)
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	return out
}

func dedupeEvidence(in []bootstrapEvidence) []bootstrapEvidence {
	seen := map[string]bool{}
	var out []bootstrapEvidence
	for _, item := range in {
		key := fmt.Sprintf("%s:%d", item.File, item.Line)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].Line < out[j].Line
	})
	return out
}

func renderContractBootstrapPrompt(res bootstrapResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## Proposed repair-contract bootstrap (tool-level)\n")
	fmt.Fprintf(&b, "This bootstrap is derived mechanically from issue text, fail-to-pass tests, repo surfaces, and optional AWG cross-reference.\n")
	fmt.Fprintf(&b, "Treat it as a proposed contract scaffold to verify, tighten, or reject.\n\n")
	fmt.Fprintf(&b, "AWG status: %s\n\n", res.AWGStatus)
	if note := bootstrapAWGStatusNote(res); note != "" {
		fmt.Fprintf(&b, "%s\n\n", note)
	}
	fmt.Fprintf(&b, "Contract status: %s\n", res.ContractStatus)
	fmt.Fprintf(&b, "Proof status: %s\n\n", res.ProofStatus)
	fmt.Fprintf(&b, "Likely implementation files:\n")
	for _, f := range res.LikelyImplementationFiles {
		fmt.Fprintf(&b, "  - %s\n", f)
	}
	fmt.Fprintf(&b, "\nLikely proving tests:\n")
	for _, f := range res.LikelyProvingTests {
		fmt.Fprintf(&b, "  - %s\n", f)
	}
	fmt.Fprintf(&b, "\nIssue-and-test candidate files:\n")
	for _, f := range res.CandidateFiles {
		fmt.Fprintf(&b, "  - %s\n", f)
	}
	fmt.Fprintf(&b, "\nMechanical evidence anchors:\n")
	for _, ev := range res.MechanicalEvidence {
		fmt.Fprintf(&b, "  - %s:%d:%s\n", ev.File, ev.Line, strings.TrimSpace(ev.Text))
	}
	if len(res.AWGAnchors) > 0 {
		fmt.Fprintf(&b, "\nAWG anchors consulted:\n")
		for _, id := range res.AWGAnchors {
			fmt.Fprintf(&b, "  - %s\n", id)
		}
	}
	if len(res.RequiredActions) > 0 {
		fmt.Fprintf(&b, "\nAWG required actions:\n")
		for _, item := range res.RequiredActions {
			fmt.Fprintf(&b, "  - %s\n", item)
		}
	}
	if len(res.ForbiddenFixes) > 0 {
		fmt.Fprintf(&b, "\nAWG forbidden fixes:\n")
		for _, item := range res.ForbiddenFixes {
			fmt.Fprintf(&b, "  - %s\n", item)
		}
	}
	if len(res.TestsToRun) > 0 {
		fmt.Fprintf(&b, "\nAWG tests to run:\n")
		for _, item := range res.TestsToRun {
			fmt.Fprintf(&b, "  - %s\n", item)
		}
	}
	if len(res.BlindSpots) > 0 {
		fmt.Fprintf(&b, "\nAWG blind spots:\n")
		for _, item := range res.BlindSpots {
			fmt.Fprintf(&b, "  - %s\n", item)
		}
	}
	if res.ProofRequiredProposed || len(res.ProofProvenance) > 0 {
		fmt.Fprintf(&b, "\nAdvisory proof obligations proposed by bootstrap:\n")
		fmt.Fprintf(&b, "  - proof_required_proposed: %t\n", res.ProofRequiredProposed)
		if len(res.RequiredTestPathsProposed) > 0 {
			fmt.Fprintf(&b, "  - required_test_paths_proposed:\n")
			for _, item := range res.RequiredTestPathsProposed {
				fmt.Fprintf(&b, "    - %s\n", item)
			}
		}
		if len(res.RequiredTestSymbolsProposed) > 0 {
			fmt.Fprintf(&b, "  - required_test_symbols_proposed:\n")
			for _, item := range res.RequiredTestSymbolsProposed {
				fmt.Fprintf(&b, "    - %s\n", item)
			}
		}
		if len(res.ProofProvenance) > 0 {
			fmt.Fprintf(&b, "  - proof_provenance:\n")
			for _, item := range res.ProofProvenance {
				fmt.Fprintf(&b, "    - source: %s (%s) — %s\n", item.Source, item.Confidence, item.Evidence)
			}
		}
		fmt.Fprintf(&b, "  - promotion_required: %t\n", res.PromotionRequired)
		fmt.Fprintf(&b, "  - advisory only: proposed proof does not affect contract_clean until frozen into authoritative contract fields\n")
	}
	fmt.Fprintf(&b, "\nExpected bootstrap discipline:\n")
	fmt.Fprintf(&b, "  - start required_scope from the likely implementation files unless contradicted by code\n")
	fmt.Fprintf(&b, "  - start allowed_related_scope from the proving tests and AWG-required tests, and only expand if directly required\n")
	fmt.Fprintf(&b, "  - if a candidate file carries AWG invariants/failure modes/intents, cite them in contract_block.json before patching\n")
	fmt.Fprintf(&b, "  - if the code forces a helper outside this bootstrap, record it as an allowed_related_scope_candidate rather than silently broadening\n")
	return b.String()
}

func renderContractBootstrapText(res bootstrapResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "AWG status: %s\n", res.AWGStatus)
	if note := bootstrapAWGStatusNote(res); note != "" {
		fmt.Fprintf(&b, "AWG note: %s\n", note)
	}
	if res.Domain != "" {
		fmt.Fprintf(&b, "Domain: %s\n", res.Domain)
	}
	if res.IssueSource != "" {
		fmt.Fprintf(&b, "Issue source: %s\n", res.IssueSource)
	}
	fmt.Fprintf(&b, "Contract status: %s\n", res.ContractStatus)
	fmt.Fprintf(&b, "Proof status: %s\n", res.ProofStatus)
	fmt.Fprintf(&b, "\nLikely implementation files:\n")
	for _, f := range res.LikelyImplementationFiles {
		fmt.Fprintf(&b, "  - %s\n", f)
	}
	fmt.Fprintf(&b, "\nLikely proving tests:\n")
	for _, f := range res.LikelyProvingTests {
		fmt.Fprintf(&b, "  - %s\n", f)
	}
	fmt.Fprintf(&b, "\nCandidate files:\n")
	for _, f := range res.CandidateFiles {
		fmt.Fprintf(&b, "  - %s\n", f)
	}
	fmt.Fprintf(&b, "\nMechanical evidence:\n")
	for _, ev := range res.MechanicalEvidence {
		fmt.Fprintf(&b, "  - %s:%d:%s\n", ev.File, ev.Line, strings.TrimSpace(ev.Text))
	}
	if len(res.AWGFiles) > 0 {
		fmt.Fprintf(&b, "\nAWG cross-reference by file:\n")
		for _, entry := range res.AWGFiles {
			fmt.Fprintf(&b, "  - %s\n", entry.File)
			if len(entry.Architecture) > 0 {
				fmt.Fprintf(&b, "    architecture: %s\n", strings.Join(entry.Architecture, ", "))
			}
			if len(entry.Invariants) > 0 {
				fmt.Fprintf(&b, "    invariants: %s\n", strings.Join(entry.Invariants, ", "))
			}
			if len(entry.FailureModes) > 0 {
				fmt.Fprintf(&b, "    failure_modes: %s\n", strings.Join(entry.FailureModes, ", "))
			}
			if len(entry.Intents) > 0 {
				fmt.Fprintf(&b, "    intents: %s\n", strings.Join(entry.Intents, ", "))
			}
			if len(entry.RequiredTests) > 0 {
				fmt.Fprintf(&b, "    required_tests: %s\n", strings.Join(entry.RequiredTests, ", "))
			}
		}
	}
	if len(res.RequiredActions) > 0 {
		fmt.Fprintf(&b, "\nAWG required actions:\n")
		for _, item := range res.RequiredActions {
			fmt.Fprintf(&b, "  - %s\n", item)
		}
	}
	if len(res.ForbiddenFixes) > 0 {
		fmt.Fprintf(&b, "\nAWG forbidden fixes:\n")
		for _, item := range res.ForbiddenFixes {
			fmt.Fprintf(&b, "  - %s\n", item)
		}
	}
	if len(res.TestsToRun) > 0 {
		fmt.Fprintf(&b, "\nAWG tests to run:\n")
		for _, item := range res.TestsToRun {
			fmt.Fprintf(&b, "  - %s\n", item)
		}
	}
	if len(res.FilesToRead) > 0 {
		fmt.Fprintf(&b, "\nAWG files to read:\n")
		for _, item := range res.FilesToRead {
			fmt.Fprintf(&b, "  - %s\n", item)
		}
	}
	if len(res.BlindSpots) > 0 {
		fmt.Fprintf(&b, "\nAWG blind spots:\n")
		for _, item := range res.BlindSpots {
			fmt.Fprintf(&b, "  - %s\n", item)
		}
	}
	if res.ProofRequiredProposed || len(res.ProofProvenance) > 0 {
		fmt.Fprintf(&b, "\nProposed proof obligations (advisory only):\n")
		fmt.Fprintf(&b, "  - proof_required_proposed: %t\n", res.ProofRequiredProposed)
		for _, item := range res.RequiredTestPathsProposed {
			fmt.Fprintf(&b, "  - required_test_paths_proposed: %s\n", item)
		}
		for _, item := range res.RequiredTestSymbolsProposed {
			fmt.Fprintf(&b, "  - required_test_symbols_proposed: %s\n", item)
		}
		for _, item := range res.ProofProvenance {
			fmt.Fprintf(&b, "  - proof_provenance: %s (%s): %s\n", item.Source, item.Confidence, item.Evidence)
		}
		fmt.Fprintf(&b, "  - promotion_required: %t\n", res.PromotionRequired)
	}
	return b.String()
}

func bootstrapAWGStatusNote(res bootstrapResult) string {
	switch res.AWGStatus {
	case "AWG-down":
		return "The awareness-graph backend was unreachable. AWG cross-reference was not obtained; this is not an empty or no-guidance AWG result."
	case "AWG-check-error":
		return "AWG cross-reference failed during verification. Treat any missing AWG guidance as unknown rather than clean absence."
	case "AWG-non-authoritative":
		return "AWG responded but could not prove current graph authority. Cross-reference guidance was intentionally withheld."
	default:
		return ""
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func proposedTestSymbols(tests []string) []string {
	out := make([]string, 0, len(tests))
	for _, test := range tests {
		if base := strings.TrimSpace(baseTestName(test)); base != "" {
			out = append(out, base)
		}
	}
	return dedupeStrings(out)
}

func proposedProofProvenance(f2pTests, testPaths []string) []bootstrapProofProvenance {
	var out []bootstrapProofProvenance
	if len(f2pTests) > 0 {
		out = append(out, bootstrapProofProvenance{
			Source:     "fail_to_pass_tests",
			Confidence: "high",
			Evidence:   fmt.Sprintf("task listed %d fail-to-pass test(s)", len(dedupeStrings(f2pTests))),
		})
	}
	if len(testPaths) > 0 {
		out = append(out, bootstrapProofProvenance{
			Source:     "test_file_matching",
			Confidence: "medium",
			Evidence:   fmt.Sprintf("derived %d adjacent proving test file(s) from fail-to-pass anchors", len(dedupeStrings(testPaths))),
		})
	}
	return out
}

func buildBootstrapContractScaffold(task bootstrapTask, res bootstrapResult) bootstrapContractScaffold {
	scaffold := bootstrapContractScaffold{
		ContractSetVersion: 1,
		TaskID:             strings.TrimSpace(task.InstanceID),
		Repo:               strings.TrimSpace(task.Domain),
		Contracts: []bootstrapScaffoldContract{{
			ID:                  proposedContractID(task),
			Kind:                "invariant",
			Confidence:          "proposed",
			Statement:           "TODO: replace with explicit frozen contract statement grounded in issue text and code evidence.",
			RequiredScope:       bootstrapScaffoldScope{Files: dedupeStrings(append([]string{}, res.LikelyImplementationFiles...))},
			AllowedRelatedScope: bootstrapScaffoldScope{Files: dedupeStrings(append([]string{}, res.LikelyProvingTests...))},
			Governs: bootstrapScaffoldGoverns{
				Files:         dedupeStrings(append([]string{}, res.LikelyImplementationFiles...)),
				Symbols:       nil,
				Invariants:    anchorIDsByClass(res.AWGAnchors, "invariant", "meta_principle"),
				FailureModes:  anchorIDsByClass(res.AWGAnchors, "failure_mode"),
				Intents:       anchorIDsByClass(res.AWGAnchors, "intent"),
				RequiredTests: anchorIDsByClass(res.AWGAnchors, "test", "required_test"),
				Components:    anchorIDsByClass(res.AWGAnchors, "component"),
			},
			AWGAnchors:          dedupeStrings(append([]string{}, res.AWGAnchors...)),
			Invariants:          anchorIDsByClass(res.AWGAnchors, "invariant", "meta_principle"),
			FailureModes:        anchorIDsByClass(res.AWGAnchors, "failure_mode"),
			Intents:             anchorIDsByClass(res.AWGAnchors, "intent"),
			RequiredTests:       anchorIDsByClass(res.AWGAnchors, "test", "required_test"),
			Components:          anchorIDsByClass(res.AWGAnchors, "component"),
			ProofRequired:       res.ProofRequiredProposed,
			RequiredTestPaths:   dedupeStrings(append([]string{}, res.RequiredTestPathsProposed...)),
			RequiredTestSymbols: dedupeStrings(append([]string{}, res.RequiredTestSymbolsProposed...)),
			PromotionRequired:   res.PromotionRequired,
			ProofProvenance:     append([]bootstrapProofProvenance{}, res.ProofProvenance...),
		}},
	}
	if scaffold.TaskID == "" {
		scaffold.TaskID = "manual"
	}
	if scaffold.Repo == "" {
		scaffold.Repo = strings.TrimSpace(res.Domain)
	}
	return scaffold
}

func proposedContractID(task bootstrapTask) string {
	base := strings.TrimSpace(task.InstanceID)
	if base == "" {
		base = strings.TrimSpace(task.Issue)
	}
	base = strings.ToLower(base)
	var b strings.Builder
	lastDot := false
	for _, r := range base {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDot = false
		default:
			if !lastDot {
				b.WriteByte('.')
				lastDot = true
			}
		}
	}
	slug := strings.Trim(b.String(), ".")
	if slug == "" {
		slug = "todo"
	}
	return "contract.bootstrap." + slug
}

func anchorIDsByClass(refs []string, classes ...string) []string {
	if len(refs) == 0 {
		return nil
	}
	allowed := map[string]bool{}
	for _, cls := range classes {
		allowed[cls] = true
	}
	var out []string
	for _, ref := range refs {
		colon := strings.IndexByte(ref, ':')
		if colon < 0 {
			continue
		}
		cls := strings.TrimSpace(ref[:colon])
		if !allowed[cls] {
			continue
		}
		id := strings.TrimSpace(ref[colon+1:])
		if id == "" {
			continue
		}
		out = append(out, id)
	}
	return dedupeStrings(out)
}
