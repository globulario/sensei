# Sensei Deployment and Integration Strategy

Status: Proposed

## Purpose

This document defines how Sensei should be deployed and integrated across local coding agents, GitHub pull-request governance, a small GitHub App, and a future Globular-hosted service.

The strategy is intentionally incremental. The first version must deliver useful local guidance and independent GitHub verification without requiring Sensei to operate a multi-tenant hosted control plane. The later hosted version may remove local installation requirements for remote agents such as ChatGPT, Codex Cloud, and Claude API clients.

## Architectural thesis

Sensei must run in two distinct positions during a software change:

1. **Adjacent to the coding agent during implementation.**
   This instance provides task preflight, file and symbol briefings, impact analysis, edit checks, and proof requirements against the exact working repository.

2. **Independently in GitHub after the change is proposed.**
   This instance reconstructs the relevant world from exact base and head revisions and verifies the final change without trusting the agent-local runtime.

```text
local guidance
      +
independent verification
      =
credible architectural governance
```

A local Sensei instance helps the agent choose the right change. A fresh GitHub instance verifies that the resulting change actually respected the governed architecture.

Neither instance creates architectural authority merely by producing a graph or a model result. Repository-owned governed sources remain authoritative according to their declared ownership and promotion rules.

## Product sequence

Sensei should be delivered through four progressively broader surfaces:

```text
Sensei CLI and Docker appliance
    private, local, open source

Sensei GitHub Action
    deterministic independent pull-request governance

Sensei GitHub App
    installation, onboarding, identity, and native GitHub coordination

Sensei on Globular
    hosted remote MCP sessions for agents that cannot run Sensei locally
```

The first product version consists of the first three surfaces. The Globular-hosted remote service is a later product layer.

## Design laws

The deployment architecture must preserve the following laws.

### 1. Repository-owned architecture remains in the repository

Governed knowledge such as invariants, contracts, failure modes, authority declarations, required tests, decisions, and policy must remain versioned with the repository.

Typical sources include:

```text
docs/awareness/
.sensei/gate-policy.yaml
AGENTS.md
CLAUDE.md
repository decisions and evidence policies
```

The GitHub App, GitHub Action, local runtime, and hosted service may compile or evaluate these sources. They must not become hidden replacement owners of architectural truth.

### 2. The graph is a compiled projection

Oxigraph stores a queryable projection of governed sources and extracted repository structure. The presence of a triple does not make the represented proposition authoritative.

Every graph must remain bound to its exact repository identity, revision or working tree, source set, and compilation policy.

### 3. Guidance and judgment must be separated

The same local environment may guide the agent, but final pull-request verification must run in a fresh environment against exact Git revisions.

The independent verifier must not accept the local runtime's success claim as proof.

### 4. No stale or ambiguous world may produce a briefing

Every non-local briefing session must bind at least:

```text
GitHub installation identity
repository identity
base revision
result or working-tree identity when available
graph digest
policy digest
task identity or task text
Sensei version
```

If the requested repository world cannot be reconstructed consistently, Sensei must fail closed rather than serve a convenient briefing from another revision.

### 5. Provider integrations remain thin

Codex, Claude, Cursor, ChatGPT, and future agents should integrate through the stable Sensei service contract, MCP tools, and portable Sensei Architect skill.

Sensei must avoid separate architectural implementations for each provider.

## Version 1 architecture

Version 1 has three cooperating components.

```text
┌─────────────────────────────────────────────────────────────┐
│ Local developer or agent environment                        │
│                                                             │
│ exact repository checkout                                   │
│   ├── Sensei CLI or Docker appliance                        │
│   ├── Oxigraph                                              │
│   ├── governed gRPC service                                 │
│   ├── awareness-mcp                                         │
│   └── Codex, Claude Code, Cursor, or another MCP client     │
│                                                             │
│ preflight -> briefing -> edit -> diff check -> proof        │
└───────────────────────────┬─────────────────────────────────┘
                            │ push and pull request
┌───────────────────────────▼─────────────────────────────────┐
│ GitHub                                                      │
│                                                             │
│ Small Sensei GitHub App                                     │
│   ├── installation and repository selection                 │
│   ├── onboarding coordination                               │
│   ├── GitHub identity and webhook envelope                  │
│   └── native checks and links to evidence                   │
│                                                             │
│ Sensei GitHub Action                                        │
│   ├── fresh checkout of exact base and head                 │
│   ├── disposable Sensei runtime                             │
│   ├── deterministic graph build                             │
│   ├── diff gate and proof checks                            │
│   └── report, SARIF, and evidence artifacts                 │
└─────────────────────────────────────────────────────────────┘
```

## Local Sensei runtime

### Responsibility

The local runtime provides interactive architectural guidance before and during editing.

The expected loop is:

```text
receive task
-> start or discover Sensei runtime
-> bind runtime to exact checkout
-> call awareness_preflight
-> call awareness_briefing for affected files and symbols
-> inspect impact and authority boundaries
-> edit
-> call awareness_edit_check or equivalent diff check
-> derive and discharge required proof
-> prepare pull request
```

### Runtime choices

Sensei should support two equivalent local launch modes.

#### Docker appliance

Use the published Sensei appliance when Docker is available.

```text
agent workspace
  ├── repository checkout
  ├── Sensei appliance
  │     ├── Oxigraph
  │     └── awareness graph gRPC service
  └── awareness-mcp client bridge
```

Normal service mode mounts the repository read-only. Runtime state belongs in a separate Sensei data volume. Bootstrap remains an explicit write operation because it creates repository-owned files.

#### Self-contained release binaries

Use the bundled Sensei binaries and Oxigraph when Docker is unavailable or undesirable.

This fallback is important for cloud coding sandboxes where nested Docker support cannot be assumed.

The service contract and resulting graph behavior must remain equivalent across both launch modes.

### Agent integrations

The provider-specific layer should be minimal:

| Environment | Initial integration |
|---|---|
| Claude Code | `.mcp.json` plus Sensei Architect skill |
| Codex CLI or local Codex | MCP configuration plus Sensei Architect skill |
| Cursor | MCP configuration plus repository rule or skill |
| Other MCP-compatible agent | Standard MCP configuration plus portable skill |

The local agent receives only governed Sensei tools such as preflight, briefing, impact, query, edit check, investigation, and proposal surfaces. Direct Oxigraph access is not an agent contract.

## GitHub Action

### Responsibility

The GitHub Action is the independent verifier. It runs on a fresh GitHub runner and evaluates the exact proposed change.

The action should:

1. Check out complete base and head history required for a verified diff.
2. Install the pinned Sensei release or use the pinned appliance image.
3. Validate repository-owned awareness sources.
4. Start Oxigraph and the governed Sensei service.
5. Compile the graph deterministically.
6. Evaluate the exact pull-request diff.
7. Emit a human-readable job summary.
8. Emit machine-readable findings and SARIF when enabled.
9. Preserve evidence digests and relevant artifacts.
10. Fail closed in enforcement mode when the diff or bindings cannot be verified.

### Modes

```text
advisory
    reports findings without blocking

enforce
    blocks on governed findings and unverifiable state
```

The action must remain usable without the GitHub App. Teams that prefer a fully transparent repository workflow should be able to install only the action.

## Small GitHub App

### Responsibility

The first GitHub App is an installer and coordinator. It is not a persistent hosted architectural brain.

Its bounded responsibilities are:

- install Sensei on selected repositories;
- identify repositories that have not been initialized;
- coordinate a reviewable onboarding flow;
- install or propose the Sensei GitHub Action workflow;
- receive installation and pull-request webhooks;
- create or update native Sensei check runs;
- associate GitHub identities with review and approval events;
- link check results to GitHub Action evidence;
- present clear remediation and initialization actions.

### Explicit non-responsibilities in version 1

The App does not:

- host a long-lived Oxigraph database for each repository;
- replace repository-owned awareness sources;
- silently promote inferred candidates to canonical knowledge;
- mutate source code as part of normal review;
- certify a result without an independently reproduced action run;
- expose a public remote MCP service;
- operate a multi-tenant source-code analysis platform.

### Onboarding flow

A repository should be installable without first installing Sensei locally.

```text
install Sensei GitHub App
-> select repositories
-> App detects missing Sensei initialization
-> user requests Initialize Sensei
-> bounded GitHub workflow runs bootstrap
-> workflow creates candidate repository files
-> App opens an onboarding pull request
-> maintainers review governed sources and policies
-> merge activates normal pull-request governance
```

Generated architectural material must be presented as reviewable candidates. Inference alone does not grant authority.

### Permission strategy

The initial review mode should request the smallest practical permission set:

```text
Metadata: read
Contents: read
Pull requests: read
Checks: write
Actions: read
```

Creating onboarding branches and pull requests requires broader permissions:

```text
Contents: write
Pull requests: write
```

These write permissions should be requested or activated only for an explicit initialization capability. A repository may trust Sensei to review changes before trusting it to create branches.

### Relationship to the Action

The App should delegate analysis to the existing Action or to the same underlying runner implementation.

```text
GitHub App receives event
-> identifies exact repository and revision
-> starts or observes Sensei Action
-> Action performs disposable independent analysis
-> App publishes native check state and links evidence
```

There must not be one architectural evaluator in the App and another in the Action. Both surfaces must use the same Sensei engine and contracts.

## Future Globular-hosted service

The later hosted product removes the requirement that remote agents run Sensei locally.

```text
OpenAI, Anthropic, or another remote agent
                 │
                 │ authenticated HTTPS MCP
                 ▼
┌──────────────────────────────────────────────┐
│ Globular-hosted Sensei MCP gateway           │
│ authentication, authorization, session bind │
└─────────────────────┬────────────────────────┘
                      │ private service network
┌─────────────────────▼────────────────────────┐
│ Isolated revision-bound Sensei worker        │
│                                              │
│ exact repository checkout                    │
│ Sensei appliance or binaries                 │
│ governed gRPC service                        │
│ private Oxigraph instance                    │
│ immutable graph and evidence artifacts       │
└──────────────────────────────────────────────┘
```

OpenAI and Anthropic do not need to receive, host, or control Oxigraph or Sensei's internal gRPC service. They call the externally exposed MCP contract. Sensei and Globular retain control of the internal runtime.

### Hosted session lifecycle

A remote request should follow this lifecycle:

```text
authenticate user or agent
-> resolve GitHub App installation authorization
-> bind repository and exact revision
-> create or reuse an isolated immutable graph cache
-> start a repository-scoped Sensei worker
-> answer preflight, briefing, impact, or diff-check request
-> return exact revision and digest bindings
-> preserve required evidence
-> stop the worker when the session expires
```

A hosted service must never choose a graph merely because it is the newest graph associated with a repository name.

### Public and private boundaries

The externally reachable surface is:

```text
HTTPS MCP
```

The private internal surface remains:

```text
MCP gateway
-> Sensei session broker
-> governed gRPC service
-> Oxigraph
```

Repository checkouts, graph state, artifacts, and logs must be isolated by installation, repository, revision, and session.

## Authority boundaries

The four deployment surfaces have different owners.

```text
Repository
    owns architectural truth

Local Sensei runtime
    owns task-local guidance derived from the exact checkout

GitHub Action
    owns independent reproducible pull-request evaluation

GitHub App
    owns installation, GitHub identity envelopes, and coordination

Globular-hosted service
    owns remote session execution and isolation
```

None of these roles imply authority to rewrite repository knowledge without the repository's governed promotion path.

## Implementation phases

### Phase A: local integration hardening

Deliver:

- one documented local launch command;
- Docker and self-contained binary parity;
- automatic MCP wiring for supported agents;
- exact checkout and graph binding surfaced in metadata;
- proof that preflight occurs before the first edit in an agent workflow;
- proof that final diff checking occurs before completion is claimed.

Success criterion:

> A developer can clone an initialized repository, start Sensei, and have Claude Code or Codex request a preflight and exact file or symbol briefing before editing.

### Phase B: GitHub Action productization

Deliver:

- pinned release usage;
- advisory and enforcement modes;
- exact base and head verification;
- deterministic evidence artifacts;
- clear job summary and SARIF;
- documented installation workflow;
- external repository case study.

Success criterion:

> An unrelated repository can add one workflow file and receive reproducible Sensei review without installing Sensei on a developer machine.

### Phase C: small GitHub App

Deliver:

- GitHub App registration and permission model;
- installation and repository-selection handling;
- webhook signature validation;
- installation-token lifecycle;
- onboarding status check;
- explicit initialization workflow;
- onboarding pull request creation;
- native check-run coordination with the Sensei Action;
- uninstall and data-deletion behavior.

Success criterion:

> A repository owner can install the App, initialize Sensei through a reviewable pull request, and receive native pull-request checks without operating hosted Sensei infrastructure.

### Phase D: Globular remote MCP

Deliver later:

- authenticated remote MCP gateway;
- GitHub installation authorization;
- isolated revision-bound workers;
- immutable graph artifact caching;
- remote preflight and briefing tools;
- provider-neutral OAuth or token flow;
- usage accounting and operational limits;
- private repository isolation proof;
- independent GitHub verification of remotely produced changes.

Success criterion:

> A remote ChatGPT, Codex, Claude, or compatible agent can request a Sensei preflight and briefing for an authorized exact repository revision without requiring a local Sensei installation.

## First end-to-end proof

Before expanding the GitHub App or building the hosted service, Sensei should demonstrate one complete external workflow:

1. Select an unrelated repository with repository-owned Sensei awareness sources.
2. Start Sensei beside a real Codex or Claude Code task environment.
3. Record that the agent called preflight before its first edit.
4. Record that the agent requested briefings for the actual files and symbols it changed.
5. Record that the agent submitted its final diff to Sensei before claiming completion.
6. Open a pull request.
7. Run a fresh Sensei GitHub Action against the exact base and head.
8. Compare local guidance, final diff evidence, and independent GitHub findings.
9. Preserve the result as a reproducible case study.

This proof establishes the core product promise:

> Sensei guides the agent while it works and independently verifies what the agent finally proposes.

## Risks and mitigations

### Risk: the GitHub App becomes a hidden authority source

Mitigation: keep governed architecture in the repository and route initialization through reviewable pull requests.

### Risk: local and GitHub behavior diverge

Mitigation: use one Sensei engine, one graph compiler, one policy model, and common contract tests across CLI, appliance, Action, and App workers.

### Risk: stale graph served to an agent

Mitigation: bind every graph and response to exact repository, revision, source, policy, and Sensei version digests. Refuse ambiguous sessions.

### Risk: cloud-agent environment does not support Docker

Mitigation: preserve the self-contained binary launch path and test parity with the appliance.

### Risk: the App requests excessive permissions

Mitigation: separate read-and-check operation from explicit onboarding write capability.

### Risk: hosted private source crosses tenant boundaries

Mitigation: use installation-scoped credentials, isolated workers, repository-scoped storage, immutable cache keys, bounded logs, and explicit deletion contracts.

### Risk: an agent treats a briefing as permission

Mitigation: every briefing must distinguish descriptive evidence, governed authority, unresolved knowledge, and required approval. Briefing availability does not authorize mutation.

## Decision summary

The correct initial strategy is:

```text
local Sensei installation
    for interactive preflight and briefing

GitHub Action
    for fresh independent pull-request verification

small GitHub App
    for installation, onboarding, identity, and native coordination
```

The later strategy is:

```text
Globular-hosted remote MCP
    for OpenAI, Anthropic, and other agents that cannot run Sensei locally
```

Sensei should run beside the agent when guidance is needed and independently in GitHub when proof is required. The App coordinates these worlds. The repository remains the owner of architectural truth.