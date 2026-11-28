package personal_context

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Python 语义对照：代码块/行内代码内的链接不重写，散文内非 web 链接降级为"原文链接"文本。

func TestNormalizeSourceLinks_代码围栏保护(t *testing.T) {
	markdown := "# 标题\n\n参考 [docs](./local.md)\n\n```python\nx = [\"a\"](./keep.md)\n```\n\n尾行 [b](other.md)\n"
	out := NormalizeSourceLinks(markdown, nil)
	require.Contains(t, out, "参考 docs（原文链接：./local.md）")
	require.Contains(t, out, "尾行 b（原文链接：other.md）")
	// 围栏内逐字节保留
	require.Contains(t, out, "x = [\"a\"](./keep.md)")
}

func TestNormalizeSourceLinks_web链接保留(t *testing.T) {
	out := NormalizeSourceLinks("[gitee](https://example.com/a) 与 [本地](x.md)", nil)
	require.Contains(t, out, "[gitee](https://example.com/a)")
	require.Contains(t, out, "本地（原文链接：x.md）")
}

func TestNormalizeSourceLinks_行内代码保护(t *testing.T) {
	out := NormalizeSourceLinks("看 `code [x](y.md)` 与 [a](b.md)", nil)
	require.Contains(t, out, "`code [x](y.md)`")
	require.Contains(t, out, "a（原文链接：b.md）")
}

func TestNormalizeSourceLinks_链接定义收集(t *testing.T) {
	markdown := "[ref]: ./guide.md\n\n前往 [guide][ref]\n"
	out := NormalizeSourceLinks(markdown, nil)
	require.Contains(t, out, "guide（原文链接：./guide.md）")
}

func TestNormalizeSourceLinks_renderLink回调(t *testing.T) {
	out := NormalizeSourceLinks("[a](b.md)", func(label, target string) string {
		return "<R:" + label + "|" + target + ">"
	})
	require.Equal(t, "<R:a|b.md>", out)
}

func TestMarkdownReferenceText_剔除代码(t *testing.T) {
	markdown := "prose [x](y.md)\n\n```\nliteral [z](w.md)\n```\n\n`inline [k](v.md)` tail\n"
	out := MarkdownReferenceText(markdown)
	require.Contains(t, out, "prose [x](y.md)")
	require.NotContains(t, out, "literal [z]")
	require.NotContains(t, out, "[k](v.md)")
}

func TestRewriteMarkdownProse_逐字节保留代码(t *testing.T) {
	markdown := "before\n\n```go\nraw [x](y)\n```\n\nafter\n"
	out := RewriteMarkdownProse(markdown, func(s string) string { return strings.ToUpper(s) })
	require.Contains(t, out, "```go\nraw [x](y)\n```")
	require.Contains(t, out, "BEFORE")
	require.Contains(t, out, "AFTER")
}

func TestCodeEnd_闭围栏定位(t *testing.T) {
	require.Equal(t, 3, codeEnd("`a`", 0))
	// Python start+length+closing.end()：``ab`` → 0+2+4=6
	require.Equal(t, 6, codeEnd("``ab``", 0))
	// 无精确闭合（后随更长反引号段）→ 原样返回 start
	require.Equal(t, 0, codeEnd("``x```", 0))
	require.Equal(t, 6, codeEnd("``ab``cd", 0))
}

func TestDestination_目的地剥离(t *testing.T) {
	require.Equal(t, "./a.md", destination("<./a.md>"))
	require.Equal(t, "./a.md", destination("./a.md \"标题\""))
	require.Equal(t, "./a.md", destination("./a.md 'title'"))
	require.Equal(t, "./a.md", destination("./a.md"))
}

func TestWebTarget_协议判定(t *testing.T) {
	require.True(t, webTarget("https://example.com/x"))
	require.True(t, webTarget("<http://example.com>"))
	require.False(t, webTarget("./local.md"))
	require.False(t, webTarget("ftp://example.com"))
}

func TestLiteral_转义(t *testing.T) {
	require.Equal(t, "a&lt;b&gt;&amp;c", literal("a<b>&c"))
	require.Contains(t, literal("[x]"), "&#91;")
}
