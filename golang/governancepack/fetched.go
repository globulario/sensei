// SPDX-License-Identifier: AGPL-3.0-only

package governancepack

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const FetchedRecordSchemaV1 = "awg.fetched-governance.v1"

type FetchedRecord struct {
	SchemaVersion        string `json:"schema_version"`
	PackID               string `json:"pack_id"`
	PackVersion          string `json:"pack_version"`
	PublisherID          string `json:"publisher_id"`
	PayloadDigestSHA256  string `json:"payload_digest_sha256"`
	PayloadTripleCount   int64  `json:"payload_triple_count"`
	PayloadMarkerIRI     string `json:"payload_marker_iri"`
	FetchedAt            string `json:"fetched_at"`
	Source               string `json:"source"`
	RequestedChannel     string `json:"requested_channel,omitempty"`
	RequestedPackVersion string `json:"requested_pack_version,omitempty"`
	ManifestPath         string `json:"manifest_path"`
}

func FetchedPackDir(root, packID, packVersion string) string {
	return filepath.Join(GovernanceDirPath(root), "fetched", packID, packVersion)
}

func FetchedRecordPath(root, packID, packVersion string) string {
	return filepath.Join(FetchedPackDir(root, packID, packVersion), "fetch.json")
}

func (r FetchedRecord) Validate() error {
	switch {
	case strings.TrimSpace(r.SchemaVersion) == "":
		return fmt.Errorf("schema_version is required")
	case r.SchemaVersion != FetchedRecordSchemaV1:
		return fmt.Errorf("unsupported schema_version %q", r.SchemaVersion)
	case strings.TrimSpace(r.PackID) == "":
		return fmt.Errorf("pack_id is required")
	case strings.TrimSpace(r.PackVersion) == "":
		return fmt.Errorf("pack_version is required")
	case strings.TrimSpace(r.PublisherID) == "":
		return fmt.Errorf("publisher_id is required")
	case strings.TrimSpace(r.PayloadDigestSHA256) == "":
		return fmt.Errorf("payload_digest_sha256 is required")
	case r.PayloadTripleCount <= 0:
		return fmt.Errorf("payload_triple_count must be positive")
	case strings.TrimSpace(r.PayloadMarkerIRI) == "":
		return fmt.Errorf("payload_marker_iri is required")
	case strings.TrimSpace(r.FetchedAt) == "":
		return fmt.Errorf("fetched_at is required")
	case strings.TrimSpace(r.Source) == "":
		return fmt.Errorf("source is required")
	case strings.TrimSpace(r.ManifestPath) == "":
		return fmt.Errorf("manifest_path is required")
	}
	if _, err := time.Parse(time.RFC3339, r.FetchedAt); err != nil {
		return fmt.Errorf("fetched_at invalid: %w", err)
	}
	return nil
}

func ReadFetchedRecord(path string) (FetchedRecord, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return FetchedRecord{}, err
	}
	var rec FetchedRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return FetchedRecord{}, fmt.Errorf("decode fetched governance record: %w", err)
	}
	if err := rec.Validate(); err != nil {
		return FetchedRecord{}, err
	}
	return rec, nil
}

func WriteFetchedRecord(path string, rec FetchedRecord) error {
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

func ListFetchedRecords(root string) ([]FetchedRecord, error) {
	fetchedRoot := filepath.Join(GovernanceDirPath(root), "fetched")
	if _, err := os.Stat(fetchedRoot); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var records []FetchedRecord
	err := filepath.WalkDir(fetchedRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || d.Name() != "fetch.json" {
			return nil
		}
		rec, err := ReadFetchedRecord(path)
		if err != nil {
			return err
		}
		records = append(records, rec)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(records, func(i, j int) bool {
		return records[i].FetchedAt > records[j].FetchedAt
	})
	return records, nil
}
