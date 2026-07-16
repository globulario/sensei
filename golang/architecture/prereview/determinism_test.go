// SPDX-License-Identifier: Apache-2.0

package prereview

import (
	"bytes"
	"testing"
)

func TestTemporaryPathsDoNotAffectDigest(t *testing.T) {
	r := mustFinalize(t, governedDraft())
	base := r.ReportDigestSHA256

	// Attaching display metadata (temp path, render time, PR number) must not
	// change the semantic digest.
	r.Display = &DisplayMetadata{PRNumber: 7, PRURL: "https://example/pr/7", BranchName: "feat/x", CheckoutPath: "/tmp/checkout-abc", RenderedAt: "2026-07-16T09:09:09Z"}
	got, err := ComputeReportDigest(r)
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	if got != base {
		t.Fatalf("display metadata changed the digest: %q != %q", got, base)
	}

	// A non-authoritative narrative is also excluded.
	r.Narrative = &Narrative{GeneratedBy: "test", Text: "prose", Authoritative: false}
	got, err = ComputeReportDigest(r)
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	if got != base {
		t.Fatalf("narrative changed the digest: %q != %q", got, base)
	}
}

func TestRepeatedRenderingByteIdentical(t *testing.T) {
	r := mustFinalize(t, positiveBuilders(t)["terminally-closed"])

	assertStable := func(name string, render func(PreReviewReport) ([]byte, error)) {
		a, err := render(r)
		if err != nil {
			t.Fatalf("%s render: %v", name, err)
		}
		b, err := render(r)
		if err != nil {
			t.Fatalf("%s render 2: %v", name, err)
		}
		if !bytes.Equal(a, b) {
			t.Fatalf("%s rendering not byte-identical", name)
		}
	}
	assertStable("json", RenderJSON)
	assertStable("yaml", RenderYAML)
	assertStable("markdown", func(r PreReviewReport) ([]byte, error) { return RenderMarkdown(r, RenderOptions{}) })
}

func TestNormalizeMakesEqualContentDigestEqual(t *testing.T) {
	a := baseDraft()
	a.Change.FilesModified = []string{"gin.go", "server.go"}
	a.Binding.PolicyIDs = []string{"gate.default.v1", "gate.strict.v2"}

	b := baseDraft()
	b.Change.FilesModified = []string{"server.go", "gin.go"} // reversed
	b.Binding.PolicyIDs = []string{"gate.strict.v2", "gate.default.v1"}

	ra := mustFinalize(t, a)
	rb := mustFinalize(t, b)
	if ra.ReportDigestSHA256 != rb.ReportDigestSHA256 {
		t.Fatalf("input ordering changed the digest: %q != %q", ra.ReportDigestSHA256, rb.ReportDigestSHA256)
	}
}
