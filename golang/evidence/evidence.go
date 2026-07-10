// SPDX-License-Identifier: Apache-2.0

// Package evidence is the outcome ledger behind AWG's "control" claim: an
// append-only record of what the gate and pre-edit guard actually DID — every
// block, warn, and allow — so the claim "caught N drift incidents across M
// repos" is backed by data, not assertion.
//
// The ledger is plain JSONL (one Event per line). Append is best-effort and
// never fails a caller — instrumentation must not break the tool it measures.
// Aggregate is pure so the reported numbers are exhaustively testable.
package evidence

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Decision labels. A "block" is a caught drift incident: a change the gate
// stopped because it tripped an enforcement:block rule. would_block is the same
// finding in a non-enforcing (dry-run/report) mode. cannot_verify is a
// fail-closed non-answer (the gate could not evaluate the diff).
const (
	DecisionBlock        = "block"
	DecisionWouldBlock   = "would_block"
	DecisionWarn         = "warn"
	DecisionAllow        = "allow"
	DecisionCannotVerify = "cannot_verify"
)

// Event is one gate/guard invocation's outcome. TS is RFC3339 (the caller
// stamps it, so tests and replays are deterministic).
type Event struct {
	TS           string   `json:"ts"`
	Tool         string   `json:"tool"`           // "gate" | "edit-guard"
	Repo         string   `json:"repo,omitempty"` // domain/repo scope
	Decision     string   `json:"decision"`
	Enforced     bool     `json:"enforced"`
	BlockedRules []string `json:"blocked_rules,omitempty"`
	WarnedRules  []string `json:"warned_rules,omitempty"`
	Files        []string `json:"files,omitempty"`
	DiffRange    string   `json:"diff_range,omitempty"`
	Commit       string   `json:"commit,omitempty"`
}

// Append writes one event as a JSONL line to path, creating the parent
// directory if needed. Best-effort: any error is returned but callers are
// expected to ignore it — instrumentation must never wedge the gate.
func Append(path string, ev Event) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	line, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	_, err = f.Write(append(line, '\n'))
	return err
}

// Load reads all events from a JSONL ledger. A missing file is an empty ledger,
// not an error. Malformed lines are skipped (a partially-written last line must
// not lose the whole history).
func Load(path string) ([]Event, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	var out []Event
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var ev Event
		if json.Unmarshal([]byte(line), &ev) == nil {
			out = append(out, ev)
		}
	}
	return out, sc.Err()
}

// RepoStat is per-repo rollup.
type RepoStat struct {
	Repo   string `json:"repo"`
	Events int    `json:"events"`
	Blocks int    `json:"blocks"`
}

// Summary is the aggregated evidence — the headline being Blocks caught across
// len(Repos) distinct repos.
type Summary struct {
	Events        int            `json:"events"`
	Blocks        int            `json:"blocks"`      // block + would_block
	HardBlocks    int            `json:"hard_blocks"` // enforced blocks only
	Warns         int            `json:"warns"`
	Allows        int            `json:"allows"`
	CannotVerify  int            `json:"cannot_verify"`
	Repos         []string       `json:"repos"`
	CatchesByRule map[string]int `json:"catches_by_rule"`
	ByRepo        []RepoStat     `json:"by_repo"`
	FirstTS       string         `json:"first_ts,omitempty"`
	LastTS        string         `json:"last_ts,omitempty"`
}

// Aggregate rolls a ledger into a Summary. Pure — no I/O — so the reported
// numbers are unit-tested exactly. A block or would_block counts as a caught
// incident; each blocked rule is tallied in CatchesByRule.
func Aggregate(events []Event) Summary {
	s := Summary{CatchesByRule: map[string]int{}}
	repoSeen := map[string]*RepoStat{}
	var repoOrder []string
	for _, ev := range events {
		s.Events++
		isBlock := ev.Decision == DecisionBlock || ev.Decision == DecisionWouldBlock
		switch ev.Decision {
		case DecisionBlock, DecisionWouldBlock:
			s.Blocks++
			if ev.Decision == DecisionBlock && ev.Enforced {
				s.HardBlocks++
			}
		case DecisionWarn:
			s.Warns++
		case DecisionAllow:
			s.Allows++
		case DecisionCannotVerify:
			s.CannotVerify++
		}
		for _, r := range ev.BlockedRules {
			s.CatchesByRule[r]++
		}
		repo := ev.Repo
		if repo == "" {
			repo = "(unscoped)"
		}
		rs, ok := repoSeen[repo]
		if !ok {
			rs = &RepoStat{Repo: repo}
			repoSeen[repo] = rs
			repoOrder = append(repoOrder, repo)
		}
		rs.Events++
		if isBlock {
			rs.Blocks++
		}
		if ev.TS != "" {
			if s.FirstTS == "" || ev.TS < s.FirstTS {
				s.FirstTS = ev.TS
			}
			if ev.TS > s.LastTS {
				s.LastTS = ev.TS
			}
		}
	}
	sort.Strings(repoOrder)
	for _, r := range repoOrder {
		s.Repos = append(s.Repos, r)
		s.ByRepo = append(s.ByRepo, *repoSeen[r])
	}
	return s
}
