// SPDX-License-Identifier: Apache-2.0

package resultpipeline

import (
	"os"
	"testing"
)

// TestMain isolates this package's temporary directories from the global system
// temp dir. The pipeline (via resulttransition.BindRepositoryResult) and its
// materialization create os.MkdirTemp dirs; resulttransition's own
// temp-index-cleanup tests count sensei-result-index-* dirs in os.TempDir()
// before and after a bind. Because Go runs packages as concurrent processes over
// the same /tmp, this package's concurrent builds would otherwise race that
// count. Pointing TMPDIR at a private, package-local directory keeps every
// temp artifact this package creates out of the shared /tmp.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "resultpipeline-tmproot-")
	if err != nil {
		panic(err)
	}
	os.Setenv("TMPDIR", dir)
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
