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

// EscapeMarkdownCode prepares a value for an inline Markdown code span (`...`).
// In that context underscores and asterisks do not need escaping, but backticks
// would break the surrounding code span, so we normalize common legacy escapes
// and replace backticks/newlines with safe plain-text equivalents.
func EscapeMarkdownCode(s string) string {
	s = NormalizeMarkdownEscapes(s)
	s = strings.ReplaceAll(s, "`", "'")
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	return s
}

// NormalizeMarkdownEscapes removes legacy backslashes before Markdown control
// characters so values can be re-rendered safely without accumulating escapes.
func NormalizeMarkdownEscapes(s string) string {
	if strings.TrimSpace(s) == "" {
		return s
	}
	replacer := strings.NewReplacer(
		`\\_`, "_",
		`\\*`, "*",
		`\\[`, "[",
		"\\\\`", "`",
		"\\_", "_",
		"\\*", "*",
		"\\[", "[",
		"\\`", "`",
	)
	return replacer.Replace(s)
}
