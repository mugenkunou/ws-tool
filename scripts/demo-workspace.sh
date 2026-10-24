#!/usr/bin/env bash
# demo-workspace.sh — build a throwaway workspace to try `ws tui` on.
#
# Everything lives in one new directory (default: a fresh one under
# ~/.cache/ws-demo). HOME, the XDG directories, the crontab and git's global
# config all point inside it, so nothing of yours is read or changed: not your
# crontab, not ~/.bashrc, not your trash, not ~/Workspace.
#
#   scripts/demo-workspace.sh [dir]     build it (dir must not exist yet)
#   source <dir>/env.sh && ws tui       use it in this shell
#
# The demo has: symlinks (one broken, one drifted, one found but not added), repos (clean, dirty,
# ahead, behind, diverged, no upstream, detached HEAD), scratch directories,
# a big dump and deep paths for the Ignore screen, fake secrets in a deploy script, cron jobs
# and a few trashed items.
set -euo pipefail

here=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
base=${XDG_CACHE_HOME:-$HOME/.cache}/ws-demo
if [[ $# -ge 1 ]]; then
  demo=$1
  [[ -e $demo ]] && { echo "demo-workspace: $demo already exists; pick a new directory" >&2; exit 1; }
  mkdir -p "$demo"
else
  mkdir -p "$base"
  demo=$(mktemp -d "$base/demo.XXXXXX")
fi
demo=$(cd "$demo" && pwd)

# Build first, with your own Go environment: the demo's HOME has no module cache.
mkdir -p "$demo"/{home,bin,xdg/config,xdg/state,xdg/data,remotes,root}
echo "building ws…"
(cd "$here" && go build -o "$demo/bin/ws" .)

# ── the isolated environment ──────────────────────────────────────
cat > "$demo/env.sh" <<ENV
# source this to use the demo workspace; nothing outside $demo is touched
export HOME="$demo/home"
export XDG_CONFIG_HOME="$demo/xdg/config"
export XDG_STATE_HOME="$demo/xdg/state"
export XDG_DATA_HOME="$demo/xdg/data"
export WS_WORKSPACE="$demo/home/Workspace"
export WS_CRONTAB_FILE="$demo/crontab"
export WS_SYMLINK_ROOT="$demo/root"
export GIT_CONFIG_GLOBAL="$demo/gitconfig"
export GIT_TERMINAL_PROMPT=0
export PASSWORD_STORE_DIR="$demo/home/.password-store"
export VISUAL=true
unset DISPLAY WS_CONFIG
export PATH="$demo/bin:\$PATH"
ENV
# shellcheck disable=SC1091
source "$demo/env.sh"
git config --global user.name "Demo User"
git config --global user.email "demo@example.invalid"
git config --global init.defaultBranch main
: > "$WS_CRONTAB_FILE"

ws() { "$demo/bin/ws" "$@"; }

ws init --quiet
W=$WS_WORKSPACE

# ── symlinks ──────────────────────────────────────────────────────
printf 'alias ll="ls -la"\nexport EDITOR=vim\n' > "$HOME/.bashrc"
printf '[user]\n\tname = Demo User\n' > "$HOME/.gitconfig"
printf 'set number\n' > "$HOME/.vimrc"
printf 'Host *\n  ServerAliveInterval 60\n' > "$HOME/.sshconfig"
for f in .bashrc .gitconfig .vimrc .sshconfig; do
  ws symlink add --quiet "$HOME/$f"
done
# broken: the workspace copy is gone
rm -f "$W/symlinks/home/.vimrc"
# drifted: something replaced the link with a real file
rm -f "$HOME/.sshconfig"
printf 'Host *\n  ServerAliveInterval 5\n' > "$HOME/.sshconfig"
# found: a known config that is not symlinked yet (`ws symlink add` offers it)
printf 'export ZDOTDIR=$HOME\n' > "$HOME/.zshrc"

# ── repos ─────────────────────────────────────────────────────────
mkrepo() { # name state
  local name=$1 state=$2 dir=$W/experiments/$1 remote=$demo/remotes/$1.git
  git init -q --bare "$remote"
  git init -q "$dir"
  (
    cd "$dir"
    echo "# $name" > README.md
    git add -A && git commit -qm "first commit"
    if [[ $state != noupstream ]]; then
      git remote add origin "$remote"
      git push -q -u origin main
    fi
    case $state in
      dirty) echo "work in progress" >> README.md; echo "scratch" > notes.txt ;;
      ahead) echo "local" > local.txt; git add -A; git commit -qm "local work" ;;
      behind|diverged)
        local other=$demo/remotes/$name-other
        git clone -q "$remote" "$other"
        ( cd "$other" && echo "upstream" > up.txt && git add -A && git commit -qm "upstream work" && git push -q )
        if [[ $state == diverged ]]; then echo "local" > local.txt; git add -A; git commit -qm "local work"; fi
        git fetch -q
        ;;
      detached) git checkout -q --detach ;;
    esac
  )
}
mkdir -p "$W/experiments"
mkrepo clean clean
mkrepo blog dirty
mkrepo api ahead
mkrepo notes behind
mkrepo bruno diverged
mkrepo sketch noupstream
mkrepo old detached

# ── ignore and secrets ────────────────────────────────────────────
head -c 30000000 /dev/zero > "$W/experiments/api/dump.sql"
mkdir -p "$W/experiments/blog/node_modules/left-pad"
echo "module.exports = 1" > "$W/experiments/blog/node_modules/left-pad/index.js"
mkdir -p "$W/notes/a/b/c/d/e/f/g" && echo deep > "$W/notes/a/b/c/d/e/f/g/note.md"
printf 'AWS_SECRET_ACCESS_KEY=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY\n-----BEGIN RSA PRIVATE KEY-----\nabc\n' > "$W/notes/deploy.sh"

# ── scratch and trash ─────────────────────────────────────────────
for s in dns-debug proxy-timeout old-idea; do ws scratch new "$s" --quiet >/dev/null 2>&1 || true; done
ws scratch tag dns-debug dns network --quiet 2>/dev/null || true
ws scratch rm old-idea --quiet 2>/dev/null || true
echo "draft" > "$W/notes/draft.md"
ws trash enable --quiet 2>/dev/null || true

# ── cron ──────────────────────────────────────────────────────────
ws cron add mega-sync session-prune --quiet 2>/dev/null || true

cat <<MSG

Demo workspace ready: $demo

  source $demo/env.sh
  ws tui            # the full-screen interface
  ws repo ls        # or any CLI command: it only sees the demo

Everything is inside $demo. Delete that directory to remove the demo.
MSG
