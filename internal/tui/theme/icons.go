package theme

import (
	"os"
	"strings"
)

// Emoji are decorative. Only characters with Emoji_Presentation (always two
// cells wide, no variation selector) are used, so column widths stay exact on
// every terminal. They are turned off with WS_NO_EMOJI=1 and on the Linux
// console, which cannot draw them.
var emojiOn = emojiSupported()

func emojiSupported() bool {
	if v := os.Getenv("WS_NO_EMOJI"); v != "" && v != "0" {
		return false
	}
	return os.Getenv("TERM") != "linux"
}

// SetEmoji overrides emoji rendering (call before the program starts; tests).
func SetEmoji(on bool) { emojiOn = on }

// EmojiOn reports whether emoji are rendered.
func EmojiOn() bool { return emojiOn }

// Icon returns e followed by a space, or "" when emoji are off.
func Icon(e string) string {
	if !emojiOn || e == "" {
		return ""
	}
	return e + " "
}

// Decorative icons shared across screens.
const (
	IconParty    = "🎉"
	IconSparkles = "✨"
	IconAlarm    = "🚨"
	IconClip     = "📋"
	IconOops     = "😬"
	IconCheck    = "✅"
	IconWork     = "🚧"
	IconFire     = "🔥"
	IconBulb     = "💡"
	IconWave     = "👋"
	IconSprout   = "🌱"
	IconBroom    = "🧹"
	IconLock     = "🔐"
	IconTape     = "📼"
	IconPin      = "📌"
	IconHome     = "🏠"
)

var (
	moonFrames    = []string{"🌑", "🌒", "🌓", "🌔", "🌕", "🌖", "🌗", "🌘"}
	brailleFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
)

// Spinner returns the animation frame for tick n: moon phases with emoji,
// braille dots without.
func Spinner(n int) string {
	frames := brailleFrames
	if emojiOn {
		frames = moonFrames
	}
	return frames[((n%len(frames))+len(frames))%len(frames)]
}

// Greeting is a time-of-day hello for the dashboard; hour is 0-23.
func Greeting(hour int) string {
	switch {
	case hour >= 5 && hour < 12:
		return Icon("🌅") + "Good morning"
	case hour >= 12 && hour < 18:
		return Icon("🌞") + "Good afternoon"
	case hour >= 18 && hour < 23:
		return Icon("🌆") + "Good evening"
	default:
		return Icon("🌙") + "Burning the midnight oil"
	}
}

// Celebrate wraps a success message with a flourish.
func Celebrate(text string) string {
	return Icon(IconParty) + strings.TrimSpace(text)
}
