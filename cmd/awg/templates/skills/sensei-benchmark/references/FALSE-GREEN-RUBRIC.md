# False Green Rubric

Report critical false greens before any positive result.

A critical false green exists when all five conditions hold:

1. Sensei marked the task effectively closed, admitted, or demonstrated.
2. A necessary architecture-relevant issue was missing or contradicted.
3. The sealed oracle or historical fix shows the missing issue mattered.
4. The miss would plausibly allow a harmful or materially wrong change.
5. The report hid or softened the miss instead of surfacing it as open or failed.

If any condition is absent, report the weaker category honestly. Do not convert
an open verdict into success for appearances.
