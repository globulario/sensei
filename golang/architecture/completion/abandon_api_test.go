// SPDX-License-Identifier: AGPL-3.0-only

// Package completion_test exercises the PUBLIC surface from outside the package,
// which is the only place the following properties are observable.
package completion_test

import (
	"context"
	"testing"

	"github.com/globulario/sensei/golang/architecture/completion"
)

// TestAbandonRequestKeepsItsPublicContract pins three properties that an
// in-package test cannot see, and that a keyed-literal check does not reveal.
//
// An earlier attempt at the fault seam put an unexported []ledger.StoreOption
// field on AbandonRequest. Keyed construction still compiled, so it looked
// harmless. It was not: a slice makes the struct non-comparable, and an
// unexported field makes external positional literals illegal. Both are breaking
// changes to callers, and both are invisible from inside the package.
//
// This file is the check that would have caught it.
func TestAbandonRequestKeepsItsPublicContract(t *testing.T) {
	// 1. KEYED construction from outside the package.
	keyed := completion.AbandonRequest{
		RepositoryRoot:                 "/repo",
		TaskDirectory:                  "/repo/.sensei/tasks/task.defect.aaaa",
		IdentityRoot:                   "/repo/.sensei/identity",
		ExpectedLedgerHeadDigestSHA256: "abc",
		Reason:                         "why",
	}

	// 2. POSITIONAL construction from outside the package. This does not compile
	//    if the struct gains an unexported field, and it does not compile if the
	//    field order or count changes.
	positional := completion.AbandonRequest{
		"/repo",
		"/repo/.sensei/tasks/task.defect.aaaa",
		"/repo/.sensei/identity",
		"abc",
		"why",
	}

	// 3. COMPARABILITY. This does not compile if the struct gains a slice, map or
	//    func field.
	if keyed != positional {
		t.Fatal("keyed and positional construction of the same values differ")
	}
	var zero completion.AbandonRequest
	if keyed == zero {
		t.Fatal("a populated request compares equal to the zero value")
	}
}

// TestAbandonTaskIsReachableWithProductionDefaults proves the public wrapper
// delegates and runs the real implementation.
//
// The recovery-path test drives the unexported helper so it can arm a ledger
// fault. That would be worth little if the exported function had drifted away
// from it, so this exercises AbandonTask itself: it must reach the same
// implementation, refuse this obviously-invalid request through the same typed
// outcome, and return no error value.
func TestAbandonTaskIsReachableWithProductionDefaults(t *testing.T) {
	res, err := completion.AbandonTask(context.Background(), completion.AbandonRequest{
		RepositoryRoot: t.TempDir(),
		TaskDirectory:  t.TempDir(),
		IdentityRoot:   t.TempDir(),
		// No expected head and no reason: the implementation's own input
		// validation must answer, which proves we reached it.
	})
	if err != nil {
		t.Fatalf("AbandonTask returned a transport error rather than a typed outcome: %v", err)
	}
	if res.Outcome != completion.OutcomeInputInvalid {
		t.Fatalf("outcome = %q, want input_invalid: the public wrapper did not reach the implementation",
			res.Outcome)
	}
	if res.Detail == "" {
		t.Fatal("the refusal carries no detail")
	}
}
