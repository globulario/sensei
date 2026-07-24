// SPDX-License-Identifier: AGPL-3.0-only

package protection

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// SchemaVersion is the current wire/snapshot schema identity.
const SchemaVersion = "protection.coverage/v1"

// Derive computes the complete effective ProtectionCoverage for the
// repository at repoRoot: the deterministic union of manual, unconditional
// governed-source, structural-contract, governed-relation, and
// candidate-signal protection (contract §2, §4).
//
// Derive never requires a running graph/MCP service — every input is read
// directly from the repository checkout, so the pre-edit hook and CLI can
// call it with no server dependency (contract §5).
func Derive(repoRoot string) (ProtectionCoverage, error) {
	abs, err := filepath.Abs(repoRoot)
	if err != nil {
		return ProtectionCoverage{}, fmt.Errorf("resolve repo root: %w", err)
	}
	repoRoot = abs

	cov := ProtectionCoverage{SchemaVersion: SchemaVersion, Status: CoverageEmpty}
	paths := map[string]*ProtectedPath{}
	var gaps []string
	// sourceFiles collects every repo-relative file this derivation actually
	// read, so GenerationIdentity can bind their raw CONTENT — not just the
	// paths/reasons extracted from them. A change to a source file that
	// doesn't happen to alter any extracted reason (e.g. an invariant's
	// severity or title changes, or a new invariant is added with no
	// protects.files) must still invalidate a published snapshot (contract
	// §3 correction: "source content changes" must change identity).
	var sourceFiles []string

	ensure := func(p string) *ProtectedPath {
		if existing, ok := paths[p]; ok {
			return existing
		}
		pp := &ProtectedPath{Path: p}
		paths[p] = pp
		return pp
	}
	addAll := func(byPath map[string][]ProtectionReason) {
		for p, reasons := range byPath {
			ensure(p).Reasons = append(ensure(p).Reasons, reasons...)
		}
	}

	// 1. Manual registry (additive; absence/emptiness is NOT itself a gap —
	// it is the expected default state a fresh repo starts in).
	manualEntries, manualPresent, manualMalformed, manualErr := ManualEntries(repoRoot)
	if manualErr != nil {
		gaps = append(gaps, "manual_registry_invalid: "+manualErr.Error())
	} else if manualPresent {
		for _, m := range manualMalformed {
			gaps = append(gaps, "manual_registry_malformed_entry: "+m)
		}
		sourceFiles = append(sourceFiles, ManualRegistryFile)
		// Manual entries protect by directory/file PREFIX, not just files that
		// already exist — record one path per configured prefix so callers can
		// see manual coverage even over not-yet-created files.
		for _, prefix := range manualEntries {
			ensure(prefix).Reasons = append(ensure(prefix).Reasons, ProtectionReason{
				Origin: OriginManual,
				Kind:   "high_risk_files.yaml",
				Source: ManualRegistryFile,
			})
		}
	}

	// 2. Unconditional governed-source protection.
	governedReasons, govErr := GovernedSourceReasons(repoRoot)
	if govErr != nil {
		gaps = append(gaps, "governed_source_unavailable: "+govErr.Error())
	} else {
		addAll(governedReasons)
		if files, gerr := GovernedSourceFiles(repoRoot); gerr == nil {
			sourceFiles = append(sourceFiles, files...)
		}
	}

	// 3. Structural contract + annotation signals.
	structReasons, structErr := StructuralContractReasons(repoRoot)
	if structErr != nil {
		gaps = append(gaps, "structural_scan_unavailable: "+structErr.Error())
	} else {
		addAll(structReasons)
		sourceFiles = append(sourceFiles, sortedKeys(structReasons)...)
	}

	// 4. Direct governed relations (protects/enforces/configures/observes,
	// required tests) read from the same authored governed sources — already
	// covered by governedSourceFiles above (relations are read from the same
	// files), so no additional sourceFiles entries are needed here.
	relationReasons, relationMalformed, relErr := GovernedRelationReasons(repoRoot)
	if relErr != nil {
		gaps = append(gaps, "governed_relations_unavailable: "+relErr.Error())
	} else {
		addAll(relationReasons)
		for _, m := range relationMalformed {
			gaps = append(gaps, "governed_relation_malformed_source: "+m)
		}
	}

	// 5. Candidate-derived provisional caution.
	candidateReasons, candidateMalformed, candErr := CandidateSignalReasons(repoRoot)
	if candErr != nil {
		gaps = append(gaps, "candidate_scan_unavailable: "+candErr.Error())
	} else {
		addAll(candidateReasons)
		for _, m := range candidateMalformed {
			gaps = append(gaps, "candidate_malformed_source: "+m)
		}
		if files, cerr := candidateSourceFiles(repoRoot); cerr == nil {
			sourceFiles = append(sourceFiles, files...)
		}
	}

	for _, pp := range paths {
		cov.ProtectedPaths = append(cov.ProtectedPaths, *pp)
	}
	cov.SortPaths()
	cov.Gaps = gaps

	manual, derived, provisional := 0, 0, 0
	for _, pp := range cov.ProtectedPaths {
		isManual, isDerived := false, false
		for _, r := range pp.Reasons {
			if r.Origin == OriginManual {
				isManual = true
			} else {
				isDerived = true
			}
		}
		if isManual {
			manual++
		}
		if isDerived {
			derived++
		}
		if pp.AllProvisional() {
			provisional++
		}
	}
	cov.ManualCount = manual
	cov.DerivedCount = derived
	cov.ProvisionalCount = provisional

	hasMalformedInputs := len(manualMalformed) > 0 || len(relationMalformed) > 0 || len(candidateMalformed) > 0
	cov.Status = computeCoverageStatus(cov, manualErr, govErr, structErr, relErr, candErr, hasMalformedInputs)
	cov.GenerationIdentity = semanticDigest(repoRoot, cov, sourceFiles)

	return cov, nil
}

// candidateSourceFiles lists the repo-relative docs/awareness/candidates/
// files (mirrors CandidateSignalReasons' own directory scan) so their raw
// content participates in the generation identity even for entries that
// don't resolve to a valid protected-path target.
func candidateSourceFiles(repoRoot string) ([]string, error) {
	dir := joinRepo(repoRoot, candidatesDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if norm, ok := NormalizePath(candidatesDir + "/" + e.Name()); ok {
			out = append(out, norm)
		}
	}
	return out, nil
}

// semanticDigest binds GenerationIdentity to BOTH the fully-assembled,
// already-sorted ProtectedPaths (every reason field: origin, kind, source,
// knowledge ref, provisional flag — not just the path key) AND the raw byte
// content of every source file this derivation consulted. Either dimension
// changing — a different rule protecting the same file, a reason's kind
// changing, or an unrelated edit to a governed source's content — changes
// the identity (contract §3 correction). Carries no timestamp.
func semanticDigest(repoRoot string, cov ProtectionCoverage, sourceFiles []string) string {
	h := sha256.New()
	for _, pp := range cov.ProtectedPaths {
		fmt.Fprintf(h, "path:%s\n", pp.Path)
		for _, r := range pp.Reasons {
			fmt.Fprintf(h, "  reason:%s|%s|%s|%s|%v\n", r.Origin, r.Kind, r.Source, r.KnowledgeRef, r.Provisional)
		}
	}
	seen := map[string]bool{}
	files := append([]string(nil), sourceFiles...)
	sort.Strings(files)
	for _, f := range files {
		if seen[f] {
			continue
		}
		seen[f] = true
		fmt.Fprintf(h, "file:%s\n", f)
		data, err := os.ReadFile(joinRepo(repoRoot, f))
		if err != nil {
			fmt.Fprintf(h, "  unreadable:%v\n", err)
			continue
		}
		h.Write(data)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// computeCoverageStatus applies contract §3.5's closed vocabulary. Hard input
// failures (governed-source or governed-relation scan failure) DEGRADE
// coverage — these are the tiers the contract treats as load-bearing
// ("contracts/invariants/governed sources... exist but the effective
// protected set cannot be established safely"). A manual-registry read
// error or any individual malformed input (contract §6 correction — a
// dropped entry or unparseable source file is never silently absorbed into
// a clean-looking COMPLETE result) forces PARTIAL coverage at minimum. Every
// named structural source (protobuf, OpenAPI, JSON Schema, annotations) is
// now actually implemented — COMPLETE is reachable when every input truly
// evaluated cleanly (contract §4 correction: COMPLETE must not coexist with
// an admitted "supported input not implemented" gap).
func computeCoverageStatus(cov ProtectionCoverage, manualErr, govErr, structErr, relErr, candErr error, hasMalformedInputs bool) ProtectionCoverageStatus {
	if govErr != nil || relErr != nil {
		return CoverageDegraded
	}
	hasHardGap := manualErr != nil || structErr != nil || candErr != nil || hasMalformedInputs
	if len(cov.ProtectedPaths) == 0 {
		if hasHardGap {
			return CoverageDegraded
		}
		// A conclusive, successful, empty scan is EMPTY — an awareness gap,
		// never presented as "no high-risk files" safety (contract §3.5).
		return CoverageEmpty
	}
	if hasHardGap || len(cov.Gaps) > 0 {
		return CoveragePartial
	}
	return CoverageComplete
}

func sortedKeys(m map[string][]ProtectionReason) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
