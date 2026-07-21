// SPDX-License-Identifier: AGPL-3.0-only

package ledger

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sync"
)

// verificationScope is an evaluation-scoped memo of semantic-digest computations,
// keyed by media type + the SHA-256 of the exact payload bytes. Because the key is
// the content hash of the bytes themselves, any byte-for-byte change forces a
// recompute — the memo can never hand back a stale digest for mutated bytes, so it
// cannot launder a payload-digest mismatch into a false match. It exists to bound
// repeated work inside one completion evaluation (which re-verifies the same
// immutable chain many times) without introducing a process-global cache.
//
// It memoizes only the pure digest of (media type, bytes). It does NOT memoize, skip,
// or short-circuit payload validation: verifyAndLoadChain still runs the event-type
// PayloadValidator for every entry on every verification. Nothing here changes which
// verification errors or warnings are produced or their order.
type verificationScope struct {
	mu     sync.Mutex
	digest map[string]string
	// computations counts the semantic digests actually computed (memo misses). A
	// test can assert that identical payload bytes are digested once per evaluation
	// regardless of how many verifications or projections run over the same chain.
	computations int
}

type scopeContextKey struct{}

// VerificationScope is an opaque handle to an evaluation's digest memo. It exposes
// only bounded-work instrumentation; callers cannot read or mutate memoized digests.
type VerificationScope struct{ inner *verificationScope }

// WithVerificationScope returns a context carrying a fresh evaluation-scoped digest
// memo and a handle to it. If ctx already carries a scope (a nested evaluation), the
// existing scope is reused so one logical evaluation shares exactly one memo — the
// memo never outlives the evaluation that opened it. A context without a scope (the
// default, e.g. context.Background()) memoizes nothing and preserves today's behavior.
func WithVerificationScope(ctx context.Context) (context.Context, *VerificationScope) {
	if ctx == nil {
		ctx = context.Background()
	}
	if existing := scopeFrom(ctx); existing != nil {
		return ctx, &VerificationScope{inner: existing}
	}
	s := &verificationScope{digest: map[string]string{}}
	return context.WithValue(ctx, scopeContextKey{}, s), &VerificationScope{inner: s}
}

// DigestComputations reports how many semantic digests were actually computed (memo
// misses) within this scope. Repeated verifications of the same immutable chain add
// no computations; a byte-for-byte change to a payload adds exactly one.
func (v *VerificationScope) DigestComputations() int {
	if v == nil || v.inner == nil {
		return 0
	}
	v.inner.mu.Lock()
	defer v.inner.mu.Unlock()
	return v.inner.computations
}

func scopeFrom(ctx context.Context) *verificationScope {
	if ctx == nil {
		return nil
	}
	s, _ := ctx.Value(scopeContextKey{}).(*verificationScope)
	return s
}

// lookup returns a memoized digest for these exact bytes, if present.
func (s *verificationScope) lookup(key string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.digest[key]
	return d, ok
}

// store records a freshly computed digest and counts the computation.
func (s *verificationScope) store(key, digest string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.digest[key] = digest
	s.computations++
}

// digestMemoKey binds a digest to the media type and the exact payload bytes. The
// content hash is the whole safety story: identical bytes reuse the digest; any
// change yields a different key and a recompute.
func digestMemoKey(mediaType string, data []byte) string {
	sum := sha256.Sum256(data)
	return mediaType + "\x00" + hex.EncodeToString(sum[:])
}
