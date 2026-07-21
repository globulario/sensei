// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture/identity"
	qd "github.com/globulario/sensei/golang/architecture/questiondisposition"
)

// seedPromotableCLI extends the disposition CLI seed with a recorded
// answered+reusable_candidate disposition ready to promote.
func seedPromotableCLI(t *testing.T) (dispCLIEnv, string) {
	t.Helper()
	env := seedDispositionCLI(t)
	qid := firstQuestionID(t, env)
	cand, err := qd.Prepare(qd.PrepareRequest{
		TaskDirectory: env.TaskDir, RepositoryRoot: env.Repo, IdentityRoot: identity.Root(env.Repo),
		QuestionID: qid, Disposition: qd.DispositionAnswered, Reusability: qd.ReusabilityReusableCandidate,
		Rationale: "the intended basis is X", AnswerID: "answer.1", AnswerBytes: []byte("the intended basis is X"),
	})
	if err != nil {
		t.Fatalf("disposition prepare: %v", err)
	}
	if _, err := qd.RecordDisposition(context.Background(), qd.RecordRequest{TaskDirectory: env.TaskDir, Candidate: cand}); err != nil {
		t.Fatalf("disposition record: %v", err)
	}
	return env, cand.Receipt.ReceiptDigestSHA256
}

func capturePromote(t *testing.T, args []string) (string, int) {
	t.Helper()
	old := os.Stdout
	pr, pw, _ := os.Pipe()
	os.Stdout = pw
	code := runPromoteAnswer(args)
	_ = pw.Close()
	os.Stdout = old
	out, _ := io.ReadAll(pr)
	return string(out), code
}

func promoteArgs(env dispCLIEnv, disp string) []string {
	return []string{
		"-repo", env.Repo, "-task-dir", env.TaskDir, "-domain", "github.com/globulario/sensei",
		"-disposition-receipt", disp,
		"-kind", "invariant", "-id", "invariant.promoted.reload_validates",
		"-title", "Reload validates before serving", "-description", "promoted from an accepted architect answer",
		"-source-file", "golang/server/reload.go", "-related-failure", "failure.x",
		"-format", "json",
	}
}

// TestCLIPromoteHappyPathAndReplay: the CLI reaches promotion_committed through the
// production owner (exit 0), and an exact replay returns the same identities (exit 0).
func TestCLIPromoteHappyPathAndReplay(t *testing.T) {
	env, disp := seedPromotableCLI(t)
	out, code := capturePromote(t, promoteArgs(env, disp))
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	var o promoteOutput
	if err := json.Unmarshal([]byte(out), &o); err != nil {
		t.Fatalf("json: %v\n%s", err, out)
	}
	if o.Outcome != "committed" {
		t.Fatalf("outcome = %s, want committed", o.Outcome)
	}
	if o.ReceiptDigestSHA256 == "" || o.CommittedCausalIdentitySHA256 == "" || o.PromotionLineageID == "" {
		t.Fatalf("committed output missing identities: %+v", o)
	}
	if o.CorrectnessCertified {
		t.Fatal("correctness_certified must be false")
	}
	out2, code2 := capturePromote(t, promoteArgs(env, disp))
	if code2 != 0 {
		t.Fatalf("replay exit %d: %s", code2, out2)
	}
	var o2 promoteOutput
	_ = json.Unmarshal([]byte(out2), &o2)
	if o2.Outcome != "exact_replay" || o2.ReceiptDigestSHA256 != o.ReceiptDigestSHA256 {
		t.Fatalf("replay = %s / %s, want exact_replay with same receipt", o2.Outcome, o2.ReceiptDigestSHA256)
	}
}

// TestCLIPromoteRefusalsRenderDistinctly: ineligible and unauthorized refuse with
// distinct outcomes and a non-zero refusal exit code, before any mutation.
func TestCLIPromoteRefusalsRenderDistinctly(t *testing.T) {
	env, disp := seedPromotableCLI(t)
	// Wrong disposition digest → ineligible.
	out, code := capturePromote(t, promoteArgs(env, strings.Repeat("0", 64)))
	if code != 3 {
		t.Fatalf("ineligible exit = %d, want 3\n%s", code, out)
	}
	var o promoteOutput
	_ = json.Unmarshal([]byte(out), &o)
	if o.Outcome != "ineligible_disposition" {
		t.Fatalf("outcome = %s, want ineligible_disposition", o.Outcome)
	}
	// Unauthorized promotion actor (empty identity root) → authority refusal, same env+disposition.
	args := append(promoteArgs(env, disp), "-identity-root", t.TempDir())
	out3, code3 := capturePromote(t, args)
	if code3 != 3 {
		t.Fatalf("unauthorized exit = %d, want 3\n%s", code3, out3)
	}
	var o3 promoteOutput
	_ = json.Unmarshal([]byte(out3), &o3)
	if o3.Outcome != "authority_refusal" {
		t.Fatalf("outcome = %s, want authority_refusal", o3.Outcome)
	}
}

// TestCLIPromoteIsThinAdapter proves by source scan that the CLI holds no policy
// and does not import the owner's internal machinery.
func TestCLIPromoteIsThinAdapter(t *testing.T) {
	data, err := os.ReadFile("cmd_promote_answer.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(data)
	for _, forbidden := range []string{
		"architecture/governedmutation", "architecture/repograph", "OpenJournal",
		"architecture/ledger", "architecture/certification", "architecture/completion",
		"ComputeLineageID", "CorrectnessCertified: true",
	} {
		if strings.Contains(src, forbidden) {
			t.Errorf("CLI references forbidden internal %q", forbidden)
		}
	}
	// The receipt digest / lineage id / node IRI must not be settable inputs.
	for _, forbidden := range []string{"receipt-digest", "lineage-id", "node-iri", "graph-digest", "committed-causal"} {
		if strings.Contains(src, forbidden) {
			t.Errorf("CLI exposes a trusted derived field as input %q", forbidden)
		}
	}
	_ = filepath.Separator
}
