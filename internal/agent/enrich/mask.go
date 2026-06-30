package enrich

import "strings"

// Mask returns a redacted hint for a sensitive value. It never returns the full
// value. Emails keep the domain; other values keep at most the last 4 chars
// when the value is long enough to make the tail non-identifying.
func Mask(label, value string) string {
	if label == "email" {
		if at := strings.LastIndex(value, "@"); at >= 0 {
			// Safe: '@' is a single-byte ASCII char so at+1 is always a valid rune boundary.
			return "***@" + value[at+1:]
		}
	}
	const tail = 4
	runes := []rune(value)
	if len(runes) <= tail+2 {
		return "***"
	}
	return "…" + string(runes[len(runes)-tail:])
}
