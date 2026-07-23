// SPDX-License-Identifier: AGPL-3.0-only

// Package dashboardprojection implements the canonical producer-side data
// model for the sensei.dashboard.projection.v1 and
// sensei.dashboard.agent-handoff.v1 contracts adopted from
// globulario/sensei-dashboard (docs/schemas/dashboard-projection/v1). It holds
// only the typed shape, referential-integrity validation, and canonical
// digesting of a projection — it does not gather architectural evidence
// itself. Callers (cmd/awg) assemble a Projection from real Sensei sources and
// pass it here to validate and digest.
//
// This package must never invent architectural values. A field with no honest
// source is left at its zero/empty value with a documented limitation, never
// filled with a guessed or heuristic value.
package dashboardprojection

// SchemaVersion is the exact, adopted projection schema identifier. Schema
// evolution is strict: any field addition, removal, rename, or semantic
// change requires a new identifier, and Validate rejects any other value.
const SchemaVersion = "sensei.dashboard.projection.v1"

// HandoffSchemaVersion is the exact, adopted agent-handoff schema identifier.
const HandoffSchemaVersion = "sensei.dashboard.agent-handoff.v1"

// Projection is the top-level sensei.dashboard.projection.v1 document.
type Projection struct {
	SchemaVersion string          `json:"schema_version"`
	Identity      Identity        `json:"identity"`
	Availability  Availability    `json:"availability"`
	Assessments   Assessments     `json:"assessments"`
	ActiveContext *ActiveContext  `json:"active_context"`
	Briefing      []Briefing      `json:"briefing"`
	Facts         []Fact          `json:"facts,omitempty"`
	Regions       []Region        `json:"regions"`
	Components    []Component     `json:"components"`
	Boundaries    []Boundary      `json:"boundaries"`
	Contracts     []Contract      `json:"contracts"`
	Flows         []Flow          `json:"flows"`
	Attention     []AttentionItem `json:"attention"`
	Evolution     Evolution       `json:"evolution"`
	FocusRecords  []FocusRecord   `json:"focus_records"`
	Capabilities  *Capabilities   `json:"capabilities,omitempty"`
}

type Provenance struct {
	EvidenceRefs []string `json:"evidence_refs"`
	DecisionRefs []string `json:"decision_refs,omitempty"`
	SourceRefs   []string `json:"source_refs,omitempty"`
	ObservedAt   *string  `json:"observed_at,omitempty"`
	Limitations  []string `json:"limitations,omitempty"`
}

type Identity struct {
	ProjectionID   string         `json:"projection_id"`
	Repository     Repository     `json:"repository"`
	Revision       Revision       `json:"revision"`
	GraphAuthority GraphAuthority `json:"graph_authority"`
	GeneratedAt    string         `json:"generated_at"`
}

type Repository struct {
	Key         string  `json:"key"`
	DisplayName string  `json:"display_name"`
	URL         *string `json:"url,omitempty"`
	Domain      *string `json:"domain,omitempty"`
}

type Revision struct {
	ID          string  `json:"id"`
	Display     *string `json:"display,omitempty"`
	Ref         *string `json:"ref,omitempty"`
	CommittedAt *string `json:"committed_at,omitempty"`
}

// TriState mirrors the adopted schema's $defs/triState: "yes" | "no" | "unknown".
type TriState string

const (
	TriYes     TriState = "yes"
	TriNo      TriState = "no"
	TriUnknown TriState = "unknown"
)

type GraphAuthority struct {
	Observed   TriState    `json:"observed"`
	Current    TriState    `json:"current"`
	Identity   *string     `json:"identity"`
	Summary    string      `json:"summary"`
	Provenance *Provenance `json:"provenance,omitempty"`
}

// AvailabilityState mirrors $defs/availabilityState.
type AvailabilityState string

const (
	Available   AvailabilityState = "available"
	Partial     AvailabilityState = "partial"
	Unavailable AvailabilityState = "unavailable"
)

// KnowledgeState mirrors $defs/knowledgeState.
type KnowledgeState string

const (
	StateHealthy       KnowledgeState = "healthy"
	StateAttention     KnowledgeState = "attention"
	StateAtRisk        KnowledgeState = "at_risk"
	StateDegraded      KnowledgeState = "degraded"
	StateOpen          KnowledgeState = "open"
	StateProven        KnowledgeState = "proven"
	StateContested     KnowledgeState = "contested"
	StateUnknown       KnowledgeState = "unknown"
	StateUnobserved    KnowledgeState = "unobserved"
	StateNotApplicable KnowledgeState = "not_applicable"
)

// Severity mirrors $defs/severity.
type Severity string

const (
	SeverityInfo          Severity = "info"
	SeverityLow           Severity = "low"
	SeverityMedium        Severity = "medium"
	SeverityHigh          Severity = "high"
	SeverityCritical      Severity = "critical"
	SeverityUnknown       Severity = "unknown"
	SeverityNotApplicable Severity = "not_applicable"
)

type SourceState struct {
	Owner        string            `json:"owner"`
	Availability AvailabilityState `json:"availability"`
	Summary      string            `json:"summary"`
	Limitations  []string          `json:"limitations,omitempty"`
}

type Availability struct {
	State       AvailabilityState `json:"state"`
	Summary     string            `json:"summary"`
	Limitations []string          `json:"limitations"`
	Sources     []SourceState     `json:"sources"`
}

type Assessment struct {
	State      KnowledgeState `json:"state"`
	Label      string         `json:"label"`
	Summary    string         `json:"summary"`
	Severity   Severity       `json:"severity"`
	Provenance Provenance     `json:"provenance"`
}

type Coverage struct {
	Observed *int   `json:"observed"`
	Total    *int   `json:"total"`
	Unit     string `json:"unit"`
}

type ObservationAssessment struct {
	State      KnowledgeState `json:"state"`
	Label      string         `json:"label"`
	Summary    string         `json:"summary"`
	Severity   Severity       `json:"severity"`
	Coverage   Coverage       `json:"coverage"`
	Provenance Provenance     `json:"provenance"`
}

type Assessments struct {
	ArchitectureHealth      Assessment            `json:"architecture_health"`
	ProjectionIntegrity     Assessment            `json:"projection_integrity"`
	ObservationCompleteness ObservationAssessment `json:"observation_completeness"`
}

// ActiveContextKind mirrors $defs/activeContext.kind.
type ActiveContextKind string

const (
	ContextTask        ActiveContextKind = "task"
	ContextPullRequest ActiveContextKind = "pull_request"
	ContextChange      ActiveContextKind = "change"
	ContextSession     ActiveContextKind = "session"
)

type ActiveContext struct {
	Kind        ActiveContextKind `json:"kind"`
	ID          string            `json:"id"`
	Label       string            `json:"label"`
	URL         *string           `json:"url,omitempty"`
	ElementRefs []string          `json:"element_refs,omitempty"`
}

type Briefing struct {
	ID          string     `json:"id"`
	Kind        string     `json:"kind"`
	Text        string     `json:"text"`
	Severity    Severity   `json:"severity"`
	ElementRefs []string   `json:"element_refs"`
	Provenance  Provenance `json:"provenance"`
}

type Fact struct {
	ID          string         `json:"id"`
	Label       string         `json:"label"`
	Value       any            `json:"value"`
	Unit        *string        `json:"unit,omitempty"`
	State       KnowledgeState `json:"state"`
	ElementRefs []string       `json:"element_refs,omitempty"`
}

type VisualAnchor struct {
	Order int     `json:"order"`
	Lane  *string `json:"lane,omitempty"`
	Group *string `json:"group,omitempty"`
}

type Region struct {
	ID             string         `json:"id"`
	Name           string         `json:"name"`
	Responsibility string         `json:"responsibility"`
	State          KnowledgeState `json:"state"`
	ComponentRefs  []string       `json:"component_refs"`
	VisualAnchor   VisualAnchor   `json:"visual_anchor"`
	Provenance     Provenance     `json:"provenance"`
}

type Component struct {
	ID             string         `json:"id"`
	Name           string         `json:"name"`
	RegionRef      string         `json:"region_ref"`
	Responsibility string         `json:"responsibility"`
	State          KnowledgeState `json:"state"`
	AuthorityRefs  []string       `json:"authority_refs"`
	VisualAnchor   VisualAnchor   `json:"visual_anchor"`
	Provenance     Provenance     `json:"provenance"`
}

type Boundary struct {
	ID         string         `json:"id"`
	Name       string         `json:"name"`
	Kind       string         `json:"kind"`
	MemberRefs []string       `json:"member_refs"`
	State      KnowledgeState `json:"state"`
	Summary    string         `json:"summary"`
	Provenance Provenance     `json:"provenance"`
}

type Contract struct {
	ID           string         `json:"id"`
	Name         string         `json:"name"`
	SourceRef    string         `json:"source_ref"`
	TargetRef    string         `json:"target_ref"`
	Kind         string         `json:"kind"`
	Direction    string         `json:"direction"`
	State        KnowledgeState `json:"state"`
	Summary      string         `json:"summary"`
	BoundaryRefs []string       `json:"boundary_refs,omitempty"`
	Provenance   Provenance     `json:"provenance"`
}

type FlowStep struct {
	Order       int     `json:"order"`
	ElementRef  string  `json:"element_ref"`
	ContractRef *string `json:"contract_ref"`
	Summary     *string `json:"summary,omitempty"`
}

type Flow struct {
	ID         string         `json:"id"`
	Name       string         `json:"name"`
	Kind       string         `json:"kind"`
	State      KnowledgeState `json:"state"`
	Steps      []FlowStep     `json:"steps"`
	Summary    string         `json:"summary"`
	Provenance Provenance     `json:"provenance"`
}

type AttentionItem struct {
	ID          string         `json:"id"`
	Kind        string         `json:"kind"`
	Title       string         `json:"title"`
	Summary     string         `json:"summary"`
	Severity    Severity       `json:"severity"`
	State       KnowledgeState `json:"state"`
	ElementRefs []string       `json:"element_refs"`
	Selectable  bool           `json:"selectable,omitempty"`
	Provenance  Provenance     `json:"provenance"`
}

type Change struct {
	ID          string     `json:"id"`
	Kind        string     `json:"kind"`
	Title       string     `json:"title"`
	Summary     string     `json:"summary"`
	Impact      string     `json:"impact"`
	ElementRefs []string   `json:"element_refs"`
	Provenance  Provenance `json:"provenance"`
}

type Evolution struct {
	Availability AvailabilityState `json:"availability"`
	BaseRevision *string           `json:"base_revision"`
	HeadRevision string            `json:"head_revision"`
	Summary      *string           `json:"summary,omitempty"`
	Limitations  []string          `json:"limitations,omitempty"`
	Changes      []Change          `json:"changes"`
}

type SourceLink struct {
	Label  string `json:"label"`
	Target string `json:"target"`
}

type FocusRecord struct {
	ElementRef       string         `json:"element_ref"`
	ElementKind      string         `json:"element_kind"`
	Name             string         `json:"name"`
	Responsibility   string         `json:"responsibility"`
	State            KnowledgeState `json:"state"`
	OwnerRefs        []string       `json:"owner_refs"`
	OwnedRefs        []string       `json:"owned_refs,omitempty"`
	ContractRefs     []string       `json:"contract_refs"`
	FlowRefs         []string       `json:"flow_refs"`
	AttentionRefs    []string       `json:"attention_refs"`
	DecisionRefs     []string       `json:"decision_refs"`
	RecentChangeRefs []string       `json:"recent_change_refs,omitempty"`
	SourceLinks      []SourceLink   `json:"source_links,omitempty"`
	Provenance       Provenance     `json:"provenance"`
}

// AgentHandoffCapability mirrors the adopted schema's capabilities.agent_handoff enum.
type AgentHandoffCapability string

const (
	HandoffNone   AgentHandoffCapability = "none"
	HandoffExport AgentHandoffCapability = "export"
	HandoffLive   AgentHandoffCapability = "live"
)

type Capabilities struct {
	LiveRefresh     bool                   `json:"live_refresh"`
	RevisionCompare bool                   `json:"revision_compare"`
	AgentHandoff    AgentHandoffCapability `json:"agent_handoff"`
	SourceDeepLinks bool                   `json:"source_deep_links"`
}

// --- sensei.dashboard.agent-handoff.v1 ---

type SelectedElement struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
}

type VisibleConcern struct {
	Route   string  `json:"route"`
	Summary *string `json:"summary"`
}

type ReferencedIDs struct {
	AttentionRefs []string `json:"attention_refs,omitempty"`
	ContractRefs  []string `json:"contract_refs,omitempty"`
	BoundaryRefs  []string `json:"boundary_refs,omitempty"`
	FlowRefs      []string `json:"flow_refs,omitempty"`
	EvidenceRefs  []string `json:"evidence_refs,omitempty"`
	DecisionRefs  []string `json:"decision_refs,omitempty"`
}

// HandoffCapability mirrors the agent-handoff schema's capability enum
// (distinct from the projection's transport-level AgentHandoffCapability).
type HandoffCapability string

const (
	HandoffReadOnly HandoffCapability = "read_only"
	HandoffPropose  HandoffCapability = "propose"
)

type HandoffEnvelope struct {
	SchemaVersion          string            `json:"schema_version"`
	Repository             Repository        `json:"repository"`
	Revision               Revision          `json:"revision"`
	GraphAuthority         GraphAuthority    `json:"graph_authority"`
	SelectedElement        *SelectedElement  `json:"selected_element"`
	Lens                   string            `json:"lens"`
	VisibleConcern         VisibleConcern    `json:"visible_concern"`
	ReferencedIDs          ReferencedIDs     `json:"referenced_ids"`
	ActiveContext          *ActiveContext    `json:"active_context"`
	RequestedIntent        string            `json:"requested_intent"`
	ObservationLimitations []string          `json:"observation_limitations"`
	Capability             HandoffCapability `json:"capability"`
}
