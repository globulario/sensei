# Publishing the Sensei VS Code extension

The extension is packaged and validated by CI (`sensei-<version>.vsix`). Getting
it onto the **Visual Studio Marketplace** needs a one-time publisher + token
setup that only the account owner can do — Microsoft ties it to your Azure
DevOps / Marketplace identity.

## One-time setup

1. **Create the publisher.** Go to <https://marketplace.visualstudio.com/manage>,
   sign in with the Microsoft account that should own the listing, and create a
   publisher with the ID **`globulario`** (must match `"publisher"` in
   `package.json`).

2. **Generate a Personal Access Token (PAT).** In Azure DevOps
   (<https://dev.azure.com/>) → User settings → **Personal access tokens** →
   New Token:
   - Organization: **All accessible organizations**
   - Scopes: **Custom defined** → **Marketplace › Manage**
   - Copy the token (shown once).

3. **Store the token** so CI can publish:
   - Repo → **Settings → Secrets and variables → Actions → New repository secret**
   - Name: **`VSCE_PAT`**, value: the token.

## Publish

Once `VSCE_PAT` is set, publish either way:

- **From GitHub** (recommended): Actions → **Publish VS Code extension** → *Run
  workflow*. It tests, packages, uploads the `.vsix` artifact, and publishes.
- **By tag:** `git tag vscode-v0.1.0 && git push origin vscode-v0.1.0`.

The version published is the one in `package.json` — **bump it before each
release** (and add a `CHANGELOG.md` entry).

## Publish locally instead (no CI)

```sh
cd editor/vscode
npm ci
npx @vscode/vsce publish -p <YOUR_PAT>     # or: vsce login globulario, then vsce publish
```

## Install without the Marketplace

Any packaged build installs directly:

```sh
code --install-extension sensei-awareness-0.1.0.vsix
```
