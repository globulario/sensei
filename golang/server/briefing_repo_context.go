// SPDX-License-Identifier: Apache-2.0

// @awareness namespace=globular.awareness_graph
// @awareness component=server.briefing
// @awareness file_role=startup_repository_context
// @awareness implements=globular.awareness_graph:invariant.closure.briefing_feedback_server_repository_context_is_startup_owned
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// briefingRepositoryContext is the immutable startup-owned pair identifying the ONE repository
// whose committed promotions the server may verify for briefing feedback. The zero value
// (nil pointer on the server) means feedback context is unconfigured — feedback is unavailable
// while the graph briefing stays usable. No request handler mutates it.
type briefingRepositoryContext struct {
	Root   string // absolute, symlink-resolved, existing directory
	Domain string // exact, unpadded, whitespace-free repository domain (NOT homeDomain)
}

// establishBriefingRepositoryContext validates the startup-owned repo-root + repo-domain pair
// for the DIRECT server binary. It is strict and independent of the CLI wrapper:
//   - both absent → (nil, nil): feedback disabled, graph briefing unaffected;
//   - exactly one present → error (the two form one context);
//   - the root must be unpadded and ABSOLUTE — a relative root fails startup and is NEVER
//     passed through filepath.Abs (authority must not be derived from the process cwd);
//   - the root is resolved through symlinks ONCE and must be an existing directory;
//   - the domain must be present, unpadded, and free of embedded whitespace.
//
// homeDomain is not repository identity; this Domain identifies the filesystem repository.
func establishBriefingRepositoryContext(root, domain string) (*briefingRepositoryContext, error) {
	if root == "" && domain == "" {
		return nil, nil
	}
	if root == "" || domain == "" {
		return nil, fmt.Errorf("-repo-root and -repo-domain must be configured together")
	}
	if root != strings.TrimSpace(root) {
		return nil, fmt.Errorf("-repo-root is padded")
	}
	if !filepath.IsAbs(root) {
		return nil, fmt.Errorf("-repo-root must be absolute; a relative root would derive authority from the working directory")
	}
	if domain != strings.TrimSpace(domain) || strings.ContainsAny(domain, " \t\r\n") {
		return nil, fmt.Errorf("-repo-domain is padded or contains whitespace")
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, fmt.Errorf("-repo-root does not resolve: %w", err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return nil, fmt.Errorf("-repo-root: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("-repo-root is not a directory")
	}
	return &briefingRepositoryContext{Root: resolved, Domain: domain}, nil
}
