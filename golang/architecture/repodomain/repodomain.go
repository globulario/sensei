// SPDX-License-Identifier: AGPL-3.0-only

// Package repodomain holds the canonical, importable primitives for reading
// and validating a checkout's configured repository domain from
// .sensei/config.yaml (docs/design/checkout-repository-domain-binding.md).
//
// It is a mechanical extraction of exactly the config-reading/validation
// core that already lived, unexported, in cmd/awg/repo_domain_binding.go —
// so a second caller (the awareness-mcp bridge, for governed workspace
// identity) can read and validate the same configured domain without
// reimplementing config parsing or domain-shape validation. cmd/awg's
// resolver (which additionally layers explicit-flag and SENSEI_DOMAIN/
// AWG_DOMAIN environment precedence, and the init/bootstrap write path) is
// unchanged in behavior and now delegates its config-read/validate steps to
// this package; its full existing test suite is the coverage for this
// extraction.
//
// This package does not implement the full checkout-scoped resolver
// precedence (explicit flag / SENSEI_DOMAIN / AWG_DOMAIN) — only ordinary
// CLI commands use that broader precedence. A governed external contract
// (sensei.workspace.identity.v1) requires the stricter rule that configured
// identity is the ONLY source of governed repository domain; environment
// variables must never establish it (contract
// docs/design/workspace-identity-admission-contracts.md §3.3). Callers that
// need the broader CLI precedence keep their own resolver; this package
// gives them (and any governed caller) one shared, tested config primitive
// to build it from.
package repodomain

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/globulario/sensei/golang/statedir"
	"gopkg.in/yaml.v3"
)

// Config is the `repository:` section of .sensei/config.yaml. Only the
// fields this package understands are modeled.
type Config struct {
	Repository struct {
		Domain string `yaml:"domain"`
	} `yaml:"repository"`
}

// ConfigPath returns the resolved .sensei/config.yaml (or legacy
// .awg/config.yaml) path for root.
func ConfigPath(root string) string {
	return statedir.Path(root, "config.yaml")
}

// LoadConfig reads the repository.domain section of root's config. A
// missing config file is not an error — it returns a zero-value config, the
// same as an existing config with no repository section (the domain
// section is optional).
func LoadConfig(root string) (Config, error) {
	raw, err := os.ReadFile(ConfigPath(root))
	if err != nil {
		if os.IsNotExist(err) {
			return Config{}, nil
		}
		return Config{}, fmt.Errorf("read %s: %w", ConfigPath(root), err)
	}
	var cfg Config
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse %s: %w", ConfigPath(root), err)
	}
	return cfg, nil
}

// domainHostRe matches a DNS-like hostname: at least two dot-separated
// labels (e.g. "github.com"), each a valid label per RFC 1123. Bare
// hostnames ("localhost"), schemes, and whitespace are rejected by Validate
// before this ever runs.
var domainHostRe = regexp.MustCompile(`(?i)^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+$`)

// Validate rejects anything that is not a canonical repository domain
// string: host.tld/path (e.g. "github.com/owner/repo") — no scheme, no
// whitespace, a DNS-like host, and a non-empty path. Every caller that
// resolves, configures, or persists a repository domain shares this one
// validator so no caller can act on a guessed/garbage identity.
func Validate(d string) error {
	if d == "" {
		return fmt.Errorf("must not be empty")
	}
	if strings.ContainsAny(d, " \t\n\r") {
		return fmt.Errorf("must not contain whitespace")
	}
	if strings.Contains(d, "://") {
		return fmt.Errorf("must be host/path (e.g. github.com/owner/repo), not a URL with a scheme")
	}
	host, path, found := strings.Cut(d, "/")
	if !found || path == "" {
		return fmt.Errorf("must be host/path (e.g. github.com/owner/repo)")
	}
	if !domainHostRe.MatchString(host) {
		return fmt.Errorf("host %q is not a valid domain name", host)
	}

	// The host shape check above is case-insensitive by design (so a
	// mixed-case host gets THIS specific message, not "invalid domain
	// name") — canonical form additionally requires it already be
	// lowercase, exactly as the git-origin parser always produces: a value
	// that only differs from the graph's own canonical domain by case,
	// trailing slash, ".git" suffix, or path noise resolves and persists
	// successfully but silently never matches.
	if host != strings.ToLower(host) {
		return fmt.Errorf("host %q must be lowercase (canonical form: %q)", host, strings.ToLower(host))
	}
	if strings.ContainsAny(path, "?#") {
		return fmt.Errorf("path %q must not contain a query string or fragment", path)
	}
	if strings.Contains(path, `\`) {
		return fmt.Errorf("path %q must use forward slashes", path)
	}
	if strings.HasPrefix(path, "/") || strings.HasSuffix(path, "/") {
		return fmt.Errorf("path %q must not have a leading or trailing slash (canonical form: %q)", path, strings.Trim(path, "/"))
	}
	segments := strings.Split(path, "/")
	for _, seg := range segments {
		switch seg {
		case "":
			return fmt.Errorf("path %q must not contain empty (\"//\") segments", path)
		case ".", "..":
			return fmt.Errorf("path %q must not contain \".\" or \"..\" segments", path)
		}
	}
	if strings.HasSuffix(strings.ToLower(path), ".git") {
		return fmt.Errorf("path %q must not end with \".git\" (canonical form strips it, matching the git-origin parser)", path)
	}
	return nil
}

// Configured resolves ONLY the canonical configured repository domain for
// root — never an explicit override, never SENSEI_DOMAIN/AWG_DOMAIN. This
// is the strict rule a governed external contract requires (workspace
// identity contract §3.3); ordinary CLI commands that also accept an
// explicit flag or environment fallback keep their own broader resolver.
//
// Returns ("", nil) when no repository.domain is configured (a legitimate,
// non-error "unbound" state). Returns a non-nil error when the config file
// exists but is malformed, or the configured value fails Validate — both
// are identity-authority failures that must never be silently treated as
// "unbound" (an invalid configured value is not the same fact as no
// configured value at all).
func Configured(root string) (string, error) {
	cfg, err := LoadConfig(root)
	if err != nil {
		return "", fmt.Errorf("repository domain configuration is malformed: %w", err)
	}
	d := strings.TrimSpace(cfg.Repository.Domain)
	if d == "" {
		return "", nil
	}
	if err := Validate(d); err != nil {
		return "", fmt.Errorf("configured repository domain %q is invalid: %w", d, err)
	}
	return d, nil
}

// DeclarationPath returns the COMMITTED repository-identity declaration for
// root: docs/awareness/repository.yaml.
//
// This is deliberately a tracked file, unlike .sensei/config.yaml, which is
// local runtime state and gitignored. A SourceFile identity must be the
// same subject "across checkouts, machines, and publication domains"
// (issue #197), and an identity that lives only in ignored local state
// cannot be: a fresh clone -- CI, a new machine, another contributor --
// would carry none at all.
func DeclarationPath(root string) string {
	return filepath.Join(root, "docs", "awareness", "repository.yaml")
}

// loadDeclaration reads root's committed repository-identity declaration.
// A missing file is not an error.
func loadDeclaration(root string) (string, error) {
	path := DeclarationPath(root)
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return "", fmt.Errorf("parse %s: %w", path, err)
	}
	return strings.TrimSpace(cfg.Repository.Domain), nil
}

// IdentityForTree resolves the canonical repository identity of the
// repository that owns dir, by walking up from dir for the nearest checkout
// that declares one, and validating it.
//
// This is the identity SourceFile subjects are scoped to (issue #197): a
// durable property of the repository, NOT the publication domain a build
// selects with --repo and NOT the checkout path. Walking up from the tree
// being imported (rather than from the process's working directory) is what
// makes a cross-repo build attribute the other repository's files to the
// other repository: a file belongs to the repository it lives in.
//
// Two declarations are read at each level:
//
//   - docs/awareness/repository.yaml, the COMMITTED declaration. It travels
//     with the repository, so every checkout on every machine resolves the
//     same identity;
//   - .sensei/config.yaml repository.domain, the local one `sensei init`
//     establishes. It is gitignored runtime state, so it is honored only
//     where no committed declaration exists -- a checkout that was
//     initialized but has not yet committed its identity.
//
// If both exist and DISAGREE, resolution fails rather than picking one.
// That is the same law the endpoint guard applies (issue #212): two signals
// naming different things must never be resolved silently, because the
// wrong one is indistinguishable from the right one afterwards.
//
// Returns "" with a nil error when nothing above dir declares an identity --
// an unresolved identity, which callers that mint identities must refuse
// rather than substitute a fallback for. A malformed config, or a declared
// value that fails Validate, is a non-nil error and never falls through to
// a higher directory: configuration that fails to parse must fail visibly,
// not be worked around by guessing.
func IdentityForTree(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", dir, err)
	}
	for current := abs; ; {
		declared, err := loadDeclaration(current)
		if err != nil {
			return "", err
		}
		var configured string
		if _, statErr := os.Stat(ConfigPath(current)); statErr == nil {
			cfg, err := LoadConfig(current)
			if err != nil {
				return "", err
			}
			configured = strings.TrimSpace(cfg.Repository.Domain)
		}
		switch {
		case declared != "" && configured != "" && declared != configured:
			return "", fmt.Errorf(
				"repository identity is declared twice and they disagree: %s says %q, %s says %q.\n"+
					"The committed declaration is the durable one; align the local configuration with it, or correct the declaration",
				DeclarationPath(current), declared, ConfigPath(current), configured)
		case declared != "":
			if err := Validate(declared); err != nil {
				return "", fmt.Errorf("declared repository domain %q in %s is invalid: %w", declared, DeclarationPath(current), err)
			}
			return declared, nil
		case configured != "":
			if err := Validate(configured); err != nil {
				return "", fmt.Errorf("configured repository domain %q in %s is invalid: %w", configured, ConfigPath(current), err)
			}
			return configured, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", nil
		}
		current = parent
	}
}
