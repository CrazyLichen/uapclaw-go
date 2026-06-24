package ability

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/mcp"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/rail"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/resource"
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

// fakeResourceManager 用于测试的模拟资源管理器
type fakeResourceManager struct {
	tools     map[string]tool.Tool
	workflows map[string]interfaces.Workflow
	agents    map[string]interfaces.BaseAgent
}

func newFakeResourceManager() *fakeResourceManager {
	return &fakeResourceManager{
		tools:     make(map[string]tool.Tool),
		workflows: make(map[string]interfaces.Workflow),
		agents:    make(map[string]interfaces.BaseAgent),
	}
}

func (f *fakeResourceManager) GetTool(toolID string, _ ...resource.ResourceOption) (tool.Tool, error) {
	t, ok := f.tools[toolID]
	if !ok {
		return nil, exception.BuildError(exception.StatusAbilityNotFound, exception.WithParam("ability_name", toolID))
	}
	return t, nil
}

func (f *fakeResourceManager) GetWorkflow(workflowID string, _ ...resource.ResourceOption) (any, error) {
	w, ok := f.workflows[workflowID]
	if !ok {
		return nil, exception.BuildError(exception.StatusAbilityNotFound, exception.WithParam("ability_name", workflowID))
	}
	return w, nil
}

func (f *fakeResourceManager) GetAgent(agentID string, _ ...resource.ResourceOption) (any, error) {
	a, ok := f.agents[agentID]
	if !ok {
		return nil, exception.BuildError(exception.StatusAbilityNotFound, exception.WithParam("ability_name", agentID))
	}
	return a, nil
}

func (f *fakeResourceManager) GetMcpToolInfos(_ string) ([]*schema.ToolInfo, error) {
	return nil, nil
}

// ──────────────────────────── 导出函数 ────────────────────────────

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
	frm := newFakeResourceManager()
	am := NewAbilityManager(frm)

	// 注册工具 Card
	toolCard := tool.NewToolCard("search", "搜索", nil, nil)
	am.Add(toolCard)

	// 注册工具实例
	frm.tools[toolCard.ID] = &fakeTool{
		card:   toolCard,
		result: map[string]any{"result": "搜索结果"},
	}

	results := am.Execute(context.Background(), []*llmschema.ToolCall{
		llmschema.NewToolCall("call_1", "search", `{"query": "test"}`),
	}, nil, "")

	if len(results) != 1 {
		t.Fatalf("结果数量 = %d, want 1", len(results))
	}
	if results[0].Err != nil {
		t.Fatalf("不应有错误: %v", results[0].Err)
	}
	if results[0].ToolMsg == nil {
		t.Fatal("ToolMsg 不应为 nil")
	}
	if results[0].ToolMsg.ToolCallID != "call_1" {
		t.Errorf("ToolCallID = %q, want call_1", results[0].ToolMsg.ToolCallID)
	}
}

func TestAbilityManager_Execute_并行多工具(t *testing.T) {
	frm := newFakeResourceManager()
	am := NewAbilityManager(frm)

	card1 := tool.NewToolCard("tool_a", "工具A", nil, nil)
	card2 := tool.NewToolCard("tool_b", "工具B", nil, nil)
	am.Add(card1)
	am.Add(card2)

	frm.tools[card1.ID] = &fakeTool{card: card1, result: map[string]any{"result": "A"}}
	frm.tools[card2.ID] = &fakeTool{card: card2, result: map[string]any{"result": "B"}}

	results := am.Execute(context.Background(), []*llmschema.ToolCall{
		llmschema.NewToolCall("call_1", "tool_a", `{}`),
		llmschema.NewToolCall("call_2", "tool_b", `{}`),
	}, nil, "")

	if len(results) != 2 {
		t.Fatalf("结果数量 = %d, want 2", len(results))
	}
	// 两个都应成功
	errCount := 0
	for _, r := range results {
		if r.Err != nil {
			errCount++
		}
	}
	if errCount > 0 {
		t.Errorf("不应有错误，实际 %d 个", errCount)
	}
}

func TestAbilityManager_Execute_参数解析失败(t *testing.T) {
	frm := newFakeResourceManager()
	am := NewAbilityManager(frm)

	toolCard := tool.NewToolCard("search", "搜索", nil, nil)
	am.Add(toolCard)

	results := am.Execute(context.Background(), []*llmschema.ToolCall{
		llmschema.NewToolCall("call_1", "search", `not json at all`),
	}, nil, "")

	if len(results) != 1 {
		t.Fatalf("结果数量 = %d, want 1", len(results))
	}
	if results[0].Err == nil {
		t.Fatal("应有错误")
	}
	// 错误应为 AbilityExecutionError
	var execErr *AbilityExecutionError
	if !isAbilityExecutionError(results[0].Err, &execErr) {
		t.Errorf("错误类型应为 *AbilityExecutionError，实际 %T", results[0].Err)
	}
}

func TestAbilityManager_Execute_能力未找到(t *testing.T) {
	frm := newFakeResourceManager()
	am := NewAbilityManager(frm)

	results := am.Execute(context.Background(), []*llmschema.ToolCall{
		llmschema.NewToolCall("call_1", "nonexistent", `{}`),
	}, nil, "")

	if len(results) != 1 {
		t.Fatalf("结果数量 = %d, want 1", len(results))
	}
	if results[0].Err == nil {
		t.Fatal("应有错误")
	}
}

// isAbilityExecutionError 检查错误是否为 *AbilityExecutionError。
func isAbilityExecutionError(err error, target **AbilityExecutionError) bool {
	switch e := err.(type) {
	case *AbilityExecutionError:
		*target = e
		return true
	default:
		return false
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

func (f *fakeAgent) RegisterCallback(_ context.Context, _ any, _ any, _ ...callback.CallbackOption) error {
	return nil
}

func (f *fakeAgent) RegisterRail(_ context.Context, _ any, _ ...callback.CallbackOption) error { return nil }

func (f *fakeAgent) UnregisterRail(_ context.Context, _ any) error { return nil }

func TestAbilityManager_SetContextEngine(t *testing.T) {
	am := NewAbilityManager(nil)
	am.SetContextEngine(nil) // 不应 panic，nil 满足 iface.ContextEngine 接口
}

func TestAbilityManager_SetRail(t *testing.T) {
	am := NewAbilityManager(nil)
	am.SetRail(nil) // 不应 panic
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
	frm := newFakeResourceManager()
	am := NewAbilityManager(frm)

	toolCard := tool.NewToolCard("search", "搜索", nil, nil)
	am.Add(toolCard)
	// 不注册工具实例 → GetTool 返回 NotFound

	results := am.Execute(context.Background(), []*llmschema.ToolCall{
		llmschema.NewToolCall("call_1", "search", `{}`),
	}, nil, "")

	if len(results) != 1 {
		t.Fatalf("结果数量 = %d, want 1", len(results))
	}
	if results[0].Err == nil {
		t.Fatal("应有错误")
	}
}

func TestAbilityManager_Execute_工具执行错误(t *testing.T) {
	frm := newFakeResourceManager()
	am := NewAbilityManager(frm)

	toolCard := tool.NewToolCard("fail_tool", "失败工具", nil, nil)
	am.Add(toolCard)

	frm.tools[toolCard.ID] = &fakeTool{
		card: toolCard,
		err:  exception.BuildError(exception.StatusAbilityExecutionError, exception.WithMsg("执行失败")),
	}

	results := am.Execute(context.Background(), []*llmschema.ToolCall{
		llmschema.NewToolCall("call_1", "fail_tool", `{}`),
	}, nil, "")

	if len(results) != 1 {
		t.Fatalf("结果数量 = %d, want 1", len(results))
	}
	if results[0].Err == nil {
		t.Fatal("应有错误")
	}
}

func TestAbilityManager_Execute_Workflow成功(t *testing.T) {
	frm := newFakeResourceManager()
	am := NewAbilityManager(frm)

	wf := schema.NewWorkflowCard(schema.WithName("my_wf"), schema.WithDescription("工作流"))
	am.Add(wf)

	frm.workflows[wf.ID] = &fakeWorkflow{result: map[string]any{"output": "done"}}

	results := am.Execute(context.Background(), []*llmschema.ToolCall{
		llmschema.NewToolCall("call_wf", "my_wf", `{}`),
	}, nil, "")

	if len(results) != 1 {
		t.Fatalf("结果数量 = %d, want 1", len(results))
	}
	if results[0].Err != nil {
		t.Fatalf("不应有错误: %v", results[0].Err)
	}
}

func TestAbilityManager_Execute_Workflow未找到(t *testing.T) {
	frm := newFakeResourceManager()
	am := NewAbilityManager(frm)

	wf := schema.NewWorkflowCard(schema.WithName("my_wf"), schema.WithDescription("工作流"))
	am.Add(wf)
	// 不注册 workflow 实例

	results := am.Execute(context.Background(), []*llmschema.ToolCall{
		llmschema.NewToolCall("call_wf", "my_wf", `{}`),
	}, nil, "")

	if len(results) != 1 {
		t.Fatalf("结果数量 = %d, want 1", len(results))
	}
	if results[0].Err == nil {
		t.Fatal("应有错误")
	}
}

func TestAbilityManager_Execute_Workflow执行错误(t *testing.T) {
	frm := newFakeResourceManager()
	am := NewAbilityManager(frm)

	wf := schema.NewWorkflowCard(schema.WithName("my_wf"), schema.WithDescription("工作流"))
	am.Add(wf)

	frm.workflows[wf.ID] = &fakeWorkflow{err: exception.BuildError(exception.StatusAbilityExecutionError, exception.WithMsg("wf错误"))}

	results := am.Execute(context.Background(), []*llmschema.ToolCall{
		llmschema.NewToolCall("call_wf", "my_wf", `{}`),
	}, nil, "")

	if len(results) != 1 {
		t.Fatalf("结果数量 = %d, want 1", len(results))
	}
	if results[0].Err == nil {
		t.Fatal("应有错误")
	}
}

func TestAbilityManager_Execute_Agent成功(t *testing.T) {
	frm := newFakeResourceManager()
	am := NewAbilityManager(frm)

	ag := agentschema.NewAgentCard(schema.WithName("my_agent"), schema.WithDescription("Agent"))
	am.Add(ag)

	frm.agents[ag.ID] = &fakeAgent{result: map[string]any{"response": "ok"}}

	results := am.Execute(context.Background(), []*llmschema.ToolCall{
		llmschema.NewToolCall("call_ag", "my_agent", `{}`),
	}, nil, "")

	if len(results) != 1 {
		t.Fatalf("结果数量 = %d, want 1", len(results))
	}
	if results[0].Err != nil {
		t.Fatalf("不应有错误: %v", results[0].Err)
	}
}

func TestAbilityManager_Execute_Agent未找到(t *testing.T) {
	frm := newFakeResourceManager()
	am := NewAbilityManager(frm)

	ag := agentschema.NewAgentCard(schema.WithName("my_agent"), schema.WithDescription("Agent"))
	am.Add(ag)
	// 不注册 agent 实例

	results := am.Execute(context.Background(), []*llmschema.ToolCall{
		llmschema.NewToolCall("call_ag", "my_agent", `{}`),
	}, nil, "")

	if len(results) != 1 {
		t.Fatalf("结果数量 = %d, want 1", len(results))
	}
	if results[0].Err == nil {
		t.Fatal("应有错误")
	}
}

func TestAbilityManager_Execute_Agent执行错误(t *testing.T) {
	frm := newFakeResourceManager()
	am := NewAbilityManager(frm)

	ag := agentschema.NewAgentCard(schema.WithName("my_agent"), schema.WithDescription("Agent"))
	am.Add(ag)

	frm.agents[ag.ID] = &fakeAgent{err: exception.BuildError(exception.StatusAbilityExecutionError, exception.WithMsg("agent错误"))}

	results := am.Execute(context.Background(), []*llmschema.ToolCall{
		llmschema.NewToolCall("call_ag", "my_agent", `{}`),
	}, nil, "")

	if len(results) != 1 {
		t.Fatalf("结果数量 = %d, want 1", len(results))
	}
	if results[0].Err == nil {
		t.Fatal("应有错误")
	}
}

func TestAbilityManager_Execute_McpServer暂未实现(t *testing.T) {
	am := NewAbilityManager(nil)

	mc := mcp.NewMcpServerConfig("my_mcp", "http://localhost:8080/sse", "sse")
	am.Add(mc)

	results := am.Execute(context.Background(), []*llmschema.ToolCall{
		llmschema.NewToolCall("call_mcp", "my_mcp", `{}`),
	}, nil, "")

	if len(results) != 1 {
		t.Fatalf("结果数量 = %d, want 1", len(results))
	}
	if results[0].Err == nil {
		t.Fatal("MCP 执行应返回错误")
	}
}

func TestAbilityManager_Execute_空ToolCall(t *testing.T) {
	am := NewAbilityManager(nil)
	results := am.Execute(context.Background(), nil, nil, "")
	if results != nil {
		t.Errorf("空 ToolCall 应返回 nil")
	}
}

func TestAbilityManager_Execute_兜底Tool成功(t *testing.T) {
	frm := newFakeResourceManager()
	am := NewAbilityManager(frm)

	// 不注册 Card，但注册 Tool 实例（用 tool name 作 key）
	fallbackCard := tool.NewToolCard("fallback_tool", "兜底工具", nil, nil)
	frm.tools["fallback_tool"] = &fakeTool{
		card:   fallbackCard,
		result: map[string]any{"result": "fallback"},
	}

	results := am.Execute(context.Background(), []*llmschema.ToolCall{
		llmschema.NewToolCall("call_fb", "fallback_tool", `{}`),
	}, nil, "")

	if len(results) != 1 {
		t.Fatalf("结果数量 = %d, want 1", len(results))
	}
	if results[0].Err != nil {
		t.Fatalf("不应有错误: %v", results[0].Err)
	}
}

func TestAbilityManager_Execute_兜底Tool执行错误(t *testing.T) {
	frm := newFakeResourceManager()
	am := NewAbilityManager(frm)

	fallbackCard := tool.NewToolCard("fail_fb", "失败兜底", nil, nil)
	frm.tools["fail_fb"] = &fakeTool{
		card: fallbackCard,
		err:  exception.BuildError(exception.StatusAbilityExecutionError, exception.WithMsg("兜底失败")),
	}

	results := am.Execute(context.Background(), []*llmschema.ToolCall{
		llmschema.NewToolCall("call_fb", "fail_fb", `{}`),
	}, nil, "")

	if len(results) != 1 {
		t.Fatalf("结果数量 = %d, want 1", len(results))
	}
	if results[0].Err == nil {
		t.Fatal("应有错误")
	}
}

func TestAbilityManager_Execute_带Tag(t *testing.T) {
	frm := newFakeResourceManager()
	am := NewAbilityManager(frm)

	toolCard := tool.NewToolCard("tagged_tool", "带标签工具", nil, nil)
	am.Add(toolCard)

	frm.tools[toolCard.ID] = &fakeTool{
		card:   toolCard,
		result: map[string]any{"result": "ok"},
	}

	results := am.Execute(context.Background(), []*llmschema.ToolCall{
		llmschema.NewToolCall("call_tag", "tagged_tool", `{}`),
	}, nil, "my_tag")

	if len(results) != 1 {
		t.Fatalf("结果数量 = %d, want 1", len(results))
	}
	if results[0].Err != nil {
		t.Fatalf("不应有错误: %v", results[0].Err)
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
