# Built-in skills

Each folder here is a skill that ships inside the ws binary: `<name>/SKILL.md`
plus any sibling `.md` files it references. `ws skill` extracts them, read-only,
to `$XDG_DATA_HOME/ws/skills/<name>/` (default `~/.local/share/ws/skills`) and
links tools to that copy, so a new ws updates every tool.

While editing them, `ws skill install --dev` links the managed copy to this
checkout instead, so edits are live.
