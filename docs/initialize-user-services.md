# Local User Services

Use `systemd --user` when you want a supervised local Sensei stack instead of ad
hoc background commands.

If you already have a stable service-managed Oxigraph on `127.0.0.1:7878`,
reuse it and supervise only `awareness-graph`.

## Install

```bash
cd /home/dave/Documents/github.com/globulario/sensei
./scripts/install-awg-user-services.sh
```

Auto behavior:

- if `http://127.0.0.1:7878/query` is already healthy, the script reuses that
  Oxigraph and installs only `awg-awareness-graph.service`
- otherwise it installs both:
  - `awg-oxigraph.service`
  - `awg-awareness-graph.service`

To force reuse of an existing service-managed Oxigraph:

```bash
./scripts/install-awg-user-services.sh --reuse-existing-oxigraph
```

To force Sensei to own both local user services:

```bash
./scripts/install-awg-user-services.sh --no-reuse-existing-oxigraph
```

The `awareness-graph` unit starts with `-require-store=true` and performs a
pre-start check against the configured Oxigraph endpoint, so it fails closed if
the backend is unavailable. When Sensei installs its own local Oxigraph unit, the
service also depends on `awg-oxigraph.service`.

## Verify

```bash
./bin/awg metadata
```

Expected:

```text
Freshness state:     current
Seed state:          current
```

## Operate

If you reused an existing Oxigraph:

```bash
systemctl --user restart awg-awareness-graph.service
systemctl --user stop awg-awareness-graph.service
systemctl --user status awg-awareness-graph.service
journalctl --user -u awg-awareness-graph.service -n 100 --no-pager
```

If Sensei installed both local user units:

```bash
systemctl --user restart awg-oxigraph.service awg-awareness-graph.service
systemctl --user stop awg-awareness-graph.service awg-oxigraph.service
systemctl --user status awg-oxigraph.service awg-awareness-graph.service
journalctl --user -u awg-oxigraph.service -u awg-awareness-graph.service -n 100 --no-pager
```
