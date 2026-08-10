// SPDX-License-Identifier: AGPL-3.0-only

//go:build windows

package runnercomposition

import "testing"

// mkfifoOrSkip skips the calling test on Windows. FIFOs do not exist there, so
// the hang-prevention behavior the callers prove has nothing to exercise -- the
// tests are skipped rather than deleted so the Unix build keeps proving it.
// See fifo_unix_test.go for the real implementation.
func mkfifoOrSkip(t *testing.T, path string) {
	t.Helper()
	t.Skip("FIFOs do not exist on Windows")
}
