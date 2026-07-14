# Oracle Evaluation

Evaluate only after blind reconstruction and question review are frozen.

```bash
sensei benchmark-evaluate --workspace <workspace> --oracle <sealed-oracle.yaml> --question-review <review.yaml> --oracle-mapping <mapping.yaml> --output <report.yaml> --format yaml
```

Status check:

```bash
sensei benchmark-status --workspace <workspace> --report <report.yaml>
```

The oracle is comparative Evidence. It helps judge whether Sensei's blind
questions, gaps, and closure behavior matched the future fix. It is not an
automatic correctness authority and does not promote architecture.
