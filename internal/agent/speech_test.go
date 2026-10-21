package agent

import (
	"reflect"
	"strings"
	"testing"
)

func TestStripMarkdown(t *testing.T) {
	cases := []struct{ in, want string }{
		{"# 标题", "标题"},
		{"## Title here", "Title here"},
		{"这是**加粗**和*斜体*和__粗__以及_斜_文字", "这是加粗和斜体和粗以及斜文字"},
		{"snake_case_name 保持", "snake_case_name 保持"},
		{"用 `ls -l` 命令", "用 ls -l 命令"},
		{"前面\n```go\nfmt.Println(1)\n```\n后面", "前面\n后面"},
		{"前面\n```\n未闭合代码", "前面"},
		{"看 [官网](https://a.com/x.html) 吧", "看 官网 吧"},
		{"![图片](http://a/b.png)你好", "你好"},
		{"- 苹果\n* 香蕉\n+ 梨\n1. 一\n2) 二", "苹果\n香蕉\n梨\n一\n二"},
		{"> 引用内容\n>> 嵌套", "引用内容\n嵌套"},
		{"| 名称 | 价格 |\n|---|:---:|\n| 苹果 | 3 |\n| 香蕉 | 5 |", "名称，价格\n苹果，3\n香蕉，5"},
		{"上面\n---\n下面", "上面\n下面"},
		{"~~删除~~ 文本", "删除 文本"},
		{"纯文本，没有标记。", "纯文本，没有标记。"},
	}
	for _, c := range cases {
		if got := StripMarkdown(c.in); got != c.want {
			t.Errorf("StripMarkdown(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func splitAll(chunks []string, chunkSize int) []string {
	sp := NewSplitter()
	var out []string
	for _, c := range chunks {
		rs := []rune(c)
		for i := 0; i < len(rs); i += chunkSize {
			out = append(out, sp.Push(string(rs[i:min(i+chunkSize, len(rs))]))...)
		}
	}
	if s := sp.Flush(); s != "" {
		out = append(out, s)
	}
	return out
}

func TestSplitterChinese(t *testing.T) {
	in := "你好！今天天气不错。要出去玩吗？好的；再见"
	want := []string{"你好！", "今天天气不错。", "要出去玩吗？", "好的；", "再见"}
	for _, size := range []int{1, 2, 3, 100} {
		if got := splitAll([]string{in}, size); !reflect.DeepEqual(got, want) {
			t.Errorf("size %d: got %q want %q", size, got, want)
		}
	}
}

func TestSplitterNumbersAndEnglish(t *testing.T) {
	in := "Pi is 3.14 today. Price is 1,000 yuan! Ok"
	want := []string{"Pi is 3.14 today.", "Price is 1,000 yuan!", "Ok"}
	for _, size := range []int{1, 5, 100} {
		if got := splitAll([]string{in}, size); !reflect.DeepEqual(got, want) {
			t.Errorf("size %d: got %q want %q", size, got, want)
		}
	}
	got := splitAll([]string{"圆周率是3.14159，很长"}, 2)
	if len(got) != 1 {
		t.Errorf("short comma/number split: %q", got)
	}
}

func TestSplitterComma(t *testing.T) {
	short := "短句，不切分，继续"
	if got := splitAll([]string{short}, 3); len(got) != 1 {
		t.Errorf("short: %q", got)
	}
	long := strings.Repeat("长", 41) + "，后面还有内容"
	got := splitAll([]string{long}, 7)
	if len(got) != 2 || !strings.HasSuffix(got[0], "，") {
		t.Errorf("long: %q", got)
	}
}

func TestSplitterNewlineMarkdownTable(t *testing.T) {
	in := "# 标题\n| 名称 | 价格 |\n|---|---|\n| 苹果 | 3 |\n\n```\ncode。here\n```\n结束"
	want := []string{"标题", "名称，价格", "苹果，3", "结束"}
	for _, size := range []int{1, 4, 1000} {
		if got := splitAll([]string{in}, size); !reflect.DeepEqual(got, want) {
			t.Errorf("size %d: got %q want %q", size, got, want)
		}
	}
}

func TestSplitterMinLengthAndPunctOnly(t *testing.T) {
	got := splitAll([]string{"好。。！\nA\nB。"}, 100)
	want := []string{"好。", "A\nB。"}
	_ = want
	// "好。" emitted; "。！" dropped (punct only); "A" merges with "B。"
	if len(got) != 2 || got[0] != "好。" || !strings.Contains(got[1], "A") || !strings.Contains(got[1], "B。") {
		t.Errorf("got %q", got)
	}
	// markdown emphasis across the stream
	got = splitAll([]string{"**重要**：请注意。"}, 2)
	if !reflect.DeepEqual(got, []string{"重要：请注意。"}) {
		t.Errorf("got %q", got)
	}
}
