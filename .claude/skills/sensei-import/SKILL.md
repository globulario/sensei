---
name: sensei-import
description: Use to import, onboard, bootstrap, or refresh a repository into Sensei, including project reconstruction, domain-scoped Oxigraph loading, and live verification. Trigger when the user says "import <repo>", gives a git clone URL, asks to learn/bootstrap a foreign codebase, or expects imported claims and knowledge in the live graph. Never auto-governs candidates, never lets a foreign repo's rules leak into the home graph, and never mines Sensei's own scaffolded charter as the target repo's contracts.
---

# Sensei Import

Turn "import gin" into a safe, repeatable onboarding run.

Sensei is the architectural memory. This skill is the hand that fills it for a
*new* repository — mechanically, from the repo's own structure and history —
without inventing authority the repo has not earned.

Use this skill when the user wants a repository brought into Sensei: an explicit
`import <name>`, a bare git URL, or "bootstrap / onboard / learn this repo". Stay
proportional: a tiny repo does not need the full ceremony, and a repo already
imported only needs a refresh of its slice.

This is an orchestration reflex, not a passive checklist. You run the steps, you
pause at the two decisions only the user can make (how deep, and what to
govern), and you report honestly what actually landed.

## Non-negotiable guardrails

Read these first. They are the reason this is a skill and not a loose script.

1. **Never auto-govern.** Extraction and mining may stage candidates or create
   traceable `machine_adopted` knowledge under an explicit delegated policy.
   Neither outcome is governed human authority. Only a human promotion or
   governance action may create governed knowledge.
2. **Always scope by domain.** Every build and extraction for a foreign repo
   carries `--repo <domain>` (e.g. `github.com/gin-gonic/gin`). This tags the
   repo's nodes to its own domain so its rules never leak into the home graph,
   never surface on another repo's briefing, and never ride inside the shipped
   seed. A briefing scoped to one domain returns that domain plus `shared`
   meta-principles only.
3. **History mining needs full history.** A shallow clone silently produces no
   revert/regression or PR signals. Unshallow before mining, or say plainly that
   history mining was skipped.
4. **Degrade honestly, never fake.** PR mining needs `gh` auth and an
   `owner/name` slug. If either is missing, fall back to structural-only and say
   so. Do not claim signals you did not gather.
5. **The triangulation gate is real.** Cold signals require ≥2 distinct source
   types before a candidate is drafted. A quiet or solo repo will yield little or
   nothing — that is expected, not a failure. Report the honest count; do not pad
   it.
6. **Verify, don't assert.** Do not claim the import "learned the architecture."
   Prove it with `sensei metadata` and a real `sensei briefing`, and report what
   actually surfaced.
7. **Never let Sensei read itself as the repo's charter.** `sensei bootstrap`
   scaffolds Sensei's own `CLAUDE.md`/`AGENTS.md`/`docs/awareness/` into the
   checkout. If contract/intent extraction runs *after* that, it mines Sensei's
   meta-rules (`surgical changes`, `required tests must pass`, …) back as if the
   target repo authored them. Always extract contracts on the **pristine clone,
   before** `bootstrap`. If you must extract later, drop any intent whose
   `expressed_by` is `CLAUDE.md`/`AGENTS.md`/`docs/awareness/*`.
8. **A compiled handoff is not a live import.** Use these exact completion
   states:
   - `artifact_ready`: reconstruction artifacts exist, but no store was touched.
   - `live_loaded`: the domain-scoped slice, including `.sensei/project`, was
     loaded and verified through typed live queries.
   Never report an artifact-only run as imported into Oxigraph.
9. **Load the reconstruction bundle.** A foreign-domain build must include
   `<checkout>/.sensei/project` in addition to the awareness directories. If a
   non-empty claim document exists but the live `architecture_claim` query is
   empty after loading, the import is incomplete and must fail verification.

## Fast path: one command

`sensei import` wraps the whole pipeline in the correct order (contracts on the
pristine clone → structural → optional history → domain-scoped load), never
promotes, and never touches a store unless you pass `--store-url`:

```
sensei import <git-url | path> --domain <domain> [--depth full|basic] \
              [--store-url <url> --graph-marker-file <server-marker>] [--dry-run]
sensei import --refresh <checkout-path> --domain <domain> [--depth full|basic] \
              [--store-url <url> --graph-marker-file <server-marker>] [--dry-run]
```

- `--dry-run` prints the exact plan and stops — use it first to confirm the
  derived domain/slug and step order.
- `--refresh` re-extracts an existing checkout and optionally reloads the same
  domain-scoped slice. It never clones.
- Full depth uses `--drafter auto` for the contract layer; it degrades to
  structural-only (with a clear notice) when no authenticated drafter is
  available.
- Pass `--graph-marker-file <the server's marker>` alongside `--store-url` so a
  *served* store stays fresh for briefing.
- Omit `--store-url` to have it print the exact `sensei build` command instead
  of touching any store. This is `artifact_ready`, not `live_loaded`.
- When the user asks to import into Sensei and a target store is configured,
  include `--store-url` and the served graph marker. Use artifact-only mode only
  when the user explicitly asks for offline reconstruction or no store exists.

Prefer this command. Fall back to the manual core loop below only when you need
to inspect or intervene between steps, or the wrapper is unavailable.

## Core loop (manual)

1. **Resolve target and domain.**
   - Get the clone URL (or an existing checkout path).
   - Derive the domain tag from the URL host + path, e.g.
     `https://github.com/gin-gonic/gin` → `github.com/gin-gonic/gin`.
   - Derive the `owner/name` slug for PR mining, e.g. `gin-gonic/gin`.
   - Confirm the clone destination and the domain with the user if ambiguous.

2. **Clone and guarantee full history.**
   - `git clone <url> <dest>` (skip if a checkout already exists).
   - Check shallow: `git -C <dest> rev-parse --is-shallow-repository`.
   - If `true`, run `git -C <dest> fetch --unshallow` before any history mining.

3. **Choose extraction depth — ask the user unless already specified.**
   Present the two modes plainly:
   - **Basic** — deterministic structural only. Fast, offline, no key. Extracts
     components, tests, the import graph (Go/TS/Python/Rust), and the
     **deterministic contract layer** — no key needed:
     - proto contracts (`.proto` → gRPC service/RPC Contract nodes)
     - REST contracts (OpenAPI/Swagger specs → endpoint Contract nodes)
     - **code→contract authority surfaces** from Go source (HTTP handlers,
       guards, lifecycle control, state mutations → AuthoritySurface candidates),
       kept at **medium+ confidence**: an external route/handler, a lifecycle
       control, or a guarded mutation is kept; a bare unguarded mutation (a plain
       setter) scores low and is dropped.
     - **boundaries inferred from the import graph** — Go `internal/` visibility
       boundaries (compiler-enforced) and dependency-hub / contract-exposure
       seams (a component many others depend on → a stability boundary)
     - **invariants from the single `extract-invariants` substrate**, gated at
       **medium confidence** (corroborated only). One Go-AST pass yields guard,
       write, schema, and rule-signaling-test facts; a candidate is kept when it
       has a second signal — a guard with a test, an owned write path, or a
       rule-signaling test that attests a behavioral law (race/panic/idempotency).
       An uncorroborated single guard scores low and is dropped, so Sensei never
       manufactures invariants it can't justify. Authority surfaces and invariants
       come from the SAME parse — no double scan.
     - web components + gRPC-web consumption edges (TS/JS)
     Coverage depends on how the repo is written: a repo with `.proto`/OpenAPI or
     `mux.HandleFunc`-style handlers yields contracts even in Basic; a pure
     router library (e.g. gin registers via its own DSL) may yield few — that is
     a detector-breadth limit, not a missing capability.
   - **Full** — Basic **plus the LLM contract/intent layer**: `intent-mine`
     grounds a repo's stated intent (from docs/comments/tests) against the code,
     and (optionally) day-0 history mining (revert/regression commits + PR review
     comments). This deepens the deterministic layer with intent no AST can infer.
     Use `--drafter auto` by default: it prefers an authenticated Claude CLI,
     then an authenticated Codex CLI, then direct Anthropic API credentials. PR
     mining also needs full history and `gh` auth + the `owner/name` slug.
   If the user's request already names a depth, honor it. Degrade honestly: no
   authenticated drafter → say the contract layer is skipped; no `gh` → skip PR
   mining.

   > Extractors take `--path <checkout>`. `sensei build --repo <domain>` is the
   > domain-scoped load flag. Do not pass a domain to extractors or a path to
   > `build --repo`.

4. **Extract the contract layer — Full only, on the PRISTINE clone (before step 5).**
   Do this first, while the checkout still contains only the target repo's own
   files (see guardrail 7). `intent-mine`'s `--path` is the checkout path.
   - Review first (writes nothing):
     `sensei intent-mine --path <checkout-path> --sources docs,comments,tests --drafter auto --max <N>`
   - Then adopt under policy: add `--adopt`. Valid strong intents at certainty
     ≥0.80 become `machine_adopted`, `model_inferred`, `not_human_reviewed`
     `docs/awareness/intent_<id>.yaml`; weaker, hidden, contradictory, or invalid
     ones park under `docs/awareness/candidates/`.
   - Machine-adopted is useful inferred architecture, not governed human
     authority. Human governance still requires explicit promotion/review.
   - `--drafter auto` uses CLI subscription auth when available
     (`claude-cli`, then `codex-cli`) before direct API credentials. Explicit
     `--drafter llm` requires `ANTHROPIC_API_KEY` or `ANTHROPIC_AUTH_TOKEN`.
     Without an authenticated drafter, `--drafter echo` is deterministic but
     shallow; prefer to skip and say the contract layer was not extracted rather
     than ship thin guesses.
   - Sanity-check the output: drop any intent whose `expressed_by` points at
     `CLAUDE.md`/`AGENTS.md`/`docs/awareness/*` — that is Sensei bleed, not a
     repo contract.

5. **Structural extraction — writes YAML into the checkout.**
   `bootstrap`'s `--path` is the checkout path; it writes
   `docs/awareness/generated/*.yaml` inside the cloned repo (scaffolding the repo
   first if it has no `docs/awareness/` — which is why contract extraction runs
   *before* this step).
   - Basic: `sensei bootstrap --path <checkout-path> --skip-history --skip-build`
   - Full (structural pass): `sensei bootstrap --path <checkout-path> --skip-build`
   - Preview first with `--dry-run` when the repo is large or unfamiliar.
   `--skip-build` here on purpose: the domain-tagged load happens in step 7
   against the target store, not inside the throwaway checkout.

6. **Day-0 history / PR mining (Full only).**
   - Online: `sensei cold-bootstrap --path <checkout-path> --repo-slug <owner/name> --auto-window`
   - Offline PR comments: `sensei cold-bootstrap --path <checkout-path> --pr-comments <file.json> --auto-window`
   - `--auto-window` widens the commit-scan window (bounded — never full history)
     until enough revert/regression signals appear; cap it with
     `--auto-window-target <N>` or bound output with `--max <N>`.
   - Narrow the window explicitly with `--since <ref>` when you already know the
     interesting range.

7. **Reconstruct and load the complete slice into the target store.**
   `sensei import` writes `.sensei/project/graph.nt`, `claims.yaml`,
   `claim-audit.yaml`, typed adopted/staged knowledge, readiness, and a
   reconstruction receipt before loading. The manual build must feed all three
   inputs and tag them with the domain:
   ```
   sensei build --input <checkout>/docs/awareness \
                --input <checkout>/docs/awareness/generated \
                --input <checkout>/.sensei/project \
                --store-url <url> \
                --graph-marker-file <server-marker> \
                --repo <domain>
   ```
   `build`'s `--repo` tags every untagged node to `<domain>` and does a
   non-destructive, in-place update: it replaces **only** this domain's triples
   and never touches other domains, shared nodes, or the home slice.
   - **The store must already be non-empty.** A scoped `--repo` update needs an
     existing graph; on a fresh store run `sensei build --all` once to seed the
     base, then add the domain. Do not use `--all` for the foreign slice itself —
     that is a destructive whole-graph load reserved for a cold start.
   - A scoped load recomputes the whole-graph marker after the slice update. Pass
     `--graph-marker-file` when targeting a served store so briefing sees the
     refreshed runtime authority immediately.

8. **Verify what landed in the live store.**
   - `sensei metadata --domain <domain>` — confirm authority, freshness, graph
     digest, and scoped counts for the imported domain.
   - Query `architecture_claim`, `failure_mode`, `boundary`, `intent`,
     `contract`, and `decision` with `sensei query --mode by_class --class
     <class> --domain <domain>`. Honest zeroes are allowed, but the live counts
     must agree with readiness/adoption artifacts.
   - When `.sensei/project/claims.yaml` is non-empty, an empty live
     `architecture_claim` result is a failed import, not thin coverage.
   - `sensei briefing --file <a-real-file-in-the-repo> --domain <domain>` — prove
     a real fact surfaces for a file the repo owns. Pick a file an extracted node
     actually anchors: a Full import shows contracts (e.g. `intent.*`), a Basic
     one only components/tests.
   - Record the completion state as `live_loaded` only after these checks pass.

9. **Summarize and hand off to task awareness.**
   - Report: contracts/intents extracted (Full), structural nodes that landed,
     how many candidates sit in `candidates/` awaiting review, and the honest
     signal count from mining.
   - State the post-import handoff:
     ```text
     Import establishes a reconstructed architectural awareness slice.
     It does not establish bounded closure for a task.

     For the first real change:
       run sensei prepare-change with the exact files,
       inspect the generated questions and probes,
       and obey the resulting admission decision before mutation.
     ```
   - Name the next human step: answer task-bound questions when work reaches the
     affected surface, and review candidates that may deserve governance. Do not
     answer, promote, or govern for them.

## Refresh vs first import

If the domain is already present (`sensei metadata --domain <domain>` shows
nodes), this is a **refresh**. Prefer the facade:

```
sensei import --refresh <checkout-path> --domain <domain> [--depth full|basic] \
              [--store-url <url> --graph-marker-file <server-marker>]
```

The command re-runs extraction and the domain-scoped build in place. The build is
non-destructive to every other domain, so a refresh is safe to run repeatedly.

## What this skill does not do

- It does not promote candidates, decide authority, or edit the graph's rules.
- It does not replace source inspection, tests, builds, or the user's judgment
  about what is worth keeping.
- It does not import a repo's rules into any domain but its own.
- It does not establish bounded task closure.
- It does not freeze historical tasks, create oracle manifests, run external
  benchmarks, or claim blind proof.

## Import vs Benchmark

```text
sensei-import:
  onboarding a repository into a domain slice

sensei-benchmark:
  blind historical evaluation with sealed future oracle
```

See [references/IMPORT-PLAYBOOK.md](references/IMPORT-PLAYBOOK.md) for a worked
end-to-end example and the failure/degradation branches.
