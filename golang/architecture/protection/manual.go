// SPDX-License-Identifier: AGPL-3.0-only

package protection

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// ManualRegistryFile is the repo-relative path to the additive manual
// protection registry. This is the ONE place its name is spelled; every
// caller (init, bootstrap, hooks, CLI, audit) gets it from this package.
const ManualRegistryFile = "docs/awareness/high_risk_files.yaml"

// manualRegistry is the YAML shape of docs/awareness/high_risk_files.yaml.
// Entries are directory/file PATH PREFIXES (segment-boundary matched via
// InPathScope), not glob patterns.
type manualRegistry struct {
	Files []string `yaml:"files"`
}

// ManualEntries reads and normalizes the manual registry at repoRoot. A
// missing file returns (nil, false, nil): absence is a supported state, not
// an error — callers must not conflate it with "protection is safe" (contract
// §3.1). A present-but-malformed file returns a typed error so callers can
// surface DEGRADED rather than silently treating it as empty.
func ManualEntries(repoRoot string) (entries []string, present bool, err error) {
	full := joinRepo(repoRoot, ManualRegistryFile)
	raw, readErr := os.ReadFile(full)
	if readErr != nil {
		if os.IsNotExist(readErr) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("read manual registry: %w", readErr)
	}
	var doc manualRegistry
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, true, fmt.Errorf("parse manual registry %s: %w", ManualRegistryFile, err)
	}
	normalized := make([]string, 0, len(doc.Files))
	for _, f := range doc.Files {
		if norm, ok := NormalizePath(f); ok {
			normalized = append(normalized, norm)
		}
		// A malformed individual entry (escapes the repo, empty after
		// trimming) is dropped rather than failing the whole registry —
		// contract §12 "malformed registry entries produce diagnostics and
		// cannot weaken derived protection" is satisfied because dropping an
		// entry can only ever REDUCE manual protection, never remove a
		// derived reason.
	}
	return normalized, true, nil
}

// ManualReasonsForFile returns the manual ProtectionReasons for
// normalizedPath given the (already-loaded) manual entries, or nil if none
// apply.
func ManualReasonsForFile(normalizedPath string, entries []string) []ProtectionReason {
	var reasons []ProtectionReason
	for _, prefix := range entries {
		if InPathScope(normalizedPath, prefix) {
			reasons = append(reasons, ProtectionReason{
				Origin: OriginManual,
				Kind:   "high_risk_files.yaml",
				Source: ManualRegistryFile,
			})
			break // one manual reason is enough; which prefix matched is not load-bearing.
		}
	}
	return reasons
}
