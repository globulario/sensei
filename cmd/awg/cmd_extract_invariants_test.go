// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture"
)

func TestExtractInvariantsGuardAndRegressionTestProduceCandidate(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/inv\n")
	writeFile(t, filepath.Join(root, "state.go"), `package inv

func Apply(state string) error {
	if state == "revoked" {
		return errInvalid
	}
	return nil
}

var errInvalid error
`)
	writeFile(t, filepath.Join(root, "state_test.go"), `package inv

func TestApplyRejectsRevokedState(t *testing.T) {
	if Apply("revoked") == nil {
		t.Fatal("must reject")
	}
}
`)
	report := buildInvariantReportForTest(t, root)
	c := findInvariantCandidate(report, "transition")
	if c == nil {
		t.Fatalf("missing transition/state candidate: %#v", report.Candidates)
	}
	if c.Confidence.Score < 35 {
		t.Fatalf("candidate score too low: %#v", c.Confidence)
	}
	if c.Status != "candidate" || c.Promotion.Eligible {
		t.Fatalf("candidate promoted unexpectedly: %#v", c.Promotion)
	}
}

func TestExtractInvariantsSingleGuardStaysLowConfidence(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/inv\n")
	writeFile(t, filepath.Join(root, "state.go"), `package inv
func Apply(state string) error {
	if state == "bad" { return errInvalid }
	return nil
}
var errInvalid error
`)
	report := buildInvariantReportForTest(t, root)
	c := findInvariantCandidate(report, "state")
	if c == nil {
		c = findInvariantCandidate(report, "transition")
	}
	if c == nil {
		t.Fatal("missing isolated guard candidate")
	}
	if c.Confidence.Level != "low" {
		t.Fatalf("isolated guard level=%s want low (%#v)", c.Confidence.Level, c.Confidence)
	}
}

func TestExtractInvariantsExampleTestIsNotGeneralized(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/inv\n")
	writeFile(t, filepath.Join(root, "calc.go"), "package inv\nfunc Add(a,b int) int { return a+b }\n")
	writeFile(t, filepath.Join(root, "calc_test.go"), `package inv
func TestAddReturnsSum(t *testing.T) {
	if Add(1, 2) != 3 { t.Fatal("bad") }
}
`)
	report := buildInvariantReportForTest(t, root)
	for _, c := range report.Candidates {
		if strings.Contains(c.Statement, "Add returns sum") {
			t.Fatalf("example test generalized into candidate: %#v", c)
		}
	}
}

func TestExtractInvariantsCommentOnlyIsFactNotCandidate(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/inv\n")
	writeFile(t, filepath.Join(root, "doc.go"), `package inv
// All writes must be atomic eventually.
func Noop() {}
`)
	report := buildInvariantReportForTest(t, root)
	if !hasInvariantFact(report, "documentation_claim") {
		t.Fatal("missing documentation fact")
	}
	for _, c := range report.Candidates {
		for _, doc := range c.Evidence.Documentation {
			if doc == "doc.go" {
				t.Fatalf("comment-only evidence produced candidate: %#v", c)
			}
		}
	}
}

func TestExtractInvariantsAcceptedButUnconsumedCandidate(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/inv\n")
	writeFile(t, filepath.Join(root, "schema.go"), `package inv
type Config struct {
	Supported string `+"`json:\"supported\"`"+`
	Unused    string `+"`json:\"unused\"`"+`
}
func Use(c Config) string { return c.Supported }
`)
	report := buildInvariantReportForTest(t, root)
	var found bool
	for _, c := range report.Candidates {
		if c.Kind == "safety" && strings.Contains(c.ID, "unused") {
			found = true
		}
		if strings.Contains(c.ID, "supported") {
			t.Fatalf("consumed field reported as unconsumed: %#v", c)
		}
	}
	if !found {
		t.Fatalf("missing accepted-but-unconsumed candidate: %#v", report.Candidates)
	}
}

func TestExtractInvariantsSingleWriterWithoutCorroborationDoesNotBecomeAuthority(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/inv\n")
	writeFile(t, filepath.Join(root, "state.go"), `package inv
type State struct{ Value string }
func Write(s *State) { s.Value = "x" }
`)
	report := buildInvariantReportForTest(t, root)
	for _, c := range report.Candidates {
		if c.Kind == "authority" {
			t.Fatalf("single writer without corroboration became authority candidate: %#v", c)
		}
	}
}

func TestExtractInvariantsContradictoryWritersRemainVisible(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/inv\n")
	writeFile(t, filepath.Join(root, "state.go"), `package inv
type State struct{ Value string }
func WriteA(s *State) {
	if s == nil { return }
	s.Value = "a"
}
func WriteB(s *State) { s.Value = "b" }
`)
	writeFile(t, filepath.Join(root, "state_test.go"), `package inv
func TestValueMustOnlyChangeThroughWriter(t *testing.T) {}
`)
	report := buildInvariantReportForTest(t, root)
	c := findInvariantCandidate(report, "authority")
	if c == nil {
		t.Fatalf("missing authority candidate with contradictions: %#v", report.Candidates)
	}
	if len(c.Contradictions) == 0 {
		t.Fatalf("contradictory writers not visible: %#v", c)
	}
	if c.Confidence.Score >= 35 {
		t.Fatalf("contradiction did not reduce confidence enough: %#v", c.Confidence)
	}
}

func TestExtractInvariantsGenerationRequiresBumpAndIndependentCheck(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/inv\n")
	writeFile(t, filepath.Join(root, "gen.go"), `package inv
type State struct{ Generation int }
func Check(s State, expected int) error {
	if s.Generation != expected { return errStale }
	return nil
}
var errStale error
`)
	report := buildInvariantReportForTest(t, root)
	for _, c := range report.Candidates {
		if c.Kind == "freshness" {
			t.Fatalf("freshness candidate emitted without bump: %#v", c)
		}
	}
	writeFile(t, filepath.Join(root, "bump.go"), `package inv
func Bump(s *State) { s.Generation++ }
`)
	report = buildInvariantReportForTest(t, root)
	if findInvariantCandidate(report, "freshness") == nil {
		t.Fatalf("missing freshness candidate after bump+check: %#v", report.Candidates)
	}
}

func TestExtractInvariantsDeterministicAndDoesNotModifySource(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/inv\n")
	writeFile(t, filepath.Join(root, "state.go"), `package inv
func Apply(state string) error {
	if state == "bad" { return errInvalid }
	return nil
}
var errInvalid error
`)
	before := snapshotFiles(t, root)
	report := buildInvariantReportForTest(t, root)
	a, err := renderInvariantExtractionReport(report, "json")
	if err != nil {
		t.Fatal(err)
	}
	report = buildInvariantReportForTest(t, root)
	b, err := renderInvariantExtractionReport(report, "json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Fatal("extraction output is not deterministic")
	}
	after := snapshotFiles(t, root)
	if !equalStringMaps(before, after) {
		t.Fatal("extraction modified source files")
	}
}

func TestExtractInvariantsFactIDsRemainStable(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/inv\n")
	writeFile(t, filepath.Join(root, "state.go"), `package inv
func Apply(state string) error {
	if state == "bad" { return errInvalid }
	return nil
}
var errInvalid error
`)
	report := buildInvariantReportForTest(t, root)
	want := architecture.StableID("transition", "inv.Apply", "rejects_transition_when", `state == "bad"`, "state.go", 3, "go_guard_extractor")
	if !hasFactID(report, want) {
		t.Fatalf("missing stable fact id %s in %#v", want, report.Facts)
	}
}

func TestExtractInvariantsCandidatesRemainEquivalentAfterFactMigration(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/inv\n")
	writeFile(t, filepath.Join(root, "state.go"), `package inv
type State struct{ Value string }
func WriteA(s *State) {
	if s == nil { return }
	s.Value = "a"
}
func WriteB(s *State) { s.Value = "b" }
`)
	writeFile(t, filepath.Join(root, "state_test.go"), `package inv
func TestValueMustOnlyChangeThroughWriter(t *testing.T) {}
`)
	report := buildInvariantReportForTest(t, root)
	c := findInvariantCandidate(report, "authority")
	if c == nil {
		t.Fatalf("missing authority candidate: %#v", report.Candidates)
	}
	if c.ID != "candidate.invariant.authority.value" || c.Status != "candidate" || c.Promotion.Eligible {
		t.Fatalf("authority candidate compatibility drift: %#v", c)
	}
	if len(c.Contradictions) == 0 {
		t.Fatalf("contradiction visibility lost: %#v", c)
	}
}

func TestExtractInvariantsJSONRemainsDeterministic(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/inv\n")
	writeFile(t, filepath.Join(root, "state.go"), "package inv\nfunc Noop() {}\n")
	a, err := renderInvariantExtractionReport(buildInvariantReportForTest(t, root), "json")
	if err != nil {
		t.Fatal(err)
	}
	b, err := renderInvariantExtractionReport(buildInvariantReportForTest(t, root), "json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Fatal("json output is not deterministic")
	}
}

func TestExtractInvariantsYAMLRemainsDeterministic(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/inv\n")
	writeFile(t, filepath.Join(root, "state.go"), "package inv\nfunc Noop() {}\n")
	a, err := renderInvariantExtractionReport(buildInvariantReportForTest(t, root), "yaml")
	if err != nil {
		t.Fatal(err)
	}
	b, err := renderInvariantExtractionReport(buildInvariantReportForTest(t, root), "yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Fatal("yaml output is not deterministic")
	}
}

func TestGoArchitectureStillUsesSingleASTPass(t *testing.T) {
	raw, err := os.ReadFile("cmd_extract_invariants.go")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(raw), "parser.ParseFile"); got != 1 {
		t.Fatalf("extractGoArchitecture should keep one parser.ParseFile call, got %d", got)
	}
	if !strings.Contains(string(raw), "scanAuthorityDeclsAndFacts") {
		t.Fatal("single AST pass no longer feeds authority observation extraction")
	}
}

func TestCandidateFactsRemainNonAuthoritative(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/inv\n")
	writeFile(t, filepath.Join(root, "server.go"), `package inv
func SaveConfig() { setConfig("x", "y") }
`)
	report := buildInvariantReportForTest(t, root)
	for _, f := range report.Facts {
		if strings.Contains(f.Predicate, "owns_state") || strings.Contains(f.Predicate, "is_authoritative") || f.Kind == "contract" || f.Kind == "invariant" {
			t.Fatalf("fact became authoritative: %#v", f)
		}
	}
}

func TestHistoryNotRequestedIsNotALimitation(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/inv\n")
	writeFile(t, filepath.Join(root, "state.go"), "package inv\n")
	report := buildInvariantReportForTest(t, root)
	if len(report.Limitations) != 0 {
		t.Fatalf("limitations = %#v, want none", report.Limitations)
	}
}

func TestHistoryRequestedOutsideGitIsReported(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/inv\n")
	writeFile(t, filepath.Join(root, "state.go"), "package inv\n")
	opts := invariantExtractOptions{Repo: root, Format: "json", IncludeDocs: true, IncludeTests: true, IncludeHistory: true, MinimumConfidence: "low"}
	report, err := buildInvariantExtractionReport(root, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Limitations) == 0 || report.Limitations[0].Scope != "git_history" {
		t.Fatalf("missing git history limitation: %#v", report.Limitations)
	}
}

func TestUnreadableRequestedSourceIsNotSilentlyIgnored(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/inv\n")
	writeFile(t, filepath.Join(root, "broken.go"), "package inv\nfunc Broken(\n")
	if _, err := buildInvariantExtractionReport(root, invariantExtractOptions{Repo: root, Format: "json", IncludeDocs: true, IncludeTests: true, MinimumConfidence: "low"}); err == nil {
		t.Fatal("expected parse error for requested unreadable/uninspectable source")
	}
}

func TestExtractInvariantsMutationAnalysisUsesIsolatedTemp(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/inv\n")
	writeFile(t, filepath.Join(root, "state.go"), "package inv\n")
	opts := invariantExtractOptions{Repo: root, Format: "json", IncludeDocs: true, IncludeTests: true, IncludeMutationAnalysis: true}
	report, err := buildInvariantExtractionReport(root, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !report.MutationAnalysis.Enabled || report.MutationAnalysis.WorkDir == "" {
		t.Fatalf("mutation analysis did not allocate isolated workspace: %#v", report.MutationAnalysis)
	}
	if strings.HasPrefix(report.MutationAnalysis.WorkDir, root) {
		t.Fatalf("mutation workspace is inside source repo: %s", report.MutationAnalysis.WorkDir)
	}
}

func buildInvariantReportForTest(t *testing.T, root string) invariantExtractionReport {
	t.Helper()
	opts := invariantExtractOptions{Repo: root, Format: "json", IncludeDocs: true, IncludeTests: true, MinimumConfidence: "low"}
	report, err := buildInvariantExtractionReport(root, opts)
	if err != nil {
		t.Fatalf("buildInvariantExtractionReport: %v", err)
	}
	return report
}

func hasFactID(report invariantExtractionReport, id string) bool {
	for _, f := range report.Facts {
		if f.ID == id {
			return true
		}
	}
	return false
}

func findInvariantCandidate(report invariantExtractionReport, kind string) *extractedInvariantCandidate {
	for i := range report.Candidates {
		if report.Candidates[i].Kind == kind {
			return &report.Candidates[i]
		}
	}
	return nil
}

func hasInvariantFact(report invariantExtractionReport, kind string) bool {
	for _, f := range report.Facts {
		if f.Kind == kind {
			return true
		}
	}
	return false
}

func snapshotFiles(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		out[filepath.ToSlash(rel)] = string(raw)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func equalStringMaps(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, av := range a {
		if b[k] != av {
			return false
		}
	}
	return true
}
