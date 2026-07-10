# Sensei extraction layer v1: structure from code, intent from humans

*A milestone note: what the multi-language extraction layer now ships, what it
deliberately does not claim, and where it could go next. Captured for the
record — not a launch. The canonical design reference is
[extraction-architecture.md](extraction-architecture.md); this is the status
snapshot and the prioritization.*

This milestone closes the first arc of mechanical architecture extraction: Sensei
now derives **observable architectural structure** from a polyglot codebase and
joins it with the human-authored intent layer in one graph — without ever
pretending one is the other.

---

## 1. What shipped (as of 2026-06-16, on master)

| Node type | Auto-extracted from | Extractor |
|---|---|---|
| **Component + `dependsOn`/`reads_from`/…** | **Go, TypeScript/JavaScript, Python, Rust** imports | `golang/extractor/importgraph` (one registered parser per language) |
| **Contract** (Interface + Operation) | **gRPC** (`.proto`) **+ REST** (OpenAPI 3.0/3.1, Swagger 2.0, YAML/JSON) | `protoscan` + `openapiscan` |

Landed across PRs **#65–#69**; the test-infra cleanup that makes a bare
`go test ./...` green landed in **#70**.

**The shape that held:** one generic pipeline — *language parser → normalized
`ImportFact` → component edges → optional config-driven classifier → deterministic
YAML*. Resolution is **language-aware at the edge**, **language-neutral in the
graph model**. Each new language/source was **one registered file** over an
unchanged shared core, reusing the existing `components:`/`contracts:` schemas
(no importer/vocab/ontology changes), and self-dogfooded into Sensei's own graph.

## 2. What it does NOT claim (the boundary)

- **Structure only.** No call graph, no semantic/type analysis, no runtime/DI,
  no framework inference. Imports, specs, and anchors — not behavior.
- **No inferred intent.** Decisions, Boundaries, Meta-principles, Design patterns
  stay human-authored; everything emitted is `assertion: inferred`.
- **No project meaning in core.** Project conventions live only in the optional,
  language-neutral `classifiers:` config; Sensei core ships none.

## 3. Where it could go next (deliberately paused)

Pausing before adding more extractor teeth. Ranked for when we resume:

1. **Web-components extractor** — best next product-visible win (Globular/frontend
   architecture). Reuses the vendored tree-sitter TS; emits `Component` nodes for
   custom-element / framework component declarations. Lowest swamp factor of the
   three.
2. **Per-language misuse candidates** — powerful but riskier. Generalizes the
   Go storage-driver detector to TS/Python/Rust. Must stay strictly
   **candidate / review-only** (`status: candidate`, never auto-promoted) — the
   candidate/review boundary is the whole safety story here.
3. **Code-driven framework routes** — most useful long-term (routes without a
   spec file), but the **highest swamp factor** (per-framework parsing,
   Express/Gin/FastAPI/Flask/Axum, decorators, dynamic registration). Do later.

Each is an independent extractor following the same pattern — no core changes
expected, observable facts only, human intent untouched.
