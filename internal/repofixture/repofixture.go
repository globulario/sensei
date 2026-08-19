// SPDX-License-Identifier: AGPL-3.0-only

// Package repofixture gives tests a checkout that carries a canonical
// repository identity.
//
// SourceFile subjects are scoped to the repository that owns them (issue
// #197), and a build refuses a tree whose repository identity it cannot
// resolve rather than mint unscoped identities that collapse across
// repositories. A test corpus in a temp directory is a repository too, so
// it declares one -- through the same .sensei/config.yaml the real
// resolution path reads, not through a test-only override, so the fixture
// exercises what production does.
package repofixture

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/statedir"
)

// DefaultDomain is the repository identity fixtures use when they do not
// care which repository they are.
const DefaultDomain = "github.com/test/repo"

// WriteRepositoryIdentity declares domain as root's canonical repository
// identity, creating root's state directory if needed. It appends to an
// existing config rather than replacing it, so a fixture that already wrote
// other configuration keeps it.
func WriteRepositoryIdentity(t *testing.T, root, domain string) {
	t.Helper()
	if domain == "" {
		domain = DefaultDomain
	}
	path := statedir.Path(root, "config.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	content := string(existing)
	// Idempotent: a fixture helper is often called once per source root
	// against the same checkout, and a second `repository:` key would make
	// the config a duplicate-key parse error -- which the resolver
	// correctly refuses, failing the test for the wrong reason.
	if strings.Contains(content, "\nrepository:") || strings.HasPrefix(content, "repository:") {
		return
	}
	if len(content) > 0 && content[len(content)-1] != '\n' {
		content += "\n"
	}
	content += "repository:\n  domain: " + domain + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
