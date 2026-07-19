// SPDX-License-Identifier: AGPL-3.0-only

package diffaudit

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path"
	"strings"
)

// ParsedDiff Hunk represents one line-range hunk in a file patch.
type DiffHunk struct {
	Index        int      `json:"index"`
	OldStart     int      `json:"old_start"`
	OldLines     int      `json:"old_lines"`
	NewStart     int      `json:"new_start"`
	NewLines     int      `json:"new_lines"`
	Header       string   `json:"header"`
	Lines        []string `json:"lines"`
	AddedLines   int      `json:"added_lines"`
	DeletedLines int      `json:"deleted_lines"`
}

// ParsedFilePatch represents one parsed file patch in the diff.
type ParsedFilePatch struct {
	Path         string     `json:"path"`
	OldPath      string     `json:"old_path,omitempty"`
	Kind         ChangeKind `json:"kind"`
	OldMode      string     `json:"old_mode,omitempty"`
	NewMode      string     `json:"new_mode,omitempty"`
	IsBinary     bool       `json:"is_binary"`
	Hunks        []DiffHunk `json:"hunks"`
	TotalAdded   int        `json:"total_added"`
	TotalDeleted int        `json:"total_deleted"`
}

// ParsedDiff represents the validated result of parsing a raw git diff.
type ParsedDiff struct {
	InputDigest string            `json:"input_digest"`
	Files       []ParsedFilePatch `json:"files"`
	ReasonCodes []ReasonCode      `json:"reason_codes,omitempty"`
	Errors      []string          `json:"errors,omitempty"`
}

// ParseOptions sets limits and rules for parsing.
type ParseOptions struct {
	MaxBytes int
	MaxFiles int
	MaxHunks int
}

// DefaultParseOptions returns standard safe limits.
func DefaultParseOptions() ParseOptions {
	return ParseOptions{
		MaxBytes: MaxDiffBytes,
		MaxFiles: MaxFileCount,
		MaxHunks: MaxHunkCount,
	}
}

// ParseDiff parses raw unified Git diff text cleanly and securely.
func ParseDiff(diffText string, opts ParseOptions) (*ParsedDiff, error) {
	if opts.MaxBytes == 0 {
		opts.MaxBytes = MaxDiffBytes
	}
	if opts.MaxFiles == 0 {
		opts.MaxFiles = MaxFileCount
	}
	if opts.MaxHunks == 0 {
		opts.MaxHunks = MaxHunkCount
	}

	if len(diffText) > opts.MaxBytes {
		return nil, fmt.Errorf("diff payload exceeds maximum size limit of %d bytes", opts.MaxBytes)
	}

	hash := sha256.Sum256([]byte(diffText))
	digest := hex.EncodeToString(hash[:])

	trimmed := strings.TrimSpace(diffText)
	if trimmed == "" {
		return nil, fmt.Errorf("diff payload is empty")
	}

	// Check for invalid NUL bytes or unprintable control characters
	if strings.ContainsRune(diffText, 0) {
		return nil, fmt.Errorf("diff payload contains invalid NUL byte")
	}

	scanner := bufio.NewScanner(strings.NewReader(diffText))
	var patches []ParsedFilePatch
	var current *ParsedFilePatch
	var currentHunk *DiffHunk
	var reasons []ReasonCode
	var errs []string

	pathSeen := make(map[string]bool)

	finishCurrentHunk := func() {
		if current != nil && currentHunk != nil {
			current.Hunks = append(current.Hunks, *currentHunk)
			currentHunk = nil
		}
	}

	finishCurrentFile := func() error {
		finishCurrentHunk()
		if current != nil {
			if len(current.Hunks) > opts.MaxHunks {
				return fmt.Errorf("file %q exceeds maximum hunk limit of %d", current.Path, opts.MaxHunks)
			}
			if pathSeen[current.Path] {
				return fmt.Errorf("duplicate or ambiguous file path in diff: %q", current.Path)
			}
			pathSeen[current.Path] = true
			patches = append(patches, *current)
			current = nil
		}
		return nil
	}

	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		if strings.HasPrefix(line, "diff --git ") {
			if err := finishCurrentFile(); err != nil {
				return nil, err
			}
			if len(patches) >= opts.MaxFiles {
				return nil, fmt.Errorf("diff payload exceeds maximum file count of %d", opts.MaxFiles)
			}

			parts := strings.Split(line[11:], " ")
			if len(parts) < 2 {
				return nil, fmt.Errorf("line %d: malformed diff --git header: %s", lineNum, line)
			}

			oldPath := cleanDiffPath(parts[0])
			newPath := cleanDiffPath(parts[len(parts)-1])

			p, err := sanitizePath(newPath)
			if err != nil {
				// Try oldPath if newPath is /dev/null (deleted file)
				if oldP, oldErr := sanitizePath(oldPath); oldErr == nil && oldP != "" {
					p = oldP
				} else {
					return nil, fmt.Errorf("line %d: invalid target path in diff: %w", lineNum, err)
				}
			}

			current = &ParsedFilePatch{
				Path:    p,
				OldPath: oldPath,
				Kind:    ChangeModify,
			}
			continue
		}

		if current == nil {
			// Skip unparsed preamble lines before first diff header
			continue
		}

		if strings.HasPrefix(line, "new file mode ") {
			current.Kind = ChangeAdd
			current.NewMode = strings.TrimSpace(line[14:])
			continue
		}
		if strings.HasPrefix(line, "deleted file mode ") {
			current.Kind = ChangeDelete
			current.OldMode = strings.TrimSpace(line[18:])
			continue
		}
		if strings.HasPrefix(line, "similarity index ") {
			continue
		}
		if strings.HasPrefix(line, "rename from ") {
			current.Kind = ChangeRename
			current.OldPath = strings.TrimSpace(line[12:])
			continue
		}
		if strings.HasPrefix(line, "rename to ") {
			current.Kind = ChangeRename
			newP, err := sanitizePath(strings.TrimSpace(line[10:]))
			if err == nil && newP != "" {
				current.Path = newP
			}
			continue
		}
		if strings.HasPrefix(line, "old mode ") {
			current.Kind = ChangeModeChange
			current.OldMode = strings.TrimSpace(line[9:])
			continue
		}
		if strings.HasPrefix(line, "new mode ") {
			current.Kind = ChangeModeChange
			current.NewMode = strings.TrimSpace(line[9:])
			continue
		}
		if strings.HasPrefix(line, "Binary files ") || strings.HasPrefix(line, "GIT binary patch") {
			current.IsBinary = true
			current.Kind = ChangeBinary
			reasons = append(reasons, ReasonUnsupportedDiffFeature)
			continue
		}
		if strings.HasPrefix(line, "--- ") || strings.HasPrefix(line, "+++ ") {
			continue
		}
		if strings.HasPrefix(line, "@@ ") {
			finishCurrentHunk()
			oldStart, oldLines, newStart, newLines, err := parseHunkHeader(line)
			if err != nil {
				return nil, fmt.Errorf("line %d: malformed hunk header: %w", lineNum, err)
			}
			currentHunk = &DiffHunk{
				Index:    len(current.Hunks) + 1,
				OldStart: oldStart,
				OldLines: oldLines,
				NewStart: newStart,
				NewLines: newLines,
				Header:   line,
			}
			continue
		}

		if currentHunk != nil {
			if strings.HasPrefix(line, "+") {
				currentHunk.AddedLines++
				current.TotalAdded++
				currentHunk.Lines = append(currentHunk.Lines, line)
			} else if strings.HasPrefix(line, "-") {
				currentHunk.DeletedLines++
				current.TotalDeleted++
				currentHunk.Lines = append(currentHunk.Lines, line)
			} else if strings.HasPrefix(line, " ") || line == "" {
				currentHunk.Lines = append(currentHunk.Lines, line)
			}
		}
	}

	if err := finishCurrentFile(); err != nil {
		return nil, err
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan error: %w", err)
	}

	if len(patches) == 0 {
		return nil, fmt.Errorf("no valid file patches parsed from diff")
	}

	return &ParsedDiff{
		InputDigest: digest,
		Files:       patches,
		ReasonCodes: reasons,
		Errors:      errs,
	}, nil
}

func cleanDiffPath(p string) string {
	p = strings.TrimSpace(p)
	p = strings.Trim(p, "\"")
	if strings.HasPrefix(p, "a/") || strings.HasPrefix(p, "b/") {
		return p[2:]
	}
	return p
}

// sanitizePath validates and cleans a file path, rejecting traversal attacks.
func sanitizePath(p string) (string, error) {
	p = cleanDiffPath(p)
	if p == "" || p == "/dev/null" {
		return "", nil
	}
	if strings.HasPrefix(p, "/") || strings.HasPrefix(p, "\\") {
		return "", fmt.Errorf("absolute path forbidden: %s", p)
	}
	// Check for windows drive letters e.g. C:
	if len(p) >= 2 && p[1] == ':' {
		return "", fmt.Errorf("windows drive path forbidden: %s", p)
	}
	clean := path.Clean(strings.ReplaceAll(p, "\\", "/"))
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("path traversal forbidden: %s", p)
	}
	return clean, nil
}

// parseHunkHeader parses @@ -oldStart,oldLines +newStart,newLines @@
func parseHunkHeader(line string) (oldStart, oldLines, newStart, newLines int, err error) {
	parts := strings.Split(line, "@@")
	if len(parts) < 3 {
		return 0, 0, 0, 0, fmt.Errorf("invalid header format")
	}
	spec := strings.TrimSpace(parts[1])
	subParts := strings.Fields(spec)
	if len(subParts) < 2 {
		return 0, 0, 0, 0, fmt.Errorf("invalid header spec: %s", spec)
	}

	oldSpec := strings.TrimPrefix(subParts[0], "-")
	newSpec := strings.TrimPrefix(subParts[1], "+")

	oldStart, oldLines = parseRange(oldSpec)
	newStart, newLines = parseRange(newSpec)
	return oldStart, oldLines, newStart, newLines, nil
}

func parseRange(spec string) (start, count int) {
	parts := strings.Split(spec, ",")
	fmt.Sscanf(parts[0], "%d", &start)
	if len(parts) > 1 {
		fmt.Sscanf(parts[1], "%d", &count)
	} else {
		count = 1
	}
	return start, count
}
