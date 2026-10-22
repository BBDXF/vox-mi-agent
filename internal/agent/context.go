package agent

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/BBDXF/vox-mi-agent/internal/llm"
)

// maxOldToolResult is the max runes kept of a tool result from earlier turns.
const maxOldToolResult = 1500

// applyIdle keeps only the messages after the last gap >= idle between
// consecutive messages (nothing if the last message is older than idle).
// times is parallel to h; without it (or idle <= 0) h is unchanged.
func applyIdle(h []llm.Message, times []time.Time, idle time.Duration, now time.Time) []llm.Message {
	if idle <= 0 || len(h) == 0 || len(times) != len(h) {
		return h
	}
	if now.Sub(times[len(h)-1]) >= idle {
		return nil
	}
	start := 0
	for i := len(h) - 1; i > 0; i-- {
		if times[i].Sub(times[i-1]) >= idle {
			start = i
			break
		}
	}
	return fromUser(h[start:])
}

// fromUser drops leading non-user messages so tool results are never orphaned.
func fromUser(h []llm.Message) []llm.Message {
	for i, m := range h {
		if m.Role == llm.RoleUser {
			return h[i:]
		}
	}
	return nil
}

// truncateToolResults shortens long tool messages (copying, not mutating h).
func truncateToolResults(h []llm.Message) []llm.Message {
	var out []llm.Message
	for i, m := range h {
		if m.Role == llm.RoleTool && utf8.RuneCountInString(m.Content) > maxOldToolResult {
			if out == nil {
				out = append([]llm.Message(nil), h...)
			}
			out[i].Content = string([]rune(m.Content)[:maxOldToolResult]) + "…(已截断)"
		}
	}
	if out == nil {
		return h
	}
	return out
}

// estTokens is a cheap token estimate: CJK rune ≈ 1, other ≈ bytes/4.
func estTokens(s string) int {
	cjk, other := 0, 0
	for _, r := range s {
		if r >= 0x2E80 {
			cjk++
		} else {
			other++
		}
	}
	return cjk + (other+3)/4
}

func estMessage(m llm.Message) int {
	n := estTokens(m.Content) + 4
	for _, c := range m.ToolCalls {
		n += estTokens(c.Name) + estTokens(string(c.Arguments)) + 4
	}
	return n
}

// fitBudget drops the oldest history (whole turns, starting at a user
// message) until fixed + history fits budget.
func fitBudget(h []llm.Message, fixed, budget int) []llm.Message {
	total := fixed
	for _, m := range h {
		total += estMessage(m)
	}
	for total > budget && len(h) > 0 {
		next := len(h)
		for i := 1; i < len(h); i++ {
			if h[i].Role == llm.RoleUser {
				next = i
				break
			}
		}
		for _, m := range h[:next] {
			total -= estMessage(m)
		}
		h = h[next:]
	}
	return h
}

// isContextOverflow recognises provider "prompt too long" errors.
func isContextOverflow(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	for _, k := range []string{"context_length_exceeded", "maximum context length", "context length",
		"too many tokens", "input is too long", "prompt is too long", "request too large", "request entity too large"} {
		if strings.Contains(s, k) {
			return true
		}
	}
	return false
}
