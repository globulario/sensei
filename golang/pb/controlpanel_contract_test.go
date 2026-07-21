// SPDX-License-Identifier: AGPL-3.0-only

package awarenesspb_test

// Wire-contract shape proofs for the Phase 9.5 Checkpoint-2 control-panel surfaces, checked by
// protobuf reflection over the GENERATED descriptors (so they hold for every consumer, not just
// the Go server):
//
//   - no inbound request carries a repository root / cwd / workspace path, a caller-supplied
//     canonical class, a search-text field, or ANY semantic-verdict message (attention items,
//     closure, severity) — the transport-boundary proof;
//   - the navigation-descriptor request is empty (no filesystem context);
//   - response models carry no correctness-certification claim or synthetic score;
//   - Phase 9.6 BriefingResponse.feedback stays field 7 (additive evolution).

import (
	"strings"
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"

	awarenesspb "github.com/globulario/sensei/golang/pb"
)

func controlPanelRequests() []protoMsg {
	return []protoMsg{
		{"GetArchitectureControlSnapshotRequest", (&awarenesspb.GetArchitectureControlSnapshotRequest{}).ProtoReflect().Descriptor()},
		{"ListArchitectureArtifactsRequest", (&awarenesspb.ListArchitectureArtifactsRequest{}).ProtoReflect().Descriptor()},
		{"GetArchitectureArtifactStateRequest", (&awarenesspb.GetArchitectureArtifactStateRequest{}).ProtoReflect().Descriptor()},
		{"GetOntologyNavigationDescriptorRequest", (&awarenesspb.GetOntologyNavigationDescriptorRequest{}).ProtoReflect().Descriptor()},
	}
}

type protoMsg struct {
	name string
	desc protoreflect.MessageDescriptor
}

// No request field may name a filesystem root, working directory, workspace folder, canonical
// class, or search text. (ListArchitectureArtifactsRequest.class_filter is a VISIBILITY filter
// over already-resolved rows, explicitly allowed; canonical_class is not.)
func TestControlPanelRequests_NoForbiddenFields(t *testing.T) {
	forbidden := []string{"root", "cwd", "workspace", "path", "dir", "canonical_class", "search"}
	for _, m := range controlPanelRequests() {
		fields := m.desc.Fields()
		for i := 0; i < fields.Len(); i++ {
			name := string(fields.Get(i).Name())
			for _, bad := range forbidden {
				if strings.Contains(name, bad) {
					t.Errorf("%s.%s must not exist: requests carry logical identities only", m.name, name)
				}
			}
		}
	}
}

// Transport-boundary proof: no inbound request (transitively) accepts an AttentionItem or any
// other semantic-verdict message. Every attention record a client sees originates from validated
// controlstate construction on the server.
func TestControlPanelRequests_NoClientSuppliedSemanticVerdict(t *testing.T) {
	var verdictMessages = map[string]bool{
		"ArchitectureAttentionItem":       true,
		"ArchitectureArtifactState":       true,
		"ArchitectureControlSnapshot":     true,
		"ArchitectureArtifactIndex":       true,
		"ArchitectureArtifactSummary":     true,
		"ArchitectureDimensionAssessment": true,
		"ArchitectureLifecycleAssessment": true,
		"ArchitectureProjectionMeta":      true,
		"ArchitectureSourceStatus":        true,
		"ArchitectureScopedFeedbackRef":   true,
	}
	var walk func(t *testing.T, root string, d protoreflect.MessageDescriptor, seen map[protoreflect.FullName]bool)
	walk = func(t *testing.T, root string, d protoreflect.MessageDescriptor, seen map[protoreflect.FullName]bool) {
		if seen[d.FullName()] {
			return
		}
		seen[d.FullName()] = true
		fields := d.Fields()
		for i := 0; i < fields.Len(); i++ {
			f := fields.Get(i)
			if f.Kind() != protoreflect.MessageKind {
				continue
			}
			sub := f.Message()
			if verdictMessages[string(sub.Name())] {
				t.Errorf("%s transitively accepts semantic-verdict message %s (field %s)", root, sub.Name(), f.Name())
			}
			walk(t, root, sub, seen)
		}
	}
	for _, m := range controlPanelRequests() {
		walk(t, m.name, m.desc, map[protoreflect.FullName]bool{})
	}
}

// The navigation-descriptor request is EMPTY: registry-derived, no filesystem/repository input.
func TestNavigationDescriptorRequest_IsEmpty(t *testing.T) {
	d := (&awarenesspb.GetOntologyNavigationDescriptorRequest{}).ProtoReflect().Descriptor()
	if d.Fields().Len() != 0 {
		t.Fatalf("GetOntologyNavigationDescriptorRequest must have no fields, has %d", d.Fields().Len())
	}
}

// Response models carry no correctness-certification claim or synthetic score (Phase 6 stays the
// only correctness certifier; the panel never launders a score).
func TestControlPanelResponses_NoCertificationOrScore(t *testing.T) {
	responses := []protoMsg{
		{"GetArchitectureControlSnapshotResponse", (&awarenesspb.GetArchitectureControlSnapshotResponse{}).ProtoReflect().Descriptor()},
		{"ListArchitectureArtifactsResponse", (&awarenesspb.ListArchitectureArtifactsResponse{}).ProtoReflect().Descriptor()},
		{"GetArchitectureArtifactStateResponse", (&awarenesspb.GetArchitectureArtifactStateResponse{}).ProtoReflect().Descriptor()},
		{"GetOntologyNavigationDescriptorResponse", (&awarenesspb.GetOntologyNavigationDescriptorResponse{}).ProtoReflect().Descriptor()},
	}
	var walk func(t *testing.T, root string, d protoreflect.MessageDescriptor, seen map[protoreflect.FullName]bool)
	walk = func(t *testing.T, root string, d protoreflect.MessageDescriptor, seen map[protoreflect.FullName]bool) {
		if seen[d.FullName()] {
			return
		}
		seen[d.FullName()] = true
		fields := d.Fields()
		for i := 0; i < fields.Len(); i++ {
			f := fields.Get(i)
			name := string(f.Name())
			for _, bad := range []string{"certif", "score", "color", "layout"} {
				if strings.Contains(name, bad) {
					t.Errorf("%s carries forbidden field %s", root, name)
				}
			}
			if f.Kind() == protoreflect.MessageKind {
				walk(t, root, f.Message(), seen)
			}
		}
	}
	for _, m := range responses {
		walk(t, m.name, m.desc, map[protoreflect.FullName]bool{})
	}
}

// Phase 9.6 compatibility: BriefingResponse.feedback stays FIELD 7 with the same message type.
// Renumbering/retyping an existing field silently corrupts deployed clients.
func TestBriefingFeedbackField7Unchanged(t *testing.T) {
	d := (&awarenesspb.BriefingResponse{}).ProtoReflect().Descriptor()
	f := d.Fields().ByName("feedback")
	if f == nil {
		t.Fatal("BriefingResponse.feedback is missing")
	}
	if f.Number() != 7 {
		t.Fatalf("BriefingResponse.feedback must stay field 7, got %d", f.Number())
	}
	if string(f.Message().Name()) != "BriefingFeedbackProjection" {
		t.Fatalf("BriefingResponse.feedback must stay BriefingFeedbackProjection, got %s", f.Message().Name())
	}
}

// Every closed-vocabulary control-panel enum reserves 0 for UNSPECIFIED (which validation always
// rejects — it never means unknown; unknown is an explicit value).
func TestControlPanelEnums_UnspecifiedIsZero(t *testing.T) {
	enums := []protoreflect.EnumDescriptor{
		awarenesspb.ArchitectureAvailability(0).Descriptor(),
		awarenesspb.ArchitectureSourceAvailability(0).Descriptor(),
		awarenesspb.ArchitectureSourceImpact(0).Descriptor(),
		awarenesspb.ArchitectureArtifactClosure(0).Descriptor(),
		awarenesspb.ArchitectureDimensionState(0).Descriptor(),
		awarenesspb.ArchitectureLifecycleState(0).Descriptor(),
		awarenesspb.ArchitectureAttentionSeverity(0).Descriptor(),
		awarenesspb.ArchitectureAssessmentCoverage(0).Descriptor(),
	}
	for _, e := range enums {
		zero := e.Values().ByNumber(0)
		if zero == nil || !strings.HasSuffix(string(zero.Name()), "_UNSPECIFIED") {
			t.Errorf("enum %s must reserve 0 for *_UNSPECIFIED", e.FullName())
		}
	}
}

// Compile-time pins: the four read-only RPCs exist on both generated stubs (client + server).
// If protoc is re-run with a proto that drops or renames one, this stops compiling.
var (
	_ = awarenesspb.AwarenessGraphClient.GetArchitectureControlSnapshot
	_ = awarenesspb.AwarenessGraphClient.ListArchitectureArtifacts
	_ = awarenesspb.AwarenessGraphClient.GetArchitectureArtifactState
	_ = awarenesspb.AwarenessGraphClient.GetOntologyNavigationDescriptor
	_ = (&awarenesspb.UnimplementedAwarenessGraphServer{}).GetArchitectureControlSnapshot
	_ = (&awarenesspb.UnimplementedAwarenessGraphServer{}).ListArchitectureArtifacts
	_ = (&awarenesspb.UnimplementedAwarenessGraphServer{}).GetArchitectureArtifactState
	_ = (&awarenesspb.UnimplementedAwarenessGraphServer{}).GetOntologyNavigationDescriptor
)
