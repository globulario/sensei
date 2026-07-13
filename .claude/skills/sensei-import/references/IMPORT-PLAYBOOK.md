# Import Playbook

A worked end-to-end import and the branches for when signals are thin or tools
are missing. Substitute the real domain and slug for the repo you are importing.

## Worked example: import `gin`

Target: `https://github.com/gin-gonic/gin`
Domain: `github.com/gin-gonic/gin`
Slug: `gin-gonic/gin`

The one-command path (`sensei import https://github.com/gin-gonic/gin --domain
github.com/gin-gonic/gin`) runs all of the below in order. This is the manual
walkthrough so you can see what each step produces.

1. **Clone + full history**
   - `git clone https://github.com/gin-gonic/gin /tmp/gin`
   - `git -C /tmp/gin rev-parse --is-shallow-repository` → if `true`,
     `git -C /tmp/gin fetch --unshallow`.

2. **Ask depth.** User picks **Full** (`ANTHROPIC_API_KEY` set; gin has heavy
   PR-review history too).

   > Extractors take `--path <checkout>`. `sensei build --repo <domain>` is the
   > domain-scoped load flag. (`--repo` is still accepted on the extractors as a
   > deprecated alias for `--path`.)

3. **Extract the LLM intent layer — FIRST, on the pristine clone.**
   Run this *before* step 4, or `bootstrap`'s scaffolded `CLAUDE.md`/`AGENTS.md`
   pollute it (see the contamination branch below).
   - Review: `sensei intent-mine --path /tmp/gin --sources docs,comments,tests --drafter llm --max 12`
   - Land it: add `--apply`. On gin this produced **7 grounded intents** —
     `context_copy_isolation` (a copied Context must not affect the original,
     grounded at `executable_truth`), `basic_auth_default_realm`,
     `upload_filename_untrusted`, `trust_unix_socket_xff`, `clientip_non_ip_guard`,
     `validatestruct_no_panic`, `method_not_allowed_empty_tree_no_panic` — as
     `docs/awareness/intent_<id>.yaml`, plus weaker ones under `candidates/`.

4. **Structural + deterministic contract pass — writes YAML into the checkout.**
   `sensei bootstrap --path /tmp/gin --skip-history --skip-build` runs the whole
   deterministic layer (no key). On gin it produces:
   - `generated/components.yaml` — **6 components**; `generated/tests.yaml` — **630 tests**
   - `candidates/authority_surface_candidates.yaml` — **0** (gin registers routes
     via its own DSL, not `mux.HandleFunc`; a detector-breadth limit, not a gap)
   - `candidates/boundary_candidates.yaml` — **2** compiler-enforced `internal/`
     visibility boundaries (`internal/bytesconv`, `internal/fs`)
   - `candidates/invariant_candidates.yaml` — **~207** invariants from the single
     `extract-invariants` extractor at **medium confidence** (corroborated only:
     a guard with a test, an owned write path, or a rule-signaling test that
     attests a behavioral law like `Race context copy` / `Context copy should not
     cancel`). Uncorroborated single guards score low and are dropped.
   - proto/OpenAPI contracts — 0 (gin has neither)
   - (Large/unfamiliar repo? Preview with `--dry-run` first.)

5. **Day-0 mining** (Full, optional)
   - `sensei cold-bootstrap --path /tmp/gin --repo-slug gin-gonic/gin --auto-window`
   - Bound it with `--max <N>` or `--auto-window-target <N>` if the window keeps
     widening. Use `--since <ref>` when you already know the range of interest.

6. **Load the slice, tagged to the domain**
   - Fresh store only: `sensei build --all` once to seed a base graph.
   - `sensei build --input /tmp/gin/docs/awareness --input /tmp/gin/docs/awareness/generated --repo github.com/gin-gonic/gin`
   - Non-destructive, in place; needs a non-empty store (see step above).

7. **Verify**
   - `sensei metadata --domain github.com/gin-gonic/gin`
   - `sensei briefing --file context.go --domain github.com/gin-gonic/gin` — with
     the contract layer, this surfaces the real intents (`context_copy_isolation`,
     `clientip_non_ip_guard`, `trust_unix_socket_xff`, `upload_filename_untrusted`),
     not just `[component]` boxes. Brief a file an extracted node actually anchors.

8. **Hand off**
   - Report all four candidate layers (contracts/intents, authority, boundaries,
     invariants) + node counts + the candidate queue. Tell the user to review and
     `sensei promote` the load-bearing ones. Stop.

## Degradation branches

**Sensei self-contamination (extraction order).** If `intent-mine` runs *after*
`bootstrap`, it reads the scaffolded `CLAUDE.md`/`AGENTS.md` and mines Sensei's
own charter as the repo's contracts — on gin this produced three bogus intents
(`surgical_changes`, `required_tests_must_pass`, `briefing_invariants_authority`,
all `expressed_by: AGENTS.md`). Fix: extract on the pristine clone first. If you
can't, drop every intent whose `expressed_by` is `CLAUDE.md`/`AGENTS.md`/
`docs/awareness/*` before building.

**No `ANTHROPIC_API_KEY`.** The `--drafter llm` contract layer is unavailable.
`--drafter echo` is deterministic but shallow; prefer to skip the contract layer
and say so rather than ship thin guesses. Basic (structural) still runs.

**Shallow clone.** History mining silently yields nothing on `--depth 1`.
Unshallow first, or run Basic and state that history mining was skipped.
(A shallow clone is fine for `intent-mine`, which reads the tree, not history.)

**No `gh` / no slug.** PR-comment mining is unavailable. Two honest options:
run **Basic** (`sensei bootstrap --path <checkout> --skip-history --skip-build`,
then the step-6 `build`), or run `cold-bootstrap` with
`--pr-comments <file.json>` if an offline export exists.
Never invent PR signals. (Basic still yields the full deterministic layer:
components, tests, authority surfaces, boundaries, and invariants-from-tests.)

**Quiet or solo repo.** The triangulation gate needs ≥2 distinct source types.
A repo with few reverts and no review threads harvests little or nothing. Report
the real count. This is expected — structural extraction still gives value; the
history layer just has nothing to stand on.

**Domain already present.** `sensei metadata --domain <domain>` shows existing
nodes → treat as a refresh. Re-run extraction and the step-6
`sensei build --input <checkout>/... --repo <domain>`; it updates only that slice
and is safe to repeat (the store is already non-empty).

**Large repo.** Preview with `--dry-run`, bound mining with `--max`, and consider
`--skip-build` to review the generated slice before loading it into the store.

## Honesty checklist before you report

- Did the graph actually gain nodes for this domain? (metadata, not assumption)
- Did a real file's briefing surface something? (verified, not asserted)
- For each of the four layers — contracts/intents, authority surfaces,
  boundaries, invariants-from-tests — report the real count, including the zeros
  (a 0 is a fact about the repo, e.g. gin's DSL yields 0 authority surfaces).
- How many candidates are queued, and are they candidates (not authority)?
- What was skipped or degraded, and why?
- What is the exact human next step to promote?
