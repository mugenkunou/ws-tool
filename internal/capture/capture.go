package capture

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type Location struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Exists bool   `json:"exists"`
}

type PinOptions struct {
	CapturesFile string
	AssetsDir    string
	Topic        string
	DryRun       bool
	Amend        bool
}

type PinResult struct {
	Source      string `json:"source"`
	Topic       string `json:"topic"`
	File        string `json:"file"`
	Lines       int    `json:"lines"`
	Images      int    `json:"images"`
	WindowTitle string `json:"window_title,omitempty"`
	DryRun      bool   `json:"dry_run,omitempty"`
	Amended     bool   `json:"amended,omitempty"`
}

type Entry struct {
	Topic     string
	Timestamp time.Time
	Source    string
	Body      string
}

// Locations returns the full list of capture locations including the implicit default.
func Locations(wsDir string, configured map[string]string) []Location {
	defaultPath := filepath.Join(wsDir, "captures", "captures.md")
	locs := []Location{{
		Name:   "default",
		Path:   defaultPath,
		Exists: fileExists(defaultPath),
	}}
	for name, dir := range configured {
		if name == "default" {
			continue
		}
		p := filepath.Join(dir, "captures", "captures.md")
		locs = append(locs, Location{
			Name:   name,
			Path:   p,
			Exists: fileExists(p),
		})
	}
	return locs
}

// ResolveLocation returns the captures file path and assets dir for a given location name.
func ResolveLocation(wsDir string, configured map[string]string, name string) (capturesFile string, assetsDir string, err error) {
	if name == "" || name == "default" {
		capturesFile = filepath.Join(wsDir, "captures", "captures.md")
		assetsDir = filepath.Join(wsDir, "captures", "assets")
		return capturesFile, assetsDir, nil
	}
	dir, ok := configured[name]
	if !ok {
		return "", "", fmt.Errorf("unknown location: %q (configure it in capture.locations)", name)
	}
	capturesFile = filepath.Join(dir, "captures", "captures.md")
	assetsDir = filepath.Join(dir, "captures", "assets")
	return capturesFile, assetsDir, nil
}

// PinText appends a text entry to the captures file.
func PinText(content string, opts PinOptions) (PinResult, error) {
	if opts.Amend {
		return amendContent(content, opts)
	}
	topic := topicOrDerive(opts.Topic, content)
	lines := countLines(content)
	entry := formatEntry(topic, "stdin-pipe", content)
	if opts.DryRun {
		return PinResult{
			Source: "stdin-pipe",
			Topic:  topic,
			File:   opts.CapturesFile,
			Lines:  lines,
			DryRun: true,
		}, nil
	}
	if err := appendEntry(opts.CapturesFile, entry); err != nil {
		return PinResult{}, err
	}
	return PinResult{
		Source: "stdin-pipe",
		Topic:  topic,
		File:   opts.CapturesFile,
		Lines:  lines,
	}, nil
}

// PinClipboard reads clipboard content and appends to captures file.
func PinClipboard(opts PinOptions) (PinResult, error) {
	tool, format, err := detectClipboard()
	if err != nil {
		return PinResult{}, err
	}

	windowTitle := getWindowTitle()

	if opts.Amend {
		return pinClipboardAmend(tool, format, opts, windowTitle)
	}

	switch format {
	case "text/html":
		html, err := readClipboardFormat(tool, "text/html")
		if err != nil {
			return pinClipboardPlainText(tool, opts, windowTitle)
		}
		return pinClipboardHTML(html, opts, windowTitle)
	case "image/png":
		return pinClipboardImage(tool, opts, windowTitle)
	default:
		return pinClipboardPlainText(tool, opts, windowTitle)
	}
}

// GetDryRunPreview returns the formatted entry text for dry-run display.
func GetDryRunPreview(content, source string) string {
	topic := deriveTopic(content)
	return formatEntry(topic, source, content)
}

func pinClipboardPlainText(tool string, opts PinOptions, windowTitle string) (PinResult, error) {
	text, err := readClipboardText(tool)
	if err != nil {
		return PinResult{}, err
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return PinResult{}, errors.New("clipboard is empty")
	}
	topic := topicOrDerive(opts.Topic, text)
	lines := countLines(text)
	source := "clipboard-text"
	body := text
	if looksLikeCode(text) {
		lang := detectLanguageHint(text)
		body = "```" + lang + "\n" + text + "\n```"
	}
	entry := formatEntry(topic, source, body)
	if opts.DryRun {
		return PinResult{
			Source:      source,
			Topic:       topic,
			File:        opts.CapturesFile,
			Lines:       lines,
			WindowTitle: windowTitle,
			DryRun:      true,
		}, nil
	}
	if err := appendEntry(opts.CapturesFile, entry); err != nil {
		return PinResult{}, err
	}
	return PinResult{
		Source:      source,
		Topic:       topic,
		File:        opts.CapturesFile,
		Lines:       lines,
		WindowTitle: windowTitle,
	}, nil
}

func pinClipboardHTML(html string, opts PinOptions, windowTitle string) (PinResult, error) {
	md, imgURLs := htmlToMarkdown(html)
	md = strings.TrimSpace(md)
	if md == "" {
		return PinResult{}, errors.New("clipboard is empty")
	}

	images := 0
	if !opts.DryRun {
		if err := os.MkdirAll(opts.AssetsDir, 0o755); err != nil {
			return PinResult{}, err
		}
		// Save raw HTML as fallback for fidelity
		htmlName := assetFilename(time.Now(), 0, ".html")
		htmlPath := filepath.Join(opts.AssetsDir, htmlName)
		_ = os.WriteFile(htmlPath, []byte(html), 0o644)
	}
	if len(imgURLs) > 0 && !opts.DryRun {
		for i, url := range imgURLs {
			assetName := assetFilename(time.Now(), i+1, ".png")
			assetPath := filepath.Join(opts.AssetsDir, assetName)
			if err := downloadFile(url, assetPath); err != nil {
				continue
			}
			relPath := relativeAssetPath(opts.CapturesFile, assetPath)
			md = strings.Replace(md, url, relPath, 1)
			images++
		}
	} else {
		images = len(imgURLs)
	}

	topic := topicOrDerive(opts.Topic, md)
	lines := countLines(md)
	source := "clipboard-html"
	entry := formatEntry(topic, source, md)

	if opts.DryRun {
		return PinResult{
			Source:      source,
			Topic:       topic,
			File:        opts.CapturesFile,
			Lines:       lines,
			Images:      images,
			WindowTitle: windowTitle,
			DryRun:      true,
		}, nil
	}
	if err := appendEntry(opts.CapturesFile, entry); err != nil {
		return PinResult{}, err
	}
	return PinResult{
		Source:      source,
		Topic:       topic,
		File:        opts.CapturesFile,
		Lines:       lines,
		Images:      images,
		WindowTitle: windowTitle,
	}, nil
}

func pinClipboardImage(tool string, opts PinOptions, windowTitle string) (PinResult, error) {
	topic := topicOrDerive(opts.Topic, "[pinned image]")
	if opts.DryRun {
		return PinResult{
			Source:      "clipboard-image",
			Topic:       topic,
			File:        opts.CapturesFile,
			Images:      1,
			WindowTitle: windowTitle,
			DryRun:      true,
		}, nil
	}
	if err := os.MkdirAll(opts.AssetsDir, 0o755); err != nil {
		return PinResult{}, err
	}
	assetName := assetFilename(time.Now(), 1, ".png")
	assetPath := filepath.Join(opts.AssetsDir, assetName)
	if err := readClipboardImage(tool, assetPath); err != nil {
		return PinResult{}, fmt.Errorf("failed to read clipboard image: %w", err)
	}

	relPath := relativeAssetPath(opts.CapturesFile, assetPath)
	body := "![](" + relPath + ")"
	entry := formatEntry(topic, "clipboard-image", body)
	if err := appendEntry(opts.CapturesFile, entry); err != nil {
		return PinResult{}, err
	}
	return PinResult{
		Source:      "clipboard-image",
		Topic:       topic,
		File:        opts.CapturesFile,
		Images:      1,
		WindowTitle: windowTitle,
	}, nil
}

func pinClipboardAmend(tool, format string, opts PinOptions, windowTitle string) (PinResult, error) {
	switch format {
	case "image/png":
		if opts.DryRun {
			return PinResult{
				Source:      "amend",
				Topic:       "[last entry]",
				File:        opts.CapturesFile,
				Images:      1,
				WindowTitle: windowTitle,
				DryRun:      true,
				Amended:     true,
			}, nil
		}
		if err := os.MkdirAll(opts.AssetsDir, 0o755); err != nil {
			return PinResult{}, err
		}
		assetName := assetFilename(time.Now(), 1, ".png")
		assetPath := filepath.Join(opts.AssetsDir, assetName)
		if err := readClipboardImage(tool, assetPath); err != nil {
			return PinResult{}, fmt.Errorf("failed to read clipboard image: %w", err)
		}
		return amendImage(assetPath, opts)
	case "text/html":
		html, err := readClipboardFormat(tool, "text/html")
		if err != nil {
			// Fall through to plain text
			text, err2 := readClipboardText(tool)
			if err2 != nil {
				return PinResult{}, err2
			}
			text = strings.TrimSpace(text)
			if text == "" {
				return PinResult{}, errors.New("clipboard is empty")
			}
			return amendContent(text, opts)
		}
		md, _ := htmlToMarkdown(html)
		md = strings.TrimSpace(md)
		if md == "" {
			return PinResult{}, errors.New("clipboard is empty")
		}
		return amendContent(md, opts)
	default:
		text, err := readClipboardText(tool)
		if err != nil {
			return PinResult{}, err
		}
		text = strings.TrimSpace(text)
		if text == "" {
			return PinResult{}, errors.New("clipboard is empty")
		}
		return amendContent(text, opts)
	}
}

func formatEntry(topic, source, body string) string {
	var sb strings.Builder
	sb.WriteString("## ")
	sb.WriteString(topic)
	sb.WriteString("\n")
	sb.WriteString("_")
	sb.WriteString(time.Now().Format("2006-01-02 15:04"))
	sb.WriteString(" · ")
	sb.WriteString(source)
	sb.WriteString("_\n\n")
	sb.WriteString(body)
	sb.WriteString("\n")
	sb.WriteString("\n---\n")
	return sb.String()
}

func appendEntry(capturesFile, entry string) error {
	dir := filepath.Dir(capturesFile)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if !fileExists(capturesFile) {
		if err := os.WriteFile(capturesFile, []byte("# Captures\n\n"), 0o644); err != nil {
			return err
		}
	}
	f, err := os.OpenFile(capturesFile, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(entry)
	return err
}

// amendEntry inserts body content into the last entry of the captures file,
// just before the final "---" separator. Returns the topic of the amended entry.
func amendEntry(capturesFile, body string) (string, error) {
	data, err := os.ReadFile(capturesFile)
	if err != nil {
		return "", fmt.Errorf("cannot amend: %w", err)
	}
	content := string(data)

	// Find the last "---" separator
	lastSep := strings.LastIndex(content, "\n---\n")
	if lastSep < 0 {
		return "", errors.New("cannot amend: no existing entry found")
	}

	// Extract the topic from the last entry's "## " heading
	// Search backwards from the separator for the heading
	before := content[:lastSep]
	topicIdx := strings.LastIndex(before, "\n## ")
	topic := ""
	if topicIdx >= 0 {
		lineEnd := strings.Index(before[topicIdx+1:], "\n")
		if lineEnd >= 0 {
			topic = strings.TrimPrefix(before[topicIdx+1:topicIdx+1+lineEnd], "## ")
		} else {
			topic = strings.TrimPrefix(before[topicIdx+1:], "## ")
		}
	}

	// Insert body before the separator
	amended := content[:lastSep] + "\n\n" + body + "\n" + content[lastSep:]
	if err := os.WriteFile(capturesFile, []byte(amended), 0o644); err != nil {
		return "", err
	}
	return topic, nil
}

// amendContent appends body text to the last entry in the captures file.
func amendContent(body string, opts PinOptions) (PinResult, error) {
	if opts.DryRun {
		return PinResult{
			Source:  "amend",
			Topic:   "[last entry]",
			File:    opts.CapturesFile,
			Lines:   countLines(body),
			DryRun:  true,
			Amended: true,
		}, nil
	}
	topic, err := amendEntry(opts.CapturesFile, body)
	if err != nil {
		return PinResult{}, err
	}
	return PinResult{
		Source:  "amend",
		Topic:   topic,
		File:    opts.CapturesFile,
		Lines:   countLines(body),
		Amended: true,
	}, nil
}

// amendImage copies an image to assets and appends a markdown embed to the last entry.
func amendImage(imgPath string, opts PinOptions) (PinResult, error) {
	if opts.DryRun {
		return PinResult{
			Source:  "amend",
			Topic:   "[last entry]",
			File:    opts.CapturesFile,
			Images:  1,
			DryRun:  true,
			Amended: true,
		}, nil
	}
	relPath := relativeAssetPath(opts.CapturesFile, imgPath)
	body := "![](" + relPath + ")"
	topic, err := amendEntry(opts.CapturesFile, body)
	if err != nil {
		return PinResult{}, err
	}
	return PinResult{
		Source:  "amend",
		Topic:   topic,
		File:    opts.CapturesFile,
		Images:  1,
		Amended: true,
	}, nil
}

func deriveTopic(content string) string {
	lines := strings.SplitN(strings.TrimSpace(content), "\n", 2)
	if len(lines) == 0 || strings.TrimSpace(lines[0]) == "" {
		return "[captured]"
	}
	topic := strings.TrimSpace(lines[0])
	// Strip markdown heading prefix if present
	topic = strings.TrimLeft(topic, "# ")
	// Strip code fences
	topic = strings.TrimPrefix(topic, "```")
	topic = strings.TrimSpace(topic)
	if topic == "" {
		return "[captured]"
	}
	if len(topic) > 120 {
		topic = topic[:120]
	}
	return topic
}

func topicOrDerive(override, content string) string {
	if strings.TrimSpace(override) != "" {
		return strings.TrimSpace(override)
	}
	return deriveTopic(content)
}

func countLines(s string) int {
	if s == "" {
		return 0
	}
	n := strings.Count(s, "\n")
	if !strings.HasSuffix(s, "\n") {
		n++
	}
	return n
}

func looksLikeCode(s string) bool {
	lines := strings.SplitN(s, "\n", 10)
	codeIndicators := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "$") || strings.HasPrefix(trimmed, "#!") ||
			strings.Contains(trimmed, "()") || strings.Contains(trimmed, "= ") ||
			strings.HasPrefix(trimmed, "export ") || strings.HasPrefix(trimmed, "sudo ") ||
			strings.HasPrefix(trimmed, "kubectl ") || strings.HasPrefix(trimmed, "docker ") ||
			strings.HasPrefix(trimmed, "curl ") || strings.HasPrefix(trimmed, "rsync ") ||
			strings.HasPrefix(trimmed, "git ") || strings.HasPrefix(trimmed, "go ") {
			codeIndicators++
		}
	}
	return codeIndicators > 0
}

func detectLanguageHint(s string) string {
	first := strings.SplitN(s, "\n", 2)[0]
	first = strings.TrimSpace(first)
	if strings.HasPrefix(first, "#!/bin/bash") || strings.HasPrefix(first, "#!/bin/sh") ||
		strings.HasPrefix(first, "$ ") || strings.HasPrefix(first, "export ") ||
		strings.HasPrefix(first, "sudo ") {
		return "bash"
	}
	if strings.HasPrefix(first, "{") || strings.HasPrefix(first, "[") {
		return "json"
	}
	return ""
}

func assetFilename(t time.Time, seq int, ext string) string {
	return fmt.Sprintf("%s-%d%s", t.Format("2006-01-02-1504"), seq, ext)
}

func relativeAssetPath(capturesFile, assetPath string) string {
	dir := filepath.Dir(capturesFile)
	rel, err := filepath.Rel(dir, assetPath)
	if err != nil {
		return assetPath
	}
	return rel
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func downloadFile(url, dst string) error {
	cmd := exec.Command("curl", "-fsSL", "-o", dst, "--max-time", "10", url)
	if err := cmd.Run(); err != nil {
		return err
	}
	return validateImage(dst)
}

// validateImage checks that a file starts with known image magic bytes.
// If not, removes the file and returns an error.
func validateImage(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	var hdr [8]byte
	n, _ := f.Read(hdr[:])
	f.Close()
	if n < 4 {
		os.Remove(path)
		return errors.New("downloaded file too small to be an image")
	}
	switch {
	case hdr[0] == 0x89 && hdr[1] == 'P' && hdr[2] == 'N' && hdr[3] == 'G': // PNG
	case hdr[0] == 0xFF && hdr[1] == 0xD8: // JPEG
	case hdr[0] == 'G' && hdr[1] == 'I' && hdr[2] == 'F': // GIF
	case hdr[0] == 'R' && hdr[1] == 'I' && hdr[2] == 'F' && hdr[3] == 'F': // WEBP (RIFF)
	case hdr[0] == 'B' && hdr[1] == 'M': // BMP
	default:
		os.Remove(path)
		return errors.New("downloaded file is not a valid image")
	}
	return nil
}

// detectClipboard probes for available clipboard tools and returns
// the tool name and the richest available format.
func detectClipboard() (tool string, format string, err error) {
	// Try wl-paste only if it exists AND can talk to a Wayland compositor.
	if _, err := exec.LookPath("wl-paste"); err == nil {
		out, err := exec.Command("wl-paste", "--list-types").Output()
		if err == nil {
			types := string(out)
			if strings.Contains(types, "text/html") {
				return "wl-paste", "text/html", nil
			}
			if strings.Contains(types, "image/png") {
				return "wl-paste", "image/png", nil
			}
			if strings.Contains(types, "text/plain") || strings.Contains(types, "TEXT") || strings.Contains(types, "STRING") {
				return "wl-paste", "text/plain", nil
			}
			return "", "", errors.New("clipboard contains no supported content type")
		}
		// wl-paste failed (no Wayland session, empty clipboard, etc.) — fall through to xclip.
	}
	if _, err := exec.LookPath("xclip"); err == nil {
		out, err := exec.Command("xclip", "-selection", "clipboard", "-t", "TARGETS", "-o").Output()
		if err != nil {
			return "", "", errors.New("clipboard is empty (xclip found but no content available)")
		}
		targets := string(out)
		if strings.Contains(targets, "text/html") {
			return "xclip", "text/html", nil
		}
		if strings.Contains(targets, "image/png") {
			return "xclip", "image/png", nil
		}
		if strings.Contains(targets, "TEXT") || strings.Contains(targets, "STRING") || strings.Contains(targets, "text/plain") {
			return "xclip", "text/plain", nil
		}
		return "", "", errors.New("clipboard contains no supported content type")
	}
	return "", "", errors.New("clipboard tool not found: install xclip (X11) or wl-clipboard (Wayland)")
}

func readClipboardText(tool string) (string, error) {
	var cmd *exec.Cmd
	switch tool {
	case "wl-paste":
		cmd = exec.Command("wl-paste", "--no-newline")
	case "xclip":
		cmd = exec.Command("xclip", "-selection", "clipboard", "-o")
	default:
		return "", fmt.Errorf("unsupported clipboard tool: %s", tool)
	}
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to read clipboard: %w", err)
	}
	return string(out), nil
}

func readClipboardFormat(tool, format string) (string, error) {
	var cmd *exec.Cmd
	switch tool {
	case "wl-paste":
		cmd = exec.Command("wl-paste", "--no-newline", "--type", format)
	case "xclip":
		cmd = exec.Command("xclip", "-selection", "clipboard", "-t", format, "-o")
	default:
		return "", fmt.Errorf("unsupported clipboard tool: %s", tool)
	}
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to read clipboard format %s: %w", format, err)
	}
	return string(out), nil
}

func readClipboardImage(tool, dstPath string) error {
	var cmd *exec.Cmd
	switch tool {
	case "wl-paste":
		cmd = exec.Command("wl-paste", "--no-newline", "--type", "image/png")
	case "xclip":
		cmd = exec.Command("xclip", "-selection", "clipboard", "-t", "image/png", "-o")
	default:
		return fmt.Errorf("unsupported clipboard tool: %s", tool)
	}
	out, err := cmd.Output()
	if err != nil {
		return err
	}
	return os.WriteFile(dstPath, out, 0o644)
}

func getWindowTitle() string {
	if _, err := exec.LookPath("xdotool"); err != nil {
		return ""
	}
	out, err := exec.Command("xdotool", "getactivewindow", "getwindowname").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// stripHTMLChrome removes non-content HTML elements before markdown conversion.
// Strips navigation, scripts, styles, buttons, forms, and other UI chrome
// that produce noise when converted to markdown.
func stripHTMLChrome(s string) string {
	// Remove entire elements (including content) for tags that never contain
	// user-meaningful text.
	for _, tag := range []string{
		"script", "style", "nav", "footer", "header",
		"button", "svg", "form", "select", "option",
		"noscript", "iframe", "object", "embed",
	} {
		s = removeElementFull(s, tag)
	}
	// Remove common tracking/decoration images: 1x1 pixels, empty src, data-URIs
	s = removeJunkImages(s)
	return s
}

// removeElementFull strips all occurrences of <tag ...>...</tag> including content.
func removeElementFull(s, tag string) string {
	lower := strings.ToLower(s)
	open := "<" + tag
	close := "</" + tag + ">"
	for {
		idx := strings.Index(lower, open)
		if idx < 0 {
			break
		}
		// Verify it's a real tag start (not e.g. <styles)
		if idx+len(open) < len(lower) {
			next := lower[idx+len(open)]
			if next != '>' && next != ' ' && next != '\t' && next != '\n' {
				// Not actually this tag — skip past
				lower = lower[:idx] + strings.Repeat("x", len(open)) + lower[idx+len(open):]
				continue
			}
		}
		endIdx := strings.Index(lower[idx:], close)
		if endIdx < 0 {
			// No close tag — remove just the open tag up to >
			gt := strings.Index(s[idx:], ">")
			if gt < 0 {
				break
			}
			s = s[:idx] + s[idx+gt+1:]
			lower = strings.ToLower(s)
			continue
		}
		s = s[:idx] + s[idx+endIdx+len(close):]
		lower = strings.ToLower(s)
	}
	return s
}

// removeJunkImages strips <img> tags that are tracking pixels, empty, or decorative.
func removeJunkImages(s string) string {
	var result strings.Builder
	for {
		idx := strings.Index(strings.ToLower(s), "<img")
		if idx < 0 {
			result.WriteString(s)
			break
		}
		result.WriteString(s[:idx])
		end := strings.Index(s[idx:], ">")
		if end < 0 {
			result.WriteString(s[idx:])
			break
		}
		tag := s[idx : idx+end+1]
		src := extractAttr(tag, "src")
		width := extractAttr(tag, "width")
		height := extractAttr(tag, "height")

		junk := false
		if src == "" {
			junk = true
		} else if strings.HasPrefix(src, "data:") {
			junk = true
		} else if (width == "1" || width == "0") && (height == "1" || height == "0") {
			junk = true
		}

		if junk {
			// Drop the tag entirely
		} else {
			result.WriteString(tag)
		}
		s = s[idx+end+1:]
	}
	return result.String()
}

// cleanMarkdownNoise removes noise lines from converted markdown:
// bare punctuation list items, lone numbers, empty list items, and
// other artifacts from UI chrome.
func cleanMarkdownNoise(s string) string {
	lines := strings.Split(s, "\n")
	var out []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		// Drop list items that are just punctuation or tiny noise
		if strings.HasPrefix(trimmed, "- ") {
			content := strings.TrimSpace(trimmed[2:])
			if isNoiseLine(content) {
				continue
			}
		}
		// Drop standalone noise (not in a list)
		if isNoiseLine(trimmed) {
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// isNoiseLine returns true for content that is just UI chrome debris:
// bare punctuation, single dots, lone small numbers, "Like", "Reply", etc.
func isNoiseLine(s string) bool {
	if s == "" {
		return false // empty lines are handled by the whitespace cleanup
	}
	// Exact matches for common UI actions
	switch s {
	case ".", "..", "...", "|", "·", "•", "-", "—", "×", "✕":
		return true
	case "Like", "Reply", "Share", "Comment", "Follow", "Unfollow",
		"Edit", "Delete", "Report", "Pin", "Mute", "Block",
		"like", "reply", "share", "comment":
		return true
	}
	// Lone small numbers (reaction counts, pagination)
	if len(s) <= 3 {
		allDigits := true
		for _, r := range s {
			if r < '0' || r > '9' {
				allDigits = false
				break
			}
		}
		if allDigits {
			return true
		}
	}
	return false
}

// htmlToMarkdown converts simple HTML to markdown and extracts image URLs.
func htmlToMarkdown(html string) (string, []string) {
	var imgURLs []string
	s := stripHTMLChrome(html)

	// Extract and preserve images
	for {
		idx := strings.Index(s, "<img")
		if idx < 0 {
			break
		}
		end := strings.Index(s[idx:], ">")
		if end < 0 {
			break
		}
		tag := s[idx : idx+end+1]
		src := extractAttr(tag, "src")
		if src != "" {
			imgURLs = append(imgURLs, src)
			s = s[:idx] + "![](" + src + ")" + s[idx+end+1:]
		} else {
			s = s[:idx] + s[idx+end+1:]
		}
	}

	// Convert links
	for {
		idx := strings.Index(s, "<a")
		if idx < 0 {
			break
		}
		closeTag := strings.Index(s[idx:], "</a>")
		if closeTag < 0 {
			break
		}
		openEnd := strings.Index(s[idx:], ">")
		if openEnd < 0 {
			break
		}
		tag := s[idx : idx+openEnd+1]
		href := extractAttr(tag, "href")
		linkText := s[idx+openEnd+1 : idx+closeTag]
		linkText = stripTags(linkText)
		if href != "" {
			replacement := "[" + linkText + "](" + href + ")"
			s = s[:idx] + replacement + s[idx+closeTag+4:]
		} else {
			s = s[:idx] + linkText + s[idx+closeTag+4:]
		}
	}

	// Block-level elements
	s = replaceTag(s, "h1", "# ")
	s = replaceTag(s, "h2", "## ")
	s = replaceTag(s, "h3", "### ")
	s = replaceTag(s, "h4", "#### ")
	s = replaceTag(s, "p", "")
	s = replaceTag(s, "div", "")
	s = replaceTag(s, "br", "")
	s = replaceListItems(s)

	// Bold and italic
	s = replaceInlineTag(s, "strong", "**")
	s = replaceInlineTag(s, "b", "**")
	s = replaceInlineTag(s, "em", "_")
	s = replaceInlineTag(s, "i", "_")
	s = replaceInlineTag(s, "code", "`")

	// Strip remaining tags
	s = stripTags(s)

	// Clean up whitespace
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = cleanMarkdownNoise(s)
	lines := strings.Split(s, "\n")
	var cleaned []string
	prevEmpty := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			if !prevEmpty {
				cleaned = append(cleaned, "")
				prevEmpty = true
			}
			continue
		}
		cleaned = append(cleaned, trimmed)
		prevEmpty = false
	}
	return strings.TrimSpace(strings.Join(cleaned, "\n")), imgURLs
}

func extractAttr(tag, attr string) string {
	search := attr + "=\""
	idx := strings.Index(strings.ToLower(tag), strings.ToLower(search))
	if idx < 0 {
		// Try single quotes
		search = attr + "='"
		idx = strings.Index(strings.ToLower(tag), strings.ToLower(search))
		if idx < 0 {
			return ""
		}
		start := idx + len(search)
		end := strings.Index(tag[start:], "'")
		if end < 0 {
			return ""
		}
		return tag[start : start+end]
	}
	start := idx + len(search)
	end := strings.Index(tag[start:], "\"")
	if end < 0 {
		return ""
	}
	return tag[start : start+end]
}

func replaceTag(s, tag, prefix string) string {
	openTag := "<" + tag
	closeTag := "</" + tag + ">"
	for {
		idx := strings.Index(strings.ToLower(s), strings.ToLower(openTag))
		if idx < 0 {
			break
		}
		// Find end of open tag
		openEnd := strings.Index(s[idx:], ">")
		if openEnd < 0 {
			break
		}
		closeIdx := strings.Index(strings.ToLower(s[idx+openEnd+1:]), strings.ToLower(closeTag))
		if closeIdx < 0 {
			// Self-closing or no close tag — just remove the open tag
			s = s[:idx] + "\n" + s[idx+openEnd+1:]
			continue
		}
		content := s[idx+openEnd+1 : idx+openEnd+1+closeIdx]
		s = s[:idx] + "\n" + prefix + content + "\n" + s[idx+openEnd+1+closeIdx+len(closeTag):]
	}
	return s
}

func replaceInlineTag(s, tag, wrapper string) string {
	openTag := "<" + tag + ">"
	closeTag := "</" + tag + ">"
	for {
		idx := strings.Index(strings.ToLower(s), strings.ToLower(openTag))
		if idx < 0 {
			break
		}
		closeIdx := strings.Index(strings.ToLower(s[idx+len(openTag):]), strings.ToLower(closeTag))
		if closeIdx < 0 {
			break
		}
		content := s[idx+len(openTag) : idx+len(openTag)+closeIdx]
		s = s[:idx] + wrapper + content + wrapper + s[idx+len(openTag)+closeIdx+len(closeTag):]
	}
	return s
}

func replaceListItems(s string) string {
	openTag := "<li"
	closeTag := "</li>"
	for {
		idx := strings.Index(strings.ToLower(s), strings.ToLower(openTag))
		if idx < 0 {
			break
		}
		openEnd := strings.Index(s[idx:], ">")
		if openEnd < 0 {
			break
		}
		closeIdx := strings.Index(strings.ToLower(s[idx+openEnd+1:]), strings.ToLower(closeTag))
		if closeIdx < 0 {
			break
		}
		content := s[idx+openEnd+1 : idx+openEnd+1+closeIdx]
		s = s[:idx] + "\n- " + content + s[idx+openEnd+1+closeIdx+len(closeTag):]
	}
	// Strip <ul>, <ol> wrapper tags
	s = stripSpecificTag(s, "ul")
	s = stripSpecificTag(s, "ol")
	return s
}

func stripSpecificTag(s, tag string) string {
	s = strings.ReplaceAll(s, "<"+tag+">", "")
	s = strings.ReplaceAll(s, "</"+tag+">", "")
	s = strings.ReplaceAll(s, "<"+strings.ToUpper(tag)+">", "")
	s = strings.ReplaceAll(s, "</"+strings.ToUpper(tag)+">", "")
	return s
}

func stripTags(s string) string {
	var out strings.Builder
	inTag := false
	for _, r := range s {
		if r == '<' {
			inTag = true
			continue
		}
		if r == '>' {
			inTag = false
			continue
		}
		if !inTag {
			out.WriteRune(r)
		}
	}
	return out.String()
}
