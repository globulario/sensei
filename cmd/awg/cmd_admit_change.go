// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/globulario/sensei/golang/architecture/admission"
	"gopkg.in/yaml.v3"
)

func runAdmitChange(args []string) int {
	fs := flag.NewFlagSet("sensei admit-change", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var bundleDir, requestPath, graphNT, repo, policyID, detail, format, output string
	var check, requireAdmitted, requireWriteAdmitted bool
	fs.StringVar(&bundleDir, "bundle", "", "convergence bundle directory")
	fs.StringVar(&requestPath, "request", "", "architecture change request YAML")
	fs.StringVar(&graphNT, "graph-nt", "", "explicit graph snapshot N-Triples file")
	fs.StringVar(&repo, "repo", "", "repository checkout")
	fs.StringVar(&policyID, "policy", admission.PolicyStrictID, "admission policy id")
	fs.StringVar(&detail, "mode", "compact", "output detail: compact|full")
	fs.StringVar(&format, "format", "text", "output format: text|yaml|json")
	fs.StringVar(&output, "output", "", "write canonical full admission decision YAML")
	fs.BoolVar(&check, "check", false, "compare canonical decision bytes with --output and write nothing")
	fs.BoolVar(&requireAdmitted, "require-admitted", false, "exit non-zero unless requested decision is admitted")
	fs.BoolVar(&requireWriteAdmitted, "require-write-admitted", false, "exit non-zero unless mutation capability is admitted")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: sensei admit-change --bundle <dir> --request <request.yaml> --graph-nt <graph.nt> --repo <checkout> [flags]

Evaluates one exact bounded action against a verified convergence session.
Admission is permission to attempt, not proof of correctness.
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if bundleDir == "" || requestPath == "" || graphNT == "" || repo == "" {
		fmt.Fprintln(os.Stderr, "sensei admit-change: --bundle, --request, --graph-nt, and --repo are required")
		return 2
	}
	if check && output == "" {
		fmt.Fprintln(os.Stderr, "sensei admit-change: --check requires --output")
		return 2
	}
	if !validAdmissionFormat(format) || !validAdmissionDetail(detail) {
		fmt.Fprintln(os.Stderr, "sensei admit-change: --format must be text|yaml|json and --mode must be compact|full")
		return 2
	}
	decision, err := admission.Evaluate(admission.EvaluateOptions{BundleDir: bundleDir, RequestPath: requestPath, GraphNT: graphNT, Repo: repo, PolicyID: policyID})
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei admit-change: %v\n", err)
		return 1
	}
	if check {
		if err := admission.CheckDecision(output, decision); err != nil {
			fmt.Fprintf(os.Stderr, "sensei admit-change: %v\n", err)
			return 1
		}
	} else if output != "" {
		if err := admission.WriteCanonicalDecision(output, decision); err != nil {
			fmt.Fprintf(os.Stderr, "sensei admit-change: write decision: %v\n", err)
			return 1
		}
		if format == "text" {
			fmt.Fprintf(os.Stdout, "Admission: %s (%s)\n", decision.Decision, decision.AdmissionID)
		}
	}
	if output == "" || format != "text" {
		if err := printAdmissionDecision(os.Stdout, decision, format, detail); err != nil {
			fmt.Fprintf(os.Stderr, "sensei admit-change: %v\n", err)
			return 1
		}
	}
	if requireAdmitted && decision.Decision != admission.DecisionAdmitted && decision.Decision != admission.DecisionAdmittedWithConditions {
		return 1
	}
	if requireWriteAdmitted && decision.MutationCapability != admission.CapabilityAdmitted && decision.MutationCapability != admission.CapabilityAdmittedWithConditions {
		return 1
	}
	return 0
}

func runVerifyAdmission(args []string) int {
	fs := flag.NewFlagSet("sensei verify-admission", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var decisionPath, bundleDir, repo, format, output string
	var check, requireCompliant bool
	fs.StringVar(&decisionPath, "decision", "", "admission decision YAML")
	fs.StringVar(&bundleDir, "bundle", "", "current convergence bundle directory")
	fs.StringVar(&repo, "repo", "", "repository working tree")
	fs.StringVar(&format, "format", "text", "output format: text|yaml|json")
	fs.StringVar(&output, "output", "", "write canonical verification YAML")
	fs.BoolVar(&check, "check", false, "compare canonical verification bytes with --output and write nothing")
	fs.BoolVar(&requireCompliant, "require-compliant", false, "exit non-zero unless scope verification is compliant")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: sensei verify-admission --decision <decision.yaml> --bundle <dir> --repo <working-tree> [flags]

Verifies that the working-tree diff stayed inside an admission envelope.
Scope compliance is not correctness certification.
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if decisionPath == "" || bundleDir == "" || repo == "" {
		fmt.Fprintln(os.Stderr, "sensei verify-admission: --decision, --bundle, and --repo are required")
		return 2
	}
	if check && output == "" {
		fmt.Fprintln(os.Stderr, "sensei verify-admission: --check requires --output")
		return 2
	}
	if !validAdmissionFormat(format) {
		fmt.Fprintln(os.Stderr, "sensei verify-admission: --format must be text|yaml|json")
		return 2
	}
	verification, err := admission.Verify(admission.VerifyOptions{DecisionPath: decisionPath, BundleDir: bundleDir, Repo: repo})
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei verify-admission: %v\n", err)
		return 1
	}
	if check {
		if err := admission.CheckVerification(output, verification); err != nil {
			fmt.Fprintf(os.Stderr, "sensei verify-admission: %v\n", err)
			return 1
		}
	} else if output != "" {
		if err := admission.WriteCanonicalVerification(output, verification); err != nil {
			fmt.Fprintf(os.Stderr, "sensei verify-admission: write verification: %v\n", err)
			return 1
		}
		if format == "text" {
			fmt.Fprintf(os.Stdout, "Scope verification: %s\n", verification.Status)
		}
	}
	if output == "" || format != "text" {
		if err := printAdmissionVerification(os.Stdout, verification, format); err != nil {
			fmt.Fprintf(os.Stderr, "sensei verify-admission: %v\n", err)
			return 1
		}
	}
	if requireCompliant && verification.Status != admission.VerificationScopeCompliant {
		return 1
	}
	return 0
}

func runAdmissionStatus(args []string) int {
	fs := flag.NewFlagSet("sensei admission-status", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var decisionPath, verificationPath, format string
	fs.StringVar(&decisionPath, "decision", "", "admission decision YAML")
	fs.StringVar(&verificationPath, "verification", "", "optional admission verification YAML")
	fs.StringVar(&format, "format", "text", "output format: text|yaml|json")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: sensei admission-status --decision <decision.yaml> [--verification <verification.yaml>]

Reads admission receipts and verifies their digests without querying a graph or repository.
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if decisionPath == "" {
		fmt.Fprintln(os.Stderr, "sensei admission-status: --decision is required")
		return 2
	}
	if !validAdmissionFormat(format) {
		fmt.Fprintln(os.Stderr, "sensei admission-status: --format must be text|yaml|json")
		return 2
	}
	decision, err := admission.LoadDecision(decisionPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei admission-status: %v\n", err)
		return 1
	}
	var verification *admission.Verification
	if verificationPath != "" {
		v, err := admission.LoadVerification(verificationPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "sensei admission-status: %v\n", err)
			return 1
		}
		verification = &v
	}
	if err := printAdmissionStatus(os.Stdout, decision, verification, format); err != nil {
		fmt.Fprintf(os.Stderr, "sensei admission-status: %v\n", err)
		return 1
	}
	return 0
}

func validAdmissionFormat(format string) bool {
	switch strings.TrimSpace(format) {
	case "text", "yaml", "json":
		return true
	default:
		return false
	}
}

func validAdmissionDetail(detail string) bool {
	switch strings.TrimSpace(detail) {
	case "compact", "full":
		return true
	default:
		return false
	}
}

func printAdmissionDecision(out *os.File, d admission.Decision, format, detail string) error {
	switch format {
	case "text":
		_, err := fmt.Fprint(out, admission.RenderText(d, detail))
		return err
	case "yaml":
		data, err := admission.MarshalCanonicalDecisionYAML(d)
		if err != nil {
			return err
		}
		_, err = out.Write(data)
		return err
	case "json":
		data, err := admission.MarshalCanonicalDecisionJSON(d)
		if err != nil {
			return err
		}
		_, err = out.Write(append(data, '\n'))
		return err
	default:
		return fmt.Errorf("unknown format %s", format)
	}
}

func printAdmissionVerification(out *os.File, v admission.Verification, format string) error {
	switch format {
	case "text":
		_, err := fmt.Fprint(out, admission.RenderVerificationText(v))
		return err
	case "yaml":
		data, err := admission.MarshalCanonicalVerificationYAML(v)
		if err != nil {
			return err
		}
		_, err = out.Write(data)
		return err
	case "json":
		data, err := admission.MarshalCanonicalVerificationJSON(v)
		if err != nil {
			return err
		}
		_, err = out.Write(append(data, '\n'))
		return err
	default:
		return fmt.Errorf("unknown format %s", format)
	}
}

func printAdmissionStatus(out *os.File, d admission.Decision, v *admission.Verification, format string) error {
	switch format {
	case "text":
		_, err := fmt.Fprint(out, admission.StatusText(d, v))
		return err
	case "yaml":
		payload := map[string]interface{}{"admission": d}
		if v != nil {
			payload["verification"] = *v
		}
		data, err := yaml.Marshal(payload)
		if err != nil {
			return err
		}
		_, err = out.Write(data)
		return err
	case "json":
		payload := map[string]interface{}{"admission": d}
		if v != nil {
			payload["verification"] = *v
		}
		data, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			return err
		}
		_, err = out.Write(append(data, '\n'))
		return err
	default:
		return fmt.Errorf("unknown format %s", format)
	}
}
