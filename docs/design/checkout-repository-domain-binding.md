# Checkout Repository Domain Binding

Status: additive implementation contract for PR #117  
Base revision: `1d098cd4923fac25ba445bbf1dedf3072f0d5058`  
Target repository: `globulario/sensei`

## 1. Problem

A Sensei store may legitimately contain more than one repository domain. Today `sensei briefing --file <path>` sends an empty domain unless the caller supplies `--domain`. The server can reject that request as ambiguous when multiple domains are loaded.

This makes a checkout-local operation depend on ambient store contents:

```text
same checkout + same file + one loaded domain   -> briefing works
same checkout + same file + two loaded domains  -> briefing becomes ambiguous
```

The repository already has enough information to own its identity, but the scaffolded `.sensei/config.yaml` does not record a repository domain, and the hooks do not establish one. Setting `AWG_DOMAIN` manually is a useful compatibility workaround, not an acceptable product contract.

## 2. Mission

Make the repository domain of a Sensei-backed checkout a durable repository-local identity used automatically by checkout-scoped commands and hooks.

The selected domain must come from the checkout or an explicit caller choice. It must never be chosen from store row order, the first loaded domain, repository basename, current directory name, or a single-candidate guess.

This contract is additive to `docs/design/semantic-protection-coverage-bootstrap.md`. The semantic-protection implementation and its hook integration must use the same canonical repository-domain resolver.

## 3. Core laws

### 3.1 Store contents do not own checkout identity

The set of domains currently loaded into Oxigraph is observation state. It is not authority for deciding which repository the current checkout represents.

A CLI request may use store metadata to diagnose a mismatch or ambiguity, but may not select the checkout domain from the store.

### 3.2 Repository domain is durable checkout configuration

Extend `.sensei/config.yaml` with an optional canonical repository identity section:

```yaml
repository:
  domain: github.com/globulario/sensei-dashboard
```

The exact Go type and YAML organization may follow existing configuration conventions. The semantic requirement is one canonical unpadded domain string.

The domain identifies the repository represented by this checkout. It is not a graph-authority verdict, task identity, branch identity, or server home domain.

### 3.3 Initialization establishes identity conservatively

`sensei init` and `sensei bootstrap` must support establishing `repository.domain` through this bounded order:

1. an explicit initialization/bootstrap domain flag;
2. an already configured canonical `repository.domain`;
3. deterministic parsing of the checkout's Git `remote.origin.url` using one shared parser;
4. otherwise, leave the repository domain explicitly unbound.

Do not infer from directory basename, organization defaults, store contents, current user, or a different remote.

The existing Git remote parser may be extracted or reused, but domain parsing must have one canonical implementation rather than diverging copies.

### 3.4 Established identity is not silently rewritten

Re-running init or bootstrap must not replace an existing repository domain merely because `remote.origin.url` changed.

When the configured domain and current origin-derived domain differ:

- preserve the configured identity;
- report a visible mismatch diagnostic;
- require an explicit rebind operation or explicit user edit under a future bounded contract;
- do not silently follow the remote.

This slice need not add a general rebind command. It must not improvise one.

### 3.5 Checkout-scoped resolution has one precedence contract

Introduce one canonical resolver used by checkout-scoped commands.

Resolution order:

1. explicit command `--domain` or equivalent typed argument;
2. canonical `.sensei/config.yaml` `repository.domain` from the resolved checkout root;
3. `SENSEI_DOMAIN` environment variable as a compatibility fallback when repository configuration is absent;
4. legacy `AWG_DOMAIN` as a deprecated compatibility fallback when neither canonical configuration nor `SENSEI_DOMAIN` is present;
5. otherwise, unresolved.

An explicit command flag is an intentional cross-domain query and may override checkout configuration for that invocation.

Repository configuration must not be silently overridden by ambient environment variables. If useful, surface a diagnostic when an ignored environment value conflicts with configured identity.

### 3.6 Unresolved identity remains honest

When a checkout-scoped file or task command cannot resolve a domain:

- it may preserve existing backward-compatible single-domain server behavior where that behavior is deterministic;
- it must never select a domain by first-row or arbitrary iteration order;
- if the server reports multi-domain ambiguity, the CLI must return an actionable error naming the remedies: configure `repository.domain`, pass `--domain`, or set `SENSEI_DOMAIN`/legacy `AWG_DOMAIN`;
- hooks must treat unresolved identity as degraded configuration rather than silently declaring that no briefing is needed.

Newly initialized or bootstrapped repositories should normally avoid this state because initialization binds the origin-derived domain when available.

### 3.7 Domain binding is request scope, not new authority

Automatically attaching the checkout domain to a briefing, preflight, protection, or task request does not:

- make graph knowledge current or authoritative;
- authorize a mutation;
- establish server repository-feedback authority;
- replace the server's explicit immutable `--repo-root` plus `--repo-domain` startup contract;
- promote candidates;
- certify repository identity beyond the configured checkout binding.

The existing server rule that repository-feedback authority is established explicitly at startup remains unchanged.

## 4. Commands and surfaces

Use the canonical resolver for checkout-scoped surfaces that already accept or require repository/domain context, including at minimum:

- `sensei briefing` for ordinary file/task graph briefings;
- `sensei preflight`;
- protection status/check commands introduced by PR #117;
- task-preparation or task-briefing wrappers where the checkout root is already known;
- Claude hook adapters installed by `sensei init`.

Other commands may adopt the resolver when they have the same checkout-scoped semantics. Do not broaden domain inference into domain-neutral or explicitly cross-repository commands without evidence.

`--domain` remains available and visible in help output.

## 5. Hook behavior

The installed hooks must not require the user to export `AWG_DOMAIN` in every shell.

Requirements:

1. resolve the project root safely;
2. invoke the typed CLI, which resolves the configured checkout domain;
3. pass the same resolved domain to protection classification and briefing-marker recording;
4. bind a briefing marker to at least canonical project root, resolved domain, and exact file path;
5. reject or invalidate a marker if the configured domain changes;
6. fail visibly on unresolved or malformed repository identity when a protected edit requires briefing;
7. keep environment variables as compatibility fallbacks only.

The shell scripts remain transport adapters and must not implement their own Git-remote parser or YAML-domain precedence table.

## 6. Configuration publication

Initialization/bootstrap configuration updates must be:

- idempotent;
- non-destructive to unrelated `.sensei/config.yaml` fields;
- schema/format validated before replacement;
- atomically published when an existing configuration file is updated;
- explicit in reports and dry-run/check output.

A user-modified valid repository domain must be preserved.

If configuration is invalid, report a typed or stable diagnostic and do not guess around it.

## 7. Required proof

Add tests proving at least:

### Domain parsing and establishment

- HTTPS, SSH/scp-style, and supported Git remote forms normalize to the same canonical domain;
- `.git` suffixes and surrounding whitespace are removed according to the canonical parser contract;
- malformed remote URLs do not produce a guessed domain;
- no origin leaves the repository explicitly unbound unless an explicit flag is supplied;
- init/bootstrap writes an origin-derived domain into new configuration;
- re-running init/bootstrap is idempotent;
- an existing configured domain is not rewritten after the Git remote changes;
- a configured/remote mismatch is reported visibly.

### Resolver precedence

- explicit `--domain` wins for one intentional invocation;
- configured `repository.domain` is selected automatically;
- `SENSEI_DOMAIN` is used only when configuration is absent;
- legacy `AWG_DOMAIN` is used only when configuration and `SENSEI_DOMAIN` are absent;
- ambient environment does not silently replace configured identity;
- malformed configured identity fails honestly.

### Multi-domain behavior

Using a test server/store with at least two domains:

- `sensei briefing --file <path>` from a configured checkout sends the configured domain and returns that repository's briefing;
- the same command does not return knowledge from the other loaded domain;
- explicit `--domain <other>` performs an intentional cross-domain query without mutating checkout configuration;
- an unbound checkout receives an actionable ambiguity error rather than arbitrary selection.

### Hook integration

- a protected file can obtain and record a briefing without exporting any domain variable;
- the marker is bound to the configured domain;
- a marker from a different domain cannot authorize the edit;
- unresolved/malformed identity does not silently bypass protected-file enforcement;
- `SENSEI_DOMAIN` and legacy `AWG_DOMAIN` compatibility paths remain covered.

## 8. Required evidence

The PR #117 implementation handoff must additionally report:

```text
Repository-domain binding:
- configured domain:
- establishment source: explicit | existing_config | git_origin | unbound
- resolver precedence tests:
- two-domain briefing proof:
- hook proof without AWG_DOMAIN:
- mismatch/non-rewrite proof:
```

The temporary foreign-repository proof required by the semantic-protection contract must load at least two domains and demonstrate that the target checkout selects its own configured domain automatically.

## 9. Non-goals

This contract must not:

- choose repository identity from store contents;
- silently rewrite configured identity when Git remotes change;
- infer identity from a directory basename;
- make an environment variable the primary persistent authority;
- change server startup repository-feedback authority;
- add cross-repository mutation authority;
- auto-promote candidates or graph knowledge;
- require a running MCP server to read checkout configuration;
- duplicate domain parsing or precedence logic across commands and hooks.

## 10. Stop conditions

Stop and post `ARCHITECT QUESTION` before coding around any of these:

- no existing configuration owner can be safely extended without destructive rewrite;
- domain parsing cannot be centralized without changing established repository identity semantics;
- a checkout-scoped command cannot receive a resolved domain without changing its authority model;
- hook marker identity cannot include domain without breaking the session-bound contract in an unbounded way;
- the server currently performs arbitrary first-domain selection and fixing it would require a separate incompatible wire change.

## 11. Acceptance criteria

This addendum is complete only when:

1. a normal configured checkout no longer needs `AWG_DOMAIN` or repeated `--domain` flags;
2. two loaded domains do not make checkout-local briefing ambiguous;
3. explicit `--domain` remains an intentional one-invocation override;
4. repository configuration outranks ambient environment;
5. unbound or malformed identity is explicit and never guessed;
6. hooks and CLI consume one canonical resolver;
7. configured identity is stable across init/bootstrap reruns and remote changes;
8. the two-domain integration proof and full PR #117 verification pass on the exact reviewed SHA.
