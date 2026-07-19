# Sensei Adoption Plan

A step-by-step, follow-it-in-order plan to give Sensei visibility to its real
audience: **developers using AI coding agents** (Claude Code, Cursor, Cline,
Continue) and the teams around them. Two products to surface: the **AWG runtime
/ CLI** and the **VS Code extension**.

**Guiding principle:** influencers are the *second* domino, not the first. They
amplify momentum; they don't create it. So we build signal-generating artifacts
first (demo, writeup), launch to *warm* audiences who already believe the
premise, and only then reach the big amplifiers — who convert far more easily
once there's a ripple to ride.

Status legend: `[ ]` todo · `[~]` in progress · `[x]` done

---

## Phase 0 — Close the demo gap (the prerequisite for everything)

Nothing below works without a way to *see* the value in under a minute. This is
the single highest-leverage work. Do it first.

- [ ] **0.1 — Record a 30–45s VS Code extension GIF/video.**
  Open a real file in a bootstrapped repo → show the "This File" panel surfacing
  invariants / forbidden fixes / failure modes → show the Project Dashboard.
  This closes the `TODO` at `editor/vscode/README.md:267` and the missing
  dashboard screenshot. Use the Caddy graph or `examples/payment-cold-start`.
  *Done when:* an MP4 + GIF exist in `media/` and are embedded at the top of
  both the root `README.md` and `editor/vscode/README.md`.

- [ ] **0.2 — Record a 60–90s CLI "agent gets it wrong, then gets it right" clip.**
  The core story: agent about to make a forbidden fix → `sensei` briefing stops
  it. This is the emotional hook. `sensei demo` already stands up the stack on
  throwaway ports — build the clip around it.
  *Done when:* asciinema or MP4 embedded near the top of the README.

- [ ] **0.3 — Add a one-screen "why you care" narrative above the fold.**
  The README pitch is good but jumps to mechanics. Add 3–4 sentences: the
  problem (agents edit code they don't understand), the cost (they repeat fixed
  bugs / break invariants), the fix (repo-aware briefing before the edit).
  *Done when:* a reader who has never heard of Sensei understands the value in
  15 seconds of scrolling.

**Exit criteria for Phase 0:** a stranger landing on the repo sees a moving
picture of the value within one scroll. Do not proceed until this is true.

---

## Phase 1 — Warm audiences (people who already believe the premise)

These communities don't need convincing that "agents need better context." Low
risk, honest feedback, seeds early adopters. Post as a *builder sharing*, not a
marketer selling.

- [ ] **1.1 — Claude Code / MCP community.** Share in the Anthropic MCP
  discussions / Claude Code community channels. Lead with the MCP angle:
  "an MCP server that gives your agent architectural memory before it edits."
- [ ] **1.2 — Cursor / Cline / Continue.dev communities.** These users already
  pay for better editor context. Lead with the **VS Code extension** demo GIF.
- [ ] **1.3 — r/ChatGPTCoding, r/LocalLLaMA (tooling threads), AI-agent Discords.**
  Lead with the "forbidden fix" clip from 0.2.
- [ ] **1.4 — Collect the feedback.** File every "how do I…" and "it broke when…"
  as issues. Friction you hear here is what would kill an HN launch later.

**Exit criteria:** 3–5 real external users have run `sensei demo` or installed
the extension, and you've fixed the top 2–3 friction points they hit.

---

## Phase 2 — Owned content (your credibility artifact)

The thing that makes step-4 influencers say yes. Honest, technical, specific —
this is the asset Simon Willison-type readers forward.

- [ ] **2.1 — Write the launch blog post:** *"Giving coding agents architectural
  memory — what worked and what didn't."* Use real material you already have:
  the Caddy case study (`docs/case-studies/caddy.md`), the eval harness results,
  the edit-guard dogfooding. **Include the limitations** — honesty is the whole
  credibility play with this audience.
- [ ] **2.2 — Include the before/after.** One concrete agent interaction without
  Sensei vs. with it. Screenshots or transcript. This is the shareable core.
- [ ] **2.3 — Publish on Medium (paid account).** Two required settings for a
  dev launch: (a) **opt the story OUT of the metered paywall** ("make free to
  read") so HN/Reddit readers never hit a member-only wall; (b) put any real
  code/config in **embedded GitHub Gists** (Medium's native code blocks have no
  syntax highlighting). Keep the before/after agent transcript as a Gist. Later,
  if you add a GitHub Pages landing (closes the "no landing page" gap), set
  Medium's **canonical URL** to your own domain so SEO credit accrues to you.
- [ ] **2.4 — Turn the post into a Twitter/X + LinkedIn thread** with the GIF
  from 0.1 as the lead media.

**Exit criteria:** one URL you'd be proud to have a skeptical senior engineer read.

---

## Phase 3 — Public launch (manufacture the ripple)

Now create the signal that influencers ride. Sequence matters — do these within
a tight window so momentum compounds.

- [ ] **3.1 — Show HN.** Title like *"Show HN: Sensei – architectural memory for
  AI coding agents (MCP + VS Code)."* Link the repo, lead the comment with the
  demo GIF and the honest "what it doesn't do yet." Be present in the thread all
  day to answer.
- [ ] **3.2 — r/programming + r/coding.** Different framing from HN — lead with
  the case study / concrete result, not the pitch.
- [ ] **3.3 — Product Hunt (optional).** Only if you can rally the Phase-1 early
  adopters to show up day-one; a flat launch hurts more than helps.
- [ ] **3.4 — Submit the extension to VS Code "trending"/newsletters** and any
  MCP-server registries/awesome-lists (awesome-mcp, etc.).

**Exit criteria:** at least one thread with real discussion (not just upvotes) —
that's the "momentum" the next phase needs.

---

## Phase 4 — Amplifiers (the second domino)

Only after Phase 3 produced a ripple. Ranked by realistic odds for *this* tool.
Never ask "will you review my project" — send the *artifact* and let it land.

- [ ] **4.1 — Simon Willison** *(highest-signal, most reachable).* Polite, short
  note on Mastodon/Bluesky with the working demo link and the honest framing,
  *or* just make sure your Phase-2 post is discoverable — he finds good things.
  Rewards novelty + honesty about limitations.
- [ ] **4.2 — swyx / Latent Space.** Don't cold-DM. Get into the AI Engineer /
  Latent Space community, share the *technique* (grounding agents with a graph)
  as a contributor. The conference/podcast is actively looking for this.
- [ ] **4.3 — ThePrimeagen.** You don't pitch him — you get Sensei to the front
  of HN/Reddit (Phase 3) and he may react on stream. The polyglot/backend
  codebase-awareness angle is a genuine fit for his audience.
- [ ] **4.4 — Fireship / Theo.** Amplifiers of existing trends, not discoverers.
  Only worth it once 4.1–4.3 or Phase 3 created traction they can ride.

**Note on the teachable angle:** the "repo awareness can be taught" value you
flagged fits **swyx and Prime best** (they turn tools into lessons for their
audiences) and, secondarily, a written tutorial/course *you* publish that an
educator could point to. That tutorial is a Phase-2 extension, not a cold ask.

---

## Phase 5 — Sustain & measure

- [ ] **5.1 — Instrument what you can.** GitHub stars, extension install count
  (Marketplace dashboard), release download counts, `sensei demo` runs if you
  add opt-in telemetry.
- [ ] **5.2 — Ship a second case study** on a well-known OSS repo (like Caddy but
  different domain) every few weeks — reproducible proof compounds.
- [ ] **5.3 — Convert issues/feedback into public "we heard you" changelog notes.**
  Visible responsiveness is its own adoption driver.
- [ ] **5.4 — Re-run outreach** on each meaningful release with a fresh artifact.

---

## The one-line version

**Demo GIF (0.1) → warm communities (Phase 1) → honest writeup (Phase 2) →
Show HN / Reddit (Phase 3) → then and only then, amplifiers (Phase 4).**

Do them in order. Skipping to Phase 4 before Phase 0/3 is the most common way
this fails.
