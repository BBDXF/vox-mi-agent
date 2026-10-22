package speaker

import (
	"regexp"
	"sort"
	"strings"
)

// Routes of a handled (or ignored) ask.
const (
	RouteDirect       = "direct"        // search hit played without the LLM
	RouteArtistRandom = "artist_random" // random track of a named artist
	RouteAgent        = "agent"         // agent turn
	RouteIgnored      = "ignored"       // left to the speaker's own assistant
)

const punct = " \t\r\n,，。.、:：!！?？;；~～…"

var politePrefixes = []string{"麻烦你", "麻烦", "请你", "帮我", "给我", "请"}

func byLenDesc(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return len([]rune(out[i])) > len([]rune(out[j])) })
	return out
}

func stripAny(q string, prefixes []string) (string, bool) {
	for _, p := range prefixes {
		if strings.HasPrefix(q, p) {
			return q[len(p):], true
		}
	}
	return q, false
}

// Normalize strips leading wake words and punctuation (and, with polite, the
// polite prefixes 请/帮我/给我/麻烦) until nothing more changes.
func Normalize(q string, wake []string, polite bool) string {
	w := byLenDesc(wake)
	pp := byLenDesc(politePrefixes)
	q = strings.Trim(q, punct)
	for {
		changed := false
		var c bool
		if q, c = stripAny(q, w); c {
			changed = true
		}
		q = strings.TrimLeft(q, punct)
		if polite {
			if q, c = stripAny(q, pp); c {
				changed = true
			}
			q = strings.TrimLeft(q, punct)
		}
		if !changed {
			return strings.TrimRight(q, punct)
		}
	}
}

var (
	leadCountRe = regexp.MustCompile(`^(?:一首|一下|一曲|一点|一些|几首|首|点|些|下)+`)
	trailSuffix = []string{"的歌曲", "的歌", "谢谢", "吧"}
)

// CleanContent removes counters ("一首") and polite tails from the part after
// a play verb.
func CleanContent(s string) string {
	s = strings.Trim(s, punct)
	for {
		n := strings.Trim(leadCountRe.ReplaceAllString(s, ""), punct)
		for _, suf := range trailSuffix {
			n = strings.TrimSpace(strings.TrimSuffix(n, suf))
		}
		n = strings.Trim(n, punct)
		if n == s {
			return s
		}
		s = n
	}
}

// ParsedMusic is the result of ParseMusic.
type ParsedMusic struct {
	Normalized string // wake words / punctuation / polite prefixes stripped
	Content    string // what to play (cleaned); empty unless Music
	Music      bool
}

// ParseMusic decides whether query is a music request: after Normalize it
// must start with a play verb followed by non-empty content, and must not
// start with an excluded phrase (放假, 听说, …). Bare controls such as
// 暂停 / 下一首 / 音量 never match.
func ParseMusic(query string, wake, triggers, exclude []string) ParsedMusic {
	q := Normalize(query, wake, true)
	p := ParsedMusic{Normalized: q}
	for _, ex := range exclude {
		if ex = strings.TrimSpace(ex); ex != "" && strings.HasPrefix(q, ex) {
			return p
		}
	}
	for _, t := range byLenDesc(triggers) {
		if !strings.HasPrefix(q, t) {
			continue
		}
		if c := CleanContent(q[len(t):]); c != "" {
			p.Content, p.Music = c, true
			return p
		}
	}
	return p
}
