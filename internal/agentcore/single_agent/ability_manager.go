package single_agent

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/mcp"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AbilityManager Agent 能力注册与调度中心。
//
// 职责：
//   - 存储可用 Ability Card（仅元数据，不持有实例）
//   - 提供 add/remove/query 接口
//   - 将 Card 转为 ToolInfo 供 LLM 使用
//   - 执行 Ability 调用（从 ResourceManager 获取实例）
//
// 对应 Python: openjiuwen/core/single_agent/ability_manager.py (AbilityManager)
type AbilityManager struct {
	tools         map[string]*tool.ToolCard
	workflows     map[string]*schema.WorkflowCard
	agents        map[string]*schema.AgentCard
	mcpServers    map[string]*mcp.McpServerConfig
	contextEngine ContextEngine   // ⤵️ 预留，领域五回填
	resourceMgr   ResourceManager
	rail          ToolRail // ⤵️ 预留，6.4-6.10 回填
}

// toolItem 内部辅助类型，用于 prioritizePaidSearch 的输入。
type toolItem struct {
	name string
	card *tool.ToolCard
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewAbilityManager 创建 AbilityManager 实例。
func NewAbilityManager(resourceMgr ResourceManager) *AbilityManager {
	if resourceMgr == nil {
		resourceMgr = &NoopResourceManager{}
	}
	return &AbilityManager{
		tools:       make(map[string]*tool.ToolCard),
		workflows:   make(map[string]*schema.WorkflowCard),
		agents:      make(map[string]*schema.AgentCard),
		mcpServers:  make(map[string]*mcp.McpServerConfig),
		resourceMgr: resourceMgr,
	}
}

// SetContextEngine 设置上下文引擎。
func (am *AbilityManager) SetContextEngine(ce ContextEngine) {
	am.contextEngine = ce
}

// SetRail 设置工具调用生命周期钩子（预留，6.4-6.10 回填）。
func (am *AbilityManager) SetRail(rail ToolRail) {
	am.rail = rail
}

// Add 添加单个能力。重复 name 时保留已有的，记录 Warn 日志，返回 Added=false。
func (am *AbilityManager) Add(ability schema.Ability) AddAbilityResult {
	switch a := ability.(type) {
	case *tool.ToolCard:
		existing, ok := am.tools[a.Name]
		if ok {
			logger.Warn(logger.ComponentAgentCore).
				Str("ability_name", a.Name).
				Str("existing_id", existing.ID).
				Str("new_id", a.ID).
				Msg("检测到重复工具能力，保留已有能力，跳过新增")
			return AddAbilityResult{Name: a.Name, Added: false, Reason: "duplicate_tool"}
		}
		am.tools[a.Name] = a
		return AddAbilityResult{Name: a.Name, Added: true, Reason: "added_tool"}

	case *schema.WorkflowCard:
		existing, ok := am.workflows[a.Name]
		if ok {
			logger.Warn(logger.ComponentAgentCore).
				Str("ability_name", a.Name).
				Str("existing_id", existing.ID).
				Str("new_id", a.ID).
				Msg("检测到重复工作流能力，保留已有能力，跳过新增")
			return AddAbilityResult{Name: a.Name, Added: false, Reason: "duplicate_workflow"}
		}
		am.workflows[a.Name] = a
		return AddAbilityResult{Name: a.Name, Added: true, Reason: "added_workflow"}

	case *schema.AgentCard:
		existing, ok := am.agents[a.Name]
		if ok {
			logger.Warn(logger.ComponentAgentCore).
				Str("ability_name", a.Name).
				Str("existing_id", existing.ID).
				Str("new_id", a.ID).
				Msg("检测到重复Agent能力，保留已有能力，跳过新增")
			return AddAbilityResult{Name: a.Name, Added: false, Reason: "duplicate_agent"}
		}
		am.agents[a.Name] = a
		return AddAbilityResult{Name: a.Name, Added: true, Reason: "added_agent"}

	case *mcp.McpServerConfig:
		existing, ok := am.mcpServers[a.ServerName]
		if ok {
			logger.Warn(logger.ComponentAgentCore).
				Str("ability_name", a.ServerName).
				Str("existing_id", existing.ServerID).
				Str("new_id", a.ServerID).
				Msg("检测到重复MCP服务器能力，保留已有能力，跳过新增")
			return AddAbilityResult{Name: a.ServerName, Added: false, Reason: "duplicate_mcp_server"}
		}
		am.mcpServers[a.ServerName] = a
		return AddAbilityResult{Name: a.ServerName, Added: true, Reason: "added_mcp_server"}

	default:
		name := "unknown"
		if a != nil {
			name = ability.AbilityName()
		}
		logger.Warn(logger.ComponentAgentCore).
			Str("ability_type", fmt.Sprintf("%T", a)).
			Msg("未知能力类型")
		return AddAbilityResult{Name: name, Added: false, Reason: "unknown_ability_type"}
	}
}

// AddMany 批量添加能力。
func (am *AbilityManager) AddMany(abilities []schema.Ability) []AddAbilityResult {
	results := make([]AddAbilityResult, len(abilities))
	for i, a := range abilities {
		results[i] = am.Add(a)
	}
	return results
}

// Remove 按名称移除能力，返回被移除的 Ability（未找到返回 nil）。
// 移除 McpServer 时同时移除其关联工具。
func (am *AbilityManager) Remove(name string) schema.Ability {
	if toolCard, ok := am.tools[name]; ok {
		delete(am.tools, name)
		return toolCard
	}
	if wf, ok := am.workflows[name]; ok {
		delete(am.workflows, name)
		return wf
	}
	if ag, ok := am.agents[name]; ok {
		delete(am.agents, name)
		return ag
	}
	if mcpServer, ok := am.mcpServers[name]; ok {
		delete(am.mcpServers, name)
		// 级联删除该 MCP 服务器下的工具
		serverID := mcpServer.ServerID
		prefix := serverID + "."
		for toolName, toolCard := range am.tools {
			if toolCard.ID != "" && strings.HasPrefix(toolCard.ID, prefix) {
				delete(am.tools, toolName)
			}
		}
		return mcpServer
	}
	return nil
}

// RemoveMany 批量移除能力。
func (am *AbilityManager) RemoveMany(names []string) []schema.Ability {
	results := make([]schema.Ability, len(names))
	for i, name := range names {
		results[i] = am.Remove(name)
	}
	return results
}

// Get 按名称查询能力（依次查找 tools → workflows → agents → mcpServers）。
func (am *AbilityManager) Get(name string) schema.Ability {
	if t, ok := am.tools[name]; ok {
		return t
	}
	if w, ok := am.workflows[name]; ok {
		return w
	}
	if a, ok := am.agents[name]; ok {
		return a
	}
	if m, ok := am.mcpServers[name]; ok {
		return m
	}
	return nil
}

// List 列出所有已注册能力。
func (am *AbilityManager) List() []schema.Ability {
	var abilities []schema.Ability
	for _, t := range am.tools {
		abilities = append(abilities, t)
	}
	for _, w := range am.workflows {
		abilities = append(abilities, w)
	}
	for _, a := range am.agents {
		abilities = append(abilities, a)
	}
	for _, m := range am.mcpServers {
		abilities = append(abilities, m)
	}
	return abilities
}

// ReorderTools 按给定名称顺序重排 tools 注册表。
func (am *AbilityManager) ReorderTools(orderedNames []string) {
	if len(orderedNames) == 0 || len(am.tools) == 0 {
		return
	}
	var preferred []string
	for _, name := range orderedNames {
		if _, ok := am.tools[name]; ok {
			preferred = append(preferred, name)
		}
	}
	if len(preferred) == 0 {
		return
	}
	reordered := make(map[string]*tool.ToolCard, len(am.tools))
	for _, name := range preferred {
		reordered[name] = am.tools[name]
	}
	for name, card := range am.tools {
		if _, ok := reordered[name]; !ok {
			reordered[name] = card
		}
	}
	am.tools = reordered
}

// ListToolInfo 获取 ToolInfo 列表供 LLM function calling 消费。
// names 非空时只返回指定名称的工具；为空时返回全部。
func (am *AbilityManager) ListToolInfo(ctx context.Context, names []string) ([]*schema.ToolInfo, error) {
	var toolInfos []*schema.ToolInfo

	// 1. ToolCards → ToolInfo
	items := make([]toolItem, 0, len(am.tools))
	for name, card := range am.tools {
		items = append(items, toolItem{name: name, card: card})
	}
	items = prioritizePaidSearch(items)

	for _, item := range items {
		if len(names) > 0 {
			found := false
			for _, n := range names {
				if n == item.name {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		// 排除 MCP 服务器下的工具
		if am.isToolInMcpServer(item.card.ID) {
			continue
		}
		toolInfos = append(toolInfos, item.card.ToolInfo())
	}

	// 2. WorkflowCards → ToolInfo
	for name, wf := range am.workflows {
		if len(names) > 0 {
			found := false
			for _, n := range names {
				if n == name {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		toolInfos = append(toolInfos, wf.ToolInfo())
	}

	// 3. AgentCards → ToolInfo
	for name, ag := range am.agents {
		if len(names) > 0 {
			found := false
			for _, n := range names {
				if n == name {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		toolInfos = append(toolInfos, ag.ToolInfo())
	}

	// 4. MCP 懒加载：⤵️ 预留，等 ResourceManager 实现后回填
	// for mcpServerName, mcpServer := range am.mcpServers {
	//     mcpToolInfos, err := am.resourceMgr.GetMcpToolInfos(mcpServer.ServerID)
	//     ...
	// }

	return toolInfos, nil
}

// Execute 并行执行多个 ToolCall，返回每个调用的结果。
// 使用 WaitGroup + channel 收集，与 Python asyncio.gather(return_exceptions=True) 语义一致：
// 所有任务都执行完毕，错误作为 ExecuteResult.Err 返回。
func (am *AbilityManager) Execute(
	ctx context.Context,
	toolCalls []*llmschema.ToolCall,
	session Session,
	tag string,
) []ExecuteResult {
	if len(toolCalls) == 0 {
		return nil
	}

	var wg sync.WaitGroup
	resultCh := make(chan ExecuteResult, len(toolCalls))

	for _, tc := range toolCalls {
		wg.Add(1)
		go func(toolCall *llmschema.ToolCall) {
			defer wg.Done()
			result := am.railedExecuteSingleToolCall(ctx, toolCall, session, tag)
			resultCh <- result
		}(tc)
	}

	wg.Wait()
	close(resultCh)

	results := make([]ExecuteResult, 0, len(toolCalls))
	for r := range resultCh {
		results = append(results, r)
	}

	// ⤵️ 预留：force_finish 信号传播（等 6.4-6.10 Rail 系统就绪后回填）

	return results
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// railedExecuteSingleToolCall 在 Rail 生命周期内执行单个工具调用。
// 当前阶段直接调用 executeSingleToolCall，Rail 钩子调用点预留。
func (am *AbilityManager) railedExecuteSingleToolCall(
	ctx context.Context,
	toolCall *llmschema.ToolCall,
	session Session,
	tag string,
) ExecuteResult {
	// ⤵️ 预留：BeforeToolCall Rail 钩子
	// if am.rail != nil { ... }

	result := am.executeSingleToolCall(ctx, toolCall, session, tag)

	// ⤵️ 预留：AfterToolCall Rail 钩子
	// if am.rail != nil { ... }

	return result
}

// executeSingleToolCall 执行单个工具调用。
// 路由逻辑：按 tool_name 查找 Card → 从 ResourceManager 获取实例 → 执行。
func (am *AbilityManager) executeSingleToolCall(
	ctx context.Context,
	toolCall *llmschema.ToolCall,
	session Session,
	tag string,
) ExecuteResult {
	toolName := toolCall.Name

	// 解析参数
	toolArgs, err := ParseToolArguments(toolCall.Arguments)
	if err != nil {
		logger.Error(logger.ComponentAgentCore).
			Str("tool_name", toolName).
			Err(err).
			Msg("工具收到畸形参数")
		execErr := NewAbilityExecutionError(
			exception.StatusAbilityMalformedArguments,
			toolCall.ID,
			err.Error(),
			exception.WithParam("tool_name", toolName),
		)
		return ExecuteResult{Err: execErr, ToolMsg: execErr.ToolMessage}
	}

	// 路由分发
	if _, ok := am.tools[toolName]; ok {
		return am.executeTool(ctx, toolCall, toolName, toolArgs, session, tag)
	}
	if _, ok := am.workflows[toolName]; ok {
		return am.executeWorkflow(ctx, toolCall, toolName, toolArgs, session, tag)
	}
	if _, ok := am.agents[toolName]; ok {
		return am.executeAgent(ctx, toolCall, toolName, toolArgs, session, tag)
	}
	if _, ok := am.mcpServers[toolName]; ok {
		execErr := NewAbilityExecutionError(
			exception.StatusAbilityExecutionError,
			toolCall.ID,
			"MCP 工具执行暂未实现: "+toolName,
			exception.WithParam("tool_name", toolName),
		)
		return ExecuteResult{Err: execErr, ToolMsg: execErr.ToolMessage}
	}

	// 兜底：尝试从 ResourceManager 按 name 获取 Tool
	return am.executeFallbackTool(ctx, toolCall, toolName, toolArgs, session, tag)
}

// executeTool 执行 Tool 类型能力。
func (am *AbilityManager) executeTool(
	ctx context.Context,
	toolCall *llmschema.ToolCall,
	toolName string,
	toolArgs map[string]any,
	session Session,
	tag string,
) ExecuteResult {
	toolCard := am.tools[toolName]
	toolID := toolCard.ID
	if toolID == "" {
		toolID = toolCard.Name
	}

	var opts []ResourceOption
	if tag != "" {
		opts = append(opts, WithResourceTag(tag))
	}

	t, err := am.resourceMgr.GetTool(toolID, opts...)
	if err != nil {
		execErr := NewAbilityExecutionError(
			exception.StatusAbilityNotFound,
			toolCall.ID,
			"工具实例未找到: "+toolID,
			exception.WithParam("tool_name", toolName),
			exception.WithCause(err),
		)
		return ExecuteResult{Err: execErr, ToolMsg: execErr.ToolMessage}
	}

	result, err := t.Invoke(ctx, toolArgs)
	if err != nil {
		logger.Error(logger.ComponentAgentCore).
			Str("tool_name", toolName).
			Err(err).
			Msg("工具执行错误")
		execErr := NewAbilityExecutionError(
			exception.StatusAbilityExecutionError,
			toolCall.ID,
			"工具执行错误: "+err.Error(),
			exception.WithParam("tool_name", toolName),
			exception.WithCause(err),
		)
		return ExecuteResult{Err: execErr, ToolMsg: execErr.ToolMessage}
	}

	content := BuildToolMessageContent(result)
	toolMsg := llmschema.NewToolMessage(toolCall.ID, content)
	return ExecuteResult{Result: result, ToolMsg: toolMsg}
}

// executeWorkflow 执行 Workflow 类型能力。⤵️ 预留，领域八回填。
func (am *AbilityManager) executeWorkflow(
	ctx context.Context,
	toolCall *llmschema.ToolCall,
	toolName string,
	toolArgs map[string]any,
	session Session,
	tag string,
) ExecuteResult {
	wfCard := am.workflows[toolName]
	wfID := wfCard.ID
	if wfID == "" {
		wfID = wfCard.Name
	}

	wf, err := am.resourceMgr.GetWorkflow(wfID)
	if err != nil {
		execErr := NewAbilityExecutionError(
			exception.StatusAbilityNotFound,
			toolCall.ID,
			"工作流实例未找到: "+wfID,
			exception.WithParam("tool_name", toolName),
			exception.WithCause(err),
		)
		return ExecuteResult{Err: execErr, ToolMsg: execErr.ToolMessage}
	}

	result, err := wf.Execute(ctx, toolArgs)
	if err != nil {
		logger.Error(logger.ComponentAgentCore).
			Str("workflow_name", toolName).
			Err(err).
			Msg("工作流执行错误")
		execErr := NewAbilityExecutionError(
			exception.StatusAbilityExecutionError,
			toolCall.ID,
			"工作流执行错误: "+err.Error(),
			exception.WithParam("tool_name", toolName),
			exception.WithCause(err),
		)
		return ExecuteResult{Err: execErr, ToolMsg: execErr.ToolMessage}
	}

	content := BuildToolMessageContent(result)
	toolMsg := llmschema.NewToolMessage(toolCall.ID, content)
	return ExecuteResult{Result: result, ToolMsg: toolMsg}
}

// executeAgent 执行 Agent 类型能力。⤵️ 预留，领域六回填。
func (am *AbilityManager) executeAgent(
	ctx context.Context,
	toolCall *llmschema.ToolCall,
	toolName string,
	toolArgs map[string]any,
	session Session,
	tag string,
) ExecuteResult {
	agentCard := am.agents[toolName]
	agentID := agentCard.ID
	if agentID == "" {
		agentID = agentCard.Name
	}

	ag, err := am.resourceMgr.GetAgent(agentID)
	if err != nil {
		execErr := NewAbilityExecutionError(
			exception.StatusAbilityNotFound,
			toolCall.ID,
			"Agent 实例未找到: "+agentID,
			exception.WithParam("tool_name", toolName),
			exception.WithCause(err),
		)
		return ExecuteResult{Err: execErr, ToolMsg: execErr.ToolMessage}
	}

	result, err := ag.Invoke(ctx, toolArgs)
	if err != nil {
		logger.Error(logger.ComponentAgentCore).
			Str("agent_name", toolName).
			Err(err).
			Msg("Agent 执行错误")
		execErr := NewAbilityExecutionError(
			exception.StatusAbilityExecutionError,
			toolCall.ID,
			"Agent 执行错误: "+err.Error(),
			exception.WithParam("tool_name", toolName),
			exception.WithCause(err),
		)
		return ExecuteResult{Err: execErr, ToolMsg: execErr.ToolMessage}
	}

	content := BuildToolMessageContent(result)
	toolMsg := llmschema.NewToolMessage(toolCall.ID, content)
	return ExecuteResult{Result: result, ToolMsg: toolMsg}
}

// executeFallbackTool 兜底：从 ResourceManager 按 name 获取 Tool。
func (am *AbilityManager) executeFallbackTool(
	ctx context.Context,
	toolCall *llmschema.ToolCall,
	toolName string,
	toolArgs map[string]any,
	session Session,
	tag string,
) ExecuteResult {
	var opts []ResourceOption
	if tag != "" {
		opts = append(opts, WithResourceTag(tag))
	}

	t, err := am.resourceMgr.GetTool(toolName, opts...)
	if err != nil {
		execErr := NewAbilityExecutionError(
			exception.StatusAbilityNotFound,
			toolCall.ID,
			"能力未找到: "+toolName,
			exception.WithParam("ability_name", toolName),
			exception.WithCause(err),
		)
		return ExecuteResult{Err: execErr, ToolMsg: execErr.ToolMessage}
	}

	result, invokeErr := t.Invoke(ctx, toolArgs)
	if invokeErr != nil {
		logger.Error(logger.ComponentAgentCore).
			Str("tool_name", toolName).
			Err(invokeErr).
			Msg("工具执行错误")
		execErr := NewAbilityExecutionError(
			exception.StatusAbilityExecutionError,
			toolCall.ID,
			"工具执行错误: "+invokeErr.Error(),
			exception.WithParam("tool_name", toolName),
			exception.WithCause(invokeErr),
		)
		return ExecuteResult{Err: execErr, ToolMsg: execErr.ToolMessage}
	}

	content := BuildToolMessageContent(result)
	toolMsg := llmschema.NewToolMessage(toolCall.ID, content)
	return ExecuteResult{Result: result, ToolMsg: toolMsg}
}

// prioritizePaidSearch 当 paid_search 和 free_search 同时存在时，
// 确保 paid_search 排在 free_search 前面。
func prioritizePaidSearch(items []toolItem) []toolItem {
	if len(items) == 0 {
		return items
	}
	var paidIdx, freeIdx int = -1, -1
	for i, item := range items {
		if item.name == "paid_search" {
			paidIdx = i
		}
		if item.name == "free_search" {
			freeIdx = i
		}
	}
	if paidIdx < 0 || freeIdx < 0 || paidIdx < freeIdx {
		return items
	}
	// paid 在 free 后面，将 paid 移到 free 前面
	reordered := make([]toolItem, len(items))
	copy(reordered, items)
	paidItem := reordered[paidIdx]
	// 移除 paid
	reordered = append(reordered[:paidIdx], reordered[paidIdx+1:]...)
	// 找到 free 的新位置（因为移除了一个元素）
	newFreeIdx := freeIdx
	if paidIdx < freeIdx {
		newFreeIdx--
	}
	// 插入 paid 到 free 之前
	reordered = append(reordered[:newFreeIdx], append([]toolItem{paidItem}, reordered[newFreeIdx:]...)...)
	return reordered
}

// isToolInMcpServer 判断工具 ID 是否属于某个 MCP 服务器。
func (am *AbilityManager) isToolInMcpServer(toolID string) bool {
	for _, mcpServer := range am.mcpServers {
		if strings.HasPrefix(toolID, mcpServer.ServerID+".") {
			return true
		}
	}
	return false
}
