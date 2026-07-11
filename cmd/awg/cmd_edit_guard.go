// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/globulario/sensei/golang/evidence"
	awarenesspb "github.com/globulario/sensei/golang/pb"
)

// runEditGuard is the Claude Code PreToolUse guard for Edit/Write/MultiEdit.
// It reads the hook payload on stdin, runs the proposed edit content through
// edit_check, and emits a `{"decision":"block","reason":...}` document when the
// edit would introduce a forbidden-fix shape or trip a high-severity rule.
//
// It is the compliance half of enforcement: enforce-briefing.sh checks that a
// briefing was *requested*; this checks that what is about to be *written* does
// not violate a rule. All the decision logic lives here (tested) rather than in
// the shell hook, which stays a thin `exec sensei edit-guard` wrapper.
//
// AWG's edit_check RPC is advisory and never blocks — that contract is intact.
// This guard is an opt-in local enforcement layer over it. It fails OPEN: if
// the payload can't be parsed, the file is outside the project, or the server
// is unreachable, it allows the edit. It only ever blocks on an actual rule
// match, and always exits 0 (Claude Code reads the decision from stdout JSON).
func runEditGuard(args []string) int {
	fs := flag.NewFlagSet("sensei edit-guard", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	addr := fs.String("addr", defaultServiceAddr(), "AWG gRPC server address")
	domain := fs.String("domain", os.Getenv("AWG_DOMAIN"), "domain/repo scope (required on a multi-domain graph)")
	root := fs.String("root", "", "project root (default: walk up for docs/awareness or .sensei/config.yaml)")
	blockSeverity := fs.String("block-severity", envOr("AWG_EDIT_CHECK_BLOCK_SEVERITY", "critical,high"),
		"comma-separated severities that block the edit")
	advisory := fs.Bool("advisory", os.Getenv("AWG_EDIT_CHECK_ADVISORY") == "1",
		"warn-only: surface advisories on stderr, never block")
	fileFlag := fs.String("file", "", "neutral input: the edited file path (any agent/CI). When set, the Claude Code hook payload is not read.")
	contentFile := fs.String("content-file", "", "neutral input: path to a file holding the proposed content (with --file); '-' or omitted reads content from stdin")
	format := fs.String("format", envOr("AWG_EDIT_GUARD_FORMAT", "claude"),
		"output adapter: claude (PreToolUse decision JSON) | json (neutral verdict) | exit-code (block => exit 2)")
	eventLog := fs.String("event-log", os.Getenv("AWG_EVENT_LOG"), "append a JSONL outcome event to this ledger for evidence; see `sensei evidence`. Default: $AWG_EVENT_LOG (off when empty).")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: sensei edit-guard [flags]

Runs a proposed edit's content through edit_check and blocks when it would
introduce a forbidden-fix shape or a high-severity rule violation. The decision
core is agent-neutral; --format selects the output adapter and the Claude Code
hook is just one of them.

Input (pick one):
  (default)          read a Claude Code PreToolUse payload on stdin
  --file F           neutral: check file F with content from --content-file or stdin

Output adapters (--format):
  claude (default)   {"decision":"block","reason":...} on stdout, always exit 0
  json               neutral {"file","decision","reason","warnings":[...]}, exit 0
  exit-code          human reason on stderr; exit 2 on block, 0 otherwise (CI/agents)

Fails OPEN in every mode: an unparseable payload, an out-of-project file, or an
unreachable server allows the edit (exit 0). It only blocks on a real rule match.
For a fail-CLOSED CI gate over a diff, use 'sensei gate --enforce' instead.

Flags:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 0 // never wedge editing on a flag parse error
	}
	if *format != "claude" && *format != "json" && *format != "exit-code" {
		fmt.Fprintf(os.Stderr, "sensei edit-guard: --format must be claude|json|exit-code, got %q\n", *format)
		return 0
	}

	payload, err := io.ReadAll(os.Stdin)
	if err != nil {
		payload = nil
	}
	file, content, ok := resolveGuardInput(*fileFlag, *contentFile, payload)
	if !ok || strings.TrimSpace(content) == "" {
		return 0 // nothing to check (no file, or a pure deletion)
	}

	projectRoot, err := resolveProjectRoot(*root)
	if err != nil {
		return 0
	}
	rel, ok := relWithinRoot(projectRoot, file)
	if !ok {
		return 0 // edit outside the AWG project — not our concern
	}

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	resp, err := editGuardCheckRPC(ctx, *addr, rel, content, *domain)
	if err != nil {
		// Advisory backend being unreachable is NOT a rule match. Never block on
		// it; note it so it is not silently swallowed.
		fmt.Fprintf(os.Stderr, "sensei edit-guard: edit-check unavailable (allowing edit): %s\n", firstLine(err.Error()))
		return 0
	}

	opts := guardOptions{advisory: *advisory, blockSeverity: parseBlockSeverity(*blockSeverity)}
	decision := decideGuard(rel, resp.GetWarnings(), opts)
	emitGuardEvent(*eventLog, rel, *domain, decision, resp.GetWarnings(), opts)
	return emitGuardVerdict(*format, rel, decision, resp.GetWarnings())
}

// emitGuardEvent records one pre-edit guard outcome to the evidence ledger
// (best-effort; never affects the guard). No-op when logPath is empty. A block
// counts as a caught drift incident, tallied by the rules that triggered it.
func emitGuardEvent(logPath, rel, domain string, d guardDecision, warnings []*awarenesspb.EditWarning, opts guardOptions) {
	if strings.TrimSpace(logPath) == "" {
		return
	}
	var blocked, warned []string
	for _, w := range warnings {
		if warningBlocks(w, opts) {
			blocked = append(blocked, w.GetRuleId())
		} else {
			warned = append(warned, w.GetRuleId())
		}
	}
	decision := evidence.DecisionAllow
	switch {
	case d.block:
		decision = evidence.DecisionBlock
	case d.advisoryText != "":
		decision = evidence.DecisionWarn
	}
	_ = evidence.Append(logPath, evidence.Event{
		TS:           time.Now().UTC().Format(time.RFC3339),
		Tool:         "edit-guard",
		Repo:         domain,
		Decision:     decision,
		Enforced:     d.block,
		BlockedRules: blocked,
		WarnedRules:  warned,
		Files:        []string{rel},
	})
}

// resolveGuardInput picks the edit target from the neutral flags first, falling
// back to the Claude Code PreToolUse payload. This is the seam that makes the CC
// hook one adapter: --file (with content from --content-file or stdin) lets any
// agent/CI drive the same guard without speaking Claude Code's hook JSON.
func resolveGuardInput(fileFlag, contentFile string, stdin []byte) (file, content string, ok bool) {
	if strings.TrimSpace(fileFlag) != "" {
		switch {
		case strings.TrimSpace(contentFile) != "" && contentFile != "-":
			data, err := os.ReadFile(contentFile)
			if err != nil {
				return "", "", false
			}
			content = string(data)
		default:
			content = string(stdin) // content on stdin
		}
		return fileFlag, content, true
	}
	return extractGuardTarget(stdin) // Claude Code adapter
}

// emitGuardVerdict renders one guard decision in the requested adapter format
// and returns the process exit code. claude/json always exit 0 (fail-open, the
// decision is in stdout); exit-code returns 2 on block so a generic agent or CI
// step can act on the exit status alone.
func emitGuardVerdict(format, rel string, d guardDecision, warnings []*awarenesspb.EditWarning) int {
	switch format {
	case "json":
		out := map[string]interface{}{
			"file":     rel,
			"decision": "allow",
			"warnings": warningsToJSON(warnings),
		}
		if d.block {
			out["decision"] = "block"
			out["reason"] = d.reason
		}
		if b, err := json.Marshal(out); err == nil {
			fmt.Println(string(b))
		}
		return 0
	case "exit-code":
		if d.block {
			fmt.Fprintln(os.Stderr, d.reason)
			return 2
		}
		if d.advisoryText != "" {
			fmt.Fprintln(os.Stderr, d.advisoryText)
		}
		return 0
	default: // "claude" — the PreToolUse adapter
		if d.block {
			// Current Claude Code hook shape: hookSpecificOutput.permissionDecision
			// = "deny" with a reason. (The legacy top-level {"decision":"block"}
			// is deprecated.)
			out := map[string]any{
				"hookSpecificOutput": map[string]any{
					"hookEventName":            "PreToolUse",
					"permissionDecision":       "deny",
					"permissionDecisionReason": d.reason,
				},
			}
			if b, err := json.Marshal(out); err == nil {
				fmt.Println(string(b))
			}
			return 0
		}
		if d.advisoryText != "" {
			fmt.Fprintln(os.Stderr, d.advisoryText)
		}
		return 0
	}
}

// warningsToJSON serializes edit_check warnings for the neutral verdict.
func warningsToJSON(warnings []*awarenesspb.EditWarning) []map[string]string {
	out := make([]map[string]string, 0, len(warnings))
	for _, w := range warnings {
		wo := map[string]string{
			"rule_id":     w.GetRuleId(),
			"class":       w.GetClass(),
			"severity":    w.GetSeverity(),
			"message":     w.GetMessage(),
			"enforcement": w.GetEnforcement(),
		}
		if d := w.GetDetail(); d != "" {
			wo["detail"] = d
		}
		if p := w.GetProvenance(); p != "" {
			wo["provenance"] = p
		}
		out = append(out, wo)
	}
	return out
}

// editGuardCheckRPC runs edit_check for the guard; overridable in tests.
var editGuardCheckRPC = func(ctx context.Context, addr, file, content, domain string) (*awarenesspb.EditCheckResponse, error) {
	c, err := connectAWG(addr)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	return c.EditCheck(ctx, file, content, domain)
}

// extractGuardTarget pulls the edited file and the content the edit INTRODUCES
// out of a Claude Code PreToolUse payload. Write -> content; Edit -> new_string;
// MultiEdit -> every edits[].new_string joined. A pure deletion yields "".
func extractGuardTarget(payload []byte) (file, content string, ok bool) {
	var p struct {
		ToolInput struct {
			FilePath  string  `json:"file_path"`
			File      string  `json:"file"`
			Content   *string `json:"content"`
			NewString *string `json:"new_string"`
			Edits     []struct {
				NewString *string `json:"new_string"`
			} `json:"edits"`
		} `json:"tool_input"`
	}
	if err := json.Unmarshal(payload, &p); err != nil {
		return "", "", false
	}
	file = p.ToolInput.FilePath
	if file == "" {
		file = p.ToolInput.File
	}
	if file == "" {
		return "", "", false
	}
	var parts []string
	if p.ToolInput.Content != nil {
		parts = append(parts, *p.ToolInput.Content)
	}
	if p.ToolInput.NewString != nil {
		parts = append(parts, *p.ToolInput.NewString)
	}
	for _, e := range p.ToolInput.Edits {
		if e.NewString != nil {
			parts = append(parts, *e.NewString)
		}
	}
	return file, strings.Join(parts, "\n"), true
}

type guardOptions struct {
	advisory      bool
	blockSeverity map[string]bool
}

type guardDecision struct {
	block        bool
	reason       string // set when block is true
	advisoryText string // set when there are non-blocking warnings to surface
}

func parseBlockSeverity(s string) map[string]bool {
	out := map[string]bool{}
	for _, p := range strings.Split(s, ",") {
		if p = strings.ToLower(strings.TrimSpace(p)); p != "" {
			out[p] = true
		}
	}
	return out
}

// warningBlocks reports whether a single warning should stop the edit.
//
// Enforcement is the authored "this should block" signal — a detect block with
// `enforcement: block`. It is the primary trigger, because edit_check stamps
// every warning's severity as "warning", so severity alone cannot distinguish a
// block-worthy rule from an advisory one. A forbidden-fix class always blocks;
// the configurable severity set is a fallback for callers that add real
// severities out of band.
func warningBlocks(w *awarenesspb.EditWarning, opts guardOptions) bool {
	if strings.EqualFold(strings.TrimSpace(w.GetEnforcement()), "block") {
		return true
	}
	if strings.Contains(strings.ToLower(w.GetClass()), "forbidden") {
		return true
	}
	return opts.blockSeverity[strings.ToLower(w.GetSeverity())]
}

// decideGuard turns the edit_check warnings into an allow/block decision.
func decideGuard(rel string, warnings []*awarenesspb.EditWarning, opts guardOptions) guardDecision {
	if len(warnings) == 0 {
		return guardDecision{}
	}
	var blocking []*awarenesspb.EditWarning
	if !opts.advisory {
		for _, w := range warnings {
			if warningBlocks(w, opts) {
				blocking = append(blocking, w)
			}
		}
	}
	if len(blocking) == 0 {
		// Advisory mode, or only low-severity warnings: surface, but allow.
		return guardDecision{advisoryText: fmt.Sprintf("AWG edit-check advisory for %s:\n%s", rel, formatWarnings(warnings))}
	}
	reason := fmt.Sprintf(
		"AWG edit-check: this edit to %s introduces a shape an active rule forbids.\n\n%s\n\n"+
			"Revise the edit to respect the rule. If the change is deliberate and correct, say so "+
			"explicitly and re-apply; or set AWG_EDIT_CHECK_ADVISORY=1 to downgrade this guard to warn-only.",
		rel, formatWarnings(blocking))
	return guardDecision{block: true, reason: reason}
}

func formatWarnings(warnings []*awarenesspb.EditWarning) string {
	var b strings.Builder
	for i, w := range warnings {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "[%s] %s (%s): %s", w.GetSeverity(), w.GetRuleId(), w.GetClass(), w.GetMessage())
		if d := strings.TrimSpace(w.GetDetail()); d != "" {
			fmt.Fprintf(&b, "\n    %s", d)
		}
		if p := strings.TrimSpace(w.GetProvenance()); p != "" {
			fmt.Fprintf(&b, "\n    provenance: %s", p)
		}
	}
	return b.String()
}

// relWithinRoot resolves file to a path relative to root, reporting false if it
// escapes the project (so the guard never reaches outside its own repo).
func relWithinRoot(root, file string) (string, bool) {
	abs := file
	if !filepath.IsAbs(abs) {
		if wd, err := os.Getwd(); err == nil {
			abs = filepath.Join(wd, file)
		}
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return rel, true
}

func envOr(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
