package secret

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// CredentialRequest holds the key=value pairs git sends on stdin.
type CredentialRequest struct {
	Protocol string
	Host     string
	Path     string
	Username string
}

// CredentialResponse holds the key=value pairs we return on stdout.
type CredentialResponse struct {
	Username string
	Password string
}

// ParseCredentialInput reads git credential protocol from r.
// Format: key=value lines terminated by a blank line or EOF.
func ParseCredentialInput(r io.Reader) CredentialRequest {
	var req CredentialRequest
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			break
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch k {
		case "protocol":
			req.Protocol = v
		case "host":
			req.Host = v
		case "path":
			req.Path = v
		case "username":
			req.Username = v
		}
	}
	return req
}

// FormatCredentialOutput writes a CredentialResponse in git credential format.
func FormatCredentialOutput(w io.Writer, resp CredentialResponse) {
	if resp.Password != "" {
		fmt.Fprintf(w, "password=%s\n", resp.Password)
	}
	if resp.Username != "" {
		fmt.Fprintf(w, "username=%s\n", resp.Username)
	}
}

// LookupCredential attempts to find credentials in the pass store for the
// given request. It tries specific path-based entries first, then falls back
// to host-only entries.
//
// Lookup order:
//  1. git/<host>/<path>  (if path is non-empty)
//  2. git/<host>
//
// Returns empty response if nothing is found.
func LookupCredential(req CredentialRequest) CredentialResponse {
	if req.Host == "" {
		return CredentialResponse{}
	}

	// Clean the path: strip trailing .git suffix.
	path := strings.TrimSuffix(req.Path, ".git")
	path = strings.TrimSuffix(path, "/")

	var candidates []string
	if path != "" {
		candidates = append(candidates, "git/"+req.Host+"/"+path)
	}
	candidates = append(candidates, "git/"+req.Host)

	for _, entry := range candidates {
		resp, ok := tryPassEntry(entry)
		if ok {
			return resp
		}
	}

	return CredentialResponse{}
}

// tryPassEntry runs `pass show <entry>` and parses the output.
// Returns (response, true) on success, (empty, false) on failure.
func tryPassEntry(entry string) (CredentialResponse, bool) {
	out, err := runPassShow(entry)
	if err != nil {
		return CredentialResponse{}, false
	}
	return ParsePassEntry(out), true
}

// ParsePassEntry parses standard pass entry format:
//
//	<password>
//	username: <user>
//	(other metadata lines ignored)
func ParsePassEntry(content string) CredentialResponse {
	var resp CredentialResponse
	lines := strings.Split(content, "\n")
	if len(lines) == 0 {
		return resp
	}

	resp.Password = strings.TrimRight(lines[0], "\r\n\t ")

	for _, line := range lines[1:] {
		lower := strings.ToLower(strings.TrimSpace(line))
		for _, prefix := range []string{"username: ", "user: ", "login: ", "username=", "user=", "login="} {
			if strings.HasPrefix(lower, prefix) {
				// Use the original case for the value.
				resp.Username = strings.TrimSpace(line[len(prefix):])
				return resp
			}
		}
	}

	return resp
}

// PassEntryExists checks whether a pass entry exists without decrypting it.
// It checks for the .gpg file in the store directory.
func PassEntryExists(entry string) bool {
	h := CheckPass()
	if !h.Initialized {
		return false
	}
	gpgFile := filepath.Join(h.StorePath, entry+".gpg")
	return fileExists(gpgFile)
}

// runPassShow executes `pass show <entry>` and returns the decrypted content.
func runPassShow(entry string) (string, error) {
	cmd := exec.Command("pass", "show", entry)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("pass show %s: %w", entry, err)
	}
	return string(out), nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
