// SPDX-License-Identifier: AGPL-3.0-only

package main

import "testing"

func TestAdmitChangeRequiresBundleRequestGraphAndRepo(t *testing.T) {
	if code := runAdmitChange(nil); code != 2 {
		t.Fatalf("expected usage exit 2, got %d", code)
	}
}

func TestVerifyAdmissionRequiresDecisionBundleAndRepo(t *testing.T) {
	if code := runVerifyAdmission(nil); code != 2 {
		t.Fatalf("expected usage exit 2, got %d", code)
	}
}

func TestAdmissionStatusRequiresDecision(t *testing.T) {
	if code := runAdmissionStatus(nil); code != 2 {
		t.Fatalf("expected usage exit 2, got %d", code)
	}
}

func TestAdmissionFormatsAreClosedSet(t *testing.T) {
	for _, format := range []string{"text", "yaml", "json"} {
		if !validAdmissionFormat(format) {
			t.Fatalf("format %s should be valid", format)
		}
	}
	if validAdmissionFormat("markdown") {
		t.Fatal("unexpected admission format accepted")
	}
}
