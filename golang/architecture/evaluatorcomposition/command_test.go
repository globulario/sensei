// SPDX-License-Identifier: AGPL-3.0-only

package evaluatorcomposition

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOSCommandRunnerHelperProcess(t *testing.T) {
	if os.Getenv("SENSEI_O4_HELPER_PROCESS") != "1" {
		return
	}
	switch os.Getenv("SENSEI_O4_HELPER_MODE") {
	case "emit":
		_, _ = fmt.Fprint(os.Stdout, "abcdefghij")
		_, _ = fmt.Fprint(os.Stderr, "ABCDEFGHIJ")
	case "exit":
		_, _ = fmt.Fprint(os.Stderr, "intentional exit")
		os.Exit(7)
	case "sleep":
		time.Sleep(10 * time.Second)
	}
	os.Exit(0)
}

func commandHelperRequest(t *testing.T, mode string) CommandRequest {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return CommandRequest{
		Executable: executable,
		Args:       []string{"-test.run=TestOSCommandRunnerHelperProcess"},
		Env: append(os.Environ(),
			"SENSEI_O4_HELPER_PROCESS=1",
			"SENSEI_O4_HELPER_MODE="+mode,
		),
		Dir: t.TempDir(),
	}
}

func TestOSCommandRunnerClassifiesExitAndBoundsCombinedOutput(t *testing.T) {
	runner := OSCommandRunner{}
	emit, err := runner.Run(context.Background(), commandHelperRequest(t, "emit"), 12)
	if err != nil {
		t.Fatal(err)
	}
	if emit.Outcome != CommandOutcomeCompleted || emit.ExitCode != 0 {
		t.Fatalf("emit outcome = %q/%d", emit.Outcome, emit.ExitCode)
	}
	if !emit.Truncated {
		t.Fatal("combined stdout/stderr output exceeding the cap was not marked truncated")
	}
	if len(emit.Stdout)+len(emit.Stderr) != 12 {
		t.Fatalf("captured bytes = %d, want exactly 12", len(emit.Stdout)+len(emit.Stderr))
	}

	exited, err := runner.Run(context.Background(), commandHelperRequest(t, "exit"), 1024)
	if err != nil {
		t.Fatal(err)
	}
	if exited.Outcome != CommandOutcomeExited || exited.ExitCode != 7 {
		t.Fatalf("exit outcome = %q/%d, want exited/7", exited.Outcome, exited.ExitCode)
	}
	if !bytes.Contains(exited.Stderr, []byte("intentional exit")) {
		t.Fatalf("stderr did not preserve process evidence: %q", exited.Stderr)
	}
}

func TestOSCommandRunnerClassifiesDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	result, err := (OSCommandRunner{}).Run(ctx, commandHelperRequest(t, "sleep"), 1024)
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != CommandOutcomeTimedOut {
		t.Fatalf("outcome = %q, want %q", result.Outcome, CommandOutcomeTimedOut)
	}
	if result.ExitCode != -1 {
		t.Fatalf("timed-out exit code = %d, want -1", result.ExitCode)
	}
}

func TestEvidenceSinksAreContentAddressedAndRefuseConflictingBytes(t *testing.T) {
	content := []byte("bounded evaluator evidence")
	memory := NewMemoryEvidenceSink()
	first, err := memory.Put(context.Background(), content)
	if err != nil {
		t.Fatal(err)
	}
	second, err := memory.Put(context.Background(), append([]byte(nil), content...))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("same evidence bytes produced different references: %+v vs %+v", first, second)
	}
	stored, ok := memory.Get(first.DigestSHA256)
	if !ok || !bytes.Equal(stored, content) {
		t.Fatal("memory evidence sink did not retain exact bytes")
	}

	root := t.TempDir()
	fsSink, err := NewFSEvidenceSink(root)
	if err != nil {
		t.Fatal(err)
	}
	fsRef, err := fsSink.Put(context.Background(), content)
	if err != nil {
		t.Fatal(err)
	}
	if fsRef != first {
		t.Fatalf("filesystem and memory content addressing differ: %+v vs %+v", fsRef, first)
	}
	blob, err := os.ReadFile(filepath.Join(root, fsRef.DigestSHA256+".blob"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(blob, content) {
		t.Fatal("filesystem evidence sink persisted different bytes")
	}
	if _, err := fsSink.Put(context.Background(), content); err != nil {
		t.Fatalf("idempotent same-content Put failed: %v", err)
	}
}
