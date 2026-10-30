# 🧰 ws

**One folder. One binary. Your whole machine, rebuildable.**

`ws` keeps `~/Workspace` tidy, safe and synced, and brings a fresh laptop back to life in minutes.
CLI for scripts, full-screen TUI (`ws tui`) for humans.

```text
 your configs ──┐
 your repos ────┤
 your secrets ──┼──►  ~/Workspace  ──►  ☁️ any sync client  ──►  🖥️ new machine: ws init, ws symlink link, done
 your scratch ──┘
```

---

## ⚡ 60-second start

```bash
mkdir -p ~/.local/bin
curl -fL -o ws.download "https://github.com/mugenkunou/ws-tool/releases/latest/download/ws-$(uname -s | tr A-Z a-z)-$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')"
chmod +x ws.download
mv ws.download ~/.local/bin/ws

ws init                    # 🏗️  layout + ws.json + sync rules; offers the built-in skills
ws symlink add ~/.gitconfig  # 🔗 move a file into the workspace, link it back
ws tui                     # 🖥️  look around
ws version                 # ⬆️  your versions, and whether a newer ws or agent-tmux is out (--check looks now)
```

That's it. `~/.gitconfig` (and the rest you ticked) now lives in `~/Workspace/symlinks/home/`, at the same path it had on your machine, and syncs with everything else. To add one yourself: `ws symlink add ~/.bashrc`.

---

## 🎒 What you get

| | | |
| --- | --- | --- |
| 🔗 **Symlinks** | `ws symlink add` | ws finds your configs; originals in the workspace, symlinks out |
| 🧹 **Sync hygiene** | `ws ignore lint` | find the 200 MB log dump *before* it syncs |
| 🔐 **Secrets** | `ws secret ls` | catches `password=hunter2`, moves it to `pass` |
| 🪪 **Git tokens** | `ws credential enable` | git reads credentials from `pass` |
| 📼 **Sessions** | `ws session create web` | recorded tmux sessions, run by [agent-tmux](https://github.com/mugenkunou/agent-tmux); "what was that rsync flag?" → `ws session grep rsync` |
| 📦 **Repos** | `ws repo ls`, `ws repo git pull` | the whole fleet at a glance, hygiene warnings, any git command across every repo |
| 🧪 **Scratch** | `ws scratch new proxy-bug` | a named throwaway dir, opened in your editor |
| 🗑️ **Trash** | `ws trash enable` | `rm` becomes undoable, everywhere |
| 🧩 **Skills** | `ws skill install` | built-in skills that update with ws, plus yours in `agent/skills/` (and the skill `agent-tmux` ships, via `--providers`), linked into Claude Code, Codex, Cursor and more; `ws skill check` catches drift |
| ⏰ **Cron** | `ws cron add sync-push` | managed jobs with logs and missed-run catch-up |

Everything is `ws <noun> <verb>`. `ls` lists, `add` adopts, `rm` deletes, `prune` cleans.
Stuck? `ws help`, or `ws <noun> --help`.

---

## 🗺️ The workspace

```text
~/Workspace/
├── ws.json            ⚙️  ws's own state (travels with the folder)
├── .megaignore        🚫 generated sync rules (MEGA only, see below)
├── symlinks/          🔗 originals, at the same path they have on your machine
│   ├── home/            ~/.gitconfig → symlinks/home/.gitconfig
│   └── system/          /etc/hosts → symlinks/system/etc/hosts
└── notes/ experiments/ artifacts/ references/ archive/ scripts/ agent/ configs/ data/
```

`ws layout` prints every folder with what belongs in it, and `ws env` prints the paths scripts use (`eval "$(ws env)"`).

Machine-local bits (cron state, trash hooks) stay **outside**, in `~/.local/state/ws-tool/`. Scratch directories live in `~/Scratch`. None of them sync.

---

## ☁️ Pick a sync client

ws doesn't sync anything itself. It prepares one folder so a folder-sync tool can carry it.

### 🧠 How it works, in one breath

`~/Workspace` on the machine you work on is **the truth**. Sync goes **one way**: laptop → cloud. The cloud copy is a backup you never edit, and the only way back is a deliberate restore onto a fresh machine ([Factor XII](HANDBOOK.md#xii-sync-is-one-way)).

```text
MEGA       ~/Workspace ──(MEGAsync reads .megaignore)──────────────────────────────►  ☁️ MEGA:/Workspace

WorkDrive  ~/Workspace ──ws sync push──► ~/ZohoWorkDrive/My Folders/Workspace ──TrueSync──►  ☁️ WorkDrive
           (the truth)   rsync, ws's      (the mirror, on TrueSync's drive:   (uploads
                         ignore rules      ws owns it, you never touch it)      by itself)
```

- **MEGA** can pair any local folder with any remote one, so it syncs `~/Workspace` directly and honours the generated `.megaignore`.
- **WorkDrive** can't. Zoho's TrueSync app mounts your *whole* account as a virtual drive at one fixed place (`~/ZohoWorkDrive`) and asks you for no folder, and it doesn't read ws's ignore rules. So ws copies the workspace into a mirror folder on that drive, using its own ignore rules, and lets the app upload the mirror.

| | 🔴 MEGA | 🟠 Zoho WorkDrive |
| --- | --- | --- |
| Client | MEGAsync (GUI) or MEGAcmd | Zoho WorkDrive TrueSync (the old Desktop Sync app is discontinued) |
| What it syncs | `~/Workspace` itself | the mirror folder that `ws sync push` fills |
| Honours ws's ignore rules | ✅ reads `.megaignore` | ✅ ws filters while copying |
| Scheduled by `ws cron` | ✅ `mega-sync` | ✅ `sync-push` |
| Symlinks | ❌ skipped (`-s:*`) | ❌ skipped by ws |
| Direction | two-way by nature: one-way is *your* discipline | one-way by construction |

### 🔴 MEGA

1. Install MEGAsync from [mega.io/desktop](https://mega.io/desktop) and sign in.
2. Add a sync pair: local `~/Workspace`, remote `/Workspace`.
3. Run `ws init`. It writes `.megaignore` into the folder, and MEGA reads it from the sync root.
4. Optional: `ws cron add mega-sync`. It restarts MEGAsync every 30 minutes for a short sync window, handy if you don't want it running all day.
5. After you change ignore rules (`ws ignore add`, `ws ignore edit`), ws rewrites `.megaignore` for you.

### 🟠 Zoho WorkDrive

1. Install Zoho WorkDrive TrueSync and sign in. It mounts your account at `~/ZohoWorkDrive` (a virtual drive; it doesn't ask where). Let it start at login.
2. Tell ws where the mirror lives. There's no default, so ws refuses to push until you set it:

   ```jsonc
   // ws.json
   "sync": {
     "mirror":  { "target": "~/ZohoWorkDrive/My Folders/Workspace" },
     "exclude": [],          // optional: extra paths to keep out of the mirror only
     "max_delete_pct": 20    // optional: ask before a push deletes more than this % of the mirror
   }
   ```

3. Push, look first, then for real:

   ```bash
   ws sync push -n    # what would change, and how many deletions
   ws sync push       # copy (needs rsync); the app uploads the result
   ws sync status     # last push, changes and deletions waiting
   ```

4. Schedule it: `ws cron add sync-push` (every 30 minutes). TrueSync runs all the time, so there is no restart job like MEGA's.
5. Run `ws sync push` twice. The second must copy nothing; if it re-copies files, see the TrueSync gotcha below.

Coming from the old Desktop Sync app? Change `sync.mirror.target` from `~/Zoho WorkDrive/…` to `~/ZohoWorkDrive/…`, and remove a leftover `zoho-sync` cron block with `ws cron rm zoho-sync` (the job is retired).

<details>
<summary>🩹 Without the app: rclone, one way</summary>

rclone's `zoho` backend talks to WorkDrive directly, no app needed. Use **`rclone sync`** (one way), **not `rclone bisync`**: bisync is two-way, which breaks the [one-way principle](HANDBOOK.md#xii-sync-is-one-way), and the backend has no modtimes or hashes, so it can't detect conflicts reliably.

1. `rclone config` → new remote → type `zoho` → name `workdrive` → your data-centre region → browser sign-in. (Your office may block third-party OAuth apps.)
2. Copy ws's rules by hand into an rclone filter file (`ws ignore ls` shows what doesn't sync), then:

   ```bash
   rclone sync ~/Workspace workdrive:Workspace --filter-from ~/.config/ws-rclone.filter --dry-run   # look first
   rclone sync ~/Workspace workdrive:Workspace --filter-from ~/.config/ws-rclone.filter
   ```

3. Schedule it with your own crontab line: `*/30 * * * *  rclone sync … (same as above)`.

</details>

### ⚠️ Gotchas

- 🔗 **Symlinks never sync**, on either client. `ws ignore lint` warns about any symlink in a synced path: make it a real file or folder, or `ws ignore add` it.
- 🧾 **WorkDrive's own exclusions work by file extension only** (`mp3, png`): no folders, no globs. That's why ws filters before the copy.
- 🕓 **TrueSync's drive doesn't keep file times.** It stamps each copy with its upload time, so rsync's "same size and time" check never matches. ws push uses `rsync --update`, which skips a file whose mirror copy is newer. The catch: swapping in an *older* version of a file (`cp -p`) won't push until you `touch` it.
- 🗑️ **The old Desktop Sync app asked before every local delete.** Not checked for TrueSync: if a dialog appears when a push deletes something, turn it off in the app.
- 👀 **An office WorkDrive is readable by its admins**, and the mirror includes `symlinks/` (`.ssh`, `.kube`, `.docker`, …). MEGA is end-to-end encrypted; WorkDrive is not. Know what you're shipping.
- 🔐 **Executable bits don't come back** from a WorkDrive restore (`ws sync restore` reminds you). `chmod +x` your scripts by hand.
- 🔄 **`ws cron add mega-sync` kills and restarts MEGAsync** every 30 minutes. Expect the tray icon to blink.
- 🕰️ **rclone's Zoho backend has no modtimes or hashes**, so it spots changes by size only.

### 🎭 What if…?

Real accidents, real outcomes. Everything here follows from one rule: the truth only flows *out*.

**🅰️ …I wipe the mirror folder on my laptop?**
```text
~/Workspace ✅   mirror 💥 → app deletes in cloud → ☁️ empty (files in WorkDrive trash)
```
Your workspace doesn't notice. The next `ws sync push` finds its stamp missing, warns *"mirror was wiped, re-uploading N files"*, and refills everything. Cloud gap: until that push (≤ 30 min with cron).

**🅱️ …someone empties WorkDrive on the web?**
- **B1, the app pulls the deletes down** (usual sync-app behaviour): the mirror empties, and it plays out exactly like 🅰️.
- **B2, the app doesn't pull them down** 🕳️: the mirror still looks full, rsync sees nothing to do, and the cloud stays empty, silently. ws only sees local folders, so it can't tell. Fix: empty or rename the mirror and push, so everything uploads fresh.

Zoho only documents local → cloud deletes. Test yours once: delete a test file on the web and see if it vanishes locally.

**🅲 …I wipe `~/Workspace` itself?** The one that actually hurts.
- `ws.json` gone → push refuses. The cloud copy survives. 🛡️
- More than `max_delete_pct` (20%) of the mirror would disappear → push asks you first; cron refuses. 🛡️
- A handful of files deleted → that *is* propagated. Fish them out of the WorkDrive trash (or MEGA's Rubbish Bin).

**✏️ …someone edits a file in the mirror or on the web?** The next push overwrites it with the workspace version. By design: the mirror is a copy, not a place to work.

**💻💻 …a second laptop pushes to the same mirror?** The stamp names the push it came from, so the other machine's push sees a foreign stamp: it asks you, and cron refuses. One truth, one pusher.

**😴 …the WorkDrive app isn't running, or got logged out?** If TrueSync isn't running, its drive isn't mounted: the mirror's parent folder is gone and push refuses (see below). If it runs but is logged out, ws can't tell: it fills the mirror and nothing reaches the cloud. Glance at the app's tray icon now and then.

**📦 …the mirror's parent folder is missing (TrueSync stopped, app reinstalled, account relinked)?** Push refuses instead of creating a stray folder. Start the app, or point `sync.mirror.target` at the new place.

**🙈 …I ignore something that already went up?** The next push deletes it from the mirror (`--delete-excluded`), and the app removes it from the cloud. With MEGA, the newly ignored file just stops syncing; delete the cloud copy yourself.

**🔴 …I'm on MEGA and edit on two machines?** MEGA happily syncs both ways, so the one-way rule is your job: work on one machine at a time, and let the other one catch up before you touch it.

> 💡 Either way, the **same** ws commands work. Only step 1 of the restore changes.

---

## 🔁 New laptop? Five minutes.

```bash
# 1. install your sync client, sign in, let it download
#    (MEGA: ~/Workspace arrives directly; WorkDrive: the mirror arrives)
# 2. install ws
ws sync restore ~/ZohoWorkDrive/My\ Folders/Workspace   # WorkDrive only: mirror → ~/Workspace
ws init               # adds anything missing, keeps your values
ws symlink link       # every symlink, back where it was
ws trash enable       # undoable rm
ws credential enable  # git tokens from pass
ws cron apply         # your scheduled jobs
```

Your SSH config, shell setup and editor settings are back. Go make tea. 🍵

---

## 🖥️ The TUI

`ws tui`: one screen per noun, a dashboard of what needs attention, and a Settings tab for
ws itself (`ws.json` to view or edit, versions and update check, shell completion). k9s-style keys:

| Key | Does |
| --- | --- |
| `enter` / `esc` | open / back |
| `:` | command bar: `:sym` jumps to Symlinks, `:help`, `:q` |
| `.` | action menu: the screen's verbs that have no key there |
| `/` | filter (`status=broken`) |
| `space` | mark rows. Actions hit the marks. |
| `a` `e` `ctrl+d` | add · edit · delete (always shows a checklist first; `e` only where a screen has it) |
| `?` / `q` | help / quit |

Nothing changes until you confirm a checklist.

---

## 🧭 Philosophy, in five lines

- 🏠 **One source of truth.** Originals live in the workspace; the rest are symlinks.
- 🤝 **Consent per action.** Every write shows its plan: `y` do it · `n` skip · `a` all · `q` stop. `--dry-run` previews, and there is no `--force`.
- ♻️ **Soft delete first.** Whatever ws deletes goes to the trash and can come back.
- 🔧 **Offload, don't reimplement.** `git`, `pass`, `crontab`, agent-tmux do the work; ws orchestrates.
- ➡️ **Sync is one-way.** Your laptop's `~/Workspace` is the truth; the cloud copy is a mirror you never edit. The only way back is a deliberate restore onto a fresh machine.

The long version is in the handbook.

---

## 📚 Go deeper

[**HANDBOOK.md**](HANDBOOK.md) is the other file: principles, every command, the TUI, design decisions and the developer guide, all in one place.

License: [MIT](LICENSE)
