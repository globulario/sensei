#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
#
# Example model bridge for Sensei's modelexec CommandProvider.
#
# It reads ONE sensei.modelexec.command_request.v1 envelope on stdin and writes
# ONE sensei.modelexec.command_response.v1 envelope on stdout. Sensei's executor
# decides the terminal status; this script only carries the question and the
# answer across the process boundary.
#
# Two rules this script exists to demonstrate:
#
#   1. Nothing is shown to the model that the request did not supply. The prompt
#      is built ONLY from the envelope's bounded evidence. No repository text, no
#      file reads, no clock, no hidden system hints. The fixed template below is
#      what prompt_contract_digest must pin.
#
#   2. A refusal is reported as a refusal, not as a failure. Sensei distinguishes
#      "the provider said no" from "the provider broke", and that distinction is
#      only preserved if the bridge preserves it.
#
# Credentials come from the environment and are never echoed, logged, or placed
# in the response. Usage:
#
#   ANTHROPIC_API_KEY=... sensei ... --model-provider-path examples/model-bridge/anthropic-bridge.sh
set -euo pipefail

MODEL="${SENSEI_BRIDGE_MODEL:-claude-opus-5}"
API="${ANTHROPIC_API_URL:-https://api.anthropic.com/v1/messages}"

if [[ -z "${ANTHROPIC_API_KEY:-}" ]]; then
  echo "anthropic-bridge: ANTHROPIC_API_KEY is not set" >&2
  exit 64
fi

REQ="$(cat)"

# --- bounded material only -----------------------------------------------
DOMAIN="$(jq -r '.repository_domain // ""' <<<"$REQ")"
TARGETS="$(jq -r '(.target_observation_ids // []) | join(", ")' <<<"$REQ")"
EVIDENCE="$(jq -r '
  (.supplied_evidence // [])
  | map("- id: \(.id)\n  file: \(.file_path // "(unspecified)")\n  excerpt: \(.excerpt)")
  | join("\n")' <<<"$REQ")"
ALLOWED_IDS="$(jq -c '[(.supplied_evidence // [])[].id]' <<<"$REQ")"

# The FIXED prompt template. Changing these bytes changes what the model is
# asked, so prompt_contract_digest must be updated with it.
SYSTEM="You are assisting a software architecture investigation.
You may use ONLY the evidence supplied below. Do not assume, recall, or invent
any other file, symbol, or fact about this repository.
Every candidate_claim must cite at least one supplied evidence id.
Cite only these evidence ids: ${ALLOWED_IDS}.
Reference only files that appear in the supplied evidence.
You have no authority: never mark anything canonical, promoted, or admitted."

USER="Repository domain: ${DOMAIN}
Investigation targets: ${TARGETS:-(none specified)}

Supplied evidence:
${EVIDENCE:-(none)}

Propose derived findings about the architecture these excerpts show."

SCHEMA='{
  "type": "object",
  "properties": {
    "items": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "kind": {"type": "string", "enum": ["candidate_claim", "question", "challenge", "limitation"]},
          "text": {"type": "string"},
          "cited_evidence_ids": {"type": "array", "items": {"type": "string"}},
          "file_paths": {"type": "array", "items": {"type": "string"}}
        },
        "required": ["kind", "text", "cited_evidence_ids", "file_paths"],
        "additionalProperties": false
      }
    }
  },
  "required": ["items"],
  "additionalProperties": false
}'

BODY="$(jq -n --arg model "$MODEL" --arg system "$SYSTEM" --arg user "$USER" --argjson schema "$SCHEMA" '{
  model: $model,
  max_tokens: 16000,
  system: $system,
  messages: [{role: "user", content: $user}],
  output_config: {format: {type: "json_schema", schema: $schema}}
}')"

HTTP_BODY="$(mktemp)"
trap 'rm -f "$HTTP_BODY"' EXIT
CODE="$(curl -sS -o "$HTTP_BODY" -w '%{http_code}' "$API" \
  -H "content-type: application/json" \
  -H "x-api-key: ${ANTHROPIC_API_KEY}" \
  -H "anthropic-version: 2023-06-01" \
  --data-binary "$BODY")"

# A transport or API failure is an ERROR. Exiting non-zero is how Sensei's
# adapter learns that, and the key never appears in the diagnostic.
if [[ "$CODE" != "200" ]]; then
  echo "anthropic-bridge: model API returned HTTP ${CODE}: $(jq -c '.error // .' "$HTTP_BODY" 2>/dev/null || echo unparsable)" >&2
  exit 65
fi

# A policy decline is a REFUSAL, and must not be reported as an outage.
STOP="$(jq -r '.stop_reason // ""' "$HTTP_BODY")"
if [[ "$STOP" == "refusal" ]]; then
  jq -n --arg reason "$(jq -r '.stop_details.category // "unspecified"' "$HTTP_BODY")" '{
    schema: "sensei.modelexec.command_response.v1",
    refusal: {reason: ("model declined: " + $reason)}
  }'
  exit 0
fi

PAYLOAD="$(jq -r '[.content[] | select(.type == "text") | .text] | join("")' "$HTTP_BODY")"
if ! jq -e . >/dev/null 2>&1 <<<"$PAYLOAD"; then
  echo "anthropic-bridge: model did not return the requested JSON object" >&2
  exit 66
fi

jq -n --argjson items "$(jq -c '.items // []' <<<"$PAYLOAD")" '{
  schema: "sensei.modelexec.command_response.v1",
  artifact: {
    schema_version: "sensei.model_artifact.v1",
    nondeterminism_declaration: "model_response_not_replayable",
    items: $items
  }
}'
