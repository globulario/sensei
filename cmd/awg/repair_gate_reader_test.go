// SPDX-License-Identifier: AGPL-3.0-only

package main

// ANTIGRAVITY FINDING (P1, cmd_repair_report.go:196): runRepairGate is not routed through
// productionReaderFor.
//
// cmd_repair_report.go holds TWO graph-reaching commands. runRepairReport was migrated to the
// G2 owner; runRepairGate was not, and #361 emptied its --addr default at the same time. So
// `sensei repair-gate` with no flag dialled "" -- a command that worked before this PR -- and
// with a flag dialled it directly, unresolved and without the non-canonical override notice.
//
// TestEveryGraphReachingCommandResolvesThroughTheOwner did not catch it because it enumerated
// FILES: runRepairReport's call satisfied the substring check for the whole file. That census
// is repaired alongside this, in the same commit, because the census IS the evidence that the
// migration is complete and it was unsound.
//
// These witnesses drive the real runGate entry point and observe the address the command
// actually dials, captured at repairReportMetadata -- the seam generateRepairReport uses. A
// source-text assertion would prove the text, not the run.

import (
	"context"
	"strings"
	"sync"
	"testing"

	awarenesspb "github.com/globulario/sensei/golang/pb"
)

// dialedAddr runs fn with the repair-report graph seams stubbed and returns the address the
// command handed them.
func dialedAddr(t *testing.T, fn func()) string {
	t.Helper()
	var mu sync.Mutex
	seen := ""
	record := func(addr string) {
		mu.Lock()
		defer mu.Unlock()
		if seen == "" {
			seen = addr
		}
	}
	prevMeta, prevEdit, prevPre := repairReportMetadata, repairReportEditCheck, repairReportPreflight
	t.Cleanup(func() {
		repairReportMetadata, repairReportEditCheck, repairReportPreflight = prevMeta, prevEdit, prevPre
	})
	repairReportMetadata = func(_ context.Context, addr, _ string) (*awarenesspb.MetadataResponse, error) {
		record(addr)
		return &awarenesspb.MetadataResponse{Authority: &awarenesspb.GraphAuthority{Authoritative: true}}, nil
	}
	repairReportEditCheck = func(_ context.Context, addr string, _ *awarenesspb.EditCheckRequest) (*awarenesspb.EditCheckResponse, error) {
		record(addr)
		return &awarenesspb.EditCheckResponse{}, nil
	}
	repairReportPreflight = func(_ context.Context, addr string, _ *awarenesspb.PreflightRequest) (*awarenesspb.PreflightResponse, error) {
		record(addr)
		return &awarenesspb.PreflightResponse{}, nil
	}
	fn()
	mu.Lock()
	defer mu.Unlock()
	return seen
}

func repairGateWorld(t *testing.T, configuredAddr string) string {
	t.Helper()
	root := projectRoot(t, t.TempDir())
	if configuredAddr != "" {
		writeProjectConfig(t, root, configuredAddr)
	}
	t.Chdir(root)
	return root
}

// 1. THE FINDING. With no --addr the command must dial the endpoint the owner resolves, never
// the empty string.
func TestRepairGateResolvesItsEndpointThroughTheOwner(t *testing.T) {
	root := repairGateWorld(t, "localhost:10122")
	got := dialedAddr(t, func() {
		_ = runRepairGate([]string{"--repo-root", root, "--task", "t", "--file", "golang/thing.go"})
	})
	if got == "" {
		t.Fatalf("repair-gate dialed the empty string: it never resolved its endpoint through the G2 owner")
	}
	if got != "localhost:10122" {
		t.Errorf("repair-gate dialed %q, want the endpoint the project config names", got)
	}
}

// 2. An explicit endpoint is honoured, and announced as a non-canonical override -- the
// override changes WHERE, never WHETHER resolution happened.
func TestRepairGateHonoursAnExplicitEndpointAndAnnouncesIt(t *testing.T) {
	root := repairGateWorld(t, "localhost:10122")
	var got string
	out := captureStderr(t, func() {
		got = dialedAddr(t, func() {
			_ = runRepairGate([]string{"--repo-root", root, "--task", "t", "--file", "golang/thing.go",
				"--addr", "localhost:19191", "--domain", "example.com/acme/thing"})
		})
	})
	if got != "localhost:19191" {
		t.Errorf("the explicit endpoint was not honoured: dialed %q", got)
	}
	if !strings.Contains(out, "non-canonical override") {
		t.Errorf("an explicitly named endpoint was not announced as non-canonical:\n%s", out)
	}
}

// 3. NO FALLBACK. With no project config and no flag the command still resolves through the
// owner -- which yields the owner's built-in default, the ONLY place that default may come
// from -- rather than inventing one locally or dialing nothing.
func TestRepairGateHasNoEndpointOfItsOwn(t *testing.T) {
	root := repairGateWorld(t, "")
	got := dialedAddr(t, func() {
		_ = runRepairGate([]string{"--repo-root", root, "--task", "t", "--file", "golang/thing.go"})
	})
	if got == "" {
		t.Fatalf("repair-gate dialed nothing")
	}
	if got != defaultServiceAddr() {
		t.Errorf("repair-gate dialed %q; with nothing configured the owner's default is the only admissible answer (%q)",
			got, defaultServiceAddr())
	}
}
