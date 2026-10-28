---
name: workspace-run
description: Do a task on real systems with the blocks in ~/Workspace (scripts, references, configs, WISDOM.md), whether it is to investigate, debug, build, deploy or a chore. Use when the user gives a goal in plain English that touches a system ~/Workspace knows about, such as a DC, a host, a registry, SD, ZDCM or Stratus.
---

# Workspace run

You are an engineer who knows where everything in ~/Workspace is kept. The user gives you a goal. You do the work and most of the thinking yourself. You ask once, up front, so the run does not stall later waiting for the user. Every answer you get is written down, so the next run asks less.

`workspace-write` keeps the blocks in order. This skill uses them. Every file you create or change in ~/Workspace goes through workspace-write's rules (`$(ws skill show workspace-write --path)/write-rules.md`).

## 1. Recon

Quick, read-only and thorough. The goal is to find every question the run will raise before you start it.

The layout is the index; `ws layout` lists the folders and the homes. For the systems the goal names, read:

- `references/WISDOM.md`, the sections for those systems and their neighbours: lessons, and decisions the user already made
- the header comments of `scripts/<system>-*` and `scripts/lib/<system>.*`: what you can do, and what usually comes next
- `references/<system>*.openapi.yaml`: how the system's API behaves
- `configs/<system>-*/`: where things are, and facts only the user knows
- `references/zoho-glossary.md` for abbreviations, `experiments/` for bigger scripts, `notes/` (read only; skip `notes/.obsidian/`)

Listing pass entry names (`pass ls svc/`) is fine; reading their values is not.

Find the system from the filename prefixes. Grep WISDOM.md for the exact error text when the goal is a symptom.

Then, if a saved session still works (`<system>-login.sh --check`), run the read-only lookups the task needs: targets, ids, current state.

Sketch the run in your head, step by step, and check each step:

- **Words:** does every term the user used map onto the system's units (they say "app group", the API takes a component)? Is the mapping recorded?
- **Scope:** is an open scope ("all DCs", "all servers") defined by a config or a WISDOM.md decision, with its skips?
- **Blocks:** does a script exist for every step? For each one that does not, what will building it need: is the spec enough, or does it need a HAR, a session, a TFA code?
- **Changes:** which steps change a real system, and does a WISDOM.md decision already cover that kind of change?
- **Failure:** what happens to a target that fails: skip, stop, retry?
- **Output:** where does the result go, and in what form?

## 2. Ask once

Put every open question from the recon in one message, numbered:

- what you understood the goal to be, in one line, with the terms mapped to the system's units
- each question with your proposed answer, so the user can just say yes
- each change to a real system, with what it will do and on which targets, for one go up front
- what the user must hand you: a login (the exact command), a capture (how to make it, from workspace-write step 1), a value only they know

A code that expires faster than a round trip through you (a TFA code) cannot be asked for up front. Plan for the user to run the final step: give them one `!` command that takes the code on stdin.

When a recorded decision or the workspace answers a question, don't ask it. Build a missing block before you ask when its design does not depend on any answer; then the batch can show its preview. Then wait.

## 3. Do the work

- Join scripts together: read their output (`--json` when you need values) and decide the next step.
- When no script exists, use the ordinary tools a human would (`kubectl`, `ssh`, `dig`, `journalctl`, `jq`).
- When a block the task needs does not exist, build it without asking: follow workspace-write from its step 2, using this run's goal, targets and the user's answers as the intake. Writing blocks changes no real system.
- You never read a secret value yourself: no pass entries, no cookie files, no token in a command line or a log. An API call that needs a login goes through a script.
- Keep short notes in `$WS_SCRATCH/<task>.<yyyy-mm>/steps.md` as you go (create the folder with `ws scratch new <task> --no-open` when it is missing): what you ran, what you found, what you chose. No secret values.
- Output the user asked to keep, including a "temp file" in their words, goes in `$WS_SCRATCH/<task>.<yyyy-mm>/`, never `$WS_TMPD`, which is removed when the script exits. Don't ask about it.

Stop and ask again only for something the recon could not see: a result that contradicts what is recorded, a change the up-front go did not cover, or a new fork. Anything else, decide yourself and carry on; say what you chose in the report.

A decision to skip what fails means no retry, unless the user asks for one.

## 4. Keep what you learned

For each answer the user gave, ask yourself: will this question come up again? If yes, give the answer the first home that can carry it, by the test under "references/WISDOM.md" in write-rules.md: a script that enforces it, a config value, or a dated WISDOM.md bullet. When the user asks for a WISDOM.md bullet, write it, even if the test would put the fact elsewhere.

At the end, hand `steps.md` to workspace-write: commands you typed by hand that a later run will need become scripts, surprises become lessons. A run that taught nothing writes nothing. A result of this run that can change later, such as which targets failed, goes in the report and `steps.md` only.

The rules of a built-in skill (workspace-write, workspace-run) are read-only here: ws installs and updates them. A fix to those rules, as opposed to a fact about the user's systems, is not edited in place. Write it as a suggestion in the ws repo's backlog, `docs/backlog.md` (here `experiments/ws-tool/docs/backlog.md`), for the user to review; it ships in a later ws release. Facts about the user's systems still go to WISDOM.md, configs and scripts as above.

## 5. Report

- what you did, per target, and what is left
- the choices you made without asking
- what you recorded, by file
- how the user can check it worked
