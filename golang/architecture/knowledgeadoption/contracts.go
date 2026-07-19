// SPDX-License-Identifier: Apache-2.0

package knowledgeadoption

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/globulario/sensei/golang/architecture/adoption"
	"github.com/globulario/sensei/golang/architecture/graphsnapshot"
	"github.com/globulario/sensei/golang/rdf"
	"gopkg.in/yaml.v3"
)

type contractIntent struct {
	adoption.Receipt       `yaml:",inline"`
	ID                     string   `yaml:"id"`
	Level                  string   `yaml:"level"`
	Title                  string   `yaml:"title"`
	Intent                 string   `yaml:"intent"`
	ExpressedBy            []string `yaml:"expressed_by"`
	RequiredTests          []string `yaml:"required_tests"`
	PublicConsumerCategory string   `yaml:"public_consumer_category"`
	ReadOrWrite            string   `yaml:"read_or_write"`
	Stability              string   `yaml:"stability"`
	Interaction            string   `yaml:"interaction"`
}

func loadContractCandidates(opts Options) ([]candidate, error) {
	componentByFile, err := graphComponentFiles(opts.ProvisionalGraph)
	if err != nil {
		return nil, fmt.Errorf("contract component index: %w", err)
	}
	var paths []string
	for _, root := range []string{filepath.Join(opts.RepositoryRoot, "docs", "intent"), filepath.Join(opts.RepositoryRoot, "docs", "awareness")} {
		_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil || entry.IsDir() {
				return nil
			}
			if strings.HasSuffix(entry.Name(), ".yaml") || strings.HasSuffix(entry.Name(), ".yml") {
				if !strings.Contains(filepath.ToSlash(path), "/candidates/") {
					paths = append(paths, path)
				}
			}
			return nil
		})
	}
	sort.Strings(paths)
	seen := map[string]bool{}
	var out []candidate
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var intent contractIntent
		if err := yaml.Unmarshal(raw, &intent); err != nil || intent.ID == "" || strings.ToLower(strings.TrimSpace(intent.Level)) != "contract" || seen[intent.ID] {
			continue
		}
		seen[intent.ID] = true
		providers := map[string]bool{}
		var receipts []string
		for _, file := range intent.ExpressedBy {
			file = filepath.ToSlash(strings.TrimSpace(strings.TrimPrefix(file, "services/")))
			if file == "" {
				continue
			}
			receipts = append(receipts, "file:"+file)
			for _, component := range componentByFile[file] {
				providers[component] = true
			}
		}
		for _, test := range intent.RequiredTests {
			receipts = append(receipts, "test:"+strings.TrimSpace(test))
		}
		consumer := strings.TrimSpace(intent.PublicConsumerCategory)
		if consumer == "" {
			consumer = inferPublicConsumer(intent)
		}
		readWrite := strings.ToLower(strings.TrimSpace(intent.ReadOrWrite))
		if readWrite == "" {
			readWrite = inferReadWrite(intent)
		}
		interaction := strings.TrimSpace(intent.Interaction)
		if interaction == "" {
			interaction = inferInteraction(intent)
		}
		stability := strings.TrimSpace(intent.Stability)
		if stability == "" && intent.Status == adoption.PromotionMachineAdopted {
			stability = "stable"
		}
		out = append(out, candidate{
			ID: "candidate." + intent.ID, Class: ClassContract, Path: relative(opts.RepositoryRoot, path), Source: "intent_materialization",
			IntentID: intent.ID, Theme: intent.ID, Title: intent.Title, Statement: strings.TrimSpace(intent.Intent),
			SourceReceipts:     append(receipts, "documentation:"+relative(opts.RepositoryRoot, path)),
			ProviderComponents: keys(providers), PublicConsumerCategory: consumer, Interaction: interaction,
			ReadOrWrite: readWrite, Stability: stability, IntentMachineAdopted: adoption.ValidateMachineAdoption(intent.Receipt) == nil,
			InvalidationConditions: []string{"source interface, provider component, consumer category, or required Tests change"},
		})
	}
	return out, nil
}

func graphComponentFiles(raw []byte) (map[string][]string, error) {
	out := map[string][]string{}
	if len(bytes.TrimSpace(raw)) == 0 {
		return out, nil
	}
	triples, err := graphsnapshot.Read(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	components := map[string]string{}
	for _, triple := range triples {
		if triple.Predicate == rdf.PropType && triple.ObjectIsIRI && triple.Object == rdf.ClassComponent {
			if id, ok := mintedID(triple.Subject, "component"); ok {
				components[triple.Subject] = id
			}
		}
	}
	for _, triple := range triples {
		component := components[triple.Subject]
		if component == "" || !triple.ObjectIsIRI || triple.Predicate != rdf.PropAnchoredIn {
			continue
		}
		if file, ok := mintedID(triple.Object, "sourceFile"); ok {
			out[file] = append(out[file], component)
		}
	}
	for file := range out {
		out[file] = normalized(out[file])
	}
	return out, nil
}

func mintedID(iri, classPath string) (string, bool) {
	prefix := rdf.AwNS + classPath + "/"
	if !strings.HasPrefix(iri, prefix) {
		return "", false
	}
	return rdf.DecodeIRIPath(strings.TrimPrefix(iri, prefix)), true
}

func inferPublicConsumer(intent contractIntent) string {
	value := strings.ToLower(intent.ID + " " + intent.Title)
	switch {
	case strings.Contains(value, "render"):
		return "net/http integration"
	case strings.Contains(value, "validator"):
		return "Binding implementation"
	case strings.Contains(value, "binding"):
		return "external Go caller"
	case strings.Contains(value, "response_writer") || strings.Contains(value, "responsewriter"):
		return "net/http integration"
	default:
		return ""
	}
}

func inferReadWrite(intent contractIntent) string {
	value := strings.ToLower(intent.ID + " " + intent.Title + " " + intent.Intent)
	reads := strings.Contains(value, "read") || strings.Contains(value, "decode") || strings.Contains(value, "validate")
	writes := strings.Contains(value, "write") || strings.Contains(value, "populate") || strings.Contains(value, "render")
	switch {
	case reads && writes:
		return "read_write"
	case writes:
		return "write"
	case reads:
		return "read"
	default:
		return ""
	}
}

func inferInteraction(intent contractIntent) string {
	value := strings.ToLower(intent.Title + " " + intent.Intent)
	if strings.Contains(value, "interface") || strings.Contains(value, "implements") || strings.Contains(value, "method") {
		return "public_go_interface"
	}
	if strings.Contains(value, "responsewriter") || strings.Contains(value, "render") || strings.Contains(value, "binding") {
		return "public_go_behavioral_boundary"
	}
	return ""
}

func keys(values map[string]bool) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
