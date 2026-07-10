// SPDX-License-Identifier: Apache-2.0

// @awareness namespace=globular.awareness_graph
// @awareness component=build.principle_check.ruleguard_scan
// @awareness file_role=ruleguard_dispatch_for_analysis_mode_ruleguard
package main

import (
	"bufio"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// runRuleguard invokes the `ruleguard` CLI against the services repo with
// the rule file colocated alongside this scanner in the awareness-graph
// repo, then parses its output into the same []site shape the regex
// classifier consumes downstream.
//
// Format produced by ruleguard:
//
//	<path>:<line>:<col>: <ruleID>: <message> (<rules-file>:<rule-line>)
//
// The classifier then runs unchanged: each site's path is matched against
// the invariant's exception_files / workflow_step_handler_files /
// unknown_helper_files lists, producing CONFORMANT / EXCEPTION /
// UNKNOWN / DRIFT verdicts in the same buckets the regex mode uses.
//
// Pre-flight: the ruleguard binary must be on PATH. We check before
// shelling out so the error message is actionable.
func runRuleguard(stderr io.Writer, repoRoot, awarenessGraphRoot, rulesFile string, actorScopes []string) ([]site, error) {
	if _, err := exec.LookPath("ruleguard"); err != nil {
		return nil, fmt.Errorf("ruleguard binary not found on PATH (install with: GOTOOLCHAIN=go1.25.0 go install github.com/quasilyte/go-ruleguard/cmd/ruleguard@latest) — %w", err)
	}

	rulesPath, _ := filepath.Abs(filepath.Join(awarenessGraphRoot, rulesFile))

	// Build target package args. actor_writer_dirs in the YAML use the
	// repo-relative `golang/<package>` form to match the regex-mode
	// invariants. ruleguard runs from `<repo>/golang` (the Go module
	// root), so we strip the leading `golang/` from each scope before
	// passing it as a `./<package>/...` pattern.
	args := []string{"-rules", rulesPath}
	for _, scope := range actorScopes {
		s := strings.TrimPrefix(scope, "golang/")
		args = append(args, "./"+s+"/...")
	}

	cmd := exec.Command("ruleguard", args...)
	cmd.Dir = filepath.Join(repoRoot, "golang")

	out, err := cmd.CombinedOutput()
	if err != nil {
		// ruleguard exits non-zero when it finds reports OR when it
		// emits diagnostic warnings (e.g. "package requires newer Go
		// version" — emitted as exit 3). We accept 1 (findings) and 3
		// (findings + warnings) as "scan completed, has output to
		// parse"; only fail for other codes.
		if ee, ok := err.(*exec.ExitError); !ok {
			return nil, fmt.Errorf("ruleguard execution: %w (output: %s)", err, string(out))
		} else if code := ee.ExitCode(); code != 1 && code != 3 {
			return nil, fmt.Errorf("ruleguard execution: exit status %d (output: %s)", code, string(out))
		}
	}

	return parseRuleguardOutput(stderr, string(out), repoRoot), nil
}

// parseRuleguardOutput converts ruleguard's stdout into []site. Lines
// that don't match the expected format (build-version warnings, blank
// lines) are silently skipped — we only care about findings.
//
// Expected line shape:
//
//	/abs/path/to/file.go:123:45: ruleID: message text (rules.go:NN)
//
// We also tolerate ruleguard's "package requires newer Go version" noise
// that appears when the binary's Go version is below the target module's
// — those lines are diagnostic and don't carry findings.
func parseRuleguardOutput(stderr io.Writer, out, repoRoot string) []site {
	findingRe := regexp.MustCompile(`^(.+?):(\d+):(\d+):\s+([A-Za-z_][A-Za-z0-9_]*):\s+(.+?)\s+\([^)]+\)\s*$`)

	var sites []site
	scanner := bufio.NewScanner(strings.NewReader(out))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		// Skip well-known noise lines.
		if strings.Contains(line, "package requires newer Go version") {
			continue
		}
		m := findingRe.FindStringSubmatch(line)
		if m == nil {
			// Unrecognized — surface to stderr in detail mode? For now
			// silent; the regex classifier doesn't crash on noise either.
			continue
		}
		absPath := m[1]
		lineNo, _ := strconv.Atoi(m[2])
		colNo, _ := strconv.Atoi(m[3])
		// ruleID := m[4] — captured but not stored; the bucket reason
		// carries the message text which already names the rule context.
		message := m[5]

		// Skip _test.go files — the regex scanner does the same in scan().
		// ruleguard scans everything by default; we filter to keep the two
		// engines comparable.
		if strings.HasSuffix(absPath, "_test.go") {
			continue
		}

		// Convert absolute path back to repo-relative so the classifier
		// can match against the suffix-based exception_files patterns.
		relPath, err := filepath.Rel(repoRoot, absPath)
		if err != nil {
			relPath = absPath
		}

		sites = append(sites, site{
			path:   relPath,
			line:   lineNo,
			column: colNo,
			text:   message,
		})
	}
	return sites
}
