package resources_manager

import (
	"context"
	"testing"

	maschema "github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent/schema"
)

// ──────────────────────────── 导出函数测试 ────────────────────────────

// TestAgentTeamMgr_创建不返回nil 测试 NewAgentTeamMgr 不返回 nil
func TestAgentTeamMgr_创建不返回nil(t *testing.T) {
	mgr := NewAgentTeamMgr()
	if mgr == nil {
		t.Error("NewAgentTeamMgr 不应返回 nil")
	}
}

// TestAgentTeamMgr_添加团队 测试 AddAgentTeam 正常注册。
func TestAgentTeamMgr_添加团队(t *testing.T) {
	mgr := NewAgentTeamMgr()
	provider := func(_ context.Context, _ maschema.TeamCardInterface) (maschema.BaseTeam, error) {
		return nil, nil
	}
	err := mgr.AddAgentTeam("team-1", provider)
	if err != nil {
		t.Fatalf("AddAgentTeam 失败: %v", err)
	}
}

// TestAgentTeamMgr_重复添加报错 测试重复注册同一 ID。
func TestAgentTeamMgr_重复添加报错(t *testing.T) {
	mgr := NewAgentTeamMgr()
	provider := func(_ context.Context, _ maschema.TeamCardInterface) (maschema.BaseTeam, error) {
		return nil, nil
	}
	_ = mgr.AddAgentTeam("team-dup", provider)
	err := mgr.AddAgentTeam("team-dup", provider)
	if err == nil {
		t.Fatal("重复添加应返回错误")
	}
}

// TestAgentTeamMgr_空ID报错 测试空 ID 校验。
func TestAgentTeamMgr_空ID报错(t *testing.T) {
	mgr := NewAgentTeamMgr()
	provider := func(_ context.Context, _ maschema.TeamCardInterface) (maschema.BaseTeam, error) {
		return nil, nil
	}
	err := mgr.AddAgentTeam("", provider)
	if err == nil {
		t.Fatal("空 ID 应返回错误")
	}
}

// TestAgentTeamMgr_nilProvider报错 测试 nil provider 校验。
func TestAgentTeamMgr_nilProvider报错(t *testing.T) {
	mgr := NewAgentTeamMgr()
	err := mgr.AddAgentTeam("team-nil", nil)
	if err == nil {
		t.Fatal("nil provider 应返回错误")
	}
}

// TestAgentTeamMgr_移除团队 测试 RemoveAgentTeam 正常注销。
func TestAgentTeamMgr_移除团队(t *testing.T) {
	mgr := NewAgentTeamMgr()
	provider := func(_ context.Context, _ maschema.TeamCardInterface) (maschema.BaseTeam, error) {
		return nil, nil
	}
	_ = mgr.AddAgentTeam("team-rm", provider)
	removed, err := mgr.RemoveAgentTeam("team-rm")
	if err != nil {
		t.Fatalf("RemoveAgentTeam 失败: %v", err)
	}
	if removed == nil {
		t.Fatal("移除后应返回 provider")
	}
}

// TestAgentTeamMgr_移除不存在报错 测试移除不存在的团队。
func TestAgentTeamMgr_移除不存在报错(t *testing.T) {
	mgr := NewAgentTeamMgr()
	_, err := mgr.RemoveAgentTeam("notexist")
	if err == nil {
		t.Fatal("移除不存在的团队应返回错误")
	}
}

// ──────────────────────────── 非导出函数测试 ────────────────────────────
