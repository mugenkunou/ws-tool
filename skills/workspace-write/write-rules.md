# Write rules

Where every file in ~/Workspace goes, how scripts handle paths and secrets, the formats of WISDOM.md, RUNBOOKS.md and scripts/README.md, and how to verify a write.

## Where a file goes

`ws layout` lists every workspace folder with its purpose, and where the homes (`$WS_TMP`, `$WS_CACHE`, `$WS_RUN`, `$WS_SCRATCH`) are on this machine. The rules below decide which one a file goes in.

For settings and state, ask in order and stop at the first yes:

1. Would leaking it hurt? A file with even one secret counts.
   - A secret a script uses: break it into single values in pass, under `svc/<system>-<instance>/<name>`. An entry with several values holds `KEY=value` lines.
   - A whole file an app reads as it is (yml, xml, env, properties): `configs/<system>-<instance>/`, or `symlinks/` when it lives at a system path.
2. Does the user edit it to change how something runs? `configs/<system>-<instance>/`. Facts only the user knows, such as a rollout order or DCs to skip, go here too.
3. Does a program write it, and would losing it lose work? `data/<topic>/`.
4. Can a command recreate it? It lives outside ~/Workspace: a cache in `$WS_CACHE/<system>-<instance>/`, temp files in `$WS_TMPD`, a session in `$WS_RUN/<system>-<instance>/`.

Everything else:

- Raw blobs (PDF, pptx, images, datasets, tarballs): `artifacts/`.
- Structured docs for agents (API specs, converted docs, `WISDOM.md`, `RUNBOOKS.md`): `references/`.
- Small composable blocks: `scripts/`, with shared code in `scripts/lib/`. Hand-written or multi-step scripts: `experiments/`.
- Code projects, git repos included: `experiments/<name>/`.
- Throwaway work and captures: `$WS_SCRATCH/<name>.<yyyy-mm>/`, created with `ws scratch new <name> --no-open` when missing (ws adds the `.<yyyy-mm>`).
- A file put away for good: `archive/`, at the same relative path it had.
- The user's own skills: `agent/skills/<name>/SKILL.md`. Built-in skills, such as this one, ship with ws and are read-only; `ws skill show <name> --path` prints any skill's folder.
- `notes/` is the user's own. Read it, and write elsewhere.

A system with one instance still gets an instance name, such as `zdcm-prod`, for its folders under `configs/`, `$WS_CACHE` and `$WS_RUN`.

Create no `.owner` files; ownership markers are on hold.

Put the topic in the filename, not in a new folder. If nothing fits, ask the user. There is no inbox and no catch-all folder.

A file that belongs at a system path (`~/.something`, `/etc/...`) comes in through `ws symlink add <path>`, which asks for consent on a terminal. Give the user the command to run.

## Paths in scripts

`ws env` prints the homes as this machine resolves them (`WS_ROOT`, `WS_TMP`, `WS_CACHE`, `WS_RUN`, `WS_SCRATCH`), and `scripts/lib/common.sh` takes its values from it ([script-contract.md](script-contract.md)). Use the variable names, never the paths they resolve to. A script gets:

- root: `$WS_ROOT`, the ~/Workspace tree
- temp: `$WS_TMPD`, a private dir made by `mktemp -d` under `$WS_TMP`, removed by an EXIT trap
- cache: `$WS_CACHE/<system>-<instance>/`
- session: `$WS_RUN/<system>-<instance>/`; files mode 0600
- config: `$WS_ROOT/configs/<system>-<instance>/`

A script writes only to these homes, so its own folder and the synced tree stay clean. Temp files go to `$WS_TMPD`, never `/tmp`, which every user can read and which is mounted noexec here.

Create a dir that holds secrets with `mkdir -m 700`, and write a file that holds secrets under `umask 077`.

Symlinks point into ~/Workspace from system paths. Create none inside it.

## Secrets

- Secrets live in pass, or in a whole file under `configs/` or `symlinks/` (see above). Scripts, references, WISDOM.md, RUNBOOKS.md and scripts/README.md hold none.
- Hand a secret to a command through a file in `$WS_TMPD`, so it stays out of `ps` and out of URLs: `curl -H @file`, `-F "name=<file"`, `--data-binary @file`.
- Mask the values of secret-looking keys whenever a script prints values. The system's lib holds the list of key names.
- When a write must resend a secret, such as a whole-file save, the script reads it from the system and sends it back itself, so the secret never passes through the agent.
- Renaming or moving a `svc/` pass entry includes updating every script that reads it, in the same change: `grep -rn '<path>' ~/Workspace/scripts/`.

## references/WISDOM.md

One file of **lessons**: what we learned the hard way, and time-savers such as "no venv for this, run it with y". It also holds the user's standing **decisions**, such as "restart = redeploy the build the DC runs", so a later run need not ask again. Date a decision, because decisions change. Quote the exact error text of a symptom, so a search finds it.

A surprise goes to the first home that can carry it:

1. A script can enforce or handle it: the script, and its header says so.
2. It is how an API behaves: the system's spec.
3. It is a value, or a fact only the user knows: `configs/<system>-<instance>/`.
4. It is how to run a script: the script's header comment.
5. It is how the task is done, an order of steps or a call between blocks: the task's entry in RUNBOOKS.md.
6. A future run would still go wrong or waste time without it: a bullet in WISDOM.md.

A decision that a script enforces lives in the script and its header only, with no WISDOM.md bullet.

If none fits, it goes nowhere. A run that taught nothing new leaves WISDOM.md as it is.

Format:

- One `## <system or tool>` section each (`## dns`, `## k8s`, `## python`), in alphabetical order.
- One bullet per lesson, with no prefix: the fact, then what to do. As short as it can be without losing information. No story. Add a date only when the fact can expire.
- A lesson that touches two systems goes under the one you act on, and its text names the other.
- Read the whole file before adding. Update a bullet that already covers the lesson, and fix or remove one that has gone stale.

## references/RUNBOOKS.md

One file of **routes**: how a task is done here. A route is not a script. It names the blocks and their order, and says what to decide on the way. `ws init` seeds the file with its purpose and an empty table; a run reads the table first and matches the user's goal to a row by meaning, not by wording.

The table has one line per task:

| Task | Words people use | System | Not this |
| --- | --- | --- | --- |
| Whitelist domains in the forward proxy | whitelist the domain, allow a domain in the proxy, open a domain for kites | sd | whitelisting an IP |

- **Task**: what is done, as a verb phrase in the system's terms. The entry below has the same heading.
- **Words people use**: phrases users said or would say, comma separated. Add a phrase when a run was asked in new words.
- **System**: the prefix of its scripts (`sd`), or `several`.
- **Not this**: near-misses that belong to another row, or nothing.

An entry is `## <Task>`, then these fields in this order, one to three lines each, and a field with nothing to say is left out:

- **Means**: the task in the system's terms, with its defaults and who decided them, dated ("no product named means KITES_APP, user 2026-10-30").
- **Input**: what the user gives, in which messy forms, and how to read it.
- **Route**: numbered steps, each naming the block that does it by its path (`scripts/sd-domain-whitelist.sh`), or "By hand:" where no block exists yet. A script that does the whole task is one step; its header holds the details.
- **Judgment calls**: what to decide, skip or report on the way, with the default.
- **Not covered**: what the route does not do or check.
- **Last run**: the date, the targets, and what went wrong or took longer than it should and is not fixed yet, such as a slow step or a block not yet run for real. Not the state of a target: a later run reads that live.

Rules:

- Link, do not copy. A lesson is a WISDOM.md bullet and a rule a script enforces is in its header; the entry names them.
- After a run that followed an entry, update it: new words, a step that was wrong or missing, **Last run**. After a good run that followed none, add a row and an entry. Neither needs a go. Restructuring an entry or removing its steps does.
- Every script an entry names exists. Renaming or removing a script includes fixing the entries in the same change: `grep -n 'scripts/<file>' ~/Workspace/references/RUNBOOKS.md`.
- When the file passes about 400 lines, move the entries to `references/runbooks/<task>.md`, keep the table here, and link each row to its file.

## scripts/README.md

The map of scripts for human developers.

- Top-level scripts only, grouped under one heading per system. `scripts/lib/` stays out.
- Per script: one line on what it does, then at least one example with its flags and real values filled in, ready to copy and run.
- Leave out internals such as cache paths or config keys. A script's header comment is its full usage.

Shape, shown for an example DNS system:

````markdown
## DNS: zone records

- `dns-login.sh`: save the DNS console session from a request copied as cURL in the browser.
  ```bash
  bash ~/Workspace/scripts/dns-login.sh            # reads the clipboard
  bash ~/Workspace/scripts/dns-login.sh --check    # is the saved session still good?
  ```
- `dns-record-set.sh`: set one record in some zones; previews unless `--apply`.
  ```bash
  bash ~/Workspace/scripts/dns-record-set.sh example.com,example.org www A 203.0.113.10 --apply
  ```
````

## Verify

- Search every file you wrote for each secret value you handled. Only pass, and whole secret files under `configs/` or `symlinks/`, may hold one.
- `ws secret ls` shows no new finding in the files you wrote.
- A file under `symlinks/`: `ws symlink ls` shows it as ok.
- After changing scripts or skills: `ws skill check` reports no drift.
- After changing `references/RUNBOOKS.md`, or renaming a script: every script it names exists. From ~/Workspace: `grep -o 'scripts/[A-Za-z0-9_./-]*\.\(sh\|py\)' references/RUNBOOKS.md | sort -u | while read -r f; do [ -e "$f" ] || echo "missing: $f"; done` prints nothing.
