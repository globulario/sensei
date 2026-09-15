// SPDX-License-Identifier: AGPL-3.0-only

package main

// The review finding on active_generation.go:160: two domains publishing concurrently to
// their own stores hold store-specific locks, and those do not serialize the SHARED
// registry. Both read the same YAML, both rewrite the whole file, and both use the same
// fixed `<path>.tmp` -- so one update can overwrite the other, or one rename can fail
// after the other consumed the temp file, leaving a published domain's ACTIVE pointer
// stale.
//
// Reproduced against a disposable registry rather than argued from the code.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// disposableRegistry writes a registry declaring n domains with no ACTIVE generation.
func disposableRegistry(t *testing.T, domains ...string) string {
	t.Helper()
	var b strings.Builder
	b.WriteString("domains:\n")
	for _, d := range domains {
		fmt.Fprintf(&b, "    %s:\n        repository_identity: %s\n        allowed_corpus_roots:\n            - docs/awareness\n",
			d, strings.ReplaceAll(d, "/", "-"))
	}
	path := filepath.Join(t.TempDir(), "domains.yaml")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// THE FINDING. Two legitimate concurrent updates to DIFFERENT domains must both survive.
// A shared registry is the one place a per-store lock cannot protect.
func TestTwoConcurrentActiveGenerationUpdatesBothSurvive(t *testing.T) {
	const (
		domA = "example.com/acme/alpha"
		domB = "example.com/acme/beta"
		genA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		genB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	)
	// Repeated, because a lost update is a race: one attempt can interleave benignly.
	for attempt := 0; attempt < 40; attempt++ {
		registry := disposableRegistry(t, domA, domB)
		var wg sync.WaitGroup
		errs := make([]error, 2)
		wg.Add(2)
		go func() { defer wg.Done(); errs[0] = recordActiveGeneration(registry, domA, genA) }()
		go func() { defer wg.Done(); errs[1] = recordActiveGeneration(registry, domB, genB) }()
		wg.Wait()

		for i, err := range errs {
			if err != nil {
				t.Fatalf("attempt %d: update %d failed: %v", attempt, i, err)
			}
		}
		// Both publications reported success, so both pointers must be present.
		if got := declaredActiveGeneration(registry, domA); got != genA {
			t.Fatalf("attempt %d: %s ACTIVE = %q, want %q — a successful publication's pointer was lost",
				attempt, domA, got, genA)
		}
		if got := declaredActiveGeneration(registry, domB); got != genB {
			t.Fatalf("attempt %d: %s ACTIVE = %q, want %q — a successful publication's pointer was lost",
				attempt, domB, got, genB)
		}
	}
}

// The registry must remain parseable. A torn or interleaved write is worse than a lost
// pointer: it takes every domain down, not one.
func TestTheRegistrySurvivesConcurrentUpdatesAsValidYAML(t *testing.T) {
	domains := []string{"example.com/a/one", "example.com/a/two", "example.com/a/three", "example.com/a/four"}
	for attempt := 0; attempt < 20; attempt++ {
		registry := disposableRegistry(t, domains...)
		var wg sync.WaitGroup
		for i, d := range domains {
			wg.Add(1)
			go func(d string, i int) {
				defer wg.Done()
				_ = recordActiveGeneration(registry, d, strings.Repeat(fmt.Sprint(i+1), 64))
			}(d, i)
		}
		wg.Wait()
		reg, err := LoadDomainRegistry(registry)
		if err != nil {
			t.Fatalf("attempt %d: the registry no longer parses after concurrent updates: %v", attempt, err)
		}
		if reg == nil || len(reg.Domains) != len(domains) {
			n := 0
			if reg != nil {
				n = len(reg.Domains)
			}
			t.Fatalf("attempt %d: the registry lists %d domains, want %d — a concurrent rewrite dropped entries",
				attempt, n, len(domains))
		}
	}
}

// Same-domain competing updates: the registry states ONE active generation per domain, so
// one of the two values must win whole. What must never happen is a third value, an absent
// pointer, or a torn file.
func TestCompetingUpdatesToOneDomainLeaveExactlyOneOfThem(t *testing.T) {
	const dom = "example.com/acme/alpha"
	genA := strings.Repeat("a", 64)
	genB := strings.Repeat("b", 64)
	for attempt := 0; attempt < 40; attempt++ {
		registry := disposableRegistry(t, dom)
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); _ = recordActiveGeneration(registry, dom, genA) }()
		go func() { defer wg.Done(); _ = recordActiveGeneration(registry, dom, genB) }()
		wg.Wait()
		got := declaredActiveGeneration(registry, dom)
		if got != genA && got != genB {
			t.Fatalf("attempt %d: ACTIVE = %q, which is neither competing value", attempt, got)
		}
	}
}

// writeFileAtomic is shared by 19 call sites, and only this one holds a registry lock. The
// unique temp file therefore has to be proven on the function itself: a mutant restoring
// the fixed "<path>.tmp" survived every registry witness above, because the lock makes the
// collision unreachable HERE while leaving it reachable for the other eighteen.
func TestWriteFileAtomicDoesNotLetConcurrentWritersTearAFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shared.yaml")
	// Distinct lengths and bytes, so a torn or interleaved result matches none of them.
	payloads := [][]byte{
		[]byte(strings.Repeat("a", 4096)),
		[]byte(strings.Repeat("b", 8192)),
		[]byte(strings.Repeat("c", 16384)),
		[]byte(strings.Repeat("d", 32768)),
	}
	for attempt := 0; attempt < 30; attempt++ {
		var wg sync.WaitGroup
		for _, p := range payloads {
			wg.Add(1)
			go func(p []byte) { defer wg.Done(); _ = writeFileAtomic(path, p) }(p)
		}
		wg.Wait()
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("attempt %d: the file is unreadable after concurrent writes: %v", attempt, err)
		}
		matched := false
		for _, p := range payloads {
			if string(got) == string(p) {
				matched = true
			}
		}
		if !matched {
			t.Fatalf("attempt %d: the file is %d bytes and equals no single writer's content — a shared temp file was torn",
				attempt, len(got))
		}
	}
}

// The lock must be shared by everyone writing the SAME registry, and not by writers of a
// different one. It is a pure function of the registry path, because two processes are
// never told each other's identity — they only know which registry they were given.
func TestTheRegistryLockIsDerivedFromTheRegistryPath(t *testing.T) {
	a := filepath.Join(t.TempDir(), "domains.yaml")
	b := filepath.Join(t.TempDir(), "domains.yaml")

	if domainRegistryLockPath(a) != domainRegistryLockPath(a) {
		t.Errorf("the lock path is not stable for one registry")
	}
	if domainRegistryLockPath(a) == domainRegistryLockPath(b) {
		t.Errorf("two different registries share a lock: %s", domainRegistryLockPath(a))
	}
	// Beside the registry it guards. A lock kept anywhere process-specific (a pid, this
	// process's temp dir) would serialize a process against itself and nothing else,
	// which is precisely the case the finding is about.
	if got := filepath.Dir(domainRegistryLockPath(a)); got != filepath.Dir(a) {
		t.Errorf("the lock lives in %s, not beside the registry in %s", got, filepath.Dir(a))
	}
	if !strings.Contains(filepath.Base(domainRegistryLockPath(a)), filepath.Base(a)) {
		t.Errorf("the lock name does not identify the registry it guards: %s", domainRegistryLockPath(a))
	}
}

// A lock that cannot be taken is reported, never skipped. Proceeding unserialized is how
// the pointer was lost, and losing it is silent: the graph keeps answering, from the wrong
// generation.
func TestAnUnobtainableRegistryLockIsReportedNotSkipped(t *testing.T) {
	dir := t.TempDir()
	registry := filepath.Join(dir, "domains.yaml")
	if err := os.WriteFile(registry, []byte("domains: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Occupy the lock path with a directory, so it cannot be opened as a file.
	if err := os.Mkdir(domainRegistryLockPath(registry), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := lockDomainRegistry(registry); err == nil {
		t.Fatalf("an unobtainable lock was treated as held")
	}
	// And the caller must refuse rather than write unserialized.
	if err := recordActiveGeneration(registry, "example.com/acme/alpha", strings.Repeat("a", 64)); err == nil {
		t.Errorf("recordActiveGeneration proceeded although it could not serialize the registry")
	}
}
