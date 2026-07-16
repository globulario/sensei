// SPDX-License-Identifier: Apache-2.0

package prereview

import "testing"

func TestBindingRequiresRepositoryAndDiff(t *testing.T) {
	cases := map[string]func(*PreReviewReport){
		"repository": func(r *PreReviewReport) { r.Binding.RepositoryDomain = "" },
		"base_tree":  func(r *PreReviewReport) { r.Binding.BaseTreeDigestSHA256 = "" },
		"head_tree":  func(r *PreReviewReport) { r.Binding.HeadTreeDigestSHA256 = "" },
		"diff":       func(r *PreReviewReport) { r.Binding.DiffDigestSHA256 = "" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			d := baseDraft()
			mutate(&d)
			if _, err := Finalize(d); err == nil {
				t.Fatalf("finalize accepted a report missing binding.%s", name)
			}
		})
	}
}

func TestTaskBackedReportBindsToLedgerHead(t *testing.T) {
	d := governedDraft()
	d.Binding.LedgerHeadDigestSHA256 = ""
	if _, err := Finalize(d); err == nil {
		t.Fatal("finalize accepted a task-backed report without a ledger head")
	}
}

func TestPreReviewReportIDStable(t *testing.T) {
	a := mustFinalize(t, baseDraft())

	// Different display metadata and reviewer content, same binding.
	d := baseDraft()
	d.Display = &DisplayMetadata{PRNumber: 42, CheckoutPath: "/tmp/checkout-xyz", RenderedAt: "2026-07-16T12:00:00Z"}
	d.Summary.Purpose = "A completely different description."
	b := mustFinalize(t, d)

	if a.ReportID != b.ReportID {
		t.Fatalf("report id changed with display/content: %q != %q", a.ReportID, b.ReportID)
	}

	// A different diff yields a different id.
	d2 := baseDraft()
	d2.Binding.DiffDigestSHA256 = "otherdiff00000000000000000000000000000000000000000000000000009999"
	c := mustFinalize(t, d2)
	if a.ReportID == c.ReportID {
		t.Fatal("report id did not change with a different diff")
	}
}
