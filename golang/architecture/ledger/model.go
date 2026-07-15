// SPDX-License-Identifier: Apache-2.0

package ledger

import (
	"context"
	"time"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

const HeadSchemaVersion = "1"

type Entry = closureprotocol.LedgerEntry

type Head struct {
	SchemaVersion     string `json:"schema_version" yaml:"schema_version"`
	TaskID            string `json:"task_id" yaml:"task_id"`
	Sequence          int    `json:"sequence" yaml:"sequence"`
	EntryDigestSHA256 string `json:"entry_digest_sha256" yaml:"entry_digest_sha256"`
	EntryPath         string `json:"entry_path" yaml:"entry_path"`
}

type AppendRequest struct {
	TaskID                   string
	SessionID                string
	ExpectedHeadDigestSHA256 string
	EventType                closureprotocol.LedgerEventType
	Payload                  any
	PayloadMediaType         string
	ProducerID               string
	ProducedAt               time.Time
}

type AppendResult struct {
	Entry       Entry
	Head        Head
	PayloadPath string
	Replay      bool
}

type VerificationError struct {
	Code   string `json:"code" yaml:"code"`
	Detail string `json:"detail" yaml:"detail"`
	Path   string `json:"path,omitempty" yaml:"path,omitempty"`
}

type VerificationWarning struct {
	Code   string `json:"code" yaml:"code"`
	Detail string `json:"detail" yaml:"detail"`
	Path   string `json:"path,omitempty" yaml:"path,omitempty"`
}

type VerificationReport struct {
	Valid            bool                  `json:"valid" yaml:"valid"`
	TaskID           string                `json:"task_id,omitempty" yaml:"task_id,omitempty"`
	EntryCount       int                   `json:"entry_count" yaml:"entry_count"`
	HeadDigestSHA256 string                `json:"head_digest_sha256,omitempty" yaml:"head_digest_sha256,omitempty"`
	Errors           []VerificationError   `json:"errors,omitempty" yaml:"errors,omitempty"`
	Warnings         []VerificationWarning `json:"warnings,omitempty" yaml:"warnings,omitempty"`
	OrphanArtifacts  []string              `json:"orphan_artifacts,omitempty" yaml:"orphan_artifacts,omitempty"`
	ProjectionState  string                `json:"projection_state,omitempty" yaml:"projection_state,omitempty"`
}

type PayloadValidator func(eventType closureprotocol.LedgerEventType, mediaType string, data []byte) error

type Store struct {
	taskDir          string
	payloadValidator PayloadValidator
}

type VerifiedEntry struct {
	Entry       Entry
	EntryPath   string
	PayloadPath string
}

type VerifiedChain struct {
	TaskID          string
	Entries         []VerifiedEntry
	Head            Head
	OrphanArtifacts []string
}

type StoreOption func(*Store)

func WithPayloadValidator(fn PayloadValidator) StoreOption {
	return func(s *Store) { s.payloadValidator = fn }
}

func NewStore(taskDir string, opts ...StoreOption) *Store {
	s := &Store{taskDir: taskDir}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *Store) Append(ctx context.Context, req AppendRequest) (AppendResult, error) {
	return appendEntry(ctx, s, req)
}

func (s *Store) Verify() (VerificationReport, error) {
	return verifyTaskLedger(s.taskDir, s.payloadValidator)
}

func VerifyTaskLedger(taskDir string) (VerificationReport, error) {
	return verifyTaskLedger(taskDir, nil)
}
