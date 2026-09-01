// SPDX-License-Identifier: AGPL-3.0-only

package main

// Reachability must reach the AUTOMATED caller, not only the human one.
//
// It was computed while printing the human-readable authority block, so --json
// callers and the MCP bridge still saw "authoritative (current)" with no
// indication that the corpus had moved past the serving graph. That left the
// exact false-green path this check exists to close open for every automated
// caller -- which is most of them.

import (
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	awarenesspb "github.com/globulario/sensei/golang/pb"
)

// Captures what emitProtoJSON ACTUALLY PRINTS.
//
// A first version of this called withReachability directly, so removing the
// call from emitProtoJSON left the test green -- it proved the helper worked
// and not that anything used it, which is the whole finding it was written for.
func TestEmitProtoJSONActuallyPrintsReachability(t *testing.T) {
	resp := &awarenesspb.BriefingResponse{
		Authority: &awarenesspb.GraphAuthority{GraphBuildCommit: "96f19456f5fb"},
	}
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	code := emitProtoJSON(resp)
	w.Close()
	os.Stdout = old
	if code != 0 {
		t.Fatalf("emitProtoJSON returned %d", code)
	}
	out, _ := io.ReadAll(r)
	if !strings.Contains(string(out), `"reachability"`) {
		t.Fatalf("the printed JSON carries no reachability, so an automated caller sees none:\n%s", out)
	}
}

func TestJSONOutputCarriesReachability(t *testing.T) {
	resp := &awarenesspb.BriefingResponse{
		Authority: &awarenesspb.GraphAuthority{GraphBuildCommit: "96f19456f5fb"},
	}
	encoded := []byte(`{"authority":{"graph_build_commit":"96f19456f5fb"}}`)

	out := withReachability(encoded, resp)
	var obj map[string]interface{}
	if err := json.Unmarshal(out, &obj); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	r, ok := obj["reachability"].(map[string]interface{})
	if !ok {
		t.Fatalf("an automated caller sees no reachability at all: %s", out)
	}
	for _, k := range []string{"state", "reachable", "detail", "asserts_absence"} {
		if _, present := r[k]; !present {
			t.Errorf("the structured assessment omits %q: %s", k, out)
		}
	}
	// No state may claim the graph has nothing to say.
	if r["asserts_absence"] != false {
		t.Error("a reachability state claimed absence of law")
	}
	// The authority block itself must be untouched: this is additive.
	if _, ok := obj["authority"]; !ok {
		t.Errorf("adding reachability displaced the response: %s", out)
	}
}

// A response whose authority states no build commit gets NO reachability key.
//
// Absent, not a fabricated "unknown": the question was never askable for that
// response, and answering it anyway would be an assessment of nothing.
func TestAResponseWithNoBuildCommitCarriesNoAssessment(t *testing.T) {
	resp := &awarenesspb.BriefingResponse{Authority: &awarenesspb.GraphAuthority{}}
	out := withReachability([]byte(`{"authority":{}}`), resp)
	if strings.Contains(string(out), "reachability") {
		t.Fatalf("an unaskable question was answered anyway: %s", out)
	}
}
