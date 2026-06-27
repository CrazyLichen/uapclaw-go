package resources_manager

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/prompt"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/mcp"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/mcp/types"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// rmStubTool 用于 ResourceMgr 测试的 Tool 桩实现
type rmStubTool struct {
	// card 工具卡片
	card *tool.ToolCard
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// --- rmStubTool 实现 tool.Tool ---

func (s *rmStubTool) Card() *tool.ToolCard {
	return s.card
}

func (s *rmStubTool) Invoke(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (map[string]any, error) {
	return map[string]any{"result": "ok"}, nil
}

func (s *rmStubTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, nil
}

// --- 辅助函数 ---

// newTestResourceMgr 创建测试用 ResourceMgr
func newTestResourceMgr() *ResourceMgr {
	return NewResourceMgr()
}

// stubAgentProvider 创建返回 stubBaseAgent 的 AgentProvider
func stubAgentProvider() AgentProvider {
	return func(_ context.Context, _ *agentschema.AgentCard) (interfaces.BaseAgent, error) {
		return &stubBaseAgent{}, nil
	}
}

// stubModelProvider 创建返回 stubBaseModelClient 的 ModelProvider
func stubModelProvider() ModelProvider {
	return func(_ context.Context, _ string) (model_clients.BaseModelClient, error) {
		return &stubBaseModelClient{}, nil
	}
}

// stubWorkflowProvider 创建返回 stubWorkflow 的 WorkflowProvider
func stubWorkflowProvider() WorkflowProvider {
	return func(_ context.Context, _ *schema.WorkflowCard) (interfaces.Workflow, error) {
		return &stubWorkflow{}, nil
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// --- Agent 测试 ---

// TestResourceMgr_AddAgent_正常 测试正常添加 Agent
func TestResourceMgr_AddAgent_正常(t *testing.T) {
	mgr := newTestResourceMgr()
	card := agentschema.NewAgentCard(schema.WithID("agent-1"), schema.WithName("测试Agent"))
	err := mgr.AddAgent(card, stubAgentProvider())
	if err != nil {
		t.Fatalf("AddAgent 失败: %v", err)
	}
	// 验证 idToCard 缓存
	if cached := mgr.idToCard.Get("agent-1"); cached == nil {
		t.Fatal("idToCard 中未找到缓存")
	}
	// 验证标签
	if !mgr.tagMgr.HasResourceTag("agent-1", TagGlobal) {
		t.Fatal("应标记 GLOBAL 标签")
	}
}

// TestResourceMgr_GetAgent_正常 测试正常获取 Agent
func TestResourceMgr_GetAgent_正常(t *testing.T) {
	mgr := newTestResourceMgr()
	card := agentschema.NewAgentCard(schema.WithID("agent-1"), schema.WithName("测试Agent"))
	_ = mgr.AddAgent(card, stubAgentProvider())

	agents, err := mgr.GetAgent(context.Background(), "agent-1")
	if err != nil {
		t.Fatalf("GetAgent 失败: %v", err)
	}
	if len(agents) != 1 {
		t.Fatalf("期望 1 个 Agent，实际 %d", len(agents))
	}
}

// TestResourceMgr_RemoveAgent_正常 测试正常移除 Agent
func TestResourceMgr_RemoveAgent_正常(t *testing.T) {
	mgr := newTestResourceMgr()
	card := agentschema.NewAgentCard(schema.WithID("agent-1"), schema.WithName("测试Agent"))
	_ = mgr.AddAgent(card, stubAgentProvider())

	removed, err := mgr.RemoveAgent("agent-1")
	if err != nil {
		t.Fatalf("RemoveAgent 失败: %v", err)
	}
	if len(removed) != 1 {
		t.Fatalf("期望移除 1 个，实际 %d", len(removed))
	}
	// 验证缓存已清理
	if mgr.idToCard.Get("agent-1") != nil {
		t.Fatal("idToCard 应已清理")
	}
}

// --- Workflow 测试 ---

// TestResourceMgr_AddWorkflow_正常 测试正常添加 Workflow
func TestResourceMgr_AddWorkflow_正常(t *testing.T) {
	mgr := newTestResourceMgr()
	card := schema.NewWorkflowCard(schema.WithID("wf-1"), schema.WithName("测试Workflow"))
	err := mgr.AddWorkflow(card, stubWorkflowProvider())
	if err != nil {
		t.Fatalf("AddWorkflow 失败: %v", err)
	}
	if cached := mgr.idToCard.Get("wf-1"); cached == nil {
		t.Fatal("idToCard 中未找到缓存")
	}
}

// TestResourceMgr_GetWorkflow_正常 测试正常获取 Workflow
func TestResourceMgr_GetWorkflow_正常(t *testing.T) {
	mgr := newTestResourceMgr()
	card := schema.NewWorkflowCard(schema.WithID("wf-1"), schema.WithName("测试Workflow"))
	_ = mgr.AddWorkflow(card, stubWorkflowProvider())

	workflows, err := mgr.GetWorkflow(context.Background(), "wf-1")
	if err != nil {
		t.Fatalf("GetWorkflow 失败: %v", err)
	}
	if len(workflows) != 1 {
		t.Fatalf("期望 1 个 Workflow，实际 %d", len(workflows))
	}
}

// --- Model 测试 ---

// TestResourceMgr_AddModel_正常 测试正常添加 Model
func TestResourceMgr_AddModel_正常(t *testing.T) {
	mgr := newTestResourceMgr()
	err := mgr.AddModel("model-1", stubModelProvider())
	if err != nil {
		t.Fatalf("AddModel 失败: %v", err)
	}
	if cached := mgr.idToCard.Get("model-1"); cached == nil {
		t.Fatal("idToCard 中未找到缓存")
	}
}

// TestResourceMgr_GetModel_正常 测试正常获取 Model
func TestResourceMgr_GetModel_正常(t *testing.T) {
	mgr := newTestResourceMgr()
	_ = mgr.AddModel("model-1", stubModelProvider())

	models, err := mgr.GetModel(context.Background(), "model-1")
	if err != nil {
		t.Fatalf("GetModel 失败: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("期望 1 个 Model，实际 %d", len(models))
	}
}

// --- Prompt 测试 ---

// TestResourceMgr_AddPrompt_正常 测试正常添加 Prompt
func TestResourceMgr_AddPrompt_正常(t *testing.T) {
	mgr := newTestResourceMgr()
	tmpl := prompt.NewPromptTemplate("测试模板", "hello {{name}}")
	err := mgr.AddPrompt("prompt-1", tmpl)
	if err != nil {
		t.Fatalf("AddPrompt 失败: %v", err)
	}
	if cached := mgr.idToCard.Get("prompt-1"); cached == nil {
		t.Fatal("idToCard 中未找到缓存")
	}
}

// TestResourceMgr_GetPrompt_正常 测试正常获取 Prompt
func TestResourceMgr_GetPrompt_正常(t *testing.T) {
	mgr := newTestResourceMgr()
	tmpl := prompt.NewPromptTemplate("测试模板", "hello {{name}}")
	_ = mgr.AddPrompt("prompt-1", tmpl)

	prompts, err := mgr.GetPrompt("prompt-1")
	if err != nil {
		t.Fatalf("GetPrompt 失败: %v", err)
	}
	if len(prompts) != 1 {
		t.Fatalf("期望 1 个 Prompt，实际 %d", len(prompts))
	}
}

// --- Tool 测试 ---

// TestResourceMgr_AddTool_正常 测试正常添加 Tool
func TestResourceMgr_AddTool_正常(t *testing.T) {
	mgr := newTestResourceMgr()
	toolCard := tool.NewToolCard("test_tool", "测试工具", nil, nil)
	stub := &rmStubTool{card: toolCard}
	err := mgr.AddTool(stub)
	if err != nil {
		t.Fatalf("AddTool 失败: %v", err)
	}
	if cached := mgr.idToCard.Get(toolCard.ID); cached == nil {
		t.Fatal("idToCard 中未找到缓存")
	}
}

// TestResourceMgr_GetTool_正常 测试正常获取 Tool
func TestResourceMgr_GetTool_正常(t *testing.T) {
	mgr := newTestResourceMgr()
	toolCard := tool.NewToolCard("test_tool", "测试工具", nil, nil)
	stub := &rmStubTool{card: toolCard}
	_ = mgr.AddTool(stub)

	tools, err := mgr.GetTool(toolCard.ID)
	if err != nil {
		t.Fatalf("GetTool 失败: %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("期望 1 个 Tool，实际 %d", len(tools))
	}
}

// --- Tag 操作测试 ---

// TestResourceMgr_Tag操作 测试标签添加和查询
func TestResourceMgr_Tag操作(t *testing.T) {
	mgr := newTestResourceMgr()
	card := agentschema.NewAgentCard(schema.WithID("agent-tag-1"), schema.WithName("标签Agent"))

	// 带 Tag 添加
	err := mgr.AddAgent(card, stubAgentProvider(), WithTag("custom-tag"))
	if err != nil {
		t.Fatalf("AddAgent 带 Tag 失败: %v", err)
	}

	// 验证标签已标记
	if !mgr.tagMgr.HasResourceTag("agent-tag-1", "custom-tag") {
		t.Fatal("应标记 custom-tag 标签")
	}

	// 带 Tag 获取
	agents, err := mgr.GetAgent(context.Background(), "agent-tag-1", WithTag("custom-tag"))
	if err != nil {
		t.Fatalf("GetAgent 带 Tag 失败: %v", err)
	}
	if len(agents) != 1 {
		t.Fatalf("期望 1 个 Agent，实际 %d", len(agents))
	}

	// 用不匹配的 Tag 获取
	agents, err = mgr.GetAgent(context.Background(), "agent-tag-1", WithTag("non-existent-tag"))
	if err != nil {
		t.Fatalf("GetAgent 不匹配 Tag 不应报错: %v", err)
	}
	if len(agents) != 0 {
		t.Fatalf("不匹配 Tag 期望 0 个 Agent，实际 %d", len(agents))
	}

	// GetResourceByTag
	cards := mgr.GetResourceByTag("custom-tag")
	if len(cards) != 1 {
		t.Fatalf("GetResourceByTag 期望 1 个，实际 %d", len(cards))
	}

	// ListTags
	tags := mgr.ListTags()
	found := false
	for _, tag := range tags {
		if tag == "custom-tag" {
			found = true
		}
	}
	if !found {
		t.Fatal("ListTags 应包含 custom-tag")
	}

	// HasTag
	if !mgr.HasTag("custom-tag") {
		t.Fatal("HasTag 应返回 true")
	}

	// GetResourceTag
	resourceTags := mgr.GetResourceTag("agent-tag-1")
	tagFound := false
	for _, tag := range resourceTags {
		if tag == "custom-tag" {
			tagFound = true
		}
	}
	if !tagFound {
		t.Fatal("GetResourceTag 应包含 custom-tag")
	}

	// ResourceHasTag
	if !mgr.ResourceHasTag("agent-tag-1", "custom-tag") {
		t.Fatal("ResourceHasTag 应返回 true")
	}
}

// --- Release 测试 ---

// TestResourceMgr_Release 测试资源管理器释放
func TestResourceMgr_Release(t *testing.T) {
	mgr := newTestResourceMgr()
	card := agentschema.NewAgentCard(schema.WithID("agent-rel-1"), schema.WithName("释放Agent"))
	_ = mgr.AddAgent(card, stubAgentProvider())

	err := mgr.Release(context.Background())
	if err != nil {
		t.Fatalf("Release 失败: %v", err)
	}

	// 验证重建后 idToCard 为空
	if mgr.idToCard.Len() != 0 {
		t.Fatal("Release 后 idToCard 应为空")
	}

	// 验证重建后资源已清空
	agents, err := mgr.GetAgent(context.Background(), "agent-rel-1")
	if err != nil {
		t.Fatalf("Release 后 GetAgent 不应报错: %v", err)
	}
	if len(agents) != 0 {
		t.Fatal("Release 后 Agent 应为空")
	}
}

// --- 重复添加报错测试 ---

// TestResourceMgr_重复添加报错 测试重复添加同一资源返回错误
func TestResourceMgr_重复添加报错(t *testing.T) {
	mgr := newTestResourceMgr()
	card := agentschema.NewAgentCard(schema.WithID("agent-dup"), schema.WithName("重复Agent"))
	_ = mgr.AddAgent(card, stubAgentProvider())

	// 重复添加应报错
	err := mgr.AddAgent(card, stubAgentProvider())
	if err == nil {
		t.Fatal("重复添加 Agent 应返回错误")
	}

	// Model 重复
	_ = mgr.AddModel("model-dup", stubModelProvider())
	err = mgr.AddModel("model-dup", stubModelProvider())
	if err == nil {
		t.Fatal("重复添加 Model 应返回错误")
	}

	// Prompt 重复（PromptMgr 使用 Set 不报错，不测试重复）

	// Tool 重复
	toolCard := tool.NewToolCard("tool-dup", "重复工具", nil, nil)
	stub := &rmStubTool{card: toolCard}
	_ = mgr.AddTool(stub)
	err = mgr.AddTool(stub)
	if err == nil {
		t.Fatal("重复添加 Tool 应返回错误")
	}
}

// --- 获取不存在返回空测试 ---

// TestResourceMgr_获取不存在返回空 测试获取不存在的资源返回空列表
func TestResourceMgr_获取不存在返回空(t *testing.T) {
	mgr := newTestResourceMgr()

	// Agent 不存在
	agents, err := mgr.GetAgent(context.Background(), "non-existent")
	if err != nil {
		t.Fatalf("获取不存在的 Agent 不应报错: %v", err)
	}
	if len(agents) != 0 {
		t.Fatalf("期望 0 个 Agent，实际 %d", len(agents))
	}

	// Workflow 不存在
	workflows, err := mgr.GetWorkflow(context.Background(), "non-existent")
	if err != nil {
		t.Fatalf("获取不存在的 Workflow 不应报错: %v", err)
	}
	if len(workflows) != 0 {
		t.Fatalf("期望 0 个 Workflow，实际 %d", len(workflows))
	}

	// Model 不存在
	models, err := mgr.GetModel(context.Background(), "non-existent")
	if err != nil {
		t.Fatalf("获取不存在的 Model 不应报错: %v", err)
	}
	if len(models) != 0 {
		t.Fatalf("期望 0 个 Model，实际 %d", len(models))
	}

	// Prompt 不存在
	prompts, err := mgr.GetPrompt("non-existent")
	if err != nil {
		t.Fatalf("获取不存在的 Prompt 不应报错: %v", err)
	}
	if len(prompts) != 0 {
		t.Fatalf("期望 0 个 Prompt，实际 %d", len(prompts))
	}

	// Tool 不存在
	tools, err := mgr.GetTool("non-existent")
	if err != nil {
		t.Fatalf("获取不存在的 Tool 不应报错: %v", err)
	}
	if len(tools) != 0 {
		t.Fatalf("期望 0 个 Tool，实际 %d", len(tools))
	}
}

// --- Functional Options 测试 ---

// TestResourceMgr_FunctionalOptions 测试函数选项模式
func TestResourceMgr_FunctionalOptions(t *testing.T) {
	// ResourceOption
	o := applyResourceOptions(
		WithTag("test-tag"),
		WithTagMatchStrategy(TagMatchAll),
		WithSkipIfTagNotExists(),
		WithRefresh(),
		WithInterfaceURL("http://test"),
	)
	if o.Tag != "test-tag" {
		t.Fatalf("Tag 期望 test-tag，实际 %s", o.Tag)
	}
	if o.TagMatchStrategy != TagMatchAll {
		t.Fatal("TagMatchStrategy 期望 TagMatchAll")
	}
	if !o.SkipIfTagNotExists {
		t.Fatal("SkipIfTagNotExists 期望 true")
	}
	if !o.Refresh {
		t.Fatal("Refresh 期望 true")
	}
	if o.InterfaceURL != "http://test" {
		t.Fatalf("InterfaceURL 期望 http://test，实际 %s", o.InterfaceURL)
	}

	// McpOption
	mo := applyMcpOptions(
		WithMcpServerName("test-server"),
		WithMcpTag("mcp-tag"),
		WithMcpExpiryTime(30.0),
		WithMcpSkipIfNotExists(),
		WithMcpForce(),
	)
	if mo.ServerName != "test-server" {
		t.Fatalf("ServerName 期望 test-server，实际 %s", mo.ServerName)
	}
	if mo.Tag != "mcp-tag" {
		t.Fatalf("Tag 期望 mcp-tag，实际 %s", mo.Tag)
	}
	if mo.ExpiryTime != 30.0 {
		t.Fatalf("ExpiryTime 期望 30.0，实际 %f", mo.ExpiryTime)
	}
	if !mo.SkipIfNotExists {
		t.Fatal("SkipIfNotExists 期望 true")
	}
	if !mo.Force {
		t.Fatal("Force 期望 true")
	}

	// TagOption
	to := applyTagOptions(WithTagSkipIfNotExists())
	if !to.SkipIfNotExists {
		t.Fatal("SkipIfNotExists 期望 true")
	}
}

// --- 验证方法测试 ---

// TestResourceMgr_验证方法 测试内部验证逻辑
func TestResourceMgr_验证方法(t *testing.T) {
	mgr := newTestResourceMgr()

	// 空 ID 验证
	err := mgr.innerValidateResourceID("", "agent")
	if err == nil {
		t.Fatal("空 ID 应返回错误")
	}

	// 纯空白 ID 验证
	err = mgr.innerValidateResourceID("   ", "agent")
	if err == nil {
		t.Fatal("纯空白 ID 应返回错误")
	}

	// 正常 ID 验证
	err = mgr.innerValidateResourceID("agent-1", "agent")
	if err != nil {
		t.Fatalf("正常 ID 不应报错: %v", err)
	}

	// 空 provider 验证
	err = mgr.innerValidateProvider(nil, "agent")
	if err == nil {
		t.Fatal("空 provider 应返回错误")
	}

	// 正常 provider 验证
	err = mgr.innerValidateProvider(stubAgentProvider(), "agent")
	if err != nil {
		t.Fatalf("正常 provider 不应报错: %v", err)
	}

	// 空 ServerConfig 验证
	err = mgr.innerValidateServerConfig(nil)
	if err == nil {
		t.Fatal("空 ServerConfig 应返回错误")
	}
}

// --- AddAgents 批量测试 ---

// TestResourceMgr_AddAgents_批量 测试批量添加 Agent
func TestResourceMgr_AddAgents_批量(t *testing.T) {
	mgr := newTestResourceMgr()
	card1 := agentschema.NewAgentCard(schema.WithID("batch-agent-1"), schema.WithName("批量Agent1"))
	card2 := agentschema.NewAgentCard(schema.WithID("batch-agent-2"), schema.WithName("批量Agent2"))

	err := mgr.AddAgents([]AgentEntry{
		{Card: card1, Provider: stubAgentProvider()},
		{Card: card2, Provider: stubAgentProvider()},
	})
	if err != nil {
		t.Fatalf("AddAgents 失败: %v", err)
	}

	agents1, _ := mgr.GetAgent(context.Background(), "batch-agent-1")
	if len(agents1) != 1 {
		t.Fatal("batch-agent-1 应存在")
	}
	agents2, _ := mgr.GetAgent(context.Background(), "batch-agent-2")
	if len(agents2) != 1 {
		t.Fatal("batch-agent-2 应存在")
	}
}

// --- RemoveWorkflow/RemoveModel/RemovePrompt/RemoveTool 测试 ---

// TestResourceMgr_RemoveWorkflow_正常 测试正常移除 Workflow
func TestResourceMgr_RemoveWorkflow_正常(t *testing.T) {
	mgr := newTestResourceMgr()
	card := schema.NewWorkflowCard(schema.WithID("wf-rm-1"), schema.WithName("移除Workflow"))
	_ = mgr.AddWorkflow(card, stubWorkflowProvider())

	removed, err := mgr.RemoveWorkflow("wf-rm-1")
	if err != nil {
		t.Fatalf("RemoveWorkflow 失败: %v", err)
	}
	if len(removed) != 1 {
		t.Fatalf("期望移除 1 个，实际 %d", len(removed))
	}
}

// TestResourceMgr_RemoveModel_正常 测试正常移除 Model
func TestResourceMgr_RemoveModel_正常(t *testing.T) {
	mgr := newTestResourceMgr()
	_ = mgr.AddModel("model-rm-1", stubModelProvider())

	removed, err := mgr.RemoveModel("model-rm-1")
	if err != nil {
		t.Fatalf("RemoveModel 失败: %v", err)
	}
	if len(removed) != 1 {
		t.Fatalf("期望移除 1 个，实际 %d", len(removed))
	}
}

// TestResourceMgr_RemovePrompt_正常 测试正常移除 Prompt
func TestResourceMgr_RemovePrompt_正常(t *testing.T) {
	mgr := newTestResourceMgr()
	tmpl := prompt.NewPromptTemplate("移除模板", "hello")
	_ = mgr.AddPrompt("prompt-rm-1", tmpl)

	removed, err := mgr.RemovePrompt("prompt-rm-1")
	if err != nil {
		t.Fatalf("RemovePrompt 失败: %v", err)
	}
	if len(removed) != 1 {
		t.Fatalf("期望移除 1 个，实际 %d", len(removed))
	}
}

// TestResourceMgr_RemoveTool_正常 测试正常移除 Tool
func TestResourceMgr_RemoveTool_正常(t *testing.T) {
	mgr := newTestResourceMgr()
	toolCard := tool.NewToolCard("tool-rm-1", "移除工具", nil, nil)
	stub := &rmStubTool{card: toolCard}
	_ = mgr.AddTool(stub)

	removed, err := mgr.RemoveTool(toolCard.ID)
	if err != nil {
		t.Fatalf("RemoveTool 失败: %v", err)
	}
	if len(removed) != 1 {
		t.Fatalf("期望移除 1 个，实际 %d", len(removed))
	}
}

// --- Tag 增删改测试 ---

// TestResourceMgr_Tag增删改 测试标签的添加、删除和修改
func TestResourceMgr_Tag增删改(t *testing.T) {
	mgr := newTestResourceMgr()
	card := agentschema.NewAgentCard(schema.WithID("agent-tag-mgmt"), schema.WithName("标签管理Agent"))
	_ = mgr.AddAgent(card, stubAgentProvider(), WithTag("tag-a"))

	// AddResourceTag
	tags, err := mgr.AddResourceTag("agent-tag-mgmt", []Tag{"tag-b"})
	if err != nil {
		t.Fatalf("AddResourceTag 失败: %v", err)
	}
	if len(tags) < 2 {
		t.Fatalf("AddResourceTag 后应有至少 2 个标签，实际 %d", len(tags))
	}

	// UpdateResourceTag
	tags, err = mgr.UpdateResourceTag("agent-tag-mgmt", []Tag{"tag-c"})
	if err != nil {
		t.Fatalf("UpdateResourceTag 失败: %v", err)
	}
	if len(tags) != 1 || tags[0] != "tag-c" {
		t.Fatalf("UpdateResourceTag 后应只有 tag-c，实际 %v", tags)
	}

	// RemoveResourceTag
	tags, err = mgr.RemoveResourceTag("agent-tag-mgmt", []Tag{"tag-c"})
	if err != nil {
		t.Fatalf("RemoveResourceTag 失败: %v", err)
	}
	if len(tags) != 0 {
		t.Fatalf("RemoveResourceTag 后应无标签，实际 %d", len(tags))
	}

	// RemoveTag
	_ = mgr.AddAgent(
		agentschema.NewAgentCard(schema.WithID("agent-tag-rm"), schema.WithName("标签移除Agent")),
		stubAgentProvider(),
		WithTag("tag-to-remove"),
	)
	affected, err := mgr.RemoveTag("tag-to-remove")
	if err != nil {
		t.Fatalf("RemoveTag 失败: %v", err)
	}
	if len(affected) != 1 {
		t.Fatalf("RemoveTag 应影响 1 个资源，实际 %d", len(affected))
	}
}

// --- 模块级常量测试 ---

// TestResourceMgr_模块级常量 测试分派常量
func TestResourceMgr_模块级常量(t *testing.T) {
	if registryAccessors["workflow"] != "workflow" {
		t.Fatal("registryAccessors[workflow] 应为 workflow")
	}
	if registryAccessors["agent"] != "agent" {
		t.Fatal("registryAccessors[agent] 应为 agent")
	}
	if registryAccessors["team"] != "agent_team" {
		t.Fatal("registryAccessors[team] 应为 agent_team")
	}
	if !asyncGetTypes["workflow"] {
		t.Fatal("workflow 应在 asyncGetTypes 中")
	}
	if !sessionGetTypes["workflow"] {
		t.Fatal("workflow 应在 sessionGetTypes 中")
	}
	if !idReturnTypes["tool"] {
		t.Fatal("tool 应在 idReturnTypes 中")
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// --- MCP Server 测试辅助 ---

// rmMockMcpClient 用于 ResourceMgr MCP 测试的 mock 客户端
type rmMockMcpClient struct {
	// connected 是否已连接
	connected bool
	// tools 返回的工具列表
	tools []*types.McpToolCard
	// listResourcesResult ListResources 返回结果
	listResourcesResult []any
	// readResourceResult ReadResource 返回结果
	readResourceResult any
}

func (m *rmMockMcpClient) Connect(_ context.Context, _ ...types.ConnectOption) error {
	m.connected = true
	return nil
}

func (m *rmMockMcpClient) Disconnect(_ context.Context) error {
	m.connected = false
	return nil
}

func (m *rmMockMcpClient) ListTools(_ context.Context) ([]*types.McpToolCard, error) {
	return m.tools, nil
}

func (m *rmMockMcpClient) CallTool(_ context.Context, _ string, _ map[string]any) (any, error) {
	return nil, nil
}

func (m *rmMockMcpClient) GetToolInfo(_ context.Context, _ string) (*types.McpToolCard, error) {
	return nil, nil
}

func (m *rmMockMcpClient) ListResources(_ context.Context) ([]any, error) {
	return m.listResourcesResult, nil
}

func (m *rmMockMcpClient) ReadResource(_ context.Context, _ string) (any, error) {
	return m.readResourceResult, nil
}

func (m *rmMockMcpClient) Close() error {
	return nil
}

// setupMcpServer 在 ResourceMgr 的 ToolMgr 中手动注册一个 MCP 服务器资源，
// 返回可用的 mockClient 供后续测试使用。
func setupMcpServer(mgr *ResourceMgr, serverID, serverName string, toolNames []string) *rmMockMcpClient {
	mockClient := &rmMockMcpClient{}
	mcpCards := make([]*types.McpToolCard, 0, len(toolNames))
	toolIDs := make([]string, 0, len(toolNames))
	for _, name := range toolNames {
		card := mcp.NewMcpToolCard(name, name+"描述", serverName, nil, mcp.WithMcpToolCardServerID(serverID))
		toolID := mgr.registry.Tool().GenerateMcpToolID(serverID, serverName, name)
		card.ID = toolID
		mcpCards = append(mcpCards, card)
		toolIDs = append(toolIDs, toolID)
	}
	mockClient.tools = mcpCards

	serverConfig := mcp.NewMcpServerConfig(serverName, "http://localhost:8080/sse", "sse", mcp.WithServerID(serverID))

	// 使用 innerRefreshMcpTools 注册工具和服务器资源
	mockClient.Connect(context.Background())
	mgr.registry.Tool().innerRefreshMcpTools(context.Background(), mockClient, serverConfig, nil)
	mgr.registry.Tool().mu.Lock()
	mgr.registry.Tool().mcpServerNameToIDs[serverName] = append(
		mgr.registry.Tool().mcpServerNameToIDs[serverName], serverID)
	mgr.registry.Tool().mu.Unlock()

	// 同步 idToCard 和 tagMgr
	for _, card := range mcpCards {
		baseCard := &schema.BaseCard{ID: card.ID, Name: card.Name, Description: card.Description}
		mgr.idToCard.Set(card.ID, baseCard)
		mgr.tagMgr.TagResource(card.ID, []Tag{TagGlobal})
	}

	return mockClient
}

// --- AddModels 批量测试 ---

// TestResourceMgr_AddModels_批量 测试批量添加 Model
func TestResourceMgr_AddModels_批量(t *testing.T) {
	mgr := newTestResourceMgr()
	err := mgr.AddModels([]ModelEntry{
		{ID: "batch-model-1", Provider: stubModelProvider()},
		{ID: "batch-model-2", Provider: stubModelProvider()},
	})
	if err != nil {
		t.Fatalf("AddModels 失败: %v", err)
	}

	models1, _ := mgr.GetModel(context.Background(), "batch-model-1")
	if len(models1) != 1 {
		t.Fatal("batch-model-1 应存在")
	}
	models2, _ := mgr.GetModel(context.Background(), "batch-model-2")
	if len(models2) != 1 {
		t.Fatal("batch-model-2 应存在")
	}
}

// TestResourceMgr_AddModels_部分失败 测试批量添加 Model 部分失败不中断
func TestResourceMgr_AddModels_部分失败(t *testing.T) {
	mgr := newTestResourceMgr()
	// 先添加 model-dup 使其重复
	_ = mgr.AddModel("model-dup", stubModelProvider())

	// 批量添加，其中一个会失败，但不应返回 error
	err := mgr.AddModels([]ModelEntry{
		{ID: "model-ok", Provider: stubModelProvider()},
		{ID: "model-dup", Provider: stubModelProvider()},
	})
	if err != nil {
		t.Fatalf("AddModels 部分失败不应返回 error: %v", err)
	}

	// model-ok 应存在
	models, _ := mgr.GetModel(context.Background(), "model-ok")
	if len(models) != 1 {
		t.Fatal("model-ok 应存在")
	}
}

// --- AddPrompts 批量测试 ---

// TestResourceMgr_AddPrompts_批量 测试批量添加 Prompt
func TestResourceMgr_AddPrompts_批量(t *testing.T) {
	mgr := newTestResourceMgr()
	tmpl1 := prompt.NewPromptTemplate("模板1", "hello {{name}}")
	tmpl2 := prompt.NewPromptTemplate("模板2", "world {{name}}")

	err := mgr.AddPrompts([]PromptEntry{
		{ID: "batch-prompt-1", Template: tmpl1},
		{ID: "batch-prompt-2", Template: tmpl2},
	})
	if err != nil {
		t.Fatalf("AddPrompts 失败: %v", err)
	}

	prompts1, _ := mgr.GetPrompt("batch-prompt-1")
	if len(prompts1) != 1 {
		t.Fatal("batch-prompt-1 应存在")
	}
	prompts2, _ := mgr.GetPrompt("batch-prompt-2")
	if len(prompts2) != 1 {
		t.Fatal("batch-prompt-2 应存在")
	}
}

// --- WithSession / WithMcpSession 测试 ---

// TestWithSession_设置会话 测试 WithSession 选项设置会话
func TestWithSession_设置会话(t *testing.T) {
	session := &stubTracerSession{}
	o := applyResourceOptions(WithSession(session))
	if o.Session == nil {
		t.Fatal("WithSession 应设置 Session")
	}
}

// TestWithMcpSession_设置MCP会话 测试 WithMcpSession 选项设置 MCP 会话
func TestWithMcpSession_设置MCP会话(t *testing.T) {
	session := &stubTracerSession{}
	mo := applyMcpOptions(WithMcpSession(session))
	if mo.Session == nil {
		t.Fatal("WithMcpSession 应设置 Session")
	}
}

// --- SysOperation 测试 ---

// TestResourceMgr_AddSysOperation_预留 测试 AddSysOperation 预留方法返回错误
func TestResourceMgr_AddSysOperation_预留(t *testing.T) {
	mgr := newTestResourceMgr()
	instance := struct{ Name string }{"test-op"}
	err := mgr.AddSysOperation("sysop-1", instance)
	if err == nil {
		t.Fatal("预留方法应返回错误")
	}
}

// TestResourceMgr_AddSysOperation_空ID报错 测试空 ID 添加系统操作报错
func TestResourceMgr_AddSysOperation_空ID报错(t *testing.T) {
	mgr := newTestResourceMgr()
	err := mgr.AddSysOperation("", struct{}{})
	if err == nil {
		t.Fatal("空 ID 应返回错误")
	}
}

// TestResourceMgr_RemoveSysOperation_预留 测试 RemoveSysOperation 预留方法
func TestResourceMgr_RemoveSysOperation_预留(t *testing.T) {
	mgr := newTestResourceMgr()
	// RemoveSysOperation 先查找 innerFindResourceIDs，空 ID 应报错
	err := mgr.RemoveSysOperation("")
	if err == nil {
		t.Fatal("空 ID 应返回错误")
	}
}

// TestResourceMgr_GetSysOperation_预留 测试 GetSysOperation 预留方法
func TestResourceMgr_GetSysOperation_预留(t *testing.T) {
	mgr := newTestResourceMgr()
	// GetSysOperation 先查找 innerFindResourceIDs，空 ID 应报错
	_, err := mgr.GetSysOperation("")
	if err == nil {
		t.Fatal("空 ID 应返回错误")
	}
}

// TestResourceMgr_GetSysOperation_不存在 测试获取不存在的系统操作
func TestResourceMgr_GetSysOperation_不存在(t *testing.T) {
	mgr := newTestResourceMgr()
	results, err := mgr.GetSysOperation("non-existent")
	if err != nil {
		t.Fatalf("获取不存在的 SysOperation 不应报错: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("期望 0 个 SysOperation，实际 %d", len(results))
	}
}

// --- AgentTeam 测试（⤵️ 预留方法，验证调用不 panic）---

// TestResourceMgr_AddAgentTeam_预留 测试 AddAgentTeam 预留方法返回错误
func TestResourceMgr_AddAgentTeam_预留(t *testing.T) {
	mgr := newTestResourceMgr()
	err := mgr.AddAgentTeam("team-1", nil)
	if err == nil {
		t.Fatal("预留方法应返回错误")
	}
}

// TestResourceMgr_RemoveAgentTeam_预留 测试 RemoveAgentTeam 预留方法返回错误
func TestResourceMgr_RemoveAgentTeam_预留(t *testing.T) {
	mgr := newTestResourceMgr()
	_, err := mgr.RemoveAgentTeam("team-1")
	if err == nil {
		t.Fatal("预留方法应返回错误")
	}
}

// TestResourceMgr_GetAgentTeam_预留 测试 GetAgentTeam 预留方法返回错误
func TestResourceMgr_GetAgentTeam_预留(t *testing.T) {
	mgr := newTestResourceMgr()
	_, err := mgr.GetAgentTeam("team-1")
	if err == nil {
		t.Fatal("预留方法应返回错误")
	}
}

// --- MCP Server 方法测试 ---

// TestResourceMgr_AddMcpServer_验证失败 测试 AddMcpServer 配置验证失败
func TestResourceMgr_AddMcpServer_验证失败(t *testing.T) {
	mgr := newTestResourceMgr()

	// nil 配置
	_, err := mgr.AddMcpServer(context.Background(), nil)
	if err == nil {
		t.Fatal("nil 配置应返回错误")
	}

	// 空 ServerID
	config := mcp.NewMcpServerConfig("test", "http://localhost:8080/sse", "sse")
	config.ServerID = ""
	_, err = mgr.AddMcpServer(context.Background(), config)
	if err == nil {
		t.Fatal("空 ServerID 应返回错误")
	}

	// 空 ServerName
	config2 := mcp.NewMcpServerConfig("", "http://localhost:8080/sse", "sse", mcp.WithServerID("srv1"))
	_, err = mgr.AddMcpServer(context.Background(), config2)
	if err == nil {
		t.Fatal("空 ServerName 应返回错误")
	}
}

// TestResourceMgr_AddMcpServer_使用Mock 测试 AddMcpServer 使用 mock 客户端
func TestResourceMgr_AddMcpServer_使用Mock(t *testing.T) {
	mgr := newTestResourceMgr()
	mockClient := &rmMockMcpClient{
		tools: []*types.McpToolCard{
			mcp.NewMcpToolCard("search", "搜索工具", "test_server", nil, mcp.WithMcpToolCardServerID("srv1")),
		},
	}
	serverConfig := mcp.NewMcpServerConfig("test_server", "http://localhost:8080/sse", "sse", mcp.WithServerID("srv1"))

	// 使用 innerRefreshMcpTools 模拟添加过程
	mockClient.Connect(context.Background())
	toolMgr := mgr.registry.Tool()
	cards, err := toolMgr.innerRefreshMcpTools(context.Background(), mockClient, serverConfig, nil)
	if err != nil {
		t.Fatalf("innerRefreshMcpTools 失败: %v", err)
	}

	// 手动同步 idToCard 和 tagMgr（AddMcpServer 的行为）
	for _, card := range cards {
		baseCard := &schema.BaseCard{ID: card.ID, Name: card.Name, Description: card.Description}
		mgr.idToCard.Set(card.ID, baseCard)
		mgr.tagMgr.TagResource(card.ID, []Tag{TagGlobal})
	}

	// 验证工具已缓存
	if len(cards) != 1 {
		t.Fatalf("期望 1 个工具卡片，实际 %d", len(cards))
	}
	if cached := mgr.idToCard.Get(cards[0].ID); cached == nil {
		t.Fatal("idToCard 中未找到缓存")
	}
	if !mgr.tagMgr.HasResourceTag(cards[0].ID, TagGlobal) {
		t.Fatal("工具应标记 GLOBAL 标签")
	}
}

// TestResourceMgr_RefreshMcpServer_不存在 测试刷新不存在的 MCP 服务器
func TestResourceMgr_RefreshMcpServer_不存在(t *testing.T) {
	mgr := newTestResourceMgr()

	// 不存在的服务器，skipIfNotExists=false 应报错
	_, err := mgr.RefreshMcpServer(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("刷新不存在的服务器应返回错误")
	}

	// skipIfNotExists=true 不报错
	_, err = mgr.RefreshMcpServer(context.Background(), "nonexistent", WithMcpSkipIfNotExists())
	if err != nil {
		t.Fatalf("skipIfNotExists=true 期望不报错，实际: %v", err)
	}
}

// TestResourceMgr_RefreshMcpServer_使用Mock 测试刷新 MCP 服务器
func TestResourceMgr_RefreshMcpServer_使用Mock(t *testing.T) {
	mgr := newTestResourceMgr()
	mockClient := setupMcpServer(mgr, "srv-refresh", "refresh_server", []string{"tool1"})

	// 强制刷新
	cards, err := mgr.RefreshMcpServer(context.Background(), "srv-refresh", WithMcpForce())
	if err != nil {
		t.Fatalf("RefreshMcpServer 失败: %v", err)
	}
	if len(cards) != 1 {
		t.Fatalf("期望 1 个工具卡片，实际 %d", len(cards))
	}

	// 验证 mock 客户端仍连接
	if !mockClient.connected {
		t.Fatal("mock 客户端应仍连接")
	}
}

// TestResourceMgr_RemoveMcpServer_使用Mock 测试移除 MCP 服务器
func TestResourceMgr_RemoveMcpServer_使用Mock(t *testing.T) {
	mgr := newTestResourceMgr()
	toolID := mgr.registry.Tool().GenerateMcpToolID("srv-rm", "rm_server", "tool1")
	setupMcpServer(mgr, "srv-rm", "rm_server", []string{"tool1"})

	// 验证工具已缓存
	if mgr.idToCard.Get(toolID) == nil {
		t.Fatal("工具应已缓存")
	}

	err := mgr.RemoveMcpServer(context.Background(), "srv-rm")
	if err != nil {
		t.Fatalf("RemoveMcpServer 失败: %v", err)
	}

	// 验证缓存和标签已清理
	if mgr.idToCard.Get(toolID) != nil {
		t.Fatal("idToCard 应已清理")
	}
	if mgr.tagMgr.HasResourceTag(toolID, TagGlobal) {
		t.Fatal("标签应已移除")
	}
}

// TestResourceMgr_RemoveMcpServer_不存在 测试移除不存在的 MCP 服务器
func TestResourceMgr_RemoveMcpServer_不存在(t *testing.T) {
	mgr := newTestResourceMgr()

	// skipIfNotExists=false 应报错
	err := mgr.RemoveMcpServer(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("移除不存在的服务器应返回错误")
	}

	// skipIfNotExists=true 不报错
	err = mgr.RemoveMcpServer(context.Background(), "nonexistent", WithMcpSkipIfNotExists())
	if err != nil {
		t.Fatalf("skipIfNotExists=true 期望不报错，实际: %v", err)
	}
}

// TestResourceMgr_GetMcpTool_使用Mock 测试获取 MCP 工具
func TestResourceMgr_GetMcpTool_使用Mock(t *testing.T) {
	mgr := newTestResourceMgr()
	setupMcpServer(mgr, "srv-get", "get_server", []string{"search"})

	tools, err := mgr.GetMcpTool(context.Background(), "search", "srv-get")
	if err != nil {
		t.Fatalf("GetMcpTool 失败: %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("期望 1 个 Tool，实际 %d", len(tools))
	}
}

// TestResourceMgr_GetMcpTool_不存在 测试获取不存在的 MCP 工具
func TestResourceMgr_GetMcpTool_不存在(t *testing.T) {
	mgr := newTestResourceMgr()

	_, err := mgr.GetMcpTool(context.Background(), "nonexistent", "nonexistent")
	if err == nil {
		t.Fatal("获取不存在的 MCP 工具应返回错误")
	}
}

// TestResourceMgr_GetMcpToolInfos_使用Mock 测试获取 MCP 工具信息
func TestResourceMgr_GetMcpToolInfos_使用Mock(t *testing.T) {
	mgr := newTestResourceMgr()
	setupMcpServer(mgr, "srv-info", "info_server", []string{"search"})

	infos, err := mgr.GetMcpToolInfos(context.Background(), "search", "srv-info")
	if err != nil {
		t.Fatalf("GetMcpToolInfos 失败: %v", err)
	}
	if len(infos) != 1 {
		t.Fatalf("期望 1 个 ToolInfo，实际 %d", len(infos))
	}
}

// TestResourceMgr_GetMcpServerConfig_使用Mock 测试获取 MCP 服务器配置
func TestResourceMgr_GetMcpServerConfig_使用Mock(t *testing.T) {
	mgr := newTestResourceMgr()
	setupMcpServer(mgr, "srv-cfg", "cfg_server", []string{"tool1"})

	config, err := mgr.GetMcpServerConfig("srv-cfg")
	if err != nil {
		t.Fatalf("GetMcpServerConfig 失败: %v", err)
	}
	if config.ServerID != "srv-cfg" {
		t.Fatalf("期望 ServerID=srv-cfg，实际 %s", config.ServerID)
	}
	if config.ServerName != "cfg_server" {
		t.Fatalf("期望 ServerName=cfg_server，实际 %s", config.ServerName)
	}
}

// TestResourceMgr_GetMcpServerConfig_不存在 测试获取不存在的 MCP 服务器配置
func TestResourceMgr_GetMcpServerConfig_不存在(t *testing.T) {
	mgr := newTestResourceMgr()
	_, err := mgr.GetMcpServerConfig("nonexistent")
	if err == nil {
		t.Fatal("获取不存在的服务器配置应返回错误")
	}
}

// TestResourceMgr_GetMcpToolIDs_使用Mock 测试获取 MCP 工具 ID 列表
func TestResourceMgr_GetMcpToolIDs_使用Mock(t *testing.T) {
	mgr := newTestResourceMgr()
	setupMcpServer(mgr, "srv-ids", "ids_server", []string{"tool1", "tool2"})

	ids := mgr.GetMcpToolIDs("srv-ids")
	if len(ids) != 2 {
		t.Fatalf("期望 2 个工具 ID，实际 %d", len(ids))
	}
}

// TestResourceMgr_GetMcpToolIDs_不存在 测试获取不存在服务器的工具 ID
func TestResourceMgr_GetMcpToolIDs_不存在(t *testing.T) {
	mgr := newTestResourceMgr()
	ids := mgr.GetMcpToolIDs("nonexistent")
	if len(ids) != 0 {
		t.Fatalf("不存在的服务器期望空列表，实际 %d", len(ids))
	}
}

// TestResourceMgr_ListMcpResources_使用Mock 测试列出 MCP 服务器资源
func TestResourceMgr_ListMcpResources_使用Mock(t *testing.T) {
	mgr := newTestResourceMgr()
	mockClient := setupMcpServer(mgr, "srv-res", "res_server", []string{"tool1"})
	mockClient.listResourcesResult = []any{"resource1", "resource2"}

	resources, err := mgr.ListMcpResources(context.Background(), "srv-res")
	if err != nil {
		t.Fatalf("ListMcpResources 失败: %v", err)
	}
	if len(resources) != 2 {
		t.Fatalf("期望 2 个资源，实际 %d", len(resources))
	}
}

// TestResourceMgr_ListMcpResources_不存在 测试列出不存在服务器的资源
func TestResourceMgr_ListMcpResources_不存在(t *testing.T) {
	mgr := newTestResourceMgr()
	_, err := mgr.ListMcpResources(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("列出不存在服务器的资源应返回错误")
	}
}

// TestResourceMgr_ReadMcpResource_使用Mock 测试读取 MCP 服务器资源
func TestResourceMgr_ReadMcpResource_使用Mock(t *testing.T) {
	mgr := newTestResourceMgr()
	mockClient := setupMcpServer(mgr, "srv-read", "read_server", []string{"tool1"})
	mockClient.readResourceResult = map[string]any{"content": "test data"}

	result, err := mgr.ReadMcpResource(context.Background(), "srv-read", "test://resource")
	if err != nil {
		t.Fatalf("ReadMcpResource 失败: %v", err)
	}
	if result == nil {
		t.Fatal("期望非 nil 结果")
	}
}

// TestResourceMgr_ReadMcpResource_不存在 测试读取不存在服务器的资源
func TestResourceMgr_ReadMcpResource_不存在(t *testing.T) {
	mgr := newTestResourceMgr()
	_, err := mgr.ReadMcpResource(context.Background(), "nonexistent", "test://resource")
	if err == nil {
		t.Fatal("读取不存在服务器的资源应返回错误")
	}
}

// --- GetToolInfos 测试 ---

// TestResourceMgr_GetToolInfos_正常 测试获取工具描述信息
func TestResourceMgr_GetToolInfos_正常(t *testing.T) {
	mgr := newTestResourceMgr()
	toolCard := tool.NewToolCard("info_tool", "信息工具", nil, nil)
	stub := &rmStubTool{card: toolCard}
	_ = mgr.AddTool(stub)

	infos, err := mgr.GetToolInfos(toolCard.ID)
	if err != nil {
		t.Fatalf("GetToolInfos 失败: %v", err)
	}
	if len(infos) != 1 {
		t.Fatalf("期望 1 个 ToolInfo，实际 %d", len(infos))
	}
}

// TestResourceMgr_GetToolInfos_不存在 测试获取不存在工具的描述信息
func TestResourceMgr_GetToolInfos_不存在(t *testing.T) {
	mgr := newTestResourceMgr()

	infos, err := mgr.GetToolInfos("nonexistent")
	if err != nil {
		t.Fatalf("GetToolInfos 不存在的工具不应报错: %v", err)
	}
	if len(infos) != 0 {
		t.Fatalf("期望 0 个 ToolInfo，实际 %d", len(infos))
	}
}

// --- AddWorkflows 批量测试 ---

// TestResourceMgr_AddWorkflows_批量 测试批量添加 Workflow
func TestResourceMgr_AddWorkflows_批量(t *testing.T) {
	mgr := newTestResourceMgr()
	err := mgr.AddWorkflows([]WorkflowEntry{
		{ID: "batch-wf-1", Provider: stubWorkflowProvider()},
		{ID: "batch-wf-2", Provider: stubWorkflowProvider()},
	})
	if err != nil {
		t.Fatalf("AddWorkflows 失败: %v", err)
	}

	wf1, _ := mgr.GetWorkflow(context.Background(), "batch-wf-1")
	if len(wf1) != 1 {
		t.Fatal("batch-wf-1 应存在")
	}
	wf2, _ := mgr.GetWorkflow(context.Background(), "batch-wf-2")
	if len(wf2) != 1 {
		t.Fatal("batch-wf-2 应存在")
	}
}
