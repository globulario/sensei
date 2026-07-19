// SPDX-License-Identifier: AGPL-3.0-only

package ledger

import (
	"fmt"
	"path/filepath"
	"strings"
)

func (s *Store) ledgerDir() string {
	return filepath.Join(s.taskDir, "ledger")
}

func (s *Store) artifactsDir() string {
	return filepath.Join(s.taskDir, "artifacts", "sha256")
}

func (s *Store) headPath() string {
	return filepath.Join(s.ledgerDir(), "HEAD.yaml")
}

func (s *Store) lockDir() string {
	return filepath.Join(s.ledgerDir(), ".append.lock")
}

func sanitizeTaskID(taskID string) string {
	return strings.TrimSpace(taskID)
}

func mediaTypeExtension(mediaType string) (string, error) {
	switch strings.TrimSpace(mediaType) {
	case "application/yaml", "text/yaml", "application/x-yaml":
		return ".yaml", nil
	case "application/json":
		return ".json", nil
	case "application/n-triples":
		return ".nt", nil
	case "application/octet-stream":
		return ".bin", nil
	default:
		return "", fmt.Errorf("unsupported payload media type %q", mediaType)
	}
}
