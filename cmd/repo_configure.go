package cmd

import (
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/mugenkunou/ws-tool/internal/repo"
)

// runCredentialEnable runs `ws credential enable`. Tests replace it.
var runCredentialEnable func(e *repoEnv) error

func init() {
	runCredentialEnable = func(e *repoEnv) error {
		args := []string{"--workspace", e.workspace, "--config", e.configPath, "credential", "enable"}
		if code := Execute(args, e.stdin, e.stdout, e.stderr); code != 0 {
			return fmt.Errorf("ws credential enable exited %d", code)
		}
		return nil
	}
}

// configurer asks the questions of `ws repo configure` and collects the
// answers as a plan.
type configurer struct {
	e       *repoEnv
	actions []Action
	eof     bool

	// Answers that apply to every repo are asked once.
	globalName, globalEmail string // identity chosen for all repos
	askedDefaultBranch      bool
	askedCredentials        bool
}

// ask prints a prompt and reads one trimmed line. At end of input it returns
// "" and stops further questions.
func (c *configurer) ask(prompt string) string {
	if c.eof {
		return ""
	}
	fmt.Fprint(c.e.stdout, prompt+" ")
	line, err := readLine(c.e.stdin)
	if err != nil {
		c.eof = true
		fmt.Fprintln(c.e.stdout)
		return ""
	}
	return strings.TrimSpace(line)
}

// askYN asks a yes/no question; the prompt carries its own [Y/n] or [y/N].
func (c *configurer) askYN(prompt string, def bool) bool {
	switch strings.ToLower(c.ask(prompt)) {
	case "y", "yes":
		return true
	case "n", "no":
		return false
	}
	return def && !c.eof
}

func (c *configurer) add(id, desc string, run func() error) {
	c.actions = append(c.actions, Action{ID: id, Description: desc, Execute: run})
}

func repoConfigure(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	var filters repoFilterFlags
	fs := flag.NewFlagSet("repo-configure", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	registerRepoFilterFlags(fs, &filters)
	registerGlobalFlags(fs, &globals)
	if err := parseInterspersed(fs, args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if err := filters.resolve(); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if globals.quiet || globals.json || !repoInteractive(stdin, stdout) {
		fmt.Fprintln(stderr, "ws repo configure needs a terminal")
		return 1
	}
	e, ok := loadRepoEnv(globals, stdin, stdout, stderr)
	if !ok {
		return 1
	}
	repos, ok := e.targets(fs.Args(), filters)
	if !ok {
		return 1
	}
	statuses := e.scan(repos, filters)
	findings := repo.Doctor(e.workspace, pathsOf(statuses), repo.DoctorOptions{})
	entries := repo.Entries(statuses, findings)

	// Only repos with a finding have anything to ask.
	c := &configurer{e: e}
	for _, en := range entries {
		if len(en.Findings) > 0 && !c.eof {
			c.askRepo(en)
		}
	}
	if len(entries) == 0 {
		fmt.Fprintln(stdout, "No repositories found.")
		return 0
	}
	if len(c.actions) == 0 {
		fmt.Fprintln(stdout, "Nothing to configure.")
		return 0
	}
	fmt.Fprintln(stdout)
	planResult := RunPlan(Plan{Command: "repo.configure", Actions: c.actions}, stdin, stdout, globals)
	return planResult.ExitCode()
}

// askRepo asks the questions one repo's findings need.
func (c *configurer) askRepo(en repo.Entry) {
	has := func(check string, min repo.Severity) bool {
		for _, f := range en.Findings {
			if f.Check == check && f.Severity >= min {
				return true
			}
		}
		return false
	}
	path := en.Status.Path
	fmt.Fprintf(c.e.stdout, "\n%s\n", path)
	facts := repo.ReadFacts(path)

	if has("identity", repo.SeverityWarn) {
		c.askIdentity(path, facts)
	}
	if has("upstream", repo.SeverityWarn) && !en.Status.Detached && en.Status.Error == "" {
		c.askUpstream(path, facts)
	}
	if has("default-branch", repo.SeverityInfo) && !c.askedDefaultBranch {
		c.askedDefaultBranch = true
		answer := c.ask("Default branch for new repos [main]:")
		if answer == "" && !c.eof {
			answer = "main"
		}
		if answer != "" {
			c.add("config-global-init.defaultBranch", fmt.Sprintf("Set init.defaultBranch to %q for all repos (git config --global)", answer),
				func() error { return repo.SetConfig(path, true, "init.defaultBranch", answer) })
		}
	}
	if has("credentials", repo.SeverityInfo) && !c.askedCredentials {
		c.askedCredentials = true
		if c.askYN("Use ws (pass) as git's credential helper? [y/N]", false) {
			c.add("credential-enable", "Enable ws as git's credential helper (ws credential enable)",
				func() error { return runCredentialEnable(c.e) })
		}
	}
	if has("fetch-staleness", repo.SeverityInfo) && len(facts.Remotes) > 0 {
		if c.askYN("Fetch now? [Y/n]", true) {
			c.add("fetch-"+path, "Fetch "+path, func() error {
				if r := repo.FetchOne(c.e.workspace, repo.Repository{Path: path}); !r.Success {
					return fmt.Errorf("%s", oneLine(r.Error))
				}
				return nil
			})
		}
	}
}

// askIdentity asks for the name and email that are missing, with nothing
// suggested, then whether they go in every repo or only this one.
func (c *configurer) askIdentity(path string, f repo.Facts) {
	needName := f.LocalName == "" && f.GlobalName == "" && c.globalName == ""
	needEmail := f.LocalEmail == "" && f.GlobalEmail == "" && c.globalEmail == ""
	if !needName && !needEmail {
		return
	}
	var name, email string
	if needName {
		name = c.ask("Name:")
	}
	if needEmail {
		email = c.ask("Email:")
	}
	if name == "" && email == "" {
		return
	}
	global := f.GlobalName == "" && f.GlobalEmail == ""
	switch strings.ToLower(c.ask("For all repos (global) or only this one? [g/l]")) {
	case "g", "global":
		global = true
	case "l", "local":
		global = false
	}
	set := func(key, value string) {
		if value == "" {
			return
		}
		if global {
			c.add("config-global-"+key, fmt.Sprintf("Set %s to %q for all repos (git config --global)", key, value),
				func() error { return repo.SetConfig(path, true, key, value) })
			return
		}
		c.add("config-local-"+key+"-"+path, fmt.Sprintf("Set %s to %q in %s", key, value, path),
			func() error { return repo.SetConfig(path, false, key, value) })
	}
	set("user.name", name)
	set("user.email", email)
	if global {
		if name != "" {
			c.globalName = name
		}
		if email != "" {
			c.globalEmail = email
		}
	}
}

// askUpstream asks how the current branch gets an upstream.
func (c *configurer) askUpstream(path string, f repo.Facts) {
	branch := f.Branch
	if branch == "" || branch == "HEAD" {
		return
	}
	switch {
	case f.HasRemote("origin") && f.OriginHasBranch:
		if c.askYN(fmt.Sprintf("Track origin/%s? [Y/n]", branch), true) {
			c.add("upstream-"+path, fmt.Sprintf("Track origin/%s in %s", branch, path),
				func() error { return repo.TrackOrigin(path, branch) })
		}
	case f.HasRemote("origin"):
		if c.askYN(fmt.Sprintf("Push %s to origin and track it? [y/N]", branch), false) {
			c.add("upstream-"+path, fmt.Sprintf("Push %s to origin and track it in %s", branch, path),
				func() error { return repo.PushUpstream(path, branch) })
		}
	default:
		if url := c.ask("Remote URL (blank to skip):"); url != "" {
			c.add("remote-"+path, fmt.Sprintf("Add remote origin %s to %s", url, path),
				func() error { return repo.AddOrigin(path, url) })
		}
	}
}
