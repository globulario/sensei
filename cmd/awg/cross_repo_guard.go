// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func ensureCrossRepoRebuildPrereqs(agRepo, svcRepo string) error {
	if !requiresCombinedServicesRepo(agRepo) {
		return nil
	}
	if !isServicesRepoPath(svcRepo) {
		return fmt.Errorf("cross-repo rebuild requires the paired services repo; rerun with --services-repo or restore the sibling services checkout")
	}
	return nil
}

func isServicesRepoPath(repo string) bool {
	if strings.TrimSpace(repo) == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(repo, "docs", "awareness", "namespaces.yaml"))
	return err == nil
}

func requiresCombinedServicesRepo(agRepo string) bool {
	if strings.TrimSpace(agRepo) == "" {
		return false
	}
	txPath := defaultTransactionPath(agRepo)
	b, err := os.ReadFile(txPath)
	if err != nil {
		return false
	}
	for _, raw := range strings.Split(string(b), "\n") {
		line := strings.TrimSpace(raw)
		if !strings.HasPrefix(line, "repo\tservices\t") {
			continue
		}
		return line != "repo\tservices\tmissing"
	}
	return false
}
