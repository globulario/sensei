# UML profile — optional architectural vocabulary

AWG is **not** a UML tool and does not become one. The UML profile is an
**optional** layer of classification metadata that lets architects and AI agents
share a standard architectural vocabulary on top of AWG's native concepts
(MetaPrinciple, Invariant, FailureMode, Decision, Component, Boundary, Contract,
Evidence, …).

UML is **metadata, never authority**: a node's AWG class and AWG relations stay
canonical. UML is used as (a) a naming vocabulary, (b) a diagram/view
classification system, and (c) a mapping layer — not as a source of truth, an
ontology replacement, or a requirement to model everything.

## The `uml` block

Any authored node may carry an optional `uml` block:

```yaml
uml:
  kind: Component        # UML metaclass (closed v1 set, see below)
  stereotype: service    # free-form; lowercase snake_case when generated
  view: structural       # structural | behavioral | interaction | deployment | awareness
  notes: ...             # optional short note
  confidence: inferred   # declared | inferred (omit ⇒ declared)
```

All fields are optional; a node without a `uml` block emits nothing and behaves
exactly as before. Nothing requires UML on any node.

**`uml.kind` (v1):** Component, Package, Interface, Operation, Class, DataType,
Artifact, Node, Deployment, Dependency, Realization, Usage, Association, Signal,
Event, StateMachine, State, Activity, Constraint, UseCase, Actor.

**`uml.view` (v1):** structural, behavioral, interaction, deployment,
**awareness**. `awareness` is the AWG-specific view for the
principle/invariant/failure/decision/evidence layer UML does not cover — kept
explicit so that layer is never flattened into a generic UML Constraint.

## Two authoring paths

1. **Inline** — add a `uml:` block directly on an authored node
   (Component / Boundary / Contract / Decision / Evidence / Invariant /
   ForbiddenFix / FailureMode / Test).

2. **Overlay** — `uml_profiles:` files attach a profile to **any** node by
   class-qualified id, for nodes that are not authored inline (e.g. SourceFiles,
   which exist only as anchors) or where UML labelling is useful without owning
   the node. Linking never types the node.

   ```yaml
   uml_profiles:
     - node: source_file:proto/awareness_graph.proto
       kind: Artifact
       stereotype: proto
       view: structural
   ```

## RDF

Emitted as literals on the node: `aw:umlKind`, `aw:umlStereotype`, `aw:umlView`,
`aw:umlNotes`, `aw:umlConfidence`. Resolve and Query expose them
(`KnowledgeNode.uml_kind/uml_stereotype/uml_view`, `QueryRow.uml_*`) when
present.

## Proto/API extraction

`cmd/proto-scan` populates UML automatically (`confidence: inferred`):

| proto element | uml.kind  | stereotype          | view        |
|---------------|-----------|---------------------|-------------|
| gRPC service  | Interface | grpc_service        | interaction |
| RPC method    | Operation | rpc / rpc_stream    | interaction |

## Validation

`awg validate` rejects an out-of-set `uml.kind` (`invalid_uml_kind`) or
`uml.view` (`invalid_uml_view`). Stereotype is free-form.

## Recommended AWG → UML mappings

| AWG class             | uml.kind (typical)        | uml.view    |
|-----------------------|---------------------------|-------------|
| Component (service…)  | Component / Node          | structural / deployment |
| Contract (service)    | Interface                 | interaction |
| Contract (RPC)        | Operation                 | interaction |
| Contract (message)    | Class / DataType          | structural  |
| Contract (event)      | Signal                    | interaction |
| SourceFile / artifact | Artifact                  | structural  |
| Boundary              | Interface / Constraint    | interaction / awareness |
| Invariant             | Constraint                | awareness   |
| MetaPrinciple         | Constraint                | awareness   |
| Decision              | Constraint / Artifact     | awareness   |
| FailureMode / ForbiddenFix | Constraint           | awareness   |
| Test                  | UseCase / Constraint      | behavioral  |
| Evidence              | Artifact                  | awareness   |
| DesignPattern         | Constraint / StateMachine | awareness / behavioral |
| ImplementationPattern | Activity / Component      | behavioral  |

AWG relation names stay canonical; the UML relation vocabulary (Realization,
Usage, Dependency, …) is an optional mapping for diagram export, not a rename.
