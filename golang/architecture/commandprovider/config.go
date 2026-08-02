// SPDX-License-Identifier: AGPL-3.0-only

// Package commandprovider implements the generic O6 command-backed
// providerport.Provider adapter. It executes one explicitly configured argv,
// exchanges one closed O2 request/result pair over stdin/stdout, and records
// bounded stderr as observation evidence only.
package commandprovider

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/globulario/sensei/golang/architecture/providerport"
	"github.com/globulario/sensei/golang/architecture/synthesis"
)

const (
	defaultProviderKind = "command"
	configurationTime   = "1970-01-01T00:00:00Z"
)

// Config is the complete immutable capability granted to one command adapter.
// Command must be an absolute executable path. Args are passed directly, never
// through a shell. WorkDir, environment inheritance, supported operations, and
// stdout/stderr limits are all explicit and fixed before execution.
type Config struct {
	ProviderID      string
	ProviderKind    string
	ModelIdentifier string

	Command string
	Args    []string
	WorkDir string

	EnvironmentAllowlist []string
	SupportedOperations  []providerport.Operation

	MaxStdoutBytes int64
	MaxStderrBytes int64
}

// Adapter is a generic command-backed providerport.Provider.
type Adapter struct {
	config       Config
	capabilities providerport.Capabilities
}

// New validates and freezes cfg. The capability snapshot is computed once, so
// Describe cannot consult mutable ambient state or accidentally acquire routing
// authority later.
func New(cfg Config) (*Adapter, error) {
	cfg = normalizeConfig(cfg)
	if err := validateConfig(cfg); err != nil {
		return nil, err
	}

	capabilities := providerport.Capabilities{
		SchemaVersion: providerport.CapabilitiesSchemaVersion,
		ProviderObservation: synthesis.ProviderObservation{
			ProviderID:      cfg.ProviderID,
			ProviderKind:    cfg.ProviderKind,
			ModelIdentifier: cfg.ModelIdentifier,
			ObservedAt:      configurationTime,
		},
		SupportedOperations: append([]providerport.Operation(nil), cfg.SupportedOperations...),
	}
	digest, err := providerport.CapabilitiesDigest(capabilities)
	if err != nil {
		return nil, fmt.Errorf("commandprovider: compute capabilities digest: %w", err)
	}
	capabilities.CapabilitiesDigestSHA256 = digest

	return &Adapter{config: cfg, capabilities: capabilities}, nil
}

func normalizeConfig(cfg Config) Config {
	cfg.ProviderID = strings.TrimSpace(cfg.ProviderID)
	cfg.ProviderKind = strings.TrimSpace(cfg.ProviderKind)
	if cfg.ProviderKind == "" {
		cfg.ProviderKind = defaultProviderKind
	}
	cfg.ModelIdentifier = strings.TrimSpace(cfg.ModelIdentifier)
	cfg.Command = strings.TrimSpace(cfg.Command)
	cfg.WorkDir = strings.TrimSpace(cfg.WorkDir)
	cfg.Args = append([]string(nil), cfg.Args...)
	cfg.EnvironmentAllowlist = uniqueSortedStrings(cfg.EnvironmentAllowlist)
	cfg.SupportedOperations = uniqueSortedOperations(cfg.SupportedOperations)
	return cfg
}

func validateConfig(cfg Config) error {
	if cfg.ProviderID == "" {
		return errors.New("commandprovider: provider id is required")
	}
	if cfg.Command == "" {
		return errors.New("commandprovider: command is required")
	}
	if !filepath.IsAbs(cfg.Command) {
		return fmt.Errorf("commandprovider: command must be an absolute path: %q", cfg.Command)
	}
	info, err := os.Stat(cfg.Command)
	if err != nil {
		return fmt.Errorf("commandprovider: stat command: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("commandprovider: command is a directory: %q", cfg.Command)
	}
	if cfg.WorkDir != "" {
		if !filepath.IsAbs(cfg.WorkDir) {
			return fmt.Errorf("commandprovider: work directory must be absolute: %q", cfg.WorkDir)
		}
		info, err := os.Stat(cfg.WorkDir)
		if err != nil {
			return fmt.Errorf("commandprovider: stat work directory: %w", err)
		}
		if !info.IsDir() {
			return fmt.Errorf("commandprovider: work directory is not a directory: %q", cfg.WorkDir)
		}
	}
	if len(cfg.SupportedOperations) == 0 {
		return errors.New("commandprovider: at least one supported operation is required")
	}
	for _, op := range cfg.SupportedOperations {
		if !validOperation(op) {
			return fmt.Errorf("commandprovider: unsupported configured operation %q", op)
		}
	}
	if cfg.MaxStdoutBytes <= 0 {
		return errors.New("commandprovider: max stdout bytes must be positive")
	}
	if cfg.MaxStderrBytes <= 0 {
		return errors.New("commandprovider: max stderr bytes must be positive")
	}
	for _, name := range cfg.EnvironmentAllowlist {
		if name == "" || strings.ContainsRune(name, '=') {
			return fmt.Errorf("commandprovider: invalid environment variable name %q", name)
		}
	}
	return nil
}

func validOperation(op providerport.Operation) bool {
	switch op {
	case providerport.OperationInterpretation,
		providerport.OperationPlanning,
		providerport.OperationGeneration,
		providerport.OperationEvaluationObservation:
		return true
	default:
		return false
	}
}

func supports(operations []providerport.Operation, wanted providerport.Operation) bool {
	for _, operation := range operations {
		if operation == wanted {
			return true
		}
	}
	return false
}

func allowedEnvironment(names []string) []string {
	environment := make([]string, 0, len(names))
	for _, name := range names {
		if value, ok := os.LookupEnv(name); ok {
			environment = append(environment, name+"="+value)
		}
	}
	sort.Strings(environment)
	return environment
}

func uniqueSortedStrings(values []string) []string {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			set[value] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for value := range set {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func uniqueSortedOperations(values []providerport.Operation) []providerport.Operation {
	set := make(map[providerport.Operation]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	out := make([]providerport.Operation, 0, len(set))
	for value := range set {
		out = append(out, value)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
