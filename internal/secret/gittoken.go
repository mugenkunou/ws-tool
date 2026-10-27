package secret

import (
	"bytes"
	"net/url"
	"os"
	"os/exec"
	"strings"
)

// ResolveGitToken finds the token for a git remote: the explicit passEntry if
// it has one, else whatever `git credential fill` returns. Asking git means
// the lookup runs through the same helper (and so the same entry, down to
// git/<host>/<owner>/<repo>) that a push to the remote would use.
// Returns "" when no token is available (non-fatal).
func ResolveGitToken(remoteURL, passEntry string) string {
	if passEntry != "" {
		if tok, ok := passToken(passEntry); ok {
			return tok
		}
	}
	return gitCredentialToken(remoteURL)
}

// passToken returns the password (token) of the pass entry named entry, if
// pass can show it. The entry is read as it is: LookupCredential would prefix
// it with git/ again and look for git/git/<host>.
func passToken(entry string) (string, bool) {
	resp, ok := tryPassEntry(entry)
	if ok && resp.Password != "" {
		return resp.Password, true
	}
	return "", false
}

// gitCredentialToken runs `git credential fill` for an HTTPS remote. It never
// prompts: a missing credential is "", the same as no helper at all. SSH and
// other remotes have no token to fill.
func gitCredentialToken(remoteURL string) string {
	u, err := url.Parse(strings.TrimSpace(remoteURL))
	if err != nil || u.Scheme != "https" || u.Hostname() == "" {
		return ""
	}
	var in bytes.Buffer
	in.WriteString("protocol=https\nhost=" + u.Host + "\n")
	if p := strings.Trim(u.Path, "/"); p != "" {
		in.WriteString("path=" + p + "\n")
	}
	in.WriteString("\n")

	cmd := exec.Command("git", "-c", "credential.useHttpPath=true", "credential", "fill")
	cmd.Stdin = &in
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=true", "SSH_ASKPASS=true")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		if v, ok := strings.CutPrefix(line, "password="); ok {
			return strings.TrimRight(v, "\r")
		}
	}
	return ""
}
