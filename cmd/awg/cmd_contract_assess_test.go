// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

func TestRunContractAssess_Text(t *testing.T) {
	code, stdout, stderr := captureContractAssess(t, []string{
		"--governing-test",
		"--direct-source-annotation", "2",
		"--existing-tests-proving-behavior", "4",
		"--repeated-implementation-pattern", "1",
		"--ownership-authority-path", "3",
		"--failure-mode-or-incident-history", "1",
		"--nearby-human-intent", "2",
		"--cross-repo-consistency", "1",
		"--absence-of-conflicting-contracts", "2",
	})
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, "Outcome: contract-synthesis-safe") {
		t.Fatalf("stdout missing safe outcome:\n%s", stdout)
	}
	if !strings.Contains(stdout, "Required actions:\n  - draft-contract-with-citations") {
		t.Fatalf("stdout missing required action:\n%s", stdout)
	}
	if stderr != "" {
		t.Fatalf("unexpected stderr: %q", stderr)
	}
}

func TestRunContractAssess_JSON(t *testing.T) {
	code, stdout, stderr := captureContractAssess(t, []string{
		"--json",
		"--direct-source-annotation", "3",
		"--existing-tests-proving-behavior", "2",
		"--repeated-implementation-pattern", "2",
		"--ownership-authority-path", "3",
		"--failure-mode-or-incident-history", "2",
		"--nearby-human-intent", "3",
		"--cross-repo-consistency", "2",
		"--absence-of-conflicting-contracts", "3",
	})
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, `"outcome": "contract-proposal-only"`) {
		t.Fatalf("stdout missing proposal-only outcome:\n%s", stdout)
	}
	if !strings.Contains(stdout, `"required_actions": [`) {
		t.Fatalf("stdout missing required_actions:\n%s", stdout)
	}
	if stderr != "" {
		t.Fatalf("unexpected stderr: %q", stderr)
	}
}

func TestRunContractAssess_InvalidBlocker(t *testing.T) {
	code, _, stderr := captureContractAssess(t, []string{"--blocker", "made-up"})
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if !strings.Contains(stderr, `unknown blocker "made-up"`) {
		t.Fatalf("stderr missing blocker error: %q", stderr)
	}
}

func captureContractAssess(t *testing.T, args []string) (int, string, string) {
	t.Helper()

	oldStdout := os.Stdout
	oldStderr := os.Stderr
	defer func() {
		os.Stdout = oldStdout
		os.Stderr = oldStderr
	}()

	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stderr pipe: %v", err)
	}

	os.Stdout = outW
	os.Stderr = errW

	code := runContractAssess(args)

	_ = outW.Close()
	_ = errW.Close()

	var stdoutBuf bytes.Buffer
	var stderrBuf bytes.Buffer
	_, _ = io.Copy(&stdoutBuf, outR)
	_, _ = io.Copy(&stderrBuf, errR)
	_ = outR.Close()
	_ = errR.Close()

	return code, stdoutBuf.String(), stderrBuf.String()
}
