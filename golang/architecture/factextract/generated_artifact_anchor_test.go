// SPDX-License-Identifier: AGPL-3.0-only

package factextract

import (
	"os"
	"path/filepath"
	"testing"
)

// A generated artifact can be a directory, and observing that it exists is
// legitimate. Anchoring that observation to the directory as though it were a
// source file is not: source-digest capture reported "is a directory", and the
// #131 evidence-resolvability pass counted it as an anchor that does not
// resolve. The path is already the fact's subject and object, so the anchor
// carries no information that dropping it loses.
func TestGeneratedArtifactFactsDoNotAnchorToADirectory(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs", "awareness", "generated"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "golang", "server", "embeddata"), 0o755); err != nil {
		t.Fatal(err)
	}
	seed := filepath.Join(root, "golang", "server", "embeddata", "awareness.nt")
	if err := os.WriteFile(seed, []byte("<a> <b> <c> .\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	facts := extractGeneratedAuthorityFacts(invariantRepositoryIdentity{Root: root, Repository: "example.test/repo"})

	var sawDirectory, sawFile bool
	for _, f := range facts {
		if f.Predicate != "generated_artifact_exists" {
			continue
		}
		switch f.Subject {
		case "docs/awareness/generated":
			sawDirectory = true
			if f.Evidence.SourceFile != "" {
				t.Fatalf("a directory was anchored as a source file: %q", f.Evidence.SourceFile)
			}
			if f.Meta["generated_artifact_kind"] != "directory" {
				t.Fatalf("the absent anchor is not explained: meta=%v", f.Meta)
			}
			if f.Object != "docs/awareness/generated" {
				t.Fatalf("dropping the anchor lost the path: %+v", f)
			}
		case "golang/server/embeddata/awareness.nt":
			sawFile = true
			if f.Evidence.SourceFile != "golang/server/embeddata/awareness.nt" {
				t.Fatalf("a real file lost its anchor: %+v", f)
			}
			if f.Meta["generated_artifact_kind"] != "file" {
				t.Fatalf("a real file is not recorded as one: meta=%v", f.Meta)
			}
		}
	}
	if !sawDirectory || !sawFile {
		t.Fatalf("fixture did not produce both cases: directory=%v file=%v", sawDirectory, sawFile)
	}
}
