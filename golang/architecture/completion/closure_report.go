// SPDX-License-Identifier: AGPL-3.0-only

package completion

import (
	"sort"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/questionresolution"
)

const phase8ReportSchemaVersion = "completion.phase8_closure_report/v1"

// Phase8Owner names one accepted Phase-8 component owner by its concern and durable
// identity (schema version / authority grant), so the report references the exact
// component identities rather than a prose claim.
type Phase8Owner struct {
	Name     string `json:"name" yaml:"name"`
	Concern  string `json:"concern" yaml:"concern"`
	Identity string `json:"identity" yaml:"identity"`
}

// Phase8ClosureReport is a deterministic attestation that the Phase-8 completion
// architecture is implemented and proven. It is EVIDENCE ABOUT THE IMPLEMENTATION —
// not a task-completion fact — and must never be used as runtime completion
// authority. Its DigestSHA256 is a self-excluding content address; no wall clock is
// read, so it is byte-identical across builds of the same code.
type Phase8ClosureReport struct {
	SchemaVersion      string        `json:"schema_version" yaml:"schema_version"`
	Owners             []Phase8Owner `json:"owners" yaml:"owners"`
	TerminalStates     []string      `json:"terminal_states" yaml:"terminal_states"`
	CompletionOutcomes []string      `json:"completion_outcomes" yaml:"completion_outcomes"`
	RecoveryOutcomes   []string      `json:"recovery_outcomes" yaml:"recovery_outcomes"`
	ClosureVerdicts    []string      `json:"closure_verdicts" yaml:"closure_verdicts"`
	// Distinctions are the three claims Phase-8 closure must never collapse.
	Distinctions []string `json:"distinctions" yaml:"distinctions"`
	Bound        []string `json:"bound" yaml:"bound"`
	DigestSHA256 string   `json:"digest_sha256,omitempty" yaml:"digest_sha256,omitempty"`
}

// BuildPhase8ClosureReport returns the deterministic Phase-8 closure report.
func BuildPhase8ClosureReport() Phase8ClosureReport {
	owners := []Phase8Owner{
		{Name: "phase6_correctness_certification", Concern: "correctness", Identity: "closureprotocol.CertificationReceipt(verdict=certified|certified_with_conditions)"},
		{Name: "question_resolution_certification", Concern: "closed current question loop", Identity: questionresolution.CertificateSchemaVersion + " / " + questionresolution.GrantCertification},
		{Name: "readiness_assessment", Concern: "readiness conjunction", Identity: ReadinessSchemaVersion},
		{Name: "terminal_completion", Concern: "authoritative completion mutation", Identity: TerminalReceiptSchemaVersion + " / " + GrantTerminalCompletion},
		{Name: "terminal_reconstruction_and_recovery", Concern: "durable reconstruction + projection recovery", Identity: inspectSchemaVersion},
		{Name: "completion_closure_integration", Concern: "end-to-end composition", Identity: closureAssessmentSchemaVersion},
	}
	sort.Slice(owners, func(i, j int) bool { return owners[i].Name < owners[j].Name })

	terminalStates := make([]string, 0, len(AssessmentBoundStates()))
	for _, s := range AssessmentBoundStates() {
		terminalStates = append(terminalStates, string(s))
	}
	report := Phase8ClosureReport{
		SchemaVersion:  phase8ReportSchemaVersion,
		Owners:         owners,
		TerminalStates: terminalStates,
		CompletionOutcomes: sortedStrings([]string{
			string(OutcomeCommitted), string(OutcomeExactReplay), string(OutcomeNotReady),
			string(OutcomeStaleExpectedHead), string(OutcomeAuthorityRefusal), string(OutcomeIntegrityFailure),
			string(OutcomeConflictingCompletion), string(OutcomeLedgerInvalid), string(OutcomeInputInvalid),
		}),
		RecoveryOutcomes: sortedStrings([]string{
			string(RecoverProjectionsRebuilt), string(RecoverAlreadyCurrent), string(RecoverNothingToRecover),
			string(RecoverContradictory), string(RecoverBrokenCompletion), string(RecoverUnsupported), string(RecoverInputInvalid),
		}),
		ClosureVerdicts: sortedStrings([]string{
			string(ClosureAuthoritativeCompletion), string(ClosureNotCompleted), string(ClosureBroken),
			string(ClosureContradictory), string(ClosureUnsupported),
		}),
		Distinctions: []string{
			"Phase-8 implementation closure: the completion architecture — readiness assessment, terminal-completion mutation, durable-conjunction verification, terminal reconstruction, and projection recovery — is implemented and proven by the referenced tests. It is evidence about the implementation, not a runtime fact.",
			"Completion of one task: a single task is terminally completed only when the completion owner produces the unique completed-event + matching-receipt conjunction for that task's current result. This report is not that fact and confers no completion.",
			"Repository-wide perfection: this report makes no claim about the correctness of code outside the tested owner surfaces, nor about the combined embedded seed, nor about any other repository property. It is neither a warranty nor a global certification.",
		},
		Bound: []string{
			"this report is Phase-8 implementation evidence; it is not a task-completion fact and must never be used as runtime completion authority",
			"implementation closure, completion of one task, and repository-wide perfection are three distinct claims and are never collapsed into one",
		},
	}
	report.DigestSHA256 = ""
	if d, err := closureprotocol.SemanticDigest(report); err == nil {
		report.DigestSHA256 = d
	}
	return report
}

func sortedStrings(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}
