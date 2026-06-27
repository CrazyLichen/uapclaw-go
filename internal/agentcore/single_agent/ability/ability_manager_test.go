package ability

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/mcp"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	resourcesmanager "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/resources_manager"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/rail"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// fakeTool 用于测试的模拟工具
type fakeTool struct {
	card   *tool.ToolCard
	result map[string]any
	err    error
}

func (f *fakeTool) Card() *tool.ToolCard { return f.card }
func (f *fakeTool) Invoke(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (map[string]any, error) {
	return f.result, f.err
}
func (f *fakeTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.ErrStreamNotSupported
}

// newTestResourceMgr 创建用于测试的 ResourceMgr，并注册指定的工具/工作流/Agent 实例。
func newTestResourceMgr(
	tools map[string]tool.Tool,
	workflows map[string]interfaces.Workflow,
	agents map[string]interfaces.BaseAgent,
) *resourcesmanager.ResourceMgr {
	mgr := resourcesmanager.NewResourceMgr()
	for id, t := range tools {
		_ = mgr.AddTool(t)
		_ = mgr.AddTool(t, resourcesmanager.WithTag(resourcesmanager.Tag(id)))
	}
	for id, wf := range workflows {
		card := wf.Card()
		if card == nil {
			card = schema.NewWorkflowCard(schema.WithID(id))
		}
		provider := func(_ context.Context, _ *schema.WorkflowCard) (interfaces.Workflow, error) {
			return wf, nil
		}
		_ = mgr.AddWorkflow(card, resourcesmanager.WorkflowProvider(provider))
	}
	for id, ag := range agents {
		agCard := ag.Card()
		if agCard == nil {
			agCard = agentschema.NewAgentCard(schema.WithID(id))
		}
		provider := func(_ context.Context, _ *agentschema.AgentCard) (interfaces.BaseAgent, error) {
			return ag, nil
		}
		_ = mgr.AddAgent(agCard, resourcesmanager.AgentProvider(provider))
	}
	return mgr
}

// ──────────────────────────── 导出函数 ────────────────────────────

// isAbilityExecutionErrorInResult 检查 ExecuteResult.Result 是否为 *AbilityExecutionError。
func isAbilityExecutionErrorInResult(result any) bool {
	err, ok := result.(error)
	if !ok {
		return false
	}
	var aee *AbilityExecutionError
	return errors.As(err, &aee)
}

func TestAbilityManager_Add_Tool(t *testing.T) {
	am := NewAbilityManager(nil)
	card := tool.NewToolCard("search", "搜索", nil, nil)
	result := am.Add(card)
	if !result.Added {
		t.Error("应成功添加")
	}
	if result.Reason != "added_tool" {
		t.Errorf("Reason = %q, want added_tool", result.Reason)
	}
}

func TestAbilityManager_Add_重复Tool(t *testing.T) {
	am := NewAbilityManager(nil)
	card1 := tool.NewToolCard("search", "搜索1", nil, nil)
	card2 := tool.NewToolCard("search", "搜索2", nil, nil)
	am.Add(card1)
	result := am.Add(card2)
	if result.Added {
		t.Error("重复 name 不应添加")
	}
	if result.Reason != "duplicate_tool" {
		t.Errorf("Reason = %q, want duplicate_tool", result.Reason)
	}
	// 保留旧的
	got := am.Get("search")
	if got == nil {
		t.Fatal("Get 不应返回 nil")
	}
	if got.AbilityID() != card1.ID {
		t.Errorf("应保留旧的 Card")
	}
}

func TestAbilityManager_Add_Workflow(t *testing.T) {
	am := NewAbilityManager(nil)
	wf := schema.NewWorkflowCard(schema.WithName("my_wf"), schema.WithDescription("工作流"))
	result := am.Add(wf)
	if !result.Added {
		t.Error("应成功添加")
	}
	if result.Reason != "added_workflow" {
		t.Errorf("Reason = %q, want added_workflow", result.Reason)
	}
}

func TestAbilityManager_Add_Agent(t *testing.T) {
	am := NewAbilityManager(nil)
	ag := agentschema.NewAgentCard(schema.WithName("my_agent"), schema.WithDescription("Agent"))
	result := am.Add(ag)
	if !result.Added {
		t.Error("应成功添加")
	}
	if result.Reason != "added_agent" {
		t.Errorf("Reason = %q, want added_agent", result.Reason)
	}
}

func TestAbilityManager_Add_McpServer(t *testing.T) {
	am := NewAbilityManager(nil)
	mc := mcp.NewMcpServerConfig("test_server", "http://localhost:8080/sse", "sse")
	result := am.Add(mc)
	if !result.Added {
		t.Error("应成功添加")
	}
	if result.Reason != "added_mcp_server" {
		t.Errorf("Reason = %q, want added_mcp_server", result.Reason)
	}
}

func TestAbilityManager_AddMany(t *testing.T) {
	am := NewAbilityManager(nil)
	results := am.AddMany([]schema.Ability{
		tool.NewToolCard("t1", "工具1", nil, nil),
		schema.NewWorkflowCard(schema.WithName("w1"), schema.WithDescription("工作流1")),
	})
	if len(results) != 2 {
		t.Fatalf("len = %d, want 2", len(results))
	}
	if !results[0].Added || results[0].Reason != "added_tool" {
		t.Errorf("第一个结果应为 added_tool，实际 %v", results[0])
	}
	if !results[1].Added || results[1].Reason != "added_workflow" {
		t.Errorf("第二个结果应为 added_workflow，实际 %v", results[1])
	}
}

func TestAbilityManager_Remove_Tool(t *testing.T) {
	am := NewAbilityManager(nil)
	card := tool.NewToolCard("search", "搜索", nil, nil)
	am.Add(card)
	removed := am.Remove("search")
	if removed == nil {
		t.Fatal("应返回被移除的 Ability")
	}
	if removed.AbilityName() != "search" {
		t.Errorf("AbilityName = %q, want search", removed.AbilityName())
	}
	if am.Get("search") != nil {
		t.Error("移除后 Get 应返回 nil")
	}
}

func TestAbilityManager_Remove_McpServer级联删除(t *testing.T) {
	am := NewAbilityManager(nil)
	// 添加 MCP 服务器
	mc := mcp.NewMcpServerConfig("my_server", "http://localhost:8080/sse", "sse")
	am.Add(mc)
	// 手动添加属于该 MCP 服务器的工具
	mcpTool := tool.NewToolCard("mcp_my_server_search", "MCP搜索", nil, nil)
	mcpTool.ID = mc.ServerID + ".search"
	am.Add(mcpTool)
	// 添加不属于该 MCP 服务器的工具
	otherTool := tool.NewToolCard("other_tool", "其他工具", nil, nil)
	am.Add(otherTool)
	// 移除 MCP 服务器
	am.Remove("my_server")
	// 验证 MCP 工具被级联删除
	if am.Get("mcp_my_server_search") != nil {
		t.Error("MCP 工具应被级联删除")
	}
	// 验证其他工具不受影响
	if am.Get("other_tool") == nil {
		t.Error("其他工具不应被删除")
	}
}

func TestAbilityManager_Get(t *testing.T) {
	am := NewAbilityManager(nil)
	am.Add(tool.NewToolCard("tool1", "工具", nil, nil))
	am.Add(schema.NewWorkflowCard(schema.WithName("wf1"), schema.WithDescription("工作流")))
	am.Add(agentschema.NewAgentCard(schema.WithName("ag1"), schema.WithDescription("Agent")))

	if am.Get("tool1") == nil {
		t.Error("tool1 应存在")
	}
	if am.Get("wf1") == nil {
		t.Error("wf1 应存在")
	}
	if am.Get("ag1") == nil {
		t.Error("ag1 应存在")
	}
	if am.Get("nonexistent") != nil {
		t.Error("不存在的名称应返回 nil")
	}
}

func TestAbilityManager_List(t *testing.T) {
	am := NewAbilityManager(nil)
	am.Add(tool.NewToolCard("t1", "工具", nil, nil))
	am.Add(schema.NewWorkflowCard(schema.WithName("w1"), schema.WithDescription("工作流")))
	am.Add(agentschema.NewAgentCard(schema.WithName("a1"), schema.WithDescription("Agent")))
	list := am.List()
	if len(list) != 3 {
		t.Errorf("List 长度 = %d, want 3", len(list))
	}
}

func TestAbilityManager_ReorderTools(t *testing.T) {
	am := NewAbilityManager(nil)
	am.Add(tool.NewToolCard("c", "C", nil, nil))
	am.Add(tool.NewToolCard("a", "A", nil, nil))
	am.Add(tool.NewToolCard("b", "B", nil, nil))
	am.ReorderTools([]string{"a", "b", "c"})
	// 验证三个工具都还存在
	if am.Get("a") == nil || am.Get("b") == nil || am.Get("c") == nil {
		t.Error("ReorderTools 后所有工具应仍存在")
	}
	// 验证总数不变
	list := am.List()
	toolCount := 0
	for _, a := range list {
		if a.AbilityKind() == schema.AbilityKindTool {
			toolCount++
		}
	}
	if toolCount != 3 {
		t.Errorf("工具数量 = %d, want 3", toolCount)
	}
}

func TestPrioritizePaidSearch_paid在free前面(t *testing.T) {
	items := []toolItem{
		{name: "paid_search", card: tool.NewToolCard("paid_search", "付费搜索", nil, nil)},
		{name: "free_search", card: tool.NewToolCard("free_search", "免费搜索", nil, nil)},
	}
	result := prioritizePaidSearch(items)
	if result[0].name != "paid_search" {
		t.Errorf("paid_search 应在第一个，实际 %s", result[0].name)
	}
}

func TestPrioritizePaidSearch_paid在free后面(t *testing.T) {
	items := []toolItem{
		{name: "free_search", card: tool.NewToolCard("free_search", "免费搜索", nil, nil)},
		{name: "other_tool", card: tool.NewToolCard("other", "其他", nil, nil)},
		{name: "paid_search", card: tool.NewToolCard("paid_search", "付费搜索", nil, nil)},
	}
	result := prioritizePaidSearch(items)
	// paid 应在 free 前面
	paidIdx, freeIdx := -1, -1
	for i, item := range result {
		if item.name == "paid_search" {
			paidIdx = i
		}
		if item.name == "free_search" {
			freeIdx = i
		}
	}
	if paidIdx >= freeIdx {
		t.Errorf("paid_search(idx=%d) 应在 free_search(idx=%d) 前面", paidIdx, freeIdx)
	}
}

func TestPrioritizePaidSearch_只有paid(t *testing.T) {
	items := []toolItem{
		{name: "paid_search", card: tool.NewToolCard("paid_search", "付费搜索", nil, nil)},
		{name: "other", card: tool.NewToolCard("other", "其他", nil, nil)},
	}
	result := prioritizePaidSearch(items)
	if len(result) != 2 {
		t.Errorf("长度应为 2，实际 %d", len(result))
	}
}

func TestAbilityManager_ListToolInfo(t *testing.T) {
	am := NewAbilityManager(nil)
	am.Add(tool.NewToolCard("tool1", "工具1", nil, nil))
	am.Add(schema.NewWorkflowCard(schema.WithName("wf1"), schema.WithDescription("工作流1")))
	am.Add(agentschema.NewAgentCard(schema.WithName("ag1"), schema.WithDescription("Agent1")))

	infos, err := am.ListToolInfo(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListToolInfo 错误: %v", err)
	}
	names := make(map[string]bool)
	for _, info := range infos {
		names[info.Name] = true
	}
	if !names["tool1"] || !names["wf1"] || !names["ag1"] {
		t.Errorf("缺少工具，实际 %v", names)
	}
}

func TestAbilityManager_ListToolInfo_按名称过滤(t *testing.T) {
	am := NewAbilityManager(nil)
	am.Add(tool.NewToolCard("tool1", "工具1", nil, nil))
	am.Add(tool.NewToolCard("tool2", "工具2", nil, nil))

	infos, err := am.ListToolInfo(context.Background(), []string{"tool1"})
	if err != nil {
		t.Fatalf("ListToolInfo 错误: %v", err)
	}
	if len(infos) != 1 || infos[0].Name != "tool1" {
		t.Errorf("应只返回 tool1，实际 %v", infos)
	}
}

func TestAbilityManager_Execute_单工具成功(t *testing.T) {
	// 注册工具 Card 和实例
	toolCard := tool.NewToolCard("search", "搜索", nil, nil)
	ft := &fakeTool{
		card:   toolCard,
		result: map[string]any{"result": "搜索结果"},
	}
	frm := newTestResourceMgr(map[string]tool.Tool{toolCard.ID: ft}, nil, nil)
	am := NewAbilityManager(frm)
	am.Add(toolCard)

	results := am.Execute(context.Background(), nil, []*llmschema.ToolCall{
		llmschema.NewToolCall("call_1", "search", `{"query": "test"}`),
	}, nil, "")

	if len(results) != 1 {
		t.Fatalf("结果数量 = %d, want 1", len(results))
	}
	if isAbilityExecutionErrorInResult(results[0].Result) {
		t.Fatalf("不应有错误: %v", results[0].Result)
	}
	if results[0].ToolMsg == nil {
		t.Fatal("ToolMsg 不应为 nil")
	}
	if results[0].ToolMsg.ToolCallID != "call_1" {
		t.Errorf("ToolCallID = %q, want call_1", results[0].ToolMsg.ToolCallID)
	}
}

func TestAbilityManager_Execute_并行多工具(t *testing.T) {
	card1 := tool.NewToolCard("tool_a", "工具A", nil, nil)
	card2 := tool.NewToolCard("tool_b", "工具B", nil, nil)
	ft1 := &fakeTool{card: card1, result: map[string]any{"result": "A"}}
	ft2 := &fakeTool{card: card2, result: map[string]any{"result": "B"}}
	frm := newTestResourceMgr(map[string]tool.Tool{card1.ID: ft1, card2.ID: ft2}, nil, nil)
	am := NewAbilityManager(frm)
	am.Add(card1)
	am.Add(card2)

	results := am.Execute(context.Background(), nil, []*llmschema.ToolCall{
		llmschema.NewToolCall("call_1", "tool_a", `{}`),
		llmschema.NewToolCall("call_2", "tool_b", `{}`),
	}, nil, "")

	if len(results) != 2 {
		t.Fatalf("结果数量 = %d, want 2", len(results))
	}
	// 两个都应成功
	errCount := 0
	for _, r := range results {
		if isAbilityExecutionErrorInResult(r.Result) {
			errCount++
		}
	}
	if errCount > 0 {
		t.Errorf("不应有错误，实际 %d 个", errCount)
	}
}

func TestAbilityManager_Execute_参数解析失败(t *testing.T) {
	frm := newTestResourceMgr(nil, nil, nil)
	am := NewAbilityManager(frm)

	toolCard := tool.NewToolCard("search", "搜索", nil, nil)
	am.Add(toolCard)

	results := am.Execute(context.Background(), nil, []*llmschema.ToolCall{
		llmschema.NewToolCall("call_1", "search", `not json at all`),
	}, nil, "")

	if len(results) != 1 {
		t.Fatalf("结果数量 = %d, want 1", len(results))
	}
	if !isAbilityExecutionErrorInResult(results[0].Result) {
		t.Fatal("应有错误")
	}
	// 错误应为 AbilityExecutionError
	if !isAbilityExecutionErrorInResult(results[0].Result) {
		t.Errorf("错误类型应为 *AbilityExecutionError，实际 %T", results[0].Result)
	}
}

func TestAbilityManager_Execute_能力未找到(t *testing.T) {
	frm := newTestResourceMgr(nil, nil, nil)
	am := NewAbilityManager(frm)

	results := am.Execute(context.Background(), nil, []*llmschema.ToolCall{
		llmschema.NewToolCall("call_1", "nonexistent", `{}`),
	}, nil, "")

	if len(results) != 1 {
		t.Fatalf("结果数量 = %d, want 1", len(results))
	}
	if !isAbilityExecutionErrorInResult(results[0].Result) {
		t.Fatal("应有错误")
	}
}

// fakeWorkflow 用于测试的模拟工作流
type fakeWorkflow struct {
	result any
	err    error
	card   *schema.WorkflowCard
}

func (f *fakeWorkflow) Invoke(_ context.Context, _ map[string]any, _ ...interfaces.WorkflowOption) (any, error) {
	return f.result, f.err
}

func (f *fakeWorkflow) Stream(_ context.Context, _ map[string]any, _ ...interfaces.WorkflowOption) (<-chan stream.Schema, error) {
	ch := make(chan stream.Schema)
	close(ch)
	return ch, nil
}

func (f *fakeWorkflow) Card() *schema.WorkflowCard {
	return f.card
}

// fakeAgent 用于测试的模拟 Agent
type fakeAgent struct {
	result any
	err    error
}

func (f *fakeAgent) Invoke(_ context.Context, _ map[string]any, _ ...interfaces.AgentOption) (any, error) {
	return f.result, f.err
}

func (f *fakeAgent) Configure(_ context.Context, _ interfaces.AgentConfig) error { return nil }

func (f *fakeAgent) Stream(_ context.Context, _ map[string]any, _ ...interfaces.AgentOption) (<-chan stream.Schema, error) {
	ch := make(chan stream.Schema)
	close(ch)
	return ch, nil
}

func (f *fakeAgent) Card() *agentschema.AgentCard { return nil }

func (f *fakeAgent) Config() interfaces.AgentConfig { return nil }

func (f *fakeAgent) AbilityManager() any { return nil }

func (f *fakeAgent) CallbackManager() *rail.AgentCallbackManager { return nil }

func (f *fakeAgent) AgentID() string { return "" }

func (f *fakeAgent) RegisterCallback(_ context.Context, _ any, _ any, _ ...callback.CallbackOption) error {
	return nil
}

func (f *fakeAgent) RegisterRail(_ context.Context, _ rail.AgentRail, _ ...callback.CallbackOption) error {
	return nil
}

func (f *fakeAgent) UnregisterRail(_ context.Context, _ rail.AgentRail) error { return nil }

func TestAbilityManager_SetContextEngine(t *testing.T) {
	am := NewAbilityManager(nil)
	am.SetContextEngine(nil) // 不应 panic，nil 满足 iface.ContextEngine 接口
}

func TestAbilityManager_RemoveMany(t *testing.T) {
	am := NewAbilityManager(nil)
	am.Add(tool.NewToolCard("t1", "工具1", nil, nil))
	am.Add(tool.NewToolCard("t2", "工具2", nil, nil))
	removed := am.RemoveMany([]string{"t1", "t2", "nonexistent"})
	if len(removed) != 3 {
		t.Fatalf("len = %d, want 3", len(removed))
	}
	if removed[0] == nil || removed[0].AbilityName() != "t1" {
		t.Errorf("第一个应移除 t1")
	}
	if removed[1] == nil || removed[1].AbilityName() != "t2" {
		t.Errorf("第二个应移除 t2")
	}
	if removed[2] != nil {
		t.Errorf("不存在的应返回 nil")
	}
}

func TestAbilityManager_Remove_不存在(t *testing.T) {
	am := NewAbilityManager(nil)
	removed := am.Remove("nonexistent")
	if removed != nil {
		t.Errorf("不存在的应返回 nil")
	}
}

func TestAbilityManager_Remove_Workflow(t *testing.T) {
	am := NewAbilityManager(nil)
	wf := schema.NewWorkflowCard(schema.WithName("wf1"), schema.WithDescription("工作流"))
	am.Add(wf)
	removed := am.Remove("wf1")
	if removed == nil {
		t.Fatal("应返回被移除的 Workflow")
	}
	if removed.AbilityKind() != schema.AbilityKindWorkflow {
		t.Errorf("AbilityKind 应为 Workflow")
	}
}

func TestAbilityManager_Remove_Agent(t *testing.T) {
	am := NewAbilityManager(nil)
	ag := agentschema.NewAgentCard(schema.WithName("ag1"), schema.WithDescription("Agent"))
	am.Add(ag)
	removed := am.Remove("ag1")
	if removed == nil {
		t.Fatal("应返回被移除的 Agent")
	}
	if removed.AbilityKind() != schema.AbilityKindAgent {
		t.Errorf("AbilityKind 应为 Agent")
	}
}

func TestAbilityManager_Add_重复Workflow(t *testing.T) {
	am := NewAbilityManager(nil)
	wf1 := schema.NewWorkflowCard(schema.WithName("wf"), schema.WithDescription("工作流1"))
	wf2 := schema.NewWorkflowCard(schema.WithName("wf"), schema.WithDescription("工作流2"))
	am.Add(wf1)
	result := am.Add(wf2)
	if result.Added {
		t.Error("重复 name 不应添加")
	}
	if result.Reason != "duplicate_workflow" {
		t.Errorf("Reason = %q, want duplicate_workflow", result.Reason)
	}
}

func TestAbilityManager_Add_重复Agent(t *testing.T) {
	am := NewAbilityManager(nil)
	ag1 := agentschema.NewAgentCard(schema.WithName("ag"), schema.WithDescription("Agent1"))
	ag2 := agentschema.NewAgentCard(schema.WithName("ag"), schema.WithDescription("Agent2"))
	am.Add(ag1)
	result := am.Add(ag2)
	if result.Added {
		t.Error("重复 name 不应添加")
	}
	if result.Reason != "duplicate_agent" {
		t.Errorf("Reason = %q, want duplicate_agent", result.Reason)
	}
}

func TestAbilityManager_Add_重复McpServer(t *testing.T) {
	am := NewAbilityManager(nil)
	mc1 := mcp.NewMcpServerConfig("srv", "http://a", "sse")
	mc2 := mcp.NewMcpServerConfig("srv", "http://b", "sse")
	am.Add(mc1)
	result := am.Add(mc2)
	if result.Added {
		t.Error("重复 name 不应添加")
	}
	if result.Reason != "duplicate_mcp_server" {
		t.Errorf("Reason = %q, want duplicate_mcp_server", result.Reason)
	}
}

func TestAbilityManager_Add_未知类型(t *testing.T) {
	am := NewAbilityManager(nil)
	result := am.Add(nil)
	if result.Added {
		t.Error("nil 不应添加")
	}
	if result.Reason != "unknown_ability_type" {
		t.Errorf("Reason = %q, want unknown_ability_type", result.Reason)
	}
}

func TestAbilityManager_ListToolInfo_MCP工具排除(t *testing.T) {
	am := NewAbilityManager(nil)
	mc := mcp.NewMcpServerConfig("my_server", "http://localhost:8080/sse", "sse")
	am.Add(mc)
	// 添加属于该 MCP 服务器的工具
	mcpTool := tool.NewToolCard("mcp_search", "MCP搜索", nil, nil)
	mcpTool.ID = mc.ServerID + ".search"
	am.Add(mcpTool)
	// 添加不属于 MCP 的工具
	normalTool := tool.NewToolCard("normal", "普通工具", nil, nil)
	am.Add(normalTool)

	infos, err := am.ListToolInfo(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListToolInfo 错误: %v", err)
	}
	for _, info := range infos {
		if info.Name == "mcp_search" {
			t.Error("MCP 服务器下的工具应被排除")
		}
	}
	// normal 工具应存在
	found := false
	for _, info := range infos {
		if info.Name == "normal" {
			found = true
		}
	}
	if !found {
		t.Error("普通工具应存在")
	}
}

func TestAbilityManager_Execute_工具实例未找到(t *testing.T) {
	frm := newTestResourceMgr(nil, nil, nil)
	am := NewAbilityManager(frm)

	toolCard := tool.NewToolCard("search", "搜索", nil, nil)
	am.Add(toolCard)
	// 不注册工具实例 → GetTool 返回 NotFound

	results := am.Execute(context.Background(), nil, []*llmschema.ToolCall{
		llmschema.NewToolCall("call_1", "search", `{}`),
	}, nil, "")

	if len(results) != 1 {
		t.Fatalf("结果数量 = %d, want 1", len(results))
	}
	if !isAbilityExecutionErrorInResult(results[0].Result) {
		t.Fatal("应有错误")
	}
}

func TestAbilityManager_Execute_工具执行错误(t *testing.T) {
	toolCard := tool.NewToolCard("fail_tool", "失败工具", nil, nil)
	ft := &fakeTool{
		card: toolCard,
		err:  exception.BuildError(exception.StatusAbilityExecutionError, exception.WithMsg("执行失败")),
	}
	frm := newTestResourceMgr(map[string]tool.Tool{toolCard.ID: ft}, nil, nil)
	am := NewAbilityManager(frm)
	am.Add(toolCard)

	results := am.Execute(context.Background(), nil, []*llmschema.ToolCall{
		llmschema.NewToolCall("call_1", "fail_tool", `{}`),
	}, nil, "")

	if len(results) != 1 {
		t.Fatalf("结果数量 = %d, want 1", len(results))
	}
	if !isAbilityExecutionErrorInResult(results[0].Result) {
		t.Fatal("应有错误")
	}
}

func TestAbilityManager_Execute_Workflow成功(t *testing.T) {
	wf := schema.NewWorkflowCard(schema.WithName("my_wf"), schema.WithDescription("工作流"))
	fw := &fakeWorkflow{result: map[string]any{"output": "done"}, card: wf}
	frm := newTestResourceMgr(nil, map[string]interfaces.Workflow{wf.ID: fw}, nil)
	am := NewAbilityManager(frm)
	am.Add(wf)

	results := am.Execute(context.Background(), nil, []*llmschema.ToolCall{
		llmschema.NewToolCall("call_wf", "my_wf", `{}`),
	}, nil, "")

	if len(results) != 1 {
		t.Fatalf("结果数量 = %d, want 1", len(results))
	}
	if isAbilityExecutionErrorInResult(results[0].Result) {
		t.Fatalf("不应有错误: %v", results[0].Result)
	}
}

func TestAbilityManager_Execute_Workflow未找到(t *testing.T) {
	frm := newTestResourceMgr(nil, nil, nil)
	am := NewAbilityManager(frm)

	wf := schema.NewWorkflowCard(schema.WithName("my_wf"), schema.WithDescription("工作流"))
	am.Add(wf)
	// 不注册 workflow 实例

	results := am.Execute(context.Background(), nil, []*llmschema.ToolCall{
		llmschema.NewToolCall("call_wf", "my_wf", `{}`),
	}, nil, "")

	if len(results) != 1 {
		t.Fatalf("结果数量 = %d, want 1", len(results))
	}
	if !isAbilityExecutionErrorInResult(results[0].Result) {
		t.Fatal("应有错误")
	}
}

func TestAbilityManager_Execute_Workflow执行错误(t *testing.T) {
	wf := schema.NewWorkflowCard(schema.WithName("my_wf"), schema.WithDescription("工作流"))
	fw := &fakeWorkflow{err: exception.BuildError(exception.StatusAbilityExecutionError, exception.WithMsg("wf错误")), card: wf}
	frm := newTestResourceMgr(nil, map[string]interfaces.Workflow{wf.ID: fw}, nil)
	am := NewAbilityManager(frm)
	am.Add(wf)

	results := am.Execute(context.Background(), nil, []*llmschema.ToolCall{
		llmschema.NewToolCall("call_wf", "my_wf", `{}`),
	}, nil, "")

	if len(results) != 1 {
		t.Fatalf("结果数量 = %d, want 1", len(results))
	}
	if !isAbilityExecutionErrorInResult(results[0].Result) {
		t.Fatal("应有错误")
	}
}

func TestAbilityManager_Execute_Agent成功(t *testing.T) {
	ag := agentschema.NewAgentCard(schema.WithName("my_agent"), schema.WithDescription("Agent"))
	fa := &fakeAgent{result: map[string]any{"response": "ok"}}
	frm := newTestResourceMgr(nil, nil, map[string]interfaces.BaseAgent{ag.ID: fa})
	am := NewAbilityManager(frm)
	am.Add(ag)

	results := am.Execute(context.Background(), nil, []*llmschema.ToolCall{
		llmschema.NewToolCall("call_ag", "my_agent", `{}`),
	}, nil, "")

	if len(results) != 1 {
		t.Fatalf("结果数量 = %d, want 1", len(results))
	}
	if isAbilityExecutionErrorInResult(results[0].Result) {
		t.Fatalf("不应有错误: %v", results[0].Result)
	}
}

func TestAbilityManager_Execute_Agent未找到(t *testing.T) {
	frm := newTestResourceMgr(nil, nil, nil)
	am := NewAbilityManager(frm)

	ag := agentschema.NewAgentCard(schema.WithName("my_agent"), schema.WithDescription("Agent"))
	am.Add(ag)
	// 不注册 agent 实例

	results := am.Execute(context.Background(), nil, []*llmschema.ToolCall{
		llmschema.NewToolCall("call_ag", "my_agent", `{}`),
	}, nil, "")

	if len(results) != 1 {
		t.Fatalf("结果数量 = %d, want 1", len(results))
	}
	if !isAbilityExecutionErrorInResult(results[0].Result) {
		t.Fatal("应有错误")
	}
}

func TestAbilityManager_Execute_Agent执行错误(t *testing.T) {
	ag := agentschema.NewAgentCard(schema.WithName("my_agent"), schema.WithDescription("Agent"))
	fa := &fakeAgent{err: exception.BuildError(exception.StatusAbilityExecutionError, exception.WithMsg("agent错误"))}
	frm := newTestResourceMgr(nil, nil, map[string]interfaces.BaseAgent{ag.ID: fa})
	am := NewAbilityManager(frm)
	am.Add(ag)

	results := am.Execute(context.Background(), nil, []*llmschema.ToolCall{
		llmschema.NewToolCall("call_ag", "my_agent", `{}`),
	}, nil, "")

	if len(results) != 1 {
		t.Fatalf("结果数量 = %d, want 1", len(results))
	}
	if !isAbilityExecutionErrorInResult(results[0].Result) {
		t.Fatal("应有错误")
	}
}

func TestAbilityManager_Execute_McpServer暂未实现(t *testing.T) {
	am := NewAbilityManager(nil)

	mc := mcp.NewMcpServerConfig("my_mcp", "http://localhost:8080/sse", "sse")
	am.Add(mc)

	results := am.Execute(context.Background(), nil, []*llmschema.ToolCall{
		llmschema.NewToolCall("call_mcp", "my_mcp", `{}`),
	}, nil, "")

	if len(results) != 1 {
		t.Fatalf("结果数量 = %d, want 1", len(results))
	}
	if !isAbilityExecutionErrorInResult(results[0].Result) {
		t.Fatal("MCP 执行应返回错误")
	}
}

func TestAbilityManager_Execute_空ToolCall(t *testing.T) {
	am := NewAbilityManager(nil)
	results := am.Execute(context.Background(), nil, nil, nil, "")
	if results != nil {
		t.Errorf("空 ToolCall 应返回 nil")
	}
}

func TestAbilityManager_Execute_兜底Tool成功(t *testing.T) {
	// 不注册 Card，但注册 Tool 实例（用 tool name 作 ID，使 GetTool 能按 name 找到）
	fallbackCard := tool.NewToolCard("fallback_tool", "兜底工具", nil, nil)
	fallbackCard.ID = "fallback_tool"
	ft := &fakeTool{
		card:   fallbackCard,
		result: map[string]any{"result": "fallback"},
	}
	frm := newTestResourceMgr(map[string]tool.Tool{"fallback_tool": ft}, nil, nil)
	am := NewAbilityManager(frm)

	results := am.Execute(context.Background(), nil, []*llmschema.ToolCall{
		llmschema.NewToolCall("call_fb", "fallback_tool", `{}`),
	}, nil, "")

	if len(results) != 1 {
		t.Fatalf("结果数量 = %d, want 1", len(results))
	}
	if isAbilityExecutionErrorInResult(results[0].Result) {
		t.Fatalf("不应有错误: %v", results[0].Result)
	}
}

func TestAbilityManager_Execute_兜底Tool执行错误(t *testing.T) {
	fallbackCard := tool.NewToolCard("fail_fb", "失败兜底", nil, nil)
	fallbackCard.ID = "fail_fb"
	ft := &fakeTool{
		card: fallbackCard,
		err:  exception.BuildError(exception.StatusAbilityExecutionError, exception.WithMsg("兜底失败")),
	}
	frm := newTestResourceMgr(map[string]tool.Tool{"fail_fb": ft}, nil, nil)
	am := NewAbilityManager(frm)

	results := am.Execute(context.Background(), nil, []*llmschema.ToolCall{
		llmschema.NewToolCall("call_fb", "fail_fb", `{}`),
	}, nil, "")

	if len(results) != 1 {
		t.Fatalf("结果数量 = %d, want 1", len(results))
	}
	if !isAbilityExecutionErrorInResult(results[0].Result) {
		t.Fatal("应有错误")
	}
}

func TestAbilityManager_Execute_带Tag(t *testing.T) {
	toolCard := tool.NewToolCard("tagged_tool", "带标签工具", nil, nil)
	ft := &fakeTool{
		card:   toolCard,
		result: map[string]any{"result": "ok"},
	}
	mgr := resourcesmanager.NewResourceMgr()
	_ = mgr.AddTool(ft, resourcesmanager.WithTag(resourcesmanager.Tag("my_tag")))
	am := NewAbilityManager(mgr)
	am.Add(toolCard)

	results := am.Execute(context.Background(), nil, []*llmschema.ToolCall{
		llmschema.NewToolCall("call_tag", "tagged_tool", `{}`),
	}, nil, "my_tag")

	if len(results) != 1 {
		t.Fatalf("结果数量 = %d, want 1", len(results))
	}
	if isAbilityExecutionErrorInResult(results[0].Result) {
		t.Fatalf("不应有错误: %v", results[0].Result)
	}
}

func TestAbilityManager_ReorderTools_空参数(t *testing.T) {
	am := NewAbilityManager(nil)
	am.Add(tool.NewToolCard("a", "A", nil, nil))
	am.ReorderTools(nil)        // 不应 panic
	am.ReorderTools([]string{}) // 不应 panic
}

func TestAbilityManager_ReorderTools_无匹配(t *testing.T) {
	am := NewAbilityManager(nil)
	am.Add(tool.NewToolCard("a", "A", nil, nil))
	am.ReorderTools([]string{"nonexistent"}) // 无匹配，不应 panic
}

func TestPrioritizePaidSearch_空列表(t *testing.T) {
	result := prioritizePaidSearch(nil)
	if result != nil {
		t.Errorf("空列表应返回 nil")
	}
}

func TestPrioritizePaidSearch_无paid无free(t *testing.T) {
	items := []toolItem{
		{name: "other", card: tool.NewToolCard("other", "其他", nil, nil)},
	}
	result := prioritizePaidSearch(items)
	if len(result) != 1 || result[0].name != "other" {
		t.Errorf("无 paid/free 时应原样返回")
	}
}

// ──────────────────────────── Rail 集成测试 ────────────────────────────

// fakeRailAgentForAbility 实现 rail.RailAgent 接口，用于 ability 测试
type fakeRailAgentForAbility struct {
	cbMgr   *rail.AgentCallbackManager
	agentID string
}

func (f *fakeRailAgentForAbility) CallbackManager() *rail.AgentCallbackManager { return f.cbMgr }
func (f *fakeRailAgentForAbility) AgentID() string                             { return f.agentID }

// TestAbilityManager_Execute_forceFinish传播 验证子 toolCtx 的 force-finish 信号传播到父 cbc
func TestAbilityManager_Execute_forceFinish传播(t *testing.T) {
	mgr := rail.NewAgentCallbackManager("test_ff_prop")
	defer mgr.Clear()

	// 注册 after_tool_call 钩子，在第一个工具调用后设置 force-finish
	callCount := 0
	mgr.RegisterCallback(context.Background(), rail.CallbackAfterToolCall, func(_ context.Context, railCtx any) error {
		cbc := railCtx.(*rail.AgentCallbackContext)
		callCount++
		if callCount == 1 {
			cbc.RequestForceFinish(map[string]any{"reason": "budget_exceeded"})
		}
		return nil
	})

	agent := &fakeRailAgentForAbility{cbMgr: mgr}
	cbc := rail.NewAgentCallbackContext(agent, &rail.InvokeInputs{}, nil)

	am := NewAbilityManager(nil)
	am.Add(tool.NewToolCard("echo", "回显工具", nil, nil))

	toolCalls := []*llmschema.ToolCall{
		{Name: "echo", Arguments: `{}`, ID: "tc1"},
	}

	results := am.Execute(context.Background(), cbc, toolCalls, nil, "")
	_ = results

	// 父 cbc 应收到 force-finish 信号
	assert.True(t, cbc.HasForceFinishRequest())
	finish := cbc.ConsumeForceFinish()
	assert.NotNil(t, finish)
	assert.Equal(t, "budget_exceeded", finish.Result["reason"])
}

// TestAbilityManager_Execute_Rail包装 验证 ToolCallRail 自动触发 before/after 钩子
func TestAbilityManager_Execute_Rail包装(t *testing.T) {
	mgr := rail.NewAgentCallbackManager("test_rail_wrap")
	defer mgr.Clear()

	var firedEvents []rail.AgentCallbackEvent
	registerHook := func(event rail.AgentCallbackEvent) {
		mgr.RegisterCallback(context.Background(), event, func(_ context.Context, railCtx any) error {
			cbc := railCtx.(*rail.AgentCallbackContext)
			firedEvents = append(firedEvents, cbc.Event())
			return nil
		})
	}
	registerHook(rail.CallbackBeforeToolCall)
	registerHook(rail.CallbackAfterToolCall)

	agent := &fakeRailAgentForAbility{cbMgr: mgr}
	cbc := rail.NewAgentCallbackContext(agent, &rail.InvokeInputs{}, nil)

	am := NewAbilityManager(nil)
	am.Add(tool.NewToolCard("echo", "回显工具", nil, nil))

	toolCalls := []*llmschema.ToolCall{
		{Name: "echo", Arguments: `{}`, ID: "tc1"},
	}

	_ = am.Execute(context.Background(), cbc, toolCalls, nil, "")

	assert.Contains(t, firedEvents, rail.CallbackBeforeToolCall)
	assert.Contains(t, firedEvents, rail.CallbackAfterToolCall)
}

// TestAbilityManager_Execute_skipTool before hook 通过 _skip_tool 跳过工具执行
// 对齐 Python L664-667: skip_result = ctx.extra.pop("_skip_tool", None)
func TestAbilityManager_Execute_skipTool(t *testing.T) {
	mgr := rail.NewAgentCallbackManager("test_skip_tool")
	defer mgr.Clear()

	// before_tool_call 钩子：设置 _skip_tool 并预设拒绝结果
	mgr.RegisterCallback(context.Background(), rail.CallbackBeforeToolCall, func(_ context.Context, railCtx any) error {
		cbc := railCtx.(*rail.AgentCallbackContext)
		inputs, ok := cbc.Inputs().(*rail.ToolCallInputs)
		if !ok {
			return nil
		}
		// 模拟安全护栏拒绝工具执行
		cbc.Extra()["_skip_tool"] = true
		inputs.ToolResult = map[string]any{"error": "工具被安全护栏拒绝"}
		inputs.ToolMsg = llmschema.NewToolMessage("tc1", "工具被安全护栏拒绝")
		return nil
	})

	// after_tool_call 钩子：验证 after 仍然触发（finally 语义）
	afterFired := false
	mgr.RegisterCallback(context.Background(), rail.CallbackAfterToolCall, func(_ context.Context, railCtx any) error {
		afterFired = true
		// after 不应再看到 _skip_tool（pop 一次性消费）
		_, exists := railCtx.(*rail.AgentCallbackContext).Extra()["_skip_tool"]
		assert.False(t, exists, "after 钩子不应看到 _skip_tool（应已被 pop 消费）")
		return nil
	})

	agent := &fakeRailAgentForAbility{cbMgr: mgr}
	cbc := rail.NewAgentCallbackContext(agent, &rail.InvokeInputs{}, nil)

	am := NewAbilityManager(nil)
	am.Add(tool.NewToolCard("echo", "回显工具", nil, nil))

	toolCalls := []*llmschema.ToolCall{
		{Name: "echo", Arguments: `{}`, ID: "tc1"},
	}

	results := am.Execute(context.Background(), cbc, toolCalls, nil, "")

	// 验证结果：应为 before hook 预设的拒绝结果
	require.Len(t, results, 1)
	resultMap, ok := results[0].Result.(map[string]any)
	require.True(t, ok, "Result 应为 map[string]any")
	assert.Equal(t, "工具被安全护栏拒绝", resultMap["error"])
	assert.NotNil(t, results[0].ToolMsg, "ToolMsg 应为 before hook 预设的消息")
	assert.Equal(t, "工具被安全护栏拒绝", results[0].ToolMsg.Content.Text())

	// after 钩子仍应触发（finally 语义）
	assert.True(t, afterFired, "after 钩子应触发（finally 语义）")
}

// TestAbilityManager_Execute_skipTool非bool值 忽略非 bool 的 _skip_tool 值
func TestAbilityManager_Execute_skipTool非bool值(t *testing.T) {
	mgr := rail.NewAgentCallbackManager("test_skip_tool_nonbool")
	defer mgr.Clear()

	// before_tool_call 钩子：设置 _skip_tool 为字符串（非 bool），应被忽略
	mgr.RegisterCallback(context.Background(), rail.CallbackBeforeToolCall, func(_ context.Context, railCtx any) error {
		cbc := railCtx.(*rail.AgentCallbackContext)
		cbc.Extra()["_skip_tool"] = "yes" // 非 bool，应被忽略
		return nil
	})

	agent := &fakeRailAgentForAbility{cbMgr: mgr}
	cbc := rail.NewAgentCallbackContext(agent, &rail.InvokeInputs{}, nil)

	am := NewAbilityManager(nil)
	am.Add(tool.NewToolCard("echo", "回显工具", nil, nil))

	toolCalls := []*llmschema.ToolCall{
		{Name: "echo", Arguments: `{}`, ID: "tc1"},
	}

	results := am.Execute(context.Background(), cbc, toolCalls, nil, "")

	// _skip_tool 非bool值被忽略，工具应正常执行
	require.Len(t, results, 1)
	// echo 工具不存在实例，会返回 AbilityExecutionError，但不是 skip 结果
	assert.True(t, isAbilityExecutionErrorInResult(results[0].Result), "工具应正常执行（非skip路径），因无实例返回错误")
}
