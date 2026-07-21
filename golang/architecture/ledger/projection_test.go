// SPDX-License-Identifier: AGPL-3.0-only

package ledger

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

func TestRebuildProjectionsIsDeterministicAndRepairsDrift(t *testing.T) {
	taskDir := t.TempDir()
	store := NewStore(taskDir, WithPayloadValidator(func(eventType closureprotocol.LedgerEventType, mediaType string, data []byte) error {
		return ValidateTaskEventPayload(eventType, data)
	}))
	sessionRef := storeArtifactForTest(t, taskDir, "schema_version: \"1\"\ntask_id: task.example\n")
	controlRef := storeArtifactForTest(t, taskDir, "schema_version: \"1\"\nnext:\n  action: perform admitted edit\n")
	statusRef := storeArtifactForTest(t, taskDir, "schema_version: \"1\"\nstatus: ready_for_mutation\n")
	payload := TaskEventPayload{
		SchemaVersion: EventPayloadSchemaVersion,
		EventType:     closureprotocol.LedgerEventTaskControlProjected,
		TaskID:        "task.example",
		SessionID:     "session.example",
		Artifacts: map[string]closureprotocol.LedgerPayloadRef{
			"session": sessionRef, "task_control": controlRef, "status": statusRef,
		},
	}
	if _, err := store.Append(context.Background(), AppendRequest{
		TaskID: "task.example", SessionID: "session.example", ExpectedHeadDigestSHA256: "",
		EventType: closureprotocol.LedgerEventTaskControlProjected,
		Payload:   payload, PayloadMediaType: "application/yaml", ProducerID: "sensei.test",
		ProducedAt: time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatal(err)
	}
	setA, err := RebuildProjections(taskDir, store.payloadValidator)
	if err != nil {
		t.Fatal(err)
	}
	digestA := projectionDigest(t, setA)
	if state := ProjectionState(taskDir, setA); state != "current" {
		t.Fatalf("projection state=%s want current", state)
	}
	if err := os.WriteFile(filepath.Join(taskDir, "session.yaml"), []byte("tampered\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if state := ProjectionState(taskDir, setA); state != "projection_drift" {
		t.Fatalf("projection state=%s want projection_drift", state)
	}
	if _, err := RebuildProjections(taskDir, store.payloadValidator); err != nil {
		t.Fatal(err)
	}
	setB, err := RebuildProjections(taskDir, store.payloadValidator)
	if err != nil {
		t.Fatal(err)
	}
	if digestA != projectionDigest(t, setB) {
		t.Fatal("projection rebuild changed bytes")
	}
}

func projectionDigest(t *testing.T, set ProjectionSet) string {
	t.Helper()
	data, err := buildProjectionManifest("task.example", "head", set.Files)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func storeArtifactForTest(t *testing.T, taskDir, content string) closureprotocol.LedgerPayloadRef {
	t.Helper()
	rendered, err := renderPayload([]byte(content), "application/yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := storePayloadArtifacts(taskDir, rendered); err != nil {
		t.Fatal(err)
	}
	return closureprotocol.LedgerPayloadRef{Path: rendered.path, MediaType: rendered.mediaType, DigestSHA256: rendered.semanticDigest}
}
