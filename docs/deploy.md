# Deploying Sensei (self-host)

Sensei is two things: a **service** (the awareness-graph gRPC server + an Oxigraph
store) and **client tools** (`sensei` CLI, `awareness-mcp` bridge) that agents and
CI point at it. This guide self-hosts both. Managed hosting comes later; the
same client tools will point at it unchanged.

## 1. Run the service

```bash
cd deploy
docker compose up --build       # Oxigraph + awareness-graph on :10120
```

The server seeds the store from its embedded corpus on first start (idempotent —
it skips seeding a non-empty store), so there is nothing else to load. The store
lives in the `awg-oxigraph` volume and survives restarts.

> **Local dev without Docker?** `sensei serve` runs the same server directly, but it
> needs an `oxigraph` binary on PATH (or `--no-oxigraph` pointing at an external
> one) — `go install` does not provide it. Compose bundles Oxigraph, so it is the
> recommended path; reach for `sensei serve` only when you already have Oxigraph.

Verify it:

```bash
SENSEI_ADDR=localhost:10120 sensei metadata  # coverage + freshness
```

## 2. Install the client tools

```bash
curl -fsSL https://raw.githubusercontent.com/globulario/sensei/master/deploy/install.sh | sh
# or, from a checkout:  ./deploy/install.sh
```

This installs `sensei` and `awareness-mcp` with Go into `$(go env GOBIN)` (or
`$GOPATH/bin`). Point every client at the service with `SENSEI_ADDR` (or each
command's `--addr`). Legacy `AWG_ADDR` still works as a fallback.

## 3. Enable auth (optional, opt-in)

By default the service is **open** — correct for a trusted local network or a
single-host dev setup. To require a bearer token, set `SENSEI_TOKEN` for the
service and give clients the **same** token:

```bash
# service (deploy/.env or the environment):
echo "SENSEI_TOKEN=$(openssl rand -hex 24)" > deploy/.env
docker compose up -d

# clients:
export SENSEI_TOKEN=<the same token>
sensei metadata                       # now authenticated
```

- Health and reflection stay open so liveness probes and tooling keep working.
- The token rides in the gRPC `authorization: Bearer …` metadata and is allowed
  over plaintext, so put a **TLS-terminating reverse proxy** in front for any
  untrusted network. (mTLS via the Globular cert path remains available for the
  managed tier.)
- A client without the token gets `Unauthenticated: missing bearer token`; a
  wrong token gets `invalid bearer token`.

## 4. Wire it to an agent

- **CI gate:** see `docs/awg-gate.example.yml` (`sensei gate --enforce`), and
  `SENSEI_EVENT_LOG` + `sensei evidence` for outcome tracking (`docs/awg-gate.example.yml`).
- **Any agent over MCP:** run `awareness-mcp --awareness-addr localhost:10120`;
  every tool returns structured `structuredContent` (see Pillar 3.1).
- **Pre-edit guard, any agent:** `sensei edit-guard --format exit-code`
  (`docs/awg-edit-guard-neutral.example.md`).

All three honor `SENSEI_TOKEN`, so enabling auth secures the whole surface at once.
