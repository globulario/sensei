// SPDX-License-Identifier: AGPL-3.0-only

package governancepack

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const StagedTrustRecordSchemaV1 = "awg.staged-trust-root.v1"

type StagedTrustRecord struct {
	SchemaVersion  string `json:"schema_version"`
	Source         string `json:"source"`
	FetchedAt      string `json:"fetched_at"`
	PublisherCount int    `json:"publisher_count"`
}

func StagedTrustRecordPath(root string) string {
	base := StagedTrustStorePath(root)
	ext := filepath.Ext(base)
	if ext == "" {
		return base + ".fetch.json"
	}
	return strings.TrimSuffix(base, ext) + ".fetch.json"
}

func (r StagedTrustRecord) Validate() error {
	switch {
	case strings.TrimSpace(r.SchemaVersion) == "":
		return fmt.Errorf("schema_version is required")
	case r.SchemaVersion != StagedTrustRecordSchemaV1:
		return fmt.Errorf("unsupported schema_version %q", r.SchemaVersion)
	case strings.TrimSpace(r.Source) == "":
		return fmt.Errorf("source is required")
	case strings.TrimSpace(r.FetchedAt) == "":
		return fmt.Errorf("fetched_at is required")
	case r.PublisherCount < 0:
		return fmt.Errorf("publisher_count must be non-negative")
	}
	if _, err := time.Parse(time.RFC3339, r.FetchedAt); err != nil {
		return fmt.Errorf("fetched_at invalid: %w", err)
	}
	return nil
}

func WriteStagedTrustRecord(path string, rec StagedTrustRecord) error {
	if err := rec.Validate(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp := path + ".tmp"
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func ReadStagedTrustRecord(path string) (StagedTrustRecord, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return StagedTrustRecord{}, err
	}
	var rec StagedTrustRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return StagedTrustRecord{}, fmt.Errorf("decode staged trust record: %w", err)
	}
	if err := rec.Validate(); err != nil {
		return StagedTrustRecord{}, err
	}
	return rec, nil
}
