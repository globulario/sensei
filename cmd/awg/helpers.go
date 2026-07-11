// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/globulario/sensei/golang/client"
	"github.com/globulario/sensei/golang/statedir"
)

// connectAWG dials the awareness-graph server at the given address.
// All gRPC commands share this helper.
func connectAWG(addr string) (*client.Client, error) {
	return client.Dial(addr)
}

// resolveProjectRoot walks up from cwd looking for docs/awareness/ or a state
// dir config (.sensei/config.yaml, or legacy .awg/config.yaml). Returns cwd as
// fallback.
func resolveProjectRoot(explicit string) (string, error) {
	if explicit != "" {
		return filepath.Abs(explicit)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := cwd
	for {
		if _, err := os.Stat(filepath.Join(dir, "docs", "awareness")); err == nil {
			return dir, nil
		}
		// Detect a project by its state dir marker (.sensei, or legacy .awg).
		for _, name := range []string{statedir.DefaultName, statedir.LegacyName} {
			if _, err := os.Stat(filepath.Join(dir, name, "config.yaml")); err == nil {
				return dir, nil
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return cwd, nil
}

// resolveServicesRepo walks up from cwd looking for docs/awareness/namespaces.yaml.
func resolveServicesRepo(explicit string) (string, error) {
	if explicit != "" {
		return filepath.Abs(explicit)
	}
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, sErr := os.Stat(filepath.Join(dir, "docs", "awareness", "namespaces.yaml")); sErr == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	// When invoked from inside the awareness-graph repo, the services checkout is
	// usually a sibling directory rather than an ancestor. Rebuild/audit must find
	// that paired corpus or they regenerate an AWG-only seed and drop offline
	// preflight knowledge for service-owned paths.
	cwd, err := os.Getwd()
	if err != nil {
		return "", nil
	}
	for dir := cwd; ; {
		if _, sErr := os.Stat(filepath.Join(dir, "golang", "server", "embeddata")); sErr == nil {
			candidate := filepath.Join(filepath.Dir(dir), "services")
			if _, cErr := os.Stat(filepath.Join(candidate, "docs", "awareness", "namespaces.yaml")); cErr == nil {
				return candidate, nil
			}
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", nil // not found
}

// resolveAGRepo finds the awareness-graph repo as a sibling of svcRepo.
func resolveAGRepo(explicit, svcRepo string) (string, error) {
	if explicit != "" {
		return filepath.Abs(explicit)
	}
	// If we're already inside the AG repo, detect it.
	cwd, _ := os.Getwd()
	for dir := cwd; ; {
		if _, err := os.Stat(filepath.Join(dir, "golang", "server", "embeddata")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	// Try sibling of services repo.
	if svcRepo != "" {
		candidate := filepath.Join(filepath.Dir(svcRepo), "awareness-graph")
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate, nil
		}
	}
	return "", nil // not found
}

// collectInputDirs returns awareness YAML directories and the intent directory.
func collectInputDirs(svcRepo, agRepo string) (inputDirs []string, intentDir string, err error) {
	var dirs []string
	if agRepo != "" {
		dirs = appendExistingDir(dirs,
			filepath.Join(agRepo, "docs", "awareness"),
			filepath.Join(agRepo, "docs", "awareness", "generated"),
			filepath.Join(agRepo, "eval", "multi-swe-bench", "contracts"),
			filepath.Join(agRepo, "eval", "multi-swe-bench", "notes", "learning_events"),
			// Always include the ag-repo's own intent dir so awg.* intent refs
			// resolve even when a services repo (with its own docs/intent) is present.
			filepath.Join(agRepo, "docs", "intent"),
		)
	}
	if svcRepo != "" {
		dirs = appendExistingDir(dirs,
			filepath.Join(svcRepo, "docs", "awareness"),
			filepath.Join(svcRepo, "docs", "awareness", "generated"),
		)
	}
	if len(dirs) == 0 {
		// Fallback: look in cwd
		cwd, _ := os.Getwd()
		dirs = appendExistingDir(dirs,
			filepath.Join(cwd, "docs", "awareness"),
			filepath.Join(cwd, "eval", "multi-swe-bench", "contracts"),
			filepath.Join(cwd, "eval", "multi-swe-bench", "notes", "learning_events"),
		)
	}
	if svcRepo != "" {
		intent := filepath.Join(svcRepo, "docs", "intent")
		if _, sErr := os.Stat(intent); sErr == nil {
			intentDir = intent
		}
	}
	// When no services-repo intent dir was found, fall back to the ag-repo's
	// own intent dir so repo-eval on the awareness-graph repo itself resolves
	// intent refs (e.g. awg.briefing_is_the_primary_interface) that live there.
	if intentDir == "" && agRepo != "" {
		intent := filepath.Join(agRepo, "docs", "intent")
		if _, sErr := os.Stat(intent); sErr == nil {
			intentDir = intent
		}
	}
	return dirs, intentDir, nil
}

func appendExistingDir(dirs []string, candidates ...string) []string {
	for _, dir := range candidates {
		if _, err := os.Stat(dir); err == nil {
			dirs = append(dirs, dir)
		}
	}
	return dirs
}

func servicesGeneratedDir(svcRepo string) string {
	return filepath.Join(svcRepo, "docs", "awareness", "generated")
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func relTo(root, path string) string {
	r, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return r
}

func strOrDash(s string) string {
	if s == "" {
		return "(unstamped)"
	}
	return s
}

func stringsField(m map[string]interface{}, key string) []string {
	v, ok := m[key]
	if !ok {
		return nil
	}
	list, ok := v.([]interface{})
	if !ok {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
			out = append(out, strings.TrimSpace(s))
		}
	}
	return out
}
