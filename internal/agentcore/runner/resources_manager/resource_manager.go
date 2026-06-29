package resources_manager

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/prompt"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/mcp"
	multiagents "github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/tracer/decorator"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ResourceMgr 资源管理器门面，聚合 ResourceRegistry、TagMgr 和 idToCard 三大核心组件，
// 提供统一的资源增删查改入口，是 Runner 依赖的最核心管理类。
//
// 对应 Python: ResourceManager (openjiuwen/core/runner/resources_manager/resource_manager.py)
type ResourceMgr struct {
	// registry 资源注册表，聚合 7 个子管理器
	registry *ResourceRegistry
	// tagMgr 标签管理器，维护资源与标签的双向映射
	tagMgr *TagMgr
	// idToCard 资源 ID → BaseCard 的缓存索引
	idToCard *ThreadSafeDict[string, *schema.BaseCard]
}

// resourceOptions 资源操作选项，通过 ResourceOption 函数式选项模式设置。
//
// 对应 Python: _ResourceOptions (resource_manager.py)
type resourceOptions struct {
	// Tag 资源标签
	Tag Tag
	// TagMatchStrategy 标签匹配策略
	TagMatchStrategy TagMatchStrategy
	// SkipIfTagNotExists 标签不存在时是否跳过
	SkipIfTagNotExists bool
	// Session 追踪会话
	Session decorator.TracerSession
	// Refresh 是否刷新（Tool 专用）
	Refresh bool
	// InterfaceURL 分布式接口 URL（Agent 专用，⤵️ 预留）
	InterfaceURL string
}

// mcpOptions MCP 操作选项，通过 McpOption 函数式选项模式设置。
//
// 对应 Python: _McpOptions (resource_manager.py)
type mcpOptions struct {
	// ServerName MCP 服务器名称
	ServerName string
	// Tag 资源标签
	Tag Tag
	// ExpiryTime 过期时间（秒）
	ExpiryTime float64
	// SkipIfNotExists 服务器不存在时是否跳过
	SkipIfNotExists bool
	// Force 是否强制刷新
	Force bool
	// Session 追踪会话
	Session decorator.TracerSession
}

// tagOptions 标签操作选项，通过 TagOption 函数式选项模式设置。
//
// 对应 Python: _TagOptions (resource_manager.py)
type tagOptions struct {
	// SkipIfNotExists 标签不存在时是否跳过
	SkipIfNotExists bool
}

// ──────────────────────────── 枚举 ────────────────────────────

// ResourceOption 资源操作选项函数。
type ResourceOption func(*resourceOptions)

// McpOption MCP 操作选项函数。
type McpOption func(*mcpOptions)

// TagOption 标签操作选项函数。
type TagOption func(*tagOptions)

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// registryAccessors 资源类型 → 子管理器访问器名称的映射
	//
	// 对应 Python: _REGISTRY_ACCESSORS (resource_manager.py)
	registryAccessors = map[string]string{
		"workflow":      "workflow",
		"agent":         "agent",
		"team":          "agent_team",
		"tool":          "tool",
		"prompt":        "prompt",
		"model":         "model",
		"sys_operation": "sys_operation",
	}
	// asyncGetTypes 需要异步获取的资源类型集合
	//
	// 对应 Python: _ASYNC_GET_TYPES (resource_manager.py)
	asyncGetTypes = map[string]bool{
		"workflow": true,
		"agent":    true,
		"team":     true,
		"model":    true,
	}
	// sessionGetTypes 需要追踪会话的资源类型集合
	//
	// 对应 Python: _SESSION_GET_TYPES (resource_manager.py)
	sessionGetTypes = map[string]bool{
		"workflow": true,
		"model":    true,
		"tool":     true,
	}
	// idReturnTypes 返回 ID 而非实例的资源类型集合
	//
	// 对应 Python: _ID_RETURN_TYPES (resource_manager.py)
	idReturnTypes = map[string]bool{
		"tool":   true,
		"prompt": true,
	}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewResourceMgr 创建资源管理器门面实例，初始化 registry、tagMgr 和 idToCard。
//
// 对应 Python: ResourceManager.__init__()
func NewResourceMgr() *ResourceMgr {
	mgr := &ResourceMgr{
		registry: NewResourceRegistry(),
		tagMgr:   NewTagMgr(),
		idToCard: NewThreadSafeDict[string, *schema.BaseCard](),
	}

	logger.Info(logger.ComponentAgentCore).
		Str("event_type", "RESOURCE_MGR_INIT").
		Msg("资源管理器初始化完成")

	return mgr
}

// --- 函数选项：ResourceOption ---

// WithTag 设置资源标签。
func WithTag(tag Tag) ResourceOption {
	return func(o *resourceOptions) { o.Tag = tag }
}

// WithTagMatchStrategy 设置标签匹配策略。
func WithTagMatchStrategy(strategy TagMatchStrategy) ResourceOption {
	return func(o *resourceOptions) { o.TagMatchStrategy = strategy }
}

// WithSkipIfTagNotExists 设置标签不存在时跳过。
func WithSkipIfTagNotExists() ResourceOption {
	return func(o *resourceOptions) { o.SkipIfTagNotExists = true }
}

// WithSession 设置追踪会话。
func WithSession(session decorator.TracerSession) ResourceOption {
	return func(o *resourceOptions) { o.Session = session }
}

// WithRefresh 设置刷新标记（Tool 专用）。
func WithRefresh() ResourceOption {
	return func(o *resourceOptions) { o.Refresh = true }
}

// WithInterfaceURL 设置分布式接口 URL（Agent 专用，⤵️ 预留）。
func WithInterfaceURL(url string) ResourceOption {
	return func(o *resourceOptions) { o.InterfaceURL = url }
}

// --- 函数选项：McpOption ---

// WithMcpServerName 设置 MCP 服务器名称。
func WithMcpServerName(name string) McpOption {
	return func(o *mcpOptions) { o.ServerName = name }
}

// WithMcpTag 设置 MCP 资源标签。
func WithMcpTag(tag Tag) McpOption {
	return func(o *mcpOptions) { o.Tag = tag }
}

// WithMcpExpiryTime 设置 MCP 过期时间（秒）。
func WithMcpExpiryTime(seconds float64) McpOption {
	return func(o *mcpOptions) { o.ExpiryTime = seconds }
}

// WithMcpSkipIfNotExists 设置 MCP 服务器不存在时跳过。
func WithMcpSkipIfNotExists() McpOption {
	return func(o *mcpOptions) { o.SkipIfNotExists = true }
}

// WithMcpForce 设置 MCP 强制刷新。
func WithMcpForce() McpOption {
	return func(o *mcpOptions) { o.Force = true }
}

// WithMcpSession 设置 MCP 追踪会话。
func WithMcpSession(session decorator.TracerSession) McpOption {
	return func(o *mcpOptions) { o.Session = session }
}

// --- 函数选项：TagOption ---

// WithTagSkipIfNotExists 设置标签不存在时跳过。
func WithTagSkipIfNotExists() TagOption {
	return func(o *tagOptions) { o.SkipIfNotExists = true }
}

// --- Agent 操作 ---

// AddAgent 注册 Agent，将 provider 存入 agentMgr，缓存 card 到 idToCard，标记 tag。
//
// 对应 Python: ResourceManager.add_agent(agent_card, provider, **kwargs)
func (m *ResourceMgr) AddAgent(card *agentschema.AgentCard, provider AgentProvider, opts ...ResourceOption) error {
	o := applyResourceOptions(opts...)
	agentID := card.ID

	if err := m.innerValidateResourceID(agentID, "agent"); err != nil {
		return err
	}
	if err := m.innerValidateProvider(provider, "agent"); err != nil {
		return err
	}

	if err := m.registry.Agent().AddAgent(agentID, provider); err != nil {
		return err
	}

	// 缓存 card 到 idToCard
	baseCard := &schema.BaseCard{ID: card.ID, Name: card.Name, Description: card.Description}
	m.idToCard.Set(agentID, baseCard)

	// 标记 tag
	tag := o.Tag
	if tag == "" {
		tag = TagGlobal
	}
	m.tagMgr.TagResource(agentID, []Tag{tag})

	logger.Info(logger.ComponentAgentCore).
		Str("event_type", "RESOURCE_MGR_ADD_AGENT").
		Str("agent_id", agentID).
		Str("tag", tag).
		Msg("ResourceManager 添加 Agent 成功")
	return nil
}

// AddAgents 批量注册 Agent。
//
// 对应 Python: ResourceManager.add_agents(agents, **kwargs)
func (m *ResourceMgr) AddAgents(agents []AgentEntry, opts ...ResourceOption) error {
	for _, entry := range agents {
		if err := m.AddAgent(entry.Card, entry.Provider, opts...); err != nil {
			logger.Error(logger.ComponentAgentCore).
				Str("event_type", "RESOURCE_MGR_ADD_AGENTS_ERROR").
				Str("agent_id", entry.Card.ID).
				Err(err).
				Msg("批量添加 Agent 失败")
		}
	}
	return nil
}

// RemoveAgent 注销 Agent，返回被注销的 AgentCard 列表。
//
// 对应 Python: ResourceManager.remove_agent(agent_id, **kwargs)
func (m *ResourceMgr) RemoveAgent(agentIDs []string, opts ...ResourceOption) ([]*agentschema.AgentCard, error) {
	o := applyResourceOptions(opts...)
	if err := innerValidateResourceIDs(agentIDs, "agent"); err != nil {
		return nil, err
	}
	resourceIDs, _, err := m.innerFindResourceIDs(agentIDs, "agent", o.Tag, o.TagMatchStrategy)
	if err != nil {
		return nil, err
	}

	removed := make([]*agentschema.AgentCard, 0)
	for _, id := range resourceIDs {
		_, rmErr := m.registry.Agent().RemoveAgent(id)
		if rmErr != nil {
			logger.Error(logger.ComponentAgentCore).
				Str("event_type", "RESOURCE_MGR_REMOVE_AGENT_ERROR").
				Str("agent_id", id).
				Err(rmErr).
				Msg("移除 Agent 失败")
			continue
		}
		// 从 idToCard 获取 card
		if card := m.idToCard.Pop(id); card != nil {
			removed = append(removed, &agentschema.AgentCard{
				BaseCard: schema.BaseCard{ID: card.ID, Name: card.Name, Description: card.Description},
			})
		}
		// 移除标签
		m.tagMgr.RemoveResource(id)
	}

	return removed, nil
}

// GetAgent 获取 Agent 实例列表。
//
// 对应 Python: ResourceManager.get_agent(agent_id, **kwargs)
func (m *ResourceMgr) GetAgent(ctx context.Context, agentIDs []string, opts ...ResourceOption) ([]interfaces.BaseAgent, error) {
	o := applyResourceOptions(opts...)
	if err := innerValidateResourceIDs(agentIDs, "agent"); err != nil {
		return nil, err
	}
	resourceIDs, _, err := m.innerFindResourceIDs(agentIDs, "agent", o.Tag, o.TagMatchStrategy)
	if err != nil {
		return nil, err
	}

	results := make([]interfaces.BaseAgent, 0)
	for _, id := range resourceIDs {
		agent, getErr := m.registry.Agent().GetAgent(ctx, id)
		if getErr != nil {
			logger.Error(logger.ComponentAgentCore).
				Str("event_type", "RESOURCE_MGR_GET_AGENT_ERROR").
				Str("agent_id", id).
				Err(getErr).
				Msg("获取 Agent 失败")
			continue
		}
		results = append(results, agent)
	}

	return results, nil
}

// --- Workflow 操作 ---

// AddWorkflow 注册 Workflow，将 provider 存入 workflowMgr，缓存 card，标记 tag。
//
// 对应 Python: ResourceManager.add_workflow(workflow_card, provider, **kwargs)
func (m *ResourceMgr) AddWorkflow(card *schema.WorkflowCard, provider WorkflowProvider, opts ...ResourceOption) error {
	o := applyResourceOptions(opts...)
	workflowID := card.ID

	if err := m.innerValidateResourceID(workflowID, "workflow"); err != nil {
		return err
	}
	if err := m.innerValidateProvider(provider, "workflow"); err != nil {
		return err
	}

	if err := m.registry.Workflow().AddWorkflow(workflowID, provider); err != nil {
		return err
	}

	baseCard := &schema.BaseCard{ID: card.ID, Name: card.Name, Description: card.Description}
	m.idToCard.Set(workflowID, baseCard)

	tag := o.Tag
	if tag == "" {
		tag = TagGlobal
	}
	m.tagMgr.TagResource(workflowID, []Tag{tag})

	logger.Info(logger.ComponentAgentCore).
		Str("event_type", "RESOURCE_MGR_ADD_WORKFLOW").
		Str("workflow_id", workflowID).
		Str("tag", tag).
		Msg("ResourceManager 添加 Workflow 成功")
	return nil
}

// AddWorkflows 批量注册 Workflow。
//
// 对应 Python: ResourceManager.add_workflows(workflows, **kwargs)
func (m *ResourceMgr) AddWorkflows(workflows []WorkflowEntry, opts ...ResourceOption) error {
	for _, entry := range workflows {
		card := schema.NewWorkflowCard(schema.WithID(entry.ID))
		if err := m.AddWorkflow(card, entry.Provider, opts...); err != nil {
			logger.Error(logger.ComponentAgentCore).
				Str("event_type", "RESOURCE_MGR_ADD_WORKFLOWS_ERROR").
				Str("workflow_id", entry.ID).
				Err(err).
				Msg("批量添加 Workflow 失败")
		}
	}
	return nil
}

// RemoveWorkflow 注销 Workflow，返回被注销的 WorkflowCard 列表。
//
// 对应 Python: ResourceManager.remove_workflow(workflow_id, **kwargs)
func (m *ResourceMgr) RemoveWorkflow(workflowIDs []string, opts ...ResourceOption) ([]*schema.WorkflowCard, error) {
	o := applyResourceOptions(opts...)
	if err := innerValidateResourceIDs(workflowIDs, "workflow"); err != nil {
		return nil, err
	}
	resourceIDs, _, err := m.innerFindResourceIDs(workflowIDs, "workflow", o.Tag, o.TagMatchStrategy)
	if err != nil {
		return nil, err
	}

	removed := make([]*schema.WorkflowCard, 0)
	for _, id := range resourceIDs {
		_, rmErr := m.registry.Workflow().RemoveWorkflow(id)
		if rmErr != nil {
			logger.Error(logger.ComponentAgentCore).
				Str("event_type", "RESOURCE_MGR_REMOVE_WORKFLOW_ERROR").
				Str("workflow_id", id).
				Err(rmErr).
				Msg("移除 Workflow 失败")
			continue
		}
		if card := m.idToCard.Pop(id); card != nil {
			removed = append(removed, &schema.WorkflowCard{
				BaseCard: schema.BaseCard{ID: card.ID, Name: card.Name, Description: card.Description},
			})
		}
		m.tagMgr.RemoveResource(id)
	}

	return removed, nil
}

// GetWorkflow 获取 Workflow 实例列表。
//
// 对应 Python: ResourceManager.get_workflow(workflow_id, **kwargs)
func (m *ResourceMgr) GetWorkflow(ctx context.Context, workflowIDs []string, opts ...ResourceOption) ([]interfaces.Workflow, error) {
	o := applyResourceOptions(opts...)
	if err := innerValidateResourceIDs(workflowIDs, "workflow"); err != nil {
		return nil, err
	}
	resourceIDs, _, err := m.innerFindResourceIDs(workflowIDs, "workflow", o.Tag, o.TagMatchStrategy)
	if err != nil {
		return nil, err
	}

	results := make([]interfaces.Workflow, 0)
	for _, id := range resourceIDs {
		wf, getErr := m.registry.Workflow().GetWorkflow(ctx, id, o.Session)
		if getErr != nil {
			logger.Error(logger.ComponentAgentCore).
				Str("event_type", "RESOURCE_MGR_GET_WORKFLOW_ERROR").
				Str("workflow_id", id).
				Err(getErr).
				Msg("获取 Workflow 失败")
			continue
		}
		results = append(results, wf)
	}

	return results, nil
}

// --- Tool 操作 ---

// AddTool 注册 Tool，缓存 card，标记 tag。
//
// 对应 Python: ResourceManager.add_tool(tool, **kwargs)
func (m *ResourceMgr) AddTool(t tool.Tool, opts ...ResourceOption) error {
	o := applyResourceOptions(opts...)
	toolCard := t.Card()
	toolID := toolCard.ID

	if err := m.innerValidateResourceID(toolID, "tool"); err != nil {
		return err
	}

	// ⤵️ 预留：WithRefresh() 时先 RemoveTool 再 AddTool
	if o.Refresh {
		_, _ = m.registry.Tool().RemoveTool(toolID)
	}

	if err := m.registry.Tool().AddTool(toolID, t); err != nil {
		return err
	}

	baseCard := &schema.BaseCard{ID: toolCard.ID, Name: toolCard.Name, Description: toolCard.Description}
	m.idToCard.Set(toolID, baseCard)

	tag := o.Tag
	if tag == "" {
		tag = TagGlobal
	}
	m.tagMgr.TagResource(toolID, []Tag{tag})

	logger.Info(logger.ComponentAgentCore).
		Str("event_type", "RESOURCE_MGR_ADD_TOOL").
		Str("tool_id", toolID).
		Str("tag", tag).
		Bool("refresh", o.Refresh).
		Msg("ResourceManager 添加 Tool 成功")
	return nil
}

// GetTool 获取 Tool 实例列表。
//
// 对应 Python: ResourceManager.get_tool(tool_id, **kwargs)
func (m *ResourceMgr) GetTool(toolIDs []string, opts ...ResourceOption) ([]tool.Tool, error) {
	o := applyResourceOptions(opts...)
	if err := innerValidateResourceIDs(toolIDs, "tool"); err != nil {
		return nil, err
	}
	resourceIDs, _, err := m.innerFindResourceIDs(toolIDs, "tool", o.Tag, o.TagMatchStrategy)
	if err != nil {
		return nil, err
	}

	results := make([]tool.Tool, 0)
	for _, id := range resourceIDs {
		t, getErr := m.registry.Tool().GetTool(id, o.Session)
		if getErr != nil {
			logger.Error(logger.ComponentAgentCore).
				Str("event_type", "RESOURCE_MGR_GET_TOOL_ERROR").
				Str("tool_id", id).
				Err(getErr).
				Msg("获取 Tool 失败")
			continue
		}
		results = append(results, t)
	}

	return results, nil
}

// RemoveTool 注销 Tool，返回被注销的工具 ID 列表。
//
// 对应 Python: ResourceManager.remove_tool(tool_id, **kwargs)
func (m *ResourceMgr) RemoveTool(toolIDs []string, opts ...ResourceOption) ([]string, error) {
	o := applyResourceOptions(opts...)
	if err := innerValidateResourceIDs(toolIDs, "tool"); err != nil {
		return nil, err
	}
	resourceIDs, _, err := m.innerFindResourceIDs(toolIDs, "tool", o.Tag, o.TagMatchStrategy)
	if err != nil {
		return nil, err
	}

	removed := make([]string, 0)
	for _, id := range resourceIDs {
		_, rmErr := m.registry.Tool().RemoveTool(id)
		if rmErr != nil {
			logger.Error(logger.ComponentAgentCore).
				Str("event_type", "RESOURCE_MGR_REMOVE_TOOL_ERROR").
				Str("tool_id", id).
				Err(rmErr).
				Msg("移除 Tool 失败")
			continue
		}
		m.idToCard.Pop(id)
		m.tagMgr.RemoveResource(id)
		removed = append(removed, id)
	}

	return removed, nil
}

// --- Model 操作 ---

// AddModel 注册 Model，将 provider 存入 modelMgr，缓存 card，标记 tag。
//
// 对应 Python: ResourceManager.add_model(model_id, provider, **kwargs)
func (m *ResourceMgr) AddModel(modelID string, provider ModelProvider, opts ...ResourceOption) error {
	o := applyResourceOptions(opts...)

	if err := m.innerValidateResourceID(modelID, "model"); err != nil {
		return err
	}
	if err := m.innerValidateProvider(provider, "model"); err != nil {
		return err
	}

	if err := m.registry.Model().AddModel(modelID, provider); err != nil {
		return err
	}

	baseCard := &schema.BaseCard{ID: modelID, Name: modelID}
	m.idToCard.Set(modelID, baseCard)

	tag := o.Tag
	if tag == "" {
		tag = TagGlobal
	}
	m.tagMgr.TagResource(modelID, []Tag{tag})

	logger.Info(logger.ComponentAgentCore).
		Str("event_type", "RESOURCE_MGR_ADD_MODEL").
		Str("model_id", modelID).
		Str("tag", tag).
		Msg("ResourceManager 添加 Model 成功")
	return nil
}

// AddModels 批量注册 Model。
//
// 对应 Python: ResourceManager.add_models(models, **kwargs)
func (m *ResourceMgr) AddModels(models []ModelEntry, opts ...ResourceOption) error {
	for _, entry := range models {
		if err := m.AddModel(entry.ID, entry.Provider, opts...); err != nil {
			logger.Error(logger.ComponentAgentCore).
				Str("event_type", "RESOURCE_MGR_ADD_MODELS_ERROR").
				Str("model_id", entry.ID).
				Err(err).
				Msg("批量添加 Model 失败")
		}
	}
	return nil
}

// RemoveModel 注销 Model，返回被注销的模型 ID 列表。
//
// 对应 Python: ResourceManager.remove_model(model_id, **kwargs)
func (m *ResourceMgr) RemoveModel(modelIDs []string, opts ...ResourceOption) ([]string, error) {
	o := applyResourceOptions(opts...)
	if err := innerValidateResourceIDs(modelIDs, "model"); err != nil {
		return nil, err
	}
	resourceIDs, _, err := m.innerFindResourceIDs(modelIDs, "model", o.Tag, o.TagMatchStrategy)
	if err != nil {
		return nil, err
	}

	removed := make([]string, 0)
	for _, id := range resourceIDs {
		_, rmErr := m.registry.Model().RemoveModel(id)
		if rmErr != nil {
			logger.Error(logger.ComponentAgentCore).
				Str("event_type", "RESOURCE_MGR_REMOVE_MODEL_ERROR").
				Str("model_id", id).
				Err(rmErr).
				Msg("移除 Model 失败")
			continue
		}
		m.idToCard.Pop(id)
		m.tagMgr.RemoveResource(id)
		removed = append(removed, id)
	}

	return removed, nil
}

// GetModel 获取 Model 实例列表。
//
// 对应 Python: ResourceManager.get_model(model_id, **kwargs)
func (m *ResourceMgr) GetModel(ctx context.Context, modelIDs []string, opts ...ResourceOption) ([]model_clients.BaseModelClient, error) {
	o := applyResourceOptions(opts...)
	if err := innerValidateResourceIDs(modelIDs, "model"); err != nil {
		return nil, err
	}
	resourceIDs, _, err := m.innerFindResourceIDs(modelIDs, "model", o.Tag, o.TagMatchStrategy)
	if err != nil {
		return nil, err
	}

	results := make([]model_clients.BaseModelClient, 0)
	for _, id := range resourceIDs {
		model, getErr := m.registry.Model().GetModel(ctx, id, o.Session)
		if getErr != nil {
			logger.Error(logger.ComponentAgentCore).
				Str("event_type", "RESOURCE_MGR_GET_MODEL_ERROR").
				Str("model_id", id).
				Err(getErr).
				Msg("获取 Model 失败")
			continue
		}
		results = append(results, model)
	}

	return results, nil
}

// --- Prompt 操作 ---

// AddPrompt 注册 Prompt，缓存 card，标记 tag。
//
// 对应 Python: ResourceManager.add_prompt(prompt_id, template, **kwargs)
func (m *ResourceMgr) AddPrompt(promptID string, template *prompt.PromptTemplate, opts ...ResourceOption) error {
	o := applyResourceOptions(opts...)

	if err := m.innerValidateResourceID(promptID, "prompt"); err != nil {
		return err
	}

	if err := m.registry.Prompt().AddPrompt(promptID, template); err != nil {
		return err
	}

	baseCard := &schema.BaseCard{ID: promptID, Name: promptID}
	m.idToCard.Set(promptID, baseCard)

	tag := o.Tag
	if tag == "" {
		tag = TagGlobal
	}
	m.tagMgr.TagResource(promptID, []Tag{tag})

	logger.Info(logger.ComponentAgentCore).
		Str("event_type", "RESOURCE_MGR_ADD_PROMPT").
		Str("prompt_id", promptID).
		Str("tag", tag).
		Msg("ResourceManager 添加 Prompt 成功")
	return nil
}

// AddPrompts 批量注册 Prompt。
//
// 对应 Python: ResourceManager.add_prompts(prompts, **kwargs)
func (m *ResourceMgr) AddPrompts(prompts []PromptEntry, opts ...ResourceOption) error {
	for _, entry := range prompts {
		if err := m.AddPrompt(entry.ID, entry.Template, opts...); err != nil {
			logger.Error(logger.ComponentAgentCore).
				Str("event_type", "RESOURCE_MGR_ADD_PROMPTS_ERROR").
				Str("prompt_id", entry.ID).
				Err(err).
				Msg("批量添加 Prompt 失败")
		}
	}
	return nil
}

// RemovePrompt 注销 Prompt，返回被注销的 ID 列表。
//
// 对应 Python: ResourceManager.remove_prompt(prompt_id, **kwargs)
func (m *ResourceMgr) RemovePrompt(promptIDs []string, opts ...ResourceOption) ([]string, error) {
	o := applyResourceOptions(opts...)
	if err := innerValidateResourceIDs(promptIDs, "prompt"); err != nil {
		return nil, err
	}
	resourceIDs, _, err := m.innerFindResourceIDs(promptIDs, "prompt", o.Tag, o.TagMatchStrategy)
	if err != nil {
		return nil, err
	}

	removed := make([]string, 0)
	for _, id := range resourceIDs {
		_, rmErr := m.registry.Prompt().RemovePrompt(id)
		if rmErr != nil {
			logger.Error(logger.ComponentAgentCore).
				Str("event_type", "RESOURCE_MGR_REMOVE_PROMPT_ERROR").
				Str("prompt_id", id).
				Err(rmErr).
				Msg("移除 Prompt 失败")
			continue
		}
		m.idToCard.Pop(id)
		m.tagMgr.RemoveResource(id)
		removed = append(removed, id)
	}

	return removed, nil
}

// GetPrompt 获取 Prompt 模板列表。
//
// 对应 Python: ResourceManager.get_prompt(prompt_id, **kwargs)
func (m *ResourceMgr) GetPrompt(promptIDs []string, opts ...ResourceOption) ([]*prompt.PromptTemplate, error) {
	o := applyResourceOptions(opts...)
	if err := innerValidateResourceIDs(promptIDs, "prompt"); err != nil {
		return nil, err
	}
	resourceIDs, _, err := m.innerFindResourceIDs(promptIDs, "prompt", o.Tag, o.TagMatchStrategy)
	if err != nil {
		return nil, err
	}

	results := make([]*prompt.PromptTemplate, 0)
	for _, id := range resourceIDs {
		tmpl, getErr := m.registry.Prompt().GetPrompt(id)
		if getErr != nil {
			logger.Error(logger.ComponentAgentCore).
				Str("event_type", "RESOURCE_MGR_GET_PROMPT_ERROR").
				Str("prompt_id", id).
				Err(getErr).
				Msg("获取 Prompt 失败")
			continue
		}
		results = append(results, tmpl)
	}

	return results, nil
}

// --- SysOperation 操作（部分 ⤵️ 预留）---

// AddSysOperation 注册系统操作，基础部分+工具注册⤵️。
//
// 对应 Python: ResourceManager.add_sys_operation(sys_operation_id, instance, **kwargs)
// AddSysOperation 添加系统操作。
//
// ⤵️ 预留：9.32 实现后补充 registerSysOperationTools 调用。
// Python 逻辑：add 成功后自动调用 _register_sys_operation_tools 将操作方法注册为工具。
// 当前缺少该步骤，等 SysOperationToolAdapter 实现后回填。
//
// 对应 Python: ResourceManager.add_sys_operation(card, *, tag=None)
func (m *ResourceMgr) AddSysOperation(sysOperationID string, instance any, opts ...ResourceOption) error {
	o := applyResourceOptions(opts...)

	if err := m.innerValidateResourceID(sysOperationID, "sys_operation"); err != nil {
		return err
	}

	if err := m.registry.SysOperation().AddSysOperation(sysOperationID, instance); err != nil {
		return err
	}

	baseCard := &schema.BaseCard{ID: sysOperationID, Name: sysOperationID}
	m.idToCard.Set(sysOperationID, baseCard)

	tag := o.Tag
	if tag == "" {
		tag = TagGlobal
	}
	m.tagMgr.TagResource(sysOperationID, []Tag{tag})

	logger.Info(logger.ComponentAgentCore).
		Str("event_type", "RESOURCE_MGR_ADD_SYS_OPERATION").
		Str("sys_operation_id", sysOperationID).
		Str("tag", tag).
		Msg("ResourceManager 添加 SysOperation 成功")
	return nil
}

// RemoveSysOperation 注销系统操作。
//
// ⤵️ 预留：9.32 实现后补充关联工具清理逻辑。
// Python 逻辑：remove 后调用 tool.remove_sys_operation_tools(op_id) 获取关联工具 ID，
// 再调用 _inner_remove_resources(tool_ids, resource_type="tool") 清理关联工具。
// 当前缺少该步骤，等 SysOperationToolAdapter 实现后回填。
//
// 对应 Python: ResourceManager.remove_sys_operation(sys_operation_id, **kwargs)
func (m *ResourceMgr) RemoveSysOperation(sysOperationIDs []string, opts ...ResourceOption) error {
	o := applyResourceOptions(opts...)
	if err := innerValidateResourceIDs(sysOperationIDs, "sys_operation"); err != nil {
		return err
	}
	resourceIDs, _, err := m.innerFindResourceIDs(sysOperationIDs, "sys_operation", o.Tag, o.TagMatchStrategy)
	if err != nil {
		return err
	}

	for _, id := range resourceIDs {
		_, rmErr := m.registry.SysOperation().RemoveSysOperation(id)
		if rmErr != nil {
			logger.Error(logger.ComponentAgentCore).
				Str("event_type", "RESOURCE_MGR_REMOVE_SYS_OPERATION_ERROR").
				Str("sys_operation_id", id).
				Err(rmErr).
				Msg("移除 SysOperation 失败")
			continue
		}
		m.idToCard.Pop(id)
		m.tagMgr.RemoveResource(id)
	}

	return nil
}

// GetSysOperation 获取系统操作实例列表。
//
// 对应 Python: ResourceManager.get_sys_operation(sys_operation_id, **kwargs)
func (m *ResourceMgr) GetSysOperation(sysOperationIDs []string, opts ...ResourceOption) ([]any, error) {
	o := applyResourceOptions(opts...)
	if err := innerValidateResourceIDs(sysOperationIDs, "sys_operation"); err != nil {
		return nil, err
	}
	resourceIDs, _, err := m.innerFindResourceIDs(sysOperationIDs, "sys_operation", o.Tag, o.TagMatchStrategy)
	if err != nil {
		return nil, err
	}

	results := make([]any, 0)
	for _, id := range resourceIDs {
		instance, getErr := m.registry.SysOperation().GetSysOperation(id)
		if getErr != nil {
			logger.Error(logger.ComponentAgentCore).
				Str("event_type", "RESOURCE_MGR_GET_SYS_OPERATION_ERROR").
				Str("sys_operation_id", id).
				Err(getErr).
				Msg("获取 SysOperation 失败")
			continue
		}
		results = append(results, instance)
	}

	return results, nil
}

// registerSysOperationTools 自动注册系统操作的方法为工具。
//
// ⤵️ 预留：9.32 SysOperation 接口实现后回填。回填内容：
//  1. 调用 SysOperationToolAdapter.ExtractTools(card, instance) 提取 (toolID, LocalFunction) 列表
//  2. 对每个工具调用 innerAddResource 注册到 ToolMgr
//  3. 调用 ToolMgr.AddSysOperationTools(card.ID, toolIDs) 维护关联索引
//
// 对应 Python: ResourceManager._register_sys_operation_tools(card, instance, tag=tag)
func (m *ResourceMgr) registerSysOperationTools(_ string, _ any, _ Tag) {
	// ⤵️ 预留：9.32 实现后回填
	// 需要：SysOperationToolAdapter, LocalFunction, OperationRegistry
}

// GetSysOpToolCards 获取系统操作的工具卡片。
//
// ⤵️ 预留：9.32 SysOperation 接口实现后回填。回填内容：
//  1. 获取 SysOperation 实例
//  2. 通过 OperationRegistry 获取支持的 operation_name 列表
//  3. 按 operation_name + tool_name 过滤 idToCard 查找 ToolCard
//  4. 当 operation_name 为列表时不允许同时指定 tool_name
//
// 对应 Python: ResourceManager.get_sys_op_tool_cards(sys_operation_id, operation_name=, tool_name=)
func (m *ResourceMgr) GetSysOpToolCards(_ string, _ string, _ string) ([]*schema.BaseCard, error) {
	// ⤵️ 预留：9.32 实现后回填
	return nil, fmt.Errorf("GetSysOpToolCards not implemented: pending 9.32 SysOperation interface")
}

// --- MCP Server 操作 ---

// AddMcpServer 添加 MCP 工具服务器。
//
// 对应 Python: ResourceManager.add_mcp_server(server_config, **kwargs)
func (m *ResourceMgr) AddMcpServer(ctx context.Context, serverConfig *mcp.McpServerConfig, opts ...McpOption) ([]*mcp.McpToolCard, error) {
	o := applyMcpOptions(opts...)

	if err := m.innerValidateServerConfig(serverConfig); err != nil {
		return nil, err
	}

	var expiryTime *float64
	if o.ExpiryTime > 0 {
		expiryTime = &o.ExpiryTime
	}

	cards, err := m.registry.Tool().AddToolServer(ctx, serverConfig, expiryTime)
	if err != nil {
		return nil, err
	}

	// 为每个工具标记 tag
	tag := o.Tag
	if tag == "" {
		tag = TagGlobal
	}
	for _, card := range cards {
		m.tagMgr.TagResource(card.ID, []Tag{tag})
		baseCard := &schema.BaseCard{ID: card.ID, Name: card.Name, Description: card.Description}
		m.idToCard.Set(card.ID, baseCard)
	}

	logger.Info(logger.ComponentAgentCore).
		Str("event_type", "RESOURCE_MGR_ADD_MCP_SERVER").
		Str("server_id", serverConfig.ServerID).
		Str("server_name", serverConfig.ServerName).
		Int("tool_count", len(cards)).
		Msg("ResourceManager 添加 MCP 服务器成功")
	return cards, nil
}

// RefreshMcpServer 刷新 MCP 工具服务器。
//
// 对应 Python: ResourceManager.refresh_mcp_server(server_id, **kwargs)
func (m *ResourceMgr) RefreshMcpServer(ctx context.Context, serverID string, opts ...McpOption) ([]*mcp.McpToolCard, error) {
	o := applyMcpOptions(opts...)
	return m.registry.Tool().RefreshToolServer(ctx, serverID, o.SkipIfNotExists, o.Force)
}

// RemoveMcpServer 移除 MCP 工具服务器，返回被移除的服务器 ID 列表。
//
// 对应 Python: ResourceManager.remove_mcp_server(server_id, **kwargs)
// Python 返回 Result[str, Exception] | list[Result[str, Exception]]，Go 返回 ([]string, error)
func (m *ResourceMgr) RemoveMcpServer(ctx context.Context, serverID string, opts ...McpOption) ([]string, error) {
	o := applyMcpOptions(opts...)
	toolIDs, err := m.registry.Tool().RemoveToolServer(ctx, serverID, o.SkipIfNotExists)
	if err != nil {
		return nil, err
	}

	// 清理标签和缓存
	for _, id := range toolIDs {
		m.tagMgr.RemoveResource(id)
		m.idToCard.Pop(id)
	}

	// Python 中返回 Ok(mcp_server_id)，Go 中返回 serverID
	return []string{serverID}, nil
}

// GetMcpTool 通过工具名和服务器 ID 获取 MCP 工具。
//
// 对应 Python: ResourceManager.get_mcp_tool(name, server_id, **kwargs)
func (m *ResourceMgr) GetMcpTool(ctx context.Context, name, serverID string, opts ...McpOption) ([]tool.Tool, error) {
	o := applyMcpOptions(opts...)
	t, err := m.registry.Tool().GetMcpTool(ctx, name, serverID, o.Session)
	if err != nil {
		return nil, err
	}
	return []tool.Tool{t}, nil
}

// GetMcpToolInfos 通过工具名和服务器 ID 获取 MCP 工具信息。
//
// 对应 Python: ResourceManager.get_mcp_tool_infos(name, server_id, **kwargs)
func (m *ResourceMgr) GetMcpToolInfos(ctx context.Context, name, serverID string, opts ...McpOption) ([]*schema.ToolInfo, error) {
	tools, err := m.GetMcpTool(ctx, name, serverID, opts...)
	if err != nil {
		return nil, err
	}

	results := make([]*schema.ToolInfo, 0, len(tools))
	for _, t := range tools {
		if info := t.Card().ToolInfo(); info != nil {
			results = append(results, info)
		}
	}
	return results, nil
}

// GetMcpServerConfig 获取 MCP 服务器配置。
//
// 对应 Python: ResourceManager.get_mcp_server_config(server_id)
func (m *ResourceMgr) GetMcpServerConfig(serverID string) (*mcp.McpServerConfig, error) {
	return m.registry.Tool().GetMcpServerConfig(serverID)
}

// GetMcpToolIDs 获取指定服务器下所有工具 ID。
//
// 对应 Python: ResourceManager.get_mcp_tool_ids(server_id)
func (m *ResourceMgr) GetMcpToolIDs(serverID string) []string {
	return m.registry.Tool().GetMcpToolIDs(serverID)
}

// ListMcpResources 列出 MCP 服务器资源。
//
// 对应 Python: ResourceManager.list_mcp_resources(server_id)
// ⤵️ 预留：等 MCP ListResources 实现后回填
func (m *ResourceMgr) ListMcpResources(ctx context.Context, serverID string) ([]any, error) {
	client, err := m.registry.Tool().GetMcpClient(serverID)
	if err != nil {
		return nil, err
	}
	return client.ListResources(ctx)
}

// ReadMcpResource 读取 MCP 服务器资源。
//
// 对应 Python: ResourceManager.read_mcp_resource(server_id, uri)
// ⤵️ 预留：等 MCP ReadResource 实现后回填
func (m *ResourceMgr) ReadMcpResource(ctx context.Context, serverID, uri string) (any, error) {
	client, err := m.registry.Tool().GetMcpClient(serverID)
	if err != nil {
		return nil, err
	}
	return client.ReadResource(ctx, uri)
}

// --- Tag 操作（委托 tagMgr）---

// GetResourceByTag 根据标签获取资源卡片列表。
//
// 对应 Python: ResourceManager.get_resource_by_tag(tag)
func (m *ResourceMgr) GetResourceByTag(tag Tag) []*schema.BaseCard {
	resourceIDs := m.tagMgr.GetTagResources(tag)
	results := make([]*schema.BaseCard, 0, len(resourceIDs))
	for _, id := range resourceIDs {
		if card := m.idToCard.Get(id); card != nil {
			results = append(results, card)
		}
	}
	return results
}

// ListTags 获取所有标签。
//
// 对应 Python: ResourceManager.list_tags()
func (m *ResourceMgr) ListTags() []Tag {
	return m.tagMgr.ListTags()
}

// HasTag 检查标签是否存在。
//
// 对应 Python: ResourceManager.has_tag(tag)
func (m *ResourceMgr) HasTag(tag Tag) bool {
	return m.tagMgr.HasTag(tag)
}

// RemoveTag 完全移除标签及其所有关联资源。
//
// 对应 Python: ResourceManager.remove_tag(tag, **kwargs)
// Python 逻辑：对每个受影响的 resource_id 调用 self._resource_registry.remove_by_id(resource_id)，
// 即标签移除会同时清理注册表中的资源。
func (m *ResourceMgr) RemoveTag(tag Tag, opts ...TagOption) ([]string, error) {
	o := applyTagOptions(opts...)
	removedResourceIDs, err := m.tagMgr.RemoveTag(tag, o.SkipIfNotExists)
	if err != nil {
		return nil, err
	}

	// 同步清理注册表中的资源（与 Python 对齐）
	for _, resourceID := range removedResourceIDs {
		m.registry.RemoveByID(resourceID)
		m.idToCard.Pop(resourceID)
	}

	logger.Info(logger.ComponentAgentCore).
		Str("event_type", "RESOURCE_MGR_REMOVE_TAG").
		Str("tag", string(tag)).
		Strs("removal_resource_ids", removedResourceIDs).
		Msg("移除标签及关联资源成功")

	return removedResourceIDs, nil
}

// UpdateResourceTag 更新资源标签。
//
// 对应 Python: ResourceManager.update_resource_tag(resource_id, tags)
func (m *ResourceMgr) UpdateResourceTag(resourceID string, tags []Tag) ([]Tag, error) {
	return m.tagMgr.UpdateResourceTags(resourceID, tags, TagUpdateReplace)
}

// AddResourceTag 为资源添加标签。
//
// 对应 Python: ResourceManager.add_resource_tag(resource_id, tags)
func (m *ResourceMgr) AddResourceTag(resourceID string, tags []Tag) ([]Tag, error) {
	return m.tagMgr.UpdateResourceTags(resourceID, tags, TagUpdateMerge)
}

// RemoveResourceTag 移除资源的指定标签。
//
// 对应 Python: ResourceManager.remove_resource_tag(resource_id, tags, **kwargs)
func (m *ResourceMgr) RemoveResourceTag(resourceID string, tags []Tag, opts ...TagOption) ([]Tag, error) {
	o := applyTagOptions(opts...)
	return m.tagMgr.RemoveResourceTags(resourceID, tags, o.SkipIfNotExists)
}

// GetResourceTag 获取资源的所有标签。
//
// 对应 Python: ResourceManager.get_resource_tag(resource_id)
func (m *ResourceMgr) GetResourceTag(resourceID string) []Tag {
	return m.tagMgr.GetResourcesTags(resourceID)
}

// ResourceHasTag 检查资源是否拥有指定标签。
//
// 对应 Python: ResourceManager.resource_has_tag(resource_id, tag)
func (m *ResourceMgr) ResourceHasTag(resourceID string, tag Tag) bool {
	return m.tagMgr.HasResourceTag(resourceID, tag)
}

// --- ToolInfo 操作 ---

// GetToolInfos 获取工具描述信息列表，支持按 toolType 过滤。
//
// 对应 Python: ResourceManager.get_tool_infos(tool_id, *, tool_type=None, tag=, ...)
// Python 用 _get_card_type(card) 和 tool_type 列表做过滤，Go 用 getCardType + toolTypes 参数对齐。
func (m *ResourceMgr) GetToolInfos(toolIDs []string, toolTypes []string, opts ...ResourceOption) ([]*schema.ToolInfo, error) {
	tools, err := m.GetTool(toolIDs, opts...)
	if err != nil {
		return nil, err
	}

	results := make([]*schema.ToolInfo, 0, len(tools))
	for _, t := range tools {
		card := t.Card()
		// 按类型过滤（与 Python 对齐）
		if len(toolTypes) > 0 {
			cardType := getCardType(&schema.BaseCard{ID: card.ID, Name: card.Name})
			matched := false
			for _, tt := range toolTypes {
				if cardType == tt {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		if info := card.ToolInfo(); info != nil {
			results = append(results, info)
		}
	}
	return results, nil
}

// --- 生命周期 ---

// Release 释放资源管理器，调用 registry.Tool().Release(ctx) + 重建 registry/tagMgr/idToCard。
//
// 对应 Python: ResourceManager.release()
func (m *ResourceMgr) Release(ctx context.Context) error {
	err := m.registry.Tool().Release(ctx)

	// 重建三大核心组件
	m.registry = NewResourceRegistry()
	m.tagMgr = NewTagMgr()
	m.idToCard = NewThreadSafeDict[string, *schema.BaseCard]()

	logger.Info(logger.ComponentAgentCore).
		Str("event_type", "RESOURCE_MGR_RELEASE").
		Msg("ResourceManager 已释放并重建")

	return err
}

// --- AgentTeam 操作（标记 ⤵️ 预留）---

// AddAgentTeam 注册 Agent 团队。
//
// 对应 Python: ResourceManager.add_agent_team(agent_team_id, provider, **kwargs)
func (m *ResourceMgr) AddAgentTeam(card *multiagents.TeamCard, provider multiagents.AgentTeamProvider, opts ...ResourceOption) error {
	if err := m.innerValidateProvider(provider, "team"); err != nil {
		return err
	}
	return m.innerAddResource(card.ID, "team", provider, &card.BaseCard, "", "")
}

// RemoveAgentTeam 注销 Agent 团队。
//
// 对应 Python: ResourceManager.remove_agent_team(agent_team_id, **kwargs)
func (m *ResourceMgr) RemoveAgentTeam(agentTeamIDs []string, opts ...ResourceOption) ([]multiagents.AgentTeamProvider, error) {
	results := make([]multiagents.AgentTeamProvider, 0, len(agentTeamIDs))
	for _, id := range agentTeamIDs {
		provider, err := m.registry.AgentTeam().RemoveAgentTeam(id)
		if err != nil {
			return nil, err
		}
		results = append(results, provider)
	}
	return results, nil
}

// GetAgentTeam 获取 Agent 团队。
//
// 对应 Python: ResourceManager.get_agent_team(agent_team_id, **kwargs)
func (m *ResourceMgr) GetAgentTeam(ctx context.Context, agentTeamIDs []string, opts ...ResourceOption) ([]multiagents.BaseTeam, error) {
	results := make([]multiagents.BaseTeam, 0, len(agentTeamIDs))
	for _, id := range agentTeamIDs {
		team, err := m.registry.AgentTeam().GetAgentTeam(ctx, id)
		if err != nil {
			return nil, err
		}
		results = append(results, team)
	}
	return results, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// applyResourceOptions 应用资源选项。
func applyResourceOptions(opts ...ResourceOption) resourceOptions {
	o := resourceOptions{}
	for _, opt := range opts {
		opt(&o)
	}
	return o
}

// applyMcpOptions 应用 MCP 选项。
func applyMcpOptions(opts ...McpOption) mcpOptions {
	o := mcpOptions{}
	for _, opt := range opts {
		opt(&o)
	}
	return o
}

// applyTagOptions 应用标签选项。
func applyTagOptions(opts ...TagOption) tagOptions {
	o := tagOptions{}
	for _, opt := range opts {
		opt(&o)
	}
	return o
}

// innerFindResourceIDs 根据 resourceIDs、resourceType、tag 和 tagMatchStrategy 查找匹配的资源 ID 列表。
// 返回值：资源ID列表、是否跳过（因标签不存在）、错误。
// 如果 resourceIDs 非空且指定了非全局 tag，按 tag 过滤 resourceIDs；
// 如果 resourceIDs 非空且无 tag（或 TagGlobal），直接返回 resourceIDs；
// 如果 resourceIDs 为空且指定了 tag，按 tag 查找所有匹配资源。
//
// 对应 Python: ResourceManager._inner_find_resource_ids(resource_id, resource_type, tag, tag_match_strategy)
func (m *ResourceMgr) innerFindResourceIDs(resourceIDs []string, resourceType string, tag Tag, tagMatchStrategy TagMatchStrategy) ([]string, bool, error) {
	// 如果指定了 resourceIDs，按标签过滤或直接返回
	if len(resourceIDs) > 0 {
		// 指定了非全局标签，需过滤出同时拥有该标签的 ID
		if tag != "" && tag != TagGlobal {
			foundIDs, err := m.tagMgr.FindResourcesByTags([]Tag{tag}, tagMatchStrategy, true)
			if err != nil {
				return nil, false, err
			}
			foundSet := make(map[string]struct{}, len(foundIDs))
			for _, id := range foundIDs {
				foundSet[id] = struct{}{}
			}
			result := make([]string, 0, len(resourceIDs))
			for _, id := range resourceIDs {
				if _, ok := foundSet[id]; ok {
					result = append(result, id)
				}
			}
			return result, false, nil
		}
		return resourceIDs, false, nil
	}

	// resourceIDs 为空时，按标签查找
	if tag != "" && tag != TagGlobal {
		foundIDs, err := m.tagMgr.FindResourcesByTags([]Tag{tag}, tagMatchStrategy, true)
		if err != nil {
			return nil, false, err
		}
		return foundIDs, false, nil
	}

	// TagGlobal 或无标签且无 ID 列表，返回空
	return nil, false, nil
}

// innerValidateResourceID 验证资源 ID 非空、非纯空白。
//
// 对应 Python: ResourceManager._inner_validate_resource_id(resource_id, resource_type)
func (m *ResourceMgr) innerValidateResourceID(resourceID, resourceType string) error {
	if resourceID == "" {
		return exception.BuildError(exception.StatusResourceIDValueInvalid,
			exception.WithParam("resource_type", resourceType),
			exception.WithParam("reason", "resource id is empty"),
		)
	}
	if strings.TrimSpace(resourceID) == "" {
		return exception.BuildError(exception.StatusResourceIDValueInvalid,
			exception.WithParam("resource_type", resourceType),
			exception.WithParam("reason", "resource id is whitespace only"),
		)
	}
	return nil
}

// innerValidateProvider 验证 provider 非空。
//
// 对应 Python: ResourceManager._inner_validate_provider(provider, resource_type)
// innerValidateProvider 校验 provider 非空。
//
// 对应 Python: ResourceManager._inner_validate_provider(provider, resource_type)
// ⤵️ 预留：Python 还校验 provider 可调用性和 RemoteAgent 特殊处理（9.84 实现后回填）。
func (m *ResourceMgr) innerValidateProvider(provider any, resourceType string) error {
	if provider == nil {
		return exception.BuildError(exception.StatusResourceProviderInvalid,
			exception.WithParam("resource_type", resourceType),
			exception.WithParam("reason", "provider is nil"),
		)
	}
	return nil
}

// innerValidateServerConfig 校验 MCP 服务器配置。
//
// 对应 Python: ResourceManager._inner_validate_server_config(server_config)
// ⤵️ 预留：Python 支持列表校验（检查 server_id 类型/空白/列表内重复 ID），Go 当前仅校验单个。
// 批量场景出现时扩展为 innerValidateServerConfigs([]*McpServerConfig)。
func (m *ResourceMgr) innerValidateServerConfig(serverConfig *mcp.McpServerConfig) error {
	if serverConfig == nil {
		return exception.BuildError(exception.StatusResourceMCPServerParamInvalid,
			exception.WithParam("reason", "server config is nil"),
		)
	}
	if serverConfig.ServerID == "" {
		return exception.BuildError(exception.StatusResourceMCPServerParamInvalid,
			exception.WithParam("reason", "server id is empty"),
		)
	}
	if serverConfig.ServerName == "" {
		return exception.BuildError(exception.StatusResourceMCPServerParamInvalid,
			exception.WithParam("reason", "server name is empty"),
		)
	}
	return nil
}

// --- 核心分派方法 ---

// getMgr 根据 resourceType 获取子管理器。
// 不支持的类型返回 nil。
//
// 对应 Python: ResourceManager._get_mgr(resource_type)
func (m *ResourceMgr) getMgr(resourceType string) any {
	switch resourceType {
	case "workflow":
		return m.registry.Workflow()
	case "agent":
		return m.registry.Agent()
	case "team":
		return m.registry.AgentTeam()
	case "tool":
		return m.registry.Tool()
	case "prompt":
		return m.registry.Prompt()
	case "model":
		return m.registry.Model()
	case "sys_operation":
		return m.registry.SysOperation()
	default:
		return nil
	}
}

// dispatchAdd 分发到子管理器的 add 方法。
// resource 为资源实例或 provider，resourceCard 和 interfaceURL 仅 agent 类型使用。
//
// 对应 Python: ResourceManager._dispatch_add(resource_type, resource_id, resource, resource_card=None, interface_url=None)
func (m *ResourceMgr) dispatchAdd(resourceType, resourceID string, resource any, resourceCard *schema.BaseCard, interfaceURL string) error {
	switch resourceType {
	case "workflow":
		provider, ok := resource.(WorkflowProvider)
		if !ok {
			return exception.BuildError(exception.StatusResourceAddError,
				exception.WithParam("resource_type", resourceType),
				exception.WithParam("resource_id", resourceID),
				exception.WithParam("reason", "resource is not WorkflowProvider"),
			)
		}
		return m.registry.Workflow().AddWorkflow(resourceID, provider)
	case "agent":
		provider, ok := resource.(AgentProvider)
		if !ok {
			return exception.BuildError(exception.StatusResourceAddError,
				exception.WithParam("resource_type", resourceType),
				exception.WithParam("resource_id", resourceID),
				exception.WithParam("reason", "resource is not AgentProvider"),
			)
		}
		// ⤵️ 预留：interface_url 用于分布式场景
		_ = interfaceURL
		return m.registry.Agent().AddAgent(resourceID, provider)
	case "team":
		provider, ok := resource.(multiagents.AgentTeamProvider)
		if !ok {
			return exception.BuildError(exception.StatusResourceProviderInvalid,
				exception.WithParam("resource_type", "team"),
				exception.WithParam("reason", "resource is not AgentTeamProvider"),
			)
		}
		return m.registry.AgentTeam().AddAgentTeam(resourceID, provider)
	case "tool":
		t, ok := resource.(tool.Tool)
		if !ok {
			return exception.BuildError(exception.StatusResourceAddError,
				exception.WithParam("resource_type", resourceType),
				exception.WithParam("resource_id", resourceID),
				exception.WithParam("reason", "resource is not Tool"),
			)
		}
		return m.registry.Tool().AddTool(resourceID, t)
	case "prompt":
		tmpl, ok := resource.(*prompt.PromptTemplate)
		if !ok {
			return exception.BuildError(exception.StatusResourceAddError,
				exception.WithParam("resource_type", resourceType),
				exception.WithParam("resource_id", resourceID),
				exception.WithParam("reason", "resource is not PromptTemplate"),
			)
		}
		return m.registry.Prompt().AddPrompt(resourceID, tmpl)
	case "model":
		provider, ok := resource.(ModelProvider)
		if !ok {
			return exception.BuildError(exception.StatusResourceAddError,
				exception.WithParam("resource_type", resourceType),
				exception.WithParam("resource_id", resourceID),
				exception.WithParam("reason", "resource is not ModelProvider"),
			)
		}
		return m.registry.Model().AddModel(resourceID, provider)
	case "sys_operation":
		return m.registry.SysOperation().AddSysOperation(resourceID, resource)
	default:
		return exception.BuildError(exception.StatusResourceAddError,
			exception.WithParam("resource_type", resourceType),
			exception.WithParam("resource_id", resourceID),
			exception.WithParam("reason", fmt.Sprintf("unsupported resource type: %s", resourceType)),
		)
	}
}

// dispatchRemove 分发到子管理器的 remove 方法，返回被移除的资源。
//
// 对应 Python: ResourceManager._dispatch_remove(resource_type, resource_id)
func (m *ResourceMgr) dispatchRemove(resourceType, resourceID string) (any, error) {
	switch resourceType {
	case "workflow":
		return m.registry.Workflow().RemoveWorkflow(resourceID)
	case "agent":
		return m.registry.Agent().RemoveAgent(resourceID)
	case "team":
		return m.registry.AgentTeam().RemoveAgentTeam(resourceID)
	case "tool":
		return m.registry.Tool().RemoveTool(resourceID)
	case "prompt":
		return m.registry.Prompt().RemovePrompt(resourceID)
	case "model":
		return m.registry.Model().RemoveModel(resourceID)
	case "sys_operation":
		return m.registry.SysOperation().RemoveSysOperation(resourceID)
	default:
		return nil, exception.BuildError(exception.StatusResourceGetError,
			exception.WithParam("resource_type", resourceType),
			exception.WithParam("resource_id", resourceID),
			exception.WithParam("reason", fmt.Sprintf("unsupported resource type: %s", resourceType)),
		)
	}
}

// dispatchGet 分发到子管理器的 get 方法。
// 仅 workflow/model/tool 类型传 session。
//
// 对应 Python: ResourceManager._dispatch_get(resource_type, resource_id, session=None)
func (m *ResourceMgr) dispatchGet(ctx context.Context, resourceType, resourceID string, session any) (any, error) {
	switch resourceType {
	case "workflow":
		s, _ := session.(decorator.TracerSession)
		return m.registry.Workflow().GetWorkflow(ctx, resourceID, s)
	case "agent":
		return m.registry.Agent().GetAgent(ctx, resourceID)
	case "team":
		return m.registry.AgentTeam().GetAgentTeam(ctx, resourceID)
	case "tool":
		s, _ := session.(decorator.TracerSession)
		return m.registry.Tool().GetTool(resourceID, s)
	case "prompt":
		return m.registry.Prompt().GetPrompt(resourceID)
	case "model":
		s, _ := session.(decorator.TracerSession)
		return m.registry.Model().GetModel(ctx, resourceID, s)
	case "sys_operation":
		return m.registry.SysOperation().GetSysOperation(resourceID)
	default:
		return nil, exception.BuildError(exception.StatusResourceGetError,
			exception.WithParam("resource_type", resourceType),
			exception.WithParam("resource_id", resourceID),
			exception.WithParam("reason", fmt.Sprintf("unsupported resource type: %s", resourceType)),
		)
	}
}

// --- 内部核心流转方法 ---

// innerAddResource 核心添加逻辑：检查重复 → 分发 add → 缓存 card → 标记 tag → 日志。
//
// 对应 Python: ResourceManager._inner_add_resource(resource_id, resource_type, resource, resource_card=None, tag=None, interface_url=None)
func (m *ResourceMgr) innerAddResource(resourceID, resourceType string, resource any, resourceCard *schema.BaseCard, tag Tag, interfaceURL string) error {
	// 1. 检查资源是否已存在
	if m.tagMgr.HasResource(resourceID) {
		return exception.BuildError(exception.StatusResourceAddError,
			exception.WithParam("card", resourceCardStr(resourceCard, resourceID)),
			exception.WithParam("reason", "resource already exist"),
		)
	}

	// 2. 分发到子管理器 add
	if err := m.dispatchAdd(resourceType, resourceID, resource, resourceCard, interfaceURL); err != nil {
		return err
	}

	// 3. 缓存 card 到 idToCard
	if resourceCard != nil {
		m.idToCard.Set(resourceID, resourceCard)
	}

	// 4. 标记 tag
	effectiveTag := tag
	if effectiveTag == "" {
		effectiveTag = TagGlobal
	}
	m.tagMgr.TagResource(resourceID, []Tag{effectiveTag})

	// 5. 日志记录
	logger.Info(logger.ComponentAgentCore).
		Str("event_type", "RESOURCE_MGR_ADD_RESOURCE").
		Str("resource_id", resourceID).
		Str("resource_type", resourceType).
		Str("tag", effectiveTag).
		Str("card", resourceCardStr(resourceCard, resourceID)).
		Msg("添加资源成功")

	return nil
}

// innerRemoveResources 核心移除逻辑：按 tag 查找或直接按 ID → 遍历移除 → 分发 remove → Pop card → 日志。
//
// 对应 Python: ResourceManager._inner_remove_resources(resource_id, resource_type, tag, tag_match_strategy, skip_if_tag_not_exists)
func (m *ResourceMgr) innerRemoveResources(resourceIDs []string, resourceType string, tag Tag, tagMatchStrategy TagMatchStrategy, skipIfTagNotExists bool) ([]any, error) {
	idsToRemove := resourceIDs

	// 如果未指定 ID 列表，按 tag 查找
	if len(idsToRemove) == 0 {
		if err := innerValidateTag([]Tag{tag}); err != nil {
			return nil, err
		}
		effectiveTag := tag
		if effectiveTag == "" {
			effectiveTag = TagGlobal
		}
		found, err := m.tagMgr.FindResourcesByTags([]Tag{effectiveTag}, tagMatchStrategy, skipIfTagNotExists)
		if err != nil {
			return nil, err
		}
		idsToRemove = found
		if len(idsToRemove) == 0 {
			return []any{}, nil
		}
	}

	results := make([]any, 0, len(idsToRemove))
	for _, removeID := range idsToRemove {
		// 1. 移除标签
		m.tagMgr.RemoveResource(removeID)

		// 2. 分发到子管理器 remove
		_, rmErr := m.dispatchRemove(resourceType, removeID)

		// 3. 从 idToCard Pop
		removedCard := m.idToCard.Pop(removeID)

		if rmErr != nil {
			logger.Error(logger.ComponentAgentCore).
				Str("event_type", "RESOURCE_MGR_REMOVE_RESOURCE").
				Str("resource_id", removeID).
				Str("resource_type", resourceType).
				Str("tag", tag).
				Str("card", resourceCardStr(removedCard, removeID)).
				Err(rmErr).
				Msg("移除资源失败")
			return nil, rmErr
		}

		// 4. 根据 idReturnTypes 决定返回内容
		if _, isIDReturn := idReturnTypes[resourceType]; isIDReturn {
			results = append(results, removeID)
		} else {
			if removedCard != nil {
				results = append(results, removedCard)
			}
		}

		logger.Info(logger.ComponentAgentCore).
			Str("event_type", "RESOURCE_MGR_REMOVE_RESOURCE").
			Str("resource_id", removeID).
			Str("resource_type", resourceType).
			Str("tag", tag).
			Str("card", resourceCardStr(removedCard, removeID)).
			Msg("移除资源成功")
	}

	return results, nil
}

// innerGetResources 同步获取资源：查找 ID → 遍历 dispatchGet → 日志。
//
// 对应 Python: ResourceManager._inner_get_resources(resource_id, resource_type, tag, tag_match_strategy, session)
func (m *ResourceMgr) innerGetResources(ctx context.Context, resourceIDs []string, resourceType string, tag Tag, tagMatchStrategy TagMatchStrategy, session any) ([]any, error) {
	ids, _, err := m.innerFindResourceIDs(resourceIDs, resourceType, tag, tagMatchStrategy)
	if err != nil {
		return nil, err
	}

	results := make([]any, 0, len(ids))
	for _, id := range ids {
		if !m.tagMgr.HasResource(id) {
			continue
		}
		resource, getErr := m.dispatchGet(ctx, resourceType, id, session)
		if getErr != nil {
			logger.Error(logger.ComponentAgentCore).
				Str("event_type", "RESOURCE_MGR_GET_RESOURCE").
				Str("resource_id", id).
				Str("resource_type", resourceType).
				Err(getErr).
				Msg("获取资源失败")
			continue
		}
		if resource != nil {
			results = append(results, resource)
		}
	}

	return results, nil
}

// innerGetResourcesByProvider 通过 Provider 获取资源（workflow/agent/team/model 类型）。
// 逻辑同 innerGetResources，但用于 Provider 模式。
//
// 对应 Python: ResourceManager._inner_get_resources_by_provider(resource_id, resource_type, tag, tag_match_strategy, session)
func (m *ResourceMgr) innerGetResourcesByProvider(ctx context.Context, resourceIDs []string, resourceType string, tag Tag, tagMatchStrategy TagMatchStrategy, session any) ([]any, error) {
	ids, _, err := m.innerFindResourceIDs(resourceIDs, resourceType, tag, tagMatchStrategy)
	if err != nil {
		return nil, err
	}

	if len(ids) == 0 {
		return []any{}, nil
	}

	results := make([]any, 0, len(ids))
	for _, id := range ids {
		if !m.tagMgr.HasResource(id) {
			continue
		}
		resource, getErr := m.dispatchGet(ctx, resourceType, id, session)
		if getErr != nil {
			logger.Error(logger.ComponentAgentCore).
				Str("event_type", "RESOURCE_MGR_GET_RESOURCE").
				Str("resource_id", id).
				Str("resource_type", resourceType).
				Err(getErr).
				Msg("通过 Provider 获取资源失败")
			continue
		}
		if resource != nil {
			results = append(results, resource)
		}
	}

	return results, nil
}

// --- 验证方法 ---

// innerValidateTag 验证标签：空值、GLOBAL 与其他 tag 混用、空元素、重复 tag。
// Go 中 tag 是单个 Tag (string)，验证单个 tag 非空即可。
//
// 对应 Python: ResourceManager._inner_validate_tag(tag)
// innerValidateTag 校验标签列表。
//
// 对应 Python: ResourceManager._inner_validate_tag(tag)
// Python 校验：(1) 空值 (2) GLOBAL 与其他标签混用 (3) 空元素 (4) 重复元素
func innerValidateTag(tags []Tag) error {
	if len(tags) == 0 {
		return exception.BuildError(exception.StatusResourceTagValueInvalid,
			exception.WithParam("tag", ""),
			exception.WithParam("reason", "tag list is empty"),
		)
	}
	// 检查 GLOBAL 与其他标签混用
	hasGlobal := false
	for _, t := range tags {
		if t == TagGlobal {
			hasGlobal = true
			break
		}
	}
	if hasGlobal && len(tags) > 1 {
		return exception.BuildError(exception.StatusResourceTagValueInvalid,
			exception.WithParam("tag", fmt.Sprintf("%v", tags)),
			exception.WithParam("reason", "The GLOBAL tag already exists and cannot be assigned additional tags."),
		)
	}
	// 检查空元素和重复
	seen := make(map[Tag]bool, len(tags))
	for _, t := range tags {
		if t == "" {
			return exception.BuildError(exception.StatusResourceTagValueInvalid,
				exception.WithParam("tag", fmt.Sprintf("%v", tags)),
				exception.WithParam("reason", "has empty tag value"),
			)
		}
		if seen[t] {
			return exception.BuildError(exception.StatusResourceTagValueInvalid,
				exception.WithParam("tag", fmt.Sprintf("%v", tags)),
				exception.WithParam("reason", fmt.Sprintf("has duplicate tag '%s' item", t)),
			)
		}
		seen[t] = true
	}
	return nil
}

// innerValidateResourceCard 验证 Card 类型：使用 reflect 检查 card 是否为 cardClassType 实例。
//
// 对应 Python: ResourceManager._inner_validate_resource_card(card, resource_type, card_class_type)
func innerValidateResourceCard(card *schema.BaseCard, resourceType string, cardClassType reflect.Type) error {
	if card == nil {
		return exception.BuildError(exception.StatusResourceCardValueInvalid,
			exception.WithParam("resource_type", resourceType),
			exception.WithParam("reason", fmt.Sprintf("card cannot be nil, must be an instance of %s", cardClassType.Name())),
		)
	}
	cardType := reflect.TypeOf(card)
	if cardType != cardClassType {
		return exception.BuildError(exception.StatusResourceCardValueInvalid,
			exception.WithParam("resource_type", resourceType),
			exception.WithParam("reason", fmt.Sprintf("cannot be nil, must be an instance of %s", cardClassType.Name())),
		)
	}
	return nil
}

// innerValidateResourceIDs 批量 ID 校验：列表非空、每个 ID 有效、无重复。
//
// 对应 Python: ResourceManager._inner_validate_resource_ids(resource_id, resource_type)
func innerValidateResourceIDs(resourceIDs []string, resourceType string) error {
	if len(resourceIDs) == 0 {
		return exception.BuildError(exception.StatusResourceIDValueInvalid,
			exception.WithParam("resource_type", resourceType),
			exception.WithParam("reason", fmt.Sprintf("%s id list cannot be empty", resourceType)),
		)
	}

	seen := make(map[string]struct{}, len(resourceIDs))
	for idx, rid := range resourceIDs {
		if rid == "" {
			return exception.BuildError(exception.StatusResourceIDValueInvalid,
				exception.WithParam("resource_type", resourceType),
				exception.WithParam("reason", fmt.Sprintf("invalid %s id at idx %d: cannot be empty", resourceType, idx)),
			)
		}
		if strings.TrimSpace(rid) == "" {
			return exception.BuildError(exception.StatusResourceIDValueInvalid,
				exception.WithParam("resource_type", resourceType),
				exception.WithParam("reason", fmt.Sprintf("invalid %s id at idx %d: cannot be whitespace only", resourceType, idx)),
			)
		}
		if _, exists := seen[rid]; exists {
			return exception.BuildError(exception.StatusResourceIDValueInvalid,
				exception.WithParam("resource_type", resourceType),
				exception.WithParam("reason", fmt.Sprintf("duplicate %s id found: '%s' appears multiple times", resourceType, rid)),
			)
		}
		seen[rid] = struct{}{}
	}
	return nil
}

// innerValidateProviders 批量 Provider 校验：列表非空、每个 provider 非 nil。
// Go 静态类型语言中，provider 类型由编译器保证，此处仅校验空值。
//
// 对应 Python: ResourceManager._inner_validate_providers(providers, resource_type, card_class_type=None)
func innerValidateProviders(providers []any, resourceType string, cardClassType reflect.Type) error {
	if len(providers) == 0 {
		return exception.BuildError(exception.StatusResourceProviderInvalid,
			exception.WithParam("resource_type", resourceType),
			exception.WithParam("reason", "cannot be empty: expected a non-empty list of providers"),
		)
	}

	for idx, provider := range providers {
		if provider == nil {
			expectedName := "any"
			if cardClassType != nil {
				expectedName = cardClassType.Name()
			}
			return exception.BuildError(exception.StatusResourceProviderInvalid,
				exception.WithParam("resource_type", resourceType),
				exception.WithParam("reason", fmt.Sprintf("invalid provider at idx %d: provider cannot be nil, must be an instance of %s", idx, expectedName)),
			)
		}
	}
	return nil
}

// innerValidateResource 资源实例类型校验：非 nil，且 reflect 类型匹配。
//
// 对应 Python: ResourceManager._inner_validate_resource(instance, resource_type, resource_class_type)
func innerValidateResource(instance any, resourceType string, resourceClassType reflect.Type) error {
	if instance == nil {
		return exception.BuildError(exception.StatusResourceValueInvalid,
			exception.WithParam("resource_type", resourceType),
			exception.WithParam("reason", fmt.Sprintf("%s cannot be nil: expected an instance of %s", resourceType, resourceClassType.Name())),
		)
	}

	instanceType := reflect.TypeOf(instance)
	if instanceType != resourceClassType {
		return exception.BuildError(exception.StatusResourceValueInvalid,
			exception.WithParam("resource_type", resourceType),
			exception.WithParam("reason", fmt.Sprintf("invalid %s type: expected %s, got %s", resourceType, resourceClassType.Name(), instanceType.Name())),
		)
	}
	return nil
}

// getCardType 从 Card 推断资源类型。
// 判断 card 的 reflect 类型，返回 "mcp"/"function"/"team"/"workflow"/"agent" 或空字符串。
//
// 对应 Python: ResourceManager._get_card_type(card)
func getCardType(card *schema.BaseCard) string {
	if card == nil {
		return ""
	}
	// 通过 reflect 判断 card 的实际类型
	cardType := reflect.TypeOf(card)
	// 检查各 Card 类型
	switch cardType {
	case reflect.TypeOf((*mcp.McpToolCard)(nil)):
		return "mcp"
	case reflect.TypeOf((*tool.ToolCard)(nil)):
		return "function"
	case reflect.TypeOf((*schema.WorkflowCard)(nil)):
		return "workflow"
	case reflect.TypeOf((*agentschema.AgentCard)(nil)):
		return "agent"
	case reflect.TypeOf((*multiagents.TeamCard)(nil)):
		return "team"
	default:
		return ""
	}
}

// innerGetServerIDs 按 serverID/serverName/tag 查找服务器 ID 列表。
// 返回: (server_id 列表, 是否精确匹配)。
//
// 对应 Python: ResourceManager._inner_get_server_ids(server_id, server_name, tag, tag_match_strategy, skip_if_tag_not_exists, error_code)
func (m *ResourceMgr) innerGetServerIDs(serverID, serverName string, tag Tag, tagMatchStrategy TagMatchStrategy, skipIfNotExists bool, errorCode exception.StatusCode) ([]string, bool, error) {
	serverIDs := make([]string, 0)
	exactMatch := false

	if serverID != "" {
		serverIDs = append(serverIDs, serverID)
		exactMatch = true
	} else {
		effectiveTag := tag
		if effectiveTag == "" {
			effectiveTag = TagGlobal
		}

		if serverName == "" {
			// 按 tag 查找
			found, err := m.tagMgr.FindResourcesByTags([]Tag{effectiveTag}, tagMatchStrategy, skipIfNotExists)
			if err != nil {
				return nil, false, err
			}
			serverIDs = found
		} else {
			// 按 server name 查找
			serverIDs = m.registry.Tool().GetMcpServerIDs(serverName)
		}
	}

	return serverIDs, exactMatch, nil
}

// resourceCardStr 返回 card 的字符串表示，用于日志。
// card 为 nil 时回退到 resourceID。
func resourceCardStr(card *schema.BaseCard, resourceID string) string {
	if card != nil {
		return card.String()
	}
	return resourceID
}
