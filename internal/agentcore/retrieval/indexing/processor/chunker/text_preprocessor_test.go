package chunker

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// ──────────────────────────── WhitespaceNormalizer ────────────────────────────

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

// ──────────────────────────── URLEmailRemover ────────────────────────────

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
