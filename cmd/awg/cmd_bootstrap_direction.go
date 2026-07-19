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

type bootstrapDirectionDigestReport struct {
	RepositoryRoot       string   `json:"repository_root" yaml:"repository_root"`
	BaseRevision         string   `json:"base_revision" yaml:"base_revision"`
	File                 string   `json:"file" yaml:"file"`
	GovernedRecordIDs    []string `json:"governed_record_ids" yaml:"governed_record_ids"`
	MutationDigestSHA256 string   `json:"mutation_digest_sha256" yaml:"mutation_digest_sha256"`
}

func runBootstrapDirectionDigest(args []string) int {
	fs := flag.NewFlagSet("sensei bootstrap-direction-digest", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var repo, baseRevision, file, proposedFile, format string
	var records repeatStrings
	fs.StringVar(&repo, "repo", ".", "repository checkout")
	fs.StringVar(&baseRevision, "base-revision", "", "exact base git revision bound to the authorization")
	fs.StringVar(&file, "file", "", "repository-relative file covered by the bootstrap authorization")
	fs.StringVar(&proposedFile, "proposed-file", "", "filesystem path to the proposed post-change file bytes")
	fs.Var(&records, "record-id", "governed direction record id covered by the authorization (repeatable)")
	fs.StringVar(&format, "format", "text", "output format: text|yaml|json")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: sensei bootstrap-direction-digest --base-revision <sha> --file <path> --proposed-file <path> --record-id <id> [flags]

Computes the canonical mutation digest used by bootstrap direction authorizations.
The digest is bound to the exact file, base blob digest, proposed post-change bytes,
and declared governed record IDs. It is not a Git patch-text hash.
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if fs.NArg() != 0 {
		fs.Usage()
		return 2
	}
	repo = strings.TrimSpace(repo)
	baseRevision = strings.TrimSpace(baseRevision)
	file = strings.TrimSpace(file)
	proposedFile = strings.TrimSpace(proposedFile)
	if baseRevision == "" || file == "" || proposedFile == "" || len(records) == 0 {
		fs.Usage()
		return 2
	}
	d, err := admission.DirectionBootstrapMutationDigestFromFile(repo, baseRevision, file, proposedFile, records)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei bootstrap-direction-digest: %v\n", err)
		return 1
	}
	report := bootstrapDirectionDigestReport{
		RepositoryRoot:       repo,
		BaseRevision:         baseRevision,
		File:                 file,
		GovernedRecordIDs:    records,
		MutationDigestSHA256: d,
	}
	switch format {
	case "", "text":
		fmt.Fprintln(os.Stdout, d)
		return 0
	case "yaml":
		data, err := yaml.Marshal(map[string]bootstrapDirectionDigestReport{"architecture_bootstrap_direction_digest": report})
		if err != nil {
			fmt.Fprintf(os.Stderr, "sensei bootstrap-direction-digest: %v\n", err)
			return 1
		}
		_, _ = os.Stdout.Write(data)
		return 0
	case "json":
		data, err := json.MarshalIndent(map[string]bootstrapDirectionDigestReport{"architecture_bootstrap_direction_digest": report}, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "sensei bootstrap-direction-digest: %v\n", err)
			return 1
		}
		_, _ = os.Stdout.Write(append(data, '\n'))
		return 0
	default:
		fmt.Fprintln(os.Stderr, "sensei bootstrap-direction-digest: --format must be text|yaml|json")
		return 2
	}
}
