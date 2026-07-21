// SPDX-License-Identifier: AGPL-3.0-only

package admission

import "testing"

// The observed change set has a canonical digest that the scope verification
// binds, so the exact observed mutation is carried forward — not merely a
// result-tree digest.

func TestObservedChangeSetDigestDeterministicAndSensitive(t *testing.T) {
	base := ObservedChangeSet{
		BaseTreeDigestSHA256:   "base",
		ResultTreeDigestSHA256: "result",
		Files:                  []ObservedFile{{Path: "a.go", ChangeType: "modify"}},
	}
	d1, err := ObservedChangeSetDigest(base)
	if err != nil {
		t.Fatal(err)
	}
	d2, err := ObservedChangeSetDigest(base)
	if err != nil {
		t.Fatal(err)
	}
	if d1 != d2 {
		t.Fatalf("digest is not deterministic: %s vs %s", d1, d2)
	}
	changed := base
	changed.Files = []ObservedFile{{Path: "b.go", ChangeType: "modify"}}
	d3, err := ObservedChangeSetDigest(changed)
	if err != nil {
		t.Fatal(err)
	}
	if d1 == d3 {
		t.Fatal("digest did not change when the observed files changed")
	}
}

func TestVerifyScopeBindsObservedChangeDigest(t *testing.T) {
	exp, observed := scopeFixture(t)
	v, err := VerifyScope(exp, observed, v2VerifiedAt)
	if err != nil {
		t.Fatal(err)
	}
	want, err := ObservedChangeSetDigest(observed)
	if err != nil {
		t.Fatal(err)
	}
	if v.ObservedChangeSetDigestSHA256 != want {
		t.Fatalf("scope verification did not bind the observed change digest: got %s want %s",
			v.ObservedChangeSetDigestSHA256, want)
	}
}
