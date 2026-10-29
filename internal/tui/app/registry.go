package app

import (
	"charm.land/bubbles/v2/help"

	"github.com/mugenkunou/ws-tool/internal/tui/env"
	"github.com/mugenkunou/ws-tool/internal/tui/keys"
	"github.com/mugenkunou/ws-tool/internal/tui/nav"
	"github.com/mugenkunou/ws-tool/internal/tui/screens/cron"
	"github.com/mugenkunou/ws-tool/internal/tui/screens/dashboard"
	"github.com/mugenkunou/ws-tool/internal/tui/screens/ignore"
	"github.com/mugenkunou/ws-tool/internal/tui/screens/repos"
	"github.com/mugenkunou/ws-tool/internal/tui/screens/scratch"
	"github.com/mugenkunou/ws-tool/internal/tui/screens/secrets"
	"github.com/mugenkunou/ws-tool/internal/tui/screens/sessions"
	"github.com/mugenkunou/ws-tool/internal/tui/screens/skills"
	"github.com/mugenkunou/ws-tool/internal/tui/screens/symlinks"
	"github.com/mugenkunou/ws-tool/internal/tui/screens/sync"
	"github.com/mugenkunou/ws-tool/internal/tui/screens/trash"
	"github.com/mugenkunou/ws-tool/internal/tui/screens/workspace"
)

// screenEntry is how the app builds one screen.
type screenEntry struct {
	New func(env.Env) nav.Component
	// Keys is a list screen's own key map (without the shared list keys), which
	// the key tests check against the shared ones. Nil for a screen that is not
	// a reslist; TestRegistryCoversEveryScreen fails when a reslist screen (one
	// with Declared) has none.
	Keys func() help.KeyMap
}

// registry is every screen of nav.Screens(). TestRegistryCoversEveryScreen
// fails when a screen has no entry, so a new nav.Screen cannot be left without
// a component.
var registry = map[nav.Screen]screenEntry{
	nav.ScreenDashboard: {New: component(dashboard.New)},
	nav.ScreenRepos:     {New: component(repos.New), Keys: keyMap(keys.Repos)},
	nav.ScreenSymlinks:  {New: component(symlinks.New), Keys: keyMap(keys.Symlinks)},
	nav.ScreenScratch:   {New: component(scratch.New), Keys: keyMap(keys.Scratch)},
	nav.ScreenSessions:  {New: component(sessions.New), Keys: keyMap(keys.Sessions)},
	nav.ScreenIgnore:    {New: component(ignore.New), Keys: keyMap(keys.Ignore)},
	nav.ScreenSync:      {New: component(sync.New)},
	nav.ScreenSecrets:   {New: component(secrets.New), Keys: keyMap(keys.Secrets)},
	nav.ScreenCron:      {New: component(cron.New), Keys: keyMap(keys.Cron)},
	nav.ScreenTrash:     {New: component(trash.New), Keys: keyMap(keys.Trash)},
	nav.ScreenSkills:    {New: component(skills.New), Keys: keyMap(keys.Skills)},
	nav.ScreenWorkspace: {New: component(workspace.New), Keys: keyMap(keys.Workspace)},
}

func component[T nav.Component](f func(env.Env) T) func(env.Env) nav.Component {
	return func(e env.Env) nav.Component { return f(e) }
}

func keyMap[T help.KeyMap](f func() T) func() help.KeyMap {
	return func() help.KeyMap { return f() }
}
