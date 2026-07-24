// SPDX-License-Identifier: AGPL-3.0-only

package protection

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
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
	digestInputs := []string{}

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
	manualEntries, manualPresent, manualErr := ManualEntries(repoRoot)
	if manualErr != nil {
		gaps = append(gaps, "manual_registry_invalid: "+manualErr.Error())
	} else if manualPresent {
		digestInputs = append(digestInputs, "manual:"+joinSorted(manualEntries))
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
		digestInputs = append(digestInputs, "governed:"+joinSorted(sortedKeys(governedReasons)))
	}

	// 3. Structural contract + annotation signals.
	structReasons, structErr := StructuralContractReasons(repoRoot)
	if structErr != nil {
		gaps = append(gaps, "structural_scan_unavailable: "+structErr.Error())
	} else {
		addAll(structReasons)
		digestInputs = append(digestInputs, "structural:"+joinSorted(sortedKeys(structReasons)))
	}
	// JSON Schema is a named-but-unimplemented structural source (see
	// structural.go doc comment) — always a partial-coverage gap, never
	// silently absent.
	gaps = append(gaps, "json_schema_scanner_not_implemented")

	// 4. Direct governed relations (protects/enforces/configures/observes,
	// required tests) read from the same authored governed sources.
	relationReasons, relErr := GovernedRelationReasons(repoRoot)
	if relErr != nil {
		gaps = append(gaps, "governed_relations_unavailable: "+relErr.Error())
	} else {
		addAll(relationReasons)
		digestInputs = append(digestInputs, "relations:"+joinSorted(sortedKeys(relationReasons)))
	}

	// 5. Candidate-derived provisional caution.
	candidateReasons, candErr := CandidateSignalReasons(repoRoot)
	if candErr != nil {
		gaps = append(gaps, "candidate_scan_unavailable: "+candErr.Error())
	} else {
		addAll(candidateReasons)
		digestInputs = append(digestInputs, "candidates:"+joinSorted(sortedKeys(candidateReasons)))
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

	cov.Status = computeCoverageStatus(cov, manualErr, govErr, structErr, relErr, candErr)
	cov.GenerationIdentity = digestOf(digestInputs)

	return cov, nil
}

// computeCoverageStatus applies contract §3.5's closed vocabulary. Hard input
// failures (governed-source or governed-relation scan failure) DEGRADE
// coverage — these are the tiers the contract treats as load-bearing
// ("contracts/invariants/governed sources... exist but the effective
// protected set cannot be established safely"). A manual-registry read
// error, or the always-open JSON-Schema gap, only PARTIALs coverage.
func computeCoverageStatus(cov ProtectionCoverage, manualErr, govErr, structErr, relErr, candErr error) ProtectionCoverageStatus {
	if govErr != nil || relErr != nil {
		return CoverageDegraded
	}
	hasHardGap := manualErr != nil || structErr != nil || candErr != nil
	if len(cov.ProtectedPaths) == 0 {
		if hasHardGap {
			return CoverageDegraded
		}
		// A conclusive, successful, empty scan is EMPTY — an awareness gap,
		// never presented as "no high-risk files" safety (contract §3.5).
		return CoverageEmpty
	}
	if hasHardGap || len(cov.Gaps) > 1 { // ">1" because the JSON-schema gap is always present.
		return CoveragePartial
	}
	return CoverageComplete
}

func joinSorted(items []string) string {
	sorted := append([]string(nil), items...)
	sort.Strings(sorted)
	out := ""
	for _, s := range sorted {
		out += s + "\n"
	}
	return out
}

func sortedKeys(m map[string][]ProtectionReason) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// digestOf returns a deterministic hex digest of the given deterministically
// pre-sorted inputs. Carries no timestamp — identical inputs always produce
// the identical identity (contract §5).
func digestOf(inputs []string) string {
	h := sha256.New()
	for _, in := range inputs {
		h.Write([]byte(in))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}
