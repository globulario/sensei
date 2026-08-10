// SPDX-License-Identifier: AGPL-3.0-only

//go:build unix

package runnercomposition

import (
	"syscall"
	"testing"
)

// mkfifoOrSkip creates a FIFO at path, or fails the test if it cannot.
//
// This lives behind a build tag because syscall.Mkfifo is not defined on
// Windows -- FIFOs are a POSIX construct with no Windows equivalent. Calling
// it unguarded from a test file with no build tag is what broke the Windows
// compile gate; see fifo_windows_test.go for the skipping counterpart and
// failure.sensei.platform_specific_source_never_compiled_by_ci for the class.
func mkfifoOrSkip(t *testing.T, path string) {
	t.Helper()
	if err := syscall.Mkfifo(path, 0o644); err != nil {
		t.Fatal(err)
	}
}
