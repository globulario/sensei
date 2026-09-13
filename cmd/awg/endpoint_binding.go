// SPDX-License-Identifier: AGPL-3.0-only

// endpoint_binding.go closes the disagreement issue #212 names: a project
// config file that states where a command reads or writes, and a command
// that decides from a flag default instead and never consults it.
//
// The cost it records is concrete: a throwaway domain published into a
// shared services store because an edited store_url was believed to have
// redirected the build. Seven aw:repo tags landed on subjects another
// domain owned, the global marker rotated, and that domain lost its proof
// — while the build reported success and the closure report said PROVEN.
// The config had been edited first, precisely to prevent it.
//
// The rule here does not change where anything resolves to. It refuses to
// let a configured endpoint and an unstated resolved endpoint disagree in
// silence:
//
//   - a flag given on the command line wins, always. That is the operator
//     naming the endpoint at the point of use, not a silent disagreement,
//     and it keeps every existing scripted invocation working;
//   - no flag, and the configured value equals the resolved one: proceed;
//   - no flag, and they differ: refuse, naming the config path, the
//     configured value and the resolved value, BEFORE the command reaches
//     the endpoint. For a store load that is before a single triple
//     changes, beside publication's existing pre-mutation admission gate;
//   - no configured value, or no config file: today's behavior, unchanged.
//
// A malformed config is an error, never a skipped tier — the same law
// repo_domain_binding.go applies to checkout identity: configuration that
// fails to parse must fail visibly, not be silently worked around by
// falling through to a guessed value.
package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/globulario/sensei/golang/statedir"
	"gopkg.in/yaml.v3"
)

// endpointConfig is the store: and server: sections of .sensei/config.yaml.
// Only the endpoint fields are modeled; every other section is ignored, so
// this read never constrains what else the file may carry.
type endpointConfig struct {
	Store struct {
		QueryURL string `yaml:"query_url"`
		StoreURL string `yaml:"store_url"`
	} `yaml:"store"`
	Server struct {
		Addr string `yaml:"addr"`
	} `yaml:"server"`
}

// endpointConfigPath returns the resolved .sensei/config.yaml (or legacy
// .awg/config.yaml) path for root.
func endpointConfigPath(root string) string {
	return statedir.Path(root, "config.yaml")
}

// loadEndpointConfig reads root's endpoint configuration. A missing config
// file is not an error — it returns a zero-value config, the same as an
// existing config that states no endpoint.
func loadEndpointConfig(root string) (endpointConfig, error) {
	path := endpointConfigPath(root)
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return endpointConfig{}, nil
		}
		return endpointConfig{}, fmt.Errorf("read %s: %w", path, err)
	}
	var cfg endpointConfig
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return endpointConfig{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return cfg, nil
}

// configuredStoreURL / configuredServerAddr return the endpoint root's
// config states, or "" when it states none.
func (c endpointConfig) configuredStoreURL() string   { return strings.TrimSpace(c.Store.StoreURL) }
func (c endpointConfig) configuredServerAddr() string { return strings.TrimSpace(c.Server.Addr) }

// requireEndpointAgreement refuses when root's config names an endpoint,
// the operator did not name one on the command line, and the value the
// command resolved differs from the configured one. flagName is the flag
// as an operator types it (e.g. "-store-url"); configKey is the config
// path it corresponds to (e.g. "store.store_url").
//
// Returns nil when the command may proceed. The returned error is already
// phrased for stderr and names both values, so a caller prints it as-is.
func requireEndpointAgreement(fs *flag.FlagSet, root, flagName, configKey, configured, resolved string) error {
	if flagPassed(fs, strings.TrimPrefix(flagName, "-")) {
		return nil
	}
	configured = strings.TrimSpace(configured)
	if configured == "" {
		return nil
	}
	if configured == strings.TrimSpace(resolved) {
		return nil
	}
	return fmt.Errorf(`refusing to act on an endpoint the project config does not name.

  %s states  %s: %s
  resolved endpoint  %s

Nothing has been read from or written to either endpoint. The configured
value is the one an operator reads before running the command, so a
resolved endpoint that differs from it is never assumed to be intended.

Resolve it one of two ways:
  - use the configured endpoint, by removing whatever redirects it
    (a SENSEI_* environment variable in this shell, most often); or
  - name the endpoint you mean explicitly: %s <endpoint>`,
		endpointConfigPath(root), configKey, configured, resolved, flagName)
}

// resolveServiceAddr is the ONE place the awareness endpoint precedence lives.
//
// The rule was already established by endpoint_binding.go and repo_domain_binding.go
// and already written down in cmd_edit_brief.go: an explicit flag is the operator
// naming the endpoint at the point of use and always wins; otherwise the project's
// own configuration decides; only then the built-in default.
//
// It is extracted here because `metadata` did not follow it. On 2026-09-13 that
// command -- the one whose job is to report canonical graph state -- dialled
// netcfg's DefaultServicePort 10120 and failed, while two healthy awareness services
// were running on :10121 and :10122 and a production reader reached them from its
// project configuration. The command meant to describe the canonical graph was the
// only participant that could not see any graph.
//
// G5 requires that this report come from the same authority production readers use.
// One function, so a fourth command cannot invent a fourth precedence.
//
// An unreadable project root falls back to the built-in default rather than failing:
// the caller's job is to report state, and refusing to resolve an endpoint would
// replace a readable answer with nothing.
func resolveServiceAddr(fs *flag.FlagSet, projectRoot, flagValue string) string {
	if flagPassed(fs, "addr") {
		return flagValue
	}
	if cfg, err := loadEndpointConfig(projectRoot); err == nil {
		if a := cfg.configuredServerAddr(); a != "" {
			return a
		}
	}
	return flagValue
}

// serviceAddrSource says WHERE the resolved endpoint came from, so a report can
// state it.
//
// Two runs of the same command from different directories can legitimately reach
// different graphs, print different digests and different triple counts, and be
// equally correct. Without naming the source, the output gives no way to tell that
// apart from a graph having changed -- which is the confusion this front exists to
// end.
func serviceAddrSource(fs *flag.FlagSet, projectRoot string) string {
	if flagPassed(fs, "addr") {
		return "named on the command line"
	}
	if cfg, err := loadEndpointConfig(projectRoot); err == nil {
		if cfg.configuredServerAddr() != "" {
			return "this project's configuration"
		}
	}
	return "the built-in default"
}

// markerAgreement reports whether the marker certifies the graph actually served.
//
// Law 10: a marker cannot certify a different generation or store than the one being
// served. Nothing compared them before, so a divergence was invisible -- on
// 2026-09-13 a disposable generation wrote digest 230a74f6.../35,255 while the live
// marker still read c0b660fc.../35,268, and no command would have said so.
//
// Both halves are named in every outcome, because "they disagree" without the two
// values is a statement a reader cannot act on. An absent marker digest is reported
// as unverifiable rather than as agreement: under law 13 a triple count is evidence,
// never identity, so a matching count cannot stand in for a missing digest.
func markerAgreement(liveDigest string, liveTriples int, markerDigest string, markerTriples int) string {
	ld, md := strings.TrimSpace(liveDigest), strings.TrimSpace(markerDigest)
	switch {
	case md == "":
		return "cannot be verified: the marker states no digest, and a triple count is evidence rather than identity"
	case ld == "":
		return "cannot be verified: the served graph states no digest to compare with the marker's " + md
	case ld == md && liveTriples == markerTriples:
		return "agrees with the served graph (" + ld + ", " + strconv.Itoa(liveTriples) + " triples)"
	default:
		return "DOES NOT match the served graph: marker " + md + " / " + strconv.Itoa(markerTriples) +
			" triples, served " + ld + " / " + strconv.Itoa(liveTriples) + " triples"
	}
}

// resolveDomainServiceAddr resolves domain -> endpoint through the registry (G2).
//
// Precedence, highest first:
//
//  1. an explicit flag      the operator naming it at the point of use, visible
//     in the command they ran
//  2. the domain registry   operator-controlled and OUTSIDE any published
//     repository, so a repository cannot redirect its own
//     graph
//  3. the project config    what an operator edits alongside the code
//  4. the built-in default  netcfg
//
// The registry sits above the project because of why it exists: it is already
// trusted not to live inside the thing it vouches for. It is also INERT until an
// operator declares a service_addr, so adding this cannot move any caller's
// endpoint today.
//
// Falls through on every absence -- unregistered domain, unreadable registry, no
// declared endpoint -- because this reports an endpoint rather than gating access.
// A resolver that refused here would turn a missing convenience into an outage.
func resolveDomainServiceAddr(fs *flag.FlagSet, projectRoot, domain, flagValue, registryPath string) (addr, source string) {
	if flagPassed(fs, "addr") {
		return flagValue, "named on the command line"
	}
	if strings.TrimSpace(domain) != "" && strings.TrimSpace(registryPath) != "" {
		if reg, err := LoadDomainRegistry(registryPath); err == nil && reg != nil {
			if rd, ok := reg.Domains[strings.TrimSpace(domain)]; ok {
				if a := strings.TrimSpace(rd.ServiceAddr); a != "" {
					return a, "the domain registry (" + registryPath + ")"
				}
			}
		}
	}
	if cfg, err := loadEndpointConfig(projectRoot); err == nil {
		if a := cfg.configuredServerAddr(); a != "" {
			return a, "this project's configuration"
		}
	}
	return flagValue, "the built-in default"
}

// nonCanonicalStoreURLNotice makes a raw --store-url override visible.
//
// Law 14 of the graph-identity front: a raw store URL is test/dev/maintenance
// authority, not normal production discovery; if it is retained it must be
// explicit AND visibly non-canonical. Today it is explicit and SILENT —
// requireEndpointAgreement returns nil the moment the flag is passed, and `import`
// cannot load a slice without passing it, so on that path the endpoint custody
// guard (#212) can never fire. The flag required to publish is the flag that
// disables the check.
//
// Three stores answer healthily on this machine and hold different graphs, so the
// cost of silence is publishing into a graph nobody asked for. This does not take
// the escape away — an operator with a reason keeps it — it stops the escape from
// looking like an ordinary choice between equals.
//
// Returns "" when the endpoint the config names is the one being used.
func nonCanonicalStoreURLNotice(configured, resolved string) string {
	configured, resolved = strings.TrimSpace(configured), strings.TrimSpace(resolved)
	if configured != "" && configured == resolved {
		return ""
	}
	const tail = " This is a non-canonical override: a raw store URL is maintenance authority, " +
		"not production discovery, and nothing has verified that this endpoint serves the graph you mean."
	if configured == "" {
		return "sensei: the project config names no store, so it cannot corroborate the endpoint " +
			resolved + "." + tail
	}
	return "sensei: publishing to " + resolved + " while the project config states " + configured + "." + tail
}

// requireStoreURLAgreement is requireEndpointAgreement for a command's
// -store-url, the flag that decides which store a load mutates.
func requireStoreURLAgreement(fs *flag.FlagSet, root, resolved string) error {
	cfg, err := loadEndpointConfig(root)
	if err != nil {
		return fmt.Errorf("endpoint configuration is malformed: %w", err)
	}
	return requireEndpointAgreement(fs, root, "-store-url", "store.store_url", cfg.configuredStoreURL(), resolved)
}

// requireServerAddrAgreement is requireEndpointAgreement for a command's
// -addr, the flag that decides which server's authority is asserted.
func requireServerAddrAgreement(fs *flag.FlagSet, root, resolved string) error {
	cfg, err := loadEndpointConfig(root)
	if err != nil {
		return fmt.Errorf("endpoint configuration is malformed: %w", err)
	}
	return requireEndpointAgreement(fs, root, "-addr", "server.addr", cfg.configuredServerAddr(), resolved)
}

// --- LAW 3: ONE OWNER FOR EVERY PRODUCTION READER ---------------------------------
//
// "All production readers resolve graph identity through one owner. They must not
// independently choose an Oxigraph port."
//
// That was false. Eighteen commands in this package declared
// `fs.String("addr", defaultServiceAddr(), …)` and dialled it; exactly one, `metadata`,
// resolved through resolveDomainServiceAddr. `briefing` was the sharpest case: it
// already resolved the governed domain and then ignored it, so in a repository whose
// config states :10122 it failed with "dial tcp 127.0.0.1:10120: connection refused"
// while the graph it should read was running the whole time.
//
// The repair is not a different default port. A reader now states the domain it reads
// for and RECEIVES the endpoint, its provenance, and the generation the registry
// declares ACTIVE. The port stops being something a reader picks, which is the only
// form of this law that can hold: as long as a command owns a default port, it owns a
// graph choice.

// graphReader is what a production reader resolves BEFORE it dials: where to read, why
// that endpoint, which domain it is reading for, and which generation is declared ACTIVE.
//
// Resolution is complete before any connection is attempted, deliberately. A resolver
// that probed endpoints would be selecting a graph by liveness, and "it answered" is
// exactly the evidence law 2 exists to reject.
type graphReader struct {
	Addr   string
	Source string
	Domain string
	// DeclaredGeneration is the registry's ACTIVE generation for Domain, or "" when
	// none is declared.
	DeclaredGeneration string
	// Overridden records that an operator named the endpoint at the point of use, so a
	// caller can report the answer as non-canonical (law 14's rule for raw overrides,
	// applied to the read side).
	Overridden bool
}

// resolveGraphReader is the one resolution every production reader uses.
func resolveGraphReader(fs *flag.FlagSet, projectRoot, domain, flagValue, registryPath string) graphReader {
	r := graphReader{Domain: strings.TrimSpace(domain)}
	// An explicitly named endpoint wins, and is recorded as an override. The flag's
	// default is deliberately EMPTY in every reader, so a non-empty value is always an
	// operator naming it rather than a port the command chose for them.
	if strings.TrimSpace(flagValue) != "" || flagPassed(fs, "addr") {
		r.Addr, r.Source, r.Overridden = strings.TrimSpace(flagValue), "named on the command line", true
		if r.Addr == "" {
			r.Addr = defaultServiceAddr()
		}
		r.DeclaredGeneration = declaredActiveGeneration(registryPath, r.Domain)
		return r
	}
	r.Addr, r.Source = resolveDomainServiceAddr(fs, projectRoot, r.Domain, "", registryPath)
	if strings.TrimSpace(r.Addr) == "" {
		r.Addr, r.Source = defaultServiceAddr(), "the built-in default"
	}
	r.DeclaredGeneration = declaredActiveGeneration(registryPath, r.Domain)
	return r
}

// verifyServed refuses a response answered by a generation other than the one declared
// ACTIVE for this reader's domain.
//
// Inert when nothing is declared, for the same reason the pointer itself is: an absent
// declaration contradicts nothing. When a declaration exists, an answer from another
// generation is refused rather than reported -- a well-formed briefing from rules this
// domain does not declare active is worse than no briefing, because it carries the
// authority of one graph and the content of another.
func (r graphReader) verifyServed(servedDigest string) error {
	if strings.TrimSpace(r.DeclaredGeneration) == "" {
		return nil
	}
	return verifyActiveGeneration(r.Domain, r.DeclaredGeneration, servedDigest)
}

// nonCanonicalReaderNotice states that a reader's endpoint was named rather than
// resolved, so an override can never pass for canonical discovery.
func nonCanonicalReaderNotice(r graphReader) string {
	if !r.Overridden {
		return ""
	}
	return "sensei: reading " + r.Addr + " because it was named on the command line. " +
		"This is a non-canonical override: nothing has verified that this endpoint serves the graph for " +
		domainOrAny(r.Domain) + "."
}

func domainOrAny(domain string) string {
	if strings.TrimSpace(domain) == "" {
		return "any particular domain"
	}
	return domain
}

// productionReaderFor is the thin wiring every graph-reading command uses, so the
// migration is one line per command rather than sixteen partial re-implementations.
//
// It resolves through the owner and announces a non-canonical override. Callers assign
// the result's Addr over their own flag variable, which leaves every existing use site
// untouched and makes the canonical endpoint the only value any of them can dial:
//
//	reader := productionReaderFor(fs, *domain, *addr)
//	*addr = reader.Addr
//
// The project root is resolved here rather than plumbed through sixteen signatures.
// resolveProjectRoot fails open to the working directory, which is acceptable for the
// CONFIG tier specifically — reading a config that may not exist — and is what metadata
// already did. Where a root must be proven (the graph marker), looksLikeProjectRoot is
// asked instead.
func productionReaderFor(fs *flag.FlagSet, domain, addrFlag string) graphReader {
	root, _ := resolveProjectRoot("")
	r := resolveGraphReader(fs, root, domain, addrFlag, DefaultDomainRegistryPath())
	if notice := nonCanonicalReaderNotice(r); notice != "" {
		fmt.Fprintln(os.Stderr, notice)
	}
	return r
}
