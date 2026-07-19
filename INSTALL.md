# Installing Sensei

Sensei has three pieces:

| Piece | What it is | How you get it |
|---|---|---|
| `sensei` | the CLI (init, build, serve, briefing, validate, audit) | prebuilt bundle / installer, or source build |
| `awareness-graph` | the gRPC server `sensei serve` launches | prebuilt bundle / installer, or source build |
| `oxigraph` | the RDF store (one static binary, from the upstream project) | bundled in the prebuilt download (or fetched by the source installer) |

> **Honest dependency note:** Sensei is not a single zero-dependency binary.
> It needs the Oxigraph store binary — but every prebuilt download ships it in
> the bundle, so in practice there's nothing extra to fetch: no Docker, no
> services, no database to administer.

## Install paths

### One-line installer (recommended)

Prebuilt, self-contained (`sensei` + `awareness-graph` + `awareness-mcp` +
`oxigraph`) — no Go toolchain, no Docker. Detects your platform, downloads and
checksum-verifies the matching release, and puts the binaries on your PATH.

```bash
# Linux / macOS
curl -fsSL https://raw.githubusercontent.com/globulario/sensei/main/install.sh | sh
```
```powershell
# Windows (PowerShell)
irm https://raw.githubusercontent.com/globulario/sensei/main/install.ps1 | iex
```

`SENSEI_VERSION=v1.1.1` pins a release; `SENSEI_PREFIX=…` changes the target dir.

### Package manager

```bash
brew install globulario/tap/sensei     # Homebrew — macOS (Apple Silicon), Linux
```
```powershell
winget install Globulario.Sensei       # winget — Windows
```

`brew upgrade` / `winget upgrade` update in place.

### Prebuilt tarball (grab it yourself)

Each [release](https://github.com/globulario/sensei/releases) ships a
self-contained `sensei-<platform>.tar.gz` for `linux-amd64`, `linux-arm64`,
`darwin-arm64`, and `windows-amd64` — the binaries (Oxigraph included) plus a
`setup.sh`:

```bash
curl -fsSL -O https://github.com/globulario/sensei/releases/latest/download/sensei-linux-amd64.tar.gz
tar xzf sensei-linux-amd64.tar.gz && cd sensei-linux-amd64 && ./setup.sh
```

### Source build (Linux / macOS)

Requires **Go1.25+**, `git`, `curl`, `python3`.

```bash
git clone https://github.com/globulario/sensei
cd sensei
./scripts/install.sh            # builds sensei + awareness-graph, fetches oxigraph → bin/
export PATH="$PWD/bin:$PATH"
```

When it finishes, `bin/` holds all three binaries.

Recommended local runtime:

```bash
bash ./scripts/install-sensei-user-services.sh
```

This installs supervised `systemd --user` services, reuses an already-healthy
Oxigraph if one exists, and otherwise starts a local Sensei-owned Oxigraph.

Ad hoc runtime remains available:

```bash
sensei serve -no-seed &            # starts oxigraph + the server, no Docker
```

See **[docs/initialize-user-services.md](docs/initialize-user-services.md)** for
the supervised path.

## Platform support

| Platform | Status | Notes |
|---|---|---|
| **Linux amd64** | ✅ prebuilt | primary CI target |
| **Linux arm64** | ✅ prebuilt | native-runner build |
| **macOS arm64 (Apple Silicon)** | ✅ prebuilt | upstream `aarch64_apple` Oxigraph bundled |
| **macOS amd64 (Intel)** | ⚠️ source only | no prebuilt bundle; build from source |
| **Windows amd64** | ✅ prebuilt | `sensei.exe` runs natively; the *local* pre-edit enforcement hooks are bash, so use Git Bash/WSL for those |

Every prebuilt bundle ships the matching Oxigraph binary. For a source build, the
installer maps your `uname` to the right upstream Oxigraph asset automatically
(`scripts/fetch-oxigraph.sh`). To pin a version:

```bash
scripts/fetch-oxigraph.sh 0.5.8
```

## Just the Oxigraph binary

If you already build `sensei` yourself and only need the store:

```bash
scripts/fetch-oxigraph.sh        # → bin/oxigraph (matched to your platform)
```

Or download it directly from <https://github.com/oxigraph/oxigraph/releases>
and place it on your `PATH` or in `./bin/`. `sensei serve` looks for `oxigraph`
next to the `sensei` binary, in `./bin/`, then on `PATH`.

## Docker alternative

If you prefer not to fetch a binary, `sensei serve -no-oxigraph` connects to
an Oxigraph you run yourself:

```bash
docker run -d -p 7878:7878 ghcr.io/oxigraph/oxigraph serve --bind 0.0.0.0:7878
sensei serve -no-oxigraph -no-seed &
```

## Next

→ [QUICKSTART.md](QUICKSTART.md) — zero to your first enforced briefing in 15 minutes.
