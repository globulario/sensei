// SPDX-License-Identifier: AGPL-3.0-only

package investigationsurface

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/howextract"
	"github.com/globulario/sensei/golang/architecture/investigation"
	"github.com/globulario/sensei/golang/architecture/investigator"
	"github.com/globulario/sensei/golang/architecture/whyinvestigation"
)

type HowRequest struct {
	Root           string
	CapturedAt     string
	Repository     architecture.ClaimDocumentBinding
	ResourceLimits map[string]string
}

type WhyRequest struct {
	Root           string
	CapturedAt     string
	How            investigation.Document
	QueryID        string
	ObservationIDs []string
	EvidenceIDs    []string
	HistoryStart   string
	HistoryEnd     string
	ProviderIDs    []string
}

type ArchitectureRequest struct {
	How       investigation.Document
	Why       investigation.Document
	Grounding investigator.GroundingSnapshot
	Digests   investigator.InputDigests
	Options   investigator.ComposeOptions
}

func RunHow(request HowRequest) (investigation.Document, error) {
	if strings.TrimSpace(request.Root) == "" {
		return investigation.Document{}, errors.New("repository root is required")
	}
	if len(request.ResourceLimits) == 0 {
		request.ResourceLimits = map[string]string{"surface": "bounded"}
	}
	return howextract.Extract(request.Root, howextract.Options{CapturedAt: request.CapturedAt, Repository: request.Repository, ResourceLimits: request.ResourceLimits})
}

func RunWhy(ctx context.Context, request WhyRequest) (investigation.Document, error) {
	if err := investigation.Validate(request.How); err != nil {
		return investigation.Document{}, fmt.Errorf("HOW artifact is invalid: %w", err)
	}
	if request.How.Mode != investigation.ModeHow {
		return investigation.Document{}, errors.New("HOW artifact mode is not how")
	}
	if strings.TrimSpace(request.QueryID) == "" {
		return investigation.Document{}, errors.New("query id is required")
	}
	if len(request.ObservationIDs) == 0 && len(request.EvidenceIDs) == 0 {
		return investigation.Document{}, errors.New("at least one observation or evidence target is required")
	}
	if strings.TrimSpace(request.HistoryStart) == "" || strings.TrimSpace(request.HistoryEnd) == "" {
		return investigation.Document{}, errors.New("explicit history start and end are required")
	}
	if len(request.ProviderIDs) == 0 {
		return investigation.Document{}, errors.New("at least one provider is required")
	}
	return whyinvestigation.Orchestrate(ctx, request.Root, whyinvestigation.CaptureRequest{Repository: request.How.Binding.Repository, How: request.How, Query: whyinvestigation.Query{ID: request.QueryID, TargetObservationIDs: request.ObservationIDs, TargetEvidenceIDs: request.EvidenceIDs}, Range: whyinvestigation.GitRange{Start: request.HistoryStart, End: request.HistoryEnd}, CapturedAt: request.CapturedAt}, whyinvestigation.Plan{ID: "plan.why.surface.v1", Description: "Phase 10.7 caller-bounded WHY investigation", RequestedProviderIDs: request.ProviderIDs})
}

func RunArchitecture(request ArchitectureRequest) (investigator.Result, error) {
	canonical := GroundingFromDocuments(request.How, request.Why)
	want, err := investigator.GroundingSnapshotDigest(canonical)
	if err != nil {
		return investigator.Result{}, err
	}
	got, err := investigator.GroundingSnapshotDigest(request.Grounding)
	if err != nil {
		return investigator.Result{}, err
	}
	if got != want {
		return investigator.Result{}, errors.New("Phase 10.7 requires grounding to exactly match the canonical HOW and WHY grounding snapshot")
	}
	return investigator.Compose(investigator.ComposeInput{How: request.How, Why: request.Why, Grounding: request.Grounding, Digests: request.Digests}, request.Options)
}
