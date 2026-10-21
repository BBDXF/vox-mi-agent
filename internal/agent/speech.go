package agent

import (
	"regexp"
	"strings"
	"unicode"
)

var (
	reFence      = regexp.MustCompile("(?s)```.*?(?:```|$)")
	reHR         = regexp.MustCompile(`^\s*([-*_]\s*){3,}$`)
	reTableSep   = regexp.MustCompile(`^\s*\|?[\s:|\-]+\|?\s*$`)
	reHeading    = regexp.MustCompile(`^\s*#{1,6}\s*`)
	reQuote      = regexp.MustCompile(`^\s*(?:>\s?)+`)
	reList       = regexp.MustCompile(`^\s*(?:[-*+]|\d{1,3}[.)])\s+`)
	reImage      = regexp.MustCompile(`!\[[^\]]*\]\([^)]*\)`)
	reLink       = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
	reInlineCode = regexp.MustCompile("`+([^`]*)`+")
	reStars      = regexp.MustCompile(`\*+`)
	reStrike     = regexp.MustCompile(`~~`)
	reUnderscore = regexp.MustCompile(`(^|[^A-Za-z0-9_])_{1,3}([^_\n]+?)_{1,3}($|[^A-Za-z0-9_])`)
	reSpaces     = regexp.MustCompile(`[ \t]{2,}`)
)

// StripMarkdown removes Markdown markup so that the text is suitable for TTS.
// Fenced code blocks and images are dropped, links keep their text, table
// cells are joined with "，", and list/heading/quote markers are removed.
func StripMarkdown(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = reFence.ReplaceAllString(s, "")
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if reHR.MatchString(line) {
			continue
		}
		if strings.Contains(line, "|") && strings.Contains(line, "-") && reTableSep.MatchString(line) {
			continue
		}
		line = reQuote.ReplaceAllString(line, "")
		line = reHeading.ReplaceAllString(line, "")
		line = reList.ReplaceAllString(line, "")
		if strings.Count(line, "|") >= 2 || strings.HasPrefix(strings.TrimSpace(line), "|") {
			line = tableLine(line)
		}
		line = reImage.ReplaceAllString(line, "")
		line = reLink.ReplaceAllString(line, "$1")
		line = reInlineCode.ReplaceAllString(line, "$1")
		line = reStars.ReplaceAllString(line, "")
		line = reStrike.ReplaceAllString(line, "")
		for i := 0; i < 2; i++ { // second pass handles adjacent matches
			line = reUnderscore.ReplaceAllString(line, "$1$2$3")
		}
		line = reSpaces.ReplaceAllString(line, " ")
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

func tableLine(line string) string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "|")
	line = strings.TrimSuffix(line, "|")
	cells := strings.Split(line, "|")
	var keep []string
	for _, c := range cells {
		if c = strings.TrimSpace(c); c != "" {
			keep = append(keep, c)
		}
	}
	return strings.Join(keep, "，")
}

func hasContent(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

// Splitter is a streaming sentence splitter producing TTS-ready sentences.
//
// It splits on 。！？；, on newlines, on ASCII . ! ? ; when followed by
// whitespace (so "3.14" is never split), and on ， / "," only once more than
// CommaThreshold runes have accumulated. Fenced code blocks are dropped.
// Fragments shorter than MinRunes are merged into the following text.
type Splitter struct {
	// CommaThreshold is the accumulated rune count above which a comma splits (default 40).
	CommaThreshold int
	// MinRunes is the minimum sentence length (default 2).
	MinRunes int

	buf     []rune
	ticks   int
	inFence bool
	ambig   rune // pending ASCII punctuation awaiting a lookahead rune
	ambigOK bool // for '.', whether the previous rune allowed a split
}

// NewSplitter returns a Splitter with default settings.
func NewSplitter() *Splitter { return &Splitter{CommaThreshold: 40, MinRunes: 2} }

func (p *Splitter) thresholds() (int, int) {
	c, m := p.CommaThreshold, p.MinRunes
	if c <= 0 {
		c = 40
	}
	if m <= 0 {
		m = 2
	}
	return c, m
}

// Push feeds a chunk of streamed text and returns any completed sentences.
func (p *Splitter) Push(chunk string) []string {
	var out []string
	emit := func(force bool) {
		if s, ok := p.cut(force); ok {
			out = append(out, s)
		}
	}
	commaTh, _ := p.thresholds()
	for _, r := range chunk {
		if p.inFence {
			if r == '`' {
				p.ticks++
				if p.ticks >= 3 {
					p.inFence, p.ticks = false, 0
				}
			} else {
				p.ticks = 0
			}
			continue
		}
		if p.ambig != 0 {
			c, ok := p.ambig, p.ambigOK
			p.ambig = 0
			if unicode.IsSpace(r) && ok {
				if c != ',' || len(p.buf) > commaTh {
					emit(false)
				}
			}
		}
		if r == '`' {
			p.buf = append(p.buf, r)
			p.ticks++
			if p.ticks == 3 {
				p.buf = p.buf[:len(p.buf)-3]
				p.inFence, p.ticks = true, 0
				emit(true)
			}
			continue
		}
		p.ticks = 0
		switch r {
		case '。', '！', '？', '；':
			p.buf = append(p.buf, r)
			emit(false)
		case '，':
			p.buf = append(p.buf, r)
			if len(p.buf) > commaTh {
				emit(false)
			}
		case '\n':
			emit(false)
		case '.', '!', '?', ';', ',':
			ok := true
			if r == '.' && len(p.buf) > 0 && unicode.IsDigit(p.buf[len(p.buf)-1]) {
				ok = false
			}
			p.buf = append(p.buf, r)
			p.ambig, p.ambigOK = r, ok
		default:
			if len(p.buf) == 0 && unicode.IsSpace(r) {
				continue
			}
			p.buf = append(p.buf, r)
		}
	}
	return out
}

// Flush returns whatever text remains (possibly short), or "" if none.
func (p *Splitter) Flush() string {
	p.ambig = 0
	p.ticks = 0
	p.inFence = false
	s, _ := p.cut(true)
	return s
}

// cut cleans the buffer and, if it forms a sentence, returns it. Fragments
// shorter than MinRunes are kept in the buffer (unless force) to merge with
// the following text; fragments without any letter/digit are dropped.
func (p *Splitter) cut(force bool) (string, bool) {
	if len(p.buf) == 0 {
		return "", false
	}
	_, minRunes := p.thresholds()
	s := StripMarkdown(string(p.buf))
	if !hasContent(s) {
		p.buf = p.buf[:0]
		return "", false
	}
	if !force && len([]rune(s)) < minRunes {
		return "", false
	}
	p.buf = p.buf[:0]
	return s, true
}
