// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The World 3 amendment (#131) turns on one claim: v1 names SQLite, v2 names
// gin, and neither may stand in for the other. These tests exist so that claim
// is checkable rather than asserted, and so a future edit that reintroduces a
// compatibility alias fails here instead of in an adjudicated score.

// TestV1StillNamesSQLite. v1 is historical evidence. If it silently became gin
// the record of what was measured under it would be false, and the negative
// SQLite result that motivated v2 would be unreproducible.
func TestV1StillNamesSQLite(t *testing.T) {
	if got := protocolV1.Domains[worldIndependentCalibration]; got != "sqlite.org/sqlite" {
		t.Errorf("v1 World 3 domain = %q, want the SQLite calibration it froze", got)
	}
	if got := protocolV1.Remotes[worldIndependentCalibration]; got != "github.com/sqlite/sqlite" {
		t.Errorf("v1 World 3 remote = %q, want SQLite", got)
	}
	if strings.Contains(protocolV1.Remotes[worldIndependentCalibration], "gin") {
		t.Error("v1 World 3 now points at gin; v1 must remain what it froze")
	}
	if protocolV1.ID != "phase10-reference-protocol-v1" {
		t.Errorf("v1 identity drifted to %q", protocolV1.ID)
	}
}

// TestV2IdentifiesGinAtTheExactPinnedRevision.
func TestV2IdentifiesGinAtTheExactPinnedRevision(t *testing.T) {
	if got := protocolV2.Domains[worldIndependentCalibration]; got != "github.com/gin-gonic/gin" {
		t.Errorf("v2 World 3 domain = %q, want gin", got)
	}
	if got := protocolV2.Remotes[worldIndependentCalibration]; got != "github.com/gin-gonic/gin" {
		t.Errorf("v2 World 3 remote = %q, want gin", got)
	}
	// The revision is part of the protocol's identity, not a convenience.
	const pinned = "34dac209ffb6ef85cc78c5d217bbb7ad001d68fd"
	if got := protocolV2.Revisions[worldIndependentCalibration]; got != pinned {
		t.Errorf("v2 World 3 revision = %q, want the revision section 3 pins (%s)", got, pinned)
	}
	if defaultProtocol.ID != protocolV2.ID {
		t.Errorf("the default protocol is %q, want v2", defaultProtocol.ID)
	}
}

// gitRepo makes a checkout whose origin is `remote`, at a real commit.
func gitRepo(t *testing.T, remote string) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	run("remote", "add", "origin", remote)
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "-A")
	run("commit", "-qm", "seed")
	return dir
}

// TestAGinCheckoutCannotSatisfyV1 is the alias the amendment forbids, from the
// gin side. Section 0 of v2 is explicit that a run accepting this substitution
// would produce a manifest claiming a compliance it does not have.
func TestAGinCheckoutCannotSatisfyV1(t *testing.T) {
	gin := gitRepo(t, "https://github.com/gin-gonic/gin.git")
	note, err := verifyRequiredWorldCheckout(protocolV1, true, worldIndependentCalibration, gin)
	if err == nil {
		t.Fatalf("a gin checkout was accepted as v1's SQLite World 3 (note=%q)", note)
	}
	if !strings.Contains(err.Error(), "sqlite") {
		t.Errorf("the refusal does not say what v1 actually names: %v", err)
	}
}

// TestSQLiteCannotSatisfyV2 is the same alias from the other side.
func TestSQLiteCannotSatisfyV2(t *testing.T) {
	sqlite := gitRepo(t, "https://github.com/sqlite/sqlite.git")
	if _, err := verifyRequiredWorldCheckout(protocolV2, true, worldIndependentCalibration, sqlite); err == nil {
		t.Fatal("a SQLite checkout was accepted as v2's gin World 3")
	} else if !strings.Contains(err.Error(), "gin") {
		t.Errorf("the refusal does not say what v2 names: %v", err)
	}
}

// TestTheWrongGinRevisionIsRefused. Right repository, wrong commit — which
// passes every remote check and is still a different experiment, because v2
// pins the revision as part of its identity.
func TestTheWrongGinRevisionIsRefused(t *testing.T) {
	gin := gitRepo(t, "https://github.com/gin-gonic/gin.git")
	_, err := verifyRequiredWorldCheckout(protocolV2, true, worldIndependentCalibration, gin)
	if err == nil {
		t.Fatal("gin at an arbitrary commit was accepted under a protocol that pins one")
	}
	if !strings.Contains(err.Error(), protocolV2.Revisions[worldIndependentCalibration]) {
		t.Errorf("the refusal does not name the pinned revision: %v", err)
	}
	if !strings.Contains(err.Error(), "different experiment") {
		t.Errorf("the refusal does not say why the commit matters: %v", err)
	}
}

// TestTheExactPinnedGinSucceeds is the positive case. Without it the four
// refusals above could all be satisfied by a check that refuses everything.
func TestTheExactPinnedGinSucceeds(t *testing.T) {
	src := os.Getenv("GIN_SRC")
	if src == "" {
		src = filepath.Join(os.Getenv("HOME"), "Documents", "github.com", "gin-gonic", "gin")
	}
	if _, err := os.Stat(filepath.Join(src, ".git")); err != nil {
		t.Skipf("no gin checkout at %s to verify against", src)
	}
	dir := t.TempDir()
	clone := filepath.Join(dir, "gin")
	if out, err := exec.Command("git", "clone", "-q", src, clone).CombinedOutput(); err != nil {
		t.Skipf("could not clone gin: %v\n%s", err, out)
	}
	want := protocolV2.Revisions[worldIndependentCalibration]
	if out, err := exec.Command("git", "-C", clone, "checkout", "-q", want).CombinedOutput(); err != nil {
		t.Skipf("checkout %s: %v\n%s", want, err, out)
	}
	note, err := verifyRequiredWorldCheckout(protocolV2, true, worldIndependentCalibration, clone)
	if err != nil {
		t.Fatalf("the exact pinned gin was refused under v2: %v", err)
	}
	if note != "" {
		t.Errorf("the exact pinned gin was accepted only as unverified: %s", note)
	}
	// And the same tree must NOT satisfy v1.
	if _, err := verifyRequiredWorldCheckout(protocolV1, true, worldIndependentCalibration, clone); err == nil {
		t.Error("the pinned gin tree also satisfied v1; that is the alias the amendment forbids")
	}
}

// TestProtocolIDAndDigestCannotDriftIndependently.
//
// Each row is a way the pair could lie. A registered document under another
// identity is how a gin run would come to be filed as v1; a registered identity
// over other bytes is the same lie reversed; a registered identity with an
// unreadable document is an identity with nothing behind it.
func TestProtocolIDAndDigestCannotDriftIndependently(t *testing.T) {
	other := "0000000000000000000000000000000000000000000000000000000000000000"
	for _, tc := range []struct {
		name       string
		id, digest string
		readErr    error
		wantErr    bool
		wantKnown  bool
	}{
		{name: "v1 pair", id: protocolV1.ID, digest: protocolV1.DigestSHA256, wantKnown: true},
		{name: "v2 pair", id: protocolV2.ID, digest: protocolV2.DigestSHA256, wantKnown: true},
		{name: "v1 document under v2 identity", id: protocolV2.ID, digest: protocolV1.DigestSHA256, wantErr: true},
		{name: "v2 document under v1 identity", id: protocolV1.ID, digest: protocolV2.DigestSHA256, wantErr: true},
		{name: "v2 document relabelled", id: "operator-v9", digest: protocolV2.DigestSHA256, wantErr: true},
		{name: "v1 identity over other bytes", id: protocolV1.ID, digest: other, wantErr: true},
		{name: "v2 identity over an unreadable document", id: protocolV2.ID, digest: "", readErr: os.ErrNotExist, wantErr: true},
		{name: "operator protocol", id: "operator-v9", digest: other},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, known, err := resolveProtocol("some/path.md", tc.id, tc.digest, tc.readErr)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("the pair (%s, %s) was accepted", tc.id, tc.digest[:8])
				}
				return
			}
			if err != nil {
				t.Fatalf("a legitimate pair was refused: %v", err)
			}
			if known != tc.wantKnown {
				t.Errorf("registered = %v, want %v", known, tc.wantKnown)
			}
			if tc.wantKnown && p.ID != tc.id {
				t.Errorf("resolved to %q, want %q", p.ID, tc.id)
			}
		})
	}
}

// TestChangingTheWorldBindingChangesTheExperimentIdentity.
//
// The two protocols must not be interchangeable at the level the sample
// manifest records. If v1 and v2 agreed on World 3, a score computed under one
// could be read as evidence about the other, which section 0 forbids.
func TestChangingTheWorldBindingChangesTheExperimentIdentity(t *testing.T) {
	if protocolV1.DigestSHA256 == protocolV2.DigestSHA256 {
		t.Fatal("v1 and v2 hash identically; the amendment changed nothing")
	}
	if protocolV1.ID == protocolV2.ID {
		t.Fatal("v1 and v2 share an identity")
	}
	if protocolV1.Domains[worldIndependentCalibration] == protocolV2.Domains[worldIndependentCalibration] {
		t.Fatal("both protocols bind World 3 to the same repository; there is no amendment")
	}

	// A world set that satisfies v2 must NOT satisfy v1, and vice versa.
	v2Worlds := map[string]string{
		worldSenseiSelf:             protocolV2.Domains[worldSenseiSelf],
		worldGlobular:               protocolV2.Domains[worldGlobular],
		worldIndependentCalibration: protocolV2.Domains[worldIndependentCalibration],
		worldMutantSuite:            "",
	}
	if got := protocolV2.missingWorlds(v2Worlds); len(got) != 0 {
		t.Fatalf("a complete v2 world set reported missing: %v", got)
	}
	missingUnderV1 := protocolV1.missingWorlds(v2Worlds)
	if len(missingUnderV1) == 0 {
		t.Fatal("a v2 world set satisfied v1's completeness check; the experiments are interchangeable")
	}
	if !strings.Contains(strings.Join(missingUnderV1, " "), "sqlite") {
		t.Errorf("v1 did not report the SQLite world as unmet: %v", missingUnderV1)
	}
}

// repoRoot is the checkout root, from which protocolVersion.Path is relative.
// This package always lives at cmd/eval-arms, so two levels up is the root.
func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// TestCompiledProtocolIdentitiesMatchTheirDocuments closes the gap the test
// above leaves open.
//
// TestProtocolIDAndDigestCannotDriftIndependently compares the compiled
// constants to EACH OTHER; it would still pass if both had drifted away from
// the documents they claim to identify. But the constants are the whole basis
// on which resolveProtocol decides that some bytes are "the v2 protocol", so a
// constant that no longer hashes the file at Path means the binary is enforcing
// an identity nothing on disk carries.
//
// The revision check is the same defect one level down, and the more dangerous
// half: the DOCUMENT is the authority, the compiled pin is only the enforcement
// of it. Editing section 3's revision without editing Revisions (or the
// reverse) would have the evaluator refuse every checkout except one the
// protocol never named, while stamping a digest of a document that says
// otherwise. Bound in BOTH directions so neither side can move alone.
func TestCompiledProtocolIdentitiesMatchTheirDocuments(t *testing.T) {
	root := repoRoot(t)
	sha := regexp.MustCompile(`\b[0-9a-f]{40}\b`)

	for _, p := range registeredProtocols {
		t.Run(p.ID, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join(root, p.Path))
			if err != nil {
				t.Fatalf("%s names %s, which cannot be read: %v", p.ID, p.Path, err)
			}
			sum := sha256.Sum256(body)
			if got := hex.EncodeToString(sum[:]); got != p.DigestSHA256 {
				t.Errorf("%s: compiled digest %s but %s hashes to %s;\n"+
					"the binary would refuse the very document this protocol IS",
					p.ID, p.DigestSHA256, p.Path, got)
			}

			// Every revision this protocol enforces must be one its document
			// states, and every revision its document states must be one it
			// enforces. Either half alone lets the two drift.
			doc := map[string]bool{}
			for _, rev := range sha.FindAllString(string(body), -1) {
				doc[rev] = true
			}
			for world, rev := range p.Revisions {
				if !doc[rev] {
					t.Errorf("%s pins %s to revision %s, which %s never states",
						p.ID, world, rev, p.Path)
				}
				delete(doc, rev)
			}
			for rev := range doc {
				t.Errorf("%s states revision %s but %s enforces no pin for it",
					p.Path, rev, p.ID)
			}
		})
	}
}

// TestEachProtocolWorldHasARegisteredBinding. A world listed in Worlds but
// absent from Remotes fails closed at run time by design, which is safe but
// arrives as a run-time refusal rather than a build-time error. Since the
// bindings are now per-protocol, a future protocol that adds a world and
// forgets its binding should fail here instead.
func TestEachProtocolWorldHasARegisteredBinding(t *testing.T) {
	for _, p := range registeredProtocols {
		for _, w := range p.Worlds {
			if _, ok := p.Remotes[w]; !ok {
				t.Errorf("%s names world %s but registers no upstream identity for it", p.ID, w)
			}
		}
	}
}
