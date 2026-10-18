package splitter

import (
	"strings"
	"testing"
)

func TestDetectChinese(t *testing.T) {
	tests := []struct {
		name string
		text string
		want string
	}{
		{"纯中文", "这是中文测试句子。", "zh"},
		{"纯英文", "This is English text.", "en"},
		{"空文本", "", "en"},
		{"中文刚好过10%阈值", strings.Repeat("a", 8) + "中中", "zh"},   // 2/10=20% > 10%
		{"中文低于10%阈值", strings.Repeat("a", 20) + "中", "en"},      // 1/21 ≈ 4.8% < 10%
		{"标点启发fallback", "真的吗？太好了！Really？Great！", "zh"},     // CJK 0 但 ？> ? 且 ！> !
		{"中英混排过半", "Hello 世界 Hello 世界 你好你好。", "zh"},
		{"英文标点为主", "Is this right? Yes! Great!", "en"},
		{"全角标点但无中文", "真的吗？太好了！", "zh"},                  // ？> ? 且 ！> !
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := detectChinese(tt.text, 0.1); got != tt.want {
				t.Fatalf("detectChinese(%q) = %q, want %q", tt.text, got, tt.want)
			}
		})
	}
}
