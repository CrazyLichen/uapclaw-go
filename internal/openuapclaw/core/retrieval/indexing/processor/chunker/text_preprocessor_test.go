package chunker

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// ──────────────────────────── WhitespaceNormalizer 空白规整器 ────────────────────────────

// TestWhitespaceNormalizer_正常文本 对齐 Python test_process_normal_text
func TestWhitespaceNormalizer_正常文本(t *testing.T) {
	n := WhitespaceNormalizer{}
	assert.Equal(t, "This is a test", n.Process("This is a test"))
}

// TestWhitespaceNormalizer_多空格 对齐 Python test_process_multiple_spaces
func TestWhitespaceNormalizer_多空格(t *testing.T) {
	n := WhitespaceNormalizer{}
	assert.Equal(t, "This is a test", n.Process("This   is    a     test"))
}

// TestWhitespaceNormalizer_换行 对齐 Python test_process_newlines
func TestWhitespaceNormalizer_换行(t *testing.T) {
	n := WhitespaceNormalizer{}
	assert.Equal(t, "This is a test", n.Process("This\nis\na\ntest"))
}

// TestWhitespaceNormalizer_制表符 对齐 Python test_process_tabs
func TestWhitespaceNormalizer_制表符(t *testing.T) {
	n := WhitespaceNormalizer{}
	assert.Equal(t, "This is a test", n.Process("This\tis\ta\ttest"))
}

// TestWhitespaceNormalizer_混合空白 对齐 Python test_process_mixed_whitespace
func TestWhitespaceNormalizer_混合空白(t *testing.T) {
	n := WhitespaceNormalizer{}
	assert.Equal(t, "This is a test", n.Process("This  \n\t  is  \n\t  a  \n\t  test"))
}

// TestWhitespaceNormalizer_两端空白 对齐 Python test_process_leading_trailing_whitespace
func TestWhitespaceNormalizer_两端空白(t *testing.T) {
	n := WhitespaceNormalizer{}
	assert.Equal(t, "This is a test", n.Process("   This is a test   "))
}

// TestWhitespaceNormalizer_空文本 对齐 Python test_process_empty_string + test_process_none（并入）
func TestWhitespaceNormalizer_空文本(t *testing.T) {
	n := WhitespaceNormalizer{}
	assert.Equal(t, "", n.Process(""))
}

// TestWhitespaceNormalizer_全角空格归一 Go 专项：U+3000 在 Python \s 集内（对拍基准）
func TestWhitespaceNormalizer_全角空格归一(t *testing.T) {
	n := WhitespaceNormalizer{}
	assert.Equal(t, "This is a test", n.Process("This　is a test"))
}

// TestWhitespaceNormalizer_补偿集命中 Go 专项：\x1c 在 Python \s 集内（.NET \s 缺）
func TestWhitespaceNormalizer_补偿集命中(t *testing.T) {
	n := WhitespaceNormalizer{}
	assert.Equal(t, "a b", n.Process("a\x1cb"))
}

// ──────────────────────────── URLEmailRemover URL邮箱移除器 ────────────────────────────

// TestURLEmailRemover_构造默认值 对齐 Python test_init_defaults
func TestURLEmailRemover_构造默认值(t *testing.T) {
	r := URLEmailRemover{}
	assert.False(t, r.RemoveURLs) // Go 零值 false；"默认 true" 由构造方显式传参
	assert.False(t, r.RemoveEmails)
	assert.Equal(t, "", r.Replacement)
}

// TestURLEmailRemover_构造自定义值 对齐 Python test_init_custom
func TestURLEmailRemover_构造自定义值(t *testing.T) {
	r := URLEmailRemover{RemoveURLs: false, RemoveEmails: true, Replacement: "[removed]"}
	assert.False(t, r.RemoveURLs)
	assert.True(t, r.RemoveEmails)
	assert.Equal(t, "[removed]", r.Replacement)
}

// TestURLEmailRemover_移除HTTP_URL 对齐 Python test_remove_urls_http
// Python 基准: 'Visit http://example.com for more info' → 'Visit  for more info'
func TestURLEmailRemover_移除HTTP_URL(t *testing.T) {
	r := URLEmailRemover{RemoveURLs: true, RemoveEmails: true}
	assert.Equal(t, "Visit  for more info", r.Process("Visit http://example.com for more info"))
}

// TestURLEmailRemover_移除HTTPS_URL 对齐 Python test_remove_urls_https
func TestURLEmailRemover_移除HTTPS_URL(t *testing.T) {
	r := URLEmailRemover{RemoveURLs: true, RemoveEmails: true}
	assert.Equal(t, "Visit  for more info", r.Process("Visit https://example.com for more info"))
}

// TestURLEmailRemover_移除WWW_URL 对齐 Python test_remove_urls_www
func TestURLEmailRemover_移除WWW_URL(t *testing.T) {
	r := URLEmailRemover{RemoveURLs: true, RemoveEmails: true}
	assert.Equal(t, "Visit  for more info", r.Process("Visit www.example.com for more info"))
}

// TestURLEmailRemover_移除邮箱 对齐 Python test_remove_emails
// Python 基准: 'Contact us at test@example.com for support' → 'Contact us at  for support'
func TestURLEmailRemover_移除邮箱(t *testing.T) {
	r := URLEmailRemover{RemoveURLs: true, RemoveEmails: true}
	assert.Equal(t, "Contact us at  for support", r.Process("Contact us at test@example.com for support"))
}

// TestURLEmailRemover_URL替换字符串 对齐 Python test_remove_urls_with_replacement
// Python 基准: 'Visit http://example.com for more info' + '[URL]' → 'Visit [URL] for more info'
func TestURLEmailRemover_URL替换字符串(t *testing.T) {
	r := URLEmailRemover{RemoveURLs: true, RemoveEmails: true, Replacement: "[URL]"}
	assert.Equal(t, "Visit [URL] for more info", r.Process("Visit http://example.com for more info"))
}

// TestURLEmailRemover_邮箱替换字符串 对齐 Python test_remove_emails_with_replacement
// Python 基准: 'Contact test@example.com' + '[EMAIL]' → 'Contact [EMAIL]'
func TestURLEmailRemover_邮箱替换字符串(t *testing.T) {
	r := URLEmailRemover{RemoveURLs: true, RemoveEmails: true, Replacement: "[EMAIL]"}
	assert.Equal(t, "Contact [EMAIL]", r.Process("Contact test@example.com"))
}

// TestURLEmailRemover_禁用URL移除 对齐 Python test_disable_url_removal
func TestURLEmailRemover_禁用URL移除(t *testing.T) {
	r := URLEmailRemover{RemoveURLs: false, RemoveEmails: true}
	assert.Contains(t, r.Process("Visit http://example.com for more info"), "http://example.com")
}

// TestURLEmailRemover_禁用邮箱移除 Go 专项（对偶覆盖）
// Python 基准（纯 re 复刻验证）：remove_emails=False 时 EMAIL_PATTERN 不应用；
// 注意 remove_urls=True 时 COM_PATTERN 仍会吞掉 'xxx@example.com'（\S+?\.com
// 把邮箱当域名匹配——Python 原样行为），故本用例断言直通需两开关全关
func TestURLEmailRemover_禁用邮箱移除(t *testing.T) {
	r := URLEmailRemover{RemoveURLs: false, RemoveEmails: false}
	assert.Contains(t, r.Process("Contact test@example.com"), "test@example.com")
}

// TestURLEmailRemover_空文本 对齐 Python test_process_empty_string + test_process_none（并入）
func TestURLEmailRemover_空文本(t *testing.T) {
	r := URLEmailRemover{RemoveURLs: true, RemoveEmails: true}
	assert.Equal(t, "", r.Process(""))
}

// TestURLEmailRemover_URL加邮箱混合 Go 专项（对拍基准）
// Python 基准: 'Visit https://example.com and email a@b.com now' → 'Visit  and email  now'
func TestURLEmailRemover_URL加邮箱混合(t *testing.T) {
	r := URLEmailRemover{RemoveURLs: true, RemoveEmails: true}
	assert.Equal(t, "Visit  and email  now", r.Process("Visit https://example.com and email a@b.com now"))
}

// ──────────────────────────── SpecialCharacterNormalizer 特殊字符规整器 ────────────────────────────

// TestSpecialCharacterNormalizer_构造默认值 对齐 Python test_init_defaults
func TestSpecialCharacterNormalizer_构造默认值(t *testing.T) {
	n := SpecialCharacterNormalizer{}
	assert.Equal(t, "", n.CharsToRemove)
	assert.Empty(t, n.CharsToReplace)
}

// TestSpecialCharacterNormalizer_移除控制字符 对齐 Python test_remove_control_characters
// Python 基准: 'Test\x00text\x1fwith\x7fcontrol' → 'Testtextwithcontrol'
func TestSpecialCharacterNormalizer_移除控制字符(t *testing.T) {
	n := SpecialCharacterNormalizer{}
	assert.Equal(t, "Testtextwithcontrol", n.Process("Test\x00text\x1fwith\x7fcontrol"))
}

// TestSpecialCharacterNormalizer_替换字符 对齐 Python test_replace_characters
// Python 基准: 'Tom & Jerry @ home' + {'&':'and','@':'at'} → 'Tom and Jerry at home'
// 注意：& 和 @ 均为单字符，REDUNDANT {2,} 不触发（Python 行为）
func TestSpecialCharacterNormalizer_替换字符(t *testing.T) {
	n := SpecialCharacterNormalizer{CharsToReplace: []CharReplacement{{Old: "&", New: "and"}, {Old: "@", New: "at"}}}
	assert.Equal(t, "Tom and Jerry at home", n.Process("Tom & Jerry @ home"))
}

// TestSpecialCharacterNormalizer_移除指定字符 对齐 Python test_remove_specified_characters
// Python 基准: 'Test!text@with#special' + '!@#' → 'Testtextwithspecial'
func TestSpecialCharacterNormalizer_移除指定字符(t *testing.T) {
	n := SpecialCharacterNormalizer{CharsToRemove: "!@#"}
	assert.Equal(t, "Testtextwithspecial", n.Process("Test!text@with#special"))
}

// TestSpecialCharacterNormalizer_空文本 对齐 Python test_process_empty_string + test_process_none（并入）
func TestSpecialCharacterNormalizer_空文本(t *testing.T) {
	n := SpecialCharacterNormalizer{}
	assert.Equal(t, "", n.Process(""))
}

// TestSpecialCharacterNormalizer_冗余符号删除 Go 专项（对拍基准，Python \w 语义验证）
// Python 基准: 'hello!!!world' → 'helloworld'（!!! 连续 3 个非词非空白符号 → 删）
func TestSpecialCharacterNormalizer_冗余符号删除(t *testing.T) {
	n := SpecialCharacterNormalizer{}
	assert.Equal(t, "helloworld", n.Process("hello!!!world"))
}

// TestSpecialCharacterNormalizer_CJK保护字符不动 Go 专项（对拍基准）
// Python 基准: '中文。。。句子' → 不变（。在 protected 集，双前瞻命中跳过）
func TestSpecialCharacterNormalizer_CJK保护字符不动(t *testing.T) {
	n := SpecialCharacterNormalizer{}
	assert.Equal(t, "中文。。。句子", n.Process("中文。。。句子"))
}

// TestSpecialCharacterNormalizer_双标记符不动 Go 专项（对拍基准）
// Python 基准: '###标题###' → 不变（(?![#*\-_]{2,}) 前瞻命中跳过）
func TestSpecialCharacterNormalizer_双标记符不动(t *testing.T) {
	n := SpecialCharacterNormalizer{}
	assert.Equal(t, "###标题###", n.Process("###标题###"))
}

// TestSpecialCharacterNormalizer_货币符号删除 Go 专项（对拍基准）
// Python 基准: '￥￥￥三个' → '三个'（￥ 连续 3 个非词符号 → 删）
func TestSpecialCharacterNormalizer_货币符号删除(t *testing.T) {
	n := SpecialCharacterNormalizer{}
	assert.Equal(t, "三个", n.Process("￥￥￥三个"))
}

// TestSpecialCharacterNormalizer_单符号不动 Go 专项（对拍基准）
// Python 基准: '单一&符号' → 不变（{2,} 需连续 2+）
func TestSpecialCharacterNormalizer_单符号不动(t *testing.T) {
	n := SpecialCharacterNormalizer{}
	assert.Equal(t, "单一&符号", n.Process("单一&符号"))
}

// TestSpecialCharacterNormalizer_带圈数字保留 Go 专项（D1' 显式 \w 集回归，对拍基准）
// Python 基准: '①②③序号' → 不变（①②③ 是 Python \w 词字符；regexp2 原生 \w 缺 No 类会误删）
func TestSpecialCharacterNormalizer_带圈数字保留(t *testing.T) {
	n := SpecialCharacterNormalizer{}
	assert.Equal(t, "①②③序号", n.Process("①②③序号"))
}

// TestSpecialCharacterNormalizer_连字符不动 Go 专项（对拍基准）
// Python 基准: 'a-b-c正常' → 不变（- 在字符类白名单内）
func TestSpecialCharacterNormalizer_连字符不动(t *testing.T) {
	n := SpecialCharacterNormalizer{}
	assert.Equal(t, "a-b-c正常", n.Process("a-b-c正常"))
}

// TestSpecialCharacterNormalizer_邮箱符号不动 Go 专项（对拍基准）
// Python 基准: 'a@b@c' → 不变（@ 单字符不触发 {2,}）
func TestSpecialCharacterNormalizer_邮箱符号不动(t *testing.T) {
	n := SpecialCharacterNormalizer{}
	assert.Equal(t, "a@b@c", n.Process("a@b@c"))
}

// TestSpecialCharacterNormalizer_中英混合不动 Go 专项（对拍基准）
func TestSpecialCharacterNormalizer_中英混合不动(t *testing.T) {
	n := SpecialCharacterNormalizer{}
	assert.Equal(t, "mixed中English文", n.Process("mixed中English文"))
}

// TestSpecialCharacterNormalizer_链式替换插入序 Go 专项（F 决策回归，对拍基准）
// Python 基准: '链式a→b→c' + {'a':'b','b':'c'} → '链式c→c→c'
// （dict 插入序 a→b 先执行，b→c 后执行，a 经两步变 c）
func TestSpecialCharacterNormalizer_链式替换插入序(t *testing.T) {
	n := SpecialCharacterNormalizer{CharsToReplace: []CharReplacement{{Old: "a", New: "b"}, {Old: "b", New: "c"}}}
	assert.Equal(t, "链式c→c→c", n.Process("链式a→b→c"))
}

// TestSpecialCharacterNormalizer_组合记号不动 Go 专项（对拍基准，Mn 类回归）
// Python 基准: 'á组合记号' → 不变（组合尖音符后 REDUNDANT 段不足 2 字符）
func TestSpecialCharacterNormalizer_组合记号不动(t *testing.T) {
	n := SpecialCharacterNormalizer{}
	assert.Equal(t, "á组合记号", n.Process("á组合记号"))
}

// ──────────────────────────── PreprocessingPipeline 预处理管线 ────────────────────────────

// TestPreprocessingPipeline_空管线 对齐 Python test_init_empty
func TestPreprocessingPipeline_空管线(t *testing.T) {
	p := NewPreprocessingPipeline()
	assert.Equal(t, 0, p.Len())
}

// TestPreprocessingPipeline_带预处理器构造 对齐 Python test_init_with_preprocessors
func TestPreprocessingPipeline_带预处理器构造(t *testing.T) {
	p := NewPreprocessingPipeline(WhitespaceNormalizer{}, URLEmailRemover{RemoveURLs: true, RemoveEmails: true})
	assert.Equal(t, 2, p.Len())
}

// TestPreprocessingPipeline_追加预处理器 对齐 Python test_add_preprocessor
func TestPreprocessingPipeline_追加预处理器(t *testing.T) {
	p := NewPreprocessingPipeline()
	p.AddPreprocessor(WhitespaceNormalizer{})
	assert.Equal(t, 1, p.Len())
}

// TestPreprocessingPipeline_单预处理器 对齐 Python test_process_single_preprocessor
func TestPreprocessingPipeline_单预处理器(t *testing.T) {
	p := NewPreprocessingPipeline(WhitespaceNormalizer{})
	assert.Equal(t, "This is a test", p.Process("This   is   a   test"))
}

// TestPreprocessingPipeline_顺序应用 Go 专项（对拍基准）
// TextChunker 组装序：whitespace 先、url 后
// Python 基准: 'Visit https://example.com and email a@b.com now' → 'Visit  and email  now'
func TestPreprocessingPipeline_顺序应用(t *testing.T) {
	p := NewPreprocessingPipeline(
		WhitespaceNormalizer{},
		URLEmailRemover{RemoveURLs: true, RemoveEmails: true},
	)
	assert.Equal(t, "Visit  and email  now", p.Process("Visit https://example.com and email a@b.com now"))
}

// TestPreprocessingPipeline_空文本 对齐 Python test_process_empty_string
func TestPreprocessingPipeline_空文本(t *testing.T) {
	p := NewPreprocessingPipeline(WhitespaceNormalizer{})
	assert.Equal(t, "", p.Process(""))
}
