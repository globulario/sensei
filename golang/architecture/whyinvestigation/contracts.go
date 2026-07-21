// SPDX-License-Identifier: AGPL-3.0-only

// Package whyinvestigation captures deterministic, evidence-only historical
// context. It never creates claims, explanations, or architectural authority.
package whyinvestigation

import (
	"context"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/investigation"
)

const (
	GitProviderID      = "git_history_provider"
	GitProviderVersion = "1.0"
)

type GitRange struct{ Start, End string }

type Query struct {
	ID                   string
	TargetObservationIDs []string
	TargetEvidenceIDs    []string
}

type CaptureRequest struct {
	Repository architecture.ClaimDocumentBinding
	How        investigation.Document
	Query      Query
	Range      GitRange
	CapturedAt string
}

type Snapshot struct {
	Provider       investigation.ProviderBinding
	Digest         string
	RequestedRange GitRange
	ResolvedRange  GitRange
	Incomplete     bool
	Commits        []Commit
}

// Commit is raw historical evidence, not an interpretation of the change.
type Commit struct {
	ID, Parents, AuthorTime, CommitterTime, Message, ChangedPaths, PatchDigest string
}

type Result struct {
	RawEvidence []investigation.EvidenceReceipt
	Coverage    investigation.CoverageEntry
	Limitations []architecture.Limitation
}

type Provider interface {
	Identity() investigation.ProviderBinding
	Capture(context.Context, CaptureRequest) (Snapshot, error)
	Investigate(context.Context, Snapshot, CaptureRequest) (Result, error)
}
