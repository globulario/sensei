// SPDX-License-Identifier: Apache-2.0

package probeexec

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/maintenance"
	"github.com/globulario/sensei/golang/architecture/probe"
)

const (
	ExecutorID = "sensei static-probe-executor/v1"

	ReasonKindUnsupported = "task.probe.kind_unsupported"
	ReasonScopeEscape     = "task.probe.scope_escape"
	ReasonBudgetExhausted = "task.probe.budget_exhausted"
	ReasonInputStale      = "task.probe.input_stale"
	ReasonCommandRejected = "task.probe.command_forbidden"
	ReasonSecretRejected  = "task.probe.secret_forbidden"
)

type Budget struct {
	MaxProbes int   `json:"max_probes" yaml:"max_probes"`
	MaxFiles  int   `json:"max_files" yaml:"max_files"`
	MaxBytes  int64 `json:"max_bytes" yaml:"max_bytes"`
}

func DefaultBudget() Budget {
	return Budget{MaxProbes: 32, MaxFiles: 128, MaxBytes: 16 << 20}
}

type Decision struct {
	ProbeID    string `json:"probe_id" yaml:"probe_id"`
	Eligible   bool   `json:"eligible" yaml:"eligible"`
	Executed   bool   `json:"executed" yaml:"executed"`
	Replayed   bool   `json:"replayed" yaml:"replayed"`
	ReasonCode string `json:"reason_code,omitempty" yaml:"reason_code,omitempty"`
	ResultID   string `json:"result_id,omitempty" yaml:"result_id,omitempty"`
}

type Metrics struct {
	ProbesConsidered int   `json:"probes_considered" yaml:"probes_considered"`
	ProbesExecuted   int   `json:"probes_executed" yaml:"probes_executed"`
	FilesRead        int   `json:"files_read" yaml:"files_read"`
	BytesRead        int64 `json:"bytes_read" yaml:"bytes_read"`
	ReplayPrevented  int   `json:"duplicate_executions_prevented" yaml:"duplicate_executions_prevented"`
	BudgetExhausted  bool  `json:"budget_exhausted" yaml:"budget_exhausted"`
}

type Context struct {
	RepositoryRoot      string
	Binding             architecture.ClaimDocumentBinding
	Probes              probe.ProbeDocument
	ProbeDocumentDigest string
	Claims              architecture.ClaimDocument
	ExistingResults     *probe.ResultDocument
	EvidenceState       maintenance.EvidenceStateDocument
	ObservedAt          string
	Budget              Budget
}

type BatchResult struct {
	Results       probe.ResultDocument
	EvidenceState maintenance.EvidenceStateDocument
	Decisions     []Decision
	Metrics       Metrics
}

type Executor interface {
	Kind() string
	Validate(probe.EvidenceProbe) error
	Execute(*executionContext, probe.EvidenceProbe) (probe.ProbeResult, error)
}

type sourceReceiptExecutor struct{}

func (sourceReceiptExecutor) Kind() string { return probe.KindSourceReceiptVerification }

func (sourceReceiptExecutor) Validate(p probe.EvidenceProbe) error {
	if p.ProbeKind != probe.KindSourceReceiptVerification {
		return errors.New(ReasonKindUnsupported)
	}
	if p.SafetyClass != probe.SafetyStaticRead || p.ApprovalGate != probe.GateNone || !p.AutomaticExecutionAllowed {
		return errors.New("task.probe.safety_not_automatic")
	}
	for _, step := range p.Steps {
		if strings.TrimSpace(step.Command) != "" {
			return errors.New(ReasonCommandRejected)
		}
		if step.Kind != probe.StepVerifySourceDigest && step.Kind != probe.StepInspectSource {
			return errors.New(ReasonKindUnsupported)
		}
	}
	return nil
}

type executionContext struct {
	repoRoot   string
	observedAt string
	facts      map[string]architecture.ClaimFactReceipt
	files      map[string][]byte
	metrics    *Metrics
	budget     Budget
}

func ExecuteBatch(ctx Context) (BatchResult, error) {
	ctx.RepositoryRoot = strings.TrimSpace(ctx.RepositoryRoot)
	if ctx.RepositoryRoot == "" {
		return BatchResult{}, errors.New("repository root is required")
	}
	abs, err := filepath.Abs(ctx.RepositoryRoot)
	if err != nil {
		return BatchResult{}, err
	}
	ctx.RepositoryRoot = abs
	if !bindingEqual(ctx.Binding, ctx.Probes.Binding) || !bindingEqual(ctx.Binding, ctx.Claims.Binding) || !bindingEqual(ctx.Binding, ctx.EvidenceState.Binding) {
		return BatchResult{}, errors.New(ReasonInputStale)
	}
	if len(ctx.ProbeDocumentDigest) != 64 {
		return BatchResult{}, errors.New("probe document digest is required")
	}
	if ctx.Budget.MaxProbes <= 0 || ctx.Budget.MaxFiles <= 0 || ctx.Budget.MaxBytes <= 0 {
		ctx.Budget = DefaultBudget()
	}
	resultDoc := probe.ResultDocument{SchemaVersion: probe.SchemaVersion, GeneratedBy: ExecutorID, Binding: ctx.Binding, SourceProbeDocumentDigestSHA256: ctx.ProbeDocumentDigest}
	existing := map[string]probe.ProbeResult{}
	if ctx.ExistingResults != nil {
		if !bindingEqual(ctx.Binding, ctx.ExistingResults.Binding) || ctx.ExistingResults.SourceProbeDocumentDigestSHA256 != ctx.ProbeDocumentDigest {
			return BatchResult{}, errors.New(ReasonInputStale)
		}
		resultDoc = *ctx.ExistingResults
		for _, result := range ctx.ExistingResults.Results {
			existing[result.ProbeID] = result
		}
	}
	facts := map[string]architecture.ClaimFactReceipt{}
	for _, receipt := range ctx.Claims.FactReceipts {
		facts[receipt.Fact.ID] = receipt
		facts["evidence:"+receipt.Fact.ID] = receipt
	}
	metrics := Metrics{ProbesConsidered: len(ctx.Probes.Probes)}
	execCtx := &executionContext{repoRoot: abs, observedAt: ctx.ObservedAt, facts: facts, files: map[string][]byte{}, metrics: &metrics, budget: ctx.Budget}
	registry := map[string]Executor{probe.KindSourceReceiptVerification: sourceReceiptExecutor{}}
	var decisions []Decision
	for _, planned := range ctx.Probes.Probes {
		if prior, ok := existing[planned.ID]; ok {
			metrics.ReplayPrevented++
			decisions = append(decisions, Decision{ProbeID: planned.ID, Eligible: true, Replayed: true, ReasonCode: "task.probe.replay", ResultID: prior.ID})
			continue
		}
		executor, ok := registry[planned.ProbeKind]
		if !ok {
			decisions = append(decisions, Decision{ProbeID: planned.ID, ReasonCode: ReasonKindUnsupported})
			continue
		}
		if err := executor.Validate(planned); err != nil {
			decisions = append(decisions, Decision{ProbeID: planned.ID, ReasonCode: err.Error()})
			continue
		}
		if metrics.ProbesExecuted >= ctx.Budget.MaxProbes {
			metrics.BudgetExhausted = true
			decisions = append(decisions, Decision{ProbeID: planned.ID, Eligible: true, ReasonCode: ReasonBudgetExhausted})
			continue
		}
		decision := Decision{ProbeID: planned.ID, Eligible: true}
		result, execErr := executor.Execute(execCtx, planned)
		metrics.ProbesExecuted++
		decision.Executed = true
		if execErr != nil {
			if errors.Is(execErr, errBudgetExhausted) {
				metrics.BudgetExhausted = true
			}
			result = probe.ProbeResult{ProbeID: planned.ID, QuestionID: planned.QuestionID, ResultStatus: probe.ResultFailed, ExecutedBy: ExecutorID, ObservedAt: ctx.ObservedAt, Notes: []string{execErr.Error()}}
		}
		result.ID = probe.StableResultID(result)
		decision.ResultID = result.ID
		resultDoc.Results = append(resultDoc.Results, result)
		decisions = append(decisions, decision)
	}
	normalized, err := probe.NormalizeResultDocument(resultDoc, ctx.Probes)
	if err != nil {
		return BatchResult{}, err
	}
	evidence := ingestDiagnosticEvidence(ctx.EvidenceState, normalized.Results)
	return BatchResult{Results: normalized, EvidenceState: evidence, Decisions: decisions, Metrics: metrics}, nil
}

func (sourceReceiptExecutor) Execute(ctx *executionContext, p probe.EvidenceProbe) (probe.ProbeResult, error) {
	result := probe.ProbeResult{ProbeID: p.ID, QuestionID: p.QuestionID, ResultStatus: probe.ResultCompleted, ExecutedBy: ExecutorID, ObservedAt: ctx.observedAt, ObservationSource: "repository_local_static_read"}
	var notes []string
	receipts := map[string]probe.ArtifactReceipt{}
	for _, step := range p.Steps {
		target := filepath.ToSlash(strings.TrimSpace(step.Target))
		abs, err := inside(ctx.repoRoot, target)
		if err != nil {
			result.ResultStatus = probe.ResultRejected
			result.Notes = []string{ReasonScopeEscape + ": " + target}
			return result, nil
		}
		if sensitivePath(target) {
			result.ResultStatus = probe.ResultRejected
			result.Notes = []string{ReasonSecretRejected + ": " + target}
			return result, nil
		}
		data, ok := ctx.files[target]
		if !ok {
			if ctx.metrics.FilesRead >= ctx.budget.MaxFiles {
				return result, errBudgetExhausted
			}
			data, err = os.ReadFile(abs)
			if os.IsNotExist(err) {
				result.ResultStatus = probe.ResultUnavailable
				result.Notes = []string{"target file is absent: " + target}
				return result, nil
			}
			if err != nil {
				return result, err
			}
			if ctx.metrics.BytesRead+int64(len(data)) > ctx.budget.MaxBytes {
				return result, errBudgetExhausted
			}
			ctx.files[target] = data
			ctx.metrics.FilesRead++
			ctx.metrics.BytesRead += int64(len(data))
		}
		sum := sha256.Sum256(data)
		actual := hex.EncodeToString(sum[:])
		receipts[target] = probe.ArtifactReceipt{Path: target, Kind: "source_file", SHA256: actual, Size: int64(len(data))}
		if step.Kind == probe.StepVerifySourceDigest {
			fact, ok := ctx.facts[strings.TrimSpace(step.SourceRef)]
			if !ok || fact.Provenance.SourceDigestStatus != architecture.SourceDigestResolved {
				result.ResultStatus = probe.ResultInconclusive
				notes = append(notes, "source receipt unavailable: "+step.SourceRef)
				continue
			}
			if fact.Fact.Evidence.SourceFile != target || fact.Provenance.SourceDigest != actual {
				result.ResultStatus = probe.ResultInconclusive
				notes = append(notes, fmt.Sprintf("source receipt mismatch: %s expected %s for %s, observed %s", step.SourceRef, fact.Provenance.SourceDigest, fact.Fact.Evidence.SourceFile, actual))
			} else {
				notes = append(notes, "source receipt verified: "+step.SourceRef)
			}
		}
	}
	for _, receipt := range receipts {
		result.Artifacts = append(result.Artifacts, receipt)
	}
	sort.SliceStable(result.Artifacts, func(i, j int) bool { return result.Artifacts[i].Path < result.Artifacts[j].Path })
	result.Notes = clean(notes)
	return result, nil
}

func ingestDiagnosticEvidence(doc maintenance.EvidenceStateDocument, results []probe.ProbeResult) maintenance.EvidenceStateDocument {
	byID := map[string]maintenance.EvidenceState{}
	for _, item := range doc.Evidence {
		byID[item.ID] = item
	}
	for _, result := range results {
		id := "probe-result." + strings.TrimPrefix(result.ID, "probe-result.")
		status := maintenance.EvidenceStatusWarning
		freshness := maintenance.EvidenceFreshnessCurrent
		if result.ResultStatus == probe.ResultUnavailable || result.ResultStatus == probe.ResultFailed || result.ResultStatus == probe.ResultRejected {
			status, freshness = maintenance.EvidenceStatusUnknown, maintenance.EvidenceFreshnessUnknown
		}
		byID[id] = maintenance.EvidenceState{ID: id, Status: status, Freshness: freshness, ObservedAt: result.ObservedAt, Source: "probe_result:" + result.ID + ";probe:" + result.ProbeID}
	}
	doc.GeneratedBy = ExecutorID
	doc.Evidence = doc.Evidence[:0]
	for _, item := range byID {
		doc.Evidence = append(doc.Evidence, item)
	}
	sort.SliceStable(doc.Evidence, func(i, j int) bool { return doc.Evidence[i].ID < doc.Evidence[j].ID })
	return doc
}

func inside(root, rel string) (string, error) {
	if rel == "" || filepath.IsAbs(rel) {
		return "", errors.New(ReasonScopeEscape)
	}
	abs := filepath.Join(root, filepath.FromSlash(rel))
	back, err := filepath.Rel(root, abs)
	if err != nil || back == ".." || strings.HasPrefix(back, ".."+string(filepath.Separator)) {
		return "", errors.New(ReasonScopeEscape)
	}
	return abs, nil
}

var errBudgetExhausted = errors.New(ReasonBudgetExhausted)

func sensitivePath(rel string) bool {
	rel = filepath.ToSlash(strings.TrimSpace(rel))
	for _, part := range strings.Split(rel, "/") {
		lower := strings.ToLower(part)
		switch {
		case lower == ".git", lower == ".ssh", lower == "secrets", lower == "credentials":
			return true
		case strings.HasPrefix(lower, ".env"), strings.HasPrefix(lower, "id_rsa"), strings.HasPrefix(lower, "id_ed25519"):
			return true
		case strings.Contains(lower, "credential") || strings.Contains(lower, "private_key"):
			return true
		}
	}
	return false
}

func bindingEqual(a, b architecture.ClaimDocumentBinding) bool { return a == b }

func clean(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, item := range in {
		item = strings.TrimSpace(item)
		if item != "" && !seen[item] {
			seen[item] = true
			out = append(out, item)
		}
	}
	sort.Strings(out)
	return out
}
