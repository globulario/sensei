// SPDX-License-Identifier: AGPL-3.0-only

package interpretationclosure

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestGoCheckerDistinguishesContradictionFromUnknown(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/interpretation-probe\n\ngo 1.25\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "fixture"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "fixture", "types.go"), []byte("package fixture\n\ntype NamedString string\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	findings := CheckGoTruth(context.Background(), root, []GoProbe{
		{
			ClaimID:          "invariant.named-type-implements-text-unmarshaler",
			Kind:             GoProbeImplementsInterface,
			PackagePattern:   "example.com/interpretation-probe/fixture",
			TypeName:         "NamedString",
			Expected:         "true",
			InterfacePackage: "encoding",
			InterfaceName:    "TextUnmarshaler",
		},
		{
			ClaimID:        "invariant.named-type-has-string-underlying-kind",
			Kind:           GoProbeUnderlyingTypeEquals,
			PackagePattern: "example.com/interpretation-probe/fixture",
			TypeName:       "NamedString",
			Expected:       "string",
		},
		{
			ClaimID:        "invariant.unresolvable-package-remains-unknown",
			Kind:           GoProbeTypeExists,
			PackagePattern: "example.com/interpretation-probe/does-not-exist",
			TypeName:       "Missing",
			Expected:       "true",
		},
	})
	if len(findings) != 3 {
		t.Fatalf("findings=%d", len(findings))
	}
	if findings[0].Status != TruthContradicted {
		t.Fatalf("interface premise status=%q detail=%q, want contradicted", findings[0].Status, findings[0].Detail)
	}
	if findings[1].Status != TruthSupported {
		t.Fatalf("underlying-type premise status=%q detail=%q, want supported", findings[1].Status, findings[1].Detail)
	}
	if findings[2].Status != TruthUnknown {
		t.Fatalf("unresolvable package status=%q detail=%q, want unknown", findings[2].Status, findings[2].Detail)
	}
}
