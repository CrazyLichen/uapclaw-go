package harness

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestRestrictToWorkDirValue_nil返回默认 测试 *bool 为 nil 时返回默认值
func TestRestrictToWorkDirValue_nil返回默认(t *testing.T) {
	assert.True(t, restrictToWorkDirValue(nil, true), "nil + 默认 true → true")
	assert.False(t, restrictToWorkDirValue(nil, false), "nil + 默认 false → false")
}

// TestRestrictToWorkDirValue_显式true 测试 *bool 显式 true
func TestRestrictToWorkDirValue_显式true(t *testing.T) {
	val := true
	assert.True(t, restrictToWorkDirValue(&val, false), "显式 true 应返回 true，忽略默认值")
}

// TestRestrictToWorkDirValue_显式false 测试 *bool 显式 false
func TestRestrictToWorkDirValue_显式false(t *testing.T) {
	val := false
	assert.False(t, restrictToWorkDirValue(&val, true), "显式 false 应返回 false，忽略默认值")
}
