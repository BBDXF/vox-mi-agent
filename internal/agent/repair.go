package agent

import "strings"

// repairJSON escapes unescaped double quotes inside JSON string values, a
// common small-model mistake (e.g. {"filter": "select(.a == "x")"}). A quote
// inside a string is treated as closing only when the next non-space
// character is one of , } ] : (or end of input); otherwise it is escaped.
func repairJSON(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 8)
	in, esc := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !in {
			if c == '"' {
				in = true
			}
			b.WriteByte(c)
			continue
		}
		switch {
		case esc:
			esc = false
		case c == '\\':
			esc = true
		case c == '"':
			j := i + 1
			for j < len(s) && (s[j] == ' ' || s[j] == '\t' || s[j] == '\n' || s[j] == '\r') {
				j++
			}
			if j == len(s) || strings.IndexByte(",}]:", s[j]) >= 0 {
				in = false
			} else {
				b.WriteString(`\"`)
				continue
			}
		}
		b.WriteByte(c)
	}
	return b.String()
}
