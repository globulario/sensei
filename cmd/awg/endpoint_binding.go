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
	"path/filepath"
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
	return endpointDisagreement(root, flagName, configKey, configured, resolved)
}

// endpointDisagreement is the comparison itself, without the command line.
//
// Split out so the verdict can be formed where the flag set is visible and
// consumed where the command knows whether it will touch an endpoint at all
// (#377). The operator-named-it-at-the-point-of-use rule stays above, in the
// one function that can see the flags: this half only knows what the config
// says and what was resolved.
func endpointDisagreement(root, flagName, configKey, configured, resolved string) error {
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

// endpointAuthority names WHICH authority decided an endpoint.
//
// A closed set, read by membership. The human-readable source string beside it exists for
// operators and is duplicated across functions; deciding behaviour by matching that prose is how a
// closed vocabulary silently fails open when a new source is added and one reader is not updated.
// Callers that need to know who outranked whom compare these values.
//
// The order is the precedence order, so `>` on the constants is `outranks`.
type endpointAuthority int

const (
	endpointAuthorityUnset endpointAuthority = iota
	// endpointAuthorityBuiltInDefault is netcfg: nobody chose this.
	endpointAuthorityBuiltInDefault
	// endpointAuthorityProjectConfig is .sensei/config.yaml -- what an operator edits alongside
	// the code, and therefore INSIDE the thing being vouched for.
	endpointAuthorityProjectConfig
	// endpointAuthorityDomainRegistry is ~/.sensei/domains.yaml -- operator-controlled and
	// OUTSIDE any published repository, so a repository cannot redirect its own graph. This is
	// why it outranks the project config.
	endpointAuthorityDomainRegistry
	// endpointAuthorityOperatorFlag is the operator naming an endpoint at the point of use,
	// visible in the command they ran. Law 14's diagnostic/maintenance authority.
	endpointAuthorityOperatorFlag
)

// outranksProjectConfig reports whether an authority sits ABOVE the repository's own
// configuration, and therefore may not be vetoed by it.
func (a endpointAuthority) outranksProjectConfig() bool { return a > endpointAuthorityProjectConfig }

func (a endpointAuthority) String() string {
	switch a {
	case endpointAuthorityOperatorFlag:
		return "an explicit operator override"
	case endpointAuthorityDomainRegistry:
		return "the domain registry"
	case endpointAuthorityProjectConfig:
		return "this project's configuration"
	case endpointAuthorityBuiltInDefault:
		return "the built-in default"
	}
	return "an unrecorded source"
}

// endpointDisagreementNotice reports that endpoint ownership followed a higher authority than the
// repository's own configuration, so the operator is told rather than refused.
//
// THIS REPLACED A REFUSAL, and the reason is the precedence contract above. The refusal compared
// the configured address against the resolved one, and the only case in which it could produce a
// refusal was a registry-declared endpoint the project config does not name -- which is exactly the
// case the registry exists to permit. A repository-local file must not veto the canonical owner, or
// the registry stops being the authority that "cannot be redirected by the repository" and becomes
// a suggestion the repository can decline.
//
// #212's actual failure -- a command reporting a verdict from a server the operator did not name --
// is now unreachable: the owner reads the project configuration itself as precedence 3, and no
// subject carries an endpoint default of its own (issue_212_reachability_test.go).
//
// Takes semantic state rather than inferring it: the endpoint in use, WHO decided it, what the
// repository configured if anything, and whether the operator overrode it explicitly.
//
// Returns "" when there is nothing to report.
func endpointDisagreementNotice(resolved string, authority endpointAuthority, configured string, overridden bool) string {
	resolved, configured = strings.TrimSpace(resolved), strings.TrimSpace(configured)
	// An explicit override is already reported as non-canonical by nonCanonicalReaderNotice, which
	// says the stronger thing: nothing has verified that this endpoint serves the domain's graph.
	// Saying it twice, in weaker words, would train an operator to skim both.
	if overridden || authority == endpointAuthorityOperatorFlag {
		return ""
	}
	// No repository-local configuration means no disagreement to report.
	if configured == "" {
		return ""
	}
	if configured == resolved {
		return ""
	}
	return "sensei: reading " + resolved + " because " + authority.String() +
		" names it for this domain. This project's configuration states " + configured +
		", which is therefore stale or applies to another endpoint; the owner's answer is the one " +
		"in use. Nothing was read from " + configured + "."
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
func resolveDomainServiceAddr(fs *flag.FlagSet, projectRoot, domain, flagValue, registryPath string) (addr, source string, authority endpointAuthority) {
	if flagPassed(fs, "addr") {
		return flagValue, "named on the command line", endpointAuthorityOperatorFlag
	}
	if strings.TrimSpace(domain) != "" && strings.TrimSpace(registryPath) != "" {
		if reg, err := LoadDomainRegistry(registryPath); err == nil && reg != nil {
			if rd, ok := reg.Domains[strings.TrimSpace(domain)]; ok {
				if a := strings.TrimSpace(rd.ServiceAddr); a != "" {
					return a, "the domain registry (" + registryPath + ")", endpointAuthorityDomainRegistry
				}
			}
		}
	}
	if cfg, err := loadEndpointConfig(projectRoot); err == nil {
		if a := cfg.configuredServerAddr(); a != "" {
			return a, "this project's configuration", endpointAuthorityProjectConfig
		}
	}
	return flagValue, "the built-in default", endpointAuthorityBuiltInDefault
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
	// Authority is WHO decided Addr, as a closed set rather than as prose. A caller that must
	// know whether the repository's own configuration may object reads this, never Source.
	Authority endpointAuthority
}

// resolveGraphReader is the one resolution every production reader uses.
func resolveGraphReader(fs *flag.FlagSet, projectRoot, domain, flagValue, registryPath string) graphReader {
	r := graphReader{Domain: strings.TrimSpace(domain)}
	// An explicitly named endpoint wins, and is recorded as an override. The flag's
	// default is deliberately EMPTY in every reader, so a non-empty value is always an
	// operator naming it rather than a port the command chose for them.
	if strings.TrimSpace(flagValue) != "" || flagPassed(fs, "addr") {
		r.Addr, r.Source, r.Overridden = strings.TrimSpace(flagValue), "named on the command line", true
		r.Authority = endpointAuthorityOperatorFlag
		if r.Addr == "" {
			r.Addr = defaultServiceAddr()
		}
		r.DeclaredGeneration = declaredActiveGeneration(registryPath, r.Domain)
		return r
	}
	r.Addr, r.Source, r.Authority = resolveDomainServiceAddr(fs, projectRoot, r.Domain, "", registryPath)
	if strings.TrimSpace(r.Addr) == "" {
		r.Addr, r.Source, r.Authority = defaultServiceAddr(), "the built-in default", endpointAuthorityBuiltInDefault
	}
	r.DeclaredGeneration = declaredActiveGeneration(registryPath, r.Domain)
	return r
}

// productionReaderFor is the ONE line a production graph-reading command writes to obtain its
// reader, and the reason there is a helper rather than seventeen copies.
//
// It is not new design. cmd_briefing.go on this tree already says "Every other reader resolves the
// root by walking up, via productionReaderFor" -- a reference to a helper that did not exist yet,
// left by the change that introduced the owner and migrated only two commands. This completes that
// intent.
//
// THE PROJECT ROOT IS ALWAYS WALKED UP TO -- from the command's target checkout when it names one,
// and from the working directory otherwise. Both halves were paid for:
//
//   - briefing hit the first: passing `--repo` (default ".") straight through as projectRoot made
//     the SAME command resolve a different endpoint from a subdirectory than from the root, because
//     ./.sensei/config.yaml was simply not found. The defect was passing an UNWALKED path, not
//     consulting the flag.
//   - blind review of the first version of this helper hit the second: hardcoding the working
//     directory breaks OUT-OF-TREE execution. `sensei preflight --repo /path/to/target` run from
//     /tmp would not find the target's configuration and would fall through to the built-in
//     default -- dialling the wrong graph while looking correctly resolved.
//
// So the rule subsumes both: walk up from the hint when there is one. A command's target-repo flag
// answers "which checkout do I operate on"; walking up from it answers "which project's
// configuration names that checkout's graph endpoint". They are different questions, and the second
// is derived from the first rather than ignoring it.
//
// Resolution fails open to the starting directory. That is acceptable for this tier specifically:
// it reads a configuration file that may not exist, and the fallback is the same built-in default
// the owner would have chosen anyway. Where a root must be PROVEN, callers ask looksLikeProjectRoot.
//
// Callers assign the result over their own flag variable:
//
//	reader := productionReaderFor(fs, *domain, *addr)
//	*addr = reader.Addr
//
// which leaves every later use of *addr untouched and makes the canonically resolved endpoint the
// only value any of them can dial. The alternative -- rewriting each command's plumbing to carry a
// graphReader -- would be a far larger diff for the same guarantee, and this family is scoped to
// endpoint ownership.
func productionReaderFor(fs *flag.FlagSet, rootHint, domain, addrFlag string) graphReader {
	r := resolveGraphReader(fs, graphConfigRoot(rootHint), domain, addrFlag, DefaultDomainRegistryPath())
	if notice := nonCanonicalReaderNotice(r); notice != "" {
		fmt.Fprintln(os.Stderr, notice)
	}
	return r
}

// graphConfigRoot finds the project whose configuration names the graph endpoint.
//
// It WALKS UP from hint when a command names a target checkout, and from the working directory when
// it does not. resolveProjectRoot cannot be used for the first case: given a non-empty argument it
// returns filepath.Abs of it without walking, which is exactly how `--repo .` from a subdirectory
// resolved the subdirectory and found no configuration there.
func graphConfigRoot(hint string) string {
	hint = strings.TrimSpace(hint)
	if hint == "" {
		root, _ := resolveProjectRoot("")
		return root
	}
	start, err := filepath.Abs(hint)
	if err != nil {
		return hint
	}
	for dir := start; ; {
		if looksLikeProjectRoot(dir) {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return start
		}
		dir = parent
	}
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

// governedRepoRoot normalises a repository/root hint so that "omitted" and "the operator chose this
// path" stop being the same value.
//
//	empty     no operator-selected root: discover the governed repository by walking upward
//	non-empty the operator selected exactly that path, and it is used verbatim
//
// WHY THIS EXISTS. resolveProjectRoot walks up only for an empty argument, and every graph-reading
// subject's repository flag defaulted to ".", which is never empty. So the walk-up branch was
// unreachable by default, and a command run from a subdirectory looked for the governed domain in
// whatever directory the operator happened to be standing in. It did not fail: it found no domain,
// which silently removed the registry from endpoint resolution and let the weaker project-config
// authority answer -- at the same address, so nothing looked wrong. The endpoint path had already
// been given a walking wrapper in graphConfigRoot; the domain path never was.
//
// The result is a CONCRETE root rather than a sentinel, because callers use the value directly --
// os.ReadFile(root + "/go.mod"), git -C root, filepath.Abs(root) -- and an empty string silently
// becomes "/" or the process working directory in those. One normalisation, at the point the flag is
// parsed, so every later use of the value means the same repository.
//
// This deliberately does NOT change resolveProjectRoot, which has callers far beyond the graph
// readers.
func governedRepoRoot(hint string) string {
	if strings.TrimSpace(hint) == "" {
		return graphConfigRoot("")
	}
	return hint
}
