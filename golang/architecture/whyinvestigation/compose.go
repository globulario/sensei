// SPDX-License-Identifier: AGPL-3.0-only

package whyinvestigation

import (
	"encoding/json"
	"sort"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/investigation"
)

func composeDocumentMulti(
	req CaptureRequest,
	plan Plan,
	compositeSnapshotDigest string,
	evidence []investigation.EvidenceReceipt,
	coverage []investigation.CoverageEntry,
	limitations []architecture.Limitation,
	resolvedProviders []investigation.ProviderBinding,
) (investigation.Document, error) {
	query, err := digestQuery(req.Query)
	if err != nil {
		return investigation.Document{}, err
	}

	invPlan := investigation.Plan{
		ID:          plan.ID,
		Description: plan.Description,
		Queries:     []string{query},
	}

	// 5. The plan digest includes the canonical requested-provider set.
	canonicalRequestedProviders := append([]string{}, plan.RequestedProviderIDs...)
	sort.Strings(canonicalRequestedProviders)
	planDescriptor := struct {
		Plan               investigation.Plan `json:"plan"`
		RequestedProviders []string           `json:"requested_providers"`
	}{
		Plan:               invPlan,
		RequestedProviders: canonicalRequestedProviders,
	}
	planData, err := json.Marshal(planDescriptor)
	if err != nil {
		return investigation.Document{}, err
	}

	// 6. The extractor-profile digest includes orchestrator version plus exact resolved provider identities and versions.
	profileDescriptor := struct {
		OrchestratorVersion string                          `json:"orchestrator_version"`
		ResolvedProviders   []investigation.ProviderBinding `json:"resolved_providers"`
	}{
		OrchestratorVersion: "1.0",
		ResolvedProviders:   resolvedProviders,
	}
	profileBytes, err := json.Marshal(profileDescriptor)
	if err != nil {
		return investigation.Document{}, err
	}
	profile := investigation.SHA256Bytes(profileBytes)

	doc := investigation.Document{
		SchemaVersion: "investigation.schema.v1",
		GeneratedBy:   "sensei.whyinvestigation.orchestrator",
		Mode:          investigation.ModeWhy,
		Binding: investigation.Binding{
			Repository:                    req.Repository,
			EvidenceSnapshotDigestSHA256:  compositeSnapshotDigest,
			InvestigationPlanDigestSHA256: investigation.SHA256Bytes(planData),
			ExtractorProfileDigestSHA256:  profile,
			Model: investigation.ModelBinding{
				Status: investigation.ModelStatusDisabled,
			},
			Why: investigation.WhyBinding{
				HowDocumentDigestSHA256:   req.How.Receipt.OutputDocumentDigestSHA256,
				QueryDigestSHA256:         query,
				TargetObservationIDs:      req.Query.TargetObservationIDs,
				TargetEvidenceIDs:         req.Query.TargetEvidenceIDs,
				HistoryRangeStart:         req.Range.Start,
				HistoryRangeEnd:           req.Range.End,
				ResolvedHistoryRangeStart: req.Range.Start,
				ResolvedHistoryRangeEnd:   req.Range.End,
			},
		},
		Plan:        invPlan,
		Coverage:    coverage,
		RawEvidence: evidence,
		Limitations: limitations,
		Receipt: investigation.RunReceipt{
			SchemaVersion:                "investigation.schema.v1",
			GeneratedBy:                  "sensei.whyinvestigation.orchestrator",
			Repository:                   req.Repository,
			GraphDigestSHA256:            req.Repository.GraphDigestSHA256,
			PlanDigestSHA256:             investigation.SHA256Bytes(planData),
			ExtractorProfileDigestSHA256: profile,
			EvidenceSnapshotDigestSHA256: compositeSnapshotDigest,
			Model: investigation.ModelBinding{
				Status: investigation.ModelStatusDisabled,
			},
			PostProcessingVersion:     "why.orchestrator.v1",
			TimestampSource:           req.CapturedAt,
			ResourceLimits:            map[string]string{"orchestrator": "local_only"},
			NondeterminismDeclaration: "deterministic_only",
		},
	}

	norm, err := investigation.Normalize(doc)
	if err != nil {
		return investigation.Document{}, err
	}
	digest, err := investigation.CalculateDocumentDigest(norm)
	if err != nil {
		return investigation.Document{}, err
	}
	norm.Receipt.OutputDocumentDigestSHA256 = digest
	if err := investigation.Validate(norm); err != nil {
		return investigation.Document{}, err
	}
	return norm, nil
}
