// SPDX-License-Identifier: AGPL-3.0-only

package protection

// FileClassification is the answer to "must an agent consult Sensei before
// editing this file?" for one exact file, plus enough structure to render an
// honest message. It answers nothing about architectural correctness,
// ownership, or a legal mutation path (contract §3.4).
type FileClassification struct {
	// Path is the normalized repo-relative path that was classified.
	Path string
	// Protected is true when at least one reason (manual, governed-source,
	// structural, relation, or candidate) applies.
	Protected bool
	// Provisional is true when Protected is true but EVERY reason is
	// candidate-derived (contract §3.2: caution without promotion).
	Provisional bool
	Reasons     []ProtectionReason
	// CoverageStatus is the overall repository coverage status this
	// classification was computed against — always present so a caller can
	// distinguish "not classified as protected by current coverage" from
	// "coverage itself is degraded/empty" (contract §6 text guidance).
	CoverageStatus ProtectionCoverageStatus
}

// ClassifyFile answers whether path is protected under an already-derived
// ProtectionCoverage for repoRoot. path may be absolute or relative — it is
// resolved via ResolveRepoPath (symlink-aware, root-contained) internally, so
// a path that escapes the repository (an absolute path outside it, `..`
// traversal, or a symlink that resolves outside it) returns ok=false. The
// caller MUST treat ok=false as a typed failure, never as "not protected"
// (contract §2/§3.7).
func ClassifyFile(repoRoot string, cov ProtectionCoverage, path string) (FileClassification, bool) {
	norm, ok := ResolveRepoPath(repoRoot, path)
	if !ok {
		return FileClassification{}, false
	}
	fc := FileClassification{Path: norm, CoverageStatus: cov.Status}

	// Direct file match.
	if pp, found := cov.Find(norm); found {
		fc.Reasons = append(fc.Reasons, pp.Reasons...)
	}
	// Manual entries protect by directory/file PREFIX: a manual reason may
	// be recorded against the prefix itself (Derive stores one ProtectedPath
	// per configured prefix) rather than against every file under it, so a
	// direct Find miss must still check prefix scope explicitly.
	for _, pp := range cov.ProtectedPaths {
		hasManual := false
		for _, r := range pp.Reasons {
			if r.Origin == OriginManual {
				hasManual = true
				break
			}
		}
		if !hasManual || pp.Path == norm {
			continue
		}
		if InPathScope(norm, pp.Path) {
			fc.Reasons = append(fc.Reasons, ProtectionReason{
				Origin: OriginManual,
				Kind:   "high_risk_files.yaml",
				Source: ManualRegistryFile,
			})
		}
	}

	fc.Protected = len(fc.Reasons) > 0
	if fc.Protected {
		fc.Provisional = true
		for _, r := range fc.Reasons {
			if !r.Provisional {
				fc.Provisional = false
				break
			}
		}
	}
	return fc, true
}
