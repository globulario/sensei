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
	Valid            bool                `json:"valid" yaml:"valid"`
	TaskID           string              `json:"task_id,omitempty" yaml:"task_id,omitempty"`
	EntryCount       int                 `json:"entry_count" yaml:"entry_count"`
	HeadDigestSHA256 string              `json:"head_digest_sha256,omitempty" yaml:"head_digest_sha256,omitempty"`
	Errors           []VerificationError `json:"errors,omitempty" yaml:"errors,omitempty"`
	// Warnings currently has NO producer, and a finding about chain integrity
	// must not become one. Valid is len(Errors) == 0, so anything recorded here
	// leaves the report valid: ledger.head_stale lived here and a truncated
	// ledger therefore verified clean, which is how a consumed admission reverted
	// to ready_for_mutation (issue #352). A fact that means the history may be
	// damaged belongs in Errors. This stays for the wire shape and for advisories
	// that genuinely do not bear on validity.
	Warnings        []VerificationWarning `json:"warnings,omitempty" yaml:"warnings,omitempty"`
	OrphanArtifacts []string              `json:"orphan_artifacts,omitempty" yaml:"orphan_artifacts,omitempty"`
	ProjectionState string                `json:"projection_state,omitempty" yaml:"projection_state,omitempty"`
}

type PayloadValidator func(eventType closureprotocol.LedgerEventType, mediaType string, data []byte) error

type Store struct {
	taskDir          string
	payloadValidator PayloadValidator
	// headFault is the error returned by the next headFaults HEAD publications,
	// each of which decrements the counter. See WithHeadPublicationFault.
	headFault  error
	headFaults int
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
//	counted           each publication attempt consumes one fault, so once the
//	                  count runs out the real path runs
//	explicit          it is armed by construction; a Store built without it takes
//	                  a byte-identical path
//
// One fault is absorbed by the bounded retry inside a single Append, so this
// arms the RECOVERED case. Use WithHeadPublicationFaults(2, err) to leave HEAD
// genuinely unpublished and reach ErrEntryDurable.
func WithHeadPublicationFault(err error) StoreOption {
	return WithHeadPublicationFaults(1, err)
}

// WithHeadPublicationFaults makes the next n HEAD publications on THIS store
// fail with the supplied error.
//
// The count matters because Append publishes HEAD and, if that fails, retries
// once under the same lock: n=1 exercises the recovery succeeding, n=2 leaves
// HEAD unpublished and produces ErrEntryDurable. There is no third attempt and
// no public repair afterwards, so n>=2 is how a test reaches the fail-closed
// state a truncated ledger is indistinguishable from.
func WithHeadPublicationFaults(n int, err error) StoreOption {
	return func(s *Store) { s.headFault, s.headFaults = err, n }
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
