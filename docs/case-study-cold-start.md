# Case study: turning a codebase into an agent-ready system

*A short, honest account of why Sensei exists and one piece of proof that it works.*

## The problem: agents write code but lose the architecture

AI coding agents are good at the local task — write the function, fix the
bug, add the endpoint. They are bad at the thing senior engineers do without
thinking: holding the *architecture* in mind. Which state is authoritative.
Which "obvious" fix reintroduces a past outage. Which file is load-bearing.

That knowledge is almost never written down in a form a machine can use. It
lives in people's heads, in Slack threads, in the scar tissue of old
incidents. So a capable agent makes a reasonable-looking change that violates
a rule nobody told it about, and the codebase drifts a little further from its
own design. Multiply that across thousands of AI-assisted edits and the
architecture erodes — quietly, then all at once.

This is not a model-quality problem. A smarter model with the same missing
context makes the same class of mistake, more convincingly.

## The insight: the missing layer is machine-readable project memory

The fix isn't a better prompt or a bigger context window. It's a **durable,
queryable layer of project memory** that sits next to the code and answers one
question at the moment it matters:

> *"What do I need to know before editing this file?"*

Not documentation a human might read someday — a graph an agent queries
automatically, before the edit, every time.

## The solution: Sensei

You write your project's intent, invariants, failure modes, forbidden fixes,
and high-risk paths as small YAML files in your repo. Sensei compiles them into a
graph (RDF, stored in a local Oxigraph instance) and serves briefings over a
small gRPC API and CLI. An editor integration or a CI step asks for a briefing
before code changes; the agent receives the rules that apply.

Three deliberate properties:

- **The project owns the graph.** It's YAML in your repo, versioned with the code.
- **It's tool-agnostic.** CLI + local server; works with Claude Code, Cursor, Codex, CI, or a shell.
- **It's standalone.** No cloud, no account, no Globular. `-no-seed` keeps the graph entirely yours.

## The proof: a stranger's first run, on a machine we don't control

The risk with any "it works" claim is that it only works on the author's
laptop. So the cold-start path is exercised on a **clean GitHub Actions
runner** — fresh OS, no prior state — as a required CI gate:

1. fetch the Oxigraph binary for the platform
2. scaffold a throwaway project (`sensei init`)
3. write one source file and one invariant linked to a universal principle
4. `sensei serve -no-seed` → `sensei build` → `sensei briefing`

It asserts two things, and both must hold for CI to go green:

- **Positive:** the project's own invariant
  (`payments.paid_state_requires_processor_confirmation`) resolves in the
  briefing for the file it protects.
- **Negative:** Globular's seeded invariants **do not leak** into a
  cold-start project — a Globular-only invariant returns *not found*, and no
  Globular-specific strings appear in the output.

The negative assertion is the one that matters for trust: it proves a stranger
gets *their* graph, not ours.

Alongside it, three standing gates keep the kit honest: the principle pack
cannot drift from its canonical source, the docs cannot misstate the principle
count, and the embedded seed cannot fall out of sync with its YAML. Each of
these was added because it caught a real drift — including drift introduced
while building this very kit.

## Why this matters

A codebase with an awareness graph is a different kind of artifact. Its rules
are no longer trapped in people; they are explicit, versioned, and reachable
by whatever tool touches the code. New agents and new engineers inherit the
architecture instead of rediscovering it by breaking it.

Sensei turns a codebase into an **agent-ready system** — one that can absorb a
high volume of AI-assisted change without losing the design that makes it work.

---

*Status: early. The cold-start path is validated on Linux and macOS with Go
installed. Try it and [open an issue](https://github.com/globulario/sensei/issues)
where it breaks.*
