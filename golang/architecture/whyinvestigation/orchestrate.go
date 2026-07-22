// SPDX-License-Identifier: AGPL-3.0-only

package whyinvestigation

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/investigation"
)

type Plan struct {
	ID                   string   `json:"id" yaml:"id"`
	Description          string   `json:"description,omitempty" yaml:"description,omitempty"`
	RequestedProviderIDs []string `json:"requested_provider_ids" yaml:"requested_provider_ids"`
}

func Orchestrate(ctx context.Context, root string, req CaptureRequest, plan Plan) (investigation.Document, error) {
	// 1. Validate inputs
	if err := validateRequest(req); err != nil {
		return investigation.Document{}, err
	}

	// 2. Canonicalize provider execution order (alphabetical by ID)
	sortedProviderIDs := append([]string{}, plan.RequestedProviderIDs...)
	sort.Strings(sortedProviderIDs)

	// 3. Captures immutable snapshots before querying providers
	snapshots := make(map[string]Snapshot)
	providers := make(map[string]Provider)
	captureErrors := make(map[string]error)

	for _, providerID := range sortedProviderIDs {
		p, exists := GetProvider(providerID, root)
		if !exists {
			continue
		}
		providers[providerID] = p
		snap, err := p.Capture(ctx, req)
		if err != nil {
			captureErrors[providerID] = err
		} else {
			snapshots[providerID] = snap
		}
	}

	// 4. Run providers and collect results
	var allEvidence []investigation.EvidenceReceipt
	var allCoverage []investigation.CoverageEntry
	var allLimitations []architecture.Limitation
	var resolvedProviders []investigation.ProviderBinding

	for _, providerID := range sortedProviderIDs {
		if p, exists := providers[providerID]; exists {
			resolvedProviders = append(resolvedProviders, p.Identity())
		}
	}

	type ProviderOutcome struct {
		ProviderID     string `json:"provider_id" yaml:"provider_id"`
		Status         string `json:"status" yaml:"status"`
		SnapshotDigest string `json:"snapshot_digest,omitempty" yaml:"snapshot_digest,omitempty"`
		Reason         string `json:"reason,omitempty" yaml:"reason,omitempty"`
	}
	var outcomes []ProviderOutcome

	for _, providerID := range sortedProviderIDs {
		_, exists := providers[providerID]
		if !exists {
			outcomes = append(outcomes, ProviderOutcome{
				ProviderID: providerID,
				Status:     string(investigation.CoverageNotConfigured),
				Reason:     fmt.Sprintf("provider %q is not registered in the registry", providerID),
			})
			continue
		}

		if captureErr, failed := captureErrors[providerID]; failed {
			outcomes = append(outcomes, ProviderOutcome{
				ProviderID: providerID,
				Status:     string(investigation.CoverageUnavailable),
				Reason:     fmt.Sprintf("snapshot capture failed for provider %q: %v", providerID, captureErr),
			})
			continue
		}

		snap, captured := snapshots[providerID]
		if !captured {
			outcomes = append(outcomes, ProviderOutcome{
				ProviderID: providerID,
				Status:     string(investigation.CoverageUnavailable),
				Reason:     fmt.Sprintf("snapshot capture failed for provider %q", providerID),
			})
			continue
		}

		outcomes = append(outcomes, ProviderOutcome{
			ProviderID:     providerID,
			Status:         "captured",
			SnapshotDigest: snap.Digest,
		})
	}

	outcomeBytes, err := json.Marshal(outcomes)
	if err != nil {
		return investigation.Document{}, err
	}
	compositeSnapshotDigest := investigation.SHA256Bytes(outcomeBytes)

	for _, providerID := range sortedProviderIDs {
		p, exists := providers[providerID]
		if !exists {
			// Emit a CoverageNotConfigured entry
			queryDigest, err := digestQuery(req.Query)
			if err != nil {
				return investigation.Document{}, err
			}
			target, err := digestTarget(req, queryDigest)
			if err != nil {
				return investigation.Document{}, err
			}
			allCoverage = append(allCoverage, investigation.CoverageEntry{
				ProviderID:         providerID,
				ProviderVersion:    "",
				Category:           "",
				TargetDigestSHA256: target,
				Status:             investigation.CoverageNotConfigured,
				Reason:             fmt.Sprintf("provider %q is not registered in the registry", providerID),
			})
			continue
		}

		snap, captured := snapshots[providerID]
		if !captured {
			// The provider was registered but capture failed/unavailable
			queryDigest, err := digestQuery(req.Query)
			if err != nil {
				return investigation.Document{}, err
			}
			target, err := digestTarget(req, queryDigest)
			if err != nil {
				return investigation.Document{}, err
			}
			reason := fmt.Sprintf("snapshot capture failed for provider %q", providerID)
			if captureErr, failed := captureErrors[providerID]; failed {
				reason = fmt.Sprintf("snapshot capture failed for provider %q: %v", providerID, captureErr)
			}
			allCoverage = append(allCoverage, investigation.CoverageEntry{
				ProviderID:         providerID,
				ProviderVersion:    p.Identity().Version,
				Category:           categoryForProvider(providerID),
				TargetDigestSHA256: target,
				Status:             investigation.CoverageUnavailable,
				Reason:             reason,
			})
			allLimitations = append(allLimitations, architecture.Limitation{
				Source: providerID,
				Scope:  "capture",
				Reason: reason,
			})
			continue
		}

		// Investigate
		res, err := p.Investigate(ctx, snap, req)
		if err != nil {
			// Handle investigation failure gracefully as unavailable/invalid
			queryDigest, errD := digestQuery(req.Query)
			if errD != nil {
				return investigation.Document{}, errD
			}
			target, errT := digestTarget(req, queryDigest)
			if errT != nil {
				return investigation.Document{}, errT
			}
			allCoverage = append(allCoverage, investigation.CoverageEntry{
				ProviderID:         providerID,
				ProviderVersion:    p.Identity().Version,
				Category:           categoryForProvider(providerID),
				TargetDigestSHA256: target,
				Status:             investigation.CoverageInvalid,
				Reason:             fmt.Sprintf("investigation failed: %v", err),
			})
			allLimitations = append(allLimitations, architecture.Limitation{
				Source: providerID,
				Scope:  "investigation",
				Reason: err.Error(),
			})
			continue
		}

		allEvidence = append(allEvidence, res.RawEvidence...)
		allCoverage = append(allCoverage, res.Coverage)
		allLimitations = append(allLimitations, res.Limitations...)
	}

	// 5. Compose the normalized WHY document
	return composeDocumentMulti(req, plan, compositeSnapshotDigest, allEvidence, allCoverage, allLimitations, resolvedProviders)
}

func categoryForProvider(id string) investigation.EvidenceCategory {
	switch id {
	case GitProviderID:
		return investigation.EvidenceSourceControl
	case "documentation_provider":
		return investigation.EvidenceDocumentation
	case "tests_provider":
		return investigation.EvidenceTests
	case "awareness_provider":
		return investigation.EvidenceDesignDocuments
	case "scars_provider":
		return investigation.EvidenceErrorTracking
	case "architect_answers_provider":
		return investigation.EvidenceArchitectFeedback
	case "imported_evidence_provider":
		return investigation.EvidenceRuntime
	default:
		return investigation.EvidenceSourceCode
	}
}
