// SPDX-License-Identifier: Apache-2.0

package proofdischarge

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"gopkg.in/yaml.v3"
)

// TestFixtureProofDischarges cross-checks that the proof_discharge records the
// shared closure fixtures carry are valid under the frozen validator this engine
// targets — the same pattern closureprotocol/fixtures_test.go uses. Bundles that
// carry no proof_discharge are skipped.
func TestFixtureProofDischarges(t *testing.T) {
	root := filepath.Join("..", "..", "..", "docs", "fixtures", "architectural-closure", "v1")
	paths := []string{
		"completed/bundle.yaml",
		"migration-in-progress/bundle.yaml",
	}
	for _, rel := range paths {
		t.Run(rel, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(root, rel))
			if err != nil {
				t.Fatal(err)
			}
			var env struct {
				Fixture struct {
					Records struct {
						ProofDischarge *closureprotocol.ProofDischarge `yaml:"proof_discharge"`
					} `yaml:"records"`
				} `yaml:"closure_protocol_fixture"`
			}
			if err := yaml.Unmarshal(data, &env); err != nil {
				t.Fatal(err)
			}
			pd := env.Fixture.Records.ProofDischarge
			if pd == nil {
				t.Skip("no proof_discharge in bundle")
			}
			if err := closureprotocol.ValidateProofDischarge(*pd); err != nil {
				t.Fatalf("frozen validator rejected fixture proof_discharge: %v", err)
			}
		})
	}
}
