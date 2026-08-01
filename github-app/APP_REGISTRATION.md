# GitHub App registration

Register a GitHub App with the following minimum contract.

## General

- Webhook URL: `https://<host>/webhooks/github`
- Webhook secret: generate a high-entropy secret and provide the same value as `SENSEI_GITHUB_WEBHOOK_SECRET`
- SSL verification: enabled

## Repository permissions

| Permission | Access | Purpose |
|---|---:|---|
| Metadata | Read | Repository identity |
| Pull requests | Read | PR event and changed-file metadata |
| Issues | Read and write | Sticky PR conversation comment |
| Checks | Read and write | Idempotent Sensei Check Run |

No Actions, Administration, Pages, Workflows, Secrets, or repository-content write permission is required for the first PR-briefing slice.

## Events

Subscribe to:

- Pull request

The service processes only:

- `opened`
- `reopened`
- `synchronize`

Other actions are acknowledged without analysis.

## Private key

Generate a private key from the GitHub App settings page. Store it as a secret and provide it through either:

- `SENSEI_GITHUB_PRIVATE_KEY`
- `SENSEI_GITHUB_PRIVATE_KEY_FILE`

Never commit the private key or webhook secret.

## First live proof

1. Deploy the service with `/healthz` reachable.
2. Register the webhook URL and secret.
3. Install the app on one test repository only.
4. Open or update a pull request.
5. Confirm that one sticky briefing comment and one `Sensei Architectural Briefing` Check Run are created for the exact head SHA.
6. Redeliver the same webhook and confirm that the existing surfaces are updated rather than duplicated.
