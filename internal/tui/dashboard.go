package tui

import (
	"time"

	"github.com/mugenkunou/ws-tool/internal/config"
	"github.com/mugenkunou/ws-tool/internal/dotfile"
	"github.com/mugenkunou/ws-tool/internal/ignore"
	wslog "github.com/mugenkunou/ws-tool/internal/log"
	"github.com/mugenkunou/ws-tool/internal/manifest"
	"github.com/mugenkunou/ws-tool/internal/secret"
	"github.com/mugenkunou/ws-tool/internal/trash"
)

// Violation is a single health violation entry surfaced in the TUI.
type Violation struct {
	Group    string
	Type     string
	Severity string
	Path     string
	Message  string
	SizeMB   int
}

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
	Violations []Violation

	// Log panel
	Sessions  []wslog.Session
	LogActive bool
	ActiveTag string

	// Storage panel (placeholder counts)
	TrashConfigured bool
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
		d.Violations = append(d.Violations, Violation{
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
				d.Violations = append(d.Violations, Violation{
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

	return d
}

// LoadDashboardFromHealth is a compatibility alias for LoadDashboard.
// Previously used a notify daemon health.json fast-path; now always runs live scans.
func LoadDashboardFromHealth(workspacePath, configPath, manifestPath string, cfg config.Config) DashboardData {
	return LoadDashboard(workspacePath, configPath, manifestPath, cfg)
}
