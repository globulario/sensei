// SPDX-License-Identifier: AGPL-3.0-only

package evaluatorcomposition

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/globulario/sensei/golang/architecture/runnercomposition"
	"golang.org/x/text/unicode/norm"
)

// ErrEvaluatorSurfaceClosed is returned by a surface after Close has begun.
// Close marks the typed handle closed before removing any backing directory,
// so a retained cooperative handle fails closed before its bytes disappear.
var ErrEvaluatorSurfaceClosed = errors.New("evaluator surface closed")

// SurfaceMode states which disposable filesystem shape an evaluator needs.
// Plain contains only the final candidate tree. GitDiff additionally contains
// a private .git directory whose HEAD is the exact verified base snapshot and
// whose working tree is the exact sealed candidate, so the existing Sensei
// gate owner can run `git diff HEAD` without consulting a live checkout.
type SurfaceMode string

const (
	SurfaceModePlain   SurfaceMode = "plain"
	SurfaceModeGitDiff SurfaceMode = "git-diff"
)

// EvaluatorSurface is one fresh, disposable materialization for exactly one
// evaluator invocation. Ref is the opaque logical identity carried by
// EvaluationInput. RootPath is intentionally available only through this
// typed handle and fails after Close; callers must not cache or publish it.
type EvaluatorSurface interface {
	Ref() string
	Mode() SurfaceMode
	RootPath() (string, error)
	Close() error
}

type fsEvaluatorSurface struct {
	ref    string
	mode   SurfaceMode
	root   string
	parent string

	closed atomic.Bool
	once   sync.Once
	err    error
}

func (s *fsEvaluatorSurface) Ref() string       { return s.ref }
func (s *fsEvaluatorSurface) Mode() SurfaceMode { return s.mode }

func (s *fsEvaluatorSurface) RootPath() (string, error) {
	if s.closed.Load() {
		return "", ErrEvaluatorSurfaceClosed
	}
	return s.root, nil
}

func (s *fsEvaluatorSurface) Close() error {
	s.once.Do(func() {
		// Revoke the sanctioned typed handle before removing bytes. An
		// in-process evaluator remains a cooperative boundary, just as O3's
		// CandidateWorkspace contract documents; this ordering prevents a
		// retained typed handle from racing directory destruction.
		s.closed.Store(true)
		s.err = os.RemoveAll(s.parent)
	})
	return s.err
}

// CandidateMaterializer derives evaluator surfaces from the canonical sealed
// CandidateArtifact plus the exact git object database containing its pinned
// BaseRevision. RepositoryRoot is used only by runnercomposition.ExtractSnapshot,
// which reads the full commit object directly and never reads HEAD or the live
// working tree. RepositoryDomain is an explicit caller binding and must equal
// the artifact's structural domain.
type CandidateMaterializer struct {
	RepositoryDomain string
	RepositoryRoot   string
}

// NewCandidateMaterializer validates the caller-owned repository-object
// capability once. It does not derive repository identity from the checkout.
func NewCandidateMaterializer(repositoryDomain, repositoryRoot string) (*CandidateMaterializer, error) {
	if strings.TrimSpace(repositoryDomain) == "" {
		return nil, fmt.Errorf("NewCandidateMaterializer: repositoryDomain must not be empty")
	}
	if !filepath.IsAbs(repositoryRoot) {
		return nil, fmt.Errorf("NewCandidateMaterializer: repositoryRoot %q must be absolute", repositoryRoot)
	}
	info, err := os.Lstat(repositoryRoot)
	if err != nil {
		return nil, fmt.Errorf("NewCandidateMaterializer: inspect repositoryRoot: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, fmt.Errorf("NewCandidateMaterializer: repositoryRoot %q must be a real, non-symlink directory", repositoryRoot)
	}
	return &CandidateMaterializer{RepositoryDomain: repositoryDomain, RepositoryRoot: repositoryRoot}, nil
}

// Materialize creates a fresh surface for evaluatorID. Before returning any
// path it independently proves all content lineage available at checkpoint 4:
//
//   - the artifact passes its canonical O3 validation;
//   - ExtractSnapshot reads exactly BaseRevision from git objects;
//   - that snapshot digest equals InputCandidateDigestSHA256;
//   - the sealed final manifest is materialized without traversal/collision;
//   - GitChangeDigest(snapshot, final) equals ProposedChangeDigestSHA256.
//
// A failure removes every partially-created directory. No live working-tree
// byte is read or made authoritative.
func (m *CandidateMaterializer) Materialize(ctx context.Context, artifact runnercomposition.CandidateArtifact, evaluatorID string, mode SurfaceMode) (EvaluatorSurface, error) {
	if m == nil {
		return nil, fmt.Errorf("CandidateMaterializer.Materialize: nil materializer")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := runnercomposition.ValidateCandidateArtifact(artifact); err != nil {
		return nil, fmt.Errorf("CandidateMaterializer.Materialize: invalid candidate artifact: %w", err)
	}
	if artifact.RepositoryDomain != m.RepositoryDomain {
		return nil, fmt.Errorf("CandidateMaterializer.Materialize: artifact repository_domain %q does not match materializer binding %q", artifact.RepositoryDomain, m.RepositoryDomain)
	}
	evaluatorID = strings.TrimSpace(evaluatorID)
	if evaluatorID == "" {
		return nil, fmt.Errorf("CandidateMaterializer.Materialize: evaluatorID must not be empty")
	}
	if mode != SurfaceModePlain && mode != SurfaceModeGitDiff {
		return nil, fmt.Errorf("CandidateMaterializer.Materialize: unsupported surface mode %q", mode)
	}
	if err := validateGitControlPaths(artifact.Manifest); err != nil {
		return nil, fmt.Errorf("CandidateMaterializer.Materialize: %w", err)
	}

	baseDir, baseManifest, baseDigest, baseCleanup, err := runnercomposition.ExtractSnapshot(ctx, m.RepositoryRoot, artifact.BaseRevision)
	if err != nil {
		return nil, fmt.Errorf("CandidateMaterializer.Materialize: extract exact base snapshot: %w", err)
	}
	baseCleaned := false
	defer func() {
		if !baseCleaned {
			_ = baseCleanup()
		}
	}()
	if baseDigest != artifact.InputCandidateDigestSHA256 {
		return nil, fmt.Errorf("CandidateMaterializer.Materialize: exact base snapshot digest %q does not match artifact input_candidate_digest_sha256 %q", baseDigest, artifact.InputCandidateDigestSHA256)
	}

	parent, err := os.MkdirTemp("", "sensei-o4-evaluator-surface-")
	if err != nil {
		return nil, fmt.Errorf("CandidateMaterializer.Materialize: create surface parent: %w", err)
	}
	succeeded := false
	defer func() {
		if !succeeded {
			_ = os.RemoveAll(parent)
		}
	}()

	finalDir := filepath.Join(parent, "final")
	if err := materializeManifest(finalDir, artifact.Manifest); err != nil {
		return nil, fmt.Errorf("CandidateMaterializer.Materialize: materialize sealed final manifest: %w", err)
	}
	changeDigest, err := runnercomposition.GitChangeDigest(ctx, baseDir, finalDir)
	if err != nil {
		return nil, fmt.Errorf("CandidateMaterializer.Materialize: recompute proposed change digest: %w", err)
	}
	if changeDigest != artifact.ProposedChangeDigestSHA256 {
		return nil, fmt.Errorf("CandidateMaterializer.Materialize: recomputed proposed change digest %q does not match artifact %q", changeDigest, artifact.ProposedChangeDigestSHA256)
	}

	root := finalDir
	if mode == SurfaceModeGitDiff {
		root = filepath.Join(parent, "gate-repo")
		if err := materializeManifest(root, baseManifest); err != nil {
			return nil, fmt.Errorf("CandidateMaterializer.Materialize: materialize gate base: %w", err)
		}
		if err := initializeDisposableGitBase(ctx, root, parent); err != nil {
			return nil, fmt.Errorf("CandidateMaterializer.Materialize: initialize disposable gate repository: %w", err)
		}
		if err := clearWorktreeExceptGit(root); err != nil {
			return nil, fmt.Errorf("CandidateMaterializer.Materialize: clear disposable gate working tree: %w", err)
		}
		if err := materializeManifestIntoExistingRoot(root, artifact.Manifest); err != nil {
			return nil, fmt.Errorf("CandidateMaterializer.Materialize: install sealed candidate in disposable gate repository: %w", err)
		}
		if err := stageDisposableGitCandidate(ctx, root, parent); err != nil {
			return nil, fmt.Errorf("CandidateMaterializer.Materialize: stage sealed candidate for exact HEAD diff: %w", err)
		}
	}

	if err := baseCleanup(); err != nil {
		return nil, fmt.Errorf("CandidateMaterializer.Materialize: clean exact base snapshot: %w", err)
	}
	baseCleaned = true

	ref := fmt.Sprintf("surface://%s/%s/%s", artifact.CandidateArtifactDigestSHA256, url.PathEscape(evaluatorID), mode)
	succeeded = true
	return &fsEvaluatorSurface{ref: ref, mode: mode, root: root, parent: parent}, nil
}

func materializeManifest(rootPath string, entries []runnercomposition.CandidateManifestEntry) error {
	if err := os.MkdirAll(rootPath, 0o755); err != nil {
		return err
	}
	return materializeManifestIntoExistingRoot(rootPath, entries)
}

func materializeManifestIntoExistingRoot(rootPath string, entries []runnercomposition.CandidateManifestEntry) error {
	canonical, err := runnercomposition.CanonicalizeManifest(entries)
	if err != nil {
		return err
	}
	if err := validateManifestFilesystemCollisions(canonical); err != nil {
		return err
	}
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return err
	}
	defer root.Close()

	for _, entry := range canonical {
		name := filepath.FromSlash(entry.Path)
		if err := root.MkdirAll(filepath.Dir(name), 0o755); err != nil {
			return fmt.Errorf("mkdir for %q: %w", entry.Path, err)
		}
		switch entry.Mode {
		case runnercomposition.ModeRegular:
			if err := root.WriteFile(name, entry.Content, 0o644); err != nil {
				return fmt.Errorf("write %q: %w", entry.Path, err)
			}
		case runnercomposition.ModeExecutable:
			if err := root.WriteFile(name, entry.Content, 0o755); err != nil {
				return fmt.Errorf("write executable %q: %w", entry.Path, err)
			}
		case runnercomposition.ModeSymlink:
			if err := validateMaterializedSymlink(entry.Path, entry.SymlinkTarget); err != nil {
				return err
			}
			if err := root.Symlink(entry.SymlinkTarget, name); err != nil {
				return fmt.Errorf("symlink %q: %w", entry.Path, err)
			}
		default:
			return fmt.Errorf("path %q has unsupported mode %q", entry.Path, entry.Mode)
		}
	}
	return nil
}

func validateGitControlPaths(entries []runnercomposition.CandidateManifestEntry) error {
	for _, entry := range entries {
		folded := strings.ToLower(entry.Path)
		if folded == ".git" || strings.HasPrefix(folded, ".git/") {
			return fmt.Errorf("candidate manifest path %q targets the reserved Git control directory", entry.Path)
		}
		if entry.Mode == runnercomposition.ModeSymlink {
			resolved := strings.ToLower(path.Clean(path.Join(path.Dir(entry.Path), entry.SymlinkTarget)))
			if resolved == ".git" || strings.HasPrefix(resolved, ".git/") {
				return fmt.Errorf("candidate symlink %q targets the reserved Git control directory through %q", entry.Path, entry.SymlinkTarget)
			}
		}
	}
	return nil
}

func validateManifestFilesystemCollisions(entries []runnercomposition.CandidateManifestEntry) error {
	byFold := make(map[string]string, len(entries))
	byNFC := make(map[string]string, len(entries))
	leaf := make(map[string]bool, len(entries))

	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		paths = append(paths, entry.Path)
	}
	sort.Strings(paths)
	for _, p := range paths {
		fold := strings.ToLower(p)
		if prior, ok := byFold[fold]; ok && prior != p {
			return fmt.Errorf("manifest paths %q and %q collide under case-insensitive folding", prior, p)
		}
		byFold[fold] = p
		nfc := norm.NFC.String(p)
		if prior, ok := byNFC[nfc]; ok && prior != p {
			return fmt.Errorf("manifest paths %q and %q collide under Unicode NFC normalization", prior, p)
		}
		byNFC[nfc] = p

		parts := strings.Split(p, "/")
		for i := 1; i < len(parts); i++ {
			parent := strings.Join(parts[:i], "/")
			if leaf[parent] {
				return fmt.Errorf("manifest path %q is nested beneath leaf entry %q", p, parent)
			}
		}
		leaf[p] = true
	}
	return nil
}

func validateMaterializedSymlink(entryPath, target string) error {
	if strings.Contains(target, "\\") {
		return fmt.Errorf("path %q: symlink target %q contains a backslash and is unsafe across platforms", entryPath, target)
	}
	if path.IsAbs(target) {
		return fmt.Errorf("path %q: absolute symlink target %q would escape the evaluator surface", entryPath, target)
	}
	resolved := path.Clean(path.Join(path.Dir(entryPath), target))
	if resolved == ".." || strings.HasPrefix(resolved, "../") {
		return fmt.Errorf("path %q: symlink target %q resolves outside the evaluator surface", entryPath, target)
	}
	return nil
}

func initializeDisposableGitBase(ctx context.Context, repoRoot, parent string) error {
	home := filepath.Join(parent, "git-home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		return err
	}
	for _, args := range [][]string{
		{"init", "-q"},
		{"-c", "core.autocrlf=false", "-c", "core.safecrlf=false", "add", "-A"},
		{"-c", "user.name=Sensei O4", "-c", "user.email=sensei-o4@invalid", "-c", "commit.gpgsign=false", "commit", "-q", "--allow-empty", "--no-gpg-sign", "-m", "sealed base"},
	} {
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = repoRoot
		cmd.Env = isolatedGitEnvironment(home)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
		}
	}
	return nil
}

func stageDisposableGitCandidate(ctx context.Context, repoRoot, parent string) error {
	home := filepath.Join(parent, "git-home")
	cmd := exec.CommandContext(ctx, "git", "-c", "core.autocrlf=false", "-c", "core.safecrlf=false", "add", "-A")
	cmd.Dir = repoRoot
	cmd.Env = isolatedGitEnvironment(home)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git add -A: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func isolatedGitEnvironment(home string) []string {
	env := make([]string, 0, len(os.Environ())+7)
	for _, item := range os.Environ() {
		key := item
		if i := strings.IndexByte(item, '='); i >= 0 {
			key = item[:i]
		}
		if strings.HasPrefix(strings.ToUpper(key), "GIT_") || key == "HOME" || key == "XDG_CONFIG_HOME" {
			continue
		}
		env = append(env, item)
	}
	nullDevice := os.DevNull
	env = append(env,
		"HOME="+home,
		"XDG_CONFIG_HOME="+home,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_SYSTEM="+nullDevice,
		"GIT_CONFIG_GLOBAL="+nullDevice,
		"GIT_TERMINAL_PROMPT=0",
	)
	return env
}

func clearWorktreeExceptGit(root string) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Name() == ".git" {
			continue
		}
		if err := os.RemoveAll(filepath.Join(root, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}
