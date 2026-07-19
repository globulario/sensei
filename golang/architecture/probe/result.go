// SPDX-License-Identifier: AGPL-3.0-only

package probe

import (
	"bytes"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/maintenance"
	"gopkg.in/yaml.v3"
)

type ProbeResult struct {
	ID           string `json:"id" yaml:"id"`
	ProbeID      string `json:"probe_id" yaml:"probe_id"`
	QuestionID   string `json:"question_id" yaml:"question_id"`
	ResultStatus string `json:"result_status" yaml:"result_status"`

	ExecutedBy      string `json:"executed_by,omitempty" yaml:"executed_by,omitempty"`
	ObservedAt      string `json:"observed_at,omitempty" yaml:"observed_at,omitempty"`
	ApprovalReceipt string `json:"approval_receipt,omitempty" yaml:"approval_receipt,omitempty"`

	EvidenceID        string `json:"evidence_id,omitempty" yaml:"evidence_id,omitempty"`
	EvidenceStatus    string `json:"evidence_status,omitempty" yaml:"evidence_status,omitempty"`
	EvidenceFreshness string `json:"evidence_freshness,omitempty" yaml:"evidence_freshness,omitempty"`
	EvidenceRole      string `json:"evidence_role,omitempty" yaml:"evidence_role,omitempty"`

	ObservationSource string            `json:"observation_source,omitempty" yaml:"observation_source,omitempty"`
	Artifacts         []ArtifactReceipt `json:"artifacts,omitempty" yaml:"artifacts,omitempty"`
	Notes             []string          `json:"notes,omitempty" yaml:"notes,omitempty"`
}

type ArtifactReceipt struct {
	Path   string `json:"path" yaml:"path"`
	Kind   string `json:"kind" yaml:"kind"`
	SHA256 string `json:"sha256" yaml:"sha256"`
	Size   int64  `json:"size" yaml:"size"`
}

type ResultDocument struct {
	SchemaVersion                   string                            `json:"schema_version" yaml:"schema_version"`
	GeneratedBy                     string                            `json:"generated_by" yaml:"generated_by"`
	Binding                         architecture.ClaimDocumentBinding `json:"binding" yaml:"binding"`
	SourceProbeDocumentDigestSHA256 string                            `json:"source_probe_document_digest_sha256" yaml:"source_probe_document_digest_sha256"`
	Results                         []ProbeResult                     `json:"results" yaml:"results"`
	Limitations                     []architecture.Limitation         `json:"limitations,omitempty" yaml:"limitations,omitempty"`
}

type ResultDocumentEnvelope struct {
	ArchitectureProbeResults ResultDocument `json:"architecture_probe_results" yaml:"architecture_probe_results"`
}

type RecordOptions struct {
	ProbeID                 string
	ResultStatus            string
	ExecutedBy              string
	ObservedAt              string
	ApprovalReceipt         string
	EvidenceStatus          string
	EvidenceFreshness       string
	ObservationSource       string
	Artifacts               []ArtifactInput
	Notes                   []string
	ReplaceExistingEvidence bool
}

type ArtifactInput struct {
	Kind string
	Path string
}

type RecordContext struct {
	Probes              ProbeDocument
	Existing            *ResultDocument
	Claims              *architecture.ClaimDocument
	Graph               *GraphIndex
	EvidenceState       *maintenance.EvidenceStateDocument
	ProbeDocumentDigest string
}

type RecordResult struct {
	Document      ResultDocument
	EvidenceState *maintenance.EvidenceStateDocument
	Report        RecordingReport
}

type RecordingReport struct {
	SchemaVersion            string                    `json:"schema_version" yaml:"schema_version"`
	GeneratedBy              string                    `json:"generated_by" yaml:"generated_by"`
	ProbeID                  string                    `json:"probe_id" yaml:"probe_id"`
	ResultID                 string                    `json:"result_id" yaml:"result_id"`
	QuestionID               string                    `json:"question_id" yaml:"question_id"`
	ResultStatus             string                    `json:"result_status" yaml:"result_status"`
	SafetyClass              string                    `json:"safety_class" yaml:"safety_class"`
	ApprovalGate             string                    `json:"approval_gate" yaml:"approval_gate"`
	ApprovalReceiptPresent   bool                      `json:"approval_receipt_present" yaml:"approval_receipt_present"`
	ArtifactCount            int                       `json:"artifact_count" yaml:"artifact_count"`
	TargetEvidenceID         string                    `json:"target_evidence_id,omitempty" yaml:"target_evidence_id,omitempty"`
	EvidenceStateDisposition string                    `json:"evidence_state_disposition" yaml:"evidence_state_disposition"`
	NonAuthoritativeWarning  string                    `json:"non_authoritative_warning" yaml:"non_authoritative_warning"`
	Limitations              []architecture.Limitation `json:"limitations,omitempty" yaml:"limitations,omitempty"`
}

type recordingReportEnvelope struct {
	ArchitectureProbeResultRecording RecordingReport `json:"architecture_probe_result_recording" yaml:"architecture_probe_result_recording"`
}

func Record(ctx RecordContext, opts RecordOptions) (RecordResult, error) {
	opts = normalizeRecordOptions(opts)
	if !sha256RE.MatchString(ctx.ProbeDocumentDigest) {
		return RecordResult{}, errors.New("source probe document digest is required")
	}
	probe, ok := probeByID(ctx.Probes, opts.ProbeID)
	if !ok {
		return RecordResult{}, fmt.Errorf("probe %s not found", opts.ProbeID)
	}
	artifacts, err := readArtifactReceipts(opts.Artifacts)
	if err != nil {
		return RecordResult{}, err
	}
	res := ProbeResult{
		ProbeID: probe.ID, QuestionID: probe.QuestionID, ResultStatus: opts.ResultStatus,
		ExecutedBy: opts.ExecutedBy, ObservedAt: opts.ObservedAt, ApprovalReceipt: opts.ApprovalReceipt,
		EvidenceID: probe.TargetEvidenceID, EvidenceStatus: opts.EvidenceStatus, EvidenceFreshness: opts.EvidenceFreshness, EvidenceRole: probe.EvidenceRole,
		ObservationSource: opts.ObservationSource, Artifacts: artifacts, Notes: opts.Notes,
	}
	res.ID = StableResultID(res)
	if err := ValidateResult(res, probe); err != nil {
		return RecordResult{}, err
	}
	doc := ResultDocument{SchemaVersion: SchemaVersion, GeneratedBy: ResultBy, Binding: ctx.Probes.Binding, SourceProbeDocumentDigestSHA256: ctx.ProbeDocumentDigest}
	if ctx.Existing != nil {
		if !BindingEqual(ctx.Existing.Binding, ctx.Probes.Binding) {
			return RecordResult{}, errors.New("existing result binding does not match probe document")
		}
		doc = *ctx.Existing
		doc.SourceProbeDocumentDigestSHA256 = ctx.ProbeDocumentDigest
	}
	doc.Results = append(doc.Results, res)
	normalized, err := NormalizeResultDocument(doc, ctx.Probes)
	if err != nil {
		return RecordResult{}, err
	}
	updated, disposition, limitations, err := maybeUpdateEvidenceState(ctx, probe, res, opts.ReplaceExistingEvidence)
	if err != nil {
		return RecordResult{}, err
	}
	report := RecordingReport{
		SchemaVersion: SchemaVersion, GeneratedBy: ResultBy, ProbeID: probe.ID, ResultID: res.ID, QuestionID: probe.QuestionID,
		ResultStatus: res.ResultStatus, SafetyClass: probe.SafetyClass, ApprovalGate: probe.ApprovalGate,
		ApprovalReceiptPresent: res.ApprovalReceipt != "", ArtifactCount: len(res.Artifacts), TargetEvidenceID: probe.TargetEvidenceID,
		EvidenceStateDisposition: disposition,
		NonAuthoritativeWarning:  "ProbeResult is an offline receipt and does not prove a claim or mutate the graph by existing.",
		Limitations:              limitations,
	}
	return RecordResult{Document: normalized, EvidenceState: updated, Report: normalizeRecordingReport(report)}, nil
}

func StableResultID(r ProbeResult) string {
	r = canonicalizeResult(r)
	var sums []string
	for _, a := range r.Artifacts {
		sums = append(sums, a.SHA256)
	}
	sort.Strings(sums)
	parts := []string{r.ProbeID, r.ObservedAt, r.ExecutedBy, r.ResultStatus, r.EvidenceID, strings.Join(sums, ",")}
	sum := sha1.Sum([]byte(strings.Join(parts, "|")))
	return "probe_result." + hex.EncodeToString(sum[:])[:16]
}

func NormalizeResultDocument(in ResultDocument, probes ProbeDocument) (ResultDocument, error) {
	doc := in
	doc.SchemaVersion = strings.TrimSpace(doc.SchemaVersion)
	if doc.SchemaVersion == "" {
		doc.SchemaVersion = SchemaVersion
	}
	doc.GeneratedBy = strings.TrimSpace(doc.GeneratedBy)
	if doc.GeneratedBy == "" {
		doc.GeneratedBy = ResultBy
	}
	doc.Binding = canonicalBinding(doc.Binding)
	doc.SourceProbeDocumentDigestSHA256 = strings.TrimSpace(doc.SourceProbeDocumentDigestSHA256)
	out := make([]ProbeResult, 0, len(doc.Results))
	for _, r := range doc.Results {
		r = canonicalizeResult(r)
		if r.ID == "" {
			r.ID = StableResultID(r)
		}
		p, ok := probeByID(probes, r.ProbeID)
		if !ok {
			return ResultDocument{}, fmt.Errorf("result references missing probe %s", r.ProbeID)
		}
		if err := ValidateResult(r, p); err != nil {
			return ResultDocument{}, err
		}
		out = append(out, r)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	doc.Results = out
	if !sha256RE.MatchString(doc.SourceProbeDocumentDigestSHA256) {
		return ResultDocument{}, errors.New("source probe document digest must be lowercase SHA-256")
	}
	return doc, nil
}

func ValidateResult(r ProbeResult, p EvidenceProbe) error {
	var errs []string
	if r.ProbeID == "" || r.QuestionID == "" {
		errs = append(errs, "probe_id and question_id are required")
	}
	if !oneOf(r.ResultStatus, ResultCompleted, ResultInconclusive, ResultUnavailable, ResultFailed, ResultRejected) {
		errs = append(errs, "unknown result status")
	}
	if r.QuestionID != p.QuestionID {
		errs = append(errs, "result question does not match probe")
	}
	switch r.ResultStatus {
	case ResultCompleted:
		if r.ExecutedBy == "" || r.ObservedAt == "" {
			errs = append(errs, "completed result requires executor and observed_at")
		}
	case ResultInconclusive:
		if len(r.Notes)+len(r.Artifacts) == 0 {
			errs = append(errs, "inconclusive result requires note or artifact")
		}
	case ResultUnavailable:
		if len(r.Notes) == 0 {
			errs = append(errs, "unavailable result requires reason note")
		}
	}
	if r.ObservedAt != "" {
		if _, err := time.Parse(time.RFC3339, r.ObservedAt); err != nil {
			errs = append(errs, "observed_at must be RFC3339")
		}
	}
	if (r.ResultStatus == ResultCompleted || r.ResultStatus == ResultInconclusive) && RequiresApprovalReceipt(p.ApprovalGate) && r.ApprovalReceipt == "" {
		errs = append(errs, "approval receipt required for probe safety gate")
	}
	if p.TargetEvidenceID != "" && r.ResultStatus != ResultRejected {
		if r.EvidenceStatus == "" || r.EvidenceFreshness == "" {
			errs = append(errs, "bound result requires explicit evidence status and freshness")
		}
		if r.EvidenceStatus != "" && !oneOf(r.EvidenceStatus, maintenance.EvidenceStatusPass, maintenance.EvidenceStatusFail, maintenance.EvidenceStatusWarning, maintenance.EvidenceStatusStale, maintenance.EvidenceStatusUnknown) {
			errs = append(errs, "unknown evidence status")
		}
		if r.EvidenceFreshness != "" && !oneOf(r.EvidenceFreshness, maintenance.EvidenceFreshnessCurrent, maintenance.EvidenceFreshnessStale, maintenance.EvidenceFreshnessUnknown, maintenance.EvidenceFreshnessHistorical) {
			errs = append(errs, "unknown evidence freshness")
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("probe result %s: %s", r.ID, strings.Join(errs, "; "))
	}
	return nil
}

func MarshalResultDocumentYAML(doc ResultDocument, probes ProbeDocument) ([]byte, error) {
	doc, err := NormalizeResultDocument(doc, probes)
	if err != nil {
		return nil, err
	}
	return yaml.Marshal(ResultDocumentEnvelope{ArchitectureProbeResults: doc})
}

func MarshalResultDocumentJSON(doc ResultDocument, probes ProbeDocument) ([]byte, error) {
	doc, err := NormalizeResultDocument(doc, probes)
	if err != nil {
		return nil, err
	}
	return marshalJSON(ResultDocumentEnvelope{ArchitectureProbeResults: doc})
}

func UnmarshalResultDocumentYAML(data []byte, probes ProbeDocument) (ResultDocument, error) {
	var env ResultDocumentEnvelope
	if err := yaml.Unmarshal(data, &env); err != nil {
		return ResultDocument{}, err
	}
	if env.ArchitectureProbeResults.SchemaVersion == "" && len(env.ArchitectureProbeResults.Results) == 0 {
		return ResultDocument{}, errors.New("missing architecture_probe_results document")
	}
	return NormalizeResultDocument(env.ArchitectureProbeResults, probes)
}

func LoadResultDocument(path string, probes ProbeDocument) (ResultDocument, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ResultDocument{}, err
	}
	return UnmarshalResultDocumentYAML(data, probes)
}

func MarshalRecordingReportYAML(report RecordingReport) ([]byte, error) {
	return yaml.Marshal(recordingReportEnvelope{ArchitectureProbeResultRecording: normalizeRecordingReport(report)})
}

func MarshalRecordingReportJSON(report RecordingReport) ([]byte, error) {
	return marshalJSON(recordingReportEnvelope{ArchitectureProbeResultRecording: normalizeRecordingReport(report)})
}

func normalizeRecordOptions(in RecordOptions) RecordOptions {
	in.ProbeID = strings.TrimSpace(in.ProbeID)
	in.ResultStatus = strings.TrimSpace(in.ResultStatus)
	in.ExecutedBy = strings.TrimSpace(in.ExecutedBy)
	in.ObservedAt = strings.TrimSpace(in.ObservedAt)
	in.ApprovalReceipt = strings.TrimSpace(in.ApprovalReceipt)
	in.EvidenceStatus = strings.TrimSpace(in.EvidenceStatus)
	in.EvidenceFreshness = strings.TrimSpace(in.EvidenceFreshness)
	in.ObservationSource = strings.TrimSpace(in.ObservationSource)
	in.Notes = cleanStrings(in.Notes)
	return in
}

func canonicalizeResult(in ProbeResult) ProbeResult {
	r := in
	r.ID = strings.TrimSpace(r.ID)
	r.ProbeID = strings.TrimSpace(r.ProbeID)
	r.QuestionID = strings.TrimSpace(r.QuestionID)
	r.ResultStatus = strings.TrimSpace(r.ResultStatus)
	r.ExecutedBy = strings.TrimSpace(r.ExecutedBy)
	r.ObservedAt = strings.TrimSpace(r.ObservedAt)
	r.ApprovalReceipt = strings.TrimSpace(r.ApprovalReceipt)
	r.EvidenceID = normalizeEvidenceRef(r.EvidenceID)
	r.EvidenceStatus = strings.TrimSpace(r.EvidenceStatus)
	r.EvidenceFreshness = strings.TrimSpace(r.EvidenceFreshness)
	r.EvidenceRole = strings.TrimSpace(r.EvidenceRole)
	r.ObservationSource = strings.TrimSpace(r.ObservationSource)
	r.Notes = cleanStrings(r.Notes)
	for i := range r.Artifacts {
		r.Artifacts[i].Kind = strings.TrimSpace(r.Artifacts[i].Kind)
		r.Artifacts[i].Path = normalizePath(r.Artifacts[i].Path)
		r.Artifacts[i].SHA256 = strings.TrimSpace(r.Artifacts[i].SHA256)
	}
	sort.SliceStable(r.Artifacts, func(i, j int) bool {
		if r.Artifacts[i].SHA256 != r.Artifacts[j].SHA256 {
			return r.Artifacts[i].SHA256 < r.Artifacts[j].SHA256
		}
		return r.Artifacts[i].Path < r.Artifacts[j].Path
	})
	return r
}

func readArtifactReceipts(inputs []ArtifactInput) ([]ArtifactReceipt, error) {
	var out []ArtifactReceipt
	for _, in := range inputs {
		kind := strings.TrimSpace(in.Kind)
		path := strings.TrimSpace(in.Path)
		if kind == "" || path == "" {
			return nil, errors.New("artifact requires kind and path")
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(data)
		out = append(out, ArtifactReceipt{Path: normalizePath(path), Kind: kind, SHA256: hex.EncodeToString(sum[:]), Size: int64(len(data))})
	}
	return out, nil
}

func maybeUpdateEvidenceState(ctx RecordContext, p EvidenceProbe, r ProbeResult, replace bool) (*maintenance.EvidenceStateDocument, string, []architecture.Limitation, error) {
	if r.ResultStatus == ResultRejected {
		return nil, EvidenceStateRejected, nil, nil
	}
	if p.EvidenceRole == RoleDiagnostic {
		return nil, EvidenceStateDiagnosticOnly, nil, nil
	}
	if p.TargetEvidenceID == "" {
		return nil, EvidenceStateUnboundResult, nil, nil
	}
	if ctx.Claims == nil || ctx.Graph == nil {
		return nil, EvidenceStateUnboundResult, []architecture.Limitation{{Source: p.ID, Scope: "evidence_state", Reason: "claims and graph snapshot are required for evidence-state update", Blocking: false}}, nil
	}
	if !ctx.Graph.Has("evidence", bareEvidenceID(p.TargetEvidenceID)) {
		return nil, EvidenceStateUnboundResult, []architecture.Limitation{{Source: p.TargetEvidenceID, Scope: "evidence_state", Reason: "target Evidence is absent from graph snapshot", Blocking: false}}, nil
	}
	if !boundClaimEvidence(*ctx.Claims, p) {
		return nil, EvidenceStateUnboundResult, []architecture.Limitation{{Source: p.TargetEvidenceID, Scope: "evidence_state", Reason: "target claim does not reference Evidence in declared role", Blocking: false}}, nil
	}
	doc := maintenance.EvidenceStateDocument{SchemaVersion: "1", GeneratedBy: ResultBy, Binding: ctx.Probes.Binding}
	if ctx.EvidenceState != nil {
		if !BindingEqual(ctx.EvidenceState.Binding, ctx.Probes.Binding) {
			return nil, "", nil, errors.New("evidence-state binding does not match probe document")
		}
		doc = *ctx.EvidenceState
	}
	id := bareEvidenceID(p.TargetEvidenceID)
	state := maintenance.EvidenceState{ID: id, Status: r.EvidenceStatus, Freshness: r.EvidenceFreshness, ObservedAt: r.ObservedAt, Source: "probe_result:" + r.ID + ";probe:" + p.ID}
	disposition := EvidenceStateCreated
	replaced := false
	for i := range doc.Evidence {
		if doc.Evidence[i].ID == id {
			if !replace {
				return nil, "", nil, errors.New("existing evidence-state record requires --replace-existing-evidence")
			}
			doc.Evidence[i] = state
			replaced = true
			disposition = EvidenceStateReplaced
			break
		}
	}
	if !replaced {
		doc.Evidence = append(doc.Evidence, state)
	}
	data, err := maintenance.MarshalCanonicalEvidenceStateYAML(doc)
	if err != nil {
		return nil, "", nil, err
	}
	parsed, err := maintenance.UnmarshalEvidenceStateDocumentYAML(data)
	if err != nil {
		return nil, "", nil, err
	}
	return &parsed, disposition, nil, nil
}

func boundClaimEvidence(doc architecture.ClaimDocument, p EvidenceProbe) bool {
	target := normalizeEvidenceRef(p.TargetEvidenceID)
	for _, id := range p.ClaimIDs {
		c, ok := claimByID(doc, id)
		if !ok {
			continue
		}
		if p.EvidenceRole == RoleSupporting && contains(c.SupportingEvidence, target) {
			return true
		}
		if p.EvidenceRole == RoleRefuting && contains(c.RefutingEvidence, target) {
			return true
		}
	}
	return false
}

func probeByID(doc ProbeDocument, id string) (EvidenceProbe, bool) {
	for _, p := range doc.Probes {
		if p.ID == id {
			return p, true
		}
	}
	return EvidenceProbe{}, false
}

func normalizeRecordingReport(r RecordingReport) RecordingReport {
	r.SchemaVersion = SchemaVersion
	if r.GeneratedBy == "" {
		r.GeneratedBy = ResultBy
	}
	return r
}

func resultDocumentsEqual(a, b ResultDocument) bool {
	aj, _ := MarshalResultDocumentYAML(a, ProbeDocument{})
	bj, _ := MarshalResultDocumentYAML(b, ProbeDocument{})
	return bytes.Equal(aj, bj)
}
