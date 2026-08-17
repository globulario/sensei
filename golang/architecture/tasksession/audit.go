// SPDX-License-Identifier: AGPL-3.0-only

package tasksession

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Task-directory audit dispositions. Closed vocabulary.
const (
	// AuditValid means session.yaml loaded and describes a task.
	AuditValid = "valid"
	// AuditInvalidOrUnreadable means the directory is named like a task but its
	// session cannot be loaded. It is ONE disposition on purpose: "malformed"
	// and "unreadable" are both "we cannot describe this task", and inventing a
	// finer verdict would mean guessing which.
	AuditInvalidOrUnreadable = "invalid_or_unreadable"
)

// TaskAuditEntry is one task directory's disposition.
type TaskAuditEntry struct {
	Dir         string `json:"dir" yaml:"dir"`
	Disposition string `json:"disposition" yaml:"disposition"`
	TaskID      string `json:"task_id,omitempty" yaml:"task_id,omitempty"`
	Status      string `json:"status,omitempty" yaml:"status,omitempty"`
	Detail      string `json:"detail,omitempty" yaml:"detail,omitempty"`
	Active      bool   `json:"active,omitempty" yaml:"active,omitempty"`
}

// TaskAuditReport is the whole read-only inventory.
type TaskAuditReport struct {
	TasksDir     string           `json:"tasks_dir" yaml:"tasks_dir"`
	Present      bool             `json:"present" yaml:"present"`
	Entries      []TaskAuditEntry `json:"entries" yaml:"entries"`
	ValidCount   int              `json:"valid_count" yaml:"valid_count"`
	InvalidCount int              `json:"invalid_count" yaml:"invalid_count"`
	ActiveDir    string           `json:"active_dir,omitempty" yaml:"active_dir,omitempty"`
	ActiveDetail string           `json:"active_detail,omitempty" yaml:"active_detail,omitempty"`
}

// AuditTasks inventories every task directory and classifies it.
//
// It is STRICTLY READ-ONLY, and that is the contract, not an implementation
// detail. A malformed task directory is preserved and reported; it is never
// deleted, cleared, superseded, or reconstructed. Reconstructing a session.yaml
// would manufacture a session that never existed — a plausible-looking record of
// work nobody did — which is worse than the malformed directory it replaced.
// Deciding what to do about a malformed task is an operator's call, and this
// command exists to inform it, not to pre-empt it.
//
// An absent tasks directory is reported as Present=false with no entries, not as
// an error: a repository that has never created a task session has nothing wrong
// with it.
func AuditTasks(repoRoot string) (TaskAuditReport, error) {
	root := strings.TrimSpace(repoRoot)
	if root == "" {
		root = "."
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return TaskAuditReport{}, err
	}
	tasksDir := filepath.Join(abs, ".sensei", "tasks")
	report := TaskAuditReport{TasksDir: tasksDir}

	entries, err := os.ReadDir(tasksDir)
	if err != nil {
		if os.IsNotExist(err) {
			return report, nil // absent, not broken
		}
		return TaskAuditReport{}, err
	}
	report.Present = true

	// The active pointer is resolved once, so an entry can be marked active
	// without re-reading it per directory.
	activeDir := ""
	if ptr, perr := LoadActivePointer(abs); perr == nil {
		activeDir = filepath.Dir(filepath.Join(abs, filepath.FromSlash(ptr.SessionPath)))
		report.ActiveDir = activeDir
	} else if !os.IsNotExist(perr) {
		// A present-but-unusable pointer is reported, never silently ignored:
		// "no active task" and "the active pointer is broken" are different
		// facts and only one of them needs an operator.
		report.ActiveDetail = "active pointer unusable: " + perr.Error()
	}

	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "task.") {
			continue
		}
		dir := filepath.Join(tasksDir, e.Name())
		entry := TaskAuditEntry{Dir: dir, Active: activeDir != "" && dir == activeDir}

		session, serr := LoadSession(filepath.Join(dir, "session.yaml"))
		if serr != nil {
			entry.Disposition = AuditInvalidOrUnreadable
			entry.Detail = serr.Error()
			report.InvalidCount++
		} else {
			entry.Disposition = AuditValid
			entry.TaskID = session.TaskID
			entry.Status = session.OperationalStatus
			report.ValidCount++
		}
		report.Entries = append(report.Entries, entry)
	}
	sort.Slice(report.Entries, func(i, j int) bool { return report.Entries[i].Dir < report.Entries[j].Dir })
	return report, nil
}
