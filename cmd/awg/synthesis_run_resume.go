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
