// SPDX-License-Identifier: AGPL-3.0-only

package maintenance

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/globulario/sensei/golang/architecture"
	"gopkg.in/yaml.v3"
)

const (
	EvidenceStatusPass    = "pass"
	EvidenceStatusFail    = "fail"
	EvidenceStatusWarning = "warning"
	EvidenceStatusStale   = "stale"
	EvidenceStatusUnknown = "unknown"

	EvidenceFreshnessCurrent    = "current"
	EvidenceFreshnessStale      = "stale"
	EvidenceFreshnessUnknown    = "unknown"
	EvidenceFreshnessHistorical = "historical"
)

type EvidenceStateDocument struct {
	SchemaVersion string                            `json:"schema_version" yaml:"schema_version"`
	GeneratedBy   string                            `json:"generated_by" yaml:"generated_by"`
	Binding       architecture.ClaimDocumentBinding `json:"binding" yaml:"binding"`
	Evidence      []EvidenceState                   `json:"evidence" yaml:"evidence"`
}

type EvidenceState struct {
	ID         string `json:"id" yaml:"id"`
	Status     string `json:"status" yaml:"status"`
	Freshness  string `json:"freshness" yaml:"freshness"`
	ObservedAt string `json:"observed_at,omitempty" yaml:"observed_at,omitempty"`
	Source     string `json:"source,omitempty" yaml:"source,omitempty"`
}

type evidenceStateEnvelope struct {
	ArchitectureEvidenceState EvidenceStateDocument `json:"architecture_evidence_state" yaml:"architecture_evidence_state"`
}

func LoadEvidenceStateDocument(path string) (EvidenceStateDocument, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return EvidenceStateDocument{}, err
	}
	return UnmarshalEvidenceStateDocumentYAML(data)
}

func UnmarshalEvidenceStateDocumentYAML(data []byte) (EvidenceStateDocument, error) {
	var env evidenceStateEnvelope
	if err := yaml.Unmarshal(data, &env); err != nil {
		return EvidenceStateDocument{}, err
	}
	doc := normalizeEvidenceStateDocument(env.ArchitectureEvidenceState)
	if err := ValidateEvidenceStateDocument(doc, nil); err != nil {
		return EvidenceStateDocument{}, err
	}
	return doc, nil
}

func MarshalCanonicalEvidenceStateYAML(doc EvidenceStateDocument) ([]byte, error) {
	doc = normalizeEvidenceStateDocument(doc)
	if err := ValidateEvidenceStateDocument(doc, nil); err != nil {
		return nil, err
	}
	return yaml.Marshal(evidenceStateEnvelope{ArchitectureEvidenceState: doc})
}

func ValidateEvidenceStateDocument(doc EvidenceStateDocument, expected *architecture.ClaimDocumentBinding) error {
	var errs []string
	if doc.Binding.RevisionStatus == "" {
		errs = append(errs, "binding revision_status is required")
	}
	if doc.Binding.GraphDigestStatus == "" {
		errs = append(errs, "binding graph_digest_status is required")
	}
	if doc.Binding.RevisionStatus != "" && !oneOf(doc.Binding.RevisionStatus, architecture.RevisionResolved, architecture.RevisionUnavailable, architecture.RevisionNotGit, architecture.RevisionNotRequested) {
		errs = append(errs, "binding revision_status is invalid")
	}
	if doc.Binding.GraphDigestStatus != "" && !oneOf(doc.Binding.GraphDigestStatus, architecture.GraphDigestResolved, architecture.GraphDigestUnavailable, architecture.GraphDigestNotRequested) {
		errs = append(errs, "binding graph_digest_status is invalid")
	}
	if expected != nil {
		if expected.RepositoryDomain != "" && doc.Binding.RepositoryDomain != "" && expected.RepositoryDomain != doc.Binding.RepositoryDomain {
			errs = append(errs, "evidence binding repository does not match observed binding")
		}
		if expected.RevisionStatus == architecture.RevisionResolved && doc.Binding.RevisionStatus == architecture.RevisionResolved && expected.Revision != doc.Binding.Revision {
			errs = append(errs, "evidence binding revision does not match observed binding")
		}
		if expected.GraphDigestStatus == architecture.GraphDigestResolved && doc.Binding.GraphDigestStatus == architecture.GraphDigestResolved && expected.GraphDigestSHA256 != doc.Binding.GraphDigestSHA256 {
			errs = append(errs, "evidence binding graph digest does not match observed binding")
		}
	}
	seen := map[string]bool{}
	for _, ev := range doc.Evidence {
		if ev.ID == "" {
			errs = append(errs, "evidence id is required")
		}
		if seen[ev.ID] {
			errs = append(errs, "duplicate evidence id "+ev.ID)
		}
		seen[ev.ID] = true
		if !oneOf(ev.Status, EvidenceStatusPass, EvidenceStatusFail, EvidenceStatusWarning, EvidenceStatusStale, EvidenceStatusUnknown) {
			errs = append(errs, "unknown evidence status for "+ev.ID)
		}
		if !oneOf(ev.Freshness, EvidenceFreshnessCurrent, EvidenceFreshnessStale, EvidenceFreshnessUnknown, EvidenceFreshnessHistorical) {
			errs = append(errs, "unknown evidence freshness for "+ev.ID)
		}
		if ev.ObservedAt != "" {
			if _, err := time.Parse(time.RFC3339, ev.ObservedAt); err != nil {
				errs = append(errs, "observed_at must be RFC3339 for "+ev.ID)
			}
		}
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func EvidenceIsActive(ev EvidenceState, binding, observed architecture.ClaimDocumentBinding) bool {
	return ev.Status == EvidenceStatusPass &&
		ev.Freshness == EvidenceFreshnessCurrent &&
		bindingsMatchResolved(binding, observed)
}

func (d EvidenceStateDocument) ByID() map[string]EvidenceState {
	out := map[string]EvidenceState{}
	for _, ev := range d.Evidence {
		out[ev.ID] = ev
		out["evidence:"+ev.ID] = ev
	}
	return out
}

func normalizeEvidenceStateDocument(in EvidenceStateDocument) EvidenceStateDocument {
	doc := in
	doc.SchemaVersion = strings.TrimSpace(doc.SchemaVersion)
	doc.GeneratedBy = strings.TrimSpace(doc.GeneratedBy)
	doc.Binding.RepositoryDomain = strings.TrimSpace(doc.Binding.RepositoryDomain)
	doc.Binding.Revision = strings.TrimSpace(doc.Binding.Revision)
	doc.Binding.RevisionStatus = strings.TrimSpace(doc.Binding.RevisionStatus)
	doc.Binding.GraphDigestSHA256 = strings.TrimSpace(doc.Binding.GraphDigestSHA256)
	doc.Binding.GraphDigestStatus = strings.TrimSpace(doc.Binding.GraphDigestStatus)
	for i := range doc.Evidence {
		doc.Evidence[i].ID = strings.TrimSpace(strings.TrimPrefix(doc.Evidence[i].ID, "evidence:"))
		doc.Evidence[i].Status = strings.TrimSpace(doc.Evidence[i].Status)
		doc.Evidence[i].Freshness = strings.TrimSpace(doc.Evidence[i].Freshness)
		doc.Evidence[i].ObservedAt = strings.TrimSpace(doc.Evidence[i].ObservedAt)
		doc.Evidence[i].Source = strings.TrimSpace(doc.Evidence[i].Source)
	}
	sort.SliceStable(doc.Evidence, func(i, j int) bool { return doc.Evidence[i].ID < doc.Evidence[j].ID })
	return doc
}

func oneOf(v string, allowed ...string) bool {
	for _, a := range allowed {
		if v == a {
			return true
		}
	}
	return false
}

func bindingsMatchResolved(a, b architecture.ClaimDocumentBinding) bool {
	if a.RepositoryDomain != "" && b.RepositoryDomain != "" && a.RepositoryDomain != b.RepositoryDomain {
		return false
	}
	if a.RevisionStatus == architecture.RevisionResolved && b.RevisionStatus == architecture.RevisionResolved && a.Revision != b.Revision {
		return false
	}
	if a.GraphDigestStatus == architecture.GraphDigestResolved && b.GraphDigestStatus == architecture.GraphDigestResolved && a.GraphDigestSHA256 != b.GraphDigestSHA256 {
		return false
	}
	return true
}

func evidenceReason(prefix string, ref string, ev *EvidenceState) Reason {
	if ev == nil {
		return Reason{Code: prefix + ".unknown", Detail: fmt.Sprintf("%s missing from evidence-state snapshot", ref)}
	}
	switch {
	case ev.Status == EvidenceStatusPass && ev.Freshness == EvidenceFreshnessCurrent:
		return Reason{Code: prefix + ".current", Detail: ref + " is current and active"}
	case ev.Freshness == EvidenceFreshnessStale || ev.Status == EvidenceStatusStale:
		return Reason{Code: prefix + ".stale", Detail: ref + " is stale"}
	case ev.Status == EvidenceStatusUnknown || ev.Freshness == EvidenceFreshnessUnknown:
		return Reason{Code: prefix + ".unknown", Detail: ref + " is unknown"}
	default:
		return Reason{Code: prefix + ".inactive", Detail: ref + " is inactive"}
	}
}
