// SPDX-License-Identifier: AGPL-3.0-only

package repodomain

import (
	"os"
	"path/filepath"
	"testing"
)

func writeConfig(t *testing.T, root, content string) {
	t.Helper()
	full := filepath.Join(root, ".sensei", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestConfigured_ReturnsConfiguredDomain(t *testing.T) {
	root := t.TempDir()
	writeConfig(t, root, "repository:\n  domain: github.com/globulario/sensei\n")
	got, err := Configured(root)
	if err != nil {
		t.Fatal(err)
	}
	if got != "github.com/globulario/sensei" {
		t.Fatalf("Configured = %q, want github.com/globulario/sensei", got)
	}
}

func TestConfigured_UnboundWhenNoConfig(t *testing.T) {
	root := t.TempDir()
	got, err := Configured(root)
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("Configured = %q, want empty (unbound)", got)
	}
}

func TestConfigured_UnboundWhenConfigHasNoRepositorySection(t *testing.T) {
	root := t.TempDir()
	writeConfig(t, root, "other:\n  key: value\n")
	got, err := Configured(root)
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("Configured = %q, want empty (unbound)", got)
	}
}

func TestConfigured_ErrorsOnMalformedYAML(t *testing.T) {
	root := t.TempDir()
	writeConfig(t, root, "repository: [unterminated\n")
	if _, err := Configured(root); err == nil {
		t.Fatal("expected an error for malformed config.yaml, got nil")
	}
}

func TestConfigured_ErrorsOnInvalidConfiguredDomain(t *testing.T) {
	root := t.TempDir()
	writeConfig(t, root, "repository:\n  domain: not-a-valid-domain\n")
	if _, err := Configured(root); err == nil {
		t.Fatal("expected an error for an invalid configured domain, got nil")
	}
}

func TestValidate_AcceptsCanonicalShape(t *testing.T) {
	if err := Validate("github.com/globulario/sensei"); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestValidate_RejectsNonCanonicalForms(t *testing.T) {
	cases := []string{
		"",
		"GitHub.com/globulario/sensei",
		"github.com/globulario/sensei/",
		"github.com/globulario/sensei.git",
		"github.com//sensei",
		"github.com/../sensei",
		"https://github.com/globulario/sensei",
		"localhost/sensei",
		"github.com/globulario/sensei?x=1",
		`github.com\globulario\sensei`,
	}
	for _, c := range cases {
		if err := Validate(c); err == nil {
			t.Errorf("Validate(%q): expected error, got nil", c)
		}
	}
}

func TestConfigPath_UsesStatedir(t *testing.T) {
	root := t.TempDir()
	got := ConfigPath(root)
	want := filepath.Join(root, ".sensei", "config.yaml")
	if got != want {
		t.Fatalf("ConfigPath = %q, want %q", got, want)
	}
}
