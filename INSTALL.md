# Installing Sensei

Sensei has three pieces:

| Piece | What it is | How you get it |
|---|---|---|
| `sensei` | the CLI (init, build, serve, briefing, validate, audit) | source build or Linux `amd64` prebuilt bundle |
| `awareness-graph` | the gRPC server `sensei serve` launches | source build or Linux `amd64` prebuilt bundle |
| `oxigraph` | the RDF store (one static binary, from the upstream project) | fetched by the installer |

> **Honest dependency note:** Sensei is not a single zero-dependency binary.
> It needs the Oxigraph store binary. The installer fetches it for you, so
> in practice the cost is one extra download — no Docker, no services, no
> database to administer.

## Install paths

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
bash ./scripts/install-awg-user-services.sh
```

This installs supervised `systemd --user` services, reuses an already-healthy
Oxigraph if one exists, and otherwise starts a local Sensei-owned Oxigraph.

Ad hoc runtime remains available:

```bash
sensei serve -no-seed &            # starts oxigraph + the server, no Docker
```

See **[docs/initialize-user-services.md](docs/initialize-user-services.md)** for
the supervised path.

### Prebuilt Linux bundle

For `linux/amd64`, releases now publish a local runtime tarball:

- `awg-local_<version>_linux_amd64.tgz`
- `awg-local_<version>_linux_amd64.tgz.sha256`

It contains:

- `bin/sensei`
- `bin/awareness-mcp`
- `bin/awareness-graph`
- `scripts/fetch-oxigraph.sh`
- `scripts/install-awg-user-services.sh`

Install shape:

```bash
tar -xzf awg-local_<version>_linux_amd64.tgz
cd extracted-dir
export PATH="$PWD/bin:$PATH"
bash ./scripts/fetch-oxigraph.sh
bash ./scripts/install-awg-user-services.sh --skip-build
```

This path does not require Go. It is the first prebuilt workstation/CI
distribution path. Other supported platforms still use the source-build path
above.

## Platform support

| Platform | Status | Notes |
|---|---|---|
| **Linux amd64** | ✅ supported | primary CI target |
| **Linux arm64** | ✅ supported | upstream Oxigraph ships an aarch64 build |
| **macOS arm64 (Apple Silicon)** | ✅ supported | source build + upstream `aarch64_apple` Oxigraph |
| **macOS amd64 (Intel)** | ✅ supported | source build + upstream `x86_64_apple` Oxigraph |
| **Windows amd64** | 🚧 coming next | binaries build; the enforcement hooks are bash and need a PowerShell port |

The installer maps your `uname` to the matching upstream Oxigraph asset
automatically (`scripts/fetch-oxigraph.sh`). To pin a version:

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
