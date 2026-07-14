# Blind Evaluation

A blind external benchmark tests Sensei, not the target repository.

The blind runner receives the frozen historical task state and Sensei-generated
artifacts but not the future oracle. The evaluator reveals oracle receipts only
after question review and blind reconstruction are frozen.

Freeze:

```bash
sensei benchmark-freeze --task <task.yaml> --source-repo <repo> --oracle <sealed-oracle.yaml> --output-dir <workspace> --format yaml
```

Reconstruct:

```bash
sensei benchmark-reconstruct --workspace <workspace> --question-created-at <RFC3339> --format yaml
```

The commands write deterministic receipts. They do not execute an agent, run
tests, mutate source, or perform network operations.
