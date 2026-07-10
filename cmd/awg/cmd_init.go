// SPDX-License-Identifier: Apache-2.0

package main

import (
	"embed"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

//go:embed templates/*
var templates embed.FS

func runInit(args []string) int {
	fs := flag.NewFlagSet("awg init", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	dir := fs.String("dir", ".", "project root directory")
	withHooks := fs.Bool("hooks", true, "generate Claude Code hook scripts")
	withClaudeMD := fs.Bool("claude-md", true, "append AWG snippet to CLAUDE.md")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: awg init [flags]

Scaffolds awareness for a new project. Creates:
  docs/awareness/invariants.yaml         Your architectural rules
  docs/awareness/failure_modes.yaml      Known/potential incidents
  docs/awareness/incident_patterns.yaml  Edit shapes that introduce bugs
  docs/awareness/high_risk_files.yaml    Files requiring briefing
  docs/awareness/activation_rules.yaml   When briefing is required
  .awg/config.yaml                       AWG configuration

Flags:
`)
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return 2
	}

	root, err := filepath.Abs(*dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "awg init: %v\n", err)
		return 1
	}

	created, err := scaffoldProject(root, *withHooks, *withClaudeMD)
	if err != nil {
		fmt.Fprintf(os.Stderr, "awg init: %v\n", err)
		return 1
	}

	fmt.Fprintf(os.Stdout, "AWG initialized. Created:\n")
	for _, f := range created {
		rel, _ := filepath.Rel(root, f)
		if rel == "" {
			rel = f
		}
		fmt.Fprintf(os.Stdout, "  %s\n", rel)
	}

	fmt.Fprintf(os.Stdout, `
Next steps:
  1. Edit docs/awareness/high_risk_files.yaml — add your critical paths
  2. Edit docs/awareness/invariants.yaml — add your first rule
     (the 7-category meta-principle pack is already installed:
      docs/awareness/meta_principles.yaml — link your rules to it
      via related_invariants)
  3. Start the server:  awg serve -no-seed &
     (-no-seed: your project builds its own graph — without it the
      server seeds the embedded Globular reference graph)
  4. Compile your graph: awg build
  5. First briefing:     awg briefing -file <your-critical-file>
`)
	return 0
}

func scaffoldProject(root string, withHooks, withClaudeMD bool) ([]string, error) {
	var created []string

	// Create docs/awareness/ files from templates.
	awarenessFiles := []string{
		"invariants.yaml",
		"failure_modes.yaml",
		"incident_patterns.yaml",
		"high_risk_files.yaml",
		"activation_rules.yaml",
		"meta_principles.yaml",
	}
	awarenessDir := filepath.Join(root, "docs", "awareness")
	if err := os.MkdirAll(awarenessDir, 0o755); err != nil {
		return nil, fmt.Errorf("create docs/awareness: %w", err)
	}
	for _, name := range awarenessFiles {
		dst := filepath.Join(awarenessDir, name)
		if _, err := os.Stat(dst); err == nil {
			continue // don't overwrite existing files
		}
		content, err := templates.ReadFile("templates/awareness/" + name)
		if err != nil {
			return nil, fmt.Errorf("read template %s: %w", name, err)
		}
		if err := os.WriteFile(dst, content, 0o644); err != nil {
			return nil, fmt.Errorf("write %s: %w", dst, err)
		}
		created = append(created, dst)
	}

	// Create .awg/config.yaml.
	awgDir := filepath.Join(root, ".awg")
	if err := os.MkdirAll(awgDir, 0o755); err != nil {
		return nil, fmt.Errorf("create .awg: %w", err)
	}
	cfgPath := filepath.Join(awgDir, "config.yaml")
	if _, err := os.Stat(cfgPath); err != nil {
		content, err := templates.ReadFile("templates/config.yaml")
		if err != nil {
			return nil, fmt.Errorf("read config template: %w", err)
		}
		if err := os.WriteFile(cfgPath, content, 0o644); err != nil {
			return nil, fmt.Errorf("write config: %w", err)
		}
		created = append(created, cfgPath)
	}

	// Create Claude Code hooks.
	if withHooks {
		hookFiles, err := scaffoldHooks(root)
		if err != nil {
			return nil, fmt.Errorf("hooks: %w", err)
		}
		created = append(created, hookFiles...)
	}

	// Append to CLAUDE.md.
	if withClaudeMD {
		claudePath := filepath.Join(root, "CLAUDE.md")
		if f, err := appendClaudeMD(claudePath); err != nil {
			return nil, fmt.Errorf("CLAUDE.md: %w", err)
		} else if f != "" {
			created = append(created, f)
		}
	}

	return created, nil
}

func scaffoldHooks(root string) ([]string, error) {
	var created []string
	hookDir := filepath.Join(root, ".claude", "hooks")
	if err := os.MkdirAll(hookDir, 0o755); err != nil {
		return nil, err
	}

	entries, err := fs.ReadDir(templates, "templates/hooks")
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		dst := filepath.Join(hookDir, entry.Name())
		if _, err := os.Stat(dst); err == nil {
			continue // don't overwrite
		}
		content, err := templates.ReadFile("templates/hooks/" + entry.Name())
		if err != nil {
			return nil, err
		}
		if err := os.WriteFile(dst, content, 0o755); err != nil {
			return nil, err
		}
		created = append(created, dst)
	}
	return created, nil
}

func appendClaudeMD(path string) (string, error) {
	marker := "## Awareness Graph (AWG)"

	// Check if already present.
	if data, err := os.ReadFile(path); err == nil {
		if strings.Contains(string(data), marker) {
			return "", nil // already has AWG section
		}
	}

	snippet, err := templates.ReadFile("templates/claude-md-snippet.md")
	if err != nil {
		return "", err
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return "", err
	}
	defer f.Close()

	if _, err := io.WriteString(f, "\n\n"); err != nil {
		return "", err
	}
	if _, err := f.Write(snippet); err != nil {
		return "", err
	}
	return path, nil
}
