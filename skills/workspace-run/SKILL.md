---
name: workspace-run
description: "Do a task on a real system (a server, a service, a cluster, a DC, or this machine) with the blocks in ~/Workspace (runbooks, scripts, references, configs, WISDOM.md): investigate, debug, build, deploy, change a config, or a chore. Use for any such goal in plain English, whether or not it names the system, and whether or not ~/Workspace knows that system yet. Also use it to plan a task without doing it: its first two steps change no real system. Not for changing code in a repo."
---

# Workspace run

You are an engineer who knows where everything in ~/Workspace is kept. The user gives you a goal. You do the work and most of the thinking yourself. You ask once, up front, so the run does not stall later waiting for the user. Every answer you get is written down, so the next run asks less.

`workspace-write` keeps the blocks in order. This skill uses them. Every file you create or change in ~/Workspace goes through workspace-write's rules (`$(ws skill show workspace-write --path)/write-rules.md`).

## 1. Recon

Quick, read-only and thorough. The goal is to find every question the run will raise before you start it.

Start with `ws layout`: it names what each folder holds and where the homes are on this machine. Read only the folders the goal needs; `artifacts/`, `archive/` and `data/` rarely are. Never run `find` or `ls -R` over ~/Workspace.

Then read `references/RUNBOOKS.md`, its table first. Match the goal to a row by meaning, not by wording: **Words people use** lists phrases said before, **Not this** rules out near-misses. When a row fits, read its entry in full; its **Route** is the starting plan for the run, and you check it below instead of inventing one. Its **Last run** says what went wrong last time: check whether it was fixed since, and when it was not, plan around it and say so in the ask. When two rows fit, or none does, say so in the ask.

For the systems the goal names (the matched row names its system), read:

- `references/WISDOM.md`, the sections for those systems and their neighbours: lessons, and decisions the user already made
- the header comments of `scripts/<system>-*` and `scripts/lib/<system>.*`: what you can do, and what usually comes next
- `references/<system>*.openapi.yaml`: how the system's API behaves
- `configs/<system>-*/`: where things are, and facts only the user knows
- `references/zoho-glossary.md` for abbreviations, `experiments/` for bigger scripts, `notes/` (read only; skip `notes/.obsidian/`)

Listing pass entry names (`pass ls svc/`) is fine; reading their values is not.

Without a matched row, find the system from the filename prefixes. When the goal names no system, grep the script headers and WISDOM.md for the user's words. Grep WISDOM.md for the exact error text when the goal is a symptom.

Then check every session the run will need, in one call: `<system>-login.sh --check` for each system the steps touch, or the script the entry's Route names for it. Run the read-only lookups the task needs (targets, ids, current state) with the sessions that work, and from the cache where a script answers without one. A missing login goes in the ask with its exact command, all of them in the same message, so the user logs in once for the run.

Sketch the run in your head, step by step (from the entry's Route when there is one), and check each step:

- **Words:** does every term the user used map onto the system's units (they say "app group", the API takes a component)? Is the mapping recorded?
- **Scope:** is an open scope ("all DCs", "all servers") defined by a config or a WISDOM.md decision, with its skips?
- **Blocks:** does a script exist for every step? For each one that does not, what will building it need: is the spec enough, or does it need a HAR, a session, a TFA code?
- **Changes:** which steps change a real system, and does a WISDOM.md decision already cover that kind of change?
- **Per target:** what you do when a target is already as asked (skip), lacks what the change needs (skip and report), is in a state you did not expect, such as another version (report, don't touch), or fails (skip, no retry). A WISDOM.md decision gives the default. Otherwise, propose one in the ask.
- **Output:** where does the result go, and in what form?

## 2. Ask once

Put every open question from the recon in one message, numbered:

- what you understood the goal to be, in one line, with the terms mapped to the system's units, and the runbook entry you follow (or that none fits)
- each question with your proposed answer, so the user can just say yes
- each change to a real system, with what it will do and on which targets, for one go up front
- what the user must hand you: a login (the exact command), a capture (how to make it, from workspace-write step 1), a value only they know

With several targets, show a table: each target and what changes on it, before → after, in plain words. Don't ask the user to approve a "preview" or a "dry run". End the message with the numbered decisions, each with your default filled in, so a plain "go" accepts them all.

A code that expires faster than a round trip through you (a TFA code) cannot be asked for up front. Plan for the user to run the final step: give them one `!` command that takes the code on stdin.

When a recorded decision or the workspace answers a question, don't ask it. Build a missing block before you ask when its design does not depend on any answer; then the batch can show its preview. Then wait.

## 3. Do the work

- Join scripts together: read their output (`--json` when you need values) and decide the next step.
- When no script exists, use the ordinary tools a human would (`kubectl`, `ssh`, `dig`, `journalctl`, `jq`).
- A step over many targets runs as one call, in the background when it can take minutes. Don't loop over targets one call at a time in the foreground.
- When a block the task needs does not exist, build it without asking, unless step 4 says to propose it first: follow workspace-write from its step 2, using this run's goal, targets and the user's answers as the intake. Writing blocks changes no real system.
- You never read a secret value yourself: no pass entries, no cookie files, no token in a command line or a log. An API call that needs a login goes through a script.
- Keep short notes in `$WS_SCRATCH/<task>.<yyyy-mm>/steps.md` as you go (create the folder with `ws scratch new <task> --no-open` when it is missing): what you ran, what you found, what you chose, and what cost a round trip or a retry: a question the recon missed, a call that was blocked or timed out, a read that found nothing, a call repeated for the same answer. No secret values.
- Output the user asked to keep, including a "temp file" in their words, goes in `$WS_SCRATCH/<task>.<yyyy-mm>/`, never `$WS_TMPD`, which is removed when the script exits. Don't ask about it.

Stop and ask again only for something the recon could not see: a result that contradicts what is recorded, a change the up-front go did not cover, or a new fork. Anything else, decide yourself and carry on; say what you chose in the report.

A decision to skip what fails means no retry, unless the user asks for one.

## 4. Keep what you learned

Review the run first. From the costs in `steps.md`, pick the ones a later run of this task would pay again. A cost the entry's **Last run** already named is a repeat, and it comes first. Give each cost one fix, in the first place that can carry it:

- the goal matched no row, or the wrong one: **Words people use** or **Not this**
- a question the recon missed, or a fork asked mid-run: the entry's **Means** or **Judgment calls**, with the default
- a step done by hand, in the wrong order, or a read that found nothing: the entry's **Route**, naming the block
- calls repeated, or a loop over targets: a flag or a batch mode on the script
- a login that blocked a read-only lookup: a cache fallback in the script
- a blocked call: a permission rule, which is a setting outside ~/Workspace, so propose it

Without an entry, the home test in write-rules.md decides. Keep the five that save the most. You cannot see token counts, so count calls and round trips instead.

For each answer the user gave, ask yourself: will this question come up again? If yes, give the answer the first home that can carry it, by the test under "references/WISDOM.md" in write-rules.md: a script that enforces it, a config value, or a dated WISDOM.md bullet. When the user asks for a WISDOM.md bullet, write it, even if the test would put the fact elsewhere.

At the end, hand `steps.md` to workspace-write: commands you typed by hand that a later run will need become scripts, surprises become lessons. A run that taught nothing writes nothing. A result of this run that can change later, such as which targets failed, goes in the report and `steps.md` only.

The runbook is where the route of a task lives. After a run that followed an entry, update it by the format in write-rules.md ("references/RUNBOOKS.md"): the words the user used, when they are new; a step that was wrong or missing; **Last run**. After a run that followed none and went well, add a row and an entry for the task. Neither needs a go.

Write without asking what records a fact or an answer: a WISDOM.md bullet, a config value, a script header, a fix or a flag on an existing script, a new script for a step this run did by hand, or a runbook row, entry or update as above. Write nothing for a fact an agent can work out from the input on its own. Propose first, and write only after the user's go, anything that changes how the user works or what the agent may touch:

- reading credentials or sessions from a new place, such as browser cookies
- a new kind of file or concept the workspace does not have yet, such as an index
- restructuring a runbook entry, or removing its steps
- a change to a decision already recorded
- a setting outside ~/Workspace, such as permission rules
- moving or deleting a file

The rules of a built-in skill (workspace-write, workspace-run) are read-only here: ws installs and updates them. A fix to those rules, as opposed to a fact about the user's systems, is not edited in place. Write it as a suggestion in the ws repo's backlog, `docs/backlog.md` (here `experiments/ws-tool/docs/backlog.md`), for the user to review; it ships in a later ws release. Facts about the user's systems still go to WISDOM.md, configs and scripts as above.

## 5. Report

- **Outcome**, in one line: done, partly done, or blocked.
- **Per target:** done, skipped and why, failed.
- **Decisions:** the ones the user gave in this run, the recorded ones you used (with their date), and the ones you made without asking.
- **Runbook:** the entry you followed, or the one you added, and what you changed in it.
- **Know this:** delayed effects, risks, and what was not verified.
- **Left to do**, and how the user can check that it worked.
- **Cost:** the round trips, blocked calls, timeouts and repeated calls in this run, in one line, and which of them repeat **Last run**.
- **Recorded:** what you wrote, by file, each with the cost it saves the next run.
- **For your go:** the proposals, numbered, each with the cost it saves the next run, so the user can reply "go 1,3".

Leave out an empty section.
