# The Workspace Factors

Inspired by the [Twelve-Factor App](https://12factor.net) methodology.

**Governing philosophy:** Flat hierarchy first. Every factor below exists to reduce capture friction, improve searchability, and keep recovery fast.

---

### Table of Contents <!-- markdownlint-disable-line MD001 -->

| # | Factor | Core idea |
| --- | --- | --- |
| I | [🏠 Single Source of Truth](#-i-single-source-of-truth) | One directory. Everything that matters. |
| II | [📥 Friction Kills Memory](#-ii-friction-kills-memory) | Capture fast. Search finds it later. |
| III | [🔍 Searchability Is Survival](#-iii-searchability-is-survival) | If you can't grep it, it doesn't exist. |
| IV | [🔗 Originals In, Symlinks Out](#-iv-originals-in-symlinks-out) | Real files in sync, pointers at system paths. |
| V | [🧱 Flat Hierarchy First](#-v-flat-hierarchy-first) | Flat hierarchy first. Separate only when truly needed. |
| VI | [🔀 Git and Sync Are Complementary](#-vi-git-and-sync-are-complementary) | MEGA moves files. Git moves history. |
| VII | [🧹 Sync Scripts, Quarantine State](#-vii-sync-scripts-quarantine-state) | Scripts are synced. Log dumps are not. |
| VIII | [📏 Sync With Intent](#-viii-sync-with-intent) | Some files don't deserve sync. |
| IX | [🗑️ Always Soft Delete](#️-ix-always-soft-delete) | Delete means recoverable move to Trash. |
| X | [🎯 Per-Action Consent](#-x-per-action-consent) | Confirm each mutation, not the batch. |

---

## 🏠 I. Single Source of Truth

*Everything that matters in one directory. Anyplace else, it's not going to be synced.*

Config files represent **accumulated state**: SSH proxy jump rules that took a week to figure out, Docker daemon settings tuned for a specific network, Kubernetes cluster configs with custom contexts. Small in size. Enormous in reconstruction cost. Every config gets synced. No exceptions for "I haven't used this in months." The cost of syncing a 2 KB file is zero. The cost of recreating it from memory is unbounded.

The `~/Workspace` avoids the "scattered files" problem: `~/.ssh/config` in one place, `~/.bashrc` in another, screenshots in `~/Pictures/`, scripts in `/tmp/`. All consolidated into one tree that syncs as a unit. Dotfiles are managed under `ws/dotfiles/` with symlinks pointing from system paths back in. It holds every config, script, note, artifact, and reference you care about.

The directory is structured in a way a fresh Linux machine can be restored to working state with minimal steps. It is designed for the day you lose everything.

---

## 📥 II. Friction Kills Memory

*If it's hard to write down, it won't get written down.*

The bottleneck is never retrieval — search tools handle that. The bottleneck is **capture**. The moment you hear a useful command in a Slack thread, see a URL to an internal tool, overhear a quote from an executive that explains a weird priority, or stumble on a config flag that finally makes something work — that's the moment it needs to be recorded. Not later. Not "when I have time to organize it." Now.

So optimize for **write speed**, not read speed:

- Don't agonize over which subdirectory. Drop it in the closest relevant folder.
- Don't invent a naming convention. `notes.md`, `stuff.md`, `2026-03.md` — all fine.
- Don't limit the format. A screenshot, a copy-pasted Slack message, a raw URL, a one-liner with no context — all valid. Something is always better than nothing.
- Don't create a subfolder for three files. Flat is fast.

The recording can be anything: a screenshot dropped into `Screenshots/`, a URL pasted into a daily note in `Notes/`, a `curl` command appended to a scratch script in `Experiments/`, a photo of a whiteboard. If the workspace makes you think "where should this go?" for more than 5 seconds, the workspace has failed.

---

## 🔍 III. Searchability Is Survival

*If you can't find it, you don't have it.*

A workspace with 500+ files across 6 directories is only useful if you can locate things. Three layers of search:

1. **Textual** — `grep -rn "rsync"` across scripts to find the exact command
2. **Contextual** — knowing that GPU setup lives in `Experiments/` and cert management in `Artifacts/`
3. **Semantic** — "where's the script that checks if nodes are reachable?" → `check-node-status.sh`

Search tools — `grep`, Obsidian search, IDE find-in-files, even `find -name` — are good enough to locate anything later. The directory structure, file naming, and this documentation all serve manual searchability. When in doubt, name things descriptively and put them where you'd look first.

⚠️ **Eyes open:** Grep only works on text. The 150+ PDFs, screenshots, and binary datasets are invisible to `grep -rn`. Descriptive directory names and this documentation are the only search index for non-text files.

---

## 🔗 IV. Originals In, Symlinks Out

*The real file lives in sync folder. Anywhere else gets a pointer.*

Config files that the OS expects at specific paths (`~/.ssh/`, `~/.bashrc`, `/etc/docker/daemon.json`) have their **originals** stored inside `~/Workspace/ws/dotfiles/`. The system paths are symlinks pointing back in. The `ws dotfile add` command handles this: it moves the original into `ws/dotfiles/` and replaces the system path with a symlink.

Never the reverse. Never store a symlink inside the sync directory. Why? If you keep the original at `~/.bashrc` and symlink it into sync, you're trusting a non-synced location as the source. One bad `apt upgrade` replaces the real file, and sync now points at a dangling link — or worse, a default-overwritten config.

```text
┌─────────── ORIGINALS (in ~/Workspace/ws/dotfiles/) ────────────────────────┐
│                                                                            │
│  ws/dotfiles/ssh/*         ws/dotfiles/bashrc      ws/dotfiles/daemon.json │
│  ws/dotfiles/kubeconfig    ws/dotfiles/vscode-settings.json                │
│  ws/dotfiles/megaignore                                                    │
│                                                                            │
└──────────────────────┬──────────────────────┬──────────────────────────────┘
                       │     SYMLINKS          │
                       ▼                       ▼
          ~/.ssh/ → ws/dotfiles/ssh   ~/.bashrc → ws/dotfiles/bashrc
          ~/.kube/config              /etc/docker/daemon.json
          ~/.config/Code/User/
            settings.json
```

⚠️ **Eyes open:** Any changes to the symlinks affects the original file in sync as well. Version control important configs to prevent accidental overwrites (enable `dotfile.git.enabled` in `ws/config.json`).

⚠️ **Hard rule:** When dotfile Git versioning pushes to a remote, the remote **must** be a private repository. This is enforced by `ws`, not left to user discipline. Dotfiles contain secrets by nature — SSH private keys, proxy-jump configs, Kubernetes cluster credentials, API tokens embedded in shell profiles. Pushing these to a public repo is an irreversible leak. `ws` verifies repository visibility on connect and before every push. There is no override.

---

## 🧱 V. Flat Hierarchy First

*Flat hierarchy first. Separate only when truly needed.*

A work laptop is also the primary laptop. Tech resources, personal notes, resumes, side projects — all need to persist effortlessly. Manually uploading to a personal cloud is friction that kills the habit.

The philosophy: **flat hierarchy first**. Work and personal files can live side by side in the same top-level folders. The priority is capture speed and retrieval speed — not rigid folder purity.

Use separation only when it gives real operational value (compliance export, handoff bundle, or access boundary). Otherwise, avoid deep nesting and keep paths short.

Decision rules:

- **Default to top-level placement.** If you are unsure, place it in the nearest top-level domain (`Artifacts/`, `Experiments/`, `Notes/`, `Data/`). Dotfiles are managed separately under `ws/dotfiles/`.
- **Delay taxonomy.** Create a new subfolder only after repeated pressure, not in anticipation.
- **Optimize for future search, not present neatness.** A slightly messy flat tree is better than a perfectly nested tree nobody can navigate quickly.
- **Separate only for real boundaries.** Compliance, access control, handoff packaging, or large-volume isolation are valid reasons; aesthetics alone is not.

```text
Artifacts/
├── dev.properties
├── env-template

Artifacts/
├── resume/
├── architecture/
├── infra-review.pptx
└── cert-chain.pem

Experiments/
├── blog/
├── fun-with-sockets/
├── monitor.sh
└── k8s-node-debug.sh
```

Ambiguous items (resume? interview prep? incident notes?) go where they're **most useful**, not where they're most "correct." Flat and findable beats perfectly classified.

---

## 🔀 VI. Git and Sync Are Complementary

*MEGA handles file sync. Git handles version history. Some repos deserve both.*

The default: git-backed repos live in `~/Repositories` (not synced). Synced files live in `~/Workspace` (not git-backed). But reality has exceptions:

- **Work repos** need sync because access to the remote can vanish overnight (layoff, credential revocation, VPN loss). Laptop lost too → code gone forever.
- **Personal knowledge repos** (`Notes/second-brain/`, `Data/bruno/`, `Experiments/blog/`) deserve redundant backup — git for versioning, MEGA for instant cloud availability even if GitHub is down.

For these, both coexist: `.git/` is excluded from sync (via `-:.*` in `.megaignore`), so MEGA syncs the working tree while git tracks history independently.

```text
MEGA syncs files  ──── fast, automatic, encrypted
Git tracks history ──── intentional, versioned, distributed

Some repos get both. .git/ is always excluded from MEGA.
```

---

## 🧹 VII. Sync Scripts, Quarantine State

*Ad-hoc scripts deserve sync. Ad-hoc state doesn't.*

Debugging scripts, one-off commands that took 30 minutes to get right, investigation findings — these are **knowledge**. They get synced. They contain the hard-won flags, the exact `rsync` invocation, the `jq` pipeline that finally parsed the broken JSON.

The **state** those scripts produce — 200 MB log dumps, container images, downloaded binaries, intermediate CSV files — that's transient. It goes to `~/Scratch/` (not synced) or gets excluded by `.megaignore`.

The investigation model pairs these: a synced directory for the script and a scratch directory for its output. `ws scratch new` creates the scratch directory and opens it in VS Code. When you find scripts or notes worth keeping, you manually copy them into `<workspace>`.

```text
# Quick start — creates a scratch directory for the investigation
ws scratch new proxy-timeout
# ✔ Created   ~/Scratch/proxy-timeout.2026-03/
# ✔ Opening   VS Code → ~/Scratch/proxy-timeout.2026-03/
#
# When you find scripts/notes worth keeping:
#   cp ~/Scratch/proxy-timeout.2026-03/debug.sh ~/Workspace/Experiments/

┌────────────────────────────────────┐
│          THE SPLIT                 │
│                                    │
│  ~/Workspace/...   ← SYNCED        │
│    scripts, notes, findings        │
│    small configs, analysis         │
│                                    │
│  ~/Scratch/...     ← NOT SYNCED    │
│    log dumps, binaries, blobs      │
│    anything re-downloadable        │
└────────────────────────────────────┘
```

---

## 📏 VIII. Sync With Intent

*Keep sync lean. Large blobs get quarantined. Deep nesting gets avoided.*

Don't sync large artifacts, especially the reproducible ones. If you must sync a large blob — a custom-compiled binary `.tar.gz`, an internal VPN client `.deb`, a critical dataset `.csv` — do it consciously.

Deeply nested directories like `.git`, `.venv/`, `__pycache` will hurt megasync. Exclude the directories which causes scan overhead.

The `.megaignore` acts as a sync firewall:

```bash
# OS junk
-:Thumbs.db
-:desktop.ini
-:~*
-g:*~                    # Editor backup files (vim file~, emacs file~)
-:.*                    # All hidden files/dirs (.git, .obsidian, .venv, etc.)

# Build artifact directories
-:node_modules
-:__pycache__
-:.venv
-:venv
-:.jekyll-cache
-:_site
-:*.egg-info             # Python setuptools metadata

# Compiled output (always reproducible)
-g:*.class
-g:*.pyc
-g:*.pyo
-g:*.pyd
-g:*.o
-g:*.obj
-g:*.so
-g:*.a
-g:*.dll
-g:*.dylib
-g:*.exe

# Logs and crash dumps (ephemeral)
-g:*.log
-g:hs_err_pid*           # JVM crash dumps

# Lock files (reproducible from manifests)
-g:Gemfile.lock
-g:package-lock.json
-g:yarn.lock
-g:poetry.lock
-g:pnpm-lock.yaml
-g:composer.lock
-g:go.sum

# Packages and archives (never sync)
-g:*.tar
-g:*.tar.gz
-g:*.tgz
-g:*.deb
-g:*.rpm
-g:*.run
-g:*.zip
-g:*.rar
-g:*.7z
-g:*.jar
-g:*.war
-g:*.ear
-g:*.whl
-g:*.egg
-g:*.gem

# Large datasets (never sync)
-g:*.csv
-g:*.orc
-g:*.parquet

# Safe harbors (override ALL excludes above — last match wins)
+:ws                    # ws metadata directory (dotfiles, config, manifest)
+g:ws/**                # ws metadata contents (dotfiles, logs, config, index)

# Sync everything else
-s:*
```

---

## 🗑️ IX. Always Soft Delete

*Delete should mean reversible by default.*

Accidental deletion is a high-cost failure mode. Recovery should be possible without backups, forensic tools, or panic. The default deletion path across tools must be **soft delete** (move to Trash), not hard delete.

Policy:

- Terminal deletion via `rm` must route to Trash.
- VS Code delete action must use system Trash.
- File explorer delete action must go to Trash (avoid permanent-delete shortcuts).

Operational stance:

- Permanent deletion is an explicit, deliberate act.
- Routine cleanup is always recoverable.
- If a tool has both delete and trash modes, trash is the default.

---

## 🎯 X. Per-Action Consent

*Confirm each mutation, not the batch.*

A single "Proceed? Y/n" gate before N operations is a rubber-stamp. The user sees a summary, hits Enter out of habit, and regrets it when one of the N items was wrong. The `ws` CLI adopts the `git add -p` model: every discrete mutation gets its own prompt.

The pattern is **Plan → Present → Confirm-Each → Execute**:

1. The command builds a **Plan** — an ordered list of **Actions**, each with an ID, a human-readable description, and an execute closure.
2. In interactive mode, each action is presented individually with `y/n/a/q`:
   - `y` (default, Enter) — execute this action
   - `n` — skip this action, continue to next
   - `a` — accept all remaining without further prompts
   - `q` — quit, skip everything remaining
3. In `--dry-run` mode, the plan is printed without execution.
4. In `--quiet` or `--json` mode, all actions are auto-accepted (non-interactive).

Why per-action, not per-batch:

- **Precision.** `ws repo pull` across 12 repos — you can skip the one with uncommitted changes.
- **Auditability.** Each action's outcome (executed, skipped, failed) is tracked in `PlanResult` and available in JSON output.
- **Partial success.** If action 3 of 5 fails, actions 1–2 are already done and actions 4–5 still get offered. No all-or-nothing rollback needed.
- **Muscle memory.** The `y/n/a/q` vocabulary is identical to `git add -p`, reducing cognitive load.

Granularity rule: **one Action per independently meaningful mutation**. For `ws init`, that's one action per file created. For `ws repo pull`, one per repository. For `ws log prune`, one per session removed. The test is: "would a user ever want to say yes to this but no to the next?" If so, they're separate actions.
