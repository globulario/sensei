// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/globulario/sensei/golang/contractassess"
	"github.com/globulario/sensei/golang/extractor"
	"github.com/globulario/sensei/golang/rdf"
	"gopkg.in/yaml.v3"
)

func runAudit(args []string) int {
	fs := flag.NewFlagSet("sensei audit", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	verbose := fs.Bool("verbose", false, "show per-finding details")
	ciMode := fs.Bool("check", false, "exit 1 on any FAIL (CI mode)")
	warnStale := fs.Bool("warn-stale", false, "downgrade embeddata-freshness FAIL to WARN (PR-advisory, GC-2): a stale seed does not block; the seed-rebuild workflow auto-reconciles on merge. All other checks (orphans, validity, coverage) stay hard")
	fix := fs.Bool("fix", false, "auto-repair mechanical issues")
	domain := fs.String("domain", "", "repo/domain scope for corpus checks (e.g. github.com/caddyserver/caddy); includes shared knowledge")
	svcRepoFlag := fs.String("services-repo", "", "path to services repo (auto-detect)")
	agRepoFlag := fs.String("ag-repo", "", "path to awareness-graph repo (auto-detect)")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: sensei audit [flags]

Self-audit the awareness graph for drift, gaps, and inconsistencies.

Checks:
  embeddata-freshness    Is committed awareness.nt current with YAML sources?
  yaml-validity          Do all YAML files parse and import cleanly?
  ntriples-validity      Is the generated N-Triples output well-formed?
  coverage-gaps          High-risk files with zero awareness anchors?
  stale-file-refs        Entries referencing files that no longer exist?
  test-coverage          Critical/high invariants missing required_tests?
  meta-principle-coverage  Are all meta.* principles classified by enforcement tier?
  contract-assessment    Report-only contract gate outcomes from explicit intent evidence
  seed-orphans           Committed-seed nodes whose authoring YAML no longer exists?

Use --warn-stale on PRs that add services-side awareness: it downgrades
embeddata-freshness to an advisory WARN (the seed-rebuild workflow reconciles the
seed on merge) while keeping every correctness check a hard FAIL.

Flags:
`)
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return 2
	}
	auditDomain := strings.TrimSpace(*domain)
	if err := rejectPathLikeBuildDomain("audit --domain", auditDomain); err != nil {
		fmt.Fprintf(os.Stderr, "sensei audit: %v\n", err)
		return 2
	}

	svcRepo, _ := resolveServicesRepo(*svcRepoFlag)
	agRepo, _ := resolveAGRepo(*agRepoFlag, svcRepo)
	domainRepo := auditRepoForDomain(auditDomain, svcRepo, agRepo)

	if svcRepo == "" && agRepo == "" {
		fmt.Fprintln(os.Stderr, "sensei audit: cannot find repos; use --services-repo / --ag-repo")
		return 1
	}

	fmt.Println("Awareness graph self-audit")
	if auditDomain != "" {
		fmt.Printf("Domain scope: %s (+ shared)\n", auditDomain)
	}
	fmt.Println()

	inputDirs, intentDir, err := collectInputDirs(svcRepo, agRepo)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei audit: %v\n", err)
		return 1
	}
	validityInputDirs, validityIntentDir := inputDirs, intentDir
	if auditDomain != "" && domainRepo != "" {
		validityInputDirs, validityIntentDir = auditInputsForRepo(inputDirs, intentDir, domainRepo)
	}

	var checks []auditResult

	fmt.Println("  generating N-Triples...")
	seedInputDirs, seedIntentDir := auditSeedGenerationInputs(inputDirs, intentDir, svcRepo, agRepo)
	tagByRepo := auditDomain != ""
	ntBytes, totalTriples, yamlCount, genErr := generateNT(seedInputDirs, seedIntentDir, svcRepo, agRepo, tagByRepo)
	checkNTBytes := ntBytes
	checkTriples := totalTriples
	var scoped domainFilterResult
	if genErr == nil && auditDomain != "" {
		scoped = filterNTriplesToDomainResult(ntBytes, auditDomain)
		checkNTBytes, checkTriples = scoped.bytes, scoped.triples
	}

	seedPath := ""
	if agRepo != "" {
		seedPath = filepath.Join(agRepo, "golang", "server", "embeddata", "awareness.nt")
	}

	// 1. Domain scope sanity
	if genErr == nil && auditDomain != "" {
		checks = append(checks, checkAuditDomainScope(auditDomain, domainRepo, scoped))
	}

	// 2. Embeddata freshness
	if auditDomain != "" {
		// Freshness compares the whole committed seed artifact. A domain-scoped
		// audit intentionally evaluates a graph slice, so this whole-artifact gate
		// belongs to the unscoped audit.
	} else if genErr != nil {
		checks = append(checks, auditResult{name: "embeddata-freshness", level: auditFAIL, summary: genErr.Error()})
	} else if seedPath != "" {
		// agOnly = seed regenerated from the awareness-graph-owned corpus alone,
		// used to classify which differing triples this repo owns vs. which are
		// paired-services context (see seedfreshness.go / classifySeedDiff).
		agOnly := generateAgOnlyNT(agRepo)
		c := checkEmbeddataFreshness(ntBytes, seedPath, agOnly)
		if c.level == auditFAIL && *fix {
			c.fixFn = func() error {
				updated, err := updateSeedFile(ntBytes, seedPath)
				if err != nil {
					return err
				}
				if updated {
					fmt.Printf("    embeddata updated: %s\n", seedPath)
				}
				_ = reloadOxigraphStore(ntBytes, defaultOxigraphStoreURL())
				return nil
			}
			c.fixDesc = "rebuild embeddata + reload Oxigraph"
		}
		checks = append(checks, c)
	}

	// 3. YAML validity
	if genErr != nil {
		checks = append(checks, auditResult{name: "yaml-validity", level: auditFAIL, summary: genErr.Error()})
	} else {
		checks = append(checks, checkYAMLValidity(validityInputDirs, validityIntentDir, svcRepo, agRepo, yamlCount))
	}

	// 4. N-Triples validity
	if genErr != nil {
		checks = append(checks, auditResult{name: "ntriples-validity", level: auditFAIL, summary: genErr.Error()})
	} else {
		checks = append(checks, checkNTValidity(checkNTBytes, checkTriples))
	}

	// 5. Coverage gaps
	coverageRepo := svcRepo
	if auditDomain != "" {
		coverageRepo = domainRepo
	}
	if genErr == nil && coverageRepo != "" {
		checks = append(checks, checkCoverageGaps(coverageRepo, checkNTBytes))
	} else if genErr == nil && auditDomain != "" {
		checks = append(checks, auditResult{name: "coverage-gaps", level: auditWARN, summary: "domain repo root unavailable for high_risk_files.yaml"})
	}

	// 6. Stale file refs
	if genErr == nil {
		checks = append(checks, checkStaleFileRefs(svcRepo, agRepo, checkNTBytes))
	}

	// 7. Test coverage
	testRepo := svcRepo
	if auditDomain != "" {
		testRepo = domainRepo
	}
	if testRepo != "" {
		checks = append(checks, checkTestCoverage(testRepo))
	} else if auditDomain != "" {
		checks = append(checks, auditResult{name: "test-coverage", level: auditWARN, summary: "domain repo root unavailable for invariants.yaml"})
	}

	// 8. Meta-principle coverage completeness
	if agRepo != "" && (auditDomain == "" || domainRepo == agRepo) {
		checks = append(checks, checkMetaPrincipleCoverage(svcRepo, agRepo))
	}

	// 9. Contract assessment (report-only)
	localIntentDir, pairedIntentDir := selectAuditIntentDirs(agRepo, svcRepo)
	if auditDomain != "" {
		localIntentDir, pairedIntentDir = "", ""
		if domainRepo != "" {
			intent := filepath.Join(domainRepo, "docs", "intent")
			if _, err := os.Stat(intent); err == nil {
				localIntentDir = intent
			}
		}
	}
	if localIntentDir != "" {
		checks = append(checks, checkContractAssessment(localIntentDir, pairedIntentDir))
	}

	// 10. Contract verification wiring — a contract that claims
	// requiresVerification must carry at least one verification anchor, else the
	// promise is empty (no spine to the failure/invariant/test layer).
	if genErr == nil {
		checks = append(checks, checkContractVerificationWiring(checkNTBytes))
	}

	// 11. Seed orphans — committed-seed nodes whose authoring YAML no longer
	// exists (store-vs-YAML drift the ownership-aware freshness check tolerates
	// for the external partition). Third leg of the coherence gate (GC-1).
	if seedPath != "" {
		if auditDomain == "" {
			checks = append(checks, checkSeedOrphans(svcRepo, agRepo, seedPath))
		} else {
			checks = append(checks, checkSeedOrphansInDomain(svcRepo, agRepo, seedPath, auditDomain))
		}
	}

	// PR-advisory staleness (GC-2): downgrade ONLY embeddata-freshness FAIL to
	// WARN. A services-side awareness addition changes the seed digest, so the
	// committed AG seed lags until the seed-rebuild workflow auto-commits it on
	// merge; that lag must not block a PR. Corpus CORRECTNESS — seed-orphans,
	// yaml/ntriples validity, coverage, contract wiring — stays a hard FAIL.
	if *warnStale {
		downgradeFreshnessToAdvisory(checks)
	}

	// Report
	fmt.Println()
	fails, warns := 0, 0
	for _, c := range checks {
		marker := " "
		switch c.level {
		case auditFAIL:
			marker = "x"
			fails++
		case auditWARN:
			marker = "!"
			warns++
		}
		fmt.Printf("  %s %-24s %s  %s\n", marker, c.name, c.level, c.summary)
		if *verbose && len(c.details) > 0 {
			for _, d := range c.details {
				fmt.Printf("      %s\n", d)
			}
		}
	}

	fmt.Println()
	fmt.Printf("  %d checks: %d pass, %d warn, %d fail\n",
		len(checks), len(checks)-fails-warns, warns, fails)
	fmt.Println("  Scope: validates the corpus's internal consistency (freshness, validity,")
	fmt.Println("  coverage, test/contract wiring). A clean result does NOT assert that the repo")
	fmt.Println("  builds, that CI is green, or that the corpus is fresh vs current source — run")
	fmt.Println("  the build/tests and re-extract (make import-graph / proto-contracts / scip) for those.")
	if auditDomain != "" {
		fmt.Println("  Domain scope applies to graph-derived checks; raw YAML parse validity still checks")
		fmt.Println("  the resolved local corpus files before graph filtering.")
	}

	if *fix {
		var fixable []auditResult
		for _, c := range checks {
			if c.level != auditPASS && c.fixFn != nil {
				fixable = append(fixable, c)
			}
		}
		if len(fixable) == 0 {
			fmt.Println("\n  --fix: nothing to auto-fix")
		} else {
			fmt.Printf("\n  --fix: %d fixable issue(s)\n", len(fixable))
			fixed := 0
			for _, c := range fixable {
				fmt.Printf("\n  fixing: %s — %s\n", c.name, c.fixDesc)
				if err := c.fixFn(); err != nil {
					fmt.Printf("    FAILED: %v\n", err)
				} else {
					fmt.Println("    done")
					fixed++
				}
			}
			fmt.Printf("\n  %d/%d fixes applied\n", fixed, len(fixable))
		}
	}

	if *ciMode && fails > 0 {
		return 1
	}
	return 0
}

func auditSeedGenerationInputs(inputDirs []string, intentDir, svcRepo, agRepo string) ([]string, string) {
	if svcRepo != "" || agRepo == "" {
		return inputDirs, intentDir
	}
	selfAwareness := filepath.Join(agRepo, "docs", "awareness")
	if _, err := os.Stat(selfAwareness); err != nil {
		return inputDirs, ""
	}
	return []string{selfAwareness}, ""
}

func auditRepoForDomain(domain, svcRepo, agRepo string) string {
	domain = strings.TrimSpace(domain)
	if domain == "" {
		return ""
	}
	if svcRepo != "" && gitRemoteDomain(svcRepo) == domain {
		return svcRepo
	}
	if agRepo != "" && gitRemoteDomain(agRepo) == domain {
		return agRepo
	}
	return ""
}

func auditInputsForRepo(inputDirs []string, intentDir, repoRoot string) ([]string, string) {
	if strings.TrimSpace(repoRoot) == "" {
		return inputDirs, intentDir
	}
	var dirs []string
	for _, dir := range inputDirs {
		if dirUnder(dir, repoRoot) {
			dirs = append(dirs, dir)
		}
	}
	if intentDir != "" && !dirUnder(intentDir, repoRoot) {
		intentDir = ""
	}
	return dirs, intentDir
}

// filterNTriplesToDomain returns the graph slice visible to one repo/domain:
// nodes explicitly tagged with aw:repo == domain plus shared nodes. This mirrors
// the runtime query scope for repo-selected views while staying deterministic
// over the freshly generated audit corpus.
type domainFilterResult struct {
	bytes          []byte
	triples        int
	repoSubjects   int
	sharedSubjects int
}

func filterNTriplesToDomain(ntBytes []byte, domain string) ([]byte, int) {
	res := filterNTriplesToDomainResult(ntBytes, domain)
	return res.bytes, res.triples
}

func filterNTriplesToDomainResult(ntBytes []byte, domain string) domainFilterResult {
	domain = strings.TrimSpace(domain)
	if domain == "" {
		return domainFilterResult{bytes: ntBytes, triples: countNTriplesLines(ntBytes)}
	}

	repoPred := "<" + rdf.PropRepo + ">"
	domainPred := "<" + rdf.PropDomain + ">"
	keep := map[string]bool{}
	repoSubjects := map[string]bool{}
	sharedSubjects := map[string]bool{}

	sc := bufio.NewScanner(bytes.NewReader(ntBytes))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		s, p, rest, ok := splitNTripleLine(sc.Text())
		if !ok {
			continue
		}
		switch p {
		case repoPred:
			if v, ok := ntLiteralValue(rest); ok && v == domain {
				keep[s] = true
				repoSubjects[s] = true
			}
		case domainPred:
			if v, ok := ntLiteralValue(rest); ok && v == rdf.DomainShared {
				keep[s] = true
				sharedSubjects[s] = true
			}
		}
	}

	var out bytes.Buffer
	count := 0
	sc = bufio.NewScanner(bytes.NewReader(ntBytes))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		s, _, _, ok := splitNTripleLine(line)
		if !ok || !keep[s] {
			continue
		}
		out.WriteString(line)
		out.WriteByte('\n')
		count++
	}
	return domainFilterResult{
		bytes:          out.Bytes(),
		triples:        count,
		repoSubjects:   len(repoSubjects),
		sharedSubjects: len(sharedSubjects),
	}
}

func checkAuditDomainScope(domain, repoRoot string, scoped domainFilterResult) auditResult {
	const name = "domain-scope"
	domain = strings.TrimSpace(domain)
	if domain == "" {
		return auditResult{name: name, level: auditPASS, summary: "unscoped audit"}
	}
	if scoped.repoSubjects == 0 {
		detail := fmt.Sprintf("no generated graph subjects carry aw:repo %q", domain)
		if repoRoot == "" {
			detail += "; no matching local repo root was found"
		}
		return auditResult{
			name:    name,
			level:   auditFAIL,
			summary: fmt.Sprintf("domain %q matched 0 repo-owned subjects", domain),
			details: []string{detail},
		}
	}
	summary := fmt.Sprintf("%s scoped: %d repo-owned subject(s), %d shared subject(s), %d triples",
		domain, scoped.repoSubjects, scoped.sharedSubjects, scoped.triples)
	if repoRoot == "" {
		return auditResult{name: name, level: auditWARN, summary: summary + "; local repo root unavailable"}
	}
	return auditResult{name: name, level: auditPASS, summary: summary}
}

func splitNTripleLine(line string) (subject, predicate, rest string, ok bool) {
	f := strings.SplitN(strings.TrimSpace(line), " ", 3)
	if len(f) < 3 {
		return "", "", "", false
	}
	return f[0], f[1], f[2], true
}

func countNTriplesLines(ntBytes []byte) int {
	count := 0
	sc := bufio.NewScanner(bytes.NewReader(ntBytes))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		if strings.TrimSpace(sc.Text()) != "" {
			count++
		}
	}
	return count
}

// downgradeFreshnessToAdvisory implements --warn-stale (GC-2): it turns ONLY an
// embeddata-freshness FAIL into a WARN, leaving every other check untouched. A
// services-side awareness addition lags the committed seed until the
// seed-rebuild workflow auto-reconciles it on merge, so seed staleness must not
// block a PR — but corpus correctness (seed-orphans, validity, coverage) stays
// a hard FAIL.
func downgradeFreshnessToAdvisory(checks []auditResult) {
	for i := range checks {
		if checks[i].name == "embeddata-freshness" && checks[i].level == auditFAIL {
			checks[i].level = auditWARN
			checks[i].summary = "ADVISORY (--warn-stale): " + checks[i].summary + " — auto-reconciled on merge by seed-rebuild"
		}
	}
}

// ── audit types ──────────────────────────────────────────────────────────

type auditLevel int

const (
	auditPASS auditLevel = iota
	auditWARN
	auditFAIL
)

func (l auditLevel) String() string {
	switch l {
	case auditPASS:
		return "PASS"
	case auditWARN:
		return "WARN"
	case auditFAIL:
		return "FAIL"
	}
	return "?"
}

type auditResult struct {
	name    string
	level   auditLevel
	summary string
	details []string
	fixFn   func() error
	fixDesc string
}

type auditCoverageEntry struct {
	Principle    string   `yaml:"principle"`
	Tier         string   `yaml:"tier"`
	Reason       string   `yaml:"reason"`
	IntendedTier string   `yaml:"intended_tier"`
	Tests        []string `yaml:"tests"`
}

type auditCoverageRegistry struct {
	EnforcementRatchet struct {
		MaxReviewOnly int `yaml:"max_review_only"`
	} `yaml:"enforcement_ratchet"`
	Coverage []auditCoverageEntry `yaml:"meta_principle_coverage"`
}

type auditCoverageDefect struct {
	Principle string
	Kind      string
	Detail    string
}

// ── check 1: embeddata freshness ─────────────────────────────────────────

// checkEmbeddataFreshness verifies the committed seed is current with the YAML
// sources. It is ownership-aware (see seedfreshness.go): only drift in
// awareness-graph-OWNED triples fails. Triples authored by the paired services
// YAML that the committed (awareness-graph master) seed does not have yet are
// cross-repo context — they are reported but do not fail the services-side gate,
// so a services PR is not blocked merely because the paired awareness-graph seed
// PR has not merged. agOnly is the seed regenerated from the awareness-graph
// corpus alone; nil means ownership is unknown and we fall back to strict
// comparison so a generation failure cannot hide drift.
func checkEmbeddataFreshness(ntBytes []byte, seedPath string, agOnly []byte) auditResult {
	committed, err := os.ReadFile(seedPath)
	if err != nil {
		return auditResult{name: "embeddata-freshness", level: auditFAIL, summary: "cannot read: " + err.Error()}
	}
	return evaluateSeedFreshness(committed, ntBytes, agOnly)
}

func evaluateSeedFreshness(committed, generated, agOnly []byte) auditResult {
	if sha256.Sum256(generated) == sha256.Sum256(committed) {
		return auditResult{name: "embeddata-freshness", level: auditPASS, summary: "current"}
	}

	if agOnly == nil {
		newLines := bytes.Count(generated, []byte("\n"))
		oldLines := bytes.Count(committed, []byte("\n"))
		return auditResult{
			name: "embeddata-freshness", level: auditFAIL,
			summary: fmt.Sprintf("STALE (committed: %d lines, generated: %d lines; strict — owned corpus unavailable)", oldLines, newLines),
		}
	}

	owned, external := classifySeedDiff(committed, generated, agOnly)
	if len(owned) == 0 {
		summary := "current"
		if len(external) > 0 {
			summary = fmt.Sprintf("owned triples current; %d external/context triple(s) differ (cross-repo lag, gated by the owning repo)", len(external))
		}
		return auditResult{name: "embeddata-freshness", level: auditPASS, summary: summary}
	}
	res := auditResult{
		name: "embeddata-freshness", level: auditFAIL,
		summary: fmt.Sprintf("STALE — %d awareness-graph-owned triple(s) drift", len(owned)),
	}
	for i, l := range owned {
		if i >= 10 {
			break
		}
		res.details = append(res.details, l)
	}
	return res
}

// ── check 2: YAML validity ───────────────────────────────────────────────

func checkYAMLValidity(inputDirs []string, intentDir, svcRepo, agRepo string, totalFiles int) auditResult {
	opts := extractor.ImportDirOptions{
		StripPathPrefixes: []string{agRepo, svcRepo},
	}
	var invalidCount, unknownCount int
	var scannedFiles int
	seenDirs := map[string]bool{}
	var details []string

	scanDir := func(dir string) {
		clean := filepath.Clean(dir)
		if seenDirs[clean] {
			return
		}
		seenDirs[clean] = true
		_, report, err := extractor.ImportAwarenessDirWithOpts(dir, &bytes.Buffer{}, opts)
		if err != nil {
			return
		}
		scannedFiles += len(report.Files)
		for _, f := range report.Skipped() {
			switch f.Status {
			case extractor.StatusInvalid:
				invalidCount++
				details = append(details, fmt.Sprintf("INVALID: %s (%s)", f.Path, f.Reason))
			case extractor.StatusUnknownSchema:
				unknownCount++
				details = append(details, fmt.Sprintf("unknown: %s (%s)", f.Path, f.Reason))
			}
		}
	}

	for _, dir := range inputDirs {
		scanDir(dir)
	}
	if intentDir != "" {
		scanDir(intentDir)
	}
	if scannedFiles > 0 {
		totalFiles = scannedFiles
	}

	if invalidCount > 0 {
		return auditResult{
			name: "yaml-validity", level: auditFAIL,
			summary: fmt.Sprintf("%d invalid, %d unknown (of %d files)", invalidCount, unknownCount, totalFiles),
			details: details,
		}
	}
	if unknownCount > 0 {
		return auditResult{
			name: "yaml-validity", level: auditWARN,
			summary: fmt.Sprintf("%d unknown schema, 0 invalid (of %d files)", unknownCount, totalFiles),
			details: details,
		}
	}
	return auditResult{name: "yaml-validity", level: auditPASS, summary: fmt.Sprintf("%d files clean", totalFiles)}
}

// ── check 3: N-Triples validity ──────────────────────────────────────────

func checkNTValidity(ntBytes []byte, totalTriples int) auditResult {
	errs := extractor.ValidateNTriples(bytes.NewReader(ntBytes))
	if len(errs) == 0 {
		return auditResult{name: "ntriples-validity", level: auditPASS, summary: fmt.Sprintf("%d triples, all valid", totalTriples)}
	}
	var details []string
	for i, e := range errs {
		if i >= 10 {
			details = append(details, fmt.Sprintf("... %d more", len(errs)-i))
			break
		}
		details = append(details, e.Error())
	}
	return auditResult{
		name: "ntriples-validity", level: auditFAIL,
		summary: fmt.Sprintf("%d errors in %d triples", len(errs), totalTriples),
		details: details,
	}
}

// ── check 4: coverage gaps ───────────────────────────────────────────────

func checkCoverageGaps(svcRepo string, ntBytes []byte) auditResult {
	hrfPath := filepath.Join(svcRepo, "docs", "awareness", "high_risk_files.yaml")
	raw, err := os.ReadFile(hrfPath)
	if err != nil {
		return auditResult{name: "coverage-gaps", level: auditWARN, summary: "no high_risk_files.yaml"}
	}
	var doc struct {
		Files []string `yaml:"files"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return auditResult{name: "coverage-gaps", level: auditWARN, summary: "parse error: " + err.Error()}
	}

	fileRefs := collectFilePathsFromNT(ntBytes)
	var uncovered []string
	for _, f := range doc.Files {
		f = strings.TrimSuffix(f, "/")
		found := false
		for ref := range fileRefs {
			if strings.Contains(ref, f) {
				found = true
				break
			}
		}
		if !found {
			uncovered = append(uncovered, f)
		}
	}

	if len(uncovered) == 0 {
		return auditResult{name: "coverage-gaps", level: auditPASS,
			summary: fmt.Sprintf("all %d high-risk files have anchors", len(doc.Files))}
	}
	return auditResult{name: "coverage-gaps", level: auditWARN,
		summary: fmt.Sprintf("%d/%d high-risk files have no anchors", len(uncovered), len(doc.Files)),
		details: uncovered,
	}
}

// ── check 5: stale file refs ─────────────────────────────────────────────

func checkStaleFileRefs(svcRepo, agRepo string, ntBytes []byte) auditResult {
	fileRefs := collectFilePathsFromNT(ntBytes)
	var stale []string
	checked := 0
	for path := range fileRefs {
		if !strings.HasPrefix(path, "golang/") && !strings.HasPrefix(path, "docs/") &&
			!strings.HasPrefix(path, "proto/") && !strings.HasPrefix(path, "typescript/") {
			continue
		}
		if strings.ContainsAny(path, "*?[") {
			continue
		}
		checked++
		svcOK := svcRepo != "" && fileExists(filepath.Join(svcRepo, path))
		agOK := agRepo != "" && fileExists(filepath.Join(agRepo, path))
		if !svcOK && !agOK {
			stale = append(stale, path)
		}
	}
	if len(stale) == 0 {
		return auditResult{name: "stale-file-refs", level: auditPASS,
			summary: fmt.Sprintf("all %d referenced files exist", checked)}
	}
	return auditResult{name: "stale-file-refs", level: auditWARN,
		summary: fmt.Sprintf("%d/%d referenced files missing", len(stale), checked),
		details: stale,
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// ── check 7: meta-principle coverage completeness ───────────────────────

func checkMetaPrincipleCoverage(svcRepo, agRepo string) auditResult {
	metas, instToMeta, err := loadAuditAwarenessYAMLs(filepath.Join(agRepo, "docs", "awareness", "generic"))
	if err != nil {
		return auditResult{name: "meta-principle-coverage", level: auditFAIL, summary: err.Error()}
	}
	if svcRepo != "" && fileExists(filepath.Join(svcRepo, "docs", "awareness")) {
		svcMetas, svcInst, err := loadAuditAwarenessYAMLs(filepath.Join(svcRepo, "docs", "awareness"))
		if err != nil {
			return auditResult{name: "meta-principle-coverage", level: auditFAIL, summary: err.Error()}
		}
		for k := range svcMetas {
			metas[k] = true
		}
		for k, v := range svcInst {
			if _, ok := instToMeta[k]; !ok {
				instToMeta[k] = v
			}
		}
	}

	raw, err := os.ReadFile(filepath.Join(agRepo, "docs", "awareness-control", "meta_principle_coverage.yaml"))
	if err != nil {
		return auditResult{name: "meta-principle-coverage", level: auditFAIL, summary: "cannot read coverage registry: " + err.Error()}
	}
	var reg auditCoverageRegistry
	if err := yaml.Unmarshal(raw, &reg); err != nil {
		return auditResult{name: "meta-principle-coverage", level: auditFAIL, summary: "cannot parse coverage registry: " + err.Error()}
	}
	gated, err := auditGatedInstancesFromMakefile(filepath.Join(agRepo, "Makefile"))
	if err != nil {
		return auditResult{name: "meta-principle-coverage", level: auditFAIL, summary: "cannot derive code_scanner coverage: " + err.Error()}
	}
	return evaluateMetaPrincipleCoverage(metas, instToMeta, reg, gated)
}

func evaluateMetaPrincipleCoverage(metas map[string]bool, instToMeta map[string]string, reg auditCoverageRegistry, gatedInstances []string) auditResult {
	const name = "meta-principle-coverage"
	validResidualTiers := map[string]bool{
		"behavioral":  true,
		"declaration": true,
		"planned":     true,
		"review_only": true,
	}

	codeScanner := map[string]bool{}
	var notes []string
	for _, inst := range gatedInstances {
		if strings.HasPrefix(inst, "meta.") {
			codeScanner[inst] = true
		} else if mp, ok := instToMeta[inst]; ok {
			codeScanner[mp] = true
		} else {
			notes = append(notes, fmt.Sprintf("gated instance %q has no meta parent via related_invariants", inst))
		}
	}

	registry := map[string]auditCoverageEntry{}
	var invalid []string
	for _, e := range reg.Coverage {
		if e.Principle == "" {
			invalid = append(invalid, "a coverage entry is missing `principle:`")
			continue
		}
		if !validResidualTiers[e.Tier] {
			invalid = append(invalid, fmt.Sprintf("%s: invalid tier %q", e.Principle, e.Tier))
		}
		if strings.TrimSpace(e.Reason) == "" {
			invalid = append(invalid, fmt.Sprintf("%s: tier %q requires a reason", e.Principle, e.Tier))
		}
		if e.Tier == "behavioral" && len(e.Tests) == 0 {
			invalid = append(invalid, fmt.Sprintf("%s: behavioral tier must cite gating test(s)", e.Principle))
		}
		if e.Tier == "planned" && strings.TrimSpace(e.IntendedTier) == "" {
			invalid = append(invalid, fmt.Sprintf("%s: planned tier must name intended_tier", e.Principle))
		}
		registry[e.Principle] = e
	}

	var duplicates []string
	for _, d := range detectAuditCoverageSelfDefects(reg.Coverage) {
		if d.Kind == "conflict" {
			duplicates = append(duplicates, fmt.Sprintf("%s: conflicting classifications (%s)", d.Principle, d.Detail))
		} else {
			duplicates = append(duplicates, fmt.Sprintf("%s: duplicate coverage entry (%s)", d.Principle, d.Detail))
		}
	}

	var unclassified []string
	counts := map[string]int{}
	for p := range metas {
		switch {
		case codeScanner[p]:
			counts["code_scanner"]++
		case registry[p].Principle != "":
			counts[registry[p].Tier]++
		default:
			unclassified = append(unclassified, p)
		}
	}
	for p, e := range registry {
		if !metas[p] {
			invalid = append(invalid, fmt.Sprintf("%s: listed in coverage registry but not a known meta.* principle", p))
		}
		if codeScanner[p] {
			notes = append(notes, fmt.Sprintf("%s is now auto-covered (code_scanner); its registry entry (%s) is redundant", p, e.Tier))
		}
	}
	sort.Strings(invalid)
	sort.Strings(duplicates)
	sort.Strings(unclassified)
	sort.Strings(notes)

	if reg.EnforcementRatchet.MaxReviewOnly <= 0 {
		invalid = append(invalid, "enforcement_ratchet.max_review_only is unset")
	} else if counts["review_only"] > reg.EnforcementRatchet.MaxReviewOnly {
		invalid = append(invalid, fmt.Sprintf("review_only count %d exceeds ratchet ceiling %d", counts["review_only"], reg.EnforcementRatchet.MaxReviewOnly))
	}

	var details []string
	if len(unclassified) > 0 {
		details = append(details, fmt.Sprintf("%d meta.* principle(s) have no enforcement tier:", len(unclassified)))
		for i, p := range unclassified {
			if i >= 10 {
				details = append(details, fmt.Sprintf("... %d more unclassified", len(unclassified)-i))
				break
			}
			details = append(details, p)
		}
	}
	if len(invalid) > 0 {
		details = append(details, invalid...)
	}
	if len(duplicates) > 0 {
		details = append(details, duplicates...)
	}
	if len(unclassified) > 0 || len(invalid) > 0 || len(duplicates) > 0 {
		summaryParts := []string{}
		if len(unclassified) > 0 {
			summaryParts = append(summaryParts, fmt.Sprintf("%d unclassified", len(unclassified)))
		}
		if len(invalid) > 0 {
			summaryParts = append(summaryParts, fmt.Sprintf("%d invalid registry issue(s)", len(invalid)))
		}
		if len(duplicates) > 0 {
			summaryParts = append(summaryParts, fmt.Sprintf("%d duplicate/conflict issue(s)", len(duplicates)))
		}
		return auditResult{name: name, level: auditFAIL, summary: strings.Join(summaryParts, "; "), details: details}
	}

	for i, note := range notes {
		if i >= 10 {
			details = append(details, fmt.Sprintf("... %d more notes", len(notes)-i))
			break
		}
		details = append(details, note)
	}

	return auditResult{
		name:  name,
		level: auditPASS,
		summary: fmt.Sprintf("%d meta.* principles classified (%d code_scanner, %d declaration, %d behavioral, %d planned, %d review_only)",
			len(metas), counts["code_scanner"], counts["declaration"], counts["behavioral"], counts["planned"], counts["review_only"]),
		details: details,
	}
}

func detectAuditCoverageSelfDefects(entries []auditCoverageEntry) []auditCoverageDefect {
	seen := map[string]auditCoverageEntry{}
	var out []auditCoverageDefect
	for _, e := range entries {
		if e.Principle == "" {
			continue
		}
		if prev, dup := seen[e.Principle]; dup {
			if prev.Tier != e.Tier || prev.IntendedTier != e.IntendedTier {
				out = append(out, auditCoverageDefect{
					Principle: e.Principle,
					Kind:      "conflict",
					Detail:    fmt.Sprintf("%s/intended=%q vs %s/intended=%q", prev.Tier, prev.IntendedTier, e.Tier, e.IntendedTier),
				})
			} else {
				out = append(out, auditCoverageDefect{
					Principle: e.Principle,
					Kind:      "duplicate",
					Detail:    fmt.Sprintf("declared twice with tier %s", e.Tier),
				})
			}
		}
		seen[e.Principle] = e
	}
	return out
}

func auditWalkYAML(node interface{}, fn func(map[string]interface{})) {
	switch v := node.(type) {
	case map[string]interface{}:
		fn(v)
		for _, child := range v {
			auditWalkYAML(child, fn)
		}
	case []interface{}:
		for _, child := range v {
			auditWalkYAML(child, fn)
		}
	}
}

func loadAuditAwarenessYAMLs(dir string) (metas map[string]bool, instToMeta map[string]string, err error) {
	metas = map[string]bool{}
	instToMeta = map[string]string{}
	files, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil {
		return nil, nil, fmt.Errorf("glob %s: %w", dir, err)
	}
	if len(files) == 0 {
		return nil, nil, fmt.Errorf("no awareness YAMLs under %s", dir)
	}
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			return nil, nil, fmt.Errorf("read %s: %w", f, err)
		}
		var doc interface{}
		if err := yaml.Unmarshal(raw, &doc); err != nil {
			return nil, nil, fmt.Errorf("parse %s: %w", f, err)
		}
		auditWalkYAML(doc, func(m map[string]interface{}) {
			id, _ := m["id"].(string)
			if id == "" {
				return
			}
			if strings.HasPrefix(id, "meta.") {
				if _, hasStatus := m["status"]; hasStatus {
					metas[id] = true
				}
			}
			if rel, ok := m["related_invariants"].([]interface{}); ok {
				for _, r := range rel {
					if rs, ok := r.(string); ok && strings.HasPrefix(rs, "meta.") {
						if _, seen := instToMeta[id]; !seen {
							instToMeta[id] = rs
						}
						break
					}
				}
			}
		})
	}
	return metas, instToMeta, nil
}

func auditGatedInstancesFromMakefile(path string) ([]string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	mk := string(raw)
	set := map[string]bool{}
	for _, m := range regexp.MustCompile(`-principle (\S+)`).FindAllStringSubmatch(mk, -1) {
		id := m[1]
		if strings.HasPrefix(id, "$") || id == "proof" {
			continue
		}
		set[id] = true
	}
	if blk := regexp.MustCompile(`(?s)RULEGUARD_INSTANCES :=(.*?)\n\n`).FindStringSubmatch(mk); blk != nil {
		for _, tok := range regexp.MustCompile(`[A-Za-z0-9_.]+`).FindAllString(blk[1], -1) {
			if strings.Contains(tok, "_") || strings.Contains(tok, ".") {
				set[tok] = true
			}
		}
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out, nil
}

// ── check 9: seed orphans (store-vs-YAML coherence, GC-1) ────────────────
//
// checkSeedOrphans flags nodes in the COMMITTED seed whose authoring YAML no
// longer exists under any known repo root — a ghost the seed carries with no
// authored origin. This is the third leg of the coherence gate (alongside
// `sensei validate`'s duplicate_id and dangling_*_ref checks); together they make
// "no incoherent graph merges" fail-closed.
//
// Why the COMMITTED seed and not the freshly generated N-Triples: the generated
// graph is built from current YAML, so by construction it cannot contain a node
// whose source file is gone. Only the committed artifact persists ghosts.
//
// Why this is not subsumed by embeddata-freshness: freshness is ownership-aware
// (classifySeedDiff) and TOLERATES drift in the external/services partition so a
// services PR is not blocked by an un-merged paired awareness-graph seed. A
// services-authored node whose YAML was deleted therefore slips past freshness
// but is a real orphan — this check names it.
//
// authoredIn is the universal provenance discriminator: every authored node —
// hand-written or code-scanned (the generated/ corpus) — carries one. Synthetic
// markers ("generated:seed_marker") are not file paths and are skipped, so the
// check is class-agnostic without false-positiving on code-symbol nodes.
//
// Severity: FAIL when both repo roots are available (a path missing under both
// is unambiguously orphaned — no cross-repo masking can cause a false positive).
// Degrades to WARN when a root is absent, since a partial checkout cannot
// distinguish a true orphan from a node anchored in the un-checked-out repo.
func checkSeedOrphans(svcRepo, agRepo, seedPath string) auditResult {
	const name = "seed-orphans"
	committed, err := os.ReadFile(seedPath)
	if err != nil {
		return auditResult{name: name, level: auditFAIL, summary: "cannot read seed: " + err.Error()}
	}
	return checkSeedOrphansFromNT(svcRepo, agRepo, committed)
}

func checkSeedOrphansInDomain(svcRepo, agRepo, seedPath, domain string) auditResult {
	const name = "seed-orphans"
	committed, err := os.ReadFile(seedPath)
	if err != nil {
		return auditResult{name: name, level: auditFAIL, summary: "cannot read seed: " + err.Error()}
	}
	scoped, _ := filterNTriplesToDomain(committed, domain)
	res := checkSeedOrphansFromNT(svcRepo, agRepo, scoped)
	if res.level == auditPASS {
		res.summary += " in domain " + domain
	}
	return res
}

func checkSeedOrphansFromNT(svcRepo, agRepo string, committed []byte) auditResult {
	const name = "seed-orphans"
	authoredPred := "<" + rdf.PropAuthoredIn + ">"
	// subject → file-path authoredIn values (synthetic markers excluded).
	provenance := map[string][]string{}
	sc := bufio.NewScanner(bytes.NewReader(committed))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.Contains(line, authoredPred) {
			continue
		}
		f := strings.SplitN(line, " ", 3)
		if len(f) < 3 || f[1] != authoredPred {
			continue
		}
		path, ok := ntLiteralValue(f[2])
		if !ok || !isSeedProvenancePath(path) {
			continue // synthetic marker ("generated:…") or non-file literal
		}
		provenance[f[0]] = append(provenance[f[0]], path)
	}

	bothRoots := svcRepo != "" && agRepo != ""
	var orphans []string
	checked := 0
	for subj, paths := range provenance {
		checked++
		alive := false
		dead := ""
		for _, p := range paths {
			if seedPathExistsUnderRoots(p, svcRepo, agRepo) {
				alive = true
				break
			}
			if dead == "" {
				dead = p
			}
		}
		if !alive {
			orphans = append(orphans, fmt.Sprintf("%s (authoredIn %s — missing)", seedNodeShortID(subj), dead))
		}
	}
	sort.Strings(orphans)

	if len(orphans) == 0 {
		return auditResult{name: name, level: auditPASS,
			summary: fmt.Sprintf("all %d authored seed node(s) resolve to a live source file", checked)}
	}
	level := auditFAIL
	suffix := ""
	if !bothRoots {
		level = auditWARN
		suffix = " (WARN: a repo root is unavailable — cannot distinguish a true orphan from a node anchored in the un-checked-out repo)"
	}
	return auditResult{name: name, level: level,
		summary: fmt.Sprintf("%d/%d authored seed node(s) have no live authoring YAML%s", len(orphans), checked, suffix),
		details: orphans,
	}
}

// ntLiteralValue extracts the value of an N-Triples literal object term, e.g.
// `"docs/awareness/x.yaml" .` → ("docs/awareness/x.yaml", true). Awareness
// provenance literals never contain quotes or backslashes, so a scan to the
// next quote is sufficient.
func ntLiteralValue(obj string) (string, bool) {
	if len(obj) == 0 || obj[0] != '"' {
		return "", false
	}
	if i := strings.IndexByte(obj[1:], '"'); i >= 0 {
		return obj[1 : 1+i], true
	}
	return "", false
}

// isSeedProvenancePath reports whether an authoredIn literal is a repo-relative
// file path (vs. a synthetic marker such as "generated:seed_marker"). Synthetic
// markers carry a ':' scheme and no path separator; real provenance is a path.
func isSeedProvenancePath(p string) bool {
	return strings.Contains(p, "/") && !strings.Contains(p, ":")
}

// seedPathExistsUnderRoots reports whether a repo-relative provenance path
// resolves under either repo root (mirrors checkStaleFileRefs).
func seedPathExistsUnderRoots(path, svcRepo, agRepo string) bool {
	if svcRepo != "" && fileExists(filepath.Join(svcRepo, path)) {
		return true
	}
	if agRepo != "" && fileExists(filepath.Join(agRepo, path)) {
		return true
	}
	return false
}

// seedNodeShortID turns "<https://globular.io/awareness#failureMode/x>" into
// "failureMode/x" for compact reporting.
func seedNodeShortID(subj string) string {
	s := strings.TrimSuffix(strings.TrimPrefix(subj, "<"), ">")
	if i := strings.LastIndex(s, "#"); i >= 0 {
		return s[i+1:]
	}
	return s
}

// ── check 6: test coverage ──────────────────────────────────────────────

func checkTestCoverage(svcRepo string) auditResult {
	invPath := filepath.Join(svcRepo, "docs", "awareness", "invariants.yaml")
	raw, err := os.ReadFile(invPath)
	if err != nil {
		return auditResult{name: "test-coverage", level: auditWARN, summary: "cannot read invariants.yaml"}
	}
	var doc struct {
		Invariants []struct {
			ID                      string   `yaml:"id"`
			Severity                string   `yaml:"severity"`
			RequiredTests           []string `yaml:"required_tests"`
			TestNotApplicableReason string   `yaml:"test_not_applicable_reason"`
		} `yaml:"invariants"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return auditResult{name: "test-coverage", level: auditWARN, summary: "parse error: " + err.Error()}
	}

	var critical, missing int
	var details []string
	for _, inv := range doc.Invariants {
		if inv.Severity != "critical" && inv.Severity != "high" {
			continue
		}
		critical++
		if len(inv.RequiredTests) == 0 && inv.TestNotApplicableReason == "" {
			missing++
			details = append(details, fmt.Sprintf("[%s] %s", inv.Severity, inv.ID))
		}
	}
	if missing == 0 {
		return auditResult{name: "test-coverage", level: auditPASS,
			summary: fmt.Sprintf("all %d critical/high invariants covered", critical)}
	}
	return auditResult{name: "test-coverage", level: auditWARN,
		summary: fmt.Sprintf("%d/%d critical/high invariants missing tests", missing, critical),
		details: details,
	}
}

// ── check 7: contract assessment (report-only) ───────────────────────────

type auditIntentDoc struct {
	ID                string   `yaml:"id"`
	Level             string   `yaml:"level"`
	Status            string   `yaml:"status"`
	ExpressedBy       []string `yaml:"expressed_by"`
	RequiredTests     []string `yaml:"required_tests"`
	RelatedInvariants []string `yaml:"related_invariants"`
	RelatedTo         []string `yaml:"related_to"`
	ZoomsInTo         []string `yaml:"zooms_in_to"`
	ZoomsOutTo        []string `yaml:"zooms_out_to"`
}

func checkContractAssessment(intentDir, pairedIntentDir string) auditResult {
	entries, err := os.ReadDir(intentDir)
	if err != nil {
		return auditResult{name: "contract-assessment", level: auditWARN, summary: "cannot read intent dir"}
	}

	counts := map[contractassess.Outcome]int{}
	var details []string
	parseErrors := 0

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if ext != ".yaml" && ext != ".yml" {
			continue
		}

		path := filepath.Join(intentDir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			parseErrors++
			details = append(details, fmt.Sprintf("parse error: %s (%v)", entry.Name(), err))
			continue
		}

		var doc auditIntentDoc
		if err := yaml.Unmarshal(data, &doc); err != nil {
			parseErrors++
			details = append(details, fmt.Sprintf("parse error: %s (%v)", entry.Name(), err))
			continue
		}
		if strings.TrimSpace(doc.ID) == "" || strings.EqualFold(strings.TrimSpace(doc.Status), "deprecated") {
			continue
		}

		result := contractassess.Assess(assessmentInputForIntent(doc))
		counts[result.Outcome]++
		details = append(details, fmt.Sprintf("%s: %s", result.Outcome, doc.ID))
	}

	sort.Strings(details)

	summary := fmt.Sprintf("%d contract-found, %d contract-synthesis-safe, %d contract-proposal-only, %d contract-unknown (local authored intents only)",
		counts[contractassess.ContractFound],
		counts[contractassess.ContractSynthesisSafe],
		counts[contractassess.ContractProposalOnly],
		counts[contractassess.ContractUnknown],
	)
	if pairedIntentDir != "" {
		summary = fmt.Sprintf("%s; sibling intent docs excluded from self-audit", summary)
	}
	if parseErrors > 0 {
		summary = fmt.Sprintf("%s; %d parse error(s)", summary, parseErrors)
	}

	level := auditPASS
	if parseErrors > 0 || counts[contractassess.ContractUnknown] > 0 || counts[contractassess.ContractProposalOnly] > 0 {
		level = auditWARN
	}
	if len(details) > 10 {
		details = append(details[:10], fmt.Sprintf("... %d more", len(details)-10))
	}
	return auditResult{name: "contract-assessment", level: level, summary: summary, details: details}
}

func assessmentInputForIntent(doc auditIntentDoc) contractassess.AssessmentInput {
	expressedBy := nonEmptyStrings(doc.ExpressedBy)
	requiredTests := nonEmptyStrings(doc.RequiredTests)

	var blockers []contractassess.Blocker
	if len(expressedBy) == 0 {
		blockers = append(blockers, contractassess.BlockerMissingOwnershipAuthority)
	}

	nearbyRelations := len(nonEmptyStrings(doc.RelatedInvariants)) +
		len(nonEmptyStrings(doc.RelatedTo)) +
		len(nonEmptyStrings(doc.ZoomsInTo)) +
		len(nonEmptyStrings(doc.ZoomsOutTo))

	scores := contractassess.EvidenceScores{}
	if len(expressedBy) > 0 {
		scores.DirectSourceAnnotation = 3
		scores.OwnershipAuthorityPath = 3
		scores.AbsenceOfConflictingContracts = 3
	}
	if len(requiredTests) > 0 {
		scores.ExistingTestsProvingBehavior = 4
	}
	if nearbyRelations > 0 {
		scores.NearbyHumanIntent = 3
	}
	if strings.EqualFold(strings.TrimSpace(doc.Level), "pattern") {
		scores.RepeatedImplementationPattern = 2
	}

	return contractassess.AssessmentInput{
		ExplicitContractExists: strings.EqualFold(strings.TrimSpace(doc.Level), "contract"),
		HasGoverningTest:       len(requiredTests) > 0,
		Scores:                 scores,
		Blockers:               blockers,
	}
}

func selectAuditIntentDirs(agRepo, svcRepo string) (localIntentDir, pairedIntentDir string) {
	if agRepo != "" {
		intent := filepath.Join(agRepo, "docs", "intent")
		if _, err := os.Stat(intent); err == nil {
			localIntentDir = intent
		}
	}
	if localIntentDir == "" {
		cwd, _ := os.Getwd()
		intent := filepath.Join(cwd, "docs", "intent")
		if _, err := os.Stat(intent); err == nil {
			localIntentDir = intent
		}
	}
	if svcRepo != "" {
		intent := filepath.Join(svcRepo, "docs", "intent")
		if _, err := os.Stat(intent); err == nil && intent != localIntentDir {
			pairedIntentDir = intent
		}
	}
	return localIntentDir, pairedIntentDir
}

func nonEmptyStrings(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if trimmed := strings.TrimSpace(s); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// ── helpers ──────────────────────────────────────────────────────────────

// checkContractVerificationWiring flags any contract that claims
// requiresVerification but carries no verification anchor — no required test, no
// constraining invariant, no known violation (violatedBy), and no detect rule.
// Such a contract promises verification it cannot deliver. WARN-level: it
// surfaces the gap without failing CI on pre-existing benchmark fixtures
// (promote to FAIL once the corpus is clean).
func checkContractVerificationWiring(ntBytes []byte) auditResult {
	const name = "contract-verification-wiring"
	wrap := func(p string) string { return "<" + p + ">" }
	typePred := wrap(rdf.PropType)
	contractObj := wrap(rdf.ClassContract)
	verifPred := wrap(rdf.PropRequiresVerification)
	repoPred := wrap(rdf.PropRepo)
	anchorPreds := map[string]bool{
		wrap(rdf.PropRequiresTest):           true,
		wrap(rdf.PropConstrainedByInvariant): true,
		wrap(rdf.PropViolatedBy):             true,
		wrap(rdf.PropDetectForbiddenPattern): true,
	}
	isContract := map[string]bool{}
	needsVerif := map[string]bool{}
	hasAnchor := map[string]bool{}
	repoTagged := map[string]bool{} // foreign/benchmark-domain fixtures — verified by their own harness, not this gate

	sc := bufio.NewScanner(bytes.NewReader(ntBytes))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		f := strings.SplitN(strings.TrimSpace(sc.Text()), " ", 3)
		if len(f) < 3 {
			continue
		}
		s, p, rest := f[0], f[1], f[2]
		switch {
		case p == typePred && strings.HasPrefix(rest, contractObj):
			isContract[s] = true
		case p == verifPred:
			needsVerif[s] = true
		case p == repoPred:
			repoTagged[s] = true
		case anchorPreds[p]:
			hasAnchor[s] = true
		}
	}

	// Police only HOME-domain (untagged) architectural contracts. Repo-tagged
	// contracts are benchmark / cross-repo fixtures (frozen_contract_set under
	// eval/) with their own verification model (the frozen-contract gate); this
	// awareness-corpus gate must not police them.
	var unbacked []string
	total := 0
	for s := range needsVerif {
		if !isContract[s] || repoTagged[s] {
			continue
		}
		total++
		if !hasAnchor[s] {
			unbacked = append(unbacked, contractShortID(s))
		}
	}
	sort.Strings(unbacked)
	if len(unbacked) == 0 {
		return auditResult{name: name, level: auditPASS,
			summary: fmt.Sprintf("all %d home-domain requiresVerification contract(s) carry a test/invariant/violatedBy/detect anchor (benchmark/fixture contracts excluded)", total)}
	}
	// FAIL (not WARN): a home-domain contract that claims it needs verification
	// but wires none is a hard regression.
	return auditResult{name: name, level: auditFAIL,
		summary: fmt.Sprintf("%d/%d home-domain requiresVerification contract(s) have no test/invariant/violatedBy/detect anchor", len(unbacked), total),
		details: unbacked}
}

// contractShortID turns "<…#contract/contract.x>" into "contract.x".
func contractShortID(subj string) string {
	s := strings.TrimSuffix(strings.TrimPrefix(subj, "<"), ">")
	if i := strings.LastIndex(s, "#contract/"); i >= 0 {
		return s[i+len("#contract/"):]
	}
	return s
}

func collectFilePathsFromNT(ntBytes []byte) map[string]bool {
	paths := make(map[string]bool)
	scanner := bufio.NewScanner(bytes.NewReader(ntBytes))
	scanner.Buffer(make([]byte, 1<<20), 1<<20)
	for scanner.Scan() {
		line := scanner.Text()
		idx := strings.Index(line, "#sourceFile/")
		if idx < 0 {
			continue
		}
		rest := line[idx+len("#sourceFile/"):]
		end := strings.IndexByte(rest, '>')
		if end < 0 {
			continue
		}
		path := rest[:end]
		path = strings.ReplaceAll(path, "%2F", "/")
		path = strings.ReplaceAll(path, "%2f", "/")
		paths[path] = true
	}
	return paths
}
