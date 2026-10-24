package secret

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

// PassHealth describes the state of the Unix Password Store (pass) on this machine.
type PassHealth struct {
	Installed    bool   `json:"installed"`     // pass binary found in PATH
	GPGAvailable bool   `json:"gpg_available"` // gpg binary found in PATH
	Initialized  bool   `json:"initialized"`   // ~/.password-store exists and has .gpg-id
	GitBacked    bool   `json:"git_backed"`    // ~/.password-store/.git exists
	GitRemote    bool   `json:"git_remote"`    // git remote origin is configured
	EntryCount   int    `json:"entry_count"`   // number of .gpg files in the store
	StorePath    string `json:"store_path"`    // resolved store directory
}

// PassAuditFinding is a single advisory finding from auditing the pass store.
type PassAuditFinding struct {
	Entry   string `json:"entry"`
	Message string `json:"message"`
}

// PassAuditResult holds findings from auditing pass store contents.
type PassAuditResult struct {
	Findings []PassAuditFinding `json:"findings"`
}

// CheckPass detects the state of the Unix Password Store on this machine.
func CheckPass() PassHealth {
	h := PassHealth{}

	// Check gpg
	if _, err := exec.LookPath("gpg"); err == nil {
		h.GPGAvailable = true
	} else if _, err := exec.LookPath("gpg2"); err == nil {
		h.GPGAvailable = true
	}

	// Check pass
	if _, err := exec.LookPath("pass"); err == nil {
		h.Installed = true
	}

	// Resolve store path: PASSWORD_STORE_DIR or ~/.password-store
	// Shown to the user, so always absolute (spec "Path Rules").
	storePath := os.Getenv("PASSWORD_STORE_DIR")
	if storePath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return h
		}
		storePath = filepath.Join(home, ".password-store")
	} else {
		abs, err := wsconfig.ExpandUserPath(storePath)
		if err != nil {
			return h
		}
		storePath = abs
	}
	h.StorePath = storePath

	// Check initialized (store dir exists with .gpg-id)
	gpgIDPath := filepath.Join(storePath, ".gpg-id")
	if _, err := os.Stat(gpgIDPath); err == nil {
		h.Initialized = true
	}

	// Check git-backed
	gitPath := filepath.Join(storePath, ".git")
	if info, err := os.Stat(gitPath); err == nil && info.IsDir() {
		h.GitBacked = true
		h.GitRemote = hasGitRemote(filepath.Join(gitPath, "config"))
	}

	// Count entries
	if h.Initialized {
		h.EntryCount = countGPGFiles(storePath)
	}

	return h
}

// AuditPassStore inspects pass entries for best-practice adherence.
// It reads file metadata only — never decrypts entries.
func AuditPassStore(h PassHealth) PassAuditResult {
	result := PassAuditResult{Findings: []PassAuditFinding{}}

	if !h.Initialized {
		result.Findings = append(result.Findings, PassAuditFinding{
			Entry:   "",
			Message: "pass store is not initialized: run `ws pass init`",
		})
		return result
	}

	if !h.GitBacked {
		result.Findings = append(result.Findings, PassAuditFinding{
			Entry:   "",
			Message: "pass store is not git-backed: run `ws pass init`",
		})
	} else if !h.GitRemote {
		result.Findings = append(result.Findings, PassAuditFinding{
			Entry:   "",
			Message: "pass store has no git remote: run `ws pass backup enable --remote <url>`",
		})
	}

	if h.EntryCount == 0 {
		result.Findings = append(result.Findings, PassAuditFinding{
			Entry:   "",
			Message: "pass store is empty: no credentials stored yet",
		})
	}

	// Walk entries and check for single-line entries (missing metadata)
	if h.Initialized && h.EntryCount > 0 {
		findings := auditEntryMetadata(h.StorePath)
		result.Findings = append(result.Findings, findings...)
	}

	return result
}

// hasGitRemote reads a git config file and returns true if it contains
// a [remote "origin"] section with a url.
func hasGitRemote(gitConfigPath string) bool {
	data, err := os.ReadFile(gitConfigPath)
	if err != nil {
		return false
	}
	// Look for any [remote "..."] section with a url = line.
	// This is a lightweight check — no full INI parser needed.
	inRemote := false
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[remote ") {
			inRemote = true
			continue
		}
		if strings.HasPrefix(trimmed, "[") {
			inRemote = false
			continue
		}
		if inRemote && strings.HasPrefix(trimmed, "url = ") {
			return true
		}
	}
	return false
}

// countGPGFiles counts .gpg files in the store directory.
func countGPGFiles(root string) int {
	count := 0
	filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == ".extensions" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(d.Name(), ".gpg") {
			count++
		}
		return nil
	})
	return count
}

// auditEntryMetadata flags entries that are likely password-only (no username
// or URL metadata). Since we never decrypt entries, this uses a size heuristic:
// GPG-encrypted content has a fixed overhead of ~100-400 bytes depending on
// key type and algorithm. A single short password (8-40 chars) typically
// produces a .gpg file under 400 bytes with RSA-2048 and under 250 bytes
// with ECC. Multi-line entries (password + username + URL) are consistently
// larger. We only flag entries under git/ (the credential convention) since
// those benefit most from having username metadata.
func auditEntryMetadata(root string) []PassAuditFinding {
	var findings []PassAuditFinding
	filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == ".extensions" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".gpg") {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		entry := strings.TrimSuffix(rel, ".gpg")

		// Only audit git/ entries — these are credentials that should
		// have username metadata for the credential helper to work.
		if !strings.HasPrefix(filepath.ToSlash(entry), "git/") {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return nil
		}
		// Entries under ~450 bytes are likely single-line (password only).
		// This threshold accounts for GPG overhead across common key types.
		if info.Size() > 0 && info.Size() < 450 {
			findings = append(findings, PassAuditFinding{
				Entry:   entry,
				Message: "entry may lack metadata: add `username: <user>` on line 2",
			})
		}
		return nil
	})
	return findings
}

// ── Pass store operations (delegates to pass/gpg binaries) ──

// GPGKey represents a GPG secret key available on the system.
type GPGKey struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ListGPGKeys returns GPG secret keys available for pass initialization.
func ListGPGKeys() ([]GPGKey, error) {
	cmd := exec.Command("gpg", "--list-secret-keys", "--with-colons")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("gpg --list-secret-keys failed: %w", err)
	}
	return parseGPGKeys(string(out)), nil
}

func parseGPGKeys(output string) []GPGKey {
	var keys []GPGKey
	var currentID string
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Split(line, ":")
		if len(fields) < 10 {
			continue
		}
		if fields[0] == "sec" {
			currentID = fields[4]
		}
		if fields[0] == "uid" && currentID != "" {
			keys = append(keys, GPGKey{
				ID:   currentID,
				Name: fields[9],
			})
			currentID = ""
		}
	}
	return keys
}

// InitStore initializes the pass store with the given GPG key ID.
func InitStore(gpgID string) error {
	cmd := exec.Command("pass", "init", gpgID)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("pass init failed: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// InitGit enables git versioning for the pass store.
func InitGit() error {
	cmd := exec.Command("pass", "git", "init")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("pass git init failed: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// GitRemoteURL returns the origin remote URL of the pass store, or empty string.
func GitRemoteURL() string {
	h := CheckPass()
	if !h.GitBacked {
		return ""
	}
	cmd := exec.Command("git", "-C", h.StorePath, "remote", "get-url", "origin")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// InsertEntry stores a value in the pass store under the given entry name. It
// never overwrites: an existing entry is an error, and the caller picks another
// name (spec §5 secret store). The value goes to `pass insert -m` on stdin,
// never on a command line. -m reads until EOF, so one call serves single and
// multi-line values; plain `pass insert` asks for a retyped copy and the old
// `-f` form (which also overwrote) failed on that second read.
func InsertEntry(entryName, value string) error {
	if PassEntryExists(entryName) {
		return fmt.Errorf("pass entry %s already exists; pick another name", entryName)
	}
	cmd := exec.Command("pass", "insert", "-m", entryName)
	cmd.Stdin = strings.NewReader(value + "\n")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("pass insert failed: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// CopyCommand is the `symlink add` command (without the leading ws) that puts the
// pass store into the workspace, as one folder entry. The store path is written
// with ~ when it lies inside $HOME. MirrorPath lands it in symlinks/home/.password-store.
// A copy there is a second sync path: pass keeps syncing through its own git remote.
func CopyCommand(storePath string) string {
	p := storePath
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if rel, err := filepath.Rel(home, storePath); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
			p = "~/" + filepath.ToSlash(rel)
		}
	}
	return "symlink add " + p + " --dir"
}

// WorkspaceCopy reports where the pass store lives inside the workspace, when
// the store path is a symlink into it (what `ws symlink add --dir` leaves).
// ok is false when the store is not such a link.
func WorkspaceCopy(storePath, workspacePath string) (target string, ok bool) {
	info, err := os.Lstat(storePath)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		return "", false
	}
	resolved, err := filepath.EvalSymlinks(storePath)
	if err != nil {
		return "", false
	}
	ws := workspacePath
	if r, err := filepath.EvalSymlinks(workspacePath); err == nil {
		ws = r
	}
	rel, err := filepath.Rel(ws, resolved)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return filepath.Join(workspacePath, rel), true
}
