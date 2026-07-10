// SPDX-License-Identifier: Apache-2.0

package governancepack

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const ActivationLogRelativePath = ".awg/governance/activation-log.jsonl"

type ActivationLogEntry struct {
	TimestampUTC         string        `json:"timestamp_utc"`
	AttemptedPackID      string        `json:"attempted_pack_id,omitempty"`
	AttemptedPackVersion string        `json:"attempted_pack_version,omitempty"`
	PublisherID          string        `json:"publisher_id,omitempty"`
	ManifestDigestSHA256 string        `json:"manifest_digest_sha256,omitempty"`
	PayloadDigestSHA256  string        `json:"payload_digest_sha256,omitempty"`
	PreviousActivePack   *ActiveRecord `json:"previous_active_pack,omitempty"`
	Result               string        `json:"result"`
	FailureState         string        `json:"failure_state,omitempty"`
	FailureDetail        string        `json:"failure_detail,omitempty"`
	ResultingActivePack  *ActiveRecord `json:"resulting_active_pack,omitempty"`
}

func ActivationLogPath(root string) string {
	if root == "" {
		return ActivationLogRelativePath
	}
	return filepath.Join(root, ActivationLogRelativePath)
}

func WriteActiveRecord(path string, rec ActiveRecord) error {
	if err := rec.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir active record dir: %w", err)
	}
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal active record: %w", err)
	}
	data = append(data, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write active record temp file: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename active record: %w", err)
	}
	return nil
}

func AppendActivationLog(path string, entry ActivationLogEntry) error {
	if entry.TimestampUTC == "" {
		entry.TimestampUTC = time.Now().UTC().Format(time.RFC3339)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir activation log dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open activation log: %w", err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	return enc.Encode(entry)
}
