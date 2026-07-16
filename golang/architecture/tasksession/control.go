// SPDX-License-Identifier: Apache-2.0

package tasksession

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/admission"
	bindingpkg "github.com/globulario/sensei/golang/architecture/binding"
	"github.com/globulario/sensei/golang/architecture/closure"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/convergence"
	"github.com/globulario/sensei/golang/architecture/ledger"
	"github.com/globulario/sensei/golang/architecture/maintenance"
	"github.com/globulario/sensei/golang/architecture/probe"
	"github.com/globulario/sensei/golang/architecture/probeexec"
	"github.com/globulario/sensei/golang/architecture/taskcontrol"
	"gopkg.in/yaml.v3"
)

const (
	AdvanceGeneratedBy         = "sensei advance-task"
	AdvanceReplay              = "replay_no_new_iteration"
	AdvanceCompleted           = "advanced"
	ReasonTaskLockHeld         = "task.lock.held"
	ReasonTaskLockStale        = "task.lock.stale"
	ReasonIncompleteGeneration = "task.control.incomplete_generation"
	AdoptionPolicyVersion      = "task.machine_adopted_knowledge.v1"
	ProbeRegistryVersion       = "static-probe-registry.v1"
)

type AdvanceTaskOptions struct {
	RepoRoot   string
	TaskDir    string
	Active     bool
	ObservedAt string
	Budget     probeexec.Budget
	LockWait   time.Duration
	Clock      func() time.Time
}

type AdvanceTaskResult struct {
	Disposition string                       `json:"disposition" yaml:"disposition"`
	TaskDir     string                       `json:"task_dir" yaml:"task_dir"`
	Generation  string                       `json:"generation,omitempty" yaml:"generation,omitempty"`
	Control     taskcontrol.TaskControlState `json:"control" yaml:"control"`
	Probe       probeexec.Metrics            `json:"probe_metrics" yaml:"probe_metrics"`
	Convergence convergence.StatusReport     `json:"convergence" yaml:"convergence"`
}

type controlGenerationPointer struct {
	SchemaVersion string `json:"schema_version" yaml:"schema_version"`
	Generation    string `json:"generation" yaml:"generation"`
	DigestSHA256  string `json:"digest_sha256" yaml:"digest_sha256"`
}

type controlGenerationPointerEnvelope struct {
	TaskControlGeneration controlGenerationPointer `json:"task_control_generation" yaml:"task_control_generation"`
}

type GenerationReceipt struct {
	SchemaVersion                string            `json:"schema_version" yaml:"schema_version"`
	GeneratedBy                  string            `json:"generated_by" yaml:"generated_by"`
	TaskID                       string            `json:"task_id" yaml:"task_id"`
	PreviousGenerationDigest     string            `json:"previous_generation_digest,omitempty" yaml:"previous_generation_digest,omitempty"`
	InputArtifactDigests         map[string]string `json:"input_artifact_digests" yaml:"input_artifact_digests"`
	ExecutedProbeIDs             []string          `json:"executed_probe_ids,omitempty" yaml:"executed_probe_ids,omitempty"`
	ProbeResultDigestSHA256      string            `json:"probe_result_digest_sha256" yaml:"probe_result_digest_sha256"`
	ConvergenceIteration         int               `json:"convergence_iteration" yaml:"convergence_iteration"`
	ClosureDigestSHA256          string            `json:"closure_digest_sha256" yaml:"closure_digest_sha256"`
	AdmissionDigestSHA256        string            `json:"admission_digest_sha256" yaml:"admission_digest_sha256"`
	TaskControlDigestSHA256      string            `json:"task_control_digest_sha256" yaml:"task_control_digest_sha256"`
	GenerationDigestSHA256       string            `json:"generation_digest_sha256" yaml:"generation_digest_sha256"`
	StartedAt                    string            `json:"started_at" yaml:"started_at"`
	CompletedAt                  string            `json:"completed_at" yaml:"completed_at"`
	ToolVersion                  string            `json:"tool_version" yaml:"tool_version"`
	AdoptionPolicyVersion        string            `json:"adoption_policy_version" yaml:"adoption_policy_version"`
	ProbeRegistryVersion         string            `json:"probe_registry_version" yaml:"probe_registry_version"`
	ProbeMetrics                 probeexec.Metrics `json:"probe_metrics" yaml:"probe_metrics"`
	ClassificationDurationMillis int64             `json:"classification_duration_ms" yaml:"classification_duration_ms"`
	ProbeExecutionDurationMillis int64             `json:"probe_execution_duration_ms" yaml:"probe_execution_duration_ms"`
	ConvergenceDurationMillis    int64             `json:"convergence_duration_ms" yaml:"convergence_duration_ms"`
	ArtifactCommitDurationMillis int64             `json:"artifact_commit_duration_ms" yaml:"artifact_commit_duration_ms"`
	InputBlockerCount            int               `json:"input_blocker_count" yaml:"input_blocker_count"`
	RootBlockerCount             int               `json:"root_blocker_count" yaml:"root_blocker_count"`
	QuestionDispositionCounts    map[string]int    `json:"question_disposition_counts" yaml:"question_disposition_counts"`
	ProbeDispositionCounts       map[string]int    `json:"probe_disposition_counts" yaml:"probe_disposition_counts"`
	CacheHits                    int               `json:"cache_hits" yaml:"cache_hits"`
	CacheMisses                  int               `json:"cache_misses" yaml:"cache_misses"`
}

type generationReceiptEnvelope struct {
	TaskControlGenerationReceipt GenerationReceipt `json:"task_control_generation_receipt" yaml:"task_control_generation_receipt"`
}

type taskLockReceipt struct {
	PID        int    `json:"pid"`
	Owner      string `json:"owner"`
	AcquiredAt string `json:"acquired_at"`
}

type controlPaths struct {
	Claims      string
	Dialogue    string
	Evidence    string
	Probes      string
	Results     string
	Convergence string
	Session     string
}

func AdvanceTask(opts AdvanceTaskOptions) (AdvanceTaskResult, error) {
	now := opts.Clock
	if now == nil {
		now = time.Now
	}
	started := now().UTC()
	repoRoot, taskDir, ptr, err := resolveControlTask(opts.RepoRoot, opts.TaskDir, opts.Active)
	if err != nil {
		return AdvanceTaskResult{}, err
	}
	unlock, err := acquireTaskLock(taskDir, opts.LockWait, now)
	if err != nil {
		return AdvanceTaskResult{}, err
	}
	defer unlock()

	baseSession, err := LoadSession(filepath.Join(taskDir, "session.yaml"))
	if err != nil {
		return AdvanceTaskResult{}, err
	}
	if verifyErrors := verifySession(repoRoot, taskDir, baseSession, ptr); len(verifyErrors) > 0 {
		return AdvanceTaskResult{}, fmt.Errorf("task binding stale: %s", strings.Join(verifyErrors, "; "))
	}
	paths, previousDigest, err := currentControlPaths(taskDir)
	if err != nil {
		return AdvanceTaskResult{}, err
	}
	claims, err := architecture.LoadClaimDocument(paths.Claims)
	if err != nil {
		return AdvanceTaskResult{}, fmt.Errorf("load control claims: %w", err)
	}
	dialogue, err := architecture.LoadDialogueDocument(paths.Dialogue)
	if err != nil {
		return AdvanceTaskResult{}, fmt.Errorf("load control dialogue: %w", err)
	}
	evidence, err := maintenance.LoadEvidenceStateDocument(paths.Evidence)
	if err != nil {
		return AdvanceTaskResult{}, fmt.Errorf("load control evidence: %w", err)
	}
	probes, err := probe.LoadDocument(paths.Probes, nil)
	if err != nil {
		return AdvanceTaskResult{}, fmt.Errorf("load control probes: %w", err)
	}
	probeBytes, err := os.ReadFile(paths.Probes)
	if err != nil {
		return AdvanceTaskResult{}, err
	}
	probeDigest := digest(probeBytes)
	var existingResults *probe.ResultDocument
	if paths.Results != "" {
		if existing, loadErr := probe.LoadResultDocument(paths.Results, probes); loadErr == nil {
			existingResults = &existing
		} else if !os.IsNotExist(loadErr) {
			return AdvanceTaskResult{}, loadErr
		}
	}
	observedAt := strings.TrimSpace(opts.ObservedAt)
	if observedAt == "" {
		observedAt = started.Format(time.RFC3339)
	}
	probeStarted := now()
	batch, err := probeexec.ExecuteBatch(probeexec.Context{
		RepositoryRoot: repoRoot, Binding: baseSession.Binding, Probes: probes,
		ProbeDocumentDigest: probeDigest, Claims: claims, ExistingResults: existingResults,
		EvidenceState: evidence, ObservedAt: observedAt, Budget: opts.Budget,
	})
	if err != nil {
		return AdvanceTaskResult{}, err
	}
	probeDuration := now().Sub(probeStarted)
	resultsBytes, err := probe.MarshalResultDocumentYAML(batch.Results, probes)
	if err != nil {
		return AdvanceTaskResult{}, err
	}
	evidenceBytes, err := maintenance.MarshalCanonicalEvidenceStateYAML(batch.EvidenceState)
	if err != nil {
		return AdvanceTaskResult{}, err
	}

	controlRoot := filepath.Join(taskDir, "control")
	if err := os.MkdirAll(filepath.Join(controlRoot, "generations"), 0o755); err != nil {
		return AdvanceTaskResult{}, err
	}
	tmpRoot, err := os.MkdirTemp(filepath.Join(controlRoot, "generations"), ".generation.tmp-")
	if err != nil {
		return AdvanceTaskResult{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.RemoveAll(tmpRoot)
		}
	}()
	if err := writeFileAtomic(filepath.Join(tmpRoot, "probe-results.yaml"), resultsBytes); err != nil {
		return AdvanceTaskResult{}, err
	}
	if err := writeFileAtomic(filepath.Join(tmpRoot, "evidence-state.yaml"), evidenceBytes); err != nil {
		return AdvanceTaskResult{}, err
	}

	convStarted := now()
	var existingSession *convergence.Session
	if current, loadErr := convergence.LoadSession(paths.Session); loadErr == nil {
		existingSession = &current
	} else {
		return AdvanceTaskResult{}, loadErr
	}
	conv, err := convergence.Advance(convergence.Options{
		Paths: convergence.InputPaths{
			ClosureRequest: filepath.Join(taskDir, "closure-request.yaml"), Claims: paths.Claims,
			Dialogue: paths.Dialogue, EvidenceState: filepath.Join(tmpRoot, "evidence-state.yaml"),
			GraphNT: filepath.Join(taskDir, "source", "graph.nt"), RepositoryRoot: repoRoot,
			ExistingProbes: paths.Probes,
		},
		QuestionCreatedAt: questionCreatedAt(dialogue), PolicyID: convergence.PolicyStrictV1, Session: existingSession,
	})
	if err != nil {
		return AdvanceTaskResult{}, err
	}
	convDuration := now().Sub(convStarted)
	if conv.Disposition == convergence.DispositionReplay {
		if existing, loadErr := LoadTaskControl(filepath.Join(taskDir, "control", "latest.yaml")); loadErr == nil {
			if err := updateActiveControlPointer(repoRoot, ptr, existing.ReceiptDigestSHA256, nil); err != nil {
				return AdvanceTaskResult{}, err
			}
			return AdvanceTaskResult{Disposition: AdvanceReplay, TaskDir: taskDir, Generation: previousDigest, Control: existing, Probe: batch.Metrics, Convergence: conv.Report}, nil
		}
	}
	if conv.Disposition == convergence.DispositionBudgetExhausted {
		return AdvanceTaskResult{}, errors.New("convergence budget exhausted")
	}
	convDir := filepath.Join(tmpRoot, "convergence")
	if err := convergence.WriteBundle(convDir, conv.Bundle); err != nil {
		return AdvanceTaskResult{}, err
	}

	latestIter := conv.Session.Iterations[len(conv.Session.Iterations)-1]
	admitReq := admission.Request{
		SchemaVersion: admission.SchemaVersion, Binding: baseSession.Binding,
		Convergence: admission.ConvergenceBinding{SessionID: conv.Session.SessionID, IterationDigestSHA256: latestIter.IterationDigestSHA256, SemanticStateDigestSHA256: latestIter.SemanticStateDigestSHA256},
		Mode:        baseSession.TaskRequest.Mode, TaskClass: baseSession.TaskRequest.TaskClass,
		Scope:       admission.ChangeScope{Files: admissionFiles(baseSession.TaskRequest.Scope.Files)},
		RequestedBy: baseSession.TaskRequest.RequestedBy, Note: baseSession.TaskRequest.Description,
	}
	admitReqBytes, err := admission.MarshalCanonicalRequestYAML(admitReq)
	if err != nil {
		return AdvanceTaskResult{}, err
	}
	if err := writeFileAtomic(filepath.Join(tmpRoot, "admission-request.yaml"), admitReqBytes); err != nil {
		return AdvanceTaskResult{}, err
	}
	decision, err := admission.Evaluate(admission.EvaluateOptions{BundleDir: convDir, RequestPath: filepath.Join(tmpRoot, "admission-request.yaml"), GraphNT: filepath.Join(taskDir, "source", "graph.nt"), Repo: repoRoot, PolicyID: admission.PolicyStrictID})
	if err != nil {
		return AdvanceTaskResult{}, err
	}
	decisionBytes, err := admission.MarshalCanonicalDecisionYAML(decision)
	if err != nil {
		return AdvanceTaskResult{}, err
	}
	if err := writeFileAtomic(filepath.Join(tmpRoot, "admission-decision.yaml"), decisionBytes); err != nil {
		return AdvanceTaskResult{}, err
	}

	closureReport, err := closure.LoadReport(filepath.Join(convDir, "latest", "closure-after-dialogue.yaml"))
	if err != nil {
		return AdvanceTaskResult{}, err
	}
	latestClaims, err := architecture.LoadClaimDocument(filepath.Join(convDir, "latest", "maintained-claims.yaml"))
	if err != nil {
		return AdvanceTaskResult{}, err
	}
	latestDialogue, err := architecture.LoadDialogueDocument(filepath.Join(convDir, "latest", "dialogue.yaml"))
	if err != nil {
		return AdvanceTaskResult{}, err
	}
	latestProbes, err := probe.LoadDocument(filepath.Join(convDir, "latest", "probes.yaml"), nil)
	if err != nil {
		return AdvanceTaskResult{}, err
	}
	// Typed admission-v2 governance is authoritative for the mutation grant:
	// legacy admission no longer hands out modify permission on its own.
	gov := governanceDisposition(taskDir, now().UTC())
	modifyCapability := decision.MutationCapability
	modifyScope := decision.Envelope.ModifyPaths
	if gov.Resolved {
		if gov.Status == StatusReadyForMutation {
			modifyCapability = admission.CapabilityAdmitted
			modifyScope = gov.ModifyPaths
		} else {
			modifyCapability = admission.CapabilityWaiting
		}
	} else if modifyCapability == admission.CapabilityAdmitted || modifyCapability == admission.CapabilityAdmittedWithConditions {
		// A task that has not resolved typed governance must not be granted a
		// mutation capability through the legacy path.
		modifyCapability = admission.CapabilityWaiting
	}
	classStarted := now()
	controlState, err := taskcontrol.Project(taskcontrol.Inputs{
		TaskID: baseSession.TaskID, Iteration: latestIter.Index, Binding: baseSession.Binding,
		Permission: taskcontrol.PermissionSummary{Inspect: decision.InspectionCapability, Modify: modifyCapability, ExactScope: append(append([]string{}, decision.Envelope.ReadPaths...), modifyScope...)},
		Closure:    closureReport, Dialogue: latestDialogue, Claims: latestClaims, Probes: latestProbes,
		Results: &batch.Results, BindingHealthy: true, GeneratedAt: observedAt, Receipts: iterationReceiptIDs(latestIter),
	})
	if err != nil {
		return AdvanceTaskResult{}, err
	}
	classDuration := now().Sub(classStarted)
	controlBytes, err := taskcontrol.MarshalYAML(controlState)
	if err != nil {
		return AdvanceTaskResult{}, err
	}
	if err := writeFileAtomic(filepath.Join(tmpRoot, "task-control.yaml"), controlBytes); err != nil {
		return AdvanceTaskResult{}, err
	}

	inputDigests := map[string]string{}
	for name, path := range map[string]string{"claims": paths.Claims, "dialogue": paths.Dialogue, "evidence_state": paths.Evidence, "probes": paths.Probes, "graph": filepath.Join(taskDir, "source", "graph.nt"), "task_request": filepath.Join(taskDir, "task-request.yaml")} {
		inputDigests[name], _ = digestFile(path)
	}
	closureDigest, _ := digestFile(filepath.Join(convDir, "latest", "closure-after-dialogue.yaml"))
	var executed []string
	for _, d := range batch.Decisions {
		if d.Executed {
			executed = append(executed, d.ProbeID)
		}
	}
	generationDigest := semanticGenerationDigest(previousDigest, inputDigests, digest(resultsBytes), latestIter.IterationDigestSHA256, digest(decisionBytes), controlState.ReceiptDigestSHA256)
	receipt := GenerationReceipt{
		SchemaVersion: SchemaVersion, GeneratedBy: AdvanceGeneratedBy, TaskID: baseSession.TaskID,
		PreviousGenerationDigest: previousDigest, InputArtifactDigests: inputDigests,
		ExecutedProbeIDs: cleanStrings(executed), ProbeResultDigestSHA256: digest(resultsBytes),
		ConvergenceIteration: latestIter.Index, ClosureDigestSHA256: closureDigest,
		AdmissionDigestSHA256: digest(decisionBytes), TaskControlDigestSHA256: controlState.ReceiptDigestSHA256,
		GenerationDigestSHA256: generationDigest, StartedAt: started.Format(time.RFC3339), CompletedAt: now().UTC().Format(time.RFC3339),
		ToolVersion: "0.0.0-dev", AdoptionPolicyVersion: AdoptionPolicyVersion, ProbeRegistryVersion: ProbeRegistryVersion,
		ProbeMetrics: batch.Metrics, ClassificationDurationMillis: classDuration.Milliseconds(), ProbeExecutionDurationMillis: probeDuration.Milliseconds(), ConvergenceDurationMillis: convDuration.Milliseconds(),
		InputBlockerCount: len(closureReport.Blockers), RootBlockerCount: controlState.Summary.ActiveRootBlockers,
		QuestionDispositionCounts: questionDispositionCounts(controlState), ProbeDispositionCounts: probeDispositionCounts(controlState), CacheMisses: 1,
	}
	receiptBytes, err := yaml.Marshal(generationReceiptEnvelope{TaskControlGenerationReceipt: receipt})
	if err != nil {
		return AdvanceTaskResult{}, err
	}
	if err := writeFileAtomic(filepath.Join(tmpRoot, "receipt.yaml"), receiptBytes); err != nil {
		return AdvanceTaskResult{}, err
	}
	target := filepath.Join(controlRoot, "generations", generationDigest)
	commitStarted := now()
	if _, err := os.Stat(target); err == nil {
		return AdvanceTaskResult{Disposition: AdvanceReplay, TaskDir: taskDir, Generation: generationDigest, Control: controlState, Probe: batch.Metrics, Convergence: conv.Report}, nil
	}
	if err := os.Rename(tmpRoot, target); err != nil {
		return AdvanceTaskResult{}, err
	}
	committed = true
	receipt.ArtifactCommitDurationMillis = now().Sub(commitStarted).Milliseconds()
	pointerBytes, err := yaml.Marshal(controlGenerationPointerEnvelope{TaskControlGeneration: controlGenerationPointer{SchemaVersion: SchemaVersion, Generation: filepath.ToSlash(filepath.Join("generations", generationDigest)), DigestSHA256: generationDigest}})
	if err != nil {
		return AdvanceTaskResult{}, err
	}
	if err := writeFileAtomic(filepath.Join(controlRoot, "latest.yaml"), controlBytes); err != nil {
		return AdvanceTaskResult{}, err
	}
	if err := writeFileAtomic(filepath.Join(controlRoot, "latest-generation.yaml"), pointerBytes); err != nil {
		return AdvanceTaskResult{}, err
	}
	status := StatusResult{
		TaskID:      baseSession.TaskID,
		Phase:       baseSession.WorkflowPhase,
		Status:      governedStatus(taskDir, baseSession.OperationalStatus),
		Closure:     baseSession.ClosureVerdict,
		Convergence: baseSession.ConvergenceStatus,
		Admission:   baseSession.AdmissionDecision,
		WaitingOn:   baseSession.WaitingOn,
		Next:        firstNext(baseSession),
		Session:     baseSession,
	}
	statusBytes, err := yaml.Marshal(map[string]StatusResult{"architecture_task_status": status})
	if err != nil {
		return AdvanceTaskResult{}, err
	}
	if err := writeFileAtomic(filepath.Join(taskDir, "receipts", "task-status.yaml"), statusBytes); err != nil {
		return AdvanceTaskResult{}, err
	}
	ledgerHead, err := appendLedgerControlState(repoRoot, taskDir, baseSession, controlBytes, statusBytes)
	if err != nil {
		return AdvanceTaskResult{}, err
	}
	if err := updateActiveControlPointer(repoRoot, ptr, controlState.ReceiptDigestSHA256, &ledgerHead); err != nil {
		return AdvanceTaskResult{}, err
	}
	return AdvanceTaskResult{Disposition: AdvanceCompleted, TaskDir: taskDir, Generation: generationDigest, Control: controlState, Probe: batch.Metrics, Convergence: conv.Report}, nil
}

func LoadTaskControl(path string) (taskcontrol.TaskControlState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return taskcontrol.TaskControlState{}, err
	}
	return taskcontrol.UnmarshalYAML(data)
}

func ControlStatus(repoRoot, taskDir string, active bool) (taskcontrol.TaskControlState, string, error) {
	return projectControlStatus(repoRoot, taskDir, active, true)
}

func projectControlStatus(repoRoot, taskDir string, active, useLatest bool) (taskcontrol.TaskControlState, string, error) {
	repoRoot, taskDir, ptr, err := resolveControlTask(repoRoot, taskDir, active)
	if err != nil {
		return taskcontrol.TaskControlState{}, "", err
	}
	var latestState *taskcontrol.TaskControlState
	if state, loadErr := LoadTaskControl(filepath.Join(taskDir, "control", "latest.yaml")); useLatest && loadErr == nil {
		latestState = &state
	} else if useLatest && !os.IsNotExist(loadErr) {
		return taskcontrol.TaskControlState{}, "", loadErr
	}
	session, loadErrors, err := loadSessionForControl(filepath.Join(taskDir, "session.yaml"))
	if err != nil {
		return taskcontrol.TaskControlState{}, "", err
	}
	verifyErrors := cleanStrings(append(loadErrors, verifySession(repoRoot, taskDir, session, ptr)...))
	if len(verifyErrors) == 0 && latestState != nil {
		return *latestState, taskDir, nil
	}
	paths := baseControlPaths(taskDir)
	if useLatest {
		paths, _, err = currentControlPaths(taskDir)
		if err != nil {
			return taskcontrol.TaskControlState{}, "", err
		}
	}
	claims, err := architecture.LoadClaimDocument(paths.Claims)
	if err != nil {
		return taskcontrol.TaskControlState{}, "", err
	}
	dialogue, err := architecture.LoadDialogueDocument(paths.Dialogue)
	if err != nil {
		return taskcontrol.TaskControlState{}, "", err
	}
	probes, err := probe.LoadDocument(paths.Probes, nil)
	if err != nil {
		return taskcontrol.TaskControlState{}, "", err
	}
	closureReport, err := closure.LoadReport(filepath.Join(paths.Convergence, "latest", "closure-after-dialogue.yaml"))
	if err != nil {
		return taskcontrol.TaskControlState{}, "", err
	}
	decision, err := admission.LoadDecision(filepath.Join(taskDir, "admission", "decision.yaml"))
	if err != nil {
		return taskcontrol.TaskControlState{}, "", err
	}
	inspectCapability := decision.InspectionCapability
	mutationCapability := decision.MutationCapability
	if len(verifyErrors) > 0 {
		inspectCapability = "uncertifiable"
		mutationCapability = admission.CapabilityRefused
	}
	var results *probe.ResultDocument
	if paths.Results != "" {
		if doc, loadErr := probe.LoadResultDocument(paths.Results, probes); loadErr == nil {
			results = &doc
		}
	}
	iteration := 0
	var receipts []string
	if conv, loadErr := convergence.LoadSession(paths.Session); loadErr == nil && len(conv.Iterations) > 0 {
		latest := conv.Iterations[len(conv.Iterations)-1]
		iteration = latest.Index
		receipts = iterationReceiptIDs(latest)
	}
	state, err := taskcontrol.Project(taskcontrol.Inputs{
		TaskID: session.TaskID, Iteration: iteration, Binding: session.Binding,
		Permission: taskcontrol.PermissionSummary{Inspect: inspectCapability, Modify: mutationCapability, ExactScope: append(append([]string{}, decision.Envelope.ReadPaths...), decision.Envelope.ModifyPaths...)},
		Closure:    closureReport, Dialogue: dialogue, Claims: claims, Probes: probes, Results: results,
		BindingHealthy: len(verifyErrors) == 0, BindingErrors: verifyErrors, GeneratedAt: "1970-01-01T00:00:00Z", Receipts: receipts,
	})
	return state, taskDir, err
}

func loadSessionForControl(path string) (Session, []string, error) {
	session, err := LoadSession(path)
	if err == nil {
		return session, nil, nil
	}
	if !strings.Contains(err.Error(), "task session digest mismatch") {
		return Session{}, nil, err
	}
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		return Session{}, nil, readErr
	}
	var env sessionEnvelope
	if unmarshalErr := yaml.Unmarshal(data, &env); unmarshalErr != nil {
		return Session{}, nil, unmarshalErr
	}
	if env.ArchitectureTaskSession.SchemaVersion == "" {
		return Session{}, nil, errors.New("missing architecture_task_session")
	}
	return normalizeSession(env.ArchitectureTaskSession), []string{"task.binding.session_digest_mismatch"}, nil
}

func questionDispositionCounts(state taskcontrol.TaskControlState) map[string]int {
	counts := map[string]int{}
	for _, question := range state.Questions {
		counts[question.ResolutionClass]++
	}
	return counts
}

func probeDispositionCounts(state taskcontrol.TaskControlState) map[string]int {
	counts := map[string]int{}
	for _, evidenceProbe := range state.Probes {
		counts[evidenceProbe.Disposition]++
	}
	return counts
}

func iterationReceiptIDs(iteration convergence.Iteration) []string {
	var ids []string
	for _, receipt := range iteration.StageReceipts {
		if receipt.DigestSHA256 != "" {
			ids = append(ids, receipt.Stage+":"+receipt.DigestSHA256)
		}
	}
	return cleanStrings(ids)
}

func resolveControlTask(repoRoot, taskDir string, active bool) (string, string, *ActivePointer, error) {
	if strings.TrimSpace(repoRoot) == "" {
		repoRoot = "."
	}
	abs, err := filepath.Abs(repoRoot)
	if err != nil {
		return "", "", nil, err
	}
	if strings.TrimSpace(taskDir) != "" {
		if !filepath.IsAbs(taskDir) {
			taskDir = filepath.Join(abs, taskDir)
		}
		back, relErr := filepath.Rel(abs, taskDir)
		if relErr != nil || back == ".." || strings.HasPrefix(back, ".."+string(filepath.Separator)) {
			return "", "", nil, errors.New("task directory must be inside the repository")
		}
		return abs, taskDir, nil, nil
	}
	if active || taskDir == "" {
		ptr, err := LoadActivePointer(abs)
		if err == nil {
			taskDir = filepath.Dir(filepath.Join(abs, filepath.FromSlash(ptr.SessionPath)))
			return abs, taskDir, &ptr, nil
		}
		if !os.IsNotExist(err) {
			return "", "", nil, err
		}
		single, findErr := singleNonCompletedTask(abs)
		if findErr != nil {
			return "", "", nil, findErr
		}
		return abs, single, nil, nil
	}
	return "", "", nil, errors.New("task is required")
}

func singleNonCompletedTask(repoRoot string) (string, error) {
	entries, err := os.ReadDir(filepath.Join(repoRoot, ".sensei", "tasks"))
	if err != nil {
		return "", err
	}
	var candidates []string
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "task.") {
			continue
		}
		taskDir := filepath.Join(repoRoot, ".sensei", "tasks", entry.Name())
		if _, err := LoadSession(filepath.Join(taskDir, "session.yaml")); err != nil {
			continue
		}
		if state, err := LoadTaskControl(filepath.Join(taskDir, "control", "latest.yaml")); err == nil && state.NextAction.Kind == taskcontrol.ActionCompleteTask {
			continue
		}
		candidates = append(candidates, taskDir)
	}
	sort.Strings(candidates)
	if len(candidates) == 1 {
		return candidates[0], nil
	}
	if len(candidates) == 0 {
		return "", errors.New("no non-completed task found")
	}
	return "", errors.New("multiple non-completed tasks; specify --task")
}

func updateActiveControlPointer(repoRoot string, ptr *ActivePointer, controlDigest string, head *ledger.Head) error {
	if ptr == nil {
		return nil
	}
	ptr.LastTaskControlDigestSHA256 = strings.TrimSpace(controlDigest)
	if head != nil {
		ptr.LedgerHeadDigestSHA256 = head.EntryDigestSHA256
		ptr.LedgerSequence = head.Sequence
	}
	return WriteActivePointer(repoRoot, *ptr)
}

func appendLedgerControlState(repoRoot, taskDir string, session Session, controlBytes, statusBytes []byte) (ledger.Head, error) {
	store := ledger.NewStore(taskDir, ledger.WithPayloadValidator(func(eventType closureprotocol.LedgerEventType, mediaType string, data []byte) error {
		return ledger.ValidateTaskEventPayload(eventType, data)
	}))
	report, err := store.Verify()
	if err != nil {
		return ledger.Head{}, err
	}
	if !report.Valid {
		return ledger.Head{}, errors.New("task ledger invalid")
	}
	base, err := bindingpkg.ResolveBase(bindingpkg.ResolveBaseOptions{
		RepoRoot:         repoRoot,
		RepositoryDomain: session.Binding.RepositoryDomain,
		GraphPath:        filepath.Join(taskDir, "source", "graph.nt"),
		TaskID:           session.TaskID,
		SessionID:        stableTaskSessionID(session.TaskID),
		Policies:         closurePolicyBinding(),
	})
	if err != nil {
		return ledger.Head{}, err
	}
	sessionBytes, err := os.ReadFile(filepath.Join(taskDir, "session.yaml"))
	if err != nil {
		return ledger.Head{}, err
	}
	sessionRef, err := store.StoreArtifactBytes(sessionBytes, "application/yaml")
	if err != nil {
		return ledger.Head{}, err
	}
	controlRef, err := store.StoreArtifactBytes(controlBytes, "application/yaml")
	if err != nil {
		return ledger.Head{}, err
	}
	statusRef, err := store.StoreArtifactBytes(statusBytes, "application/yaml")
	if err != nil {
		return ledger.Head{}, err
	}
	appendPayload := func(expected string, eventType closureprotocol.LedgerEventType) (ledger.AppendResult, error) {
		return store.Append(context.Background(), ledger.AppendRequest{
			TaskID: session.TaskID, SessionID: stableTaskSessionID(session.TaskID), ExpectedHeadDigestSHA256: expected,
			EventType: eventType,
			Payload: ledger.TaskEventPayload{
				SchemaVersion: ledger.EventPayloadSchemaVersion,
				EventType:     eventType,
				TaskID:        session.TaskID,
				SessionID:     stableTaskSessionID(session.TaskID),
				Status:        session.OperationalStatus,
				BaseBinding:   &base,
				Artifacts: map[string]closureprotocol.LedgerPayloadRef{
					"session":      sessionRef,
					"task_control": controlRef,
					"status":       statusRef,
				},
				Limitations: []string{
					"legacy_scope_admission",
					"typed_actor_authority_not_yet_resolved",
					"single_use_capability_not_available",
					"correctness_not_certified",
				},
			},
			PayloadMediaType: "application/yaml",
			ProducerID:       AdvanceGeneratedBy,
			ProducedAt:       time.Unix(0, 0).UTC(),
		})
	}
	first, err := appendPayload(report.HeadDigestSHA256, closureprotocol.LedgerEventConvergenceAdvanced)
	if err != nil {
		return ledger.Head{}, err
	}
	second, err := appendPayload(first.Head.EntryDigestSHA256, closureprotocol.LedgerEventClosureAssessed)
	if err != nil {
		return ledger.Head{}, err
	}
	final, err := appendPayload(second.Head.EntryDigestSHA256, closureprotocol.LedgerEventTaskControlProjected)
	if err != nil {
		return ledger.Head{}, err
	}
	if _, err := ledger.RebuildProjections(taskDir, func(eventType closureprotocol.LedgerEventType, mediaType string, data []byte) error {
		return ledger.ValidateTaskEventPayload(eventType, data)
	}); err != nil {
		return ledger.Head{}, err
	}
	return final.Head, nil
}

func currentControlPaths(taskDir string) (controlPaths, string, error) {
	base := baseControlPaths(taskDir)
	data, err := os.ReadFile(filepath.Join(taskDir, "control", "latest-generation.yaml"))
	if os.IsNotExist(err) {
		return base, "", nil
	}
	if err != nil {
		return controlPaths{}, "", err
	}
	var env controlGenerationPointerEnvelope
	if err := yaml.Unmarshal(data, &env); err != nil {
		return controlPaths{}, "", err
	}
	ptr := env.TaskControlGeneration
	if ptr.SchemaVersion != SchemaVersion || ptr.Generation == "" || ptr.DigestSHA256 == "" {
		return controlPaths{}, "", errors.New(ReasonIncompleteGeneration)
	}
	root := filepath.Join(taskDir, "control", filepath.FromSlash(ptr.Generation))
	if filepath.Base(root) != ptr.DigestSHA256 {
		return controlPaths{}, "", errors.New(ReasonIncompleteGeneration)
	}
	return controlPaths{
		Claims: filepath.Join(root, "convergence", "latest", "maintained-claims.yaml"), Dialogue: filepath.Join(root, "convergence", "latest", "dialogue.yaml"),
		Evidence: filepath.Join(root, "evidence-state.yaml"), Probes: filepath.Join(root, "convergence", "latest", "probes.yaml"), Results: filepath.Join(root, "probe-results.yaml"),
		Convergence: filepath.Join(root, "convergence"), Session: filepath.Join(root, "convergence", "session.yaml"),
	}, ptr.DigestSHA256, nil
}

func baseControlPaths(taskDir string) controlPaths {
	return controlPaths{
		Claims:      filepath.Join(taskDir, "convergence", "latest", "maintained-claims.yaml"),
		Dialogue:    filepath.Join(taskDir, "convergence", "latest", "dialogue.yaml"),
		Evidence:    filepath.Join(taskDir, "source", "evidence-state.yaml"),
		Probes:      filepath.Join(taskDir, "convergence", "latest", "probes.yaml"),
		Convergence: filepath.Join(taskDir, "convergence"), Session: filepath.Join(taskDir, "convergence", "session.yaml"),
	}
}

func acquireTaskLock(taskDir string, wait time.Duration, clock func() time.Time) (func(), error) {
	if wait <= 0 {
		wait = 2 * time.Second
	}
	lockPath := filepath.Join(taskDir, "control", ".lock")
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		return nil, err
	}
	deadline := clock().Add(wait)
	for {
		file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			receipt := taskLockReceipt{PID: os.Getpid(), Owner: AdvanceGeneratedBy, AcquiredAt: clock().UTC().Format(time.RFC3339)}
			_ = json.NewEncoder(file).Encode(receipt)
			_ = file.Close()
			return func() { _ = os.Remove(lockPath) }, nil
		}
		if !os.IsExist(err) {
			return nil, err
		}
		if staleTaskLock(lockPath, clock()) {
			_ = os.Remove(lockPath)
			continue
		}
		if !clock().Before(deadline) {
			return nil, errors.New(ReasonTaskLockHeld)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func staleTaskLock(path string, now time.Time) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return true
	}
	var receipt taskLockReceipt
	if json.Unmarshal(data, &receipt) != nil {
		return true
	}
	acquired, err := time.Parse(time.RFC3339, receipt.AcquiredAt)
	if err != nil {
		return true
	}
	if now.Sub(acquired) > 10*time.Minute {
		return true
	}
	if receipt.PID <= 0 {
		return true
	}
	_, err = os.Stat(filepath.Join("/proc", strconv.Itoa(receipt.PID)))
	return os.IsNotExist(err)
}

func questionCreatedAt(dialogue architecture.DialogueDocument) string {
	for _, q := range dialogue.OpenQuestions {
		if q.CreatedAt != "" {
			return q.CreatedAt
		}
	}
	return "1970-01-01T00:00:00Z"
}

func semanticGenerationDigest(previous string, inputs map[string]string, resultDigest, iterationDigest, admissionDigest, controlDigest string) string {
	payload := struct {
		Previous                               string
		Inputs                                 map[string]string
		Results, Iteration, Admission, Control string
	}{previous, inputs, resultDigest, iterationDigest, admissionDigest, controlDigest}
	raw, _ := json.Marshal(payload)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func digestFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return digest(data), nil
}
