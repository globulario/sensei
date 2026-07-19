// SPDX-License-Identifier: AGPL-3.0-only

package changebinding

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func sha(c byte, n int) string { return strings.Repeat(string(c), n) }

// validBinding returns a complete, digest-stamped, canonical v1 binding.
func validBinding(t *testing.T) ChangeTaskBinding {
	t.Helper()
	b := ChangeTaskBinding{
		SchemaVersion:                SchemaVersion,
		Repository:                   RepositoryIdentity{Provider: "github", Identity: "github.com/globulario/sensei"},
		Change:                       ChangeIdentity{Provider: "github", ID: "81", HeadSHA: sha('a', 40), BaseSHA: sha('b', 40)},
		Task:                         TaskIdentity{Directory: ".sensei/tasks/task.x", ID: "task.x", SessionID: "session.x"},
		CompletionResultDigestSHA256: sha('c', 64),
		Issuer:                       "sensei.ci",
		Publication:                  PublicationIdentity{ID: "pub-1"},
		Provenance:                   Provenance{EventSource: "github_pull_request", Checkout: "actions_checkout_v4", Tool: "sensei", ToolVersion: "1.1.0"},
	}
	d, err := BindingDigest(b)
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	b.DigestSHA256 = d
	return b
}

func subjectFor(b ChangeTaskBinding) ExpectedSubject {
	task := b.Task
	return ExpectedSubject{Repository: b.Repository, Change: b.Change, Task: &task}
}

type fakeVerifier struct{ r ProvenanceVerification }

func (f fakeVerifier) VerifyProvenance(ChangeTaskBinding) ProvenanceVerification { return f.r }

// restamp recomputes and sets the digest so a mutated binding stays self-consistent.
func restamp(t *testing.T, b ChangeTaskBinding) ChangeTaskBinding {
	t.Helper()
	d, err := BindingDigest(b)
	if err != nil {
		t.Fatal(err)
	}
	b.DigestSHA256 = d
	return b
}

// ---- Canonicalization & digest (1-8) -----------------------------------------------

func TestDigest_ReorderedSourceSameDigest(t *testing.T) {
	// Two YAML documents with different top-level key order must parse to the same
	// binding and thus the same digest.
	a := "schema_version: " + SchemaVersion + "\n" +
		"repository: {provider: github, identity: r}\n"
	b := "repository: {identity: r, provider: github}\n" +
		"schema_version: " + SchemaVersion + "\n"
	var ba, bb ChangeTaskBinding
	if err := yaml.Unmarshal([]byte(a), &ba); err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal([]byte(b), &bb); err != nil {
		t.Fatal(err)
	}
	da, _ := BindingDigest(ba)
	db, _ := BindingDigest(bb)
	if da != db {
		t.Fatal("reordered source fields must yield the same canonical digest")
	}
}

func TestDigest_EveryBoundFieldAffectsDigest(t *testing.T) {
	base := validBinding(t)
	d0, _ := BindingDigest(base)
	mutators := map[string]func(*ChangeTaskBinding){
		"repo_identity":  func(b *ChangeTaskBinding) { b.Repository.Identity = "other" },
		"repo_provider":  func(b *ChangeTaskBinding) { b.Repository.Provider = "gitlab" },
		"change_id":      func(b *ChangeTaskBinding) { b.Change.ID = "82" },
		"head":           func(b *ChangeTaskBinding) { b.Change.HeadSHA = sha('d', 40) },
		"base":           func(b *ChangeTaskBinding) { b.Change.BaseSHA = sha('e', 40) },
		"task_id":        func(b *ChangeTaskBinding) { b.Task.ID = "task.y" },
		"task_session":   func(b *ChangeTaskBinding) { b.Task.SessionID = "session.y" },
		"task_dir":       func(b *ChangeTaskBinding) { b.Task.Directory = ".sensei/tasks/task.y" },
		"completion_dig": func(b *ChangeTaskBinding) { b.CompletionResultDigestSHA256 = sha('f', 64) },
		"issuer":         func(b *ChangeTaskBinding) { b.Issuer = "attacker" },
		"publication":    func(b *ChangeTaskBinding) { b.Publication.ID = "pub-2" },
		"provenance":     func(b *ChangeTaskBinding) { b.Provenance.EventSource = "elsewhere" },
	}
	for name, m := range mutators {
		b := base
		m(&b)
		if d, _ := BindingDigest(b); d == d0 {
			t.Fatalf("changing %s must change the binding digest", name)
		}
	}
}

func TestDigest_ExcludesOnlyItself(t *testing.T) {
	b := validBinding(t)
	d0, _ := BindingDigest(b)
	b.DigestSHA256 = "whatever-different"
	if d, _ := BindingDigest(b); d != d0 {
		t.Fatal("the digest field must be excluded from its own computation")
	}
}

func TestDigest_DeterministicAndNoNormalization(t *testing.T) {
	b := validBinding(t)
	d1, _ := BindingDigest(b)
	d2, _ := BindingDigest(b)
	if d1 != d2 {
		t.Fatal("digest must be deterministic")
	}
	// Identity-value whitespace is NOT normalized away: a padded value digests differently.
	padded := b
	padded.Issuer = " sensei.ci"
	if d, _ := BindingDigest(padded); d == d1 {
		t.Fatal("whitespace in an identity value must not be normalized away")
	}
	// Path conventions are not cleaned: a non-cleaned directory digests differently.
	pathy := b
	pathy.Task.Directory = ".sensei/./tasks/../tasks/task.x"
	if d, _ := BindingDigest(pathy); d == d1 {
		t.Fatal("task directory must not be path-cleaned during digest")
	}
}

// ---- Parsing (9-16) ----------------------------------------------------------------

func TestParse_ValidAndStrict(t *testing.T) {
	good, err := yaml.Marshal(validBinding(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, v := ParseBinding(good); v != "" {
		t.Fatalf("valid binding must parse, got %s", v)
	}

	cases := []struct {
		name string
		data string
		want BindingValidity
	}{
		{"unknown_field", string(good) + "surprise: 1\n", BindingMalformed},
		{"duplicate_field", "schema_version: x\nschema_version: y\n", BindingMalformed},
		{"missing_fields", "schema_version: " + SchemaVersion + "\n", BindingMalformed},
		{"wrong_type", "schema_version: " + SchemaVersion + "\nrepository: notamap\n", BindingMalformed},
		{"unsupported_version", strings.Replace(string(good), SchemaVersion, "completion.change_task_binding/v2", 1), BindingUnsupportedVersion},
		{"trailing_second_doc", string(good) + "---\nschema_version: x\n", BindingMalformed},
		{"empty", "", BindingMalformed},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, v := ParseBinding([]byte(c.data)); v != c.want {
				t.Fatalf("got %s, want %s", v, c.want)
			}
		})
	}
}

func TestParse_RejectsNoncanonicalIdentities(t *testing.T) {
	for _, m := range []func(*ChangeTaskBinding){
		func(b *ChangeTaskBinding) { b.Change.HeadSHA = "abc123" },                      // shortened
		func(b *ChangeTaskBinding) { b.Change.HeadSHA = strings.ToUpper(sha('a', 40)) }, // upper-case
		func(b *ChangeTaskBinding) { b.Issuer = "sensei ci" },                           // embedded whitespace
		func(b *ChangeTaskBinding) { b.Task.ID = " task.x" },                            // padded
		func(b *ChangeTaskBinding) { b.CompletionResultDigestSHA256 = sha('c', 40) },    // wrong length
	} {
		b := validBinding(t)
		m(&b)
		b = restamp(t, b)
		data, _ := yaml.Marshal(b)
		if _, v := ParseBinding(data); v != BindingMalformed {
			t.Fatalf("noncanonical identity must be malformed, got %s", v)
		}
	}
}

// ---- Subject matching (17-24) ------------------------------------------------------

func TestValidate_SubjectMatching(t *testing.T) {
	b := validBinding(t)
	verified := ProvenanceVerified

	if r := ValidateBinding(b, subjectFor(b), verified); !r.IsAuthoritative() {
		t.Fatalf("exact subject must be authoritative, got %s", r.Validity)
	}

	cases := []struct {
		name string
		exp  func(ExpectedSubject) ExpectedSubject
		want BindingValidity
	}{
		{"wrong_repo", func(e ExpectedSubject) ExpectedSubject { e.Repository.Identity = "github.com/other/repo"; return e }, BindingRepositoryMismatch},
		{"wrong_head", func(e ExpectedSubject) ExpectedSubject { e.Change.HeadSHA = sha('9', 40); return e }, BindingStaleHead},
		{"wrong_base", func(e ExpectedSubject) ExpectedSubject { e.Change.BaseSHA = sha('9', 40); return e }, BindingChangeRangeMismatch},
		{"wrong_change_id", func(e ExpectedSubject) ExpectedSubject { e.Change.ID = "999"; return e }, BindingChangeRangeMismatch},
		{"wrong_task", func(e ExpectedSubject) ExpectedSubject {
			e.Task = &TaskIdentity{Directory: b.Task.Directory, ID: "task.z", SessionID: b.Task.SessionID}
			return e
		}, BindingTaskMismatch},
		{"case_repo", func(e ExpectedSubject) ExpectedSubject {
			e.Repository.Identity = "github.com/globulario/SENSEI"
			return e
		}, BindingRepositoryMismatch},
		{"whitespace_head", func(e ExpectedSubject) ExpectedSubject { e.Change.HeadSHA = " " + sha('a', 40); return e }, BindingStaleHead},
		{"shortened_head", func(e ExpectedSubject) ExpectedSubject { e.Change.HeadSHA = "aaaaaaa"; return e }, BindingStaleHead},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if r := ValidateBinding(b, c.exp(subjectFor(b)), verified); r.Validity != c.want {
				t.Fatalf("got %s, want %s", r.Validity, c.want)
			}
		})
	}
}

// ---- Authority & publication validity (25-30) --------------------------------------

func TestValidate_AuthorityAndDigest(t *testing.T) {
	b := validBinding(t)
	subj := subjectFor(b)

	// 25/26: populated issuer without positive verification is not authoritative.
	if r := ValidateBinding(b, subj, ProvenanceUnverifiable); r.Validity != BindingUnverifiableProvenance {
		t.Fatalf("unverifiable provenance → %s, want unverifiable", r.Validity)
	}
	// 27: invalid provenance → the frozen provenance class.
	if r := ValidateBinding(b, subj, ProvenanceInvalid); r.Validity != BindingUnverifiableProvenance {
		t.Fatalf("invalid provenance → %s, want unverifiable", r.Validity)
	}
	// 28/30: digest mismatch → publication invalid, even with verified provenance.
	tampered := b
	tampered.Issuer = "attacker" // content changed but digest NOT restamped
	if r := ValidateBinding(tampered, subj, ProvenanceVerified); r.Validity != BindingPublicationInvalid {
		t.Fatalf("digest mismatch → %s, want publication_invalid", r.Validity)
	}
	// 29: a valid digest does not compensate for unverifiable provenance.
	if r := ValidateBinding(b, subj, ProvenanceUnverifiable); r.IsAuthoritative() {
		t.Fatal("a valid digest must not compensate for unverifiable provenance")
	}
}

// ---- Contradictions / set validation (31-37) ---------------------------------------

func TestValidateSet_Contradictions(t *testing.T) {
	b := validBinding(t)
	subj := subjectFor(b)
	verifier := fakeVerifier{ProvenanceVerified}

	if r := ValidateBindingSet(nil, subj, verifier); r.Validity != BindingAbsent {
		t.Fatalf("no records → %s, want absent", r.Validity)
	}
	if r := ValidateBindingSet([]ChangeTaskBinding{b}, subj, verifier); !r.IsAuthoritative() {
		t.Fatalf("one verified record → %s, want authoritative", r.Validity)
	}

	// A conflicting second record (different task).
	conflict := restamp(t, func() ChangeTaskBinding { c := b; c.Task.ID = "task.other"; c.Publication.ID = "pub-2"; return c }())
	for _, set := range [][]ChangeTaskBinding{{b, conflict}, {conflict, b}} { // order-independent
		if r := ValidateBindingSet(set, subj, verifier); r.Validity != BindingContradictory {
			t.Fatalf("two records → %s, want contradictory", r.Validity)
		}
	}
	// A valid record cannot outvote a second (even junk) record.
	junk := ChangeTaskBinding{SchemaVersion: "bogus"}
	if r := ValidateBindingSet([]ChangeTaskBinding{b, junk}, subj, verifier); r.Validity != BindingContradictory {
		t.Fatalf("valid+junk → %s, want contradictory (never select the valid one)", r.Validity)
	}
}

// ---- Isolation / determinism (38-42) -----------------------------------------------

func TestValidate_PureNoAmbientReads(t *testing.T) {
	// A binding naming a nonexistent task directory that MATCHES the expected subject is
	// authoritative — proving validation compares strings and never touches the filesystem.
	b := validBinding(t)
	b.Task.Directory = "/definitely/not/on/disk/task.x"
	b = restamp(t, b)
	subj := subjectFor(b)
	if r := ValidateBinding(b, subj, ProvenanceVerified); !r.IsAuthoritative() {
		t.Fatalf("validation must be pure string comparison (no fs), got %s", r.Validity)
	}
	// Deterministic across repeated evaluation.
	first := ValidateBinding(b, subj, ProvenanceVerified)
	for i := 0; i < 20; i++ {
		if r := ValidateBinding(b, subj, ProvenanceVerified); r != first {
			t.Fatal("validation must be deterministic")
		}
	}
}
