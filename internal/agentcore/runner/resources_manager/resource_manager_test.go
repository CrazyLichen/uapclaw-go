package resources_manager

import (
	"context"
	"reflect"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/prompt"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/mcp"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/mcp/types"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
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
	_ = mockClient.Connect(context.Background())
	_, _ = mgr.registry.Tool().innerRefreshMcpTools(context.Background(), mockClient, serverConfig, nil)
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
	_ = mockClient.Connect(context.Background())
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

	ids, err := mgr.RemoveMcpServer(context.Background(), "srv-rm")
	if err != nil {
		t.Fatalf("RemoveMcpServer 失败: %v", err)
	}
	if len(ids) != 1 || ids[0] != "srv-rm" {
		t.Fatalf("期望返回 [srv-rm]，实际 %v", ids)
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
	_, err := mgr.RemoveMcpServer(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("移除不存在的服务器应返回错误")
	}

	// skipIfNotExists=true 不报错
	_, err = mgr.RemoveMcpServer(context.Background(), "nonexistent", WithMcpSkipIfNotExists())
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

// ──────────────────────────── 核心分派方法测试 ────────────────────────────

// TestGetMgr_各资源类型 测试 getMgr 对各资源类型返回正确的子管理器
func TestGetMgr_各资源类型(t *testing.T) {
	mgr := newTestResourceMgr()

	// workflow → WorkflowMgr
	wm := mgr.getMgr("workflow")
	if wm == nil {
		t.Fatal("getMgr(workflow) 不应返回 nil")
	}
	// 指针类型 Name() 为空，需取 Elem
	wmType := reflect.TypeOf(wm)
	if wmType.Kind() == reflect.Ptr {
		wmType = wmType.Elem()
	}
	if wmType.Name() != "WorkflowMgr" {
		t.Fatalf("getMgr(workflow) 期望 WorkflowMgr，实际 %s", wmType.Name())
	}

	// agent → AgentMgr
	am := mgr.getMgr("agent")
	if am == nil {
		t.Fatal("getMgr(agent) 不应返回 nil")
	}
	amType := reflect.TypeOf(am)
	if amType.Kind() == reflect.Ptr {
		amType = amType.Elem()
	}
	if amType.Name() != "AgentMgr" {
		t.Fatalf("getMgr(agent) 期望 AgentMgr，实际 %s", amType.Name())
	}

	// team → AgentTeamMgr
	tm := mgr.getMgr("team")
	if tm == nil {
		t.Fatal("getMgr(team) 不应返回 nil")
	}
	tmType := reflect.TypeOf(tm)
	if tmType.Kind() == reflect.Ptr {
		tmType = tmType.Elem()
	}
	if tmType.Name() != "AgentTeamMgr" {
		t.Fatalf("getMgr(team) 期望 AgentTeamMgr，实际 %s", tmType.Name())
	}

	// tool → ToolMgr
	tlm := mgr.getMgr("tool")
	if tlm == nil {
		t.Fatal("getMgr(tool) 不应返回 nil")
	}
	tlmType := reflect.TypeOf(tlm)
	if tlmType.Kind() == reflect.Ptr {
		tlmType = tlmType.Elem()
	}
	if tlmType.Name() != "ToolMgr" {
		t.Fatalf("getMgr(tool) 期望 ToolMgr，实际 %s", tlmType.Name())
	}

	// prompt → PromptMgr
	pm := mgr.getMgr("prompt")
	if pm == nil {
		t.Fatal("getMgr(prompt) 不应返回 nil")
	}
	pmType := reflect.TypeOf(pm)
	if pmType.Kind() == reflect.Ptr {
		pmType = pmType.Elem()
	}
	if pmType.Name() != "PromptMgr" {
		t.Fatalf("getMgr(prompt) 期望 PromptMgr，实际 %s", pmType.Name())
	}

	// model → ModelMgr
	mm := mgr.getMgr("model")
	if mm == nil {
		t.Fatal("getMgr(model) 不应返回 nil")
	}
	mmType := reflect.TypeOf(mm)
	if mmType.Kind() == reflect.Ptr {
		mmType = mmType.Elem()
	}
	if mmType.Name() != "ModelMgr" {
		t.Fatalf("getMgr(model) 期望 ModelMgr，实际 %s", mmType.Name())
	}

	// sys_operation → SysOperationMgr
	som := mgr.getMgr("sys_operation")
	if som == nil {
		t.Fatal("getMgr(sys_operation) 不应返回 nil")
	}
	somType := reflect.TypeOf(som)
	if somType.Kind() == reflect.Ptr {
		somType = somType.Elem()
	}
	if somType.Name() != "SysOperationMgr" {
		t.Fatalf("getMgr(sys_operation) 期望 SysOperationMgr，实际 %s", somType.Name())
	}
}

// TestGetMgr_未知类型返回nil 测试 getMgr 对未知类型返回 nil
func TestGetMgr_未知类型返回nil(t *testing.T) {
	mgr := newTestResourceMgr()
	if mgr.getMgr("unknown") != nil {
		t.Fatal("getMgr(unknown) 应返回 nil")
	}
	if mgr.getMgr("") != nil {
		t.Fatal("getMgr(空字符串) 应返回 nil")
	}
}

// TestDispatchAdd_各资源类型 测试 dispatchAdd 对各资源类型正确分派
func TestDispatchAdd_各资源类型(t *testing.T) {
	mgr := newTestResourceMgr()
	ctx := context.Background()

	// workflow
	wfProvider := stubWorkflowProvider()
	wfCard := &schema.BaseCard{ID: "da-wf-1", Name: "测试Workflow"}
	if err := mgr.dispatchAdd("workflow", "da-wf-1", wfProvider, wfCard, ""); err != nil {
		t.Fatalf("dispatchAdd workflow 失败: %v", err)
	}
	// 验证已添加
	wfList, _ := mgr.GetWorkflow(ctx, "da-wf-1")
	if len(wfList) != 1 {
		t.Fatalf("dispatchAdd workflow 后应存在 1 个，实际 %d", len(wfList))
	}

	// agent
	agentProvider := stubAgentProvider()
	agentCard := &schema.BaseCard{ID: "da-agent-1", Name: "测试Agent"}
	if err := mgr.dispatchAdd("agent", "da-agent-1", agentProvider, agentCard, ""); err != nil {
		t.Fatalf("dispatchAdd agent 失败: %v", err)
	}

	// tool
	toolCard := tool.NewToolCard("da-tool-1", "分派工具", nil, nil)
	stubTool := &rmStubTool{card: toolCard}
	if err := mgr.dispatchAdd("tool", toolCard.ID, stubTool, nil, ""); err != nil {
		t.Fatalf("dispatchAdd tool 失败: %v", err)
	}

	// prompt
	tmpl := prompt.NewPromptTemplate("分派模板", "hello")
	if err := mgr.dispatchAdd("prompt", "da-prompt-1", tmpl, nil, ""); err != nil {
		t.Fatalf("dispatchAdd prompt 失败: %v", err)
	}

	// model
	modelProvider := stubModelProvider()
	if err := mgr.dispatchAdd("model", "da-model-1", modelProvider, nil, ""); err != nil {
		t.Fatalf("dispatchAdd model 失败: %v", err)
	}

	// sys_operation（预留方法，预期返回错误）
	if err := mgr.dispatchAdd("sys_operation", "da-sysop-1", struct{ Name string }{"op"}, nil, ""); err == nil {
		t.Fatal("dispatchAdd sys_operation 预留方法应返回错误")
	}
}

// TestDispatchAdd_类型不匹配报错 测试 dispatchAdd 对类型不匹配的资源报错
func TestDispatchAdd_类型不匹配报错(t *testing.T) {
	mgr := newTestResourceMgr()

	// workflow 传入错误类型
	if err := mgr.dispatchAdd("workflow", "bad-wf", "not-a-provider", nil, ""); err == nil {
		t.Fatal("dispatchAdd workflow 传入非 WorkflowProvider 应报错")
	}

	// agent 传入错误类型
	if err := mgr.dispatchAdd("agent", "bad-agent", "not-a-provider", nil, ""); err == nil {
		t.Fatal("dispatchAdd agent 传入非 AgentProvider 应报错")
	}

	// tool 传入错误类型
	if err := mgr.dispatchAdd("tool", "bad-tool", "not-a-tool", nil, ""); err == nil {
		t.Fatal("dispatchAdd tool 传入非 Tool 应报错")
	}

	// prompt 传入错误类型
	if err := mgr.dispatchAdd("prompt", "bad-prompt", "not-a-template", nil, ""); err == nil {
		t.Fatal("dispatchAdd prompt 传入非 PromptTemplate 应报错")
	}

	// model 传入错误类型
	if err := mgr.dispatchAdd("model", "bad-model", "not-a-provider", nil, ""); err == nil {
		t.Fatal("dispatchAdd model 传入非 ModelProvider 应报错")
	}

	// 不支持的资源类型
	if err := mgr.dispatchAdd("unknown", "bad-unknown", nil, nil, ""); err == nil {
		t.Fatal("dispatchAdd 未知类型应报错")
	}
}

// TestDispatchRemove_各资源类型 测试 dispatchRemove 对各资源类型正确分派
func TestDispatchRemove_各资源类型(t *testing.T) {
	mgr := newTestResourceMgr()
	ctx := context.Background()

	// 先添加 agent
	agentCard := agentschema.NewAgentCard(schema.WithID("dr-agent-1"), schema.WithName("移除Agent"))
	_ = mgr.AddAgent(agentCard, stubAgentProvider())

	// dispatchRemove agent
	_, err := mgr.dispatchRemove("agent", "dr-agent-1")
	if err != nil {
		t.Fatalf("dispatchRemove agent 失败: %v", err)
	}

	// 先添加 workflow
	wfCard := schema.NewWorkflowCard(schema.WithID("dr-wf-1"), schema.WithName("移除Workflow"))
	_ = mgr.AddWorkflow(wfCard, stubWorkflowProvider())

	// dispatchRemove workflow
	_, err = mgr.dispatchRemove("workflow", "dr-wf-1")
	if err != nil {
		t.Fatalf("dispatchRemove workflow 失败: %v", err)
	}

	// 先添加 model
	_ = mgr.AddModel("dr-model-1", stubModelProvider())

	// dispatchRemove model
	_, err = mgr.dispatchRemove("model", "dr-model-1")
	if err != nil {
		t.Fatalf("dispatchRemove model 失败: %v", err)
	}

	// 先添加 prompt
	tmpl := prompt.NewPromptTemplate("移除模板", "hello")
	_ = mgr.AddPrompt("dr-prompt-1", tmpl)

	// dispatchRemove prompt
	_, err = mgr.dispatchRemove("prompt", "dr-prompt-1")
	if err != nil {
		t.Fatalf("dispatchRemove prompt 失败: %v", err)
	}

	// 先添加 tool
	toolCard := tool.NewToolCard("dr-tool-1", "移除工具", nil, nil)
	stubTool := &rmStubTool{card: toolCard}
	_ = mgr.AddTool(stubTool)

	// dispatchRemove tool
	_, err = mgr.dispatchRemove("tool", toolCard.ID)
	if err != nil {
		t.Fatalf("dispatchRemove tool 失败: %v", err)
	}

	// 不支持的类型应报错
	_, err = mgr.dispatchRemove("unknown", "nonexistent")
	if err == nil {
		t.Fatal("dispatchRemove 未知类型应报错")
	}

	// 验证移除后资源不存在
	agents, _ := mgr.GetAgent(ctx, "dr-agent-1")
	if len(agents) != 0 {
		t.Fatal("dispatchRemove agent 后资源应不存在")
	}
}

// TestDispatchGet_各资源类型 测试 dispatchGet 对各资源类型正确分派
func TestDispatchGet_各资源类型(t *testing.T) {
	mgr := newTestResourceMgr()
	ctx := context.Background()

	// 添加 agent 并获取
	agentCard := agentschema.NewAgentCard(schema.WithID("dg-agent-1"), schema.WithName("获取Agent"))
	_ = mgr.AddAgent(agentCard, stubAgentProvider())
	result, err := mgr.dispatchGet(ctx, "agent", "dg-agent-1", nil)
	if err != nil {
		t.Fatalf("dispatchGet agent 失败: %v", err)
	}
	if result == nil {
		t.Fatal("dispatchGet agent 结果不应为 nil")
	}

	// 添加 workflow 并获取（传 session）
	wfCard := schema.NewWorkflowCard(schema.WithID("dg-wf-1"), schema.WithName("获取Workflow"))
	_ = mgr.AddWorkflow(wfCard, stubWorkflowProvider())
	session := &stubTracerSession{}
	result, err = mgr.dispatchGet(ctx, "workflow", "dg-wf-1", session)
	if err != nil {
		t.Fatalf("dispatchGet workflow 失败: %v", err)
	}
	if result == nil {
		t.Fatal("dispatchGet workflow 结果不应为 nil")
	}

	// 添加 model 并获取（传 session）
	_ = mgr.AddModel("dg-model-1", stubModelProvider())
	result, err = mgr.dispatchGet(ctx, "model", "dg-model-1", session)
	if err != nil {
		t.Fatalf("dispatchGet model 失败: %v", err)
	}
	if result == nil {
		t.Fatal("dispatchGet model 结果不应为 nil")
	}

	// 添加 prompt 并获取
	tmpl := prompt.NewPromptTemplate("获取模板", "hello")
	_ = mgr.AddPrompt("dg-prompt-1", tmpl)
	result, err = mgr.dispatchGet(ctx, "prompt", "dg-prompt-1", nil)
	if err != nil {
		t.Fatalf("dispatchGet prompt 失败: %v", err)
	}
	if result == nil {
		t.Fatal("dispatchGet prompt 结果不应为 nil")
	}

	// 添加 tool 并获取（传 session）
	toolCard := tool.NewToolCard("dg-tool-1", "获取工具", nil, nil)
	stubTool := &rmStubTool{card: toolCard}
	_ = mgr.AddTool(stubTool)
	result, err = mgr.dispatchGet(ctx, "tool", toolCard.ID, session)
	if err != nil {
		t.Fatalf("dispatchGet tool 失败: %v", err)
	}
	if result == nil {
		t.Fatal("dispatchGet tool 结果不应为 nil")
	}

	// 不支持的类型应报错
	_, err = mgr.dispatchGet(ctx, "unknown", "nonexistent", nil)
	if err == nil {
		t.Fatal("dispatchGet 未知类型应报错")
	}
}

// ──────────────────────────── 内部核心流转方法测试 ────────────────────────────

// TestInnerAddResource_正常 测试 innerAddResource 正常添加流程
func TestInnerAddResource_正常(t *testing.T) {
	mgr := newTestResourceMgr()

	agentProvider := stubAgentProvider()
	agentCard := &schema.BaseCard{ID: "iar-agent-1", Name: "内部添加Agent"}
	err := mgr.innerAddResource("iar-agent-1", "agent", agentProvider, agentCard, "custom-tag", "")
	if err != nil {
		t.Fatalf("innerAddResource 失败: %v", err)
	}

	// 验证 idToCard 缓存
	if cached := mgr.idToCard.Get("iar-agent-1"); cached == nil {
		t.Fatal("idToCard 中未找到缓存")
	}

	// 验证 tag 标记
	if !mgr.tagMgr.HasResourceTag("iar-agent-1", "custom-tag") {
		t.Fatal("应标记 custom-tag 标签")
	}
}

// TestInnerAddResource_重复添加报错 测试 innerAddResource 重复添加同一资源返回错误
func TestInnerAddResource_重复添加报错(t *testing.T) {
	mgr := newTestResourceMgr()

	agentProvider := stubAgentProvider()
	agentCard := &schema.BaseCard{ID: "iar-dup-1", Name: "重复Agent"}
	_ = mgr.innerAddResource("iar-dup-1", "agent", agentProvider, agentCard, "", "")

	// 重复添加应报错
	err := mgr.innerAddResource("iar-dup-1", "agent", agentProvider, agentCard, "", "")
	if err == nil {
		t.Fatal("innerAddResource 重复添加应返回错误")
	}
}

// TestInnerAddResource_默认TagGlobal 测试 innerAddResource 空 tag 时默认标记 TagGlobal
func TestInnerAddResource_默认TagGlobal(t *testing.T) {
	mgr := newTestResourceMgr()

	modelProvider := stubModelProvider()
	modelCard := &schema.BaseCard{ID: "iar-model-g", Name: "默认Tag"}
	err := mgr.innerAddResource("iar-model-g", "model", modelProvider, modelCard, "", "")
	if err != nil {
		t.Fatalf("innerAddResource 失败: %v", err)
	}

	// 空 tag 应默认标记 TagGlobal
	if !mgr.tagMgr.HasResourceTag("iar-model-g", TagGlobal) {
		t.Fatal("空 tag 应默认标记 TagGlobal")
	}
}

// TestInnerAddResource_缓存Card为nil时不缓存 测试 innerAddResource 传入 nil card 时不缓存到 idToCard
func TestInnerAddResource_缓存Card为nil时不缓存(t *testing.T) {
	mgr := newTestResourceMgr()

	modelProvider := stubModelProvider()
	err := mgr.innerAddResource("iar-nil-card", "model", modelProvider, nil, "", "")
	if err != nil {
		t.Fatalf("innerAddResource 失败: %v", err)
	}

	// nil card 时不应缓存
	if mgr.idToCard.Get("iar-nil-card") != nil {
		t.Fatal("nil card 时不应缓存到 idToCard")
	}
}

// TestInnerRemoveResources_按ID移除 测试 innerRemoveResources 按 ID 列表移除
func TestInnerRemoveResources_按ID移除(t *testing.T) {
	mgr := newTestResourceMgr()

	// 先添加
	agentCard := agentschema.NewAgentCard(schema.WithID("irr-agent-1"), schema.WithName("移除Agent"))
	_ = mgr.AddAgent(agentCard, stubAgentProvider())

	// 按 ID 移除
	results, err := mgr.innerRemoveResources([]string{"irr-agent-1"}, "agent", "", TagMatchAll, false)
	if err != nil {
		t.Fatalf("innerRemoveResources 失败: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("期望移除 1 个，实际 %d", len(results))
	}

	// 验证缓存已清理
	if mgr.idToCard.Get("irr-agent-1") != nil {
		t.Fatal("idToCard 应已清理")
	}
}

// TestInnerRemoveResources_按Tag移除 测试 innerRemoveResources 按 tag 移除
func TestInnerRemoveResources_按Tag移除(t *testing.T) {
	mgr := newTestResourceMgr()

	// 添加带自定义 tag 的 agent
	agentCard := agentschema.NewAgentCard(schema.WithID("irr-tag-1"), schema.WithName("TagAgent"))
	_ = mgr.AddAgent(agentCard, stubAgentProvider(), WithTag("rm-tag"))

	// 按 tag 积除（空 ID 列表触发 tag 查找）
	results, err := mgr.innerRemoveResources(nil, "agent", "rm-tag", TagMatchAll, false)
	if err != nil {
		t.Fatalf("innerRemoveResources 按 tag 失败: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("期望移除 1 个，实际 %d", len(results))
	}

	// 验证缓存已清理
	if mgr.idToCard.Get("irr-tag-1") != nil {
		t.Fatal("idToCard 应已清理")
	}
}

// TestInnerRemoveResources_idReturnTypes返回ID 测试 innerRemoveResources 对 idReturnTypes 类型返回 ID 而非 Card
func TestInnerRemoveResources_idReturnTypes返回ID(t *testing.T) {
	mgr := newTestResourceMgr()

	// 添加 tool（idReturnTypes 中）
	toolCard := tool.NewToolCard("irr-tool-1", "ID返回工具", nil, nil)
	stubTool := &rmStubTool{card: toolCard}
	_ = mgr.AddTool(stubTool)

	// 积除 tool，结果应为 ID 字符串
	results, err := mgr.innerRemoveResources([]string{toolCard.ID}, "tool", "", TagMatchAll, false)
	if err != nil {
		t.Fatalf("innerRemoveResources 失败: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("期望移除 1 个，实际 %d", len(results))
	}
	// idReturnTypes 返回的是 ID 字符串
	if id, ok := results[0].(string); !ok || id != toolCard.ID {
		t.Fatalf("idReturnTypes 类型应返回 ID 字符串，实际 %v", results[0])
	}
}

// TestInnerRemoveResources_空Tag报错 测试 innerRemoveResources 空 tag 且空 ID 列表时报错
func TestInnerRemoveResources_空Tag报错(t *testing.T) {
	mgr := newTestResourceMgr()

	// 空 ID 列表 + 空 tag → innerValidateTag 报错
	_, err := mgr.innerRemoveResources(nil, "agent", "", TagMatchAll, false)
	if err == nil {
		t.Fatal("空 tag 且空 ID 列表应返回错误")
	}
}

// TestInnerGetResources_同步获取 测试 innerGetResources 同步获取（tool/prompt/sys_operation）
func TestInnerGetResources_同步获取(t *testing.T) {
	mgr := newTestResourceMgr()
	ctx := context.Background()

	// 添加 tool 并获取
	toolCard := tool.NewToolCard("igr-tool-1", "获取工具", nil, nil)
	stubTool := &rmStubTool{card: toolCard}
	_ = mgr.AddTool(stubTool)

	results, err := mgr.innerGetResources(ctx, toolCard.ID, "tool", "", TagMatchAll, nil)
	if err != nil {
		t.Fatalf("innerGetResources tool 失败: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("期望 1 个 tool，实际 %d", len(results))
	}

	// 添加 prompt 并获取
	tmpl := prompt.NewPromptTemplate("获取模板", "hello")
	_ = mgr.AddPrompt("igr-prompt-1", tmpl)

	results, err = mgr.innerGetResources(ctx, "igr-prompt-1", "prompt", "", TagMatchAll, nil)
	if err != nil {
		t.Fatalf("innerGetResources prompt 失败: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("期望 1 个 prompt，实际 %d", len(results))
	}

	// 获取不存在的资源
	results, err = mgr.innerGetResources(ctx, "nonexistent", "tool", "", TagMatchAll, nil)
	if err != nil {
		t.Fatalf("innerGetResources 不存在资源不应报错: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("期望 0 个，实际 %d", len(results))
	}
}

// TestInnerGetResourcesByProvider_通过Provider获取 测试 innerGetResourcesByProvider 通过 provider 获取
func TestInnerGetResourcesByProvider_通过Provider获取(t *testing.T) {
	mgr := newTestResourceMgr()
	ctx := context.Background()

	// 添加 agent 并通过 provider 获取
	agentCard := agentschema.NewAgentCard(schema.WithID("igrp-agent-1"), schema.WithName("ProviderAgent"))
	_ = mgr.AddAgent(agentCard, stubAgentProvider())

	results, err := mgr.innerGetResourcesByProvider(ctx, "igrp-agent-1", "agent", "", TagMatchAll, nil)
	if err != nil {
		t.Fatalf("innerGetResourcesByProvider agent 失败: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("期望 1 个 agent，实际 %d", len(results))
	}

	// 添加 workflow 并通过 provider 获取
	wfCard := schema.NewWorkflowCard(schema.WithID("igrp-wf-1"), schema.WithName("ProviderWorkflow"))
	_ = mgr.AddWorkflow(wfCard, stubWorkflowProvider())

	results, err = mgr.innerGetResourcesByProvider(ctx, "igrp-wf-1", "workflow", "", TagMatchAll, nil)
	if err != nil {
		t.Fatalf("innerGetResourcesByProvider workflow 失败: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("期望 1 个 workflow，实际 %d", len(results))
	}

	// 添加 model 并通过 provider 获取
	_ = mgr.AddModel("igrp-model-1", stubModelProvider())

	results, err = mgr.innerGetResourcesByProvider(ctx, "igrp-model-1", "model", "", TagMatchAll, nil)
	if err != nil {
		t.Fatalf("innerGetResourcesByProvider model 失败: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("期望 1 个 model，实际 %d", len(results))
	}

	// 获取不存在的资源
	results, err = mgr.innerGetResourcesByProvider(ctx, "nonexistent", "agent", "", TagMatchAll, nil)
	if err != nil {
		t.Fatalf("innerGetResourcesByProvider 不存在资源不应报错: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("期望 0 个，实际 %d", len(results))
	}
}

// ──────────────────────────── 验证方法测试 ────────────────────────────

// TestInnerValidateTag_空值报错 测试 innerValidateTag 空 tag 报错
func TestInnerValidateTag_空值报错(t *testing.T) {
	err := innerValidateTag("")
	if err == nil {
		t.Fatal("空 tag 应返回错误")
	}
}

// TestInnerValidateTag_正常值 测试 innerValidateTag 正常值不报错
func TestInnerValidateTag_正常值(t *testing.T) {
	if err := innerValidateTag(TagGlobal); err != nil {
		t.Fatalf("TagGlobal 不应报错: %v", err)
	}
	if err := innerValidateTag("custom-tag"); err != nil {
		t.Fatalf("自定义 tag 不应报错: %v", err)
	}
}

// TestInnerValidateResourceCard_Card类型不匹配 测试 innerValidateResourceCard Card 类型不匹配报错
func TestInnerValidateResourceCard_Card类型不匹配(t *testing.T) {
	// BaseCard 类型与 AgentCard 类型不匹配
	baseCard := &schema.BaseCard{ID: "test", Name: "测试"}
	agentCardType := reflect.TypeOf((*agentschema.AgentCard)(nil))
	err := innerValidateResourceCard(baseCard, "agent", agentCardType)
	if err == nil {
		t.Fatal("BaseCard 与 AgentCard 类型不匹配应报错")
	}
}

// TestInnerValidateResourceCard_Card为nil 测试 innerValidateResourceCard nil card 报错
func TestInnerValidateResourceCard_Card为nil(t *testing.T) {
	agentCardType := reflect.TypeOf((*agentschema.AgentCard)(nil))
	err := innerValidateResourceCard(nil, "agent", agentCardType)
	if err == nil {
		t.Fatal("nil card 应返回错误")
	}
}

// TestInnerValidateResourceCard_类型匹配 测试 innerValidateResourceCard 类型匹配正常
func TestInnerValidateResourceCard_类型匹配(t *testing.T) {
	// innerValidateResourceCard 接收 *schema.BaseCard，cardClassType 也用 BaseCard 类型时匹配
	baseCard := &schema.BaseCard{ID: "test", Name: "测试"}
	baseCardType := reflect.TypeOf((*schema.BaseCard)(nil))
	err := innerValidateResourceCard(baseCard, "model", baseCardType)
	if err != nil {
		t.Fatalf("类型匹配不应报错: %v", err)
	}
}

// TestInnerValidateResourceIDs_空列表报错 测试 innerValidateResourceIDs 空列表报错
func TestInnerValidateResourceIDs_空列表报错(t *testing.T) {
	err := innerValidateResourceIDs([]string{}, "agent")
	if err == nil {
		t.Fatal("空 ID 列表应返回错误")
	}
}

// TestInnerValidateResourceIDs_重复ID报错 测试 innerValidateResourceIDs 重复 ID 报错
func TestInnerValidateResourceIDs_重复ID报错(t *testing.T) {
	err := innerValidateResourceIDs([]string{"id-1", "id-1"}, "agent")
	if err == nil {
		t.Fatal("重复 ID 应返回错误")
	}
}

// TestInnerValidateResourceIDs_空ID报错 测试 innerValidateResourceIDs 列表中含空 ID 报错
func TestInnerValidateResourceIDs_空ID报错(t *testing.T) {
	err := innerValidateResourceIDs([]string{"id-1", ""}, "agent")
	if err == nil {
		t.Fatal("含空 ID 应返回错误")
	}
}

// TestInnerValidateResourceIDs_纯空白ID报错 测试 innerValidateResourceIDs 列表中含纯空白 ID 报错
func TestInnerValidateResourceIDs_纯空白ID报错(t *testing.T) {
	err := innerValidateResourceIDs([]string{"id-1", "   "}, "agent")
	if err == nil {
		t.Fatal("含纯空白 ID 应返回错误")
	}
}

// TestInnerValidateResourceIDs_正常 测试 innerValidateResourceIDs 正常列表不报错
func TestInnerValidateResourceIDs_正常(t *testing.T) {
	err := innerValidateResourceIDs([]string{"id-1", "id-2"}, "agent")
	if err != nil {
		t.Fatalf("正常 ID 列表不应报错: %v", err)
	}
}

// TestInnerValidateProviders_空列表报错 测试 innerValidateProviders 空列表报错
func TestInnerValidateProviders_空列表报错(t *testing.T) {
	err := innerValidateProviders([]any{}, "agent", nil)
	if err == nil {
		t.Fatal("空 provider 列表应返回错误")
	}
}

// TestInnerValidateProviders_nilProvider报错 测试 innerValidateProviders 列表中含 nil provider 报错
func TestInnerValidateProviders_nilProvider报错(t *testing.T) {
	err := innerValidateProviders([]any{stubAgentProvider(), nil}, "agent", nil)
	if err == nil {
		t.Fatal("含 nil provider 应返回错误")
	}
}

// TestInnerValidateProviders_正常 测试 innerValidateProviders 正常列表不报错
func TestInnerValidateProviders_正常(t *testing.T) {
	err := innerValidateProviders([]any{stubAgentProvider(), stubAgentProvider()}, "agent", nil)
	if err != nil {
		t.Fatalf("正常 provider 列表不应报错: %v", err)
	}
}

// TestInnerValidateProviders_带CardClassType 测试 innerValidateProviders 带 cardClassType 参数
func TestInnerValidateProviders_带CardClassType(t *testing.T) {
	agentCardType := reflect.TypeOf((*agentschema.AgentCard)(nil))
	// nil provider 应报错，错误信息包含类型名
	err := innerValidateProviders([]any{nil}, "agent", agentCardType)
	if err == nil {
		t.Fatal("nil provider 应返回错误")
	}
}

// TestInnerValidateResource_nil实例报错 测试 innerValidateResource nil 实例报错
func TestInnerValidateResource_nil实例报错(t *testing.T) {
	agentType := reflect.TypeOf((*stubBaseAgent)(nil))
	err := innerValidateResource(nil, "agent", agentType)
	if err == nil {
		t.Fatal("nil 实例应返回错误")
	}
}

// TestInnerValidateResource_类型不匹配报错 测试 innerValidateResource 类型不匹配报错
func TestInnerValidateResource_类型不匹配报错(t *testing.T) {
	agentType := reflect.TypeOf((*stubBaseAgent)(nil))
	err := innerValidateResource("not-an-agent", "agent", agentType)
	if err == nil {
		t.Fatal("类型不匹配应返回错误")
	}
}

// TestInnerValidateResource_正常 测试 innerValidateResource 类型匹配正常
func TestInnerValidateResource_正常(t *testing.T) {
	agentType := reflect.TypeOf((*stubBaseAgent)(nil))
	err := innerValidateResource(&stubBaseAgent{}, "agent", agentType)
	if err != nil {
		t.Fatalf("类型匹配不应报错: %v", err)
	}
}

// TestGetCardType_各种Card类型 测试 getCardType 对各种 Card 类型返回正确字符串
func TestGetCardType_各种Card类型(t *testing.T) {
	// nil card → 空字符串
	if got := getCardType(nil); got != "" {
		t.Fatalf("getCardType(nil) 期望空字符串，实际 %s", got)
	}

	// getCardType 接收 *schema.BaseCard，reflect.TypeOf 始终为 *schema.BaseCard
	// 因此 WorkflowCard/AgentCard/McpToolCard 的 BaseCard 提取后均为 BaseCard 类型
	// 这些情况均落入 default 分支返回空字符串

	// 普通 BaseCard → 空字符串
	baseCard := &schema.BaseCard{ID: "base-1"}
	if got := getCardType(baseCard); got != "" {
		t.Fatalf("getCardType(BaseCard) 期望空字符串，实际 %s", got)
	}

	// WorkflowCard 的 BaseCard → 空字符串（BaseCard 类型不匹配 WorkflowCard 类型）
	wfCard := &schema.WorkflowCard{BaseCard: schema.BaseCard{ID: "wf-1"}}
	if got := getCardType(&wfCard.BaseCard); got != "" {
		t.Fatalf("getCardType(WorkflowCard.BaseCard) 期望空字符串，实际 %s", got)
	}

	// AgentCard 的 BaseCard → 空字符串
	agentCard := &agentschema.AgentCard{BaseCard: schema.BaseCard{ID: "agent-1"}}
	if got := getCardType(&agentCard.BaseCard); got != "" {
		t.Fatalf("getCardType(AgentCard.BaseCard) 期望空字符串，实际 %s", got)
	}
}

// TestInnerGetServerIDs_按ServerID 测试 innerGetServerIDs 按 serverID 精确查找
func TestInnerGetServerIDs_按ServerID(t *testing.T) {
	mgr := newTestResourceMgr()

	serverIDs, exactMatch, err := mgr.innerGetServerIDs("srv-1", "", "", TagMatchAll, false, exception.StatusResourceMCPServerParamInvalid)
	if err != nil {
		t.Fatalf("innerGetServerIDs 失败: %v", err)
	}
	if !exactMatch {
		t.Fatal("按 serverID 查找应精确匹配")
	}
	if len(serverIDs) != 1 || serverIDs[0] != "srv-1" {
		t.Fatalf("期望 [srv-1]，实际 %v", serverIDs)
	}
}

// TestInnerGetServerIDs_按ServerName 测试 innerGetServerIDs 按 serverName 查找
func TestInnerGetServerIDs_按ServerName(t *testing.T) {
	mgr := newTestResourceMgr()

	// 先注册 MCP 服务器
	setupMcpServer(mgr, "srv-name-1", "my_server", []string{"tool1"})

	serverIDs, exactMatch, err := mgr.innerGetServerIDs("", "my_server", "", TagMatchAll, false, exception.StatusResourceMCPServerParamInvalid)
	if err != nil {
		t.Fatalf("innerGetServerIDs 失败: %v", err)
	}
	if exactMatch {
		t.Fatal("按 serverName 查找不应精确匹配")
	}
	if len(serverIDs) < 1 {
		t.Fatalf("期望至少 1 个服务器 ID，实际 %d", len(serverIDs))
	}
}

// TestInnerGetServerIDs_按Tag查找 测试 innerGetServerIDs 按 tag 查找
func TestInnerGetServerIDs_按Tag查找(t *testing.T) {
	mgr := newTestResourceMgr()

	// 先注册 MCP 服务器
	setupMcpServer(mgr, "srv-tag-1", "tag_server", []string{"tool1"})

	serverIDs, exactMatch, err := mgr.innerGetServerIDs("", "", "custom-tag", TagMatchAll, true, exception.StatusResourceMCPServerParamInvalid)
	if err != nil {
		t.Fatalf("innerGetServerIDs 失败: %v", err)
	}
	if exactMatch {
		t.Fatal("按 tag 查找不应精确匹配")
	}
	// custom-tag 下可能没有资源（因为默认 tag 是 TagGlobal），结果为空不算错误
	_ = serverIDs
}

// TestInnerGetServerIDs_空参数默认TagGlobal 测试 innerGetServerIDs 空 serverID/serverName 时默认用 TagGlobal
func TestInnerGetServerIDs_空参数默认TagGlobal(t *testing.T) {
	mgr := newTestResourceMgr()

	// 先注册 MCP 服务器
	setupMcpServer(mgr, "srv-default-1", "default_server", []string{"tool1"})

	// 空 serverID、空 serverName、空 tag → 默认 TagGlobal
	serverIDs, exactMatch, err := mgr.innerGetServerIDs("", "", "", TagMatchAll, false, exception.StatusResourceMCPServerParamInvalid)
	if err != nil {
		t.Fatalf("innerGetServerIDs 失败: %v", err)
	}
	if exactMatch {
		t.Fatal("不应精确匹配")
	}
	// TagGlobal 下应有资源
	if len(serverIDs) == 0 {
		t.Fatal("TagGlobal 下应至少有 1 个资源")
	}
}

// ──────────────────────────── resourceCardStr 测试 ────────────────────────────

// TestResourceCardStr_card为nil 测试 resourceCardStr card 为 nil 时回退到 resourceID
func TestResourceCardStr_card为nil(t *testing.T) {
	result := resourceCardStr(nil, "fallback-id")
	if result != "fallback-id" {
		t.Fatalf("期望 fallback-id，实际 %s", result)
	}
}

// TestResourceCardStr_card不为nil 测试 resourceCardStr card 不为 nil 时返回 card.String()
func TestResourceCardStr_card不为nil(t *testing.T) {
	card := &schema.BaseCard{ID: "card-1", Name: "测试"}
	result := resourceCardStr(card, "fallback-id")
	if result == "fallback-id" {
		t.Fatal("card 不为 nil 时不应回退到 resourceID")
	}
}
