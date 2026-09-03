// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/evidence"
	awarenesspb "github.com/globulario/sensei/golang/pb"
	"github.com/globulario/sensei/golang/statedir"
)

// runBriefHook drives the hook exactly as Claude Code does: a PreToolUse
// payload on stdin, nothing else. No manual briefing call anywhere.
func runBriefHook(t *testing.T, root, file string, extra ...string) string {
	t.Helper()
	oldIn, oldOut := os.Stdin, os.Stdout
	inR, inW, _ := os.Pipe()
	outR, outW, _ := os.Pipe()
	os.Stdin, os.Stdout = inR, outW
	defer func() { os.Stdin, os.Stdout = oldIn, oldOut }()

	b, _ := json.Marshal(map[string]any{
		"tool_name":  "Edit",
		"tool_input": map[string]any{"file_path": file, "new_string": "x := 1"},
	})
	go func() { _, _ = inW.Write(b); _ = inW.Close() }()
	if code := runEditBrief(append([]string{"--root", root}, extra...)); code != 0 {
		t.Fatalf("hook exit = %d, want 0: a push must never block an edit", code)
	}
	_ = outW.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, outR)
	return buf.String()
}

func briefLedger(t *testing.T, root string) []evidence.Event {
	t.Helper()
	evs, err := evidence.Load(statedir.Path(root, "briefing-delivery.jsonl"))
	if err != nil {
		t.Fatalf("load ledger: %v", err)
	}
	return evs
}

// THE HOOK DELIVERS GOVERNING LAW WITHOUT ANYONE ASKING FOR IT.
//
// This is the criterion-5 mechanism, and the ordering is the claim: the agent
// makes no briefing call, the hook fires on the edit payload alone, and the
// law reaches the agent BEFORE the write. Measured three times on 2026-09-02,
// the governing law for a defect was the only thing the graph said about the
// file it was written in -- rank 1 of 1 -- and reached nobody, because nothing
// delivered it.
func TestTheHookDeliversGoverningLawWithoutBeingAsked(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs", "awareness"), 0o755); err != nil {
		t.Fatal(err)
	}
	const law = "FAILURE MODE failure.sensei.a_decision_surface_reported_current_while_the_admitted_knowl"

	var askedFile string
	editBriefRPC = func(_ context.Context, _, file, _, _ string) (editBriefOutcome, error) {
		askedFile = file
		return editBriefOutcome{
			Prose:      law,
			Status:     awarenesspb.BriefingStatus_BRIEFING_STATUS_OK,
			Referenced: []string{"failure_mode:failure.sensei.a_decision_surface_reported_current_while_the_admitted_knowl"},
			Generation: "gen-abc123",
		}, nil
	}

	out := runBriefHook(t, root, filepath.Join(root, "cmd", "awg", "authority_output.go"))
	if !strings.Contains(out, law) {
		t.Fatalf("the governing law did not reach the agent:\n%s", out)
	}
	if !strings.Contains(out, `"permissionDecision":"allow"`) {
		t.Errorf("push did not allow the edit: %s", out)
	}
	if askedFile != "cmd/awg/authority_output.go" {
		t.Errorf("briefed %q, want the repo-relative edited file", askedFile)
	}

	// The evidence must establish the ORDERING, not merely that a briefing
	// happened: delivered, for this file, from a named generation.
	evs := briefLedger(t, root)
	if len(evs) != 1 {
		t.Fatalf("ledger has %d event(s), want 1", len(evs))
	}
	e := evs[0]
	if e.Tool != "edit-brief" || !e.Delivered {
		t.Errorf("event does not record a delivery: %+v", e)
	}
	if e.Status != "BRIEFING_STATUS_OK" {
		t.Errorf("status = %q; the server's classification must be recorded verbatim", e.Status)
	}
	if len(e.Surfaced) != 1 || e.GraphGeneration != "gen-abc123" {
		t.Errorf("surfaced law or generation not recorded: %+v", e)
	}
	if e.Files[0] != "cmd/awg/authority_output.go" {
		t.Errorf("file = %v", e.Files)
	}
}

// NOISE IS NOT DELIVERY, AND IT IS NOT SILENCE EITHER.
//
// CONTEXT_ONLY is excluded on the vocabulary's own terms -- "naming a file's
// symbols is not knowledge about it". A hook that interrupts every edit with
// symbol lists teaches an agent to skim past Sensei, which is worse than never
// interrupting. EMPTY carries nothing by definition.
//
// The closed set is read by MEMBERSHIP: an unset/unknown status is withheld
// too, because "deliver unless EMPTY" would deliver a status added later as
// though it governed the file.
func TestOnlyGoverningStatusesInterruptTheEdit(t *testing.T) {
	for _, tc := range []struct {
		status  awarenesspb.BriefingStatus
		deliver bool
	}{
		{awarenesspb.BriefingStatus_BRIEFING_STATUS_OK, true},
		{awarenesspb.BriefingStatus_BRIEFING_STATUS_INFERRED_ONLY, true},
		{awarenesspb.BriefingStatus_BRIEFING_STATUS_DEGRADED, true},
		{awarenesspb.BriefingStatus_BRIEFING_STATUS_CONTEXT_ONLY, false},
		{awarenesspb.BriefingStatus_BRIEFING_STATUS_EMPTY, false},
		{awarenesspb.BriefingStatus(99), false},
	} {
		t.Run(tc.status.String(), func(t *testing.T) {
			root := t.TempDir()
			editBriefRPC = func(_ context.Context, _, _, _, _ string) (editBriefOutcome, error) {
				return editBriefOutcome{Prose: "some prose", Status: tc.status}, nil
			}
			out := runBriefHook(t, root, filepath.Join(root, "a.go"))
			delivered := strings.TrimSpace(out) != ""
			if delivered != tc.deliver {
				t.Fatalf("delivered=%v want %v for %s: output %q", delivered, tc.deliver, tc.status, out)
			}
			// Withheld or not, the attempt is recorded. An opportunity that
			// produced no delivery is the row a delivery count would drop.
			evs := briefLedger(t, root)
			if len(evs) != 1 {
				t.Fatalf("ledger has %d event(s), want 1 whether or not it delivered", len(evs))
			}
			if evs[0].Delivered != tc.deliver {
				t.Errorf("ledger delivered=%v want %v", evs[0].Delivered, tc.deliver)
			}
			if !tc.deliver && strings.TrimSpace(evs[0].Reason) == "" {
				t.Error("a withheld briefing recorded no reason, so the measurement " +
					"cannot separate noise-suppression from a backend failure")
			}
		})
	}
}

// AN UNREACHABLE BACKEND IS AN OPPORTUNITY, NOT AN ABSENCE.
func TestARefusedBriefingIsRecordedAsAMissedOpportunity(t *testing.T) {
	root := t.TempDir()
	editBriefRPC = func(_ context.Context, _, _, _, _ string) (editBriefOutcome, error) {
		return editBriefOutcome{}, context.DeadlineExceeded
	}
	if out := runBriefHook(t, root, filepath.Join(root, "a.go")); strings.TrimSpace(out) != "" {
		t.Fatalf("a failed briefing annotated the edit anyway: %q", out)
	}
	evs := briefLedger(t, root)
	if len(evs) != 1 || evs[0].Delivered {
		t.Fatalf("a refusal produced no undelivered row: %+v", evs)
	}
	if !strings.Contains(strings.ToLower(evs[0].Reason), "deadline") {
		t.Errorf("refusal reason not preserved: %q", evs[0].Reason)
	}
}

// THE PROJECT'S CONFIG NAMES THE INSTANCE.
//
// The flag default is one repository's port. A hook using it answers from
// whichever project owns that port -- on the machine this was measured, a
// different repository's domain entirely -- and a briefing served from the
// wrong graph is worse than none.
func TestTheHookUsesThisProjectsConfiguredInstance(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(statedir.Path(root), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := "server:\n    addr: localhost:19999\nrepository:\n    domain: github.com/example/proj\n"
	if err := os.WriteFile(statedir.Path(root, "config.yaml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}

	var gotAddr, gotDomain string
	editBriefRPC = func(_ context.Context, addr, _, _, domain string) (editBriefOutcome, error) {
		gotAddr, gotDomain = addr, domain
		return editBriefOutcome{Prose: "p", Status: awarenesspb.BriefingStatus_BRIEFING_STATUS_OK}, nil
	}
	runBriefHook(t, root, filepath.Join(root, "a.go"))
	if gotAddr != "localhost:19999" {
		t.Errorf("addr = %q; the project config names the instance, not the flag default", gotAddr)
	}
	if gotDomain != "github.com/example/proj" {
		t.Errorf("domain = %q; the project config names the domain", gotDomain)
	}

	// An explicit flag is the operator naming the endpoint at the point of
	// use, and still wins over configuration.
	runBriefHook(t, root, filepath.Join(root, "a.go"), "--addr", "localhost:18888")
	if gotAddr != "localhost:18888" {
		t.Errorf("addr = %q; an explicit -addr must win over config", gotAddr)
	}
}

// A DEGRADED SUBSYSTEM MUST NOT MAKE EVERY FILE LOOK GOVERNED.
//
// `status` folds the feedback subsystem's availability into the same field as
// the file's coverage, so a server running without --repo-root returns
// DEGRADED for every file. Measured 2026-09-02: the hook could not tell the
// file its graph governs by one direct anchor from a README it knows nothing
// about, and delivered both. file_status is the file's own verdict preserved,
// and it is what decides delivery.
func TestABackendDegradationDoesNotPromoteAnUngovernedFile(t *testing.T) {
	root := t.TempDir()
	editBriefRPC = func(_ context.Context, _, _, _, _ string) (editBriefOutcome, error) {
		return editBriefOutcome{
			Prose:  "code context only",
			Status: awarenesspb.BriefingStatus_BRIEFING_STATUS_CONTEXT_ONLY, // the FILE's verdict
			Wire:   awarenesspb.BriefingStatus_BRIEFING_STATUS_DEGRADED,     // what the wire said
		}, nil
	}
	if out := runBriefHook(t, root, filepath.Join(root, "README.md")); strings.TrimSpace(out) != "" {
		t.Fatalf("an ungoverned file was interrupted because the backend was degraded:\n%s", out)
	}
	evs := briefLedger(t, root)
	if len(evs) != 1 {
		t.Fatalf("ledger has %d event(s), want 1", len(evs))
	}
	if evs[0].Status != "BRIEFING_STATUS_CONTEXT_ONLY" {
		t.Errorf("recorded status = %q, want the FILE's verdict", evs[0].Status)
	}
	if evs[0].WireStatus != "BRIEFING_STATUS_DEGRADED" {
		t.Errorf("wire status = %q; a campaign must be able to tell an ungoverned file "+
			"from a degraded backend", evs[0].WireStatus)
	}
}

// THE SELECTION ITSELF, not a stub of it.
//
// The delivery tests above replace editBriefRPC wholesale, so they never
// execute the line that chooses between the wire status and the file status.
// A mutation deleting that choice survived every one of them — the exact
// non-execution false green this repository keeps recording. This drives the
// choice directly.
//
// file_status is `optional`, so absence is a distinct input from any value.
// That is what lets a GOVERNED file stay OK while the feedback subsystem is
// degraded; a presence-blind version had to take the weaker claim and made
// direct-anchor hits uncountable.
func TestPreferFileStatusKeepsTheFileVerdictAndDistrustsAnAbsentField(t *testing.T) {
	const (
		ok       = awarenesspb.BriefingStatus_BRIEFING_STATUS_OK
		degraded = awarenesspb.BriefingStatus_BRIEFING_STATUS_DEGRADED
		ctxOnly  = awarenesspb.BriefingStatus_BRIEFING_STATUS_CONTEXT_ONLY
		inferred = awarenesspb.BriefingStatus_BRIEFING_STATUS_INFERRED_ONLY
		empty    = awarenesspb.BriefingStatus_BRIEFING_STATUS_EMPTY
	)
	ptr := func(s awarenesspb.BriefingStatus) *awarenesspb.BriefingStatus { return &s }
	for _, tc := range []struct {
		name string
		wire awarenesspb.BriefingStatus
		file *awarenesspb.BriefingStatus
		want awarenesspb.BriefingStatus
	}{
		// The case the field exists for: an ungoverned file behind a degraded
		// backend must stay ungoverned.
		{"ungoverned file, degraded backend", degraded, ptr(ctxOnly), ctxOnly},
		{"inferred file, degraded backend", degraded, ptr(inferred), inferred},
		{"empty file, degraded backend", degraded, ptr(empty), empty},
		// And the one a presence-blind version could not express: a GOVERNED
		// file behind a degraded backend is reported OK, so direct-anchor hits
		// stay countable instead of all collapsing to DEGRADED.
		{"governed file, degraded backend", degraded, ptr(ok), ok},
		{"governed file, healthy backend", ok, ptr(ok), ok},
		// Absent, not zero. An older server reports no file verdict; reading
		// the zero value would say OK and deliver on every edit.
		{"old server: absent field falls back to the wire", ctxOnly, nil, ctxOnly},
		{"old server, empty", empty, nil, empty},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := preferFileStatus(tc.wire, tc.file); got != tc.want {
				t.Fatalf("preferFileStatus(%v, %v) = %v, want %v", tc.wire, tc.file, got, tc.want)
			}
		})
	}
}
