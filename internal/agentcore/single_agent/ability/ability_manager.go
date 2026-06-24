package ability

import (
	"context"
	"fmt"
	"strings"
	"sync"

	iface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/mcp"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/rail"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/resource"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/workflow"
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
	// mu 读写锁
	mu sync.RWMutex
	// tools 工具注册表
	tools map[string]*tool.ToolCard
	// workflows 工作流注册表
	workflows map[string]*schema.WorkflowCard
	// agents Agent 注册表
	agents map[string]*agentschema.AgentCard
	// mcpServers MCP 服务器注册表
	mcpServers map[string]*mcp.McpServerConfig
	// contextEngine 上下文引擎
	contextEngine iface.ContextEngine
	// resourceMgr 资源管理器
	resourceMgr resource.ResourceManager
}

// toolItem 内部辅助类型，用于 prioritizePaidSearch 的输入。
type toolItem struct {
	// name 工具名称
	name string
	// card 工具卡片
	card *tool.ToolCard
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewAbilityManager 创建 AbilityManager 实例。
func NewAbilityManager(resourceMgr resource.ResourceManager) *AbilityManager {
	if resourceMgr == nil {
		resourceMgr = &resource.NoopResourceManager{}
	}
	return &AbilityManager{
		tools:       make(map[string]*tool.ToolCard),
		workflows:   make(map[string]*schema.WorkflowCard),
		agents:      make(map[string]*agentschema.AgentCard),
		mcpServers:  make(map[string]*mcp.McpServerConfig),
		resourceMgr: resourceMgr,
	}
}

// SetContextEngine 设置上下文引擎。
func (am *AbilityManager) SetContextEngine(ce iface.ContextEngine) {
	am.contextEngine = ce
}

// Add 添加单个能力。重复 name 时保留已有的，记录 Warn 日志，返回 Added=false。
func (am *AbilityManager) Add(ability schema.Ability) AddAbilityResult {
	am.mu.Lock()
	defer am.mu.Unlock()

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

	case *agentschema.AgentCard:
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
	am.mu.Lock()
	defer am.mu.Unlock()

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
	am.mu.RLock()
	defer am.mu.RUnlock()

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
	am.mu.RLock()
	defer am.mu.RUnlock()

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
	am.mu.Lock()
	defer am.mu.Unlock()

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
func (am *AbilityManager) ListToolInfo(ctx context.Context, names []string, mcpServerName ...string) ([]*schema.ToolInfo, error) {
	am.mu.RLock()
	defer am.mu.RUnlock()

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
// 使用 WaitGroup + 按 index 写切片，与 Python asyncio.gather(return_exceptions=True) 语义一致：
// 所有任务都执行完毕，错误作为 ExecuteResult.Err 返回。
// 结果顺序与输入 toolCalls 顺序一致。
//
// cbc 为 Rail 系统的 AgentCallbackContext，用于：
//   - 为每个 tool_call 创建隔离子上下文（ForkForToolCall）
//   - 传播子上下文的 force-finish 信号回父 cbc
//
// 对应 Python: AbilityManager.execute(ctx, tool_call, session, tag)
func (am *AbilityManager) Execute(
	ctx context.Context,
	cbc *rail.AgentCallbackContext,
	toolCalls []*llmschema.ToolCall,
	sess *session.Session,
	tag string,
) []ExecuteResult {
	if len(toolCalls) == 0 {
		return nil
	}

	// cbc 为 nil 时走降级路径（不使用 Rail 系统，直接执行工具调用）
	if cbc == nil {
		am.mu.RLock()
		results := make([]ExecuteResult, len(toolCalls))
		var wg sync.WaitGroup
		for i, tc := range toolCalls {
			wg.Add(1)
			go func(idx int, toolCall *llmschema.ToolCall) {
				defer wg.Done()
				results[idx] = am.executeSingleToolCall(ctx, toolCall, sess, tag)
			}(i, tc)
		}
		am.mu.RUnlock()
		wg.Wait()
		return results
	}

	am.mu.RLock()
	results := make([]ExecuteResult, len(toolCalls))

	// 为每个 tool_call 创建隔离子上下文
	// 对应 Python: tool_ctx = AgentCallbackContext(agent=ctx.agent, inputs=ToolCallInputs(...), extra=ctx.extra, ...)
	toolCtxs := make([]*rail.AgentCallbackContext, len(toolCalls))
	for i, tc := range toolCalls {
		toolCtxs[i] = cbc.ForkForToolCall(tc)
	}

	var wg sync.WaitGroup
	for i, tc := range toolCalls {
		wg.Add(1)
		go func(idx int, toolCall *llmschema.ToolCall, toolCtx *rail.AgentCallbackContext) {
			defer wg.Done()
			results[idx] = am.railedExecuteSingleToolCall(ctx, toolCtx, toolCall, sess, tag)
		}(i, tc, toolCtxs[i])
	}
	am.mu.RUnlock()

	wg.Wait()

	// 从 inputs 读取 after 钩子可能改写的最终值。
	// 对齐 Python: AFTER_TOOL_CALL rails can rewrite tool_result/tool_msg in ctx.inputs.
	// 优先使用 inputs 中的值，兜底用原始 ExecuteResult。
	for i, toolCtx := range toolCtxs {
		if inputs, ok := toolCtx.Inputs().(*rail.ToolCallInputs); ok {
			if inputs.ToolResult != nil {
				results[i].Result = inputs.ToolResult
			}
			if inputs.ToolMsg != nil {
				results[i].ToolMsg = inputs.ToolMsg
			}
		}
	}

	// force-finish 信号传播：子 toolCtx → 父 cbc
	// 对应 Python: for tool_ctx in tool_contexts:
	//   ff = tool_ctx.consume_force_finish()
	//   if ff is not None: ctx.request_force_finish(ff.result); break
	for _, toolCtx := range toolCtxs {
		if ff := toolCtx.ConsumeForceFinish(); ff != nil {
			cbc.RequestForceFinish(ff.Result)
			break
		}
	}

	return results
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// railedExecuteSingleToolCall 在 Rail 生命周期内执行单个工具调用。
//
// 使用 ToolCallRail.Execute 包装，自动提供：
//   - fire(BEFORE_TOOL_CALL) → before 钩子
//   - force-finish 门控 → 可跳过工具执行
//   - 异常 → fire(ON_TOOL_EXCEPTION) → 可 request_retry() 重试
//   - fire(AFTER_TOOL_CALL) → after 钩子
//
// 对应 Python: @rail(before=BEFORE_TOOL_CALL, after=AFTER_TOOL_CALL, on_exception=ON_TOOL_EXCEPTION)
//
//	async def _railed_execute_single_tool_call(self, ctx, tool_call, session, tag=None): ...
func (am *AbilityManager) railedExecuteSingleToolCall(
	ctx context.Context,
	toolCtx *rail.AgentCallbackContext,
	toolCall *llmschema.ToolCall,
	sess *session.Session,
	tag string,
) ExecuteResult {
	var result ExecuteResult
	_ = rail.ToolCallRail.Execute(ctx, toolCtx, func() error {
		// before 钩子已执行完毕，将 inputs 中被 before 钩子改写的 ToolName/ToolArgs 写回 toolCall
		// 对齐 Python: if ctx.inputs.tool_name: tool_call.name = ctx.inputs.tool_name
		// 对齐 Python: if ctx.inputs.tool_args is not None: tool_call.arguments = ctx.inputs.tool_args
		if inputs, ok := toolCtx.Inputs().(*rail.ToolCallInputs); ok {
			if inputs.ToolName != "" {
				toolCall.Name = inputs.ToolName
			}
			if inputs.ToolArgs != "" {
				toolCall.Arguments = inputs.ToolArgs
			}
		}

		result = am.executeSingleToolCall(ctx, toolCall, sess, tag)
		// 回填结果到 inputs（对齐 Python L681-686）
		// after 钩子触发时可通过 inputs 访问执行结果，也可改写。
		if inputs, ok := toolCtx.Inputs().(*rail.ToolCallInputs); ok {
			inputs.ToolCall = toolCall
			inputs.ToolName = toolCall.Name
			inputs.ToolArgs = toolCall.Arguments
			inputs.ToolResult = result.Result
			inputs.ToolMsg = result.ToolMsg
		}
		return result.Err
	})
	return result
}

// executeSingleToolCall 执行单个工具调用。
// 路由逻辑：按 tool_name 查找 Card → 从 ResourceManager 获取实例 → 执行。
func (am *AbilityManager) executeSingleToolCall(
	ctx context.Context,
	toolCall *llmschema.ToolCall,
	sess *session.Session,
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
		return am.executeTool(ctx, toolCall, toolName, toolArgs, sess, tag)
	}
	if _, ok := am.workflows[toolName]; ok {
		return am.executeWorkflow(ctx, toolCall, toolName, toolArgs, sess, tag)
	}
	if _, ok := am.agents[toolName]; ok {
		return am.executeAgent(ctx, toolCall, toolName, toolArgs, sess, tag)
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
	return am.executeFallbackTool(ctx, toolCall, toolName, toolArgs, sess, tag)
}

// executeTool 执行 Tool 类型能力。
func (am *AbilityManager) executeTool(
	ctx context.Context,
	toolCall *llmschema.ToolCall,
	toolName string,
	toolArgs map[string]any,
	sess *session.Session,
	tag string,
) ExecuteResult {
	toolCard := am.tools[toolName]
	toolID := toolCard.ID
	if toolID == "" {
		toolID = toolCard.Name
	}

	var opts []resource.ResourceOption
	if tag != "" {
		opts = append(opts, resource.WithResourceTag(tag))
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

	// 用 LifecycleTool 包装，使 Tool 调用走完整回调链
	// （emit_before → TransformIO → STARTED → [执行] → FINISHED → TransformIO → emit_after）
	// 对齐 Python: _ToolMeta.__call__ 中的自动生命周期注入
	lt := tool.NewLifecycleTool(t)
	result, err := lt.Invoke(ctx, toolArgs, tool.WithToolSession(sess))
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

// executeWorkflow 执行 Workflow 类型能力。
//
// 对齐 Python: AbilityManager._execute_single_tool_call (workflow 分支 L760-775)
// + AbilityManager._run_workflow (L690-726)
// 完整步骤：
//  1. 获取 WorkflowCard（L761-762）
//  2. 从 ResourceManager 获取 workflow 实例（L763-764）
//  3. 创建 workflow session（L707）
//  4. 创建隔离 context（L708-712）
//  5. 通过 Runner.RunWorkflow 执行（L713-718）
//  6. 检测 INPUT_REQUIRED 中断（L719-723）
//  7. 正常完成 — 提取 result（L725）
//  8. 构建 ToolMessage（L726）
func (am *AbilityManager) executeWorkflow(
	ctx context.Context,
	toolCall *llmschema.ToolCall,
	toolName string,
	toolArgs map[string]any,
	sess *session.Session,
	tag string,
) ExecuteResult {
	// 步骤 1：获取 WorkflowCard（对齐 Python L761-762）
	wfCard := am.workflows[toolName]
	wfID := wfCard.ID
	if wfID == "" {
		wfID = wfCard.Name
	}

	// 步骤 2：从 ResourceManager 获取 workflow 实例（对齐 Python L763-764）
	var opts []resource.ResourceOption
	if tag != "" {
		opts = append(opts, resource.WithResourceTag(tag))
	}

	wfAny, err := am.resourceMgr.GetWorkflow(wfID, opts...)
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

	wf, ok := wfAny.(interfaces.Workflow)
	if !ok {
		execErr := NewAbilityExecutionError(
			exception.StatusAbilityNotFound,
			toolCall.ID,
			"工作流实例类型断言失败: "+wfID,
			exception.WithParam("tool_name", toolName),
		)
		return ExecuteResult{Err: execErr, ToolMsg: execErr.ToolMessage}
	}

	// 步骤 3：创建 workflow session（对齐 Python L707: workflow_session = session.create_workflow_session()）
	var workflowSess *session.WorkflowSession
	if sess != nil {
		workflowSess = sess.CreateWorkflowSession()
	}

	// 步骤 4：创建隔离 context（对齐 Python L708-712: workflow_context = context_engine.create_context(...)）
	var wfCtx any
	if am.contextEngine != nil && sess != nil {
		wfCtx, _ = am.contextEngine.CreateContext(ctx, wfID, sess)
	}

	// 步骤 5：通过 Runner.RunWorkflow 执行（对齐 Python L713-718: workflow_output = await Runner.run_workflow(...)）
	result, err := runner.RunWorkflow(ctx, wf, toolArgs, workflowSess, wfCtx)
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

	// 步骤 6：检测 INPUT_REQUIRED 中断（对齐 Python L719-723: if WorkflowOutput.state == INPUT_REQUIRED → return (WorkflowOutput, None)）
	if wfOut, ok := result.(*workflow.WorkflowOutput); ok && wfOut.State == workflow.WorkflowExecutionStateInputRequired {
		return ExecuteResult{Result: wfOut, ToolMsg: nil}
	}

	// 步骤 7：正常完成 — 提取 result（对齐 Python L725: result = workflow_output.result）
	actualResult := result
	if wfOut, ok := result.(*workflow.WorkflowOutput); ok {
		actualResult = wfOut.Result
	}

	// 步骤 8：构建 ToolMessage（对齐 Python L726: ToolMessage(content=str(result))）
	content := BuildToolMessageContent(actualResult)
	toolMsg := llmschema.NewToolMessage(toolCall.ID, content)
	return ExecuteResult{Result: actualResult, ToolMsg: toolMsg}
}

// executeAgent 执行 Agent 类型能力。
//
// 对齐 Python: AbilityManager._execute_single_tool_call (agent 分支 L776-807)
// 完整步骤：
//  1. 获取 AgentCard（L777-778）
//  2. 解析 agent_id（L779）
//  3. 从 ResourceManager 获取 agent 实例（L780-781）
//  4. 构造子会话 ID（L788）
//  5. 注入 conversation_id（L789）
//  6. 创建子会话（L791-794）
//  7. 传播 auto_confirm（L796-798）
//  8. 通过 Runner.RunAgent 执行（L800）
//  9. 构建 ToolMessage（L834-838）
func (am *AbilityManager) executeAgent(
	ctx context.Context,
	toolCall *llmschema.ToolCall,
	toolName string,
	toolArgs map[string]any,
	sess *session.Session,
	tag string,
) ExecuteResult {
	// 步骤 1：获取 AgentCard（对齐 Python L777-778）
	agentCard := am.agents[toolName]

	// 步骤 2：解析 agent_id（对齐 Python L779）
	agentID := agentCard.ID
	if agentID == "" {
		agentID = agentCard.Name
	}

	// 步骤 3：从 ResourceManager 获取 agent 实例（对齐 Python L780-781）
	var opts []resource.ResourceOption
	if tag != "" {
		opts = append(opts, resource.WithResourceTag(tag))
	}

	agAny, err := am.resourceMgr.GetAgent(agentID, opts...)
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

	ag, ok := agAny.(interfaces.BaseAgent)
	if !ok {
		execErr := NewAbilityExecutionError(
			exception.StatusAbilityNotFound,
			toolCall.ID,
			"Agent 实例类型断言失败: "+agentID,
			exception.WithParam("tool_name", toolName),
		)
		return ExecuteResult{Err: execErr, ToolMsg: execErr.ToolMessage}
	}

	// 步骤 4-7：子会话生命周期（仅当 sess 非 nil 时执行）
	var childSession *session.Session
	if sess != nil {
		// 步骤 4：构造子会话 ID（对齐 Python L788: child_session_id = f"{session.id}:{tool_call.id}"）
		childSessionID := fmt.Sprintf("%s:%s", sess.GetSessionID(), toolCall.ID)

		// 步骤 5：注入 conversation_id（对齐 Python L789: tool_args["conversation_id"] = child_session_id）
		toolArgs["conversation_id"] = childSessionID

		// 步骤 6：创建子会话（对齐 Python L791-794: child_session = create_agent_session(...)）
		childSession = session.CreateAgentSession(agentID, childSessionID)

		// 步骤 7：传播 auto_confirm（对齐 Python L796-798）
		autoConfirmVal, _ := sess.GetState(InterruptAutoConfirmKey)
		if autoConfirmVal != nil {
			childSession.UpdateState(map[string]any{InterruptAutoConfirmKey.String(): autoConfirmVal})
		}
	} else {
		// sess 为 nil 时仍需创建子会话（对齐 Python: _prepare_agent 始终创建 session）
		childSession = session.CreateAgentSession(agentID, toolCall.ID)
	}

	// 步骤 8：通过 Runner.RunAgent 执行（对齐 Python L800: result = await Runner.run_agent(agent, inputs, session=child_session)）
	result, err := runner.RunAgent(ctx, ag, toolArgs, childSession)
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

	// 步骤 9：构建 ToolMessage（对齐 Python L834-838）
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
	sess *session.Session,
	tag string,
) ExecuteResult {
	var opts []resource.ResourceOption
	if tag != "" {
		opts = append(opts, resource.WithResourceTag(tag))
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

	// 用 LifecycleTool 包装，使 fallback 路径走完整回调链
	// （emit_before → TransformIO → STARTED → [执行] → FINISHED → TransformIO → emit_after）
	// 对齐 Python: _ToolMeta.__call__ 中的自动生命周期注入
	lt := tool.NewLifecycleTool(t)
	result, invokeErr := lt.Invoke(ctx, toolArgs, tool.WithToolSession(sess))
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
	var paidIdx, freeIdx = -1, -1
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
