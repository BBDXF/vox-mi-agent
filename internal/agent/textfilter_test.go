package agent

import (
	"strings"
	"testing"
)

func runFilter(chunks ...string) string {
	var f TextFilter
	var sb strings.Builder
	for _, c := range chunks {
		sb.WriteString(f.Push(c))
	}
	sb.WriteString(f.Flush())
	return sb.String()
}

func TestTextFilter(t *testing.T) {
	cases := []struct {
		name   string
		chunks []string
		want   string
	}{
		{"plain", []string{"你好，", "世界。"}, "你好，世界。"},
		{"think whole", []string{"<think>hmm</think>答案"}, "答案"},
		{"think trailing ws", []string{"<think>hmm</think>\n\n答案"}, "答案"},
		{"think split open", []string{"<thi", "nk>abc</th", "ink>\n好的"}, "好的"},
		{"think char by char", strings.Split("<think>a<b</think>ok", ""), "ok"},
		{"think inner angle", []string{"<think>1 < 2 </ x</think>done"}, "done"},
		{"tool_call tail", []string{"好的", "</tool", "_call>"}, "好的"},
		{"tool_call open+close", []string{"<tool_call>", "x</tool_call>"}, "x"},
		{"special token", []string{"hi<|im_", "end|>there"}, "hithere"},
		{"special one chunk", []string{"<|endoftext|>a"}, "a"},
		{"literal lt", []string{"a < b and 3<4"}, "a < b and 3<4"},
		{"lt then text", []string{"x <", " y"}, "x < y"},
		{"unterminated partial flushed", []string{"ok <tool_ca"}, "ok <tool_ca"},
		{"unterminated think dropped", []string{"ok<think>never closed"}, "ok"},
		{"stray close think", []string{"abc</think>def"}, "abcdef"},
		{"unclosed special treated literal", []string{"<|" + strings.Repeat("x", 80)}, "<|" + strings.Repeat("x", 80)},
	}
	for _, c := range cases {
		if got := runFilter(c.chunks...); got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}

func TestTextFilterSplitEquivalence(t *testing.T) {
	in := "<think>想一想</think>\n你好<|im_end|>，世界</tool_call>。a<b"
	want := runFilter(in)
	if want != "你好，世界。a<b" {
		t.Fatalf("whole = %q", want)
	}
	rs := []rune(in)
	for i := 0; i <= len(rs); i++ {
		if got := runFilter(string(rs[:i]), string(rs[i:])); got != want {
			t.Fatalf("split at %d: got %q want %q", i, got, want)
		}
	}
}
