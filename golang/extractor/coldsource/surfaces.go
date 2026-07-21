// SPDX-License-Identifier: AGPL-3.0-only

package coldsource

import (
	"path"
	"strings"
)

// isExcludedSurface reports whether a repo-relative path is a dependency,
// build-plumbing, vendored, generated, or bundled-example surface that must
// NOT seed a standalone architecture-rule candidate.
//
// Why this exists: the live cold-bootstrap experiments on two independent
// mature repos (caddyserver/caddy and etcd-io/etcd) BOTH produced exactly one
// noise candidate, and in both cases it came from a non-architecture surface —
// caddy's `go.mod` dependency churn, and etcd's `contrib/raftexample` import-
// path/build churn. These files change for reasons (version bumps, import-path
// moves, regeneration) that are not the project's engineering laws. Theming a
// rule on them produces confident-but-shallow candidates.
//
// Excluded surfaces are still allowed to appear as *evidence* on a real source
// theme (a commit that touches both go.mod and a real .go file still themes the
// .go file); they simply never form a standalone theme of their own.
func isExcludedSurface(p string) bool {
	p = strings.TrimSpace(p)
	if p == "" {
		return false
	}
	clean := strings.ToLower(path.Clean(strings.ReplaceAll(p, "\\", "/")))
	base := path.Base(clean)

	// 1. Dependency manifests and lockfiles — version/plumbing, not rules.
	// Manifests are excluded for every ecosystem, not just Go: a fix bumping a
	// version in package.json is churn exactly like a go.mod bump. (Lockfiles
	// alone are not enough — package.json churn was the dominant non-Go noise.)
	switch base {
	case "go.mod", "go.sum", "go.work", "go.work.sum",
		"vendor/modules.txt", "modules.txt",
		"package.json", "package-lock.json", "yarn.lock", "pnpm-lock.yaml",
		"gopkg.lock", "gopkg.toml",
		"cargo.toml", "cargo.lock",
		"pyproject.toml", "setup.py", "setup.cfg", "poetry.lock",
		"composer.json", "composer.lock",
		"gemfile", "gemfile.lock",
		"build.gradle", "pom.xml":
		return true
	}

	// 2. Vendored / third-party / bundled-example trees — not the product's
	//    own architecture surface.
	for _, seg := range strings.Split(clean, "/") {
		switch seg {
		case "vendor", "third_party", "contrib", "example", "examples", "testdata",
			// JS/TS build output + dependency trees — churn, not architecture.
			"node_modules", "dist":
			return true
		}
	}

	// 3. Generated code — regenerated from a source of truth, never a rule
	//    surface itself.
	switch {
	case strings.HasSuffix(base, ".pb.go"),
		strings.HasSuffix(base, ".pb.gw.go"),
		strings.HasSuffix(base, ".gen.go"),
		strings.HasSuffix(base, "_generated.go"),
		strings.HasPrefix(base, "zz_generated"),
		strings.HasPrefix(base, "mock_"):
		return true
	}

	return false
}

// surfaceTheme is the theme a file-anchored signal should carry: its file-
// concept theme, UNLESS the file is an excluded surface, in which case it
// returns "" so the caller withholds it from forming a standalone theme.
func surfaceTheme(p string) string {
	if isExcludedSurface(p) {
		return ""
	}
	return themeFromPath(p)
}
