# Contributing to Sensei

Thanks for your interest in Sensei. This project turns a codebase's architectural
knowledge — invariants, failure modes, forbidden fixes, intent — into a graph an
AI agent consults before it edits and a CI gate enforces. Contributions that make
that loop more trustworthy are very welcome.

## Developer Certificate of Origin (DCO)

We use the [Developer Certificate of Origin](https://developercertificate.org/)
instead of a CLA. It is a lightweight statement that you wrote, or have the right
to submit, the code you contribute.

Every commit must be signed off. Add a `Signed-off-by` line by committing with
`-s`:

```bash
git commit -s -m "your message"
```

This appends, using your real name and email:

```
Signed-off-by: Jane Developer <jane@example.com>
```

By signing off you agree to the DCO (reproduced at the link above). Pull requests
whose commits are not signed off cannot be merged.

## Getting set up

Requires **Go 1.23+** (Linux or macOS; Windows is not yet a validated path).

```bash
git clone https://github.com/globulario/sensei.git
cd awareness-graph
./scripts/install.sh          # builds sensei + server, fetches the oxigraph binary
export PATH="$PWD/bin:$PATH"
```

## Building and testing

```bash
go build ./...                # compile everything
go test ./...                 # run the test suite
go vet ./...                  # static checks
gofmt -l .                    # must print nothing — run `gofmt -w` to fix
```

Please keep `go build`, `go vet`, and `go test ./...` green, and the tree
`gofmt`-clean, before opening a PR.

### The awareness seed

The server ships an embedded awareness graph built from this repo's own corpus.
If you change the corpus under `docs/awareness/`, regenerate the seed:

```bash
scripts/build-awareness-graph-self.sh          # rebuild seed + transaction stamp
scripts/build-awareness-graph-self.sh --check  # CI: fail if the committed seed drifted
```

## Making changes

- **Match the surrounding code** — naming, comment density, and idiom.
- **Test what you change.** New behavior needs a test; a bug fix needs a
  regression test.
- **Grow the corpus honestly.** New invariants, failure modes, and forbidden
  fixes should be grounded in a real incident or a concrete design rule, not
  generic advice. Absence of knowledge must never read as safety.
- **One focused change per PR.** Keep diffs reviewable.

## License

By contributing, you agree that your contributions are licensed under the
[Apache License 2.0](LICENSE), the same license that covers this project.
