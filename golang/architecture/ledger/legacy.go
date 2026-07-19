// SPDX-License-Identifier: AGPL-3.0-only

package ledger

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"gopkg.in/yaml.v3"
)

var legacyImportLimitations = []string{
	"legacy_task_session",
	"scope_verified_legacy",
	"typed_actor_unavailable",
	"authority_resolution_unavailable",
	"capability_consumption_unavailable",
	"certification_unavailable_legacy",
	"terminal_completion_unavailable",
}

func ImportLegacyTask(taskDir string, opts ImportOptions) (ImportResult, error) {
	taskDir, err := filepath.Abs(taskDir)
	if err != nil {
		return ImportResult{}, err
	}
	if strings.TrimSpace(opts.ProducerID) == "" {
		opts.ProducerID = "sensei task-ledger import-legacy"
	}
	if opts.ProducedAt.IsZero() {
		opts.ProducedAt = time.Now().UTC()
	}
	taskID, err := legacyTaskID(taskDir)
	if err != nil {
		return ImportResult{}, err
	}
	sessionID := "session.legacy." + shortDigest(taskID)
	store := NewStore(taskDir, WithPayloadValidator(func(eventType closureprotocol.LedgerEventType, mediaType string, data []byte) error {
		return ValidateTaskEventPayload(eventType, data)
	}))
	if report, err := store.Verify(); err == nil && report.Valid && report.EntryCount > 0 {
		chain, loadErr := loadVerifiedChain(taskDir, store.payloadValidator)
		if loadErr == nil && len(chain.Entries) == 1 && chain.Entries[0].Entry.EventType == closureprotocol.LedgerEventLegacyImport {
			return ImportResult{TaskID: taskID, SessionID: sessionID, Head: chain.Head, Replay: true, Limitations: append([]string{}, legacyImportLimitations...)}, nil
		}
	}
	refs, err := storeLegacyArtifacts(taskDir)
	if err != nil {
		return ImportResult{}, err
	}
	payload := TaskEventPayload{
		SchemaVersion: EventPayloadSchemaVersion,
		EventType:     closureprotocol.LedgerEventLegacyImport,
		TaskID:        taskID,
		SessionID:     sessionID,
		Artifacts:     refs,
		Limitations:   append([]string{}, legacyImportLimitations...),
	}
	res, err := store.Append(context.Background(), AppendRequest{
		TaskID: taskID, SessionID: sessionID, ExpectedHeadDigestSHA256: "",
		EventType: closureprotocol.LedgerEventLegacyImport,
		Payload:   payload, PayloadMediaType: "application/yaml",
		ProducerID: opts.ProducerID, ProducedAt: opts.ProducedAt,
	})
	if err != nil {
		return ImportResult{}, err
	}
	if _, err := RebuildProjections(taskDir, store.payloadValidator); err != nil {
		return ImportResult{}, err
	}
	return ImportResult{TaskID: taskID, SessionID: sessionID, Head: res.Head, Limitations: append([]string{}, legacyImportLimitations...)}, nil
}

func storeLegacyArtifacts(taskDir string) (map[string]closureprotocol.LedgerPayloadRef, error) {
	paths := map[string]string{
		"session":         "session.yaml",
		"task_request":    "task-request.yaml",
		"closure_request": "closure-request.yaml",
		"task_control":    "control/latest.yaml",
		"status":          "receipts/task-status.yaml",
	}
	refs := map[string]closureprotocol.LedgerPayloadRef{}
	for name, rel := range paths {
		path := filepath.Join(taskDir, filepath.FromSlash(rel))
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		sum := sha256.Sum256(data)
		digest := hex.EncodeToString(sum[:])
		ref := closureprotocol.LedgerPayloadRef{
			Path:         filepath.ToSlash(filepath.Join("artifacts", "sha256", digest+".yaml")),
			MediaType:    "application/yaml",
			DigestSHA256: digest,
		}
		if err := storePayloadArtifacts(taskDir, renderedPayload{
			semanticDigest: digest,
			byteDigest:     digest,
			mediaType:      ref.MediaType,
			path:           ref.Path,
			data:           data,
		}); err != nil {
			return nil, err
		}
		refs[name] = ref
	}
	return refs, nil
}

func legacyTaskID(taskDir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(taskDir, "session.yaml"))
	if err != nil {
		return "", err
	}
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return "", err
	}
	if env, ok := raw["architecture_task_session"].(map[string]any); ok {
		if taskID, ok := env["task_id"].(string); ok && strings.TrimSpace(taskID) != "" {
			return strings.TrimSpace(taskID), nil
		}
	}
	return "", fmt.Errorf("legacy task session missing task_id")
}

func shortDigest(v string) string {
	sum := sha256.Sum256([]byte(v))
	return hex.EncodeToString(sum[:])[:12]
}
