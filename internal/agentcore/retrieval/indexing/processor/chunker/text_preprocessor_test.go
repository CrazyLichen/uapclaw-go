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
