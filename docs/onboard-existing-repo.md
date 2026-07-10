# Onboarding Sensei to an existing repository

A repeatable recipe for standing up an awareness graph on a repo you didn't
author it for — the exact flow validated on a large third-party codebase
(`caddyserver/caddy`). It separates the **deterministic** passes (which a script
runs) from the **judgment** passes (which you or an AI agent must do).

## TL;DR — run the script

```bash
scripts/awg-init-repo.sh \
  --repo /path/to/target-repo \
  --domain github.com/owner/name \
  --repo-slug owner/name            # optional: enables GitHub history mining
```

It runs the deterministic passes and prints a checklist for the rest. Then you
author/promote rules (steps A–E below).

## The layers you're building

An awareness graph for a repo has three sources, in increasing cost:

| Layer | How | Automated? |
|---|---|---|
| **Structural** — components, import edges, code symbols, tests | `awg bootstrap` | ✅ deterministic |
| **History-mined** — forbidden-fixes/failure-modes from reverts + PR reviews | `awg cold-bootstrap` | ✅ deterministic (triangulated candidates) |
| **Authored knowledge** — invariants, contracts, the rules judgment requires | `awg onboard` + human/agent | ⚠️ judgment |

## The deterministic passes (what the script does)

1. **Namespace registry** — scaffold `docs/awareness/namespaces.yaml`.
   **This is required for code-symbol extraction** — without it, `bootstrap`
   silently skips symbols. `owns` must list your source roots (the script infers
   them from where `.go` files live).

2. **`awg bootstrap --repo <path> --skip-build`** — deterministic extraction of
   components, import dependencies, code symbols, tests, and source anchors into
   `docs/awareness/generated/`. `--skip-build` so it never loads a store.

3. **`awg onboard export`** — writes a self-contained brief (architecture scan +
   candidate schema + drafting prompt) to `.awg-onboard-brief.md` for your agent.

4. **`awg cold-bootstrap`** — mines revert/regression commits + PR-review comments,
   **triangulates** (a candidate needs ≥2 distinct source types), and writes
   `status:candidate` YAML. Nothing is promoted.

5. **`awg validate`** — over `docs/awareness` **and** `docs/awareness/generated`.

## The judgment passes (the script prints these; you do them)

- **A. Author rules.** Hand `.awg-onboard-brief.md` to your agent; it drafts
  invariants/contracts/forbidden-fixes/failure-modes as JSON grounded in the code.
  `awg onboard import --from drafts.json` validates (contract-first) and lands them
  in the review queue. Promote the good ones into the canonical
  `docs/awareness/{invariants,contracts,forbidden_fixes,failure_modes}.yaml`.

- **B. Ground-check history-mined candidates** in `docs/awareness/candidates/`
  against current code *before* promoting. Does the cited file/line/symbol still
  exist and hold? **Reverts are the strongest signal** (something was tried and
  backed out = forbidden-fix). A reviewer *question* is not a settled rule.

- **C. Make rules gateable** — add a `detect` block + `enforcement: block` to the
  rules you want CI to enforce:
  ```yaml
  detect:
    applies_to_paths: ["**/caddyfile.go"]
    forbidden_pattern: '\bfmt\.Errorf\('
    required_pattern:  '\b(d|dispenser)\.(Errf|ArgErr)\('
    enforcement: block
  ```

- **D. Wire the gate.** Copy `docs/awg-gate.example.yml` to
  `<repo>/.github/workflows/awg-gate.yml`; add `--domain github.com/owner/name`
  to the gate step for a foreign repo. Test locally:
  ```bash
  awg serve -no-seed & ; awg build --repo <domain> --input docs/awareness --input docs/awareness/generated
  awg gate --enforce --repo-root . --diff HEAD --domain <domain>
  ```

- **E. Verify the payoff.** `awg briefing --file <path> --domain <domain>` — the
  rules that govern the file appear before you'd edit it.

## Gotchas this recipe encodes (learned the hard way)

- **`namespaces.yaml` is mandatory for symbols.** Missing it → `source_symbols`
  silently skipped.
- **`cold-bootstrap --repo-slug` fetches *all* PRs → hangs on big repos.** The
  script fetches a bounded recent window with `gh` and feeds `--pr-comments`
  (the offline path) instead. Tune with `--pr-window` / `--commit-window`.
- **Foreign repos need `--repo <domain>`** on every `build`/`bootstrap` so
  structural nodes are domain-scoped (and briefings can filter to them).
- **`validate` needs both dirs** — `docs/awareness` *and* `docs/awareness/generated`
  — or contract `exposed_by` component refs look dangling.
- **Severity vocab is `critical|high|warning|info|degraded`** (not `medium`/`low`).
- **Nothing auto-promotes.** `onboard import` and `cold-bootstrap` only write to
  the review queue. Promotion into canonical files is a deliberate human step.
- **A rule only gates if it has `detect` + `enforcement: block`.** Semantic rules
  without a regex signature inform briefings but don't block CI.
- **Use isolated ports for the briefing/gate demo** (`serve --addr :10121
  --oxigraph-bind 127.0.0.1:7901`) so you never touch a live `:10120`/`:7878`.

## Worked example — caddy

```bash
scripts/awg-init-repo.sh --repo ~/src/caddy --domain github.com/caddyserver/caddy \
  --repo-slug caddyserver/caddy --commit-window HEAD~500..HEAD
```
yielded: 17 components, 35 import edges, 490 symbols, 490 tests (structural);
10 triangulated candidates from real reverts + PR reviews (history); and — after
authoring/grounding — invariants, contracts, and a `detect`-backed Caddyfile rule
that **blocks** a `fmt.Errorf` reintroduction in CI.
