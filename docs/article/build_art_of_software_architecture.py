#!/usr/bin/env python3
"""Render 'The Art of Software Architecture' as a single Markdown field book,
faithfully from cmd/awg/templates/awareness/meta_principles.yaml.

Every one of the 133 maxims is emitted in full (no truncation). Per-maxim
'trap' and 'discipline' prose is composed from the real principle fields
(wrong_instinct, antipattern, summary, enforcement); the literary frame
(preface, chapter openers, field notes, afterword) is authored here.
"""
import re
import yaml
from pathlib import Path

SRC = Path("cmd/awg/templates/awareness/meta_principles.yaml")
OUT = Path("docs/article/The_Art_of_Software_Architecture.md")

# --- text cleaning -----------------------------------------------------------

# Markers where a summary stops being an aphorism and becomes a checklist or
# project-local provenance. We keep only the essence above these.
CUT = re.compile(
    r"(?im)^\s*(before (reading|writing|you|automating)|known (violations|good)|"
    r"for (every|each)\b|this is (a )?different|example[:\s]|for every|"
    r"non-owner actors must|owner apis must distinguish|every fallback result must)"
)

def inline(s: str) -> str:
    return re.sub(r"\s+", " ", s).strip()

# Sentences carrying project-local provenance or cross-references that should
# not appear in a portable field book. Dropped sentence-by-sentence.
LEAK = re.compile(
    r"instance of meta\.|in this codebase|from our (codebase|history)|"
    r"INC-20\d\d|INC-2025|Bug shapes|Known violations|Known good|Incidents:",
    re.IGNORECASE,
)

def scrub(s: str) -> str:
    """Remove internal cross-reference / provenance sentences and dangling refs."""
    parts = re.split(r"(?<=[.!?])\s+", s)
    kept = [p for p in parts if not LEAK.search(p)]
    s = " ".join(kept)
    s = re.sub(r"\s*\+\s*meta\.[\w.]+", "", s)   # trailing " + meta.foo" fragments
    s = re.sub(r"\s{2,}", " ", s).strip()
    return s

def first_para(text: str, cut_checklists: bool = False) -> str:
    """First paragraph, cleaned and scrubbed of internal references."""
    if not text:
        return ""
    para = re.split(r"\n[ \t]*\n", text.strip())[0]
    if cut_checklists:
        m = CUT.search(para)
        if m:
            para = para[: m.start()]
    return scrub(inline(para))

def parse_list(block: str):
    """Parse a numbered/bulleted enforcement block into clean item strings."""
    items, cur = [], None
    for line in block.split("\n"):
        m = re.match(r"\s*\d+\.\s+(.*)", line) or re.match(r"\s*[-*]\s+(.*)", line)
        if m:
            if cur is not None:
                items.append(cur)
            cur = m.group(1).strip()
        elif cur is not None:
            cur += " " + line.strip()
    if cur is not None:
        items.append(cur)
    return [scrub(inline(i)) for i in items if i.strip()]

def discipline(text: str):
    """Return (intro, items). If the rule introduces a list, items is non-empty."""
    if not text:
        return "", []
    blocks = re.split(r"\n[ \t]*\n", text.strip())
    intro = inline(blocks[0])
    if intro.rstrip().endswith(":") and len(blocks) > 1:
        items = parse_list(blocks[1])
        if items:
            return scrub(intro), items
    return first_para(text), []

def ensure_period(s: str) -> str:
    s = s.strip()
    if s and s[-1] not in ".!?:":
        s += "."
    return s

# --- load & group ------------------------------------------------------------

principles = yaml.safe_load(SRC.read_text())["invariants"]

CHAPTERS = [
    ("authority",  "On Authority and the Ownership of Truth",
     "A system has many hands, but truth must have one owner. The architect who "
     "confuses access with authority convenes a court in which every clerk may "
     "rewrite the law. Such systems look flexible — right up to the first repair, "
     "when every actor is a king and the database is the battlefield they fight on."),
    ("signal",     "On Signals, Silence, and Honest Uncertainty",
     "The machine speaks through status, metrics, errors, clocks, and absence. "
     "Most outages are not born mute; they are born speaking a dialect no decision "
     "was ever bound to. The wise architect does not ask whether there is telemetry. "
     "The wise architect asks whether the telemetry can tell the truth without a costume."),
    ("lifecycle",  "On Time, State, and the Long March of Work",
     "Every write is a promise, and a promise no one is appointed to keep is a "
     "blockage wearing the face of progress. The long-running campaign is not lost "
     "to the enemy you see. It is lost to the half-finished state that looks finished, "
     "the retry that becomes a siege aimed inward, and the intermediate step that "
     "answers 'yes' when asked if it is done."),
    ("dependency", "On Dependencies, Topology, and Retreat",
     "A general studies the road home before he marches. So must the architect study "
     "the recovery path before the failure — for a dependency you did not need on the "
     "happy path will be the one that kills you on the road back. Decide the answer to "
     "the partition before the partition arrives; to choose in the moment the network "
     "splits is not strategy, it is the bug arriving on schedule."),
    ("perception", "On the Operator's Eye",
     "The screen is the only ground the operator can see, and so it is where the war "
     "is truly won or lost. A green badge that shines because the model in memory said "
     "so is a sentry reporting a peace that does not exist. Certainty is part of the "
     "value: loading, stale, unknown, optimistic, and confirmed must each look like "
     "exactly what they are."),
    ("composition","On Arrangement, Weight, and Visual Command",
     "Spacing is information; equal spacing makes unrelated facts look like kin. The "
     "layout is an argument the eye believes before the mind has read a word — so let "
     "weight, order, and grouping follow the operator's decision, never the designer's "
     "mood. Safety evidence outranks decoration. Always."),
    ("structure",  "On Boundaries, Reuse, and the Shape of Code",
     "A module earns its life by hiding more complexity than its interface adds; the "
     "rest are names, files, and imports that hide nothing and give the bug a place to "
     "sleep. Contracts outlive the fashion of their implementation. Reuse follows "
     "meaning, never resemblance — two strangers welded back to back do not become one "
     "traveller."),
    ("evolution",  "On Change, Governance, and the Releasable Road",
     "The main branch must remain forever releasable, for a thousand small conveniences "
     "become one large ruin the day they are all due at once. Change the intent before "
     "the structure; let discovery propose and only a human promote. And know the last "
     "law, above all the green tests: there is no resolution without a respected contract."),
]

CHAPTER_ROMAN = ["I", "II", "III", "IV", "V", "VI", "VII", "VIII"]

# One closing field note per chapter — authored in the book's voice.
FIELD_NOTES = {
    "authority":  "Granting database credentials to a service does not crown it king. "
                  "It merely gives the future incident report better evidence.",
    "signal":     "A dashboard with forty-seven green lights may still be a beautifully "
                  "illuminated absence of proof.",
    "lifecycle":  "The system did not hang because the work was hard. It hung because "
                  "someone wrote a promise and appointed no one to keep it.",
    "dependency": "You do not discover your circular dependency in the design review. "
                  "You discover it at 3 a.m., when the thing that would fix it is the "
                  "thing that is down.",
    "perception": "The operator did not misread the screen. The screen misrepresented "
                  "the system, in good faith, in a pleasant shade of green.",
    "composition":"The eye obeys the layout before the mind reads the label. Put the "
                  "warning where the hand is already moving, or do not bother to write it.",
    "structure":  "Every wrapper that hides nothing is a small tax, collected forever, "
                  "on everyone who reads the code after you.",
    "evolution":  "'We'll mechanize it later' is the most expensive sentence in "
                  "engineering, because 'later' is denominated in incidents.",
}

by_cat = {}
for p in principles:
    by_cat.setdefault(p["category"], []).append(p)

# --- render ------------------------------------------------------------------

out = []
w = out.append

# Title page
w("# The Art of Software Architecture")
w("")
w("### *133 maxims for building systems that refuse to lie*")
w("")
w("*Compiled from the meta-principles of Sensei — a field book for "
  "architects, operators, and the brave soul who inherited the service nobody "
  "admits to owning.*")
w("")
w("> *The victorious architect does not begin with code. The victorious architect "
  "begins by deciding what may be called true.*")
w("")
w("---")
w("")

# Preface
w("## Preface — Before the First Deployment")
w("")
w("The old books of strategy open with ground, supply, command, deception, and the "
  "cost of a prolonged campaign. Software differs chiefly in one comic detail: its "
  "generals are often surprised to learn that the battlefield has been running in "
  "production for six years.")
w("")
w("This is not a book about drawing diagrams. It is a book about the *government of "
  "truth through time* — how a system keeps knowing what it is while release after "
  "release washes over it. Its enemy is not complexity; complexity can be measured "
  "and paid down. The more dangerous enemies are the ones that look like friends: "
  "ambiguity that looks convenient, a fallback that looks helpful, a partial write "
  "that looks finished, cached state that looks authoritative, and a green status "
  "that looks like comfort.")
w("")
w("Every maxim here was born from pain. Somewhere, a timeout was mistaken for a "
  "failure; a database row was mistaken for ownership; a retry became a siege engine "
  "aimed inward; a recovery path depended on the very service it was meant to "
  "recover; a button announced permission before permission had been proved. The "
  "maxims are what remained after the incident review, once the shouting stopped and "
  "the shape of the mistake stood plain.")
w("")
w("Read it as a campaign manual. The passages are short because the lesson usually "
  "arrives mid-incident, when nobody has appetite for a twelve-page architecture "
  "decision record. The humour is deliberate: a trap that makes an architect smile "
  "is easier to remember than one embalmed in committee language. And the aim is "
  "modest. It is not to prevent every failure — that is a fantasy sold by people who "
  "have not yet met distributed time. The aim is to make failures *bounded, visible, "
  "classifiable, recoverable, and unable to impersonate success.*")
w("")
w("Each maxim is given in three parts: **the law** itself; **the trap**, which is the "
  "reasonable-sounding instinct that leads you to break it; and **the discipline**, "
  "which is the smallest concrete rule that keeps you honest. The traps are quoted "
  "almost as engineers actually say them — because you will recognise your own voice "
  "in more than one, and that recognition is the whole point.")
w("")
w("---")
w("")

# Contents
w("## Contents")
w("")
for i, (cat, title, _intro) in enumerate(CHAPTERS):
    n = len(by_cat[cat])
    w(f"**{CHAPTER_ROMAN[i]}. {title}** — {n} maxims  ")
w("**Afterword — The Architecture That Remembers**  ")
w("**Index of the 133 Principles**")
w("")
w("*This is a reference, not a novel — it is meant to be kept and consulted, not "
  "read at a sitting. Every maxim is numbered and anchored: the index at the back "
  "and the `See also` cross-references link straight to the relevant one. Each "
  "maxim carries a severity tag, though you should not expect it to narrow things "
  "much — 80 of the 133 are marked **critical**. That is the quiet lesson of the "
  "whole book: in a system that must tell the truth about itself, most mistakes do "
  "not merely slow it down. They make it lie.*")
w("")
w("---")
w("")

# --- flat ordering + id -> maxim-number map (for cross-links) -----------------
ordered = [p for cat, _t, _i in CHAPTERS for p in by_cat[cat]]
idmap = {p["id"]: n for n, p in enumerate(ordered, start=1)}

def enriched_summary(summary: str) -> str:
    """First paragraph; if it is only a lead-in (ends ':'), pull the next block."""
    summ = first_para(summary, cut_checklists=True)
    blocks = re.split(r"\n[ \t]*\n", summary.strip())
    if summ.rstrip().endswith(":") and len(blocks) > 1 and len(summ) < 130:
        body = scrub(inline(blocks[1]))
        body = " ".join(re.split(r"(?<=[.!?])\s+", body)[:3]).strip()
        summ = f"{summ} {body}".strip()
    return summ

# --- Chapters ----------------------------------------------------------------
maxim_no = 0
for i, (cat, title, intro) in enumerate(CHAPTERS):
    roman = CHAPTER_ROMAN[i]
    w(f"## {roman}. {title}")
    w("")
    w(f"*{intro}*")
    w("")
    for p in by_cat[cat]:
        maxim_no += 1
        law = p["title"].strip().strip('"')
        wrong = ensure_period(scrub(inline(p["wrong_instinct"])))
        anti = p["antipattern"].strip().strip('"')
        summ = enriched_summary(p["summary"])
        disc_intro, disc_items = discipline(p.get("enforcement", ""))

        trap = f"{wrong} Its familiar disguise: *“{anti}”*."
        if summ:
            trap += f" {summ}"

        rel = []
        for r in (p.get("related_invariants") or []):
            n = idmap.get(r) if isinstance(r, str) else None
            if n and n != maxim_no and n not in rel:
                rel.append(n)
        rel = sorted(rel)[:5]

        w(f'<a id="m{maxim_no}"></a>')
        w(f"#### {maxim_no}. {law}")
        w("")
        w(f"*{p['severity'].capitalize()} severity.*")
        w("")
        w(f"**The trap.** {trap}")
        w("")
        if disc_intro:
            if disc_items:
                w(f"**The discipline.** {disc_intro}")
                w("")
                for it in disc_items:
                    w(f"- {it}")
                w("")
            else:
                w(f"**The discipline.** {disc_intro}")
                w("")
        if rel:
            w("**See also:** " + ", ".join(f"[{r}](#m{r})" for r in rel))
            w("")
    # chapter-closing field note
    note = FIELD_NOTES.get(cat)
    if note:
        w(f"> *Field note — {note}*")
        w("")
    w("---")
    w("")

# Afterword
w("## Afterword — The Architecture That Remembers")
w("")
w("You have now read one hundred and thirty-three ways to be wrong with total "
  "confidence. Take heart: they are not one hundred and thirty-three unrelated "
  "mistakes. They are a small number of *shapes*, recurring — a fallback that hides "
  "a failure by wearing truth's face; a write with no appointed keeper; two writers "
  "racing on one field; an intermediate state that satisfies a completeness check; "
  "a green light standing in for a proof no one performed. Learn to see the shape "
  "and you can find the next instance before it fires, in code that has not yet "
  "broken.")
w("")
w("But here is the harder truth the old strategists knew and we keep forgetting: "
  "knowledge that lives only in a person leaves when the person does. The maxims in "
  "this book were, for most of software's history, unwritten — carried in the heads "
  "of the three engineers who were on the incident call, in a post-mortem nobody "
  "re-reads, in a review comment that scrolled out of history. The next contributor "
  "could not see them, and so made the reasonable-looking change that quietly broke "
  "one, and the system drifted a little further from its own design.")
w("")
w("That is no longer only a human problem. An AI agent arrives at your repository "
  "every session with a flawless reading of the syntax and no memory of the "
  "architecture. It will write a patch that compiles, passes the tests it can see, "
  "reads beautifully — and violates a law that was in none of the files it opened. "
  "A stronger model reads the code better; it still cannot read what the code does "
  "not contain.")
w("")
w("So the final maxim is not in the list, because it is about the list itself: "
  "**write the memory down where the work happens, and make it answer at the moment "
  "of the edit.** That is what Sensei does with these principles — it "
  "compiles them into a graph the repository carries, and serves the ones that apply "
  "to the file you are about to change, before you change it, to human and agent "
  "alike. The counsel arrives before the mistake, not after.")
w("")
w("The highest skill was never to write the most code the fastest. It is to keep the "
  "system knowing what it is — while everyone who once knew is asleep.")
w("")
w("---")
w("")

# Index
w("## Index of the 133 Principles")
w("")
w("*The maxims above are paraphrase and field-craft. Below are the principles "
  "themselves, by their true identifiers, as they live — machine-queryable, with "
  "their enforcement tiers and the scars of the incidents that taught them — in "
  "`cmd/awg/templates/awareness/meta_principles.yaml`.*")
w("")
idx = 0
for i, (cat, title, _intro) in enumerate(CHAPTERS):
    w(f"**{CHAPTER_ROMAN[i]}. {title}**")
    w("")
    for p in by_cat[cat]:
        idx += 1
        w(f"[{idx}](#m{idx}). `{p['id']}` — {p['title'].strip().strip(chr(34))}  ")
    w("")

w("---")
w("")
w("*The Art of Software Architecture is drawn from Sensei, open "
  "source at [github.com/globulario/sensei](https://github.com/globulario/sensei). "
  "The 133 meta-principles were distilled from real production incidents on the "
  "[Globular](https://github.com/globulario) platform and are shipped as portable, "
  "domain-independent seed knowledge with every `awg init`. What bit us is provenance; "
  "what we learned belongs to everyone.*")

OUT.write_text("\n".join(out) + "\n")
print(f"Wrote {OUT} — {maxim_no} maxims, {len(out)} lines.")
