// SPDX-License-Identifier: AGPL-3.0-only

// repo_domain_binding.go implements docs/design/checkout-repository-domain-binding.md:
// a durable, checkout-local repository domain identity, and the one canonical
// resolution precedence every checkout-scoped command and hook must share.
//
// Store contents (which domains happen to be loaded into Oxigraph) are
// observation state, never checkout identity (contract §3.1) — this file
// never queries the graph. The resolved domain comes ONLY from: an explicit
// flag, durable .sensei/config.yaml configuration, or a bounded environment
// fallback.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/globulario/sensei/golang/statedir"
	"gopkg.in/yaml.v3"
)

// repoDomainConfig is the `repository:` section of .sensei/config.yaml. Only
// the fields this package understands are modeled; unmarshaling into a
// yaml.Node round-trip (writeRepositoryDomain) preserves every other section
// and any comments verbatim.
type repoDomainConfig struct {
	Repository struct {
		Domain string `yaml:"domain"`
	} `yaml:"repository"`
}

// repoDomainConfigPath returns the resolved .sensei/config.yaml (or legacy
// .awg/config.yaml) path for root.
func repoDomainConfigPath(root string) string {
	return statedir.Path(root, "config.yaml")
}

// loadRepoDomainConfig reads the repository.domain section of root's config.
// A missing config file is not an error — it returns a zero-value config, the
// same as an existing config with no repository section (contract §3.2: the
// section is optional).
func loadRepoDomainConfig(root string) (repoDomainConfig, error) {
	raw, err := os.ReadFile(repoDomainConfigPath(root))
	if err != nil {
		if os.IsNotExist(err) {
			return repoDomainConfig{}, nil
		}
		return repoDomainConfig{}, fmt.Errorf("read %s: %w", repoDomainConfigPath(root), err)
	}
	var cfg repoDomainConfig
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return repoDomainConfig{}, fmt.Errorf("parse %s: %w", repoDomainConfigPath(root), err)
	}
	return cfg, nil
}

// domainResolution is the typed result of resolveRepositoryDomain: the
// resolved domain (possibly "") plus which precedence tier supplied it, so
// callers and diagnostics never have to re-derive or guess the reason.
type domainResolution struct {
	Domain string
	// Source is one of: "explicit", "configured", "SENSEI_DOMAIN",
	// "AWG_DOMAIN" (legacy), or "unresolved".
	Source string
	// Err is non-nil when the tier identified by Source could not be
	// trusted: a malformed .sensei/config.yaml, or a value at ANY tier that
	// fails validateDomain. Checkout identity is an authority boundary — a
	// malformed or invalid value must fail visibly here and stop resolution
	// outright, never be silently skipped in favor of a lower-precedence
	// tier or a guessed fallback (contract §3.6 correction). Domain is
	// always "" when Err is non-nil.
	Err error
}

const (
	domainSourceExplicit   = "explicit"
	domainSourceConfigured = "configured"
	domainSourceEnvNew     = "SENSEI_DOMAIN"
	domainSourceEnvLegacy  = "AWG_DOMAIN"
	domainSourceUnresolved = "unresolved"
)

// domainHostRe matches a DNS-like hostname: at least two dot-separated
// labels (e.g. "github.com"), each a valid label per RFC 1123. Bare
// hostnames ("localhost"), schemes, and whitespace are rejected by
// validateDomain before this ever runs.
var domainHostRe = regexp.MustCompile(`(?i)^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+$`)

// validateDomain rejects anything that is not a canonical repository domain
// string: host.tld/path (e.g. "github.com/owner/repo") — no scheme, no
// whitespace, a DNS-like host, and a non-empty path (contract §3.7
// correction: explicit, configured, and newly-published values all share
// this one validator so no caller can persist or act on a guessed/garbage
// identity).
func validateDomain(d string) error {
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
	// lowercase, exactly as domainFromRemoteURL (the git-origin parser)
	// always produces (contract §3.7 correction, second round: a value
	// that only differs from the graph's own canonical domain by case,
	// trailing slash, ".git" suffix, or path noise resolves and persists
	// successfully but silently never matches).
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

// resolveRepositoryDomain implements the checkout-scoped resolver precedence
// (contract §3.5):
//
//  1. explicit (a command's --domain flag or equivalent);
//  2. canonical .sensei/config.yaml repository.domain from the resolved
//     checkout root;
//  3. SENSEI_DOMAIN environment variable, only when repository configuration
//     is absent;
//  4. legacy AWG_DOMAIN, only when neither canonical configuration nor
//     SENSEI_DOMAIN is present;
//  5. otherwise unresolved ("").
//
// Repository configuration is never silently overridden by an ambient
// environment variable — configured identity always outranks it. A
// malformed .sensei/config.yaml, or a value at any tier that fails
// validateDomain, stops resolution immediately with Err set — it never
// falls through to a lower-precedence tier (contract §3.6 correction: a
// checkout's identity configuration failing to parse must never be silently
// "worked around" by guessing at a different domain from the environment).
func resolveRepositoryDomain(root, explicit string) domainResolution {
	if e := strings.TrimSpace(explicit); e != "" {
		if err := validateDomain(e); err != nil {
			return domainResolution{Source: domainSourceExplicit, Err: fmt.Errorf("--domain value %q is invalid: %w", e, err)}
		}
		return domainResolution{Domain: e, Source: domainSourceExplicit}
	}
	cfg, cfgErr := loadRepoDomainConfig(root)
	if cfgErr != nil {
		return domainResolution{Source: domainSourceConfigured, Err: fmt.Errorf("repository domain configuration is malformed: %w", cfgErr)}
	}
	if d := strings.TrimSpace(cfg.Repository.Domain); d != "" {
		if err := validateDomain(d); err != nil {
			return domainResolution{Source: domainSourceConfigured, Err: fmt.Errorf("configured repository domain %q is invalid: %w", d, err)}
		}
		return domainResolution{Domain: d, Source: domainSourceConfigured}
	}
	if v := strings.TrimSpace(os.Getenv("SENSEI_DOMAIN")); v != "" {
		if err := validateDomain(v); err != nil {
			return domainResolution{Source: domainSourceEnvNew, Err: fmt.Errorf("SENSEI_DOMAIN value %q is invalid: %w", v, err)}
		}
		return domainResolution{Domain: v, Source: domainSourceEnvNew}
	}
	if v := strings.TrimSpace(os.Getenv("AWG_DOMAIN")); v != "" {
		if err := validateDomain(v); err != nil {
			return domainResolution{Source: domainSourceEnvLegacy, Err: fmt.Errorf("AWG_DOMAIN value %q is invalid: %w", v, err)}
		}
		return domainResolution{Domain: v, Source: domainSourceEnvLegacy}
	}
	return domainResolution{Domain: "", Source: domainSourceUnresolved}
}

// establishmentResult reports what establishRepositoryDomain did, for
// init/bootstrap reports and the PR #117 required-evidence format.
type establishmentResult struct {
	Domain    string
	Source    string // "explicit" | "existing_config" | "git_origin" | "unbound"
	Written   bool   // true if config.yaml was created/updated
	Mismatch  bool   // true if a configured domain disagrees with the current git origin
	OriginURL string // the git origin domain observed, if any (diagnostic only)
}

// establishRepositoryDomain implements the bounded establishment order
// (contract §3.3) used by `sensei init` / `sensei bootstrap`:
//
//  1. an explicit initialization/bootstrap domain flag;
//  2. an already-configured canonical repository.domain (PRESERVED — never
//     silently rewritten, even if the git remote later changes — contract
//     §3.4);
//  3. deterministic parsing of the checkout's git remote.origin.url;
//  4. otherwise, leave the repository domain explicitly unbound.
//
// A configured/remote mismatch is reported (Mismatch=true) rather than
// silently followed or silently ignored.
func establishRepositoryDomain(root, explicitFlag string) (establishmentResult, error) {
	cfg, err := loadRepoDomainConfig(root)
	if err != nil {
		return establishmentResult{}, fmt.Errorf("repository domain configuration is malformed: %w", err)
	}
	existing := strings.TrimSpace(cfg.Repository.Domain)
	if existing != "" {
		if verr := validateDomain(existing); verr != nil {
			return establishmentResult{}, fmt.Errorf("configured repository domain %q is invalid: %w", existing, verr)
		}
	}
	origin := gitRemoteDomain(root)

	res := establishmentResult{OriginURL: origin}
	switch {
	case strings.TrimSpace(explicitFlag) != "":
		d := strings.TrimSpace(explicitFlag)
		if verr := validateDomain(d); verr != nil {
			return establishmentResult{}, fmt.Errorf("--domain value %q is invalid: %w", d, verr)
		}
		res.Domain, res.Source = d, "explicit"
	case existing != "":
		res.Domain, res.Source = existing, "existing_config"
		if origin != "" && origin != existing {
			res.Mismatch = true
		}
	case origin != "":
		res.Domain, res.Source = origin, "git_origin"
	default:
		res.Source = "unbound"
	}

	// Never rewrite an existing configured domain (contract §3.4) — only
	// write when we resolved a NEW domain that the config didn't already have.
	if res.Domain != "" && res.Domain != existing {
		if err := writeRepositoryDomain(root, res.Domain); err != nil {
			return res, err
		}
		res.Written = true
	}
	return res, nil
}

// writeRepositoryDomain sets repository.domain in root's .sensei/config.yaml,
// preserving every other section and comment via a yaml.Node round-trip
// rather than a lossy generic-map re-marshal. Creates the config file (using
// the standard scaffolded template as a base) if it does not exist yet.
//
// Validates domain before writing anything — a defense-in-depth boundary so
// no caller of this function (present or future) can ever persist an
// invalid value regardless of how it validated its own input upstream
// (contract §3.7 correction: "a shared validator is needed for configured,
// explicit, and newly published values").
func writeRepositoryDomain(root, domain string) error {
	if err := validateDomain(domain); err != nil {
		return fmt.Errorf("refusing to persist invalid repository domain %q: %w", domain, err)
	}
	path := repoDomainConfigPath(root)
	raw, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("read %s: %w", path, err)
		}
		tmpl, terr := templates.ReadFile("templates/config.yaml")
		if terr != nil {
			return fmt.Errorf("read config template: %w", terr)
		}
		raw = tmpl
	}

	var doc yaml.Node
	if len(strings.TrimSpace(string(raw))) == 0 {
		doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}}
	} else if err := yaml.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	if len(doc.Content) == 0 {
		doc.Content = []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}
	}
	root0 := doc.Content[0]
	if root0.Kind != yaml.MappingNode {
		return fmt.Errorf("%s: top-level document is not a mapping", path)
	}

	repoNode := findOrInsertMappingKey(root0, "repository", yaml.MappingNode, "!!map")
	domainNode := findOrInsertMappingKey(repoNode, "domain", yaml.ScalarNode, "!!str")
	domainNode.SetString(domain)

	out, err := yaml.Marshal(&doc)
	if err != nil {
		return fmt.Errorf("marshal %s: %w", path, err)
	}
	return writeFileAtomicDomain(path, out)
}

// findOrInsertMappingKey finds key's value node within mapping (a
// yaml.MappingNode), or appends a new key/value pair of the given kind/tag
// and returns the new value node. mapping's Content alternates key, value,
// key, value, ... per yaml.v3's Node representation.
func findOrInsertMappingKey(mapping *yaml.Node, key string, valueKind yaml.Kind, valueTag string) *yaml.Node {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1]
		}
	}
	keyNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}
	valueNode := &yaml.Node{Kind: valueKind, Tag: valueTag}
	mapping.Content = append(mapping.Content, keyNode, valueNode)
	return valueNode
}

// writeFileAtomicDomain writes data to path via a temp-file-then-rename in
// the same directory, mirroring golang/architecture/protection's atomic
// snapshot publication pattern.
func writeFileAtomicDomain(path string, data []byte) error {
	if err := os.MkdirAll(dirOf(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dirOf(path), ".config.yaml.tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func dirOf(path string) string {
	i := strings.LastIndexByte(path, '/')
	if i < 0 {
		return "."
	}
	return path[:i]
}

// runRepoDomain implements `sensei repo-domain`: prints the resolved
// checkout-scoped repository domain (contract §3.5 precedence) as plain text,
// so shell hooks (record-briefing.sh, enforce-briefing.sh) and other
// non-Go callers bind to the exact same domain the Go CLI resolves — never a
// separately reimplemented precedence chain. Prints nothing (exit 0) when
// unresolved: an unbound domain is a legitimate, non-error state.
func runRepoDomain(args []string) int {
	fs := flag.NewFlagSet("sensei repo-domain", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	path := fs.String("path", ".", "repository root to resolve")
	explicit := fs.String("domain", "", "explicit domain override (wins over configured/env)")
	asJSON := fs.Bool("json", false, "output as JSON")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: sensei repo-domain [--path <repo>] [--domain <explicit>] [--json]

Resolves and prints the checkout-scoped repository domain using the
canonical precedence: explicit flag > configured .sensei/config.yaml >
SENSEI_DOMAIN > legacy AWG_DOMAIN > unresolved.

Flags:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}

	res := resolveRepositoryDomain(*path, *explicit)
	if res.Err != nil {
		// Malformed/invalid checkout identity must fail visibly in BOTH
		// output modes with a non-zero exit — never silently print an empty
		// domain that a caller might mistake for "legitimately unbound"
		// (contract §3.6 correction).
		if *asJSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			_ = enc.Encode(map[string]any{
				"error":  res.Err.Error(),
				"source": res.Source,
			})
			return 1
		}
		fmt.Fprintf(os.Stderr, "sensei repo-domain: %v\n", res.Err)
		return 1
	}
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return exitOnEncodeErr(enc.Encode(map[string]any{
			"domain": res.Domain,
			"source": res.Source,
		}))
	}
	fmt.Println(res.Domain)
	return 0
}
