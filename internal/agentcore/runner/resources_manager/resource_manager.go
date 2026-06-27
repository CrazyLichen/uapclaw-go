package resources_manager

import (
	"context"
	"fmt"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/prompt"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/mcp"
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

// --- Functional Options: ResourceOption ---

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

// --- Functional Options: McpOption ---

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

// --- Functional Options: TagOption ---

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
func (m *ResourceMgr) RemoveAgent(agentID string, opts ...ResourceOption) ([]*agentschema.AgentCard, error) {
	o := applyResourceOptions(opts...)
	resourceIDs, _, err := m.innerFindResourceIDs(agentID, "agent", o.Tag, o.TagMatchStrategy)
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
func (m *ResourceMgr) GetAgent(ctx context.Context, agentID string, opts ...ResourceOption) ([]interfaces.BaseAgent, error) {
	o := applyResourceOptions(opts...)
	resourceIDs, _, err := m.innerFindResourceIDs(agentID, "agent", o.Tag, o.TagMatchStrategy)
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
func (m *ResourceMgr) RemoveWorkflow(workflowID string, opts ...ResourceOption) ([]*schema.WorkflowCard, error) {
	o := applyResourceOptions(opts...)
	resourceIDs, _, err := m.innerFindResourceIDs(workflowID, "workflow", o.Tag, o.TagMatchStrategy)
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
func (m *ResourceMgr) GetWorkflow(ctx context.Context, workflowID string, opts ...ResourceOption) ([]interfaces.Workflow, error) {
	o := applyResourceOptions(opts...)
	resourceIDs, _, err := m.innerFindResourceIDs(workflowID, "workflow", o.Tag, o.TagMatchStrategy)
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
		m.registry.Tool().RemoveTool(toolID)
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
func (m *ResourceMgr) GetTool(toolID string, opts ...ResourceOption) ([]tool.Tool, error) {
	o := applyResourceOptions(opts...)
	resourceIDs, _, err := m.innerFindResourceIDs(toolID, "tool", o.Tag, o.TagMatchStrategy)
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
func (m *ResourceMgr) RemoveTool(toolID string, opts ...ResourceOption) ([]string, error) {
	o := applyResourceOptions(opts...)
	resourceIDs, _, err := m.innerFindResourceIDs(toolID, "tool", o.Tag, o.TagMatchStrategy)
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
func (m *ResourceMgr) RemoveModel(modelID string, opts ...ResourceOption) ([]string, error) {
	o := applyResourceOptions(opts...)
	resourceIDs, _, err := m.innerFindResourceIDs(modelID, "model", o.Tag, o.TagMatchStrategy)
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
func (m *ResourceMgr) GetModel(ctx context.Context, modelID string, opts ...ResourceOption) ([]model_clients.BaseModelClient, error) {
	o := applyResourceOptions(opts...)
	resourceIDs, _, err := m.innerFindResourceIDs(modelID, "model", o.Tag, o.TagMatchStrategy)
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
func (m *ResourceMgr) RemovePrompt(promptID string, opts ...ResourceOption) ([]string, error) {
	o := applyResourceOptions(opts...)
	resourceIDs, _, err := m.innerFindResourceIDs(promptID, "prompt", o.Tag, o.TagMatchStrategy)
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
func (m *ResourceMgr) GetPrompt(promptID string, opts ...ResourceOption) ([]*prompt.PromptTemplate, error) {
	o := applyResourceOptions(opts...)
	resourceIDs, _, err := m.innerFindResourceIDs(promptID, "prompt", o.Tag, o.TagMatchStrategy)
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
// 对应 Python: ResourceManager.remove_sys_operation(sys_operation_id, **kwargs)
func (m *ResourceMgr) RemoveSysOperation(sysOperationID string, opts ...ResourceOption) error {
	o := applyResourceOptions(opts...)
	resourceIDs, _, err := m.innerFindResourceIDs(sysOperationID, "sys_operation", o.Tag, o.TagMatchStrategy)
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
func (m *ResourceMgr) GetSysOperation(sysOperationID string, opts ...ResourceOption) ([]any, error) {
	o := applyResourceOptions(opts...)
	resourceIDs, _, err := m.innerFindResourceIDs(sysOperationID, "sys_operation", o.Tag, o.TagMatchStrategy)
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

// RemoveMcpServer 移除 MCP 工具服务器。
//
// 对应 Python: ResourceManager.remove_mcp_server(server_id, **kwargs)
func (m *ResourceMgr) RemoveMcpServer(ctx context.Context, serverID string, opts ...McpOption) error {
	o := applyMcpOptions(opts...)
	toolIDs, err := m.registry.Tool().RemoveToolServer(ctx, serverID, o.SkipIfNotExists)
	if err != nil {
		return err
	}

	// 清理标签和缓存
	for _, id := range toolIDs {
		m.tagMgr.RemoveResource(id)
		m.idToCard.Pop(id)
	}

	return nil
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

// RemoveTag 完全移除标签及其所有关联。
//
// 对应 Python: ResourceManager.remove_tag(tag, **kwargs)
func (m *ResourceMgr) RemoveTag(tag Tag, opts ...TagOption) ([]string, error) {
	o := applyTagOptions(opts...)
	return m.tagMgr.RemoveTag(tag, o.SkipIfNotExists)
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

// GetToolInfos 获取工具描述信息列表。
//
// 对应 Python: ResourceManager.get_tool_infos(tool_id, **kwargs)
func (m *ResourceMgr) GetToolInfos(toolID string, opts ...ResourceOption) ([]*schema.ToolInfo, error) {
	tools, err := m.GetTool(toolID, opts...)
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
// ⤵️ 预留：等 TeamCard/BaseTeam 类型实现后回填
func (m *ResourceMgr) AddAgentTeam(agentTeamID string, provider any, opts ...ResourceOption) error {
	return fmt.Errorf("agent team not implemented, agent_team_id=%s", agentTeamID)
}

// RemoveAgentTeam 注销 Agent 团队。
//
// 对应 Python: ResourceManager.remove_agent_team(agent_team_id, **kwargs)
// ⤵️ 预留：等 TeamCard/BaseTeam 类型实现后回填
func (m *ResourceMgr) RemoveAgentTeam(agentTeamID string, opts ...ResourceOption) (any, error) {
	return nil, fmt.Errorf("agent team not implemented, agent_team_id=%s", agentTeamID)
}

// GetAgentTeam 获取 Agent 团队。
//
// 对应 Python: ResourceManager.get_agent_team(agent_team_id, **kwargs)
// ⤵️ 预留：等 TeamCard/BaseTeam 类型实现后回填
func (m *ResourceMgr) GetAgentTeam(agentTeamID string, opts ...ResourceOption) (any, error) {
	return nil, fmt.Errorf("agent team not implemented, agent_team_id=%s", agentTeamID)
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

// innerFindResourceIDs 根据 resourceID、resourceType、tag 和 tagMatchStrategy 查找匹配的资源 ID 列表。
// 返回值：资源ID列表、是否跳过（因标签不存在）、错误。
//
// 对应 Python: ResourceManager._inner_find_resource_ids(resource_id, resource_type, tag, tag_match_strategy)
func (m *ResourceMgr) innerFindResourceIDs(resourceID, resourceType string, tag Tag, tagMatchStrategy TagMatchStrategy) ([]string, bool, error) {
	if resourceID == "" {
		return nil, false, exception.BuildError(exception.StatusResourceIDValueInvalid,
			exception.WithParam("resource_type", resourceType),
			exception.WithParam("reason", "resource id is empty"),
		)
	}

	// 如果指定了标签，先按标签查找
	if tag != "" && tag != TagGlobal {
		foundIDs, err := m.tagMgr.FindResourcesByTags([]Tag{tag}, tagMatchStrategy, true)
		if err != nil {
			return nil, false, err
		}
		// 过滤出匹配 resourceID 的结果
		result := make([]string, 0)
		for _, id := range foundIDs {
			if id == resourceID {
				result = append(result, id)
			}
		}
		return result, false, nil
	}

	// TagGlobal 或无标签，直接返回 resourceID
	return []string{resourceID}, false, nil
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
func (m *ResourceMgr) innerValidateProvider(provider any, resourceType string) error {
	if provider == nil {
		return exception.BuildError(exception.StatusResourceProviderInvalid,
			exception.WithParam("resource_type", resourceType),
			exception.WithParam("reason", "provider is nil"),
		)
	}
	return nil
}

// innerValidateServerConfig 验证 MCP 服务器配置。
//
// 对应 Python: ResourceManager._inner_validate_server_config(server_config)
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
