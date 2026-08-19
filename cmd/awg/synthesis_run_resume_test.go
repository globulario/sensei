// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture/synthesisdriver"
)

// --resume names one boundary. "Latest" is not an input, and neither is a
// prefix: a command that resolves which history to continue by recency makes
// the invocation an incomplete record of what actually ran, and two operators
// running the same command line on different days would continue different
// sessions.
func TestResumeRefusesAnythingButAnExactDigest(t *testing.T) {
	exact := strings.Repeat("a", 64)
	if !isCheckpointDigest(exact) {
		t.Fatal("an exact 64-hex digest was refused")
	}
	// Surrounding whitespace is normalized rather than refused: it does not
	// make the value name two boundaries. Case is refused for exactly that
	// reason -- it would give one boundary two spellings.
	if !isCheckpointDigest("  " + exact + "  ") {
		t.Fatal("surrounding whitespace should be normalized, not treated as ambiguity")
	}
	for _, ambiguous := range []string{
		"",                      // "just pick one"
		"latest",                // the magic word this must not learn
		strings.Repeat("a", 63), // a near miss is still not a name
		strings.Repeat("a", 65), // nor is an overlong one
		strings.Repeat("A", 64), // canonical form only, so one boundary has one name
		strings.Repeat("g", 64), // not hex
		"checkpoints/" + exact,  // a path is not a digest
	} {
		if isCheckpointDigest(ambiguous) {
			t.Errorf("accepted %q as a checkpoint digest", ambiguous)
		}
	}
}

func assessmentFixture(digest string, detail string) synthesisdriver.ResumeAssessment {
	return synthesisdriver.ResumeAssessment{
		SchemaVersion:          synthesisdriver.ResumeAssessmentSchemaVersion,
		AssessmentID:           "assessment." + digest[:12],
		Detail:                 detail,
		AssessmentDigestSHA256: digest,
	}
}

// A refusal is evidence. It names which identity moved, and an operator
// reconstructing a stopped session needs it as much as the boundaries
// themselves -- so it must outlive the terminal it was printed on.
func TestARefusedResumePersistsItsAssessment(t *testing.T) {
	dir := t.TempDir()
	assessment := assessmentFixture(strings.Repeat("b", 64), "graph authority moved")

	if err := persistResumeAssessment(context.Background(), dir, assessment); err != nil {
		t.Fatalf("persist: %v", err)
	}
	path := filepath.Join(dir, assessment.AssessmentDigestSHA256+".resume-assessment.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the assessment was not recorded: %v", err)
	}
	var reloaded synthesisdriver.ResumeAssessment
	if err := json.Unmarshal(data, &reloaded); err != nil {
		t.Fatalf("recorded assessment is not readable: %v", err)
	}
	if reloaded.Detail != "graph authority moved" {
		t.Fatalf("the recorded assessment lost its detail: %q", reloaded.Detail)
	}
}

// Re-recording the same assessment is a no-op; recording a DIFFERENT one under
// the same identity is refused. Two different decisions cannot be the same
// decision, and quietly replacing one would erase a refusal that really
// happened.
func TestAssessmentRecordsAreAppendOnly(t *testing.T) {
	dir := t.TempDir()
	digest := strings.Repeat("c", 64)

	first := assessmentFixture(digest, "task control generation advanced")
	if err := persistResumeAssessment(context.Background(), dir, first); err != nil {
		t.Fatalf("persist: %v", err)
	}
	if err := persistResumeAssessment(context.Background(), dir, first); err != nil {
		t.Fatalf("re-recording identical evidence should be a no-op: %v", err)
	}

	forged := assessmentFixture(digest, "actually it was fine")
	if err := persistResumeAssessment(context.Background(), dir, forged); err == nil {
		t.Fatal("a different assessment overwrote an existing one under the same identity")
	}

	data, err := os.ReadFile(filepath.Join(dir, digest+".resume-assessment.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "task control generation advanced") {
		t.Fatal("the original assessment did not survive the attempted overwrite")
	}
}

// The CLI must not grow its own copy of the drift law. This is a source-level
// assertion because the failure it guards against is not a wrong answer but a
// SECOND answer: a comparison in cmd/ that disagrees with AssessResume would
// be the one no checkpoint test ever exercises.
func TestTheCLIDoesNotReimplementDriftComparison(t *testing.T) {
	for _, file := range []string{"cmd_synthesis_run.go", "synthesis_run_resume.go"} {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		source := string(data)
		for _, forbidden := range []string{
			"RefusalRepositoryDomainDrift",
			"RefusalBaseRevisionDrift",
			"RefusalWorkspaceIdentityDrift",
			"RefusalGraphAuthorityDrift",
			"RefusalTaskIdentityDrift",
			"RefusalTaskControlDrift",
			"RefusalClosureDrift",
		} {
			if strings.Contains(source, forbidden) {
				t.Errorf("%s names %s: the CLI must surface O7's verdict, not decide or re-classify it", file, forbidden)
			}
		}
	}
}
