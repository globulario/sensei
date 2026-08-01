// SPDX-License-Identifier: AGPL-3.0-only

package whyinvestigation

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/investigation"
)

type ScarsProvider struct {
	Root string
}

func (p ScarsProvider) Identity() investigation.ProviderBinding {
	return investigation.ProviderBinding{ID: "scars_provider", Version: "1.0"}
}

func (p ScarsProvider) Capture(ctx context.Context, req CaptureRequest) (Snapshot, error) {
	scarsDir := filepath.Join(p.Root, "docs", "scars")
	var entries []SnapshotEntry
	if _, err := os.Stat(scarsDir); err == nil {
		err = filepath.WalkDir(scarsDir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() {
				rel, _ := filepath.Rel(p.Root, path)
				content, err := os.ReadFile(path)
				if err != nil {
					return nil
				}
				entries = append(entries, SnapshotEntry{
					SourceIdentity:      "scar:" + rel,
					Path:                rel,
					Content:             content,
					ContentDigestSHA256: investigation.SHA256Bytes(content),
				})
			}
			return nil
		})
		if err != nil {
			return Snapshot{}, err
		}
	}

	snap := Snapshot{
		Provider: p.Identity(),
		Category: investigation.EvidenceErrorTracking,
		Entries:  entries,
	}

	digest, err := computeSnapshotDigest(&snap)
	if err != nil {
		return Snapshot{}, err
	}
	snap.Digest = digest

	return snap, nil
}

func (p ScarsProvider) Investigate(ctx context.Context, snap Snapshot, req CaptureRequest) (Result, error) {
	queryDigest, err := digestQuery(req.Query)
	if err != nil {
		return Result{}, err
	}
	target, err := digestTarget(req, queryDigest)
	if err != nil {
		return Result{}, err
	}

	coverage := investigation.CoverageEntry{
		ProviderID:                 "scars_provider",
		ProviderVersion:            "1.0",
		Category:                   investigation.EvidenceErrorTracking,
		TargetDigestSHA256:         target,
		SourceSnapshotDigestSHA256: snap.Digest,
	}

	var rawEvidence []investigation.EvidenceReceipt

	// Read exclusively from the frozen snapshot entries
	for _, entry := range snap.Entries {
		content := entry.Content
		matched := false
		var matchedTargets []string
		for _, targetID := range req.Query.TargetObservationIDs {
			if strings.Contains(string(content), targetID) {
				matched = true
				matchedTargets = append(matchedTargets, targetID)
			}
		}
		for _, targetID := range req.Query.TargetEvidenceIDs {
			if strings.Contains(string(content), targetID) {
				matched = true
				matchedTargets = append(matchedTargets, targetID)
			}
		}

		if matched {
			id := "evidence_" + investigation.SHA256String("scars|" + entry.Path)[:16]
			coverage.ResultEvidenceIDs = append(coverage.ResultEvidenceIDs, id)
			rawEvidence = append(rawEvidence, investigation.EvidenceReceipt{
				ID:                  id,
				Category:            investigation.EvidenceErrorTracking,
				Provider:            p.Identity(),
				ProofStrength:       investigation.ProofStaticSource,
				SourceIdentity:      entry.SourceIdentity,
				SourceDigestSHA256:  entry.ContentDigestSHA256,
				ContentDigestSHA256: entry.ContentDigestSHA256,
				CapturedContent:     string(content),
				Scope: architecture.ClaimScope{
					Repository: req.Repository.RepositoryDomain,
					Symbols:    matchedTargets,
				},
				CapturedAt: req.CapturedAt,
			})
		}
	}

	if len(rawEvidence) == 0 {
		coverage.Status = investigation.CoverageNoResult
	} else {
		coverage.Status = investigation.CoverageSupporting
	}

	return Result{
		RawEvidence: rawEvidence,
		Coverage:    coverage,
	}, nil
}
