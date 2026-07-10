// SPDX-License-Identifier: AGPL-3.0-only

package governancepack

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	GovernanceDirRelativePath = ".awg/governance"
	ActiveRecordRelativePath  = ".awg/governance/active.json"
)

type ActiveRecord struct {
	SchemaVersion             string `json:"schema_version"`
	PackID                    string `json:"pack_id"`
	PackVersion               string `json:"pack_version"`
	PublisherID               string `json:"publisher_id"`
	PayloadDigestSHA256       string `json:"payload_digest_sha256"`
	PayloadTripleCount        int64  `json:"payload_triple_count"`
	PayloadMarkerIRI          string `json:"payload_marker_iri"`
	ActivatedAt               string `json:"activated_at"`
	ManifestPath              string `json:"manifest_path"`
	ManifestDigestSHA256      string `json:"manifest_digest_sha256,omitempty"`
	CombinedGraphDigestSHA256 string `json:"combined_graph_digest_sha256,omitempty"`
	CombinedGraphTripleCount  int64  `json:"combined_graph_triple_count,omitempty"`
}

func GovernanceDirPath(root string) string {
	if root == "" {
		return GovernanceDirRelativePath
	}
	return filepath.Join(root, GovernanceDirRelativePath)
}

func ActiveRecordPath(root string) string {
	if root == "" {
		return ActiveRecordRelativePath
	}
	return filepath.Join(root, ActiveRecordRelativePath)
}

func (r ActiveRecord) Validate() error {
	if strings.TrimSpace(r.SchemaVersion) == "" {
		return fmt.Errorf("schema_version is required")
	}
	if strings.TrimSpace(r.SchemaVersion) != ActiveRecordSchemaV1 {
		return fmt.Errorf("unsupported schema_version %q", r.SchemaVersion)
	}
	if strings.TrimSpace(r.PackID) == "" {
		return fmt.Errorf("pack_id is required")
	}
	if strings.TrimSpace(r.PackVersion) == "" {
		return fmt.Errorf("pack_version is required")
	}
	if strings.TrimSpace(r.PublisherID) == "" {
		return fmt.Errorf("publisher_id is required")
	}
	if strings.TrimSpace(r.PayloadDigestSHA256) == "" {
		return fmt.Errorf("payload_digest_sha256 is required")
	}
	if r.PayloadTripleCount <= 0 {
		return fmt.Errorf("payload_triple_count must be positive")
	}
	if strings.TrimSpace(r.PayloadMarkerIRI) == "" {
		return fmt.Errorf("payload_marker_iri is required")
	}
	return nil
}

func ReadActiveRecord(path string) (ActiveRecord, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ActiveRecord{}, err
	}
	var rec ActiveRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return ActiveRecord{}, fmt.Errorf("decode active governance record: %w", err)
	}
	if err := rec.Validate(); err != nil {
		return ActiveRecord{}, err
	}
	return rec, nil
}
