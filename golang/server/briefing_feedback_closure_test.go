// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture/briefingfeedback"
	awarenesspb "github.com/globulario/sensei/golang/pb"
	"github.com/globulario/sensei/golang/store"
	"google.golang.org/protobuf/proto"
)

// fingerprint is the consumer-agnostic semantic content of a projection — everything EXCEPT
// legitimate consumer-binding fields (task id, session id, requested identity, self-digest).
func fingerprint(p briefingfeedback.Projection) string {
	type rec struct {
		Node, Class, EffDomain string
		Files                  []string
	}
	type fnd struct{ Class, Reason string }
	v := struct {
		Availability string
		Records      []rec
		Findings     []fnd
	}{Availability: string(p.Availability)}
	for _, r := range p.Records {
		v.Records = append(v.Records, rec{r.GovernedNodeIRI, string(r.VerificationClass), r.EffectiveDomain, r.EffectiveFileScope})
	}
	for _, f := range p.Findings {
		v.Findings = append(v.Findings, fnd{string(f.Class), f.ReasonCode})
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// §7 parity: the canonical owner, a server-style request (no task binding), and a task-style
// request (task binding, file set = [file]) yield the SAME semantic fingerprint — differences
// are only the consumer-binding task id/session/digest.
func TestClosure_OwnerServerTaskParity(t *testing.T) {
	file := "golang/server/reload.go"
	repo := seedServerPromotion(t, []string{file})

	owner, err := briefingfeedback.Build(context.Background(), briefingfeedback.Request{
		RepositoryRoot: repo, RepositoryIdentity: feedbackTestDomain, RequestedDomain: feedbackTestDomain,
		RequestedFiles: []string{file},
	})
	if err != nil {
		t.Fatal(err)
	}
	server, err := briefingfeedback.Build(context.Background(), briefingfeedback.Request{
		RepositoryRoot: repo, RepositoryIdentity: feedbackTestDomain, RequestedDomain: feedbackTestDomain,
		RequestedFiles: []string{file}, // server: no task binding
	})
	if err != nil {
		t.Fatal(err)
	}
	task, err := briefingfeedback.Build(context.Background(), briefingfeedback.Request{
		RepositoryRoot: repo, RepositoryIdentity: feedbackTestDomain, RequestedDomain: feedbackTestDomain,
		RequestedFiles: []string{file},
		Task:           &briefingfeedback.TaskBinding{TaskID: "task.consumer", SessionID: "session.consumer", RepositoryDomain: feedbackTestDomain, Files: []string{file}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if fingerprint(owner) != fingerprint(server) || fingerprint(owner) != fingerprint(task) {
		t.Fatalf("owner/server/task fingerprints differ:\n owner=%s\n server=%s\n task=%s", fingerprint(owner), fingerprint(server), fingerprint(task))
	}
	// The task-bound projection differs ONLY in consumer-binding identity.
	if task.TaskID != "task.consumer" || server.TaskID != "" {
		t.Fatalf("consumer-binding identity not as expected")
	}
	if owner.DigestSHA256 == task.DigestSHA256 {
		t.Fatalf("task binding must change the self-digest")
	}
}

// §7 multi-file task test: a broader VERIFIED task file set (not consumer policy) is the only
// thing that changes the result — a promotion scoped to G is admitted for a task whose file set
// includes G but excluded for a server request scoped only to F.
func TestClosure_TaskFileSetExpansionChangesScopeNotPolicy(t *testing.T) {
	g := "golang/server/other.go"
	repo := seedServerPromotion(t, []string{g})

	// Server request scoped only to F (a different file) → out of scope → empty.
	server, err := briefingfeedback.Build(context.Background(), briefingfeedback.Request{
		RepositoryRoot: repo, RepositoryIdentity: feedbackTestDomain, RequestedDomain: feedbackTestDomain,
		RequestedFiles: []string{"golang/server/reload.go"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if server.Availability != briefingfeedback.FeedbackEmpty {
		t.Fatalf("server scoped to F must be empty, got %q", server.Availability)
	}
	// Task whose verified file set includes G → admitted.
	task, err := briefingfeedback.Build(context.Background(), briefingfeedback.Request{
		RepositoryRoot: repo, RepositoryIdentity: feedbackTestDomain, RequestedDomain: feedbackTestDomain,
		RequestedFiles: []string{"golang/server/reload.go"},
		Task:           &briefingfeedback.TaskBinding{TaskID: "t", SessionID: "s", RepositoryDomain: feedbackTestDomain, Files: []string{"golang/server/reload.go", g}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if task.Availability != briefingfeedback.FeedbackAvailable || len(task.Records) != 1 {
		t.Fatalf("task with G in its verified file set must admit, got %q recs=%d", task.Availability, len(task.Records))
	}
}

// §9 privacy: raw answer/rationale text, the absolute repo root, and internal errors never
// appear in any user-visible feedback output.
func TestClosure_PrivacySentinelsAbsent(t *testing.T) {
	const answerSentinel = "ZZ_ANSWER_SENTINEL_ZZ"
	file := "golang/server/reload.go"
	repo := seedServerPromotionWithAnswer(t, []string{file}, answerSentinel)
	s := testFeedbackServer(&briefingRepositoryContext{Root: repo, Domain: feedbackTestDomain})

	p, err := s.briefingFeedback(context.Background(), feedbackBriefingScope{effectiveDomain: feedbackTestDomain, file: file, rawFile: file, rawDomain: feedbackTestDomain})
	if err != nil {
		t.Fatal(err)
	}
	wire, err := briefingFeedbackToProto(p)
	if err != nil {
		t.Fatal(err)
	}
	pjson, _ := json.Marshal(p)
	wjson, _ := proto.Marshal(wire)
	surfaces := []string{string(pjson), string(wjson), briefingFeedbackProse(p), strings.Join(feedbackReferencedIDs(p), " ")}
	for _, blob := range surfaces {
		if strings.Contains(blob, answerSentinel) {
			t.Fatalf("raw answer sentinel leaked into a feedback surface")
		}
		if strings.Contains(blob, repo) {
			t.Fatalf("absolute repository root leaked into a feedback surface")
		}
	}
	// The record still carries structured provenance identities (question/answer IDs), which
	// are authorized — but never the raw answer text.
	if p.Records[0].AnswerID == "" || p.Records[0].QuestionID == "" {
		t.Fatalf("authorized structured provenance identities must be present")
	}
}

// §10 no-mutation: a byte-level repository snapshot is identical before and after a battery of
// task/server/owner feedback calls (available, out-of-scope, invalid, unavailable, task-only).
func TestClosure_NoMutation(t *testing.T) {
	file := "golang/server/reload.go"
	repo := seedServerPromotion(t, []string{file})
	before := snapshotRepo(t, repo)

	s := testFeedbackServer(&briefingRepositoryContext{Root: repo, Domain: feedbackTestDomain})
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		s.briefingFeedback(ctx, feedbackBriefingScope{effectiveDomain: feedbackTestDomain, file: file, rawFile: file, rawDomain: feedbackTestDomain})
		s.briefingFeedback(ctx, feedbackBriefingScope{effectiveDomain: feedbackTestDomain, file: "cmd/x/other.go", rawFile: "cmd/x/other.go", rawDomain: feedbackTestDomain})
		s.briefingFeedback(ctx, feedbackBriefingScope{effectiveDomain: feedbackTestDomain, file: file, rawFile: " padded.go", rawDomain: feedbackTestDomain}) // invalid
		s.briefingFeedback(ctx, feedbackBriefingScope{effectiveDomain: "github.com/foreign/x", file: file, rawFile: file, rawDomain: "github.com/foreign/x"}) // unavailable
		s.briefingFeedback(ctx, feedbackBriefingScope{taskOnly: true, effectiveDomain: feedbackTestDomain, rawDomain: feedbackTestDomain})                    // task-only
		briefingfeedback.Build(ctx, briefingfeedback.Request{RepositoryRoot: repo, RepositoryIdentity: feedbackTestDomain, RequestedDomain: feedbackTestDomain, RequestedFiles: []string{file}})
	}
	if after := snapshotRepo(t, repo); before != after {
		t.Fatal("feedback calls mutated the repository")
	}
}

// §11 determinism: repeated unchanged execution yields identical projection JSON, digest, and
// deterministic protobuf bytes.
func TestClosure_Determinism(t *testing.T) {
	file := "golang/server/reload.go"
	repo := seedServerPromotion(t, []string{file})
	s := testFeedbackServer(&briefingRepositoryContext{Root: repo, Domain: feedbackTestDomain})
	scope := feedbackBriefingScope{effectiveDomain: feedbackTestDomain, file: file, rawFile: file, rawDomain: feedbackTestDomain}

	a, _ := s.briefingFeedback(context.Background(), scope)
	b, _ := s.briefingFeedback(context.Background(), scope)
	aj, _ := json.Marshal(a)
	bj, _ := json.Marshal(b)
	if string(aj) != string(bj) || a.DigestSHA256 != b.DigestSHA256 {
		t.Fatal("projection is not deterministic")
	}
	wa, _ := briefingFeedbackToProto(a)
	wb, _ := briefingFeedbackToProto(b)
	da, _ := proto.MarshalOptions{Deterministic: true}.Marshal(wa)
	db, _ := proto.MarshalOptions{Deterministic: true}.Marshal(wb)
	if hex.EncodeToString(da) != hex.EncodeToString(db) {
		t.Fatal("deterministic protobuf bytes differ across identical calls")
	}
}

// snapshotRepo walks the repo and hashes every file's path + content into one digest.
func snapshotRepo(t *testing.T, root string) string {
	t.Helper()
	h := sha256.New()
	var paths []string
	filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			paths = append(paths, p)
		}
		return nil
	})
	sort.Strings(paths)
	for _, p := range paths {
		data, _ := os.ReadFile(p)
		h.Write([]byte(p))
		h.Write([]byte{0})
		h.Write(data)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// §8 end-to-end parity: on a configured server the RPC response's field 7, prose, referenced
// ids, and status all describe the SAME feedback world (available record).
func TestClosure_RpcStructuredProseReferenceParity(t *testing.T) {
	file := "golang/server/reload.go"
	repo := seedServerPromotion(t, []string{file})
	// Base EMPTY graph briefing (no impact) + available feedback → combined OK.
	s := newServer(fakeStore{impactForFile: func(context.Context, string) ([]store.ImpactFact, error) { return nil, nil }})
	s.briefingRepo = &briefingRepositoryContext{Root: repo, Domain: feedbackTestDomain}

	resp, err := s.Briefing(context.Background(), &awarenesspb.BriefingRequest{File: file, Domain: feedbackTestDomain})
	if err != nil {
		t.Fatal(err)
	}
	if resp.GetFeedback() == nil || resp.GetFeedback().GetAvailability() != awarenesspb.BriefingFeedbackAvailability_BRIEFING_FEEDBACK_AVAILABILITY_AVAILABLE {
		t.Fatalf("field 7 must be available, got %+v", resp.GetFeedback())
	}
	if len(resp.GetFeedback().GetRecords()) != 1 || resp.GetFeedback().GetRecords()[0].GetCanonicalRecordId() != "invariant.promoted.x" {
		t.Fatalf("field 7 record wrong: %+v", resp.GetFeedback().GetRecords())
	}
	const govID = "invariant:invariant.promoted.x"
	if !strings.Contains(resp.GetProse(), govID) {
		t.Fatalf("governed record missing from prose: %q", resp.GetProse())
	}
	found := false
	for _, id := range resp.GetReferencedIds() {
		if id == govID {
			found = true
		}
	}
	if !found {
		t.Fatalf("governed record missing from referenced_ids: %v", resp.GetReferencedIds())
	}
	// EMPTY base + available feedback = OK.
	if resp.GetStatus() != awarenesspb.BriefingStatus_BRIEFING_STATUS_OK {
		t.Fatalf("EMPTY base + available feedback must be OK, got %v", resp.GetStatus())
	}
}
