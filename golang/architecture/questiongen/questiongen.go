// SPDX-License-Identifier: Apache-2.0

package questiongen

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/closure"
	"gopkg.in/yaml.v3"
)

const (
	SchemaVersion = "1"
	GeneratedBy   = "sensei generate-questions"

	DispositionGenerated             = "generated"
	DispositionExistingCovers        = "existing_covers"
	DispositionSkippedMechanical     = "skipped_mechanical"
	DispositionInsufficientGrounding = "insufficient_grounding"
	DispositionUnsupportedTemplate   = "unsupported_template"
	DispositionNoLongerBacked        = "no_longer_backed"
)

type TemplateDescriptor struct {
	ID                  string   `json:"id" yaml:"id"`
	Version             string   `json:"version" yaml:"version"`
	Title               string   `json:"title" yaml:"title"`
	Description         string   `json:"description,omitempty" yaml:"description,omitempty"`
	BlockerCodes        []string `json:"blocker_codes" yaml:"blocker_codes"`
	NextActionClasses   []string `json:"next_action_classes" yaml:"next_action_classes"`
	QuestionStatus      string   `json:"question_status" yaml:"question_status"`
	ArchitectRequired   bool     `json:"architect_required" yaml:"architect_required"`
	AcceptedAnswerTypes []string `json:"accepted_answer_types" yaml:"accepted_answer_types"`
	KnownLimitations    []string `json:"known_limitations,omitempty" yaml:"known_limitations,omitempty"`
}

type Template interface {
	Descriptor() TemplateDescriptor
	Generate(Context, closure.Blocker) (Candidate, error)
}

type Context struct {
	Closure                       closure.Report
	Claims                        architecture.ClaimDocument
	Graph                         closure.GraphIndex
	Existing                      *architecture.DialogueDocument
	CreatedAt                     string
	ClosureAssessmentDigestSHA256 string
}

type Candidate struct {
	Question architecture.OpenQuestion
	Blocker  closure.Blocker
}

type Registry struct {
	templates []Template
	byID      map[string]Template
}

type Result struct {
	Dialogue architecture.DialogueDocument
	Report   Report
}

type Report struct {
	SchemaVersion                       string                            `json:"schema_version" yaml:"schema_version"`
	GeneratedBy                         string                            `json:"generated_by" yaml:"generated_by"`
	Binding                             architecture.ClaimDocumentBinding `json:"binding" yaml:"binding"`
	SourceClosureAssessmentDigestSHA256 string                            `json:"source_closure_assessment_digest_sha256" yaml:"source_closure_assessment_digest_sha256"`
	Generated                           []Item                            `json:"generated,omitempty" yaml:"generated,omitempty"`
	ExistingCoverage                    []Item                            `json:"existing_coverage,omitempty" yaml:"existing_coverage,omitempty"`
	Skipped                             []Item                            `json:"skipped,omitempty" yaml:"skipped,omitempty"`
	NoLongerBacked                      []Item                            `json:"no_longer_backed,omitempty" yaml:"no_longer_backed,omitempty"`
	Limitations                         []architecture.Limitation         `json:"limitations,omitempty" yaml:"limitations,omitempty"`
}

type Item struct {
	BlockerID   string `json:"blocker_id,omitempty" yaml:"blocker_id,omitempty"`
	BlockerCode string `json:"blocker_code,omitempty" yaml:"blocker_code,omitempty"`
	Disposition string `json:"disposition" yaml:"disposition"`
	TemplateID  string `json:"template_id,omitempty" yaml:"template_id,omitempty"`
	QuestionID  string `json:"question_id,omitempty" yaml:"question_id,omitempty"`
	ReasonCode  string `json:"reason_code" yaml:"reason_code"`
	Detail      string `json:"detail,omitempty" yaml:"detail,omitempty"`
}

type reportEnvelope struct {
	ArchitectureQuestionGeneration Report `json:"architecture_question_generation" yaml:"architecture_question_generation"`
}

func NewRegistry(templates ...Template) (*Registry, error) {
	r := &Registry{byID: map[string]Template{}}
	for _, tmpl := range templates {
		d := tmpl.Descriptor()
		if d.ID == "" || !strings.Contains(d.ID, ".v") || d.Version == "" {
			return nil, fmt.Errorf("template %q must have stable versioned ID and version", d.ID)
		}
		if _, ok := r.byID[d.ID]; ok {
			return nil, fmt.Errorf("duplicate question template %s", d.ID)
		}
		r.byID[d.ID] = tmpl
		r.templates = append(r.templates, tmpl)
	}
	sort.SliceStable(r.templates, func(i, j int) bool {
		return r.templates[i].Descriptor().ID < r.templates[j].Descriptor().ID
	})
	return r, nil
}

func DefaultRegistry() (*Registry, error) {
	return NewRegistry(
		staticTemplate{
			desc: TemplateDescriptor{
				ID: "question.authority_definition.v1", Version: "v1", Title: "Authority definition",
				BlockerCodes: []string{
					"closure.authority.owner_missing", "closure.authority.owner_ambiguous", "closure.authority.state_unmapped",
					"closure.authority.truth_layer_missing", "closure.authority.allowed_writer_missing", "closure.authority.mutation_path_missing",
					"closure.authority.allowed_reader_missing", "closure.authority.read_path_missing",
				},
				NextActionClasses: []string{"define_authority"}, QuestionStatus: architecture.QuestionStatusAwaitingArchitect,
				ArchitectRequired: true,
				AcceptedAnswerTypes: []string{
					architecture.AnswerTypeIntentStatement, architecture.AnswerTypeGovernedDecisionCandidate, architecture.AnswerTypeHistoricalContext,
					architecture.AnswerTypeScopeClarification, architecture.AnswerTypeExceptionAuthorization, architecture.AnswerTypeEvidencePointer,
					architecture.AnswerTypeUnknownAcknowledgement, architecture.AnswerTypeQuestionReframing,
				},
			},
			text: authorityQuestionText,
		},
		staticTemplate{
			desc: TemplateDescriptor{
				ID: "question.claim_evidence_gap.v1", Version: "v1", Title: "Claim evidence gap",
				BlockerCodes: []string{
					"closure.question.missing_artifact", "closure.evidence.claim_unknown", "closure.evidence.claim_stale",
					"closure.evidence.support_missing", "closure.evidence.required_test_missing", "closure.evidence.current_test_or_evidence_missing",
					"closure.behavior.claim_unknown", "closure.behavior.claim_stale", "closure.contract.required_test_missing",
					"closure.agent.required_test_unidentified",
				},
				NextActionClasses: []string{"create_open_question", "add_evidence", "add_test"}, QuestionStatus: architecture.QuestionStatusAwaitingEvidence,
				ArchitectRequired: false,
				AcceptedAnswerTypes: []string{
					architecture.AnswerTypeEvidencePointer, architecture.AnswerTypeHistoricalContext, architecture.AnswerTypeScopeClarification,
					architecture.AnswerTypeUnknownAcknowledgement, architecture.AnswerTypeQuestionReframing,
				},
			},
			text: evidenceQuestionText,
		},
		staticTemplate{
			desc: TemplateDescriptor{
				ID: "question.contract_definition.v1", Version: "v1", Title: "Contract definition",
				BlockerCodes: []string{
					"closure.structural.cross_component_boundary_missing", "closure.contract.crossing_without_contract",
					"closure.contract.stability_unknown", "closure.contract.read_write_unknown", "closure.contract.deprecated",
					"closure.contract.invariant_or_evidence_missing",
				},
				NextActionClasses: []string{"define_contract"}, QuestionStatus: architecture.QuestionStatusAwaitingArchitect, ArchitectRequired: true,
				AcceptedAnswerTypes: []string{
					architecture.AnswerTypeIntentStatement, architecture.AnswerTypeGovernedDecisionCandidate, architecture.AnswerTypeScopeClarification,
					architecture.AnswerTypeHistoricalContext, architecture.AnswerTypeEvidencePointer, architecture.AnswerTypeUnknownAcknowledgement,
					architecture.AnswerTypeQuestionReframing,
				},
			},
			text: contractQuestionText,
		},
		staticTemplate{
			desc: TemplateDescriptor{
				ID: "question.contradiction_resolution.v1", Version: "v1", Title: "Contradiction resolution",
				BlockerCodes: []string{
					"closure.contradiction.claim_contested", "closure.contradiction.explicit_conflict", "closure.contradiction.current_intent_refuted",
					"closure.contradiction.current_desired_refuted", "closure.behavior.claim_contested", "closure.behavior.claim_refuted",
					"closure.direction.target_refuted",
				},
				NextActionClasses: []string{"resolve_contradiction"}, QuestionStatus: architecture.QuestionStatusAwaitingArchitect, ArchitectRequired: true,
				AcceptedAnswerTypes: []string{
					architecture.AnswerTypeIntentStatement, architecture.AnswerTypeGovernedDecisionCandidate, architecture.AnswerTypeHistoricalContext,
					architecture.AnswerTypeExceptionAuthorization, architecture.AnswerTypeEvidencePointer, architecture.AnswerTypeScopeClarification,
					architecture.AnswerTypeUnknownAcknowledgement, architecture.AnswerTypeQuestionReframing,
				},
			},
			text: contradictionQuestionText,
		},
		staticTemplate{
			desc: TemplateDescriptor{
				ID: "question.direction_definition.v1", Version: "v1", Title: "Direction definition",
				BlockerCodes:      []string{"closure.direction.intended_missing", "closure.direction.desired_missing", "closure.direction.historical_missing", "closure.direction.migration_constraint_missing"},
				NextActionClasses: []string{"promote_architectural_knowledge"}, QuestionStatus: architecture.QuestionStatusAwaitingArchitect, ArchitectRequired: true,
				AcceptedAnswerTypes: []string{
					architecture.AnswerTypeIntentStatement, architecture.AnswerTypeDesiredDirection, architecture.AnswerTypeGovernedDecisionCandidate,
					architecture.AnswerTypeHistoricalContext, architecture.AnswerTypeScopeClarification, architecture.AnswerTypeUnknownAcknowledgement,
					architecture.AnswerTypeQuestionReframing,
				},
			},
			text: directionQuestionText,
		},
		staticTemplate{
			desc: TemplateDescriptor{
				ID: "question.failure_surface.v1", Version: "v1", Title: "Failure surface",
				BlockerCodes: []string{"closure.behavior.failure_mode_missing"}, NextActionClasses: []string{"add_failure_mode"},
				QuestionStatus: architecture.QuestionStatusAwaitingArchitect, ArchitectRequired: true,
				AcceptedAnswerTypes: []string{
					architecture.AnswerTypeHistoricalContext, architecture.AnswerTypeIntentStatement, architecture.AnswerTypeEvidencePointer,
					architecture.AnswerTypeScopeClarification, architecture.AnswerTypeUnknownAcknowledgement, architecture.AnswerTypeQuestionReframing,
				},
			},
			text: failureQuestionText,
		},
		staticTemplate{
			desc: TemplateDescriptor{
				ID: "question.scope_clarification.v1", Version: "v1", Title: "Scope clarification",
				BlockerCodes:      []string{"closure.scope.empty_measured_surface", "closure.agent.scope_unrepresented", "closure.agent.guidance_surface_empty"},
				NextActionClasses: []string{"reassess_scope"}, QuestionStatus: architecture.QuestionStatusAwaitingArchitect, ArchitectRequired: true,
				AcceptedAnswerTypes: []string{
					architecture.AnswerTypeScopeClarification, architecture.AnswerTypeIntentStatement, architecture.AnswerTypeUnknownAcknowledgement,
					architecture.AnswerTypeQuestionReframing,
				},
			},
			text: scopeQuestionText,
		},
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
	seen := map[string]bool{}
	var selected []Template
	for _, id := range cleanStrings(ids) {
		t, ok := r.byID[id]
		if !ok {
			return nil, fmt.Errorf("unknown question template %s", id)
		}
		if !seen[id] {
			selected = append(selected, t)
			seen[id] = true
		}
	}
	return NewRegistry(selected...)
}

func (r *Registry) templateFor(blocker closure.Blocker) (Template, bool) {
	for _, t := range r.templates {
		d := t.Descriptor()
		if contains(d.BlockerCodes, blocker.Code) && contains(d.NextActionClasses, blocker.RequiredNextAction) {
			return t, true
		}
	}
	return nil, false
}

func Generate(ctx Context, registry *Registry) (Result, error) {
	if registry == nil {
		var err error
		registry, err = DefaultRegistry()
		if err != nil {
			return Result{}, err
		}
	}
	if ctx.CreatedAt == "" {
		return Result{}, errors.New("created_at is required")
	}
	if ctx.ClosureAssessmentDigestSHA256 == "" {
		return Result{}, errors.New("closure assessment digest is required")
	}
	if ctx.Existing != nil && !bindingsEqual(ctx.Existing.Binding, ctx.Closure.Request.Binding) {
		return Result{}, errors.New("existing dialogue binding does not match closure assessment")
	}
	dialogue := architecture.DialogueDocument{SchemaVersion: "1", CompiledBy: GeneratedBy, Binding: ctx.Closure.Request.Binding}
	if ctx.Existing != nil {
		dialogue = *ctx.Existing
	}
	report := Report{
		SchemaVersion: SchemaVersion, GeneratedBy: GeneratedBy, Binding: ctx.Closure.Request.Binding,
		SourceClosureAssessmentDigestSHA256: ctx.ClosureAssessmentDigestSHA256,
	}
	currentBlockers := map[string]closure.Blocker{}
	for _, blocker := range ctx.Closure.Blockers {
		currentBlockers[blocker.ID] = blocker
		if existing := coveringQuestion(dialogue.OpenQuestions, blocker); existing != "" {
			report.ExistingCoverage = append(report.ExistingCoverage, item(blocker, DispositionExistingCovers, "", existing, "questiongen.existing_covers", "existing question covers blocker"))
			continue
		}
		if mechanical(blocker) {
			report.Skipped = append(report.Skipped, item(blocker, DispositionSkippedMechanical, "", "", "questiongen.skipped_mechanical", "blocker requires mechanical input or repair"))
			continue
		}
		tmpl, ok := registry.templateFor(blocker)
		if !ok {
			report.Skipped = append(report.Skipped, item(blocker, DispositionUnsupportedTemplate, "", "", "questiongen.unsupported_template", "no registered template for blocker"))
			continue
		}
		cand, err := tmpl.Generate(ctx, blocker)
		if err != nil {
			report.Skipped = append(report.Skipped, item(blocker, DispositionInsufficientGrounding, tmpl.Descriptor().ID, "", "questiongen.insufficient_grounding", err.Error()))
			continue
		}
		q := cand.Question
		if existing := findQuestionByID(dialogue.OpenQuestions, q.ID); existing != nil {
			if !questionsEquivalent(*existing, q) {
				return Result{}, fmt.Errorf("generated question id collision for %s", q.ID)
			}
			report.ExistingCoverage = append(report.ExistingCoverage, item(blocker, DispositionExistingCovers, tmpl.Descriptor().ID, q.ID, "questiongen.existing_covers", "same generated question already exists"))
			continue
		}
		dialogue.OpenQuestions = append(dialogue.OpenQuestions, q)
		report.Generated = append(report.Generated, item(blocker, DispositionGenerated, tmpl.Descriptor().ID, q.ID, "questiongen.generated", "generated deterministic question"))
	}
	for _, q := range dialogue.OpenQuestions {
		if q.QuestionTemplateID == "" || len(q.BlocksClosureBlockers) == 0 {
			continue
		}
		backed := false
		for _, id := range q.BlocksClosureBlockers {
			if _, ok := currentBlockers[id]; ok {
				backed = true
				break
			}
		}
		if !backed {
			report.NoLongerBacked = append(report.NoLongerBacked, Item{
				Disposition: DispositionNoLongerBacked, TemplateID: q.QuestionTemplateID, QuestionID: q.ID,
				ReasonCode: "questiongen.no_longer_backed", Detail: "generated question blocker IDs are absent from current closure report",
			})
		}
	}
	normalized, err := architecture.NormalizeDialogueDocument(dialogue)
	if err != nil {
		return Result{}, err
	}
	report = normalizeReport(report)
	if err := validateDispositionAccounting(ctx.Closure.Blockers, report); err != nil {
		return Result{}, err
	}
	return Result{Dialogue: normalized, Report: report}, nil
}

type staticTemplate struct {
	desc TemplateDescriptor
	text func(Context, closure.Blocker) string
}

func (t staticTemplate) Descriptor() TemplateDescriptor { return normalizeDescriptor(t.desc) }

func (t staticTemplate) Generate(ctx Context, blocker closure.Blocker) (Candidate, error) {
	claims := resolvedClaims(ctx.Claims, blocker.ClaimIDs)
	nodeRefs, err := resolvedNodeRefs(ctx.Graph, blocker.NodeIDs)
	if err != nil {
		return Candidate{}, err
	}
	d := t.Descriptor()
	if d.ID == "question.claim_evidence_gap.v1" && len(claims)+len(nodeRefs) == 0 {
		return Candidate{}, errors.New("evidence-gap question requires claim or graph-node grounding")
	}
	q := architecture.OpenQuestion{
		QuestionText:                        t.text(ctx, blocker),
		Scope:                               questionScope(ctx, blocker, claims, nodeRefs),
		BlocksClosureDimension:              blocker.Dimension,
		BlocksClaims:                        claimIDs(claims),
		BlocksNodes:                         nodeRefs,
		BlocksClosureBlockers:               []string{blocker.ID},
		QuestionTemplateID:                  d.ID,
		QuestionTemplateVersion:             d.Version,
		SourceClosureAssessmentDigestSHA256: ctx.ClosureAssessmentDigestSHA256,
		AcceptedAnswerTypes:                 d.AcceptedAnswerTypes,
		ReasonsOpen:                         reasonsOpen(blocker, claims),
		KnownFactIDs:                        knownFacts(claims),
		KnownEvidence:                       knownEvidence(claims),
		CompetingHypotheses:                 hypotheses(d.ID, blocker, claims),
		MissingEvidence:                     missingEvidence(d.ID, blocker, claims),
		Priority:                            blocker.Severity,
		RiskIfUnresolved:                    "Closure dimension " + blocker.Dimension + " remains blocked by " + blocker.Code + ": " + blocker.Summary + ".",
		ArchitectRequired:                   d.ArchitectRequired,
		Status:                              d.QuestionStatus,
		CreatedAt:                           ctx.CreatedAt,
	}
	q.ID = architecture.StableOpenQuestionID(q)
	return Candidate{Question: q, Blocker: blocker}, nil
}

func authorityQuestionText(_ Context, b closure.Blocker) string {
	if b.Code == "closure.authority.owner_ambiguous" {
		return "The grounded authority surface has multiple explicit owners or writers. Is this intentional shared authority, delegation, migration, or an authority split?"
	}
	return "Which component is intended to own the grounded authority surface, and which mutation and observation paths are permitted?"
}

func contractQuestionText(_ Context, b closure.Blocker) string {
	if strings.Contains(b.Code, "stability") || strings.Contains(b.Code, "read_write") {
		return "What stability and read/write semantics should the grounded contract carry?"
	}
	return "What explicit contract is intended to govern the represented crossing?"
}

func contradictionQuestionText(_ Context, _ closure.Blocker) string {
	return "The grounded claims remain explicitly opposed in this scope. Which interpretation is current, are they scoped differently, or is one historical?"
}

func directionQuestionText(_ Context, b closure.Blocker) string {
	switch b.Code {
	case "closure.direction.desired_missing":
		return "What explicit desired target should this change move toward?"
	case "closure.direction.historical_missing":
		return "What historical architecture is being migrated away from?"
	case "closure.direction.migration_constraint_missing":
		return "What current constraint governs this migration?"
	default:
		return "What current intended architecture must this task preserve?"
	}
}

func evidenceQuestionText(ctx Context, b closure.Blocker) string {
	if len(b.NodeIDs) > 0 {
		return "Which runnable test, runtime observation, or source evidence should prove the behavior represented by the grounded node?"
	}
	claims := resolvedClaims(ctx.Claims, b.ClaimIDs)
	if len(claims) == 1 {
		return "Which current test, runtime observation, or source evidence establishes or refutes this proposition: " + claimProposition(claims[0]) + "?"
	}
	return "What current test, runtime observation, or source evidence can establish or refute the grounded claim?"
}

func claimProposition(claim architecture.Claim) string {
	statement := claim.Statement
	return strings.TrimSpace(strings.Join([]string{statement.Subject, statement.Predicate, statement.Object}, " "))
}

func failureQuestionText(_ Context, _ closure.Blocker) string {
	return "Which failure modes must this scope prevent, detect, or recover from?"
}

func scopeQuestionText(ctx Context, _ closure.Blocker) string {
	anchors := append([]string{}, ctx.Closure.ScopeReceipt.Files...)
	anchors = append(anchors, ctx.Closure.ScopeReceipt.Symbols...)
	anchors = append(anchors, ctx.Closure.ScopeReceipt.Components...)
	anchors = cleanStrings(anchors)
	if len(anchors) == 0 {
		return "Which bounded repository scope is intended for this closure request?"
	}
	return "Which bounded repository scope is intended for the requested anchors: " + strings.Join(anchors, ", ") + "?"
}

func MarshalCanonicalReportYAML(report Report) ([]byte, error) {
	return yaml.Marshal(reportEnvelope{ArchitectureQuestionGeneration: normalizeReport(report)})
}

func MarshalCanonicalReportJSON(report Report) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetIndent("", "  ")
	if err := enc.Encode(reportEnvelope{ArchitectureQuestionGeneration: normalizeReport(report)}); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func normalizeDescriptor(in TemplateDescriptor) TemplateDescriptor {
	d := in
	d.BlockerCodes = cleanStrings(d.BlockerCodes)
	d.NextActionClasses = cleanStrings(d.NextActionClasses)
	d.AcceptedAnswerTypes = cleanStrings(d.AcceptedAnswerTypes)
	d.KnownLimitations = cleanStrings(d.KnownLimitations)
	return d
}

func normalizeReport(in Report) Report {
	r := in
	r.SchemaVersion = SchemaVersion
	if r.GeneratedBy == "" {
		r.GeneratedBy = GeneratedBy
	}
	sortItems(r.Generated)
	sortItems(r.ExistingCoverage)
	sortItems(r.Skipped)
	sortItems(r.NoLongerBacked)
	return r
}

func sortItems(items []Item) {
	sort.SliceStable(items, func(i, j int) bool {
		a := items[i].Disposition + "\x00" + items[i].BlockerID + "\x00" + items[i].QuestionID
		b := items[j].Disposition + "\x00" + items[j].BlockerID + "\x00" + items[j].QuestionID
		return a < b
	})
}

func item(b closure.Blocker, disposition, templateID, questionID, reason, detail string) Item {
	return Item{BlockerID: b.ID, BlockerCode: b.Code, Disposition: disposition, TemplateID: templateID, QuestionID: questionID, ReasonCode: reason, Detail: detail}
}

func mechanical(b closure.Blocker) bool {
	if strings.HasPrefix(b.Code, "closure.binding.") || strings.HasPrefix(b.Code, "closure.input.") {
		return true
	}
	if b.Code == "closure.risk.unknown" || b.Code == "closure.agent.task_class_missing" ||
		b.Code == "closure.agent.access_mode_unknown" || b.Code == "closure.agent.direction_unknown" {
		return true
	}
	if b.RequiredNextAction == "answer_open_question" || strings.Contains(b.Code, "question_unresolved") || b.Code == "closure.question.accepted_unknown_blocks" {
		return true
	}
	return false
}

func coveringQuestion(questions []architecture.OpenQuestion, b closure.Blocker) string {
	for _, q := range questions {
		if q.Status == architecture.QuestionStatusSuperseded {
			continue
		}
		if contains(q.BlocksClosureBlockers, b.ID) {
			return q.ID
		}
		if q.BlocksClosureDimension == b.Dimension && (intersects(q.BlocksClaims, b.ClaimIDs) || nodeRefsIntersect(q.BlocksNodes, b.NodeIDs)) {
			return q.ID
		}
	}
	return ""
}

func findQuestionByID(questions []architecture.OpenQuestion, id string) *architecture.OpenQuestion {
	for i := range questions {
		if questions[i].ID == id {
			return &questions[i]
		}
	}
	return nil
}

func questionsEquivalent(a, b architecture.OpenQuestion) bool {
	a.Status, b.Status = "", ""
	a.ResolvedByAnswers, b.ResolvedByAnswers = nil, nil
	a.LastReviewedAt, b.LastReviewedAt = "", ""
	aj, _ := json.Marshal(a)
	bj, _ := json.Marshal(b)
	return bytes.Equal(aj, bj)
}

func resolvedClaims(doc architecture.ClaimDocument, ids []string) []architecture.Claim {
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	var out []architecture.Claim
	for _, c := range doc.Claims {
		if want[c.ID] {
			out = append(out, c)
		}
	}
	return out
}

func resolvedNodeRefs(graph closure.GraphIndex, ids []string) ([]string, error) {
	var out []string
	for _, id := range cleanStrings(ids) {
		var matches []closure.Node
		for _, n := range graph.Nodes {
			if n.ID == id {
				matches = append(matches, n)
			}
		}
		if len(matches) != 1 {
			return nil, fmt.Errorf("node ID %s is not uniquely grounded", id)
		}
		class := ""
		for _, c := range matches[0].Classes {
			if c != "" {
				class = c
				break
			}
		}
		if class == "" {
			return nil, fmt.Errorf("node ID %s has no explicit class", id)
		}
		out = append(out, class+":"+id)
	}
	return cleanStrings(out), nil
}

func questionScope(ctx Context, b closure.Blocker, claims []architecture.Claim, nodeRefs []string) architecture.ClaimScope {
	scope := architecture.ClaimScope{Repository: ctx.Closure.Request.Binding.RepositoryDomain, Repo: ctx.Closure.Request.Binding.RepositoryDomain, Domain: ctx.Closure.Request.Scope.Domain, SourceSet: ctx.Closure.Request.Scope.SourceSet}
	scope.Files = append(scope.Files, b.Files...)
	for _, c := range claims {
		scope.Files = append(scope.Files, c.Scope.Files...)
		scope.Symbols = append(scope.Symbols, c.Scope.Symbols...)
		scope.Components = append(scope.Components, c.Scope.Components...)
	}
	for _, ref := range nodeRefs {
		class, id, _ := architecture.ParseClassQualifiedReference(ref)
		if class == "component" {
			scope.Components = append(scope.Components, id)
		}
		if n, ok := findNode(ctx.Graph, id); ok {
			scope.Files = append(scope.Files, n.SourcePath)
			scope.Files = append(scope.Files, n.AuthoredIn...)
		}
	}
	scope.Files = cleanStrings(scope.Files)
	scope.Symbols = cleanStrings(scope.Symbols)
	scope.Components = cleanStrings(scope.Components)
	return scope
}

func findNode(graph closure.GraphIndex, id string) (closure.Node, bool) {
	for _, n := range graph.Nodes {
		if n.ID == id {
			return n, true
		}
	}
	return closure.Node{}, false
}

func reasonsOpen(b closure.Blocker, claims []architecture.Claim) []string {
	out := []string{b.Code + ": " + b.Summary}
	for _, c := range claims {
		out = append(out, c.ID+" status "+c.EpistemicStatus)
	}
	return cleanStrings(out)
}

func knownFacts(claims []architecture.Claim) []string {
	var out []string
	for _, c := range claims {
		out = append(out, c.PremiseFacts...)
	}
	return cleanStrings(out)
}

func knownEvidence(claims []architecture.Claim) []string {
	var out []string
	for _, c := range claims {
		out = append(out, c.SupportingEvidence...)
		out = append(out, c.RefutingEvidence...)
	}
	return cleanStrings(out)
}

func missingEvidence(templateID string, b closure.Blocker, claims []architecture.Claim) []string {
	var out []string
	if strings.Contains(templateID, "evidence") {
		out = append(out, b.Summary)
	}
	for _, c := range claims {
		out = append(out, c.Unknowns...)
		out = append(out, c.InvalidationConditions...)
	}
	return cleanStrings(out)
}

func hypotheses(templateID string, b closure.Blocker, claims []architecture.Claim) []architecture.QuestionHypothesis {
	var out []architecture.QuestionHypothesis
	for _, c := range claims {
		for _, alt := range c.AlternativeExplanations {
			out = append(out, architecture.QuestionHypothesis{ID: stableAlternativeHypothesisID(c.ID, alt), Statement: alt})
		}
		for _, conflict := range c.ConflictsWith {
			out = append(out, architecture.QuestionHypothesis{ID: safeHypothesisID(conflict), Statement: "Conflicting claim " + conflict + " is current."})
		}
	}
	if templateID == "question.authority_definition.v1" && (b.Code == "closure.authority.owner_ambiguous" || len(b.NodeIDs) > 1) {
		out = append(out,
			architecture.QuestionHypothesis{ID: "single_owner_delegates", Statement: "single owner with delegated writers"},
			architecture.QuestionHypothesis{ID: "temporary_migration_exception", Statement: "temporary migration exception"},
			architecture.QuestionHypothesis{ID: "intentional_shared_authority", Statement: "intentional shared authority"},
			architecture.QuestionHypothesis{ID: "unintended_authority_split", Statement: "unintended authority split"},
		)
	}
	if len(out) == 1 {
		return nil
	}
	byID := map[string]architecture.QuestionHypothesis{}
	for _, hypothesis := range out {
		if existing, ok := byID[hypothesis.ID]; ok {
			if existing.Statement == hypothesis.Statement {
				continue
			}
			hypothesis.ID += "_" + shortHypothesisDigest(hypothesis.Statement)
		}
		byID[hypothesis.ID] = hypothesis
	}
	out = out[:0]
	for _, hypothesis := range byID {
		out = append(out, hypothesis)
	}
	if len(out) == 1 {
		return nil
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func stableAlternativeHypothesisID(claimID, statement string) string {
	return "alternative_" + shortHypothesisDigest(strings.TrimSpace(claimID)+"\x00"+strings.TrimSpace(statement))
}

func shortHypothesisDigest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])[:12]
}

func safeHypothesisID(s string) string {
	s = strings.TrimPrefix(s, "claim.")
	s = strings.NewReplacer(".", "_", "-", "_", ":", "_").Replace(s)
	if s == "" {
		return "conflict"
	}
	return "conflict_" + s
}

func claimIDs(claims []architecture.Claim) []string {
	var out []string
	for _, c := range claims {
		out = append(out, c.ID)
	}
	return cleanStrings(out)
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

func contains(in []string, want string) bool {
	for _, item := range in {
		if item == want {
			return true
		}
	}
	return false
}

func intersects(a, b []string) bool {
	seen := map[string]bool{}
	for _, item := range a {
		seen[item] = true
	}
	for _, item := range b {
		if seen[item] {
			return true
		}
	}
	return false
}

func nodeRefsIntersect(classRefs, nodeIDs []string) bool {
	seen := map[string]bool{}
	for _, ref := range classRefs {
		_, id, ok := architecture.ParseClassQualifiedReference(ref)
		if ok {
			seen[id] = true
			continue
		}
		seen[ref] = true
	}
	for _, id := range nodeIDs {
		if seen[id] {
			return true
		}
	}
	return false
}

func bindingsEqual(a, b architecture.ClaimDocumentBinding) bool {
	return a.RepositoryDomain == b.RepositoryDomain &&
		a.Revision == b.Revision &&
		a.RevisionStatus == b.RevisionStatus &&
		a.TreeDigestSHA256 == b.TreeDigestSHA256 &&
		a.GraphDigestSHA256 == b.GraphDigestSHA256 &&
		a.GraphDigestStatus == b.GraphDigestStatus
}

func validateDispositionAccounting(blockers []closure.Blocker, report Report) error {
	want := map[string]bool{}
	for _, b := range blockers {
		want[b.ID] = true
	}
	got := map[string]bool{}
	for _, items := range [][]Item{report.Generated, report.ExistingCoverage, report.Skipped} {
		for _, item := range items {
			if item.BlockerID != "" {
				got[item.BlockerID] = true
			}
		}
	}
	for id := range want {
		if !got[id] {
			return fmt.Errorf("blocker %s has no generation disposition", id)
		}
	}
	return nil
}

func StableDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
