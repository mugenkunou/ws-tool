package dotfile

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"
)

// FileClass is the heuristic classification of a file or directory entry.
type FileClass int

const (
	// ClassConfig is a likely user config file worth tracking.
	ClassConfig FileClass = iota
	// ClassState is likely app state, cache, or runtime data — skipped by default.
	ClassState
	// ClassSecret is likely a secret — the user is warned before tracking.
	ClassSecret
)

// String returns a short human-readable label.
func (c FileClass) String() string {
	switch c {
	case ClassConfig:
		return "config"
	case ClassState:
		return "likely state"
	case ClassSecret:
		return "secret"
	default:
		return "unknown"
	}
}

// secretBaseNames is the exact set of filenames that are always treated as secrets.
var secretBaseNames = map[string]bool{
	"id_rsa":      true,
	"id_dsa":      true,
	"id_ecdsa":    true,
	"id_ed25519":  true,
	"id_rsa_sk":   true,
	"id_ecdsa_sk": true,
	"credentials": true,
}

// secretPattern matches file basenames that indicate a secret.
var secretPattern = regexp.MustCompile(`(?i)(private[_\-]?key|_secret|_password|_token|_credential|\.pem$|\.key$|\.p12$|\.pfx$|\.asc$)`)

// statePattern matches directory or file names indicating app state or cache.
// Anchored to the full basename to avoid false positives.
var statePattern = regexp.MustCompile(`(?i)^(cache|caches|logs?|tmp|temp|history|sessions?|storage|state|locks?|run|pid)$`)

// stateExactNames are file basenames that are always machine-specific state.
var stateExactNames = map[string]bool{
	"known_hosts":     true,
	"known_hosts.old": true,
}

const secretContentSniffBytes = 512

// ClassifyFile returns the classification of a regular file at absPath.
// It uses filename heuristics and a lightweight content sniff.
func ClassifyFile(absPath string) FileClass {
	base := strings.ToLower(filepath.Base(absPath))

	if secretBaseNames[base] {
		return ClassSecret
	}
	if secretPattern.MatchString(base) {
		return ClassSecret
	}
	if stateExactNames[base] {
		return ClassState
	}
	if statePattern.MatchString(base) {
		return ClassState
	}
	if hasPEMPrivateKey(absPath) {
		return ClassSecret
	}
	if isBinaryFile(absPath) {
		return ClassState
	}
	return ClassConfig
}

// ClassifyDir returns the classification of a directory by name, file count, and size.
func ClassifyDir(name string, fileCount int, totalBytes int64) FileClass {
	lower := strings.ToLower(name)
	if statePattern.MatchString(lower) {
		return ClassState
	}
	const largeCount = 100
	const largeMB int64 = 10 << 20
	if fileCount > largeCount || totalBytes > largeMB {
		return ClassState
	}
	return ClassConfig
}

func hasPEMPrivateKey(absPath string) bool {
	f, err := os.Open(absPath)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, secretContentSniffBytes)
	n, _ := f.Read(buf)
	if n == 0 {
		return false
	}
	s := string(buf[:n])
	return strings.Contains(s, "-----BEGIN") &&
		(strings.Contains(s, "PRIVATE KEY") ||
			strings.Contains(s, "RSA PRIVATE") ||
			strings.Contains(s, "EC PRIVATE"))
}

func isBinaryFile(absPath string) bool {
	f, err := os.Open(absPath)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, 512)
	n, _ := f.Read(buf)
	if n == 0 {
		return false
	}
	for _, b := range buf[:n] {
		if b == 0 {
			return true
		}
	}
	return !utf8.Valid(buf[:n])
}
