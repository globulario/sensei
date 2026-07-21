// SPDX-License-Identifier: AGPL-3.0-only

package controlstate

import "fmt"

// ArtifactClosure is the CLOSED artifact-closure vocabulary. Zero value fails closed.
type ArtifactClosure string

const (
	ClosureClosed        ArtifactClosure = "closed"
	ClosureOpen          ArtifactClosure = "open"
	ClosureDegraded      ArtifactClosure = "degraded"
	ClosureUnknown       ArtifactClosure = "unknown"
	ClosureNotApplicable ArtifactClosure = "not_applicable"
)

func validClosure(c ArtifactClosure) bool {
	switch c {
	case ClosureClosed, ClosureOpen, ClosureDegraded, ClosureUnknown, ClosureNotApplicable:
		return true
	}
	return false
}

// DimensionState is the CLOSED per-dimension state vocabulary.
type DimensionState string

const (
	DimSatisfied     DimensionState = "satisfied"
	DimOpen          DimensionState = "open"
	DimDegraded      DimensionState = "degraded"
	DimUnknown       DimensionState = "unknown"
	DimNotApplicable DimensionState = "not_applicable"
)

func validDimState(s DimensionState) bool {
	switch s {
	case DimSatisfied, DimOpen, DimDegraded, DimUnknown, DimNotApplicable:
		return true
	}
	return false
}

// DimensionAssessment is one owner-projected per-dimension verdict for an artifact.
type DimensionAssessment struct {
	Dimension  string         `json:"dimension" yaml:"dimension"`
	Label      string         `json:"label" yaml:"label"`
	Applicable bool           `json:"applicable" yaml:"applicable"`
	Required   bool           `json:"required" yaml:"required"`
	State      DimensionState `json:"state" yaml:"state"`
	ReasonCode string         `json:"reason_code,omitempty" yaml:"reason_code,omitempty"`
	Blockers   []string       `json:"blockers,omitempty" yaml:"blockers,omitempty"`
	Evidence   []string       `json:"evidence,omitempty" yaml:"evidence,omitempty"`
	Questions  []string       `json:"questions,omitempty" yaml:"questions,omitempty"`
	Owner      string         `json:"owner" yaml:"owner"`
	NextAction string         `json:"next_action_owner,omitempty" yaml:"next_action_owner,omitempty"`
}

// LifecycleState is the CLOSED lifecycle vocabulary.
type LifecycleState string

const (
	LifecycleActive        LifecycleState = "active"
	LifecycleProposed      LifecycleState = "proposed"
	LifecycleDeprecated    LifecycleState = "deprecated"
	LifecycleSuperseded    LifecycleState = "superseded"
	LifecycleRevoked       LifecycleState = "revoked"
	LifecycleUnknown       LifecycleState = "unknown"
	LifecycleNotApplicable LifecycleState = "not_applicable"
)

func validLifecycleState(s LifecycleState) bool {
	switch s {
	case LifecycleActive, LifecycleProposed, LifecycleDeprecated, LifecycleSuperseded,
		LifecycleRevoked, LifecycleUnknown, LifecycleNotApplicable:
		return true
	}
	return false
}

// LifecycleAssessment is typed independently of closure. Absence never synthesizes active.
type LifecycleAssessment struct {
	Applicable         bool               `json:"applicable" yaml:"applicable"`
	Vocabulary         string             `json:"vocabulary,omitempty" yaml:"vocabulary,omitempty"`
	State              LifecycleState     `json:"state" yaml:"state"`
	SourceOwner        string             `json:"source_owner,omitempty" yaml:"source_owner,omitempty"`
	SourceIdentity     string             `json:"source_identity,omitempty" yaml:"source_identity,omitempty"`
	SourceAvailability SourceAvailability `json:"source_availability" yaml:"source_availability"`
	ReasonCode         string             `json:"reason_code,omitempty" yaml:"reason_code,omitempty"`
}

// AttentionSeverity is the CLOSED severity vocabulary, low→high.
type AttentionSeverity string

const (
	SeverityInformational AttentionSeverity = "informational"
	SeverityAttention     AttentionSeverity = "attention"
	SeverityWarning       AttentionSeverity = "warning"
	SeverityCritical      AttentionSeverity = "critical"
)

func validSeverity(s AttentionSeverity) bool {
	switch s {
	case SeverityInformational, SeverityAttention, SeverityWarning, SeverityCritical:
		return true
	}
	return false
}

// severityRank orders severities high→low for sorting (0 = highest).
func severityRank(s AttentionSeverity) int {
	switch s {
	case SeverityCritical:
		return 0
	case SeverityWarning:
		return 1
	case SeverityAttention:
		return 2
	case SeverityInformational:
		return 3
	}
	return 4
}

// AttentionItem is the shared canonical attention record (architecture.attention_item/v1). Its
// identity is the deterministic digest of its canonical fields (no prose/order/time/style).
type AttentionItem struct {
	ID             string            `json:"id" yaml:"id"`
	SourceOwner    string            `json:"source_owner" yaml:"source_owner"`
	SourceSchema   string            `json:"source_schema" yaml:"source_schema"`
	SourceIdentity string            `json:"source_identity" yaml:"source_identity"`
	AttentionClass string            `json:"attention_class" yaml:"attention_class"`
	ReasonCode     string            `json:"reason_code" yaml:"reason_code"`
	Severity       AttentionSeverity `json:"severity" yaml:"severity"`
	SeverityBasis  string            `json:"severity_basis" yaml:"severity_basis"`
	SourceDigest   string            `json:"source_digest,omitempty" yaml:"source_digest,omitempty"`
	Affected       []string          `json:"affected_artifacts,omitempty" yaml:"affected_artifacts,omitempty"`
	Blocking       bool              `json:"blocking" yaml:"blocking"`
	Evidence       []string          `json:"evidence,omitempty" yaml:"evidence,omitempty"`
	NextAction     string            `json:"next_action_owner,omitempty" yaml:"next_action_owner,omitempty"`
	ArchitectInput bool              `json:"architect_input_required" yaml:"architect_input_required"`
}

// attentionIdentity is the deterministic digest of the canonical identity fields.
func (a AttentionItem) attentionIdentity() (string, error) {
	key := struct {
		Owner, Schema, Identity, Class, Reason string
		Severity                               AttentionSeverity
		Affected                               []string
		Blocking                               bool
	}{a.SourceOwner, a.SourceSchema, a.SourceIdentity, a.AttentionClass, a.ReasonCode, a.Severity, a.Affected, a.Blocking}
	return digestOf(key)
}

func validateAttentionItem(a AttentionItem) error {
	if a.SourceOwner == "" || a.SourceSchema == "" || a.SourceIdentity == "" {
		return fmt.Errorf("attention item missing source identity")
	}
	if a.AttentionClass == "" || a.ReasonCode == "" {
		return fmt.Errorf("attention item missing class/reason")
	}
	if !validSeverity(a.Severity) {
		return fmt.Errorf("attention severity %q is off-vocabulary", a.Severity)
	}
	if a.SeverityBasis == "" {
		return fmt.Errorf("attention item missing severity basis")
	}
	want, err := a.attentionIdentity()
	if err != nil {
		return err
	}
	if a.ID != want {
		return fmt.Errorf("attention item id does not match its canonical identity")
	}
	return nil
}
