package gateway

import (
	"testing"
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
