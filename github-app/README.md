# Sensei GitHub App

Zero-install architectural briefings for pull requests.

This directory is an incubation module intended to be extracted into
`globulario/sensei-github-app` once the first vertical slice is proven.

## MVP

- verify GitHub webhook signatures
- authenticate as the installation that emitted the event
- handle `pull_request` events for `opened`, `reopened`, and `synchronize`
- bind every result to exact base and head SHAs
- collect changed-file metadata without executing repository code
- publish one sticky briefing comment
- publish one completed Check Run

The service is a thin orchestration layer. Architectural reasoning remains in
Sensei. LLM-derived knowledge is never authoritative until reviewed and
promoted through repository-owned sources.

## Current boundary

The first slice is intentionally mechanical and advisory. It reports immutable
change identity, changed-file scope, test-file presence, file types, and
sensitive structural surfaces. It does not execute pull-request code.

The next slice will replace the mechanical report source with a pinned Sensei
engine or appliance run so the same GitHub surfaces can include governed
invariants, contracts, failure modes, forbidden fixes, and proof obligations.

A merge-ready release also requires one live GitHub App installation proof:
an authenticated webhook must create one app-owned sticky comment and one
app-owned Check Run, and redelivery must update rather than duplicate them.

## Required environment

- `SENSEI_GITHUB_APP_ID`
- `SENSEI_GITHUB_PRIVATE_KEY` or `SENSEI_GITHUB_PRIVATE_KEY_FILE`
- `SENSEI_GITHUB_WEBHOOK_SECRET`
- `SENSEI_GITHUB_LISTEN_ADDR` (optional, defaults to `:8080`)
- `SENSEI_GITHUB_API_URL` (optional, defaults to `https://api.github.com`)

## Development

```bash
go test ./...
go run ./cmd/sensei-github-app
```

Health endpoint:

```text
GET /healthz
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

Subscribe to the `pull_request` webhook event.
