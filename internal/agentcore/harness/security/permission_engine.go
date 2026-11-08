package security

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/security/toolguard"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/utils"
)

// ──────────────────────────── 结构体 ────────────────────────────

// PermissionEngine 权限引擎 — 负责加载配置、评估权限。
//
// Python: PermissionEngine (core.py L33-52)
//
// 并发说明：Python 依赖 GIL 保证属性读写的原子性；Go 端多工具并行执行时
// BeforeToolCall 回调会在多个 goroutine 并发进入 UpdateConfig/CheckPermission，
// 因此用 RWMutex 保护全部可变字段（包括 config、enabled、fileGuard、netGuard、
// llm、modelName、permissionChecksActive、workspaceRoot、trustedDirs、sceneHook）。
//
// # 判定管线
//
//	Pipeline A：EvaluateTieredPolicy（工具级 + 参数规则 + defaults）
//	Pipeline B：fileGuard（路径防护；含 ExternalDirectory Legacy 投影）
//	Pipeline C：netGuard（fetch 工具 URL 白黑名单；只有 allow/deny）
//	→ Strictest(A, B, C)
//
// Python: PermissionEngine.check_permission (core.py L266-396)
type PermissionEngine struct {
	// mu 可变字段读写锁
	mu sync.RWMutex
	// config 权限配置（运行时为可变 dict，已过 ingest）
	config map[string]any
	// enabled 是否启用
	enabled bool
	// permissionChecksActive 权限校验是否活跃
	permissionChecksActive func() bool
	// workspaceRoot 工作空间根目录（仅构造时设定，对齐 Python _workspace_root）
	workspaceRoot string
	// trustedDirs 受信目录（请求级热更新，对齐 Python _trusted_dirs）
	trustedDirs []string
	// fileGuard 路径防护判定器（Pipeline B）
	fileGuard FileGuardEvaluator
	// netGuard 网络防护判定器（Pipeline C）
	netGuard NetGuardEvaluator
	// llm 保留字段（对齐 Python _llm，供 PermissionInterruptRail 等热更新模型）
	llm *llm.Model
	// modelName 保留字段（对齐 Python _model_name）
	modelName string
	// sceneHook 宿主场景钩子（对齐 Python PermissionSceneHook）
	sceneHook PermissionSceneHookFn
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewPermissionEngine 创建权限引擎。
//
// 配置先经 PreparePermissionsForEngine 注入内置规则，再编译 file_guard 与
// net_guard 判定器。路径层与网络层各自可独立关闭。
//
// Python: PermissionEngine.__init__(config, llm, model_name, workspace_root) (core.py L36-52)
func NewPermissionEngine(config map[string]any, llmModel *llm.Model, modelName string, workspaceRoot string) *PermissionEngine {
	prepared := PreparePermissionsForEngine(config)
	enabled := true
	if v, ok := prepared["enabled"]; ok {
		if b, isBool := v.(bool); isBool {
			enabled = b
		}
	}
	e := &PermissionEngine{
		config:        prepared,
		enabled:       enabled,
		workspaceRoot: workspaceRoot,
		llm:           llmModel,
		modelName:     modelName,
	}
	e.rebuildGuards()
	return e
}

// PreparePermissionsForEngine 在配置进入引擎前注入内置规则。
//
// 四步（对齐 Python prepare_permissions_for_engine，顺序不可调换）：
//  1. package_builtin_rules 关闭 → 只补 legacy action，全跳过
//  2. rules 无 builtin 层 → 注入命令规则
//  3. file_guard.enabled 且 paths 无 builtin 层 → 注入敏感路径
//  4. net_guard.enabled 且 urls 未含全部内置 → 注入 net_urls
//
// 最后统一补 legacy action（即使提前 return 的路径也补）。
//
// Python: prepare_permissions_for_engine(config) (core.py L100-131)
func PreparePermissionsForEngine(config map[string]any) map[string]any {
	cfg := utils.DeepCopyMap(config)
	if cfg == nil {
		cfg = map[string]any{}
	}

	if !toolguard.PackageBuiltinRulesEnabled(cfg) {
		logger.Info(logComponent).Msg("permission.builtin_rules.skip_package_builtin_rules")
		return fillLegacyHostRuleActions(cfg)
	}

	if !hasBuiltinLayer(cfg["rules"]) {
		logger.Info(logComponent).Msg("permission.builtin_rules.legacy_host.inline_command_rules")
		cfg = toolguard.InlinePackageCommandRules(cfg)
	}

	if fg := mapAnyField(cfg, "file_guard"); fg != nil && truthyAny(fg["enabled"]) {
		if !hasBuiltinLayer(fg["paths"]) {
			logger.Info(logComponent).Msg("permission.builtin_rules.legacy_host.merge_sensitive_paths")
			cfg = mergeWithSensitivePaths(cfg)
		}
	}

	if ng := mapAnyField(cfg, "net_guard"); ng != nil && truthyAny(ng["enabled"]) {
		if !checkPackageNetURLs(ng["urls"]) {
			logger.Info(logComponent).Msg("permission.builtin_rules.legacy_host.merge_net_urls")
			cfg = mergeWithNetURLs(cfg)
		}
	}
	return fillLegacyHostRuleActions(cfg)
}

// UpdateConfig 热更新配置。
//
// Python: PermissionEngine.update_config(config) (core.py L56-62)
func (e *PermissionEngine) UpdateConfig(config map[string]any) {
	prepared := PreparePermissionsForEngine(config)
	enabled := true
	if v, ok := prepared["enabled"]; ok {
		if b, isBool := v.(bool); isBool {
			enabled = b
		}
	}
	e.mu.Lock()
	e.config = prepared
	e.enabled = enabled
	workspace := e.workspaceRoot
	trusted := append([]string(nil), e.trustedDirs...)
	e.mu.Unlock()

	fileGuard := newFileGuardEvaluator(prepared, workspace, trusted)
	netGuard := newNetGuardEvaluator(prepared)

	e.mu.Lock()
	e.fileGuard = fileGuard
	e.netGuard = netGuard
	e.mu.Unlock()
}

// UpdateTrustedDirs 更新受信目录并重建路径判定器。
//
// 宿主（如数字分身 adapter）在请求到达时调用，使外部路径检查把这些子树
// 视为内部，跳过 ask/deny。
//
// Python: PermissionEngine.update_trusted_dirs(trusted_dirs) (core.py L69-71)
func (e *PermissionEngine) UpdateTrustedDirs(trustedDirs []string) {
	normalized := make([]string, 0, len(trustedDirs))
	for _, d := range trustedDirs {
		d = strings.TrimSpace(d)
		if d == "" {
			continue
		}
		normalized = append(normalized, resolveUserPath(d))
	}
	e.mu.Lock()
	e.trustedDirs = normalized
	config := e.config
	workspace := e.workspaceRoot
	e.mu.Unlock()

	fileGuard := newFileGuardEvaluator(config, workspace, normalized)

	e.mu.Lock()
	e.fileGuard = fileGuard
	e.mu.Unlock()
}

// TrustedDirs 返回当前受信目录副本。
//
// Python: PermissionEngine.trusted_dirs（property）
func (e *PermissionEngine) TrustedDirs() []string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]string, len(e.trustedDirs))
	copy(out, e.trustedDirs)
	return out
}

// UpdateLLM 热更新模型实例。
// Python: PermissionEngine.update_llm(llm, model_name) (core.py L64-67)
func (e *PermissionEngine) UpdateLLM(llmModel *llm.Model, modelName string) {
	e.mu.Lock()
	e.llm = llmModel
	e.modelName = modelName
	e.mu.Unlock()
}

// Enabled 返回权限系统是否启用。
func (e *PermissionEngine) Enabled() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.enabled
}

// SetPermissionChecksActive 设置权限校验活跃检查函数。
//
// Python: PermissionEngine.set_permission_checks_active(fn) (core.py L73-75)
func (e *PermissionEngine) SetPermissionChecksActive(fn func() bool) {
	e.mu.Lock()
	e.permissionChecksActive = fn
	e.mu.Unlock()
}

// SetSceneHook 设置宿主场景钩子。
//
// Python: PermissionEngine.set_scene_hook(fn)
func (e *PermissionEngine) SetSceneHook(fn PermissionSceneHookFn) {
	e.mu.Lock()
	e.sceneHook = fn
	e.mu.Unlock()
}

// CheckPermission 检查工具调用权限。A/B/C 三段取最严。
//
// 三段管线（对齐 Python check_permission，顺序不可调换）：
//  1. Pipeline A：工具级 + 参数规则 + 默认
//  2. Pipeline B：file_guard 路径防护（返回 nil 时不改动 A 段，但仍打日志）
//  3. Pipeline C：net_guard 网络防护（不写 external_paths）
//
// Python: PermissionEngine.check_permission(tool_name, tool_args) (core.py L266-396)
func (e *PermissionEngine) CheckPermission(ctx context.Context, toolName string, toolArgs map[string]any) *PermissionResult {
	// 并发说明：先快照可变字段（RWMutex 读锁），评估全程使用快照，
	// 避免多工具并行时读到半更新状态；判定器构造后只读，快照引用可安全并发调用。
	e.mu.RLock()
	enabled := e.enabled
	checksActive := e.permissionChecksActive
	sceneHook := e.sceneHook
	config := e.config
	fileGuard := e.fileGuard
	netGuard := e.netGuard
	e.mu.RUnlock()

	logger.Info(logComponent).
		Str("tool", toolName).
		Bool("enabled", enabled).
		Msg("permission.check.start")

	// 未启用 → 允许
	if !enabled {
		logger.Info(logComponent).Msg("permission.check.skip reason=system_disabled decision=allow")
		return &PermissionResult{
			Permission: PermissionLevelAllow,
			Reason:     "权限系统已禁用",
		}
	}

	// 宿主说不要校验 → 允许
	if checksActive != nil && !checksActive() {
		logger.Info(logComponent).Msg("permission.check.skip reason=permission_checks_inactive decision=allow")
		return &PermissionResult{
			Permission: PermissionLevelAllow,
			Reason:     "当前上下文未启用工具权限检查",
		}
	}

	if toolArgs == nil {
		toolArgs = make(map[string]any)
	}

	// 0. PermissionSceneHook 短路：在分层评估前调用宿主场景钩子
	if sceneHook != nil {
		sceneOut, err := sceneHook(PermissionSceneHookInput{
			NormalizedToolName: toolName,
			ToolArgs:           toolArgs,
			Engine:             e,
			GoCtx:              ctx,
		})
		if err != nil {
			logger.Warn(logComponent).
				Str("tool", toolName).
				Err(err).
				Msg("permission.scene_hook.failed")
		} else if len(sceneOut) > 0 {
			switch sceneOut[0] {
			case "approve":
				logger.Info(logComponent).
					Str("tool", toolName).
					Str("decision", "allow").
					Msg("permission.scene_hook.short_circuit")
				return &PermissionResult{
					Permission:  PermissionLevelAllow,
					MatchedRule: "scene_hook",
					Reason:      "由场景钩子放行",
				}
			case "reject":
				msg := "操作不被允许"
				if len(sceneOut) > 1 {
					msg = sceneOut[1]
				}
				logger.Info(logComponent).
					Str("tool", toolName).
					Str("decision", "deny").
					Msg("permission.scene_hook.short_circuit")
				return &PermissionResult{
					Permission:  PermissionLevelDeny,
					MatchedRule: "scene_hook",
					Reason:      msg,
				}
			}
		}
	}

	// 1. Pipeline A：工具级 + 参数规则 + 默认
	var externalPaths []string
	permission, matchedRule, evalErr := evaluateGlobalPolicyWithSnapshot(config, toolName, toolArgs, false)
	if evalErr != nil {
		// 对齐 Python except 行为：error 时将结果视为 PermissionLevelNone（不参与后续比较）
		logger.Warn(logComponent).
			Str("tool", toolName).
			Err(evalErr).
			Msg("permission.evaluate_global_policy.failed")
		permission = PermissionLevelNone
		matchedRule = ""
	}
	// Python: if permission is None → ASK
	if permission == PermissionLevelNone {
		permission = PermissionLevelAsk
		matchedRule = "default"
	}
	logger.Info(logComponent).
		Str("tool", toolName).
		Str("permission", permission.String()).
		Str("matched_rule", matchedRule).
		Msg("permission.policy.result")

	// 2. Pipeline B：file_guard（可独立关闭；含 ExternalDirectory Legacy 投影）
	if fileGuard != nil {
		if pathResult := fileGuard.Evaluate(toolName, toolArgs); pathResult != nil {
			pathRule := pathResult.MatchedRule
			if pathRule == "" {
				pathRule = "file_guard"
			}
			logger.Info(logComponent).
				Str("tool", toolName).
				Bool("checked", true).
				Str("permission", pathResult.Permission.String()).
				Str("matched_rule", pathRule).
				Strs("external_paths", pathResult.ExternalPaths).
				Str("merged_with", permission.String()).
				Msg("permission.file_guard.result")
			permission = Strictest(permission, pathResult.Permission)
			matchedRule = matchedRule + "|" + pathRule
			externalPaths = pathResult.ExternalPaths
		} else {
			logger.Info(logComponent).
				Str("tool", toolName).
				Bool("checked", true).
				Str("permission", "none").
				Str("matched_rule", "none").
				Strs("external_paths", []string{}).
				Msg("permission.file_guard.result")
		}
	} else {
		logger.Info(logComponent).
			Str("tool", toolName).
			Bool("checked", false).
			Str("reason", "disabled").
			Msg("permission.file_guard.result")
	}

	// 3. Pipeline C：net_guard（可独立关闭；仅 fetch 工具；无 ask）
	if netGuard != nil {
		if netResult := netGuard.Evaluate(toolName, toolArgs); netResult != nil {
			netRule := netResult.MatchedRule
			if netRule == "" {
				netRule = "net_guard"
			}
			logger.Info(logComponent).
				Str("tool", toolName).
				Bool("checked", true).
				Str("permission", netResult.Permission.String()).
				Str("matched_rule", netRule).
				Str("merged_with", permission.String()).
				Msg("permission.net_guard.result")
			permission = Strictest(permission, netResult.Permission)
			matchedRule = matchedRule + "|" + netRule
		} else {
			logger.Info(logComponent).
				Str("tool", toolName).
				Bool("checked", true).
				Str("permission", "none").
				Str("matched_rule", "none").
				Msg("permission.net_guard.result")
		}
	} else {
		logger.Info(logComponent).
			Str("tool", toolName).
			Bool("checked", false).
			Str("reason", "disabled").
			Msg("permission.net_guard.result")
	}

	result := &PermissionResult{
		Permission:    permission,
		MatchedRule:   matchedRule,
		Reason:        getReason(permission, toolName, matchedRule),
		ExternalPaths: externalPaths,
	}

	logger.Info(logComponent).
		Str("tool", toolName).
		Str("permission", permission.String()).
		Str("matched_rule", matchedRule).
		Strs("external_paths", externalPaths).
		Msg("permission.check.final")
	return result
}

// CheckToolPermissionDirectly 直接检查工具权限，不受 enabled 开关与宿主「是否校验」短路影响。
//
// Python: PermissionEngine.check_tool_permission_directly(tool_name, tool_args) (core.py L77-89)
func (e *PermissionEngine) CheckToolPermissionDirectly(toolName string, toolArgs map[string]any) (PermissionLevel, string, error) {
	return e.EvaluateGlobalPolicyDirectly(toolName, toolArgs, true)
}

// EvaluateGlobalPolicyDirectly 直接评估全局权限，不受 enabled 与宿主「是否校验」短路影响。
//
// includeExternalDirectory 为真时把路径层与网络层纳入。
//
// Python: PermissionEngine.evaluate_global_policy_directly(tool_name, tool_args, include_external_directory) (core.py L91-124)
func (e *PermissionEngine) EvaluateGlobalPolicyDirectly(toolName string, toolArgs map[string]any, includeExternalDirectory bool) (PermissionLevel, string, error) {
	if toolArgs == nil {
		toolArgs = make(map[string]any)
	}

	e.mu.RLock()
	config := e.config
	fileGuard := e.fileGuard
	netGuard := e.netGuard
	e.mu.RUnlock()

	return evaluateGlobalPolicyWithSnapshotAndGuards(config, fileGuard, netGuard, toolName, toolArgs, includeExternalDirectory)
}

// Config 返回当前配置
func (e *PermissionEngine) Config() map[string]any {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.config
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// rebuildGuards 重建 file_guard 与 net_guard 判定器。
//
// 先在读锁内快照配置，再在锁外构造（构造会编译正则/读 YAML，耗时较长，
// 不应持锁），最后写锁内替换。
//
// Python: PermissionEngine._rebuild_file_guard()
func (e *PermissionEngine) rebuildGuards() {
	e.mu.RLock()
	config := e.config
	workspace := e.workspaceRoot
	trusted := append([]string(nil), e.trustedDirs...)
	e.mu.RUnlock()

	fileGuard := newFileGuardEvaluator(config, workspace, trusted)
	netGuard := newNetGuardEvaluator(config)

	e.mu.Lock()
	e.fileGuard = fileGuard
	e.netGuard = netGuard
	e.mu.Unlock()
}

// hasBuiltinLayer 判定列表里是否有 layer=="builtin" 的项。
//
// Python: _has_builtin_layer(items)
func hasBuiltinLayer(items any) bool {
	list, ok := items.([]any)
	if !ok {
		return false
	}
	for _, item := range list {
		m, isMap := item.(map[string]any)
		if isMap && m["layer"] == "builtin" {
			return true
		}
	}
	return false
}

// fillLegacyHostRuleActions 用 severity + permission_mode 补齐缺失的 rule action。
//
// Python: _fill_legacy_host_rule_actions(cfg)
func fillLegacyHostRuleActions(cfg map[string]any) map[string]any {
	rules, ok := cfg["rules"].([]any)
	if !ok {
		return cfg
	}
	mode, _ := cfg["permission_mode"].(string)
	filled := make([]any, 0, len(rules))
	for _, rule := range rules {
		m, isMap := rule.(map[string]any)
		if !isMap {
			filled = append(filled, rule)
			continue
		}
		item := map[string]any{}
		for k, v := range m {
			item[k] = v
		}
		if action, isStr := item["action"].(string); isStr && strings.TrimSpace(action) != "" {
			filled = append(filled, item)
			continue
		}
		if mapped := severityToAction(item["severity"], mode); mapped != "" {
			item["action"] = mapped
		}
		filled = append(filled, item)
	}
	cfg["rules"] = filled
	return cfg
}

// severityToAction 把 severity 映射为 action；severity 为空时返回空串（让调用方跳过）。
//
// Python: severity_to_action(severity, permission_mode)
func severityToAction(severity any, permissionMode string) string {
	sev, _ := severity.(string)
	sev = strings.ToUpper(strings.TrimSpace(sev))
	if sev == "" {
		return ""
	}
	switch SeverityToDecision(sev, permissionMode) {
	case PermissionLevelAllow:
		return "allow"
	case PermissionLevelDeny:
		return "deny"
	}
	return "ask"
}

// resolveUserPath 把用户给的目录路径解析为绝对路径（不要求存在）。
func resolveUserPath(p string) string {
	expanded := os.ExpandEnv(p)
	if strings.HasPrefix(expanded, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			rest := strings.TrimLeft(expanded[1:], "/\\")
			if rest == "" {
				expanded = home
			} else {
				expanded = filepath.Join(home, rest)
			}
		}
	}
	if abs, err := filepath.Abs(expanded); err == nil {
		return filepath.Clean(abs)
	}
	return filepath.Clean(expanded)
}

// evaluateGlobalPolicyWithSnapshot 以配置快照执行全局权限评估。
//
// CheckPermission / EvaluateGlobalPolicyDirectly 在读锁内快照字段后调用，
// 避免评估期间并发 UpdateConfig/UpdateLLM 引发的数据竞争。
//
// Python: evaluate_global_policy_directly 主体
func evaluateGlobalPolicyWithSnapshot(
	config map[string]any,
	toolName string,
	toolArgs map[string]any,
	includeExternalDirectory bool,
) (PermissionLevel, string, error) {
	return evaluateGlobalPolicyWithSnapshotAndGuards(config, nil, nil, toolName, toolArgs, includeExternalDirectory)
}

// evaluateGlobalPolicyWithSnapshotAndGuards 与 evaluateGlobalPolicyWithSnapshot
// 相同，但可显式传入判定器（供 EvaluateGlobalPolicyDirectly 使用）。
func evaluateGlobalPolicyWithSnapshotAndGuards(
	config map[string]any,
	fileGuard FileGuardEvaluator,
	netGuard NetGuardEvaluator,
	toolName string,
	toolArgs map[string]any,
	includeExternalDirectory bool,
) (PermissionLevel, string, error) {
	permission, matchedRule := EvaluateTieredPolicy(config, toolName, toolArgs)

	// fallback(no_config) → 返回 (None, "")
	// Python: permission = None, matched_rule = None
	if matchedRule == mr+":fallback(no_config)" {
		permission = PermissionLevelNone
		matchedRule = ""
	} else if !MatchedRuleUsesApprovalOverride(matchedRule) {
		permission = MaybeEscalateShellOperators(toolName, toolArgs, permission, matchedRule)
	}

	if includeExternalDirectory {
		if fileGuard != nil {
			if pathResult := fileGuard.Evaluate(toolName, toolArgs); pathResult != nil {
				if permission == PermissionLevelNone {
					permission = pathResult.Permission
					matchedRule = pathResult.MatchedRule
				} else {
					permission = Strictest(permission, pathResult.Permission)
					matchedRule = matchedRule + "|" + pathResult.MatchedRule
				}
			}
		}
		if netGuard != nil {
			if netResult := netGuard.Evaluate(toolName, toolArgs); netResult != nil {
				if permission == PermissionLevelNone {
					permission = netResult.Permission
					matchedRule = netResult.MatchedRule
				} else {
					permission = Strictest(permission, netResult.Permission)
					matchedRule = matchedRule + "|" + netResult.MatchedRule
				}
			}
		}
	}

	return permission, matchedRule, nil
}

// getReason 根据 permission 和 matchedRule 生成 reason
// Python: PermissionEngine._get_reason(permission, tool_name, matched_rule) (core.py L225-233)
func getReason(permission PermissionLevel, toolName, matchedRule string) string {
	switch permission {
	case PermissionLevelAllow:
		return fmt.Sprintf("由规则放行: %s", matchedRule)
	case PermissionLevelDeny:
		return fmt.Sprintf("由规则拒绝: %s", matchedRule)
	default:
		return fmt.Sprintf("需审批: %s（规则: %s）", toolName, matchedRule)
	}
}
