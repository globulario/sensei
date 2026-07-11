# Adopting Sensei for your repo with cold-bootstrap

*The external-repo first-user path: mine the rules your codebase already paid for
in bugs and reviews, keep the load-bearing ones, serve them scoped to your repo,
and warn agents before they repeat the mistake. End to end, honest about what is
proven and what is not.*

> **Status: experimental.** Cold-source drafting is an experiment, not a shipped
> product. It **never auto-promotes** and **never writes to a graph on its own** —
> every run is a dry-run that prints candidates for a human to review. Promotion
> is a separate, explicit, human-gated step. Keep that framing as you adopt it.

The loop you are adopting:

```
your repo's history (reverts, fix/perf/refactor commits, PR reviews)
  → candidate rules (dry-run, cited)        [cold-bootstrap]
  → you review + label                       [human]
  → promote the load-bearing ones, scoped    [sensei promote --repo]
  → serve briefings with provenance          [sensei briefing --domain]
  → warn on a bad future edit (advisory)     [sensei edit-check / gate]
```

Each step below is shown first against the committed **Caddy pilot** (real
commands, reproducible, no key), then generalized to **your repo**.

---

## 0. Prerequisites

```bash
git clone https://github.com/globulario/sensei && cd awareness-graph
go build -o /tmp/sensei ./cmd/awg
go build -o bin/awareness-graph ./golang/server   # sensei serve execs this
# plus an `oxigraph` binary on PATH or in ./bin/
```

## 1. Mine candidates from your repo (dry-run, cited)

Cold-bootstrap scans a repo's history for two corroborating signal channels — a
**commit** signal (a revert/regression scar, or a `fix:`/`perf:`/`refactor:`
conventional commit) **and** a **review** signal (a rule-stating PR comment, or a
high-engagement review thread). A theme is only drafted when both channels agree
— one signal is a guess, not evidence.

**Your repo** (echo drafter — deterministic, no API key):

```bash
git clone --depth 600 https://github.com/<owner>/<repo> /tmp/yourrepo
# --repo-slug pulls real PR review comments via gh (needs gh + jq)
/tmp/sensei cold-bootstrap --repo /tmp/yourrepo --since HEAD~500..HEAD \
  --repo-slug <owner>/<repo> \
  --drafter echo --dry-run --max 12
```

For a **quality** read (load-bearing rate, not just triangulation), use a real
drafter. There are two, and they run the *identical* cage/grounding — they differ
only in how the model is reached:

```bash
# (a) --drafter llm — a Console API key exported in the environment
#     (never pasted into a chat or a command line):
/tmp/sensei cold-bootstrap --repo /tmp/yourrepo --since HEAD~500..HEAD \
  --repo-slug <owner>/<repo> --drafter llm --dry-run --max 12

# (b) --drafter claude-cli — no API key; uses the locally-installed, already
#     authed Claude Code CLI (e.g. a Claude subscription login). Mirrors the
#     Globular ai-executor strategy: the authed CLI is the LLM backend.
/tmp/sensei cold-bootstrap --repo /tmp/yourrepo --since HEAD~500..HEAD \
  --repo-slug <owner>/<repo> --drafter claude-cli --dry-run --max 12
```

`--drafter claude-cli` deliberately strips `ANTHROPIC_API_KEY` /
`ANTHROPIC_AUTH_TOKEN` from the CLI subprocess so it uses its own login rather
than an (possibly invalid) inherited key. For `--drafter llm`, the endpoint and
auth are overridable for gateways/proxies: set `ANTHROPIC_BASE_URL` to redirect
the Messages API, and `ANTHROPIC_AUTH_TOKEN` to send `Authorization: Bearer …`
instead of `x-api-key`.

The run prints a **funnel** (signals → themes → triangulated → drafted →
accepted) and a **scoring sheet** — one row per candidate, with its citations.
Nothing is written.

> **Citation cage.** Every drafted candidate must cite only evidence present in
> its bundle, and each citation must resolve on disk/git. Uncited or fabricated
> drafts are rejected before they reach the scoring sheet — so a candidate you
> see is anchored to real evidence.

## 2. Review and label (the human gate)

For each candidate on the scoring sheet, assign one label:

- **load-bearing** — a real architectural rule a senior engineer would protect.
  The win condition.
- **shallow** — true but generic/obvious (style, "validate input"). Not a rule.
- **wrong** — misstates the architecture, or the cited evidence doesn't support
  it. Keep this rate near zero.
- **duplicate** — restates an existing rule.

**Go / no-go:** adopt the drafter for your repo only if **≳30%** of accepted
candidates are *load-bearing* **and** the *wrong* rate is low. If the output is
mostly shallow/duplicate/wrong, the knowledge genuinely lives in heads, not
derivable signals — treat Sensei as authored-rules-only for that repo.

(Worked numbers exist: on Caddy and etcd, load-bearing ran high with **0
fabricated citations**; see [`milestone-cold-bootstrap-v0.md`](milestone-cold-bootstrap-v0.md).)

## 3. Promote the load-bearing ones — scoped to your repo

Promotion is explicit and human-gated. A reviewed candidate (with provenance:
repo, domain, citations, review label) is promoted into a **domain-scoped** graph
keyed to your repo — never into anyone else's.

The committed **Caddy pilot** is the reference shape. Reproduce it end to end:

```bash
bash pilot/caddy/demo.sh
```

It promotes one reviewed Caddy candidate (`sensei promote --repo
github.com/caddyserver/caddy …`), loads it into an **isolated** Oxigraph (never
your production store), and proves the serving + isolation below. See
[`pilot/caddy/README.md`](../pilot/caddy/README.md) for the provenance contract
and [`cold-bootstrap-demo.md`](cold-bootstrap-demo.md) for the verified
command-by-command walkthrough.

For **your repo**, mirror `pilot/caddy/`: put reviewed candidates under
`pilot/<repo>/candidates/`, then:

```bash
/tmp/sensei promote --repo github.com/<owner>/<repo> <candidate-id>
```

which writes the domain-tagged rule and loads **only** that pilot graph. No bulk
promotion, no auto-promotion.

## 4. Serve briefings — scoped, with provenance

```bash
/tmp/sensei briefing --addr localhost:10120 \
  --file <a real file the rule protects> \
  --domain github.com/<owner>/<repo>
```

The briefing returns the rule **and a compact provenance block** (repo · origin ·
review label · bundle · commit range · citations) so an agent can see *why* a
foreign rule should be trusted. A query that spans two domains with no `--domain`
**fails closed** rather than mixing repos.

## 5. Warn on a bad future edit (advisory)

If a promoted rule carries a `detect` block, `edit-check` warns when a proposed
edit violates it — **advisory only, never a block, never a code change**:

```bash
/tmp/sensei edit-check --addr localhost:10120 \
  --file <the protected file> --domain github.com/<owner>/<repo> \
  --content '<the proposed edit>'
```

And `sensei gate --diff <range> --domain …` runs the same check over a git diff as a
**dry-run** report (always exits 0). See
[`hard-gate-design.md`](hard-gate-design.md) for how this could later graduate to
a real merge gate — by design, not by default.

---

## What this is — and is NOT

- **Is:** a human-gated way to extract the laws your repo already paid for, serve
  them scoped, and surface them to agents before they repeat the mistake.
- **Is NOT:** autonomous truth. A drafter proposes; a human promotes. No
  auto-promotion, no bulk promotion, no hard blocking, no CI enforcement.
- **Not SaaS-proven** across all languages — quality-validated on Go (Caddy, etcd)
  and TypeScript (vite), and triangulation-validated (echo) on Rust (tokio) and
  Python (pydantic); a low-activity repo (flask) triangulated only doc themes, so
  the limiter is repo density, not language — treat each new repo as its own go/no-go.
- **Foreign-repo rules never leak.** Domain scope keeps your repo's rules out of
  every other repo's briefings, and vice versa (shared meta-principles excepted).

## Where to look next

- [`cold-bootstrap-demo.md`](cold-bootstrap-demo.md) — the verified Caddy
  walkthrough (commands + expected output).
- [`milestone-cold-bootstrap-v0.md`](milestone-cold-bootstrap-v0.md) — what v0
  proved and what it does not claim.
- [`pilot/caddy/README.md`](../pilot/caddy/README.md) /
  [`pilot/etcd/README.md`](../pilot/etcd/README.md) — the pilot provenance
  contracts (caddy: verified; etcd: transcript-derived candidates, not promoted).
- [`hard-gate-design.md`](hard-gate-design.md) — the warning → blocking design.
- [`coldsource-grounding-design.md`](coldsource-grounding-design.md) — the
  draft → **accept** design: verifying a cited invariant is actually encoded
  before a candidate is trusted (proposal, not implemented).
