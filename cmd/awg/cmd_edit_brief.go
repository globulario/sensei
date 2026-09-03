// SPDX-License-Identifier: AGPL-3.0-only

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
	"github.com/globulario/sensei/golang/evidence"
	awarenesspb "github.com/globulario/sensei/golang/pb"
	"github.com/globulario/sensei/golang/statedir"
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
	addr := fs.String("addr", defaultServiceAddr(), "Sensei gRPC server address")
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

	// THE PROJECT'S CONFIG NAMES THE INSTANCE, NOT THIS COMMAND'S DEFAULT.
	//
	// The flag default is one repository's port. A hook that used it answered
	// for whichever project happened to own :10120 -- on this machine, a
	// DIFFERENT repository's domain -- and a briefing served from the wrong
	// graph is worse than none: it is confidently about something else.
	//
	// Precedence is the one endpoint_binding.go and repo_domain_binding.go
	// already establish, so this command does not invent a fourth: an explicit
	// flag is the operator naming the endpoint at the point of use and always
	// wins; otherwise the project's own configuration decides; only then the
	// built-in default.
	resolvedAddr := *addr
	if !flagPassed(fs, "addr") {
		if cfg, cfgErr := loadEndpointConfig(projectRoot); cfgErr == nil {
			if a := cfg.configuredServerAddr(); a != "" {
				resolvedAddr = a
			}
		}
	}
	resolvedDomain := strings.TrimSpace(*domain)
	if resolvedDomain == "" {
		resolvedDomain = strings.TrimSpace(resolveRepositoryDomain(projectRoot, "").Domain)
	}

	ledger := editBriefLedgerPath(projectRoot)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	out, err := editBriefRPC(ctx, resolvedAddr, rel, *depth, resolvedDomain)
	if err != nil {
		// A briefing the backend can't serve is not a reason to annotate the
		// edit. Never block on it. RECORD IT: an opportunity that produced no
		// delivery is the measurement's most important row, because it is the
		// one a delivery count would silently drop.
		reason := firstLine(err.Error())
		recordEditBrief(ledger, rel, resolvedDomain, editBriefOutcome{}, false, reason)
		fmt.Fprintf(os.Stderr, "sensei edit-brief: briefing unavailable (allowing edit): %s\n", reason)
		return 0
	}

	prose := strings.TrimSpace(out.Prose)
	switch {
	case !deliverableStatuses[out.Status]:
		recordEditBrief(ledger, rel, resolvedDomain, out, false,
			"status carries no governing knowledge for this file: "+out.Status.String())
		return 0
	case prose == "":
		recordEditBrief(ledger, rel, resolvedDomain, out, false, "briefing carried no prose")
		return 0
	}

	recordEditBrief(ledger, rel, resolvedDomain, out, true, "")
	emitEditBriefContext(rel, prose)
	return 0
}

// editBriefLedgerPath resolves where delivery evidence is appended.
//
// AWG_EVENT_LOG wins, so this shares one ledger with edit-guard when an
// operator points both at the same file. Otherwise it defaults to a
// project-scoped ledger rather than to OFF.
//
// That default differs deliberately from edit-guard's, which records nothing
// unless asked. The whole point of this path is to make "did governed law
// reach the agent before the change" countable, and a measurement that must be
// switched on is one nobody switched on -- which is exactly how the hook this
// instruments came to exist, fully written, and never registered.
func editBriefLedgerPath(root string) string {
	if p := strings.TrimSpace(os.Getenv("AWG_EVENT_LOG")); p != "" {
		return p
	}
	if strings.TrimSpace(root) == "" {
		return ""
	}
	return statedir.Path(root, "briefing-delivery.jsonl")
}

// recordEditBrief appends one delivery attempt. Best-effort by the ledger's own
// contract: instrumentation must never wedge the tool it measures, and this one
// must never wedge an edit.
func recordEditBrief(ledger, rel, domain string, out editBriefOutcome, delivered bool, reason string) {
	if strings.TrimSpace(ledger) == "" {
		return
	}
	status, wireStatus := "", ""
	if out.Status != awarenesspb.BriefingStatus_BRIEFING_STATUS_OK || delivered || len(out.Referenced) > 0 {
		status = out.Status.String()
	}
	if out.Wire != out.Status && out.Wire != awarenesspb.BriefingStatus_BRIEFING_STATUS_OK {
		wireStatus = out.Wire.String()
	}
	_ = evidence.Append(ledger, evidence.Event{
		TS:              time.Now().UTC().Format(time.RFC3339),
		Tool:            "edit-brief",
		Repo:            domain,
		Decision:        evidence.DecisionAllow, // a push never blocks
		Files:           []string{rel},
		Status:          status,
		WireStatus:      wireStatus,
		Surfaced:        out.Referenced,
		Delivered:       delivered,
		GraphGeneration: out.Generation,
		Reason:          reason,
	})
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

// editBriefOutcome is what one briefing attempt establishes.
//
// The STATUS is carried, not just the prose. It is the server's own epistemic
// classification and it decides both what is delivered and what is recorded --
// re-deriving it here, or reading it off the prose, would be a second
// classifier for a question the graph already answers.
type editBriefOutcome struct {
	Prose string
	// Status is the FILE's epistemic verdict and decides delivery.
	Status awarenesspb.BriefingStatus
	// Wire is the combined status the server sent, recorded as-is so the
	// evidence shows what the server said, not only what was acted on.
	Wire       awarenesspb.BriefingStatus
	Referenced []string
	Generation string
}

// editBriefRPC fetches a compact briefing for a file; overridable in tests.
var editBriefRPC = func(ctx context.Context, addr, file, depth, domain string) (editBriefOutcome, error) {
	conn, err := client.DialConn(addr)
	if err != nil {
		return editBriefOutcome{}, err
	}
	defer conn.Close()
	c := awarenesspb.NewAwarenessGraphClient(conn)
	resp, err := c.Briefing(ctx, &awarenesspb.BriefingRequest{File: file, Depth: depth, Domain: domain})
	if err != nil {
		return editBriefOutcome{}, err
	}
	return editBriefOutcome{
		Prose:      resp.GetProse(),
		Status:     preferFileStatus(resp.GetStatus(), resp.FileStatus),
		Wire:       resp.GetStatus(),
		Referenced: resp.GetReferencedIds(),
		Generation: resp.GetAuthority().GetGraphBuildCommit(),
	}, nil
}

// preferFileStatus picks the verdict that decides delivery.
//
// `status` folds the feedback subsystem's health into the same field, so a
// server whose feedback is unavailable reports DEGRADED for every file and the
// governed/ungoverned distinction disappears. file_status is that distinction
// preserved, and it is the one delivery must key on.
//
// AN OLDER SERVER SENDS NO FIELD 8, and it is ABSENT rather than zero: the
// field is `optional`, so nil means "this server does not report a file
// verdict" and is distinguishable from a reported OK. Without that distinction
// the zero value of BriefingStatus -- which is OK -- would read as "this file is
// governed" against every server built before the field existed.
//
// The presence check is what makes a governed file measurable while the
// feedback subsystem is degraded. A first version took the weaker claim
// whenever file said OK, which was safe and made the OK case unobservable:
// every genuinely governed file recorded DEGRADED, and "useful direct-anchor
// hits" could never be counted.
func preferFileStatus(wire awarenesspb.BriefingStatus, file *awarenesspb.BriefingStatus) awarenesspb.BriefingStatus {
	if file == nil {
		return wire
	}
	return *file
}

// deliverableStatuses is the CLOSED set of briefing statuses that carry
// knowledge worth interrupting an edit with, read by MEMBERSHIP.
//
// Reading it by exclusion -- "deliver unless EMPTY" -- fails open: a status
// added later, or the unspecified zero value from a server that did not set
// one, would be delivered as though it governed the file.
//
// CONTEXT_ONLY is excluded on the vocabulary's own terms: "Naming a file's
// symbols is not knowledge about it." Delivering it is the noise case, and a
// hook that becomes noise has failed operationally even when its recall is
// perfect -- an agent that learns to skim past Sensei is worse off than one
// that was never interrupted.
//
// INFERRED_ONLY IS delivered, because the prose says out loud that it is not
// coverage. Withholding it would lose the weaker signal entirely; promoting it
// to OK would manufacture coverage. It is delivered AS inferred.
var deliverableStatuses = map[awarenesspb.BriefingStatus]bool{
	awarenesspb.BriefingStatus_BRIEFING_STATUS_OK:            true,
	awarenesspb.BriefingStatus_BRIEFING_STATUS_INFERRED_ONLY: true,
	awarenesspb.BriefingStatus_BRIEFING_STATUS_DEGRADED:      true,
}
