package symlink

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// Kind says whether a catalog item is one file or one folder.
type Kind string

const (
	KindFile   Kind = "file"
	KindFolder Kind = "folder"
)

// OS values of a catalog item. "any" matches every machine.
const (
	OSAny   = "any"
	OSLinux = "linux"
	OSMac   = "mac"
)

// Item is one well-known config the binary can suggest (docs/plans/0003/catalog.md).
// Path uses ~ for $HOME; a * is a glob over one path segment. A Secret item starts
// unchecked in the checklist, and so does a Sudo item (it needs root to link).
type Item struct {
	App    string
	Path   string
	Kind   Kind
	Secret bool
	Sudo   bool
	OS     string
}

// Catalog is the built-in list of suggestions. It lives only in the binary: it is
// never written to a file and the user cannot edit it. It is sorted by app, then
// path. A row marked "M" is one whose path varies by version or setup.
//
// Mixed folders (~/.ssh, ~/.claude, ~/.config/Code, ~/.gnupg, ~/.docker) are never
// listed whole, because they hold known_hosts, history and credentials; only their
// safe children are. A test holds the list of paths that must never appear.
var Catalog = []Item{
	{"agents", "~/.agents/skills", KindFolder, false, false, OSAny}, // M: the path varies by version or setup
	{"aider", "~/.aider.conf.yml", KindFile, false, false, OSAny},
	{"alacritty", "~/.config/alacritty/alacritty.toml", KindFile, false, false, OSAny},
	{"ansible", "~/.ansible.cfg", KindFile, false, false, OSAny},
	{"asdf", "~/.tool-versions", KindFile, false, false, OSAny},
	{"atuin", "~/.config/atuin/config.toml", KindFile, false, false, OSAny},
	{"aws", "~/.aws/config", KindFile, false, false, OSAny},
	{"aws", "~/.aws/credentials", KindFile, true, false, OSAny},
	{"bash", "~/.bash_aliases", KindFile, false, false, OSLinux},
	{"bash", "~/.bash_profile", KindFile, false, false, OSAny},
	{"bash", "~/.bashrc", KindFile, false, false, OSAny},
	{"bash", "~/.inputrc", KindFile, false, false, OSAny},
	{"bash", "~/.profile", KindFile, false, false, OSAny},
	{"bat", "~/.config/bat/config", KindFile, false, false, OSAny},
	{"bat", "~/.config/bat/themes", KindFolder, false, false, OSAny},
	{"cargo", "~/.cargo/config.toml", KindFile, false, false, OSAny},
	{"cargo", "~/.cargo/credentials.toml", KindFile, true, false, OSAny},
	{"claude", "~/.claude/agents", KindFolder, false, false, OSAny},
	{"claude", "~/.claude/CLAUDE.md", KindFile, false, false, OSAny},
	{"claude", "~/.claude/commands", KindFolder, false, false, OSAny},
	{"claude", "~/.claude/rules", KindFolder, false, false, OSAny}, // M: the path varies by version or setup
	{"claude", "~/.claude/settings.json", KindFile, false, false, OSAny},
	{"claude", "~/.claude/skills", KindFolder, false, false, OSAny},
	{"codex", "~/.codex/AGENTS.md", KindFile, false, false, OSAny},
	{"codex", "~/.codex/config.toml", KindFile, false, false, OSAny},
	{"codex", "~/.codex/skills", KindFolder, false, false, OSAny}, // M: the Codex docs list only ~/.agents/skills; this older path is kept because ws only suggests paths that exist
	{"conda", "~/.condarc", KindFile, false, false, OSAny},
	{"continue", "~/.continue/.continuerc.json", KindFile, false, false, OSAny},
	{"continue", "~/.continue/config.yaml", KindFile, false, false, OSAny},
	{"copilot", "~/.copilot/agents", KindFolder, false, false, OSAny},
	{"copilot", "~/.copilot/mcp-config.json", KindFile, true, false, OSAny}, // M: the path varies by version or setup
	{"copilot", "~/.copilot/skills", KindFolder, false, false, OSAny},
	{"curl", "~/.netrc", KindFile, true, false, OSAny},
	{"cursor", "~/.config/Cursor/User/keybindings.json", KindFile, false, false, OSLinux},
	{"cursor", "~/.config/Cursor/User/settings.json", KindFile, false, false, OSLinux},
	{"cursor", "~/.config/Cursor/User/snippets", KindFolder, false, false, OSLinux},
	{"cursor", "~/.cursor/mcp.json", KindFile, true, false, OSAny},
	{"cursor", "~/Library/Application Support/Cursor/User/keybindings.json", KindFile, false, false, OSMac},
	{"cursor", "~/Library/Application Support/Cursor/User/settings.json", KindFile, false, false, OSMac},
	{"cursor", "~/Library/Application Support/Cursor/User/snippets", KindFolder, false, false, OSMac},
	{"direnv", "~/.config/direnv/direnv.toml", KindFile, false, false, OSAny},
	{"direnv", "~/.config/direnv/direnvrc", KindFile, false, false, OSAny},
	{"docker", "~/.docker/config.json", KindFile, true, false, OSAny},
	{"docker", "/etc/docker/daemon.json", KindFile, false, true, OSLinux},
	{"editorconfig", "~/.editorconfig", KindFile, false, false, OSAny},
	{"emacs", "~/.config/doom", KindFolder, false, false, OSAny},
	{"emacs", "~/.config/emacs/init.el", KindFile, false, false, OSAny},
	{"emacs", "~/.emacs.d/init.el", KindFile, false, false, OSAny},
	{"fish", "~/.config/fish/conf.d", KindFolder, false, false, OSAny},
	{"fish", "~/.config/fish/config.fish", KindFile, false, false, OSAny},
	{"fish", "~/.config/fish/fish_plugins", KindFile, false, false, OSAny},
	{"fish", "~/.config/fish/functions", KindFolder, false, false, OSAny},
	{"gcloud", "~/.config/gcloud/configurations", KindFolder, false, false, OSAny}, // M: the path varies by version or setup
	{"gemini", "~/.gemini/commands", KindFolder, false, false, OSAny},
	{"gemini", "~/.gemini/GEMINI.md", KindFile, false, false, OSAny},
	{"gemini", "~/.gemini/settings.json", KindFile, false, false, OSAny},
	{"gh", "~/.config/gh/config.yml", KindFile, false, false, OSAny},
	{"gh", "~/.config/gh/hosts.yml", KindFile, true, false, OSAny},
	{"ghostty", "~/.config/ghostty/config", KindFile, false, false, OSAny},
	{"git", "~/.config/git/config", KindFile, false, false, OSAny},
	{"git", "~/.config/git/ignore", KindFile, false, false, OSAny},
	{"git", "~/.git-credentials", KindFile, true, false, OSAny},
	{"git", "~/.gitconfig", KindFile, false, false, OSAny},
	{"git", "~/.gitignore_global", KindFile, false, false, OSAny},
	{"gnupg", "~/.gnupg/gpg-agent.conf", KindFile, false, false, OSAny},
	{"gnupg", "~/.gnupg/gpg.conf", KindFile, false, false, OSAny},
	{"gradle", "~/.gradle/gradle.properties", KindFile, true, false, OSAny},
	{"helix", "~/.config/helix/config.toml", KindFile, false, false, OSAny},
	{"helix", "~/.config/helix/languages.toml", KindFile, false, false, OSAny},
	{"helix", "~/.config/helix/themes", KindFolder, false, false, OSAny},
	{"helm", "~/.config/helm/repositories.yaml", KindFile, true, false, OSLinux},
	{"hosts", "/etc/hosts", KindFile, false, true, OSAny},
	{"ideavim", "~/.ideavimrc", KindFile, false, false, OSAny},
	{"jetbrains", "~/.config/JetBrains/*/keymaps", KindFolder, false, false, OSLinux},                   // M: the path varies by version or setup
	{"jetbrains", "~/Library/Application Support/JetBrains/*/keymaps", KindFolder, false, false, OSMac}, // M: the path varies by version or setup
	{"k9s", "~/.config/k9s/aliases.yaml", KindFile, false, false, OSLinux},
	{"k9s", "~/.config/k9s/config.yaml", KindFile, false, false, OSLinux},
	{"k9s", "~/.config/k9s/skins", KindFolder, false, false, OSLinux},
	{"kitty", "~/.config/kitty/kitty.conf", KindFile, false, false, OSAny},
	{"kube", "~/.kube/config", KindFile, true, false, OSAny},
	{"lazygit", "~/.config/lazygit/config.yml", KindFile, false, false, OSLinux},
	{"lazygit", "~/Library/Application Support/lazygit/config.yml", KindFile, false, false, OSMac},
	{"maven", "~/.m2/settings.xml", KindFile, true, false, OSAny},
	{"mise", "~/.config/mise/config.toml", KindFile, false, false, OSAny},
	{"neovim", "~/.config/nvim", KindFolder, false, false, OSAny},
	{"nix", "~/.config/nix/nix.conf", KindFile, false, false, OSAny},
	{"npm", "~/.npmrc", KindFile, true, false, OSAny},
	{"nushell", "~/.config/nushell/config.nu", KindFile, false, false, OSLinux},
	{"nushell", "~/.config/nushell/env.nu", KindFile, false, false, OSLinux},
	{"oh-my-zsh", "~/.oh-my-zsh/custom", KindFolder, false, false, OSAny},
	{"opencode", "~/.config/opencode/opencode.json", KindFile, false, false, OSAny},
	{"opencode", "~/.config/opencode/opencode.jsonc", KindFile, false, false, OSAny},
	{"pip", "~/.config/pip/pip.conf", KindFile, false, false, OSLinux},
	{"pip", "~/.pypirc", KindFile, true, false, OSAny},
	{"pnpm", "~/.config/pnpm/rc", KindFile, true, false, OSLinux}, // M: the path varies by version or setup
	{"podman", "~/.config/containers/registries.conf", KindFile, false, false, OSLinux},
	{"poetry", "~/.config/pypoetry/config.toml", KindFile, false, false, OSLinux},
	{"postgres", "~/.pgpass", KindFile, true, false, OSAny},
	{"postgres", "~/.psqlrc", KindFile, false, false, OSAny},
	{"powerlevel10k", "~/.p10k.zsh", KindFile, false, false, OSAny},
	{"ranger", "~/.config/ranger/rc.conf", KindFile, false, false, OSAny},
	{"ranger", "~/.config/ranger/rifle.conf", KindFile, false, false, OSAny},
	{"rclone", "~/.config/rclone/rclone.conf", KindFile, true, false, OSAny},
	{"ripgrep", "~/.ripgreprc", KindFile, false, false, OSAny},
	{"ruby", "~/.gem/credentials", KindFile, true, false, OSAny},
	{"ruby", "~/.gemrc", KindFile, false, false, OSAny},
	{"screen", "~/.screenrc", KindFile, false, false, OSAny},
	{"sops", "~/.config/sops/age/keys.txt", KindFile, true, false, OSLinux}, // M: the path varies by version or setup
	{"ssh", "~/.ssh/config", KindFile, false, false, OSAny},
	{"ssh", "~/.ssh/config.d", KindFolder, false, false, OSAny}, // M: the path varies by version or setup
	{"ssh", "~/.ssh/id_*", KindFile, true, false, OSAny},
	{"ssh", "~/.ssh/id_*.pub", KindFile, false, false, OSAny},
	{"starship", "~/.config/starship.toml", KindFile, false, false, OSAny},
	{"terraform", "~/.terraform.d/credentials.tfrc.json", KindFile, true, false, OSAny},
	{"terraform", "~/.terraformrc", KindFile, false, false, OSLinux},
	{"tmux", "~/.config/tmux/tmux.conf", KindFile, false, false, OSAny},
	{"tmux", "~/.tmux.conf", KindFile, false, false, OSAny},
	{"uv", "~/.config/uv/uv.toml", KindFile, false, false, OSAny},
	{"vim", "~/.vim/colors", KindFolder, false, false, OSAny},
	{"vim", "~/.vim/ftplugin", KindFolder, false, false, OSAny},
	{"vim", "~/.vimrc", KindFile, false, false, OSAny},
	{"vscode", "~/.config/Code/User/keybindings.json", KindFile, false, false, OSLinux},
	{"vscode", "~/.config/Code/User/mcp.json", KindFile, true, false, OSLinux}, // M: the path varies by version or setup
	{"vscode", "~/.config/Code/User/prompts", KindFolder, false, false, OSLinux},
	{"vscode", "~/.config/Code/User/settings.json", KindFile, false, false, OSLinux},
	{"vscode", "~/.config/Code/User/snippets", KindFolder, false, false, OSLinux},
	{"vscode", "~/.config/Code/User/tasks.json", KindFile, false, false, OSLinux},
	{"vscode", "~/Library/Application Support/Code/User/keybindings.json", KindFile, false, false, OSMac},
	{"vscode", "~/Library/Application Support/Code/User/prompts", KindFolder, false, false, OSMac},
	{"vscode", "~/Library/Application Support/Code/User/settings.json", KindFile, false, false, OSMac},
	{"vscode", "~/Library/Application Support/Code/User/snippets", KindFolder, false, false, OSMac},
	{"vscode-insiders", "~/.config/Code - Insiders/User/keybindings.json", KindFile, false, false, OSLinux},
	{"vscode-insiders", "~/.config/Code - Insiders/User/prompts", KindFolder, false, false, OSLinux},
	{"vscode-insiders", "~/.config/Code - Insiders/User/settings.json", KindFile, false, false, OSLinux},
	{"vscode-insiders", "~/.config/Code - Insiders/User/snippets", KindFolder, false, false, OSLinux},
	{"wakatime", "~/.wakatime.cfg", KindFile, true, false, OSAny},
	{"wezterm", "~/.config/wezterm/wezterm.lua", KindFile, false, false, OSAny},
	{"wget", "~/.wgetrc", KindFile, false, false, OSAny},
	{"yarn", "~/.yarnrc.yml", KindFile, true, false, OSAny},
	{"yazi", "~/.config/yazi", KindFolder, false, false, OSAny},
	{"zed", "~/.config/zed/keymap.json", KindFile, false, false, OSAny},
	{"zed", "~/.config/zed/settings.json", KindFile, false, false, OSAny},
	{"zellij", "~/.config/zellij/config.kdl", KindFile, false, false, OSAny},
	{"zsh", "~/.zprofile", KindFile, false, false, OSAny},
	{"zsh", "~/.zshenv", KindFile, false, false, OSAny},
	{"zsh", "~/.zshrc", KindFile, false, false, OSAny},
}

// Apps is every app label in the catalog, sorted and without repeats: the values
// `ws symlink add --app` completes.
func Apps() []string {
	seen := map[string]bool{}
	var out []string
	for _, it := range Catalog {
		if !seen[it.App] {
			seen[it.App] = true
			out = append(out, it.App)
		}
	}
	sort.Strings(out)
	return out
}

// IsCatalogSecret reports whether a host path is one the catalog lists as a secret
// (an exact match or a glob match, for the machine's own $HOME). There is no
// heuristic: a path the catalog does not know is never called a secret here.
func IsCatalogSecret(host string) bool {
	home, _ := os.UserHomeDir()
	host = filepath.Clean(host)
	for _, it := range Catalog {
		if !it.Secret {
			continue
		}
		pat := it.Path
		if strings.HasPrefix(pat, "~/") {
			pat = filepath.Join(home, pat[2:])
		}
		if ok, _ := filepath.Match(pat, host); ok && !strings.HasSuffix(host, ".pub") {
			return true
		}
	}
	return false
}

// IsCatalogFolder reports whether a host path is a catalog row of kind folder, so
// `add` takes it as one entry (not one entry per file). Exact path match only, for
// the machine's own $HOME and OS.
func IsCatalogFolder(host string) bool {
	home, _ := os.UserHomeDir()
	host = filepath.Clean(host)
	for _, it := range catalogFor(runtime.GOOS) {
		if it.Kind != KindFolder {
			continue
		}
		pat := it.Path
		if strings.HasPrefix(pat, "~/") {
			pat = filepath.Join(home, pat[2:])
		}
		if ok, _ := filepath.Match(pat, host); ok {
			return true
		}
	}
	return false
}
