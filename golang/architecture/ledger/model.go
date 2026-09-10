// SPDX-License-Identifier: AGPL-3.0-only

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
	// headFault, when non-nil, makes the NEXT HEAD publication fail and is then
	// cleared. See WithHeadPublicationFault.
	headFault error
}

type VerifiedEntry struct {
	Entry       Entry
	EntryPath   string
	PayloadPath string
}

type VerifiedChain struct {
	TaskDir         string
	TaskID          string
	Entries         []VerifiedEntry
	Head            Head
	OrphanArtifacts []string
}

type StoreOption func(*Store)

func WithPayloadValidator(fn PayloadValidator) StoreOption {
	return func(s *Store) { s.payloadValidator = fn }
}

// WithHeadPublicationFault makes the next HEAD publication on THIS store fail
// once, with the supplied error.
//
// It exists because ErrEntryDurable is a real contract that was, until now,
// impossible to exercise deliberately. That condition -- the ledger entry is
// durable and HEAD.yaml is not written -- is the one moment where a caller must
// treat an error as post-commit, and every consumer's handling of it was
// therefore unproved. A recovery path nobody can run is a recovery path nobody
// has tested.
//
// Deliberately constrained:
//
//	instance-scoped   it lives on one Store, never in package state, so two
//	                  tests cannot reach each other
//	one-shot          it fires exactly once and clears itself, so a retry inside
//	                  the same test exercises the real path
//	explicit          it is armed by construction; a Store built without it takes
//	                  a byte-identical path
//
// It does not change what Append means. Append already returns ErrEntryDurable
// when the entry is durable and HEAD is not published; this makes that reachable
// on purpose rather than by filesystem accident.
func WithHeadPublicationFault(err error) StoreOption {
	return func(s *Store) { s.headFault = err }
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
	return verifyTaskLedger(context.Background(), s.taskDir, s.payloadValidator)
}

// VerifyCtx is Verify with an evaluation scope. When ctx carries a verification
// scope (see WithVerificationScope) the chain's per-payload semantic digests are
// memoized for this evaluation; the verification is otherwise identical.
func (s *Store) VerifyCtx(ctx context.Context) (VerificationReport, error) {
	return verifyTaskLedger(ctx, s.taskDir, s.payloadValidator)
}

func (s *Store) VerifyChain() (VerifiedChain, error) {
	return loadVerifiedChain(context.Background(), s.taskDir, s.payloadValidator)
}

// VerifyChainCtx is VerifyChain with an evaluation scope (see VerifyCtx).
func (s *Store) VerifyChainCtx(ctx context.Context) (VerifiedChain, error) {
	return loadVerifiedChain(ctx, s.taskDir, s.payloadValidator)
}

func VerifyTaskLedger(taskDir string) (VerificationReport, error) {
	return verifyTaskLedger(context.Background(), taskDir, nil)
}
