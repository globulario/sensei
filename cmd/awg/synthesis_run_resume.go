// SPDX-License-Identifier: AGPL-3.0-only

// synthesis_run_resume.go holds the two small pieces `sensei synthesis-run
// --resume` needs that are genuinely the CLI's own: deciding whether the
// operator named a checkpoint exactly, and persisting the assessment O7
// produced.
//
// Everything else about resume -- what may continue, what has drifted, which
// owner runs next -- belongs to golang/architecture/synthesisdriver and is
// deliberately not reimplemented here. A drift comparison in cmd/ would be a
// second authority on a question that must have exactly one answer, and it
// would be the one nobody tests against a real checkpoint.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/globulario/sensei/golang/architecture/synthesisdriver"
)

// isCheckpointDigest reports whether s names one durable boundary exactly.
//
// A prefix, a path, or an empty value meaning "the latest" are all refused,
// and that refusal is the point: resume continues a specific history, and a
// command that picks which one by recency makes the operator's invocation an
// incomplete record of what was run. `--resume` is how you say WHICH.
func isCheckpointDigest(s string) bool {
	s = strings.TrimSpace(s)
	if len(s) != 64 {
		return false
	}
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f':
		default:
			return false
		}
	}
	return true
}

// persistResumeAssessment records the decision beside the checkpoints it
// judged, whether it allowed or refused.
//
// A refusal is evidence: it names which identity moved, and an operator
// reconstructing what happened after the fact needs it as much as they need
// the boundaries themselves. Keeping it only on the terminal would make the
// one artifact that explains a stopped session the one artifact that does not
// survive the session.
//
// Written under the assessment's own digest and never overwritten with
// different bytes: two different assessments cannot be the same assessment,
// and silently replacing one would erase a decision that was really made.
func persistResumeAssessment(ctx context.Context, dir string, assessment synthesisdriver.ResumeAssessment) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	digest := strings.TrimSpace(assessment.AssessmentDigestSHA256)
	if digest == "" {
		return fmt.Errorf("resume assessment carries no digest")
	}
	data, err := json.MarshalIndent(assessment, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal resume assessment: %w", err)
	}
	data = append(data, '\n')

	path := filepath.Join(dir, digest+".resume-assessment.json")
	if existing, rerr := os.ReadFile(path); rerr == nil {
		if string(existing) == string(data) {
			return nil
		}
		return fmt.Errorf("resume assessment %s already exists with different content", digest)
	} else if !os.IsNotExist(rerr) {
		return fmt.Errorf("inspect %s: %w", path, rerr)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	return writeFileAtomic(path, data)
}

// openCheckpointStore prepares the durable boundary directory and opens it.
//
// NewFSCheckpointStore requires the directory to already exist, and
// deliberately so: a store that silently creates whatever path it is handed
// cannot refuse a typo. Creating it is therefore the CALLER's job — exactly as
// it already is for the candidate and evidence stores.
//
// This exists as its own function because omitting that one step made every
// `sensei synthesis-run` invocation stop at checkpoint-store-unusable: a task's
// checkpoint directory does not exist until the first run wants one, so the
// very first thing the new durable path did was refuse itself. Nothing in CI
// noticed, because the only test that drives the command this far is the
// ten-minute real-system smoke.
func openCheckpointStore(dir string) (synthesisdriver.CheckpointStore, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create checkpoint store %s: %w", dir, err)
	}
	store, err := synthesisdriver.NewFSCheckpointStore(dir)
	if err != nil {
		return nil, fmt.Errorf("open checkpoint store %s: %w", dir, err)
	}
	return store, nil
}
