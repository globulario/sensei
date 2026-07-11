// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/globulario/sensei/golang/client"
	awarenesspb "github.com/globulario/sensei/golang/pb"
)

// runEditBrief is the Claude Code PreToolUse *push* for Edit/Write/MultiEdit.
//
// Where `edit-guard` BLOCKS a bad write and `enforce-briefing` BLOCKS until a
// briefing was requested, this one PUSHES: it fetches a compact briefing for the
// file about to be edited and hands the invariants, forbidden fixes, and failure
// modes to the agent as `additionalContext` — so the agent receives the
// architectural constraints unprompted, in the same turn, without having to ask
// and without fighting a block. It is the "don't forget to consult Sensei" seam:
// the agent can't skip the briefing because the harness delivers it.
//
// It emits a PreToolUse "allow" decision carrying additionalContext and always
// exits 0. It fails OPEN and SILENT: an unparseable payload, a file outside the
// project, an unreachable server, or a file nothing anchors to yields no output —
// the edit proceeds under the normal permission flow, unannotated. Pushing noise
// on every edit is worse than pushing nothing, so "visible absence" stays quiet
// here (unlike the interactive `briefing` command, which says so explicitly).
func runEditBrief(args []string) int {
	fs := flag.NewFlagSet("sensei edit-brief", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	addr := fs.String("addr", defaultServiceAddr(), "AWG gRPC server address")
	domain := fs.String("domain", os.Getenv("AWG_DOMAIN"), "domain/repo scope (required on a multi-domain graph)")
	root := fs.String("root", "", "project root (default: walk up for docs/awareness or .sensei/config.yaml)")
	depth := fs.String("depth", envOr("AWG_EDIT_BRIEF_DEPTH", "agent_compact"),
		"briefing depth: agent_compact | compact | standard | deep")
	fileFlag := fs.String("file", "", "neutral input: the edited file path (any agent/CI). When set, the Claude Code hook payload is not read from stdin.")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: sensei edit-brief [flags]

Claude Code PreToolUse *push*: fetches a compact briefing for the file about to
be edited and returns it as additionalContext, so the agent sees the file's
invariants, forbidden fixes, and failure modes before it writes — without having
to call briefing itself, and without being blocked. Reads a PreToolUse payload on
stdin (or --file for any agent/CI).

Always allows the edit and exits 0. Fails open and silent: no project, no
anchors, or an unreachable server yields no output. For enforcement (blocking a
forbidden-fix write) use 'sensei edit-guard'; the two compose as separate hooks.

Flags:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 0 // never wedge editing on a flag parse error
	}

	// Resolve the edited file: the neutral --file flag first, else the Claude Code
	// PreToolUse payload on stdin.
	file := strings.TrimSpace(*fileFlag)
	if file == "" {
		payload, err := io.ReadAll(os.Stdin)
		if err != nil {
			return 0
		}
		f, _, ok := extractGuardTarget(payload)
		if !ok {
			return 0
		}
		file = f
	}
	if file == "" {
		return 0
	}

	projectRoot, err := resolveProjectRoot(*root)
	if err != nil {
		return 0
	}
	rel, ok := relWithinRoot(projectRoot, file)
	if !ok {
		return 0 // edit outside the Sensei project — not our concern
	}

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	prose, err := editBriefRPC(ctx, *addr, rel, *depth, *domain)
	if err != nil {
		// A briefing the backend can't serve is not a reason to annotate the edit.
		// Never block on it; note it so it is not silently swallowed.
		fmt.Fprintf(os.Stderr, "sensei edit-brief: briefing unavailable (allowing edit): %s\n", firstLine(err.Error()))
		return 0
	}
	if prose = strings.TrimSpace(prose); prose == "" {
		return 0 // nothing anchors to this file — stay silent in a hook
	}

	emitEditBriefContext(rel, prose)
	return 0
}

// emitEditBriefContext writes the PreToolUse allow+additionalContext decision in
// the current Claude Code hook shape (hookSpecificOutput.additionalContext).
func emitEditBriefContext(rel, prose string) {
	msg := fmt.Sprintf("Sensei — architectural context for %s. Consult these before editing; they are invariants the diff won't show:\n\n%s", rel, prose)
	out := map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName":      "PreToolUse",
			"permissionDecision": "allow",
			"additionalContext":  msg,
		},
	}
	if b, err := json.Marshal(out); err == nil {
		fmt.Println(string(b))
	}
}

// editBriefRPC fetches a compact briefing's prose for a file; overridable
// in tests.
var editBriefRPC = func(ctx context.Context, addr, file, depth, domain string) (string, error) {
	conn, err := client.DialConn(addr)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	c := awarenesspb.NewAwarenessGraphClient(conn)
	resp, err := c.Briefing(ctx, &awarenesspb.BriefingRequest{File: file, Depth: depth, Domain: domain})
	if err != nil {
		return "", err
	}
	return resp.GetProse(), nil
}
