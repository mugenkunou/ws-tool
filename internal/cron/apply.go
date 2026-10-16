package cron

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// ApplyOptions configures ApplyCrontab.
type ApplyOptions struct {
	// CrontabFilePath is the path to the workspace crontab file
	// (e.g. <workspace>/configs/crontab).
	CrontabFilePath string
	// DryRun, when true, shows what would be changed without making changes.
	DryRun bool
}

// ApplyResult is returned by ApplyCrontab.
type ApplyResult struct {
	// AddedBlocks are the ws-managed block names that were installed or updated.
	AddedBlocks []string
	// SkippedBlocks are ws-managed block names that were already up to date.
	SkippedBlocks []string
	// NonWSLines is the count of non-ws-managed crontab lines that were preserved.
	NonWSLines int
	// DryRun is true when no changes were committed.
	DryRun bool
}

// ApplyCrontab reads the workspace crontab file at opts.CrontabFilePath and
// installs (or replaces) every ws-managed block it contains into the user's
// live crontab, while preserving any unmanaged lines.
//
// The workspace crontab is authoritative for ws blocks: every block found there
// is applied verbatim; blocks that no longer appear in the workspace file are
// removed from the live crontab.
//
// Non-ws lines (no ws marker) in the live crontab are kept as-is.
func ApplyCrontab(opts ApplyOptions) (ApplyResult, error) {
	result := ApplyResult{}

	// Read the workspace crontab file.
	wsBlocks, wsNonWS, err := parseWorkspaceCrontab(opts.CrontabFilePath)
	if err != nil {
		return result, err
	}

	// Read the current live crontab.
	liveCt, err := ReadCrontab()
	if err != nil {
		return result, err
	}

	// Extract and preserve non-ws-managed lines from the live crontab.
	liveNonWS := extractNonWSLines(liveCt)
	result.NonWSLines = len(liveNonWS)

	// Build the new crontab: non-ws lines first, then all ws blocks from the
	// workspace file.
	var parts []string

	// Preserve any non-ws lines from the live crontab.
	if len(liveNonWS) > 0 {
		parts = append(parts, strings.Join(liveNonWS, "\n"))
	}

	// Also include any non-ws lines from the workspace crontab file itself
	// (e.g. CRON_TZ=UTC settings).
	if len(wsNonWS) > 0 {
		parts = append(parts, strings.Join(wsNonWS, "\n"))
	}

	// Append each ws block.
	liveBlockTexts := extractWSBlocks(liveCt)
	for _, blk := range wsBlocks {
		parts = append(parts, blk.text)
		// Detect whether this is a change from the current live crontab.
		if existing, ok := liveBlockTexts[blk.name]; ok && existing == blk.text {
			result.SkippedBlocks = append(result.SkippedBlocks, blk.name)
		} else {
			result.AddedBlocks = append(result.AddedBlocks, blk.name)
		}
	}

	newCt := strings.Join(parts, "\n")
	if newCt != "" && !strings.HasSuffix(newCt, "\n") {
		newCt += "\n"
	}

	if opts.DryRun {
		result.DryRun = true
		return result, nil
	}

	return result, WriteCrontab(newCt)
}

// cronBlock holds a named ws-managed block and its raw text.
type cronBlock struct {
	name string
	text string
}

// parseWorkspaceCrontab reads the workspace crontab file and separates
// ws-managed blocks from non-ws lines (e.g. CRON_TZ settings).
func parseWorkspaceCrontab(path string) (blocks []cronBlock, nonWS []string, err error) {
	f, openErr := os.Open(path)
	if openErr != nil {
		return nil, nil, fmt.Errorf("cannot read workspace crontab %s: %w", path, openErr)
	}
	defer f.Close()

	var currentBlock *cronBlock
	var currentLines []string

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		trimmed := strings.TrimSpace(line)

		if currentBlock != nil {
			// Inside a block: collect lines until the end marker.
			currentLines = append(currentLines, line)
			if trimmed == CronMarkerEnd(currentBlock.name) {
				currentBlock.text = strings.Join(currentLines, "\n") + "\n"
				blocks = append(blocks, *currentBlock)
				currentBlock = nil
				currentLines = nil
			}
			continue
		}

		// Look for a start marker.
		for _, name := range AllNames() {
			if trimmed == CronMarkerStart(name) {
				currentBlock = &cronBlock{name: name}
				currentLines = []string{line}
				break
			}
		}
		if currentBlock != nil {
			continue
		}

		// Non-ws line (e.g. CRON_TZ, comments, or user entries).
		if trimmed != "" {
			nonWS = append(nonWS, line)
		}
	}
	return blocks, nonWS, sc.Err()
}

// extractNonWSLines returns lines from the live crontab that are not part of
// any ws-managed block.
func extractNonWSLines(ct string) []string {
	var out []string
	inBlock := false
	for line := range strings.SplitSeq(ct, "\n") {
		trimmed := strings.TrimSpace(line)

		if inBlock {
			for _, name := range AllNames() {
				if trimmed == CronMarkerEnd(name) {
					inBlock = false
					break
				}
			}
			continue
		}

		for _, name := range AllNames() {
			if trimmed == CronMarkerStart(name) {
				inBlock = true
				break
			}
		}
		if inBlock {
			continue
		}

		if trimmed != "" {
			out = append(out, line)
		}
	}
	return out
}

// extractWSBlocks returns the raw text of each named ws block in the crontab.
func extractWSBlocks(ct string) map[string]string {
	result := make(map[string]string)
	var currentName string
	var currentLines []string

	for line := range strings.SplitSeq(ct, "\n") {
		trimmed := strings.TrimSpace(line)

		if currentName != "" {
			currentLines = append(currentLines, line)
			if trimmed == CronMarkerEnd(currentName) {
				result[currentName] = strings.Join(currentLines, "\n") + "\n"
				currentName = ""
				currentLines = nil
			}
			continue
		}

		for _, name := range AllNames() {
			if trimmed == CronMarkerStart(name) {
				currentName = name
				currentLines = []string{line}
				break
			}
		}
	}
	return result
}
