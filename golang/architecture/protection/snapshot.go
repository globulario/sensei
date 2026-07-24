// SPDX-License-Identifier: AGPL-3.0-only

package protection

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// SnapshotPath is the repo-relative location of the derived, offline
// protection snapshot (contract §5). It is derived state, never governed
// source truth — safe to regenerate, never hand-edited.
const SnapshotPath = ".sensei/project/protection-coverage.yaml"

// snapshotReasonWire/snapshotPathWire/snapshotWire mirror ProtectionReason/
// ProtectedPath/ProtectionCoverage as a stable YAML wire shape, independent
// of the in-memory Go struct layout.
type snapshotReasonWire struct {
	Origin       string `yaml:"origin"`
	Kind         string `yaml:"kind"`
	Source       string `yaml:"source"`
	KnowledgeRef string `yaml:"knowledge_ref,omitempty"`
	Provisional  bool   `yaml:"provisional,omitempty"`
}

type snapshotPathWire struct {
	Path    string               `yaml:"path"`
	Reasons []snapshotReasonWire `yaml:"reasons"`
}

type snapshotWire struct {
	SchemaVersion      string             `yaml:"schema_version"`
	Status             string             `yaml:"status"`
	ManualCount        int                `yaml:"manual_count"`
	DerivedCount       int                `yaml:"derived_count"`
	ProvisionalCount   int                `yaml:"provisional_count"`
	GenerationIdentity string             `yaml:"generation_identity"`
	Gaps               []string           `yaml:"gaps"`
	ProtectedPaths     []snapshotPathWire `yaml:"protected_paths"`
}

func toWire(cov ProtectionCoverage) snapshotWire {
	w := snapshotWire{
		SchemaVersion:      cov.SchemaVersion,
		Status:             string(cov.Status),
		ManualCount:        cov.ManualCount,
		DerivedCount:       cov.DerivedCount,
		ProvisionalCount:   cov.ProvisionalCount,
		GenerationIdentity: cov.GenerationIdentity,
		Gaps:               cov.Gaps,
	}
	for _, p := range cov.ProtectedPaths {
		pw := snapshotPathWire{Path: p.Path}
		for _, r := range p.Reasons {
			pw.Reasons = append(pw.Reasons, snapshotReasonWire{
				Origin:       string(r.Origin),
				Kind:         r.Kind,
				Source:       r.Source,
				KnowledgeRef: r.KnowledgeRef,
				Provisional:  r.Provisional,
			})
		}
		w.ProtectedPaths = append(w.ProtectedPaths, pw)
	}
	return w
}

func fromWire(w snapshotWire) ProtectionCoverage {
	cov := ProtectionCoverage{
		SchemaVersion:      w.SchemaVersion,
		Status:             ProtectionCoverageStatus(w.Status),
		ManualCount:        w.ManualCount,
		DerivedCount:       w.DerivedCount,
		ProvisionalCount:   w.ProvisionalCount,
		GenerationIdentity: w.GenerationIdentity,
		Gaps:               w.Gaps,
	}
	for _, pw := range w.ProtectedPaths {
		pp := ProtectedPath{Path: pw.Path}
		for _, rw := range pw.Reasons {
			pp.Reasons = append(pp.Reasons, ProtectionReason{
				Origin:       ProtectionOrigin(rw.Origin),
				Kind:         rw.Kind,
				Source:       rw.Source,
				KnowledgeRef: rw.KnowledgeRef,
				Provisional:  rw.Provisional,
			})
		}
		cov.ProtectedPaths = append(cov.ProtectedPaths, pp)
	}
	return cov
}

// PublishSnapshot atomically writes cov to .sensei/project/protection-coverage.yaml
// under repoRoot: derive the complete candidate content in memory, write it
// to a temp file in the same directory, validate it round-trips, then rename
// over the prior snapshot. A failure at any step leaves the previous valid
// snapshot untouched (contract §5's "a failed derivation must preserve the
// prior valid snapshot").
func PublishSnapshot(repoRoot string, cov ProtectionCoverage) error {
	data, err := yaml.Marshal(toWire(cov))
	if err != nil {
		return fmt.Errorf("marshal protection snapshot: %w", err)
	}
	// Validate round-trip before touching disk.
	var check snapshotWire
	if err := yaml.Unmarshal(data, &check); err != nil {
		return fmt.Errorf("protection snapshot failed round-trip validation: %w", err)
	}
	full := joinRepo(repoRoot, SnapshotPath)
	return writeFileAtomic(full, data)
}

// LoadSnapshot reads the published snapshot at repoRoot. Returns
// (ProtectionCoverage{}, false, nil) when no snapshot has ever been
// published — that is a supported state (contract §7: a fresh/unbootstrapped
// repository reports PARTIAL/EMPTY, not an error).
func LoadSnapshot(repoRoot string) (ProtectionCoverage, bool, error) {
	full := joinRepo(repoRoot, SnapshotPath)
	raw, err := os.ReadFile(full)
	if err != nil {
		if os.IsNotExist(err) {
			return ProtectionCoverage{}, false, nil
		}
		return ProtectionCoverage{}, false, fmt.Errorf("read protection snapshot: %w", err)
	}
	var w snapshotWire
	if err := yaml.Unmarshal(raw, &w); err != nil {
		return ProtectionCoverage{}, true, fmt.Errorf("parse protection snapshot: %w", err)
	}
	return fromWire(w), true, nil
}

// writeFileAtomic writes data to path via a temp-file-then-rename in the
// same directory (so the rename is on one filesystem and therefore atomic).
// A partial write or crash mid-write can never leave a corrupt file at path.
func writeFileAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	cleanup = false
	return nil
}
