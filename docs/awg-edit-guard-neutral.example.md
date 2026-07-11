# Driving the Sensei pre-edit guard from any agent (no Claude Code)

`sensei edit-guard` runs a *proposed edit's content* through `edit_check` and
blocks edits that would introduce a forbidden-fix shape or a high-severity rule
violation. The decision core is **agent-neutral**; `--format` selects the output
adapter. The Claude Code hook is just one adapter (`--format claude`, the
default) — Codex, Cursor, a git hook, or plain CI drive the *same* governance.

It **fails open** in every mode: an unreachable server or an out-of-project file
allows the edit. For a fail-*closed* CI gate over a whole diff, use
`sensei gate --enforce` instead.

## Any agent — decide on the exit code

```bash
# Pipe the proposed new content on stdin; exit 2 means "blocked".
printf '%s' "$PROPOSED_CONTENT" \
  | sensei edit-guard --file "$EDITED_PATH" --format exit-code
case $? in
  0) : ;;                 # allowed (or guard unavailable — fails open)
  2) echo "AWG blocked this edit; revise or override." >&2 ;;
esac
```

## Any agent — parse a structured verdict

```bash
printf '%s' "$PROPOSED_CONTENT" \
  | sensei edit-guard --file "$EDITED_PATH" --format json
# => {"file":"...","decision":"block","reason":"...",
#     "warnings":[{"rule_id":"...","enforcement":"block","message":"...","provenance":"..."}]}
```

An agent reads `decision` and the `warnings[]` (rule id + provenance) and either
revises the edit or, if the change is deliberate, proceeds.

## Content from a file instead of stdin

```bash
sensei edit-guard --file src/x.go --content-file /tmp/proposed-x.go --format json
```

## Claude Code (the built-in adapter)

The default `--format claude` reads a PreToolUse payload on stdin and emits
`{"decision":"block","reason":...}`. Wire it exactly as
`cmd/awg/templates/hooks/edit-check-guard.sh` shows — that hook is a thin
`exec sensei edit-guard`. Nothing about the mechanism is Claude-Code-specific; the
hook is one adapter over the neutral core.

## Environment knobs (all adapters)

| var | meaning |
|-----|---------|
| `AWG_ADDR` | gRPC server address (default `localhost:10120`) |
| `AWG_DOMAIN` | domain scope for a multi-domain graph |
| `AWG_EDIT_GUARD_FORMAT` | default `--format` (`claude`\|`json`\|`exit-code`) |
| `AWG_EDIT_CHECK_ADVISORY=1` | warn-only: never block, surface advisories |
| `AWG_EDIT_CHECK_BLOCK_SEVERITY` | comma list of blocking severities |
