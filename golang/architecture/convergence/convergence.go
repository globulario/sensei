// SPDX-License-Identifier: Apache-2.0

package convergence

import (
	"bytes"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/closure"
	"github.com/globulario/sensei/golang/architecture/graphsnapshot"
	"github.com/globulario/sensei/golang/architecture/maintenance"
	"github.com/globulario/sensei/golang/architecture/plane"
	"github.com/globulario/sensei/golang/architecture/probe"
	"github.com/globulario/sensei/golang/architecture/questiongen"
	"gopkg.in/yaml.v3"
)

const (
	SchemaVersion = "1"
	GeneratedBy   = "sensei advance-convergence"

	PolicyStrictV1 = "convergence.strict.v1"

	StatusClosed              = "closed"
	StatusConditionallyClosed = "conditionally_closed"
	StatusWaiting             = "waiting"
	StatusStalled             = "stalled"
	StatusOscillating         = "oscillating"
	StatusBudgetExhausted     = "budget_exhausted"
	StatusUncertifiable       = "uncertifiable"

	ProgressInitial           = "initial"
	ProgressClosureProgress   = "closure_progress"
	ProgressEpistemicProgress = "epistemic_progress"
	ProgressMixedProgress     = "mixed_progress"
	ProgressNoProgress        = "no_progress"
	ProgressRegression        = "regression"

	WaitArchitect        = "architect"
	WaitEvidence         = "evidence"
	WaitGovernance       = "governance"
	WaitMechanicalRepair = "mechanical_repair"

	StageProduced              = "produced"
	StageSkippedUncertifiable  = "skipped_uncertifiable"
	DispositionAdvanced        = "advanced"
	DispositionReplay          = "replay_no_new_iteration"
	DispositionBudgetExhausted = "budget_exhausted"
)

type Policy struct {
	ID                         string   `json:"id" yaml:"id"`
	Version                    string   `json:"version" yaml:"version"`
	MaxIterations              int      `json:"max_iterations" yaml:"max_iterations"`
	NoEffectInputLimit         int      `json:"no_effect_input_limit" yaml:"no_effect_input_limit"`
	OscillationWindow          int      `json:"oscillation_window" yaml:"oscillation_window"`
	MaxRepeatedBlockerRounds   int      `json:"max_repeated_blocker_rounds" yaml:"max_repeated_blocker_rounds"`
	ConditionalClosureTerminal bool     `json:"conditional_closure_terminal" yaml:"conditional_closure_terminal"`
	KnownLimitations           []string `json:"known_limitations,omitempty" yaml:"known_limitations,omitempty"`
}

func DefaultPolicies() ([]Policy, error) {
	return []Policy{{
		ID:                         PolicyStrictV1,
		Version:                    "v1",
		MaxIterations:              12,
		NoEffectInputLimit:         2,
		OscillationWindow:          6,
		MaxRepeatedBlockerRounds:   3,
		ConditionalClosureTerminal: true,
	}}, nil
}

func PolicyByID(id string) (Policy, bool) {
	for _, p := range mustPolicies() {
		if p.ID == strings.TrimSpace(id) {
			return p, true
		}
	}
	return Policy{}, false
}

func mustPolicies() []Policy {
	p, err := DefaultPolicies()
	if err != nil {
		panic(err)
	}
	return p
}

type InputPaths struct {
	ClosureRequest string
	Claims         string
	Dialogue       string
	EvidenceState  string
	GraphNT        string
	RepositoryRoot string
	ExistingProbes string
}

type Options struct {
	Paths             InputPaths
	QuestionCreatedAt string
	PolicyID          string
	Session           *Session
}

type Result struct {
	Disposition string
	Session     Session
	Iteration   *Iteration
	Bundle      Bundle
	Report      StatusReport
}

type Session struct {
	SchemaVersion                 string                            `json:"schema_version" yaml:"schema_version"`
	GeneratedBy                   string                            `json:"generated_by" yaml:"generated_by"`
	SessionID                     string                            `json:"session_id" yaml:"session_id"`
	PolicyID                      string                            `json:"policy_id" yaml:"policy_id"`
	PolicyVersion                 string                            `json:"policy_version" yaml:"policy_version"`
	Binding                       architecture.ClaimDocumentBinding `json:"binding" yaml:"binding"`
	ClosureRequestDigestSHA256    string                            `json:"closure_request_digest_sha256" yaml:"closure_request_digest_sha256"`
	ScopeDigestSHA256             string                            `json:"scope_digest_sha256" yaml:"scope_digest_sha256"`
	LatestStatus                  string                            `json:"latest_status" yaml:"latest_status"`
	LatestWaitClasses             []string                          `json:"latest_wait_classes,omitempty" yaml:"latest_wait_classes,omitempty"`
	ConsecutiveNoEffectIterations int                               `json:"consecutive_no_effect_iterations,omitempty" yaml:"consecutive_no_effect_iterations,omitempty"`
	Iterations                    []Iteration                       `json:"iterations" yaml:"iterations"`
}

type Iteration struct {
	Index                         int                       `json:"index" yaml:"index"`
	PreviousIterationDigestSHA256 string                    `json:"previous_iteration_digest_sha256" yaml:"previous_iteration_digest_sha256"`
	IterationDigestSHA256         string                    `json:"iteration_digest_sha256" yaml:"iteration_digest_sha256"`
	InputManifestDigestSHA256     string                    `json:"input_manifest_digest_sha256" yaml:"input_manifest_digest_sha256"`
	SemanticInputDigestSHA256     string                    `json:"semantic_input_digest_sha256" yaml:"semantic_input_digest_sha256"`
	SemanticStateDigestSHA256     string                    `json:"semantic_state_digest_sha256" yaml:"semantic_state_digest_sha256"`
	Status                        string                    `json:"status" yaml:"status"`
	ProgressStatus                string                    `json:"progress_status" yaml:"progress_status"`
	ClosureVerdict                string                    `json:"closure_verdict" yaml:"closure_verdict"`
	WaitClasses                   []string                  `json:"wait_classes,omitempty" yaml:"wait_classes,omitempty"`
	StageReceipts                 []StageReceipt            `json:"stage_receipts" yaml:"stage_receipts"`
	Changes                       ChangeSet                 `json:"changes" yaml:"changes"`
	RepeatedBlockers              []RepeatedBlocker         `json:"repeated_blockers,omitempty" yaml:"repeated_blockers,omitempty"`
	Oscillation                   *Oscillation              `json:"oscillation,omitempty" yaml:"oscillation,omitempty"`
	NoEffectInput                 bool                      `json:"no_effect_input,omitempty" yaml:"no_effect_input,omitempty"`
	NoEffectReason                string                    `json:"no_effect_reason,omitempty" yaml:"no_effect_reason,omitempty"`
	NextActions                   []NextAction              `json:"next_actions,omitempty" yaml:"next_actions,omitempty"`
	Limitations                   []architecture.Limitation `json:"limitations,omitempty" yaml:"limitations,omitempty"`
}

type InputManifest struct {
	Binding                        architecture.ClaimDocumentBinding `json:"binding" yaml:"binding"`
	ClosureRequestDigestSHA256     string                            `json:"closure_request_digest_sha256" yaml:"closure_request_digest_sha256"`
	ClaimsDigestSHA256             string                            `json:"claims_digest_sha256" yaml:"claims_digest_sha256"`
	DialogueDigestSHA256           string                            `json:"dialogue_digest_sha256" yaml:"dialogue_digest_sha256"`
	EvidenceStateDigestSHA256      string                            `json:"evidence_state_digest_sha256" yaml:"evidence_state_digest_sha256"`
	GraphSnapshotDigestSHA256      string                            `json:"graph_snapshot_digest_sha256" yaml:"graph_snapshot_digest_sha256"`
	ExistingProbesDigestSHA256     string                            `json:"existing_probes_digest_sha256" yaml:"existing_probes_digest_sha256"`
	QuestionCreatedAt              string                            `json:"question_created_at" yaml:"question_created_at"`
	SemanticInputDigestSHA256      string                            `json:"semantic_input_digest_sha256" yaml:"semantic_input_digest_sha256"`
	RepositoryRevisionSHA          string                            `json:"repository_revision_sha" yaml:"repository_revision_sha"`
	RepositoryRevisionVerified     bool                              `json:"repository_revision_verified" yaml:"repository_revision_verified"`
	GraphSnapshotDigestVerified    bool                              `json:"graph_snapshot_digest_verified" yaml:"graph_snapshot_digest_verified"`
	OptionalProbeDocumentWasAbsent bool                              `json:"optional_probe_document_was_absent" yaml:"optional_probe_document_was_absent"`
}

type StageReceipt struct {
	Stage                string                    `json:"stage" yaml:"stage"`
	Disposition          string                    `json:"disposition" yaml:"disposition"`
	ArtifactPath         string                    `json:"artifact_path" yaml:"artifact_path"`
	DigestSHA256         string                    `json:"digest_sha256" yaml:"digest_sha256"`
	SemanticDigestSHA256 string                    `json:"semantic_digest_sha256" yaml:"semantic_digest_sha256"`
	InputDigests         []DigestReceipt           `json:"input_digests,omitempty" yaml:"input_digests,omitempty"`
	Limitations          []architecture.Limitation `json:"limitations,omitempty" yaml:"limitations,omitempty"`
}

type DigestReceipt struct {
	Name         string `json:"name" yaml:"name"`
	DigestSHA256 string `json:"digest_sha256" yaml:"digest_sha256"`
}

type EntityChanges struct {
	Added     []string `json:"added,omitempty" yaml:"added,omitempty"`
	Removed   []string `json:"removed,omitempty" yaml:"removed,omitempty"`
	Changed   []string `json:"changed,omitempty" yaml:"changed,omitempty"`
	Improved  []string `json:"improved,omitempty" yaml:"improved,omitempty"`
	Regressed []string `json:"regressed,omitempty" yaml:"regressed,omitempty"`
}

type ChangeSet struct {
	Claims     EntityChanges `json:"claims" yaml:"claims"`
	Dimensions EntityChanges `json:"dimensions" yaml:"dimensions"`
	Blockers   EntityChanges `json:"blockers" yaml:"blockers"`
	Questions  EntityChanges `json:"questions" yaml:"questions"`
	Probes     EntityChanges `json:"probes" yaml:"probes"`
	Evidence   EntityChanges `json:"evidence" yaml:"evidence"`
	Reasons    []string      `json:"reasons,omitempty" yaml:"reasons,omitempty"`
}

type RepeatedBlocker struct {
	BlockerID         string `json:"blocker_id" yaml:"blocker_id"`
	FirstIteration    int    `json:"first_iteration" yaml:"first_iteration"`
	LatestIteration   int    `json:"latest_iteration" yaml:"latest_iteration"`
	ConsecutiveRounds int    `json:"consecutive_rounds" yaml:"consecutive_rounds"`
	Severity          string `json:"severity,omitempty" yaml:"severity,omitempty"`
	NextActionClass   string `json:"next_action_class,omitempty" yaml:"next_action_class,omitempty"`
}

type Oscillation struct {
	StartIteration      int      `json:"start_iteration" yaml:"start_iteration"`
	EndIteration        int      `json:"end_iteration" yaml:"end_iteration"`
	SemanticDigest      string   `json:"semantic_digest" yaml:"semantic_digest"`
	IntermediateDigests []string `json:"intermediate_digests,omitempty" yaml:"intermediate_digests,omitempty"`
}

type NextAction struct {
	Class     string `json:"class" yaml:"class"`
	Priority  string `json:"priority" yaml:"priority"`
	Reference string `json:"reference" yaml:"reference"`
	Summary   string `json:"summary" yaml:"summary"`
}

type StatusReport struct {
	SessionID        string       `json:"session_id" yaml:"session_id"`
	Iteration        int          `json:"iteration" yaml:"iteration"`
	MaxIterations    int          `json:"max_iterations" yaml:"max_iterations"`
	Status           string       `json:"status" yaml:"status"`
	ClosureVerdict   string       `json:"closure_verdict" yaml:"closure_verdict"`
	ProgressStatus   string       `json:"progress_status" yaml:"progress_status"`
	WaitClasses      []string     `json:"wait_classes,omitempty" yaml:"wait_classes,omitempty"`
	CriticalBlockers int          `json:"critical_blockers" yaml:"critical_blockers"`
	RepeatedBlockers int          `json:"repeated_blockers" yaml:"repeated_blockers"`
	NextActions      []NextAction `json:"next_actions,omitempty" yaml:"next_actions,omitempty"`
	Disposition      string       `json:"disposition,omitempty" yaml:"disposition,omitempty"`
}

type Bundle struct {
	Files map[string][]byte
}

type loadedInputs struct {
	Request        closure.Request
	Claims         architecture.ClaimDocument
	Dialogue       architecture.DialogueDocument
	Evidence       maintenance.EvidenceStateDocument
	ExistingProbe  *probe.ProbeDocument
	Manifest       InputManifest
	Policy         Policy
	RepoRevision   string
	GraphReceipt   graphsnapshot.Receipt
	GraphPath      string
	RepositoryRoot string
}

type stageOutputs struct {
	MaintainedClaims     architecture.ClaimDocument
	MaintenanceReport    maintenance.Report
	PlaneReport          plane.Report
	ClosureBefore        closure.Report
	Dialogue             architecture.DialogueDocument
	QuestionReport       questiongen.Report
	ClosureAfter         closure.Report
	ProbeDocument        probe.ProbeDocument
	ProbeReport          probe.GenerationReport
	Bytes                map[string][]byte
	Receipts             []StageReceipt
	SemanticStateDigest  string
	Changes              ChangeSet
	WaitClasses          []string
	NextActions          []NextAction
	RepeatedBlockers     []RepeatedBlocker
	Limitations          []architecture.Limitation
	CriticalBlockerCount int
}

func Advance(opts Options) (Result, error) {
	policyID := strings.TrimSpace(opts.PolicyID)
	if policyID == "" {
		policyID = PolicyStrictV1
	}
	policy, ok := PolicyByID(policyID)
	if !ok {
		return Result{}, fmt.Errorf("unknown convergence policy %s", policyID)
	}
	inputs, err := loadInputs(opts, policy)
	if err != nil {
		return Result{}, err
	}
	session, err := prepareSession(opts.Session, inputs, policy)
	if err != nil {
		return Result{}, err
	}
	if len(session.Iterations) > 0 {
		prev := session.Iterations[len(session.Iterations)-1]
		if prev.SemanticInputDigestSHA256 == inputs.Manifest.SemanticInputDigestSHA256 {
			return Result{Disposition: DispositionReplay, Session: session, Report: reportFromSession(session, policy, DispositionReplay)}, nil
		}
		if len(session.Iterations) >= policy.MaxIterations {
			return Result{Disposition: DispositionBudgetExhausted, Session: session, Report: budgetReport(session, policy)}, nil
		}
	}

	stages, err := runStages(inputs)
	if err != nil {
		return Result{}, err
	}
	prev := (*Iteration)(nil)
	if len(session.Iterations) > 0 {
		prev = &session.Iterations[len(session.Iterations)-1]
	}
	iter := buildIteration(session, inputs, stages, policy, prev)
	session.Iterations = append(session.Iterations, iter)
	session.LatestStatus = iter.Status
	session.LatestWaitClasses = iter.WaitClasses
	session.ConsecutiveNoEffectIterations = consecutiveNoEffect(session.Iterations)
	if err := ValidateSession(session); err != nil {
		return Result{}, err
	}
	bundle, err := RenderBundle(session, inputs.Manifest, iter, stages.Bytes)
	if err != nil {
		return Result{}, err
	}
	return Result{Disposition: DispositionAdvanced, Session: session, Iteration: &iter, Bundle: bundle, Report: reportFromIteration(session, policy, iter, DispositionAdvanced, stages.CriticalBlockerCount)}, nil
}

func loadInputs(opts Options, policy Policy) (loadedInputs, error) {
	if opts.Paths.ClosureRequest == "" || opts.Paths.Claims == "" || opts.Paths.Dialogue == "" || opts.Paths.EvidenceState == "" || opts.Paths.GraphNT == "" || opts.Paths.RepositoryRoot == "" {
		return loadedInputs{}, errors.New("closure request, claims, dialogue, evidence state, graph snapshot, and repository root are required")
	}
	if strings.TrimSpace(opts.QuestionCreatedAt) == "" {
		return loadedInputs{}, errors.New("question_created_at is required")
	}
	req, err := closure.LoadRequest(opts.Paths.ClosureRequest)
	if err != nil {
		return loadedInputs{}, err
	}
	claims, err := architecture.LoadClaimDocument(opts.Paths.Claims)
	if err != nil {
		return loadedInputs{}, err
	}
	dialogue, err := architecture.LoadDialogueDocument(opts.Paths.Dialogue)
	if err != nil {
		return loadedInputs{}, err
	}
	evidence, err := maintenance.LoadEvidenceStateDocument(opts.Paths.EvidenceState)
	if err != nil {
		return loadedInputs{}, err
	}
	if err := requireResolvedBinding(req.Binding); err != nil {
		return loadedInputs{}, err
	}
	for name, b := range map[string]architecture.ClaimDocumentBinding{
		"claims":         claims.Binding,
		"dialogue":       dialogue.Binding,
		"evidence_state": evidence.Binding,
	} {
		if !bindingsEqual(req.Binding, b) {
			return loadedInputs{}, fmt.Errorf("%s binding does not match closure request", name)
		}
	}
	var existing *probe.ProbeDocument
	probeDigest := emptyDigest()
	probeAbsent := true
	if strings.TrimSpace(opts.Paths.ExistingProbes) != "" {
		doc, err := probe.LoadDocument(opts.Paths.ExistingProbes, nil)
		if err != nil {
			return loadedInputs{}, err
		}
		if !bindingsEqual(req.Binding, doc.Binding) {
			return loadedInputs{}, errors.New("probe document binding does not match closure request")
		}
		data, err := probe.MarshalCanonicalDocumentYAML(doc, nil)
		if err != nil {
			return loadedInputs{}, err
		}
		probeDigest = Digest(data)
		existing = &doc
		probeAbsent = false
	}
	graphReceipt, err := graphsnapshot.Verify(opts.Paths.GraphNT, req.Binding.GraphDigestSHA256, req.Binding.GraphDigestStatus)
	if err != nil {
		return loadedInputs{}, err
	}
	if graphReceipt.DigestSHA256 != req.Binding.GraphDigestSHA256 {
		return loadedInputs{}, errors.New("graph snapshot digest does not match closure request binding")
	}
	repoRevision, err := repositoryRevision(opts.Paths.RepositoryRoot)
	if err != nil {
		return loadedInputs{}, err
	}
	if repoRevision != req.Binding.Revision {
		return loadedInputs{}, fmt.Errorf("repository revision %s does not match fixed binding %s", repoRevision, req.Binding.Revision)
	}
	requestBytes, err := closure.MarshalCanonicalRequestYAML(req)
	if err != nil {
		return loadedInputs{}, err
	}
	claimBytes, err := architecture.MarshalCanonicalClaimDocumentYAML(claims)
	if err != nil {
		return loadedInputs{}, err
	}
	dialogueBytes, err := architecture.MarshalCanonicalDialogueDocumentYAML(dialogue)
	if err != nil {
		return loadedInputs{}, err
	}
	evidenceBytes, err := maintenance.MarshalCanonicalEvidenceStateYAML(evidence)
	if err != nil {
		return loadedInputs{}, err
	}
	manifest := InputManifest{
		Binding:                        req.Binding,
		ClosureRequestDigestSHA256:     Digest(requestBytes),
		ClaimsDigestSHA256:             Digest(claimBytes),
		DialogueDigestSHA256:           Digest(dialogueBytes),
		EvidenceStateDigestSHA256:      Digest(evidenceBytes),
		GraphSnapshotDigestSHA256:      graphReceipt.DigestSHA256,
		ExistingProbesDigestSHA256:     probeDigest,
		QuestionCreatedAt:              strings.TrimSpace(opts.QuestionCreatedAt),
		RepositoryRevisionSHA:          repoRevision,
		RepositoryRevisionVerified:     true,
		GraphSnapshotDigestVerified:    true,
		OptionalProbeDocumentWasAbsent: probeAbsent,
	}
	manifest.SemanticInputDigestSHA256 = semanticInputDigest(manifest)
	return loadedInputs{Request: req, Claims: claims, Dialogue: dialogue, Evidence: evidence, ExistingProbe: existing, Manifest: manifest, Policy: policy, RepoRevision: repoRevision, GraphReceipt: graphReceipt, GraphPath: opts.Paths.GraphNT, RepositoryRoot: opts.Paths.RepositoryRoot}, nil
}

func prepareSession(existing *Session, inputs loadedInputs, policy Policy) (Session, error) {
	scopeDigest := Digest(canonicalJSON(inputs.Request.Scope))
	sessionID := StableSessionID(inputs.Request, policy)
	if existing == nil {
		return Session{
			SchemaVersion:              SchemaVersion,
			GeneratedBy:                GeneratedBy,
			SessionID:                  sessionID,
			PolicyID:                   policy.ID,
			PolicyVersion:              policy.Version,
			Binding:                    inputs.Request.Binding,
			ClosureRequestDigestSHA256: inputs.Manifest.ClosureRequestDigestSHA256,
			ScopeDigestSHA256:          scopeDigest,
			LatestStatus:               StatusWaiting,
			Iterations:                 []Iteration{},
		}, nil
	}
	s := normalizeSession(*existing)
	if err := ValidateSession(s); err != nil {
		return Session{}, err
	}
	if s.SessionID != sessionID {
		return Session{}, errors.New("existing session identity does not match fixed request")
	}
	if s.PolicyID != policy.ID || s.PolicyVersion != policy.Version {
		return Session{}, errors.New("existing session policy does not match requested policy")
	}
	if !bindingsEqual(s.Binding, inputs.Request.Binding) {
		return Session{}, errors.New("existing session binding does not match closure request")
	}
	if s.ClosureRequestDigestSHA256 != inputs.Manifest.ClosureRequestDigestSHA256 || s.ScopeDigestSHA256 != scopeDigest {
		return Session{}, errors.New("existing session closure request does not match")
	}
	return s, nil
}

func runStages(inputs loadedInputs) (stageOutputs, error) {
	out := stageOutputs{Bytes: map[string][]byte{}}
	graphDigest := inputs.Manifest.GraphSnapshotDigestSHA256
	maint, err := maintenance.Evaluate(maintenance.Context{
		RepositoryRoot: filepath.Clean(inputs.RepositoryRoot),
		Current:        inputs.Claims,
		Dialogue:       &inputs.Dialogue,
		Evidence:       &inputs.Evidence,
		ObservedBinding: architecture.ClaimDocumentBinding{
			RepositoryDomain:  inputs.Request.Binding.RepositoryDomain,
			Revision:          inputs.Request.Binding.Revision,
			RevisionStatus:    inputs.Request.Binding.RevisionStatus,
			GraphDigestSHA256: graphDigest,
			GraphDigestStatus: inputs.Request.Binding.GraphDigestStatus,
		},
	})
	if err != nil {
		return out, err
	}
	out.MaintainedClaims = maint.Document
	out.MaintenanceReport = maint.Report
	out.Bytes["maintained-claims.yaml"], err = architecture.MarshalCanonicalClaimDocumentYAML(maint.Document)
	if err != nil {
		return out, err
	}
	out.Bytes["maintenance-report.yaml"], err = maintenance.MarshalCanonicalReportYAML(maint.Report)
	if err != nil {
		return out, err
	}
	out.Receipts = append(out.Receipts,
		stageReceipt("maintain_claims.document", "maintained-claims.yaml", out.Bytes["maintained-claims.yaml"], inputs.Manifest),
		stageReceipt("maintain_claims.report", "maintenance-report.yaml", out.Bytes["maintenance-report.yaml"], inputs.Manifest),
	)

	planeGraph, err := plane.LoadGraphIndex(inputs.GraphPath)
	if err != nil {
		return out, err
	}
	planeReport, err := plane.Assess(plane.Context{
		Claims:              maint.Document,
		Maintenance:         &maint.Report,
		Graph:               planeGraph,
		Evidence:            &inputs.Evidence,
		Dialogue:            &inputs.Dialogue,
		GraphSnapshotPath:   inputs.GraphPath,
		GraphDigest:         graphDigest,
		GraphDigestStatus:   inputs.Request.Binding.GraphDigestStatus,
		GraphDigestVerified: true,
	})
	if err != nil {
		return out, err
	}
	out.PlaneReport = planeReport
	out.Bytes["plane-assessment.yaml"], err = plane.MarshalCanonicalReportYAML(planeReport)
	if err != nil {
		return out, err
	}
	out.Receipts = append(out.Receipts, stageReceipt("assess_planes", "plane-assessment.yaml", out.Bytes["plane-assessment.yaml"], inputs.Manifest))

	closureGraph, err := closure.LoadGraphIndex(inputs.GraphPath)
	if err != nil {
		return out, err
	}
	before, err := closure.Evaluate(closure.Context{
		Request: inputs.Request, Claims: maint.Document, Maintenance: &maint.Report, Plane: &planeReport, Dialogue: &inputs.Dialogue, Evidence: &inputs.Evidence,
		Graph: closureGraph, GraphReceipt: inputs.GraphReceipt, RepositoryRoot: inputs.RepositoryRoot, RepositoryRev: inputs.RepoRevision, RepositoryStatus: architecture.RevisionResolved,
	})
	if err != nil {
		return out, err
	}
	out.ClosureBefore = before
	out.Bytes["closure-before-dialogue.yaml"], err = closure.MarshalCanonicalReportYAML(before)
	if err != nil {
		return out, err
	}
	out.Receipts = append(out.Receipts, stageReceipt("assess_closure_before_dialogue", "closure-before-dialogue.yaml", out.Bytes["closure-before-dialogue.yaml"], inputs.Manifest))

	dialogue := inputs.Dialogue
	var questionReport questiongen.Report
	if before.Verdict == closure.VerdictUncertifiable {
		questionReport = questiongen.Report{SchemaVersion: questiongen.SchemaVersion, GeneratedBy: questiongen.GeneratedBy, Binding: inputs.Request.Binding, Limitations: []architecture.Limitation{{Source: "closure-before-dialogue", Scope: "question_generation", Reason: "source closure assessment is uncertifiable", Blocking: false}}}
	} else {
		qr, err := questiongen.Generate(questiongen.Context{
			Closure:                       before,
			Claims:                        maint.Document,
			Graph:                         closureGraph,
			Existing:                      &inputs.Dialogue,
			CreatedAt:                     inputs.Manifest.QuestionCreatedAt,
			ClosureAssessmentDigestSHA256: Digest(out.Bytes["closure-before-dialogue.yaml"]),
		}, nil)
		if err != nil {
			return out, err
		}
		dialogue = qr.Dialogue
		questionReport = qr.Report
	}
	out.Dialogue = dialogue
	out.QuestionReport = questionReport
	out.Bytes["dialogue.yaml"], err = architecture.MarshalCanonicalDialogueDocumentYAML(dialogue)
	if err != nil {
		return out, err
	}
	out.Bytes["question-generation.yaml"], err = questiongen.MarshalCanonicalReportYAML(questionReport)
	if err != nil {
		return out, err
	}
	out.Receipts = append(out.Receipts,
		stageReceipt("generate_questions.dialogue", "dialogue.yaml", out.Bytes["dialogue.yaml"], inputs.Manifest),
		stageReceipt("generate_questions.report", "question-generation.yaml", out.Bytes["question-generation.yaml"], inputs.Manifest),
	)

	after, err := closure.Evaluate(closure.Context{
		Request: inputs.Request, Claims: maint.Document, Maintenance: &maint.Report, Plane: &planeReport, Dialogue: &dialogue, Evidence: &inputs.Evidence,
		Graph: closureGraph, GraphReceipt: inputs.GraphReceipt, RepositoryRoot: inputs.RepositoryRoot, RepositoryRev: inputs.RepoRevision, RepositoryStatus: architecture.RevisionResolved,
	})
	if err != nil {
		return out, err
	}
	out.ClosureAfter = after
	out.Bytes["closure-after-dialogue.yaml"], err = closure.MarshalCanonicalReportYAML(after)
	if err != nil {
		return out, err
	}
	out.Receipts = append(out.Receipts, stageReceipt("assess_closure_after_dialogue", "closure-after-dialogue.yaml", out.Bytes["closure-after-dialogue.yaml"], inputs.Manifest))

	probeGraph, err := probe.LoadGraphIndex(inputs.GraphPath)
	if err != nil {
		return out, err
	}
	if after.Verdict == closure.VerdictUncertifiable {
		out.ProbeDocument = probe.ProbeDocument{SchemaVersion: probe.SchemaVersion, GeneratedBy: probe.GeneratedBy, Binding: inputs.Request.Binding, SourceClosureAssessmentDigestSHA256: Digest(out.Bytes["closure-after-dialogue.yaml"]), SourceDialogueDigestSHA256: Digest(out.Bytes["dialogue.yaml"]), SourceClaimDocumentDigestSHA256: Digest(out.Bytes["maintained-claims.yaml"])}
		out.ProbeReport = probe.GenerationReport{SchemaVersion: probe.SchemaVersion, GeneratedBy: probe.GeneratedBy, Binding: inputs.Request.Binding, SourceClosureAssessmentDigestSHA256: Digest(out.Bytes["closure-after-dialogue.yaml"]), SourceDialogueDigestSHA256: Digest(out.Bytes["dialogue.yaml"]), SourceClaimDocumentDigestSHA256: Digest(out.Bytes["maintained-claims.yaml"]), Limitations: []architecture.Limitation{{Source: "closure-after-dialogue", Scope: "probe_planning", Reason: "post-dialogue closure assessment is uncertifiable", Blocking: false}}}
	} else {
		pr, err := probe.Generate(probe.Context{
			Closure: after, Claims: maint.Document, Dialogue: dialogue, Maintenance: &maint.Report, Plane: &planeReport, Evidence: &inputs.Evidence, Graph: probeGraph, Existing: inputs.ExistingProbe,
			SourceClosureDigest: Digest(out.Bytes["closure-after-dialogue.yaml"]), SourceDialogueDigest: Digest(out.Bytes["dialogue.yaml"]), SourceClaimsDigest: Digest(out.Bytes["maintained-claims.yaml"]),
		}, nil)
		if err != nil {
			return out, err
		}
		out.ProbeDocument = pr.Document
		out.ProbeReport = pr.Report
	}
	ctx := &probe.ValidationContext{Dialogue: dialogue, Claims: maint.Document, Graph: probeGraph}
	out.Bytes["probes.yaml"], err = probe.MarshalCanonicalDocumentYAML(out.ProbeDocument, ctx)
	if err != nil {
		return out, err
	}
	out.Bytes["probe-generation.yaml"], err = probe.MarshalGenerationReportYAML(out.ProbeReport)
	if err != nil {
		return out, err
	}
	out.Receipts = append(out.Receipts,
		stageReceipt("plan_probes.document", "probes.yaml", out.Bytes["probes.yaml"], inputs.Manifest),
		stageReceipt("plan_probes.report", "probe-generation.yaml", out.Bytes["probe-generation.yaml"], inputs.Manifest),
	)
	out.SemanticStateDigest = SemanticStateDigest(maint.Document, planeReport, after, dialogue, out.ProbeDocument, inputs.Evidence)
	out.WaitClasses = WaitClasses(after, dialogue, out.ProbeDocument)
	out.NextActions = NextActions(after, dialogue, out.ProbeDocument)
	out.Limitations = collectLimitations(maint.Report.Limitations, planeReport.Limitations, before.Limitations, questionReport.Limitations, after.Limitations, out.ProbeReport.Limitations)
	out.CriticalBlockerCount = countCritical(after.Blockers)
	return out, nil
}

func buildIteration(session Session, inputs loadedInputs, stages stageOutputs, policy Policy, prev *Iteration) Iteration {
	changes := ChangeSet{}
	progress := ProgressInitial
	noEffect := false
	noEffectReason := ""
	if prev != nil {
		changes = DiffSemantic(prev.SemanticStateDigestSHA256, stages.SemanticStateDigest, session.Iterations[len(session.Iterations)-1].ClosureVerdict, stages.ClosureAfter, stages)
		progress = classifyProgress(changes, prev.ClosureVerdict, stages.ClosureAfter.Verdict)
		noEffect = prev.SemanticInputDigestSHA256 != inputs.Manifest.SemanticInputDigestSHA256 && prev.SemanticStateDigestSHA256 == stages.SemanticStateDigest
		if noEffect {
			noEffectReason = "convergence.changed_inputs_without_semantic_effect"
		}
	}
	repeated := repeatedBlockers(session.Iterations, stages.ClosureAfter.Blockers, policy)
	osc := detectOscillation(session.Iterations, stages.SemanticStateDigest, policy)
	status := classifyStatus(stages.ClosureAfter.Verdict, stages.WaitClasses, policy, noEffect, consecutiveNoEffect(append(session.Iterations, Iteration{NoEffectInput: noEffect})), osc != nil)
	limitations := append([]architecture.Limitation{}, stages.Limitations...)
	for _, rb := range repeated {
		if rb.ConsecutiveRounds >= policy.MaxRepeatedBlockerRounds {
			limitations = append(limitations, architecture.Limitation{Source: rb.BlockerID, Scope: "convergence", Reason: "convergence.blocker.repeated_without_resolution", Blocking: false})
		}
	}
	if status == StatusStalled && noEffect {
		limitations = append(limitations, architecture.Limitation{Source: inputs.Manifest.SemanticInputDigestSHA256, Scope: "convergence", Reason: noEffectReason, Blocking: false})
	}
	iter := Iteration{
		Index:                     len(session.Iterations) + 1,
		InputManifestDigestSHA256: Digest(canonicalJSON(inputs.Manifest)),
		SemanticInputDigestSHA256: inputs.Manifest.SemanticInputDigestSHA256,
		SemanticStateDigestSHA256: stages.SemanticStateDigest,
		Status:                    status,
		ProgressStatus:            progress,
		ClosureVerdict:            stages.ClosureAfter.Verdict,
		WaitClasses:               stages.WaitClasses,
		StageReceipts:             stages.Receipts,
		Changes:                   normalizeChangeSet(changes),
		RepeatedBlockers:          repeated,
		Oscillation:               osc,
		NoEffectInput:             noEffect,
		NoEffectReason:            noEffectReason,
		NextActions:               stages.NextActions,
		Limitations:               limitations,
	}
	for i := range iter.StageReceipts {
		iter.StageReceipts[i].ArtifactPath = fmt.Sprintf(iter.StageReceipts[i].ArtifactPath, iter.Index)
	}
	if prev != nil {
		iter.PreviousIterationDigestSHA256 = prev.IterationDigestSHA256
	}
	iter.IterationDigestSHA256 = iterationDigest(session.SessionID, iter)
	return iter
}

func StableSessionID(req closure.Request, p Policy) string {
	scopeDigest := Digest(canonicalJSON(req.Scope))
	parts := []string{req.Binding.RepositoryDomain, req.Binding.Revision, req.Binding.GraphDigestSHA256, scopeDigest, req.Scope.TaskClass, req.Scope.RiskClass, p.ID, p.Version}
	sum := sha1.Sum([]byte(strings.Join(parts, "|")))
	return "convergence." + stableToken(req.Scope.TaskClass) + "." + hex.EncodeToString(sum[:])[:12]
}

func SemanticStateDigest(claims architecture.ClaimDocument, planes plane.Report, report closure.Report, dialogue architecture.DialogueDocument, probes probe.ProbeDocument, evidence maintenance.EvidenceStateDocument) string {
	type claimState struct {
		ID, Status, Promotion string
		Support, Refute       []string
	}
	type planeState struct{ ID, Plane, State string }
	type dimState struct {
		Dimension, State     string
		Required, Applicable bool
		Blockers, Conditions []string
	}
	type blockerState struct {
		ID, Code, Severity, Action         string
		Claims, Nodes, Questions, Evidence []string
	}
	type questionState struct {
		ID, Status                       string
		ArchitectRequired                bool
		Claims, Nodes, Blockers, Answers []string
	}
	type answerState struct {
		ID, Status string
		Questions  []string
	}
	type probeState struct{ ID, Status, Question, Role, Safety, Approval, Evidence string }
	type evState struct{ ID, Status, Freshness string }
	snap := struct {
		Claims  []claimState
		Planes  []planeState
		Closure struct {
			Verdict    string
			Dimensions []dimState
			Blockers   []blockerState
			Conditions []string
		}
		Questions []questionState
		Answers   []answerState
		Probes    []probeState
		Evidence  []evState
	}{}
	for _, c := range claims.Claims {
		snap.Claims = append(snap.Claims, claimState{c.ID, c.EpistemicStatus, c.PromotionStatus, clean(c.SupportingEvidence), clean(c.RefutingEvidence)})
	}
	for _, a := range planes.ClaimAssessments {
		snap.Planes = append(snap.Planes, planeState{a.ClaimID, a.DeclaredPlane, a.PlaneState})
	}
	snap.Closure.Verdict = report.Verdict
	for _, d := range report.Dimensions {
		snap.Closure.Dimensions = append(snap.Closure.Dimensions, dimState{d.Dimension, d.State, d.Required, d.Applicable, clean(d.BlockerIDs), clean(d.ConditionIDs)})
	}
	for _, b := range report.Blockers {
		snap.Closure.Blockers = append(snap.Closure.Blockers, blockerState{b.ID, b.Code, b.Severity, b.RequiredNextAction, clean(b.ClaimIDs), clean(b.NodeIDs), clean(b.QuestionIDs), clean(b.EvidenceIDs)})
	}
	for _, c := range report.Conditions {
		snap.Closure.Conditions = append(snap.Closure.Conditions, c.ID+":"+c.Code+":"+c.RequiredNextAction)
	}
	for _, q := range dialogue.OpenQuestions {
		snap.Questions = append(snap.Questions, questionState{q.ID, q.Status, q.ArchitectRequired, clean(q.BlocksClaims), clean(q.BlocksNodes), clean(q.BlocksClosureBlockers), clean(q.ResolvedByAnswers)})
	}
	for _, a := range dialogue.Answers {
		snap.Answers = append(snap.Answers, answerState{a.ID, a.GovernanceStatus, clean(a.AnswersQuestions)})
	}
	for _, p := range probes.Probes {
		snap.Probes = append(snap.Probes, probeState{p.ID, p.Status, p.QuestionID, p.EvidenceRole, p.SafetyClass, p.ApprovalGate, p.TargetEvidenceID})
	}
	for _, e := range evidence.Evidence {
		snap.Evidence = append(snap.Evidence, evState{e.ID, e.Status, e.Freshness})
	}
	sort.Slice(snap.Claims, func(i, j int) bool { return snap.Claims[i].ID < snap.Claims[j].ID })
	sort.Slice(snap.Planes, func(i, j int) bool { return snap.Planes[i].ID < snap.Planes[j].ID })
	sort.Slice(snap.Closure.Dimensions, func(i, j int) bool {
		return snap.Closure.Dimensions[i].Dimension < snap.Closure.Dimensions[j].Dimension
	})
	sort.Slice(snap.Closure.Blockers, func(i, j int) bool { return snap.Closure.Blockers[i].ID < snap.Closure.Blockers[j].ID })
	sort.Strings(snap.Closure.Conditions)
	sort.Slice(snap.Questions, func(i, j int) bool { return snap.Questions[i].ID < snap.Questions[j].ID })
	sort.Slice(snap.Answers, func(i, j int) bool { return snap.Answers[i].ID < snap.Answers[j].ID })
	sort.Slice(snap.Probes, func(i, j int) bool { return snap.Probes[i].ID < snap.Probes[j].ID })
	sort.Slice(snap.Evidence, func(i, j int) bool { return snap.Evidence[i].ID < snap.Evidence[j].ID })
	return Digest(canonicalJSON(snap))
}

func WaitClasses(report closure.Report, dialogue architecture.DialogueDocument, probes probe.ProbeDocument) []string {
	seen := map[string]bool{}
	for _, q := range dialogue.OpenQuestions {
		if q.ArchitectRequired && (q.Status == architecture.QuestionStatusOpen || q.Status == architecture.QuestionStatusAwaitingArchitect || q.Status == architecture.QuestionStatusAnswered) {
			seen[WaitArchitect] = true
		}
		if q.Status == architecture.QuestionStatusAwaitingEvidence {
			seen[WaitEvidence] = true
		}
	}
	for _, a := range dialogue.Answers {
		if a.GovernanceStatus == architecture.AnswerGovernanceAwaitingEvidence {
			seen[WaitEvidence] = true
		}
		if a.GovernanceStatus == architecture.AnswerGovernanceAcceptedForQuestion {
			seen[WaitGovernance] = true
		}
	}
	for _, p := range probes.Probes {
		if p.Status == probe.StatusProposed || p.Status == probe.StatusUnavailable {
			seen[WaitEvidence] = true
		}
	}
	for _, b := range report.Blockers {
		if mechanicalAction(b.RequiredNextAction) {
			seen[WaitMechanicalRepair] = true
		}
	}
	return sortedKeys(seen)
}

func NextActions(report closure.Report, dialogue architecture.DialogueDocument, probes probe.ProbeDocument) []NextAction {
	var out []NextAction
	for _, q := range dialogue.OpenQuestions {
		switch q.Status {
		case architecture.QuestionStatusAwaitingArchitect, architecture.QuestionStatusOpen:
			out = append(out, NextAction{"answer_question", q.Priority, q.ID, "answer " + q.ID})
		case architecture.QuestionStatusAnswered:
			out = append(out, NextAction{"adjudicate_answer", q.Priority, q.ID, "adjudicate " + q.ID})
		case architecture.QuestionStatusAwaitingEvidence:
			out = append(out, NextAction{"provide_evidence", q.Priority, q.ID, "provide evidence for " + q.ID})
		}
	}
	for _, p := range probes.Probes {
		if p.Status == probe.StatusProposed {
			out = append(out, NextAction{"execute_probe_externally", "medium", p.ID, "execute approved " + p.ID + " outside Sensei"})
		}
	}
	for _, b := range report.Blockers {
		if mechanicalAction(b.RequiredNextAction) {
			out = append(out, NextAction{mapMechanicalAction(b.RequiredNextAction), b.Severity, b.ID, b.RequiredNextAction + " for " + b.ID})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if priorityRank(out[i].Priority) != priorityRank(out[j].Priority) {
			return priorityRank(out[i].Priority) < priorityRank(out[j].Priority)
		}
		if out[i].Class != out[j].Class {
			return out[i].Class < out[j].Class
		}
		return out[i].Reference < out[j].Reference
	})
	return out
}

func RenderBundle(session Session, manifest InputManifest, iter Iteration, stageBytes map[string][]byte) (Bundle, error) {
	files := map[string][]byte{}
	sessionBytes, err := MarshalSessionYAML(session)
	if err != nil {
		return Bundle{}, err
	}
	iterBytes, err := yaml.Marshal(map[string]Iteration{"architecture_convergence_iteration": iter})
	if err != nil {
		return Bundle{}, err
	}
	manifestBytes, err := yaml.Marshal(map[string]InputManifest{"architecture_convergence_input_manifest": manifest})
	if err != nil {
		return Bundle{}, err
	}
	files["session.yaml"] = sessionBytes
	iterDir := fmt.Sprintf("iterations/%04d", iter.Index)
	files[iterDir+"/iteration.yaml"] = iterBytes
	files[iterDir+"/input-manifest.yaml"] = manifestBytes
	files["latest/iteration.yaml"] = iterBytes
	files["latest/input-manifest.yaml"] = manifestBytes
	for name, data := range stageBytes {
		files[iterDir+"/"+name] = data
		files["latest/"+name] = data
	}
	return Bundle{Files: files}, nil
}

func WriteBundle(dir string, bundle Bundle) error {
	if dir == "" {
		return errors.New("output directory is required")
	}
	if protectedOutputPath(dir) {
		return errors.New("output under docs/awareness or docs/intent must be inside a candidates directory")
	}
	parent := filepath.Dir(dir)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(parent, "."+filepath.Base(dir)+".tmp-*")
	if err != nil {
		return err
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(tmp)
		}
	}()
	for rel, data := range bundle.Files {
		if filepath.IsAbs(rel) || strings.HasPrefix(filepath.ToSlash(rel), "../") {
			return fmt.Errorf("bundle path must be relative: %s", rel)
		}
		dst := filepath.Join(tmp, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			return err
		}
	}
	old := dir + ".old"
	_ = os.RemoveAll(old)
	if _, err := os.Stat(dir); err == nil {
		if err := os.Rename(dir, old); err != nil {
			return err
		}
	}
	if err := os.Rename(tmp, dir); err != nil {
		if _, statErr := os.Stat(old); statErr == nil {
			_ = os.Rename(old, dir)
		}
		return err
	}
	_ = os.RemoveAll(old)
	cleanup = false
	return nil
}

func VerifyBundle(dir string, session Session) error {
	if err := ValidateSession(session); err != nil {
		return err
	}
	if len(session.Iterations) == 0 {
		return errors.New("session has no iterations")
	}
	latest := session.Iterations[len(session.Iterations)-1]
	for _, r := range latest.StageReceipts {
		if filepath.IsAbs(r.ArtifactPath) {
			return fmt.Errorf("absolute artifact path %s", r.ArtifactPath)
		}
		data, err := os.ReadFile(filepath.Join(dir, r.ArtifactPath))
		if err != nil {
			return err
		}
		if Digest(data) != r.DigestSHA256 {
			return fmt.Errorf("stale stage artifact %s", r.ArtifactPath)
		}
		latestPath := filepath.Join(dir, "latest", filepath.Base(r.ArtifactPath))
		latestData, err := os.ReadFile(latestPath)
		if err != nil {
			return err
		}
		if !bytes.Equal(data, latestData) {
			return fmt.Errorf("latest artifact mismatch for %s", r.ArtifactPath)
		}
	}
	return nil
}

func MarshalSessionYAML(session Session) ([]byte, error) {
	session = normalizeSession(session)
	return yaml.Marshal(map[string]Session{"architecture_convergence_session": session})
}

func UnmarshalSessionYAML(data []byte) (Session, error) {
	var env map[string]Session
	if err := yaml.Unmarshal(data, &env); err != nil {
		return Session{}, err
	}
	s, ok := env["architecture_convergence_session"]
	if !ok || s.SchemaVersion == "" {
		return Session{}, errors.New("missing architecture_convergence_session document")
	}
	s = normalizeSession(s)
	if err := ValidateSession(s); err != nil {
		return Session{}, err
	}
	return s, nil
}

func LoadSession(path string) (Session, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Session{}, err
	}
	return UnmarshalSessionYAML(data)
}

func ValidateSession(s Session) error {
	if s.SessionID == "" || s.PolicyID == "" || s.PolicyVersion == "" {
		return errors.New("session identity and policy are required")
	}
	prevDigest := ""
	seen := map[int]bool{}
	for i, iter := range s.Iterations {
		wantIndex := i + 1
		if iter.Index != wantIndex || seen[iter.Index] {
			return errors.New("session iterations must be contiguous")
		}
		seen[iter.Index] = true
		if iter.PreviousIterationDigestSHA256 != prevDigest {
			return errors.New("session iteration previous digest mismatch")
		}
		got := iterationDigest(s.SessionID, iter)
		if iter.IterationDigestSHA256 != got {
			return errors.New("session iteration digest mismatch")
		}
		prevDigest = iter.IterationDigestSHA256
	}
	return nil
}

func Status(s Session) (StatusReport, error) {
	if err := ValidateSession(s); err != nil {
		return StatusReport{}, err
	}
	p, ok := PolicyByID(s.PolicyID)
	if !ok {
		return StatusReport{}, fmt.Errorf("unknown policy %s", s.PolicyID)
	}
	return reportFromSession(s, p, ""), nil
}

func Digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func emptyDigest() string { return Digest([]byte("canonical-empty")) }

func semanticInputDigest(m InputManifest) string {
	m.QuestionCreatedAt = ""
	m.SemanticInputDigestSHA256 = ""
	return Digest(canonicalJSON(m))
}

func iterationDigest(sessionID string, iter Iteration) string {
	iter.IterationDigestSHA256 = ""
	return Digest(canonicalJSON(struct {
		SessionID string
		Iteration Iteration
	}{sessionID, iter}))
}

func stageReceipt(stage, path string, data []byte, manifest InputManifest) StageReceipt {
	return StageReceipt{
		Stage:                stage,
		Disposition:          StageProduced,
		ArtifactPath:         "iterations/%04d/" + path,
		DigestSHA256:         Digest(data),
		SemanticDigestSHA256: Digest(data),
		InputDigests: []DigestReceipt{
			{"closure_request", manifest.ClosureRequestDigestSHA256},
			{"claims", manifest.ClaimsDigestSHA256},
			{"dialogue", manifest.DialogueDigestSHA256},
			{"evidence_state", manifest.EvidenceStateDigestSHA256},
			{"graph_snapshot", manifest.GraphSnapshotDigestSHA256},
			{"existing_probes", manifest.ExistingProbesDigestSHA256},
		},
	}
}

func DiffSemantic(prevDigest, currentDigest, prevVerdict string, current closure.Report, stages stageOutputs) ChangeSet {
	cs := ChangeSet{}
	if prevDigest != currentDigest {
		cs.Reasons = append(cs.Reasons, "semantic_state_changed")
	}
	for _, q := range stages.QuestionReport.Generated {
		if q.QuestionID != "" {
			cs.Questions.Added = append(cs.Questions.Added, q.QuestionID)
			cs.Questions.Improved = append(cs.Questions.Improved, q.QuestionID)
		}
	}
	for _, item := range stages.ProbeReport.Items {
		if item.Disposition == probe.DispositionGenerated {
			cs.Probes.Added = append(cs.Probes.Added, item.ProbeIDs...)
			cs.Probes.Improved = append(cs.Probes.Improved, item.ProbeIDs...)
		}
	}
	if closureRank(current.Verdict) > closureRank(prevVerdict) {
		cs.Dimensions.Improved = append(cs.Dimensions.Improved, "overall")
	}
	if closureRank(current.Verdict) < closureRank(prevVerdict) {
		cs.Dimensions.Regressed = append(cs.Dimensions.Regressed, "overall")
	}
	return normalizeChangeSet(cs)
}

func classifyProgress(cs ChangeSet, prevVerdict, currentVerdict string) string {
	closureImproved := closureRank(currentVerdict) > closureRank(prevVerdict) || len(cs.Dimensions.Improved) > 0
	closureRegressed := closureRank(currentVerdict) < closureRank(prevVerdict) || len(cs.Dimensions.Regressed) > 0
	epistemic := hasEntityProgress(cs.Claims) || hasEntityProgress(cs.Questions) || hasEntityProgress(cs.Probes) || hasEntityProgress(cs.Evidence) || len(cs.Blockers.Added)+len(cs.Blockers.Changed)+len(cs.Blockers.Improved) > 0
	if epistemic && closureRegressed {
		return ProgressMixedProgress
	}
	if closureImproved && !closureRegressed {
		return ProgressClosureProgress
	}
	if epistemic {
		return ProgressEpistemicProgress
	}
	if closureRegressed {
		return ProgressRegression
	}
	return ProgressNoProgress
}

func classifyStatus(verdict string, waits []string, policy Policy, noEffect bool, noEffectCount int, oscillating bool) string {
	switch verdict {
	case closure.VerdictClosed:
		return StatusClosed
	case closure.VerdictConditionallyClosed:
		if policy.ConditionalClosureTerminal {
			return StatusConditionallyClosed
		}
	case closure.VerdictUncertifiable:
		return StatusUncertifiable
	}
	if oscillating {
		return StatusOscillating
	}
	if noEffect && noEffectCount >= policy.NoEffectInputLimit {
		return StatusStalled
	}
	if len(waits) > 0 {
		return StatusWaiting
	}
	return StatusStalled
}

func repeatedBlockers(history []Iteration, blockers []closure.Blocker, policy Policy) []RepeatedBlocker {
	if len(history) == 0 {
		return nil
	}
	current := map[string]closure.Blocker{}
	for _, b := range blockers {
		current[b.ID] = b
	}
	last := history[len(history)-1].RepeatedBlockers
	prevIDs := blockerIDsFromIteration(history[len(history)-1])
	var out []RepeatedBlocker
	for id, b := range current {
		if !prevIDs[id] {
			continue
		}
		rb := RepeatedBlocker{BlockerID: id, FirstIteration: len(history), LatestIteration: len(history) + 1, ConsecutiveRounds: 2, Severity: b.Severity, NextActionClass: b.RequiredNextAction}
		for _, prior := range last {
			if prior.BlockerID == id {
				rb.FirstIteration = prior.FirstIteration
				rb.ConsecutiveRounds = prior.ConsecutiveRounds + 1
				break
			}
		}
		out = append(out, rb)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].BlockerID < out[j].BlockerID })
	return out
}

func blockerIDsFromIteration(iter Iteration) map[string]bool {
	out := map[string]bool{}
	for _, n := range iter.NextActions {
		if strings.HasPrefix(n.Reference, "blocker.") {
			out[n.Reference] = true
		}
	}
	for _, rb := range iter.RepeatedBlockers {
		out[rb.BlockerID] = true
	}
	return out
}

func detectOscillation(history []Iteration, current string, policy Policy) *Oscillation {
	start := len(history) - policy.OscillationWindow
	if start < 0 {
		start = 0
	}
	for i := len(history) - 2; i >= start; i-- {
		if history[i].SemanticStateDigestSHA256 != current {
			continue
		}
		var mid []string
		for _, iter := range history[i+1:] {
			if iter.SemanticStateDigestSHA256 != current {
				mid = append(mid, iter.SemanticStateDigestSHA256)
			}
		}
		if len(mid) > 0 {
			return &Oscillation{StartIteration: history[i].Index, EndIteration: len(history) + 1, SemanticDigest: current, IntermediateDigests: clean(mid)}
		}
	}
	return nil
}

func consecutiveNoEffect(iterations []Iteration) int {
	count := 0
	for i := len(iterations) - 1; i >= 0; i-- {
		if !iterations[i].NoEffectInput {
			break
		}
		count++
	}
	return count
}

func repositoryRevision(root string) (string, error) {
	root = filepath.Clean(root)
	gitDir := filepath.Join(root, ".git")
	info, err := os.Stat(gitDir)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		data, err := os.ReadFile(gitDir)
		if err != nil {
			return "", err
		}
		line := strings.TrimSpace(string(data))
		if strings.HasPrefix(line, "gitdir:") {
			gitDir = strings.TrimSpace(strings.TrimPrefix(line, "gitdir:"))
			if !filepath.IsAbs(gitDir) {
				gitDir = filepath.Join(root, gitDir)
			}
		}
	}
	headBytes, err := os.ReadFile(filepath.Join(gitDir, "HEAD"))
	if err != nil {
		return "", err
	}
	head := strings.TrimSpace(string(headBytes))
	if len(head) == 40 && isHex(head) {
		return head, nil
	}
	if strings.HasPrefix(head, "ref:") {
		ref := strings.TrimSpace(strings.TrimPrefix(head, "ref:"))
		if data, err := os.ReadFile(filepath.Join(gitDir, filepath.FromSlash(ref))); err == nil {
			return strings.TrimSpace(string(data)), nil
		}
		data, err := os.ReadFile(filepath.Join(gitDir, "packed-refs"))
		if err != nil {
			return "", err
		}
		for _, line := range strings.Split(string(data), "\n") {
			fields := strings.Fields(line)
			if len(fields) == 2 && fields[1] == ref {
				return fields[0], nil
			}
		}
	}
	return "", errors.New("repository revision is unavailable")
}

func requireResolvedBinding(b architecture.ClaimDocumentBinding) error {
	if b.RepositoryDomain == "" || b.Revision == "" || b.GraphDigestSHA256 == "" {
		return errors.New("fixed binding requires repository domain, revision, and graph digest")
	}
	if b.RevisionStatus != architecture.RevisionResolved {
		return errors.New("fixed binding revision must be resolved")
	}
	if b.GraphDigestStatus != architecture.GraphDigestResolved {
		return errors.New("fixed binding graph digest must be resolved")
	}
	return nil
}

func bindingsEqual(a, b architecture.ClaimDocumentBinding) bool {
	return a.RepositoryDomain == b.RepositoryDomain && a.Revision == b.Revision && a.RevisionStatus == b.RevisionStatus && a.GraphDigestSHA256 == b.GraphDigestSHA256 && a.GraphDigestStatus == b.GraphDigestStatus
}

func normalizeSession(s Session) Session {
	s.SchemaVersion = SchemaVersion
	if s.GeneratedBy == "" {
		s.GeneratedBy = GeneratedBy
	}
	s.LatestWaitClasses = clean(s.LatestWaitClasses)
	return s
}

func stageLimitations(r StageReceipt) []architecture.Limitation { return r.Limitations }

func protectedOutputPath(path string) bool {
	rel := filepath.ToSlash(filepath.Clean(path))
	for _, root := range []string{"docs/awareness", "docs/intent"} {
		if rel == root || strings.HasPrefix(rel, root+"/") {
			for _, part := range strings.Split(rel, "/") {
				if part == "candidates" {
					return false
				}
			}
			return true
		}
	}
	return false
}

func reportFromSession(s Session, p Policy, disposition string) StatusReport {
	if len(s.Iterations) == 0 {
		return StatusReport{SessionID: s.SessionID, MaxIterations: p.MaxIterations, Status: s.LatestStatus, Disposition: disposition}
	}
	iter := s.Iterations[len(s.Iterations)-1]
	return reportFromIteration(s, p, iter, disposition, 0)
}

func reportFromIteration(s Session, p Policy, iter Iteration, disposition string, critical int) StatusReport {
	return StatusReport{SessionID: s.SessionID, Iteration: iter.Index, MaxIterations: p.MaxIterations, Status: iter.Status, ClosureVerdict: iter.ClosureVerdict, ProgressStatus: iter.ProgressStatus, WaitClasses: iter.WaitClasses, CriticalBlockers: critical, RepeatedBlockers: len(iter.RepeatedBlockers), NextActions: iter.NextActions, Disposition: disposition}
}

func budgetReport(s Session, p Policy) StatusReport {
	r := reportFromSession(s, p, DispositionBudgetExhausted)
	r.Status = StatusBudgetExhausted
	return r
}

func countCritical(blockers []closure.Blocker) int {
	n := 0
	for _, b := range blockers {
		if b.Severity == "critical" {
			n++
		}
	}
	return n
}

func collectLimitations(groups ...[]architecture.Limitation) []architecture.Limitation {
	var out []architecture.Limitation
	for _, g := range groups {
		out = append(out, g...)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Source != out[j].Source {
			return out[i].Source < out[j].Source
		}
		if out[i].Scope != out[j].Scope {
			return out[i].Scope < out[j].Scope
		}
		return out[i].Reason < out[j].Reason
	})
	return out
}

func normalizeChangeSet(cs ChangeSet) ChangeSet {
	cs.Claims = normalizeEntityChanges(cs.Claims)
	cs.Dimensions = normalizeEntityChanges(cs.Dimensions)
	cs.Blockers = normalizeEntityChanges(cs.Blockers)
	cs.Questions = normalizeEntityChanges(cs.Questions)
	cs.Probes = normalizeEntityChanges(cs.Probes)
	cs.Evidence = normalizeEntityChanges(cs.Evidence)
	cs.Reasons = clean(cs.Reasons)
	return cs
}

func normalizeEntityChanges(e EntityChanges) EntityChanges {
	e.Added = clean(e.Added)
	e.Removed = clean(e.Removed)
	e.Changed = clean(e.Changed)
	e.Improved = clean(e.Improved)
	e.Regressed = clean(e.Regressed)
	return e
}

func hasEntityProgress(e EntityChanges) bool {
	return len(e.Added)+len(e.Changed)+len(e.Improved)+len(e.Removed)+len(e.Regressed) > 0
}

func closureRank(v string) int {
	switch v {
	case closure.VerdictUncertifiable:
		return 0
	case closure.VerdictOpen:
		return 1
	case closure.VerdictConditionallyClosed:
		return 2
	case closure.VerdictClosed:
		return 3
	default:
		return -1
	}
}

func mechanicalAction(action string) bool {
	switch action {
	case "provide_input", "repair_binding", "repair_claim", "add_test", "add_failure_mode", "reassess_scope":
		return true
	default:
		return false
	}
}

func mapMechanicalAction(action string) string {
	switch action {
	case "repair_binding":
		return "repair_binding"
	case "repair_claim":
		return "repair_claim"
	case "add_test":
		return "add_test"
	case "add_failure_mode":
		return "add_failure_mode"
	case "reassess_scope":
		return "reassess_scope"
	default:
		return "provide_evidence"
	}
}

func priorityRank(p string) int {
	switch p {
	case "critical":
		return 0
	case "high":
		return 1
	case "medium":
		return 2
	case "low":
		return 3
	default:
		return 4
	}
}

func stableToken(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return "session"
	}
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return strings.Trim(b.String(), "_.-")
}

func sortedKeys(m map[string]bool) []string {
	var out []string
	for k, v := range m {
		if v {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

func clean(in []string) []string {
	seen := map[string]bool{}
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s != "" {
			seen[s] = true
		}
	}
	return sortedKeys(seen)
}

func canonicalJSON(v any) []byte {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return data
}

func isHex(s string) bool {
	for _, r := range s {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return false
		}
	}
	return true
}
