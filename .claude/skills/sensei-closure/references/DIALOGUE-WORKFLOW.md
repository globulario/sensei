# Dialogue Workflow

Questions and answers are dialogue artifacts, not Evidence.

Generate questions only from grounded closure blockers:

```bash
sensei generate-questions --closure <closure.yaml> --claims <claims.yaml> --graph-nt <awareness.nt> --created-at <RFC3339> --output <dialogue.yaml>
```

Record the architect's exact statement:

```bash
sensei record-answer --dialogue <dialogue.yaml> --question <id> --statement <text> --classification <type> --author-role <role> --recorded-at <RFC3339> --output <dialogue.yaml>
```

Then adjudicate separately:

```bash
sensei adjudicate-answer --dialogue <dialogue.yaml> --answer <id> --status <status> --output <dialogue.yaml>
```

Do not paraphrase an answer into a stronger claim. Do not treat a recorded
answer as proof of runtime behavior, file contents, or historical fact.
