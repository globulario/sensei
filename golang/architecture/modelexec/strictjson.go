// SPDX-License-Identifier: AGPL-3.0-only

package modelexec

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// rejectDuplicateKeys fails on any object in the document that names the same
// key twice.
//
// encoding/json accepts duplicates and silently keeps the LAST one, and
// DisallowUnknownFields does not help: every key is known, one just wins.
// Against a closed contract that is a real hole — a response could carry
// "claims_canonical": true followed by "claims_canonical": false and be
// accepted with the bid erased. A closed contract has to be closed about how
// many times a field may appear, not only about which fields exist.
func rejectDuplicateKeys(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	return walkForDuplicates(dec, "")
}

func walkForDuplicates(dec *json.Decoder, path string) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for dec.More() {
			keyTok, err := dec.Token()
			if err != nil {
				return err
			}
			key, ok := keyTok.(string)
			if !ok {
				return fmt.Errorf("non-string object key at %s", path)
			}
			if seen[key] {
				return fmt.Errorf("duplicate key %q at %s", key, pathOr(path))
			}
			seen[key] = true
			if err := walkForDuplicates(dec, path+"."+key); err != nil {
				return err
			}
		}
		_, err := dec.Token() // closing }
		return err
	case '[':
		for i := 0; dec.More(); i++ {
			if err := walkForDuplicates(dec, fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
		_, err := dec.Token() // closing ]
		return err
	}
	return nil
}

func pathOr(path string) string {
	if path == "" {
		return "(root)"
	}
	return path
}
