#!/usr/bin/env bash
# check-graph-equivalence.sh — the H-B migration equivalence check.
#
# Run AFTER wiring publication to the admission decision. Proves that replacing
# pathname filtering with signed admission changed the REASON identities are
# published, not WHICH ones — apart from one deliberate, enumerated repair.
#
#   G_after == G_before  MINUS  typed governed subjects whose identities appear
#                               in docs/awareness/dangling_refs_baseline.tsv
#
# Those stubs exist only because another node's relation list points at them; no
# id: declares them, so they are not admitted knowledge. Dropping them is the
# repair. Any OTHER difference means the wiring changed real authority and must
# be investigated, never re-signed.
# See decision.sensei.dangling_reference_stubs_are_not_admitted_knowledge.
#
# Usage: check-graph-equivalence.sh <before.nt> <after.nt>
# Exit:  0 equivalent-modulo-stubs; 1 unexpected difference; 2 usage/tool error.
set -euo pipefail

AG="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BEFORE="${1:-}"; AFTER="${2:-}"
[[ -f "$BEFORE" && -f "$AFTER" ]] || { echo "usage: $0 <before.nt> <after.nt>" >&2; exit 2; }
BASELINE="$AG/docs/awareness/dangling_refs_baseline.tsv"
[[ -f "$BASELINE" ]] || { echo "error: missing $BASELINE" >&2; exit 2; }

python3 - "$BEFORE" "$AFTER" "$BASELINE" <<'PY'
import re, sys
before_p, after_p, baseline_p = sys.argv[1], sys.argv[2], sys.argv[3]

def triples(path):
    with open(path, encoding="utf-8", errors="replace") as fh:
        return {line.strip() for line in fh if line.strip() and not line.startswith("#")}

# The seed carries its own digest marker; it necessarily differs and is not evidence.
marker = re.compile(r"seedBuild/sha256-")
b = {t for t in triples(before_p) if not marker.search(t)}
a = {t for t in triples(after_p) if not marker.search(t)}

allowed = {l.split("\t")[1].strip() for l in open(baseline_p, encoding="utf-8") if "\t" in l}
subject = re.compile(r"^<https://globular\.io/awareness#[A-Za-z]+/([^>]+)>")

def stub_only(ts):
    """Triples whose SUBJECT is an accepted dangling reference."""
    out = set()
    for t in ts:
        m = subject.match(t)
        if m and m.group(1) in allowed:
            out.add(t)
    return out

removed, added = b - a, a - b
unexpected_removed = removed - stub_only(removed)

print(f"triples before        : {len(b)}")
print(f"triples after         : {len(a)}")
print(f"removed               : {len(removed)}")
print(f"  accepted stub drops : {len(removed) - len(unexpected_removed)}")
print(f"  UNEXPECTED removals : {len(unexpected_removed)}")
print(f"ADDED (must be zero)  : {len(added)}")

fail = False
for t in sorted(unexpected_removed)[:10]:
    print("  - removed:", t[:150]); fail = True
for t in sorted(added)[:10]:
    print("  + added  :", t[:150]); fail = True

if len(unexpected_removed) or len(added):
    print("\n✗ NOT EQUIVALENT — the admission wiring changed real authority.")
    print("  Investigate the difference. Do not re-sign around it.")
    sys.exit(1)
print("\n✓ equivalent modulo accepted dangling-reference stubs")
print("  Same graph, different authority.")
PY
