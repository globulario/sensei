// SPDX-License-Identifier: AGPL-3.0-only

package extractor

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// writeOwner writes an AuthorityDomain claiming ownership of a state object.
// Two of these owning the same state with different services is an authority
// conflict — but only if BOTH govern.
func writeOwner(t *testing.T, dir, rel, id, service, status string) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "id: " + id + "\nclass: AuthorityDomain\nowner_service: " + service +
		"\nowns_state:\n  - state.shared\n"
	if status != "" {
		body += "status: " + status + "\n"
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func rulesFor(t *testing.T, dir string, scope AdmissionScope) map[string]bool {
	t.Helper()
	cons, err := ValidateContradictions(ContradictionRequest{Dirs: []string{dir}, Scope: scope})
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	for _, c := range cons {
		out[c.Rule] = true
	}
	return out
}

// An unadmitted candidate claiming a governed authority's state is a proposal,
// not an authority conflict. If it counted, anyone could manufacture an
// authoritative contradiction by dropping a YAML file in — inverting #166
// rather than implementing it.
func TestUnadmittedNodeCannotCreateAuthoritativeContradiction(t *testing.T) {
	dir := t.TempDir()
	writeOwner(t, dir, "a.yaml", "authority.governed", "svc-a", "")
	writeOwner(t, dir, "candidates/b.yaml", "authority.candidate", "svc-b", "")

	if rulesFor(t, dir, fakeScope{"authority.governed": true})["authority_owner_conflict"] {
		t.Fatal("an unadmitted candidate created an authoritative contradiction")
	}
	// Both admitted: now it is a genuine conflict between governing knowledge.
	if !rulesFor(t, dir, fakeScope{"authority.governed": true, "authority.candidate": true})["authority_owner_conflict"] {
		t.Fatal("two admitted authorities owning the same state did not conflict")
	}
}

// Location does not move contradiction scope in either direction.
func TestContradictionScopeIsIndependentOfPath(t *testing.T) {
	for _, rel := range []string{"b.yaml", "candidates/b.yaml", "deep/b.yaml"} {
		dir := t.TempDir()
		writeOwner(t, dir, "a.yaml", "authority.governed", "svc-a", "")
		writeOwner(t, dir, rel, "authority.other", "svc-b", "")
		scope := fakeScope{"authority.governed": true, "authority.other": true}
		if !rulesFor(t, dir, scope)["authority_owner_conflict"] {
			t.Fatalf("%s: admitted conflict was hidden by its location", rel)
		}
	}
}

// status/promotion_status no longer decide what is "active" for contradiction
// purposes — admission does.
func TestContradictionScopeIgnoresCallerEditableStatus(t *testing.T) {
	for _, status := range []string{"", "active", "candidate", "proposed"} {
		dir := t.TempDir()
		writeOwner(t, dir, "a.yaml", "authority.governed", "svc-a", "")
		writeOwner(t, dir, "b.yaml", "authority.other", "svc-b", status)
		if rulesFor(t, dir, fakeScope{"authority.governed": true})["authority_owner_conflict"] {
			t.Fatalf("status %q pulled an unadmitted node into authoritative scope", status)
		}
	}
}

func TestContradictionValidationWithoutScopeIsUnavailable(t *testing.T) {
	dir := t.TempDir()
	writeOwner(t, dir, "a.yaml", "authority.governed", "svc-a", "")
	if _, err := ValidateContradictions(ContradictionRequest{Dirs: []string{dir}}); !errors.Is(err, ErrAdmissionUnavailable) {
		t.Fatalf("err = %v, want ErrAdmissionUnavailable", err)
	}
}
