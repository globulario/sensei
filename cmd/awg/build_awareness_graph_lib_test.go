// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runBuildGraphLib(t *testing.T, expr string) string {
	t.Helper()
	lib := filepath.Join("..", "..", "scripts", "build-awareness-graph-lib.sh")
	cmd := exec.Command("bash", "-lc", ". "+lib+"; "+expr)
	cmd.Dir = filepath.Join("..", "..", "cmd", "awg")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bash expr failed: %v\n%s", err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestGeneratedOutputOwner(t *testing.T) {
	const (
		agGenerated  = "/repo/docs/awareness/generated"
		svcGenerated = "/services/docs/awareness/generated"
	)

	if got := runBuildGraphLib(t, "generated_output_owner '"+agGenerated+"' '"+agGenerated+"' '"+svcGenerated+"'"); got != "awareness-graph" {
		t.Fatalf("awareness-graph owner = %q", got)
	}
	if got := runBuildGraphLib(t, "generated_output_owner '"+svcGenerated+"' '"+agGenerated+"' '"+svcGenerated+"'"); got != "services" {
		t.Fatalf("services owner = %q", got)
	}
	if got := runBuildGraphLib(t, "generated_output_owner '/tmp/elsewhere' '"+agGenerated+"' '"+svcGenerated+"'"); got != "external" {
		t.Fatalf("external owner = %q", got)
	}
}

func TestGeneratedOutputBlocksCheck(t *testing.T) {
	const (
		agGenerated  = "/repo/docs/awareness/generated"
		svcGenerated = "/services/docs/awareness/generated"
	)

	if got := runBuildGraphLib(t, "generated_output_blocks_check '"+agGenerated+"' '"+agGenerated+"' '"+svcGenerated+"' && echo yes || echo no"); got != "yes" {
		t.Fatalf("awareness-graph output must block check, got %q", got)
	}
	if got := runBuildGraphLib(t, "generated_output_blocks_check '"+svcGenerated+"' '"+agGenerated+"' '"+svcGenerated+"' && echo yes || echo no"); got != "no" {
		t.Fatalf("services output must not block check, got %q", got)
	}
}
