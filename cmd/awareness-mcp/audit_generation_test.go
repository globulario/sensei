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
	"strings"
	"sync/atomic"
	"testing"

	"github.com/globulario/sensei/golang/architecture/diffaudit"
	awarenesspb "github.com/globulario/sensei/golang/pb"
)

func auditDiffFixture(t *testing.T, metadata func(context.Context, *awarenesspb.MetadataRequest) (*awarenesspb.MetadataResponse, error)) (string, string) {
	t.Helper()
	head := testGitHEAD(t)
	fake := fakeClient{
		metadata: metadata,
		editCheck: func(_ context.Context, _ *awarenesspb.EditCheckRequest) (*awarenesspb.EditCheckResponse, error) {
			return &awarenesspb.EditCheckResponse{}, nil
		},
		impact: func(_ context.Context, _ *awarenesspb.ImpactRequest) (*awarenesspb.ImpactResponse, error) {
			return &awarenesspb.ImpactResponse{Authority: testCurrentAuthority(head)}, nil
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
	text, gen := auditDiffFixture(t, testServingGeneration("c0b660fc42a5"))
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
	text, gen := auditDiffFixture(t, switching)
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
			return &awarenesspb.EditCheckResponse{}, nil
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
