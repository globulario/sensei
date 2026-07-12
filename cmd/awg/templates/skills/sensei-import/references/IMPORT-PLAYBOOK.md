# Import Playbook

A worked end-to-end import and the branches for when signals are thin or tools
are missing. Substitute the real domain and slug for the repo you are importing.

## Worked example: import `gin`

Target: `https://github.com/gin-gonic/gin`
Domain: `github.com/gin-gonic/gin`
Slug: `gin-gonic/gin`

1. **Clone + full history**
   - `git clone https://github.com/gin-gonic/gin /tmp/gin`
   - `git -C /tmp/gin rev-parse --is-shallow-repository` → if `true`,
     `git -C /tmp/gin fetch --unshallow`.

2. **Ask depth.** User picks **Full** (gin has heavy PR review + revert history,
   so mining pays off).

   > `bootstrap --repo` = checkout **path**. `build --repo` = **domain** string.

3. **Structural pass — writes YAML into the checkout**
   - `sensei bootstrap --repo /tmp/gin --skip-history --skip-build`
   - Writes `/tmp/gin/docs/awareness/generated/{components,tests}.yaml`.
   - (Large/unfamiliar repo? Preview with `--dry-run` first.)

4. **Day-0 mining** (Full only; skipped in this Basic example)
   - `sensei cold-bootstrap --repo /tmp/gin --repo-slug gin-gonic/gin --auto-window`
   - Bound it with `--max <N>` or `--auto-window-target <N>` if the window keeps
     widening. Use `--since <ref>` when you already know the range of interest.

5. **Load the slice, tagged to the domain**
   - Fresh store only: `sensei build --all` once to seed a base graph.
   - `sensei build --input /tmp/gin/docs/awareness --input /tmp/gin/docs/awareness/generated --repo github.com/gin-gonic/gin`
   - Non-destructive, in place; needs a non-empty store (see step above).

6. **Verify**
   - `sensei metadata --domain github.com/gin-gonic/gin`
   - `sensei briefing --file binding/binding.go --domain github.com/gin-gonic/gin`
     — surfaces `[component] component.binding`. Brief a file the domain owns and
     that an extracted node actually anchors (a bare `context.go` may have none).

7. **Hand off**
   - Report node counts and the candidate queue. Tell the user to review and
     `sensei promote` the load-bearing ones. Stop.

## Degradation branches

**Shallow clone.** History mining silently yields nothing on `--depth 1`.
Unshallow first, or run Basic and state that history mining was skipped.

**No `gh` / no slug.** PR-comment mining is unavailable. Two honest options:
run **Basic** (`sensei bootstrap --repo <checkout-path> --skip-history --skip-build`,
then the step-5 `build`), or run `cold-bootstrap` with
`--pr-comments <file.json>` if an offline export exists.
Never invent PR signals.

**Quiet or solo repo.** The triangulation gate needs ≥2 distinct source types.
A repo with few reverts and no review threads harvests little or nothing. Report
the real count. This is expected — structural extraction still gives value; the
history layer just has nothing to stand on.

**Domain already present.** `sensei metadata --domain <domain>` shows existing
nodes → treat as a refresh. Re-run structural extraction and the step-5
`sensei build --input <checkout>/... --repo <domain>`; it updates only that slice
and is safe to repeat (the store is already non-empty).

**Large repo.** Preview with `--dry-run`, bound mining with `--max`, and consider
`--skip-build` to review the generated slice before loading it into the store.

## Honesty checklist before you report

- Did the graph actually gain nodes for this domain? (metadata, not assumption)
- Did a real file's briefing surface something? (verified, not asserted)
- How many candidates are queued, and are they candidates (not authority)?
- What was skipped or degraded, and why?
- What is the exact human next step to promote?
