package agenttmux

// SetLatestURL points the release check at a test server until the returned
// func restores it.
func SetLatestURL(u string) (restore func()) {
	old := latestURL
	latestURL = u
	return func() { latestURL = old }
}

// CachePath is the release cache file.
func CachePath() (string, error) { return cachePath() }
