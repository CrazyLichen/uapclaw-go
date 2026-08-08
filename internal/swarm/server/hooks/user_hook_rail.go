package hooks

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	hookscfg "github.com/uapclaw/uapclaw-go/internal/common/hooks"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// UserHookRail 用户配置的 hooks 执行引擎，对齐 Python UserHookRail(DeepAgentRail, priority=60)
// 将用户配置的 hooks 以 Rail 形态注册到 DeepAgent，拦截工具调用和 Agent 生命周期
// Priority=60: 在 SecurityRail(80) 之后，JiuClawStreamEventRail(50) 之前
type UserHookRail struct {
	rails.DeepAgentRail
	// config hooks 配置
	config hookscfg.HooksConfig
	// executor hook 执行器
	executor *HookExecutor
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewUserHookRail 创建 UserHookRail，对齐 Python UserHookRail.__init__(hooks_config)
func NewUserHookRail(config hookscfg.HooksConfig, executor *HookExecutor) *UserHookRail {
	r := &UserHookRail{
		config:   config,
		executor: executor,
	}
	// 对齐 Python: priority=60
	base := agentinterfaces.NewBaseRail().WithPriority(60)
	r.DeepAgentRail = rails.DeepAgentRail{BaseRail: *base}
	return r
}

// BeforeToolCall 对齐 Python before_tool_call → HookEvent.PRE_TOOL_USE
// 阻塞时 → cbc.Extra()["_skip_tool"]=true, cbc.Extra()["_hook_feedback"]=error
// 修改输入时 → 修改 cbc.Inputs.ToolArgs/ToolName
// additionalContext → cbc.Extra()["_hook_additional_context"] 追加
func (r *UserHookRail) BeforeToolCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	toolInputs := cbc.Inputs().(*agentinterfaces.ToolCallInputs)
	toolName := toolInputs.ToolName

	// 对齐 Python: hook_configs = self._config.match(HookEvent.PRE_TOOL_USE.value, query=tool_name)
	hookConfigs := r.config.Match(hookscfg.HookEventPreToolUse, toolName)
	if len(hookConfigs) == 0 {
		return nil
	}

	sessionID := getSessionID(cbc)
	// 对齐 Python: hook_input={"event": "PreToolUse", "tool_name": tool_name, "tool_input": tool_args, "session_id": ...}
	hookInput := map[string]any{
		"event":      "PreToolUse",
		"tool_name":  toolName,
		"tool_input": toolInputs.ToolArgs,
		"session_id": sessionID,
	}

	// 对齐 Python: results = await self._executor.run_all(hook_configs, hook_input=hook_input)
	results := r.executor.RunAll(ctx, hookConfigs, hookInput, sessionID)

	for _, result := range results {
		if result.Outcome == HookOutcomeBlocking {
			// 对齐 Python: ctx.extra["_skip_tool"] = True; ctx.extra["_hook_feedback"] = r.error
			cbc.Extra()["_skip_tool"] = true
			cbc.Extra()["_hook_feedback"] = result.Error
			logger.Info(logComponent).Str("tool_name", toolName).Str("reason", result.Error).Msg("UserHookRail: PreToolUse BLOCKED")
			return nil
		}
		if result.ModifiedInput != nil {
			// 对齐 Python: ctx.inputs.tool_args = r.modified_input（整个 dict 赋值给 tool_args）
			jsonBytes, _ := json.Marshal(result.ModifiedInput)
			toolInputs.ToolArgs = string(jsonBytes)
			// 对齐 Python: new_name = r.modified_input.get("_tool_name")
			if newName, ok := result.ModifiedInput["_tool_name"]; ok {
				if s, ok := newName.(string); ok && s != "" {
					toolInputs.ToolName = s
					logger.Info(logComponent).Str("tool_name", toolName).Str("new_name", s).Msg("UserHookRail: PreToolUse modified tool name")
				}
			}
			logger.Info(logComponent).Str("tool_name", toolName).Msg("UserHookRail: PreToolUse modified input")
		}
		if result.AdditionalContext != "" {
			// 对齐 Python: existing = ctx.extra.get("_hook_additional_context", "")
			existing, _ := cbc.Extra()["_hook_additional_context"].(string)
			if existing != "" {
				cbc.Extra()["_hook_additional_context"] = existing + "\n" + result.AdditionalContext
			} else {
				cbc.Extra()["_hook_additional_context"] = result.AdditionalContext
			}
		}
	}
	return nil
}

// AfterToolCall 对齐 Python after_tool_call → HookEvent.POST_TOOL_USE
// 阻塞时 → cbc.Extra()["_post_tool_hook_feedback"]=error
// additionalContext → 拼接到 cbc.Inputs.ToolResult
func (r *UserHookRail) AfterToolCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	toolInputs := cbc.Inputs().(*agentinterfaces.ToolCallInputs)
	toolName := toolInputs.ToolName

	// 对齐 Python: hook_configs = self._config.match(HookEvent.POST_TOOL_USE.value, query=tool_name)
	hookConfigs := r.config.Match(hookscfg.HookEventPostToolUse, toolName)
	if len(hookConfigs) == 0 {
		return nil
	}

	sessionID := getSessionID(cbc)
	// 对齐 Python: hook_input={"event": "PostToolUse", "tool_name": ..., "tool_input": ..., "tool_result": ..., "session_id": ...}
	hookInput := map[string]any{
		"event":       "PostToolUse",
		"tool_name":   toolName,
		"tool_input":  toolInputs.ToolArgs,
		"tool_result": toolInputs.ToolResult,
		"session_id":  sessionID,
	}

	results := r.executor.RunAll(ctx, hookConfigs, hookInput, sessionID)

	for _, result := range results {
		if result.Outcome == HookOutcomeBlocking {
			// 对齐 Python: ctx.extra["_post_tool_hook_feedback"] = r.error
			cbc.Extra()["_post_tool_hook_feedback"] = result.Error
			logger.Info(logComponent).Str("tool_name", toolName).Str("reason", result.Error).Msg("UserHookRail: PostToolUse BLOCKED")
		}
		if result.AdditionalContext != "" {
			// 对齐 Python: current = ctx.inputs.tool_result or ""; ctx.inputs.tool_result = current + "\n[Hook 发现]: " + r.additional_context
			var current string
			if toolInputs.ToolResult != nil {
				if s, ok := toolInputs.ToolResult.(string); ok {
					current = s
				} else {
					// 非 string 类型，JSON 序列化保底
					jsonBytes, _ := json.Marshal(toolInputs.ToolResult)
					current = string(jsonBytes)
				}
			}
			// 对齐 Python: current + "\n[Hook 发现]: " + r.additional_context（统一拼接，不区分空/非空）
			toolInputs.ToolResult = current + "\n[Hook 发现]: " + result.AdditionalContext
		}
	}
	return nil
}

// OnToolException 对齐 Python on_tool_exception → HookEvent.POST_TOOL_USE_FAILURE
// 仅通知收集（不改变处理流程）
func (r *UserHookRail) OnToolException(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	toolInputs := cbc.Inputs().(*agentinterfaces.ToolCallInputs)
	toolName := toolInputs.ToolName

	// 对齐 Python: hook_configs = self._config.match(HookEvent.POST_TOOL_USE_FAILURE.value, query=tool_name)
	hookConfigs := r.config.Match(hookscfg.HookEventPostToolUseFailure, toolName)
	if len(hookConfigs) == 0 {
		return nil
	}

	sessionID := getSessionID(cbc)
	// 对齐 Python: hook_input={"event": "PostToolUseFailure", "tool_name": ..., "tool_input": ..., "error": ..., "session_id": ...}
	hookInput := map[string]any{
		"event":      "PostToolUseFailure",
		"tool_name":  toolName,
		"tool_input": toolInputs.ToolArgs,
		"error":      fmt.Sprintf("%v", cbc.Exception()),
		"session_id": sessionID,
	}

	// 仅执行，不改变流程，对齐 Python: await self._executor.run_all(...)
	r.executor.RunAll(ctx, hookConfigs, hookInput, sessionID)
	return nil
}

// AfterInvoke 对齐 Python after_invoke → HookEvent.STOP
// 阻塞时 → cbc.Extra()["_stop_hook_feedback"]=error
func (r *UserHookRail) AfterInvoke(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	// 对齐 Python: hook_configs = self._config.match(HookEvent.STOP.value)
	hookConfigs := r.config.Match(hookscfg.HookEventStop, "")
	if len(hookConfigs) == 0 {
		return nil
	}

	sessionID := getSessionID(cbc)
	// 对齐 Python: hook_input={"event": "Stop", "final_response": ..., "session_id": ...}
	var finalResponse any
	if invokeInputs, ok := cbc.Inputs().(*agentinterfaces.InvokeInputs); ok {
		finalResponse = invokeInputs.Result
	}

	hookInput := map[string]any{
		"event":          "Stop",
		"final_response": finalResponse,
		"session_id":     sessionID,
	}

	results := r.executor.RunAll(ctx, hookConfigs, hookInput, sessionID)

	for _, result := range results {
		if result.Outcome == HookOutcomeBlocking {
			// 对齐 Python: ctx.extra["_stop_hook_feedback"] = r.error
			cbc.Extra()["_stop_hook_feedback"] = result.Error
			reason := result.Error
			if len(reason) > 200 {
				reason = reason[:200]
			}
			logger.Info(logComponent).Str("reason", reason).Msg("UserHookRail: Stop hook feedback")
		}
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// getSessionID 从 cbc 获取 session_id
func getSessionID(cbc *agentinterfaces.AgentCallbackContext) string {
	if cbc.Session() != nil {
		return cbc.Session().GetSessionID()
	}
	return ""
}
