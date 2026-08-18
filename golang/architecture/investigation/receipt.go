// SPDX-License-Identifier: AGPL-3.0-only

package investigation

import (
	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/extractbudget"
)

type RunReceipt struct {
	SchemaVersion                string                            `json:"schema_version" yaml:"schema_version"`
	GeneratedBy                  string                            `json:"generated_by" yaml:"generated_by"`
	Repository                   architecture.ClaimDocumentBinding `json:"repository" yaml:"repository"`
	GraphDigestSHA256            string                            `json:"graph_digest_sha256,omitempty" yaml:"graph_digest_sha256,omitempty"`
	PlanDigestSHA256             string                            `json:"plan_digest_sha256" yaml:"plan_digest_sha256"`
	ExtractorProfileDigestSHA256 string                            `json:"extractor_profile_digest_sha256" yaml:"extractor_profile_digest_sha256"`
	EvidenceSnapshotDigestSHA256 string                            `json:"evidence_snapshot_digest_sha256,omitempty" yaml:"evidence_snapshot_digest_sha256,omitempty"`
	Model                        ModelBinding                      `json:"model" yaml:"model"`
	ModelArtifactDigestSHA256    string                            `json:"model_artifact_digest_sha256,omitempty" yaml:"model_artifact_digest_sha256,omitempty"`
	PostProcessingVersion        string                            `json:"post_processing_version" yaml:"post_processing_version"`
	OutputDocumentDigestSHA256   string                            `json:"output_document_digest_sha256" yaml:"output_document_digest_sha256"`
	OutputCandidateIDsAndDigests map[string]string                 `json:"output_candidate_ids_and_digests,omitempty" yaml:"output_candidate_ids_and_digests,omitempty"`
	TimestampSource              string                            `json:"timestamp_source" yaml:"timestamp_source"`
	ResourceLimits               map[string]string                 `json:"resource_limits,omitempty" yaml:"resource_limits,omitempty"`
	// ResourceBudget is the ENFORCED half of the two resource fields, and the
	// distinction is the whole point. ResourceLimits above is caller-supplied
	// strings that nothing reads; this is the contract the extractor actually
	// bound itself to, the consumption it measured, and the disposition that
	// follows.
	//
	// An extractor that produces a document should always fill it in. An
	// all-zero Budget inside it is the honest way to say "no limit was bound",
	// and it keeps the disposition (completed / partial / budget_exhausted /
	// unavailable / cancelled) available on every run rather than only on
	// budgeted ones. The pointer remains optional so documents written before
	// this field existed still parse.
	ResourceBudget            *extractbudget.Receipt `json:"resource_budget,omitempty" yaml:"resource_budget,omitempty"`
	NondeterminismDeclaration string                 `json:"nondeterminism_declaration,omitempty" yaml:"nondeterminism_declaration,omitempty"`
}
