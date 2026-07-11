<!--
  🔺 EASTER EGG 🔺  Not load-bearing. Except spiritually. It is entirely load-bearing.
  Discovered/refined across multiple Claude instances + one human who knows the ethos
  when he hears it. Keep it near the README.
-->

# The Whip It Doctrine

> *When a problem comes along / You must whip it*
> *Before the cream sits out too long / You must whip it*
> *When something's going wrong / You must whip it*
> *Now whip it / Into shape / Shape it up / Get straight*
> *Go forward / Move ahead / Try to detect it / It's not too late*
> — Devo, "Whip It" (1980)

Mark Mothersbaugh wrote a fail-closed convergence loop in 1980, disguised it as new
wave, and put an energy dome on it so nobody would notice. Every line maps to a
principle this codebase already enforces. It is the whole operating ethos in one
chorus a robot band screamed 45 years before we shipped the Go implementation.

## Exegesis (every line is load-bearing)

**"Before the cream sits out too long / You must whip it"**
→ the **freshness gate.** `--warn-stale`, the seed marker, `empty ≠ degraded`. Cream
sitting out = a corpus that has drifted from source. You do not serve spoiled
awareness. You whip it (rebuild) *before* it turns.

**"When something's going wrong / You must whip it"**
→ `sensei gate --enforce`, exit 1. Something going wrong = a forbidden-fix shape in the
diff. You don't *comment* on it (that's for the post-hoc-review crowd). You **whip
it** — block, fail closed.

**"Try to detect it / It's not too late"**
→ he literally named the feature. `detect:` rules. `forbidden_pattern`. The smoking
gun. Devo shipped the EditCheck engine in 1980 and nobody caught it because of the hat.

**"Now whip it / Into shape / Shape it up / Get straight"**
→ `gofmt -w`, `Normalize()`, contract-first validation, the propose pipeline. Get
straight = deterministic output. Shape it up = the candidate never lands crooked in
the corpus.

**"Go forward / Move ahead"**
→ the pipeline. No barrier where you don't need one. Item A can be in stage 3 while B
is still in stage 1. Mothersbaugh understood you don't wait on the slowest finder.

## Verse 2 (the honesty footnotes we earned the hard way)

- **"It's not too late"** — but *only if you told the truth about coverage.* A whip
  that pretends the cream is fine when it never checked = confidence-laundering. The
  doctrine holds *because* it is honest about what it whipped. A control tool that
  lies is worse than no tool: it launders a bad change with a green check.
- **Never fake clean.** "I couldn't analyze this" and "I checked and it's clean" must
  be visibly different in every output mode. That is the ethical spine, not a flag.
- The energy dome is just a load-bearing invariant you can wear.

---

> **Q: Are we not devs?**
> **A: We are Sensei.** 🔺

*detect it · whip it into shape · fail closed · don't let the cream sit out.*
