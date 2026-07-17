// SPDX-License-Identifier: AGPL-3.0-only

package ledger

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/globulario/sensei/golang/architecture/binding"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"gopkg.in/yaml.v3"
)

func readHead(path string) (Head, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Head{}, err
	}
	var head Head
	if err := yaml.Unmarshal(data, &head); err != nil {
		return Head{}, err
	}
	return head, nil
}

func readEntry(path string) (Entry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Entry{}, err
	}
	var entry Entry
	if err := yaml.Unmarshal(data, &entry); err != nil {
		return Entry{}, err
	}
	return entry, nil
}

// failHeadWrites forces the next N HEAD writes to fail; it exists only for
// deterministic fault-injection tests of the durable-entry / HEAD-failure path.
var failHeadWrites int

// FailNextHeadWriteForTest makes exactly the next HEAD write fail. Test-only.
func FailNextHeadWriteForTest() { failHeadWrites = 1 }

// FailHeadWritesForTest makes the next n HEAD writes fail. Test-only.
func FailHeadWritesForTest(n int) { failHeadWrites = n }

func writeHead(path string, head Head) error {
	if failHeadWrites > 0 {
		failHeadWrites--
		return errors.New("injected head write fault")
	}
	data, err := binding.CanonicalYAML(head)
	if err != nil {
		return err
	}
	return writeFileAtomic(path, data)
}

func writeEntry(path string, entry Entry) error {
	data, err := binding.CanonicalYAML(entry)
	if err != nil {
		return err
	}
	return writeFileAtomic(path, data)
}

func ledgerEntryFilename(sequence int, eventType closureprotocol.LedgerEventType, digest string) string {
	return fmt.Sprintf("%06d-%s-%s.yaml", sequence, eventType, digest)
}

func parseSequenceFromFilename(name string) (int, error) {
	parts := strings.SplitN(name, "-", 2)
	if len(parts) != 2 {
		return 0, fmt.Errorf("ledger entry filename must begin with sequence")
	}
	return strconv.Atoi(parts[0])
}

func listLedgerEntryFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var files []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if name == "HEAD.yaml" || !strings.HasSuffix(name, ".yaml") {
			continue
		}
		files = append(files, filepath.Join(dir, name))
	}
	sort.Strings(files)
	return files, nil
}
