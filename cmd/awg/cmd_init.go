// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/globulario/sensei/golang/statedir"
)

// initOptions selects which agent surfaces `sensei init` wires up.
type initOptions struct {
	hooks    bool // .claude/hooks/*
	claudeMD bool // CLAUDE.md
	agentsMD bool // AGENTS.md (cross-tool convention)
	cursor   bool // .cursor/rules/sensei.mdc
	mcp      bool // .mcp.json (opt-in; write/merge the Sensei server)
}

//go:embed templates/*
var templates embed.FS

func runInit(args []string) int {
	fs := flag.NewFlagSet("sensei init", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	dir := fs.String("dir", ".", "project root directory")
	withHooks := fs.Bool("hooks", true, "generate Claude Code hook scripts")
	withClaudeMD := fs.Bool("claude-md", true, "append Sensei snippet to CLAUDE.md")
	withAgentsMD := fs.Bool("agents-md", true, "append Sensei snippet to AGENTS.md (Codex/Cursor/others)")
	withCursor := fs.Bool("cursor", true, "write a Cursor rule (.cursor/rules/sensei.mdc)")
	withMCP := fs.Bool("mcp", false, "write/merge the Sensei MCP server into .mcp.json (opt-in)")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: sensei init [flags]

Scaffolds awareness for a new project. Creates:
  docs/awareness/invariants.yaml         Your architectural rules
  docs/awareness/failure_modes.yaml      Known/potential incidents
  docs/awareness/incident_patterns.yaml  Edit shapes that introduce bugs
  docs/awareness/high_risk_files.yaml    Files requiring briefing
  docs/awareness/activation_rules.yaml   When briefing is required
  .sensei/config.yaml                    Sensei configuration

And wires up your agent tools (each idempotent; toggle with the flags below):
  CLAUDE.md, AGENTS.md                   Sensei instructions for the agent
  .cursor/rules/sensei.mdc               Cursor rule
  .claude/hooks/*                        Claude Code PreToolUse push/guard hooks
  .mcp.json                              Sensei MCP server (with --mcp)

Flags:
`)
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return 2
	}

	root, err := filepath.Abs(*dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei init: %v\n", err)
		return 1
	}

	created, err := scaffoldProject(root, initOptions{
		hooks:    *withHooks,
		claudeMD: *withClaudeMD,
		agentsMD: *withAgentsMD,
		cursor:   *withCursor,
		mcp:      *withMCP,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei init: %v\n", err)
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
     (the 8-category meta-principle pack is already installed:
      docs/awareness/meta_principles.yaml — link your rules to it
      via related_invariants)
  3. Start the server:  sensei serve -no-seed &
     (-no-seed: your project builds its own graph — without it the
      server seeds the embedded Globular reference graph)
  4. Compile your graph: sensei build
  5. First briefing:     sensei briefing -file <your-critical-file>
`)
	return 0
}

func scaffoldProject(root string, opts initOptions) ([]string, error) {
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

	// Create the state directory (.sensei, or a pre-existing legacy .awg).
	stateDir := statedir.Path(root)
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return nil, fmt.Errorf("create %s: %w", statedir.Name(root), err)
	}
	cfgPath := filepath.Join(stateDir, "config.yaml")
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
	if opts.hooks {
		hookFiles, err := scaffoldHooks(root)
		if err != nil {
			return nil, fmt.Errorf("hooks: %w", err)
		}
		created = append(created, hookFiles...)
	}

	// Wire the agent-instruction surfaces. Each is idempotent (a marker check or
	// a don't-overwrite guard), so re-running init never duplicates.
	if opts.claudeMD {
		if f, err := appendSnippet(filepath.Join(root, "CLAUDE.md"), "## Sensei", "templates/claude-md-snippet.md"); err != nil {
			return nil, fmt.Errorf("CLAUDE.md: %w", err)
		} else if f != "" {
			created = append(created, f)
		}
	}
	if opts.agentsMD {
		if f, err := appendSnippet(filepath.Join(root, "AGENTS.md"), "## Sensei", "templates/agent-snippet.md"); err != nil {
			return nil, fmt.Errorf("AGENTS.md: %w", err)
		} else if f != "" {
			created = append(created, f)
		}
	}
	if opts.cursor {
		if f, err := writeCursorRule(root); err != nil {
			return nil, fmt.Errorf("cursor rule: %w", err)
		} else if f != "" {
			created = append(created, f)
		}
	}
	if opts.mcp {
		if f, err := writeMCPConfig(root); err != nil {
			return nil, fmt.Errorf(".mcp.json: %w", err)
		} else if f != "" {
			created = append(created, f)
		}
	}

	return created, nil
}

// writeCursorRule installs a Cursor rule at .cursor/rules/sensei.mdc. Skips an
// existing file (never overwrites a user's edits).
func writeCursorRule(root string) (string, error) {
	dir := filepath.Join(root, ".cursor", "rules")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	dst := filepath.Join(dir, "sensei.mdc")
	if _, err := os.Stat(dst); err == nil {
		return "", nil
	}
	content, err := templates.ReadFile("templates/cursor-rule.mdc")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(dst, content, 0o644); err != nil {
		return "", err
	}
	return dst, nil
}

// writeMCPConfig writes or merges the Sensei MCP server into .mcp.json. It never
// clobbers other servers or an existing "sensei" entry, and refuses to touch a
// file that isn't valid JSON.
func writeMCPConfig(root string) (string, error) {
	path := filepath.Join(root, ".mcp.json")
	cfg := map[string]any{}
	if data, err := os.ReadFile(path); err == nil && len(strings.TrimSpace(string(data))) > 0 {
		if err := json.Unmarshal(data, &cfg); err != nil {
			return "", fmt.Errorf("existing .mcp.json is not valid JSON (left untouched): %w", err)
		}
	}
	servers, _ := cfg["mcpServers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}
	if _, exists := servers["sensei"]; exists {
		return "", nil // never clobber an existing sensei entry
	}
	servers["sensei"] = map[string]any{
		"command": resolveMCPBinary(),
		"args":    []any{"--awareness-addr", "localhost:10120"},
	}
	cfg["mcpServers"] = servers
	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, append(out, '\n'), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// resolveMCPBinary finds the awareness-mcp bridge: next to this executable
// first, then PATH, else the bare name (resolved at launch time).
func resolveMCPBinary() string {
	if exe, err := os.Executable(); err == nil {
		cand := filepath.Join(filepath.Dir(exe), exeName("awareness-mcp"))
		if _, err := os.Stat(cand); err == nil {
			return cand
		}
	}
	if p, err := exec.LookPath("awareness-mcp"); err == nil {
		return p
	}
	return "awareness-mcp"
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

// appendSnippet appends a template to a markdown instructions file (CLAUDE.md,
// AGENTS.md), unless the file already contains `marker` (idempotent). Returns
// the path when it wrote, "" when it skipped.
func appendSnippet(path, marker, templateName string) (string, error) {
	if data, err := os.ReadFile(path); err == nil {
		if strings.Contains(string(data), marker) {
			return "", nil // already has the Sensei section
		}
	}

	snippet, err := templates.ReadFile(templateName)
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
