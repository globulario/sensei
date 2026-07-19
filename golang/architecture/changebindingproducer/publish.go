// SPDX-License-Identifier: Apache-2.0

package changebindingproducer

import (
	"os"
	"path/filepath"

	"github.com/globulario/sensei/golang/architecture/changebinding"
	"gopkg.in/yaml.v3"
)

// Publish writes an authoritative, self-validated binding to a single documented path as a
// deterministic, strictly-parseable v1 artifact — exactly one binding, no timestamps or
// random values, no secrets/tokens/raw event payloads. Reruns for the same subject produce
// byte-identical output (idempotent). It NEVER silently overwrites a DIFFERENT binding at
// the path: a conflicting existing publication fails closed (contradictory), which is how a
// force-push to a new head cannot reuse the previous head's publication. It returns a typed
// failure (FailNone on success).
func Publish(b changebinding.ChangeTaskBinding, path string) ProducerFailure {
	data, err := yaml.Marshal(b)
	if err != nil {
		return FailBindingConstruction
	}

	// Refuse to overwrite a different existing publication.
	if existing, rerr := os.ReadFile(path); rerr == nil {
		prev, v := changebinding.ParseBinding(existing)
		if v != "" || prev != b {
			return FailContradictoryPublication
		}
		// Identical → idempotent; nothing to rewrite.
		return FailNone
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return FailPublicationWrite
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return FailPublicationWrite
	}
	return FailNone
}
