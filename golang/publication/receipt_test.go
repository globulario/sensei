package publication

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.invalid",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.invalid",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// repoWithCorpus builds a git repo containing docs/awareness/x.yaml with the
// given contents, one commit per entry, and returns the repo path and commits.
func repoWithCorpus(t *testing.T, contents ...string) (string, []string) {
	t.Helper()
	dir := t.TempDir()
	run(t, dir, "git", "init", "-q", "-b", "main")
	aw := filepath.Join(dir, "docs", "awareness")
	if err := os.MkdirAll(aw, 0o755); err != nil {
		t.Fatal(err)
	}
	var commits []string
	for i, c := range contents {
		if err := os.WriteFile(filepath.Join(aw, "x.yaml"), []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
		// A second file changes between commits so the COMMITS differ even when
		// the awareness corpus does not.
		if err := os.WriteFile(filepath.Join(dir, "unrelated.txt"), []byte{byte('a' + i)}, 0o644); err != nil {
			t.Fatal(err)
		}
		run(t, dir, "git", "add", "-A")
		run(t, dir, "git", "commit", "-q", "-m", "c")
		commits = append(commits, run(t, dir, "git", "rev-parse", "HEAD"))
	}
	return dir, commits
}

// A clean checkout publishes its exact revision and may claim it.
func TestACleanCheckoutClaimsItsExactRevision(t *testing.T) {
	dir, commits := repoWithCorpus(t, "a: 1\n")
	rev, tree, state, root, _ := InspectSource(filepath.Join(dir, "docs", "awareness"))
	if state != CleanExact {
		t.Fatalf("state = %q, want CLEAN_EXACT", state)
	}
	if rev != commits[0] {
		t.Fatalf("revision = %q, want %q", rev, commits[0])
	}
	if tree == "" {
		t.Fatal("no source tree recorded")
	}
	if !state.ClaimsExactRevision() {
		t.Fatal("CLEAN_EXACT must permit the exact-revision claim")
	}
	if !strings.HasSuffix(root, filepath.Join("docs", "awareness")) {
		t.Fatalf("source root = %q", root)
	}
}

// A dirty tree whose HEAD is M must NOT claim it was produced from M.
func TestADirtyCheckoutCannotClaimExactRevision(t *testing.T) {
	dir, commits := repoWithCorpus(t, "a: 1\n")
	aw := filepath.Join(dir, "docs", "awareness")
	if err := os.WriteFile(filepath.Join(aw, "x.yaml"), []byte("a: 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rev, _, state, _, _ := InspectSource(aw)
	if state != Dirty {
		t.Fatalf("state = %q, want DIRTY", state)
	}
	if rev != commits[0] {
		t.Fatalf("revision = %q: HEAD is still worth recording", rev)
	}
	if state.ClaimsExactRevision() {
		t.Fatal("a DIRTY publication claimed it came from an exact revision")
	}
}

// An UNTRACKED file under the source root also breaks the exact claim: it was
// compiled, and it is in no commit.
func TestAnUntrackedInputBreaksTheExactClaim(t *testing.T) {
	dir, _ := repoWithCorpus(t, "a: 1\n")
	aw := filepath.Join(dir, "docs", "awareness")
	if err := os.WriteFile(filepath.Join(aw, "sneaked.yaml"), []byte("b: 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, state, _, _ := InspectSource(aw); state.ClaimsExactRevision() {
		t.Fatalf("state = %q: an untracked compiled input passed as exact", state)
	}
}

// THE CENTRAL FALSIFIER.
//
// Two different commits with byte-identical awareness content must publish the
// SAME content digest and DIFFERENT revision provenance, and must not collapse
// to one receipt identity. Collapsing them is the defect this package exists to
// prevent: it is how a graph loses which world produced it while still looking
// perfectly verified.
func TestIdenticalContentAtTwoCommitsKeepsDistinctProvenance(t *testing.T) {
	same := "a: 1\n"
	dir, commits := repoWithCorpus(t, same, same)
	if len(commits) != 2 || commits[0] == commits[1] {
		t.Fatalf("need two distinct commits, got %v", commits)
	}
	aw := filepath.Join(dir, "docs", "awareness")

	compiled := []byte("<s> <p> <o> .\n") // identical compiled output, by construction
	mk := func(commit string) Receipt {
		run(t, dir, "git", "checkout", "-q", commit)
		rev, tree, state, root, _ := InspectSource(aw)
		if state != CleanExact {
			t.Fatalf("state = %q at %s", state, commit)
		}
		return Receipt{
			Domain: "example.com/x", Revision: rev, Tree: tree, State: state,
			SourceRoot: root, SourceDigest: DigestBytes(compiled),
		}
	}
	first, second := mk(commits[0]), mk(commits[1])

	if first.SourceDigest != second.SourceDigest {
		t.Fatal("the specimen is wrong: the content digests must be equal for this test to mean anything")
	}
	if first.Tree != second.Tree {
		t.Fatal("the specimen is wrong: identical corpora must share a source tree")
	}
	if first.Revision == second.Revision {
		t.Fatal("two commits reported the same revision")
	}
	if first.Identity() == second.Identity() {
		t.Fatal("identical content collapsed two commits into one receipt identity — the exact defect this prevents")
	}
	if first.IRI() == second.IRI() {
		t.Fatal("two publications from different commits share a receipt IRI")
	}
}

// A source root that is not a git checkout publishes UNKNOWN and an EMPTY
// revision. It must never be inferred, and the absent field must be absent
// rather than stated empty.
func TestNonGitSourcePublishesUnknownAndNeverInfers(t *testing.T) {
	dir := t.TempDir()
	rev, tree, state, _, _ := InspectSource(dir)
	if state != Unknown {
		t.Fatalf("state = %q, want UNKNOWN", state)
	}
	if rev != "" || tree != "" {
		t.Fatalf("a revision was invented for a non-git source: rev=%q tree=%q", rev, tree)
	}
	if state.ClaimsExactRevision() {
		t.Fatal("UNKNOWN claimed an exact revision")
	}
	r := Receipt{Domain: "d", State: Unknown}
	if strings.Contains(string(r.Triples()), "publicationSourceRevision") {
		t.Fatal("an absent revision was written as a stated empty value")
	}
}

// The receipt must survive a round trip through N-Triples and still verify.
func TestAReceiptReadsBackAndVerifiesItsIdentity(t *testing.T) {
	r := Receipt{
		Domain:   "github.com/globulario/sensei-code",
		Revision: "f6b4755ff4d12591e9e802b2094b16a938260cc2",
		Tree:     "abc123", State: CleanExact, SourceRoot: "/tmp/x",
		SourceDigest: "dig",
	}
	nt := r.Triples()
	back, ok := Current(nt, r.Domain)
	if !ok {
		t.Fatal("the pointer did not resolve to a receipt")
	}
	if back != r {
		t.Fatalf("round trip changed the receipt:\n got %+v\nwant %+v", back, r)
	}
	if !VerifyIdentity(r.IRI(), back) {
		t.Fatal("a round-tripped receipt does not hash to its own IRI")
	}
}

// A tampered field must break the identity, or the IRI proves nothing.
func TestTamperingWithAPublishedReceiptBreaksItsIdentity(t *testing.T) {
	r := Receipt{Domain: "d", Revision: "aaa", State: CleanExact}
	iri := r.IRI()
	r.Revision = "bbb"
	if VerifyIdentity(iri, r) {
		t.Fatal("a rewritten revision still verified against the original receipt IRI")
	}
}

// A pointer naming a receipt that is not present must report "no current
// publication", not an empty one.
func TestADanglingPointerIsNotAnEmptyPublication(t *testing.T) {
	nt := []byte("<" + PointerIRI("d") + "> <" + pCurrent + "> <" + receiptPrefix + "deadbeef> .\n")
	if _, ok := Current(nt, "d"); ok {
		t.Fatal("a dangling pointer resolved to a receipt")
	}
}

// The closed vocabulary is read by membership.
func TestSourceStateIsReadByMembership(t *testing.T) {
	for _, s := range []SourceState{CleanExact, Dirty, Unknown} {
		if !s.Valid() {
			t.Fatalf("%q must be valid", s)
		}
	}
	for _, s := range []SourceState{"", "clean", "CLEAN", "EXACT", "unknown"} {
		if s.Valid() {
			t.Fatalf("%q must not be a member", s)
		}
		if s.ClaimsExactRevision() {
			t.Fatalf("%q claimed an exact revision", s)
		}
	}
}

// THE MIGRATION GUARD.
//
// A1's succession proof rests on this exact receipt identity, published by the
// v1 algorithm and recorded in the experiment's frozen evidence. If a later
// change to Identity() ever alters it, the historical record stops verifying
// under the code that superseded it -- which is rewriting the past in the
// costume of a repair. This test pins the real value.
func TestTheHistoricalA1ReceiptStillVerifies(t *testing.T) {
	const a1 = "2223c940bcf84f767593f58ad798f64055edb4e766673bf87c46f89692c978f7"
	r := Receipt{ // exactly as A1 published it: no Version field, so v1
		Domain:       "github.com/globulario/sensei-code",
		Revision:     "f6b4755ff4d12591e9e802b2094b16a938260cc2",
		Tree:         "ad916f771bbc07523c92ff299c27af53c852aacd",
		State:        CleanExact,
		SourceRoot:   "/tmp/claude-1000/-home-dave-Documents-github-com-globulario-sensei-code/0c05292b-ad66-420a-9985-54db3e81471f/scratchpad/M/docs/awareness",
		SourceDigest: "cff0d6113939b6f986b873dffad22847491669d903d1254386ef57c18cdf9c23",
	}
	if r.version() != ReceiptV1 {
		t.Fatalf("an unversioned receipt resolved to %q, not v1", r.version())
	}
	if got := r.Identity(); got != a1 {
		t.Fatalf("the A1 receipt identity moved:\n got %s\nwant %s", got, a1)
	}
	if !VerifyIdentity(receiptPrefix+a1, r) {
		t.Fatal("A1's historical receipt no longer verifies against its own IRI")
	}
}

// v2 is a DIFFERENT algorithm, not a correction applied to v1. The same facts
// must not produce the same identity, or the version would be decorative.
func TestV2IsADistinctAlgorithmRatherThanAPatchedV1(t *testing.T) {
	base := Receipt{
		Domain: "d", Revision: "abc", Tree: "t", State: CleanExact, SourceDigest: "sd",
	}
	v2 := base
	v2.Version = ReceiptV2
	v2.SourcePath = "docs/awareness"
	if base.Identity() == v2.Identity() {
		t.Fatal("v1 and v2 produced the same identity, so the version participates in nothing")
	}
}

// In v2 the source path is DURABLE and identity bearing: publishing the same
// corpus from a different absolute directory must not change the receipt, and
// changing the repo-relative path must.
func TestV2IdentityFollowsThePathThatMeansSomething(t *testing.T) {
	a := Receipt{Version: ReceiptV2, Domain: "d", Revision: "abc", Tree: "t",
		State: CleanExact, SourcePath: "docs/awareness", SourceDigest: "sd",
		SourceRoot: "/tmp/build-7f2/docs/awareness"}
	b := a
	b.SourceRoot = "/home/ci/checkout/docs/awareness" // different machine, same corpus
	if a.Identity() != b.Identity() {
		t.Fatal("an operational filesystem path changed a v2 receipt identity")
	}
	c := a
	c.SourcePath = "docs/other-awareness"
	if a.Identity() == c.Identity() {
		t.Fatal("the repo-relative knowledge path does not participate in v2 identity")
	}
}

// A v2 receipt must not publish the operational root at all. Present-and-
// unhashed is the exact shape v2 exists to remove.
func TestV2PublishesNoUnhashedOperationalPath(t *testing.T) {
	r := Receipt{Version: ReceiptV2, Domain: "d", Revision: "abc", Tree: "t",
		State: CleanExact, SourcePath: "docs/awareness", SourceDigest: "sd",
		SourceRoot: "/tmp/build-7f2/docs/awareness"}
	nt := string(r.Triples())
	if strings.Contains(nt, "publicationSourceRoot") {
		t.Fatal("a v2 receipt published SourceRoot, a field its digest does not cover")
	}
	if !strings.Contains(nt, "publicationSourcePath") || !strings.Contains(nt, "publicationReceiptVersion") {
		t.Fatalf("a v2 receipt did not state its path and version:\n%s", nt)
	}
	back, ok := Current(r.Triples(), "d")
	if !ok {
		t.Fatal("the v2 pointer did not resolve")
	}
	if back.Version != ReceiptV2 || back.SourcePath != "docs/awareness" {
		t.Fatalf("v2 fields did not survive the round trip: %+v", back)
	}
	if !VerifyIdentity(r.IRI(), back) {
		t.Fatal("a round-tripped v2 receipt does not hash to its own IRI")
	}
}

// InspectSource must report the repo-relative path, which is the whole point.
func TestInspectSourceReportsTheRepositoryRelativePath(t *testing.T) {
	dir, _ := repoWithCorpus(t, "a: 1\n")
	_, _, state, root, rel := InspectSource(filepath.Join(dir, "docs", "awareness"))
	if state != CleanExact {
		t.Fatalf("state = %q", state)
	}
	if rel != "docs/awareness" {
		t.Fatalf("relative path = %q, want docs/awareness", rel)
	}
	if !filepath.IsAbs(root) {
		t.Fatalf("the operational root should still be absolute, got %q", root)
	}
}

// Verification must compare the STORED target with the RECOMPUTED identity.
//
// The first implementation recomputed the identity from a parsed receipt and
// then checked it against an identity recomputed from the same fields, which
// passes for ANY tampered receipt because both sides move together. Resolve
// returns the stored target so the comparison has two independently derived
// sides.
func TestATamperedReceiptFailsAgainstItsStoredPointerTarget(t *testing.T) {
	r := Receipt{Version: ReceiptV2, Domain: "d", Revision: "aaa", Tree: "t",
		State: CleanExact, SourcePath: "docs/awareness", SourceDigest: "sd"}
	nt := string(r.Triples())
	stored := r.IRI()

	// Someone edits the published revision without moving the pointer.
	tampered := strings.Replace(nt, `"aaa"`, `"bbb"`, 1)
	if tampered == nt {
		t.Fatal("the specimen did not actually change")
	}
	target, back, state := Resolve([]byte(tampered), "d")
	if state != PointerResolved {
		t.Fatalf("state = %v, want PointerResolved", state)
	}
	if target != stored {
		t.Fatalf("the stored target moved: %s", target)
	}
	if back.Revision != "bbb" {
		t.Fatalf("the tampered field did not survive parsing: %q", back.Revision)
	}
	if VerifyIdentity(target, back) {
		t.Fatal("a tampered receipt verified against the pointer that names it")
	}
	// The tautology, demonstrated: checking it against itself still passes.
	if !VerifyIdentity(back.IRI(), back) {
		t.Fatal("self-comparison should trivially pass — that is why it proves nothing")
	}
}

// A dangling pointer is its own world, not "nothing was published".
func TestADanglingPointerIsDistinguishedFromAbsence(t *testing.T) {
	dangling := []byte("<" + PointerIRI("d") + "> <" + pCurrent + "> <" + receiptPrefix + "deadbeef> .\n")
	target, _, state := Resolve(dangling, "d")
	if state != PointerDangling {
		t.Fatalf("state = %v, want PointerDangling", state)
	}
	if target == "" {
		t.Fatal("the stored target was lost, so the caller cannot say what is missing")
	}
	if _, _, s2 := Resolve(nil, "d"); s2 != PointerAbsent {
		t.Fatalf("an empty graph reported %v, want PointerAbsent", s2)
	}
}

// Resolve round-trips a healthy publication.
func TestResolveReturnsTheStoredTargetAndItsReceipt(t *testing.T) {
	r := Receipt{Version: ReceiptV2, Domain: "d", Revision: "abc", Tree: "t",
		State: CleanExact, SourcePath: "docs/awareness", SourceDigest: "sd"}
	target, back, state := Resolve(r.Triples(), "d")
	if state != PointerResolved {
		t.Fatalf("state = %v", state)
	}
	if target != r.IRI() {
		t.Fatalf("stored target %s != %s", target, r.IRI())
	}
	if !VerifyIdentity(target, back) {
		t.Fatal("a healthy receipt failed verification against its stored target")
	}
}

// The bounded path must parse what the whole-graph path parses.
func TestReceiptFromTriplesMatchesTheDumpPath(t *testing.T) {
	r := Receipt{Version: ReceiptV2, Domain: "d", Revision: "abc", Tree: "t",
		State: CleanExact, SourcePath: "docs/awareness", SourceDigest: "sd"}
	var preds, objs []string
	for _, line := range strings.Split(string(r.Triples()), "\n") {
		if !strings.HasPrefix(line, "<"+r.IRI()+">") {
			continue
		}
		_, p, o, ok := splitTriple(line)
		if !ok {
			continue
		}
		preds = append(preds, p)
		objs = append(objs, o)
	}
	back, err := ReceiptFromTriples(r.IRI(), preds, objs, make([]bool, len(preds)))
	if err != nil {
		t.Fatalf("the bounded path parsed nothing: %v", err)
	}
	if !VerifyIdentity(r.IRI(), back) {
		t.Fatalf("the bounded path produced a different receipt: %+v", back)
	}
}

// A publication that could not resolve its tree must not claim CLEAN_EXACT.
//
// The verifier now requires a tree for the exact claim; the WRITER must not be
// able to produce a receipt the verifier would refuse, or the refusal surfaces
// at a start gate instead of at publication.
func TestUnresolvableTreeCannotClaimCleanExact(t *testing.T) {
	dir, _ := repoWithCorpus(t, "a: 1\n")
	aw := filepath.Join(dir, "docs", "awareness")

	rev, tree, state, _, _ := InspectSource(aw)
	if state != CleanExact || tree == "" || rev == "" {
		t.Fatalf("the specimen is wrong: want a clean checkout with a tree, got %q/%q/%q", rev, tree, state)
	}
	// A path inside the repo that HEAD does not contain has no tree object.
	missing := filepath.Join(dir, "docs", "not-committed")
	if err := os.MkdirAll(missing, 0o755); err != nil {
		t.Fatal(err)
	}
	_, tree, state, _, _ = InspectSource(missing)
	if state.ClaimsExactRevision() {
		t.Fatalf("state %q claimed an exact revision with tree %q, which HEAD does not contain", state, tree)
	}
}

// An IGNORED compiled input must break the exact claim.
//
// The extractor walks the corpus with filepath.WalkDir, which knows nothing
// about .gitignore, while `git status --untracked-files=all` omits ignored
// files. Cleanliness was measured over a different set than the one compiled,
// so an ignored YAML produced a CLEAN_EXACT attestation for bytes the named
// revision does not contain. This is a DIFFERENT defect family from the RDF
// ones: two definitions of "the compiled inputs" disagreeing, not a stored
// fact being normalised.
func TestAnIgnoredCompiledInputBreaksTheExactClaim(t *testing.T) {
	dir, _ := repoWithCorpus(t, "a: 1\n")
	aw := filepath.Join(dir, "docs", "awareness")
	if _, _, state, _, _ := InspectSource(aw); state != CleanExact {
		t.Fatalf("the specimen is wrong: want CLEAN_EXACT, got %q", state)
	}
	// Ignored, and therefore invisible to --untracked-files=all -- but the
	// compiler still walks and compiles it.
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("docs/awareness/hidden.yaml\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, dir, "git", "add", ".gitignore")
	run(t, dir, "git", "commit", "-q", "-m", "ignore")
	if err := os.WriteFile(filepath.Join(aw, "hidden.yaml"), []byte("b: 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out := run(t, dir, "git", "status", "--porcelain", "--untracked-files=all", "--", aw); out != "" {
		t.Fatalf("the specimen is wrong: the ignored file must be invisible to plain status, got %q", out)
	}
	if _, _, state, _, _ := InspectSource(aw); state.ClaimsExactRevision() {
		t.Fatalf("state %q claimed an exact revision while compiling a file that revision does not contain", state)
	}
}

// FAMILY B: every compiled root must be described, or no exact claim is made.
//
// The compiler read every --input root while the inspection read only the
// first, so a dirty supplementary root could contribute authenticated output
// under another root's CLEAN_EXACT commit. Two mechanisms saying "the compiled
// inputs" and quantifying over different sets.
func TestAllCompiledRootsMustAgreeForAnExactClaim(t *testing.T) {
	dir, _ := repoWithCorpus(t, "a: 1\n")
	aw := filepath.Join(dir, "docs", "awareness")

	if w := InspectCompiledSources([]string{aw}); !w.State.ClaimsExactRevision() {
		t.Fatalf("the specimen is wrong: one clean root should be exact, got %q", w.State)
	}

	// A SECOND repository, independently versioned, also compiled.
	other, _ := repoWithCorpus(t, "b: 2\n")
	otherAW := filepath.Join(other, "docs", "awareness")
	if w := InspectCompiledSources([]string{aw, otherAW}); w.State.ClaimsExactRevision() {
		t.Fatalf("two roots from different repositories produced an exact claim: rev=%q state=%q", w.Revision, w.State)
	}

	// A supplementary root that is DIRTY must also defeat the exact claim,
	// even though the first root is clean.
	if err := os.WriteFile(filepath.Join(otherAW, "x.yaml"), []byte("b: 3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if w := InspectCompiledSources([]string{aw, otherAW}); w.State.ClaimsExactRevision() {
		t.Fatal("a dirty supplementary root still produced an exact claim")
	}
}

// FAMILY B, temporal: the digest and the revision must describe one world.
//
// The bytes were compiled at one moment and HEAD/tree/cleanliness read at a
// later one, so a checkout that moved in between produced an exact claim for a
// revision that did not produce those bytes.
func TestAWitnessDetectsAMovingCheckout(t *testing.T) {
	dir, commits := repoWithCorpus(t, "a: 1\n", "a: 2\n")
	aw := filepath.Join(dir, "docs", "awareness")

	before := InspectCompiledSources([]string{aw})
	if !before.State.ClaimsExactRevision() {
		t.Fatalf("the specimen is wrong: want an exact witness, got %q", before.State)
	}
	if _, ok := before.Unchanged(); !ok {
		t.Fatal("an unmoved checkout reported as changed")
	}

	// Someone resets the checkout while the build is doing its store reads.
	run(t, dir, "git", "checkout", "-q", commits[0])
	if _, ok := before.Unchanged(); ok {
		t.Fatal("a checkout that moved between compilation and publication reported as unchanged")
	}
}

// F2: a witness must contain evidence of the EVENT, not two observations
// compatible with it.
//
// Change a file, compile, restore. Both working-tree inspections report the
// same clean revision and tree, so a before/after comparison passes -- while
// the compiled bytes came from a state no revision holds. Endpoint equality is
// not continuity.
func TestProvingConsumedBytesCatchesARestoredFile(t *testing.T) {
	dir, _ := repoWithCorpus(t, "a: 1\n")
	aw := filepath.Join(dir, "docs", "awareness")
	target := filepath.Join(aw, "x.yaml")

	rev, _, state, _, _ := InspectSource(aw)
	if state != CleanExact {
		t.Fatalf("the specimen is wrong: %q", state)
	}

	committed, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	good := sha256.Sum256(committed)
	if err := ProveConsumedAgainstRevision(dir, rev,
		[]ConsumedFile{{Path: target, Digest: hex.EncodeToString(good[:])}}); err != nil {
		t.Fatalf("bytes that ARE in the revision were rejected: %v", err)
	}

	// The compiler read different bytes; the file was then restored, so every
	// working-tree observation before and after is identical.
	tampered := sha256.Sum256([]byte("a: 999\n"))
	err = ProveConsumedAgainstRevision(dir, rev,
		[]ConsumedFile{{Path: target, Digest: hex.EncodeToString(tampered[:])}})
	if err == nil {
		t.Fatal("bytes the revision does not hold were accepted: the witness proves nothing")
	}
	if !strings.Contains(err.Error(), "does not hold") {
		t.Fatalf("the refusal does not name the mismatch: %v", err)
	}

	// A file compiled but absent from the revision is also refused.
	extra := filepath.Join(aw, "untracked.yaml")
	if err := os.WriteFile(extra, []byte("b: 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("b: 2\n"))
	if err := ProveConsumedAgainstRevision(dir, rev,
		[]ConsumedFile{{Path: extra, Digest: hex.EncodeToString(sum[:])}}); err == nil {
		t.Fatal("a compiled file absent from the revision was accepted")
	}

	// A file compiled with no recorded digest cannot be proven at all.
	if err := ProveConsumedAgainstRevision(dir, rev,
		[]ConsumedFile{{Path: target}}); err == nil {
		t.Fatal("a file with no consumed digest was accepted")
	}
}
