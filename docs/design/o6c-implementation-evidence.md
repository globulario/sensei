# O6C Implementation Evidence

This checkpoint binds the governed agent-command bridge to a real O3 execution path.

The integration proof constructs a Git repository and exact workspace identity, starts an O1-compatible attempting state, runs `agentcommand.Factory` through `runnercomposition.Run`, reloads the sealed candidate by its content digest, and verifies that O3's post-close input and proposed-change evidence matches the immutable Attempt returned through O2.

The proof uses a deterministic in-process agent and no external credentials. Codex and Claude remain thin direct-argv profiles over the same bridge. They receive no repository or candidate-buffer path and return only the closed mutation-plan document that Go applies through `CandidateWorkspace`.

Vendor commands are additionally confined to a caller-supplied absolute, real, empty working directory. The bridge validates emptiness at construction and immediately before each invocation, preventing `cwd` from becoming an ambient repository-discovery channel after configuration.
