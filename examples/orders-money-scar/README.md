# Cold-start proof: the money scar

The smallest **external** proof that Sensei is a product, not a Globular-only tool.
A fresh, non-Globular Go service + **one** human-authored rule, run through the
whole cold-start loop, ending in the moment that matters: a briefing that
surfaces a critical rule to an agent *before* it edits the file — and a test
that proves the unguided change loses money.

> **The product claim.** Sensei does **not** make architectural decisions. It
> surfaces **human-approved project truth** to an AI agent (or to you) **before**
> the code is edited. The human writes the rule; Sensei makes sure the rule is seen
> at the moment of the edit.

No Globular. No cluster. No database to stand up by hand. No network.

## The rule (the scar)

`docs/awareness/invariants.yaml` encodes one painful, non-obvious bug class:

> **`money.amounts_are_integer_minor_units`** — monetary amounts are integer
> minor units (cents); **never float**. `float64(amount) * rate` silently loses
> fractional cents that accumulate into real money across orders.

It protects `pkg/orders/total.go`, names a forbidden fix
(`convert_money_to_float_for_discount_or_tax`) and a required test, and grounds
in the portable meta-principle `meta.identity_computation_must_be_invariant`.

## Run it

```sh
# from a clone of awareness-graph (needs: go, python3, and an oxigraph binary —
# repo bin/oxigraph or on PATH; CI: scripts/fetch-oxigraph.sh). No network.
examples/orders-money-scar/run.sh
```

`run.sh` is hermetic: it builds `awg` + the server, copies this example into a
throwaway temp dir, and runs the full loop there — **the committed example is
never mutated**. Override ports with `AWG_DEMO_GRPC_PORT` / `AWG_DEMO_OXI_PORT`.

## What it proves (and the expected output)

```
==> [1] awg init (portable pack + scaffold)
    portable pack: 95 principles; scar present
==> [3] awg build -strict
  total: 803 triples, validated
  Build complete.
==> [4] no Globular seed leak
    Globular-only invariant: not found
==> [5] briefing surfaces the critical scar (task: add a 10% discount)
    briefing surfaced: [critical] money.amounts_are_integer_minor_units (never float)
==> [6-8] required test: float 899c vs integer 900c
    total_test.go:28: SCAR PROVEN — float discount = 899 cents vs exact integer = 900 (silent 1-cent divergence)
--- PASS: TestOrderTotal_DiscountStaysExactInteger
PASS — cold-start money-scar proof green
```

| # | Step | Proof |
|---|------|-------|
| 1 | `awg init` | installs the ~95-principle portable pack + scaffolds awareness files |
| 2 | human scar | `money.amounts_are_integer_minor_units` protects `total.go` |
| 3 | `awg build -strict` | the project graph validates (803 triples) |
| 4 | no seed leak | a Globular-only invariant resolves **not found** |
| 5 | task briefing | "add a 10% discount" surfaces the **critical** money rule |
| 6 | unguided fix | `Money(float64(total) * 0.9)` → **899 cents** |
| 7 | Sensei-guided fix | `total - (total*pct)/100` → **900 cents** |
| 8 | required test | the two diverge — a silent 1-cent loss, caught |

## The agent moment

For the task *"add a 10% discount to order totals"* on `pkg/orders/total.go`:

- **Without Sensei context**, the obvious fix is float — and wrong:
  ```go
  return Money(float64(total) * 0.9) // → 899 cents on a $9.99 order
  ```
- **With the Sensei briefing** (`[critical] money is integer cents; never float`),
  the edit stays exact:
  ```go
  return total - (total*Money(percentOff))/100 // → 900 cents, integer rule
  ```

`go test` (`TestOrderTotal_DiscountStaysExactInteger`) runs both and proves the
divergence. Sensei didn't choose the fix — it made the rule impossible to miss.

## Files

```
run.sh                              # hermetic proof runner
go.mod  pkg/orders/total.go         # tiny fixture service (safe integer money)
        pkg/orders/total_test.go    # float-vs-integer proof test
docs/awareness/invariants.yaml      # the human-authored money scar
.gitignore                          # excludes init-generated files (pack, CLAUDE.md, .awg/)
```

`awg init` generates the principle pack, `CLAUDE.md`, the other awareness files,
and `.awg/` — all gitignored, so this directory stays minimal and deterministic.

## Cleanup

`run.sh` cleans up after itself (temp dir + serve process removed on exit). If
you ran the steps by hand, stop the background `awg serve` and remove any
generated files: `git clean -xfd` inside your throwaway copy.
