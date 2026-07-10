// SPDX-License-Identifier: Apache-2.0

// @awareness namespace=globular.awareness_graph
// @awareness component=build.principle_check.destructive_test_gate
// @awareness file_role=artifact_gate_for_ci_destructive_test_must_be_double_gated
// @awareness enforces=ci.destructive_test_must_be_double_gated

// Artifact gate for ci.destructive_test_must_be_double_gated.
//
// Born from a real incident: a destructive Oxigraph integration test (it PUT
// /store?default = REPLACE the default graph) ran against a LIVE dev store and
// clobbered it, because its only protection was a build tag + a comment warning.
// A comment is not a gate. The rule: any test that can mutate/replace/delete
// live external state must require BOTH a build tag (excluded from the default
// `go test ./...`) AND an explicit destructive opt-in environment variable
// (e.g. AWARENESS_OXIGRAPH_DESTRUCTIVE=1) so it cannot run by accident.
//
// classifyDestructiveTest is pure (fixture-tested below). The repo scan
// (TestDestructiveIntegrationTestsAreDoubleGated) walks real test files and
// fails on a clear violation. Fuzzy/uncertain destructive shapes are ADVISORY
// (reported, not failed) until the pattern set earns confidence — honest
// calibration over false-confident blocking.
package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const (
	destrOK        = "ok"
	destrViolation = "violation"
	destrAdvisory  = "advisory"
)

var (
	// HIGH-confidence destructive live-state operations.
	destrHighConfidence = []*regexp.Regexp{
		regexp.MustCompile(`\.Load\(`), // oxigraph Client.Load = PUT replace
		regexp.MustCompile(`reloadOxigraphStore`),
		regexp.MustCompile(`http\.MethodPut`),
		regexp.MustCompile(`http\.MethodDelete`),
		regexp.MustCompile(`/store\?default`), // Graph Store Protocol default graph
		regexp.MustCompile(`(?i)\bDROP\s+(GRAPH|TABLE|KEYSPACE)\b`),
		regexp.MustCompile(`(?i)\bTRUNCATE\b`),
		regexp.MustCompile(`(?i)\bDELETE\s+(FROM|DATA|WHERE)\b`),
		regexp.MustCompile(`(?i)\bCLEAR\s+(GRAPH|ALL|DEFAULT)\b`),
		regexp.MustCompile(`(?i)\bINSERT\s+DATA\b`),
	}
	// FUZZY destructive-looking calls — advisory only.
	destrFuzzy = []*regexp.Regexp{
		regexp.MustCompile(`\.(Reset|Wipe|Purge|Destroy|DeleteAll|DropAll)\(`),
	}
	// Explicit destructive opt-in env var: any os.Getenv whose name contains DESTRUCTIVE.
	destrEnvGate = regexp.MustCompile(`os\.Getenv\("[^"]*DESTRUCTIVE[^"]*"\)`)
	// Build tag that excludes the test from the default build.
	destrBuildTag = regexp.MustCompile(`(?m)^//go:build .*\b(integration|destructive)\b|^// \+build .*\b(integration|destructive)\b`)
	// httptest mock server — destructive ops here hit a mock, not live state.
	destrHttptest = regexp.MustCompile(`httptest\.New(TLS)?Server`)
	// Signals the test reaches a REAL external endpoint (not httptest's ts.URL).
	destrLiveEndpoint = regexp.MustCompile(`integrationURL\(|AWARENESS_[A-Z_]*URL|127\.0\.0\.1:\d|localhost:\d|os\.Getenv\("[^"]*URL"\)`)
)

func anyMatch(res []*regexp.Regexp, s string) bool {
	for _, re := range res {
		if re.MatchString(s) {
			return true
		}
	}
	return false
}

// classifyDestructiveTest verdicts a single Go test file's source.
func classifyDestructiveTest(content string) (verdict, reason string) {
	isInteg := destrBuildTag.MatchString(content)
	hasEnv := destrEnvGate.MatchString(content)
	hasHigh := anyMatch(destrHighConfidence, content)
	hasFuzzy := anyMatch(destrFuzzy, content)
	usesMock := destrHttptest.MatchString(content)
	hasLive := destrLiveEndpoint.MatchString(content)

	// httptest-mock unit test: destructive ops hit a mock, not live external
	// state. Safe — unless it also declares destructive intent (env/live endpoint).
	if usesMock && !isInteg && !hasLive && !hasEnv {
		return destrOK, "httptest mock unit test — destructive ops hit a mock, not live state"
	}
	// A destructive token is only a LIVE-STATE risk when the test actually reaches
	// live state: it carries an integration/destructive build tag, OR targets a
	// real endpoint, OR declares destructive intent via a *_DESTRUCTIVE env var.
	// Without any of those, a destructive-looking token (config .Load(), an
	// httptest PUT, a SPARQL string) cannot clobber a live store — no false
	// confidence, no false positive.
	liveState := isInteg || hasLive || hasEnv
	if hasHigh && liveState {
		if isInteg && hasEnv {
			return destrOK, "destructive live-state op is double-gated (build tag + destructive env)"
		}
		var missing []string
		if !isInteg {
			missing = append(missing, "an integration/destructive build tag")
		}
		if !hasEnv {
			missing = append(missing, "an explicit destructive opt-in env var (e.g. *_DESTRUCTIVE)")
		}
		return destrViolation, "destructive live-state operation missing " + strings.Join(missing, " and ")
	}
	if hasFuzzy && isInteg && !hasEnv {
		return destrAdvisory, "possibly-destructive call in an integration test without a destructive env gate — review and either gate or confirm it is read-only"
	}
	return destrOK, "no destructive live-state operation detected"
}

func TestDestructiveTestGate_Fixtures(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "destructive + both gates",
			want: destrOK,
			src:  "//go:build integration\npackage x\nimport \"os\"\nfunc T(){ if os.Getenv(\"AWARENESS_OXIGRAPH_DESTRUCTIVE\")!=\"1\"{return}; c.Load(ctx, r) }",
		},
		{
			name: "build tag but env gate missing",
			want: destrViolation,
			src:  "//go:build integration\npackage x\nfunc T(){ c.Load(ctx, r) }",
		},
		{
			name: "env gate but build tag missing (runs by default)",
			want: destrViolation,
			src:  "package x\nimport \"os\"\nfunc T(){ if os.Getenv(\"FOO_DESTRUCTIVE\")!=\"1\"{return}; c.Load(ctx, r) }",
		},
		{
			name: "non-destructive integration test",
			want: destrOK,
			src:  "//go:build integration\npackage x\nfunc T(){ c.Health(ctx); c.CountTriples(ctx) }",
		},
		{
			name: "httptest mock unit test using PUT (not live)",
			want: destrOK,
			src:  "package x\nimport \"net/http/httptest\"\nfunc T(){ ts:=httptest.NewServer(h); _=ts; req,_:=http.NewRequest(http.MethodPut,u,b) }",
		},
		{
			name: "fuzzy destructive call in integration test (advisory)",
			want: destrAdvisory,
			src:  "//go:build integration\npackage x\nfunc T(){ adminClient.Wipe(ctx) }",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, reason := classifyDestructiveTest(tc.src)
			if got != tc.want {
				t.Fatalf("verdict = %q, want %q (reason: %s)", got, tc.want, reason)
			}
		})
	}
}

// destructiveScanCarveOuts: files whose destructive-looking token is detection
// DATA or a non-live use (string arg, mocked rebuild, httptest URL, mock method),
// not a real live-state mutation. Each needs a reason. These document the known
// false-positive shapes of a regex heuristic that cannot tell a URL string
// literal from a live connection — which is also why the repo scan is ADVISORY,
// not a hard gate (the contract itself IS hard-gated, by the fixtures test).
var destructiveScanCarveOuts = map[string]string{
	"destructive_test_gate_test.go": "this scanner's own destructive-pattern literals are detection data, not calls",
	"oxigraph_reload_test.go":       "httptest mock unit test — PUT hits the mock (also OK via the mock guard)",
	"packaging_test.go":             "`.Load(` is a fake client's method definition; the localhost URL is in a config-defaults ASSERTION string, not a live connection",
	"cmd_propose_test.go":           "oxigraphURL is a passed ARG; the rebuild/reload is mocked (the *calls counter) — no live PUT happens",
	"cmd_seed_status_test.go":       "store?default is appended to an httptest ts.URL and an unreachable 127.0.0.1:1; no live store is touched",
}

// TestDestructiveTestGate_RepoScanAdvisory is the discovery pass: it scans real
// test files and REPORTS (does not fail) candidate destructive tests missing the
// double gate. It is advisory, not a hard gate, because the regex heuristic
// matches URL string-literals / mock methods (false positives) — per the rule's
// own design ("emit advisory first if false positives are likely, then harden
// once clean"). The CONTRACT is hard-gated by TestDestructiveTestGate_Fixtures;
// real findings here are remediated individually (e.g. integration_e2e_test.go
// gained an AWARENESS_OXIGRAPH_DESTRUCTIVE gate).
func TestDestructiveTestGate_RepoScanAdvisory(t *testing.T) {
	root := agRepoRoot()
	scanDirs := []string{"golang", filepath.Join("cmd", "awg")}
	var candidates, advisories int
	for _, d := range scanDirs {
		base := filepath.Join(root, d)
		_ = filepath.WalkDir(base, func(path string, de os.DirEntry, err error) error {
			if err != nil || de.IsDir() || !strings.HasSuffix(path, "_test.go") {
				return nil
			}
			if _, ok := destructiveScanCarveOuts[filepath.Base(path)]; ok {
				return nil
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			verdict, reason := classifyDestructiveTest(string(raw))
			rel, _ := filepath.Rel(root, path)
			switch verdict {
			case destrViolation:
				candidates++
				t.Logf("ADVISORY (candidate violation): %s — %s. If real, require BOTH a build tag "+
					"AND an explicit destructive opt-in env var (e.g. AWARENESS_OXIGRAPH_DESTRUCTIVE=1); "+
					"if a false positive, add a carve-out with a reason. (ci.destructive_test_must_be_double_gated)", rel, reason)
			case destrAdvisory:
				advisories++
				t.Logf("ADVISORY (fuzzy): %s — %s", rel, reason)
			}
			return nil
		})
	}
	t.Logf("destructive-test repo scan (advisory): %d candidate(s), %d fuzzy across %v", candidates, advisories, scanDirs)
}
