// Package locale names the languages Sigmo can present text in.
package locale

import "strings"

// Tag identifies a supported language. The zero value means no preference and
// renders as English, so callers can pass it through without checking.
type Tag string

const (
	// English is the default language.
	English Tag = "en"
	// Chinese is Simplified Chinese. The web UI shows it for every zh variant,
	// and Parse does the same so both sides agree.
	Chinese Tag = "zh"
)

// Parse maps a language tag such as "zh-CN" to a supported Tag. It returns the
// zero Tag when s names no supported language.
func Parse(s string) Tag {
	// Prefix matching mirrors the web UI's pickLocale, so a browser the UI
	// shows in Chinese is never recorded as English here.
	s = strings.ToLower(strings.TrimSpace(s))
	switch {
	case strings.HasPrefix(s, "zh"):
		return Chinese
	case strings.HasPrefix(s, "en"):
		return English
	default:
		return ""
	}
}
