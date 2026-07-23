// SPDX-License-Identifier: AGPL-3.0-only

package whyinvestigation

import (
	"context"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/howextract"
	"github.com/globulario/sensei/golang/architecture/investigation"
)

func TestMissingGitProducesUnavailableDocument(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../howextract/testdata/deterministic_repo")); err != nil {
		t.Fatal(err)
	}
	run(t, root, "init")
	run(t, root, "add", ".")
	cmd := exec.Command("git", "-C", root, "-c", "user.name=test", "-c", "user.email=test@example.com", "commit", "-m", "seed")
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_DATE=2026-01-01T00:00:00Z", "GIT_COMMITTER_DATE=2026-01-01T00:00:00Z")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("commit: %v: %s", err, out)
	}
	revision := run(t, root, "rev-parse", "HEAD")
	binding := architecture.ClaimDocumentBinding{RepositoryDomain: "example.com/why", Revision: revision, RevisionStatus: "clean", GraphDigestStatus: "none"}
	how, err := howextract.Extract(root, howextract.Options{CapturedAt: "2026-01-01T00:00:00Z", Repository: binding, ResourceLimits: map[string]string{"test": "1"}})
	if err != nil {
		t.Fatal(err)
	}
	req := CaptureRequest{Repository: binding, How: how, Query: Query{ID: "query", TargetObservationIDs: []string{how.Observations[0].ID}}, Range: GitRange{Start: revision, End: revision}, CapturedAt: "2026-01-01T00:00:00Z"}
	doc, err := Extract(context.Background(), t.TempDir(), req)
	if err != nil {
		t.Fatal(err)
	}
	if err := investigation.Validate(doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Coverage) != 1 || doc.Coverage[0].Status != investigation.CoverageUnavailable || doc.Coverage[0].Reason != "local Git repository unavailable" {
		t.Fatalf("unexpected unavailable document: %+v", doc.Coverage)
	}
	if len(doc.RawEvidence) != 0 || len(doc.Observations) != 0 || len(doc.CandidateClaims) != 0 {
		t.Fatalf("unavailable provider emitted non-evidence output")
	}
}

func TestDeterministicGitEvidenceAndEmptyRange(t *testing.T) {
	root, req, first, second := gitFixture(t)
	req.Range = GitRange{Start: first + "~1", End: second}
	one, err := Extract(context.Background(), root, req)
	if err != nil {
		t.Fatal(err)
	}
	two, err := Extract(context.Background(), root, req)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(one, two) || one.Receipt.OutputDocumentDigestSHA256 != two.Receipt.OutputDocumentDigestSHA256 {
		t.Fatal("identical Git requests were not deterministic")
	}
	if len(one.RawEvidence) != 2 || !strings.Contains(one.RawEvidence[0].CapturedContent+one.RawEvidence[1].CapturedContent, "Move configuration ownership to controller") || !strings.Contains(one.RawEvidence[0].CapturedContent+one.RawEvidence[1].CapturedContent, "Configuration ownership remains with node agent") {
		t.Fatalf("contradictory history was not preserved: %+v", one.RawEvidence)
	}
	req.Range = GitRange{Start: second, End: second}
	empty, err := Extract(context.Background(), root, req)
	if err != nil {
		t.Fatal(err)
	}
	if empty.Coverage[0].Status != investigation.CoverageNoResult {
		t.Fatalf("completed empty range = %q", empty.Coverage[0].Status)
	}
}

func TestCaptureTimestampDoesNotChangeGitEvidenceIdentity(t *testing.T) {
	root, req, first, second := gitFixture(t)
	req.Range = GitRange{Start: first + "~1", End: second}
	one, err := Extract(context.Background(), root, req)
	if err != nil {
		t.Fatal(err)
	}
	req.CapturedAt = "2026-01-02T00:00:00Z"
	two, err := Extract(context.Background(), root, req)
	if err != nil {
		t.Fatal(err)
	}
	if one.Binding.EvidenceSnapshotDigestSHA256 != two.Binding.EvidenceSnapshotDigestSHA256 || one.Binding.Why.QueryDigestSHA256 != two.Binding.Why.QueryDigestSHA256 || one.RawEvidence[0].ID != two.RawEvidence[0].ID || one.RawEvidence[0].ContentDigestSHA256 != two.RawEvidence[0].ContentDigestSHA256 {
		t.Fatal("capture timestamp changed stable Git evidence identity")
	}
}

func TestInvalidWhyRequestsAreRefused(t *testing.T) {
	root, req, first, second := gitFixture(t)
	req.Range = GitRange{Start: first + "~1", End: second}
	for _, mutate := range []func(*CaptureRequest){
		func(r *CaptureRequest) { r.How.Receipt.OutputDocumentDigestSHA256 = "not-a-digest" },
		func(r *CaptureRequest) { r.How.Receipt.OutputDocumentDigestSHA256 = strings.Repeat("0", 64) },
		func(r *CaptureRequest) { r.Query.TargetObservationIDs = []string{"missing"} },
		func(r *CaptureRequest) { r.Repository.RepositoryDomain = "other.example" },
		func(r *CaptureRequest) { r.Range.End = "not-a-commit" },
	} {
		bad := req
		mutate(&bad)
		doc, err := Extract(context.Background(), root, bad)
		if err == nil || doc.SchemaVersion != "" {
			t.Fatal("invalid WHY request was accepted")
		}
	}
}

func TestShallowHistoryKeepsEvidenceButSignalsLimitation(t *testing.T) {
	root, req, first, second := gitFixture(t)
	req.Range = GitRange{Start: first, End: second}
	if err := os.WriteFile(root+"/.git/shallow", []byte(first+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	doc, err := Extract(context.Background(), root, req)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Coverage[0].Status != investigation.CoverageSupporting || len(doc.RawEvidence) == 0 || len(doc.Coverage[0].Limitations) == 0 {
		t.Fatalf("shallow evidence was not visibly partial: %+v", doc.Coverage[0])
	}
}

func gitFixture(t *testing.T) (string, CaptureRequest, string, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../howextract/testdata/deterministic_repo")); err != nil {
		t.Fatal(err)
	}
	run(t, root, "init")
	run(t, root, "add", ".")
	commit := func(message, date string) string {
		cmd := exec.Command("git", "-C", root, "-c", "user.name=test", "-c", "user.email=test@example.com", "commit", "-m", message)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_DATE="+date, "GIT_COMMITTER_DATE="+date)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("commit: %v: %s", err, out)
		}
		return run(t, root, "rev-parse", "HEAD")
	}
	commit("seed", "2025-12-31T00:00:00Z")
	if err := os.WriteFile(root+"/api/first.go", []byte("package api\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, root, "add", ".")
	first := commit("Move configuration ownership to controller", "2026-01-01T00:00:00Z")
	if err := os.WriteFile(root+"/api/note.go", []byte("package api\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, root, "add", ".")
	second := commit("Configuration ownership remains with node agent", "2026-01-02T00:00:00Z")
	binding := architecture.ClaimDocumentBinding{RepositoryDomain: "example.com/why", Revision: second, RevisionStatus: "clean", GraphDigestStatus: "none"}
	how, err := howextract.Extract(root, howextract.Options{CapturedAt: "2026-01-01T00:00:00Z", Repository: binding, ResourceLimits: map[string]string{"test": "1"}})
	if err != nil {
		t.Fatal(err)
	}
	return root, CaptureRequest{Repository: binding, How: how, Query: Query{ID: "query", TargetObservationIDs: []string{how.Observations[0].ID}}, CapturedAt: "2026-01-01T00:00:00Z"}, first, second
}

func run(t *testing.T, root string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", root}, args...)...).Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}
