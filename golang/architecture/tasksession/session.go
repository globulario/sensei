// SPDX-License-Identifier: AGPL-3.0-only

package tasksession

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
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
	"github.com/globulario/sensei/golang/architecture/taskcontrol"
	"github.com/globulario/sensei/golang/extractor"
	"gopkg.in/yaml.v3"
)

const (
	SchemaVersion = "1"
	GeneratedBy   = "sensei prepare-change"

	PhasePrepared      = "prepared"
	PhaseWaiting       = "waiting"
	PhaseAdmitted      = "admitted"
	PhaseStale         = "stale"
	PhaseUncertifiable = "uncertifiable"

	StatusReadyForInspection = "ready_for_inspection"
	StatusReadyForAdmission  = "ready_for_admission"
	StatusReadyForMutation   = "ready_for_mutation"
	StatusAdmitted           = "admitted"
	StatusMutationObserved   = "mutation_observed"
	StatusScopeVerified      = "scope_verified"
	StatusWaitingArchitect   = "waiting_architect"
	StatusWaitingEvidence    = "waiting_evidence"
	StatusWaitingGovernance  = "waiting_governance"
	StatusWaitingMechanical  = "waiting_mechanical_repair"
	StatusRefused            = "refused"
	StatusStale              = "stale"
	StatusUncertifiable      = "uncertifiable"

	NextProvideInput     = "provide missing task input"
	NextAnswerQuestion   = "record architect answer"
	NextProvideEvidence  = "record external evidence"
	NextProposeKnowledge = "propose governed knowledge"
	NextAdvanceConverge  = "advance one convergence iteration"
	NextPerformEdit      = "perform admitted edit"
	NextVerifyAdmission  = "verify admission envelope"
	NextCompleteProof    = "complete required proof"
	NextRebuildResult    = "rebuild and bind result architecture"
	NextPrepareNewTask   = "prepare a new task"
)

type FileOperation struct {
	Path      string `json:"path" yaml:"path"`
	Operation string `json:"operation" yaml:"operation"`
}

type TaskRequest struct {
	SchemaVersion        string                            `json:"schema_version" yaml:"schema_version"`
	TaskID               string                            `json:"task_id" yaml:"task_id"`
	Binding              architecture.ClaimDocumentBinding `json:"binding" yaml:"binding"`
	Description          string                            `json:"description" yaml:"description"`
	Mode                 string                            `json:"mode" yaml:"mode"`
	TaskClass            string                            `json:"task_class" yaml:"task_class"`
	RiskClass            string                            `json:"risk_class" yaml:"risk_class"`
	DirectionRequirement string                            `json:"direction_requirement" yaml:"direction_requirement"`
	OutsideModifyDigest  string                            `json:"outside_modify_digest_sha256" yaml:"outside_modify_digest_sha256"`
	Scope                TaskScope                         `json:"scope" yaml:"scope"`
	RequestedBy          string                            `json:"requested_by,omitempty" yaml:"requested_by,omitempty"`
}

type TaskScope struct {
	Files           []FileOperation `json:"files,omitempty" yaml:"files,omitempty"`
	Symbols         []string        `json:"symbols,omitempty" yaml:"symbols,omitempty"`
	Components      []string        `json:"components,omitempty" yaml:"components,omitempty"`
	ClaimIDs        []string        `json:"claim_ids,omitempty" yaml:"claim_ids,omitempty"`
	PropositionKeys []string        `json:"proposition_keys,omitempty" yaml:"proposition_keys,omitempty"`
}

type ArtifactRefs struct {
	TaskRequest                  string `json:"task_request" yaml:"task_request"`
	ClosureRequest               string `json:"closure_request" yaml:"closure_request"`
	DirectionBootstrap           string `json:"direction_bootstrap,omitempty" yaml:"direction_bootstrap,omitempty"`
	AwarenessMutationEnforcement string `json:"awareness_mutation_enforcement,omitempty" yaml:"awareness_mutation_enforcement,omitempty"`
	Claims                       string `json:"claims" yaml:"claims"`
	Dialogue                     string `json:"dialogue" yaml:"dialogue"`
	EvidenceState                string `json:"evidence_state" yaml:"evidence_state"`
	KnowledgeBundle              string `json:"knowledge_bundle" yaml:"knowledge_bundle"`
	GraphSnapshot                string `json:"graph_snapshot" yaml:"graph_snapshot"`
	GraphReceipt                 string `json:"graph_receipt" yaml:"graph_receipt"`
	ConvergenceBundle            string `json:"convergence_bundle" yaml:"convergence_bundle"`
	ConvergenceSession           string `json:"convergence_session" yaml:"convergence_session"`
	AdmissionRequest             string `json:"admission_request" yaml:"admission_request"`
	AdmissionDecision            string `json:"admission_decision" yaml:"admission_decision"`
	AdmissionVerification        string `json:"admission_verification,omitempty" yaml:"admission_verification,omitempty"`
	PrepareReceipt               string `json:"prepare_receipt" yaml:"prepare_receipt"`
	StatusReceipt                string `json:"status_receipt" yaml:"status_receipt"`
}

type NextAction struct {
	Action    string `json:"action" yaml:"action"`
	Reference string `json:"reference,omitempty" yaml:"reference,omitempty"`
	Summary   string `json:"summary,omitempty" yaml:"summary,omitempty"`
}

type Session struct {
	SchemaVersion        string                            `json:"schema_version" yaml:"schema_version"`
	GeneratedBy          string                            `json:"generated_by" yaml:"generated_by"`
	TaskID               string                            `json:"task_id" yaml:"task_id"`
	Binding              architecture.ClaimDocumentBinding `json:"binding" yaml:"binding"`
	WorkflowPhase        string                            `json:"workflow_phase" yaml:"workflow_phase"`
	OperationalStatus    string                            `json:"operational_status" yaml:"operational_status"`
	TaskRequest          TaskRequest                       `json:"task_request" yaml:"task_request"`
	Artifacts            ArtifactRefs                      `json:"artifacts" yaml:"artifacts"`
	ClosureVerdict       string                            `json:"closure_verdict,omitempty" yaml:"closure_verdict,omitempty"`
	ConvergenceStatus    string                            `json:"convergence_status,omitempty" yaml:"convergence_status,omitempty"`
	AdmissionDecision    string                            `json:"admission_decision,omitempty" yaml:"admission_decision,omitempty"`
	InspectionCapability string                            `json:"inspection_capability,omitempty" yaml:"inspection_capability,omitempty"`
	MutationCapability   string                            `json:"mutation_capability,omitempty" yaml:"mutation_capability,omitempty"`
	WaitingOn            []string                          `json:"waiting_on,omitempty" yaml:"waiting_on,omitempty"`
	ReadEnvelope         []string                          `json:"read_envelope,omitempty" yaml:"read_envelope,omitempty"`
	ModifyEnvelope       []string                          `json:"modify_envelope,omitempty" yaml:"modify_envelope,omitempty"`
	NextActions          []NextAction                      `json:"next_actions" yaml:"next_actions"`
	Limitations          []string                          `json:"limitations,omitempty" yaml:"limitations,omitempty"`
	SessionDigestSHA256  string                            `json:"session_digest_sha256" yaml:"session_digest_sha256"`
}

type ActivePointer struct {
	SchemaVersion               string `json:"schema_version" yaml:"schema_version"`
	TaskID                      string `json:"task_id" yaml:"task_id"`
	RepositoryDomain            string `json:"repository_domain" yaml:"repository_domain"`
	Revision                    string `json:"revision" yaml:"revision"`
	GraphDigestSHA256           string `json:"graph_digest_sha256" yaml:"graph_digest_sha256"`
	LedgerPath                  string `json:"ledger_path,omitempty" yaml:"ledger_path,omitempty"`
	LedgerHeadDigestSHA256      string `json:"ledger_head_digest_sha256,omitempty" yaml:"ledger_head_digest_sha256,omitempty"`
	LedgerSequence              int    `json:"ledger_sequence,omitempty" yaml:"ledger_sequence,omitempty"`
	SessionPath                 string `json:"session_path" yaml:"session_path"`
	SessionDigestSHA256         string `json:"session_digest_sha256" yaml:"session_digest_sha256"`
	LastTaskControlDigestSHA256 string `json:"last_task_control_digest_sha256" yaml:"last_task_control_digest_sha256"`
}

type PrepareOptions struct {
	RepoRoot                        string
	RepositoryDomain                string
	Description                     string
	Mode                            string
	TaskClass                       string
	RiskClass                       string
	DirectionRequirement            string
	Files                           []FileOperation
	GraphNT                         string
	Claims                          string
	Dialogue                        string
	EvidenceState                   string
	DirectionBootstrapAuthorization string
	QuestionCreatedAt               string
	RequestedBy                     string
	SetActive                       bool
}

type PrepareResult struct {
	TaskID         string     `json:"task_id" yaml:"task_id"`
	TaskDir        string     `json:"task_dir" yaml:"task_dir"`
	GraphState     string     `json:"graph_state" yaml:"graph_state"`
	Closure        string     `json:"closure" yaml:"closure"`
	Convergence    string     `json:"convergence" yaml:"convergence"`
	Inspect        string     `json:"inspect" yaml:"inspect"`
	Modify         string     `json:"modify" yaml:"modify"`
	WaitingOn      []string   `json:"waiting_on,omitempty" yaml:"waiting_on,omitempty"`
	ReadEnvelope   []string   `json:"read_envelope,omitempty" yaml:"read_envelope,omitempty"`
	ModifyEnvelope []string   `json:"modify_envelope,omitempty" yaml:"modify_envelope,omitempty"`
	Next           NextAction `json:"next" yaml:"next"`
	Session        Session    `json:"session" yaml:"session"`
	Disposition    string     `json:"disposition" yaml:"disposition"`
}

type StatusOptions struct {
	RepoRoot string
	TaskDir  string
	Active   bool
	Verify   bool
}

type StatusResult struct {
	TaskID       string     `json:"task_id" yaml:"task_id"`
	Phase        string     `json:"phase" yaml:"phase"`
	Status       string     `json:"status" yaml:"status"`
	Closure      string     `json:"closure" yaml:"closure"`
	Convergence  string     `json:"convergence" yaml:"convergence"`
	Admission    string     `json:"admission" yaml:"admission"`
	WaitingOn    []string   `json:"waiting_on,omitempty" yaml:"waiting_on,omitempty"`
	Next         NextAction `json:"next" yaml:"next"`
	Verified     bool       `json:"verified" yaml:"verified"`
	VerifyErrors []string   `json:"verify_errors,omitempty" yaml:"verify_errors,omitempty"`
	Session      Session    `json:"session" yaml:"session"`
}

type graphReceipt struct {
	SchemaVersion string `json:"schema_version" yaml:"schema_version"`
	Path          string `json:"path" yaml:"path"`
	DigestSHA256  string `json:"digest_sha256" yaml:"digest_sha256"`
	Status        string `json:"status" yaml:"status"`
	Verified      bool   `json:"verified" yaml:"verified"`
}

type taskRequestEnvelope struct {
	ArchitectureTaskRequest TaskRequest `json:"architecture_task_request" yaml:"architecture_task_request"`
}

type sessionEnvelope struct {
	ArchitectureTaskSession Session `json:"architecture_task_session" yaml:"architecture_task_session"`
}

type pointerEnvelope struct {
	ArchitectureActiveTask ActivePointer `json:"architecture_active_task" yaml:"architecture_active_task"`
}

func Prepare(opts PrepareOptions) (PrepareResult, error) {
	opts = normalizePrepareOptions(opts)
	if err := validatePrepareOptions(opts); err != nil {
		return PrepareResult{}, err
	}
	repoRoot, err := filepath.Abs(opts.RepoRoot)
	if err != nil {
		return PrepareResult{}, err
	}
	revision, revStatus, limitations := architecture.ResolveRevision(repoRoot, true)
	if revStatus != architecture.RevisionResolved {
		return PrepareResult{}, fmt.Errorf("repository revision must be resolved: %s", revStatus)
	}
	graphData, err := os.ReadFile(opts.GraphNT)
	if err != nil {
		return PrepareResult{}, fmt.Errorf("read graph snapshot: %w", err)
	}
	graphDigest := digest(graphData)
	binding := architecture.ClaimDocumentBinding{
		RepositoryDomain:  opts.RepositoryDomain,
		Revision:          revision,
		RevisionStatus:    architecture.RevisionResolved,
		GraphDigestSHA256: graphDigest,
		GraphDigestStatus: architecture.GraphDigestResolved,
	}
	claims, err := loadPrepareClaims(repoRoot, binding, opts.Claims)
	if err != nil {
		return PrepareResult{}, err
	}
	taskReq := TaskRequest{
		SchemaVersion:        SchemaVersion,
		Binding:              binding,
		Description:          opts.Description,
		Mode:                 opts.Mode,
		TaskClass:            opts.TaskClass,
		RiskClass:            opts.RiskClass,
		DirectionRequirement: opts.DirectionRequirement,
		Scope:                TaskScope{Files: opts.Files},
		RequestedBy:          opts.RequestedBy,
	}
	taskReq.OutsideModifyDigest, err = outsideModifyDigest(repoRoot, taskReq.Scope.Files)
	if err != nil {
		return PrepareResult{}, fmt.Errorf("compute outside-modify digest: %w", err)
	}
	taskReq.TaskID = StableTaskID(taskReq)
	taskRootRel := filepath.ToSlash(filepath.Join(".sensei", "tasks", taskReq.TaskID))
	taskRoot := filepath.Join(repoRoot, filepath.FromSlash(taskRootRel))
	refs := defaultArtifactRefs()

	if err := os.MkdirAll(filepath.Join(taskRoot, "source"), 0o755); err != nil {
		return PrepareResult{}, err
	}
	if err := os.MkdirAll(filepath.Join(taskRoot, "admission"), 0o755); err != nil {
		return PrepareResult{}, err
	}
	if err := os.MkdirAll(filepath.Join(taskRoot, "governance", "proposals"), 0o755); err != nil {
		return PrepareResult{}, err
	}
	if err := os.MkdirAll(filepath.Join(taskRoot, "receipts"), 0o755); err != nil {
		return PrepareResult{}, err
	}

	taskBytes, err := MarshalTaskRequestYAML(taskReq)
	if err != nil {
		return PrepareResult{}, err
	}
	if err := writeFileAtomic(filepath.Join(taskRoot, "task-request.yaml"), taskBytes); err != nil {
		return PrepareResult{}, err
	}
	directionBootstrap, directionBootstrapBytes, err := loadDirectionBootstrapAuthorization(repoRoot, taskReq, opts.DirectionBootstrapAuthorization)
	if err != nil {
		return PrepareResult{}, err
	}
	awarenessMutation, err := snapshotAwarenessMutationEnforcement(repoRoot, taskRoot, taskReq)
	if err != nil {
		return PrepareResult{}, err
	}
	closureReq := closureRequestFromTask(taskReq, awarenessMutation)
	closureReq.DirectionBootstrap = directionBootstrap
	closureBytes, err := closure.MarshalCanonicalRequestYAML(closureReq)
	if err != nil {
		return PrepareResult{}, err
	}
	if err := writeFileAtomic(filepath.Join(taskRoot, "closure-request.yaml"), closureBytes); err != nil {
		return PrepareResult{}, err
	}
	if directionBootstrap != nil {
		if err := writeFileAtomic(filepath.Join(taskRoot, "governance", "bootstrap-direction-authorization.yaml"), directionBootstrapBytes); err != nil {
			return PrepareResult{}, err
		}
	}
	if err := writeFileAtomic(filepath.Join(taskRoot, "source", "graph.nt"), graphData); err != nil {
		return PrepareResult{}, err
	}
	grBytes, err := yaml.Marshal(map[string]graphReceipt{"architecture_graph_receipt": {
		SchemaVersion: SchemaVersion,
		Path:          refs.GraphSnapshot,
		DigestSHA256:  graphDigest,
		Status:        architecture.GraphDigestResolved,
		Verified:      true,
	}})
	if err != nil {
		return PrepareResult{}, err
	}
	if err := writeFileAtomic(filepath.Join(taskRoot, "source", "graph-receipt.yaml"), grBytes); err != nil {
		return PrepareResult{}, err
	}
	if err := snapshotProjectKnowledge(repoRoot, taskRoot, opts.RiskClass); err != nil {
		return PrepareResult{}, err
	}
	if err := prepareSourceDocuments(repoRoot, taskRoot, binding, claims, opts); err != nil {
		return PrepareResult{}, err
	}

	var existing *convergence.Session
	if s, err := convergence.LoadSession(filepath.Join(taskRoot, "convergence", "session.yaml")); err == nil {
		existing = &s
	} else if !os.IsNotExist(err) {
		return PrepareResult{}, err
	}
	res, err := convergence.Advance(convergence.Options{
		Paths: convergence.InputPaths{
			ClosureRequest: filepath.Join(taskRoot, "closure-request.yaml"),
			Claims:         filepath.Join(taskRoot, "source", "claims.yaml"),
			Dialogue:       filepath.Join(taskRoot, "source", "dialogue.yaml"),
			EvidenceState:  filepath.Join(taskRoot, "source", "evidence-state.yaml"),
			GraphNT:        filepath.Join(taskRoot, "source", "graph.nt"),
			RepositoryRoot: repoRoot,
		},
		QuestionCreatedAt: opts.QuestionCreatedAt,
		PolicyID:          convergence.PolicyStrictV1,
		Session:           existing,
	})
	if err != nil {
		return PrepareResult{}, err
	}
	if res.Disposition != convergence.DispositionReplay && res.Disposition != convergence.DispositionBudgetExhausted {
		if err := convergence.WriteBundle(filepath.Join(taskRoot, "convergence"), res.Bundle); err != nil {
			return PrepareResult{}, err
		}
	}
	sessionForAdmission := res.Session
	if res.Disposition == convergence.DispositionReplay {
		sessionForAdmission = *existing
	}
	if len(sessionForAdmission.Iterations) == 0 {
		return PrepareResult{}, errors.New("convergence produced no iterations")
	}
	latest := sessionForAdmission.Iterations[len(sessionForAdmission.Iterations)-1]
	admitReq := admission.Request{
		SchemaVersion: SchemaVersion,
		Binding:       binding,
		Convergence: admission.ConvergenceBinding{
			SessionID:                 sessionForAdmission.SessionID,
			IterationDigestSHA256:     latest.IterationDigestSHA256,
			SemanticStateDigestSHA256: latest.SemanticStateDigestSHA256,
		},
		Mode:        opts.Mode,
		TaskClass:   opts.TaskClass,
		Scope:       admission.ChangeScope{Files: admissionFiles(opts.Files)},
		RequestedBy: opts.RequestedBy,
		Note:        opts.Description,
	}
	admitReqBytes, err := admission.MarshalCanonicalRequestYAML(admitReq)
	if err != nil {
		return PrepareResult{}, err
	}
	admitReqPath := filepath.Join(taskRoot, "admission", "request.yaml")
	if err := writeFileAtomic(admitReqPath, admitReqBytes); err != nil {
		return PrepareResult{}, err
	}
	decision, err := admission.Evaluate(admission.EvaluateOptions{
		BundleDir:   filepath.Join(taskRoot, "convergence"),
		RequestPath: admitReqPath,
		GraphNT:     filepath.Join(taskRoot, "source", "graph.nt"),
		Repo:        repoRoot,
		PolicyID:    admission.PolicyStrictID,
	})
	if err != nil {
		return PrepareResult{}, err
	}
	decisionBytes, err := admission.MarshalCanonicalDecisionYAML(decision)
	if err != nil {
		return PrepareResult{}, err
	}
	if err := writeFileAtomic(filepath.Join(taskRoot, "admission", "decision.yaml"), decisionBytes); err != nil {
		return PrepareResult{}, err
	}

	session := buildSession(taskReq, refs, res.Report, decision, limitations)
	sessionBytes, err := MarshalSessionYAML(session)
	if err != nil {
		return PrepareResult{}, err
	}
	sessionPath := filepath.Join(taskRoot, "session.yaml")
	if err := writeFileAtomic(sessionPath, sessionBytes); err != nil {
		return PrepareResult{}, err
	}
	result := resultFromSession(repoRoot, taskRoot, session, res.Disposition)
	receiptBytes, err := yaml.Marshal(map[string]PrepareResult{"architecture_prepare_change": result})
	if err != nil {
		return PrepareResult{}, err
	}
	if err := writeFileAtomic(filepath.Join(taskRoot, "receipts", "prepare-change.yaml"), receiptBytes); err != nil {
		return PrepareResult{}, err
	}
	status := StatusResult{TaskID: session.TaskID, Phase: session.WorkflowPhase, Status: session.OperationalStatus, Closure: session.ClosureVerdict, Convergence: session.ConvergenceStatus, Admission: session.AdmissionDecision, WaitingOn: session.WaitingOn, Next: firstNext(session), Session: session}
	statusBytes, err := yaml.Marshal(map[string]StatusResult{"architecture_task_status": status})
	if err != nil {
		return PrepareResult{}, err
	}
	if err := writeFileAtomic(filepath.Join(taskRoot, "receipts", "task-status.yaml"), statusBytes); err != nil {
		return PrepareResult{}, err
	}
	initialControl, _, err := projectControlStatus(repoRoot, taskRoot, false, false)
	if err != nil {
		return PrepareResult{}, err
	}
	controlBytes, err := taskcontrol.MarshalYAML(initialControl)
	if err != nil {
		return PrepareResult{}, err
	}
	if err := writeFileAtomic(filepath.Join(taskRoot, "control", "latest.yaml"), controlBytes); err != nil {
		return PrepareResult{}, err
	}
	sessionID := stableTaskSessionID(taskReq.TaskID)
	ledgerHead, err := initializeLedgerState(repoRoot, opts.RepositoryDomain, taskRoot, taskReq.TaskID, sessionID, taskReq, taskBytes, closureBytes, sessionBytes, controlBytes, statusBytes, session.OperationalStatus)
	if err != nil {
		return PrepareResult{}, err
	}
	if opts.SetActive {
		ptr := ActivePointer{
			SchemaVersion:               SchemaVersion,
			TaskID:                      session.TaskID,
			RepositoryDomain:            session.Binding.RepositoryDomain,
			Revision:                    session.Binding.Revision,
			GraphDigestSHA256:           session.Binding.GraphDigestSHA256,
			LedgerPath:                  filepath.ToSlash(filepath.Join(taskRootRel, "ledger")),
			LedgerHeadDigestSHA256:      ledgerHead.EntryDigestSHA256,
			LedgerSequence:              ledgerHead.Sequence,
			SessionPath:                 filepath.ToSlash(filepath.Join(taskRootRel, "session.yaml")),
			SessionDigestSHA256:         session.SessionDigestSHA256,
			LastTaskControlDigestSHA256: initialControl.ReceiptDigestSHA256,
		}
		if err := WriteActivePointer(repoRoot, ptr); err != nil {
			return PrepareResult{}, err
		}
	}
	return result, nil
}

func Status(opts StatusOptions) (StatusResult, error) {
	repoRoot := strings.TrimSpace(opts.RepoRoot)
	if repoRoot == "" {
		repoRoot = "."
	}
	abs, err := filepath.Abs(repoRoot)
	if err != nil {
		return StatusResult{}, err
	}
	taskDir := strings.TrimSpace(opts.TaskDir)
	var pointer *ActivePointer
	if opts.Active || taskDir == "" {
		p, err := LoadActivePointer(abs)
		if err != nil {
			return StatusResult{}, err
		}
		pointer = &p
		taskDir = filepath.Dir(filepath.Join(abs, filepath.FromSlash(p.SessionPath)))
	}
	session, err := LoadSession(filepath.Join(taskDir, "session.yaml"))
	if err != nil {
		return StatusResult{}, err
	}
	res := StatusResult{
		TaskID:      session.TaskID,
		Phase:       session.WorkflowPhase,
		Status:      session.OperationalStatus,
		Closure:     session.ClosureVerdict,
		Convergence: session.ConvergenceStatus,
		Admission:   session.AdmissionDecision,
		WaitingOn:   session.WaitingOn,
		Next:        firstNext(session),
		Session:     session,
	}
	if opts.Verify {
		res.VerifyErrors = verifySession(abs, taskDir, session, pointer)
		res.Verified = len(res.VerifyErrors) == 0
		if !res.Verified {
			res.Phase = PhaseStale
			res.Status = StatusStale
			res.Next = NextAction{Action: NextPrepareNewTask, Summary: "active task binding is stale or unverifiable"}
		}
	}
	if res.Status != StatusStale {
		disp := governanceDisposition(taskDir, time.Now().UTC())
		applyGovernedDisposition(&res, disp, res.Status)
	}
	return res, nil
}

func StableTaskID(req TaskRequest) string {
	req = normalizeTaskRequest(req)
	scopeBytes := canonicalJSON(req.Scope)
	descSum := sha256.Sum256([]byte(req.Description))
	parts := []string{
		req.Binding.RepositoryDomain,
		req.Binding.Revision,
		req.Binding.GraphDigestSHA256,
		req.TaskClass,
		req.Mode,
		string(scopeBytes),
		hex.EncodeToString(descSum[:]),
	}
	sum := sha1.Sum([]byte(strings.Join(parts, "|")))
	return "task." + stableToken(req.TaskClass) + "." + hex.EncodeToString(sum[:])[:12]
}

func MarshalTaskRequestYAML(req TaskRequest) ([]byte, error) {
	req = normalizeTaskRequest(req)
	if err := validateTaskRequest(req); err != nil {
		return nil, err
	}
	return yaml.Marshal(taskRequestEnvelope{ArchitectureTaskRequest: req})
}

func LoadSession(path string) (Session, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Session{}, err
	}
	var env sessionEnvelope
	if err := yaml.Unmarshal(data, &env); err != nil {
		return Session{}, err
	}
	if env.ArchitectureTaskSession.SchemaVersion == "" {
		return Session{}, errors.New("missing architecture_task_session")
	}
	s := normalizeSession(env.ArchitectureTaskSession)
	if SessionDigest(s) != s.SessionDigestSHA256 {
		return Session{}, errors.New("task session digest mismatch")
	}
	return s, nil
}

func MarshalSessionYAML(s Session) ([]byte, error) {
	s = normalizeSession(s)
	s.SessionDigestSHA256 = SessionDigest(s)
	return yaml.Marshal(sessionEnvelope{ArchitectureTaskSession: s})
}

func SessionDigest(s Session) string {
	s = normalizeSession(s)
	s.SessionDigestSHA256 = ""
	return digest(canonicalJSON(s))
}

func LoadActivePointer(repoRoot string) (ActivePointer, error) {
	data, err := os.ReadFile(filepath.Join(repoRoot, ".sensei", "tasks", "active.yaml"))
	if err != nil {
		return ActivePointer{}, err
	}
	var env pointerEnvelope
	if err := yaml.Unmarshal(data, &env); err != nil {
		return ActivePointer{}, err
	}
	ptr := env.ArchitectureActiveTask
	ptr.SchemaVersion = strings.TrimSpace(ptr.SchemaVersion)
	ptr.TaskID = strings.TrimSpace(ptr.TaskID)
	ptr.RepositoryDomain = strings.TrimSpace(ptr.RepositoryDomain)
	ptr.Revision = strings.TrimSpace(ptr.Revision)
	ptr.GraphDigestSHA256 = strings.TrimSpace(ptr.GraphDigestSHA256)
	ptr.LedgerPath = filepath.ToSlash(strings.TrimSpace(ptr.LedgerPath))
	ptr.LedgerHeadDigestSHA256 = strings.TrimSpace(ptr.LedgerHeadDigestSHA256)
	ptr.SessionPath = filepath.ToSlash(strings.TrimSpace(ptr.SessionPath))
	ptr.SessionDigestSHA256 = strings.TrimSpace(ptr.SessionDigestSHA256)
	ptr.LastTaskControlDigestSHA256 = strings.TrimSpace(ptr.LastTaskControlDigestSHA256)
	if ptr.SchemaVersion != SchemaVersion || ptr.TaskID == "" || ptr.SessionPath == "" || ptr.SessionDigestSHA256 == "" {
		return ActivePointer{}, errors.New("invalid active task pointer")
	}
	if filepath.IsAbs(ptr.SessionPath) || strings.HasPrefix(ptr.SessionPath, "../") || strings.Contains(ptr.SessionPath, "/../") {
		return ActivePointer{}, errors.New("active task session_path must be repository-relative")
	}
	if ptr.LedgerPath != "" && (filepath.IsAbs(ptr.LedgerPath) || strings.HasPrefix(ptr.LedgerPath, "../") || strings.Contains(ptr.LedgerPath, "/../")) {
		return ActivePointer{}, errors.New("active task ledger_path must be repository-relative")
	}
	return ptr, nil
}

func WriteActivePointer(repoRoot string, ptr ActivePointer) error {
	ptr.SchemaVersion = SchemaVersion
	ptr.TaskID = strings.TrimSpace(ptr.TaskID)
	ptr.RepositoryDomain = strings.TrimSpace(ptr.RepositoryDomain)
	ptr.Revision = strings.TrimSpace(ptr.Revision)
	ptr.GraphDigestSHA256 = strings.TrimSpace(ptr.GraphDigestSHA256)
	ptr.LedgerPath = filepath.ToSlash(strings.TrimSpace(ptr.LedgerPath))
	ptr.LedgerHeadDigestSHA256 = strings.TrimSpace(ptr.LedgerHeadDigestSHA256)
	ptr.SessionPath = filepath.ToSlash(strings.TrimSpace(ptr.SessionPath))
	ptr.SessionDigestSHA256 = strings.TrimSpace(ptr.SessionDigestSHA256)
	ptr.LastTaskControlDigestSHA256 = strings.TrimSpace(ptr.LastTaskControlDigestSHA256)
	if ptr.TaskID == "" || ptr.RepositoryDomain == "" || ptr.Revision == "" || ptr.GraphDigestSHA256 == "" || ptr.SessionDigestSHA256 == "" || ptr.LastTaskControlDigestSHA256 == "" {
		return errors.New("active task pointer requires repository, revision, graph, session, and task-control digests")
	}
	if filepath.IsAbs(ptr.SessionPath) || strings.HasPrefix(ptr.SessionPath, "../") || strings.Contains(ptr.SessionPath, "/../") {
		return errors.New("active task session_path must be repository-relative")
	}
	if ptr.LedgerPath != "" && (filepath.IsAbs(ptr.LedgerPath) || strings.HasPrefix(ptr.LedgerPath, "../") || strings.Contains(ptr.LedgerPath, "/../")) {
		return errors.New("active task ledger_path must be repository-relative")
	}
	data, err := yaml.Marshal(pointerEnvelope{ArchitectureActiveTask: ptr})
	if err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(repoRoot, ".sensei", "tasks", "active.yaml"), data)
}

func stableTaskSessionID(taskID string) string {
	sum := sha1.Sum([]byte(strings.TrimSpace(taskID)))
	return "session." + hex.EncodeToString(sum[:])[:12]
}

func closurePolicyBinding() closureprotocol.PolicyBinding {
	return closureprotocol.PolicyBinding{
		Admission:        "admission.strict.v2",
		Certification:    "certification.architectural_closure.v1",
		Completion:       "completion.architectural_closure.v1",
		Revocation:       "revocation.architectural_closure.v1",
		Ledger:           "ledger.task.v1",
		Canonicalization: "canonicalization.architectural_closure.v1",
	}
}

func initializeLedgerState(repoRoot, repositoryDomain, taskRoot, taskID, sessionID string, taskReq TaskRequest, taskBytes, closureBytes, sessionBytes, controlBytes, statusBytes []byte, status string) (ledger.Head, error) {
	store := ledger.NewStore(taskRoot, ledger.WithPayloadValidator(func(eventType closureprotocol.LedgerEventType, mediaType string, data []byte) error {
		return ledger.ValidateTaskEventPayload(eventType, data)
	}))
	if report, err := store.Verify(); err == nil && report.Valid && report.EntryCount > 0 {
		chain, loadErr := store.VerifyChain()
		if loadErr == nil {
			return chain.Head, nil
		}
	}
	base, err := bindingpkg.ResolveBase(bindingpkg.ResolveBaseOptions{
		RepoRoot:         repoRoot,
		RepositoryDomain: repositoryDomain,
		GraphPath:        filepath.Join(taskRoot, "source", "graph.nt"),
		TaskID:           taskID,
		SessionID:        sessionID,
		Policies:         closurePolicyBinding(),
	})
	if err != nil {
		return ledger.Head{}, err
	}
	taskReqRef, err := store.StoreArtifactBytes(taskBytes, "application/yaml")
	if err != nil {
		return ledger.Head{}, err
	}
	closureReqRef, err := store.StoreArtifactBytes(closureBytes, "application/yaml")
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
	appendPayload := func(expected string, eventType closureprotocol.LedgerEventType, payload ledger.TaskEventPayload) (ledger.AppendResult, error) {
		return store.Append(context.Background(), ledger.AppendRequest{
			TaskID: taskID, SessionID: sessionID, ExpectedHeadDigestSHA256: expected,
			EventType: eventType, Payload: payload, PayloadMediaType: "application/yaml",
			ProducerID: GeneratedBy, ProducedAt: time.Unix(0, 0).UTC(),
		})
	}
	payload := func(eventType closureprotocol.LedgerEventType, limitations []string) ledger.TaskEventPayload {
		return ledger.TaskEventPayload{
			SchemaVersion: ledger.EventPayloadSchemaVersion,
			EventType:     eventType,
			TaskID:        taskID,
			SessionID:     sessionID,
			Status:        status,
			BaseBinding:   &base,
			Artifacts: map[string]closureprotocol.LedgerPayloadRef{
				"task_request":    taskReqRef,
				"closure_request": closureReqRef,
				"session":         sessionRef,
				"task_control":    controlRef,
				"status":          statusRef,
			},
			Limitations: append([]string(nil), limitations...),
		}
	}

	// Attempt typed authority resolution. The authentication receipts have no
	// expiry, but the repository-repair grant is time-bounded, so evaluate at
	// wall-clock time (re-runs on the same task short-circuit above, preserving
	// idempotency). resolved==nil keeps the legacy admission limitations.
	resolved, resolveLimitation := resolveTaskAuthority(repoRoot, taskRoot, taskReq, base, time.Now().UTC())
	preResolution := []string{
		"legacy_scope_admission",
		"typed_actor_authority_not_yet_resolved",
		"single_use_capability_not_available",
		"correctness_not_certified",
	}
	if resolveLimitation != "" {
		preResolution = append(preResolution, resolveLimitation)
	}
	headLimitations := preResolution
	if resolved != nil {
		// Typed authority was resolved and now drives the task: the "not yet
		// resolved" and "legacy admission" limitations are no longer true. The
		// single-use capability is consumed later, and correctness is never
		// certified here (Phase 6 owns it).
		headLimitations = []string{
			"single_use_capability_not_available",
			"correctness_not_certified",
		}
	}

	first, err := appendPayload("", closureprotocol.LedgerEventTaskPrepared, payload(closureprotocol.LedgerEventTaskPrepared, preResolution))
	if err != nil {
		return ledger.Head{}, err
	}
	second, err := appendPayload(first.Head.EntryDigestSHA256, closureprotocol.LedgerEventConvergenceAdvanced, payload(closureprotocol.LedgerEventConvergenceAdvanced, preResolution))
	if err != nil {
		return ledger.Head{}, err
	}
	third, err := appendPayload(second.Head.EntryDigestSHA256, closureprotocol.LedgerEventClosureAssessed, payload(closureprotocol.LedgerEventClosureAssessed, preResolution))
	if err != nil {
		return ledger.Head{}, err
	}
	// Record the typed authority resolution right after closure_assessed, when
	// one was produced — authority precedes admission. Preparation does NOT
	// append an admission_decided event: no typed AdmissionDecision or its
	// artifact exists yet, so emitting one here would be a semantically false v2
	// decision. The typed decision is produced later by admit-change.
	resolvedHead := third.Head.EntryDigestSHA256
	if resolved != nil {
		res, err := admission.RecordAuthorityResolved(store, resolvedHead, base.Task, resolved.Resolution, resolved.Actor, resolved.ChangePlan, base, time.Unix(0, 0).UTC())
		if err != nil {
			return ledger.Head{}, err
		}
		resolvedHead = res.Head.EntryDigestSHA256
	}
	final, err := appendPayload(resolvedHead, closureprotocol.LedgerEventTaskControlProjected, payload(closureprotocol.LedgerEventTaskControlProjected, headLimitations))
	if err != nil {
		return ledger.Head{}, err
	}
	if _, err := ledger.RebuildProjections(taskRoot, func(eventType closureprotocol.LedgerEventType, mediaType string, data []byte) error {
		return ledger.ValidateTaskEventPayload(eventType, data)
	}); err != nil {
		return ledger.Head{}, err
	}
	return final.Head, nil
}

func normalizePrepareOptions(opts PrepareOptions) PrepareOptions {
	if strings.TrimSpace(opts.RepoRoot) == "" {
		opts.RepoRoot = "."
	}
	opts.RepositoryDomain = strings.TrimSpace(opts.RepositoryDomain)
	opts.Description = strings.TrimSpace(opts.Description)
	opts.Mode = strings.TrimSpace(opts.Mode)
	opts.TaskClass = strings.TrimSpace(opts.TaskClass)
	opts.RiskClass = strings.TrimSpace(opts.RiskClass)
	opts.DirectionRequirement = strings.TrimSpace(opts.DirectionRequirement)
	opts.GraphNT = strings.TrimSpace(opts.GraphNT)
	opts.Claims = strings.TrimSpace(opts.Claims)
	opts.Dialogue = strings.TrimSpace(opts.Dialogue)
	opts.EvidenceState = strings.TrimSpace(opts.EvidenceState)
	opts.DirectionBootstrapAuthorization = strings.TrimSpace(opts.DirectionBootstrapAuthorization)
	opts.QuestionCreatedAt = strings.TrimSpace(opts.QuestionCreatedAt)
	if opts.QuestionCreatedAt == "" {
		opts.QuestionCreatedAt = "1970-01-01T00:00:00Z"
	}
	opts.RequestedBy = strings.TrimSpace(opts.RequestedBy)
	if opts.RequestedBy == "" {
		opts.RequestedBy = "coding_agent"
	}
	for i := range opts.Files {
		opts.Files[i].Path = normalizeRelPath(opts.Files[i].Path)
		opts.Files[i].Operation = strings.TrimSpace(opts.Files[i].Operation)
	}
	sort.SliceStable(opts.Files, func(i, j int) bool {
		if opts.Files[i].Path != opts.Files[j].Path {
			return opts.Files[i].Path < opts.Files[j].Path
		}
		return opts.Files[i].Operation < opts.Files[j].Operation
	})
	return opts
}

func validatePrepareOptions(opts PrepareOptions) error {
	var errs []string
	if opts.RepositoryDomain == "" {
		errs = append(errs, "--repo-domain is required")
	}
	if opts.Description == "" {
		errs = append(errs, "--description is required")
	}
	if opts.Mode != admission.ModeInspect && opts.Mode != admission.ModeModify {
		errs = append(errs, "--mode must be inspect or modify")
	}
	if opts.TaskClass == "" {
		errs = append(errs, "--task-class is required")
	}
	if opts.RiskClass == "" {
		errs = append(errs, "--risk-class is required")
	}
	if opts.DirectionRequirement == "" {
		errs = append(errs, "--direction is required")
	}
	if opts.GraphNT == "" {
		errs = append(errs, "--graph-nt is required")
	}
	if len(opts.Files) == 0 {
		errs = append(errs, "at least one --file operation:path scope anchor is required")
	}
	modifies := 0
	seen := map[string]string{}
	for _, f := range opts.Files {
		if f.Path == "" || !safeRelPath(f.Path) {
			errs = append(errs, "file path must be repository-relative and non-escaping")
			continue
		}
		if strings.ContainsAny(f.Path, "*?[") {
			errs = append(errs, "directory wildcards are not supported")
		}
		if f.Operation != admission.OperationRead && f.Operation != admission.OperationModify {
			errs = append(errs, "file operation must be read or modify")
		}
		if prev, ok := seen[f.Path]; ok && prev != f.Operation {
			errs = append(errs, "file path has conflicting operations: "+f.Path)
		}
		seen[f.Path] = f.Operation
		if f.Operation == admission.OperationModify {
			modifies++
		}
	}
	if opts.Mode == admission.ModeModify && modifies == 0 {
		errs = append(errs, "modify mode requires at least one modify file")
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func normalizeTaskRequest(req TaskRequest) TaskRequest {
	req.SchemaVersion = strings.TrimSpace(req.SchemaVersion)
	if req.SchemaVersion == "" {
		req.SchemaVersion = SchemaVersion
	}
	req.TaskID = strings.TrimSpace(req.TaskID)
	req.Binding.RepositoryDomain = strings.TrimSpace(req.Binding.RepositoryDomain)
	req.Binding.Revision = strings.TrimSpace(req.Binding.Revision)
	req.Binding.RevisionStatus = strings.TrimSpace(req.Binding.RevisionStatus)
	req.Binding.GraphDigestSHA256 = strings.TrimSpace(req.Binding.GraphDigestSHA256)
	req.Binding.GraphDigestStatus = strings.TrimSpace(req.Binding.GraphDigestStatus)
	req.Description = strings.TrimSpace(req.Description)
	req.Mode = strings.TrimSpace(req.Mode)
	req.TaskClass = strings.TrimSpace(req.TaskClass)
	req.RiskClass = strings.TrimSpace(req.RiskClass)
	req.DirectionRequirement = strings.TrimSpace(req.DirectionRequirement)
	req.RequestedBy = strings.TrimSpace(req.RequestedBy)
	for i := range req.Scope.Files {
		req.Scope.Files[i].Path = normalizeRelPath(req.Scope.Files[i].Path)
		req.Scope.Files[i].Operation = strings.TrimSpace(req.Scope.Files[i].Operation)
	}
	sort.SliceStable(req.Scope.Files, func(i, j int) bool {
		if req.Scope.Files[i].Path != req.Scope.Files[j].Path {
			return req.Scope.Files[i].Path < req.Scope.Files[j].Path
		}
		return req.Scope.Files[i].Operation < req.Scope.Files[j].Operation
	})
	req.Scope.Files = dedupeFiles(req.Scope.Files)
	req.Scope.Symbols = cleanStrings(req.Scope.Symbols)
	req.Scope.Components = cleanStrings(req.Scope.Components)
	req.Scope.ClaimIDs = cleanStrings(req.Scope.ClaimIDs)
	req.Scope.PropositionKeys = cleanStrings(req.Scope.PropositionKeys)
	return req
}

func validateTaskRequest(req TaskRequest) error {
	if req.SchemaVersion != SchemaVersion || req.TaskID == "" || req.Binding.RepositoryDomain == "" || req.Binding.Revision == "" || req.Binding.GraphDigestSHA256 == "" || req.Description == "" || req.Mode == "" || req.TaskClass == "" || req.RiskClass == "" || req.DirectionRequirement == "" {
		return errors.New("task request missing required fields")
	}
	if req.Binding.RevisionStatus != architecture.RevisionResolved || req.Binding.GraphDigestStatus != architecture.GraphDigestResolved {
		return errors.New("task request requires resolved revision and graph digest")
	}
	if len(req.Scope.Files)+len(req.Scope.Symbols)+len(req.Scope.Components)+len(req.Scope.ClaimIDs)+len(req.Scope.PropositionKeys) == 0 {
		return errors.New("task request requires exact scope")
	}
	return nil
}

func defaultArtifactRefs() ArtifactRefs {
	return ArtifactRefs{
		TaskRequest:                  "task-request.yaml",
		ClosureRequest:               "closure-request.yaml",
		DirectionBootstrap:           "governance/bootstrap-direction-authorization.yaml",
		AwarenessMutationEnforcement: "source/awareness-mutation-enforcement.yaml",
		Claims:                       "source/claims.yaml",
		Dialogue:                     "source/dialogue.yaml",
		EvidenceState:                "source/evidence-state.yaml",
		KnowledgeBundle:              "source/knowledge",
		GraphSnapshot:                "source/graph.nt",
		GraphReceipt:                 "source/graph-receipt.yaml",
		ConvergenceBundle:            "convergence",
		ConvergenceSession:           "convergence/session.yaml",
		AdmissionRequest:             "admission/request.yaml",
		AdmissionDecision:            "admission/decision.yaml",
		AdmissionVerification:        "admission/verification.yaml",
		PrepareReceipt:               "receipts/prepare-change.yaml",
		StatusReceipt:                "receipts/task-status.yaml",
	}
}

func closureRequestFromTask(req TaskRequest, awarenessMutation *closure.AwarenessMutationBinding) closure.Request {
	files := make([]string, 0, len(req.Scope.Files))
	for _, f := range req.Scope.Files {
		files = append(files, f.Path)
	}
	access := closure.AccessRead
	if req.Mode == admission.ModeModify {
		access = closure.AccessReadWrite
	}
	return closure.Request{
		SchemaVersion: closure.SchemaVersion,
		TaskID:        req.TaskID,
		Binding:       req.Binding,
		Scope: closure.Scope{
			Domain:               req.Binding.RepositoryDomain,
			TaskClass:            req.TaskClass,
			RiskClass:            req.RiskClass,
			AccessMode:           access,
			DirectionRequirement: req.DirectionRequirement,
			Files:                files,
		},
		AwarenessMutation: awarenessMutation,
		RequestedBy:       req.RequestedBy,
		Note:              req.Description,
	}
}

func loadDirectionBootstrapAuthorization(repoRoot string, req TaskRequest, path string) (*closure.DirectionBootstrapAuthorization, []byte, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, nil, nil
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve bootstrap direction authorization: %w", err)
	}
	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, nil, fmt.Errorf("read bootstrap direction authorization: %w", err)
	}
	auth, err := closure.UnmarshalDirectionBootstrapYAML(data)
	if err != nil {
		return nil, nil, fmt.Errorf("load bootstrap direction authorization: %w", err)
	}
	if auth.TaskID != req.TaskID {
		return nil, nil, fmt.Errorf("invalid bootstrap direction authorization: task_id %s does not match %s", auth.TaskID, req.TaskID)
	}
	if auth.BaseRevision != req.Binding.Revision {
		return nil, nil, fmt.Errorf("invalid bootstrap direction authorization: base_revision does not match task binding")
	}
	if auth.GraphDigestSHA256 != req.Binding.GraphDigestSHA256 {
		return nil, nil, fmt.Errorf("invalid bootstrap direction authorization: graph_digest_sha256 does not match task binding")
	}
	auth.ApprovalSourcePath = absPath
	auth.ApprovalSourceDigestSHA256 = digest(data)
	auth.AuthorizationDigestSHA256 = closure.DirectionBootstrapAuthorizationDigest(auth)
	if err := closure.ValidateDirectionBootstrapApproval(auth, repoRoot); err != nil {
		return nil, nil, fmt.Errorf("invalid bootstrap direction authorization: %w", err)
	}
	if err := rejectConsumedDirectionBootstrapAuthorization(repoRoot, auth.AuthorizationDigestSHA256); err != nil {
		return nil, nil, err
	}
	snapshot, err := closure.MarshalCanonicalDirectionBootstrapYAML(auth)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal bootstrap direction authorization: %w", err)
	}
	return &auth, snapshot, nil
}

func rejectConsumedDirectionBootstrapAuthorization(repoRoot, authDigest string) error {
	if !isSHA256(authDigest) {
		return errors.New("invalid bootstrap direction authorization digest")
	}
	globalReceiptPath := filepath.Join(repoRoot, ".sensei", "bootstrap-direction-consumption", authDigest+".yaml")
	if receipt, err := admission.LoadBootstrapDirectionConsumption(globalReceiptPath); err == nil {
		return fmt.Errorf("bootstrap direction authorization already consumed by task %s", receipt.TaskID)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("load bootstrap direction consumption receipt %s: %w", globalReceiptPath, err)
	}
	taskRoot := filepath.Join(repoRoot, ".sensei", "tasks")
	entries, err := os.ReadDir(taskRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read bootstrap direction consumption receipts: %w", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		receiptPath := filepath.Join(taskRoot, entry.Name(), "receipts", "bootstrap-direction-consumption.yaml")
		receipt, err := admission.LoadBootstrapDirectionConsumption(receiptPath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("load bootstrap direction consumption receipt %s: %w", receiptPath, err)
		}
		if receipt.AuthorizationDigestSHA256 == authDigest {
			return fmt.Errorf("bootstrap direction authorization already consumed by task %s", receipt.TaskID)
		}
	}
	return nil
}

func loadPrepareClaims(repoRoot string, binding architecture.ClaimDocumentBinding, override string) (architecture.ClaimDocument, error) {
	path := strings.TrimSpace(override)
	if path == "" {
		path = filepath.Join(repoRoot, ".sensei", "project", "claims.yaml")
	}
	doc, err := architecture.LoadClaimDocument(path)
	if err != nil {
		if os.IsNotExist(err) && strings.TrimSpace(override) == "" {
			return architecture.ClaimDocument{}, fmt.Errorf("task input incomplete: inference not run; expected %s", path)
		}
		return architecture.ClaimDocument{}, fmt.Errorf("task input incomplete: load architecture claims: %w", err)
	}
	if len(doc.Claims) == 0 {
		return architecture.ClaimDocument{}, errors.New("task input incomplete: inference produced no architecture claims")
	}
	if doc.Binding.RepositoryDomain != binding.RepositoryDomain ||
		doc.Binding.Revision != binding.Revision ||
		doc.Binding.RevisionStatus != binding.RevisionStatus ||
		doc.Binding.GraphDigestSHA256 != binding.GraphDigestSHA256 ||
		doc.Binding.GraphDigestStatus != binding.GraphDigestStatus {
		return architecture.ClaimDocument{}, errors.New("task input incomplete: architecture claims binding does not match the repository revision and graph snapshot")
	}
	return doc, nil
}

func prepareSourceDocuments(repoRoot, taskRoot string, binding architecture.ClaimDocumentBinding, claims architecture.ClaimDocument, opts PrepareOptions) error {
	data, err := architecture.MarshalCanonicalClaimDocumentYAML(claims)
	if err != nil {
		return err
	}
	if err := writeFileAtomic(filepath.Join(taskRoot, "source", "claims.yaml"), data); err != nil {
		return err
	}
	if opts.Dialogue != "" {
		doc, err := architecture.LoadDialogueDocument(opts.Dialogue)
		if err != nil {
			return err
		}
		data, err := architecture.MarshalCanonicalDialogueDocumentYAML(doc)
		if err != nil {
			return err
		}
		if err := writeFileAtomic(filepath.Join(taskRoot, "source", "dialogue.yaml"), data); err != nil {
			return err
		}
	} else {
		doc := architecture.DialogueDocument{SchemaVersion: SchemaVersion, CompiledBy: GeneratedBy, Binding: binding, OpenQuestions: []architecture.OpenQuestion{}}
		data, err := architecture.MarshalCanonicalDialogueDocumentYAML(doc)
		if err != nil {
			return err
		}
		if err := writeFileAtomic(filepath.Join(taskRoot, "source", "dialogue.yaml"), data); err != nil {
			return err
		}
	}
	evidencePath := opts.EvidenceState
	if evidencePath == "" {
		projectEvidence := filepath.Join(repoRoot, ".sensei", "project", "evidence-state.yaml")
		if _, err := os.Stat(projectEvidence); err == nil {
			evidencePath = projectEvidence
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	if evidencePath != "" {
		doc, err := maintenance.LoadEvidenceStateDocument(evidencePath)
		if err != nil {
			return err
		}
		if err := maintenance.ValidateEvidenceStateDocument(doc, &binding); err != nil {
			return fmt.Errorf("task input incomplete: project Evidence state: %w", err)
		}
		data, err := maintenance.MarshalCanonicalEvidenceStateYAML(doc)
		if err != nil {
			return err
		}
		return writeFileAtomic(filepath.Join(taskRoot, "source", "evidence-state.yaml"), data)
	}
	doc := maintenance.EvidenceStateDocument{SchemaVersion: SchemaVersion, GeneratedBy: GeneratedBy, Binding: binding, Evidence: []maintenance.EvidenceState{}}
	data, err = maintenance.MarshalCanonicalEvidenceStateYAML(doc)
	if err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(taskRoot, "source", "evidence-state.yaml"), data)
}

type knowledgeSnapshotManifest struct {
	SchemaVersion  string   `yaml:"schema_version"`
	GeneratedBy    string   `yaml:"generated_by"`
	PolicyID       string   `yaml:"machine_adoption_risk_policy"`
	RiskClass      string   `yaml:"risk_class"`
	PreservesBytes bool     `yaml:"preserves_source_receipt_bytes"`
	Files          []string `yaml:"files"`
}

func snapshotProjectKnowledge(repoRoot, taskRoot, riskClass string) error {
	targetRoot := filepath.Join(taskRoot, "source", "knowledge")
	if err := os.MkdirAll(targetRoot, 0o755); err != nil {
		return err
	}
	type copySpec struct {
		source string
		target string
	}
	var specs []copySpec
	projectRoot := filepath.Join(repoRoot, ".sensei", "project", "knowledge")
	if _, err := os.Stat(projectRoot); err == nil {
		err = filepath.WalkDir(projectRoot, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.Type()&os.ModeSymlink != 0 {
				if entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if entry.IsDir() {
				return nil
			}
			rel, relErr := filepath.Rel(projectRoot, path)
			if relErr != nil || !safeRelPath(filepath.ToSlash(rel)) {
				return fmt.Errorf("invalid project knowledge path %s", path)
			}
			specs = append(specs, copySpec{source: path, target: filepath.Join(targetRoot, filepath.FromSlash(filepath.ToSlash(rel)))})
			return nil
		})
		if err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	intentPaths, err := filepath.Glob(filepath.Join(repoRoot, "docs", "awareness", "intent_*.yaml"))
	if err != nil {
		return err
	}
	sort.Strings(intentPaths)
	for _, source := range intentPaths {
		info, statErr := os.Lstat(source)
		if statErr != nil {
			return statErr
		}
		if !info.Mode().IsRegular() {
			continue
		}
		specs = append(specs, copySpec{source: source, target: filepath.Join(targetRoot, "intents", filepath.Base(source))})
	}
	sort.Slice(specs, func(i, j int) bool { return specs[i].target < specs[j].target })
	manifest := knowledgeSnapshotManifest{
		SchemaVersion: "1", GeneratedBy: GeneratedBy, PolicyID: "task.machine_adopted_knowledge.v1",
		RiskClass: riskClass, PreservesBytes: true, Files: []string{},
	}
	for _, spec := range specs {
		data, readErr := os.ReadFile(spec.source)
		if readErr != nil {
			return readErr
		}
		if writeErr := writeFileAtomic(spec.target, data); writeErr != nil {
			return writeErr
		}
		rel, _ := filepath.Rel(targetRoot, spec.target)
		manifest.Files = append(manifest.Files, filepath.ToSlash(rel))
	}
	manifestData, err := yaml.Marshal(manifest)
	if err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(targetRoot, "manifest.yaml"), manifestData)
}

func snapshotAwarenessMutationEnforcement(repoRoot, taskRoot string, req TaskRequest) (*closure.AwarenessMutationBinding, error) {
	var plans []closure.AwarenessMutationEnforcementPlan
	for _, file := range req.Scope.Files {
		if file.Operation != admission.OperationModify {
			continue
		}
		full := filepath.Join(repoRoot, filepath.FromSlash(file.Path))
		info, err := os.Lstat(full)
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			continue
		}
		desc, ok, err := extractor.DescribeAwarenessSource(full)
		if err != nil {
			return nil, err
		}
		if !ok || !desc.CanonicalMutationEligible {
			continue
		}
		plans = append(plans, closure.AwarenessMutationEnforcementPlan{
			SourcePath:  file.Path,
			SourceClass: desc.SourceClass,
			ImporterID:  desc.ImporterID,
			RequiredPreconditions: []string{
				"existing regular repository file",
				"canonical governed awareness surface",
				"registered closed-schema importer",
				"exact modify operation",
				"no task-local knowledge imported as canonical truth",
			},
			RequiredVerification: []string{
				"sensei_check",
				"sensei_validate",
				"strict_build",
				"canonical_graph_purity",
				"owner_resolution",
				"authority_scope_validation",
			},
			Limitations: []string{
				"enforcement proves valid governed structure and graph compilation coverage, not architectural optimality or correctness",
			},
		})
	}
	if len(plans) == 0 {
		return nil, nil
	}
	doc := closure.AwarenessMutationEnforcementDocument{
		SchemaVersion:      "1",
		PolicyID:           closure.AwarenessMutationEnforcementPolicyV1,
		TaskID:             req.TaskID,
		RepositoryRevision: req.Binding.Revision,
		GraphDigestSHA256:  req.Binding.GraphDigestSHA256,
		Plans:              plans,
	}
	data, err := closure.MarshalCanonicalAwarenessMutationEnforcementYAML(doc)
	if err != nil {
		return nil, err
	}
	target := filepath.Join(taskRoot, "source", "awareness-mutation-enforcement.yaml")
	if err := writeFileAtomic(target, data); err != nil {
		return nil, err
	}
	digest, err := closure.AwarenessMutationEnforcementDigest(doc)
	if err != nil {
		return nil, err
	}
	return &closure.AwarenessMutationBinding{
		TaskID:           req.TaskID,
		Path:             filepath.ToSlash(filepath.Join(".sensei", "tasks", req.TaskID, "source", "awareness-mutation-enforcement.yaml")),
		PlanDigestSHA256: digest,
		PolicyID:         closure.AwarenessMutationEnforcementPolicyV1,
	}, nil
}

func buildSession(req TaskRequest, refs ArtifactRefs, conv convergence.StatusReport, decision admission.Decision, limitations []architecture.Limitation) Session {
	waiting := cleanStrings(conv.WaitClasses)
	phase := PhaseWaiting
	status := statusFromWait(waiting)
	if decision.Decision == admission.DecisionUncertifiable || conv.Status == convergence.StatusUncertifiable {
		phase = PhaseUncertifiable
		status = StatusUncertifiable
	} else if decision.Decision == admission.DecisionRefused {
		status = StatusRefused
	} else if decision.MutationCapability == admission.CapabilityAdmitted || decision.MutationCapability == admission.CapabilityAdmittedWithConditions {
		phase = PhaseAdmitted
		status = StatusReadyForMutation
	} else if decision.InspectionCapability == admission.CapabilityAdmitted && req.Mode == admission.ModeInspect {
		phase = PhaseAdmitted
		status = StatusReadyForInspection
	}
	next := primaryNext(req, conv, decision, status)
	var lim []string
	for _, l := range limitations {
		if l.Reason != "" {
			lim = append(lim, l.Reason)
		}
	}
	s := Session{
		SchemaVersion:        SchemaVersion,
		GeneratedBy:          GeneratedBy,
		TaskID:               req.TaskID,
		Binding:              req.Binding,
		WorkflowPhase:        phase,
		OperationalStatus:    status,
		TaskRequest:          req,
		Artifacts:            refs,
		ClosureVerdict:       conv.ClosureVerdict,
		ConvergenceStatus:    conv.Status,
		AdmissionDecision:    decision.Decision,
		InspectionCapability: decision.InspectionCapability,
		MutationCapability:   decision.MutationCapability,
		WaitingOn:            waiting,
		ReadEnvelope:         cleanStrings(decision.Envelope.ReadPaths),
		ModifyEnvelope:       cleanStrings(decision.Envelope.ModifyPaths),
		NextActions:          []NextAction{next},
		Limitations:          cleanStrings(lim),
	}
	s.SessionDigestSHA256 = SessionDigest(s)
	return s
}

func statusFromWait(waiting []string) string {
	for _, w := range waiting {
		switch w {
		case convergence.WaitArchitect:
			return StatusWaitingArchitect
		case convergence.WaitEvidence:
			return StatusWaitingEvidence
		case convergence.WaitGovernance:
			return StatusWaitingGovernance
		case convergence.WaitMechanicalRepair:
			return StatusWaitingMechanical
		}
	}
	return StatusUncertifiable
}

func primaryNext(req TaskRequest, conv convergence.StatusReport, decision admission.Decision, status string) NextAction {
	if status == StatusReadyForMutation {
		return NextAction{Action: NextPerformEdit, Reference: strings.Join(decision.Envelope.ModifyPaths, ", "), Summary: "edit only the admitted modify envelope"}
	}
	if status == StatusReadyForInspection {
		return NextAction{Action: "inspect admitted envelope", Reference: strings.Join(decision.Envelope.ReadPaths, ", ")}
	}
	for _, a := range conv.NextActions {
		switch a.Class {
		case convergence.WaitArchitect:
			return NextAction{Action: NextAnswerQuestion, Reference: a.Reference, Summary: a.Summary}
		case convergence.WaitEvidence:
			return NextAction{Action: NextProvideEvidence, Reference: a.Reference, Summary: a.Summary}
		case convergence.WaitGovernance:
			return NextAction{Action: NextProposeKnowledge, Reference: a.Reference, Summary: a.Summary}
		}
	}
	if status == StatusRefused || status == StatusUncertifiable {
		return NextAction{Action: NextProvideInput, Summary: "repair binding or task inputs before mutation"}
	}
	if decision.MutationCapability == admission.CapabilityWaiting || decision.InspectionCapability == admission.CapabilityWaiting {
		return NextAction{Action: NextAdvanceConverge, Reference: req.TaskID}
	}
	return NextAction{Action: NextCompleteProof, Summary: "external proof is still required; correctness is not certified"}
}

func resultFromSession(repoRoot, taskRoot string, s Session, disposition string) PrepareResult {
	rel, _ := filepath.Rel(repoRoot, taskRoot)
	return PrepareResult{
		TaskID:         s.TaskID,
		TaskDir:        filepath.ToSlash(rel),
		GraphState:     s.Binding.GraphDigestStatus,
		Closure:        s.ClosureVerdict,
		Convergence:    s.ConvergenceStatus,
		Inspect:        s.InspectionCapability,
		Modify:         s.MutationCapability,
		WaitingOn:      s.WaitingOn,
		ReadEnvelope:   s.ReadEnvelope,
		ModifyEnvelope: s.ModifyEnvelope,
		Next:           firstNext(s),
		Session:        s,
		Disposition:    disposition,
	}
}

func firstNext(s Session) NextAction {
	if len(s.NextActions) == 0 {
		return NextAction{Action: NextProvideInput}
	}
	return s.NextActions[0]
}

func verifySession(repoRoot, taskDir string, s Session, ptr *ActivePointer) []string {
	var errs []string
	if SessionDigest(s) != s.SessionDigestSHA256 {
		errs = append(errs, "session digest mismatch")
	}
	if ptr != nil {
		if ptr.TaskID != s.TaskID {
			errs = append(errs, "active pointer task_id mismatch")
		}
		if ptr.SessionDigestSHA256 != s.SessionDigestSHA256 {
			errs = append(errs, "active pointer session digest mismatch")
		}
		if ptr.RepositoryDomain == "" {
			errs = append(errs, "task.binding.repository_domain_missing")
		} else if ptr.RepositoryDomain != s.Binding.RepositoryDomain {
			errs = append(errs, "task.binding.repository_domain_mismatch")
		}
		if ptr.Revision == "" {
			errs = append(errs, "task.binding.revision_missing")
		} else if ptr.Revision != s.Binding.Revision {
			errs = append(errs, "task.binding.revision_mismatch")
		}
		if ptr.GraphDigestSHA256 == "" {
			errs = append(errs, "task.binding.graph_digest_missing")
		} else if ptr.GraphDigestSHA256 != s.Binding.GraphDigestSHA256 {
			errs = append(errs, "task.binding.graph_digest_mismatch")
		}
		if ptr.LastTaskControlDigestSHA256 == "" {
			errs = append(errs, "task.binding.task_control_digest_missing")
		} else {
			if state, err := LoadTaskControl(filepath.Join(taskDir, "control", "latest.yaml")); err != nil || state.ReceiptDigestSHA256 != ptr.LastTaskControlDigestSHA256 {
				errs = append(errs, "task.binding.task_control_digest_mismatch")
			}
		}
		if ptr.LedgerPath != "" {
			store := ledger.NewStore(taskDir, ledger.WithPayloadValidator(func(eventType closureprotocol.LedgerEventType, mediaType string, data []byte) error {
				return ledger.ValidateTaskEventPayload(eventType, data)
			}))
			report, err := store.Verify()
			if err != nil || !report.Valid {
				errs = append(errs, "task.binding.ledger_invalid")
			} else {
				if ptr.LedgerHeadDigestSHA256 != "" && report.HeadDigestSHA256 != ptr.LedgerHeadDigestSHA256 {
					errs = append(errs, "task.binding.ledger_head_mismatch")
				}
				if ptr.LedgerSequence != 0 && report.EntryCount != ptr.LedgerSequence {
					errs = append(errs, "task.binding.ledger_sequence_mismatch")
				}
				if report.ProjectionState == "projection_drift" {
					errs = append(errs, "task.binding.projection_drift")
				}
			}
		}
	}
	graphPath := filepath.Join(taskDir, filepath.FromSlash(s.Artifacts.GraphSnapshot))
	data, err := os.ReadFile(graphPath)
	if err != nil {
		errs = append(errs, "graph snapshot unreadable")
	} else if digest(data) != s.Binding.GraphDigestSHA256 {
		errs = append(errs, "graph digest mismatch")
	}
	revision, status, _ := architecture.ResolveRevision(repoRoot, true)
	if status != architecture.RevisionResolved || revision != s.Binding.Revision {
		errs = append(errs, "repository revision changed")
	}
	if s.TaskRequest.OutsideModifyDigest != "" {
		current, err := outsideModifyDigest(repoRoot, s.TaskRequest.Scope.Files)
		if err != nil || current != s.TaskRequest.OutsideModifyDigest {
			errs = append(errs, "task.binding.working_tree_outside_envelope")
		}
	}
	for _, rel := range []string{s.Artifacts.TaskRequest, s.Artifacts.ClosureRequest, s.Artifacts.Claims, s.Artifacts.Dialogue, s.Artifacts.EvidenceState, s.Artifacts.KnowledgeBundle, s.Artifacts.ConvergenceSession, s.Artifacts.AdmissionDecision} {
		if _, err := os.Stat(filepath.Join(taskDir, filepath.FromSlash(rel))); err != nil {
			errs = append(errs, "missing artifact "+rel)
		}
	}
	return cleanStrings(errs)
}

func normalizeSession(s Session) Session {
	s.SchemaVersion = strings.TrimSpace(s.SchemaVersion)
	s.GeneratedBy = strings.TrimSpace(s.GeneratedBy)
	s.TaskID = strings.TrimSpace(s.TaskID)
	s.WorkflowPhase = strings.TrimSpace(s.WorkflowPhase)
	s.OperationalStatus = strings.TrimSpace(s.OperationalStatus)
	s.ClosureVerdict = strings.TrimSpace(s.ClosureVerdict)
	s.ConvergenceStatus = strings.TrimSpace(s.ConvergenceStatus)
	s.AdmissionDecision = strings.TrimSpace(s.AdmissionDecision)
	s.InspectionCapability = strings.TrimSpace(s.InspectionCapability)
	s.MutationCapability = strings.TrimSpace(s.MutationCapability)
	s.SessionDigestSHA256 = strings.TrimSpace(s.SessionDigestSHA256)
	s.WaitingOn = cleanStrings(s.WaitingOn)
	s.ReadEnvelope = cleanStrings(s.ReadEnvelope)
	s.ModifyEnvelope = cleanStrings(s.ModifyEnvelope)
	s.Limitations = cleanStrings(s.Limitations)
	s.TaskRequest = normalizeTaskRequest(s.TaskRequest)
	for i := range s.NextActions {
		s.NextActions[i].Action = strings.TrimSpace(s.NextActions[i].Action)
		s.NextActions[i].Reference = strings.TrimSpace(s.NextActions[i].Reference)
		s.NextActions[i].Summary = strings.TrimSpace(s.NextActions[i].Summary)
	}
	return s
}

func outsideModifyDigest(repoRoot string, files []FileOperation) (string, error) {
	excluded := map[string]bool{}
	for _, file := range files {
		if file.Operation == admission.OperationModify {
			excluded[filepath.ToSlash(filepath.Clean(file.Path))] = true
		}
	}
	cmd := exec.Command("git", "ls-files", "-co", "--exclude-standard", "-z")
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	var paths []string
	for _, raw := range strings.Split(string(out), "\x00") {
		rel := filepath.ToSlash(strings.TrimSpace(raw))
		if rel == "" || excluded[rel] || strings.HasPrefix(rel, ".sensei/") || strings.HasPrefix(rel, ".git/") {
			continue
		}
		paths = append(paths, rel)
	}
	paths = cleanStrings(paths)
	h := sha256.New()
	for _, rel := range paths {
		data, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(rel)))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		h.Write([]byte(rel))
		h.Write([]byte{0})
		h.Write(data)
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func admissionFiles(files []FileOperation) []admission.FileOperation {
	out := make([]admission.FileOperation, 0, len(files))
	for _, f := range files {
		out = append(out, admission.FileOperation{Path: f.Path, Operation: f.Operation})
	}
	return out
}

func dedupeFiles(in []FileOperation) []FileOperation {
	var out []FileOperation
	seen := map[string]bool{}
	for _, f := range in {
		key := f.Operation + "\x00" + f.Path
		if f.Path != "" && !seen[key] {
			seen[key] = true
			out = append(out, f)
		}
	}
	return out
}

func cleanStrings(in []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out
}

func normalizeRelPath(path string) string {
	return filepath.ToSlash(filepath.Clean(strings.TrimSpace(path)))
}

func safeRelPath(path string) bool {
	path = filepath.ToSlash(strings.TrimSpace(path))
	return path != "" && path != "." && !filepath.IsAbs(path) && path != ".." && !strings.HasPrefix(path, "../") && !strings.Contains(path, "/../")
}

func stableToken(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	re := regexp.MustCompile(`[^a-z0-9]+`)
	v = strings.Trim(re.ReplaceAllString(v, "-"), "-")
	if v == "" {
		return "task"
	}
	if len(v) > 48 {
		return v[:48]
	}
	return v
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func isSHA256(v string) bool {
	if len(v) != 64 {
		return false
	}
	for _, r := range v {
		switch {
		case r >= '0' && r <= '9':
		case r >= 'a' && r <= 'f':
		default:
			return false
		}
	}
	return true
}

func canonicalJSON(v any) []byte {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return data
}

func writeFileAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	cleanup = false
	return nil
}
