package agent_teams

import "testing"

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

func TestDefaultTeamMemoryDir(t *testing.T) {
	result := DefaultTeamMemoryDir("test-team")
	expected := TeamHome("test-team") + "/team-workspace/team-memory"
	if result != expected {
		t.Errorf("DefaultTeamMemoryDir = %q, expected %q", result, expected)
	}
}

func TestDefaultTeamMemoryDir_空团队名(t *testing.T) {
	result := DefaultTeamMemoryDir("")
	if result == "" {
		t.Error("期望非空路径")
	}
}
