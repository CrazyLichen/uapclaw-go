package ability

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"runtime/debug"
	"strings"
	"sync"

	iface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	mcptypes "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/mcp/types"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	resourcesmanager "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/resources_manager"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/rail"
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
// Python: openjiuwen/core/single_agent/ability_manager.py (AbilityManager)
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
	mcpServers map[string]*mcptypes.McpServerConfig
	// contextEngine 上下文引擎
	contextEngine iface.ContextEngine
	// resourceMgr 资源管理器
	resourceMgr *resourcesmanager.ResourceMgr
	// registryRevision 注册表单调递增版本，供 startup 建立的索引复用判断
	registryRevision int
	// progressiveToolEnabled 渐进式工具暴露开关（注册期策略）
	progressiveToolEnabled bool
	// directToolNames 渐进模式下保持直接可调用的工具名集合
	directToolNames map[string]struct{}
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

// logComponent 日志组件标识
const logComponent = logger.ComponentAgentCore

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewAbilityManager 创建 AbilityManager 实例。
func NewAbilityManager(resourceMgr *resourcesmanager.ResourceMgr) *AbilityManager {
	return &AbilityManager{
		tools:       make(map[string]*tool.ToolCard),
		workflows:   make(map[string]*schema.WorkflowCard),
		agents:      make(map[string]*agentschema.AgentCard),
		mcpServers:  make(map[string]*mcptypes.McpServerConfig),
		resourceMgr: resourceMgr,
		// 默认只把 tool_search 视为直通（对齐上游 _direct_tool_names 初值）
		directToolNames: map[string]struct{}{"tool_search": {}},
	}
}

// SetToolExposurePolicy 设置后续注册使用的默认曝光策略。
//
// 已注册卡片不会被改写：曝光是注册期决策，热重配只影响此后注册的卡片。
//
// Python: AbilityManager.set_tool_exposure_policy
func (am *AbilityManager) SetToolExposurePolicy(progressiveToolEnabled bool, directToolNames []string) {
	am.mu.Lock()
	defer am.mu.Unlock()
	am.progressiveToolEnabled = progressiveToolEnabled
	if directToolNames == nil {
		return
	}
	names := make(map[string]struct{}, len(directToolNames))
	for _, name := range directToolNames {
		trimmed := strings.TrimSpace(name)
		if trimmed != "" {
			names[trimmed] = struct{}{}
		}
	}
	am.directToolNames = names
}

// RegistryRevision 返回注册表版本号。
//
// Python: AbilityManager.registry_revision
func (am *AbilityManager) RegistryRevision() int {
	am.mu.RLock()
	defer am.mu.RUnlock()
	return am.registryRevision
}

// applyToolExposurePolicy 在注册期解析并写回卡片曝光策略（调用方须持锁）。
//
// 显式声明优先；否则命中 direct 名单 → direct；否则渐进开启 → deferred。
//
// Python: AbilityManager._apply_tool_exposure_policy
func (am *AbilityManager) applyToolExposurePolicy(card *tool.ToolCard) tool.ToolExposure {
	if declared, ok := card.GetExposureDeclared(); ok && declared {
		return card.EffectiveExposure()
	}
	var resolved tool.ToolExposure
	if _, direct := am.directToolNames[card.Name]; direct {
		resolved = tool.ToolExposureDirect
	} else if am.progressiveToolEnabled {
		resolved = tool.ToolExposureDeferred
	} else {
		resolved = tool.ToolExposureDirect
	}
	card.Exposure = resolved
	card.SetExposureDeclared(true)
	return resolved
}

// SetContextEngine 设置上下文引擎。
func (am *AbilityManager) SetContextEngine(ce iface.ContextEngine) {
	am.contextEngine = ce
}

// Add 添加单个能力。重复 name 时保留已有的，记录 Warn 日志，返回 Added=false。
func (am *AbilityManager) Add(ability schema.Ability) agentschema.AddAbilityResult {
	am.mu.Lock()
	defer am.mu.Unlock()

	switch a := ability.(type) {
	case *tool.ToolCard:
		existing, ok := am.tools[a.Name]
		if ok {
			logger.Warn(logComponent).
				Str("ability_name", a.Name).
				Str("existing_id", existing.ID).
				Str("new_id", a.ID).
				Msg("duplicate tool ability detected, keeping existing, skipping add")
			return agentschema.AddAbilityResult{Name: a.Name, Added: false, Reason: "duplicate_tool"}
		}
		am.applyToolExposurePolicy(a)
		am.tools[a.Name] = a
		am.registryRevision++
		return agentschema.AddAbilityResult{Name: a.Name, Added: true, Reason: "added_tool"}

	case *schema.WorkflowCard:
		existing, ok := am.workflows[a.Name]
		if ok {
			logger.Warn(logComponent).
				Str("ability_name", a.Name).
				Str("existing_id", existing.ID).
				Str("new_id", a.ID).
				Msg("duplicate workflow ability detected, keeping existing, skipping add")
			return agentschema.AddAbilityResult{Name: a.Name, Added: false, Reason: "duplicate_workflow"}
		}
		am.workflows[a.Name] = a
		am.registryRevision++
		return agentschema.AddAbilityResult{Name: a.Name, Added: true, Reason: "added_workflow"}

	case *agentschema.AgentCard:
		existing, ok := am.agents[a.Name]
		if ok {
			logger.Warn(logComponent).
				Str("ability_name", a.Name).
				Str("existing_id", existing.ID).
				Str("new_id", a.ID).
				Msg("duplicate Agent ability detected, keeping existing, skipping add")
			return agentschema.AddAbilityResult{Name: a.Name, Added: false, Reason: "duplicate_agent"}
		}
		am.agents[a.Name] = a
		am.registryRevision++
		return agentschema.AddAbilityResult{Name: a.Name, Added: true, Reason: "added_agent"}

	case *mcptypes.McpServerConfig:
		existing, ok := am.mcpServers[a.ServerName]
		if ok {
			logger.Warn(logComponent).
				Str("ability_name", a.ServerName).
				Str("existing_id", existing.ServerID).
				Str("new_id", a.ServerID).
				Msg("duplicate MCP server ability detected, keeping existing, skipping add")
			return agentschema.AddAbilityResult{Name: a.ServerName, Added: false, Reason: "duplicate_mcp_server"}
		}
		am.mcpServers[a.ServerName] = a
		am.registryRevision++
		return agentschema.AddAbilityResult{Name: a.ServerName, Added: true, Reason: "added_mcp_server"}

	default:
		name := "unknown"
		if a != nil {
			name = ability.AbilityName()
		}
		logger.Warn(logComponent).
			Str("ability_type", fmt.Sprintf("%T", a)).
			Msg("unknown ability type")
		return agentschema.AddAbilityResult{Name: name, Added: false, Reason: "unknown_ability_type"}
	}
}

// AddMany 批量添加能力。
func (am *AbilityManager) AddMany(abilities []schema.Ability) []agentschema.AddAbilityResult {
	results := make([]agentschema.AddAbilityResult, len(abilities))
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
		// 对齐上游：仅 MCP 卸载路径推进注册版本；普通工具移除不推进
		am.registryRevision++
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
func (am *AbilityManager) ListToolInfo(ctx context.Context, names []string, mcpServerName ...string) ([]schema.ToolInfoInterface, error) {
	am.mu.RLock()
	defer am.mu.RUnlock()

	var toolInfos []schema.ToolInfoInterface

	// 1. ToolCards → ToolInfo（工具卡片转工具信息）
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

	// 2. WorkflowCards → ToolInfo（工作流卡片转工具信息）
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

	// 3. AgentCards → ToolInfo（Agent 卡片转工具信息）
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

	// 4. MCP 懒加载：遍历 mcpServers，通过 ResourceMgr 获取 MCP 工具信息，
	//    重命名为 mcp_{serverName}_{toolName}，注册到 am.tools 并追加到结果。
	// Python: AbilityManager.list_tool_info() L506-518
	if am.resourceMgr != nil && len(am.mcpServers) > 0 {
		for mcpServerName, mcpServer := range am.mcpServers {
			mcpServerID := mcpServer.ServerID
			mcpToolInfos, mcpErr := am.resourceMgr.GetMcpToolInfos(ctx, "", mcpServerID)
			if mcpErr != nil {
				logger.Warn(logComponent).
					Str("event_type", "mcp_lazy_load_error").
					Str("server_name", mcpServerName).
					Str("server_id", mcpServerID).
					Err(mcpErr).
					Msg("failed to get MCP tool info")
				continue
			}
			for _, mcpToolInfo := range mcpToolInfos {
				originalName := mcpToolInfo.GetName()
				mcpToolName := "mcp_" + mcpServerName + "_" + originalName
				mcpToolID := mcpServerID + "." + mcpServerName + "." + originalName

				// 创建 ToolCard 并注册到 am.tools
				mcpParams, _ := schema.ParseJSONSchemaMap(mcpToolInfo.GetParameters())
				mcpCard := tool.NewToolCardWithID(mcpToolID, mcpToolName, mcpToolInfo.GetDescription(), mcpParams, nil)
				am.tools[mcpToolName] = mcpCard

				// 重命名 ToolInfo（修改 Name 字段）
				switch ti := mcpToolInfo.(type) {
				case *schema.ToolInfo:
					ti.Name = mcpToolName
				case *schema.McpToolInfo:
					ti.Name = mcpToolName
				}
				toolInfos = append(toolInfos, mcpToolInfo)
			}
		}
	}

	return toolInfos, nil
}

// Execute 并行执行多个 ToolCall，返回每个调用的结果。
// 使用 WaitGroup + 按 index 写切片，与 Python asyncio.gather(return_exceptions=True) 语义一致：
// 所有任务都执行完毕，错误/中断统一在 agentschema.ExecuteResult.Result 中。
// 结果顺序与输入 toolCalls 顺序一致。
//
// cbc 为 Rail 系统的 AgentCallbackContext，用于：
//   - 为每个 tool_call 创建隔离子上下文（ForkForToolCall）
//   - 传播子上下文的 force-finish 信号回父 cbc
//
// Python: AbilityManager.execute(ctx, tool_call, session, tag)
func (am *AbilityManager) Execute(
	ctx context.Context,
	cbc *interfaces.AgentCallbackContext,
	toolCalls []*llmschema.ToolCall,
	sess sessioninterfaces.SessionFacade,
	tag string,
) []agentschema.ExecuteResult {
	if len(toolCalls) == 0 {
		return nil
	}

	// cbc 为 nil 时走降级路径（不使用 Rail 系统，直接执行工具调用）
	if cbc == nil {
		am.mu.RLock()
		results := make([]agentschema.ExecuteResult, len(toolCalls))
		var wg sync.WaitGroup
		for i, tc := range toolCalls {
			wg.Add(1)
			go func(idx int, toolCall *llmschema.ToolCall) {
				defer wg.Done()
				results[idx] = am.runToolCallSafely(ctx, nil, toolCall, sess, tag)
			}(i, tc)
		}
		am.mu.RUnlock()
		wg.Wait()
		return results
	}

	am.mu.RLock()
	results := make([]agentschema.ExecuteResult, len(toolCalls))

	// 为每个 tool_call 创建隔离子上下文
	// Python: tool_ctx = AgentCallbackContext(agent=ctx.agent, inputs=ToolCallInputs(...), extra=ctx.extra, ...)
	toolCtxs := make([]*interfaces.AgentCallbackContext, len(toolCalls))
	for i, tc := range toolCalls {
		toolCtxs[i] = cbc.ForkForToolCall(tc)
	}

	var wg sync.WaitGroup
	for i, tc := range toolCalls {
		wg.Add(1)
		go func(idx int, toolCall *llmschema.ToolCall, toolCtx *interfaces.AgentCallbackContext) {
			defer wg.Done()
			results[idx] = am.runToolCallSafely(ctx, toolCtx, toolCall, sess, tag)
		}(i, tc, toolCtxs[i])
	}
	am.mu.RUnlock()

	wg.Wait()

	// ① 先按 tool-call 顺序一次性收敛子 ctx 上的 force-finish。
	//    ConsumeForceFinish 是清空式消费，每个 index 至多一条，全文唯一写 map 的地方。
	// Python: execute() 末尾 for tool_ctx in tool_contexts 收敛后取 min(force_finish_requests)
	finishByIndex := make(map[int]map[string]any, len(toolCtxs))
	for i := range toolCtxs {
		if ff := toolCtxs[i].ConsumeForceFinish(); ff != nil {
			finishByIndex[i] = ff.Result
		}
	}
	if len(finishByIndex) > 0 {
		cbc.RequestForceFinish(finishByIndex[minFinishIndex(finishByIndex)])
	}

	// ② 结果处理（对齐 Python execute() 的 ①②③④ 分支）
	for i := range results {
		res := &results[i]
		toolCall := toolCalls[i]

		// 错误兜底：ToolMsg 缺失时用 Python 同款文案补（正常错误路径由
		// errorToExecuteResult 产出，此处只兜异常构造遗漏的情形）
		if res.ToolMsg == nil && res.Result != nil {
			if err, ok := res.Result.(error); ok {
				res.ToolMsg = llmschema.NewToolMessage(toolCallIDOf(toolCall),
					"Ability execution error: "+err.Error())
			}
		}

		// before/after 钩子请求了 force_finish：工具可能压根没执行，
		// 必须补一条 ToolMessage，否则 assistant 的 tool_calls 在上下文里悬空，
		// 下一轮请求会因"tool_calls 未被响应"被 provider 拒绝。
		//
		// Python: `result is None and has_force_finish_request` 与
		// `isinstance(result, dict)` 两个分支（Go 侧短路后都是零值，故合并）。
		if ffResult, hasFF := finishByIndex[i]; hasFF && res.ToolMsg == nil {
			res.Result = ffResult
			body, marshalErr := json.Marshal(ffResult)
			if marshalErr != nil {
				body = []byte("{}")
			}
			res.ToolMsg = llmschema.NewToolMessage(toolCallIDOf(toolCall), string(body))
			// inputs 优先（after 钩子可能已写入 tool_result / tool_msg）
			if inputs, ok := toolCtxs[i].Inputs().(*interfaces.ToolCallInputs); ok {
				if inputs.ToolResult != nil {
					res.Result = inputs.ToolResult
				}
				if inputs.ToolMsg != nil {
					res.ToolMsg = inputs.ToolMsg
				}
			}
		}
	}

	return results
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// runToolCallSafely 在独立 goroutine 中执行单个工具调用，并兜底恢复 panic。
//
// 这是工具调用线程的唯一 panic 出口：
//   - 工具实现里的 panic 已在下层 executeSingleToolCall 转成 error（走 rail 的
//     异常路径，AFTER_TOOL_CALL 照常触发）；
//   - 本函数兜的是更外层的缺陷：before/after 钩子、ForkForToolCall 之后的胶水
//     代码、以及 executeSingleToolCall 进入 defer 之前的 nil 解引用（如 toolCall
//     为 nil）。这些 panic 若在 goroutine 里放任不管，整个进程会崩。
//   - raiseInterrupt 用 panic(cb.AbortError{Cause: ToolInterruptException}) 传播
//     中断，必须原样还原成 ToolInterruptException 结果。
//
// cbc 为 nil 时走降级路径（不经过 Rail 包装）。
func (am *AbilityManager) runToolCallSafely(
	ctx context.Context,
	cbc *interfaces.AgentCallbackContext,
	toolCall *llmschema.ToolCall,
	sess sessioninterfaces.SessionFacade,
	tag string,
) (res agentschema.ExecuteResult) {
	defer func() {
		if rec := recover(); rec != nil {
			res = recoverToolPanic(rec, toolCall)
		}
	}()

	if cbc == nil {
		r, err := am.executeSingleToolCall(ctx, toolCall, sess, tag)
		if err != nil {
			return errorToExecuteResult(err, toolCallIDOf(toolCall))
		}
		return r
	}
	return am.railedExecuteSingleToolCall(ctx, cbc, toolCall, sess, tag)
}

// minFinishIndex 返回 force-finish 结果表中最小索引，用于按 tool-call 顺序取首个信号。
//
// Python: min(force_finish_requests)
func minFinishIndex(m map[int]map[string]any) int {
	minIdx := math.MaxInt
	for k := range m {
		if k < minIdx {
			minIdx = k
		}
	}
	return minIdx
}

// recoverToolPanic 把 goroutine 里捕获的 panic 载荷转成 ExecuteResult，并记录日志。
//
// 日志分两级：中断（ToolInterruptException / AbortError）是 raiseInterrupt 与安全
// 护栏的正常控制流，只记 Debug 且不带栈；其余 panic 是缺陷，记 Error 并带调用栈
// ——这层若是静默，日志里只剩「executing tool call」，事后完全无从判断工具崩在哪。
func recoverToolPanic(rec any, toolCall *llmschema.ToolCall) agentschema.ExecuteResult {
	toolCallID := toolCallIDOf(toolCall)
	toolName := ""
	if toolCall != nil {
		toolName = toolCall.Name
	}

	if isInterruptPanic(rec) {
		logger.Debug(logComponent).
			Str("event_type", "tool_interrupt_panic").
			Str("tool_name", toolName).
			Str("tool_call_id", toolCallID).
			Msg("tool call interrupted via panic")
		return errorToExecuteResult(panicToError(rec), toolCallID)
	}

	panicErr := panicToError(rec)
	logger.Error(logComponent).
		Str("event_type", "tool_panic_escalated").
		Str("tool_name", toolName).
		Str("tool_call_id", toolCallID).
		Str("stack", string(debug.Stack())).
		Err(panicErr).
		Msg("tool call panicked outside tool body, converted to error result")
	return errorToExecuteResult(panicErr, toolCallID)
}

// panicToError 把 panic 载荷统一成 error（error 型载荷原样保留以维持
// errors.Is/As 语义，例如 ToolInterruptException）。
func panicToError(rec any) error {
	if e, ok := rec.(error); ok {
		return e
	}
	return fmt.Errorf("工具执行 panic: %v", rec)
}

// isInterruptPanic 判断 panic 载荷是否属于中断控制流。
// Python: raiseInterrupt 抛 ToolInterruptException；Go 用
// panic(cb.AbortError{Cause: ToolInterruptException}) 模拟同样的异常传播。
func isInterruptPanic(rec any) bool {
	e, ok := rec.(error)
	if !ok {
		return false
	}
	var tie *agentschema.ToolInterruptException
	if errors.As(e, &tie) {
		return true
	}
	var abortErr *cb.AbortError
	return errors.As(e, &abortErr)
}

// toolCallIDOf 取 tool_call id，toolCall 为 nil 时返回空串。
func toolCallIDOf(toolCall *llmschema.ToolCall) string {
	if toolCall == nil {
		return ""
	}
	return toolCall.ID
}

// railedExecuteSingleToolCall 在 Rail 生命周期内执行单个工具调用。
//
// 使用 ToolCallRail.Execute 包装，自动提供：
//   - fire(BEFORE_TOOL_CALL) → before 钩子
//   - _skip_tool 门控 → before 钩子可跳过工具执行（设置 extra["_skip_tool"]=true）
//   - force-finish 门控 → before 钩子可终止整个 Agent 循环
//   - 异常 → fire(ON_TOOL_EXCEPTION) → 可 request_retry() 重试
//   - fire(AFTER_TOOL_CALL) → after 钩子
//
// Python: @rail(before=BEFORE_TOOL_CALL, after=AFTER_TOOL_CALL, on_exception=ON_TOOL_EXCEPTION)
//
//	async def _railed_execute_single_tool_call(self, ctx, tool_call, session, tag=None): ...
func (am *AbilityManager) railedExecuteSingleToolCall(
	ctx context.Context,
	toolCtx *interfaces.AgentCallbackContext,
	toolCall *llmschema.ToolCall,
	sess sessioninterfaces.SessionFacade,
	tag string,
) agentschema.ExecuteResult {
	var result agentschema.ExecuteResult

	railErr := rail.ToolCallRail.Execute(ctx, toolCtx, func() error {
		// _skip_tool 门控：before hook 可通过设置 extra["_skip_tool"] = true 来跳过工具执行
		// Python: L664-667: skip_result = ctx.extra.pop("_skip_tool", None)
		// before hook 在设置 _skip_tool 的同时，会在 inputs 中预设 tool_result 和 tool_msg
		// 注意：_skip_tool 判断必须在 toolName/toolArgs 回写之前，skip 时不需要回写
		if skipVal, exists := toolCtx.DeleteExtra("_skip_tool"); exists {
			if skipBool, ok := skipVal.(bool); ok && skipBool {
				if inputs, ok := toolCtx.Inputs().(*interfaces.ToolCallInputs); ok {
					result = agentschema.ExecuteResult{Result: inputs.ToolResult, ToolMsg: inputs.ToolMsg}
				}
				return nil // before hook 正常返回 → 走正常路径 → after 触发
			}
		}

		// before 钩子已执行完毕，将 inputs 中被 before 钩子改写的 ToolName/ToolArgs 写回 toolCall
		// Python: L669-673: if ctx.inputs.tool_name: tool_call.name = ctx.inputs.tool_name
		// Python: L669-673: if ctx.inputs.tool_args is not None: tool_call.arguments = ctx.inputs.tool_args
		if inputs, ok := toolCtx.Inputs().(*interfaces.ToolCallInputs); ok {
			if inputs.ToolName != "" {
				toolCall.Name = inputs.ToolName
			}
			if len(inputs.ToolArgs) > 0 {
				if argsJSON, marshalErr := json.Marshal(inputs.ToolArgs); marshalErr == nil {
					toolCall.Arguments = string(argsJSON)
				}
			}
		}

		var err error
		result, err = am.executeSingleToolCall(ctx, toolCall, sess, tag)

		// 仅 err == nil 时回填结果到 inputs（D6）
		// Python: L681-686：after 钩子触发时可通过 inputs 访问执行结果，也可改写
		if err == nil {
			if inputs, ok := toolCtx.Inputs().(*interfaces.ToolCallInputs); ok {
				inputs.ToolCall = toolCall
				inputs.ToolName = toolCall.Name
				var args map[string]any
				if unmarshalErr := json.Unmarshal([]byte(toolCall.Arguments), &args); unmarshalErr != nil {
					args = map[string]any{}
				}
				inputs.ToolArgs = args
				inputs.ToolResult = result.Result
				inputs.ToolMsg = result.ToolMsg
			}
		}
		return err
	})

	// 统一处理 Rail.Execute 返回的 error
	if railErr != nil {
		// 工具正常返回 error 时这里也会命中（下层 executeTool 已记 tool execution
		// error），故用 Debug：只在排查时开启，避免每次失败工具都刷一条 Error。
		// 未记录时 rail 层自身的失败（on_exception 后仍失败、钩子 return error）
		// 在日志里是完全静默的。
		logger.Debug(logComponent).
			Str("event_type", "tool_rail_error").
			Str("tool_name", toolCall.Name).
			Str("tool_call_id", toolCallIDOf(toolCall)).
			Err(railErr).
			Msg("tool call failed inside rail wrapper")
		return errorToExecuteResult(railErr, toolCallIDOf(toolCall))
	}

	// railErr == nil，从 inputs 读取 after 钩子可能改写的值（D4）
	// Python: L625-638：AFTER_TOOL_CALL rails can rewrite tool_result/tool_msg in ctx.inputs
	if inputs, ok := toolCtx.Inputs().(*interfaces.ToolCallInputs); ok {
		if inputs.ToolResult != nil {
			result.Result = inputs.ToolResult
		}
		if inputs.ToolMsg != nil {
			result.ToolMsg = inputs.ToolMsg
		}
	}
	return result
}

// executeSingleToolCall 执行单个工具调用。
// 路由逻辑：按 tool_name 查找 Card → 从 ResourceManager 获取实例 → 执行。
//
// 返回 (agentschema.ExecuteResult, error)：
//   - agentschema.ExecuteResult 承载正常结果（Result + ToolMsg）
//   - error 承载异常（ToolInterruptException 或 AbilityExecutionError）
//
// 工具实现里的 panic（nil 解引用等）在本函数内统一转成 error 上抛：
// 一旦让 panic 逃出调用方（能力层并行 goroutine / Rail 包装层），
// AFTER_TOOL_CALL 就不会触发，stream_event_rail 不会发 tool_result，
// 前端工具卡片永久停在「执行中」；而会话本身照常结束，用户无从判断。
// 转成 error 后走的是正常异常路径：ON_TOOL_EXCEPTION → AFTER_TOOL_CALL → tool_result。
func (am *AbilityManager) executeSingleToolCall(
	ctx context.Context,
	toolCall *llmschema.ToolCall,
	sess sessioninterfaces.SessionFacade,
	tag string,
) (result agentschema.ExecuteResult, err error) {
	toolName := toolCall.Name

	defer func() {
		rec := recover()
		if rec == nil {
			return
		}
		result = agentschema.ExecuteResult{}
		// 中断控制流（raiseInterrupt / 安全护栏的 cb.AbortError）原样上抛，
		// 由调用方还原成 ToolInterruptException；其余 panic 是缺陷，记 Error。
		if !isInterruptPanic(rec) {
			logger.Error(logComponent).
				Str("event_type", "tool_execution_panic").
				Str("tool_name", toolName).
				Str("tool_call_id", toolCallIDOf(toolCall)).
				Str("stack", string(debug.Stack())).
				Err(panicToError(rec)).
				Msg("tool execution panicked, converted to error")
		}
		err = panicToError(rec)
	}()

	// 解析参数
	toolArgs, parseErr := ParseToolArguments(toolCall.Arguments)
	if parseErr != nil {
		logger.Error(logComponent).
			Str("tool_name", toolName).
			Err(parseErr).
			Msg("tool received malformed parameters")
		execErr := NewAbilityExecutionError(
			exception.StatusAbilityMalformedArguments,
			toolCall.ID,
			parseErr.Error(),
			exception.WithParam("tool_name", toolName),
		)
		return agentschema.ExecuteResult{}, execErr
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
		// MCP 工具正常走 tools 路径（通过懒加载重命名后注册），
		// 命中此分支说明 toolName 恰好等于 serverName（非 mcp_ 前缀），
		// 记录 warning 后走 fallback 尝试查找。
		// Python: AbilityManager._execute_single_tool_call L815-824
		logger.Warn(logComponent).
			Str("event_type", "mcp_tool_direct_server_name_call").
			Str("tool_name", toolName).
			Msg("MCP tool invoke used server_name instead of mcp_ prefixed tool name, trying fallback")
		return am.executeFallbackTool(ctx, toolCall, toolName, toolArgs, sess, tag)
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
	sess sessioninterfaces.SessionFacade,
	tag string,
) (agentschema.ExecuteResult, error) {
	toolCard := am.tools[toolName]
	toolID := toolCard.ID
	if toolID == "" {
		toolID = toolCard.Name
	}

	var opts []resourcesmanager.ResourceOption
	if tag != "" {
		opts = append(opts, resourcesmanager.WithTag(resourcesmanager.Tag(tag)))
	}

	if am.resourceMgr == nil {
		execErr := NewAbilityExecutionError(
			exception.StatusAbilityNotFound,
			toolCall.ID,
			"工具实例未找到: "+toolID,
			exception.WithParam("tool_name", toolName),
		)
		return agentschema.ExecuteResult{}, execErr
	}

	tools, err := am.resourceMgr.GetTool([]string{toolID}, opts...)
	if err != nil || len(tools) == 0 {
		execErr := NewAbilityExecutionError(
			exception.StatusAbilityNotFound,
			toolCall.ID,
			"工具实例未找到: "+toolID,
			exception.WithParam("tool_name", toolName),
			exception.WithCause(err),
		)
		return agentschema.ExecuteResult{}, execErr
	}
	t := tools[0]

	// 用 LifecycleTool 包装，使 Tool 调用走完整回调链
	// （emit_before → TransformIO → STARTED → [执行] → FINISHED → TransformIO → emit_after）
	// Python: _ToolMeta.__call__ 中的自动生命周期注入
	lt := tool.NewLifecycleTool(t)
	result, err := lt.Invoke(ctx, toolArgs, tool.WithToolSession(sess))
	if err != nil {
		// 扩展：tool 本身返回 ToolInterruptException → 直接透传
		var tie *agentschema.ToolInterruptException
		if errors.As(err, &tie) {
			return agentschema.ExecuteResult{}, tie
		}
		logger.Error(logComponent).
			Str("tool_name", toolName).
			Err(err).
			Msg("tool execution error")
		execErr := NewAbilityExecutionError(
			exception.StatusAbilityExecutionError,
			toolCall.ID,
			"工具执行错误: "+err.Error(),
			exception.WithParam("tool_name", toolName),
			exception.WithCause(err),
		)
		return agentschema.ExecuteResult{}, execErr
	}

	content := BuildToolMessageContent(result)
	toolMsg := llmschema.NewToolMessage(toolCall.ID, content)
	return agentschema.ExecuteResult{Result: result, ToolMsg: toolMsg}, nil
}

// executeWorkflow 执行 Workflow 类型能力。
//
// Python: AbilityManager._execute_single_tool_call (workflow 分支 L760-775)
// Python: AbilityManager._run_workflow (L690-726)
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
	sess sessioninterfaces.SessionFacade,
	tag string,
) (agentschema.ExecuteResult, error) {
	// 步骤 1：获取 WorkflowCard（对齐 Python L761-762）
	wfCard := am.workflows[toolName]
	wfID := wfCard.ID
	if wfID == "" {
		wfID = wfCard.Name
	}

	// 步骤 2：从 ResourceManager 获取 workflow 实例（对齐 Python L763-764）
	var opts []resourcesmanager.ResourceOption
	if tag != "" {
		opts = append(opts, resourcesmanager.WithTag(resourcesmanager.Tag(tag)))
	}

	if am.resourceMgr == nil {
		execErr := NewAbilityExecutionError(
			exception.StatusAbilityNotFound,
			toolCall.ID,
			"工作流实例未找到: "+wfID,
			exception.WithParam("tool_name", toolName),
		)
		return agentschema.ExecuteResult{}, execErr
	}

	wfs, err := am.resourceMgr.GetWorkflow(ctx, []string{wfID}, opts...)
	if err != nil || len(wfs) == 0 {
		execErr := NewAbilityExecutionError(
			exception.StatusAbilityNotFound,
			toolCall.ID,
			"工作流实例未找到: "+wfID,
			exception.WithParam("tool_name", toolName),
			exception.WithCause(err),
		)
		return agentschema.ExecuteResult{}, execErr
	}

	wf := wfs[0]

	// 步骤 3：创建 workflow session（对齐 Python L707: workflow_session = session.create_workflow_session()）
	var workflowSess *session.WorkflowSession
	if sess != nil {
		// 创建 workflow session 需要断言 *session.Session 具体类型
		if agentSess, ok := sess.(*session.Session); ok {
			workflowSess = agentSess.CreateWorkflowSession()
		}
	}

	// 步骤 4：创建隔离 context（对齐 Python L708-712: workflow_context = context_engine.create_context(...)）
	var wfCtx any
	if am.contextEngine != nil && sess != nil {
		wfCtx, _ = am.contextEngine.CreateContext(ctx, wfID, sess)
	}

	// 步骤 5：通过 Runner.RunWorkflow 执行（对齐 Python L713-718: workflow_output = await Runner.run_workflow(...)）
	result, err := runner.RunWorkflow(ctx, runner.ByWorkflow(wf), toolArgs, workflowSess, wfCtx, nil)
	if err != nil {
		logger.Error(logComponent).
			Str("workflow_name", toolName).
			Err(err).
			Msg("workflow execution error")
		execErr := NewAbilityExecutionError(
			exception.StatusAbilityExecutionError,
			toolCall.ID,
			"工作流执行错误: "+err.Error(),
			exception.WithParam("tool_name", toolName),
			exception.WithCause(err),
		)
		return agentschema.ExecuteResult{}, execErr
	}

	// 步骤 6：检测 INPUT_REQUIRED 中断（对齐 Python L719-723: if WorkflowOutput.state == INPUT_REQUIRED → return (WorkflowOutput, None)）
	if wfOut, ok := result.(*workflow.WorkflowOutput); ok && wfOut.State == workflow.WorkflowExecutionStateInputRequired {
		return agentschema.ExecuteResult{Result: wfOut, ToolMsg: nil}, nil
	}

	// 步骤 7：正常完成 — 提取 result（对齐 Python L725: result = workflow_output.result）
	actualResult := result
	if wfOut, ok := result.(*workflow.WorkflowOutput); ok {
		actualResult = wfOut.Result
	}

	// 步骤 8：构建 ToolMessage（对齐 Python L726: ToolMessage(content=str(result))）
	content := BuildToolMessageContent(actualResult)
	toolMsg := llmschema.NewToolMessage(toolCall.ID, content)
	return agentschema.ExecuteResult{Result: actualResult, ToolMsg: toolMsg}, nil
}

// executeAgent 执行 Agent 类型能力。
//
// Python: AbilityManager._execute_single_tool_call (agent 分支 L776-807)
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
	sess sessioninterfaces.SessionFacade,
	tag string,
) (agentschema.ExecuteResult, error) {
	// 步骤 1：获取 AgentCard（对齐 Python L777-778）
	agentCard := am.agents[toolName]

	// 步骤 2：解析 agent_id（对齐 Python L779）
	agentID := agentCard.ID
	if agentID == "" {
		agentID = agentCard.Name
	}

	// 步骤 3：从 ResourceManager 获取 agent 实例（对齐 Python L780-781）
	var opts []resourcesmanager.ResourceOption
	if tag != "" {
		opts = append(opts, resourcesmanager.WithTag(resourcesmanager.Tag(tag)))
	}

	if am.resourceMgr == nil {
		execErr := NewAbilityExecutionError(
			exception.StatusAbilityNotFound,
			toolCall.ID,
			"Agent 实例未找到: "+agentID,
			exception.WithParam("tool_name", toolName),
		)
		return agentschema.ExecuteResult{}, execErr
	}

	ags, err := am.resourceMgr.GetAgent(ctx, []string{agentID}, opts...)
	if err != nil || len(ags) == 0 {
		execErr := NewAbilityExecutionError(
			exception.StatusAbilityNotFound,
			toolCall.ID,
			"Agent 实例未找到: "+agentID,
			exception.WithParam("tool_name", toolName),
			exception.WithCause(err),
		)
		return agentschema.ExecuteResult{}, execErr
	}

	ag := ags[0]

	// 步骤 4-7：子会话生命周期（仅当 sess 非 nil 时执行）
	var childSession *session.Session
	if sess != nil {
		// 步骤 4：构造子会话 ID（对齐 Python L788: child_session_id = f"{session.id}:{tool_call.id}"）
		childSessionID := fmt.Sprintf("%s:%s", sess.GetSessionID(), toolCall.ID)

		// 步骤 5：注入 conversation_id（对齐 Python L789: tool_args["conversation_id"] = child_session_id）
		toolArgs["conversation_id"] = childSessionID

		// 步骤 6：创建子会话（对齐 Python L791-794: child_session = create_agent_session(session_id=..., card=agent.card)）
		childSession = session.CreateAgentSession(childSessionID, ag.Card(), nil)

		// 步骤 7：传播 auto_confirm（对齐 Python L796-798）
		autoConfirmVal, _ := sess.GetState(InterruptAutoConfirmKey)
		if autoConfirmVal != nil {
			childSession.UpdateState(map[string]any{InterruptAutoConfirmKey.String(): autoConfirmVal})
		}
	} else {
		// sess 为 nil 时仍需创建子会话（对齐 Python: _prepare_agent 始终创建 session）
		childSession = session.CreateAgentSession(toolCall.ID, ag.Card(), nil)
	}

	// 步骤 8：通过 Runner.RunAgent 执行（对齐 Python L800: result = await Runner.run_agent(agent, inputs, session=child_session)）
	result, err := runner.RunAgent(ctx, runner.ByAgent(ag), toolArgs, runner.BySession(childSession), nil, nil)
	if err != nil {
		logger.Error(logComponent).
			Str("agent_name", toolName).
			Err(err).
			Msg("Agent execution error")
		execErr := NewAbilityExecutionError(
			exception.StatusAbilityExecutionError,
			toolCall.ID,
			"Agent 执行错误: "+err.Error(),
			exception.WithParam("tool_name", toolName),
			exception.WithCause(err),
		)
		return agentschema.ExecuteResult{}, execErr
	}

	// 步骤 9：构建 ToolMessage（对齐 Python L834-838）
	content := BuildToolMessageContent(result)
	toolMsg := llmschema.NewToolMessage(toolCall.ID, content)
	return agentschema.ExecuteResult{Result: result, ToolMsg: toolMsg}, nil
}

// executeFallbackTool 兜底：从 ResourceManager 按 name 获取 Tool。
func (am *AbilityManager) executeFallbackTool(
	ctx context.Context,
	toolCall *llmschema.ToolCall,
	toolName string,
	toolArgs map[string]any,
	sess sessioninterfaces.SessionFacade,
	tag string,
) (agentschema.ExecuteResult, error) {
	var opts []resourcesmanager.ResourceOption
	if tag != "" {
		opts = append(opts, resourcesmanager.WithTag(resourcesmanager.Tag(tag)))
	}

	if am.resourceMgr == nil {
		execErr := NewAbilityExecutionError(
			exception.StatusAbilityNotFound,
			toolCall.ID,
			"能力未找到: "+toolName,
			exception.WithParam("ability_name", toolName),
		)
		return agentschema.ExecuteResult{}, execErr
	}

	tools, err := am.resourceMgr.GetTool([]string{toolName}, opts...)
	if err != nil || len(tools) == 0 {
		execErr := NewAbilityExecutionError(
			exception.StatusAbilityNotFound,
			toolCall.ID,
			"能力未找到: "+toolName,
			exception.WithParam("ability_name", toolName),
			exception.WithCause(err),
		)
		return agentschema.ExecuteResult{}, execErr
	}
	t := tools[0]

	// 用 LifecycleTool 包装，使 fallback 路径走完整回调链
	// （emit_before → TransformIO → STARTED → [执行] → FINISHED → TransformIO → emit_after）
	// Python: _ToolMeta.__call__ 中的自动生命周期注入
	lt := tool.NewLifecycleTool(t)
	result, invokeErr := lt.Invoke(ctx, toolArgs, tool.WithToolSession(sess))
	if invokeErr != nil {
		// 扩展：tool 本身返回 ToolInterruptException → 直接透传
		var tie *agentschema.ToolInterruptException
		if errors.As(invokeErr, &tie) {
			return agentschema.ExecuteResult{}, tie
		}
		logger.Error(logComponent).
			Str("tool_name", toolName).
			Err(invokeErr).
			Msg("tool execution error")
		execErr := NewAbilityExecutionError(
			exception.StatusAbilityExecutionError,
			toolCall.ID,
			"工具执行错误: "+invokeErr.Error(),
			exception.WithParam("tool_name", toolName),
			exception.WithCause(invokeErr),
		)
		return agentschema.ExecuteResult{}, execErr
	}

	content := BuildToolMessageContent(result)
	toolMsg := llmschema.NewToolMessage(toolCall.ID, content)
	return agentschema.ExecuteResult{Result: result, ToolMsg: toolMsg}, nil
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
