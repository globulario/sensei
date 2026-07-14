package awarenesspb_test

// Generated-code smoke test.
//
// What this test pins:
//
//   - Every message type named in the proto contract has a Go type
//     under the awarenesspb package. If `protoc` is re-run with a
//     proto file that drops or renames a message, this test stops
//     compiling — which is the right failure mode for a contract
//     surface that consumers will depend on.
//
//   - The BriefingStatus enum values exist as Go constants with the
//     expected names. The wire-format integer values are NOT
//     asserted here — those are protobuf's contract, not ours.
//
//   - protoc-gen-go-grpc has emitted both the client and server stubs
//     (UnimplementedAwarenessGraphServer is what every concrete
//     server embeds for forward-compat). If the gRPC plugin is ever
//     swapped for one that omits the unimplemented helper, the
//     compile-time assertion below catches it.
//
// What this test does NOT pin:
//
//   - Wire-format compatibility. Adding/removing fields legally per
//     proto3 won't break callers; testing the field numbers would
//     duplicate what protobuf's own conformance suite covers.
//
//   - Server behaviour. There is no server yet — that's Phase 2 Step 3.
//     This test only proves the generated package compiles and the
//     contract types exist.

import (
	"testing"

	awarenesspb "github.com/globulario/sensei/golang/pb"
)

// TestGeneratedMessageTypesExist references every message type from
// the proto contract. The references are deliberately empty struct
// literals — they cost nothing at runtime but force the compiler to
// resolve each name. A regeneration that drops a type fails to build,
// which is the signal we want.
func TestGeneratedMessageTypesExist(t *testing.T) {
	_ = &awarenesspb.BriefingRequest{}
	_ = &awarenesspb.BriefingResponse{}
	_ = &awarenesspb.ImpactRequest{}
	_ = &awarenesspb.ImpactResponse{}
	_ = &awarenesspb.KnowledgeNode{}
	_ = &awarenesspb.CodeAnchor{}
	_ = &awarenesspb.QueryRequest{}
	_ = &awarenesspb.QueryRow{}
	_ = &awarenesspb.QueryResponse{}
	_ = &awarenesspb.ResolveRequest{}
	_ = &awarenesspb.ResolveResponse{}
}

// TestBriefingStatusEnumValuesExist pins the three named enum
// constants. The wire-format values (0, 1, 2) come from protobuf and
// are not re-asserted here.
func TestBriefingStatusEnumValuesExist(t *testing.T) {
	_ = awarenesspb.BriefingStatus_BRIEFING_STATUS_OK
	_ = awarenesspb.BriefingStatus_BRIEFING_STATUS_EMPTY
	_ = awarenesspb.BriefingStatus_BRIEFING_STATUS_DEGRADED
}

func TestQueryEnumsExist(t *testing.T) {
	_ = awarenesspb.QueryMode_QUERY_MODE_BY_FILE
	_ = awarenesspb.QueryMode_QUERY_MODE_BY_ID
	_ = awarenesspb.QueryMode_QUERY_MODE_BY_CLASS
	_ = awarenesspb.QueryMode_QUERY_MODE_RELATED
	_ = awarenesspb.QueryClass_QUERY_CLASS_INVARIANT
	_ = awarenesspb.QueryClass_QUERY_CLASS_FAILURE_MODE
	_ = awarenesspb.QueryClass_QUERY_CLASS_INCIDENT_PATTERN
	_ = awarenesspb.QueryClass_QUERY_CLASS_INTENT
	_ = awarenesspb.QueryClass_QUERY_CLASS_SYMBOL
	_ = awarenesspb.QueryClass_QUERY_CLASS_SOURCE_FILE
	_ = awarenesspb.QueryClass_QUERY_CLASS_ARCHITECTURE_CLAIM
	_ = awarenesspb.QueryClass_QUERY_CLASS_OPEN_QUESTION
	_ = awarenesspb.QueryClass_QUERY_CLASS_ARCHITECT_ANSWER
}

func TestMetadataArchitectureClaimCountFieldExists(t *testing.T) {
	_ = (&awarenesspb.MetadataResponse{}).ArchitectureClaimCount
}

func TestGeneratedQueryClassArchitectureClaimExists(t *testing.T) {
	_ = awarenesspb.QueryClass_QUERY_CLASS_ARCHITECTURE_CLAIM
}

func TestGeneratedQueryClassOpenQuestionExists(t *testing.T) {
	_ = awarenesspb.QueryClass_QUERY_CLASS_OPEN_QUESTION
}

func TestGeneratedQueryClassArchitectAnswerExists(t *testing.T) {
	_ = awarenesspb.QueryClass_QUERY_CLASS_ARCHITECT_ANSWER
}

func TestMetadataOpenQuestionCountFieldExists(t *testing.T) {
	_ = (&awarenesspb.MetadataResponse{}).OpenQuestionCount
}

func TestMetadataArchitectAnswerCountFieldExists(t *testing.T) {
	_ = (&awarenesspb.MetadataResponse{}).ArchitectAnswerCount
}

// _ verifies at compile time that UnimplementedAwarenessGraphServer
// satisfies AwarenessGraphServer. Concrete server impls under
// golang/server/ (when they land in Phase 2 Step 3) will embed
// UnimplementedAwarenessGraphServer to inherit the forward-compatible
// "return Unimplemented" stubs for any future RPC additions.
var _ awarenesspb.AwarenessGraphServer = (*awarenesspb.UnimplementedAwarenessGraphServer)(nil)
