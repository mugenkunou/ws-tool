package secret

import "strings"

// ResolveGitToken tries to find a token for a git remote via pass: first the
// explicit passEntry (if any), then the auto-derived "git/<host>" entry.
// Returns "" when no token is available (non-fatal).
func ResolveGitToken(remoteURL, passEntry string) string {
	if passEntry != "" {
		if tok, ok := passToken(passEntry); ok {
			return tok
		}
	}
	if host := HostFromGitURL(remoteURL); host != "" {
		if tok, ok := passToken("git/" + host); ok {
			return tok
		}
	}
	return ""
}

// passToken returns the password (token) for entry if pass can resolve it.
func passToken(entry string) (string, bool) {
	resp := LookupCredential(CredentialRequest{Protocol: "https", Host: entry})
	if resp.Password != "" {
		return resp.Password, true
	}
	return "", false
}

// HostFromGitURL extracts the hostname from an HTTPS or SSH git URL.
func HostFromGitURL(rawURL string) string {
	rawURL = strings.TrimSpace(rawURL)
	if idx := strings.Index(rawURL, "://"); idx >= 0 {
		rest := rawURL[idx+3:]
		if slashIdx := strings.Index(rest, "/"); slashIdx >= 0 {
			return rest[:slashIdx]
		}
		return rest
	}
	if strings.HasPrefix(rawURL, "git@") {
		if colonIdx := strings.Index(rawURL, ":"); colonIdx >= 0 {
			return rawURL[4:colonIdx]
		}
	}
	return ""
}
