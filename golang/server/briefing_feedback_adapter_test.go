// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture/briefingfeedback"
	awarenesspb "github.com/globulario/sensei/golang/pb"
	"google.golang.org/protobuf/encoding/protojson"
)

// Existing BriefingResponse field numbers are unchanged and feedback is field 7.
func TestBriefingResponse_FeedbackIsAdditiveFieldSeven(t *testing.T) {
	md := (&awarenesspb.BriefingResponse{}).ProtoReflect().Descriptor()
	byName := map[string]int{}
	for i := 0; i < md.Fields().Len(); i++ {
		f := md.Fields().Get(i)
		byName[string(f.Name())] = int(f.Number())
	}
	for name, want := range map[string]int{
		"prose": 1, "generated_in_ms": 2, "referenced_ids": 3, "status": 4,
		"implementation_patterns": 5, "authority": 6, "feedback": 7,
	} {
		if byName[name] != want {
			t.Errorf("field %q number = %d, want %d", name, byName[name], want)
		}
	}
}

// Every canonical availability maps explicitly to a distinct non-unspecified enum.
func TestFeedbackAvailabilityMapsExplicitly(t *testing.T) {
	seen := map[awarenesspb.BriefingFeedbackAvailability]bool{}
	for _, a := range []briefingfeedback.Availability{
		briefingfeedback.FeedbackAvailable, briefingfeedback.FeedbackEmpty, briefingfeedback.FeedbackDegraded,
		briefingfeedback.FeedbackUnavailable, briefingfeedback.FeedbackInvalid,
	} {
		got, err := feedbackAvailabilityToProto(a)
		if err != nil {
			t.Fatalf("availability %q: %v", a, err)
		}
		if got == awarenesspb.BriefingFeedbackAvailability_BRIEFING_FEEDBACK_AVAILABILITY_UNSPECIFIED || seen[got] {
			t.Fatalf("availability %q maps to unspecified or a duplicate", a)
		}
		seen[got] = true
	}
	if _, err := feedbackAvailabilityToProto("made_up"); err == nil {
		t.Fatal("unknown availability must fail")
	}
}

// Every canonical finding class maps explicitly; unknown fails.
func TestFeedbackFindingClassMapsExplicitly(t *testing.T) {
	seen := map[awarenesspb.BriefingFeedbackFindingClass]bool{}
	for _, c := range []briefingfeedback.FindingClass{
		briefingfeedback.PromotionVerified, briefingfeedback.PromotionOutOfScope, briefingfeedback.PromotionIncomplete,
		briefingfeedback.PromotionIntegrityFailure, briefingfeedback.PromotionContradictory, briefingfeedback.PromotionStale,
		briefingfeedback.PromotionUnverifiable, briefingfeedback.PromotionDiscoveryUnavailable,
		briefingfeedback.PromotionScopeIdentityInvalid, briefingfeedback.PromotionUnknownClassification,
	} {
		got, err := feedbackFindingClassToProto(c)
		if err != nil {
			t.Fatalf("class %q: %v", c, err)
		}
		if got == awarenesspb.BriefingFeedbackFindingClass_BRIEFING_FEEDBACK_FINDING_CLASS_UNSPECIFIED || seen[got] {
			t.Fatalf("class %q maps to unspecified or a duplicate", c)
		}
		seen[got] = true
	}
	if len(seen) != 10 {
		t.Fatalf("mapped %d classes, want 10", len(seen))
	}
	if _, err := feedbackFindingClassToProto("made_up"); err == nil {
		t.Fatal("unknown finding class must fail")
	}
}

// Every disposition maps explicitly; unknown fails.
func TestFeedbackDispositionMapsExplicitly(t *testing.T) {
	seen := map[awarenesspb.BriefingFeedbackDisposition]bool{}
	for _, d := range []briefingfeedback.Disposition{
		briefingfeedback.DispositionAdmitted, briefingfeedback.DispositionExcluded, briefingfeedback.DispositionUnavailable,
	} {
		got, err := feedbackDispositionToProto(d)
		if err != nil {
			t.Fatalf("disposition %q: %v", d, err)
		}
		if got == awarenesspb.BriefingFeedbackDisposition_BRIEFING_FEEDBACK_DISPOSITION_UNSPECIFIED || seen[got] {
			t.Fatalf("disposition %q maps to unspecified or a duplicate", d)
		}
		seen[got] = true
	}
	if _, err := feedbackDispositionToProto("made_up"); err == nil {
		t.Fatal("unknown disposition must fail")
	}
}

// The adapter preserves the canonical digest + identity, never leaks a filesystem root, and
// refuses a non-canonical projection.
func TestBriefingFeedbackToProto_PreservesAndRefuses(t *testing.T) {
	p, err := briefingfeedback.BuildUnavailable(briefingfeedback.Scope{
		RepositoryIdentity: "github.com/globulario/sensei",
		RequestedDomain:    "github.com/globulario/sensei",
		RequestedFiles:     []string{"golang/server/reload.go"},
	}, briefingfeedback.RepositoryContextDomainMismatch)
	if err != nil {
		t.Fatal(err)
	}
	wire, err := briefingFeedbackToProto(p)
	if err != nil {
		t.Fatalf("adapter: %v", err)
	}
	if wire.GetDigestSha256() != p.DigestSHA256 || wire.GetDigestSha256() == "" {
		t.Fatalf("digest not preserved: %q vs %q", wire.GetDigestSha256(), p.DigestSHA256)
	}
	if wire.GetAvailability() != awarenesspb.BriefingFeedbackAvailability_BRIEFING_FEEDBACK_AVAILABILITY_UNAVAILABLE {
		t.Fatalf("availability not mapped")
	}
	// No absolute filesystem root anywhere on the wire.
	blob, err := protojson.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(blob), "/tmp/") || strings.Contains(string(blob), "RepositoryRoot") {
		t.Fatalf("filesystem root leaked to wire: %s", blob)
	}
	// A non-canonical projection is refused (digest tampered).
	bad := p
	bad.DigestSHA256 = "deadbeef"
	if _, err := briefingFeedbackToProto(bad); err == nil {
		t.Fatal("adapter must refuse a non-canonical projection")
	}
}

// The vendored VS Code proto exactly matches the canonical proto (wire sync).
func TestVendoredProtoMatchesCanonical(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	canonical, err := os.ReadFile(filepath.Join(root, "proto", "awareness_graph.proto"))
	if err != nil {
		t.Fatal(err)
	}
	vendored, err := os.ReadFile(filepath.Join(root, "editor", "vscode", "proto", "awareness_graph.proto"))
	if err != nil {
		t.Fatal(err)
	}
	if string(canonical) != string(vendored) {
		t.Fatal("vendored VS Code proto is out of sync with the canonical proto")
	}
}

// The feedback wire schema carries no filesystem repository root field.
func TestBriefingFeedbackProjection_NoRepositoryRootField(t *testing.T) {
	md := (&awarenesspb.BriefingFeedbackProjection{}).ProtoReflect().Descriptor()
	for i := 0; i < md.Fields().Len(); i++ {
		name := string(md.Fields().Get(i).Name())
		if strings.Contains(name, "root") || name == "repository_path" || name == "checkout" {
			t.Fatalf("feedback wire schema exposes a filesystem-root field %q", name)
		}
	}
}
