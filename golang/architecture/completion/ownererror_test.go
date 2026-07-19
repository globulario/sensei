// SPDX-License-Identifier: Apache-2.0

package completion

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

// The pure classifier depends only on the error TYPE. A typed identity error — even
// wrapped under arbitrary outer text — is an identity cause; any other error is runtime.
func TestOwnerErrorClass_TypedNotTextual(t *testing.T) {
	idErr := identityError("identity_out_of_scope", errors.New("task outside repo"))
	runtimeErr := errors.New("read ledger: i/o timeout")

	if c := ProjectionOwnerErrorClass(idErr); c != UnavailableProjectionOwnerIdentityError {
		t.Fatalf("identity error classified as %q, want identity", c)
	}
	if c := ProjectionOwnerErrorClass(runtimeErr); c != UnavailableProjectionOwnerRuntimeError {
		t.Fatalf("runtime error classified as %q, want runtime", c)
	}

	// Adversarial: identical outer wrapping over an identity vs a runtime cause. The
	// classification must follow the typed cause, not the (identical) message text.
	wrap := func(inner error) error { return fmt.Errorf("projection owner failed: %w", inner) }
	wrappedIdentity := wrap(idErr)
	wrappedRuntime := wrap(runtimeErr)
	if wrappedIdentity.Error() == wrappedRuntime.Error() {
		// (They differ only in the inner detail; force the outer text to be identical.)
		t.Log("note: outer texts differ by inner detail; the type test below is the real proof")
	}
	if ProjectionOwnerErrorClass(wrappedIdentity) != UnavailableProjectionOwnerIdentityError {
		t.Fatal("wrapped identity error must still classify as identity (type, not text)")
	}
	if ProjectionOwnerErrorClass(wrappedRuntime) != UnavailableProjectionOwnerRuntimeError {
		t.Fatal("wrapped runtime error must classify as runtime")
	}
}

// Test 16/17: an identity failure can NEVER be classified as runtime, even when wrapped
// with text that looks like a runtime failure — runtime is assigned only when the error
// is NOT a typed identity error (i.e. identity validation passed and the owner ran).
func TestOwnerErrorClass_IdentityNeverRuntime(t *testing.T) {
	disguised := fmt.Errorf("execution failed at runtime: %w", identityError("identity_absent", errors.New("no task")))
	if ProjectionOwnerErrorClass(disguised) != UnavailableProjectionOwnerIdentityError {
		t.Fatal("an identity error disguised with runtime-sounding text must stay identity")
	}
	if !IsProjectionIdentityError(disguised) {
		t.Fatal("IsProjectionIdentityError must see through wrapping")
	}
}

// Tests 12 & 14 end-to-end: an absent identity (empty task dir) and an out-of-scope
// identity (repo A + repo B's task dir) each produce a typed identity-cause envelope —
// never runtime, never the generic class.
func TestBuildEnvelope_IdentityFailuresAreIdentityCause(t *testing.T) {
	a := cloneNotCompleted(t)
	b := cloneNotCompleted(t)
	cases := []struct {
		name string
		req  Request
	}{
		{"absent", Request{RepositoryRoot: a.Repo, TaskDirectory: ""}},
		{"out_of_scope", Request{RepositoryRoot: a.Repo, TaskDirectory: b.TaskDir}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			env := BuildCompletionProjectionEnvelope(context.Background(), c.req)
			if env.Availability != CompletionUnavailable {
				t.Fatalf("must be unavailable: %+v", env)
			}
			if env.UnavailableClass != UnavailableProjectionOwnerIdentityError {
				t.Fatalf("class = %q, want projection_owner_identity_error (identity, never runtime)", env.UnavailableClass)
			}
		})
	}
}

// Test 15 end-to-end: a VALID identity followed by a runtime failure of the resolved
// owner is classified as the runtime cause. The in-process owner does not otherwise
// surface a post-identity runtime error, so the build seam simulates one — proving the
// runtime lane is reachable ONLY after identity succeeded and the owner was invoked.
func TestBuildEnvelope_RuntimeFailureAfterValidIdentity(t *testing.T) {
	w := cloneNotCompleted(t) // a real, valid identity

	orig := buildProjectionForEnvelope
	defer func() { buildProjectionForEnvelope = orig }()
	buildProjectionForEnvelope = func(ctx context.Context, req Request) (CompletionProjection, error) {
		// Identity is valid (the request names one world); the owner was invoked and
		// then failed at runtime. This is NOT a typed identity error.
		return CompletionProjection{}, errors.New("owner invocation failed: store unreachable")
	}

	env := BuildCompletionProjectionEnvelope(context.Background(), Request{RepositoryRoot: w.Repo, TaskDirectory: w.TaskDir})
	if env.Availability != CompletionUnavailable {
		t.Fatalf("must be unavailable: %+v", env)
	}
	if env.UnavailableClass != UnavailableProjectionOwnerRuntimeError {
		t.Fatalf("class = %q, want projection_owner_runtime_error", env.UnavailableClass)
	}
}
