// SPDX-License-Identifier: AGPL-3.0-only

package factextract

import (
	"os/exec"
	"regexp"
	"sort"
	"strings"
)

// Small, pure, generic helpers the extraction shares with the cmd/awg CLI. They
// are duplicated here (rather than imported from cmd/awg, which this package must
// not depend on) because they are trivial utilities, not extraction semantics.

var (
	gitSuffix  = regexp.MustCompile(`(?i)\.git/?$`)
	schemeURL  = regexp.MustCompile(`(?i)^[a-z][a-z0-9+.-]*://(?:[^@/]+@)?([^/:]+)(?::\d+)?/(.+)$`)
	scpURL     = regexp.MustCompile(`^(?:[^@]+@)?([^/:]+):(.+)$`)
	nonSlugRun = regexp.MustCompile(`[^a-z0-9]+`)
)

// domainFromRemoteURL maps a git remote URL to a Sensei domain key
// (host/owner/repo, no .git). Returns "" when the URL can't be parsed.
func domainFromRemoteURL(raw string) string {
	u := strings.TrimSpace(raw)
	u = gitSuffix.ReplaceAllString(u, "")
	if u == "" {
		return ""
	}
	var host, path string
	if m := schemeURL.FindStringSubmatch(u); m != nil {
		host, path = m[1], m[2]
	} else if m := scpURL.FindStringSubmatch(u); m != nil {
		host, path = m[1], m[2]
	} else {
		return ""
	}
	host = strings.ToLower(host)
	path = strings.Trim(path, "/")
	if host == "" || path == "" {
		return ""
	}
	return host + "/" + path
}

// gitRemoteDomain returns the Sensei domain key for a repo checkout, derived from
// its origin remote URL. "" when the path isn't a git repo or has no origin.
// Overridable in tests.
var gitRemoteDomain = func(repoPath string) string {
	if strings.TrimSpace(repoPath) == "" {
		return ""
	}
	out, err := exec.Command("git", "-C", repoPath, "config", "--get", "remote.origin.url").Output()
	if err != nil {
		return ""
	}
	return domainFromRemoteURL(string(out))
}

func isTestFile(name string) bool {
	lower := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lower, "_test.go"):
		return true
	case strings.HasSuffix(lower, ".test.ts") || strings.HasSuffix(lower, ".spec.ts"),
		strings.HasSuffix(lower, ".test.tsx") || strings.HasSuffix(lower, ".spec.tsx"),
		strings.HasSuffix(lower, ".test.js") || strings.HasSuffix(lower, ".spec.js"):
		return true
	case strings.HasPrefix(lower, "test_") && strings.HasSuffix(lower, ".py"),
		strings.HasSuffix(lower, "_test.py"):
		return true
	}
	return false
}

func bootstrapExcludedDir(name string) bool {
	switch name {
	case "vendor", "node_modules", ".git", "dist", "build", "bin", "out",
		"third_party", "thirdparty", "generated", "candidates", ".sensei", ".awg",
		"testdata", "target", ".venv", "venv", "__pycache__", ".idea", ".vscode":
		return true
	}
	return false
}

func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = nonSlugRun.ReplaceAllString(s, "_")
	s = strings.Trim(s, "_")
	if len(s) > 60 {
		s = strings.Trim(s[:60], "_")
	}
	if s == "" {
		s = "entry"
	}
	return s
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func nonEmptySlice(s string) []string {
	if strings.TrimSpace(s) == "" {
		return []string{}
	}
	return []string{s}
}

func dedupeSorted(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, value := range in {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
