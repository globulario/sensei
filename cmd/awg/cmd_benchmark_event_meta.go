// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
)

type benchmarkEventMeta struct {
	EventID             string `json:"event_id"`
	Task                string `json:"task,omitempty"`
	DecisionAction      string `json:"decision_action,omitempty"`
	PrimaryFailureMode  string `json:"primary_failure_mode,omitempty"`
	CertificationStatus string `json:"certification_status,omitempty"`
	LearningEvidence    string `json:"learning_evidence,omitempty"`
	RetryResultClass    string `json:"retry_result_classification,omitempty"`
}

func runBenchmarkEventMeta(args []string) int {
	fs := flag.NewFlagSet("sensei benchmark-event-meta", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	eventPath := fs.String("event", "", "learning event YAML/JSON file")
	format := fs.String("format", "text", "output format: text | json")
	asJSON := fs.Bool("json", false, "output as JSON (deprecated: same as --format json)")
	field := fs.String("field", "", "print only one field: event_id | task | decision_action | primary_failure_mode | certification_status | learning_evidence | retry_result_classification")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: sensei benchmark-event-meta [flags]

Read a benchmark learning-event file and emit small stable metadata for
orchestration layers. This keeps event introspection in sensei instead of shell
snippets or Python one-liners.

Flags:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *asJSON {
		*format = "json"
	}
	if strings.TrimSpace(*eventPath) == "" {
		fmt.Fprintln(os.Stderr, "sensei benchmark-event-meta: --event is required")
		return 2
	}
	doc, err := loadBenchmarkRetryDoc(*eventPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei benchmark-event-meta: load event: %v\n", err)
		return 1
	}
	event := benchmarkRetryUnwrapEvent(doc)
	meta := benchmarkEventMeta{
		EventID:             benchmarkString(event["id"]),
		Task:                benchmarkString(event["task"]),
		DecisionAction:      benchmarkString(benchmarkMap(event["decision"])["action"]),
		PrimaryFailureMode:  benchmarkString(benchmarkMap(event["diagnosis"])["primary_failure_mode"]),
		CertificationStatus: benchmarkString(event["certification_status"]),
		LearningEvidence:    benchmarkString(event["learning_evidence"]),
		RetryResultClass:    benchmarkString(event["retry_result_classification"]),
	}

	if strings.TrimSpace(*field) != "" {
		switch strings.TrimSpace(*field) {
		case "event_id":
			fmt.Println(meta.EventID)
		case "task":
			fmt.Println(meta.Task)
		case "decision_action":
			fmt.Println(meta.DecisionAction)
		case "primary_failure_mode":
			fmt.Println(meta.PrimaryFailureMode)
		case "certification_status":
			fmt.Println(meta.CertificationStatus)
		case "learning_evidence":
			fmt.Println(meta.LearningEvidence)
		case "retry_result_classification":
			fmt.Println(meta.RetryResultClass)
		default:
			fmt.Fprintf(os.Stderr, "sensei benchmark-event-meta: unknown --field %q\n", *field)
			return 2
		}
		return 0
	}

	switch *format {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(meta)
	default:
		fmt.Printf("Event id: %s\n", meta.EventID)
		if meta.Task != "" {
			fmt.Printf("Task: %s\n", meta.Task)
		}
		if meta.DecisionAction != "" {
			fmt.Printf("Decision action: %s\n", meta.DecisionAction)
		}
		if meta.PrimaryFailureMode != "" {
			fmt.Printf("Primary failure mode: %s\n", meta.PrimaryFailureMode)
		}
		if meta.CertificationStatus != "" {
			fmt.Printf("Certification status: %s\n", meta.CertificationStatus)
		}
		if meta.LearningEvidence != "" {
			fmt.Printf("Learning evidence: %s\n", meta.LearningEvidence)
		}
		if meta.RetryResultClass != "" {
			fmt.Printf("Retry result classification: %s\n", meta.RetryResultClass)
		}
	}
	return 0
}
