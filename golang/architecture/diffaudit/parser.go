// SPDX-License-Identifier: AGPL-3.0-only

package diffaudit

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path"
	"strconv"
	"strings"
)

// DiffHunk represents one line-range hunk in a file patch.
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

// IsHexSHA returns true if s is an exact 40-character hexadecimal Git commit SHA.
func IsHexSHA(s string) bool {
	if len(s) != 40 {
		return false
	}
	for _, r := range s {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return false
		}
	}
	return true
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

	if strings.ContainsRune(diffText, 0) {
		return nil, fmt.Errorf("diff payload contains invalid NUL byte")
	}

	scanner := bufio.NewScanner(strings.NewReader(diffText))
	var patches []ParsedFilePatch
	var current *ParsedFilePatch
	var currentHunk *DiffHunk
	var reasons []ReasonCode

	pathSeenExact := make(map[string]bool)
	pathSeenLower := make(map[string]string)

	finishCurrentHunk := func() error {
		if currentHunk != nil {
			actualOld := currentHunk.DeletedLines + (len(currentHunk.Lines) - currentHunk.AddedLines - currentHunk.DeletedLines)
			actualNew := currentHunk.AddedLines + (len(currentHunk.Lines) - currentHunk.AddedLines - currentHunk.DeletedLines)

			if currentHunk.OldLines != actualOld || currentHunk.NewLines != actualNew {
				return fmt.Errorf("hunk %d in file %q line count mismatch: declared old=%d/new=%d, actual old=%d/new=%d",
					currentHunk.Index, current.Path, currentHunk.OldLines, currentHunk.NewLines, actualOld, actualNew)
			}

			current.Hunks = append(current.Hunks, *currentHunk)
			currentHunk = nil
		}
		return nil
	}

	finishCurrentFile := func() error {
		if err := finishCurrentHunk(); err != nil {
			return err
		}
		if current != nil {
			if len(current.Hunks) == 0 && !current.IsBinary && current.Kind != ChangeRename && current.Kind != ChangeModeChange {
				return fmt.Errorf("file %q has an incomplete header-only patch with no hunks or metadata", current.Path)
			}

			if len(current.Hunks) > opts.MaxHunks {
				return fmt.Errorf("file %q exceeds maximum hunk limit of %d", current.Path, opts.MaxHunks)
			}
			if pathSeenExact[current.Path] {
				return fmt.Errorf("duplicate logical file path in diff: %q", current.Path)
			}
			lowerPath := strings.ToLower(current.Path)
			if existing, collide := pathSeenLower[lowerPath]; collide && existing != current.Path {
				return fmt.Errorf("case-collision path ambiguity in diff: %q vs %q", current.Path, existing)
			}

			pathSeenExact[current.Path] = true
			pathSeenLower[lowerPath] = current.Path
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

			oldPath, newPath, err := parseGitDiffHeader(line[11:])
			if err != nil {
				return nil, fmt.Errorf("line %d: malformed diff --git header: %w", lineNum, err)
			}

			p, err := sanitizePath(newPath)
			if err != nil {
				if oldP, oldErr := sanitizePath(oldPath); oldErr == nil && oldP != "" {
					p = oldP
				} else {
					return nil, fmt.Errorf("line %d: invalid target path in diff: %w", lineNum, err)
				}
			}

			cleanOld, _ := sanitizePath(oldPath)

			current = &ParsedFilePatch{
				Path:    p,
				OldPath: cleanOld,
				Kind:    ChangeModify,
			}
			continue
		}

		if current == nil {
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
			renOld, err := sanitizePath(line[12:])
			if err != nil || renOld == "" {
				return nil, fmt.Errorf("line %d: invalid rename from path: %w", lineNum, err)
			}
			current.OldPath = renOld
			continue
		}
		if strings.HasPrefix(line, "rename to ") {
			current.Kind = ChangeRename
			newP, err := sanitizePath(line[10:])
			if err != nil || newP == "" {
				return nil, fmt.Errorf("line %d: invalid rename to path: %w", lineNum, err)
			}
			current.Path = newP
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
		if strings.HasPrefix(line, "--- ") {
			headerOld := parseQuotedPath(line[4:])
			if headerOld != "/dev/null" {
				cleanOld, err := sanitizePath(headerOld)
				if err != nil {
					return nil, fmt.Errorf("line %d: invalid --- header path: %w", lineNum, err)
				}
				if current.OldPath != "" && cleanOld != current.OldPath && cleanOld != current.Path {
					return nil, fmt.Errorf("line %d: --- header path %q conflicts with diff header path %q", lineNum, cleanOld, current.OldPath)
				}
			}
			continue
		}
		if strings.HasPrefix(line, "+++ ") {
			headerNew := parseQuotedPath(line[4:])
			if headerNew != "/dev/null" {
				cleanNew, err := sanitizePath(headerNew)
				if err != nil {
					return nil, fmt.Errorf("line %d: invalid +++ header path: %w", lineNum, err)
				}
				if cleanNew != current.Path {
					return nil, fmt.Errorf("line %d: +++ header path %q conflicts with diff header path %q", lineNum, cleanNew, current.Path)
				}
			}
			continue
		}
		if strings.HasPrefix(line, "\\ No newline at end of file") {
			continue
		}
		if strings.HasPrefix(line, "@@ ") {
			if err := finishCurrentHunk(); err != nil {
				return nil, err
			}
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
			} else {
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
		ReasonCodes: deduplicateReasonCodes(reasons),
	}, nil
}

func parseGitDiffHeader(line string) (oldPath, newPath string, err error) {
	line = strings.TrimSpace(line)
	if strings.HasPrefix(line, "\"") {
		idx := strings.Index(line[1:], "\"")
		if idx == -1 {
			return "", "", fmt.Errorf("unclosed quote in path")
		}
		oldPath = line[:idx+2]
		rest := strings.TrimSpace(line[idx+2:])
		newPath = rest
	} else {
		parts := strings.Split(line, " ")
		if len(parts) < 2 {
			return "", "", fmt.Errorf("invalid header tokens")
		}
		oldPath = parts[0]
		newPath = parts[len(parts)-1]
	}
	return parseQuotedPath(oldPath), parseQuotedPath(newPath), nil
}

func parseQuotedPath(p string) string {
	p = strings.TrimSpace(p)
	if strings.HasPrefix(p, "\"") && strings.HasSuffix(p, "\"") {
		if unquoted, err := strconv.Unquote(p); err == nil {
			p = unquoted
		}
	}
	if strings.HasPrefix(p, "a/") || strings.HasPrefix(p, "b/") {
		return p[2:]
	}
	return p
}

func sanitizePath(p string) (string, error) {
	p = parseQuotedPath(p)
	if p == "" || p == "/dev/null" {
		return "", nil
	}
	if strings.HasPrefix(p, "/") || strings.HasPrefix(p, "\\") {
		return "", fmt.Errorf("absolute path forbidden: %s", p)
	}
	if len(p) >= 2 && p[1] == ':' {
		return "", fmt.Errorf("windows drive path forbidden: %s", p)
	}
	clean := path.Clean(strings.ReplaceAll(p, "\\", "/"))
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("path traversal forbidden: %s", p)
	}
	return clean, nil
}

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

	var err1, err2 error
	oldStart, oldLines, err1 = parseRange(oldSpec)
	newStart, newLines, err2 = parseRange(newSpec)
	if err1 != nil || err2 != nil {
		return 0, 0, 0, 0, fmt.Errorf("invalid range numbers in header spec: %s", spec)
	}
	return oldStart, oldLines, newStart, newLines, nil
}

func parseRange(spec string) (start, count int, err error) {
	parts := strings.Split(spec, ",")
	if _, err := fmt.Sscanf(parts[0], "%d", &start); err != nil {
		return 0, 0, fmt.Errorf("invalid start line: %w", err)
	}
	if len(parts) > 1 {
		if _, err := fmt.Sscanf(parts[1], "%d", &count); err != nil {
			return 0, 0, fmt.Errorf("invalid line count: %w", err)
		}
	} else {
		count = 1
	}
	return start, count, nil
}

func deduplicateReasonCodes(in []ReasonCode) []ReasonCode {
	if len(in) == 0 {
		return nil
	}
	m := make(map[ReasonCode]bool)
	for _, r := range in {
		m[r] = true
	}
	out := make([]ReasonCode, 0, len(m))
	for r := range m {
		out = append(out, r)
	}
	return out
}
