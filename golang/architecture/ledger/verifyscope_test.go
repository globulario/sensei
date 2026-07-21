// SPDX-License-Identifier: AGPL-3.0-only

package ledger

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"gopkg.in/yaml.v3"
)

// buildScopeChain appends n entries, each with a DISTINCT yaml payload, using the
// non-scoped Background context so the returned store's ledger exists without having
// populated any evaluation memo. Returns the store and the verified chain.
func buildScopeChain(t *testing.T, n int) (*Store, VerifiedChain) {
	t.Helper()
	taskDir := t.TempDir()
	store := NewStore(taskDir, WithPayloadValidator(testPayloadValidator))
	head := ""
	for i := 0; i < n; i++ {
		res, err := store.Append(context.Background(), AppendRequest{
			TaskID: "task.scope", SessionID: "session.scope", ExpectedHeadDigestSHA256: head,
			EventType:        closureprotocol.LedgerEventTaskPrepared,
			Payload:          testPayload{SchemaVersion: "1", Message: fmt.Sprintf("payload-%d", i)},
			PayloadMediaType: "application/yaml", ProducerID: "sensei.test",
			ProducedAt: time.Date(2026, 7, 15, 12, i, 0, 0, time.UTC),
		})
		if err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
		head = res.Entry.EntryDigestSHA256
	}
	chain, err := store.VerifyChain()
	if err != nil {
		t.Fatalf("verify chain: %v", err)
	}
	return store, chain
}

// The memoized digest for a payload is byte-for-byte identical to the non-memoized
// computation, and identical bytes are digested exactly once within a scope.
func TestVerificationScope_ReusedDigestIsByteIdentical(t *testing.T) {
	data := []byte("schema_version: \"1\"\nmessage: hello\n")
	want, err := computeSemanticDigest("application/yaml", data)
	if err != nil {
		t.Fatalf("compute: %v", err)
	}
	ctx, scope := WithVerificationScope(context.Background())
	for i := 0; i < 5; i++ {
		got, err := semanticDigestForBytesCtx(ctx, "application/yaml", data)
		if err != nil {
			t.Fatalf("scoped digest: %v", err)
		}
		if got != want {
			t.Fatalf("memoized digest %q != non-memoized %q", got, want)
		}
	}
	if n := scope.DigestComputations(); n != 1 {
		t.Fatalf("identical bytes must be digested once, computed %d times", n)
	}
}

// Different bytes force a recompute — the memo is keyed by content, never reused across
// distinct payloads.
func TestVerificationScope_DifferentBytesRecompute(t *testing.T) {
	ctx, scope := WithVerificationScope(context.Background())
	a, _ := semanticDigestForBytesCtx(ctx, "application/yaml", []byte("message: a\n"))
	b, _ := semanticDigestForBytesCtx(ctx, "application/yaml", []byte("message: b\n"))
	if a == b {
		t.Fatal("distinct payloads must produce distinct digests")
	}
	if n := scope.DigestComputations(); n != 2 {
		t.Fatalf("two distinct payloads must be digested twice, got %d", n)
	}
}

// Repeated verifications of the same immutable chain digest each payload once per
// evaluation — the bounded-work contract, proven by the computation counter rather
// than wall-clock.
func TestVerificationScope_DigestComputedOncePerEvaluation(t *testing.T) {
	const n = 6
	store, _ := buildScopeChain(t, n)
	ctx, scope := WithVerificationScope(context.Background())
	for i := 0; i < 4; i++ {
		if _, err := store.VerifyChainCtx(ctx); err != nil {
			t.Fatalf("verify %d: %v", i, err)
		}
	}
	// n distinct payloads, digested once each, regardless of the four verifications.
	if got := scope.DigestComputations(); got != n {
		t.Fatalf("expected %d digest computations across 4 verifications, got %d", n, got)
	}
}

// A byte-for-byte change to a payload between two verifications in the SAME scope is
// still detected: the memo key is the content hash, so mutated bytes miss the memo,
// recompute, and surface the digest mismatch. The memo can never launder a mutation.
func TestVerificationScope_MutationBetweenVerificationsIsDetected(t *testing.T) {
	store, chain := buildScopeChain(t, 4)
	ctx, _ := WithVerificationScope(context.Background())

	first, err := store.VerifyCtx(ctx)
	if err != nil || !first.Valid {
		t.Fatalf("baseline must be valid: %+v err=%v", first, err)
	}

	// Overwrite one payload artifact with different, still-parseable bytes.
	target := chain.Entries[1].PayloadPath
	mutated, _ := yaml.Marshal(testPayload{SchemaVersion: "1", Message: "TAMPERED"})
	if err := os.WriteFile(target, mutated, 0o644); err != nil {
		t.Fatalf("mutate payload: %v", err)
	}

	after, err := store.VerifyCtx(ctx)
	if err != nil {
		t.Fatalf("re-verify: %v", err)
	}
	if after.Valid {
		t.Fatal("mutation must invalidate the chain despite the shared scope")
	}
	found := false
	for _, e := range after.Errors {
		if e.Code == "ledger.payload_digest_mismatch" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected ledger.payload_digest_mismatch, got %+v", after.Errors)
	}
}

// Event-type payload validation runs on every entry of every verification and is never
// skipped by a digest-memo hit: validation and digesting are independent.
func TestVerificationScope_ValidationAlwaysRerunsOnMemoHit(t *testing.T) {
	const n = 5
	taskDir := t.TempDir()
	seed := NewStore(taskDir, WithPayloadValidator(testPayloadValidator))
	head := ""
	for i := 0; i < n; i++ {
		res, err := seed.Append(context.Background(), AppendRequest{
			TaskID: "task.v", SessionID: "session.v", ExpectedHeadDigestSHA256: head,
			EventType:        closureprotocol.LedgerEventTaskPrepared,
			Payload:          testPayload{SchemaVersion: "1", Message: fmt.Sprintf("v-%d", i)},
			PayloadMediaType: "application/yaml", ProducerID: "sensei.test",
			ProducedAt: time.Date(2026, 7, 15, 13, i, 0, 0, time.UTC),
		})
		if err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
		head = res.Entry.EntryDigestSHA256
	}

	var validatorCalls int
	counting := func(et closureprotocol.LedgerEventType, mt string, data []byte) error {
		validatorCalls++
		return testPayloadValidator(et, mt, data)
	}
	store := NewStore(taskDir, WithPayloadValidator(counting))
	ctx, scope := WithVerificationScope(context.Background())
	for i := 0; i < 2; i++ {
		if _, err := store.VerifyCtx(ctx); err != nil {
			t.Fatalf("verify %d: %v", i, err)
		}
	}
	if validatorCalls != 2*n {
		t.Fatalf("validation must run for every entry of every verification: want %d, got %d", 2*n, validatorCalls)
	}
	if got := scope.DigestComputations(); got != n {
		t.Fatalf("digests must be memoized to %d despite %d validator runs, got %d", n, validatorCalls, got)
	}
}

// A scoped verification and an unscoped verification of the same ledger produce an
// identical report: the memo changes performance, never the verdict.
func TestVerificationScope_ScopedEqualsUnscopedReport(t *testing.T) {
	store, _ := buildScopeChain(t, 5)

	plain, err := store.Verify()
	if err != nil {
		t.Fatalf("plain verify: %v", err)
	}
	ctx, _ := WithVerificationScope(context.Background())
	scoped, err := store.VerifyCtx(ctx)
	if err != nil {
		t.Fatalf("scoped verify: %v", err)
	}
	if plain.Valid != scoped.Valid || plain.EntryCount != scoped.EntryCount ||
		plain.HeadDigestSHA256 != scoped.HeadDigestSHA256 ||
		plain.ProjectionState != scoped.ProjectionState ||
		len(plain.Errors) != len(scoped.Errors) || len(plain.Warnings) != len(scoped.Warnings) {
		t.Fatalf("scoped report differs from unscoped:\n plain=%+v\nscoped=%+v", plain, scoped)
	}
}
