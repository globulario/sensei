// SPDX-License-Identifier: AGPL-3.0-only

package evaluatorcomposition

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// CommandOutcome is process-execution truth, not a check verdict.
type CommandOutcome string

const (
	CommandOutcomeCompleted   CommandOutcome = "completed"
	CommandOutcomeExited      CommandOutcome = "exited"
	CommandOutcomeUnavailable CommandOutcome = "unavailable"
	CommandOutcomeTimedOut    CommandOutcome = "timed-out"
	CommandOutcomeCancelled   CommandOutcome = "cancelled"
)

// CommandRequest is a shell-free, exact process invocation. Executable must
// be absolute, Args are passed verbatim, Env is the entire environment, and
// Dir must be the evaluator surface root.
type CommandRequest struct {
	Executable string
	Args       []string
	Env        []string
	Dir        string
}

// CommandResult records process truth with separately captured stdout/stderr.
// ExitCode is meaningful for Completed/Exited; it is -1 otherwise.
type CommandResult struct {
	Outcome   CommandOutcome
	ExitCode  int
	Stdout    []byte
	Stderr    []byte
	Truncated bool
	Detail    string
}

// CommandRunner is the only subprocess port evaluator adapters depend on.
type CommandRunner interface {
	Run(ctx context.Context, request CommandRequest, maxCapturedBytes int64) (CommandResult, error)
}

// OSCommandRunner executes a request without a shell and captures at most the
// precommitted byte budget across stdout and stderr together. Output beyond
// the cap is discarded but never blocks the child; Truncated records that
// loss explicitly.
type OSCommandRunner struct{}

func (OSCommandRunner) Run(ctx context.Context, request CommandRequest, maxCapturedBytes int64) (CommandResult, error) {
	if request.Executable == "" || !filepath.IsAbs(request.Executable) {
		return CommandResult{}, fmt.Errorf("OSCommandRunner.Run: executable %q must be absolute", request.Executable)
	}
	if request.Dir == "" || !filepath.IsAbs(request.Dir) {
		return CommandResult{}, fmt.Errorf("OSCommandRunner.Run: dir %q must be absolute", request.Dir)
	}
	if maxCapturedBytes < 0 {
		return CommandResult{}, fmt.Errorf("OSCommandRunner.Run: maxCapturedBytes must be non-negative")
	}

	stdoutCapture := newBoundedStreamCapture(maxCapturedBytes)
	stderrCapture := newBoundedStreamCapture(maxCapturedBytes)
	cmd := exec.CommandContext(ctx, request.Executable, request.Args...)
	cmd.Dir = request.Dir
	cmd.Env = append([]string(nil), request.Env...)
	cmd.Stdout = stdoutCapture
	cmd.Stderr = stderrCapture
	err := cmd.Run()
	stdoutFull, stdoutStreamTruncated := stdoutCapture.snapshot()
	stderrFull, stderrStreamTruncated := stderrCapture.snapshot()
	stdout, stderr, budgetTruncated := trimCapturedOutput(stdoutFull, stderrFull, maxCapturedBytes)
	truncated := stdoutStreamTruncated || stderrStreamTruncated || budgetTruncated

	result := CommandResult{ExitCode: -1, Stdout: stdout, Stderr: stderr, Truncated: truncated}
	if ctxErr := ctx.Err(); ctxErr != nil {
		if errors.Is(ctxErr, context.DeadlineExceeded) {
			result.Outcome = CommandOutcomeTimedOut
			result.Detail = ctxErr.Error()
			return result, nil
		}
		result.Outcome = CommandOutcomeCancelled
		result.Detail = ctxErr.Error()
		return result, nil
	}
	if err == nil {
		result.Outcome = CommandOutcomeCompleted
		result.ExitCode = 0
		return result, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		result.Outcome = CommandOutcomeExited
		result.ExitCode = exitErr.ExitCode()
		result.Detail = err.Error()
		return result, nil
	}
	result.Outcome = CommandOutcomeUnavailable
	result.Detail = err.Error()
	return result, nil
}

type boundedStreamCapture struct {
	mu        sync.Mutex
	limit     int64
	buffer    bytes.Buffer
	truncated bool
}

func newBoundedStreamCapture(limit int64) *boundedStreamCapture {
	return &boundedStreamCapture{limit: limit}
}

func (c *boundedStreamCapture) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	accepted := len(p)
	remaining := c.limit - int64(c.buffer.Len())
	if remaining < 0 {
		remaining = 0
	}
	writeN := int64(len(p))
	if writeN > remaining {
		writeN = remaining
		c.truncated = true
	}
	if writeN > 0 {
		_, _ = c.buffer.Write(p[:writeN])
	}
	if writeN < int64(len(p)) {
		c.truncated = true
	}
	return accepted, nil
}

func (c *boundedStreamCapture) snapshot() ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]byte(nil), c.buffer.Bytes()...), c.truncated
}

// trimCapturedOutput applies the shared evidence budget deterministically:
// stdout is retained first because structured machine evidence such as
// Sensei gate JSON is emitted there; stderr receives the remaining bytes.
func trimCapturedOutput(stdoutFull, stderrFull []byte, limit int64) (stdout, stderr []byte, truncated bool) {
	stdoutN := int64(len(stdoutFull))
	if stdoutN > limit {
		stdoutN = limit
		truncated = true
	}
	stdout = append([]byte(nil), stdoutFull[:stdoutN]...)
	remaining := limit - stdoutN
	stderrN := int64(len(stderrFull))
	if stderrN > remaining {
		stderrN = remaining
		truncated = true
	}
	stderr = append([]byte(nil), stderrFull[:stderrN]...)
	return stdout, stderr, truncated
}

// environmentKeys returns a stable, secret-free description of an exact
// process environment. Evidence records names only; values still reach the
// subprocess but are never persisted in O4 evidence.
func environmentKeys(environment []string) ([]string, error) {
	seen := make(map[string]bool, len(environment))
	keys := make([]string, 0, len(environment))
	for i, item := range environment {
		key, _, ok := strings.Cut(item, "=")
		if !ok || strings.TrimSpace(key) == "" {
			return nil, fmt.Errorf("environment[%d] must be KEY=value with a non-empty key", i)
		}
		if seen[key] {
			return nil, fmt.Errorf("environment key %q appears more than once", key)
		}
		seen[key] = true
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys, nil
}

// EvidenceSink persists captured evidence and returns a stable digest-bound
// reference. It owns storage only, never check interpretation.
type EvidenceSink interface {
	Put(ctx context.Context, content []byte) (EvidenceReference, error)
}

// MemoryEvidenceSink is a concurrency-safe deterministic test double.
type MemoryEvidenceSink struct {
	mu    sync.Mutex
	blobs map[string][]byte
}

func NewMemoryEvidenceSink() *MemoryEvidenceSink {
	return &MemoryEvidenceSink{blobs: make(map[string][]byte)}
}

func (s *MemoryEvidenceSink) Put(ctx context.Context, content []byte) (EvidenceReference, error) {
	if err := ctx.Err(); err != nil {
		return EvidenceReference{}, err
	}
	digest := evidenceDigest(content)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.blobs == nil {
		s.blobs = make(map[string][]byte)
	}
	s.blobs[digest] = append([]byte(nil), content...)
	return EvidenceReference{Reference: "evidence://sha256/" + digest, DigestSHA256: digest}, nil
}

func (s *MemoryEvidenceSink) Get(digest string) ([]byte, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	content, ok := s.blobs[digest]
	return append([]byte(nil), content...), ok
}

func (s *MemoryEvidenceSink) Resolve(ctx context.Context, reference EvidenceReference) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	content, ok := s.Get(reference.DigestSHA256)
	if !ok {
		return nil, fmt.Errorf("MemoryEvidenceSink.Resolve: digest %q not found", reference.DigestSHA256)
	}
	if err := validateResolvedEvidence(reference, content); err != nil {
		return nil, err
	}
	return content, nil
}

// FSEvidenceSink is a content-addressed filesystem evidence sink. Each blob is
// written once under <sha256>.blob; an existing entry must contain identical
// bytes or Put refuses it.
type FSEvidenceSink struct {
	root string
}

func NewFSEvidenceSink(root string) (*FSEvidenceSink, error) {
	if !filepath.IsAbs(root) {
		return nil, fmt.Errorf("NewFSEvidenceSink: root %q must be absolute", root)
	}
	info, err := os.Lstat(root)
	if err != nil {
		return nil, fmt.Errorf("NewFSEvidenceSink: inspect root: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, fmt.Errorf("NewFSEvidenceSink: root %q must be a real, non-symlink directory", root)
	}
	return &FSEvidenceSink{root: root}, nil
}

func (s *FSEvidenceSink) Put(ctx context.Context, content []byte) (EvidenceReference, error) {
	if s == nil {
		return EvidenceReference{}, fmt.Errorf("FSEvidenceSink.Put: nil sink")
	}
	if err := ctx.Err(); err != nil {
		return EvidenceReference{}, err
	}
	digest := evidenceDigest(content)
	name := filepath.Join(s.root, digest+".blob")
	file, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err == nil {
		if _, writeErr := file.Write(content); writeErr != nil {
			_ = file.Close()
			_ = os.Remove(name)
			return EvidenceReference{}, fmt.Errorf("FSEvidenceSink.Put: write: %w", writeErr)
		}
		if closeErr := file.Close(); closeErr != nil {
			_ = os.Remove(name)
			return EvidenceReference{}, fmt.Errorf("FSEvidenceSink.Put: close: %w", closeErr)
		}
	} else if os.IsExist(err) {
		existing, readErr := os.ReadFile(name)
		if readErr != nil {
			return EvidenceReference{}, fmt.Errorf("FSEvidenceSink.Put: verify existing: %w", readErr)
		}
		if !bytes.Equal(existing, content) {
			return EvidenceReference{}, fmt.Errorf("FSEvidenceSink.Put: digest %s already contains different bytes", digest)
		}
	} else {
		return EvidenceReference{}, fmt.Errorf("FSEvidenceSink.Put: create: %w", err)
	}
	return EvidenceReference{Reference: "evidence://sha256/" + digest, DigestSHA256: digest}, nil
}

func (s *FSEvidenceSink) Resolve(ctx context.Context, reference EvidenceReference) ([]byte, error) {
	if s == nil {
		return nil, fmt.Errorf("FSEvidenceSink.Resolve: nil sink")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	content, err := os.ReadFile(filepath.Join(s.root, reference.DigestSHA256+".blob"))
	if err != nil {
		return nil, fmt.Errorf("FSEvidenceSink.Resolve: read: %w", err)
	}
	if err := validateResolvedEvidence(reference, content); err != nil {
		return nil, err
	}
	return content, nil
}

func validateResolvedEvidence(reference EvidenceReference, content []byte) error {
	wantReference := "evidence://sha256/" + reference.DigestSHA256
	if reference.Reference != wantReference {
		return fmt.Errorf("evidence reference %q does not match digest-bound reference %q", reference.Reference, wantReference)
	}
	actual := evidenceDigest(content)
	if actual != reference.DigestSHA256 {
		return fmt.Errorf("evidence reference digest %q does not match bytes %q", reference.DigestSHA256, actual)
	}
	return nil
}

func evidenceDigest(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}
