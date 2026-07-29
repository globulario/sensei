// SPDX-License-Identifier: AGPL-3.0-only

package governedmutation_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	yaml "gopkg.in/yaml.v3"

	gm "github.com/globulario/sensei/golang/architecture/governedmutation"
	"github.com/globulario/sensei/golang/propose"
)

func repoDir(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs", "awareness"), 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

func validProposal(kind string) propose.Request {
	switch kind {
	case "failure_mode":
		return propose.Request{Kind: kind, Title: "Stale seed served after reload",
			Description: "x", Severity: "high", SourceFiles: []string{"golang/server/reload.go"},
			RelatedInvariants: []string{"awareness.some_invariant"}, Evidence: []string{"observed stale"},
			Domain: "github.com/globulario/sensei"}
	case "invariant":
		return propose.Request{Kind: kind, Title: "Reload validates before serving",
			Description: "x", SourceFiles: []string{"golang/server/reload.go"},
			RelatedFailures: []string{"awareness.some_failure"}, Domain: "github.com/globulario/sensei"}
	case "required_test":
		return propose.Request{Kind: kind, ID: "golang/server/reload_test.go:TestReloadFresh",
			Title: "Reload serves fresh triples", RelatedInvariants: []string{"awareness.some_invariant"}}
	case "forbidden_fix":
		return propose.Request{Kind: kind, Title: "Cache the reload path",
			Description: "caused the stale-serve failure", RelatedInvariants: []string{"awareness.some_invariant"},
			Domain: "github.com/globulario/sensei"}
	case "decision":
		return propose.Request{Kind: kind, Title: "Adopt append-only seed reload",
			Description: "rationale", RelatedInvariants: []string{"awareness.some_invariant"},
			Domain: "github.com/globulario/sensei"}
	}
	return propose.Request{}
}

func TestApplyRoutesEveryGovernedKind(t *testing.T) {
	wantFile := map[string]string{
		"failure_mode":  "docs/awareness/failure_modes.yaml",
		"invariant":     "docs/awareness/invariants.yaml",
		"required_test": "docs/awareness/required_tests.yaml",
		"forbidden_fix": "docs/awareness/forbidden_fixes.yaml",
		"decision":      "docs/awareness/architecture/decisions.yaml",
	}
	for _, kind := range gm.GovernedKinds() {
		t.Run(kind, func(t *testing.T) {
			root := repoDir(t)
			res, err := gm.Apply(gm.Request{RepositoryRoot: root, Proposal: validProposal(kind)})
			if err != nil {
				t.Fatalf("apply: %v", err)
			}
			if res.Disposition != gm.DispositionApplied {
				t.Fatalf("disposition = %s, want applied", res.Disposition)
			}
			if res.TargetRelPath != wantFile[kind] {
				t.Fatalf("target = %s, want %s", res.TargetRelPath, wantFile[kind])
			}
			if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(res.TargetRelPath))); err != nil {
				t.Fatalf("target file not written: %v", err)
			}
			if res.MutationDigestSHA256 == "" || res.PreManifestDigestSHA256 == "" || res.PostManifestDigestSHA256 == "" {
				t.Fatal("result must carry mutation + pre/post manifest digests")
			}
			if res.PreManifestDigestSHA256 == res.PostManifestDigestSHA256 {
				t.Fatal("a governed mutation must change the manifest digest")
			}
		})
	}
}

func TestValidationRejectsIncompleteNoWrite(t *testing.T) {
	root := repoDir(t)
	_, err := gm.Apply(gm.Request{RepositoryRoot: root, Proposal: propose.Request{Kind: "invariant", Title: "no anchors"}})
	var ve *gm.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("err = %v, want *ValidationError", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "docs/awareness/invariants.yaml")); !os.IsNotExist(statErr) {
		t.Fatal("validation failure must not create the source file")
	}
}

func TestExactReplayWritesNothing(t *testing.T) {
	root := repoDir(t)
	p := validProposal("failure_mode")
	if _, err := gm.Apply(gm.Request{RepositoryRoot: root, Proposal: p}); err != nil {
		t.Fatalf("first: %v", err)
	}
	path := filepath.Join(root, "docs/awareness/failure_modes.yaml")
	after1, _ := os.ReadFile(path)
	res, err := gm.Apply(gm.Request{RepositoryRoot: root, Proposal: p})
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if res.Disposition != gm.DispositionReplay {
		t.Fatalf("disposition = %s, want replay", res.Disposition)
	}
	after2, _ := os.ReadFile(path)
	if string(after1) != string(after2) {
		t.Fatal("replay mutated the file")
	}
}

// TestAppendMatchesExistingFileIndent is the regression ratchet for a live
// incident: renderItem used to hardcode 2-space list-item indentation
// regardless of what the target file already used. Appending a 2-space item
// after a run of 4-space items desyncs it from its siblings' YAML nesting
// level — the parser reads the shallower dash as ending the current block
// sequence, not continuing it, and the whole file fails to parse ("did not
// find expected key"). This broke `sensei build`/`briefing` for every OTHER
// entry in the file until a human found and hand-fixed it; the CLI itself
// reported status:created on the write that caused it. Every new item MUST
// be indented to match the file's own established convention.
func TestAppendMatchesExistingFileIndent(t *testing.T) {
	root := repoDir(t)
	path := filepath.Join(root, "docs/awareness/failure_modes.yaml")
	existing := "failure_modes:\n" +
		"    - id: failure.pre_existing\n" +
		"      title: A pre-existing 4-space-indented entry\n" +
		"      related_invariants:\n" +
		"        - awareness.some_invariant\n"
	if err := os.WriteFile(path, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	p := validProposal("failure_mode")
	res, err := gm.Apply(gm.Request{RepositoryRoot: root, Proposal: p})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if res.Disposition != gm.DispositionApplied {
		t.Fatalf("disposition = %s, want applied", res.Disposition)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)

	// The whole file, old entry + new entry, must still parse as YAML.
	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("resulting file is not valid YAML: %v\n---\n%s", err, text)
	}
	list, _ := doc["failure_modes"].([]any)
	if len(list) != 2 {
		t.Fatalf("expected 2 entries after append, got %d", len(list))
	}

	// The new entry's "- id:" line must be indented 4 spaces, matching the
	// pre-existing entry — not the package's old hardcoded 2.
	found := false
	for _, ln := range strings.Split(text, "\n") {
		if strings.Contains(ln, "- id: "+res.CanonicalID) {
			found = true
			if !strings.HasPrefix(ln, "    - id: ") {
				t.Fatalf("new entry indent = %q, want 4-space prefix matching the existing entry", ln)
			}
		}
	}
	if !found {
		t.Fatalf("new entry id %q not found in written file:\n%s", res.CanonicalID, text)
	}
}

// TestAppendToFreshFileDefaultsToTwoSpaceIndent verifies the default (2-space)
// still applies when there is no existing convention to match — a brand-new
// governed file, or a topKey with no items yet.
func TestAppendToFreshFileDefaultsToTwoSpaceIndent(t *testing.T) {
	root := repoDir(t)
	p := validProposal("failure_mode")
	res, err := gm.Apply(gm.Request{RepositoryRoot: root, Proposal: p})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	path := filepath.Join(root, "docs/awareness/failure_modes.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, ln := range strings.Split(string(data), "\n") {
		if strings.Contains(ln, "- id: "+res.CanonicalID) {
			found = true
			if !strings.HasPrefix(ln, "  - id: ") || strings.HasPrefix(ln, "   - id: ") {
				t.Fatalf("first-entry indent = %q, want exactly 2-space prefix", ln)
			}
		}
	}
	if !found {
		t.Fatal("new entry not found in written file")
	}
}

func TestContradictionRefusedNoWrite(t *testing.T) {
	root := repoDir(t)
	p := validProposal("failure_mode")
	p.ID = "failure.fixed"
	if _, err := gm.Apply(gm.Request{RepositoryRoot: root, Proposal: p}); err != nil {
		t.Fatalf("first: %v", err)
	}
	path := filepath.Join(root, "docs/awareness/failure_modes.yaml")
	before, _ := os.ReadFile(path)

	// Same id, different body.
	p2 := p
	p2.Description = "a genuinely different body"
	p2.Evidence = []string{"different evidence"}
	_, err := gm.Apply(gm.Request{RepositoryRoot: root, Proposal: p2})
	var ce *gm.ContradictionError
	if !errors.As(err, &ce) {
		t.Fatalf("err = %v, want *ContradictionError", err)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("contradiction must not overwrite the source")
	}
}

func TestStaleManifestRefusedNoWrite(t *testing.T) {
	root := repoDir(t)
	// Seed one record so the manifest is non-empty.
	if _, err := gm.Apply(gm.Request{RepositoryRoot: root, Proposal: validProposal("invariant")}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	path := filepath.Join(root, "docs/awareness/failure_modes.yaml")
	_, statBefore := os.Stat(path)

	_, err := gm.Apply(gm.Request{
		RepositoryRoot:               root,
		Proposal:                     validProposal("failure_mode"),
		ExpectedManifestDigestSHA256: "0000000000000000000000000000000000000000000000000000000000000000",
	})
	var se *gm.StaleManifestError
	if !errors.As(err, &se) {
		t.Fatalf("err = %v, want *StaleManifestError", err)
	}
	if _, statAfter := os.Stat(path); os.IsNotExist(statBefore) && statAfter == nil {
		t.Fatal("stale manifest must not write the new record")
	}
}

func TestManifestCASAcceptsCurrentDigest(t *testing.T) {
	root := repoDir(t)
	cur, err := gm.GovernedManifestDigest(root)
	if err != nil {
		t.Fatal(err)
	}
	res, err := gm.Apply(gm.Request{
		RepositoryRoot: root, Proposal: validProposal("failure_mode"), ExpectedManifestDigestSHA256: cur,
	})
	if err != nil {
		t.Fatalf("apply with current manifest: %v", err)
	}
	if res.Disposition != gm.DispositionApplied {
		t.Fatalf("disposition = %s", res.Disposition)
	}
}

func TestNoGraphSeedJournalReceiptSideEffects(t *testing.T) {
	root := repoDir(t)
	if _, err := gm.Apply(gm.Request{RepositoryRoot: root, Proposal: validProposal("invariant")}); err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{
		".sensei/project/graph.nt",
		".sensei/graph-authority.json",
		".sensei/project/promotions",
		"golang/server/embeddata/awareness.nt",
	} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(forbidden))); err == nil {
			t.Fatalf("governed mutation produced a forbidden side effect: %s", forbidden)
		}
	}
}

func TestContractUnknownQueuesToCandidatesManifestUnchanged(t *testing.T) {
	root := repoDir(t)
	pre, _ := gm.GovernedManifestDigest(root)
	res, err := gm.Apply(gm.Request{RepositoryRoot: root, Proposal: propose.Request{
		Kind: "contract_unknown", Title: "unknown contract", Description: "observed",
		ProposedContract: "x must y", Evidence: []string{"saw z"}, Domain: "github.com/globulario/sensei",
	}})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if res.Disposition != gm.DispositionCandidateQueued || !res.IsCandidate {
		t.Fatalf("disposition = %s candidate = %v", res.Disposition, res.IsCandidate)
	}
	if !strings.HasPrefix(res.TargetRelPath, "docs/awareness/candidates/") {
		t.Fatalf("candidate target = %s", res.TargetRelPath)
	}
	post, _ := gm.GovernedManifestDigest(root)
	if pre != post {
		t.Fatal("a candidate queue write must not change the governed manifest")
	}
}

func TestManifestDigestDeterministicAndSensitive(t *testing.T) {
	root := repoDir(t)
	a, _ := gm.GovernedManifestDigest(root)
	b, _ := gm.GovernedManifestDigest(root)
	if a != b {
		t.Fatal("manifest digest is not deterministic")
	}
	if _, err := gm.Apply(gm.Request{RepositoryRoot: root, Proposal: validProposal("invariant")}); err != nil {
		t.Fatal(err)
	}
	c, _ := gm.GovernedManifestDigest(root)
	if a == c {
		t.Fatal("manifest digest did not change after a governed mutation")
	}
}

func TestLockIsComposableAndExclusive(t *testing.T) {
	root := repoDir(t)
	now := time.Unix(1_700_000_000, 0).UTC()
	release, err := gm.AcquireLock(context.Background(), root, "test", now)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	// A second acquire must fail closed under a cancelled context (already held).
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := gm.AcquireLock(ctx, root, "test2", now); err == nil {
		t.Fatal("second acquire should fail while the lock is held")
	}
	// Apply performs no internal locking, so it composes under a held lock.
	if _, err := gm.Apply(gm.Request{RepositoryRoot: root, Proposal: validProposal("failure_mode")}); err != nil {
		t.Fatalf("apply under externally-held lock: %v", err)
	}
	release()
	// After release, the lock can be re-acquired.
	release2, err := gm.AcquireLock(context.Background(), root, "test3", now)
	if err != nil {
		t.Fatalf("re-acquire after release: %v", err)
	}
	release2()
}

// TestNoMutationPolicyLeftInCLI proves the extraction boundary: the production
// owner does not import cmd/awg, and cmd_propose.go retains no mutation-policy
// implementation (routing map, record structs, append/collision helpers).
func TestNoMutationPolicyLeftInCLI(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))

	// The owner package must not import cmd/awg.
	ownerDir := filepath.Join(root, "golang", "architecture", "governedmutation")
	entries, _ := os.ReadDir(ownerDir)
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(ownerDir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "globulario/sensei/cmd/awg") {
			t.Errorf("%s imports cmd/awg", e.Name())
		}
	}

	// cmd_propose.go must retain no mutation-policy implementation.
	proposeSrc, err := os.ReadFile(filepath.Join(root, "cmd", "awg", "cmd_propose.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{
		"proposeKindToFile", "func appendProposalEntry", "func proposalIDExists",
		"func buildCanonicalItem", "func planProposal", "func renderListItem",
	} {
		if strings.Contains(string(proposeSrc), forbidden) {
			t.Errorf("cmd_propose.go still contains mutation policy %q", forbidden)
		}
	}
}
