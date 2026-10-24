# Symlink catalog (the data for `internal/symlink/catalog.go`)

This is the flat list ws knows about. It is compiled into the binary as a Go slice and is
never written to any file. ws only ever reports the entries that exist on the machine and
are not in `ws.json` yet.

Columns: `path` uses `~` for `$HOME`; `*` is a glob over one path segment. `kind` is file or
folder. `secret` entries start unchecked in the checklist. `sudo` entries start unchecked and
need root to link. `os` is `any`, `linux` or `mac`. `conf` is `M` where the path varies by
version or setup: the agent must keep the entry but add a one-line comment in the Go source.

An entry with a glob expands to one found item per match. `~/.ssh/id_*` must not match
`*.pub`. Each private key is listed with its `.pub` as a pair (`~/.ssh/id_*` and
`~/.ssh/id_*.pub`), and the checklist shows the pair on one row.

| app | path | kind | secret | sudo | os | conf |
| --- | --- | --- | --- | --- | --- | --- |
| agents | ~/.agents/skills | folder | no | no | any | M |
| aider | ~/.aider.conf.yml | file | no | no | any | |
| alacritty | ~/.config/alacritty/alacritty.toml | file | no | no | any | |
| ansible | ~/.ansible.cfg | file | no | no | any | |
| asdf | ~/.tool-versions | file | no | no | any | |
| atuin | ~/.config/atuin/config.toml | file | no | no | any | |
| aws | ~/.aws/config | file | no | no | any | |
| aws | ~/.aws/credentials | file | yes | no | any | |
| bash | ~/.bash_aliases | file | no | no | linux | |
| bash | ~/.bash_profile | file | no | no | any | |
| bash | ~/.bashrc | file | no | no | any | |
| bash | ~/.inputrc | file | no | no | any | |
| bash | ~/.profile | file | no | no | any | |
| bat | ~/.config/bat/config | file | no | no | any | |
| bat | ~/.config/bat/themes | folder | no | no | any | |
| cargo | ~/.cargo/config.toml | file | no | no | any | |
| cargo | ~/.cargo/credentials.toml | file | yes | no | any | |
| claude | ~/.claude/agents | folder | no | no | any | |
| claude | ~/.claude/CLAUDE.md | file | no | no | any | |
| claude | ~/.claude/commands | folder | no | no | any | |
| claude | ~/.claude/rules | folder | no | no | any | M |
| claude | ~/.claude/settings.json | file | no | no | any | |
| codex | ~/.codex/AGENTS.md | file | no | no | any | |
| codex | ~/.codex/config.toml | file | no | no | any | |
| conda | ~/.condarc | file | no | no | any | |
| continue | ~/.continue/.continuerc.json | file | no | no | any | |
| continue | ~/.continue/config.yaml | file | no | no | any | |
| copilot | ~/.copilot/agents | folder | no | no | any | |
| copilot | ~/.copilot/mcp-config.json | file | yes | no | any | M |
| curl | ~/.netrc | file | yes | no | any | |
| cursor | ~/.config/Cursor/User/keybindings.json | file | no | no | linux | |
| cursor | ~/.config/Cursor/User/settings.json | file | no | no | linux | |
| cursor | ~/.config/Cursor/User/snippets | folder | no | no | linux | |
| cursor | ~/.cursor/mcp.json | file | yes | no | any | |
| cursor | ~/Library/Application Support/Cursor/User/keybindings.json | file | no | no | mac | |
| cursor | ~/Library/Application Support/Cursor/User/settings.json | file | no | no | mac | |
| cursor | ~/Library/Application Support/Cursor/User/snippets | folder | no | no | mac | |
| direnv | ~/.config/direnv/direnv.toml | file | no | no | any | |
| direnv | ~/.config/direnv/direnvrc | file | no | no | any | |
| docker | ~/.docker/config.json | file | yes | no | any | |
| docker | /etc/docker/daemon.json | file | no | yes | linux | |
| editorconfig | ~/.editorconfig | file | no | no | any | |
| emacs | ~/.config/doom | folder | no | no | any | |
| emacs | ~/.config/emacs/init.el | file | no | no | any | |
| emacs | ~/.emacs.d/init.el | file | no | no | any | |
| fish | ~/.config/fish/conf.d | folder | no | no | any | |
| fish | ~/.config/fish/config.fish | file | no | no | any | |
| fish | ~/.config/fish/fish_plugins | file | no | no | any | |
| fish | ~/.config/fish/functions | folder | no | no | any | |
| gcloud | ~/.config/gcloud/configurations | folder | no | no | any | M |
| gemini | ~/.gemini/commands | folder | no | no | any | |
| gemini | ~/.gemini/GEMINI.md | file | no | no | any | |
| gemini | ~/.gemini/settings.json | file | no | no | any | |
| gh | ~/.config/gh/config.yml | file | no | no | any | |
| gh | ~/.config/gh/hosts.yml | file | yes | no | any | |
| ghostty | ~/.config/ghostty/config | file | no | no | any | |
| git | ~/.config/git/config | file | no | no | any | |
| git | ~/.config/git/ignore | file | no | no | any | |
| git | ~/.git-credentials | file | yes | no | any | |
| git | ~/.gitconfig | file | no | no | any | |
| git | ~/.gitignore_global | file | no | no | any | |
| gnupg | ~/.gnupg/gpg-agent.conf | file | no | no | any | |
| gnupg | ~/.gnupg/gpg.conf | file | no | no | any | |
| gradle | ~/.gradle/gradle.properties | file | yes | no | any | |
| helix | ~/.config/helix/config.toml | file | no | no | any | |
| helix | ~/.config/helix/languages.toml | file | no | no | any | |
| helix | ~/.config/helix/themes | folder | no | no | any | |
| helm | ~/.config/helm/repositories.yaml | file | yes | no | linux | |
| hosts | /etc/hosts | file | no | yes | any | |
| ideavim | ~/.ideavimrc | file | no | no | any | |
| jetbrains | ~/.config/JetBrains/*/keymaps | folder | no | no | linux | M |
| jetbrains | ~/Library/Application Support/JetBrains/*/keymaps | folder | no | no | mac | M |
| k9s | ~/.config/k9s/aliases.yaml | file | no | no | linux | |
| k9s | ~/.config/k9s/config.yaml | file | no | no | linux | |
| k9s | ~/.config/k9s/skins | folder | no | no | linux | |
| kitty | ~/.config/kitty/kitty.conf | file | no | no | any | |
| kube | ~/.kube/config | file | yes | no | any | |
| lazygit | ~/.config/lazygit/config.yml | file | no | no | linux | |
| lazygit | ~/Library/Application Support/lazygit/config.yml | file | no | no | mac | |
| maven | ~/.m2/settings.xml | file | yes | no | any | |
| mise | ~/.config/mise/config.toml | file | no | no | any | |
| neovim | ~/.config/nvim | folder | no | no | any | |
| nix | ~/.config/nix/nix.conf | file | no | no | any | |
| npm | ~/.npmrc | file | yes | no | any | |
| nushell | ~/.config/nushell/config.nu | file | no | no | linux | |
| nushell | ~/.config/nushell/env.nu | file | no | no | linux | |
| oh-my-zsh | ~/.oh-my-zsh/custom | folder | no | no | any | |
| opencode | ~/.config/opencode/opencode.json | file | no | no | any | |
| opencode | ~/.config/opencode/opencode.jsonc | file | no | no | any | |
| pip | ~/.config/pip/pip.conf | file | no | no | linux | |
| pip | ~/.pypirc | file | yes | no | any | |
| pnpm | ~/.config/pnpm/rc | file | yes | no | linux | M |
| podman | ~/.config/containers/registries.conf | file | no | no | linux | |
| poetry | ~/.config/pypoetry/config.toml | file | no | no | linux | |
| postgres | ~/.pgpass | file | yes | no | any | |
| postgres | ~/.psqlrc | file | no | no | any | |
| powerlevel10k | ~/.p10k.zsh | file | no | no | any | |
| ranger | ~/.config/ranger/rc.conf | file | no | no | any | |
| ranger | ~/.config/ranger/rifle.conf | file | no | no | any | |
| rclone | ~/.config/rclone/rclone.conf | file | yes | no | any | |
| ripgrep | ~/.ripgreprc | file | no | no | any | |
| ruby | ~/.gem/credentials | file | yes | no | any | |
| ruby | ~/.gemrc | file | no | no | any | |
| screen | ~/.screenrc | file | no | no | any | |
| sops | ~/.config/sops/age/keys.txt | file | yes | no | linux | M |
| ssh | ~/.ssh/config | file | no | no | any | |
| ssh | ~/.ssh/config.d | folder | no | no | any | M |
| ssh | ~/.ssh/id_* (not *.pub) | file | yes | no | any | |
| ssh | ~/.ssh/id_*.pub | file | no | no | any | |
| starship | ~/.config/starship.toml | file | no | no | any | |
| terraform | ~/.terraform.d/credentials.tfrc.json | file | yes | no | any | |
| terraform | ~/.terraformrc | file | no | no | linux | |
| tmux | ~/.config/tmux/tmux.conf | file | no | no | any | |
| tmux | ~/.tmux.conf | file | no | no | any | |
| uv | ~/.config/uv/uv.toml | file | no | no | any | |
| vim | ~/.vim/colors | folder | no | no | any | |
| vim | ~/.vim/ftplugin | folder | no | no | any | |
| vim | ~/.vimrc | file | no | no | any | |
| vscode | ~/.config/Code/User/keybindings.json | file | no | no | linux | |
| vscode | ~/.config/Code/User/mcp.json | file | yes | no | linux | M |
| vscode | ~/.config/Code/User/prompts | folder | no | no | linux | |
| vscode | ~/.config/Code/User/settings.json | file | no | no | linux | |
| vscode | ~/.config/Code/User/snippets | folder | no | no | linux | |
| vscode | ~/.config/Code/User/tasks.json | file | no | no | linux | |
| vscode | ~/Library/Application Support/Code/User/keybindings.json | file | no | no | mac | |
| vscode | ~/Library/Application Support/Code/User/prompts | folder | no | no | mac | |
| vscode | ~/Library/Application Support/Code/User/settings.json | file | no | no | mac | |
| vscode | ~/Library/Application Support/Code/User/snippets | folder | no | no | mac | |
| vscode-insiders | ~/.config/Code - Insiders/User/keybindings.json | file | no | no | linux | |
| vscode-insiders | ~/.config/Code - Insiders/User/prompts | folder | no | no | linux | |
| vscode-insiders | ~/.config/Code - Insiders/User/settings.json | file | no | no | linux | |
| vscode-insiders | ~/.config/Code - Insiders/User/snippets | folder | no | no | linux | |
| wakatime | ~/.wakatime.cfg | file | yes | no | any | |
| wezterm | ~/.config/wezterm/wezterm.lua | file | no | no | any | |
| wget | ~/.wgetrc | file | no | no | any | |
| yarn | ~/.yarnrc.yml | file | yes | no | any | |
| yazi | ~/.config/yazi | folder | no | no | any | |
| zed | ~/.config/zed/keymap.json | file | no | no | any | |
| zed | ~/.config/zed/settings.json | file | no | no | any | |
| zellij | ~/.config/zellij/config.kdl | file | no | no | any | |
| zsh | ~/.zprofile | file | no | no | any | |
| zsh | ~/.zshenv | file | no | no | any | |
| zsh | ~/.zshrc | file | no | no | any | |

The `skills` folders of claude, codex and copilot are not here: `ws skill install` links skills
there one by one (HANDBOOK `ws skill`), and a whole-folder entry would swallow those links.
Do not add pnpm's `auth.ini` or `config.yaml`:
they can hold tokens. `sops` and `pnpm` stay Linux-only.

Left out on purpose: `btop.conf` and `htoprc` (the tools rewrite them on save and replace the
link; see the ADR, "Tools that rewrite their file"). The user's own links (`agent-tmux`,
`stratus-upload`, `Screenshots`) are not config files; they go through `add <host> <ws-path>`.

## Never suggested (a test enforces this)

A catalog test fails if any entry's path equals, is inside, or is a parent of one of these.
Whole folders on the left are never listed; only the children in the table above are.

| path | why |
| --- | --- |
| ~/.ssh (whole), ~/.ssh/known_hosts*, ~/.ssh/authorized_keys | mixes keys, sockets and per-machine trust; `authorized_keys` widens who can log in |
| ~/.claude (whole), ~/.claude/.credentials.json, ~/.claude.json | credentials, history, sessions, caches |
| ~/.config/Code (whole), Code - Insiders, Cursor (whole) | state, caches and account tokens in `globalStorage` |
| ~/.gnupg (whole), private-keys-v1.d, trustdb.gpg | private keys and sockets |
| ~/.password-store | `pass` owns it and syncs through its own git remote |
| ~/.docker (whole), ~/.config/gh (whole), ~/.config/gcloud (whole), ~/.aws (whole), ~/.kube (whole) | tokens and caches beside the listed files |
| ~/.copilot (whole), ~/.continue (whole), ~/.codex (whole), ~/.gemini (whole) | history, databases and tokens beside the listed files |
| ~/.codex/auth.json, ~/.gemini/oauth_creds.json | tokens |
| *history* (bash, zsh, python, less, viminfo, node, fish) | leaks secrets, rewritten constantly, per machine |
| ~/.config/atuin/key, ~/.local/share/atuin | history and the sync encryption key |
| ~/.cache, ~/.npm, ~/.cargo/registry, ~/.rustup, ~/.m2/repository, ~/.gradle (whole), ~/.local/share | caches and toolchains |
| browser profiles (~/.mozilla, ~/.config/chromium, google-chrome*, BraveSoftware) | cookies and sessions |
| ~/.vim (whole), ~/.emacs.d (whole), ~/.oh-my-zsh (whole) | plugin checkouts and state |
| ~/.zcompdump*, ~/.zsh_sessions | regenerated or per machine |
| /etc/passwd, /etc/shadow, /etc/fstab, /etc/sudoers*, /etc/ssh/* | per machine, or dangerous to symlink |
| ~/.Xauthority, ~/.ICEauthority, ~/.pam_environment | per-machine auth or session state |
| *.bak*, *.ws-backup, *.pre-restore*, *.tmp, *.swp | backup and temp debris |
