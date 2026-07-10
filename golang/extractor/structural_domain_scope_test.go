// SPDX-License-Identifier: AGPL-3.0-only

package extractor_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/globulario/awareness-graph/golang/extractor"
)

// importDirWithRepo imports dir with a default foreign-repo domain scope, the
// way `awg build --repo <repo>` does, and returns the N-Triples as a string.
func importDirWithRepo(t *testing.T, dir, repo, sourceSet string) string {
	t.Helper()
	var buf bytes.Buffer
	_, _, err := extractor.ImportAwarenessDirWithOpts(dir, &buf, extractor.ImportDirOptions{
		DefaultRepo:      repo,
		DefaultSourceSet: sourceSet,
	})
	if err != nil {
		t.Fatalf("ImportAwarenessDirWithOpts: %v", err)
	}
	return buf.String()
}

const cliComponents = `
components:
  - id: component.api
    name: api
    kind: module
    depends_on:
      - component.internal.ghinstance
    source_files:
      - api/client.go
`

// A foreign repo's structural extractor output is domain-agnostic. With a
// default repo scope at import (the foreign-repo bootstrap), every structural
// node must carry aw:domain=repo + aw:repo=<repo> so the scope filter can
// isolate it — instead of leaking into the untagged home domain.
func TestStructural_DefaultRepoScope_TagsComponents(t *testing.T) {
	root := makeDir(t, map[string]string{"generated/go_import_graph.yaml": cliComponents})
	nt := importDirWithRepo(t, root, "github.com/cli/cli", "pilot/cli")

	comp := `<https://globular.io/awareness#component/component.api>`
	wantTriple(t, nt, comp+` <https://globular.io/awareness#domain> "repo"`, "component domain=repo")
	wantTriple(t, nt, comp+` <https://globular.io/awareness#repo> "github.com/cli/cli"`, "component repo tag")
	wantTriple(t, nt, comp+` <https://globular.io/awareness#sourceSet> "pilot/cli"`, "component source-set")
	// The file anchor that makes impact-by-file work must still be emitted.
	wantTriple(t, nt,
		`<https://globular.io/awareness#sourceFile/api%2Fclient.go> <https://globular.io/awareness#implements> `+comp,
		"source-file implements component anchor")
}

// No default repo + no inline scope → unchanged home-domain behaviour: NO
// repo/domain triples (the embedded seed must compile byte-for-byte as before).
func TestStructural_NoDefault_StaysHomeDomain(t *testing.T) {
	root := makeDir(t, map[string]string{"components.yaml": cliComponents})
	_, nt := importDir(t, root)
	if strings.Contains(nt, `<https://globular.io/awareness#repo>`) ||
		strings.Contains(nt, `#domain> "repo"`) {
		t.Fatalf("untagged structural import must not emit repo/domain triples:\n%s", nt)
	}
}

// An inline scope on a node always wins over the import-wide default.
func TestStructural_InlineScope_OverridesDefault(t *testing.T) {
	root := makeDir(t, map[string]string{"components.yaml": `
components:
  - id: component.shared.meta
    name: shared
    kind: module
    domain: shared
`})
	nt := importDirWithRepo(t, root, "github.com/cli/cli", "pilot/cli")
	comp := `<https://globular.io/awareness#component/component.shared.meta>`
	wantTriple(t, nt, comp+` <https://globular.io/awareness#domain> "shared"`, "inline shared scope wins")
	if strings.Contains(nt, comp+` <https://globular.io/awareness#repo> "github.com/cli/cli"`) {
		t.Fatalf("default repo must NOT override an inline shared scope:\n%s", nt)
	}
}
