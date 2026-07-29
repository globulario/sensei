// SPDX-License-Identifier: AGPL-3.0-only

// Command fakesensei is a lightweight test double for the sensei CLI,
// standing in for golang/architecture/benchmark's subprocess calls
// (bootstrap/build/infer-claims/prepare-change) in benchmark_test.go. It
// deliberately has zero dependency on cmd/awg (no cgo, no tree-sitter) so it
// compiles in a fraction of a second, keeping ordinary `go test ./...` fast
// — the real cmd/awg binary is used separately for genuine end-to-end
// verification, not inside this package's automated test suite.
//
// Behavior is driven entirely by environment variables so each test case
// can reconfigure it without recompiling:
//
//	FAKESENSEI_FAIL_STAGE   subcommand name to fail (exit 1) on
//	FAKESENSEI_PREPARE_YAML literal stdout for the "prepare-change" subcommand;
//	                        defaults to a canned admitted/closed response
package main

import (
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "fakesensei: subcommand required")
		os.Exit(2)
	}
	stage := os.Args[1]
	args := os.Args[2:]
	if os.Getenv("FAKESENSEI_FAIL_STAGE") == stage {
		fmt.Fprintf(os.Stderr, "fakesensei: configured failure for %s\n", stage)
		os.Exit(1)
	}
	switch stage {
	case "bootstrap":
		path := flagValue(args, "--path")
		dir := filepath.Join(path, "docs", "awareness", "generated")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			fail(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "components.yaml"), []byte("components: []\n"), 0o644); err != nil {
			fail(err)
		}
	case "build":
		out := flagValue(args, "-output")
		if err := os.WriteFile(out, []byte("# fake graph\n"), 0o644); err != nil {
			fail(err)
		}
		fmt.Println("total: 0 triples, validated")
	case "infer-claims":
		out := flagValue(args, "--output")
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			fail(err)
		}
		if err := os.WriteFile(out, []byte("architecture_claims:\n  claims: []\n"), 0o644); err != nil {
			fail(err)
		}
		fmt.Println("infer-claims: wrote 0 claim(s)")
	case "prepare-change":
		yamlOut := os.Getenv("FAKESENSEI_PREPARE_YAML")
		if yamlOut == "" {
			yamlOut = `architecture_prepare_change:
  task_id: task.fake.000000000000
  session:
    closure_verdict: closed
    convergence_status: closed
    admission_decision: admitted
`
		}
		fmt.Println(yamlOut)
	default:
		fmt.Fprintf(os.Stderr, "fakesensei: unknown subcommand %q\n", stage)
		os.Exit(2)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "fakesensei:", err)
	os.Exit(1)
}

// flagValue returns the value following the first exact occurrence of name
// in args (e.g. flagValue(args, "--path") for ["--path", "/x"] -> "/x").
func flagValue(args []string, name string) string {
	for i, a := range args {
		if a == name && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}
