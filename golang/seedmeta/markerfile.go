// SPDX-License-Identifier: Apache-2.0

package seedmeta

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/globulario/sensei/golang/statedir"
)

func RuntimeMarkerPath(root string) string {
	return statedir.Path(root, "graph-authority.json")
}

func RuntimeTransactionPath(markerPath string) string {
	markerPath = strings.TrimSpace(markerPath)
	if markerPath == "" {
		return ""
	}
	ext := filepath.Ext(markerPath)
	base := strings.TrimSuffix(markerPath, ext)
	return base + ".transaction.tsv"
}

func WriteMarkerFile(path string, marker Marker) error {
	if marker.Digest == "" || marker.IRI == "" || marker.TripleCount <= 0 {
		return fmt.Errorf("marker is incomplete")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir marker dir: %w", err)
	}
	payload := struct {
		DigestSHA256 string `json:"digest_sha256"`
		MarkerIRI    string `json:"marker_iri"`
		TripleCount  int64  `json:"triple_count"`
	}{
		DigestSHA256: marker.Digest,
		MarkerIRI:    marker.IRI,
		TripleCount:  marker.TripleCount,
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal marker file: %w", err)
	}
	data = append(data, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write marker temp file: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename marker file: %w", err)
	}
	return nil
}

func ReadMarkerFile(path string) (Marker, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Marker{}, err
	}
	var payload struct {
		DigestSHA256 string `json:"digest_sha256"`
		MarkerIRI    string `json:"marker_iri"`
		TripleCount  int64  `json:"triple_count"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return Marker{}, fmt.Errorf("decode marker file: %w", err)
	}
	marker := Marker{
		Digest:      payload.DigestSHA256,
		IRI:         payload.MarkerIRI,
		TripleCount: payload.TripleCount,
	}
	if marker.Digest == "" || marker.IRI == "" || marker.TripleCount <= 0 {
		return Marker{}, fmt.Errorf("marker file is incomplete")
	}
	return marker, nil
}
