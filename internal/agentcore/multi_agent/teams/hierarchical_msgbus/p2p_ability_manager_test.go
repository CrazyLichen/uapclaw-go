package hierarchical_msgbus

import (
	"context"
	"fmt"
	"testing"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewP2PAbilityManager 验证构造函数。
func TestNewP2PAbilityManager(t *testing.T) {
	supervisor := &SupervisorAgent{}
	m := NewP2PAbilityManager(supervisor, 5, 300.0)
	if m.maxParallel != 5 {
		t.Errorf("期望 maxParallel = 5, 实际 = %d", m.maxParallel)
	}
	if m.timeout != 300.0 {
		t.Errorf("期望 timeout = 300.0, 实际 = %v", m.timeout)
	}
}

// TestNewP2PAbilityManager_最小并行数 验证 maxParallel 最小为 1。
func TestNewP2PAbilityManager_最小并行数(t *testing.T) {
	supervisor := &SupervisorAgent{}
	m := NewP2PAbilityManager(supervisor, 0, 300.0)
	if m.maxParallel != 1 {
		t.Errorf("期望 maxParallel = 1, 实际 = %d", m.maxParallel)
	}
}

// TestP2PAbilityManager_Execute_空ToolCalls 验证空 toolCalls 返回 nil。
func TestP2PAbilityManager_Execute_空ToolCalls(t *testing.T) {
	supervisor := &SupervisorAgent{}
	m := NewP2PAbilityManager(supervisor, 2, 300.0)
	result := m.Execute(context.Background(), nil, nil, nil, "")
	if result != nil {
		t.Errorf("期望 nil, 实际 = %v", result)
	}
}

// TestP2PAbilityManager_Execute_无Agent调用 验证 fast path 委托基类。
func TestP2PAbilityManager_Execute_无Agent调用(t *testing.T) {
	supervisor := &SupervisorAgent{}
	m := NewP2PAbilityManager(supervisor, 2, 300.0)

	// 添加一个普通工具（非 Agent），使 fast path 生效
	toolCard := tool.NewToolCard("test_tool", "test tool", nil, nil)
	m.Add(toolCard)

	// 构造 toolCalls
	toolCalls := []*llmschema.ToolCall{
		{Name: "test_tool", ID: "tc1", Arguments: "{}"},
	}

	// Execute 应委托基类，因工具未注册实例会返回错误但不会 panic
	result := m.Execute(context.Background(), nil, toolCalls, nil, "")
	if len(result) != 1 {
		t.Errorf("期望 1 个结果, 实际 = %d", len(result))
	}
}

// TestP2PAbilityManager_IsAgent 验证 IsAgent 委托。
func TestP2PAbilityManager_IsAgent(t *testing.T) {
	supervisor := &SupervisorAgent{}
	m := NewP2PAbilityManager(supervisor, 2, 300.0)

	// 注册 Agent 卡片
	agentCard := agentschema.NewAgentCard(
		agentschema.WithAgentName("sub_agent"),
		agentschema.WithAgentID("sub_agent_id"),
	)
	m.Add(agentCard)

	if !m.IsAgent("sub_agent") {
		t.Error("期望 IsAgent('sub_agent') = true")
	}
	if m.IsAgent("non_existent") {
		t.Error("期望 IsAgent('non_existent') = false")
	}
}

// TestP2PAbilityManager_Execute_纯Agent调用 验证 Agent 调用走 P2P 派发。
func TestP2PAbilityManager_Execute_纯Agent调用(t *testing.T) {
	supervisor := &SupervisorAgent{}
	m := NewP2PAbilityManager(supervisor, 2, 300.0)

	// 注册 Agent 卡片
	agentCard := agentschema.NewAgentCard(
		agentschema.WithAgentName("sub_agent"),
		agentschema.WithAgentID("sub_agent_id"),
	)
	m.Add(agentCard)

	toolCalls := []*llmschema.ToolCall{
		{Name: "sub_agent", ID: "tc1", Arguments: `{"query": "hello"}`},
	}

	// supervisor 未绑定 runtime，Send 会返回错误
	result := m.Execute(context.Background(), nil, toolCalls, nil, "")
	if len(result) != 1 {
		t.Fatalf("期望 1 个结果, 实际 = %d", len(result))
	}
	// 结果应该是错误（runtime 未绑定）
	if result[0].Result == nil {
		t.Error("期望错误结果，实际为 nil")
	}
}

// TestP2PAbilityManager_Execute_混合调用 验证 Agent + 普通工具并行。
func TestP2PAbilityManager_Execute_混合调用(t *testing.T) {
	supervisor := &SupervisorAgent{}
	m := NewP2PAbilityManager(supervisor, 2, 300.0)

	// 注册 Agent 和 Tool
	agentCard := agentschema.NewAgentCard(
		agentschema.WithAgentName("sub_agent"),
		agentschema.WithAgentID("sub_agent_id"),
	)
	m.Add(agentCard)

	toolCard := tool.NewToolCard("test_tool", "test tool", nil, nil)
	m.Add(toolCard)

	toolCalls := []*llmschema.ToolCall{
		{Name: "sub_agent", ID: "tc1", Arguments: `{"query": "hello"}`},
		{Name: "test_tool", ID: "tc2", Arguments: "{}"},
	}

	// 两者都会失败（runtime 未绑定 / 工具实例不存在），但不应 panic
	result := m.Execute(context.Background(), nil, toolCalls, nil, "")
	if len(result) != 2 {
		t.Fatalf("期望 2 个结果, 实际 = %d", len(result))
	}
}

// TestP2PAbilityManager_Execute_并行限流 验证 Semaphore 限流。
func TestP2PAbilityManager_Execute_并行限流(t *testing.T) {
	supervisor := &SupervisorAgent{}
	m := NewP2PAbilityManager(supervisor, 2, 300.0)

	// 注册 3 个 Agent
	for i := 0; i < 3; i++ {
		agentCard := agentschema.NewAgentCard(
			agentschema.WithAgentName(fmt.Sprintf("agent_%d", i)),
			agentschema.WithAgentID(fmt.Sprintf("agent_id_%d", i)),
		)
		m.Add(agentCard)
	}

	toolCalls := []*llmschema.ToolCall{
		{Name: "agent_0", ID: "tc0", Arguments: "{}"},
		{Name: "agent_1", ID: "tc1", Arguments: "{}"},
		{Name: "agent_2", ID: "tc2", Arguments: "{}"},
	}

	// maxParallel=2，3 个 Agent 调用，应限流但不死锁
	result := m.Execute(context.Background(), nil, toolCalls, nil, "")
	if len(result) != 3 {
		t.Fatalf("期望 3 个结果, 实际 = %d", len(result))
	}
}

// TestP2PAbilityManager_满足AbilityManagerInterface 编译时接口检查。
func TestP2PAbilityManager_满足AbilityManagerInterface(t *testing.T) {
	var _ agentinterfaces.AbilityManagerInterface = (*P2PAbilityManager)(nil)
	t.Log("P2PAbilityManager 满足 AbilityManagerInterface 接口")
}
