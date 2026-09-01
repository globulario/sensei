// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"encoding/json"
	"strings"

	"fmt"
	awarenesspb "github.com/globulario/sensei/golang/pb"
	"github.com/globulario/sensei/golang/reachability"
	"os"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// emitProtoJSON writes a gRPC response as canonical proto JSON and returns the
// process exit code.
//
// It exists because encoding/json on a generated proto renders enum fields as
// their integer values (risk_class: 4), forcing every agent/tool consumer to
// reverse-map the numbers. protojson renders enums as their string names
// (risk_class: "SECURITY_RISK"), which is the stable, self-describing contract
// a machine reader needs. UseProtoNames keeps the snake_case field names from
// the .proto so only the enum encoding changes; zero fields stay omitted.
func emitProtoJSON(m proto.Message) int {
	b, err := protojson.MarshalOptions{Multiline: true, Indent: "  ", UseProtoNames: true}.Marshal(m)
	if err != nil {
		fmt.Fprintf(os.Stderr, "awg: encode json: %v\n", err)
		return 1
	}
	fmt.Println(string(withReachability(b, m)))
	return 0
}

// withReachability adds the reachability assessment to a JSON response.
//
// WHY IT IS HERE AND NOT ONLY IN THE HUMAN RENDERER. Reachability was computed
// while printing the human-readable authority block, so --json callers and the
// MCP bridge still saw "authoritative (current)" with no indication that the
// corpus had moved past the serving graph -- THE EXACT FALSE-GREEN PATH THIS
// CHECK EXISTS TO CLOSE, left open for every automated caller, which is most
// of them.
//
// It is added to the encoded object rather than to the protobuf: the assessment
// is a property of the CALLER's relationship to the graph, not of the server's
// answer, and a server cannot compute it because it does not hold the corpus.
// Encoding it as a response field would claim the server asserted it.
//
// A response whose authority block states no build commit gets no reachability
// key at all -- absent rather than a fabricated "unknown", because the question
// was never askable for that response.
func withReachability(encoded []byte, m proto.Message) []byte {
	commit := authorityBuildCommit(m)
	if strings.TrimSpace(commit) == "" {
		return encoded
	}
	var obj map[string]interface{}
	if err := json.Unmarshal(encoded, &obj); err != nil {
		return encoded
	}
	a := reachability.ResolveFromGit(context.Background(), reachabilityRepoRoot(), commit)
	obj["reachability"] = map[string]interface{}{
		"state":           string(a.State),
		"reachable":       a.Reachable(),
		"commits_ahead":   a.CommitsAhead,
		"detail":          a.Detail,
		"asserts_absence": a.AssertsAbsence(),
	}
	out, err := json.MarshalIndent(obj, "", "  ")
	if err != nil {
		return encoded
	}
	return out
}

// authorityBuildCommit pulls the build commit out of whatever governed response
// this is, without the CLI needing to know which one it is.
func authorityBuildCommit(m proto.Message) string {
	type withAuthority interface {
		GetAuthority() *awarenesspb.GraphAuthority
	}
	if wa, ok := m.(withAuthority); ok {
		if a := wa.GetAuthority(); a != nil {
			return a.GetGraphBuildCommit()
		}
	}
	return ""
}
