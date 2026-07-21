// SPDX-License-Identifier: AGPL-3.0-only

package seedmeta

import (
	"bufio"
	"bytes"
	"strings"
)

type TransactionStamp struct {
	Present              bool
	SeedDigest           string
	SeedTripleCount      string
	AwarenessGraphCommit string
	ServicesCommit       string
	Yaml2NTSha256        string
	BuildScriptSha256    string
}

func ParseTransactionStamp(data []byte) TransactionStamp {
	var out TransactionStamp
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 3 {
			continue
		}
		out.Present = true
		switch {
		case fields[0] == "seed" && fields[1] == "digest_sha256":
			out.SeedDigest = fields[2]
		case fields[0] == "seed" && fields[1] == "triple_count":
			out.SeedTripleCount = fields[2]
		case fields[0] == "repo" && fields[1] == "awareness-graph":
			out.AwarenessGraphCommit = fields[2]
		case fields[0] == "repo" && fields[1] == "services":
			out.ServicesCommit = fields[2]
		case fields[0] == "tool" && fields[1] == "yaml2nt":
			out.Yaml2NTSha256 = fields[2]
		case fields[0] == "file" && fields[1] == "build_script":
			out.BuildScriptSha256 = fields[2]
		}
	}
	return out
}
