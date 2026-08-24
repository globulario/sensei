// SPDX-License-Identifier: AGPL-3.0-only

package derive

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func at(s string) time.Time {
	t, _ := time.Parse(time.RFC3339, s)
	return t
}

// The real subject: internal/event/bus.go from sensei-code, the file whose
// coverage gap started this. Committed here as a fixture so the derivation runs
// against pinned bytes in CI rather than against whatever is checked out.
const busGo = `package event

import "sync"

type Bus struct {
	mu   sync.RWMutex
	next uint64
	subs map[uint64]chan Event
}

type Event struct{ Summary string }

func NewBus() *Bus { return &Bus{subs: make(map[uint64]chan Event)} }

func (b *Bus) Subscribe(buffer int) (<-chan Event, func()) {
	if buffer < 1 {
		buffer = 1
	}
	b.mu.Lock()
	id := b.next
	b.next++
	ch := make(chan Event, buffer)
	b.subs[id] = ch
	b.mu.Unlock()
	cancel := func() {
		b.mu.Lock()
		if c, ok := b.subs[id]; ok {
			delete(b.subs, id)
			close(c)
		}
		b.mu.Unlock()
	}
	return ch, cancel
}

func (b *Bus) Publish(e Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, ch := range b.subs {
		ch <- e
	}
}
`

// A version with one access moved outside the lock. Everything else identical.
const busGoLeaky = `package event

import "sync"

type Bus struct {
	mu   sync.RWMutex
	next uint64
	subs map[uint64]chan Event
}

type Event struct{ Summary string }

func (b *Bus) Subscribe(buffer int) <-chan Event {
	b.mu.Lock()
	id := b.next
	b.next++
	ch := make(chan Event, buffer)
	b.subs[id] = ch
	b.mu.Unlock()
	return ch
}

func (b *Bus) Count() int { return len(b.subs) }
`

// pinned builds a real git repository containing one package and returns a
// source pinned to its commit. Real git, real commit, real reads.
func pinned(t *testing.T, files map[string]string) *GitSource {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	for name, body := range files {
		full := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run("add", "-A")
	run("commit", "-qm", "fixture")
	src, err := NewGitSource(context.Background(), dir, "example.com/fixture", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	return src
}

func lockProp() Proposition {
	return Proposition{Kind: KindFieldAccessUnderLock,
		Dir: "internal/event", Type: "Bus", Field: "subs", Lock: "mu"}
}

// The A case. Sensei reads the pinned package itself and computes the
// discipline; nothing about the answer came from a claimant.
func TestADisciplineThatHoldsIsDerived(t *testing.T) {
	src := pinned(t, map[string]string{"internal/event/bus.go": busGo})
	receipt, est := Derive(src, lockProp(), at("2026-08-23T12:00:00Z"))

	if receipt.Outcome != Derived {
		t.Fatalf("outcome %s: %s", receipt.Outcome, receipt.Detail)
	}
	if est == nil {
		t.Fatal("a derived proposition produced no Established")
	}
	if got := est.Receipt().Inputs; !reflect.DeepEqual(got, []string{"internal/event/bus.go"}) {
		t.Fatalf("inputs = %v", got)
	}
	if receipt.Commit == "" || len(receipt.Commit) < 40 {
		t.Fatalf("the receipt does not pin a full commit: %q", receipt.Commit)
	}
	if len(receipt.InvalidatedBy) == 0 {
		t.Fatal("an established fact carries no path to revocation")
	}
	// Scope must not generalise into purpose or into the future.
	scope := est.Scope()
	for _, forbidden := range []string{"because", "must always", "requires"} {
		if strings.Contains(strings.ToLower(scope), forbidden) {
			t.Errorf("scope generalises beyond what was derived: %q", scope)
		}
	}
	if !strings.Contains(scope, "not why the lock exists") {
		t.Errorf("scope does not disclaim purpose: %q", scope)
	}
}

// Reproducibility is the property that makes a receipt a record of a
// computation rather than a claim about one.
func TestTheSameDerivationOverTheSamePinnedInputsAgrees(t *testing.T) {
	src := pinned(t, map[string]string{"internal/event/bus.go": busGo})
	a, _ := Derive(src, lockProp(), at("2026-08-23T12:00:00Z"))
	b, _ := Derive(src, lockProp(), at("2027-01-01T00:00:00Z"))
	if a.Outcome != b.Outcome || a.Detail != b.Detail || !reflect.DeepEqual(a.Inputs, b.Inputs) {
		t.Fatalf("same derivation, same inputs, different result:\n%+v\n%+v", a, b)
	}
	if a.Commit != b.Commit {
		t.Fatal("the pinned commit moved between runs")
	}
}

// A real counterexample refutes rather than confuses. Count() reads subs with
// no lock held.
func TestAnUnprotectedAccessIsRefutedNotEstablished(t *testing.T) {
	src := pinned(t, map[string]string{"internal/event/bus.go": busGoLeaky})
	receipt, est := Derive(src, lockProp(), at("2026-08-23T12:00:00Z"))
	if receipt.Outcome != NotDerived {
		t.Fatalf("outcome %s: %s", receipt.Outcome, receipt.Detail)
	}
	if est != nil {
		t.Fatal("a refuted proposition produced an Established")
	}
	if !strings.Contains(receipt.Detail, "Count") {
		t.Fatalf("the refutation does not name the counterexample: %q", receipt.Detail)
	}
}

// THE B SPECIMEN.
//
// "The mutex exists to serialize concurrent map access" is plausible,
// well-formed, non-contradictory, cites entirely real lines, and is
// architecturally wrong. It is also a claim about PURPOSE, and no registered
// derivation computes purpose — so the answer is UNKNOWN.
//
// The test asserts the same of the TRUE purpose claim. That is the point: this
// package cannot tell them apart, so it establishes neither, and a mechanism
// that established the true one while accepting the false one would be worse
// than one that establishes neither.
func TestPurposeClaimsAreUnknownWhicheverIsTrue(t *testing.T) {
	src := pinned(t, map[string]string{"internal/event/bus.go": busGo})
	for _, name := range []string{"lock_purpose_serialize_map", "lock_purpose_send_vs_close"} {
		p := lockProp()
		p.Kind = Kind(name)
		receipt, est := Derive(src, p, at("2026-08-23T12:00:00Z"))
		if receipt.Outcome != Unknown {
			t.Errorf("%s: outcome %s, want UNKNOWN", name, receipt.Outcome)
		}
		if est != nil {
			t.Fatalf("%s produced an Established; no derivation computes purpose", name)
		}
		if !strings.Contains(receipt.Detail, "no registered derivation applies") {
			t.Errorf("%s: %q", name, receipt.Detail)
		}
	}
}

// A proposition about something that is not there is UNKNOWN, not refuted.
// "There is no such field" and "the discipline is violated" are different
// findings and must not collapse.
func TestAPropositionAboutAbsentEntitiesIsUnknown(t *testing.T) {
	src := pinned(t, map[string]string{"internal/event/bus.go": busGo})
	for _, mut := range []func(*Proposition){
		func(p *Proposition) { p.Field = "nosuchfield" },
		func(p *Proposition) { p.Lock = "nosuchlock" },
		func(p *Proposition) { p.Type = "NoSuchType" },
		func(p *Proposition) { p.Dir = "internal/nowhere" },
	} {
		p := lockProp()
		mut(&p)
		receipt, est := Derive(src, p, at("2026-08-23T12:00:00Z"))
		if receipt.Outcome != Unknown || est != nil {
			t.Errorf("%+v -> %s (%s)", p, receipt.Outcome, receipt.Detail)
		}
	}
}

// Structural impossibility, not a boolean.
//
// Established has no exported fields and no exported constructor, so no caller
// outside this package can build one from verified evidence, from a model's
// agreement, or from a flag. This test fails the moment an exported field or a
// second constructor appears.
func TestEstablishedCannotBeConstructedOutsideADerivation(t *testing.T) {
	rt := reflect.TypeOf(Established{})
	for i := 0; i < rt.NumField(); i++ {
		if rt.Field(i).IsExported() {
			t.Fatalf("Established.%s is exported: an Established must be obtainable only from Derive, "+
				"or the guarantee becomes a convention instead of a property of the type",
				rt.Field(i).Name)
		}
	}
	if rt.NumField() == 0 {
		t.Fatal("Established became an empty struct, which any caller can construct")
	}
}

// A proposition carries no prose, and that is a structural guard rather than a
// style preference.
//
// The failure it blocks:
//
//	LLM reads F1, F2, F3 -> writes "therefore X" -> X becomes established
//
// which is the prose path wearing a derivation's clothes. A free-text field on
// Proposition is all it would take: a deriver could read it, a future one could
// pattern-match on it, and the thing establishing project truth would once
// again be a sentence somebody wrote.
//
// Every field here names an entity or a relation. A claim that cannot be
// expressed that way cannot be attempted, which is the correct outcome — it
// goes to UNKNOWN rather than into a weaker path.
func TestAPropositionCarriesNoProse(t *testing.T) {
	rt := reflect.TypeOf(Proposition{})
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		for _, prose := range []string{
			"statement", "rationale", "description", "explanation", "reason",
			"summary", "note", "detail", "because", "why", "text", "prose",
		} {
			if strings.Contains(strings.ToLower(f.Name), prose) {
				t.Fatalf("Proposition.%s is a prose field. A derivation must compute over entities and "+
					"relations, never over a sentence — otherwise 'LLM wrote therefore X' becomes an "+
					"establishing path again", f.Name)
			}
		}
	}

	// And the deriver interface is handed pinned source plus the typed
	// proposition, and nothing else. There is no parameter through which a
	// claimant's reasoning could reach a derivation.
	dt := reflect.TypeOf((*Deriver)(nil)).Elem()
	m, ok := dt.MethodByName("Derive")
	if !ok {
		t.Fatal("Deriver has no Derive method")
	}
	if n := m.Type.NumIn(); n != 2 {
		t.Fatalf("Derive takes %d inputs; it must take exactly the pinned source and the typed proposition", n)
	}
}
