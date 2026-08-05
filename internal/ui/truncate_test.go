package ui

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncateRuneSafe(t *testing.T) {
	// 全中文长文本
	s := strings.Repeat("中文测试字符。", 100)
	out := Truncate(s, 20)
	if !utf8.ValidString(out) {
		t.Fatalf("Truncate 输出不是合法 UTF-8: %q", out)
	}
	if strings.ContainsRune(out, '\uFFFD') {
		t.Fatalf("Truncate 输出包含替换符: %q", out)
	}
	if got := len([]rune(out)); got != 23 { // 20 + "..."
		t.Fatalf("期望 23 个 rune，实际 %d: %q", got, out)
	}
	// 短文本不截断
	if out2 := Truncate("abc", 10); out2 != "abc" {
		t.Fatalf("短文本不应截断: %q", out2)
	}
	// n<=0 原样返回
	if out3 := Truncate("abc", 0); out3 != "abc" {
		t.Fatalf("n<=0 不应截断: %q", out3)
	}
	// 中英混合
	mixed := Truncate("hello世界hello世界hello世界", 8)
	if !utf8.ValidString(mixed) || !strings.HasSuffix(mixed, "...") {
		t.Fatalf("混合文本截断异常: %q", mixed)
	}
	t.Logf("混合文本截断结果: %q", mixed)
}
