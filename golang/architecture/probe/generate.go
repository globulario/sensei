// SPDX-License-Identifier: Apache-2.0

package probe

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/closure"
	"github.com/globulario/sensei/golang/architecture/maintenance"
	"github.com/globulario/sensei/golang/architecture/plane"
	"gopkg.in/yaml.v3"
)

const (
	DispositionGenerated             = "generated"
	DispositionExistingCovers        = "existing_covers"
	DispositionAwaitingArchitect     = "awaiting_architect"
	DispositionNoEvidenceGap         = "no_evidence_gap"
	DispositionInsufficientGrounding = "insufficient_grounding"
	DispositionUnsupportedTemplate   = "unsupported_template"
	DispositionUnavailable           = "unavailable"
	DispositionManualHighRisk        = "manual_high_risk"
	DispositionNoLongerBacked        = "no_longer_backed"
)

type TemplateDescriptor struct {
	ID                  string   `json:"id" yaml:"id"`
	Version             string   `json:"version" yaml:"version"`
	Title               string   `json:"title" yaml:"title"`
	Description         string   `json:"description,omitempty" yaml:"description,omitempty"`
	QuestionTemplateIDs []string `json:"question_template_ids,omitempty" yaml:"question_template_ids,omitempty"`
	BlockerCodes        []string `json:"blocker_codes,omitempty" yaml:"blocker_codes,omitempty"`
	ProofSlotKinds      []string `json:"proof_slot_kinds,omitempty" yaml:"proof_slot_kinds,omitempty"`
	ProbeKind           string   `json:"probe_kind" yaml:"probe_kind"`
	EvidenceLane        string   `json:"evidence_lane" yaml:"evidence_lane"`
	SafetyClass         string   `json:"safety_class" yaml:"safety_class"`
	ApprovalGate        string   `json:"approval_gate" yaml:"approval_gate"`
	KnownLimitations    []string `json:"known_limitations,omitempty" yaml:"known_limitations,omitempty"`
}

type Template interface {
	Descriptor() TemplateDescriptor
	Generate(Context, architecture.OpenQuestion) ([]EvidenceProbe, error)
}

type Registry struct {
	templates []Template
	byID      map[string]Template
}

type Context struct {
	Closure     closure.Report
	Claims      architecture.ClaimDocument
	Dialogue    architecture.DialogueDocument
	Maintenance *maintenance.Report
	Plane       *plane.Report
	Evidence    *maintenance.EvidenceStateDocument
	Graph       GraphIndex
	Existing    *ProbeDocument

	SourceClosureDigest  string
	SourceDialogueDigest string
	SourceClaimsDigest   string
}

type GenerationResult struct {
	Document ProbeDocument
	Report   GenerationReport
}

type GenerationReport struct {
	SchemaVersion                       string                            `json:"schema_version" yaml:"schema_version"`
	GeneratedBy                         string                            `json:"generated_by" yaml:"generated_by"`
	Binding                             architecture.ClaimDocumentBinding `json:"binding" yaml:"binding"`
	SourceClosureAssessmentDigestSHA256 string                            `json:"source_closure_assessment_digest_sha256" yaml:"source_closure_assessment_digest_sha256"`
	SourceDialogueDigestSHA256          string                            `json:"source_dialogue_digest_sha256" yaml:"source_dialogue_digest_sha256"`
	SourceClaimDocumentDigestSHA256     string                            `json:"source_claim_document_digest_sha256" yaml:"source_claim_document_digest_sha256"`
	Items                               []GenerationItem                  `json:"items" yaml:"items"`
	Limitations                         []architecture.Limitation         `json:"limitations,omitempty" yaml:"limitations,omitempty"`
}

type GenerationItem struct {
	QuestionID   string   `json:"question_id,omitempty" yaml:"question_id,omitempty"`
	BlockerIDs   []string `json:"blocker_ids,omitempty" yaml:"blocker_ids,omitempty"`
	TemplateID   string   `json:"template_id,omitempty" yaml:"template_id,omitempty"`
	ProbeIDs     []string `json:"probe_ids,omitempty" yaml:"probe_ids,omitempty"`
	Disposition  string   `json:"disposition" yaml:"disposition"`
	ReasonCode   string   `json:"reason_code" yaml:"reason_code"`
	Detail       string   `json:"detail,omitempty" yaml:"detail,omitempty"`
	SafetyClass  string   `json:"safety_class,omitempty" yaml:"safety_class,omitempty"`
	ApprovalGate string   `json:"approval_gate,omitempty" yaml:"approval_gate,omitempty"`
}

type generationReportEnvelope struct {
	ArchitectureProbeGeneration GenerationReport `json:"architecture_probe_generation" yaml:"architecture_probe_generation"`
}

func NewRegistry(templates ...Template) (*Registry, error) {
	r := &Registry{byID: map[string]Template{}}
	for _, t := range templates {
		d := normalizeDescriptor(t.Descriptor())
		if d.ID == "" || !strings.Contains(d.ID, ".v") || d.Version == "" {
			return nil, fmt.Errorf("probe template %q must be versioned", d.ID)
		}
		if _, ok := r.byID[d.ID]; ok {
			return nil, fmt.Errorf("duplicate probe template %s", d.ID)
		}
		r.byID[d.ID] = t
		r.templates = append(r.templates, t)
	}
	sort.SliceStable(r.templates, func(i, j int) bool { return r.templates[i].Descriptor().ID < r.templates[j].Descriptor().ID })
	return r, nil
}

func DefaultRegistry() (*Registry, error) {
	return NewRegistry(
		staticTemplate{desc: TemplateDescriptor{ID: "probe.source_receipt_verification.v1", Version: "v1", Title: "Source receipt verification", ProbeKind: KindSourceReceiptVerification, EvidenceLane: LaneStatic, SafetyClass: SafetyStaticRead, ApprovalGate: GateNone}, generate: generateSourceReceipt},
		staticTemplate{desc: TemplateDescriptor{ID: "probe.existing_test_execution.v1", Version: "v1", Title: "Existing test execution", ProbeKind: KindTestExecution, EvidenceLane: LaneTest, SafetyClass: SafetyLocalTest, ApprovalGate: GateReviewRequired, ProofSlotKinds: []string{"test_or_runtime"}}, generate: generateExistingTest},
		staticTemplate{desc: TemplateDescriptor{ID: "probe.owner_path_runtime_observation.v1", Version: "v1", Title: "Owner-path runtime observation", ProbeKind: KindOwnerPathRuntimeObservation, EvidenceLane: LaneRuntime, SafetyClass: SafetyRuntimeRead, ApprovalGate: GateReviewRequired}, generate: generateRuntimeObservation},
		staticTemplate{desc: TemplateDescriptor{ID: "probe.proof_slot_artifact_collection.v1", Version: "v1", Title: "Proof-slot artifact collection", ProbeKind: KindArtifactCollection, EvidenceLane: LaneArtifact, SafetyClass: SafetyStaticRead, ApprovalGate: GateNone, ProofSlotKinds: []string{"static_guard", "scope_mapping", "before_after", "artifact", "input_validation", "negative_contract", "process_artifact", "log_artifact"}}, generate: generateArtifactCollection},
		staticTemplate{desc: TemplateDescriptor{ID: "probe.evidence_reconciliation.v1", Version: "v1", Title: "Evidence reconciliation", ProbeKind: KindEvidenceReconciliation, EvidenceLane: LaneDiagnostic, SafetyClass: SafetyStaticRead, ApprovalGate: GateNone}, generate: generateReconciliation},
		staticTemplate{desc: TemplateDescriptor{ID: "probe.controlled_discriminating_experiment.v1", Version: "v1", Title: "Controlled discriminating experiment", ProbeKind: KindControlledExperiment, EvidenceLane: LaneHybrid, SafetyClass: SafetyIsolatedMutation, ApprovalGate: GateHumanApprovalRequired, ProofSlotKinds: []string{"failure_evidence", "runtime", "test_or_runtime"}}, generate: generateControlledExperiment},
		staticTemplate{desc: TemplateDescriptor{ID: "probe.manual_observation.v1", Version: "v1", Title: "Manual observation", ProbeKind: KindManualObservation, EvidenceLane: LaneDiagnostic, SafetyClass: SafetyRuntimeRead, ApprovalGate: GateReviewRequired}, generate: generateManualObservation},
	)
}

func (r *Registry) Descriptors() []TemplateDescriptor {
	out := make([]TemplateDescriptor, 0, len(r.templates))
	for _, t := range r.templates {
		out = append(out, normalizeDescriptor(t.Descriptor()))
	}
	return out
}

func (r *Registry) SelectIDs(ids []string) (*Registry, error) {
	if len(ids) == 0 {
		return r, nil
	}
	var selected []Template
	seen := map[string]bool{}
	for _, id := range cleanStrings(ids) {
		t, ok := r.byID[id]
		if !ok {
			return nil, fmt.Errorf("unknown probe template %s", id)
		}
		if !seen[id] {
			selected = append(selected, t)
			seen[id] = true
		}
	}
	return NewRegistry(selected...)
}

func Generate(ctx Context, registry *Registry) (GenerationResult, error) {
	if registry == nil {
		var err error
		registry, err = DefaultRegistry()
		if err != nil {
			return GenerationResult{}, err
		}
	}
	if err := validateContext(ctx); err != nil {
		return GenerationResult{}, err
	}
	doc := ProbeDocument{
		SchemaVersion: SchemaVersion, GeneratedBy: GeneratedBy, Binding: ctx.Claims.Binding,
		SourceClosureAssessmentDigestSHA256: ctx.SourceClosureDigest,
		SourceDialogueDigestSHA256:          ctx.SourceDialogueDigest,
		SourceClaimDocumentDigestSHA256:     ctx.SourceClaimsDigest,
	}
	if ctx.Existing != nil {
		if !BindingEqual(ctx.Existing.Binding, ctx.Claims.Binding) {
			return GenerationResult{}, errors.New("existing probe binding does not match claims")
		}
		doc = *ctx.Existing
	}
	report := GenerationReport{
		SchemaVersion: SchemaVersion, GeneratedBy: GeneratedBy, Binding: ctx.Claims.Binding,
		SourceClosureAssessmentDigestSHA256: ctx.SourceClosureDigest,
		SourceDialogueDigestSHA256:          ctx.SourceDialogueDigest,
		SourceClaimDocumentDigestSHA256:     ctx.SourceClaimsDigest,
	}
	currentQuestions := map[string]architecture.OpenQuestion{}
	for _, q := range ctx.Dialogue.OpenQuestions {
		currentQuestions[q.ID] = q
		itemBase := GenerationItem{QuestionID: q.ID, BlockerIDs: cleanStrings(q.BlocksClosureBlockers)}
		if q.Status == architecture.QuestionStatusAwaitingArchitect {
			report.Items = append(report.Items, withDisposition(itemBase, DispositionAwaitingArchitect, "probe.awaiting_architect", "architect judgement required before empirical probe"))
			continue
		}
		if !QuestionEligible(ctx.Dialogue, q) {
			report.Items = append(report.Items, withDisposition(itemBase, DispositionNoEvidenceGap, "probe.no_evidence_gap", "question is not awaiting empirical evidence"))
			continue
		}
		if existing := coveringProbe(doc.Probes, q); existing != "" {
			item := withDisposition(itemBase, DispositionExistingCovers, "probe.existing_covers", "existing probe covers question")
			item.ProbeIDs = []string{existing}
			report.Items = append(report.Items, item)
			continue
		}
		candidates := candidateProbes(ctx, registry, q)
		if len(candidates) == 0 {
			report.Items = append(report.Items, withDisposition(itemBase, DispositionUnsupportedTemplate, "probe.unsupported_template", "no probe template applies"))
			continue
		}
		sort.SliceStable(candidates, func(i, j int) bool {
			if SafetyRank(candidates[i].SafetyClass) != SafetyRank(candidates[j].SafetyClass) {
				return SafetyRank(candidates[i].SafetyClass) < SafetyRank(candidates[j].SafetyClass)
			}
			return candidates[i].ID < candidates[j].ID
		})
		selected := candidates[0]
		if selected.Status == StatusUnavailable {
			doc.Probes = append(doc.Probes, selected)
			item := withDisposition(itemBase, DispositionUnavailable, "probe.unavailable", strings.Join(selected.Limitations, "; "))
			item.TemplateID, item.ProbeIDs, item.SafetyClass, item.ApprovalGate = selected.TemplateID, []string{selected.ID}, selected.SafetyClass, selected.ApprovalGate
			report.Items = append(report.Items, item)
			continue
		}
		if existing := findSameProbe(doc.Probes, selected); existing != "" {
			item := withDisposition(itemBase, DispositionExistingCovers, "probe.existing_covers", "existing probe has same target")
			item.TemplateID, item.ProbeIDs, item.SafetyClass, item.ApprovalGate = selected.TemplateID, []string{existing}, selected.SafetyClass, selected.ApprovalGate
			report.Items = append(report.Items, item)
			continue
		}
		doc.Probes = append(doc.Probes, selected)
		item := withDisposition(itemBase, DispositionGenerated, "probe.generated", "generated least-invasive evidence probe")
		item.TemplateID, item.ProbeIDs, item.SafetyClass, item.ApprovalGate = selected.TemplateID, []string{selected.ID}, selected.SafetyClass, selected.ApprovalGate
		report.Items = append(report.Items, item)
	}
	for _, p := range doc.Probes {
		q, ok := currentQuestions[p.QuestionID]
		if !ok || !QuestionEligible(ctx.Dialogue, q) {
			report.Items = append(report.Items, GenerationItem{QuestionID: p.QuestionID, TemplateID: p.TemplateID, ProbeIDs: []string{p.ID}, Disposition: DispositionNoLongerBacked, ReasonCode: "probe.no_longer_backed", Detail: "probe preserved but question is absent or no longer evidence-seeking", SafetyClass: p.SafetyClass, ApprovalGate: p.ApprovalGate})
		}
	}
	normalized, err := NormalizeProbeDocument(doc, &ValidationContext{Dialogue: ctx.Dialogue, Claims: ctx.Claims, Graph: ctx.Graph})
	if err != nil {
		return GenerationResult{}, err
	}
	report = normalizeReport(report)
	return GenerationResult{Document: normalized, Report: report}, nil
}

type staticTemplate struct {
	desc     TemplateDescriptor
	generate func(Context, architecture.OpenQuestion, TemplateDescriptor) ([]EvidenceProbe, error)
}

func (t staticTemplate) Descriptor() TemplateDescriptor { return normalizeDescriptor(t.desc) }
func (t staticTemplate) Generate(ctx Context, q architecture.OpenQuestion) ([]EvidenceProbe, error) {
	return t.generate(ctx, q, t.Descriptor())
}

func generateSourceReceipt(ctx Context, q architecture.OpenQuestion, d TemplateDescriptor) ([]EvidenceProbe, error) {
	var steps []ProbeStep
	claims := targetClaims(ctx, q)
	for _, c := range claims {
		if c.EpistemicStatus != architecture.StatusUnknown && c.EpistemicStatus != architecture.StatusStale && !containsAny(q.MissingEvidence, "source", "receipt", "stale", "digest") {
			continue
		}
		for _, fid := range c.PremiseFacts {
			if r, ok := factReceipt(ctx.Claims, fid); ok && r.Fact.Evidence.SourceFile != "" {
				steps = append(steps, ProbeStep{Kind: StepVerifySourceDigest, Target: r.Fact.Evidence.SourceFile, Description: "Recompute source SHA-256 and compare with the fact receipt.", SourceRef: fid})
			}
		}
	}
	if len(steps) == 0 {
		return nil, nil
	}
	return []EvidenceProbe{baseProbe(ctx, q, d, steps, RoleDiagnostic)}, nil
}

func generateExistingTest(ctx Context, q architecture.OpenQuestion, d TemplateDescriptor) ([]EvidenceProbe, error) {
	var testIDs []string
	var steps []ProbeStep
	for _, slot := range ctx.Graph.Class("proof_slot") {
		if slot.SlotKind == "test_or_runtime" {
			for _, tid := range slot.RequiredTests {
				testIDs = append(testIDs, tid)
			}
		}
	}
	for _, n := range ctx.Graph.Class("test") {
		testIDs = append(testIDs, n.ID)
	}
	testIDs = cleanStrings(testIDs)
	if len(testIDs) == 0 {
		p := unavailableProbe(ctx, q, d, "no exact Test node or authored Evidence command is grounded")
		return []EvidenceProbe{p}, nil
	}
	for _, tid := range testIDs {
		steps = append(steps, ProbeStep{Kind: StepRunExistingTest, Target: tid, Description: "Run the exact existing Test outside Sensei and record the result."})
	}
	p := baseProbe(ctx, q, d, steps, RoleDiagnostic)
	p.TestIDs = testIDs
	for _, ev := range ctx.Graph.Class("evidence") {
		if ev.Command != "" {
			p.Steps = append(p.Steps, ProbeStep{Kind: StepRunExistingTest, Target: ev.ID, Description: "Use the exact authored Evidence command outside Sensei.", SourceRef: "evidence:" + ev.ID, Command: ev.Command})
			break
		}
	}
	p.ID = StableProbeID(p, ctx.Claims.Binding.RepositoryDomain)
	return []EvidenceProbe{p}, nil
}

func generateRuntimeObservation(ctx Context, q architecture.OpenQuestion, d TemplateDescriptor) ([]EvidenceProbe, error) {
	var out []EvidenceProbe
	for _, n := range ctx.Graph.Class("runtime_evidence") {
		if n.ObservedFromService == "" || len(n.ObservedViaPaths) == 0 {
			p := unavailableProbe(ctx, q, d, "runtime evidence profile lacks owner service or observation path")
			p.RuntimeEvidenceIDs = []string{n.ID}
			out = append(out, p)
			continue
		}
		p := baseProbe(ctx, q, d, []ProbeStep{{Kind: StepInvokeOwnerReadPath, Target: n.ObservedViaPaths[0], Description: "Observe the owner-approved read path externally and record the result.", SourceRef: "runtime_evidence:" + n.ID}}, RoleDiagnostic)
		p.RuntimeEvidenceIDs = []string{n.ID}
		p.OwnerService = n.ObservedFromService
		p.ObservationPaths = n.ObservedViaPaths
		p.FreshnessWindow = n.FreshnessWindow
		p.TrustLevel = n.TrustLevel
		p.MustComeFromOwnerPath = n.MustComeFromOwnerPath
		p.CannotPromoteToPassWhenStale = n.CannotPromoteToPassWhenStale
		p.ID = StableProbeID(p, ctx.Claims.Binding.RepositoryDomain)
		out = append(out, p)
	}
	return out, nil
}

func generateArtifactCollection(ctx Context, q architecture.OpenQuestion, d TemplateDescriptor) ([]EvidenceProbe, error) {
	var out []EvidenceProbe
	allowed := map[string]bool{}
	for _, k := range d.ProofSlotKinds {
		allowed[k] = true
	}
	for _, slot := range ctx.Graph.Class("proof_slot") {
		if !allowed[slot.SlotKind] {
			continue
		}
		safety, gate := SafetyStaticRead, GateNone
		if slot.SlotKind == "process_artifact" || slot.SlotKind == "log_artifact" {
			safety, gate = SafetyRuntimeRead, GateReviewRequired
		}
		p := baseProbe(ctx, q, d, []ProbeStep{{Kind: StepCollectArtifact, Target: slot.ID, Description: "Collect the exact artifact required by proof slot " + slot.ID + ".", SourceRef: "proof_slot:" + slot.ID}}, RoleDiagnostic)
		p.SafetyClass, p.ApprovalGate = safety, gate
		p.ProofSlotIDs = []string{slot.ID}
		p.ExpectedArtifactKinds = []string{slot.SlotKind}
		p.ID = StableProbeID(p, ctx.Claims.Binding.RepositoryDomain)
		out = append(out, p)
	}
	return out, nil
}

func generateReconciliation(ctx Context, q architecture.OpenQuestion, d TemplateDescriptor) ([]EvidenceProbe, error) {
	for _, c := range targetClaims(ctx, q) {
		if c.EpistemicStatus == architecture.StatusContested || (len(c.SupportingEvidence) > 0 && len(c.RefutingEvidence) > 0) {
			var steps []ProbeStep
			for _, ev := range append(append([]string{}, c.SupportingEvidence...), c.RefutingEvidence...) {
				steps = append(steps, ProbeStep{Kind: StepCompareEvidenceReceipts, Target: ev, Description: "Collect current receipt for explicit Evidence without choosing a winner.", SourceRef: ev})
			}
			p := baseProbe(ctx, q, d, steps, RoleDiagnostic)
			p.ClaimIDs = []string{c.ID}
			p.ID = StableProbeID(p, ctx.Claims.Binding.RepositoryDomain)
			return []EvidenceProbe{p}, nil
		}
	}
	return nil, nil
}

func generateControlledExperiment(ctx Context, q architecture.OpenQuestion, d TemplateDescriptor) ([]EvidenceProbe, error) {
	if len(q.CompetingHypotheses) < 2 {
		return nil, nil
	}
	hasGrounding := false
	for _, slot := range ctx.Graph.Class("proof_slot") {
		if slot.SlotKind == "failure_evidence" || slot.SlotKind == "runtime" || slot.SlotKind == "test_or_runtime" {
			hasGrounding = true
		}
	}
	if !hasGrounding && len(ctx.Graph.Class("repair_plan")) == 0 {
		return nil, nil
	}
	p := baseProbe(ctx, q, d, []ProbeStep{{Kind: StepPerformControlledExperiment, Target: q.ID, Description: "Design an externally approved discriminating experiment for the grounded hypotheses."}}, RoleDiagnostic)
	p.AutomaticExecutionAllowed = false
	p.ID = StableProbeID(p, ctx.Claims.Binding.RepositoryDomain)
	return []EvidenceProbe{p}, nil
}

func generateManualObservation(ctx Context, q architecture.OpenQuestion, d TemplateDescriptor) ([]EvidenceProbe, error) {
	if len(q.BlocksNodes)+len(q.KnownEvidence)+len(q.MissingEvidence) == 0 {
		return nil, nil
	}
	target := q.ID
	if len(q.BlocksNodes) > 0 {
		target = q.BlocksNodes[0]
	}
	p := baseProbe(ctx, q, d, []ProbeStep{{Kind: StepRecordManualObservation, Target: target, Description: "Record the explicitly named observation externally and attach the receipt."}}, RoleDiagnostic)
	p.AutomaticExecutionAllowed = false
	p.ID = StableProbeID(p, ctx.Claims.Binding.RepositoryDomain)
	return []EvidenceProbe{p}, nil
}

func baseProbe(ctx Context, q architecture.OpenQuestion, d TemplateDescriptor, steps []ProbeStep, role string) EvidenceProbe {
	blockers := blockersForQuestion(ctx.Closure, q)
	p := EvidenceProbe{
		Label: "Probe for " + q.ID, Description: d.Title, Status: StatusProposed,
		QuestionID: q.ID, ClosureBlockerIDs: q.BlocksClosureBlockers, ClaimIDs: claimIDs(targetClaims(ctx, q)), NodeRefs: q.BlocksNodes,
		TemplateID: d.ID, TemplateVersion: d.Version, ProbeKind: d.ProbeKind, EvidenceLane: d.EvidenceLane, EvidenceRole: role,
		SafetyClass: d.SafetyClass, ApprovalGate: d.ApprovalGate, AutomaticExecutionAllowed: d.SafetyClass == SafetyStaticRead || d.SafetyClass == SafetyLocalTest,
		Steps: steps,
	}
	for _, b := range blockers {
		p.ClosureBlockerIDs = append(p.ClosureBlockerIDs, b.ID)
		p.ClaimIDs = append(p.ClaimIDs, b.ClaimIDs...)
		for _, n := range b.NodeIDs {
			if !strings.Contains(n, ":") {
				p.NodeRefs = append(p.NodeRefs, "component:"+n)
			} else {
				p.NodeRefs = append(p.NodeRefs, n)
			}
		}
	}
	p = canonicalizeProbe(p)
	p.ID = StableProbeID(p, ctx.Claims.Binding.RepositoryDomain)
	return p
}

func unavailableProbe(ctx Context, q architecture.OpenQuestion, d TemplateDescriptor, limitation string) EvidenceProbe {
	p := baseProbe(ctx, q, d, nil, RoleDiagnostic)
	p.Status = StatusUnavailable
	p.AutomaticExecutionAllowed = false
	p.Limitations = []string{limitation}
	p.ID = StableProbeID(p, ctx.Claims.Binding.RepositoryDomain)
	return p
}

func candidateProbes(ctx Context, registry *Registry, q architecture.OpenQuestion) []EvidenceProbe {
	var out []EvidenceProbe
	for _, tmpl := range registry.templates {
		probes, err := tmpl.Generate(ctx, q)
		if err != nil {
			continue
		}
		out = append(out, probes...)
	}
	seen := map[string]EvidenceProbe{}
	for _, p := range out {
		seen[p.ID] = p
	}
	out = out[:0]
	for _, p := range seen {
		out = append(out, p)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func validateContext(ctx Context) error {
	if !BindingEqual(ctx.Claims.Binding, ctx.Dialogue.Binding) || !BindingEqual(ctx.Claims.Binding, ctx.Closure.Request.Binding) {
		return errors.New("closure, claims, and dialogue bindings must match")
	}
	for _, digest := range []string{ctx.SourceClosureDigest, ctx.SourceDialogueDigest, ctx.SourceClaimsDigest} {
		if !sha256RE.MatchString(digest) {
			return errors.New("source artifact digests must be lowercase SHA-256")
		}
	}
	return nil
}

func targetClaims(ctx Context, q architecture.OpenQuestion) []architecture.Claim {
	want := map[string]bool{}
	for _, id := range q.BlocksClaims {
		want[id] = true
	}
	for _, b := range blockersForQuestion(ctx.Closure, q) {
		for _, id := range b.ClaimIDs {
			want[id] = true
		}
	}
	var out []architecture.Claim
	for _, c := range ctx.Claims.Claims {
		if want[c.ID] {
			out = append(out, c)
		}
	}
	return out
}

func blockersForQuestion(report closure.Report, q architecture.OpenQuestion) []closure.Blocker {
	want := map[string]bool{}
	for _, id := range q.BlocksClosureBlockers {
		want[id] = true
	}
	var out []closure.Blocker
	for _, b := range report.Blockers {
		if want[b.ID] {
			out = append(out, b)
		}
	}
	return out
}

func factReceipt(doc architecture.ClaimDocument, id string) (architecture.ClaimFactReceipt, bool) {
	for _, r := range doc.FactReceipts {
		if r.Fact.ID == id {
			return r, true
		}
	}
	return architecture.ClaimFactReceipt{}, false
}

func claimIDs(claims []architecture.Claim) []string {
	var out []string
	for _, c := range claims {
		out = append(out, c.ID)
	}
	return cleanStrings(out)
}

func normalizeDescriptor(in TemplateDescriptor) TemplateDescriptor {
	d := in
	d.QuestionTemplateIDs = cleanStrings(d.QuestionTemplateIDs)
	d.BlockerCodes = cleanStrings(d.BlockerCodes)
	d.ProofSlotKinds = cleanStrings(d.ProofSlotKinds)
	d.KnownLimitations = cleanStrings(d.KnownLimitations)
	return d
}

func coveringProbe(probes []EvidenceProbe, q architecture.OpenQuestion) string {
	for _, p := range probes {
		if p.Status == StatusSuperseded {
			continue
		}
		if p.QuestionID == q.ID {
			return p.ID
		}
	}
	return ""
}

func findSameProbe(probes []EvidenceProbe, p EvidenceProbe) string {
	for _, existing := range probes {
		if existing.QuestionID == p.QuestionID && existing.TemplateID == p.TemplateID && existing.TargetEvidenceID == p.TargetEvidenceID && strings.Join(existing.ProofSlotIDs, ",") == strings.Join(p.ProofSlotIDs, ",") {
			return existing.ID
		}
		if existing.ID == p.ID {
			return existing.ID
		}
	}
	return ""
}

func withDisposition(item GenerationItem, disposition, reason, detail string) GenerationItem {
	item.Disposition = disposition
	item.ReasonCode = reason
	item.Detail = detail
	item.BlockerIDs = cleanStrings(item.BlockerIDs)
	item.ProbeIDs = cleanStrings(item.ProbeIDs)
	return item
}

func normalizeReport(r GenerationReport) GenerationReport {
	r.SchemaVersion = SchemaVersion
	if r.GeneratedBy == "" {
		r.GeneratedBy = GeneratedBy
	}
	for i := range r.Items {
		r.Items[i].BlockerIDs = cleanStrings(r.Items[i].BlockerIDs)
		r.Items[i].ProbeIDs = cleanStrings(r.Items[i].ProbeIDs)
	}
	sort.SliceStable(r.Items, func(i, j int) bool {
		a := r.Items[i].QuestionID + "\x00" + r.Items[i].Disposition + "\x00" + r.Items[i].TemplateID
		b := r.Items[j].QuestionID + "\x00" + r.Items[j].Disposition + "\x00" + r.Items[j].TemplateID
		return a < b
	})
	return r
}

func MarshalGenerationReportYAML(report GenerationReport) ([]byte, error) {
	return yaml.Marshal(generationReportEnvelope{ArchitectureProbeGeneration: normalizeReport(report)})
}

func MarshalGenerationReportJSON(report GenerationReport) ([]byte, error) {
	return marshalJSON(generationReportEnvelope{ArchitectureProbeGeneration: normalizeReport(report)})
}

func containsAny(values []string, needles ...string) bool {
	hay := strings.ToLower(strings.Join(values, " "))
	for _, n := range needles {
		if strings.Contains(hay, strings.ToLower(n)) {
			return true
		}
	}
	return false
}
