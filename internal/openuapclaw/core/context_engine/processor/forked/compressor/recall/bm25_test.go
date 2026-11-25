package recall

import (
	"math"
	"testing"
)

// containsToken 判断分词结果是否含目标 token。
func containsToken(tokens []string, target string) bool {
	for _, token := range tokens {
		if token == target {
			return true
		}
	}
	return false
}

// TestTokenize_中英混排 验证 ASCII 标识符拆分与 CJK 片段保留。
func TestTokenize_中英混排(t *testing.T) {
	tokens := Tokenize("读取 XLSX_Sheet 表格")

	for _, want := range []string{"xlsx_sheet", "xlsx", "sheet", "读取", "表格"} {
		if !containsToken(tokens, want) {
			t.Fatalf("缺少 token %q，got %v", want, tokens)
		}
	}
	if containsToken(tokens, "读") {
		t.Fatal("CJK 不应拆成单字（上游只保留整段与双字滑窗）")
	}
}

// TestTokenize_CJK双字滑窗 验证三字片段产生双字滑窗。
func TestTokenize_CJK双字滑窗(t *testing.T) {
	tokens := Tokenize("电子表格")
	for _, want := range []string{"电子表格", "电子", "子表", "表格"} {
		if !containsToken(tokens, want) {
			t.Fatalf("缺少 token %q，got %v", want, tokens)
		}
	}
}

// TestNormalizeMarkdown_去语法 验证 Markdown 语法被剥离但文本保留。
func TestNormalizeMarkdown_去语法(t *testing.T) {
	input := "---\ntitle: x\n---\n# 标题\n- 条目 [链接](http://a)\n```go\ncode\n```"
	if got := NormalizeMarkdown(input); got != "标题 条目 链接 http://a code" {
		t.Fatalf("got %q", got)
	}
}

// TestBM25Index_Scores 验证命中文档得正分、未命中为 0。
func TestBM25Index_Scores(t *testing.T) {
	index := BuildBM25Index([]string{"xlsx read sheet", "pdf extract text"}, DefaultK1, DefaultB)
	scores := index.Scores("xlsx")
	if len(scores) != 2 {
		t.Fatalf("应有 2 个分数，got %d", len(scores))
	}
	if scores[0] <= 0 {
		t.Fatalf("首篇应命正分，got %v", scores)
	}
	if scores[1] != 0 {
		t.Fatalf("次篇应 0 分，got %v", scores)
	}
}

// TestBM25Index_空输入 验证空索引与空查询的边界。
func TestBM25Index_空输入(t *testing.T) {
	index := BuildBM25Index(nil, DefaultK1, DefaultB)
	if index.DocumentCount() != 0 {
		t.Fatal("文档数应为 0")
	}
	if scores := index.Scores("x"); len(scores) != 0 {
		t.Fatalf("空索引应返回空分数，got %v", scores)
	}

	index = BuildBM25Index([]string{"abc"}, DefaultK1, DefaultB)
	if scores := index.Scores(""); len(scores) != 1 || scores[0] != 0 {
		t.Fatalf("空查询应全 0，got %v", scores)
	}
}

// TestRankBM25 验证一次性打分入口。
func TestRankBM25(t *testing.T) {
	scores := RankBM25("xlsx", []string{"xlsx read", "pdf read"}, DefaultK1, DefaultB)
	if len(scores) != 2 || scores[0] <= 0 || scores[1] != 0 {
		t.Fatalf("打分结果不符，got %v", scores)
	}
}

// TestBM25Index_重复词频影响分数 验证词频对分数单调贡献。
func TestBM25Index_重复词频影响分数(t *testing.T) {
	index := BuildBM25Index([]string{"alpha alpha alpha", "alpha beta gamma"}, DefaultK1, DefaultB)
	scores := index.Scores("alpha")
	if scores[0] <= scores[1] {
		t.Fatalf("高词频文档分数应更高，got %v", scores)
	}
	if math.IsNaN(scores[0]) || math.IsInf(scores[0], 0) {
		t.Fatalf("分数不应为 NaN/Inf，got %v", scores)
	}
}
