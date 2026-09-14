// SPDX-License-Identifier: AGPL-3.0-only

package main

// Law 5 through the real tool path: `awareness_audit_diff` is the call that decides
// admission, and until now it reported the rule-snapshot commit while saying nothing
// about WHICH graph generation answered. On this installation the rule snapshot
// commit belongs to the services repository, so two different Sensei generations
// share it, and switching between them mid-audit was invisible.
//
// These are behavioural: they go through br.callTool, so they also bind the wiring —
// the checker implementing GraphGenerationReporter, and the audit's own domain
// reaching the metadata call rather than some other domain's graph being asked.

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/globulario/sensei/golang/architecture/diffaudit"
	awarenesspb "github.com/globulario/sensei/golang/pb"
)

// auditDiffFixture runs one audit. servedGeneration is the generation ONE graph reports
// everywhere -- on the metadata samples and on the authority of every response -- because the
// evaluator now binds each contributing query to the identity its own response carries. A
// fixture whose responses named a different graph from its samples would manufacture a switch
// and refuse every audit for a disagreement the test never intended.
func auditDiffFixture(t *testing.T, servedGeneration string,
	metadata func(context.Context, *awarenesspb.MetadataRequest) (*awarenesspb.MetadataResponse, error)) (string, string) {
	t.Helper()
	head := testGitHEAD(t)
	respAuthority := func() *awarenesspb.GraphAuthority {
		a := testCurrentAuthority(head)
		a.LiveStoreGraphDigestSha256 = servedGeneration
		return a
	}
	fake := fakeClient{
		metadata: metadata,
		editCheck: func(_ context.Context, _ *awarenesspb.EditCheckRequest) (*awarenesspb.EditCheckResponse, error) {
			return &awarenesspb.EditCheckResponse{Authority: respAuthority()}, nil
		},
		impact: func(_ context.Context, _ *awarenesspb.ImpactRequest) (*awarenesspb.ImpactResponse, error) {
			return &awarenesspb.ImpactResponse{Authority: respAuthority()}, nil
		},
	}
	br := testBridge(fake)
	diff := `diff --git a/main.go b/main.go
new file mode 100644
--- /dev/null
+++ b/main.go
@@ -0,0 +1,2 @@
+package main
+func main() {}
`
	res, err := br.callTool(context.Background(), "awareness_audit_diff", map[string]interface{}{
		"diff":          diff,
		"expected_head": head,
	})
	if err != nil {
		t.Fatalf("callTool awareness_audit_diff: %v", err)
	}
	return res.Text, structGeneration(res.Structured)
}

func TestTheAuditReportsWhichGenerationAnsweredIt(t *testing.T) {
	text, gen := auditDiffFixture(t, "c0b660fc42a5", testServingGeneration("c0b660fc42a5"))
	if !strings.Contains(text, "decision: pass") {
		t.Fatalf("a fully bound audit did not pass:\n%s", text)
	}
	if gen != "c0b660fc42a5" {
		t.Errorf("the audit did not record the generation that answered it: %q", gen)
	}
}

// The graph changes underneath one audit. Both digests are reachable and healthy;
// nothing is unavailable. It must still refuse.
func TestTheAuditRefusesWhenTheGraphGenerationChangesUnderneathIt(t *testing.T) {
	var calls atomic.Int64
	switching := func(_ context.Context, _ *awarenesspb.MetadataRequest) (*awarenesspb.MetadataResponse, error) {
		if calls.Add(1) == 1 {
			return &awarenesspb.MetadataResponse{LiveStoreGraphDigestSha256: "c0b660fc42a5"}, nil
		}
		return &awarenesspb.MetadataResponse{LiveStoreGraphDigestSha256: "230a74f68fed"}, nil
	}
	text, gen := auditDiffFixture(t, "c0b660fc42a5", switching)
	if strings.Contains(text, "decision: pass") {
		t.Errorf("an audit answered by two different generations passed:\n%s", text)
	}
	if !strings.Contains(text, "cannot_verify") {
		t.Errorf("the audit was not degraded:\n%s", text)
	}
	if gen != "" {
		t.Errorf("a generation was recorded for an audit that had two: %q", gen)
	}
	if calls.Load() < 2 {
		t.Errorf("the generation was sampled %d times; law 5 needs a pair bracketing the queries", calls.Load())
	}
}

// The audit must ask about ITS OWN domain. Asking metadata about another domain
// would compare this audit against a different graph and call the disagreement a
// switch — a refusal for the wrong reason is not a passing grade.
func TestTheAuditAsksAboutTheDomainItIsAuditing(t *testing.T) {
	var asked []string
	recording := func(_ context.Context, in *awarenesspb.MetadataRequest) (*awarenesspb.MetadataResponse, error) {
		asked = append(asked, in.GetDomain())
		return &awarenesspb.MetadataResponse{LiveStoreGraphDigestSha256: "c0b660fc42a5"}, nil
	}
	head := testGitHEAD(t)
	fake := fakeClient{
		metadata: recording,
		editCheck: func(_ context.Context, _ *awarenesspb.EditCheckRequest) (*awarenesspb.EditCheckResponse, error) {
			return editCheckFromCurrentGraph(), nil
		},
		impact: func(_ context.Context, _ *awarenesspb.ImpactRequest) (*awarenesspb.ImpactResponse, error) {
			return &awarenesspb.ImpactResponse{Authority: testCurrentAuthority(head)}, nil
		},
	}
	br := testBridge(fake)
	if _, err := br.callTool(context.Background(), "awareness_audit_diff", map[string]interface{}{
		"diff": `diff --git a/main.go b/main.go
new file mode 100644
--- /dev/null
+++ b/main.go
@@ -0,0 +1,2 @@
+package main
+func main() {}
`,
		"expected_head": head,
		"domain":        "example.com/acme/thing",
	}); err != nil {
		t.Fatalf("callTool: %v", err)
	}
	if len(asked) == 0 {
		t.Fatal("the audit never asked which generation was answering")
	}
	for _, d := range asked {
		if d != "example.com/acme/thing" {
			t.Errorf("the audit asked about domain %q while auditing example.com/acme/thing", d)
		}
	}
}

// structGeneration reads GraphGeneration off the structured audit result.
func structGeneration(v interface{}) string {
	if ar, ok := v.(*diffaudit.AuditResult); ok {
		return ar.GraphGeneration
	}
	return ""
}

// A STALE per-response generation must never stand in for a missing one, and the identity the
// audit binds to must come from the RESPONSE rather than from a separate sample.
//
// LastImpactGeneration reports the identity carried by the most recent impact response. If the
// field were not cleared before each query, a response stating no generation would inherit the
// previous one, and the audit would bind a query to a graph that did not answer it -- the exact
// confusion this provenance model replaced.
func TestTheImpactGenerationComesFromTheResponseAndIsNotStale(t *testing.T) {
	head := testGitHEAD(t)
	graphCommit := strings.Repeat("a", len(head))
	served := strings.Repeat("7", 64)

	auth := testCurrentAuthority(graphCommit)
	auth.LiveStoreGraphDigestSha256 = served
	fake := fakeClient{
		impact: func(_ context.Context, _ *awarenesspb.ImpactRequest) (*awarenesspb.ImpactResponse, error) {
			return &awarenesspb.ImpactResponse{Authority: auth}, nil
		},
	}
	checker := &mcpSingleFileChecker{bridge: testBridge(fake), root: ".", expectedHead: head}
	checker.lastImpactGeneration = "a-previous-generation"

	if _, _, _, _, err := checker.GetFileImpact(context.Background(), "internal/example.go",
		"github.com/globulario/sensei-code"); err != nil {
		t.Fatalf("GetFileImpact: %v", err)
	}
	if got := checker.LastImpactGeneration(); got != served {
		t.Errorf("the generation bound to this query is %q, want the served digest %q carried by the response", got, served)
	}

	// Now a response that states none: the previous value must not survive.
	auth2 := testCurrentAuthority(graphCommit)
	auth2.LiveStoreGraphDigestSha256 = ""
	fake2 := fakeClient{
		impact: func(_ context.Context, _ *awarenesspb.ImpactRequest) (*awarenesspb.ImpactResponse, error) {
			return &awarenesspb.ImpactResponse{Authority: auth2}, nil
		},
	}
	checker2 := &mcpSingleFileChecker{bridge: testBridge(fake2), root: ".", expectedHead: head}
	checker2.lastImpactGeneration = served
	if _, _, _, _, err := checker2.GetFileImpact(context.Background(), "internal/example.go",
		"github.com/globulario/sensei-code"); err != nil {
		t.Fatalf("GetFileImpact: %v", err)
	}
	if got := checker2.LastImpactGeneration(); got != "" {
		t.Errorf("a stale generation survived a response that stated none: %q", got)
	}
}

// THE PATH WHERE CLEARING MATTERS. GetFileImpact returns early when the query fails, when the
// graph is not authoritative, and when it exposes no commit identity. On those paths nothing
// assigns the generation, so without clearing it first the PREVIOUS query's identity survives
// and the audit would bind this query to a graph that never answered it.
//
// The first version of this witness drove the success path, where an unconditional assignment
// overwrites the field anyway -- so the mutant that removed the clear was equivalent and
// survived. The claim only has content on the early returns.
func TestAFailedImpactQueryLeavesNoGenerationBehind(t *testing.T) {
	head := testGitHEAD(t)
	stale := strings.Repeat("7", 64)

	cases := map[string]fakeClient{
		"the query fails": {
			impact: func(_ context.Context, _ *awarenesspb.ImpactRequest) (*awarenesspb.ImpactResponse, error) {
				return nil, errors.New("store unavailable")
			},
		},
		"the graph is not authoritative": {
			impact: func(_ context.Context, _ *awarenesspb.ImpactRequest) (*awarenesspb.ImpactResponse, error) {
				return &awarenesspb.ImpactResponse{Authority: &awarenesspb.GraphAuthority{
					Authoritative:              false,
					LiveStoreGraphDigestSha256: strings.Repeat("9", 64),
				}}, nil
			},
		},
	}
	for name, fake := range cases {
		t.Run(name, func(t *testing.T) {
			checker := &mcpSingleFileChecker{bridge: testBridge(fake), root: ".", expectedHead: head}
			checker.lastImpactGeneration = stale
			if _, _, _, _, err := checker.GetFileImpact(context.Background(), "internal/example.go",
				"github.com/globulario/sensei-code"); err == nil {
				t.Fatal("this witness needs the query to be refused")
			}
			if got := checker.LastImpactGeneration(); got != "" {
				t.Errorf("a previous query's generation survived a refused query: %q", got)
			}
		})
	}
}
