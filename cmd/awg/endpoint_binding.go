// SPDX-License-Identifier: AGPL-3.0-only

// endpoint_binding.go closes the disagreement issue #212 names: a project
// config file that states where a command reads or writes, and a command
// that decides from a flag default instead and never consults it.
//
// The cost it records is concrete: a throwaway domain published into a
// shared services store because an edited store_url was believed to have
// redirected the build. Seven aw:repo tags landed on subjects another
// domain owned, the global marker rotated, and that domain lost its proof
// — while the build reported success and the closure report said PROVEN.
// The config had been edited first, precisely to prevent it.
//
// The rule here does not change where anything resolves to. It refuses to
// let a configured endpoint and an unstated resolved endpoint disagree in
// silence:
//
//   - a flag given on the command line wins, always. That is the operator
//     naming the endpoint at the point of use, not a silent disagreement,
//     and it keeps every existing scripted invocation working;
//   - no flag, and the configured value equals the resolved one: proceed;
//   - no flag, and they differ: refuse, naming the config path, the
//     configured value and the resolved value, BEFORE the command reaches
//     the endpoint. For a store load that is before a single triple
//     changes, beside publication's existing pre-mutation admission gate;
//   - no configured value, or no config file: today's behavior, unchanged.
//
// A malformed config is an error, never a skipped tier — the same law
// repo_domain_binding.go applies to checkout identity: configuration that
// fails to parse must fail visibly, not be silently worked around by
// falling through to a guessed value.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/globulario/sensei/golang/statedir"
	"gopkg.in/yaml.v3"
)

// endpointConfig is the store: and server: sections of .sensei/config.yaml.
// Only the endpoint fields are modeled; every other section is ignored, so
// this read never constrains what else the file may carry.
type endpointConfig struct {
	Store struct {
		QueryURL string `yaml:"query_url"`
		StoreURL string `yaml:"store_url"`
	} `yaml:"store"`
	Server struct {
		Addr string `yaml:"addr"`
	} `yaml:"server"`
}

// endpointConfigPath returns the resolved .sensei/config.yaml (or legacy
// .awg/config.yaml) path for root.
func endpointConfigPath(root string) string {
	return statedir.Path(root, "config.yaml")
}

// loadEndpointConfig reads root's endpoint configuration. A missing config
// file is not an error — it returns a zero-value config, the same as an
// existing config that states no endpoint.
func loadEndpointConfig(root string) (endpointConfig, error) {
	path := endpointConfigPath(root)
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return endpointConfig{}, nil
		}
		return endpointConfig{}, fmt.Errorf("read %s: %w", path, err)
	}
	var cfg endpointConfig
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return endpointConfig{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return cfg, nil
}

// configuredStoreURL / configuredServerAddr return the endpoint root's
// config states, or "" when it states none.
func (c endpointConfig) configuredStoreURL() string   { return strings.TrimSpace(c.Store.StoreURL) }
func (c endpointConfig) configuredServerAddr() string { return strings.TrimSpace(c.Server.Addr) }

// requireEndpointAgreement refuses when root's config names an endpoint,
// the operator did not name one on the command line, and the value the
// command resolved differs from the configured one. flagName is the flag
// as an operator types it (e.g. "-store-url"); configKey is the config
// path it corresponds to (e.g. "store.store_url").
//
// Returns nil when the command may proceed. The returned error is already
// phrased for stderr and names both values, so a caller prints it as-is.
func requireEndpointAgreement(fs *flag.FlagSet, root, flagName, configKey, configured, resolved string) error {
	if flagPassed(fs, strings.TrimPrefix(flagName, "-")) {
		return nil
	}
	configured = strings.TrimSpace(configured)
	if configured == "" {
		return nil
	}
	if configured == strings.TrimSpace(resolved) {
		return nil
	}
	return fmt.Errorf(`refusing to act on an endpoint the project config does not name.

  %s states  %s: %s
  resolved endpoint  %s

Nothing has been read from or written to either endpoint. The configured
value is the one an operator reads before running the command, so a
resolved endpoint that differs from it is never assumed to be intended.

Resolve it one of two ways:
  - use the configured endpoint, by removing whatever redirects it
    (a SENSEI_* environment variable in this shell, most often); or
  - name the endpoint you mean explicitly: %s <endpoint>`,
		endpointConfigPath(root), configKey, configured, resolved, flagName)
}

// requireStoreURLAgreement is requireEndpointAgreement for a command's
// -store-url, the flag that decides which store a load mutates.
func requireStoreURLAgreement(fs *flag.FlagSet, root, resolved string) error {
	cfg, err := loadEndpointConfig(root)
	if err != nil {
		return fmt.Errorf("endpoint configuration is malformed: %w", err)
	}
	return requireEndpointAgreement(fs, root, "-store-url", "store.store_url", cfg.configuredStoreURL(), resolved)
}

// requireServerAddrAgreement is requireEndpointAgreement for a command's
// -addr, the flag that decides which server's authority is asserted.
func requireServerAddrAgreement(fs *flag.FlagSet, root, resolved string) error {
	cfg, err := loadEndpointConfig(root)
	if err != nil {
		return fmt.Errorf("endpoint configuration is malformed: %w", err)
	}
	return requireEndpointAgreement(fs, root, "-addr", "server.addr", cfg.configuredServerAddr(), resolved)
}
