package tui

import (
	"time"

	"github.com/mugenkunou/ws-tool/internal/config"
	"github.com/mugenkunou/ws-tool/internal/dotfile"
	"github.com/mugenkunou/ws-tool/internal/ignore"
	wslog "github.com/mugenkunou/ws-tool/internal/log"
	"github.com/mugenkunou/ws-tool/internal/manifest"
	"github.com/mugenkunou/ws-tool/internal/notify"
	"github.com/mugenkunou/ws-tool/internal/secret"
	"github.com/mugenkunou/ws-tool/internal/trash"
)

// DashboardData holds all data needed to render the TUI dashboard.
type DashboardData struct {
	Workspace string
	LoadedAt  time.Time

	// Health panel
	IgnoreCritical  int
	IgnoreWarning   int
	SecretCritical  int
	SecretWarning   int
	DotfileCritical int
	DotfileWarning  int

	// Dotfiles panel
	DotfileIssues []dotfile.Issue

	// Violations panel
	Violations []notify.HealthViolation

	// Log panel
	Sessions  []wslog.Session
	LogActive bool
	ActiveTag string

	// Storage panel (placeholder counts)
	TrashConfigured bool

	// Notify daemon
	NotifyActive bool
}

// LoadDashboard runs all subsystem scans and populates DashboardData.
func LoadDashboard(workspacePath, configPath, manifestPath string, cfg config.Config) DashboardData {
	d := DashboardData{
		Workspace: workspacePath,
		LoadedAt:  time.Now(),
	}

	// Dotfile scan
	dotIssues, _ := dotfile.Scan(dotfile.ScanOptions{
		WorkspacePath: workspacePath,
		ManifestPath:  manifestPath,
	})
	d.DotfileIssues = dotIssues
	for _, iss := range dotIssues {
		if iss.Status == dotfile.StatusBroken {
			d.DotfileCritical++
		} else {
			d.DotfileWarning++
		}
	}

	// Ignore scan
	userRules, _ := ignore.LoadUserRules(ignore.UserRulesPath(workspacePath))
	engine := ignore.BuildEngine(userRules)
	ignoreViolations, _ := ignore.Scan(ignore.ScanOptions{
		WorkspacePath: workspacePath,
		WarnSizeMB:    cfg.Ignore.WarnSizeMB,
		CritSizeMB:    cfg.Ignore.CritSizeMB,
		MaxDepth:      cfg.Ignore.MaxDepth,
		Engine:        engine,
	})
	for _, v := range ignoreViolations {
		if v.Severity == "CRITICAL" {
			d.IgnoreCritical++
		} else {
			d.IgnoreWarning++
		}
		d.Violations = append(d.Violations, notify.HealthViolation{
			Group:    v.Group,
			Type:     v.Type,
			Severity: v.Severity,
			Path:     v.Path,
			Message:  v.Message,
			SizeMB:   int(v.SizeBytes / (1024 * 1024)),
		})
	}

	// Secret scan
	if cfg.Secret.Enabled {
		m, err := manifest.Load(manifestPath)
		if err == nil {
			allow := make(map[string]struct{}, len(m.Secret.Allowlist))
			for _, a := range m.Secret.Allowlist {
				allow[a] = struct{}{}
			}
			secretViolations, _ := secret.Scan(secret.ScanOptions{
				WorkspacePath: workspacePath,
				Engine:        engine,
				Allowlist:     allow,
			})
			for _, v := range secretViolations {
				if v.Severity == "CRITICAL" {
					d.SecretCritical++
				} else {
					d.SecretWarning++
				}
				d.Violations = append(d.Violations, notify.HealthViolation{
					Group:    v.Group,
					Type:     v.Type,
					Severity: v.Severity,
					Path:     v.Path,
					Message:  v.Message,
				})
			}
		}
	}

	// Log sessions
	sessions, _ := wslog.List(workspacePath)
	d.Sessions = sessions
	for _, s := range sessions {
		if s.Active {
			d.LogActive = true
			d.ActiveTag = s.Tag
			break
		}
	}

	// Trash status
	ts, err := trash.GetStatus("")
	if err == nil {
		d.TrashConfigured = ts.ShellRMConfigured
	}

	// Notify state
	ns, _ := notify.Status(workspacePath)
	d.NotifyActive = ns.Active

	return d
}

// LoadDashboardFromHealth populates DashboardData from an existing health.json
// for a fast-path render (no scans). Falls back to LoadDashboard if health.json
// is missing.
func LoadDashboardFromHealth(workspacePath, configPath, manifestPath string, cfg config.Config) DashboardData {
	h, err := notify.ReadHealth(workspacePath)
	if err != nil || h.Timestamp.IsZero() {
		return LoadDashboard(workspacePath, configPath, manifestPath, cfg)
	}

	d := DashboardData{
		Workspace:       workspacePath,
		LoadedAt:        h.Timestamp,
		IgnoreCritical:  h.Summary.Ignore.Critical,
		IgnoreWarning:   h.Summary.Ignore.Warning,
		SecretCritical:  h.Summary.Secret.Critical,
		SecretWarning:   h.Summary.Secret.Warning,
		DotfileCritical: h.Summary.Dotfile.Critical,
		DotfileWarning:  h.Summary.Dotfile.Warning,
		Violations:      h.Violations,
	}

	// Still need live dotfile list and log sessions (not in health.json)
	d.DotfileIssues, _ = dotfile.Scan(dotfile.ScanOptions{
		WorkspacePath: workspacePath,
		ManifestPath:  manifestPath,
	})
	sessions, _ := wslog.List(workspacePath)
	d.Sessions = sessions
	for _, s := range sessions {
		if s.Active {
			d.LogActive = true
			d.ActiveTag = s.Tag
			break
		}
	}

	ts, _ := trash.GetStatus("")
	d.TrashConfigured = ts.ShellRMConfigured

	ns, _ := notify.Status(workspacePath)
	d.NotifyActive = ns.Active

	return d
}
