package agent

import (
	"strings"
	"unicode"
)

const (
	thinkOpen  = "<think>"
	thinkClose = "</think>"
	// maxSpecialLen bounds how long a "<|...|>" token may be before the "<"
	// is treated as ordinary text.
	maxSpecialLen = 64
)

// strippedTags are literal tags removed from the visible stream.
var strippedTags = []string{"<tool_call>", "</tool_call>", thinkClose}

// TextFilter cleans an assistant text stream before it is shown, spoken or
// persisted: <think>…</think> blocks are dropped, and stray <tool_call>,
// </tool_call> and <|…|> special tokens are removed. Tags may be split across
// chunks; partial tags are buffered until they can be classified.
type TextFilter struct {
	inThink bool
	pending string
	emitted bool // visible (non-space) text has been returned
}

// Push feeds a chunk and returns the visible text it releases (maybe "").
func (f *TextFilter) Push(chunk string) string {
	s := f.pending + chunk
	f.pending = ""
	var out strings.Builder
	for len(s) > 0 {
		if f.inThink {
			i := strings.Index(s, thinkClose)
			if i >= 0 {
				s = s[i+len(thinkClose):]
				f.inThink = false
				continue
			}
			f.pending = partialSuffix(s, thinkClose)
			break
		}
		i := strings.IndexByte(s, '<')
		if i < 0 {
			out.WriteString(s)
			break
		}
		out.WriteString(s[:i])
		s = s[i:]
		if strings.HasPrefix(s, thinkOpen) {
			s = s[len(thinkOpen):]
			f.inThink = true
			continue
		}
		if n, hold := matchSpecial(s); n > 0 {
			s = s[n:]
			continue
		} else if hold {
			f.pending = s
			break
		}
		out.WriteByte('<')
		s = s[1:]
	}
	return f.visible(out.String())
}

// Flush releases any buffered partial text at end of stream.
func (f *TextFilter) Flush() string {
	p := f.pending
	f.pending = ""
	if f.inThink {
		return ""
	}
	return f.visible(p)
}

// visible trims leading whitespace until real text has been emitted (models
// commonly put "\n\n" after </think>).
func (f *TextFilter) visible(s string) string {
	if !f.emitted {
		s = strings.TrimLeftFunc(s, unicode.IsSpace)
		if s == "" {
			return ""
		}
		f.emitted = true
	}
	return s
}

// matchSpecial inspects s (which starts with '<'). It returns n>0 when a
// complete tag to strip is at the start of s; hold=true when s could still
// become one (need more input).
func matchSpecial(s string) (n int, hold bool) {
	for _, t := range strippedTags {
		if strings.HasPrefix(s, t) {
			return len(t), false
		}
	}
	if strings.HasPrefix(s, "<|") {
		if j := strings.Index(s, "|>"); j >= 0 && j+2 <= maxSpecialLen {
			return j + 2, false
		}
		return 0, len(s) < maxSpecialLen
	}
	for _, t := range append([]string{thinkOpen}, strippedTags...) {
		if len(s) < len(t) && strings.HasPrefix(t, s) {
			return 0, true
		}
	}
	return 0, false
}

// partialSuffix returns the longest suffix of s that is a proper prefix of tag.
func partialSuffix(s, tag string) string {
	for k := min(len(s), len(tag)-1); k > 0; k-- {
		if strings.HasPrefix(tag, s[len(s)-k:]) {
			return s[len(s)-k:]
		}
	}
	return ""
}
