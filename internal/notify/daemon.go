//go:build linux

package notify

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/mugenkunou/ws-tool/internal/config"
	"github.com/mugenkunou/ws-tool/internal/dotfile"
	"github.com/mugenkunou/ws-tool/internal/ignore"
	wslog "github.com/mugenkunou/ws-tool/internal/log"
	"github.com/mugenkunou/ws-tool/internal/manifest"
	"github.com/mugenkunou/ws-tool/internal/secret"
	"github.com/mugenkunou/ws-tool/internal/trash"
)

// RunDaemon runs the daemon event loop. It watches for filesystem changes
// via inotify and runs periodic scans. It blocks until SIGTERM or SIGINT
// is received.
func RunDaemon(opts DaemonOptions) error {
	ws := opts.WorkspacePath

	// Write active state with our PID.
	if _, err := Start(ws, os.Getpid()); err != nil {
		return fmt.Errorf("writing state: %w", err)
	}

	// Ensure state is marked inactive on exit.
	defer func() {
		_, _ = Stop(ws)
	}()

	// Set up inotify.
	ifd, err := inotifyInit()
	if err != nil {
		return fmt.Errorf("inotify init: %w", err)
	}
	defer syscall.Close(ifd)

	// Watch dotfiles directory — track the watch descriptor for routing.
	dotfilesDir := filepath.Join(ws, "ws", "dotfiles")
	var dotfileWD int = -1
	if info, err := os.Stat(dotfilesDir); err == nil && info.IsDir() {
		wd, wdErr := inotifyAddWatch(ifd, dotfilesDir)
		if wdErr == nil {
			dotfileWD = wd
		}
	}

	// Watch MEGA sync state directory if it exists.
	megaDir := megaSyncStateDir()
	if megaDir != "" {
		if info, err := os.Stat(megaDir); err == nil && info.IsDir() {
			_, _ = inotifyAddWatch(ifd, megaDir)
		}
	}

	// Signal handling for graceful shutdown.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	// inotify event channel — reads events with watch descriptors in a goroutine.
	inotifyCh := make(chan inotifyEvent, 1)
	go readInotifyEvents(ifd, inotifyCh)

	// Interval from config.
	interval := time.Duration(opts.Cfg.Notify.PollIntervalMin) * time.Minute
	if interval < time.Minute {
		interval = 10 * time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Push interval for periodic auto-push of dotfiles and pass store.
	pushInterval := time.Duration(opts.Cfg.Notify.PushIntervalMin) * time.Minute
	if pushInterval < time.Minute {
		pushInterval = 5 * time.Minute
	}
	pushTicker := time.NewTicker(pushInterval)
	defer pushTicker.Stop()

	// Run initial scan immediately.
	runDaemonScan(opts, "startup")

	// Debounce timer for health scan (all inotify events).
	var debounce *time.Timer
	debounceCh := make(chan struct{})

	// Separate debounce for dotfile git auto-sync (3s — editors do atomic saves).
	var dotfileDebounce *time.Timer
	dotfileSyncCh := make(chan struct{})

	for {
		select {
		case <-sigCh:
			return nil

		case evt := <-inotifyCh:
			// Coalesce rapid inotify events with a 500ms debounce for health scan.
			if debounce != nil {
				debounce.Stop()
			}
			debounce = time.AfterFunc(500*time.Millisecond, func() {
				select {
				case debounceCh <- struct{}{}:
				default:
				}
			})

			// If this is a dotfile change, also trigger git auto-sync (longer debounce).
			if dotfileWD >= 0 && evt.wd == dotfileWD && opts.Cfg.Dotfile.Git.Enabled && opts.Cfg.Dotfile.Git.AutoCommit {
				if dotfileDebounce != nil {
					dotfileDebounce.Stop()
				}
				dotfileDebounce = time.AfterFunc(3*time.Second, func() {
					select {
					case dotfileSyncCh <- struct{}{}:
					default:
					}
				})
			}

		case <-dotfileSyncCh:
			runDotfileGitSync(opts)

		case <-debounceCh:
			runDaemonScan(opts, "inotify")
			// Reset periodic timer after inotify-triggered scan.
			ticker.Reset(interval)

		case <-ticker.C:
			runDaemonScan(opts, "periodic")

		case <-pushTicker.C:
			runPeriodicPush(opts)
		}
	}
}

// runDaemonScan performs a full workspace scan, writes health.json,
// diffs violations for dedup, sends notifications for new issues,
// and updates notify.state.
func runDaemonScan(opts DaemonOptions, trigger string) {
	ws := opts.WorkspacePath
	cfg := opts.Cfg

	// Load current state for dedup.
	state, _ := Status(ws)

	// Run all subsystem scans (same as cmd/scan.go).
	health := performScan(ws, opts.ConfigPath, opts.ManifestPath, cfg, trigger)

	// Write health.json.
	_ = WriteHealth(ws, health)

	// Diff against known violations.
	newViolations := DiffViolations(state.KnownViolations, health.Violations)

	// Filter by configured events.
	newViolations = FilterByEvents(newViolations, cfg.Notify.Events)

	// Send notifications for net-new violations.
	for _, v := range newViolations {
		body := formatNotification(v)
		_ = SendNotification("ws", body)
	}

	// Update state.
	now := time.Now().UTC()
	state.LastScan = now
	state.UpdatedAt = now
	state.KnownViolations = ViolationKeys(health.Violations)
	if len(newViolations) > 0 {
		state.LastAlert = formatNotification(newViolations[0])
		state.LastAlertTime = now
	}
	_ = SaveState(statePath(ws), state)
}

// performScan runs all subsystem scans and builds a HealthSummary.
func performScan(workspacePath, configPath, manifestPath string, cfg config.Config, trigger string) HealthSummary {
	now := time.Now().UTC()
	h := HealthSummary{Timestamp: now, Trigger: trigger}

	var violations []HealthViolation

	// ignore scan
	userRules, err := ignore.LoadUserRules(ignore.UserRulesPath(workspacePath))
	if err != nil {
		userRules = ignore.DefaultUserRules()
	}
	engine := ignore.BuildEngine(userRules)
	{
		ignoreViolations, err := ignore.Scan(ignore.ScanOptions{
			WorkspacePath: workspacePath,
			WarnSizeMB:    cfg.Ignore.WarnSizeMB,
			CritSizeMB:    cfg.Ignore.CritSizeMB,
			MaxDepth:      cfg.Ignore.MaxDepth,
			Engine:        engine,
		})
		if err == nil {
			for _, v := range ignoreViolations {
				if v.Severity == "CRITICAL" {
					h.Summary.Ignore.Critical++
				} else {
					h.Summary.Ignore.Warning++
				}
				violations = append(violations, HealthViolation{
					Group:    v.Group,
					Type:     v.Type,
					Severity: v.Severity,
					Path:     v.Path,
					Message:  v.Message,
					SizeMB:   int(v.SizeBytes / (1024 * 1024)),
				})
			}
		}
	}

	// secret scan
	if cfg.Secret.Enabled {
		m, err := manifest.Load(manifestPath)
		if err == nil {
			allow := make(map[string]struct{}, len(m.Secret.Allowlist))
			for _, a := range m.Secret.Allowlist {
				allow[a] = struct{}{}
			}
			secretViolations, err := secret.Scan(secret.ScanOptions{
				WorkspacePath: workspacePath,
				Engine:        engine,
				Allowlist:     allow,
			})
			if err == nil {
				for _, v := range secretViolations {
					if v.Severity == "CRITICAL" {
						h.Summary.Secret.Critical++
					} else {
						h.Summary.Secret.Warning++
					}
					violations = append(violations, HealthViolation{
						Group:    v.Group,
						Type:     v.Type,
						Severity: v.Severity,
						Path:     v.Path,
						Message:  v.Message,
					})
				}
			}
		}
	}

	// dotfile scan
	dotIssues, err := dotfile.Scan(dotfile.ScanOptions{
		WorkspacePath: workspacePath,
		ManifestPath:  manifestPath,
	})
	if err == nil {
		for _, issue := range dotIssues {
			sev := "WARNING"
			if issue.Status == dotfile.StatusBroken {
				sev = "CRITICAL"
				h.Summary.Dotfile.Critical++
			} else {
				h.Summary.Dotfile.Warning++
			}
			violations = append(violations, HealthViolation{
				Group:    "dotfile",
				Type:     string(issue.Status),
				Severity: sev,
				Path:     issue.SystemPath,
				Message:  issue.Message,
			})
		}
	}

	// log scan
	logResult, err := wslog.Scan(workspacePath, cfg.Log.CapMB)
	if err == nil {
		h.Summary.Log.Active = logResult.Active
		if logResult.CapMB > 0 {
			h.Summary.Log.CapPercent = int(float64(logResult.StorageBytes) / float64(int64(logResult.CapMB)*1024*1024) * 100)
		}
	}

	// trash status + scan
	trashStatus, err := trash.GetStatus(cfg.Trash.RootDir)
	if err == nil {
		configured := trashStatus.WarningCount() == 0
		h.Summary.Trash.Configured = configured
		h.Summary.Trash.Warnings = trashStatus.WarningCount()
		if !configured {
			for _, w := range trashStatusWarnings(trashStatus) {
				violations = append(violations, HealthViolation{
					Group:    "trash",
					Type:     "machine-setup",
					Severity: "WARNING",
					Path:     w.path,
					Message:  w.message,
				})
			}
		}
	}

	trashScan, err := trash.Scan(trash.ScanOptions{
		RootDir:    cfg.Trash.RootDir,
		WarnSizeMB: cfg.Trash.WarnSizeMB,
	})
	if err == nil && trashScan.OverLimit {
		h.Summary.Trash.Warnings++
		violations = append(violations, HealthViolation{
			Group:    "trash",
			Type:     "trash-size",
			Severity: "WARNING",
			Path:     cfg.Trash.RootDir,
			Message:  fmt.Sprintf("trash size %d MB exceeds threshold %d MB", trashScan.SizeBytes/(1024*1024), trashScan.WarnSizeMB),
		})
	}

	h.Violations = violations
	h.ViolationsCount = len(violations)

	return h
}

type trashWarning struct {
	path    string
	message string
}

func trashStatusWarnings(s trash.Status) []trashWarning {
	var warnings []trashWarning
	if !s.ShellRMConfigured {
		warnings = append(warnings, trashWarning{path: "shell-rm", message: "shell rm integration not configured"})
	}
	if !s.VSCodeConfigured {
		warnings = append(warnings, trashWarning{path: "vscode", message: "VS Code delete-to-trash not configured"})
	}
	if !s.FileExplorerConfigured {
		warnings = append(warnings, trashWarning{path: "file-explorer", message: "file explorer soft-delete not configured"})
	}
	return warnings
}

// formatNotification builds a single-line notification body from a violation.
func formatNotification(v HealthViolation) string {
	switch v.Group {
	case "dotfile":
		return fmt.Sprintf("⚠ %s symlink is %s", v.Path, strings.ToLower(v.Type))
	case "secret":
		return fmt.Sprintf("🔒 new secret pattern found in %s", v.Path)
	case "ignore":
		if v.SizeMB > 0 {
			return fmt.Sprintf("📁 new %d MB file detected — %s", v.SizeMB, v.Path)
		}
		return fmt.Sprintf("📁 %s violation — %s", v.Type, v.Path)
	case "trash":
		return fmt.Sprintf("📦 %s", v.Message)
	default:
		return fmt.Sprintf("%s: %s — %s", v.Group, v.Type, v.Path)
	}
}

// megaSyncStateDir returns the MEGA sync state directory if it exists.
func megaSyncStateDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "share", "data", "Mega Limited", "MEGAsync")
}

// runDotfileGitSync performs a dotfile git auto-commit/push triggered by
// filesystem changes detected via inotify. Errors are non-fatal.
func runDotfileGitSync(opts DaemonOptions) {
	ws := opts.WorkspacePath
	cfg := opts.Cfg

	repoPath := filepath.Join(ws, "ws", "dotfiles")
	result := dotfile.GitSync(dotfile.GitSyncOptions{
		WorkspacePath: ws,
		RepoPath:      repoPath,
		RemoteURL:     cfg.Dotfile.Git.RemoteURL,
		Branch:        cfg.Dotfile.Git.Branch,
		AutoCommit:    cfg.Dotfile.Git.AutoCommit,
		AutoPush:      cfg.Dotfile.Git.AutoPush,
		CommitMessage: "auto: dotfile changed",
	})
	// Log errors to stderr for systemd journal visibility.
	if result.Error != "" {
		fmt.Fprintf(os.Stderr, "dotfile git sync: %s\n", result.Error)
	}
}

// runPeriodicPush pushes dotfiles and pass store git repos on a timer.
// This catches any commits that weren't pushed at commit time (e.g. network
// was down, or auto-commit happened but auto-push was temporarily off).
// Errors are non-fatal and logged for journal visibility.
func runPeriodicPush(opts DaemonOptions) {
	cfg := opts.Cfg
	ws := opts.WorkspacePath

	// Push dotfile git if enabled, has remote, and auto-push is on.
	if cfg.Dotfile.Git.Enabled && cfg.Dotfile.Git.AutoPush {
		repoPath := filepath.Join(ws, "ws", "dotfiles")
		if dotfile.GitHasRemote(repoPath) {
			branch := cfg.Dotfile.Git.Branch
			if branch == "" {
				branch = "main"
			}
			if err := dotfile.GitPush(repoPath, branch); err != nil {
				fmt.Fprintf(os.Stderr, "periodic dotfile push: %v\n", err)
			}
		}
	}

	// Push pass store git if it has a remote.
	passHealth := secret.CheckPass()
	if passHealth.GitBacked {
		remote := secret.GitRemoteURL()
		if remote != "" {
			if err := secret.GitPush(); err != nil {
				fmt.Fprintf(os.Stderr, "periodic pass push: %v\n", err)
			}
		}
	}
}

// ── inotify helpers (Linux-native, zero dependencies) ──

// inotifyEvent carries the watch descriptor so the daemon can route
// events to the correct handler (dotfile git sync vs full health scan).
type inotifyEvent struct {
	wd int
}

func inotifyInit() (int, error) {
	fd, err := syscall.InotifyInit1(syscall.IN_NONBLOCK | syscall.IN_CLOEXEC)
	if err != nil {
		return -1, err
	}
	return fd, nil
}

func inotifyAddWatch(fd int, path string) (int, error) {
	const mask = syscall.IN_CREATE | syscall.IN_DELETE | syscall.IN_MODIFY |
		syscall.IN_MOVED_FROM | syscall.IN_MOVED_TO
	pathBytes, err := syscall.BytePtrFromString(path)
	if err != nil {
		return -1, err
	}
	wd, _, errno := syscall.Syscall(syscall.SYS_INOTIFY_ADD_WATCH,
		uintptr(fd), uintptr(unsafe.Pointer(pathBytes)), uintptr(mask))
	if errno != 0 {
		return -1, errno
	}
	return int(wd), nil
}

func readInotifyEvents(fd int, ch chan<- inotifyEvent) {
	buf := make([]byte, 4096)
	for {
		n, err := syscall.Read(fd, buf)
		if err != nil {
			if err == syscall.EAGAIN {
				// Non-blocking read, no events ready — poll.
				time.Sleep(100 * time.Millisecond)
				continue
			}
			// fd closed or fatal error — exit goroutine.
			return
		}
		if n > 0 {
			// Parse inotify events to extract watch descriptors.
			offset := 0
			for offset < n {
				if offset+syscall.SizeofInotifyEvent > n {
					break
				}
				raw := (*syscall.InotifyEvent)(unsafe.Pointer(&buf[offset]))
				select {
				case ch <- inotifyEvent{wd: int(raw.Wd)}:
				default:
				}
				offset += syscall.SizeofInotifyEvent + int(raw.Len)
			}
		}
	}
}
