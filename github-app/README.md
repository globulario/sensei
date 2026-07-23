# Sensei GitHub App

Zero-install architectural briefings for pull requests.

This directory is an incubation module intended to be extracted into
`globulario/sensei-github-app` after the private installation and engine-integration proofs are complete.

## MVP

- verify GitHub webhook signatures
- authenticate as the installation that emitted the event
- handle `pull_request` events for `opened`, `reopened`, and `synchronize`
- bind every result to exact base and head SHAs
- reject delayed or racing deliveries that no longer match the current PR identity
- collect changed-file metadata without executing repository code
- publish one app-owned sticky briefing comment
- publish one app-owned completed Check Run
- preserve delivery, installation, repository, PR, and revision identity in structured logs

The service is a thin orchestration layer. Architectural reasoning remains in
Sensei. LLM-derived knowledge is never authoritative until reviewed and
promoted through repository-owned sources.

## Current boundary

The first slice is intentionally mechanical and advisory. It reports immutable
change identity, changed-file scope, test-file presence, file types, and
sensitive structural surfaces. It does not execute pull-request code.

The private-beta release adds:

- a private GHCR image workflow with immutable digest receipts
- mounted secret files
- `/healthz` and `/readyz`
- a hardened Docker Compose deployment behind Caddy TLS
- an authoritative private registration contract
- a live-proof capture script and runbook

See [`docs/private-beta-runbook.md`](docs/private-beta-runbook.md).

The next reasoning slice will replace the mechanical report source with a pinned
Sensei engine or appliance run so the same GitHub surfaces can include governed
invariants, contracts, failure modes, forbidden fixes, and proof obligations.

## Required environment

- `SENSEI_GITHUB_APP_ID`
- `SENSEI_GITHUB_PRIVATE_KEY` or `SENSEI_GITHUB_PRIVATE_KEY_FILE`
- `SENSEI_GITHUB_WEBHOOK_SECRET` or `SENSEI_GITHUB_WEBHOOK_SECRET_FILE`
- `SENSEI_GITHUB_LISTEN_ADDR` (optional, defaults to `:8080`)
- `SENSEI_GITHUB_API_URL` (optional, defaults to `https://api.github.com`)

## Development

```bash
go test ./...
go run ./cmd/sensei-github-app
```

Container healthcheck command:

```bash
/sensei-github-app healthcheck http://127.0.0.1:8080/readyz
```

Health endpoints:

```text
GET /healthz
GET /readyz
```

Webhook endpoint:

```text
POST /webhooks/github
```

## GitHub App permissions

Repository permissions:

- Metadata: read
- Pull requests: read
- Issues: write
- Checks: write

Subscribe only to the `pull_request` webhook event during the private beta.
