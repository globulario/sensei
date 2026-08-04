// SPDX-License-Identifier: AGPL-3.0-only

package fileinterpretation

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
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

// TestNew_RejectsForeignStampedFields covers what
// TestProvider_ExecuteStampsFieldsIgnoringForeignFileValues used to prove
// under plain json.Unmarshal: a file that tries to claim a foreign
// session_digest_sha256, schema version, or generator must not succeed in
// doing so. Under the strict AuthoredInterpretation shape validation added
// after a live review finding (validateAuthoredInterpretationTopLevelShape
// + DisallowUnknownFields), those foreign keys don't just fail to leak
// through silently -- they now cause New() to refuse the file outright, a
// strictly stronger guarantee than "ignored."
func TestNew_RejectsForeignStampedFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "interpretation.json")
	body := strings.Replace(validInterpretationJSON, `"objective": "prove the file-backed provider works",`,
		`"objective": "attempt to impersonate a different session",
		"session_digest_sha256": "deadbeef",
		"generated_by": "some-other-tool",
		"schema_version": "not.a.real.version",`, 1)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := New(Config{Path: path, ProviderID: "p", ObservedAt: testObservedAt}); err == nil {
		t.Fatal("expected an error for foreign stamped fields (session_digest_sha256, generated_by, schema_version)")
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

// TestNew_RejectsMisspelledRequiredField is the direct regression test for
// a live review finding: a misspelled governance field name (here,
// "require_proof_obligations" instead of "required_proof_obligations")
// must be rejected outright, not silently treated as an explicit empty
// declaration of the correctly-spelled field -- both used to be
// indistinguishable under plain json.Unmarshal, since a struct field a
// misspelled key never populates is left at the same Go zero value an
// explicit empty array would produce.
func TestNew_RejectsMisspelledRequiredField(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "interpretation.json")
	body := strings.Replace(validInterpretationJSON, `"required_proof_obligations": [],`, `"require_proof_obligations": [],`, 1)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := New(Config{Path: path, ProviderID: "p", ObservedAt: testObservedAt}); err == nil {
		t.Fatal("expected an error for a misspelled required field")
	}
}

// TestNew_RejectsOmittedRequiredField covers the companion case: a field
// left out of the file entirely (not merely misspelled) must be rejected
// the same way -- an authored interpretation must explicitly declare every
// field, even an empty one.
func TestNew_RejectsOmittedRequiredField(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "interpretation.json")
	body := strings.Replace(validInterpretationJSON, `"required_proof_obligations": [],`, ``, 1)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := New(Config{Path: path, ProviderID: "p", ObservedAt: testObservedAt}); err == nil {
		t.Fatal("expected an error for an omitted required field")
	}
}

// TestNew_RejectsNullRequiredField is the direct regression test for a
// live review finding: a JSON `null` value unmarshals into a []string
// field as a silent no-op leaving it nil -- the same zero value an
// explicit "[]" produces, and no error -- so an authored
// "required_proof_obligations": null previously passed the presence-only
// shape check (the key exists) and then satisfied
// validateNoRequiredProofObligations's empty check exactly as if the
// author had explicitly declared no obligations.
func TestNew_RejectsNullRequiredField(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "interpretation.json")
	body := strings.Replace(validInterpretationJSON, `"required_proof_obligations": [],`, `"required_proof_obligations": null,`, 1)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := New(Config{Path: path, ProviderID: "p", ObservedAt: testObservedAt}); err == nil {
		t.Fatal("expected an error for a null required field")
	}
}

// TestNew_RejectsNullStringField covers the same null-is-zero-value gap
// for a string field (Objective), not just a slice field -- encoding/json
// treats null identically (a no-op producing the zero value) regardless
// of the target field's type.
func TestNew_RejectsNullStringField(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "interpretation.json")
	body := strings.Replace(validInterpretationJSON, `"objective": "prove the file-backed provider works",`, `"objective": null,`, 1)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := New(Config{Path: path, ProviderID: "p", ObservedAt: testObservedAt}); err == nil {
		t.Fatal("expected an error for a null string field")
	}
}

// TestNew_RejectsDuplicateTopLevelKey is the direct regression test for the
// live review finding: a duplicate "required_proof_obligations" key (last
// one empty) must never silently shadow the first, real declaration under
// plain last-value-wins json.Unmarshal semantics.
func TestNew_RejectsDuplicateTopLevelKey(t *testing.T) {
	const duplicateKeyJSON = `{
	"objective": "prove the file-backed provider works",
	"applicable_intent": ["intent.test"],
	"binding_invariants": [],
	"relevant_contracts": [],
	"authority_boundaries": ["providers-have-no-admission-authority"],
	"known_failure_modes": [],
	"forbidden_fixes": [],
	"required_proof_obligations": ["proof.real-obligation"],
	"required_proof_obligations": [],
	"assumptions": ["hand-authored, not a governed resolver"],
	"unresolved_questions": [],
	"source_references": [],
	"limitations": []
}`
	dir := t.TempDir()
	path := filepath.Join(dir, "interpretation.json")
	if err := os.WriteFile(path, []byte(duplicateKeyJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := New(Config{Path: path, ProviderID: "p", ObservedAt: testObservedAt}); err == nil {
		t.Fatal("expected an error for a duplicate top-level key")
	}
}

// TestNew_RejectsDuplicateNestedKey covers the same ambiguity nested inside
// an array element, not just at the top level.
func TestNew_RejectsDuplicateNestedKey(t *testing.T) {
	const duplicateNestedKeyJSON = `{
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
	"source_references": [{"kind": "file", "kind": "url", "reference": "x"}],
	"limitations": []
}`
	dir := t.TempDir()
	path := filepath.Join(dir, "interpretation.json")
	if err := os.WriteFile(path, []byte(duplicateNestedKeyJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := New(Config{Path: path, ProviderID: "p", ObservedAt: testObservedAt}); err == nil {
		t.Fatal("expected an error for a duplicate nested key")
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

// TestNew_RequiredTopLevelKeysMatchAuthoredInterpretationFields guards
// authoredInterpretationFields (an explicit list, not reflection-derived)
// against drift: every json tag AuthoredInterpretation actually declares
// must appear in the list exactly once, and vice versa. A future field
// added to the struct without updating the list would otherwise silently
// stop being required, reopening the exact gap this shape check exists to
// close.
func TestNew_RequiredTopLevelKeysMatchAuthoredInterpretationFields(t *testing.T) {
	structType := reflect.TypeOf(AuthoredInterpretation{})
	fromStruct := make(map[string]struct{}, structType.NumField())
	for i := 0; i < structType.NumField(); i++ {
		tag := structType.Field(i).Tag.Get("json")
		name, _, _ := strings.Cut(tag, ",")
		if name == "" {
			t.Fatalf("field %s has no json tag", structType.Field(i).Name)
		}
		fromStruct[name] = struct{}{}
	}
	fromList := make(map[string]struct{}, len(authoredInterpretationFields))
	for _, name := range authoredInterpretationFields {
		if _, dup := fromList[name]; dup {
			t.Fatalf("authoredInterpretationFields lists %q more than once", name)
		}
		fromList[name] = struct{}{}
	}
	for name := range fromStruct {
		if _, ok := fromList[name]; !ok {
			t.Errorf("AuthoredInterpretation has json tag %q missing from authoredInterpretationFields", name)
		}
	}
	for name := range fromList {
		if _, ok := fromStruct[name]; !ok {
			t.Errorf("authoredInterpretationFields lists %q, which AuthoredInterpretation has no json tag for", name)
		}
	}
}
