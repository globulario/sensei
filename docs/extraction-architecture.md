# Sensei multi-language extraction architecture

Sensei derives **observable structure** from code and joins it with **human-authored intent** in one
graph. This document defines the generic extraction pattern, starting with import/dependency graphs.

## The pipeline (one shape for every language)

```
language parser  ->  normalized import facts  ->  component dependency edges
                 ->  optional classifier upgrades  ->  deterministic generated YAML
```

A **language parser** knows one language's syntax and resolution. A **language-neutral core** rolls
files up to Components, turns internal cross-component imports into `aw:dependsOn` edges, applies an
optional config-driven classifier, and renders deterministic YAML — identically for every language.

## Final design law

> **Sensei core extracts structure. Classifier config maps project conventions. Humans author intent.
> The graph joins them without pretending one is the other.**

Concretely:

- A language extractor **may** know language syntax (imports, modules, stdlib, path resolution).
- A language extractor **may not** know project-specific meaning (what a path *means* to a team).
- Project meaning lives **only** in optional classifier configuration.
- Intent — Decisions, Boundaries, Meta-principles, Design patterns — stays **human-authored**.
- Everything a parser emits is `assertion: inferred`: an observable fact, never a claim about "why".

## Normalized model

Each parser emits, per repo (`golang/extractor/importgraph`):

- `ImportFact{ Language, SourceFile, Raw, Kind, Resolved, TargetPath }`
  - `Resolved` ∈ `stdlib | internal | external | unresolved`
  - `TargetPath` (repo-relative package dir) is set only when `Resolved == internal`.
- `SourceFile{ Path, IsEntrypoint }` — every scanned file; `IsEntrypoint` marks a service component.

The shared core does the rest — the parser decides **no** components, edges, or meaning:

- **Rollup** (`componentForDir`): a file rolls up to the Component for its directory (one level under
  a known source root, else the top-level dir). Cross-component internal import → one `depends_on`.
- **Classifier**: a matched rule upgrades the import into `reads_from` / `writes_to` /
  `exposes_contracts` / `depends_on`. Otherwise: internal → `depends_on`, external →
  `external_imports` (review-only, not a triple), stdlib/unresolved → dropped (never fatal).
- **Render**: deterministic YAML reusing the existing `components:` schema, so the awareness importer
  ingests it with **no importer / vocabulary / ontology change**.

## Classifier configuration (language-neutral, optional)

A single optional file (`docs/awareness/import_classifiers.yaml`, or `-config`). Sensei core ships **no
rules**. First match wins, per language, in declaration order. Go regexp is **RE2 — no
backreferences**; capture groups + templates only.

```yaml
# Fictional examples only — these are NOT shipped by Sensei.
classifiers:
  - id: acme_go_gateway
    language: go
    match: "^acme\\.test/platform/([a-z]+)/[a-z]+_gateway$"
    edge: reads_from
    target: "component.$1"
    target_class: component

  - id: acme_ts_api_client
    language: typescript
    match: "^@api/(.+)Client$"
    edge: depends_on
    target: "contract.$1"
    target_class: contract

  - id: acme_python_repository
    language: python
    match: "^app\\.repositories\\.(.+)$"
    edge: reads_from
    target: "component.$1"
    target_class: component
```

## Generated files

One deterministic, committed file per language, ingested by the normal importer:

```
docs/awareness/generated/awareness_graph_go_import_graph.yaml      (implemented)
docs/awareness/generated/awareness_graph_typescript_import_graph.yaml
docs/awareness/generated/awareness_graph_python_import_graph.yaml
docs/awareness/generated/awareness_graph_rust_import_graph.yaml
```

Each carries a generated header (extractor + version + regen command + language), `assertion:
inferred`, sorted output, and a review-only `external_imports` list. Regenerate with `make
import-graph`; CI gates freshness with `make import-graph-check` (per the proto-contracts pattern).

## Adding a language

The four tree-sitter grammars (TypeScript, JavaScript, Python, Rust) are already vendored, and
`golang/scanner/typescript.go` already drives tree-sitter — so a new language is one file in the
`importgraph` package that `register("<lang>", parse<Lang>Imports)` and returns `ParseResult`. No
new dependencies, no core changes.

## Roadmap

1. **Go** — implemented (`golang.go`).
2. **TypeScript / JavaScript** — implemented (`typescript.go`): ES/side-effect/`require`/static-
   `import()`; relative + `tsconfig`/`jsconfig` path-alias resolution; **npm/pnpm/yarn workspace
   package names → in-repo packages** (so a monorepo's app→package edges resolve); other
   bare specifiers → external.
3. **Python** — implemented (`python.go`): `import`, `from x import`, relative `from .foo`/`..foo`;
   stdlib module-name set; third-party → external.
4. **Rust** — implemented (`rust.go`): crate-level — `use`/`extern crate` resolved by the first
   segment against `Cargo.toml` workspace crates; `crate::`/`self::`/`super::`/`mod` are intra-crate;
   stdlib = `std`/`core`/`alloc`/`proc_macro`/`test`; other crates → external.
5. **Higher-level** (after the import graph) — separate extractors that emit other node types:
   - **REST/OpenAPI contracts** — implemented (`golang/extractor/openapiscan` + `cmd/openapi-scan`):
     OpenAPI 3.0/3.1 + Swagger 2.0 **spec files** → Contract nodes (one Interface per spec, one
     Operation per path×method; read/write from the HTTP method), reusing the `contracts:` schema
     like `proto-scan`. Spec-file-driven only.
   - **Web components** — implemented (`golang/extractor/webcompscan` + `cmd/webcomponent-scan`):
     native custom-element registrations (`customElements.define('tag', …)`, `window.` variant, and
     Lit `@customElement('tag')`) → Component nodes (`uml.stereotype web_component`, structural),
     reusing the `components:` schema. Native custom elements only — no framework inference.
   - **gRPC-web contract consumption** — implemented (`golang/extractor/grpcwebscan` +
     `cmd/grpcweb-scan`): observable gRPC-web service-client usage in TS/JS → `consumed_by`
     edges on Contract nodes. A client symbol counts only with grpc-web provenance (imported
     from a `*_grpc_web_pb` module, by name or as a namespace member); the backend service is
     recovered from the **client symbol** (strip `Client`/`PromiseClient`), not the import path.
     The contract id is minted with `protoscan.Snake`, so the edge links to the Contract
     `proto-scan` already defines — cross-repo, when both repos are in the graph. The consumer
     is the import-graph rollup of the file (`importgraph.ComponentForFile`). Service-level
     only; no call graph, no per-method consumption. Reuses the `contracts:` schema's existing
     `consumed_by` edge — no importer/vocabulary change.
   - **Future** — code-driven framework routes, per-language misuse candidates.

The planned import-graph language set (Go, TS/JS, Python, Rust) is complete; REST/OpenAPI contracts
are the first higher-level extractor.

## Non-goals

No REST/OpenAPI/web-component/framework-route extraction; no automatic boundary/principle/decision/
intent inference; no call-graph analysis; no candidate promotion; **no project-specific rules,
paths, or names in Sensei core.**
