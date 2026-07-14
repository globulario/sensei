// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"strings"
	"testing"
)

func TestArchitectSkillLabelsDialogueNonAuthoritative(t *testing.T) {
	data, err := os.ReadFile("templates/skills/sensei-architect/references/TOOL-PLAYBOOK.md")
	if err != nil {
		t.Fatalf("read tool playbook: %v", err)
	}
	text := string(data)
	for _, want := range []string{
		"`open_question` as explicit-query-only uncertainty",
		"`architect_answer` as an exact typed human statement",
		"`accepted_for_question` only resolves the question artifact",
		"Evidence pointers are unverified literals",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("TOOL-PLAYBOOK.md missing %q", want)
		}
	}
}
