package resources_manager

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// TestResourceRegistry_创建 测试创建资源注册表后所有子管理器非 nil
func TestResourceRegistry_创建(t *testing.T) {
	registry := NewResourceRegistry()

	if registry.toolMgr == nil {
		t.Fatal("期望 toolMgr 非 nil")
	}
	if registry.workflowMgr == nil {
		t.Fatal("期望 workflowMgr 非 nil")
	}
	if registry.promptMgr == nil {
		t.Fatal("期望 promptMgr 非 nil")
	}
	if registry.modelMgr == nil {
		t.Fatal("期望 modelMgr 非 nil")
	}
	if registry.agentMgr == nil {
		t.Fatal("期望 agentMgr 非 nil")
	}
	if registry.agentTeamMgr == nil {
		t.Fatal("期望 agentTeamMgr 非 nil")
	}
	if registry.sysOperationMgr == nil {
		t.Fatal("期望 sysOperationMgr 非 nil")
	}
}

// TestResourceRegistry_访问器 测试 7 个访问器返回正确类型
func TestResourceRegistry_访问器(t *testing.T) {
	registry := NewResourceRegistry()

	if registry.Tool() == nil {
		t.Fatal("期望 Tool() 非 nil")
	}
	if registry.Workflow() == nil {
		t.Fatal("期望 Workflow() 非 nil")
	}
	if registry.Prompt() == nil {
		t.Fatal("期望 Prompt() 非 nil")
	}
	if registry.Model() == nil {
		t.Fatal("期望 Model() 非 nil")
	}
	if registry.Agent() == nil {
		t.Fatal("期望 Agent() 非 nil")
	}
	if registry.AgentTeam() == nil {
		t.Fatal("期望 AgentTeam() 非 nil")
	}
	if registry.SysOperation() == nil {
		t.Fatal("期望 SysOperation() 非 nil")
	}

	// 验证访问器返回的是同一实例
	if registry.Tool() != registry.toolMgr {
		t.Fatal("期望 Tool() 返回 toolMgr")
	}
	if registry.Workflow() != registry.workflowMgr {
		t.Fatal("期望 Workflow() 返回 workflowMgr")
	}
	if registry.Prompt() != registry.promptMgr {
		t.Fatal("期望 Prompt() 返回 promptMgr")
	}
	if registry.Model() != registry.modelMgr {
		t.Fatal("期望 Model() 返回 modelMgr")
	}
	if registry.Agent() != registry.agentMgr {
		t.Fatal("期望 Agent() 返回 agentMgr")
	}
	if registry.AgentTeam() != registry.agentTeamMgr {
		t.Fatal("期望 AgentTeam() 返回 agentTeamMgr")
	}
	if registry.SysOperation() != registry.sysOperationMgr {
		t.Fatal("期望 SysOperation() 返回 sysOperationMgr")
	}
}

// TestResourceRegistry_RemoveByID_工具 测试通过 RemoveByID 移除工具
func TestResourceRegistry_RemoveByID_工具(t *testing.T) {
	registry := NewResourceRegistry()

	// 添加工具
	t1 := &stubTool{card: tool.NewToolCard("test_tool", "测试工具", nil, nil)}
	err := registry.Tool().AddTool("tool-1", t1)
	if err != nil {
		t.Fatalf("添加工具失败: %v", err)
	}

	// 确认工具存在
	_, err = registry.Tool().GetTool("tool-1", nil)
	if err != nil {
		t.Fatalf("获取工具失败: %v", err)
	}

	// 通过 RemoveByID 移除
	registry.RemoveByID("tool-1")

	// 确认工具已被移除
	_, err = registry.Tool().GetTool("tool-1", nil)
	if err == nil {
		t.Fatal("期望工具已被移除，实际仍存在")
	}
}

// TestResourceRegistry_RemoveByID_不存在 测试不存在的 ID 不报错
func TestResourceRegistry_RemoveByID_不存在(t *testing.T) {
	registry := NewResourceRegistry()

	// 不存在的 ID 不应 panic 或报错
	registry.RemoveByID("non-existent-id")
}

// ──────────────────────────── 非导出函数 ────────────────────────────
