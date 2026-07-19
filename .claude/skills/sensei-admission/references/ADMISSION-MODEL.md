# Admission Model

Admission answers one bounded question: may this exact agent action be attempted
against this repository state and convergence bundle?

It does not answer whether the action is correct. It does not run tests, inspect
runtime behavior, promote candidates, or certify design quality.

## Inputs

- A convergence bundle directory.
- A change request YAML file describing the exact action.
- An explicit graph snapshot path.
- A repository checkout.
- Optional policy and output detail.

MCP `admit_change` accepts `bundle_dir`, `request_path`, `graph_nt`, `repo`,
optional `policy`, and optional `detail`.

CLI fallback:

```bash
sensei admit-change --bundle <dir> --request <request.yaml> --graph-nt <graph.nt> --repo <checkout> --output <decision.yaml> --format yaml
```

## Boundaries

Admission is an execution-control boundary. Preflight is advisory risk and
context. Gate and EditCheck are additional checks, not admission receipts.

Candidates are not active authority. An admitted action may still require tests,
review, runtime proof, or governance before it is complete.
