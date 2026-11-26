// SPDX-License-Identifier: AGPL-3.0-only

package questionpromotion

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Architect ruling on #390: governed promotion and repository mutation are
// distinct authorization obligations that one promotion must BOTH satisfy.
// Promotion establishes whether the knowledge may be promoted; repository
// mutation authorizes the corresponding file write. Neither grants the other.

func stripGrant(t *testing.T, repo, id string) {
	t.Helper()
	p := filepath.Join(repo, "docs", "awareness", "authority_grants.yaml")
	body, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(body), "\n")
	start := -1
	for i, l := range lines {
		if strings.TrimSpace(l) == "- id: "+id {
			start = i
			break
		}
	}
	if start < 0 {
		t.Fatalf("grant %s not present: nothing to strip", id)
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "  - id: ") {
			end = i
			break
		}
	}
	out := append(append([]string{}, lines[:start]...), lines[end:]...)
	if err := os.WriteFile(p, []byte(strings.Join(out, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Promotion without repository-mutation authority for its governed-source
// write is refused before anything is written: promotion never acquires a
// repository write the repository authority would refuse.
func TestPromotionRequiresRepositoryMutationAuthorityForItsWrite(t *testing.T) {
	p := seedPromotable(t)
	stripGrant(t, p.Repo, "grant.sensei.repository_repair")
	res, _ := Promote(context.Background(), p.request())
	if res.Outcome != OutcomeAuthorityRefusal {
		t.Fatalf("outcome = %s (%s): promotion wrote governed source without repository-mutation authority", res.Outcome, res.Detail)
	}
	if !strings.Contains(res.Detail, "authority.sensei_repository_mutation") {
		t.Fatalf("refusal does not name the missing obligation: %q", res.Detail)
	}
	assertNoGovernedRecord(t, p)
}

// The converse: repository-mutation authority does not promote knowledge.
func TestRepositoryMutationAuthorityDoesNotPromote(t *testing.T) {
	p := seedPromotable(t)
	stripGrant(t, p.Repo, "grant.sensei.governed_promotion")
	res, _ := Promote(context.Background(), p.request())
	if res.Outcome != OutcomeAuthorityRefusal {
		t.Fatalf("outcome = %s (%s): repository mutation established promoted knowledge", res.Outcome, res.Detail)
	}
	assertNoGovernedRecord(t, p)
}

// Promotion is not unrestricted write access: a committed promotion changes
// exactly the one governed source its record routes to, never another
// repository file.
func TestPromotionWritesOnlyItsGovernedSource(t *testing.T) {
	p := seedPromotable(t)
	before := snapshotRepoFiles(t, p.Repo)
	res, err := Promote(context.Background(), p.request())
	if err != nil || res.Outcome != OutcomeCommitted {
		t.Fatalf("promote: %v (%s %s)", err, res.Outcome, res.Detail)
	}
	after := snapshotRepoFiles(t, p.Repo)
	var changed []string
	for path, sum := range after {
		if before[path] != sum {
			changed = append(changed, path)
		}
	}
	for path := range before {
		if _, ok := after[path]; !ok {
			changed = append(changed, path)
		}
	}
	if len(changed) != 1 || changed[0] != "docs/awareness/invariants.yaml" {
		t.Fatalf("promotion changed %v outside Sensei's own state, want only docs/awareness/invariants.yaml", changed)
	}
}

// snapshotRepoFiles maps every repository file outside .git and .sensei (task,
// journal, graph and receipt state, owned by the transaction) to its content.
func snapshotRepoFiles(t *testing.T, repo string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(repo, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(repo, path)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if rel == ".git" || rel == ".sensei" {
				return filepath.SkipDir
			}
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out[rel] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}
