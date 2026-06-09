package cron

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// RunRecord is one entry in the state file representing a completed job run.
type RunRecord struct {
	Job      string
	Time     time.Time
	ExitCode int
}

// jsonRecord is the on-disk JSONL representation of a RunRecord.
type jsonRecord struct {
	Job  string `json:"job"`
	TS   string `json:"ts"`
	Exit int    `json:"exit"`
}

// ReadState reads all run records from the state file.
// Supports both the current JSONL format and the legacy space-delimited format
// for backward compatibility. Returns nil slice (not error) if the file does
// not exist.
func ReadState(statePath string) ([]RunRecord, error) {
	f, err := os.Open(statePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var records []RunRecord
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if r, ok := parseJSONLine(line); ok {
			records = append(records, r)
			continue
		}
		// Fall back to legacy format: "<job> <ts> <exit>".
		if r, ok := parseLegacyLine(line); ok {
			records = append(records, r)
		}
	}
	return records, sc.Err()
}

func parseJSONLine(line string) (RunRecord, bool) {
	var jr jsonRecord
	if err := json.Unmarshal([]byte(line), &jr); err != nil {
		return RunRecord{}, false
	}
	t, err := time.Parse(time.RFC3339, jr.TS)
	if err != nil {
		return RunRecord{}, false
	}
	return RunRecord{Job: jr.Job, Time: t, ExitCode: jr.Exit}, true
}

func parseLegacyLine(line string) (RunRecord, bool) {
	parts := strings.Fields(line)
	if len(parts) != 3 {
		return RunRecord{}, false
	}
	t, err := time.Parse(time.RFC3339, parts[1])
	if err != nil {
		return RunRecord{}, false
	}
	code, err := strconv.Atoi(parts[2])
	if err != nil {
		return RunRecord{}, false
	}
	return RunRecord{Job: parts[0], Time: t, ExitCode: code}, true
}

// LastRun returns the most recent run record for the named job.
// Returns a zero-value RunRecord (Time.IsZero() == true) if no records exist.
func LastRun(statePath, jobName string) (RunRecord, error) {
	records, err := ReadState(statePath)
	if err != nil {
		return RunRecord{}, err
	}
	var last RunRecord
	for _, r := range records {
		if r.Job == jobName {
			last = r
		}
	}
	return last, nil
}

// ReadLog returns at most n lines from logPath filtered to entries containing
// jobName in the log prefix. If jobName is empty, all lines are returned.
// If n <= 0, all matching lines are returned.
func ReadLog(logPath, jobName string, n int) ([]string, error) {
	f, err := os.Open(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	prefix := fmt.Sprintf("[%s]", jobName)
	var lines []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if jobName == "" || strings.Contains(line, prefix) {
			lines = append(lines, line)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}

	if n > 0 && len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines, nil
}
