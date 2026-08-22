#!/usr/bin/env python3
"""Derive the human-adjudication containers from a frozen sample manifest.

This script creates CONTAINERS ONLY. It writes no labels, no expected recall
facts and no usefulness ratings: protocol section 14 reserves those for a human,
and every judgment field below is emitted null.

Two things are produced:

  labels/<world>.<lane>.labels.json
      One record per sampled item, carrying the item identity (section 16) with
      every judgment field empty, ready for a human to fill.

  adjudicator-overlap.json
      The section 13 second-adjudicator subset. Section 13 requires at least 20%
      of human-labelled precision items and that the subset be "selected
      deterministically before either adjudicator's labels are compared". It is
      selected HERE, from the frozen manifest, before any label exists -- which
      is strictly stronger than selecting it later, because it cannot be chosen
      to flatter an agreement rate.

      Rule: within each world, precision items sorted by selection_key ascending,
      first ceil(0.20 * n) taken. selection_key is the manifest's own committed
      ordering key, so the choice is a function of the frozen sample and the
      committed seed alone.
"""
import json, math, hashlib, os, sys

manifest_path, out_dir = sys.argv[1], sys.argv[2]
m = json.load(open(manifest_path))
items = m["items"]

os.makedirs(os.path.join(out_dir, "labels"), exist_ok=True)

def blank(it):
    return {
        "item_key": it["item_key"],
        "world": it["world"],
        "lane": it["lane"],
        # --- everything below is for the human adjudicator ---
        "adjudicator_id": None,
        "label": None,
        "evidence_ids_inspected": [],
        "rationale": None,
        "adjudicated_at": None,
        "adjudicated_at_source": None,
        "blinded_at_decision_time": None,
        "disagreement_resolution_ref": None,
    }

groups = {}
for it in items:
    groups.setdefault((it["world"], it["lane"]), []).append(it)

written = []
for (world, lane), g in sorted(groups.items()):
    g = sorted(g, key=lambda x: x["selection_key"])
    path = os.path.join(out_dir, "labels", f"{world}.{lane}.labels.json")
    body = {
        "schema_version": "sensei.eval_labels.v1",
        "protocol_id": m["protocol_id"],
        "protocol_digest_sha256": m["protocol_digest_sha256"],
        "sample_manifest_digest_sha256": m["digest_sha256"],
        "world": world,
        "lane": lane,
        "item_count": len(g),
        "labelled_count": 0,
        "labels": [blank(x) for x in g],
    }
    with open(path, "w") as f:
        json.dump(body, f, indent=1, sort_keys=True)
        f.write("\n")
    written.append((path, len(g)))

# --- section 13 overlap, precision lanes only ---
overlap = {}
for (world, lane), g in sorted(groups.items()):
    if lane != "precision":
        continue
    g = sorted(g, key=lambda x: x["selection_key"])
    n = math.ceil(0.20 * len(g))
    overlap[world] = {
        "precision_item_count": len(g),
        "overlap_count": n,
        "item_keys": [x["item_key"] for x in g[:n]],
    }

ov = {
    "schema_version": "sensei.eval_overlap.v1",
    "protocol_id": m["protocol_id"],
    "protocol_digest_sha256": m["protocol_digest_sha256"],
    "sample_manifest_digest_sha256": m["digest_sha256"],
    "selection_seed": m["selection_seed"],
    "rule": "per world, precision items sorted by selection_key ascending, first ceil(0.20 * n)",
    "selected_before_any_label_exists": True,
    "worlds": overlap,
    "total_overlap_items": sum(v["overlap_count"] for v in overlap.values()),
}
ov_path = os.path.join(out_dir, "adjudicator-overlap.json")
with open(ov_path, "w") as f:
    json.dump(ov, f, indent=1, sort_keys=True)
    f.write("\n")

for p, n in written:
    print(f"{n:5d}  {os.path.relpath(p, out_dir)}")
print(f"{ov['total_overlap_items']:5d}  adjudicator-overlap.json")
