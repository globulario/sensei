// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os/exec"
	"regexp"
	"strings"
)

// domainFromRemoteURL maps a git remote URL to a Sensei domain key
// (host/owner/repo, no .git). Handles https, ssh://, and scp-like
// git@host:owner/repo forms. Mirrors editor/vscode/src/projectDomain.ts. Returns
// "" when the URL can't be parsed.
func domainFromRemoteURL(raw string) string {
	u := strings.TrimSpace(raw)
	u = gitSuffix.ReplaceAllString(u, "")
	if u == "" {
		return ""
	}
	var host, path string
	if m := schemeURL.FindStringSubmatch(u); m != nil { // scheme://[user@]host[:port]/path
		host, path = m[1], m[2]
	} else if m := scpURL.FindStringSubmatch(u); m != nil { // [user@]host:path
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

var (
	gitSuffix = regexp.MustCompile(`(?i)\.git/?$`)
	schemeURL = regexp.MustCompile(`(?i)^[a-z][a-z0-9+.-]*://(?:[^@/]+@)?([^/:]+)(?::\d+)?/(.+)$`)
	scpURL    = regexp.MustCompile(`^(?:[^@]+@)?([^/:]+):(.+)$`)
)

// gitRemoteDomain returns the Sensei domain key for a repo checkout, derived
// from its origin remote URL. "" when the path isn't a git repo or has no
// origin. Overridable in tests.
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
