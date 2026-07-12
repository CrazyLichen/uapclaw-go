package gateway

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestShouldBrowserRestart_通过导出函数 验证 ShouldBrowserRestart 导出函数可用。
func TestShouldBrowserRestart_通过导出函数(t *testing.T) {
	if ShouldBrowserRestart([]string{"MODEL_PROVIDER"}) != true {
		t.Error("MODEL_PROVIDER 应触发重启")
	}
	if ShouldBrowserRestart([]string{"UNKNOWN_KEY"}) != false {
		t.Error("UNKNOWN_KEY 不应触发重启")
	}
}

// TestIsValidationError 测试验证错误检测
func TestIsValidationError(t *testing.T) {
	assert.True(t, isValidationError("ValidationError: invalid field"))
	assert.True(t, isValidationError("got validation error"))
	assert.True(t, isValidationError("Field required"))
	assert.False(t, isValidationError("internal error"))
	assert.False(t, isValidationError(""))
	assert.False(t, isValidationError("connection refused"))
}
