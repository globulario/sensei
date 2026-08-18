// SPDX-License-Identifier: AGPL-3.0-only

package synthesisdriver

import (
	"strings"
	"testing"
	"time"

	"github.com/globulario/sensei/golang/architecture/synthesis"
)

func matchingBinding(checkpoint Checkpoint) ResumeBinding {
	return ResumeBinding{Current: checkpointIdentitySet(checkpoint)}
}

func assessFixture(t *testing.T, checkpoint Checkpoint, binding ResumeBinding) ResumeAssessment {
	t.Helper()
	assessment, err := AssessResume(checkpoint, binding, fixedClock())
	if err != nil {
		t.Fatalf("assessment must be produced even when it refuses: %v", err)
	}
	return assessment
}

func TestAssessResumeAllowsAnUnchangedBoundary(t *testing.T) {
	for _, phase := range ResumablePhases() {
		t.Run(string(phase), func(t *testing.T) {
			checkpoint := checkpointFixture(t, phase)
			assessment := assessFixture(t, checkpoint, matchingBinding(checkpoint))

			if !assessment.Allowed() {
				t.Fatalf("an unchanged boundary was refused: %v", assessment.Detail)
			}
			if assessment.RefusalReason != nil {
				t.Fatalf("an allowed assessment named reason %q", *assessment.RefusalReason)
			}
			if err := ValidateResumeAssessment(assessment); err != nil {
				t.Fatalf("assessment must be valid evidence: %v", err)
			}
		})
	}
}

// Each drift class is constructed independently and must produce its own exact
// reason. Collapsing any of these into a generic failure is what makes a
// refusal unactionable.
func TestAssessResumeRefusesEachDriftClassByName(t *testing.T) {
	other := strings.Repeat("9", 64)

	cases := []struct {
		name   string
		reason ResumeRefusalReason
		drift  func(*ResumeIdentitySet)
	}{
		{"repository domain", RefusalRepositoryDomainDrift, func(s *ResumeIdentitySet) { s.RepositoryDomain = "github.com/other/repo" }},
		{"base revision", RefusalBaseRevisionDrift, func(s *ResumeIdentitySet) { s.BaseRevision = "cafebabe" }},
		{"workspace identity", RefusalWorkspaceIdentityDrift, func(s *ResumeIdentitySet) { s.WorkspaceIdentityDigestSHA256 = other }},
		{"graph authority", RefusalGraphAuthorityDrift, func(s *ResumeIdentitySet) { s.GraphAuthorityDigestSHA256 = other }},
		{"task id", RefusalTaskIdentityDrift, func(s *ResumeIdentitySet) { s.TaskID = "task.other" }},
		{"task session", RefusalTaskIdentityDrift, func(s *ResumeIdentitySet) { s.TaskSessionDigestSHA256 = other }},
		{"task control state", RefusalTaskControlDrift, func(s *ResumeIdentitySet) { s.TaskControlStateDigestSHA256 = other }},
		{"task control generation", RefusalTaskControlDrift, func(s *ResumeIdentitySet) { s.TaskControlGeneration += 1 }},
		{"closure report", RefusalClosureDrift, func(s *ResumeIdentitySet) { s.ClosureReportDigestSHA256 = other }},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			checkpoint := checkpointFixture(t, synthesis.PhasePlanned)
			binding := matchingBinding(checkpoint)
			testCase.drift(&binding.Current)

			assessment := assessFixture(t, checkpoint, binding)
			if assessment.Allowed() {
				t.Fatalf("%s drift was allowed", testCase.name)
			}
			if *assessment.RefusalReason != testCase.reason {
				t.Fatalf("%s drift reported reason %q, want %q", testCase.name, *assessment.RefusalReason, testCase.reason)
			}
			// The assessment has to show what changed, not merely that
			// something did.
			if assessment.Expected == assessment.Observed {
				t.Fatal("assessment recorded identical expected and observed identities for a drift")
			}
			if err := ValidateResumeAssessment(assessment); err != nil {
				t.Fatalf("a refusal must still be valid evidence: %v", err)
			}
		})
	}
}

// An improved graph is drift exactly like a degraded one: resume claims this
// is the same execution, and a better premise is still a different premise.
func TestAssessResumeTreatsAnImprovedGraphAsDrift(t *testing.T) {
	checkpoint := checkpointFixture(t, synthesis.PhasePlanning)
	binding := matchingBinding(checkpoint)
	binding.Current.GraphAuthorityDigestSHA256 = strings.Repeat("7", 64) // a newer, richer graph

	assessment := assessFixture(t, checkpoint, binding)
	if assessment.Allowed() {
		t.Fatal("a changed graph authority was allowed because it might be better")
	}
	if *assessment.RefusalReason != RefusalGraphAuthorityDrift {
		t.Fatalf("reported %q", *assessment.RefusalReason)
	}
}

func TestAssessResumeRefusesAnInvalidCheckpoint(t *testing.T) {
	checkpoint := checkpointFixture(t, synthesis.PhasePlanning)
	checkpoint.StepsConsumed = checkpoint.MaxSteps + 1 // also breaks the declared digest

	assessment := assessFixture(t, checkpoint, matchingBinding(checkpoint))
	if assessment.Allowed() {
		t.Fatal("an invalid checkpoint was allowed to resume")
	}
	if *assessment.RefusalReason != RefusalCheckpointInvalid {
		t.Fatalf("reported %q, want checkpoint-invalid", *assessment.RefusalReason)
	}
}

// Externally supplied evaluating stays refused: O4 owns that handoff inside
// one call, so there is no durable boundary to continue from.
func TestAssessResumeRefusesNonResumablePhases(t *testing.T) {
	for _, phase := range []synthesis.Phase{synthesis.PhaseEvaluating, synthesis.PhaseSucceeded, synthesis.PhaseFailed} {
		t.Run(string(phase), func(t *testing.T) {
			checkpoint := checkpointFixture(t, synthesis.PhasePlanning)
			checkpoint.SessionState.Phase = string(phase)
			// Re-stamp the digest so the checkpoint is refused for its PHASE,
			// not merely because its digest no longer matches.
			digest, err := CheckpointDigest(checkpoint)
			if err != nil {
				t.Fatal(err)
			}
			checkpoint.CheckpointDigestSHA256 = digest

			assessment := assessFixture(t, checkpoint, matchingBinding(checkpoint))
			if assessment.Allowed() {
				t.Fatalf("phase %q was allowed to resume", phase)
			}
			reason := *assessment.RefusalReason
			if reason != RefusalCheckpointNotResumable && reason != RefusalCheckpointInvalid {
				t.Fatalf("phase %q reported %q", phase, reason)
			}
		})
	}
}

// Restart is not a budget refill: an exhausted budget refuses before any
// external call rather than starting a step that cannot complete.
func TestAssessResumeRefusesAnExhaustedStepBudget(t *testing.T) {
	checkpoint := checkpointFixture(t, synthesis.PhasePlanned)
	checkpoint.StepsConsumed = checkpoint.MaxSteps
	finalized, err := FinalizeCheckpoint(checkpoint)
	if err != nil {
		t.Fatal(err)
	}

	assessment := assessFixture(t, finalized, matchingBinding(finalized))
	if assessment.Allowed() {
		t.Fatal("a fully consumed step budget was allowed to resume")
	}
	if *assessment.RefusalReason != RefusalStepBudgetExhausted {
		t.Fatalf("reported %q, want step-budget-exhausted", *assessment.RefusalReason)
	}
}

// A self-contradictory checkpoint must be reported as a lineage problem, never
// as a clean environment change.
func TestAssessResumeRefusesContradictoryLineage(t *testing.T) {
	cases := map[string]func(*Checkpoint){
		"candidate before any attempt": func(c *Checkpoint) {
			digest := strings.Repeat("d", 64)
			c.CandidateArtifactDigestSHA256 = &digest
		},
		"evaluation evidence with no recorded attempt": func(c *Checkpoint) {
			c.Trace.EvaluationReceiptDigestsSHA256 = []string{strings.Repeat("e", 64)}
			c.SessionState.AttemptNumber = 0
		},
		"past created with no closure receipt": func(c *Checkpoint) {
			c.Trace.InterpretationClosureReceiptDigestsSHA256 = []string{}
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			checkpoint := checkpointFixture(t, synthesis.PhasePlanning)
			checkpoint.Trace.InterpretationClosureReceiptDigestsSHA256 = []string{strings.Repeat("c", 64)}
			mutate(&checkpoint)
			finalized, err := FinalizeCheckpoint(checkpoint)
			if err != nil {
				t.Fatalf("the fixture must be structurally valid so the lineage rule is what refuses it: %v", err)
			}

			assessment := assessFixture(t, finalized, matchingBinding(finalized))
			if assessment.Allowed() {
				t.Fatalf("%s was allowed", name)
			}
			if *assessment.RefusalReason != RefusalEvidenceLineageInvalid {
				t.Fatalf("%s reported %q, want evidence-lineage-invalid", name, *assessment.RefusalReason)
			}
		})
	}
}

// The assessment is evidence, so its identity must bind the comparison it
// records — including which reason it refused for.
func TestResumeAssessmentDigestBindsTheComparison(t *testing.T) {
	checkpoint := checkpointFixture(t, synthesis.PhasePlanned)
	allowed := assessFixture(t, checkpoint, matchingBinding(checkpoint))

	drifted := matchingBinding(checkpoint)
	drifted.Current.BaseRevision = "cafebabe"
	refused := assessFixture(t, checkpoint, drifted)

	if allowed.AssessmentDigestSHA256 == refused.AssessmentDigestSHA256 {
		t.Fatal("an allowed and a refused assessment of the same checkpoint share one identity")
	}

	// Observation time is not authority.
	later, err := AssessResume(checkpoint, matchingBinding(checkpoint), func() time.Time {
		return time.Date(2031, 3, 3, 3, 3, 3, 0, time.UTC)
	})
	if err != nil {
		t.Fatal(err)
	}
	if later.AssessmentDigestSHA256 != allowed.AssessmentDigestSHA256 {
		t.Fatal("observation time changed the assessment identity")
	}
}

// A document whose disposition and reason disagree is unreadable evidence.
func TestValidateResumeAssessmentRefusesContradictoryDispositions(t *testing.T) {
	checkpoint := checkpointFixture(t, synthesis.PhasePlanned)

	t.Run("allowed with a reason", func(t *testing.T) {
		assessment := assessFixture(t, checkpoint, matchingBinding(checkpoint))
		reason := RefusalClosureDrift
		assessment.RefusalReason = &reason
		if err := ValidateResumeAssessment(assessment); err == nil {
			t.Fatal("an allowed assessment carrying a refusal reason was accepted")
		}
	})

	t.Run("refused with no reason", func(t *testing.T) {
		drifted := matchingBinding(checkpoint)
		drifted.Current.TaskID = "task.other"
		assessment := assessFixture(t, checkpoint, drifted)
		assessment.RefusalReason = nil
		if err := ValidateResumeAssessment(assessment); err == nil {
			t.Fatal("a refused assessment with no reason was accepted")
		}
	})

	t.Run("unknown reason", func(t *testing.T) {
		drifted := matchingBinding(checkpoint)
		drifted.Current.TaskID = "task.other"
		assessment := assessFixture(t, checkpoint, drifted)
		unknown := ResumeRefusalReason("something-went-wrong")
		assessment.RefusalReason = &unknown
		if err := ValidateResumeAssessment(assessment); err == nil {
			t.Fatal("an unknown refusal reason was accepted")
		}
	})
}

// The vocabulary is closed: every reason must be a reason the validator
// accepts and the schema enumerates, so adding one without updating consumers
// fails here rather than in production output.
func TestResumeRefusalVocabularyIsClosed(t *testing.T) {
	checkpoint := checkpointFixture(t, synthesis.PhasePlanned)
	drifted := matchingBinding(checkpoint)
	drifted.Current.TaskID = "task.other"
	base := assessFixture(t, checkpoint, drifted)

	seen := map[ResumeRefusalReason]bool{}
	for _, reason := range ResumeRefusalReasons() {
		if seen[reason] {
			t.Fatalf("reason %q is listed twice", reason)
		}
		seen[reason] = true

		assessment := base
		value := reason
		assessment.RefusalReason = &value
		finalized, err := finalizeResumeAssessment(assessment)
		if err != nil {
			t.Fatalf("reason %q is in the vocabulary but rejected by validation: %v", reason, err)
		}
		if !validRefusalReason(*finalized.RefusalReason) {
			t.Fatalf("reason %q is not recognized by validRefusalReason", reason)
		}
	}
	if len(seen) != 11 {
		t.Fatalf("the contract requires 11 typed refusal reasons, found %d", len(seen))
	}
}
