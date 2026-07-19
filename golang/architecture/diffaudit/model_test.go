// SPDX-License-Identifier: Apache-2.0

package diffaudit

import (
	"testing"
)

func TestAuditResult_ComputeDigest_Deterministic(t *testing.T) {
	res1 := AuditResult{
		Schema:          SchemaV1,
		InputDiffDigest: "abc123sha256",
		InputTrust:      TrustCaller,
		Availability:    AvailabilityAvailable,
		Decision:        DecisionPass,
		ChangedFiles: []ChangedFileSummary{
			{Path: "src/b.go", Kind: ChangeModify, LinesAdded: 5, LinesDeleted: 2},
			{Path: "src/a.go", Kind: ChangeAdd, LinesAdded: 10},
		},
		Findings: []AuditFinding{
			{RecordID: "inv-2", FilePath: "src/b.go", Explanation: "rule 2"},
			{RecordID: "inv-1", FilePath: "src/a.go", Explanation: "rule 1"},
		},
		ReasonCodes: []ReasonCode{},
	}

	res2 := AuditResult{
		Schema:          SchemaV1,
		InputDiffDigest: "abc123sha256",
		InputTrust:      TrustCaller,
		Availability:    AvailabilityAvailable,
		Decision:        DecisionPass,
		ChangedFiles: []ChangedFileSummary{
			{Path: "src/a.go", Kind: ChangeAdd, LinesAdded: 10},
			{Path: "src/b.go", Kind: ChangeModify, LinesAdded: 5, LinesDeleted: 2},
		},
		Findings: []AuditFinding{
			{RecordID: "inv-1", FilePath: "src/a.go", Explanation: "rule 1"},
			{RecordID: "inv-2", FilePath: "src/b.go", Explanation: "rule 2"},
		},
		ReasonCodes: []ReasonCode{},
	}

	d1, err := res1.ComputeDigest()
	if err != nil {
		t.Fatalf("res1 digest: %v", err)
	}
	d2, err := res2.ComputeDigest()
	if err != nil {
		t.Fatalf("res2 digest: %v", err)
	}

	if d1 == "" {
		t.Fatal("computed digest is empty")
	}
	if d1 != d2 {
		t.Fatalf("expected deterministic digests for equivalent results: d1=%s, d2=%s", d1, d2)
	}
}
