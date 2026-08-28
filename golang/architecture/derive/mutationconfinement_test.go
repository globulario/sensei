// SPDX-License-Identifier: AGPL-3.0-only

package derive

import (
	"strings"
	"testing"
)

const mutGoMod = "module example.com/m\n\ngo 1.22\n"

// The owner package declares Options and fills it in a constructor -- not a
// method. Under this family that is the owner exercising its authority.
const mutOwnerPkg = `package pool

type Options struct {
	BasePath string
	Replicas int
}

type Pool struct{ opts Options }

func New(o *Options) *Pool {
	p := &Pool{}
	if o != nil {
		p.opts = *o
	}
	if p.opts.BasePath == "" {
		p.opts.BasePath = "/_pool/"
	}
	return p
}

func (p *Pool) Reset() { p.opts.Replicas = 0 }
`

// A bystander package that constructs the value but never writes the field.
const mutBystander = `package client

import "example.com/m/pool"

func Build() *pool.Pool { return pool.New(&pool.Options{BasePath: "/x/"}) }
`

// A package outside the owner writing the field through a bound receiver.
const mutBypass = `package config

import "example.com/m/pool"

func Apply(o *pool.Options) { o.BasePath = "/override/" }
`

// A write whose receiver is a call result: this derivation cannot bind it.
const mutUnbound = `package other

import "example.com/m/pool"

func fetch() *pool.Options { return nil }

func Tweak() { fetch().BasePath = "/y/" }
`

// A different type with a field of the same name, written outside pool.
const mutSameName = `package unrelated

type Route struct{ BasePath string }

func Set(r *Route) { r.BasePath = "/z/" }
`

func mutation(paths ...string) Proposition {
	if len(paths) == 0 {
		paths = []string{"."}
	}
	return Proposition{Kind: KindStateMutationConfinedToOwner, Dir: "pool", Type: "Options", Field: "BasePath", SearchPaths: paths}
}

func TestMutationConfinementDerivesWhenOnlyTheOwnerWrites(t *testing.T) {
	src := pinned(t, map[string]string{
		"go.mod":             mutGoMod,
		"pool/pool.go":       mutOwnerPkg,
		"client/client.go":   mutBystander,
		"unrelated/route.go": mutSameName,
	})
	receipt, est := Derive(src, mutation(), at("2026-08-27T12:00:00Z"))
	if receipt.Outcome != Derived || est == nil {
		t.Fatalf("%s: %s", receipt.Outcome, receipt.Detail)
	}
	// Two files read for context; one file is what the proof is about, and a
	// same-named field on another type is not a write to this subject.
	subjects := receipt.SubjectFiles()
	if len(subjects) != 1 || subjects[0] != "pool/pool.go" {
		t.Fatalf("subjects = %v", subjects)
	}
	for _, s := range receipt.Subjects {
		if s.Role != "mutation-site" || s.Entity != "Options.BasePath" {
			t.Errorf("subject %+v", s)
		}
	}
	if !strings.Contains(receipt.Detail, "all 1 observable write(s)") {
		t.Fatalf("detail: %s", receipt.Detail)
	}
}

// A cross-package write through a bound receiver is a counterexample to
// confinement, named as such and located.
func TestMutationConfinementRefutesACrossPackageWrite(t *testing.T) {
	src := pinned(t, map[string]string{
		"go.mod":           mutGoMod,
		"pool/pool.go":     mutOwnerPkg,
		"config/config.go": mutBypass,
	})
	receipt, est := Derive(src, mutation(), at("2026-08-27T12:00:00Z"))
	if receipt.Outcome != Refuted || est != nil {
		t.Fatalf("%s: %s", receipt.Outcome, receipt.Detail)
	}
	for _, want := range []string{"counterexample to confinement", "config/config.go:5 in config", "1 of 2 observable write(s)"} {
		if !strings.Contains(receipt.Detail, want) {
			t.Fatalf("detail lacks %q: %s", want, receipt.Detail)
		}
	}
}

// A receiver this derivation cannot bind is the completeness boundary:
// UNRESOLVED, with the site named, and never "not this type".
func TestMutationConfinementIsUnresolvedWhenAReceiverCannotBeBound(t *testing.T) {
	src := pinned(t, map[string]string{
		"go.mod":         mutGoMod,
		"pool/pool.go":   mutOwnerPkg,
		"other/other.go": mutUnbound,
	})
	receipt, est := Derive(src, mutation(), at("2026-08-27T12:00:00Z"))
	if receipt.Outcome != Unresolved || est != nil {
		t.Fatalf("%s: %s", receipt.Outcome, receipt.Detail)
	}
	if !strings.Contains(receipt.Detail, "other/other.go:7 (receiver fetch(...) not bound)") {
		t.Fatalf("detail: %s", receipt.Detail)
	}
	// A counterexample elsewhere wins over an unbound site: refuted is
	// stronger than unresolved, and never the other way round.
	src = pinned(t, map[string]string{
		"go.mod":           mutGoMod,
		"pool/pool.go":     mutOwnerPkg,
		"other/other.go":   mutUnbound,
		"config/config.go": mutBypass,
	})
	receipt, _ = Derive(src, mutation(), at("2026-08-27T12:00:00Z"))
	if receipt.Outcome != Refuted {
		t.Fatalf("an unbound site hid a counterexample: %s: %s", receipt.Outcome, receipt.Detail)
	}
}

// A narrower search is a weaker claim: searching only the owner package
// cannot see the bypass, and the receipt says where it looked.
func TestMutationConfinementNarrowSearchIsAWeakerClaim(t *testing.T) {
	src := pinned(t, map[string]string{
		"go.mod":           mutGoMod,
		"pool/pool.go":     mutOwnerPkg,
		"config/config.go": mutBypass,
	})
	receipt, est := Derive(src, mutation("pool"), at("2026-08-27T12:00:00Z"))
	if receipt.Outcome != Derived || est == nil {
		t.Fatalf("%s: %s", receipt.Outcome, receipt.Detail)
	}
	// Inputs are the one file in scope plus go.mod, which binds qualified
	// names and is therefore part of what the receipt rests on.
	if !strings.Contains(receipt.Detail, "under pool") || len(receipt.Inputs) != 2 || receipt.Inputs[0] != "pool/pool.go" || receipt.Inputs[1] != "go.mod" {
		t.Fatalf("the receipt does not state its narrow scope: %s / %v", receipt.Detail, receipt.Inputs)
	}
}

// Nothing writes the field: true and useless, and reported as nothing to
// establish rather than as a confinement.
func TestMutationConfinementIsUnknownWhenNothingWrites(t *testing.T) {
	src := pinned(t, map[string]string{
		"go.mod":           mutGoMod,
		"pool/pool.go":     "package pool\n\ntype Options struct{ BasePath string }\n",
		"client/client.go": mutBystander,
	})
	receipt, est := Derive(src, mutation(), at("2026-08-27T12:00:00Z"))
	if receipt.Outcome != Unknown || est != nil || !strings.Contains(receipt.Detail, "nothing to establish") {
		t.Fatalf("%s: %s", receipt.Outcome, receipt.Detail)
	}
	// And without a go.mod, a qualified reference cannot be bound: the
	// cross-package write is UNRESOLVED, not silently ignored.
	src = pinned(t, map[string]string{
		"pool/pool.go":     mutOwnerPkg,
		"config/config.go": mutBypass,
	})
	receipt, _ = Derive(src, mutation(), at("2026-08-27T12:00:00Z"))
	if receipt.Outcome != Unresolved {
		t.Fatalf("without go.mod the qualified write was %s: %s", receipt.Outcome, receipt.Detail)
	}
}

// sensei#313 review, P1: a nested := that shadows the receiver must not erase
// a genuine bypass after the block. One function-wide binding map did.
func TestMutationConfinementShadowingDoesNotHideABypass(t *testing.T) {
	const shadow = `package config

import "example.com/m/pool"

type Other struct{ BasePath string }

func Apply(o *pool.Options) {
	if true {
		o := &Other{}
		o.BasePath = "/other/"
	}
	o.BasePath = "/override/"
}
`
	src := pinned(t, map[string]string{"go.mod": mutGoMod, "pool/pool.go": mutOwnerPkg, "config/config.go": shadow})
	receipt, _ := Derive(src, mutation(), at("2026-08-27T12:00:00Z"))
	if receipt.Outcome != Refuted || !strings.Contains(receipt.Detail, "config/config.go:12 in config") {
		t.Fatalf("a shadowed name hid the bypass: %s: %s", receipt.Outcome, receipt.Detail)
	}
	// And the inner write to Other.BasePath is Other's own field: not a site.
	if strings.Contains(receipt.Detail, "config.go:10") {
		t.Fatalf("another type's own field was counted: %s", receipt.Detail)
	}
}

// sensei#313 review, P1: a write reaching F by promotion through an embedded
// field is UNRESOLVED, never silently "not this type".
func TestMutationConfinementEmbeddedPromotionIsUnresolved(t *testing.T) {
	const wrap = `package wrap

import "example.com/m/pool"

type W struct{ *pool.Options }

func Set(w *W) { w.BasePath = "/w/" }
`
	src := pinned(t, map[string]string{"go.mod": mutGoMod, "pool/pool.go": mutOwnerPkg, "wrap/wrap.go": wrap})
	receipt, _ := Derive(src, mutation(), at("2026-08-27T12:00:00Z"))
	if receipt.Outcome != Unresolved || !strings.Contains(receipt.Detail, "promotion is not followed") {
		t.Fatalf("%s: %s", receipt.Outcome, receipt.Detail)
	}
}

// sensei#313 review, P2: the sealed relation says an address of T.F taken
// outside the owner is write authority that escapes -- UNRESOLVED, not a
// counterexample. Inside the owner it is the owner's own site.
func TestMutationConfinementAddressOutsideOwnerIsUnresolved(t *testing.T) {
	const escape = `package leak

import "example.com/m/pool"

func Ptr(o *pool.Options) *string { return &o.BasePath }
`
	src := pinned(t, map[string]string{"go.mod": mutGoMod, "pool/pool.go": mutOwnerPkg, "leak/leak.go": escape})
	receipt, _ := Derive(src, mutation(), at("2026-08-27T12:00:00Z"))
	if receipt.Outcome != Unresolved || !strings.Contains(receipt.Detail, "write authority escapes") {
		t.Fatalf("%s: %s", receipt.Outcome, receipt.Detail)
	}
	const inside = `package pool

func (p *Pool) ptr() *string { return &p.opts.BasePath }
`
	src = pinned(t, map[string]string{"go.mod": mutGoMod, "pool/pool.go": mutOwnerPkg, "pool/ptr.go": inside})
	receipt, _ = Derive(src, mutation(), at("2026-08-27T12:00:00Z"))
	if receipt.Outcome != Derived || !strings.Contains(receipt.Detail, "all 2 observable write(s)") {
		t.Fatalf("an owner-side address was not the owner's own site: %s: %s", receipt.Outcome, receipt.Detail)
	}
}

// sensei#313 review, P1: a struct field's qualified type is written in the
// DECLARING file's aliases. Here the declaring file imports the owner as
// `opts`, while the mutation-site file binds `pool` to a different package
// that also has an Options type. Resolving the chain through the mutation
// site's aliases would bind h.Opts to the wrong Options and hide the write.
func TestMutationConfinementFieldChainsUseTheDeclaringFilesImports(t *testing.T) {
	const holderDecl = `package holder

import opts "example.com/m/pool"

type H struct{ Opts *opts.Options }
`
	const decoy = `package pool2

type Options struct{ BasePath string }
`
	const writer = `package writer

import (
	pool "example.com/m/pool2"
	"example.com/m/holder"
)

var _ pool.Options

func Set(h *holder.H) { h.Opts.BasePath = "/x/" }
`
	src := pinned(t, map[string]string{
		"go.mod": mutGoMod, "pool/pool.go": mutOwnerPkg,
		"holder/holder.go": holderDecl, "pool2/pool2.go": decoy, "writer/writer.go": writer,
	})
	receipt, _ := Derive(src, mutation(), at("2026-08-27T12:00:00Z"))
	if receipt.Outcome != Refuted || !strings.Contains(receipt.Detail, "writer/writer.go:10 in writer") {
		t.Fatalf("an alias collision hid a cross-package write: %s: %s", receipt.Outcome, receipt.Detail)
	}
}

// sensei#313 review, P2: go.mod decides how qualified names bind, so a
// receipt that rests on it names it as an input -- a go.mod-only change can
// then invalidate the derivation. Without a go.mod nothing is claimed read.
func TestMutationConfinementReceiptIsBoundToGoMod(t *testing.T) {
	src := pinned(t, map[string]string{"go.mod": mutGoMod, "pool/pool.go": mutOwnerPkg, "config/config.go": mutBypass})
	receipt, _ := Derive(src, mutation(), at("2026-08-27T12:00:00Z"))
	found := false
	for _, in := range receipt.Inputs {
		if in == "go.mod" {
			found = true
		}
	}
	if !found {
		t.Fatalf("go.mod is not among the receipt's inputs: %v", receipt.Inputs)
	}
	// Re-routing the module path in go.mod alone changes the answer: the
	// qualified write no longer binds, so the same tree is UNRESOLVED.
	src = pinned(t, map[string]string{"go.mod": "module example.com/elsewhere\n", "pool/pool.go": mutOwnerPkg, "config/config.go": mutBypass})
	receipt, _ = Derive(src, mutation(), at("2026-08-27T12:00:00Z"))
	if receipt.Outcome != Unresolved {
		t.Fatalf("a go.mod-only change did not change the derivation: %s: %s", receipt.Outcome, receipt.Detail)
	}
}
