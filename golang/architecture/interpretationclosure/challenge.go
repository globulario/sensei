// SPDX-License-Identifier: AGPL-3.0-only

package interpretationclosure

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

const (
	ChallengePlanSchemaVersion = "sensei.interpretation-closure.challenge.v1"
	DefaultChallengePlanBytes  = 1 << 20
)

// ChallengePlan contains questions only. It deliberately has no supported,
// contradicted, certified, or authority fields. Those outcomes can only be
// produced by an evidence owner after executing the probes against the bound
// repository checkout.
type ChallengePlan struct {
	SchemaVersion string    `json:"schema_version"`
	GoProbes      []GoProbe `json:"go_probes"`
}

func NormalizeChallengePlan(in ChallengePlan) ChallengePlan {
	out := in
	out.SchemaVersion = strings.TrimSpace(out.SchemaVersion)
	out.GoProbes = append([]GoProbe(nil), out.GoProbes...)
	for i := range out.GoProbes {
		out.GoProbes[i].ClaimID = strings.TrimSpace(out.GoProbes[i].ClaimID)
		out.GoProbes[i].PackagePattern = strings.TrimSpace(out.GoProbes[i].PackagePattern)
		out.GoProbes[i].TypeName = strings.TrimSpace(out.GoProbes[i].TypeName)
		out.GoProbes[i].Expected = strings.TrimSpace(out.GoProbes[i].Expected)
		out.GoProbes[i].InterfacePackage = strings.TrimSpace(out.GoProbes[i].InterfacePackage)
		out.GoProbes[i].InterfaceName = strings.TrimSpace(out.GoProbes[i].InterfaceName)
		// EvidenceReferences are runtime-owned and never part of an authored
		// challenge plan, even when a GoProbe is constructed programmatically.
		out.GoProbes[i].EvidenceReferences = nil
	}
	sort.Slice(out.GoProbes, func(i, j int) bool {
		a, b := out.GoProbes[i], out.GoProbes[j]
		ak := strings.Join([]string{a.ClaimID, string(a.Kind), a.PackagePattern, a.TypeName, a.Expected, a.InterfacePackage, a.InterfaceName, fmt.Sprint(a.Pointer)}, "\x00")
		bk := strings.Join([]string{b.ClaimID, string(b.Kind), b.PackagePattern, b.TypeName, b.Expected, b.InterfacePackage, b.InterfaceName, fmt.Sprint(b.Pointer)}, "\x00")
		return ak < bk
	})
	return out
}

func ValidateChallengePlan(plan ChallengePlan) error {
	plan = NormalizeChallengePlan(plan)
	if plan.SchemaVersion != ChallengePlanSchemaVersion {
		return fmt.Errorf("interpretationclosure: challenge plan schema_version %q, expected %q", plan.SchemaVersion, ChallengePlanSchemaVersion)
	}
	seen := map[string]bool{}
	for i, probe := range plan.GoProbes {
		if probe.ClaimID == "" || probe.PackagePattern == "" || probe.TypeName == "" {
			return fmt.Errorf("interpretationclosure: Go challenge probe %d requires claim_id, package_pattern, and type_name", i)
		}
		switch probe.Kind {
		case GoProbeTypeExists, GoProbeUnderlyingTypeEquals:
			if probe.Expected == "" {
				return fmt.Errorf("interpretationclosure: Go challenge probe %d requires expected", i)
			}
		case GoProbeImplementsInterface:
			if probe.Expected == "" || probe.InterfacePackage == "" || probe.InterfaceName == "" {
				return fmt.Errorf("interpretationclosure: Go implements-interface probe %d requires expected, interface_package, and interface_name", i)
			}
		default:
			return fmt.Errorf("interpretationclosure: Go challenge probe %d has unsupported kind %q", i, probe.Kind)
		}
		key := strings.Join([]string{probe.ClaimID, string(probe.Kind), probe.PackagePattern, probe.TypeName, probe.Expected, probe.InterfacePackage, probe.InterfaceName, fmt.Sprint(probe.Pointer)}, "\x00")
		if seen[key] {
			return fmt.Errorf("interpretationclosure: duplicate Go challenge probe for claim %q", probe.ClaimID)
		}
		seen[key] = true
	}
	return nil
}

func ChallengePlanDigest(plan ChallengePlan) (string, error) {
	plan = NormalizeChallengePlan(plan)
	if err := ValidateChallengePlan(plan); err != nil {
		return "", err
	}
	data, err := json.Marshal(plan)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// LoadChallengePlan reads a bounded, closed-schema JSON challenge file and
// returns both its normalized query document and semantic digest. The digest
// is suitable for evidence references on mechanically produced findings. A
// symlink is rejected rather than silently followed because this file defines
// the premise questions a run will execute.
func LoadChallengePlan(path string) (ChallengePlan, string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return ChallengePlan{}, "", errors.New("interpretationclosure: challenge plan path is required")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return ChallengePlan{}, "", fmt.Errorf("interpretationclosure: lstat challenge plan %q: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return ChallengePlan{}, "", fmt.Errorf("interpretationclosure: challenge plan %q must be a regular non-symlink file", path)
	}
	if info.Size() > DefaultChallengePlanBytes {
		return ChallengePlan{}, "", fmt.Errorf("interpretationclosure: challenge plan %q exceeds %d bytes", path, DefaultChallengePlanBytes)
	}
	file, err := os.Open(path)
	if err != nil {
		return ChallengePlan{}, "", fmt.Errorf("interpretationclosure: open challenge plan %q: %w", path, err)
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, DefaultChallengePlanBytes+1))
	if err != nil {
		return ChallengePlan{}, "", fmt.Errorf("interpretationclosure: read challenge plan %q: %w", path, err)
	}
	if len(raw) > DefaultChallengePlanBytes {
		return ChallengePlan{}, "", fmt.Errorf("interpretationclosure: challenge plan %q exceeds %d bytes while reading", path, DefaultChallengePlanBytes)
	}
	if err := rejectDuplicateChallengeKeys(raw); err != nil {
		return ChallengePlan{}, "", fmt.Errorf("interpretationclosure: challenge plan %q contains ambiguous JSON: %w", path, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var plan ChallengePlan
	if err := decoder.Decode(&plan); err != nil {
		return ChallengePlan{}, "", fmt.Errorf("interpretationclosure: parse challenge plan %q: %w", path, err)
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return ChallengePlan{}, "", fmt.Errorf("interpretationclosure: challenge plan %q contains trailing JSON", path)
	}
	plan = NormalizeChallengePlan(plan)
	digest, err := ChallengePlanDigest(plan)
	if err != nil {
		return ChallengePlan{}, "", err
	}
	return plan, digest, nil
}

// rejectDuplicateChallengeKeys rejects duplicate keys at every JSON object
// depth. encoding/json otherwise accepts them last-value-wins, which is too
// ambiguous for a governance input where a duplicate `expected` could erase
// the premise the reviewer thought they froze.
func rejectDuplicateChallengeKeys(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	var walkValue func() error
	walkValue = func() error {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		delim, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for decoder.More() {
				keyToken, err := decoder.Token()
				if err != nil {
					return err
				}
				key, ok := keyToken.(string)
				if !ok {
					return errors.New("object key is not a string")
				}
				if seen[key] {
					return fmt.Errorf("duplicate object key %q", key)
				}
				seen[key] = true
				if err := walkValue(); err != nil {
					return err
				}
			}
			end, err := decoder.Token()
			if err != nil || end != json.Delim('}') {
				return errors.New("unterminated object")
			}
		case '[':
			for decoder.More() {
				if err := walkValue(); err != nil {
					return err
				}
			}
			end, err := decoder.Token()
			if err != nil || end != json.Delim(']') {
				return errors.New("unterminated array")
			}
		default:
			return fmt.Errorf("unexpected JSON delimiter %q", delim)
		}
		return nil
	}
	if err := walkValue(); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}
