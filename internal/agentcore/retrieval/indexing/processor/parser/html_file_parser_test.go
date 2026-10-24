package parser

import (
	"strings"
	"testing"
)

// ──────────────────────────── 共享 HTML 工具测试（13.14） ────────────────────────────

// toolTestHTML 工具函数测试用 HTML 母版（正文 ≥50 字符，article 命中主内容选择器）。
const toolTestHTML = `<!DOCTYPE html>
<html><head>
<meta property="og:title" content=" 测试标题 "/>
<title>fallback title</title>
</head><body>
<script>var s = "should not appear";</script>
<style>.x { color: red; }</style>
<article><p>这是正文第一段，包含足够长的中文内容用于跨过一百字符门槛。</p>
<p>这是正文第二段，同样包含足够长的中文内容。</p><div>填充</div></article>
<nav>导航文本</nav>
</body></html>`

// longBody 生成超 50 字符正文（Task 4/5 复用）。
func longBody(n int) string { return strings.Repeat("正文内容段落。", n) }

// TestParseHTML_正常解析 goquery 文档可查询。
func TestParseHTML_正常解析(t *testing.T) {
	doc := parseHTML(toolTestHTML)
	if doc == nil {
		t.Fatal("parseHTML 应返回非 nil 文档")
	}
	if doc.Find("article").Length() != 1 {
		t.Errorf("article 数量 = %d, want 1", doc.Find("article").Length())
	}
}

// TestParseHTML_空输入 空 HTML 返回可查询空文档。
func TestParseHTML_空输入(t *testing.T) {
	doc := parseHTML("")
	if doc == nil || doc.Find("article").Length() != 0 {
		t.Error("空输入应返回空文档")
	}
}

// TestParseHTML_非法输入降级 非法 HTML 降级空文档不 panic。
func TestParseHTML_非法输入降级(t *testing.T) {
	doc := parseHTML("<<<>>>broken")
	if doc == nil {
		t.Error("非法输入应降级非 nil 空文档")
	}
}

// TestExtractTitle_ogTitle命中 og:title 优先且 TrimSpace。
func TestExtractTitle_ogTitle命中(t *testing.T) {
	doc := parseHTML(toolTestHTML)
	if got := extractTitle(doc); got != "测试标题" {
		t.Errorf("title = %q, want %q", got, "测试标题")
	}
}

// TestExtractTitle_回落title标签 og:title content 空串回落 title 标签。
func TestExtractTitle_回落title标签(t *testing.T) {
	html := `<html><head><meta property="og:title" content=""/><title>页签标题</title></head><body></body></html>`
	if got := extractTitle(parseHTML(html)); got != "页签标题" {
		t.Errorf("title = %q, want %q", got, "页签标题")
	}
}

// TestExtractTitle_缺失返回空串 无任何标题来源。
func TestExtractTitle_缺失返回空串(t *testing.T) {
	html := `<html><head></head><body><p>x</p></body></html>`
	if got := extractTitle(parseHTML(html)); got != "" {
		t.Errorf("title = %q, want 空串", got)
	}
}

// TestTextLength_排除scriptStyle script/style 文本不计入且不改树。
func TestTextLength_排除scriptStyle(t *testing.T) {
	doc := parseHTML(toolTestHTML)
	article := doc.Find("article")
	got := textLength(article)
	// 52 code points × UTF-8 3 字节 = 156 字节（Go len 按字节；Python len 按
	// code point——对齐基准是"相对长度门槛"，字节/code point 比例恒定不影响语义）
	if got != 156 {
		t.Errorf("textLength = %d, want 156", got)
	}
	// 不改树：article 仍含原 p/div 子节点
	if article.Find("p").Length() != 2 || article.Find("div").Length() != 1 {
		t.Error("textLength 不应修改树结构")
	}
}

// TestTextLength_scriptStyle子树跳过 script 在选区内其内容不计。
func TestTextLength_scriptStyle子树跳过(t *testing.T) {
	html := `<div>短文本<script>超长脚本内容超长脚本内容超长脚本内容超长脚本内容</script></div>`
	doc := parseHTML(html)
	got := textLength(doc.Find("div"))
	if got > 10 {
		t.Errorf("script 内容应不计入，got %d", got)
	}
}

// TestFindMainContent_选择器命中 article 文本>100 命中。
func TestFindMainContent_选择器命中(t *testing.T) {
	long := strings.Repeat("主内容文本。", 20) // >100
	html := `<html><body><article>` + long + `</article></body></html>`
	sel := findMainContent(parseHTML(html))
	if sel == nil || sel.Length() == 0 {
		t.Fatal("应命中 article")
	}
	if !sel.Is("article") {
		t.Error("应返回 article 节点")
	}
}

// TestFindMainContent_门槛不足继续 选择器命中但文本≤100 跳过走 fallback。
func TestFindMainContent_门槛不足继续(t *testing.T) {
	html := `<html><body><article>短</article><div>` + strings.Repeat("后备块文本。", 30) + `</div></body></html>`
	sel := findMainContent(parseHTML(html))
	if sel == nil || !sel.Is("div") {
		t.Error("article 门槛不足应走 fallback 命中 div")
	}
}

// TestFindMainContent_fallback树序第一个 body 内 article/main/div/section 树序第一个 >200。
func TestFindMainContent_fallback树序第一个(t *testing.T) {
	long := strings.Repeat("块文本。", 40) // >200
	html := `<html><body><section>短</section><div>` + long + `</div></body></html>`
	sel := findMainContent(parseHTML(html))
	if sel == nil || !sel.Is("div") {
		t.Error("树序第一个超200的应为 div")
	}
}

// TestFindMainContent_嵌套外层先到 嵌套块树序外层先遍历到。
func TestFindMainContent_嵌套外层先到(t *testing.T) {
	long := strings.Repeat("嵌套文本。", 40)
	html := `<html><body><div>` + long + `<div>` + long + `</div></div></body></html>`
	sel := findMainContent(parseHTML(html))
	if sel == nil || sel.Length() != 1 || !sel.Is("div") {
		t.Fatal("应返回单一 div")
	}
	// 外层 div 先到：返回节点应包含内层 div（外层是祖先）
	if sel.Find("div").Length() != 1 {
		t.Error("应返回外层 div（含内层）")
	}
}

// TestFindMainContent_空body返回body body 存在但无内容时返回 body 本身。
//
// goquery 解析自动补全 body（html.Parse 行为），"无 body"为不可能路径；
// Python 侧 body None 场景在 Go 对应"body 空"——返回 body 兜底。
func TestFindMainContent_空body返回body(t *testing.T) {
	html := `<html><head><title>x</title></head></html>`
	sel := findMainContent(parseHTML(html))
	if sel == nil || !sel.Is("body") {
		t.Error("空 body 应回退返回 body 自身")
	}
}

// TestGetTextFromSoup_脚本样式移除 script/style 内容不出现在产出。
func TestGetTextFromSoup_脚本样式移除(t *testing.T) {
	doc := parseHTML(toolTestHTML)
	text := getTextFromSoup(doc.Find("body"))
	if strings.Contains(text, "should not appear") {
		t.Error("script 内容不应出现")
	}
	if strings.Contains(text, "color: red") {
		t.Error("style 内容不应出现")
	}
}

// TestGetTextFromSoup_拼接与压缩 文本节点 \n 拼接 + 空白压缩。
func TestGetTextFromSoup_拼接与压缩(t *testing.T) {
	html := `<div><p>第一段</p><p>第二段</p></div>`
	text := getTextFromSoup(parseHTML(html).Find("div"))
	if text != "第一段\n第二段" {
		t.Errorf("text = %q, want %q", text, "第一段\n第二段")
	}
}

// TestGetTextFromSoup_空白归一 多空格压缩单空格。
func TestGetTextFromSoup_空白归一(t *testing.T) {
	html := `<div><p>词一   词二</p><p>第二段</p><p>第三段</p></div>`
	// 三个 p → 两处 \n 分隔；段内多空格压一
	text := getTextFromSoup(parseHTML(html).Find("div"))
	if text != "词一 词二\n第二段\n第三段" {
		t.Errorf("text = %q", text)
	}
	// 空白文本节点（节点间缩进）不产出空行
	html2 := `<div><p>a</p>   <p>b</p></div>`
	text2 := getTextFromSoup(parseHTML(html2).Find("div"))
	if text2 != "a\nb" {
		t.Errorf("text2 = %q, want %q", text2, "a\nb")
	}
}

// TestGetTextFromSoup_首尾TrimSpace 前后空白剥离。
func TestGetTextFromSoup_首尾TrimSpace(t *testing.T) {
	html := `<div>  前后有空格  </div>`
	if got := getTextFromSoup(parseHTML(html).Find("div")); got != "前后有空格" {
		t.Errorf("got %q", got)
	}
}

// TestGetTextFromSoup_nil选区防御 nil/空选区返回空串。
func TestGetTextFromSoup_nil选区防御(t *testing.T) {
	doc := parseHTML(toolTestHTML)
	if got := getTextFromSoup(nil); got != "" {
		t.Errorf("nil 选区应返回空串, got %q", got)
	}
	if got := getTextFromSoup(doc.Find("#nonexistent")); got != "" {
		t.Errorf("空选区应返回空串, got %q", got)
	}
}
