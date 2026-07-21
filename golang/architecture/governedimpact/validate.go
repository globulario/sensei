// SPDX-License-Identifier: AGPL-3.0-only

package governedimpact

import (
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

// ValidateReport re-derives every value in a report and rejects any inconsistency.
// It recomputes category manifest digests and impacts, requires all ten
// categories exactly once in canonical order, enforces unique sorted changed ids,
// and refuses any whole-graph digest masquerading as a category identity.
func ValidateReport(rep Report) error {
	if rep.SchemaVersion != SchemaVersion {
		return newErr(CodeInvalidReport, "schema version %q, want %q", rep.SchemaVersion, SchemaVersion)
	}
	if !isHex64(rep.BaseGraphDigestSHA256) || !isHex64(rep.ResultGraphDigestSHA256) {
		return newErr(CodeInvalidReport, "graph digests must be 64-hex sha256")
	}
	want := closureprotocol.GovernedKnowledgeCategories()
	if len(rep.BaseManifests) != len(want) || len(rep.ResultManifests) != len(want) || len(rep.Impacts) != len(want) {
		return newErr(CodeInvalidReport, "report must carry exactly %d categories on every axis", len(want))
	}
	graphDigests := map[string]bool{rep.BaseGraphDigestSHA256: true, rep.ResultGraphDigestSHA256: true}
	for i, name := range want {
		if err := validateManifest(rep.BaseManifests[i], name, graphDigests); err != nil {
			return err
		}
		if err := validateManifest(rep.ResultManifests[i], name, graphDigests); err != nil {
			return err
		}
		got := rep.Impacts[i]
		if got.Category != name {
			return newErr(CodeInvalidReport, "impact %d is %q, want canonical %q", i, got.Category, name)
		}
		wantImpact := impactFor(name, rep.BaseManifests[i], rep.ResultManifests[i])
		if got.BaseManifestDigestSHA256 != wantImpact.BaseManifestDigestSHA256 ||
			got.ResultManifestDigestSHA256 != wantImpact.ResultManifestDigestSHA256 {
			return newErr(CodeInvalidReport, "impact %q manifest digests do not match its manifests", name)
		}
		if !equalStrings(got.ChangedRecordIDs, wantImpact.ChangedRecordIDs) {
			return newErr(CodeInvalidReport, "impact %q changed record ids are not the exact recomputed set", name)
		}
		if !sortedUnique(got.ChangedRecordIDs) {
			return newErr(CodeInvalidReport, "impact %q changed record ids are not sorted and unique", name)
		}
		// An unequal manifest with an empty changed set is only legal when declared
		// as an explicit limitation.
		if got.BaseManifestDigestSHA256 != got.ResultManifestDigestSHA256 && len(got.ChangedRecordIDs) == 0 {
			if !hasLimitation(rep.Limitations, name) {
				return newErr(CodeInvalidReport, "category %q changed but identifies no record and declares no limitation", name)
			}
		}
	}
	return nil
}

func validateManifest(m CategoryManifest, name string, graphDigests map[string]bool) error {
	if m.Category != name {
		return newErr(CodeInvalidReport, "manifest is %q, want canonical %q", m.Category, name)
	}
	seen := map[string]bool{}
	var prev string
	for i, r := range m.RecordIdentity {
		if r.ID == "" {
			return newErr(CodeInvalidReport, "category %q has a record with an empty id", name)
		}
		if !isHex64(r.SemanticDigestSHA256) {
			return newErr(CodeInvalidReport, "category %q record %q digest is not a 64-hex sha256", name, r.ID)
		}
		if seen[r.ID] {
			return newErr(CodeDuplicateRecord, "category %q has duplicate record id %q", name, r.ID)
		}
		seen[r.ID] = true
		if i > 0 && r.ID < prev {
			return newErr(CodeInvalidReport, "category %q record ids are not sorted", name)
		}
		prev = r.ID
	}
	if d := manifestDigest(m); d != m.DigestSHA256 {
		return newErr(CodeInvalidReport, "category %q manifest digest does not recompute", name)
	}
	if graphDigests[m.DigestSHA256] {
		return newErr(CodeInvalidReport, "category %q manifest digest equals a whole-graph digest (substitution)", name)
	}
	return nil
}

func hasLimitation(limits []string, name string) bool {
	for _, l := range limits {
		if l == "unattributed_change:"+name {
			return true
		}
	}
	return false
}

func sortedUnique(ids []string) bool {
	for i := 1; i < len(ids); i++ {
		if ids[i] <= ids[i-1] {
			return false
		}
	}
	return true
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
