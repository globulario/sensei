// SPDX-License-Identifier: Apache-2.0

package main

import (
	"path/filepath"
	"testing"

	"github.com/globulario/sensei/golang/evidence"
	awarenesspb "github.com/globulario/sensei/golang/pb"
)

func TestDecisionFromCode(t *testing.T) {
	cases := []struct {
		code, warns int
		want        string
	}{
		{1, 0, evidence.DecisionBlock},
		{2, 0, evidence.DecisionCannotVerify},
		{0, 3, evidence.DecisionWarn},
		{0, 0, evidence.DecisionAllow},
	}
	for _, c := range cases {
		if got := decisionFromCode(c.code, c.warns); got != c.want {
			t.Errorf("decisionFromCode(%d,%d)=%q want %q", c.code, c.warns, got, c.want)
		}
	}
}

func TestReportDecision(t *testing.T) {
	if reportDecision(1, 0) != evidence.DecisionWouldBlock {
		t.Error("wouldBlock>0 must be would_block")
	}
	if reportDecision(0, 2) != evidence.DecisionWarn {
		t.Error("warns>0 must be warn")
	}
	if reportDecision(0, 0) != evidence.DecisionAllow {
		t.Error("clean must be allow")
	}
}

// emitGateEvent must record a catch with the blocking rule ids, and must be a
// no-op when no ledger path is set.
func TestEmitGateEvent(t *testing.T) {
	findings := []fileFinding{{
		File: "pkg/x.go",
		Warnings: []*awarenesspb.EditWarning{
			{RuleId: "rule.block", Enforcement: "block"},
			{RuleId: "rule.warn", Enforcement: "warn"},
		},
	}}

	// no-op when path empty
	emitGateEvent("", "repoA", "HEAD", true, evidence.DecisionBlock, findings, []string{"pkg/x.go"})

	path := filepath.Join(t.TempDir(), "ledger.jsonl")
	emitGateEvent(path, "repoA", "origin/main...HEAD", true, evidence.DecisionBlock, findings, []string{"pkg/x.go"})

	got, err := evidence.Load(path)
	if err != nil || len(got) != 1 {
		t.Fatalf("expected 1 event, got %d (%v)", len(got), err)
	}
	ev := got[0]
	if ev.Tool != "gate" || ev.Repo != "repoA" || ev.Decision != evidence.DecisionBlock || !ev.Enforced {
		t.Errorf("event fields wrong: %+v", ev)
	}
	if len(ev.BlockedRules) != 1 || ev.BlockedRules[0] != "rule.block" {
		t.Errorf("blocked rules wrong: %v", ev.BlockedRules)
	}
	if len(ev.WarnedRules) != 1 || ev.WarnedRules[0] != "rule.warn" {
		t.Errorf("warned rules wrong: %v", ev.WarnedRules)
	}
}

func TestRunEvidence_RequiresLedgerPath(t *testing.T) {
	t.Setenv("AWG_EVENT_LOG", "")
	if code := runEvidence(nil); code != 2 {
		t.Errorf("no ledger path must exit 2, got %d", code)
	}
}
