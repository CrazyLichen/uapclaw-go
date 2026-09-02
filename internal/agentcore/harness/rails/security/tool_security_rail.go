package security

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/interrupt"
	harnesssecurity "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/security"
	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	sessioninteraction "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interaction"
	sessionstate "github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// PermissionInterruptRail 工具权限中断护栏。
//
// 对**任意**工具名执行 before_tool_call 权限判定（不再按工具名子集短路跳过）。
// 可选 toolNames 仅传给基类作 GetTools 展示；不参与是否拦截。
//
// - ALLOW → 继续执行
// - DENY → 拒绝并返回 [PERMISSION_DENIED] 消息
// - ASK  → 中断等待用户确认（ConfirmPayload schema）
//
// Auto-confirm 存储在 session 状态 (INTERRUPT_AUTO_CONFIRM_KEY)。
// 支持 bash 类工具的细粒度 auto-confirm key（如 bash:ls）。
//
// 对齐 Python: PermissionInterruptRail(ConfirmInterruptRail) — tool_security_rail.py L52-666
type PermissionInterruptRail struct {
	interrupt.BaseInterruptRail
	// staticConfig 静态权限配置
	staticConfig map[string]any
	// engine 权限引擎
	engine *harnesssecurity.PermissionEngine
	// host 权限宿主回调
	host *harnesssecurity.ToolPermissionHost
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// permissionInterruptRailPriority 权限中断 Rail 优先级
	// 对齐 Python: PermissionInterruptRail.priority = 90
	permissionInterruptRailPriority = 90
)

// ──────────────────────────── 全局变量 ────────────────────────────

// 编译时验证 PermissionInterruptRail 满足 AgentRail 接口
var _ agentinterfaces.AgentRail = (*PermissionInterruptRail)(nil)

// toolNameAliases 工具名别名映射
// 对齐 Python: TOOL_NAME_ALIASES (tool_security_rail.py L44-49)
var toolNameAliases = map[string]string{
	"free_search":   "mcp_free_search",
	"paid_search":   "mcp_paid_search",
	"fetch_webpage": "mcp_fetch_webpage",
	"exec_command":  "mcp_exec_command",
}

var permRailLogComponent = logger.ComponentAgentCore

// ──────────────────────────── 导出函数 ────────────────────────────

// NewPermissionInterruptRail 创建权限中断护栏实例。
//
// 对齐 Python: PermissionInterruptRail.__init__(config, engine, tool_names, llm, model_name, host)
func NewPermissionInterruptRail(
	config map[string]any,
	engine *harnesssecurity.PermissionEngine,
	toolNames []string,
	host *harnesssecurity.ToolPermissionHost,
) *PermissionInterruptRail {
	r := &PermissionInterruptRail{
		BaseInterruptRail: *interrupt.NewBaseInterruptRail(toolNames...),
		staticConfig:      config,
		host:              host,
	}

	if r.staticConfig == nil {
		r.staticConfig = make(map[string]any)
	}
	if r.host == nil {
		r.host = &harnesssecurity.ToolPermissionHost{}
	}

	// 创建或使用传入的 PermissionEngine
	if engine != nil {
		r.engine = engine
	} else {
		var workspaceRoot string
		if r.host.ResolveWorkspaceDir != nil {
			if ws, err := func() (string, error) { return r.host.ResolveWorkspaceDir(), nil }(); err == nil {
				workspaceRoot = ws
			} else {
				logger.Debug(permRailLogComponent).
					Err(err).
					Msg("permission.rail.workspace_resolve_failed")
			}
		}
		r.engine = harnesssecurity.NewPermissionEngine(r.staticConfig, workspaceRoot)
	}

	// 宿主级权限校验活跃检查
	if r.host.ToolPermissionChecksActive != nil {
		r.engine.SetPermissionChecksActive(r.host.ToolPermissionChecksActive)
	}

	// 设置 ResolveInterruptFn
	r.ResolveInterruptFn = r.resolvePermissionInterrupt

	r.WithPriority(permissionInterruptRailPriority)

	// 对齐 Python: logger.info
	toolsKeys := make([]string, 0)
	if tools, ok := r.staticConfig["tools"]; ok {
		if m, ok := tools.(map[string]any); ok {
			for k := range m {
				toolsKeys = append(toolsKeys, k)
			}
		}
	}
	sortedToolTags := make([]string, 0, len(toolNames))
	copy(sortedToolTags, toolNames)
	sort.Strings(sortedToolTags)
	sort.Strings(toolsKeys)

	logger.Info(permRailLogComponent).
		Strs("optional_tool_tags", sortedToolTags).
		Strs("tools_keys", toolsKeys).
		Msg("permission.rail.init intercept=all_tools")

	return r
}

// BeforeToolCall 工具调用前拦截。
// 对齐 Python: PermissionInterruptRail.before_tool_call — 对**任意**工具名执行权限判定，
// 不再按工具名子集短路跳过（与基类 BaseInterruptRail 不同）。
//
// 对齐 Python: PermissionInterruptRail.before_tool_call(ctx) (tool_security_rail.py L159-186)
func (r *PermissionInterruptRail) BeforeToolCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	toolInputs, ok := cbc.Inputs().(*agentinterfaces.ToolCallInputs)
	if !ok {
		return nil
	}

	toolName := toolInputs.ToolName
	normalizedName := r.normalizeToolName(toolName)

	logger.Info(permRailLogComponent).
		Str("tool", toolName).
		Str("normalized", normalizedName).
		Msg("permission.rail.before_tool_call")

	toolCallID := r.resolveToolCallID(toolInputs.ToolCall)
	userInput := r.getUserInput(cbc, toolCallID)

	// 从 session 获取 auto_confirm 配置
	var autoConfirmConfig map[string]any
	if sess := cbc.Session(); sess != nil {
		if val, err := sess.GetState(sessionstate.StringKey(saschema.InterruptAutoConfirmKey)); err == nil && val != nil {
			if cfg, ok := val.(map[string]any); ok {
				autoConfirmConfig = cfg
			}
		}
	}

	decision := r.ResolveInterruptFn(ctx, cbc, toolInputs.ToolCall, userInput, autoConfirmConfig)
	cbc.Extra()["_interrupt_decision"] = decision
	r.applyDecision(cbc, toolInputs, decision)

	return nil
}

// GetCallbacks 覆盖基类回调映射，注册 BeforeToolCall 回调。
func (r *PermissionInterruptRail) GetCallbacks() map[agentinterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
	callbacks := r.BaseRail.GetCallbacks()
	callbacks[agentinterfaces.CallbackBeforeToolCall] = func(ctx context.Context, railCtx any) error {
		return r.BeforeToolCall(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}
	return callbacks
}

// UpdateConfig 热更新静态权限配置；可选 toolNames 仅更新基类标签集合。
//
// 对齐 Python: PermissionInterruptRail.update_config(config, tool_names) (tool_security_rail.py L188-204)
func (r *PermissionInterruptRail) UpdateConfig(config map[string]any, toolNames []string) {
	if config == nil {
		config = make(map[string]any)
	}
	r.staticConfig = config
	r.engine.UpdateConfig(config)
	if r.host.ToolPermissionChecksActive != nil {
		r.engine.SetPermissionChecksActive(r.host.ToolPermissionChecksActive)
	}
	if toolNames != nil {
		newToolNames := make(map[string]struct{}, len(toolNames))
		for _, name := range toolNames {
			name = strings.TrimSpace(name)
			if name != "" {
				newToolNames[name] = struct{}{}
			}
		}
		// 通过 AddTools 更新基类标签集合
		r.AddTools(toolNames)
		_ = newToolNames // toolNames 已通过 AddTools 更新
	}
	sortedTags := r.GetTools()
	sort.Strings(sortedTags)
	logger.Info(permRailLogComponent).
		Strs("optional_tool_tags", sortedTags).
		Msg("permission.rail.config_updated intercept=all_tools")
}

// Engine 返回内部 PermissionEngine（用于测试）
func (r *PermissionInterruptRail) Engine() *harnesssecurity.PermissionEngine {
	return r.engine
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// normalizeToolName 归一化工具名，使用别名映射。
//
// 对齐 Python: PermissionInterruptRail._normalize_tool_name(tool_name) (tool_security_rail.py L109-114)
func (r *PermissionInterruptRail) normalizeToolName(toolName string) string {
	if alias, ok := toolNameAliases[toolName]; ok {
		return alias
	}
	return toolName
}

// getAutoConfirmKey 生成保守的 session auto-confirm key。
// 对于 bash/mcp_exec_command/create_terminal，使用 Shell AST 解析获取细粒度 key。
//
// 对齐 Python: PermissionInterruptRail._get_auto_confirm_key(tool_call) (tool_security_rail.py L116-128)
func (r *PermissionInterruptRail) getAutoConfirmKey(toolCall *llmschema.ToolCall) string {
	if toolCall == nil {
		return ""
	}
	toolName := toolCall.Name
	if toolName == "" {
		return ""
	}
	toolArgs := parseToolArgs(toolCall)

	if toolName == "bash" || toolName == "mcp_exec_command" || toolName == "create_terminal" {
		cmd := ""
		if v, ok := toolArgs["command"]; ok {
			cmd = fmt.Sprintf("%v", v)
		} else if v, ok := toolArgs["cmd"]; ok {
			cmd = fmt.Sprintf("%v", v)
		}
		return buildShellAutoConfirmKey(toolName, cmd)
	}

	return toolName
}

// buildShellAutoConfirmKey 通过 Shell AST 解析构建细粒度 auto-confirm key。
//
// 对齐 Python: PermissionInterruptRail._build_shell_auto_confirm_key(tool_name, command) (tool_security_rail.py L130-147)
func buildShellAutoConfirmKey(toolName string, command string) string {
	text := strings.TrimSpace(command)
	if text == "" {
		return ""
	}

	shellAstResult := harnesssecurity.ParseShellForPermission(text)
	if shellAstResult.Kind != harnesssecurity.ShellAstKindSimple {
		return ""
	}
	if shellAstResult.Flags.HasRiskyStructure() {
		return ""
	}
	if len(shellAstResult.Subcommands) != 1 {
		return ""
	}

	subcommand := strings.TrimSpace(shellAstResult.Subcommands[0].Text)
	if subcommand == "" {
		return ""
	}
	return fmt.Sprintf("%s:%s", toolName, subcommand)
}

// shouldStoreAutoConfirm 判断是否应存储 auto-confirm key 到 session。
//
// 对齐 Python: PermissionInterruptRail._should_store_auto_confirm(...) (tool_security_rail.py L149-157)
func shouldStoreAutoConfirm(autoConfirm bool, session any, autoConfirmKey string, persisted bool) bool {
	return autoConfirm && session != nil && autoConfirmKey != "" && !persisted
}

// resolvePermissionInterrupt 权限中断解析核心逻辑。
//
// 对齐 Python: PermissionInterruptRail.resolve_interrupt(ctx, tool_call, user_input, auto_confirm_config) (tool_security_rail.py L288-504)
func (r *PermissionInterruptRail) resolvePermissionInterrupt(
	_ context.Context,
	cbc *agentinterfaces.AgentCallbackContext,
	toolCall *llmschema.ToolCall,
	userInput any,
	autoConfirmConfig map[string]any,
) interrupt.InterruptDecision {
	toolName := ""
	if toolCall != nil {
		toolName = toolCall.Name
	}
	normalizedName := r.normalizeToolName(toolName)
	toolArgs := parseToolArgs(toolCall)
	autoConfirmKey := r.getAutoConfirmKey(toolCall)

	logger.Info(permRailLogComponent).
		Str("tool", toolName).
		Str("normalized", normalizedName).
		Interface("auto_confirm_key", autoConfirmKey).
		Str("user_input_type", fmt.Sprintf("%T", userInput)).
		Msg("permission.rail.resolve")

	// ── 1. PermissionSceneHook 短路 ──
	if r.host.PermissionSceneHook != nil {
		sceneOut, err := r.host.PermissionSceneHook(harnesssecurity.PermissionSceneHookInput{
			Ctx:                cbc,
			ToolCall:           toolCall,
			UserInput:          userInput,
			NormalizedToolName: normalizedName,
			ToolArgs:           toolArgs,
			Engine:             r.engine,
		})
		if err != nil {
			logger.Warn(permRailLogComponent).
				Err(err).
				Msg("permission.scene_hook.failed")
		} else if sceneOut != nil && len(sceneOut) > 0 {
			switch sceneOut[0] {
			case "approve":
				return r.Approve("")
			case "reject":
				msg := "[PERMISSION_DENIED]"
				if len(sceneOut) > 1 {
					msg = sceneOut[1]
				}
				return r.Reject(msg)
			}
		}
	}

	// ── 2. 首次检查（userInput == nil） ──
	if userInput == nil {
		logger.Info(permRailLogComponent).
			Str("tool", toolName).
			Str("normalized", normalizedName).
			Msg("permission.rail.first_check")

		// 刷新配置：优先使用 get_permissions_snapshot，回退到 staticConfig
		if r.host.GetPermissionsSnapshot != nil {
			if snap := r.host.GetPermissionsSnapshot(); snap != nil {
				r.UpdateConfig(snap, nil)
			}
		} else {
			r.engine.UpdateConfig(r.staticConfig)
		}

		// 执行权限检查
		result := r.engine.CheckPermission(normalizedName, toolArgs)

		// ALLOW → 放行
		if result.Permission == harnesssecurity.PermissionLevelAllow {
			logger.Info(permRailLogComponent).
				Str("tool", toolName).
				Str("matched_rule", result.MatchedRule).
				Msg("permission.rail.result decision=allow")
			return r.Approve("")
		}

		// DENY → 拒绝
		if result.Permission == harnesssecurity.PermissionLevelDeny {
			logger.Warn(permRailLogComponent).
				Str("tool", toolName).
				Str("matched_rule", result.MatchedRule).
				Msg("permission.rail.result decision=deny")
			reason := result.Reason
			if reason == "" {
				reason = "Operation not allowed"
			}
			return r.Reject(fmt.Sprintf("[PERMISSION_DENIED] %s", reason))
		}

		// ASK → 检查 auto_confirm session 状态
		if isPermissionAutoConfirmed(autoConfirmConfig, autoConfirmKey) {
			logger.Info(permRailLogComponent).
				Str("tool", toolName).
				Str("key", autoConfirmKey).
				Msg("permission.auto_confirm.hit")
			return r.Approve("")
		}

		// ASK → 尝试 hosted 确认
		if r.host.RequestPermissionConfirmation != nil {
			extOut, err := r.host.RequestPermissionConfirmation(harnesssecurity.PermissionConfirmationRequest{
				Ctx:            cbc,
				ToolCall:       toolCall,
				Result:         result,
				AutoConfirmKey: autoConfirmKey,
			})
			if err != nil {
				logger.Warn(permRailLogComponent).
					Err(err).
					Msg("permission.hosted_confirm.error")
				extOut = nil
			}
			if extOut == nil {
				// nil → 拒绝
				reason := result.Reason
				if reason == "" {
					reason = "Operation requires approval"
				}
				return r.Reject(fmt.Sprintf("[PERMISSION_DENIED] %s (Hosted permission request failed)", reason))
			}
			// 有效响应 → 处理确认结果
			persisted := false
			if extOut.Approved && extOut.AutoConfirm {
				persisted = r.persistAllowAlways(normalizedName, toolArgs)
			}
			logger.Info(permRailLogComponent).
				Str("tool", toolName).
				Str("confirm_path", "hosted").
				Bool("persisted", persisted).
				Msg("permission.persist.result")

			session := resolveSession(cbc)
			if shouldStoreAutoConfirm(extOut.AutoConfirm, session, autoConfirmKey, persisted) {
				r.storeAutoConfirm(cbc, autoConfirmKey)
			}

			if extOut.Approved {
				decision := "allow_once"
				if extOut.AutoConfirm {
					decision = "allow_always"
				}
				logger.Info(permRailLogComponent).
					Str("tool", toolName).
					Str("confirm_path", "hosted").
					Str("decision", decision).
					Bool("persisted", persisted).
					Msg("permission.user.decision")
				return r.Approve("")
			}

			logger.Info(permRailLogComponent).
				Str("tool", toolName).
				Str("confirm_path", "hosted").
				Msg("permission.user.decision decision=deny")
			feedback := extOut.Feedback
			if feedback == "" {
				feedback = "[PERMISSION_REJECTED] User rejected the request."
			}
			return r.Reject(feedback)
		}

		// ASK → 标准 interrupt 流程
		logger.Info(permRailLogComponent).
			Str("tool", toolName).
			Str("matched_rule", result.MatchedRule).
			Msg("permission.interrupt.ask")
		message := r.buildMessage(toolCall, result)
		return r.Interrupt(&saschema.InterruptRequest{
			Message:        message,
			PayloadSchema:  confirmPayloadSchemaForPermission(),
			AutoConfirmKey: autoConfirmKey,
		})
	}

	// ── 3. 恢复调用（userInput != nil） ──
	logger.Info(permRailLogComponent).
		Str("tool", toolName).
		Msg("permission.rail.user_response")

	payload := parseConfirmPayload(userInput)
	if payload == nil {
		message := r.buildMessage(toolCall, &harnesssecurity.PermissionResult{
			Permission:  harnesssecurity.PermissionLevelAsk,
			MatchedRule: "",
			Reason:      "Invalid confirmation payload",
		})
		return r.Interrupt(&saschema.InterruptRequest{
			Message:        message,
			PayloadSchema:  confirmPayloadSchemaForPermission(),
			AutoConfirmKey: autoConfirmKey,
		})
	}

	persisted := false
	if payload.Approved && payload.AutoConfirm {
		persisted = r.persistAllowAlways(normalizedName, toolArgs)
		logger.Info(permRailLogComponent).
			Str("tool", toolName).
			Str("confirm_path", r.confirmPathLabel()).
			Bool("persisted", persisted).
			Msg("permission.persist.result")
	}

	session := resolveSession(cbc)
	if shouldStoreAutoConfirm(payload.AutoConfirm, session, autoConfirmKey, persisted) {
		r.storeAutoConfirm(cbc, autoConfirmKey)
	}

	if payload.Approved {
		decision := "allow_once"
		if payload.AutoConfirm {
			decision = "allow_always"
		}
		logger.Info(permRailLogComponent).
			Str("tool", toolName).
			Str("confirm_path", r.confirmPathLabel()).
			Str("decision", decision).
			Bool("persisted", persisted).
			Msg("permission.user.decision")
		return r.Approve("")
	}

	logger.Info(permRailLogComponent).
		Str("tool", toolName).
		Str("confirm_path", r.confirmPathLabel()).
		Msg("permission.user.decision decision=deny")
	feedback := payload.Feedback
	if feedback == "" {
		feedback = "[PERMISSION_REJECTED] User rejected the request."
	}
	return r.Reject(feedback)
}

// parseToolArgs 从 ToolCall 中提取参数字典。
//
// 对齐 Python: PermissionInterruptRail._parse_tool_args(tool_call) (tool_security_rail.py L506-519)
func parseToolArgs(toolCall *llmschema.ToolCall) map[string]any {
	if toolCall == nil {
		return map[string]any{}
	}
	args := toolCall.Arguments
	if args == "" {
		return map[string]any{}
	}

	// 尝试 JSON 解析
	var parsed map[string]any
	if err := json.Unmarshal([]byte(args), &parsed); err != nil {
		return map[string]any{}
	}
	return parsed
}

// parseConfirmPayload 解析用户输入为 PermissionConfirmResponse。
//
// 对齐 Python: PermissionInterruptRail._parse_confirm_payload(user_input) (tool_security_rail.py L521-549)
func parseConfirmPayload(userInput any) *harnesssecurity.PermissionConfirmResponse {
	switch input := userInput.(type) {
	case *harnesssecurity.PermissionConfirmResponse:
		return input
	case *interrupt.ConfirmPayload:
		return &harnesssecurity.PermissionConfirmResponse{
			Approved:    input.Approved,
			Feedback:    input.Feedback,
			AutoConfirm: input.AutoConfirm,
		}
	case map[string]any:
		approved, _ := input["approved"].(bool)
		if !approved {
			// approved 缺失 → 解析失败
			if _, hasApproved := input["approved"]; !hasApproved {
				return nil
			}
		}
		feedback, _ := input["feedback"].(string)
		autoConfirm, _ := input["auto_confirm"].(bool)
		return &harnesssecurity.PermissionConfirmResponse{
			Approved:    approved,
			Feedback:    feedback,
			AutoConfirm: autoConfirm,
		}
	case string:
		var raw map[string]any
		if err := json.Unmarshal([]byte(input), &raw); err != nil {
			return nil
		}
		return parseConfirmPayload(raw)
	default:
		return nil
	}
}

// confirmPathLabel 返回确认路径标签。
//
// 对齐 Python: PermissionInterruptRail._confirm_path_label() (tool_security_rail.py L551-552)
func (r *PermissionInterruptRail) confirmPathLabel() string {
	if r.host.RequestPermissionConfirmation != nil {
		return "hosted"
	}
	return "interrupt"
}

// isPermissionAutoConfirmed 检查 auto_confirm 配置中指定 key 是否为 truthy。
//
// 对齐 Python: PermissionInterruptRail._is_auto_confirmed(auto_confirm_config, key) (tool_security_rail.py L554-558)
func isPermissionAutoConfirmed(config map[string]any, key string) bool {
	if config == nil || key == "" {
		return false
	}
	val, ok := config[key]
	if !ok {
		return false
	}
	return isSecurityTruthy(val)
}

// storeAutoConfirm 写入 auto_confirm 到 session 状态。
//
// 对齐 Python: PermissionInterruptRail._store_auto_confirm(ctx, auto_confirm_key) (tool_security_rail.py L560-567)
func (r *PermissionInterruptRail) storeAutoConfirm(cbc *agentinterfaces.AgentCallbackContext, autoConfirmKey string) {
	sess := cbc.Session()
	if sess == nil || autoConfirmKey == "" {
		return
	}

	var config map[string]any
	if val, err := sess.GetState(sessionstate.StringKey(saschema.InterruptAutoConfirmKey)); err == nil && val != nil {
		if m, ok := val.(map[string]any); ok {
			config = m
		}
	}
	if config == nil {
		config = make(map[string]any)
	}

	config[autoConfirmKey] = true
	sess.UpdateState(map[string]any{
		saschema.InterruptAutoConfirmKey: config,
	})

	logger.Info(permRailLogComponent).
		Str("key", autoConfirmKey).
		Msg("permission.auto_confirm.store")
}

// resolveSession 从 cbc 中解析 session 对象。
//
// 对齐 Python: PermissionInterruptRail._resolve_session_id(ctx) (tool_security_rail.py L584-594)
func resolveSession(cbc *agentinterfaces.AgentCallbackContext) any {
	if cbc == nil {
		return nil
	}
	return cbc.Session()
}

// collectExternalDirectoryPersistPaths 收集外部目录白名单路径。
//
// 对齐 Python: PermissionInterruptRail._collect_external_directory_persist_paths(...) (tool_security_rail.py L206-239)
func (r *PermissionInterruptRail) collectExternalDirectoryPersistPaths(
	normalizedName string,
	toolArgs map[string]any,
	permissionsCfg map[string]any,
) []string {
	if r.host.ResolveWorkspaceDir == nil {
		return nil
	}

	var workspace string
	// 对齐 Python: try workspace resolve
	func() {
		defer func() {
			if rec := recover(); rec != nil {
				logger.Debug(permRailLogComponent).
					Interface("recover", rec).
					Msg("permission.persist.external.workspace_resolve_failed")
			}
		}()
		workspace = r.host.ResolveWorkspaceDir()
	}()

	if workspace == "" {
		return nil
	}

	checker := harnesssecurity.NewExternalDirectoryChecker(permissionsCfg, workspace)
	extResult := checker.CheckExternalPaths(normalizedName, toolArgs)
	if extResult == nil || extResult.Permission != harnesssecurity.PermissionLevelAsk {
		return nil
	}
	if extResult.ExternalPaths == nil {
		return nil
	}
	return extResult.ExternalPaths
}

// persistAllowAlways 工具级「始终允许」与 external_directory 白名单持久化。
//
// 对齐 Python: PermissionInterruptRail._persist_allow_always(normalized_name, tool_args) (tool_security_rail.py L241-286)
func (r *PermissionInterruptRail) persistAllowAlways(normalizedName string, toolArgs map[string]any) bool {
	// 深拷贝当前配置
	cfg := deepCopyMap(r.engine.Config())

	cfg, okTool := harnesssecurity.MergePermissionAllowRuleIntoPermissions(cfg, normalizedName, toolArgs)
	extPaths := r.collectExternalDirectoryPersistPaths(normalizedName, toolArgs, cfg)
	var okExt bool
	if len(extPaths) > 0 {
		cfg, okExt = harnesssecurity.MergeExternalDirectoryAllowIntoPermissions(cfg, extPaths)
	}
	if !okTool && !okExt {
		return false
	}

	// 保存旧配置以便回滚
	prevCfg := deepCopyMap(r.engine.Config())
	r.UpdateConfig(cfg, nil)

	// 持久化
	if r.host.PersistAllowRule != nil {
		persisted := func() bool {
			defer func() {
				if rec := recover(); rec != nil {
					logger.Warn(permRailLogComponent).
						Interface("recover", rec).
						Msg("permission.persist.host_failed")
				}
			}()
			return r.host.PersistAllowRule(cfg)
		}()
		if !persisted {
			r.UpdateConfig(prevCfg, nil)
		}
		return persisted
	}

	// 回退到 YAML 写盘
	if !harnesssecurity.WritePermissionsSectionToAgentConfigYAML(r.host.PermissionYAMLPath, cfg) {
		r.UpdateConfig(prevCfg, nil)
		return false
	}
	return true
}

// buildMessage 构建中断消息。
//
// 对齐 Python: PermissionInterruptRail._build_message(tool_call, result) (tool_security_rail.py L603-628)
func (r *PermissionInterruptRail) buildMessage(toolCall *llmschema.ToolCall, result *harnesssecurity.PermissionResult) string {
	toolName := ""
	if toolCall != nil {
		toolName = toolCall.Name
	}
	toolArgs := parseToolArgs(toolCall)

	parts := []string{
		fmt.Sprintf("**工具 `%s` 需要授权才能执行**\n\n", toolName),
		"请确认是否允许该操作。\n\n",
	}

	argsPreview := formatArgsPreview(toolArgs)
	if argsPreview != "" && argsPreview != "{}" {
		parts = append(parts, fmt.Sprintf("参数：\n```json\n%s\n```\n", argsPreview))
	}

	parts = append(parts, fmt.Sprintf("\n匹配规则：`%s`", result.MatchedRule))
	if result.MatchedRule == "" {
		parts[len(parts)-1] = "\n匹配规则：`N/A`"
	}

	if len(result.ExternalPaths) > 0 {
		parts = append(parts, fmt.Sprintf("\n\n**外部路径：** `%s`", strings.Join(result.ExternalPaths, ", ")))
	}

	parts = append(parts, r.buildAlwaysAllowHint(toolCall))

	return strings.Join(parts, "")
}

// buildAlwaysAllowHint 构建自动确认提示。
//
// 对齐 Python: PermissionInterruptRail._build_always_allow_hint(tool_call) (tool_security_rail.py L630-665)
func (r *PermissionInterruptRail) buildAlwaysAllowHint(toolCall *llmschema.ToolCall) string {
	if toolCall == nil {
		return ""
	}

	toolName := toolCall.Name
	if toolName == "" {
		return ""
	}
	toolArgs := parseToolArgs(toolCall)

	if toolName == "bash" || toolName == "mcp_exec_command" || toolName == "create_terminal" {
		cmd := ""
		if v, ok := toolArgs["command"]; ok {
			cmd = fmt.Sprintf("%v", v)
		} else if v, ok := toolArgs["cmd"]; ok {
			cmd = fmt.Sprintf("%v", v)
		}
		if buildShellAutoConfirmKey(toolName, cmd) != "" {
			return "\n\n> 若选择「记住 / 总是允许」并提交 ``auto_confirm: true``，" +
				"将合并权限配置并尝试写回磁盘（与仅本次允许相对）。"
		}
	}

	autoConfirmKey := r.getAutoConfirmKey(toolCall)
	if autoConfirmKey != "" {
		return fmt.Sprintf(
			"\n\n> 若选择「记住 / 总是允许」并提交 ``auto_confirm: true``，"+
				"将合并权限配置并写回磁盘；同时可在本会话内自动放行 ``%s`` 类调用。",
			autoConfirmKey,
		)
	}

	return ""
}

// formatArgsPreview 格式化工具参数预览。
//
// 对齐 Python: PermissionInterruptRail._format_args_preview(tool_args) (tool_security_rail.py L597-601)
func formatArgsPreview(toolArgs map[string]any) string {
	if toolArgs == nil {
		return ""
	}
	data, err := json.Marshal(toolArgs)
	if err != nil {
		return fmt.Sprintf("%v", toolArgs)
	}
	s := string(data)
	if len(s) > 1000 {
		s = s[:1000]
	}
	return s
}

// confirmPayloadSchemaForPermission 返回权限场景的 ConfirmPayload JSON Schema。
func confirmPayloadSchemaForPermission() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"approved": map[string]any{
				"title": "Approved",
				"type":  "boolean",
			},
			"feedback": map[string]any{
				"default": "",
				"title":   "Feedback",
				"type":    "string",
			},
			"auto_confirm": map[string]any{
				"default": false,
				"title":   "Auto Confirm",
				"type":    "boolean",
			},
		},
		"required": []string{"approved"},
		"title":    "ConfirmPayload",
	}
}

// deepCopyMap 深拷贝 map[string]any
func deepCopyMap(m map[string]any) map[string]any {
	if m == nil {
		return make(map[string]any)
	}
	data, err := json.Marshal(m)
	if err != nil {
		return make(map[string]any)
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		return make(map[string]any)
	}
	return result
}

// applyDecision 根据中断决策类型执行对应的处理逻辑。
// 对齐 Python: BaseInterruptRail._apply_decision — 复用基类逻辑。
func (r *PermissionInterruptRail) applyDecision(
	cbc *agentinterfaces.AgentCallbackContext,
	toolInputs *agentinterfaces.ToolCallInputs,
	decision interrupt.InterruptDecision,
) {
	switch d := decision.(type) {
	case *interrupt.ApproveResult:
		if d.NewArgs != "" {
			toolInputs.ToolArgs = d.NewArgs
		}
	case *interrupt.RejectResult:
		r.skipPermissionTool(cbc, toolInputs, d)
	case *interrupt.InterruptResult:
		r.raiseInterrupt(toolInputs.ToolName, toolInputs.ToolCall, d.Request)
	}
}

// skipPermissionTool 跳过工具执行（权限拒绝）。
func (r *PermissionInterruptRail) skipPermissionTool(
	cbc *agentinterfaces.AgentCallbackContext,
	toolInputs *agentinterfaces.ToolCallInputs,
	reject *interrupt.RejectResult,
) {
	toolCallID := r.resolveToolCallID(toolInputs.ToolCall)
	cbc.Extra()["_skip_tool"] = true
	toolInputs.ToolResult = reject.ToolResult
	if reject.ToolMessage != nil {
		toolInputs.ToolMsg = reject.ToolMessage
	} else {
		toolInputs.ToolMsg = llmschema.NewToolMessage(toolCallID, fmt.Sprintf("%v", reject.ToolResult))
	}
}

// raiseInterrupt 抛出工具中断异常。
func (r *PermissionInterruptRail) raiseInterrupt(
	toolName string,
	toolCall *llmschema.ToolCall,
	request saschema.InterruptRequester,
) {
	exc := &saschema.ToolInterruptException{
		Request:  request,
		ToolCall: toolCall,
	}
	panic(cb.NewAbortError(
		fmt.Sprintf("Tool execution interrupted: %s", toolName),
		exc,
	))
}

// resolveToolCallID 从 ToolCall 中提取 ID。
func (r *PermissionInterruptRail) resolveToolCallID(toolCall *llmschema.ToolCall) string {
	if toolCall == nil {
		return ""
	}
	return toolCall.ID
}

// getUserInput 从回调上下文中提取用户输入。
// 对齐 Python: BaseInterruptRail._get_user_input
func (r *PermissionInterruptRail) getUserInput(cbc *agentinterfaces.AgentCallbackContext, toolCallID string) any {
	rawInput, exists := cbc.Extra()[saschema.ResumeUserInputKey]
	if !exists || rawInput == nil {
		return nil
	}

	// InteractiveInput 格式
	if interactive, ok := rawInput.(*sessioninteraction.InteractiveInput); ok {
		if val, found := interactive.UserInputs[toolCallID]; found {
			return val
		}
		return nil
	}

	// map[string]any 格式
	if m, ok := rawInput.(map[string]any); ok {
		if val, found := m[toolCallID]; found {
			return val
		}
		return m
	}

	return rawInput
}
