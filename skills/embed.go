// Package skills holds the built-in skills: one folder per skill, each with a
// SKILL.md and its sibling files. They ship inside the ws binary; `ws skill`
// extracts them to $XDG_DATA_HOME/ws/skills and links tools to that copy.
package skills

import "embed"

// FS is this directory. The pattern takes every entry, so the build works
// before any skill folder exists; entries that are not a folder with a
// SKILL.md (this file, README.md) are skipped by internal/skill.
//
//go:embed *
var FS embed.FS
