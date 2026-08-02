// SPDX-License-Identifier: AGPL-3.0-only

// Package agentcommand adapts a bounded external coding-agent command to the
// existing O2/O3 generation contracts. The command proposes one closed
// mutation plan. Go validates and applies that plan only through O3's typed
// CandidateWorkspace, then asks O3 for canonical candidate evidence.
package agentcommand

import (
	"context"

	"github.com/globulario/sensei/golang/architecture/providerport"
	"github.com/globulario/sensei/golang/architecture/runnercomposition"
	"github.com/globulario/sensei/golang/architecture/synthesis"
)

const MutationPlanSchemaVersion = "sensei.agentcommand.mutationplan.v1"

// MutationKind is the complete operation vocabulary accepted from an agent.
type MutationKind string

const (
	MutationWrite   MutationKind = "write"
	MutationDelete  MutationKind = "delete"
	MutationRename  MutationKind = "rename"
	MutationSetMode MutationKind = "set-mode"
	MutationSymlink MutationKind = "symlink"
)

// MutationOperation carries every field in a fixed closed shape. Fields that
// do not apply to Kind must remain at their zero value.
type MutationOperation struct {
	OperationID   string                              `json:"operation_id"`
	Kind          MutationKind                        `json:"kind"`
	Path          string                              `json:"path"`
	NewPath       string                              `json:"new_path"`
	Content       []byte                              `json:"content"`
	Mode          runnercomposition.CandidateFileMode `json:"mode"`
	SymlinkTarget string                              `json:"symlink_target"`
}

// MutationPlan is the sole semantic output accepted from the external agent.
type MutationPlan struct {
	SchemaVersion string              `json:"schema_version"`
	Summary       string              `json:"summary"`
	Operations    []MutationOperation `json:"operations"`

	MutationPlanDigestSHA256 string `json:"mutation_plan_digest_sha256"`
}

// SnapshotFile is one accepted-plan file disclosed through
// CandidateWorkspace.ReadSnapshot. Missing is true only when the path does not
// exist in the pinned snapshot; no ambient filesystem lookup is used.
type SnapshotFile struct {
	Path    string `json:"path"`
	Missing bool   `json:"missing"`
	Content []byte `json:"content"`
}

// GenerationPrompt is the complete data disclosed to an Agent. It contains no
// repository root, candidate-buffer path, shell command, credential, admission
// decision, Git branch, or GitHub identity.
type GenerationPrompt struct {
	SchemaVersion       string         `json:"schema_version"`
	RequestDigestSHA256 string         `json:"request_digest_sha256"`
	RepositoryDomain    string         `json:"repository_domain"`
	BaseRevision        string         `json:"base_revision"`
	Plan                synthesis.Plan `json:"plan"`
	SnapshotFiles       []SnapshotFile `json:"snapshot_files"`
}

const GenerationPromptSchemaVersion = "sensei.agentcommand.generationprompt.v1"

// Agent executes one bounded external intelligence request and returns one
// mutation plan. It may report observations through O2's already-bounded
// Observer. InvalidOutputError distinguishes malformed semantic output from a
// local execution failure.
type Agent interface {
	Generate(ctx context.Context, prompt GenerationPrompt, observer providerport.Observer) (MutationPlan, error)
}
