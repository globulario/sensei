// SPDX-License-Identifier: Apache-2.0

package coldsource

// promptContract is the system prompt for the LLM drafter. It encodes the hard
// rules the experiment depends on; the deterministic checks (ValidateDraft,
// CheckCitations, IsShallow) enforce them regardless of whether the model obeys,
// but stating them up front improves the hit rate and keeps the model honest.
const promptContract = `You convert a bundle of cold-source evidence into ONE candidate awareness
entry for a code-architecture knowledge graph. The evidence comes from a real
repository: pull-request review comments and revert/regression commits, all on
the same component.

Output ONLY the candidate object matching the provided JSON schema. No prose,
no markdown, no preamble.

Rules (non-negotiable):
- Do NOT invent evidence. Every claim in "reason" must be supported by the
  evidence in the bundle.
- Do NOT cite anything outside the bundle. "source_paths" MUST be a subset of
  the ALLOWED CITATIONS listed in the user message, copied verbatim. If you
  cannot ground the candidate in at least one allowed citation, return an empty
  "source_paths" — it will be rejected, which is correct.
- Prefer architectural, authority, invariant, forbidden-fix, and failure-mode
  candidates. Reject shallow or generic rules (style nits, "validate input",
  restating a type signature) — if the evidence only supports something shallow,
  say so in "reason" and keep "confidence" low.
- "candidate_class" must be one of: InvariantCandidate, ForbiddenFixCandidate,
  FailureModeCandidate.
- Use "medium" confidence unless the evidence is exceptionally strong and
  multi-sourced, in which case "high"; use "low" for thin or ambiguous evidence.
- Include "required_tests" ONLY when the bundle cites test functions; otherwise
  return an empty array. Never invent test names.
- "theme" must equal the THEME given in the user message.`

// candidateSchema is the json_schema passed via output_config.format so the
// model returns strict, parseable JSON. additionalProperties:false is required
// by the structured-outputs API; every field is listed in "required" so the
// model always emits the full shape (arrays may be empty).
const candidateSchema = `{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "candidate_class": {
      "type": "string",
      "enum": ["InvariantCandidate", "ForbiddenFixCandidate", "FailureModeCandidate"]
    },
    "theme": { "type": "string" },
    "reason": { "type": "string" },
    "confidence": { "type": "string", "enum": ["low", "medium", "high"] },
    "activation_trigger": { "type": "string" },
    "required_tests": { "type": "array", "items": { "type": "string" } },
    "source_paths": { "type": "array", "items": { "type": "string" } }
  },
  "required": [
    "candidate_class", "theme", "reason", "confidence",
    "activation_trigger", "required_tests", "source_paths"
  ]
}`
