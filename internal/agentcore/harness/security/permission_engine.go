package security

import (
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// PermissionEngine 权限引擎 — 负责加载配置、评估权限。
//
// 对齐 Python: PermissionEngine (core.py L33-52)
type PermissionEngine struct {
	// config 权限配置（运行时为可变 dict）
	config map[string]any
	// enabled 是否启用
	enabled bool
	// permissionChecksActive 权限校验是否活跃
	permissionChecksActive func() bool
	// workspaceRoot 工作空间根目录
	workspaceRoot string
	// externalChecker 外部目录检查器
	externalChecker *ExternalDirectoryChecker
	// llm 保留字段（对齐 Python _llm，供 PermissionInterruptRail 等热更新模型）
	llm any
	// modelName 保留字段（对齐 Python _model_name）
	modelName string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

var engineLogComponent = logger.ComponentAgentCore

// ──────────────────────────── 导出函数 ────────────────────────────

// NewPermissionEngine 创建权限引擎。
//
// 对齐 Python: PermissionEngine.__init__(config, llm, model_name, workspace_root) (core.py L36-52)
func NewPermissionEngine(config map[string]any, llm any, modelName string, workspaceRoot string) *PermissionEngine {
	if config == nil {
		config = make(map[string]any)
	}
	enabled := true
	if v, ok := config["enabled"]; ok {
		if b, ok := v.(bool); ok {
			enabled = b
		}
	}
	return &PermissionEngine{
		config:          config,
		enabled:         enabled,
		workspaceRoot:   workspaceRoot,
		externalChecker: NewExternalDirectoryChecker(config, workspaceRoot),
		llm:             llm,
		modelName:       modelName,
	}
}

// UpdateConfig 热更新配置。
//
// 对齐 Python: PermissionEngine.update_config(config) (core.py L56-62)
func (e *PermissionEngine) UpdateConfig(config map[string]any) {
	if config == nil {
		config = make(map[string]any)
	}
	e.config = config
	enabled := true
	if v, ok := config["enabled"]; ok {
		if b, ok := v.(bool); ok {
			enabled = b
		}
	}
	e.enabled = enabled
	e.externalChecker = NewExternalDirectoryChecker(config, e.workspaceRoot)
}

// UpdateLLM 热更新模型实例。
// 对齐 Python: PermissionEngine.update_llm(llm, model_name) (core.py L64-67)
func (e *PermissionEngine) UpdateLLM(llm any, modelName string) {
	e.llm = llm
	e.modelName = modelName
}

// Enabled 返回权限系统是否启用。
func (e *PermissionEngine) Enabled() bool {
	return e.enabled
}

// SetPermissionChecksActive 设置权限校验活跃检查函数。
//
// 对齐 Python: PermissionEngine.set_permission_checks_active(fn) (core.py L73-75)
func (e *PermissionEngine) SetPermissionChecksActive(fn func() bool) {
	e.permissionChecksActive = fn
}

// SetWorkspaceRoot 设置工作空间根目录。
func (e *PermissionEngine) SetWorkspaceRoot(root string) {
	e.workspaceRoot = root
	e.externalChecker = NewExternalDirectoryChecker(e.config, root)
}

// CheckPermission 检查工具调用权限。
//
// 对齐 Python: PermissionEngine.check_permission(tool_name, tool_args) (core.py L128-221)
func (e *PermissionEngine) CheckPermission(toolName string, toolArgs map[string]any) *PermissionResult {
	logger.Info(engineLogComponent).
		Str("tool", toolName).
		Bool("enabled", e.enabled).
		Msg("permission.check.start")

	// 未启用 → 允许
	if !e.enabled {
		logger.Info(engineLogComponent).Msg("permission.check.skip: reason=system_disabled decision=allow")
		return &PermissionResult{
			Permission: PermissionLevelAllow,
			Reason:     "Permission system is disabled",
		}
	}

	// 宿主说不要校验 → 允许
	if e.permissionChecksActive != nil && !e.permissionChecksActive() {
		logger.Info(engineLogComponent).Msg("permission.check.skip: reason=permission_checks_inactive decision=allow")
		return &PermissionResult{
			Permission: PermissionLevelAllow,
			Reason:     "Tool permission checks are inactive for this context",
		}
	}

	if toolArgs == nil {
		toolArgs = make(map[string]any)
	}

	// 1. 工具级 + 参数规则 + 默认（分层策略 evaluate_tiered_policy）
	var externalPaths []string
	permission, matchedRule := e.EvaluateGlobalPolicyDirectly(toolName, toolArgs, false)
	// 对齐 Python: if permission is None → ASK
	if permission == PermissionLevelNone {
		permission = PermissionLevelAsk
		matchedRule = "default"
	}
	logger.Info(engineLogComponent).
		Str("tool", toolName).
		Str("permission", permission.String()).
		Str("matched_rule", matchedRule).
		Msg("permission.policy.result")

	// 2. 外部路径：与当前决策取更严（不放宽）
	extResult := e.externalChecker.CheckExternalPaths(toolName, toolArgs)
	if extResult != nil {
		logger.Info(engineLogComponent).
			Str("tool", toolName).
			Bool("checked", true).
			Str("ext_permission", extResult.Permission.String()).
			Str("matched_rule", extResult.MatchedRule).
			Strs("external_paths", extResult.ExternalPaths).
			Str("merged_with", permission.String()).
			Msg("permission.external.result")
		permission = Strictest(permission, extResult.Permission)
		if matchedRule != "" {
			matchedRule = matchedRule + "|" + extResult.MatchedRule
		} else {
			matchedRule = extResult.MatchedRule
		}
		if extResult.ExternalPaths != nil {
			externalPaths = extResult.ExternalPaths
		}
	}

	result := &PermissionResult{
		Permission:    permission,
		MatchedRule:   matchedRule,
		Reason:        getReason(permission, toolName, matchedRule),
		ExternalPaths: externalPaths,
	}

	logger.Info(engineLogComponent).
		Str("tool", toolName).
		Str("permission", permission.String()).
		Str("matched_rule", matchedRule).
		Msg("permission.check.final")
	return result
}

// CheckToolPermissionDirectly 直接检查工具权限，不受 enabled 开关与宿主「是否校验」短路影响。
//
// 对齐 Python: PermissionEngine.check_tool_permission_directly(tool_name, tool_args) (core.py L77-89)
func (e *PermissionEngine) CheckToolPermissionDirectly(toolName string, toolArgs map[string]any) (PermissionLevel, string) {
	return e.EvaluateGlobalPolicyDirectly(toolName, toolArgs, true)
}

// EvaluateGlobalPolicyDirectly 直接评估全局权限，不受 enabled 与宿主「是否校验」短路影响。
//
// 对齐 Python: PermissionEngine.evaluate_global_policy_directly(tool_name, tool_args, include_external_directory) (core.py L91-124)
func (e *PermissionEngine) EvaluateGlobalPolicyDirectly(toolName string, toolArgs map[string]any, includeExternalDirectory bool) (PermissionLevel, string) {
	if toolArgs == nil {
		toolArgs = make(map[string]any)
	}

	permission, matchedRule := EvaluateTieredPolicy(e.config, toolName, toolArgs)

	// fallback(no_config) → 返回 (None, "")
	// 对齐 Python: permission = None, matched_rule = None
	if matchedRule == mr+":fallback(no_config)" {
		permission = PermissionLevelNone
		matchedRule = ""
	} else if !MatchedRuleUsesApprovalOverride(matchedRule) {
		permission = MaybeEscalateShellOperators(toolName, toolArgs, permission, matchedRule)
	}

	if includeExternalDirectory {
		extResult := e.externalChecker.CheckExternalPaths(toolName, toolArgs)
		if extResult != nil {
			if permission == PermissionLevelNone {
				permission = extResult.Permission
				matchedRule = extResult.MatchedRule
			} else {
				permission = Strictest(permission, extResult.Permission)
				if matchedRule != "" {
					matchedRule = matchedRule + "|" + extResult.MatchedRule
				} else {
					matchedRule = extResult.MatchedRule
				}
			}
		}
	}

	return permission, matchedRule
}

// Config 返回当前配置
func (e *PermissionEngine) Config() map[string]any {
	return e.config
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// getReason 根据 permission 和 matchedRule 生成 reason
// 对齐 Python: PermissionEngine._get_reason(permission, tool_name, matched_rule) (core.py L225-233)
func getReason(permission PermissionLevel, toolName, matchedRule string) string {
	switch permission {
	case PermissionLevelAllow:
		return fmt.Sprintf("Allowed by rule: %s", matchedRule)
	case PermissionLevelDeny:
		return fmt.Sprintf("Denied by rule: %s", matchedRule)
	default:
		return fmt.Sprintf("Approval required for %s (rule: %s)", toolName, matchedRule)
	}
}

