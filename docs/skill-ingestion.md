# Skill Ingestion

Skill Ingestion converts external agent skill documents into reviewable Sensei candidates.

It does not import skills into the active awareness graph. External text is not authority; it becomes project governance only after candidate generation, validation, human review, and promotion into an active awareness corpus path.

## Supported input

Supported input is a skill pack containing `SKILL.md` files with YAML front matter:

```yaml
---
name: tdd
description: Test-driven development. Use when the user wants test-first work.
---
```

Required front matter fields:

- `name`
- `description`

The command scans files named exactly `SKILL.md`. It ignores `.git/`, `node_modules/`, `.changeset/`, `.claude-plugin/`, and `skills/in-progress/`. It also ignores `skills/deprecated/` unless `--include-deprecated` is set.

The category comes from the skill path:

```text
skills/engineering/tdd/SKILL.md -> engineering
skills/productivity/grilling/SKILL.md -> productivity
```

## Output

The command parses the front matter, extracts conservative procedural guidance from Markdown, and writes one `ImplementationPattern` candidate per skill:

```text
docs/awareness/candidates/skills/*.yaml
```

The generated candidate stream is named `skill_ingestion_candidate`, but the YAML itself uses the existing `ImplementationPattern` shape so normal Sensei review and promotion rules apply.

Candidate directories are skipped by normal Sensei import. Generated skill candidates are review-only until promoted.

Example generated file name:

```text
docs/awareness/candidates/skills/imported_skill_engineering_tdd.yaml
```

Example generated YAML shape:

```yaml
id: imported.skill.engineering.tdd
class: ImplementationPattern
label: "Skill: tdd"
status: candidate
confidence: medium
when_to_use:
  - "Test-driven development. Use when the user wants test-first work."
reference_files:
  - path: "skills/engineering/tdd/SKILL.md"
    role: "source_skill"
must_follow:
  - "Write the failing test first, then only enough code to pass it."
forbidden_shortcuts:
  - "Do not test private methods or implementation details."
rationale: |
  Imported from an external agent skill as a reviewable candidate.
  This candidate is procedural guidance, not live authority.
  It must be reviewed before promotion.
source_files:
  - "skills/engineering/tdd/SKILL.md"
```

## Command

```bash
sensei skill-ingest <skill-pack-root> \
  --out docs/awareness/candidates/skills \
  --repo github.com/globulario/sensei \
  --source-set external/skills
```

Useful flags:

- `--out`: output directory for generated YAML.
- `--repo`: repository domain for provenance reporting.
- `--source-set`: source-set label for provenance reporting.
- `--include-deprecated`: include `skills/deprecated/**/SKILL.md`.
- `--dry-run`: parse, render, and validate without writing files.

Example:

```bash
sensei skill-ingest ../skills-main \
  --out docs/awareness/candidates/skills \
  --repo github.com/globulario/sensei
```

Example output:

```text
skill-ingest: discovered=28 imported=28 skipped=4 invalid=0
output directory: docs/awareness/candidates/skills
wrote docs/awareness/candidates/skills/imported_skill_engineering_tdd.yaml

Candidates are review-only. Run sensei promote after human review.
```

## Review workflow

```bash
git diff docs/awareness/candidates/skills
# human edits candidate
sensei promote imported.skill.engineering.tdd
sensei build -strict
```

If `sensei promote` does not support this candidate path in your checkout, promotion is manual for now: move the reviewed candidate into an active awareness corpus path such as `docs/awareness/architecture/patterns/`, keep status according to the existing corpus convention, then run `sensei build`.

## Validation

After rendering, Skill Ingestion validates only the generated candidates. It does not run a full graph build because `candidates/` is intentionally skipped by normal import.

Each generated candidate must have:

- non-empty `id`
- `class: ImplementationPattern`
- `status: candidate`
- at least one of `must_follow`, `when_to_use`, or `forbidden_shortcuts`
- a `reference_files` entry pointing at the source `SKILL.md`

## Safety rules

- Skill ingestion never emits `status: active`.
- It never writes generated files under active awareness corpus paths by default.
- It does not treat external skill text as authority.
- It uses the existing `ImplementationPattern` schema so briefing and preflight can surface promoted patterns through the normal graph path.
- Extraction is deterministic and does not require an LLM.
- It does not rebuild the graph and does not mutate `awareness.nt`.
