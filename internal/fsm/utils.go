package fsm

import "strings"

// EscapeMarkdown escapes characters that Telegram treats as Markdown
// when ParseMode is Markdown, so values from DB display literally.
func EscapeMarkdown(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '\\', '_', '*', '`', '[':
			b.WriteRune('\\')
			b.WriteRune(r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
