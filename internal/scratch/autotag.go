package scratch

import (
	"bufio"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// autoTagRules maps file-level signals to suggested tags.
var autoTagRules = []struct {
	MatchFn func(name string, firstLine string) bool
	Tag     string
}{
	// File extensions.
	{matchExt(".sh"), "bash"},
	{matchExt(".bash"), "bash"},
	{matchExt(".py"), "python"},
	{matchExt(".go"), "go"},
	{matchExt(".rs"), "rust"},
	{matchExt(".js"), "javascript"},
	{matchExt(".ts"), "typescript"},
	{matchExt(".tf"), "terraform"},
	{matchExt(".hcl"), "terraform"},
	{matchExt(".sql"), "sql"},

	// Known file names.
	{matchName("Dockerfile"), "docker"},
	{matchName("docker-compose.yml"), "docker"},
	{matchName("docker-compose.yaml"), "docker"},
	{matchName("Makefile"), "make"},
	{matchName("Vagrantfile"), "vagrant"},
	{matchName("Jenkinsfile"), "jenkins"},

	// Shebangs (checked via firstLine).
	{matchShebang("bash"), "bash"},
	{matchShebang("python"), "python"},
	{matchShebang("node"), "node"},
	{matchShebang("ruby"), "ruby"},
	{matchShebang("perl"), "perl"},
}

// contentPatterns maps content keywords to tags.
// Checked against all text file lines (first 100 lines per file).
var contentPatterns = []struct {
	Keyword string
	Tag     string
}{
	{"kubectl", "k8s"},
	{"kubernetes", "k8s"},
	{"apiVersion", "k8s"},
	{"docker", "docker"},
	{"iptables", "networking"},
	{"nftables", "networking"},
	{"tcpdump", "networking"},
	{"curl", "networking"},
	{"systemctl", "systemd"},
	{"journalctl", "systemd"},
	{"cgroup", "cgroups"},
	{"OOMKill", "oom"},
	{"oom_kill", "oom"},
	{"terraform", "terraform"},
	{"ansible", "ansible"},
	{"git clone", "git"},
	{"git push", "git"},
	{"pip install", "python"},
	{"go build", "go"},
	{"cargo build", "rust"},
	{"npm install", "node"},
	{"apt-get", "apt"},
	{"yum install", "yum"},
	{"dnf install", "dnf"},
	{"helm", "helm"},
	{"prometheus", "prometheus"},
	{"grafana", "grafana"},
}

// AutoTag scans a scratch directory and returns suggested tags based on
// file names, shebangs, and content patterns.
func AutoTag(scratchDir string) ([]string, error) {
	seen := make(map[string]struct{})
	const maxFiles = 100
	visited := 0

	err := filepath.WalkDir(scratchDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() {
			return nil
		}
		if d.Name() == metaFile {
			return nil
		}
		if visited >= maxFiles {
			return filepath.SkipAll
		}
		visited++

		name := d.Name()
		firstLine := readFirstLine(path)

		// Check file-level rules.
		for _, rule := range autoTagRules {
			if rule.MatchFn(name, firstLine) {
				seen[rule.Tag] = struct{}{}
			}
		}

		// Check content patterns (first 100 lines).
		scanContentPatterns(path, seen)
		return nil
	})
	if err != nil {
		return nil, err
	}

	tags := make([]string, 0, len(seen))
	for t := range seen {
		tags = append(tags, t)
	}
	sort.Strings(tags)
	return tags, nil
}

func matchExt(ext string) func(string, string) bool {
	return func(name, _ string) bool {
		return strings.HasSuffix(strings.ToLower(name), ext)
	}
}

func matchName(target string) func(string, string) bool {
	lower := strings.ToLower(target)
	return func(name, _ string) bool {
		return strings.ToLower(name) == lower
	}
}

func matchShebang(keyword string) func(string, string) bool {
	return func(_, firstLine string) bool {
		return strings.HasPrefix(firstLine, "#!") && strings.Contains(firstLine, keyword)
	}
}

func readFirstLine(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	if scanner.Scan() {
		return scanner.Text()
	}
	return ""
}

func scanContentPatterns(path string, seen map[string]struct{}) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	lines := 0
	for scanner.Scan() {
		lines++
		if lines > 100 {
			break
		}
		line := scanner.Text()
		for _, cp := range contentPatterns {
			if strings.Contains(line, cp.Keyword) {
				seen[cp.Tag] = struct{}{}
			}
		}
	}
}
