// SPDX-License-Identifier: AGPL-3.0-only

package runtimedescriptor

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// trueCommand returns a command that exits 0 immediately, portable across
// the platforms this repo ships binaries for.
func trueCommand() (name string, args []string) {
	if runtime.GOOS == "windows" {
		return "cmd", []string{"/c", "exit", "0"}
	}
	return "true", nil
}

// withHomeDir redirects baseDir()'s resolution to an isolated temp HOME for
// the duration of the test, so these tests never touch the real
// ~/.local/share/sensei/runtime directory.
func withHomeDir(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	// os.UserHomeDir on non-Windows reads $HOME; on Windows it reads
	// %USERPROFILE%. Set both so this test is portable.
	t.Setenv("USERPROFILE", home)
	return home
}

func TestWriteRead_RoundTrips(t *testing.T) {
	withHomeDir(t)
	d := Descriptor{
		Kind:             KindAwarenessGraph,
		PID:              os.Getpid(), // the test process itself is guaranteed alive
		ListenAddr:       ":10120",
		OxigraphQueryURL: "http://localhost:7878/query",
		GraphMarkerFile:  "/repo/.sensei/graph-authority.json",
		RepoRoot:         "/repo",
		RepoDomain:       "github.com/owner/repo",
		StartedAtUnix:    1700000000,
		SenseiVersion:    "1.4.0",
	}
	if err := Write(d); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, err := Read(KindAwarenessGraph, ":10120")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got != d {
		t.Fatalf("got %+v, want %+v", got, d)
	}
}

func TestRead_AbsentWhenNeverWritten(t *testing.T) {
	withHomeDir(t)
	if _, err := Read(KindOxigraph, ":7878"); err != ErrAbsent {
		t.Fatalf("expected ErrAbsent, got %v", err)
	}
}

func TestRead_SelfHealsDeadPIDDescriptor(t *testing.T) {
	withHomeDir(t)
	// A PID astronomically unlikely to be alive on any real system (and
	// zero/negative PIDs are already rejected by isProcessAlive itself).
	const deadPID = 999999999
	d := Descriptor{Kind: KindOxigraph, PID: deadPID, ListenAddr: "127.0.0.1:7878"}
	if err := Write(d); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if _, err := Read(KindOxigraph, "127.0.0.1:7878"); err != ErrAbsent {
		t.Fatalf("expected ErrAbsent for a dead-PID descriptor, got %v", err)
	}
	// Self-healing: the stale file must be removed so nothing lingers.
	path, err := pathFor(KindOxigraph, "127.0.0.1:7878")
	if err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(path); statErr == nil {
		t.Fatal("expected the stale descriptor file to be removed on self-heal")
	}
}

func TestRead_SelfHealsCorruptDescriptor(t *testing.T) {
	withHomeDir(t)
	path, err := pathFor(KindOxigraph, "127.0.0.1:7878")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(KindOxigraph, "127.0.0.1:7878"); err != ErrAbsent {
		t.Fatalf("expected ErrAbsent for a corrupt descriptor, got %v", err)
	}
	if _, statErr := os.Stat(path); statErr == nil {
		t.Fatal("expected the corrupt descriptor file to be removed on self-heal")
	}
}

func TestRemove_MissingFileIsNotAnError(t *testing.T) {
	withHomeDir(t)
	if err := Remove(KindAwarenessGraph, ":10120"); err != nil {
		t.Fatalf("Remove of a never-written descriptor must not error, got %v", err)
	}
}

func TestPathFor_DerivesFromKindAndAddrNotCheckout(t *testing.T) {
	withHomeDir(t)
	p1, err := pathFor(KindAwarenessGraph, ":10120")
	if err != nil {
		t.Fatal(err)
	}
	p2, err := pathFor(KindAwarenessGraph, ":10120")
	if err != nil {
		t.Fatal(err)
	}
	if p1 != p2 {
		t.Fatalf("pathFor must be a pure function of (kind, addr): %q != %q", p1, p2)
	}
	p3, err := pathFor(KindOxigraph, ":10120")
	if err != nil {
		t.Fatal(err)
	}
	if p3 == p1 {
		t.Fatal("different kinds at the same address must not collide on the same path")
	}
}

func TestWrite_RequiresPIDAndListenAddr(t *testing.T) {
	withHomeDir(t)
	if err := Write(Descriptor{Kind: KindOxigraph, ListenAddr: ":7878"}); err == nil {
		t.Fatal("expected an error when PID is unset")
	}
	if err := Write(Descriptor{Kind: KindOxigraph, PID: os.Getpid()}); err == nil {
		t.Fatal("expected an error when ListenAddr is unset")
	}
}

// isProcessAlive itself, exercised directly: the current test process must
// be alive, and a real, deliberately-exited child process must not be.
func TestIsProcessAlive(t *testing.T) {
	if !isProcessAlive(os.Getpid()) {
		t.Fatal("expected the current process to report alive")
	}
	name, args := trueCommand()
	cmd := exec.Command(name, args...)
	if err := cmd.Run(); err != nil {
		t.Skipf("could not run a short-lived helper process: %v", err)
	}
	if isProcessAlive(cmd.Process.Pid) {
		t.Fatal("expected a process that has already exited to report not alive")
	}
}
