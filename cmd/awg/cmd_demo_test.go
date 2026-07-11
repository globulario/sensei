// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCleanBriefing_DropsProvenanceKeepsKnowledge(t *testing.T) {
	in := "Status: BRIEFING_STATUS_OK\n" +
		"Authority: authoritative (current)\n" +
		"  Live digest:  abc123\n" +
		"  Tx awg:       def456\n" +
		"  Detail:       live store matches\n" +
		"\nDecision focus:\n- Respect: [critical] payments.paid_state\n"
	got := cleanBriefing(in)
	for _, noise := range []string{"Live digest", "Tx awg", "Detail:"} {
		if strings.Contains(got, noise) {
			t.Errorf("cleanBriefing kept noise %q:\n%s", noise, got)
		}
	}
	for _, keep := range []string{"Status: BRIEFING_STATUS_OK", "Authority:", "Decision focus", "payments.paid_state"} {
		if !strings.Contains(got, keep) {
			t.Errorf("cleanBriefing dropped %q:\n%s", keep, got)
		}
	}
}

func TestDescribeTriples(t *testing.T) {
	if got := describeTriples("loaded 12650 bytes (hash, 15 triples)"); got != "15 triples" {
		t.Errorf("describeTriples = %q, want 15 triples", got)
	}
	if got := describeTriples("no number here"); got != "graph compiled" {
		t.Errorf("describeTriples fallback = %q", got)
	}
}

func TestFirstHighRiskFile(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "high_risk_files.yaml"), []byte("files:\n  - src/payment_processor.py\n  - src/other.py\n"), 0o644)
	if got := firstHighRiskFile(dir); got != "src/payment_processor.py" {
		t.Errorf("firstHighRiskFile = %q", got)
	}
}
