// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/globulario/sensei/golang/governancepack"
	awarenesspb "github.com/globulario/sensei/golang/pb"
	"gopkg.in/yaml.v3"
)

type localMetadataSummary struct {
	candidateState       awarenesspb.CandidateQueueState
	candidateFileCount   int64
	candidateEntryCount  int64
	benchmarkState       awarenesspb.BenchmarkState
	benchmarkContracts   int64
	benchmarkEvents      int64
	benchmarkLatestUnix  int64
	benchmarkLatestTask  string
	benchmarkLatestScore int64
	benchmarkLatestCert  string
	governancePackState  awarenesspb.GovernancePackState
	governancePackID     string
	governancePackVer    string
	governancePackDigest string
	governancePublisher  string
	combinedGraphDigest  string
	combinedGraphTriples int64
}

type benchmarkLearningEventDoc struct {
	LearningEvent struct {
		Task                string `yaml:"task"`
		CertificationStatus string `yaml:"certification_status"`
		Current             struct {
			Score int `yaml:"score"`
		} `yaml:"current"`
	} `yaml:"learning_event"`
}

var learningEventStampRE = regexp.MustCompile(`-(\d{8}T\d{6}Z)\.yaml$`)

func collectLocalMetadata() localMetadataSummary {
	root, ok := detectMetadataRepoRoot()
	if !ok {
		return localMetadataSummary{
			candidateState: awarenesspb.CandidateQueueState_CANDIDATE_QUEUE_STATE_NOT_DETECTED,
			benchmarkState: awarenesspb.BenchmarkState_BENCHMARK_STATE_NOT_DETECTED,
		}
	}
	return collectLocalMetadataFromRoot(root)
}

func collectLocalMetadataFromRoot(root string) localMetadataSummary {
	return localMetadataSummary{
		candidateState:      detectCandidateQueueState(root),
		candidateFileCount:  countCandidateFiles(root),
		candidateEntryCount: countCandidateEntries(root),
	}.withBenchmark(root).withGovernance(root)
}

func (s localMetadataSummary) withBenchmark(root string) localMetadataSummary {
	contractsDir := filepath.Join(root, "eval", "multi-swe-bench", "contracts")
	eventsDir := filepath.Join(root, "eval", "multi-swe-bench", "notes", "learning_events")
	if !dirExists(contractsDir) && !dirExists(eventsDir) {
		s.benchmarkState = awarenesspb.BenchmarkState_BENCHMARK_STATE_NOT_DETECTED
		return s
	}
	s.benchmarkContracts = countYAMLFiles(contractsDir, nil)
	s.benchmarkEvents = countYAMLFiles(eventsDir, func(name string) bool {
		return !strings.HasSuffix(name, "-latest.yaml")
	})
	task, score, cert, ts := latestBenchmarkLearningEvent(eventsDir)
	s.benchmarkLatestTask = task
	s.benchmarkLatestScore = int64(score)
	s.benchmarkLatestCert = cert
	s.benchmarkLatestUnix = ts
	if s.benchmarkContracts > 0 || s.benchmarkEvents > 0 {
		s.benchmarkState = awarenesspb.BenchmarkState_BENCHMARK_STATE_PRESENT
	} else {
		s.benchmarkState = awarenesspb.BenchmarkState_BENCHMARK_STATE_EMPTY
	}
	return s
}

func (s localMetadataSummary) withGovernance(root string) localMetadataSummary {
	status := governancepack.AssessLocalStatus(root, Version)
	s.governancePackState = governanceStateProto(status.State)
	if status.Active != nil {
		s.governancePackID = status.Active.PackID
		s.governancePackVer = status.Active.PackVersion
		s.governancePackDigest = status.Active.PayloadDigestSHA256
		s.governancePublisher = status.Active.PublisherID
	}
	if status.CombinedGraph.Digest != "" {
		s.combinedGraphDigest = status.CombinedGraph.Digest
		s.combinedGraphTriples = status.CombinedGraph.TripleCount
	}
	return s
}

func governanceStateProto(state string) awarenesspb.GovernancePackState {
	switch state {
	case governancepack.StateNotDetected:
		return awarenesspb.GovernancePackState_GOVERNANCE_PACK_STATE_NOT_DETECTED
	case governancepack.StateNone:
		return awarenesspb.GovernancePackState_GOVERNANCE_PACK_STATE_NONE
	case governancepack.StateCurrent:
		return awarenesspb.GovernancePackState_GOVERNANCE_PACK_STATE_CURRENT
	case governancepack.StateStale:
		return awarenesspb.GovernancePackState_GOVERNANCE_PACK_STATE_STALE
	case governancepack.StateUnknown:
		return awarenesspb.GovernancePackState_GOVERNANCE_PACK_STATE_UNKNOWN
	default:
		return awarenesspb.GovernancePackState_GOVERNANCE_PACK_STATE_UNVERIFIED
	}
}

func detectMetadataRepoRoot() (string, bool) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", false
	}
	for dir := cwd; ; dir = filepath.Dir(dir) {
		if dirExists(filepath.Join(dir, "docs", "awareness")) || dirExists(filepath.Join(dir, "eval", "multi-swe-bench")) {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
	}
}

func detectCandidateQueueState(root string) awarenesspb.CandidateQueueState {
	dir := filepath.Join(root, "docs", "awareness", "candidates")
	if !dirExists(dir) {
		return awarenesspb.CandidateQueueState_CANDIDATE_QUEUE_STATE_NOT_DETECTED
	}
	if countYAMLFiles(dir, nil) == 0 || countCandidateEntries(root) == 0 {
		return awarenesspb.CandidateQueueState_CANDIDATE_QUEUE_STATE_EMPTY
	}
	return awarenesspb.CandidateQueueState_CANDIDATE_QUEUE_STATE_PRESENT
}

func countCandidateFiles(root string) int64 {
	return countYAMLFiles(filepath.Join(root, "docs", "awareness", "candidates"), nil)
}

func countCandidateEntries(root string) int64 {
	dir := filepath.Join(root, "docs", "awareness", "candidates")
	if !dirExists(dir) {
		return 0
	}
	var total int64
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !isYAMLFile(path) {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		total += countCandidateEntriesInYAML(data)
		return nil
	})
	return total
}

func countCandidateEntriesInYAML(data []byte) int64 {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return 0
	}
	return countCandidateIDs(&doc)
}

func countCandidateIDs(n *yaml.Node) int64 {
	if n == nil {
		return 0
	}
	switch n.Kind {
	case yaml.DocumentNode:
		var total int64
		for _, child := range n.Content {
			total += countCandidateIDs(child)
		}
		return total
	case yaml.SequenceNode:
		var total int64
		for _, child := range n.Content {
			total += countCandidateIDs(child)
		}
		return total
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			if n.Content[i].Value == "id" && strings.TrimSpace(n.Content[i+1].Value) != "" {
				return 1
			}
		}
	}
	return 0
}

func latestBenchmarkLearningEvent(dir string) (task string, score int, cert string, unix int64) {
	if !dirExists(dir) {
		return "", 0, "", 0
	}
	var latestFile string
	var latestTime time.Time
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !isYAMLFile(path) || strings.HasSuffix(path, "-latest.yaml") {
			return nil
		}
		ts := learningEventTime(path)
		if ts.After(latestTime) {
			latestTime = ts
			latestFile = path
		}
		return nil
	})
	if latestFile == "" {
		return "", 0, "", 0
	}
	data, err := os.ReadFile(latestFile)
	if err != nil {
		return "", 0, "", latestTime.Unix()
	}
	var doc benchmarkLearningEventDoc
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return "", 0, "", latestTime.Unix()
	}
	return strings.TrimSpace(doc.LearningEvent.Task), doc.LearningEvent.Current.Score, strings.TrimSpace(doc.LearningEvent.CertificationStatus), latestTime.Unix()
}

func learningEventTime(path string) time.Time {
	base := filepath.Base(path)
	if m := learningEventStampRE.FindStringSubmatch(base); len(m) == 2 {
		if ts, err := time.Parse("20060102T150405Z", m[1]); err == nil {
			return ts
		}
	}
	if info, err := os.Stat(path); err == nil {
		return info.ModTime().UTC()
	}
	return time.Time{}
}

func countYAMLFiles(dir string, keep func(name string) bool) int64 {
	if !dirExists(dir) {
		return 0
	}
	var total int64
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !isYAMLFile(path) {
			return nil
		}
		if keep != nil && !keep(filepath.Base(path)) {
			return nil
		}
		total++
		return nil
	})
	return total
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func isYAMLFile(path string) bool {
	return strings.HasSuffix(path, ".yaml") || strings.HasSuffix(path, ".yml")
}
