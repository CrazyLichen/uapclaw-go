package web_tools

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// mustDoc 把 HTML 片段解析为 goquery 文档。
func mustDoc(t *testing.T, html string) *goquery.Document {
	t.Helper()
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatalf("解析 HTML 失败: %v", err)
	}
	return doc
}

// ──────────────────────────── 非导出函数测试 ────────────────────────────

// TestIsDigitOnly 验证纯数字判定。
func TestIsDigitOnly(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"", false},
		{"123", true},
		{"12a", false},
		{"a12", false},
		{"０１２", false}, // 全角数字不属于 ASCII 0-9
	}
	for _, tt := range tests {
		if got := isDigitOnly(tt.in); got != tt.want {
			t.Fatalf("isDigitOnly(%q) = %v, 期望 %v", tt.in, got, tt.want)
		}
	}
}

// TestScoredRows 验证评分结果字段齐全。
func TestScoredRows(t *testing.T) {
	rows := []searchRow{
		{Title: "A", URL: "http://a", Snippet: "sa", Origin: "oa", Date: "d", Source: "s"},
	}
	got := scoredRows("a", rows)
	if len(got) != 1 {
		t.Fatalf("应返回 1 行，实际 %d", len(got))
	}
	for _, key := range []string{"title", "url", "snippet", "origin", "date", "source", "score", "match_count"} {
		if _, ok := got[0][key]; !ok {
			t.Fatalf("缺少字段 %q，实际 %v", key, got[0])
		}
	}
	if empty := scoredRows("a", nil); len(empty) != 0 {
		t.Fatalf("空输入应返回空，实际 %v", empty)
	}
}

// TestExtractBingResultsArea 验证结果区域选择器的回退。
func TestExtractBingResultsArea(t *testing.T) {
	t.Run("优先取 main 区域", func(t *testing.T) {
		doc := mustDoc(t, `<html><body><main aria-label="Search Results"><div id="hit"></div></main><div id="other"></div></body></html>`)
		sel := extractBingResultsArea(doc)
		if sel.Find("#hit").Length() == 0 {
			t.Fatal("应命中 main 区域")
		}
	})

	t.Run("无 main 时回退 body", func(t *testing.T) {
		doc := mustDoc(t, `<html><body><div id="only"></div></body></html>`)
		sel := extractBingResultsArea(doc)
		if sel.Find("#only").Length() == 0 {
			t.Fatal("应回退到 body")
		}
	})
}

// TestExtractBingAnswerCards 验证答案卡片提取（去重、跳过短标题与内链）。
func TestExtractBingAnswerCards(t *testing.T) {
	t.Run("提取有效卡片", func(t *testing.T) {
		doc := mustDoc(t, `<html><body>
			<div class="b_ans">
				<a href="https://example.com/page">https://example.com/page</a>
				<h2>An Example Title</h2>
				<div class="b_caption"><p>some snippet</p></div>
			</div>
		</body></html>`)
		rows := extractBingAnswerCards(doc, "https://fallback")
		if len(rows) != 1 {
			t.Fatalf("应提取 1 张卡片，实际 %d: %v", len(rows), rows)
		}
		if rows[0].Title != "An Example Title" {
			t.Fatalf("标题 = %q", rows[0].Title)
		}
		// buildBingRow(href, title, snippet, origin, date, source)，
		// 调用处第 6 参传 "bing-card"，故标记落在 Source 而非 Origin。
		if rows[0].Source != "bing-card" {
			t.Fatalf("source 应为 bing-card，实际 %q", rows[0].Source)
		}
	})

	t.Run("标题过短被跳过", func(t *testing.T) {
		doc := mustDoc(t, `<html><body>
			<div class="b_ans"><h2>ab</h2><div class="b_caption"><p>s</p></div></div>
		</body></html>`)
		if rows := extractBingAnswerCards(doc, ""); len(rows) != 0 {
			t.Fatalf("短标题应被跳过，实际 %v", rows)
		}
	})

	t.Run("bing 内链不作为结果 URL，回退 fallback", func(t *testing.T) {
		doc := mustDoc(t, `<html><body>
			<div class="b_ans">
				<a href="https://www.bing.com/ck/a">x</a>
				<h2>Long Enough Title</h2>
				<div class="b_caption"><p>s</p></div>
			</div>
		</body></html>`)
		rows := extractBingAnswerCards(doc, "https://fallback")
		if len(rows) != 1 {
			t.Fatalf("应保留 1 条，实际 %v", rows)
		}
		if rows[0].URL != "https://fallback" {
			t.Fatalf("应回退 fallback URL，实际 %q", rows[0].URL)
		}
	})

	t.Run("重复卡片被去重", func(t *testing.T) {
		card := `<div class="b_ans"><h2>Duplicate Title</h2><div class="b_caption"><p>s</p></div></div>`
		doc := mustDoc(t, `<html><body>`+card+card+`</body></html>`)
		rows := extractBingAnswerCards(doc, "")
		if len(rows) != 1 {
			t.Fatalf("重复标题+URL 应去重为 1 条，实际 %d", len(rows))
		}
	})

	t.Run("无卡片返回空", func(t *testing.T) {
		doc := mustDoc(t, `<html><body><div>nothing</div></body></html>`)
		if rows := extractBingAnswerCards(doc, ""); len(rows) != 0 {
			t.Fatalf("应返回空，实际 %v", rows)
		}
	})
}

// TestGetDebugDir 验证调试目录取环境变量或默认值。
func TestGetDebugDir(t *testing.T) {
	t.Run("环境变量优先", func(t *testing.T) {
		t.Setenv(freeSearchDebugDirEnv, "  /custom/dir  ")
		if got := getDebugDir(); got != "/custom/dir" {
			t.Fatalf("应取环境变量并去空白，实际 %q", got)
		}
	})

	t.Run("未设置时用默认目录", func(t *testing.T) {
		t.Setenv(freeSearchDebugDirEnv, "")
		if got := getDebugDir(); got != ".tmp/free_search_debug" {
			t.Fatalf("应用默认目录，实际 %q", got)
		}
	})
}
