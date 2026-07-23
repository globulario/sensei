# Sensei GitHub App private-beta runbook

This runbook creates one private GitHub App owned by `globulario`, deploys a digest-pinned image, installs the app on one selected test repository, and captures evidence before any public release.

## 1. Prepare DNS and a deployment host

Choose a dedicated hostname, for example `sensei-app.globular.cloud`, and point its A/AAAA record to a host that accepts inbound TCP 80 and 443.

The only public application routes are:

- `GET /healthz`
- `GET /readyz`
- `POST /webhooks/github`

Caddy obtains and renews TLS automatically. The application container is not exposed directly to the host network.

## 2. Publish the private GHCR image

Merging a GitHub App change to `main` runs the **GitHub App Image** workflow automatically. The workflow can also be dispatched manually with an explicit private tag.

The workflow publishes:

- `ghcr.io/globulario/sensei-github-app:private-beta`
- `ghcr.io/globulario/sensei-github-app:sha-<commit>`
- a `github-app-image-receipt` artifact containing the immutable digest

A newly published GHCR package is private by default. Keep it private during this phase. Copy the receipt's `immutable_reference` value into `deploy/.env.private`.

On the deployment host, authenticate an account that can read the private package:

```bash
echo "$GHCR_TOKEN" | docker login ghcr.io -u "$GHCR_USER" --password-stdin
```

Use a package credential with `read:packages`, or another credential explicitly authorized for the package. Do not put this credential in the Compose environment file.

## 3. Generate the one authoritative webhook secret

From `github-app/deploy/` on the deployment host:

```bash
cp .env.private.example .env.private
mkdir -p secrets
chmod 700 secrets
openssl rand -hex 32 > secrets/github-webhook-secret.txt
chmod 600 secrets/github-webhook-secret.txt
```

The exact contents of `secrets/github-webhook-secret.txt` are the only authoritative webhook secret. Paste that value into GitHub during registration. Do not generate a second value in the GitHub UI.

## 4. Register the private GitHub App

In the `globulario` organization settings, create a new GitHub App using `config/private-app-registration.json` as the authoritative registration contract.

Required values:

- Name: `sensei-globulario-private-beta` or another globally unique private-beta name
- Homepage: `https://github.com/globulario/sensei/tree/main/github-app`
- Webhook active: yes
- Webhook URL: `https://<hostname>/webhooks/github`
- Webhook secret: paste the exact value from `secrets/github-webhook-secret.txt`
- Request user authorization during installation: no
- Visibility / installation target: only this account

Repository permissions:

- Metadata: read
- Pull requests: read
- Issues: write
- Checks: write

Subscribe only to:

- Pull request

Generate a private key after creating the app. Record the numeric App ID. Keep the app private.

## 5. Stage the private key and deployment identity

```bash
install -m 0600 /path/to/downloaded-private-key.pem secrets/github-app-private-key.pem
```

Set these values in `.env.private`:

- `SENSEI_GITHUB_IMAGE` to the digest-pinned GHCR reference
- `SENSEI_GITHUB_APP_ID` to the numeric App ID
- `SENSEI_GITHUB_HOSTNAME` to the public hostname

Never commit `.env.private` or files under `deploy/secrets/`.

## 6. Deploy

```bash
docker compose --env-file .env.private -f docker-compose.private.yml pull
docker compose --env-file .env.private -f docker-compose.private.yml up -d
docker compose --env-file .env.private -f docker-compose.private.yml ps
curl --fail --silent --show-error "https://$SENSEI_GITHUB_HOSTNAME/healthz"
curl --fail --silent --show-error "https://$SENSEI_GITHUB_HOSTNAME/readyz"
```

Inspect structured logs:

```bash
docker compose --env-file .env.private -f docker-compose.private.yml logs -f sensei-github-app caddy
```

The startup record must identify the application version, source commit, build time, listening address, and App ID.

## 7. Install on one selected repository

Install the private app on `globulario` and choose **Only select repositories**. Select a disposable test repository, not the entire organization.

Record the installation ID from the installation URL or API response.

## 8. Prove the PR lifecycle

Create a small pull request in the selected repository that changes:

- one implementation file
- one test file
- optionally one workflow or dependency boundary

### Opening proof

After opening the PR, verify:

- exactly one app-owned comment contains `<!-- sensei-architectural-briefing -->`
- exactly one **Sensei Architectural Briefing** Check Run is attached to the current head SHA
- the comment reports the exact base and head SHAs
- the application log contains the GitHub delivery ID, installation ID, repository, PR number, base SHA, head SHA, and external check identity

### Redelivery proof

From the GitHub App's **Advanced** settings, open the webhook delivery and select **Redeliver**.

Verify:

- the same sticky comment ID remains
- no second app-owned marker comment appears
- the same head-bound external check identity remains
- the delivery ID is visible in the application log

### Synchronize proof

Push a new commit to the PR.

Verify:

- the existing sticky comment is updated in place
- the new comment body contains the new head SHA
- a Check Run exists for the new head SHA
- the old Check Run remains historically attached only to the old SHA

### Stale replay proof

Redeliver the earlier `opened` delivery after the new commit exists.

Verify:

- no current output is replaced with the old SHA
- no additional comment or current-head check is created
- the application log records `discarded stale pull request delivery`

### Spoof-resistance proof

Create a normal user comment containing the hidden marker.

Synchronize the PR again and verify that Sensei updates only the app-owned comment, leaving the user comment untouched.

## 9. Capture the proof receipt

Collect the opening, redelivery, synchronize, and stale-replay delivery IDs. Then run:

```bash
bash scripts/capture-live-proof.sh \
  globulario/<test-repository> \
  <pr-number> \
  <app-id> \
  <installation-id> \
  <sha256-image-digest> \
  <opened>,<redelivery>,<synchronize>,<stale-replay> \
  sensei-github-app-live-proof.json
```

Commit only the proof receipt if it contains no credentials. Never commit the private key, webhook secret, package token, or raw webhook payloads containing sensitive repository information.

## 10. Private-beta acceptance

The private beta is accepted only when:

- the image is digest-pinned
- all four lifecycle proofs pass
- the receipt identifies one app-owned comment and one current-head Check Run
- stale replay is visibly discarded
- no arbitrary repository code was executed
- the app remains private and limited to selected repositories

After acceptance, the next engineering slice replaces the mechanical briefing source with a pinned Sensei appliance. Public visibility remains a separate release decision.
