// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// endpointConfigProject writes body as root's .sensei/config.yaml and
// returns root. An empty body writes no config file at all.
func endpointConfigProject(t *testing.T, body string) string {
	t.Helper()
	root := t.TempDir()
	if body == "" {
		return root
	}
	if err := os.MkdirAll(filepath.Join(root, ".sensei"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".sensei", "config.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// storeURLFlagSet builds a flag set carrying -store-url, parsed with args,
// so flagPassed observes exactly what an operator typed.
func storeURLFlagSet(t *testing.T, args ...string) *flag.FlagSet {
	t.Helper()
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.String("store-url", "http://localhost:7878/store?default", "")
	if err := fs.Parse(args); err != nil {
		t.Fatal(err)
	}
	return fs
}

const endpointConfigBody = `sources:
  - docs/awareness
store:
  data_dir: .sensei/data
  query_url: http://localhost:7878/query
  store_url: http://localhost:7878/store?default
server:
  addr: localhost:10120
`

func TestRequireStoreURLAgreementAcceptsAnEndpointTheConfigNames(t *testing.T) {
	root := endpointConfigProject(t, endpointConfigBody)
	err := requireStoreURLAgreement(storeURLFlagSet(t), root, "http://localhost:7878/store?default")
	if err != nil {
		t.Fatalf("agreeing endpoints were refused: %v", err)
	}
}

func TestRequireStoreURLAgreementRefusesAnEndpointTheConfigDoesNotName(t *testing.T) {
	root := endpointConfigProject(t, endpointConfigBody)
	err := requireStoreURLAgreement(storeURLFlagSet(t), root, "http://localhost:9999/store?default")
	if err == nil {
		t.Fatal("a store the config does not name was accepted")
	}
	// Both values have to appear, or the operator cannot see which of the
	// two signals disagreed -- that is the whole failure #212 describes.
	for _, want := range []string{
		"http://localhost:7878/store?default",
		"http://localhost:9999/store?default",
		"store.store_url",
		filepath.Join(root, ".sensei", "config.yaml"),
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal does not name %q:\n%v", want, err)
		}
	}
}

func TestRequireStoreURLAgreementYieldsToAnExplicitFlag(t *testing.T) {
	root := endpointConfigProject(t, endpointConfigBody)
	fs := storeURLFlagSet(t, "-store-url", "http://localhost:9999/store?default")
	if err := requireStoreURLAgreement(fs, root, "http://localhost:9999/store?default"); err != nil {
		t.Fatalf("an endpoint named on the command line was refused: %v", err)
	}
}

func TestRequireStoreURLAgreementIgnoresAProjectThatStatesNoEndpoint(t *testing.T) {
	for name, body := range map[string]string{
		"no config file":    "",
		"no store section":  "sources:\n  - docs/awareness\n",
		"empty store_url":   "store:\n  store_url: \"\"\n",
		"store without url": "store:\n  data_dir: .sensei/data\n",
	} {
		t.Run(name, func(t *testing.T) {
			root := endpointConfigProject(t, body)
			if err := requireStoreURLAgreement(storeURLFlagSet(t), root, "http://localhost:9999/store?default"); err != nil {
				t.Fatalf("a project stating no endpoint constrained the command: %v", err)
			}
		})
	}
}

// A config that cannot be parsed is an error, never a skipped tier: the
// same law repo_domain_binding.go applies to checkout identity. Falling
// through would resolve to an endpoint precisely when the file that was
// supposed to decide could not be read.
func TestRequireStoreURLAgreementRefusesAMalformedConfig(t *testing.T) {
	root := endpointConfigProject(t, "store:\n  store_url: [unterminated\n")
	err := requireStoreURLAgreement(storeURLFlagSet(t), root, "http://localhost:7878/store?default")
	if err == nil {
		t.Fatal("a malformed endpoint configuration was silently skipped")
	}
	if !strings.Contains(err.Error(), "malformed") {
		t.Fatalf("refusal does not say the configuration is malformed: %v", err)
	}
}

func TestRequireServerAddrAgreementRefusesAServerTheConfigDoesNotName(t *testing.T) {
	root := endpointConfigProject(t, endpointConfigBody)
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.String("addr", "localhost:10120", "")
	if err := fs.Parse(nil); err != nil {
		t.Fatal(err)
	}
	err := requireServerAddrAgreement(fs, root, "localhost:19120")
	if err == nil {
		t.Fatal("a server the config does not name was accepted")
	}
	for _, want := range []string{"localhost:10120", "localhost:19120", "server.addr"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal does not name %q:\n%v", want, err)
		}
	}

	agreeing := flag.NewFlagSet("test", flag.ContinueOnError)
	agreeing.String("addr", "localhost:10120", "")
	if err := agreeing.Parse(nil); err != nil {
		t.Fatal(err)
	}
	if err := requireServerAddrAgreement(agreeing, root, "localhost:10120"); err != nil {
		t.Fatalf("agreeing endpoints were refused: %v", err)
	}
}
