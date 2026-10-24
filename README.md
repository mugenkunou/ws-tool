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
go install github.com/mugenkunou/ws-tool@latest      # Go ≥ 1.26
sudo mv "$(go env GOPATH)/bin/ws-tool" /usr/local/bin/ws

ws init                    # 🏗️  layout + ws.json + sync rules
ws symlink add             # 🔗 a checklist of the configs ws found; Enter takes the ticked ones
ws tui                     # 🖥️  look around
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
| 📼 **Sessions** | `ws session record` | "what was that rsync flag?" → `ws session grep rsync` |
| 📦 **Repos** | `ws repo ls`, `ws repo save` | the whole fleet at a glance; commit + pull + push in one go |
| 🧪 **Scratch** | `ws scratch new proxy-bug` | a named throwaway dir, opened in your editor |
| 🗑️ **Trash** | `ws trash enable` | `rm` becomes undoable, everywhere |
| ⏰ **Cron** | `ws cron add sync` | managed jobs with logs and missed-run catch-up |

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

Machine-local bits (cron state, trash hooks) stay **outside**, in `~/.local/state/ws-tool/`. Recorded sessions live in `~/Sessions` (`session.root_dir`), and scratch directories in `~/Scratch`. None of them sync.

---

## ☁️ Pick a sync client

ws doesn't sync anything itself. It prepares one folder so *any* folder-sync tool can carry it.
Point your client at `~/Workspace`, once, and you're done. Two setups below.

| | MEGA | Zoho WorkDrive |
| --- | --- | --- |
| Client | MEGAsync (GUI) or MEGAcmd | `rclone` (its `zoho` backend), or Zoho's own desktop sync app where one exists for your OS |
| Honours ws's ignore rules | ✅ reads `.megaignore` | ⚠️ not yet: ws writes only `.megaignore` |
| Scheduled by `ws cron` | ✅ `mega-sync` job | 🛠️ your own crontab line (below) |

### 🔴 MEGA

1. Install MEGAsync from [mega.io/desktop](https://mega.io/desktop) and sign in.
2. Add a sync pair: local `~/Workspace`, remote `/Workspace`.
3. Run `ws init`. It writes `.megaignore` into the folder, and MEGA reads it from the sync root.
4. Optional: `ws cron add mega-sync`. It restarts MEGAsync every 30 minutes for a short sync window, handy if you don't want it running all day.
5. After you change ignore rules (`ws ignore add`, `ws ignore edit`), ws rewrites `.megaignore` for you.

### 🟠 Zoho WorkDrive

1. Install [rclone](https://rclone.org/downloads/) and run `rclone config`.
2. New remote → type `zoho` → name it `workdrive` → pick your data-centre region → finish the browser sign-in.
3. First sync, once, by hand. Mind the direction: the first argument is the source of truth.

   ```bash
   rclone bisync ~/Workspace workdrive:Workspace --resync --dry-run   # look first
   rclone bisync ~/Workspace workdrive:Workspace --resync
   ```

4. Then schedule it, with your own crontab line:

   ```cron
   */30 * * * *  rclone bisync ~/Workspace workdrive:Workspace --resilient --exclude-from ~/Workspace/.rclone-ignore
   ```

5. `ws ignore lint` and `ws ignore check <path>` still tell you what *shouldn't* sync, but with WorkDrive nothing enforces it for you yet. Copy those rules into the exclude file above.

> 💡 Either way, the **same** ws commands work. Only step 1 of the restore changes.

---

## 🔁 New laptop? Five minutes.

```bash
# 1. install your sync client, sign in, let ~/Workspace download
# 2. install ws
ws init               # adds anything missing, keeps your values
ws symlink link       # every symlink, back where it was
ws trash enable       # undoable rm
ws credential enable  # git tokens from pass
ws cron apply         # your scheduled jobs
```

Your SSH config, shell setup and editor settings are back. Go make tea. 🍵

---

## 🖥️ The TUI

`ws tui`: one screen per noun, a dashboard of what needs attention. k9s-style keys:

| Key | Does |
| --- | --- |
| `enter` / `esc` | open / back |
| `:` | command bar, same words as the CLI (`:sym` jumps to Symlinks) |
| `/` | filter (`status=broken`) |
| `space` | mark rows. Actions hit the marks. |
| `a` `e` `ctrl+d` | add · edit · delete (always shows a checklist first; `e` only where a screen has it) |
| `?` / `q` | help / quit |

Nothing changes until you confirm a checklist.

---

## 🧭 Philosophy, in four lines

- 🏠 **One source of truth.** Originals live in the workspace; the rest are symlinks.
- 🤝 **Consent per action.** Every write shows its plan: `y` do it · `n` skip · `a` all · `q` stop. `--dry-run` previews, and there is no `--force`.
- ♻️ **Soft delete first.** Whatever ws deletes goes to the trash and can come back.
- 🔧 **Offload, don't reimplement.** `git`, `pass`, `crontab`, `script(1)` do the work; ws orchestrates.

The long version is in the handbook.

---

## 📚 Go deeper

[**HANDBOOK.md**](HANDBOOK.md) is the other file: principles, every command, the TUI, design decisions and the developer guide, all in one place.

License: [MIT](LICENSE)
