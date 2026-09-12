// SPDX-License-Identifier: AGPL-3.0-only

package resultrecording

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"gopkg.in/yaml.v3"
)

// durableTransitionEvents counts result-transition entry files WITHOUT verifying
// the chain. countTransitionEvents verifies, and a ledger whose HEAD is missing or
// unpublished refuses verification -- which is exactly the state these tests
// need to observe.
func durableTransitionEvents(t *testing.T, taskDir string) int {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(taskDir, "ledger", "*.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, f := range files {
		if filepath.Base(f) == "HEAD.yaml" {
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var e closureprotocol.LedgerEntry
		if err := yaml.Unmarshal(data, &e); err != nil {
			t.Fatal(err)
		}
		if e.EventType == closureprotocol.LedgerEventResultTransitionRecorded {
			n++
		}
	}
	return n
}
