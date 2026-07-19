// SPDX-License-Identifier: AGPL-3.0-only

package completion

import (
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/globulario/sensei/golang/architecture/identity"
	qd "github.com/globulario/sensei/golang/architecture/questiondisposition"
)

// Shared-fixture support. Building a fully-prepared completion world runs the entire
// real closure lifecycle (seed → result transition → dispose/promote/resolve →
// certify → optionally complete), which is the dominant cost of this suite. Tests
// whose SUBJECT is downstream behavior (terminal inspection, closure/receipt
// verification, projection, contradiction handling, recovery, gate reporting) do not
// need to reconstruct that world — they need an isolated, independently mutable copy
// of it. So each canonical world CLASS is built once through the real pipeline, then
// deep-copied into a fresh per-test directory. Construction tests (readiness, seeding,
// promotion, disposition, resolution, certification, result construction, and the act
// of completion itself) still build live and are never routed through a clone.
//
// Isolation is by full file copy, never hard links: a mutation test that rewrites a
// payload must not be able to poison the immutable base or any sibling clone.

// fixtureBaseDir holds the canonical immutable base worlds for the whole package run.
var fixtureBaseDir string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "completion-fixtures-")
	if err != nil {
		panic(err)
	}
	fixtureBaseDir = dir
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// copyTree deep-copies src into dst, materializing every file as an independent,
// writable regular file so a clone can be freely mutated. Directory structure and
// symlinks are preserved; nothing is hard-linked or shared with the source.
func copyTree(dst, src string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, rerr := filepath.Rel(src, p)
		if rerr != nil {
			return rerr
		}
		target := filepath.Join(dst, rel)
		switch {
		case d.IsDir():
			return os.MkdirAll(target, 0o755)
		case d.Type()&fs.ModeSymlink != 0:
			link, lerr := os.Readlink(p)
			if lerr != nil {
				return lerr
			}
			return os.Symlink(link, target)
		default:
			data, derr := os.ReadFile(p)
			if derr != nil {
				return derr
			}
			if merr := os.MkdirAll(filepath.Dir(target), 0o755); merr != nil {
				return merr
			}
			// Force user-write so mutation tests can rewrite payloads even when the
			// source (e.g. a git object) was read-only.
			return os.WriteFile(target, data, 0o644)
		}
	})
}

// canonicalBase is a world CLASS built once via the real pipeline and cloned per test.
type canonicalBase struct {
	once          sync.Once
	build         func(t *testing.T) world // real-pipeline construction, run once
	repo          string                   // persistent immutable base repo dir
	taskRel       string                   // task dir relative to repo
	questions     []qd.OpenQuestionRef
	err           error
	constructions int64 // real pipeline runs (must be 1)
	clones        int64 // isolated clones handed to tests
}

func (b *canonicalBase) ensure(t *testing.T, name string) {
	t.Helper()
	b.once.Do(func() {
		atomic.AddInt64(&b.constructions, 1)
		w := b.build(t) // builds under t.TempDir()
		dst := filepath.Join(fixtureBaseDir, name)
		if cerr := copyTree(dst, w.Repo); cerr != nil {
			b.err = cerr
			return
		}
		rel, rerr := filepath.Rel(w.Repo, w.TaskDir)
		if rerr != nil {
			b.err = rerr
			return
		}
		b.repo = dst
		b.taskRel = rel
		b.questions = append([]qd.OpenQuestionRef(nil), w.Questions...)
	})
	if b.err != nil {
		t.Fatalf("build %s fixture: %v", name, b.err)
	}
}

// clone deep-copies the base into a fresh per-test directory and returns an isolated
// world. Every call yields a unique directory that no other test shares.
func (b *canonicalBase) clone(t *testing.T, name string) world {
	t.Helper()
	b.ensure(t, name)
	repo := filepath.Join(t.TempDir(), "repo")
	if err := copyTree(repo, b.repo); err != nil {
		t.Fatalf("clone %s fixture: %v", name, err)
	}
	atomic.AddInt64(&b.clones, 1)
	return world{
		Repo:         repo,
		TaskDir:      filepath.Join(repo, b.taskRel),
		IdentityRoot: identity.Root(repo),
		Questions:    append([]qd.OpenQuestionRef(nil), b.questions...),
	}
}

// readyBase is a fully-ready (not-yet-completed) world: both certificates present for
// the current result, ready for a CompleteTask call. committedBase is that world after
// an authoritative completion.
var (
	readyBase     = &canonicalBase{build: func(t *testing.T) world { w := seedWorld(t); w.ready(t); return w }}
	committedBase = &canonicalBase{build: func(t *testing.T) world {
		w := seedWorld(t)
		head := w.ready(t)
		if w.complete(t, head).Outcome != OutcomeCommitted {
			t.Fatal("committed fixture: completion did not commit")
		}
		return w
	}}
)

// cloneReady returns an isolated ready world plus its pre-completion head, so a test
// can drive CompleteTask (or tamper first) without rebuilding the lifecycle.
func cloneReady(t *testing.T) (world, string) {
	t.Helper()
	w := readyBase.clone(t, "ready")
	return w, currentHead(t, w.TaskDir)
}

// cloneCommitted returns an isolated, already-committed world for downstream reads.
func cloneCommitted(t *testing.T) world {
	t.Helper()
	return committedBase.clone(t, "committed")
}

// notCompletedBase is a freshly seeded world with a recorded result transition and
// open questions, but no resolution or completion — the "not completed" class.
var notCompletedBase = &canonicalBase{build: func(t *testing.T) world { return seedWorld(t) }}

// cloneNotCompleted returns an isolated seeded-but-not-completed world.
func cloneNotCompleted(t *testing.T) world {
	t.Helper()
	return notCompletedBase.clone(t, "not_completed")
}
