#!/usr/bin/env bash
# awg-bootstrap-foreign.sh — canonical foreign-repo STRUCTURAL bootstrap.
#
# Ingests a foreign repo's structural facts (components, file anchors, import
# dependencies) into an ISOLATED, domain-scoped AWG graph and proves that
# file-level context answers — so `awg impact/briefing --domain <repo>` returns
# real structural context for that repo, with no leakage to/from other domains.
#
#   extract (import-scan per language)
#     → awg build --repo <domain>   (tags every structural node to the domain)
#     → load into an ISOLATED Oxigraph (never the shared :7878 / live store)
#     → validate: impact for an extracted file is NON-EMPTY and domain-scoped
#
# Optimization: the isolated store is cacheable. If the foreign checkout HEAD,
# target domain, selected languages, and awg binary fingerprint match the last
# bootstrap recorded in DATA_DIR/.bootstrap-meta, we reuse the existing store
# instead of rescanning and rebuilding. That keeps extracted structural value
# "earned once" per repo revision while still invalidating on code/tool drift.
#
# Scar/intent cold-bootstrap is intentionally NOT here — it is optional
# enrichment layered on top (see `awg cold-bootstrap`), never the only context.
#
# Usage:
#   scripts/awg-bootstrap-foreign.sh <checkout_dir> <domain> [probe_file]
# e.g.
#   scripts/awg-bootstrap-foreign.sh /tmp/cli github.com/cli/cli api/client.go
#
# Env (all have safe isolated defaults — never the live ports):
#   AWG_BIN   awg binary (default: ./awg, built if missing)
#   PORT      isolated gRPC port      (default 10131; MUST NOT be 10120)
#   OXI_PORT  isolated Oxigraph port  (default 7889;  MUST NOT be 7878)
#   DATA_DIR  isolated store dir      (default /tmp/awg-foreign-<slug>)
#   LANGS     space-separated import-scan languages (default: auto-detect)
#   KEEP=1    leave the server running on exit (default: tear it down)
set -euo pipefail

AG_REPO="$(cd "$(dirname "$0")/.." && pwd)"
cd "$AG_REPO"

checkout="${1:?usage: awg-bootstrap-foreign.sh <checkout_dir> <domain> [probe_file]}"
domain="${2:?usage: awg-bootstrap-foreign.sh <checkout_dir> <domain> [probe_file]}"
probe="${3:-}"
slug="${domain#github.com/}"; slug="${slug//\//__}"

AWG_BIN="${AWG_BIN:-$AG_REPO/awg}"
PORT="${PORT:-10131}"
OXI_PORT="${OXI_PORT:-7889}"
DATA_DIR="${DATA_DIR:-/tmp/awg-foreign-$slug}"
OXI_URL="http://127.0.0.1:$OXI_PORT/store?default"
GEN_CACHE_DIR="${GEN_CACHE_DIR:-$DATA_DIR.generated}"
META_FILE="$GEN_CACHE_DIR/.bootstrap-meta"

# Hard isolation guards — refuse to touch the live service.
[ "$PORT" = 10120 ] && { echo "refusing PORT=10120 (live service)"; exit 2; }
[ "$OXI_PORT" = 7878 ] && { echo "refusing OXI_PORT=7878 (live Oxigraph)"; exit 2; }

[ -x "$AWG_BIN" ] || { echo "building awg…"; go build -o "$AWG_BIN" ./cmd/awg; }
export PATH="$AG_REPO/bin:$PATH"   # locate bin/oxigraph regardless of cwd

checkout_head="$(git -C "$checkout" rev-parse HEAD)"
awg_sha="$(sha256sum "$AWG_BIN" | awk '{print $1}')"

read_meta() {
  [ -f "$META_FILE" ] || return 1
  # shellcheck disable=SC1090
  source "$META_FILE"
}

write_meta() {
  mkdir -p "$GEN_CACHE_DIR"
  cat >"$META_FILE" <<EOF
BOOTSTRAP_DOMAIN='$domain'
BOOTSTRAP_CHECKOUT_HEAD='$checkout_head'
BOOTSTRAP_LANGS='${LANGS}'
BOOTSTRAP_AWG_SHA='$awg_sha'
EOF
}

lang_for_path() {
  case "$1" in
    *.go|go.mod|go.sum) echo go ;;
    *.ts|*.tsx|*.js|*.jsx|package.json|package-lock.json|pnpm-lock.yaml|yarn.lock|tsconfig.json) echo ts ;;
    *.py|pyproject.toml|requirements.txt|requirements-dev.txt|setup.py|setup.cfg) echo python ;;
    *.rs|Cargo.toml|Cargo.lock) echo rust ;;
    *) echo "" ;;
  esac
}

changed_langs_between() {
  local prev="$1" next="$2"
  local seen="" path lang
  while IFS= read -r path; do
    lang="$(lang_for_path "$path")"
    [ -n "$lang" ] || continue
    case " $seen " in
      *" $lang "*) ;;
      *) seen="$seen $lang" ;;
    esac
  done < <(git -C "$checkout" diff --name-only "$prev" "$next" 2>/dev/null || true)
  echo "$seen" | xargs
}

# --- 1. extract structural facts into a pilot dir ---------------------------
pilot="$(mktemp -d)/pilot"; mkdir -p "$pilot/generated"
trap 'rm -rf "$(dirname "$pilot")"' EXIT

detect_langs() {
  local L=""
  [ -f "$checkout/go.mod" ] && L="$L go"
  [ -f "$checkout/package.json" ] && L="$L ts"
  [ -f "$checkout/Cargo.toml" ] && L="$L rust"
  [ -n "$(find "$checkout" -maxdepth 2 -name '*.py' 2>/dev/null | head -1)" ] && L="$L python"
  echo "${L:-go}"
}
LANGS="${LANGS:-$(detect_langs)}"
cache_hit=0
reuse_reason=""
changed_langs=""
if read_meta 2>/dev/null \
  && [ "${BOOTSTRAP_DOMAIN:-}" = "$domain" ] \
  && [ "${BOOTSTRAP_LANGS:-}" = "$LANGS" ] \
  && [ "${BOOTSTRAP_AWG_SHA:-}" = "$awg_sha" ] \
  && [ -d "$DATA_DIR" ]; then
  if [ "${BOOTSTRAP_CHECKOUT_HEAD:-}" = "$checkout_head" ]; then
    cache_hit=1
    reuse_reason="exact"
  else
    changed_langs="$(changed_langs_between "${BOOTSTRAP_CHECKOUT_HEAD:-}" "$checkout_head")"
    if [ -z "$changed_langs" ]; then
      cache_hit=1
      reuse_reason="structural-equivalent"
    fi
  fi
fi

if [ "$cache_hit" = 1 ]; then
  echo "== reusing cached structural graph ($domain @ ${checkout_head:0:10}, $reuse_reason) langs:[$LANGS] =="
else
  echo "== extracting structural facts ($domain) langs:[$LANGS] =="
  mkdir -p "$GEN_CACHE_DIR"

  selective_langs="$LANGS"
  if [ -n "${BOOTSTRAP_CHECKOUT_HEAD:-}" ] \
    && [ "${BOOTSTRAP_DOMAIN:-}" = "$domain" ] \
    && [ "${BOOTSTRAP_LANGS:-}" = "$LANGS" ] \
    && [ "${BOOTSTRAP_AWG_SHA:-}" = "$awg_sha" ] \
    && [ -z "$changed_langs" ]; then
    changed_langs="$(changed_langs_between "${BOOTSTRAP_CHECKOUT_HEAD:-}" "$checkout_head")"
  fi
  if [ -n "$changed_langs" ]; then
    selective_langs="$changed_langs"
    echo "  changed language surfaces: [$changed_langs]"
  fi

  for lang in $LANGS; do
    cached="$GEN_CACHE_DIR/${lang}_import_graph.yaml"
    out="$pilot/generated/${lang}_import_graph.yaml"
    if [ -f "$cached" ]; then
      cp "$cached" "$out"
    fi
  done

  go build -o /tmp/awg-import-scan ./cmd/import-scan
  for lang in $selective_langs; do
    out="$pilot/generated/${lang}_import_graph.yaml"
    if /tmp/awg-import-scan -repo-root "$checkout" -lang "$lang" -output "$out" 2>/dev/null; then
      echo "  import-scan[$lang]: $(grep -c '^  - id:' "$out" 2>/dev/null || echo 0) components"
      cp "$out" "$GEN_CACHE_DIR/${lang}_import_graph.yaml"
    else
      echo "  import-scan[$lang]: (no output / unsupported) — skipped"
      rm -f "$GEN_CACHE_DIR/${lang}_import_graph.yaml"
    fi
  done

  for lang in $LANGS; do
    [ -f "$pilot/generated/${lang}_import_graph.yaml" ] && continue
    cached="$GEN_CACHE_DIR/${lang}_import_graph.yaml"
    [ -f "$cached" ] || { echo "  missing cached structural output for $lang — forcing full rescan next run"; continue; }
    cp "$cached" "$pilot/generated/${lang}_import_graph.yaml"
  done
fi

# --- 2. start the ISOLATED server (own ports + store) -----------------------
echo "== starting isolated AWG (gRPC :$PORT, Oxigraph :$OXI_PORT, data $DATA_DIR) =="
if [ "$cache_hit" != 1 ]; then
  rm -rf "$DATA_DIR"
fi
mkdir -p "$DATA_DIR"
MARKER_FILE="$DATA_DIR/.awg/graph-authority.json"
mkdir -p "$DATA_DIR/.awg"
"$AWG_BIN" serve --addr ":$PORT" --oxigraph-bind "127.0.0.1:$OXI_PORT" --no-seed \
    --graph-marker-file "$MARKER_FILE" \
    --home-domain "$domain" --data "$DATA_DIR" >/tmp/awg-foreign-$slug.log 2>&1 &
SERVE_PID=$!
[ "${KEEP:-0}" = 1 ] || trap 'rm -rf "$(dirname "$pilot")"; kill $SERVE_PID 2>/dev/null || true' EXIT
for _ in $(seq 1 40); do
  curl -s -m2 -o /dev/null "http://127.0.0.1:$OXI_PORT/query" --data-urlencode 'query=ASK{?s ?p ?o}' 2>/dev/null && break
  kill -0 $SERVE_PID 2>/dev/null || { echo "serve died — see /tmp/awg-foreign-$slug.log"; exit 1; }
  sleep 0.5
done

# --- 3. build with --repo (domain-tag every structural node) + load ---------
if [ "$cache_hit" = 1 ]; then
  echo "== cached structural graph already loaded for $domain =="
else
  echo "== building structural graph scoped to $domain =="
  # optional symbol layer: when a SCIP index exists for the checkout, ingest
  # per-function/method symbol nodes + reference edges into the same build input
  # so impact/briefing can answer at symbol granularity, not just component/file.
  scip_index="${AWG_SCIP_INDEX:-$checkout/index.scip}"
  if [ -f "$scip_index" ]; then
    if "$AWG_BIN" scip-ingest --scip "$scip_index" --out "$pilot/generated" --quiet; then
      echo "  scip: ingested symbol layer from $(basename "$scip_index")"
    else
      echo "  scip: ingest failed — continuing structural-only"
    fi
  fi
  "$AWG_BIN" build --input "$pilot" --repo "$domain" --source-set "pilot/$slug" \
      --store-url "$OXI_URL" --graph-marker-file "$MARKER_FILE"
  write_meta
fi

# --- 4. validate: impact for an extracted file is NON-EMPTY + scoped --------
[ -n "$probe" ] || probe="$(cd "$checkout" && git ls-files '*.go' 2>/dev/null | head -1)"
echo "== validating structural impact for: $probe (domain $domain) =="
out="$("$AWG_BIN" impact --addr "localhost:$PORT" --file "$probe" --domain "$domain" 2>&1 || true)"
echo "$out" | sed 's/^/    /'

if echo "$out" | grep -qiE '\[component\]|component\.'  || echo "$out" | grep -q 'Architecture (direct)'; then
  echo "PASS: structural context resolves for $domain — Mode C graph is real"
else
  echo "FAIL: impact returned no structural context for an extracted file — graph is INVALID"
  exit 1
fi
# Cross-domain leak guard: a foreign probe under a bogus domain must be empty.
leak="$("$AWG_BIN" impact --addr "localhost:$PORT" --file "$probe" --domain github.com/__nope__/__nope__ 2>&1 || true)"
if echo "$leak" | grep -q 'Architecture (direct)'; then
  echo "FAIL: structural context LEAKED into an unrelated domain"; exit 1
fi
echo "PASS: no cross-domain leak"
if [ "${KEEP:-0}" = 1 ]; then
  # Record the PID so a caller can later kill EXACTLY this server (never pkill).
  [ -n "${AWG_SERVE_PIDFILE:-}" ] && echo "$SERVE_PID" > "$AWG_SERVE_PIDFILE"
  echo "server left running (KEEP=1): gRPC localhost:$PORT  pid $SERVE_PID"
fi
exit 0
