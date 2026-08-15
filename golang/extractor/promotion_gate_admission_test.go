// SPDX-License-Identifier: AGPL-3.0-only

package extractor

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// fakeScope is a signed admission decision stand-in.
type fakeScope map[string]bool

func (f fakeScope) IsAuthoritativelyAdmitted(id string) bool { return f[id] }

// writePattern writes a promotable ImplementationPattern that FAILS the quality
// bar, so "was it validated" is observable as "did it produce a violation".
func writePattern(t *testing.T, dir, rel, id, status, promotionStatus string) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "id: " + id + "\nclass: ImplementationPattern\ntitle: probe\n"
	if status != "" {
		body += "status: " + status + "\n"
	}
	if promotionStatus != "" {
		body += "promotion_status: " + promotionStatus + "\n"
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func scopeOf(t *testing.T, dir string, scope AdmissionScope) map[string]bool {
	t.Helper()
	vios, err := ValidatePromotions(PromotionRequest{Dirs: []string{dir}, Scope: scope})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, v := range vios {
		got[v.NodeID] = true
	}
	return got
}

// Moving identical knowledge into or out of candidates/ must not change which
// identities are authoritatively validated.
func TestPromotionScopeIsIndependentOfPath(t *testing.T) {
	const id = "pattern.probe.one"
	scope := fakeScope{id: true}
	for _, rel := range []string{"p.yaml", "candidates/p.yaml", "deep/nested/p.yaml"} {
		dir := t.TempDir()
		writePattern(t, dir, rel, id, "", "")
		if !scopeOf(t, dir, scope)[id] {
			t.Fatalf("%s: admitted identity fell out of authoritative scope by moving", rel)
		}
	}
	// And an unadmitted identity is out of scope everywhere, including outside
	// candidates/ — the direction the old rule got wrong.
	for _, rel := range []string{"p.yaml", "candidates/p.yaml"} {
		dir := t.TempDir()
		writePattern(t, dir, rel, id, "", "")
		if scopeOf(t, dir, fakeScope{})[id] {
			t.Fatalf("%s: unadmitted identity entered authoritative scope by its location", rel)
		}
	}
}

// `status:` was half the old selector — "" / active / accepted meant promoted.
// It must no longer decide whether a node governs.
func TestPromotionScopeIgnoresCallerEditableStatus(t *testing.T) {
	const id = "pattern.probe.one"
	for _, status := range []string{"", "active", "accepted", "candidate", "deprecated"} {
		dir := t.TempDir()
		writePattern(t, dir, "p.yaml", id, status, "")
		if scopeOf(t, dir, fakeScope{})[id] {
			t.Fatalf("status %q pulled an unadmitted identity into authoritative scope", status)
		}
		if !scopeOf(t, dir, fakeScope{id: true})[id] {
			t.Fatalf("status %q pushed an admitted identity out of authoritative scope", status)
		}
	}
}

// promotion_status is caller-editable too, and receipt-field validation is not
// provenance — proven earlier when a self-authored receipt chain verified.
func TestPromotionScopeIgnoresPromotionStatus(t *testing.T) {
	const id = "pattern.probe.one"
	dir := t.TempDir()
	writePattern(t, dir, "p.yaml", id, "", "adopted")
	if scopeOf(t, dir, fakeScope{})[id] {
		t.Fatal("promotion_status: adopted manufactured authoritative scope")
	}
}

// The positive control: only the admission decision moves scope.
func TestPromotionScopeFollowsAdmission(t *testing.T) {
	const id = "pattern.probe.one"
	dir := t.TempDir()
	writePattern(t, dir, "p.yaml", id, "", "")
	if scopeOf(t, dir, fakeScope{})[id] {
		t.Fatal("unadmitted identity was validated")
	}
	if !scopeOf(t, dir, fakeScope{id: true})[id] {
		t.Fatal("admitting the identity did not bring it into scope")
	}
}

// Absent scope is unavailable, never a silent pass. Zero violations because
// nothing was checked must not read as zero violations.
func TestPromotionValidationWithoutScopeIsUnavailable(t *testing.T) {
	dir := t.TempDir()
	writePattern(t, dir, "p.yaml", "pattern.probe.one", "", "")
	if _, err := ValidatePromotions(PromotionRequest{Dirs: []string{dir}}); !errors.Is(err, ErrAdmissionUnavailable) {
		t.Fatalf("err = %v, want ErrAdmissionUnavailable", err)
	}
}

// Pre-admission review must not require prior admission, or nothing could ever
// qualify for the admission it must already hold.
func TestPreAdmissionCandidateValidationIsNotCircular(t *testing.T) {
	const id = "pattern.candidate.one"
	dir := t.TempDir()
	writePattern(t, dir, "candidates/p.yaml", id, "", "")

	vios, err := ValidatePromotions(PromotionRequest{Dirs: []string{dir}, CandidateTargets: []string{id}})
	if err != nil {
		t.Fatalf("pre-admission validation failed: %v", err)
	}
	found := false
	for _, v := range vios {
		if v.NodeID == id {
			found = true
		}
	}
	if !found {
		t.Fatal("an explicitly targeted candidate was not validated for proposed admission")
	}
}
