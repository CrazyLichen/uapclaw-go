package splitter

import (
	"fmt"
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

// oneTokPerCharEnc 1 rune = 1 token 的简化 enc，预算可手算
func oneTokPerCharEnc(s string) ([]int, error) {
	rs := []rune(s)
	ids := make([]int, len(rs))
	for i := range rs {
		ids[i] = i + 1
	}
	return ids, nil
}

func TestSentenceSplitter_SplitDoc_多短句打包(t *testing.T) {
	enc := oneTokPerCharEnc
	bs, err := NewBaseSplitter(20, 4, enc, nil)
	if err != nil {
		t.Fatal(err)
	}
	ss := NewSentenceSplitter(bs, "auto")
	bs.SetSelf(ss)

	text := "一二三。四五。六七。八九。" // 每句 4 rune（4 token），共 16 token
	spans, err := ss.SplitDoc(text)
	if err != nil {
		t.Fatal(err)
	}
	// 16 token <= 20 应打成一块
	if len(spans) != 1 {
		t.Fatalf("16 token 预算 20 应打包成 1 块, got %d: %+v", len(spans), spans)
	}
	if spans[0].Text != text {
		t.Fatalf("块文本应为全句拼接: %q", spans[0].Text)
	}
	if spans[0].Start != 0 || spans[0].End != len(text) {
		t.Fatalf("span 应覆盖全文: %+v", spans[0])
	}
}

func TestSentenceSplitter_SplitDoc_溢出flush(t *testing.T) {
	enc := oneTokPerCharEnc
	bs, _ := NewBaseSplitter(8, 0, enc, nil) // 预算 8，无 overlap
	ss := NewSentenceSplitter(bs, "auto")
	bs.SetSelf(ss)

	text := "一二三四。五六。七八。" // 句1=5, 句2=3, 句3=3 token
	spans, err := ss.SplitDoc(text)
	if err != nil {
		t.Fatal(err)
	}
	if len(spans) < 2 {
		t.Fatalf("预算 8 应至少 2 块, got %d: %+v", len(spans), spans)
	}
	// 无 overlap：相邻块不应完全相同起点重复
	for i := 1; i < len(spans); i++ {
		if spans[i].Text == spans[i-1].Text && spans[i].Start == spans[i-1].Start {
			t.Fatalf("overlap=0 不应有重复块: %+v", spans[i])
		}
	}
}

func TestSentenceSplitter_SplitDoc_overlap尾缀(t *testing.T) {
	enc := oneTokPerCharEnc
	bs, _ := NewBaseSplitter(6, 4, enc, nil) // 预算 6，overlap 4
	ss := NewSentenceSplitter(bs, "auto")
	bs.SetSelf(ss)

	// 每句 3 token：句1+句2=6 打包；flush 后尾缀句（3<=4）进下块开头
	text := "一二三。四五。六七。八九。"
	spans, err := ss.SplitDoc(text)
	if err != nil {
		t.Fatal(err)
	}
	if len(spans) < 2 {
		t.Fatalf("应至少 2 块, got %d: %+v", len(spans), spans)
	}
	// 第二块应以 flush 尾缀句开头（Python 语义：前缀句子复用）
	if len(spans) < 2 || !strings.Contains(spans[1].Text, "四五。") {
		t.Fatalf("overlap 尾缀应携带前块末句: %+v", spans)
	}
}

func TestSentenceSplitter_SplitDoc_空文本(t *testing.T) {
	enc := oneTokPerCharEnc
	bs, _ := NewBaseSplitter(8, 0, enc, nil)
	ss := NewSentenceSplitter(bs, "auto")
	bs.SetSelf(ss)
	for _, empty := range []string{"", "   \n  "} {
		spans, err := ss.SplitDoc(empty)
		if err != nil || len(spans) != 0 {
			t.Fatalf("空文本应返回空: %v %v", spans, err)
		}
	}
}

func TestSentenceSplitter_SplitDoc_英文lan透传(t *testing.T) {
	enc := oneTokPerCharEnc
	bs, _ := NewBaseSplitter(100, 0, enc, nil)
	ss := NewSentenceSplitter(bs, "en") // 显式 lan，不检测
	bs.SetSelf(ss)
	text := "Dr. Smith went home. He slept."
	spans, err := ss.SplitDoc(text) // en 模式缩写 Dr. 不切
	if err != nil {
		t.Fatal(err)
	}
	// 预算 100：两句打包成 1 块（Python 多短句打包语义），缩写不切开
	if len(spans) != 1 {
		t.Fatalf("预算内应打包 1 块: %+v", spans)
	}
	if !strings.Contains(spans[0].Text, "Dr.") || !strings.Contains(spans[0].Text, "He slept.") {
		t.Fatalf("块内容应含两句（缩写不切）: %+v", spans[0])
	}
}

func TestSentenceSplitter_SplitDoc_encode失败上抛(t *testing.T) {
	failEnc := func(s string) ([]int, error) { return nil, fmt.Errorf("enc boom") }
	bs, _ := NewBaseSplitter(8, 0, failEnc, nil)
	ss := NewSentenceSplitter(bs, "auto")
	bs.SetSelf(ss)
	if _, err := ss.SplitDoc("一二三。四五。"); err == nil {
		t.Fatal("encode 失败应上抛")
	}
}
