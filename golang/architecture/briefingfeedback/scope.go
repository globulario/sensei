// SPDX-License-Identifier: AGPL-3.0-only

package briefingfeedback

import (
	"path/filepath"
	"sort"
	"strings"

	qp "github.com/globulario/sensei/golang/architecture/questionpromotion"
)

// hasWhitespace reports whether s carries any leading/trailing or embedded ASCII whitespace —
// used to reject padded/noncanonical identities WITHOUT repairing them.
func hasWhitespace(s string) bool {
	return s != strings.TrimSpace(s) || strings.ContainsAny(s, " \t\r\n")
}

// validateRequest strictly validates request identity and returns the canonical requested
// files, the task file set, and an invalid reason (empty when valid). It never repairs an
// identity — padding, relativity, and incoherence are rejected, never normalized.
//
//   - Repository root must be explicitly supplied, unpadded, and ABSOLUTE. A relative root is
//     rejected here rather than resolved against the process working directory (that would
//     derive authority from ambient cwd). Filesystem existence/symlink resolution happens once
//     in the injected resolveRoot dependency, not in this pure function.
//   - Repository identity must be present and canonical (the established repository domain).
//   - Requested domain: empty is NOT malformed (domain-neutral request); a non-empty domain
//     must be canonical AND correspond EXACTLY to the repository identity (no case/whitespace
//     repair, no prefix/suffix/basename/home-domain fallback).
//   - Task-scoped requests require non-empty task id + session id, a task repository domain that
//     equals the repository identity exactly, and strictly repository-relative task files.
//     Backslashes canonicalize to slashes so Windows and Unix inputs yield identical identities.
func validateRequest(req Request) (files []string, taskFiles map[string]bool, invalidReason string) {
	if strings.TrimSpace(req.RepositoryRoot) == "" {
		return nil, nil, "repository_root_absent"
	}
	if req.RepositoryRoot != strings.TrimSpace(req.RepositoryRoot) {
		return nil, nil, "repository_root_padded"
	}
	if !filepath.IsAbs(req.RepositoryRoot) {
		return nil, nil, "repository_root_relative"
	}
	if req.RepositoryIdentity == "" {
		return nil, nil, "repository_identity_absent"
	}
	if hasWhitespace(req.RepositoryIdentity) {
		return nil, nil, "repository_identity_malformed"
	}
	if req.RequestedDomain != "" {
		if hasWhitespace(req.RequestedDomain) {
			return nil, nil, "domain_malformed"
		}
		if req.RequestedDomain != req.RepositoryIdentity {
			return nil, nil, "repository_identity_incoherent"
		}
	}
	for _, f := range req.RequestedFiles {
		c, ok := canonicalRelFile(f)
		if !ok {
			return nil, nil, "unsafe_requested_file"
		}
		files = append(files, c)
	}
	files = sortedUniqueCanonical(files)
	taskFiles = map[string]bool{}
	if req.Task != nil {
		if req.Task.TaskID == "" {
			return nil, nil, "task_identity_absent"
		}
		if req.Task.SessionID == "" {
			return nil, nil, "session_identity_absent"
		}
		if req.Task.RepositoryDomain != req.RepositoryIdentity {
			return nil, nil, "task_domain_incoherent"
		}
		for _, f := range req.Task.Files {
			c, ok := canonicalRelFile(f)
			if !ok {
				return nil, nil, "unsafe_task_file"
			}
			taskFiles[c] = true
		}
	}
	return files, taskFiles, ""
}

// canonicalRelFile canonicalizes a path to a repository-relative slash form, rejecting unsafe
// paths WITHOUT repairing identity. Leading/trailing whitespace is a REJECTION, never trimmed
// away (trimming would let a padded path masquerade as a distinct canonical identity).
// Backslash→slash conversion is permitted for Windows parity. Rejects empty, absolute,
// drive-qualified, or empty/"."/".." segments.
func canonicalRelFile(f string) (string, bool) {
	if f != strings.TrimSpace(f) {
		return "", false // padded identity — refuse, do not repair
	}
	s := strings.ReplaceAll(f, "\\", "/")
	if s == "" || strings.HasPrefix(s, "/") {
		return "", false
	}
	if len(s) >= 2 && s[1] == ':' { // drive-qualified (e.g. C:)
		return "", false
	}
	for _, seg := range strings.Split(s, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return "", false
		}
		if seg != strings.TrimSpace(seg) {
			return "", false // padded segment — refuse
		}
	}
	return s, true
}

func sortedUnique(in []string) []string {
	var out []string
	for _, f := range in {
		if c, ok := canonicalRelFile(f); ok {
			out = append(out, c)
		} else if f != "" {
			out = append(out, f) // preserve an already-stored value we cannot canonicalize
		}
	}
	return sortedUniqueCanonical(out)
}

func sortedUniqueCanonical(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, f := range in {
		if !seen[f] {
			seen[f] = true
			out = append(out, f)
		}
	}
	sort.Strings(out)
	return out
}

// admit applies the VERIFIED effective scope: exact domain (empty verified domain is
// domain-neutral and may qualify), and file-scope intersection (an absent effective file
// scope is never assumed global).
func admit(rc qp.QuestionPromotionReceipt, req Request, files []string, taskFiles map[string]bool) bool {
	if rc.EffectiveScopeDomain != "" && rc.EffectiveScopeDomain != req.RequestedDomain {
		return false
	}
	if len(rc.EffectiveScopeFiles) == 0 {
		return false
	}
	return filesIntersect(rc.EffectiveScopeFiles, files, taskFiles)
}

// relevantFailure decides, from UNTRUSTED claimed metadata only, whether a FAILED candidate
// plausibly targets the requested scope (so it may appear as a scoped finding). It confers no
// authority. Relevance is domain-compatibility + file-intersection only: the claimed
// ORIGINATING task id is provenance, never a relevance filter — governed promotions are
// reusable across tasks, so a broken promotion whose originating task differs from the
// consuming task is still relevant when it targets the requested files. A candidate with
// insufficient claimed routing identity (no claimed files) is unrelated.
func relevantFailure(desc qp.CandidateDescriptor, req Request, files []string, taskFiles map[string]bool) bool {
	if !desc.Readable {
		return false
	}
	if desc.ClaimedDomain != "" && desc.ClaimedDomain != req.RequestedDomain {
		return false
	}
	if len(desc.ClaimedFiles) == 0 {
		return false // insufficient routing identity — never assumed globally relevant
	}
	return filesIntersect(desc.ClaimedFiles, files, taskFiles)
}

// filesIntersect reports whether any scope file (canonicalized) equals a requested file or a
// task file.
func filesIntersect(scope []string, requested []string, taskFiles map[string]bool) bool {
	req := map[string]bool{}
	for _, f := range requested {
		req[f] = true
	}
	for _, s := range scope {
		c, ok := canonicalRelFile(s)
		if !ok {
			continue
		}
		if req[c] || taskFiles[c] {
			return true
		}
	}
	return false
}
