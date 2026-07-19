// SPDX-License-Identifier: Apache-2.0

package briefingfeedback

import (
	"sort"
	"strings"

	qp "github.com/globulario/sensei/golang/architecture/questionpromotion"
)

// validateRequest strictly validates request identity and returns the canonical requested
// files, the task file set, and an invalid reason (empty when valid). An empty domain is NOT
// malformed; a whitespace/noncanonical domain IS. Unsafe requested/task file paths are
// rejected (never normalized into a safe-looking path). Backslashes are canonicalized to
// slashes so Windows and Unix inputs yield identical repository-relative identities.
func validateRequest(req Request) (files []string, taskFiles map[string]bool, invalidReason string) {
	if strings.TrimSpace(req.RepositoryRoot) == "" {
		return nil, nil, "repository_root_absent"
	}
	if req.RequestedDomain != strings.TrimSpace(req.RequestedDomain) || strings.ContainsAny(req.RequestedDomain, " \t\r\n") {
		return nil, nil, "domain_malformed"
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

// canonicalRelFile canonicalizes a path to a repository-relative slash form, rejecting
// unsafe paths (empty, absolute, drive-qualified, or containing empty/"."/".." segments).
func canonicalRelFile(f string) (string, bool) {
	s := strings.ReplaceAll(strings.TrimSpace(f), "\\", "/")
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
