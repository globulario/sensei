// SPDX-License-Identifier: Apache-2.0

package probe

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
	"regexp"
	"sort"
	"strings"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/graphsnapshot"
	"github.com/globulario/sensei/golang/rdf"
	"gopkg.in/yaml.v3"
)

const (
	SchemaVersion = "1"
	GeneratedBy   = "sensei plan-probes"
	ResultBy      = "sensei record-probe-result"

	StatusProposed    = "proposed"
	StatusUnavailable = "unavailable"
	StatusSuperseded  = "superseded"

	KindSourceReceiptVerification   = "source_receipt_verification"
	KindTestExecution               = "test_execution"
	KindOwnerPathRuntimeObservation = "owner_path_runtime_observation"
	KindArtifactCollection          = "artifact_collection"
	KindEvidenceReconciliation      = "evidence_reconciliation"
	KindControlledExperiment        = "controlled_experiment"
	KindManualObservation           = "manual_observation"

	LaneStatic     = "static"
	LaneTest       = "test"
	LaneRuntime    = "runtime"
	LaneArtifact   = "artifact"
	LaneHybrid     = "hybrid"
	LaneDiagnostic = "diagnostic"

	RoleSupporting = "supporting"
	RoleRefuting   = "refuting"
	RoleDiagnostic = "diagnostic"

	SafetyStaticRead         = "static_read"
	SafetyLocalTest          = "local_test"
	SafetyRuntimeRead        = "runtime_read"
	SafetyIsolatedMutation   = "isolated_mutation"
	SafetyLiveMutation       = "live_mutation"
	SafetyFailureInjection   = "failure_injection"
	SafetyDestructive        = "destructive"
	SafetyExternalSideEffect = "external_side_effect"

	GateNone                      = "none"
	GateReviewRequired            = "review_required"
	GateHumanApprovalRequired     = "human_approval_required"
	GateMultiStepApprovalRequired = "multi_step_approval_required"
	GateManualOnly                = "manual_only"

	StepVerifySourceDigest          = "verify_source_digest"
	StepInspectSource               = "inspect_source"
	StepRunExistingTest             = "run_existing_test"
	StepInvokeOwnerReadPath         = "invoke_owner_read_path"
	StepCollectArtifact             = "collect_artifact"
	StepCompareEvidenceReceipts     = "compare_evidence_receipts"
	StepPerformControlledExperiment = "perform_controlled_experiment"
	StepRecordManualObservation     = "record_manual_observation"

	ResultCompleted    = "completed"
	ResultInconclusive = "inconclusive"
	ResultUnavailable  = "unavailable"
	ResultFailed       = "failed"
	ResultRejected     = "rejected"

	EvidenceStateCreated        = "created"
	EvidenceStateReplaced       = "replaced"
	EvidenceStateUnchanged      = "unchanged"
	EvidenceStateDiagnosticOnly = "diagnostic_only"
	EvidenceStateUnboundResult  = "unbound_result"
	EvidenceStateRejected       = "rejected"
)

var (
	blockerIDRE = regexp.MustCompile(`^blocker\.(structural|authority|contract|behavioral|evidence|contradiction|direction|agent)\.[a-f0-9]{12}$`)
	sha256RE    = regexp.MustCompile(`^[a-f0-9]{64}$`)
)

type EvidenceProbe struct {
	ID          string `json:"id" yaml:"id"`
	Label       string `json:"label,omitempty" yaml:"label,omitempty"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
	Status      string `json:"status" yaml:"status"`

	QuestionID        string   `json:"question_id" yaml:"question_id"`
	ClosureBlockerIDs []string `json:"closure_blocker_ids,omitempty" yaml:"closure_blocker_ids,omitempty"`
	ClaimIDs          []string `json:"claim_ids,omitempty" yaml:"claim_ids,omitempty"`
	NodeRefs          []string `json:"node_refs,omitempty" yaml:"node_refs,omitempty"`

	TemplateID      string `json:"template_id" yaml:"template_id"`
	TemplateVersion string `json:"template_version" yaml:"template_version"`

	ProbeKind    string `json:"probe_kind" yaml:"probe_kind"`
	EvidenceLane string `json:"evidence_lane" yaml:"evidence_lane"`
	EvidenceRole string `json:"evidence_role" yaml:"evidence_role"`

	TargetEvidenceID string `json:"target_evidence_id,omitempty" yaml:"target_evidence_id,omitempty"`

	RuntimeEvidenceIDs []string `json:"runtime_evidence_ids,omitempty" yaml:"runtime_evidence_ids,omitempty"`
	ProofObligationIDs []string `json:"proof_obligation_ids,omitempty" yaml:"proof_obligation_ids,omitempty"`
	ProofSlotIDs       []string `json:"proof_slot_ids,omitempty" yaml:"proof_slot_ids,omitempty"`
	RepairPlanIDs      []string `json:"repair_plan_ids,omitempty" yaml:"repair_plan_ids,omitempty"`
	TestIDs            []string `json:"test_ids,omitempty" yaml:"test_ids,omitempty"`

	OwnerService                 string   `json:"owner_service,omitempty" yaml:"owner_service,omitempty"`
	ObservationPaths             []string `json:"observation_paths,omitempty" yaml:"observation_paths,omitempty"`
	FreshnessWindow              string   `json:"freshness_window,omitempty" yaml:"freshness_window,omitempty"`
	TrustLevel                   string   `json:"trust_level,omitempty" yaml:"trust_level,omitempty"`
	MustComeFromOwnerPath        bool     `json:"must_come_from_owner_path,omitempty" yaml:"must_come_from_owner_path,omitempty"`
	CannotPromoteToPassWhenStale bool     `json:"cannot_promote_to_pass_when_stale,omitempty" yaml:"cannot_promote_to_pass_when_stale,omitempty"`

	SafetyClass               string `json:"safety_class" yaml:"safety_class"`
	ApprovalGate              string `json:"approval_gate" yaml:"approval_gate"`
	AutomaticExecutionAllowed bool   `json:"automatic_execution_allowed" yaml:"automatic_execution_allowed"`

	Preconditions         []string    `json:"preconditions,omitempty" yaml:"preconditions,omitempty"`
	Steps                 []ProbeStep `json:"steps,omitempty" yaml:"steps,omitempty"`
	ExpectedArtifactKinds []string    `json:"expected_artifact_kinds,omitempty" yaml:"expected_artifact_kinds,omitempty"`
	Limitations           []string    `json:"limitations,omitempty" yaml:"limitations,omitempty"`
}

type ProbeStep struct {
	Kind        string `json:"kind" yaml:"kind"`
	Target      string `json:"target,omitempty" yaml:"target,omitempty"`
	Description string `json:"description" yaml:"description"`
	SourceRef   string `json:"source_ref,omitempty" yaml:"source_ref,omitempty"`
	Command     string `json:"command,omitempty" yaml:"command,omitempty"`
}

type ProbeDocument struct {
	SchemaVersion                       string                            `json:"schema_version" yaml:"schema_version"`
	GeneratedBy                         string                            `json:"generated_by" yaml:"generated_by"`
	Binding                             architecture.ClaimDocumentBinding `json:"binding" yaml:"binding"`
	SourceClosureAssessmentDigestSHA256 string                            `json:"source_closure_assessment_digest_sha256" yaml:"source_closure_assessment_digest_sha256"`
	SourceDialogueDigestSHA256          string                            `json:"source_dialogue_digest_sha256" yaml:"source_dialogue_digest_sha256"`
	SourceClaimDocumentDigestSHA256     string                            `json:"source_claim_document_digest_sha256" yaml:"source_claim_document_digest_sha256"`
	Probes                              []EvidenceProbe                   `json:"probes" yaml:"probes"`
	Limitations                         []architecture.Limitation         `json:"limitations,omitempty" yaml:"limitations,omitempty"`
}

type ProbeDocumentEnvelope struct {
	ArchitectureEvidenceProbes ProbeDocument `json:"architecture_evidence_probes" yaml:"architecture_evidence_probes"`
}

type ValidationContext struct {
	Dialogue architecture.DialogueDocument
	Claims   architecture.ClaimDocument
	Graph    GraphIndex
}

func StableProbeID(p EvidenceProbe, repo string) string {
	p = canonicalizeProbe(p)
	parts := []string{
		strings.TrimSpace(repo),
		p.QuestionID,
		p.TemplateID,
		p.TemplateVersion,
		p.ProbeKind,
		p.EvidenceRole,
		p.TargetEvidenceID,
		strings.Join(p.ClaimIDs, ","),
		strings.Join(p.NodeRefs, ","),
		strings.Join(p.ProofSlotIDs, ","),
	}
	for _, st := range p.Steps {
		parts = append(parts, st.Kind+"="+st.Target)
	}
	sum := sha1.Sum([]byte(strings.Join(parts, "|")))
	return "probe." + hex.EncodeToString(sum[:])[:16]
}

func NormalizeProbeDocument(in ProbeDocument, ctx *ValidationContext) (ProbeDocument, error) {
	doc := canonicalizeProbeDocument(in)
	out := make([]EvidenceProbe, 0, len(doc.Probes))
	for _, p := range doc.Probes {
		p = canonicalizeProbe(p)
		if p.ID == "" {
			p.ID = StableProbeID(p, doc.Binding.RepositoryDomain)
		}
		if err := ValidateProbe(p, doc, ctx); err != nil {
			return ProbeDocument{}, err
		}
		out = append(out, p)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	seen := map[string]EvidenceProbe{}
	dedup := out[:0]
	for _, p := range out {
		if prior, ok := seen[p.ID]; ok {
			if !probesEqual(prior, p) {
				return ProbeDocument{}, fmt.Errorf("evidence probe id collision for %s", p.ID)
			}
			continue
		}
		seen[p.ID] = p
		dedup = append(dedup, p)
	}
	doc.Probes = dedup
	if err := ValidateProbeDocument(doc, ctx); err != nil {
		return ProbeDocument{}, err
	}
	return doc, nil
}

func ValidateProbeDocument(doc ProbeDocument, ctx *ValidationContext) error {
	var errs []string
	if doc.Binding.RevisionStatus == "" {
		errs = append(errs, "binding revision_status is required")
	}
	if doc.Binding.GraphDigestStatus == "" {
		errs = append(errs, "binding graph_digest_status is required")
	}
	for _, pair := range []struct{ label, value string }{
		{"source closure assessment digest", doc.SourceClosureAssessmentDigestSHA256},
		{"source dialogue digest", doc.SourceDialogueDigestSHA256},
		{"source claim document digest", doc.SourceClaimDocumentDigestSHA256},
	} {
		if !sha256RE.MatchString(pair.value) {
			errs = append(errs, pair.label+" must be lowercase SHA-256")
		}
	}
	ids := map[string]bool{}
	for _, p := range doc.Probes {
		if ids[p.ID] {
			errs = append(errs, "duplicate probe id "+p.ID)
		}
		ids[p.ID] = true
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func ValidateProbe(p EvidenceProbe, doc ProbeDocument, ctx *ValidationContext) error {
	var errs []string
	if p.ID == "" {
		errs = append(errs, "id is required")
	}
	if p.QuestionID == "" {
		errs = append(errs, "question_id is required")
	}
	if !oneOf(p.Status, StatusProposed, StatusUnavailable, StatusSuperseded) {
		errs = append(errs, "unknown probe status")
	}
	if p.TemplateID == "" || p.TemplateVersion == "" {
		errs = append(errs, "template ID and version are required")
	}
	if !oneOf(p.ProbeKind, KindSourceReceiptVerification, KindTestExecution, KindOwnerPathRuntimeObservation, KindArtifactCollection, KindEvidenceReconciliation, KindControlledExperiment, KindManualObservation) {
		errs = append(errs, "unknown probe kind")
	}
	if !oneOf(p.EvidenceLane, LaneStatic, LaneTest, LaneRuntime, LaneArtifact, LaneHybrid, LaneDiagnostic) {
		errs = append(errs, "unknown evidence lane")
	}
	if !oneOf(p.EvidenceRole, RoleSupporting, RoleRefuting, RoleDiagnostic) {
		errs = append(errs, "unknown evidence role")
	}
	if !oneOf(p.SafetyClass, SafetyStaticRead, SafetyLocalTest, SafetyRuntimeRead, SafetyIsolatedMutation, SafetyLiveMutation, SafetyFailureInjection, SafetyDestructive, SafetyExternalSideEffect) {
		errs = append(errs, "unknown safety class")
	}
	if !oneOf(p.ApprovalGate, GateNone, GateReviewRequired, GateHumanApprovalRequired, GateMultiStepApprovalRequired, GateManualOnly) {
		errs = append(errs, "unknown approval gate")
	}
	if weakerApproval(p.SafetyClass, p.ApprovalGate) {
		errs = append(errs, "approval gate is weaker than safety policy")
	}
	if p.AutomaticExecutionAllowed && !automaticAllowed(p.SafetyClass) {
		errs = append(errs, "automatic execution forbidden for safety class")
	}
	if p.Status == StatusUnavailable {
		if len(p.Limitations) == 0 {
			errs = append(errs, "unavailable probe requires limitation")
		}
		if len(p.Steps) > 0 || p.AutomaticExecutionAllowed {
			errs = append(errs, "unavailable probe must not claim executable steps")
		}
	}
	if len(p.ClaimIDs)+len(p.NodeRefs)+len(p.ClosureBlockerIDs)+len(p.RuntimeEvidenceIDs)+len(p.ProofObligationIDs)+len(p.ProofSlotIDs)+len(p.RepairPlanIDs)+len(p.TestIDs) == 0 && p.TargetEvidenceID == "" {
		errs = append(errs, "probe requires grounding")
	}
	for _, id := range p.ClosureBlockerIDs {
		if !blockerIDRE.MatchString(id) {
			errs = append(errs, "malformed closure blocker id")
			break
		}
	}
	for _, ref := range p.NodeRefs {
		if _, _, ok := architecture.ParseClassQualifiedReference(ref); !ok {
			errs = append(errs, "node_refs must be class-qualified")
			break
		}
	}
	if p.EvidenceRole == RoleSupporting || p.EvidenceRole == RoleRefuting {
		if p.TargetEvidenceID == "" {
			errs = append(errs, "supporting/refuting probe requires target_evidence_id")
		} else if !evidenceTargetReferenced(p, ctx) {
			errs = append(errs, "target evidence is not referenced by target claim in declared role")
		}
	}
	for _, st := range p.Steps {
		if !oneOf(st.Kind, StepVerifySourceDigest, StepInspectSource, StepRunExistingTest, StepInvokeOwnerReadPath, StepCollectArtifact, StepCompareEvidenceReceipts, StepPerformControlledExperiment, StepRecordManualObservation) {
			errs = append(errs, "unknown probe step kind")
			break
		}
		if st.Target != "" && pathEscapes(st.Target) {
			errs = append(errs, "probe step target escapes repository")
			break
		}
		if st.Command != "" && (st.SourceRef == "" || ctx == nil || !ctx.Graph.CommandMatches(st.SourceRef, st.Command)) {
			errs = append(errs, "probe command must be copied exactly from a sourced Evidence node")
			break
		}
	}
	if p.ProbeKind == KindControlledExperiment && ctx != nil {
		if q, ok := questionByID(ctx.Dialogue, p.QuestionID); ok && len(q.CompetingHypotheses) < 2 {
			errs = append(errs, "controlled experiment requires at least two hypotheses")
		}
	}
	if ctx != nil {
		q, ok := questionByID(ctx.Dialogue, p.QuestionID)
		if !ok {
			errs = append(errs, "question does not exist")
		} else if !QuestionEligible(ctx.Dialogue, q) {
			errs = append(errs, "question is not evidence-eligible")
		}
		for _, id := range p.ClaimIDs {
			if _, ok := claimByID(ctx.Claims, id); !ok {
				errs = append(errs, "claim does not exist: "+id)
				break
			}
		}
		for _, id := range p.RuntimeEvidenceIDs {
			if !ctx.Graph.Has("runtime_evidence", id) {
				errs = append(errs, "runtime evidence profile missing: "+id)
			}
		}
		for _, id := range p.ProofObligationIDs {
			if !ctx.Graph.Has("proof_obligation", id) {
				errs = append(errs, "proof obligation missing: "+id)
			}
		}
		for _, id := range p.ProofSlotIDs {
			if !ctx.Graph.Has("proof_slot", id) {
				errs = append(errs, "proof slot missing: "+id)
			}
		}
		for _, id := range p.RepairPlanIDs {
			if !ctx.Graph.Has("repair_plan", id) {
				errs = append(errs, "repair plan missing: "+id)
			}
		}
		for _, id := range p.TestIDs {
			if !ctx.Graph.Has("test", id) {
				errs = append(errs, "test missing: "+id)
			}
		}
		if p.TargetEvidenceID != "" && !ctx.Graph.Has("evidence", bareEvidenceID(p.TargetEvidenceID)) {
			errs = append(errs, "target evidence missing: "+p.TargetEvidenceID)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("probe %s: %s", p.ID, strings.Join(errs, "; "))
	}
	return nil
}

func MarshalCanonicalDocumentYAML(doc ProbeDocument, ctx *ValidationContext) ([]byte, error) {
	doc, err := NormalizeProbeDocument(doc, ctx)
	if err != nil {
		return nil, err
	}
	return yaml.Marshal(ProbeDocumentEnvelope{ArchitectureEvidenceProbes: doc})
}

func MarshalCanonicalDocumentJSON(doc ProbeDocument, ctx *ValidationContext) ([]byte, error) {
	doc, err := NormalizeProbeDocument(doc, ctx)
	if err != nil {
		return nil, err
	}
	return marshalJSON(ProbeDocumentEnvelope{ArchitectureEvidenceProbes: doc})
}

func UnmarshalDocumentYAML(data []byte, ctx *ValidationContext) (ProbeDocument, error) {
	var env ProbeDocumentEnvelope
	if err := yaml.Unmarshal(data, &env); err != nil {
		return ProbeDocument{}, err
	}
	if env.ArchitectureEvidenceProbes.SchemaVersion == "" && len(env.ArchitectureEvidenceProbes.Probes) == 0 {
		return ProbeDocument{}, errors.New("missing architecture_evidence_probes document")
	}
	return NormalizeProbeDocument(env.ArchitectureEvidenceProbes, ctx)
}

func LoadDocument(path string, ctx *ValidationContext) (ProbeDocument, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ProbeDocument{}, err
	}
	return UnmarshalDocumentYAML(data, ctx)
}

func canonicalizeProbeDocument(in ProbeDocument) ProbeDocument {
	doc := in
	doc.SchemaVersion = strings.TrimSpace(doc.SchemaVersion)
	if doc.SchemaVersion == "" {
		doc.SchemaVersion = SchemaVersion
	}
	doc.GeneratedBy = strings.TrimSpace(doc.GeneratedBy)
	if doc.GeneratedBy == "" {
		doc.GeneratedBy = GeneratedBy
	}
	doc.Binding = canonicalBinding(doc.Binding)
	doc.SourceClosureAssessmentDigestSHA256 = strings.TrimSpace(doc.SourceClosureAssessmentDigestSHA256)
	doc.SourceDialogueDigestSHA256 = strings.TrimSpace(doc.SourceDialogueDigestSHA256)
	doc.SourceClaimDocumentDigestSHA256 = strings.TrimSpace(doc.SourceClaimDocumentDigestSHA256)
	return doc
}

func canonicalizeProbe(in EvidenceProbe) EvidenceProbe {
	p := in
	p.ID = strings.TrimSpace(p.ID)
	p.Label = strings.TrimSpace(p.Label)
	p.Description = strings.TrimSpace(p.Description)
	p.Status = strings.TrimSpace(p.Status)
	if p.Status == "" {
		p.Status = StatusProposed
	}
	p.QuestionID = strings.TrimSpace(p.QuestionID)
	p.ClosureBlockerIDs = cleanStrings(p.ClosureBlockerIDs)
	p.ClaimIDs = cleanStrings(p.ClaimIDs)
	p.NodeRefs = normalizeRefs(p.NodeRefs)
	p.TemplateID = strings.TrimSpace(p.TemplateID)
	p.TemplateVersion = strings.TrimSpace(p.TemplateVersion)
	p.ProbeKind = strings.TrimSpace(p.ProbeKind)
	p.EvidenceLane = strings.TrimSpace(p.EvidenceLane)
	p.EvidenceRole = strings.TrimSpace(p.EvidenceRole)
	p.TargetEvidenceID = normalizeEvidenceRef(p.TargetEvidenceID)
	p.RuntimeEvidenceIDs = cleanStrings(p.RuntimeEvidenceIDs)
	p.ProofObligationIDs = cleanStrings(p.ProofObligationIDs)
	p.ProofSlotIDs = cleanStrings(p.ProofSlotIDs)
	p.RepairPlanIDs = cleanStrings(p.RepairPlanIDs)
	p.TestIDs = cleanStrings(p.TestIDs)
	p.OwnerService = strings.TrimSpace(p.OwnerService)
	p.ObservationPaths = cleanStrings(p.ObservationPaths)
	p.FreshnessWindow = strings.TrimSpace(p.FreshnessWindow)
	p.TrustLevel = strings.TrimSpace(p.TrustLevel)
	p.SafetyClass = strings.TrimSpace(p.SafetyClass)
	p.ApprovalGate = strings.TrimSpace(p.ApprovalGate)
	p.Preconditions = cleanStrings(p.Preconditions)
	p.ExpectedArtifactKinds = cleanStrings(p.ExpectedArtifactKinds)
	p.Limitations = cleanStrings(p.Limitations)
	for i := range p.Steps {
		p.Steps[i].Kind = strings.TrimSpace(p.Steps[i].Kind)
		p.Steps[i].Target = normalizePath(p.Steps[i].Target)
		p.Steps[i].Description = strings.TrimSpace(p.Steps[i].Description)
		p.Steps[i].SourceRef = normalizeEvidenceRef(p.Steps[i].SourceRef)
		p.Steps[i].Command = strings.TrimSpace(p.Steps[i].Command)
	}
	return p
}

func canonicalBinding(b architecture.ClaimDocumentBinding) architecture.ClaimDocumentBinding {
	b.RepositoryDomain = strings.TrimSpace(b.RepositoryDomain)
	b.Revision = strings.TrimSpace(b.Revision)
	b.RevisionStatus = strings.TrimSpace(b.RevisionStatus)
	b.GraphDigestSHA256 = strings.TrimSpace(b.GraphDigestSHA256)
	b.GraphDigestStatus = strings.TrimSpace(b.GraphDigestStatus)
	return b
}

func QuestionEligible(doc architecture.DialogueDocument, q architecture.OpenQuestion) bool {
	switch q.Status {
	case architecture.QuestionStatusAwaitingEvidence:
		return true
	case architecture.QuestionStatusAnswered:
		for _, a := range doc.Answers {
			if contains(a.AnswersQuestions, q.ID) && a.GovernanceStatus == architecture.AnswerGovernanceAwaitingEvidence {
				return true
			}
		}
		return contains(q.AcceptedAnswerTypes, architecture.AnswerTypeEvidencePointer) && len(q.MissingEvidence) > 0
	default:
		return false
	}
}

func ApprovalRank(gate string) int {
	switch gate {
	case GateNone:
		return 0
	case GateReviewRequired:
		return 1
	case GateHumanApprovalRequired:
		return 2
	case GateMultiStepApprovalRequired:
		return 3
	case GateManualOnly:
		return 4
	default:
		return -1
	}
}

func MinGateForSafety(safety string) string {
	switch safety {
	case SafetyStaticRead:
		return GateNone
	case SafetyLocalTest, SafetyRuntimeRead:
		return GateReviewRequired
	case SafetyIsolatedMutation:
		return GateHumanApprovalRequired
	case SafetyLiveMutation, SafetyFailureInjection:
		return GateMultiStepApprovalRequired
	case SafetyDestructive, SafetyExternalSideEffect:
		return GateManualOnly
	default:
		return ""
	}
}

func weakerApproval(safety, gate string) bool {
	min := MinGateForSafety(safety)
	return min == "" || ApprovalRank(gate) < ApprovalRank(min)
}

func WeakerApprovalForTest(safety, gate string) bool { return weakerApproval(safety, gate) }

func automaticAllowed(safety string) bool {
	return safety == SafetyStaticRead || safety == SafetyLocalTest
}

func AutomaticAllowedForTest(safety string) bool { return automaticAllowed(safety) }

func RequiresApprovalReceipt(gate string) bool { return gate != "" && gate != GateNone }

func SafetyRank(safety string) int {
	order := []string{SafetyStaticRead, SafetyLocalTest, SafetyRuntimeRead, SafetyIsolatedMutation, SafetyLiveMutation, SafetyFailureInjection, SafetyDestructive, SafetyExternalSideEffect}
	for i, item := range order {
		if safety == item {
			return i
		}
	}
	return len(order)
}

func Digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func cleanStrings(in []string) []string {
	seen := map[string]bool{}
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s != "" {
			seen[s] = true
		}
	}
	out := make([]string, 0, len(seen))
	for s := range seen {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func normalizeRefs(in []string) []string {
	var out []string
	for _, ref := range in {
		class, id, ok := architecture.ParseClassQualifiedReference(ref)
		if ok {
			out = append(out, class+":"+id)
		} else if strings.TrimSpace(ref) != "" {
			out = append(out, strings.TrimSpace(ref))
		}
	}
	return cleanStrings(out)
}

func normalizeEvidenceRef(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if strings.HasPrefix(s, "evidence:") {
		return s
	}
	return "evidence:" + s
}

func bareEvidenceID(s string) string { return strings.TrimPrefix(strings.TrimSpace(s), "evidence:") }

func normalizePath(s string) string {
	s = filepath.ToSlash(strings.TrimSpace(s))
	if s == "" {
		return ""
	}
	return filepath.ToSlash(filepath.Clean(filepath.FromSlash(s)))
}

func pathEscapes(s string) bool {
	s = normalizePath(s)
	return filepath.IsAbs(s) || s == ".." || strings.HasPrefix(s, "../") || strings.Contains(s, "/../")
}

func probesEqual(a, b EvidenceProbe) bool {
	aj, _ := json.Marshal(a)
	bj, _ := json.Marshal(b)
	return bytes.Equal(aj, bj)
}

func oneOf(v string, allowed ...string) bool {
	for _, item := range allowed {
		if v == item {
			return true
		}
	}
	return false
}

func contains(in []string, want string) bool {
	for _, item := range in {
		if item == want {
			return true
		}
	}
	return false
}

func questionByID(doc architecture.DialogueDocument, id string) (architecture.OpenQuestion, bool) {
	for _, q := range doc.OpenQuestions {
		if q.ID == id {
			return q, true
		}
	}
	return architecture.OpenQuestion{}, false
}

func claimByID(doc architecture.ClaimDocument, id string) (architecture.Claim, bool) {
	for _, c := range doc.Claims {
		if c.ID == id {
			return c, true
		}
	}
	return architecture.Claim{}, false
}

func evidenceTargetReferenced(p EvidenceProbe, ctx *ValidationContext) bool {
	if ctx == nil {
		return true
	}
	target := normalizeEvidenceRef(p.TargetEvidenceID)
	for _, id := range p.ClaimIDs {
		c, ok := claimByID(ctx.Claims, id)
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

func marshalJSON(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func BindingEqual(a, b architecture.ClaimDocumentBinding) bool {
	a, b = canonicalBinding(a), canonicalBinding(b)
	return a.RepositoryDomain == b.RepositoryDomain &&
		a.Revision == b.Revision &&
		a.RevisionStatus == b.RevisionStatus &&
		a.GraphDigestSHA256 == b.GraphDigestSHA256 &&
		a.GraphDigestStatus == b.GraphDigestStatus
}

type Node struct {
	IRI     string
	ID      string
	Classes []string

	Label      string
	Comment    string
	Status     string
	SourcePath string
	AuthoredIn []string
	Command    string

	ObservedFromService          string
	ObservedViaPaths             []string
	FreshnessWindow              string
	TrustLevel                   string
	ExpiresAfter                 string
	MustComeFromOwnerPath        bool
	CannotPromoteToPassWhenStale bool

	EvidenceForInvariants       []string
	EvidenceForRepairPlans      []string
	EvidenceForAuthorityDomains []string
	RequiredTests               []string
	RequiresRuntimeEvidence     []string
	Preconditions               []string
	VerificationSteps           []string
	ApprovalGate                string
	BlastRadius                 string
	AppliesToAuthorityDomains   []string
	ProofSlots                  []string
	SlotKind                    string
	Required                    bool
	EvidenceLane                string
	DerivedFromAuthoritySurface string
	AppliesToAuthoritySurfaces  []string
	SupportsClaims              []string
	RefutesClaims               []string
}

type GraphIndex struct {
	Nodes map[string]Node
	ByKey map[string]string
}

func LoadGraphIndex(path string) (GraphIndex, error) {
	triples, err := graphsnapshot.Load(path)
	if err != nil {
		return GraphIndex{}, err
	}
	return BuildGraphIndex(triples), nil
}

func BuildGraphIndex(triples []graphsnapshot.Triple) GraphIndex {
	idx := GraphIndex{Nodes: map[string]Node{}, ByKey: map[string]string{}}
	classes := map[string]map[string]bool{}
	for _, t := range triples {
		if t.Predicate == rdf.PropType && t.ObjectIsIRI {
			class := probeIndexedClass(t.Object)
			if class == "" {
				continue
			}
			if classes[t.Subject] == nil {
				classes[t.Subject] = map[string]bool{}
			}
			classes[t.Subject][class] = true
		}
	}
	for iri, set := range classes {
		n := Node{IRI: iri, Classes: sortedClasses(set)}
		n.ID = nodeID(iri, n.Classes)
		idx.Nodes[iri] = n
		for _, class := range n.Classes {
			idx.ByKey[class+":"+n.ID] = iri
		}
	}
	for _, t := range triples {
		n, ok := idx.Nodes[t.Subject]
		if !ok {
			continue
		}
		switch t.Predicate {
		case rdf.PropLabel:
			n.Label = t.Object
		case rdf.PropComment:
			n.Comment = t.Object
		case rdf.PropStatus:
			n.Status = t.Object
		case rdf.PropSourcePath:
			n.SourcePath = t.Object
		case rdf.PropAuthoredIn:
			n.AuthoredIn = append(n.AuthoredIn, t.Object)
		case rdf.PropCommand:
			n.Command = t.Object
		case rdf.PropObservedFromService:
			n.ObservedFromService = t.Object
		case rdf.PropObservedViaPath:
			n.ObservedViaPaths = append(n.ObservedViaPaths, t.Object)
		case rdf.PropHasFreshnessWindow:
			n.FreshnessWindow = t.Object
		case rdf.PropHasTrustLevel:
			n.TrustLevel = t.Object
		case rdf.PropExpiresAfter:
			n.ExpiresAfter = t.Object
		case rdf.PropMustComeFromOwnerPath:
			n.MustComeFromOwnerPath = t.Object == "true"
		case rdf.PropCannotPromoteToPassWhenStale:
			n.CannotPromoteToPassWhenStale = t.Object == "true"
		case rdf.PropRequiresPrecondition:
			n.Preconditions = append(n.Preconditions, t.Object)
		case rdf.PropRequiresVerification:
			n.VerificationSteps = append(n.VerificationSteps, t.Object)
		case rdf.PropRequiresApprovalGate:
			n.ApprovalGate = t.Object
		case rdf.PropHasBlastRadius:
			n.BlastRadius = t.Object
		case rdf.PropRequiresRuntimeEvidence:
			n.RequiresRuntimeEvidence = append(n.RequiresRuntimeEvidence, t.Object)
		case rdf.PropRequiresProofSlot:
			n.ProofSlots = append(n.ProofSlots, objectID(t, rdf.ClassProofSlot))
		case rdf.PropSlotKind:
			n.SlotKind = t.Object
		case rdf.PropRequired:
			n.Required = t.Object == "true"
		case rdf.PropHasEvidenceLane:
			n.EvidenceLane = t.Object
		case rdf.PropDerivedFromAuthoritySurface:
			n.DerivedFromAuthoritySurface = objectID(t, rdf.ClassAuthoritySurface)
		case rdf.PropAppliesToAuthoritySurface:
			n.AppliesToAuthoritySurfaces = append(n.AppliesToAuthoritySurfaces, objectID(t, rdf.ClassAuthoritySurface))
		case rdf.PropEvidenceForInvariant:
			n.EvidenceForInvariants = append(n.EvidenceForInvariants, objectID(t, rdf.ClassInvariant))
		case rdf.PropEvidenceForRepairPlan:
			n.EvidenceForRepairPlans = append(n.EvidenceForRepairPlans, objectID(t, rdf.ClassRepairPlan))
		case rdf.PropEvidenceForAuthorityDomain:
			n.EvidenceForAuthorityDomains = append(n.EvidenceForAuthorityDomains, objectID(t, rdf.ClassAuthorityDomain))
		case rdf.PropAppliesToAuthorityDomain:
			n.AppliesToAuthorityDomains = append(n.AppliesToAuthorityDomains, objectID(t, rdf.ClassAuthorityDomain))
		case rdf.PropRequiresTest:
			n.RequiredTests = append(n.RequiredTests, objectID(t, rdf.ClassTest))
		case rdf.PropSupportedByEvidence:
			evIRI := t.Object
			if ev, ok := idx.Nodes[evIRI]; ok {
				ev.SupportsClaims = append(ev.SupportsClaims, n.ID)
				idx.Nodes[evIRI] = ev
			}
		case rdf.PropRefutedByEvidence:
			evIRI := t.Object
			if ev, ok := idx.Nodes[evIRI]; ok {
				ev.RefutesClaims = append(ev.RefutesClaims, n.ID)
				idx.Nodes[evIRI] = ev
			}
		}
		n.AuthoredIn = cleanStrings(n.AuthoredIn)
		n.ObservedViaPaths = cleanStrings(n.ObservedViaPaths)
		n.Preconditions = cleanStrings(n.Preconditions)
		n.VerificationSteps = cleanStrings(n.VerificationSteps)
		n.RequiresRuntimeEvidence = cleanStrings(n.RequiresRuntimeEvidence)
		n.ProofSlots = cleanStrings(n.ProofSlots)
		n.RequiredTests = cleanStrings(n.RequiredTests)
		idx.Nodes[t.Subject] = n
	}
	return idx
}

func (g GraphIndex) Has(class, id string) bool {
	_, ok := g.ByKey[class+":"+strings.TrimPrefix(id, class+":")]
	return ok
}

func (g GraphIndex) Node(class, id string) (Node, bool) {
	iri, ok := g.ByKey[class+":"+strings.TrimPrefix(id, class+":")]
	if !ok {
		return Node{}, false
	}
	n, ok := g.Nodes[iri]
	return n, ok
}

func (g GraphIndex) Class(class string) []Node {
	var out []Node
	for _, n := range g.Nodes {
		if contains(n.Classes, class) {
			out = append(out, n)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (g GraphIndex) CommandMatches(ref, cmd string) bool {
	n, ok := g.Node("evidence", bareEvidenceID(ref))
	return ok && n.Command == cmd && cmd != ""
}

func probeIndexedClass(iri string) string {
	switch iri {
	case rdf.ClassRuntimeEvidence:
		return "runtime_evidence"
	case rdf.ClassProofObligation:
		return "proof_obligation"
	case rdf.ClassProofSlot:
		return "proof_slot"
	case rdf.ClassRepairPlan:
		return "repair_plan"
	case rdf.ClassTest:
		return "test"
	case rdf.ClassEvidence:
		return "evidence"
	case rdf.ClassAuthorityDomain:
		return "authority_domain"
	case rdf.ClassAuthoritySurface:
		return "authority_surface"
	case rdf.ClassSourceFile:
		return "source_file"
	case rdf.ClassCodeSymbol:
		return "code_symbol"
	case rdf.ClassArchitectureClaim:
		return "architecture_claim"
	case rdf.ClassOpenQuestion:
		return "open_question"
	case rdf.ClassEvidenceProbe:
		return "evidence_probe"
	default:
		return ""
	}
}

func sortedClasses(set map[string]bool) []string {
	order := []string{"runtime_evidence", "proof_obligation", "proof_slot", "repair_plan", "test", "evidence", "authority_domain", "authority_surface", "source_file", "code_symbol", "architecture_claim", "open_question", "evidence_probe"}
	var out []string
	for _, c := range order {
		if set[c] {
			out = append(out, c)
		}
	}
	return out
}

func nodeID(iri string, classes []string) string {
	for _, c := range classes {
		prefix := classPrefix(c)
		if prefix != "" && strings.HasPrefix(iri, prefix) {
			return rdf.DecodeIRIPath(strings.TrimPrefix(iri, prefix))
		}
	}
	if i := strings.LastIndex(iri, "/"); i >= 0 {
		return rdf.DecodeIRIPath(iri[i+1:])
	}
	return iri
}

func classPrefix(class string) string {
	switch class {
	case "runtime_evidence":
		return strings.TrimSuffix(rdf.MintIRI(rdf.ClassRuntimeEvidence, ""), ">")
	case "proof_obligation":
		return strings.TrimSuffix(rdf.MintIRI(rdf.ClassProofObligation, ""), ">")
	case "proof_slot":
		return strings.TrimSuffix(rdf.MintIRI(rdf.ClassProofSlot, ""), ">")
	case "repair_plan":
		return strings.TrimSuffix(rdf.MintIRI(rdf.ClassRepairPlan, ""), ">")
	case "test":
		return strings.TrimSuffix(rdf.MintIRI(rdf.ClassTest, ""), ">")
	case "evidence":
		return strings.TrimSuffix(rdf.MintIRI(rdf.ClassEvidence, ""), ">")
	case "authority_domain":
		return strings.TrimSuffix(rdf.MintIRI(rdf.ClassAuthorityDomain, ""), ">")
	case "authority_surface":
		return strings.TrimSuffix(rdf.MintIRI(rdf.ClassAuthoritySurface, ""), ">")
	case "source_file":
		return strings.TrimSuffix(rdf.MintIRI(rdf.ClassSourceFile, ""), ">")
	case "code_symbol":
		return strings.TrimSuffix(rdf.MintIRI(rdf.ClassCodeSymbol, ""), ">")
	case "architecture_claim":
		return strings.TrimSuffix(rdf.MintIRI(rdf.ClassArchitectureClaim, ""), ">")
	case "open_question":
		return strings.TrimSuffix(rdf.MintIRI(rdf.ClassOpenQuestion, ""), ">")
	case "evidence_probe":
		return strings.TrimSuffix(rdf.MintIRI(rdf.ClassEvidenceProbe, ""), ">")
	default:
		return ""
	}
}

func objectID(t graphsnapshot.Triple, classIRI string) string {
	if !t.ObjectIsIRI {
		return t.Object
	}
	prefix := strings.TrimSuffix(rdf.MintIRI(classIRI, ""), ">")
	if strings.HasPrefix(t.Object, prefix) {
		return rdf.DecodeIRIPath(strings.TrimPrefix(t.Object, prefix))
	}
	return t.Object
}
