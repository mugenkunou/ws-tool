# `ws` Developer Guide 🛠️

Welcome, builder of sane workspaces.

This guide gets you from **fresh Linux box** → **coding** → **testing** → **publishing on GitHub** with minimal pain and maximum momentum.

---

## 1) Prerequisites (OS)

Install baseline tools first:

```bash
sudo apt update
sudo apt install -y \
  git make curl wget ca-certificates gnupg pass \
  coreutils findutils grep diffutils file util-linux sudo
```

Optional-but-useful:

```bash
sudo apt install -y jq
```

> Why these? `ws` intentionally delegates to standard Linux tools (`ln`, `find`, `grep`, `script`, `git`, etc.) instead of reimplementing them.

---

## 2) Install Go (required)

Project target: **Go 1.23+**.

### Option A — Official tarball (recommended)

```bash
cd /tmp
GO_VER="$(curl -fsSL https://go.dev/VERSION?m=text | head -n1)"
curl -LO "https://go.dev/dl/${GO_VER}.linux-amd64.tar.gz"
sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf "${GO_VER}.linux-amd64.tar.gz"

echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
source ~/.bashrc

go version
```

### Option B — Use distro package

```bash
sudo apt install -y golang-go
go version
```

If distro Go is old, prefer Option A.

---

## 3) VS Code setup (plugins + settings)

Install these extensions:

- `golang.go` — Go language tooling, tests, debug
- `eamodio.gitlens` — Git history and blame
- `streetsidesoftware.code-spell-checker` — docs/readme sanity
- `yzhang.markdown-all-in-one` — markdown authoring quality
- `esbenp.prettier-vscode` — markdown/json formatting convenience

Quick install from terminal:

```bash
code --install-extension golang.go
code --install-extension eamodio.gitlens
code --install-extension streetsidesoftware.code-spell-checker
code --install-extension yzhang.markdown-all-in-one
code --install-extension esbenp.prettier-vscode
```

Recommended workspace settings (`.vscode/settings.json` if you want):

```json
{
  "go.useLanguageServer": true,
  "go.formatTool": "gofmt",
  "editor.formatOnSave": true,
  "go.testFlags": ["-v"],
  "files.insertFinalNewline": true
}
```

---

## 4) Clone and bootstrap

```bash
git clone https://github.com/mugenkunou/ws-tool.git
cd ws-tool
```

Useful first checks:

```bash
go version
make fmt
make build
./ws version
```

---

## 5) Build, run, deploy

### Build

```bash
make build
```

Binary output: `./ws`

### Run locally

```bash
./ws help
./ws version
./ws init --dry-run
```

### Deploy to your machine

```bash
sudo cp ./ws /usr/local/bin/ws
ws version
```

---

## 6) Test suite

### Standard test run

```bash
make test
```

### Verbose test run

```bash
go test -v ./...
```

### Coverage run

```bash
go test -cover ./...
```

### Use repo-local temp/cache dirs (recommended on corp/dev boxes)

```bash
mkdir -p tmp .gocache
TMPDIR=$PWD/tmp GOCACHE=$PWD/.gocache go test ./...
```

Why this helps: some managed Linux environments mount `/tmp` with `noexec`, which can break `go test`/`go build` when Go needs to execute temp artifacts during compilation or tests.

Use the same env vars for build commands too:

```bash
TMPDIR=$PWD/tmp GOCACHE=$PWD/.gocache make build
```

---

## 7) Daily developer flow

```bash
# 1) sync
git pull --rebase

# 2) code
# ... edit files ...

# 3) quality gate
make fmt
make test
make build

# 4) commit
git add .
git commit -m "feat: your change"

# 5) push
git push
```

---

## 8) Release pipeline

### Overview

Pushing a semver tag (`v*`) triggers the GitHub Actions release workflow, which:
1. Runs tests
2. Cross-compiles binaries (linux/darwin × amd64/arm64)
3. Generates SHA-256 checksums
4. Creates a GitHub Release with all artifacts

### Secret scanning (gitleaks)

Before every push, a pre-push git hook runs [gitleaks](https://github.com/gitleaks/gitleaks) to scan for leaked secrets. This is critical since the repo is public.

**Install gitleaks:**

```bash
go install github.com/zricethezav/gitleaks/v8@v8.21.2
```

**Install the hook:**

```bash
make hooks
```

This copies `scripts/pre-push` into `.git/hooks/`. Every `git push` will now scan for secrets first.

**Manual scan:**

```bash
gitleaks detect --source . --verbose
```

### Version injection

The binary version is injected at build time via ldflags. The `appVersion` variable in `cmd/version.go` defaults to `"dev"` and is overridden during CI/release builds.

```bash
# local build with version
make build VERSION=v0.2.0

# or directly
go build -ldflags "-s -w -X github.com/mugenkunou/ws-tool/cmd.appVersion=v0.2.0" -o ws .
```

`-s -w` strips debug symbols and DWARF tables — smaller binary, no local paths leaked.

### Tagging a release

```bash
# 1. quality gate
make release-check

# 2. tag
git tag -a v0.2.0 -m "v0.2.0"

# 3. push (pre-push hook runs gitleaks automatically)
git push origin main --follow-tags
```

The tag push triggers `.github/workflows/release.yml`, which builds and publishes the release.

### CI

Every push to `main` and every PR triggers `.github/workflows/ci.yml`, which runs:
- `gofmt` check (no unformatted code)
- `go vet`
- `go test -race`
- Build verification

### Quick reference

| Command | Purpose |
|---|---|
| `make build` | Local dev build (version=dev) |
| `make build VERSION=v0.2.0` | Local build with version |
| `make test` | Run tests |
| `make release-check` | Full pre-release gate (gitleaks + vet + race tests) |
| `make hooks` | Install git pre-push hook |
| `make clean` | Remove binary |

---

## 9) Adding new commands

### RO vs RW classification

Every command is either **read-only (RO)** or **read-write (RW)**. If it writes to disk, config, manifest, or system state → RW.

- **RO commands** receive `(args, globals, stdout, stderr)` — no stdin, no prompts.
- **RW commands** receive `(args, globals, stdin, stdout, stderr)` — stdin is threaded for interactive prompts.

### Action Plan pattern (required for all RW commands)

All RW commands **must** use the Action Plan pattern defined in `cmd/plan.go`. Do not use `confirm()` for new commands.

```go
func runMyCommand(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
    // 1. Parse flags, validate inputs, gather state
    // ...

    if *dryRun {
        globals.dryRun = true
    }

    // 2. Build the plan — one Action per discrete mutation
    plan := Plan{Command: "mycommand"}
    for _, item := range items {
        item := item // capture loop variable
        plan.Actions = append(plan.Actions, Action{
            ID:          fmt.Sprintf("process-%s", item.Name),
            Description: fmt.Sprintf("Process %s", item.Name),
            Execute: func() error {
                return processItem(item)
            },
        })
    }

    // 3. Execute — RunPlan handles dry-run, prompts, quiet/json auto-accept
    planResult := RunPlan(plan, stdin, stdout, globals)

    // 4. JSON output (include planResult.Actions for programmatic consumers)
    if globals.json {
        return writeJSON(stdout, stderr, "mycommand", map[string]any{
            "actions": planResult.Actions,
        })
    }

    // 5. Return appropriate exit code
    return planResult.ExitCode()
}
```

### Granularity rule

One Action per independently meaningful mutation. Ask: "would a user ever want to say yes to this but no to the next?" If yes → separate Actions.

| Command type | Granularity |
|---|---|
| Per-file operations (init, dotfile add) | One action per file |
| Fleet operations (repo pull/push/run) | One action per repo |
| Cleanup operations (log prune, scratch prune) | One action per item removed |
| Single-mutation commands (log start) | One action total |

### Prompt vocabulary

Interactive mode presents each action with `[y/n/a/q]`:
- `y` (default, Enter) — execute this action
- `n` — skip, continue to next
- `a` — accept all remaining
- `q` — quit, skip all remaining

### Testing RW commands

Tests pass `strings.NewReader("y\n")` as stdin. `promptChoice` returns the default key `"y"` on EOF, so all actions are auto-accepted in tests.

---

## 10) Troubleshooting quick hits

- **`go: toolchain not available`**
  - Install Go 1.23+ via official tarball.
- **Build/tests fail with temp-dir execution restrictions**
  - Common cause: `/tmp` is mounted with `noexec`.
  - Use the repo-local `TMPDIR` + `GOCACHE` setup from the test section for both `go test` and `go build`/`make build`.
- **`ws` command not found**
  - Ensure `/usr/local/bin` is in `PATH`.
- **Formatting drift in PRs**
  - Run `make fmt` before commit.

---

## 11) Golden rule

Before every PR/release:

```bash
make fmt && make test && make build
```

If this passes, you're in a very good place. ✅
