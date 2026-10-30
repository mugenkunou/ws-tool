---
name: workspace-write
description: Build reusable blocks in ~/Workspace (API reference, scripts, configs, pass entries, WISDOM.md lessons, runbook entries) from a task done once, whether a HAR, curl commands, shell history, a rough script, or a run just improvised. Also use before creating, editing or moving any file in ~/Workspace, and when the user asks where a file belongs.
---

# Workspace write

~/Workspace holds the **blocks** an agent composes to do a task again:

- scripts do one action each and preview every change
- references say what a system can do (`<system>.openapi.yaml`) and what we learned (`WISDOM.md`)
- runbooks say how a task is done: which blocks, in what order, and what to decide on the way (`RUNBOOKS.md`)
- configs say where, and pass says with what authority

This skill turns a task done once into blocks, reusing every block that already exists. It stops at the blocks and the runbook entry that names them in order. Doing the task is workspace-run's job.

Two references sit beside this file:

- [write-rules.md](write-rules.md): where every file goes, the path and secret rules, and the formats of WISDOM.md, RUNBOOKS.md and scripts/README.md. Read it before you place any file.
- [script-contract.md](script-contract.md): how a script looks and behaves. Read it before you write or change a script.

**Placing one file**, outside a build or when the user asks where something belongs: apply write-rules.md, including its Verify section, and stop there.

## Gate

Read-only calls and the system's own preview or verify endpoints run freely. A call that changes a real system runs only after the user says go for that specific call. Until then, every write stays a dry run. This holds while building too.

## Steps

### 1. Intake

Ask in one batch:

- the task in one sentence, and the system it runs on
- the capture. A HAR is best: DevTools → Network → Preserve log → do the task once → Export HAR. Curl commands, shell history or a rough script also work. For a run you just improvised, write the commands you ran to `$WS_SCRATCH/<task>.<yyyy-mm>/steps.md`, in order, with secret values left out.
- the targets: instances, environments, DCs
- done: how the user checks that it worked
- never: what must not happen, such as a version change, other targets, or deletes

The capture holds live sessions and secrets, so it lives in `$WS_SCRATCH/<task>.<yyyy-mm>/`, the scratch folder for the task. Create it with `ws scratch new <task> --no-open` when it is missing; ws adds the `.<yyyy-mm>` itself. Move the capture there if it sits anywhere else.

Done when you hold a capture and a one-sentence goal.

### 2. Inventory

For the system, read whatever exists of:

- the header comments of `scripts/lib/common.sh`, `scripts/lib/<system>.sh` and each `scripts/<system>-*.sh`
- `references/<system>.openapi.yaml`, and all of `references/WISDOM.md`
- the table of `references/RUNBOOKS.md`, and the entry for this task when a row matches its goal, whatever its words
- `configs/<system>-*/`, and `pass ls svc/`

Label every block the task needs as **reused**, **extended** or **new**. A block with no file yet is new.

Done when every block has a label.

### 3. Read the capture

- HAR: it is JSON; read it with jq. List the calls, with static files skipped and query strings cut:
  ```bash
  jq -r '.log.entries | to_entries[] | select(.value.request.url | test("\\.(js|css|png|svg|ico|woff2?)([?#]|$)") | not) | [.key, .value.request.method, .value.response.status, (.value.request.url | sub("[?#].*"; ""))] | @tsv' FILE
  ```
  Show call N with secret-looking headers and params masked:
  ```bash
  jq --argjson n N '
    def mask: map(if .name | test("cookie|auth|token|csrf|secret|key"; "i") then .value = "<masked>" else . end);
    .log.entries[$n] | {method: .request.method, url: (.request.url | sub("[?#].*"; "")),
      query: (.request.queryString | mask), headers: (.request.headers | mask),
      body: .request.postData.text, status: .response.status, response: .response.content.text}' FILE
  ```
  Show the writes and the reads that feed them. When a request body or an id's origin is missing, write the app's JS bundle (`.response.content.text`, base64-decoded when `encoding` is base64) into the capture folder and search it. Browsers drop most response bodies and some request bodies; fill those gaps with live read-only calls once the user hands you a session.
- Anything else: list its commands in order.

Separate the reads from the writes. Trace every id and parameter of every write to its source: a response, the app's JS, a config, or the user.

Then tell the user, in one message:

- what they did, as numbered steps with the ids involved
- every surprise: side effects, version changes, values that differ per target
- the block labels from step 2
- the open questions

Done when every parameter of every write has a known source and the user has answered.

### 4. Reference

Write or extend `references/<system>.openapi.yaml` with auth, the stable ids, and every endpoint the scripts will call: params, request body and response shape. Mark the source of each fact: capture, live, client source, or not captured. Secret values stay out.

A task that only drives local tools (kubectl, docker, ssh) has no API of its own. Skip this step and say so in the report.

Done when the YAML parses and holds every endpoint the scripts will call.

### 5. Scripts

Follow script-contract.md. Build the client in `scripts/lib/<system>.sh` first, then one script per action. Split a rough script from the user the same way. A part that has to stay one multi-step script goes to `experiments/`.

- Run every read script live.
- Run every write script as a dry run only, and diff its payload against the captured request. The only difference allowed is the intended change.
- Add every new top-level script to `scripts/README.md` in the format write-rules.md gives, with real values from your runs.

Done when every read has run live, every write's dry-run payload diffs clean, and scripts/README.md lists every new script.

### 6. Config and secrets

Place each value by write-rules.md. Then search every file you wrote for each secret value you saw in the capture.

Done when that search finds nothing.

### 7. Lessons

Give every surprise from steps 3 to 5 its home by the test under "references/WISDOM.md" in write-rules.md. Most land in a script, the spec or a config; a run can add nothing to WISDOM.md.

Done when every surprise has a home or failed the test, and no WISDOM.md bullet repeats another block.

### 8. Runbook

Find the task's row in `references/RUNBOOKS.md` by meaning, not by wording. When there is none, add a row and an entry. When there is one, update it: the words people used, the route (new scripts, changed order, steps that turned out wrong), **Last run**. Use the format in write-rules.md under "references/RUNBOOKS.md", and link to a script or a WISDOM.md bullet rather than copy it.

Done when the row and the entry exist and every script the route names exists.

### 9. Report

Tell the user:

- what was reused, extended and built, by file
- what ran live, and what is still untested (usually the writes)
- the order you ran the scripts in
- the runbook entry you added or changed
