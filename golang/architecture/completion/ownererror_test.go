// SPDX-License-Identifier: Apache-2.0

package completion

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

// The classifier grants runtime ONLY on positive typed evidence. An identity error →
// identity; a ProjectionRuntimeError → runtime; ANY other error (untyped, unknown) →
// the generic fail-closed class. Runtime is never granted by absence of identity.
func TestOwnerErrorClass_PositiveEvidenceOnly(t *testing.T) {
	idErr := identityError("identity_out_of_scope", errors.New("task outside repo"))
	rtErr := runtimeError(errors.New("store unreachable"))
	plain := errors.New("something failed") // NO positive runtime evidence

	if c := ProjectionOwnerErrorClass(idErr); c != UnavailableProjectionOwnerIdentityError {
		t.Fatalf("identity → %q, want identity", c)
	}
	if c := ProjectionOwnerErrorClass(rtErr); c != UnavailableProjectionOwnerRuntimeError {
		t.Fatalf("runtime → %q, want runtime", c)
	}
	if c := ProjectionOwnerErrorClass(plain); c != UnavailableProjectionOwnerError {
		t.Fatalf("untyped error → %q, want the generic fail-closed class (NOT runtime)", c)
	}
	if c := ProjectionOwnerErrorClass(nil); c != "" {
		t.Fatalf("nil → %q, want empty", c)
	}
}

// An identity failure can NEVER be laundered into runtime — not by outer wrapping, and
// not even by passing through the runtime marker.
func TestOwnerErrorClass_IdentityNeverRuntime(t *testing.T) {
	id := identityError("identity_absent", errors.New("no task"))
	disguised := fmt.Errorf("execution failed at runtime: %w", id)
	if ProjectionOwnerErrorClass(disguised) != UnavailableProjectionOwnerIdentityError {
		t.Fatal("identity disguised with runtime-sounding text must stay identity")
	}
	// runtimeError refuses to relabel an identity error.
	if got := runtimeError(id); ProjectionOwnerErrorClass(got) != UnavailableProjectionOwnerIdentityError {
		t.Fatal("runtimeError must not launder an identity error into runtime")
	}
}

// Classification follows the typed cause, not message text: identical outer wrapping
// over an identity vs a runtime cause classifies distinctly.
func TestOwnerErrorClass_TypedNotTextual(t *testing.T) {
	wrap := func(inner error) error { return fmt.Errorf("projection owner failed: %w", inner) }
	wrappedIdentity := wrap(identityError("identity_absent", errors.New("x")))
	wrappedRuntime := wrap(runtimeError(errors.New("x")))
	if ProjectionOwnerErrorClass(wrappedIdentity) != UnavailableProjectionOwnerIdentityError {
		t.Fatal("wrapped identity must classify identity")
	}
	if ProjectionOwnerErrorClass(wrappedRuntime) != UnavailableProjectionOwnerRuntimeError {
		t.Fatal("wrapped runtime must classify runtime")
	}
}

// Identity failures produce the identity cause end-to-end AND never invoke the owner —
// the spy proves invocation did not occur (invocation is not inferred from the error).
func TestBuildEnvelope_IdentityFailsBeforeInvocation(t *testing.T) {
	a := cloneNotCompleted(t)
	b := cloneNotCompleted(t)

	orig := buildProjectionForEnvelope
	defer func() { buildProjectionForEnvelope = orig }()
	var invoked bool
	buildProjectionForEnvelope = func(ctx context.Context, req Request) (CompletionProjection, error) {
		invoked = true
		return orig(ctx, req)
	}

	for _, c := range []struct {
		name string
		req  Request
	}{
		{"absent", Request{RepositoryRoot: a.Repo, TaskDirectory: ""}},
		{"out_of_scope", Request{RepositoryRoot: a.Repo, TaskDirectory: b.TaskDir}},
	} {
		t.Run(c.name, func(t *testing.T) {
			invoked = false
			env := BuildCompletionProjectionEnvelope(context.Background(), c.req)
			if env.UnavailableClass != UnavailableProjectionOwnerIdentityError {
				t.Fatalf("class = %q, want identity", env.UnavailableClass)
			}
			if invoked {
				t.Fatal("owner must NOT be invoked when identity is invalid")
			}
		})
	}
}

// Runtime is reached ONLY after a valid identity and an ATTEMPTED owner invocation. The
// spy records that invocation actually occurred; the runtime class is not inferred from
// the returned error alone.
func TestBuildEnvelope_RuntimeRequiresAttemptedInvocation(t *testing.T) {
	w := cloneNotCompleted(t) // a real, valid identity

	orig := buildProjectionForEnvelope
	defer func() { buildProjectionForEnvelope = orig }()
	var invoked bool
	buildProjectionForEnvelope = func(ctx context.Context, req Request) (CompletionProjection, error) {
		invoked = true
		return CompletionProjection{}, errors.New("owner invocation failed: store unreachable")
	}

	env := BuildCompletionProjectionEnvelope(context.Background(), Request{RepositoryRoot: w.Repo, TaskDirectory: w.TaskDir})
	if !invoked {
		t.Fatal("owner invocation must have been attempted for a valid identity")
	}
	if env.UnavailableClass != UnavailableProjectionOwnerRuntimeError {
		t.Fatalf("class = %q, want runtime (attempted invocation failed)", env.UnavailableClass)
	}
}

// The retained generic projection_owner_error class is fail-closed: the classifier maps
// any untyped/unknown error to it, and it is distinct from the runtime class.
func TestOwnerErrorClass_GenericIsFailClosedNotRuntime(t *testing.T) {
	if UnavailableProjectionOwnerError == UnavailableProjectionOwnerRuntimeError {
		t.Fatal("generic and runtime classes must be distinct")
	}
	for _, e := range []error{errors.New("plain"), fmt.Errorf("wrapped: %w", errors.New("plain"))} {
		if ProjectionOwnerErrorClass(e) != UnavailableProjectionOwnerError {
			t.Fatalf("untyped error must be the generic fail-closed class, got %q", ProjectionOwnerErrorClass(e))
		}
	}
}
