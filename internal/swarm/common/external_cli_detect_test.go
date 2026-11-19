package common

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestIsWindowsScriptPath_后缀判定 脚本后缀为真、可执行文件为假（与运行平台无关）。
// 随 D-15.16-d9 移位自 web_handlers_external_cli_test.go。
func TestIsWindowsScriptPath_后缀判定(t *testing.T) {
	assert.True(t, isWindowsScriptPath("cli.bat"))
	assert.True(t, isWindowsScriptPath("cli.CMD"))
	assert.True(t, isWindowsScriptPath("cli.ps1"))
	assert.False(t, isWindowsScriptPath("cli.exe"))
	assert.False(t, isWindowsScriptPath("cli"))
}

// TestParseVersionTuple 三段/两段/非数字/空串。
func TestParseVersionTuple(t *testing.T) {
	got, ok := parseVersionTuple("1.2.3")
	assert.True(t, ok)
	assert.Equal(t, [3]int{1, 2, 3}, got)

	got, ok = parseVersionTuple("1.2")
	assert.True(t, ok, "缺段按 0 补")
	assert.Equal(t, [3]int{1, 2, 0}, got)

	_, ok = parseVersionTuple("1.x.3")
	assert.False(t, ok, "非数字段不可解析")

	_, ok = parseVersionTuple("  ")
	assert.False(t, ok)
}

// TestCompareVersionTuple 三段逐位比较。
func TestCompareVersionTuple(t *testing.T) {
	assert.Equal(t, -1, compareVersionTuple([3]int{0, 2, 100}, [3]int{0, 2, 115}))
	assert.Equal(t, 0, compareVersionTuple([3]int{1, 0, 0}, [3]int{1, 0, 0}))
	assert.Equal(t, 1, compareVersionTuple([3]int{1, 10, 0}, [3]int{1, 9, 9}))
}
