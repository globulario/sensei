// SPDX-License-Identifier: Apache-2.0

package probe

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/maintenance"
)

func TestRecordProbeResultHashesArtifactAndUpdatesEvidenceState(t *testing.T) {
	artifact := filepath.Join(t.TempDir(), "probe.txt")
	if err := os.WriteFile(artifact, []byte("probe output\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := testProbe()
	doc, err := NormalizeProbeDocument(testProbeDocument(p), nil)
	if err != nil {
		t.Fatalf("NormalizeProbeDocument: %v", err)
	}
	claims := architecture.ClaimDocument{
		Binding: testBinding(),
		Claims: []architecture.Claim{{
			ID:                 "claim.config_writer",
			SupportingEvidence: []string{"evidence:config_writer_test"},
		}},
	}
	graph := GraphIndex{
		Nodes: map[string]Node{
			"evidence:config_writer_test": {ID: "config_writer_test", Classes: []string{"evidence"}},
		},
		ByKey: map[string]string{"evidence:config_writer_test": "evidence:config_writer_test"},
	}
	state := maintenance.EvidenceStateDocument{Binding: testBinding()}
	res, err := Record(RecordContext{
		Probes:              doc,
		Claims:              &claims,
		Graph:               &graph,
		EvidenceState:       &state,
		ProbeDocumentDigest: strings.Repeat("d", 64),
	}, RecordOptions{
		ProbeID:           p.ID,
		ResultStatus:      ResultCompleted,
		ExecutedBy:        "tester",
		ObservedAt:        "2026-07-13T12:00:00Z",
		ApprovalReceipt:   "review:local",
		EvidenceStatus:    maintenance.EvidenceStatusPass,
		EvidenceFreshness: maintenance.EvidenceFreshnessCurrent,
		Artifacts:         []ArtifactInput{{Kind: "test_output", Path: artifact}},
	})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if len(res.Document.Results) != 1 || len(res.Document.Results[0].Artifacts) != 1 {
		t.Fatalf("result=%+v", res.Document.Results)
	}
	if got := res.Document.Results[0].Artifacts[0].SHA256; len(got) != 64 {
		t.Fatalf("artifact sha256=%q", got)
	}
	if res.Report.EvidenceStateDisposition != EvidenceStateCreated {
		t.Fatalf("evidence state disposition=%q", res.Report.EvidenceStateDisposition)
	}
	if res.EvidenceState == nil || len(res.EvidenceState.Evidence) != 1 || res.EvidenceState.Evidence[0].ID != "config_writer_test" {
		t.Fatalf("evidence state=%+v", res.EvidenceState)
	}
}

func TestRecordProbeResultRequiresApprovalReceiptForGatedProbe(t *testing.T) {
	p := testProbe()
	doc, err := NormalizeProbeDocument(testProbeDocument(p), nil)
	if err != nil {
		t.Fatalf("NormalizeProbeDocument: %v", err)
	}
	_, err = Record(RecordContext{
		Probes:              doc,
		ProbeDocumentDigest: strings.Repeat("d", 64),
	}, RecordOptions{
		ProbeID:           p.ID,
		ResultStatus:      ResultCompleted,
		ExecutedBy:        "tester",
		ObservedAt:        "2026-07-13T12:00:00Z",
		EvidenceStatus:    maintenance.EvidenceStatusPass,
		EvidenceFreshness: maintenance.EvidenceFreshnessCurrent,
	})
	if err == nil || !strings.Contains(err.Error(), "approval receipt required") {
		t.Fatalf("err=%v", err)
	}
}
