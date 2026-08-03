// SPDX-License-Identifier: AGPL-3.0-only

package fileinterpretation

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture/providerport"
)

const testObservedAt = "2026-08-03T00:00:00Z"

type recordingObserver struct {
	observed []string
}

func (o *recordingObserver) Observe(detail string) error {
	o.observed = append(o.observed, detail)
	return nil
}

func mustNewProvider(t *testing.T, path string) *Provider {
	t.Helper()
	p, err := New(Config{Path: path, ProviderID: "test.file-interpretation", ObservedAt: testObservedAt})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return p
}

func testRequest(t *testing.T) providerport.Request {
	t.Helper()
	req := providerport.NormalizeRequest(providerport.Request{
		SchemaVersion:              providerport.RequestSchemaVersion,
		RequestID:                  "test.request",
		Operation:                  providerport.OperationInterpretation,
		SessionDigestSHA256:        "0000000000000000000000000000000000000000000000000000000000000000",
		RepositoryDomain:           "github.com/example/test",
		BaseRevision:               "0000000000000000000000000000000000000000",
		ParentArtifactDigestSHA256: "0000000000000000000000000000000000000000000000000000000000000000",
		DeadlineAt:                 "2026-08-03T01:00:00Z",
		MaxObservationCount:        32,
		MaxObservationBytes:        65536,
	})
	digest, err := providerport.RequestDigest(req)
	if err != nil {
		t.Fatalf("RequestDigest: %v", err)
	}
	req.RequestDigestSHA256 = digest
	return req
}

const validInterpretationJSON = `{
	"objective": "prove the file-backed provider works",
	"applicable_intent": ["intent.test"],
	"binding_invariants": [],
	"relevant_contracts": [],
	"authority_boundaries": ["providers-have-no-admission-authority"],
	"known_failure_modes": [],
	"forbidden_fixes": [],
	"required_proof_obligations": [],
	"assumptions": ["hand-authored, not a governed resolver"],
	"unresolved_questions": [],
	"source_references": [],
	"limitations": []
}`

func TestProvider_DescribeAdvertisesOnlyInterpretation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "interpretation.json")
	if err := os.WriteFile(path, []byte(validInterpretationJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	p := mustNewProvider(t, path)
	caps, err := p.Describe(context.Background())
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	if caps.ProviderObservation.ProviderKind != ProviderKind {
		t.Fatalf("ProviderKind = %q, want %q (must never impersonate a governed resolver)", caps.ProviderObservation.ProviderKind, ProviderKind)
	}
	if len(caps.SupportedOperations) != 1 || caps.SupportedOperations[0] != providerport.OperationInterpretation {
		t.Fatalf("SupportedOperations = %v, want only interpretation", caps.SupportedOperations)
	}
	if caps.CapabilitiesDigestSHA256 == "" {
		t.Fatal("capabilities digest not stamped")
	}
}

// TestProvider_ObjectiveReturnsAuthoredValue guards the accessor synthesis-run
// uses to refuse a run whose authored interpretation objective diverges
// from its session objective.
func TestProvider_ObjectiveReturnsAuthoredValue(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "interpretation.json")
	if err := os.WriteFile(path, []byte(validInterpretationJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	p := mustNewProvider(t, path)
	if got, want := p.Objective(), "prove the file-backed provider works"; got != want {
		t.Fatalf("Objective() = %q, want %q", got, want)
	}
}

// TestProvider_RequiredProofObligationsReturnsAuthoredValueAndCopy guards
// the accessor synthesis-run uses to refuse a run whose authored
// interpretation declares any required proof obligation, and confirms it
// returns a defensive copy rather than aliasing internal state.
func TestProvider_RequiredProofObligationsReturnsAuthoredValueAndCopy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "interpretation.json")
	withObligation := `{
		"objective": "x",
		"applicable_intent": [], "binding_invariants": [], "relevant_contracts": [],
		"authority_boundaries": [], "known_failure_modes": [], "forbidden_fixes": [],
		"required_proof_obligations": ["obligation.security_review"],
		"assumptions": [], "unresolved_questions": [], "source_references": [], "limitations": []
	}`
	if err := os.WriteFile(path, []byte(withObligation), 0o644); err != nil {
		t.Fatal(err)
	}
	p := mustNewProvider(t, path)
	got := p.RequiredProofObligations()
	if len(got) != 1 || got[0] != "obligation.security_review" {
		t.Fatalf("RequiredProofObligations() = %v, want [obligation.security_review]", got)
	}
	got[0] = "mutated"
	if second := p.RequiredProofObligations(); second[0] != "obligation.security_review" {
		t.Fatalf("mutating the returned slice affected the provider's own state: %v", second)
	}
}

func TestProvider_ExecuteValidFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "interpretation.json")
	if err := os.WriteFile(path, []byte(validInterpretationJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	p := mustNewProvider(t, path)
	req := testRequest(t)
	obs := &recordingObserver{}
	result, err := p.Execute(context.Background(), req, obs)
	if err != nil {
		t.Fatalf("Execute returned a Go error for a valid file: %v", err)
	}
	if result.TerminalOutcome != providerport.OutcomeCompleted {
		t.Fatalf("TerminalOutcome = %q, want completed; detail=%s", result.TerminalOutcome, result.Detail)
	}
	if result.InterpretationPayload == nil {
		t.Fatal("completed result has no InterpretationPayload")
	}
	if result.InterpretationPayload.SessionDigestSHA256 != req.SessionDigestSHA256 {
		t.Fatalf("interpretation session digest = %q, want stamped from request %q", result.InterpretationPayload.SessionDigestSHA256, req.SessionDigestSHA256)
	}
	if result.InterpretationPayload.Objective != "prove the file-backed provider works" {
		t.Fatalf("objective not carried through from file: %q", result.InterpretationPayload.Objective)
	}
	if result.PayloadDigestSHA256 == nil || *result.PayloadDigestSHA256 != result.InterpretationPayload.InterpretationDigestSHA256 {
		t.Fatal("PayloadDigestSHA256 does not match the interpretation's own digest")
	}

	// The consumed content's hash must be reported as evidence.
	if len(obs.observed) == 0 {
		t.Fatal("Execute reported no evidence via the Observer")
	}
	found := false
	for _, o := range obs.observed {
		if strings.Contains(o, "content_sha256=") {
			found = true
		}
	}
	if !found {
		t.Fatalf("no observation recorded the content hash: %v", obs.observed)
	}
}

func TestProvider_ExecuteNilObserverIsSafe(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "interpretation.json")
	if err := os.WriteFile(path, []byte(validInterpretationJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	p := mustNewProvider(t, path)
	if _, err := p.Execute(context.Background(), testRequest(t), nil); err != nil {
		t.Fatalf("Execute with a nil Observer should not error: %v", err)
	}
}

func TestProvider_ExecuteStampsFieldsIgnoringForeignFileValues(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "interpretation.json")
	// A file that tries to claim a foreign session_digest_sha256, schema
	// version, or generator must not succeed in doing so -- those fields
	// are unmarshaled by AuthoredInterpretation's deliberately narrower
	// struct, which has no such fields at all.
	if err := os.WriteFile(path, []byte(`{
		"objective": "attempt to impersonate a different session",
		"session_digest_sha256": "deadbeef",
		"generated_by": "some-other-tool",
		"schema_version": "not.a.real.version"
	}`), 0o644); err != nil {
		t.Fatal(err)
	}
	p := mustNewProvider(t, path)
	req := testRequest(t)
	result, err := p.Execute(context.Background(), req, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.TerminalOutcome != providerport.OutcomeCompleted {
		t.Fatalf("TerminalOutcome = %q, want completed; detail=%s", result.TerminalOutcome, result.Detail)
	}
	if result.InterpretationPayload.SessionDigestSHA256 != req.SessionDigestSHA256 {
		t.Fatalf("foreign session_digest_sha256 leaked through: got %q, want %q", result.InterpretationPayload.SessionDigestSHA256, req.SessionDigestSHA256)
	}
}

func TestProvider_ExecuteRejectsNonInterpretationOperation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "interpretation.json")
	if err := os.WriteFile(path, []byte(validInterpretationJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	p := mustNewProvider(t, path)
	req := testRequest(t)
	req.Operation = providerport.OperationPlanning
	result, err := p.Execute(context.Background(), req, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.TerminalOutcome != providerport.OutcomeUnsupportedCapability {
		t.Fatalf("TerminalOutcome = %q, want unsupported-capability", result.TerminalOutcome)
	}
}

// --- New() hardening: file access problems fail fast, at construction,
// as plain Go errors -- consistent with every other CLI precondition check
// (missing task session, non-absolute --agent-command, ...), not deferred
// into a governed Execute() disposition. ---

func TestNew_RequiresRFC3339ObservedAt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "interpretation.json")
	if err := os.WriteFile(path, []byte(validInterpretationJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := New(Config{Path: path, ProviderID: "p", ObservedAt: "not-a-timestamp"}); err == nil {
		t.Fatal("expected an error for a non-RFC3339 ObservedAt")
	}
}

func TestNew_RequiresPathAndProviderID(t *testing.T) {
	if _, err := New(Config{ProviderID: "p", ObservedAt: testObservedAt}); err == nil {
		t.Fatal("expected an error for an empty Path")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "interpretation.json")
	if err := os.WriteFile(path, []byte(validInterpretationJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := New(Config{Path: path, ObservedAt: testObservedAt}); err == nil {
		t.Fatal("expected an error for an empty ProviderID")
	}
}

func TestNew_MissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist.json")
	if _, err := New(Config{Path: path, ProviderID: "p", ObservedAt: testObservedAt}); err == nil {
		t.Fatal("expected an error for a missing file")
	}
}

func TestNew_MalformedJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "interpretation.json")
	if err := os.WriteFile(path, []byte("{not valid json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := New(Config{Path: path, ProviderID: "p", ObservedAt: testObservedAt}); err == nil {
		t.Fatal("expected an error for malformed JSON")
	}
}

func TestNew_RejectsDirectory(t *testing.T) {
	dir := t.TempDir()
	if _, err := New(Config{Path: dir, ProviderID: "p", ObservedAt: testObservedAt}); err == nil {
		t.Fatal("expected an error when Path is a directory")
	}
}

func TestNew_RejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real.json")
	if err := os.WriteFile(real, []byte(validInterpretationJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.json")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unsupported in this environment: %v", err)
	}
	if _, err := New(Config{Path: link, ProviderID: "p", ObservedAt: testObservedAt}); err == nil {
		t.Fatal("expected an error for a symlinked interpretation file")
	} else if !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("expected a symlink-specific error, got: %v", err)
	}
}

func TestNew_RejectsOversizedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "interpretation.json")
	if err := os.WriteFile(path, []byte(validInterpretationJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := New(Config{Path: path, ProviderID: "p", ObservedAt: testObservedAt, MaxFileBytes: 8}); err == nil {
		t.Fatal("expected an error when the file exceeds MaxFileBytes")
	}
}

func TestNew_DefaultMaxFileBytesAllowsOrdinaryFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "interpretation.json")
	if err := os.WriteFile(path, []byte(validInterpretationJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := New(Config{Path: path, ProviderID: "p", ObservedAt: testObservedAt}); err != nil {
		t.Fatalf("an ordinary small file should pass the default size bound: %v", err)
	}
}

func TestNew_ResolvesRelativePathToAbsolute(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "interpretation.json")
	if err := os.WriteFile(path, []byte(validInterpretationJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(wd) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	p, err := New(Config{Path: "interpretation.json", ProviderID: "p", ObservedAt: testObservedAt})
	if err != nil {
		t.Fatalf("New with a relative path: %v", err)
	}
	if !filepath.IsAbs(p.resolvedPath) {
		t.Fatalf("resolvedPath = %q, want an absolute path", p.resolvedPath)
	}
}

func TestNew_SameContentSameHashAcrossInstances(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "interpretation.json")
	if err := os.WriteFile(path, []byte(validInterpretationJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	p1 := mustNewProvider(t, path)
	p2 := mustNewProvider(t, path)
	if p1.contentSHA256 == "" || p1.contentSHA256 != p2.contentSHA256 {
		t.Fatalf("expected identical content hashes for identical content: %q vs %q", p1.contentSHA256, p2.contentSHA256)
	}
}
