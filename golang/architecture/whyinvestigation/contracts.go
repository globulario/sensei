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

type SnapshotEntry struct {
	SourceIdentity      string `json:"source_identity" yaml:"source_identity"`
	Path                string `json:"path" yaml:"path"`
	Content             []byte `json:"content" yaml:"content"`
	ContentDigestSHA256 string `json:"content_digest_sha256" yaml:"content_digest_sha256"`
}

type Snapshot struct {
	Provider       investigation.ProviderBinding  `json:"provider" yaml:"provider"`
	Category       investigation.EvidenceCategory `json:"category" yaml:"category"`
	Digest         string                         `json:"digest" yaml:"digest"`
	Entries        []SnapshotEntry                `json:"entries,omitempty" yaml:"entries,omitempty"`
	RequestedRange GitRange                       `json:"requested_range,omitempty" yaml:"requested_range,omitempty"`
	ResolvedRange  GitRange                       `json:"resolved_range,omitempty" yaml:"resolved_range,omitempty"`
	Incomplete     bool                           `json:"incomplete,omitempty" yaml:"incomplete,omitempty"`
	Commits        []Commit                       `json:"commits,omitempty" yaml:"commits,omitempty"`
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
