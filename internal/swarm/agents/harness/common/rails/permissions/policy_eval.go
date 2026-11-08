package permissions

import (
	"encoding/json"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// PolicyEvaluation 权限策略结果（不触发确认或中断）。
//
// Python: @dataclass(frozen=True) PolicyEvaluation
type PolicyEvaluation struct {
	// Level 判定级别（allow / ask / deny）
	Level string
	// Reason 原因
	Reason string
	// MatchedRule 命中的规则（多个用 | 连接）
	MatchedRule string
	// Source 结果来源（permission_engine / scene_hook / fail_closed / file_guard_adapter）
	Source string
	// PathResult send_file 路径守卫结果（9.90e 前恒 nil）
	PathResult *SendFilePathGuardResult
	// ExternalPaths 外部路径
	ExternalPaths []string
	// ManualApprovalRequired 是否需要人工审批
	ManualApprovalRequired bool
}

// SendFilePathGuardResult send_file_to_user 的路径守卫结果。
//
// 完整实现属 9.90e（send_file_path_guard.py 315 行）；此处只定义
// policy_eval 需要的三个字段，避免循环依赖。
type SendFilePathGuardResult struct {
	// Level 判定级别（neutral 表示无意见）
	Level string
	// Reason 原因
	Reason string
	// MatchedRules 命中的规则列表
	MatchedRules []string
}

// OpenJiuwenPolicyEvaluator 只做策略评估，不进入人工确认流程。
//
// Python: class OpenJiuwenPolicyEvaluator
type OpenJiuwenPolicyEvaluator struct {
	// baseRail 被评估的 base rail
	baseRail PolicyBaseRail
	// permissionConfigGetter 运行时配置取值函数（可为 nil）
	permissionConfigGetter func() map[string]any
	// workspaceRoot 工作空间根（send_file 路径守卫用）
	workspaceRoot string
}

// PolicyInvocation 一次待评估的调用（供策略评估器使用）。
//
// Python 侧是 duck-typed 的 invocation 对象；Go 用显式结构体承载三个字段。
type PolicyInvocation struct {
	// ToolName 工具名
	ToolName string
	// ToolCall 原始工具调用（可为 nil）
	ToolCall any
	// ToolArgs 回退参数（map 或 JSON 字符串）
	ToolArgs any
	// Ctx 宿主上下文（透传给场景钩子）
	Ctx any
}

// PolicyBaseRail 策略评估器依赖的 base rail 能力面。
//
// 用接口而非具体类型，避免本包 import rail 造成循环依赖。
type PolicyBaseRail interface {
	// NormalizeToolName 归一化工具名（可为空实现）
	NormalizeToolName(toolName string) string
	// ParseToolArgs 解析工具参数（可为空实现）
	ParseToolArgs(toolCall any) map[string]any
	// Engine 返回权限引擎（可为 nil）
	Engine() PolicyEngine
	// StaticConfig 返回静态配置
	StaticConfig() map[string]any
	// UpdateConfig 热更新配置
	UpdateConfig(config map[string]any)
	// PermissionSceneHook 返回场景钩子（可为 nil）
	PermissionSceneHook() PolicySceneHookFn
	// PermissionsSnapshot 返回宿主配置快照（可为 nil）
	PermissionsSnapshot() map[string]any
	// HasPermissionsSnapshot 宿主是否提供了快照钩子（对应 Python 的
	// getattr(host, "get_permissions_snapshot", None) is not None）
	HasPermissionsSnapshot() bool
	// TrustedDirs 返回受信目录
	TrustedDirs() []string
}

// PolicyEngine 策略评估器使用的权限引擎能力面。
type PolicyEngine interface {
	// CheckPermission 检查工具权限
	CheckPermission(toolName string, toolArgs map[string]any) *PolicyPermissionResult
	// UpdateConfig 热更新配置
	UpdateConfig(config map[string]any)
}

// PolicyPermissionResult 引擎判定结果投影。
type PolicyPermissionResult struct {
	// Permission 级别（allow / ask / deny）
	Permission string
	// Reason 原因
	Reason string
	// MatchedRule 命中的规则
	MatchedRule string
	// ExternalPaths 外部路径
	ExternalPaths []string
	// ManualApprovalRequired 是否需要人工审批
	ManualApprovalRequired bool
}

// PolicySceneHookFn 场景钩子签名：返回 (decision, reason)。
type PolicySceneHookFn func(invocation PolicyInvocation, normalizedToolName string, toolArgs map[string]any, engine PolicyEngine) ([]string, error)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// policyAllowLevel allow 级别
	policyAllowLevel = "allow"
	// policyAskLevel ask 级别
	policyAskLevel = "ask"
	// policyDenyLevel deny 级别
	policyDenyLevel = "deny"

	// maxExternalPaths external_paths 的最大条目数
	maxExternalPaths = 64
	// maxExternalPathLength 单条外部路径的最大长度
	maxExternalPathLength = 4096
	// maxExternalPathsTotalLength 全部外部路径的总长度上限
	maxExternalPathsTotalLength = 16384

	// sendFileToolName send_file 工具名
	sendFileToolName = "send_file_to_user"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// policyLevelPrecedence 严格度排序（数字越大越严）。max 取最严。
	policyLevelPrecedence = map[string]int{
		policyAllowLevel: 0,
		policyAskLevel:   1,
		policyDenyLevel:  2,
	}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewOpenJiuwenPolicyEvaluator 创建策略评估器。
//
// permissionConfigGetter 与 workspaceRoot 可为 nil / 空。
//
// Python: OpenJiuwenPolicyEvaluator.__init__(base_rail, *, permission_config_getter=None,
//
//	workspace_root=None, path_guard_evaluator=None)
func NewOpenJiuwenPolicyEvaluator(
	baseRail PolicyBaseRail,
	permissionConfigGetter func() map[string]any,
	workspaceRoot string,
) *OpenJiuwenPolicyEvaluator {
	return &OpenJiuwenPolicyEvaluator{
		baseRail:               baseRail,
		permissionConfigGetter: permissionConfigGetter,
		workspaceRoot:          workspaceRoot,
	}
}

// Evaluate 评估策略，不进入人工确认流程。
//
// 七步顺序不可调换（对齐 Python evaluate）：
//  1. 归一化工具名、解析参数
//  2. 取生效配置；配置刷新失败时记 refresh_failed
//  3. apply 一次 config（先于场景钩子，让钩子看到最新配置）
//  4. 场景钩子：DENY 或「场景审批即最终」时直接返回
//  5. 引擎不可用 / 刷新失败 / 检查异常 → fail_closed ASK；否则再 apply 一次
//     config（场景钩子可能 await 过，配置可能已被改）后调引擎
//  6. send_file 分支：path_result（9.90e 前恒 nil）
//  7. 三者取最严并合并 matched_rule 与 external_paths
//
// Python: OpenJiuwenPolicyEvaluator.evaluate(invocation, *, scene_approval_is_final=False)
func (e *OpenJiuwenPolicyEvaluator) Evaluate(invocation PolicyInvocation, sceneApprovalIsFinal bool) PolicyEvaluation {
	normalizedToolName := e.normalizeToolName(invocation.ToolName)
	toolArgs := e.parseToolArgs(invocation.ToolCall, invocation.ToolArgs)

	engine := e.engine()
	effectiveConfig := e.effectivePermissionConfig()

	// refresh_failed：配置取不到但宿主提供了快照钩子（说明本应能取到，
	// 取不到即是刷新失败，必须 fail-closed）。
	refreshFailed := false
	if effectiveConfig == nil && e.baseRail != nil && e.baseRail.HasPermissionsSnapshot() {
		refreshFailed = true
	}
	if engine != nil && effectiveConfig != nil {
		if err := e.applyConfig(engine, effectiveConfig); err != nil {
			refreshFailed = true
		}
	}

	// 场景钩子
	sceneDecision := e.evaluateSceneHook(invocation, normalizedToolName, toolArgs, engine)
	if sceneDecision != nil && (sceneDecision.Level == policyDenyLevel || sceneApprovalIsFinal) {
		return *sceneDecision
	}

	// 引擎判定
	var engineDecision PolicyEvaluation
	switch {
	case engine == nil:
		engineDecision = PolicyEvaluation{
			Level:  policyAskLevel,
			Reason: "policy_engine_unavailable",
			Source: "fail_closed",
		}
	case refreshFailed:
		engineDecision = PolicyEvaluation{
			Level:  policyAskLevel,
			Reason: "policy_config_refresh_failed",
			Source: "fail_closed",
		}
	default:
		// 场景钩子可能 await 过其他回调，引擎配置可能已被别处改掉；
		// 同步管线开始前再 apply 一次本次捕获的配置。
		if effectiveConfig != nil {
			_ = e.applyConfig(engine, effectiveConfig)
		}
		engineDecision = evaluationFromEngine(engine.CheckPermission(normalizedToolName, toolArgs))
	}

	// send_file 路径守卫：⤵️ 9.90e 回填
	// SendFilePathGuardEvaluator 未实现，path_result 恒 nil。
	// 9.90e 落地 send_file_path_guard.py（315 行）后在此构造并调用。
	var pathResult *SendFilePathGuardResult
	if normalizedToolName == sendFileToolName {
		pathResult = nil
	}

	return strictestEvaluation(
		[]*PolicyEvaluation{sceneDecision, &engineDecision, evaluationFromPathResult(pathResult)},
		pathResult,
	)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// normalizeToolName 先试 base rail 的归一化，再经 permission_tools 归一。
//
// Python: OpenJiuwenPolicyEvaluator._normalize_tool_name(tool_name)
func (e *OpenJiuwenPolicyEvaluator) normalizeToolName(toolName string) string {
	if e.baseRail != nil {
		if normalized := e.baseRail.NormalizeToolName(toolName); strings.TrimSpace(normalized) != "" {
			return NormalizeToolName(normalized)
		}
	}
	return NormalizeToolName(toolName)
}

// parseToolArgs 依次尝试 base rail 解析、map 直取、JSON 字符串解码。
//
// Python: OpenJiuwenPolicyEvaluator._parse_tool_args(tool_call, fallback_args)
func (e *OpenJiuwenPolicyEvaluator) parseToolArgs(toolCall any, fallbackArgs any) map[string]any {
	if e.baseRail != nil {
		if parsed := e.baseRail.ParseToolArgs(toolCall); parsed != nil {
			return parsed
		}
	}
	if m, ok := fallbackArgs.(map[string]any); ok {
		out := make(map[string]any, len(m))
		for k, v := range m {
			out[k] = v
		}
		return out
	}
	if s, ok := fallbackArgs.(string); ok {
		var decoded map[string]any
		if err := json.Unmarshal([]byte(s), &decoded); err != nil {
			return map[string]any{}
		}
		return decoded
	}
	return map[string]any{}
}

// evaluateSceneHook 调场景钩子；异常或返回格式不符时返回 nil（不短路）。
//
// Python: OpenJiuwenPolicyEvaluator._evaluate_scene_hook(...)
func (e *OpenJiuwenPolicyEvaluator) evaluateSceneHook(
	invocation PolicyInvocation,
	normalizedToolName string,
	toolArgs map[string]any,
	engine PolicyEngine,
) *PolicyEvaluation {
	if e.baseRail == nil {
		return nil
	}
	hook := e.baseRail.PermissionSceneHook()
	if hook == nil {
		return nil
	}
	result, err := hook(invocation, normalizedToolName, toolArgs, engine)
	if err != nil || len(result) == 0 {
		return nil
	}
	decision := strings.ToLower(strings.TrimSpace(result[0]))
	switch decision {
	case "approve":
		return &PolicyEvaluation{
			Level:  policyAllowLevel,
			Reason: "scene_hook_approved",
			Source: "scene_hook",
		}
	case "reject":
		reason := "scene_hook_rejected"
		if len(result) > 1 && strings.TrimSpace(result[1]) != "" {
			reason = strings.TrimSpace(result[1])
		}
		return &PolicyEvaluation{
			Level:  policyDenyLevel,
			Reason: reason,
			Source: "scene_hook",
		}
	}
	return nil
}

// effectivePermissionConfig 按优先级取生效配置。
//
// 三层（对齐 Python _effective_permission_config）：
//  1. runtime getter（若配置成功返回非 nil，用它）
//  2. 宿主配置快照（若快照非 nil 用它；若有快照钩子但取不到 → 返回 nil
//     表示刷新失败，由调用方 fail-closed）
//  3. 静态配置
//
// Python: OpenJiuwenPolicyEvaluator._effective_permission_config()
func (e *OpenJiuwenPolicyEvaluator) effectivePermissionConfig() map[string]any {
	if e.permissionConfigGetter != nil {
		if cfg := e.permissionConfigGetter(); cfg != nil {
			return cfg
		}
	}
	if e.baseRail != nil {
		if snap := e.baseRail.PermissionsSnapshot(); snap != nil {
			return snap
		}
	}
	if e.baseRail != nil {
		if staticCfg := e.baseRail.StaticConfig(); staticCfg != nil {
			return staticCfg
		}
	}
	return nil
}

// applyConfig 热更新配置：优先 rail 的 update_config，其次 engine 的。
//
// Python: OpenJiuwenPolicyEvaluator._apply_config(engine, config)
func (e *OpenJiuwenPolicyEvaluator) applyConfig(engine PolicyEngine, config map[string]any) error {
	if e.baseRail != nil {
		e.baseRail.UpdateConfig(config)
		return nil
	}
	if engine != nil {
		engine.UpdateConfig(config)
	}
	return nil
}

// engine 返回权限引擎（可为 nil）。
func (e *OpenJiuwenPolicyEvaluator) engine() PolicyEngine {
	if e.baseRail == nil {
		return nil
	}
	return e.baseRail.Engine()
}

// evaluationFromEngine 把引擎结果转为 PolicyEvaluation。
//
// 未知 level 或 external_paths 格式非法且非 DENY 时 fail-closed 到 ASK
// ——external_paths 非法说明上游产出了不合规数据，不能直接放行。
//
// Python: _evaluation_from_permission_result(result)
func evaluationFromEngine(result *PolicyPermissionResult) PolicyEvaluation {
	if result == nil {
		return PolicyEvaluation{
			Level:  policyAskLevel,
			Reason: "unknown_policy_result",
			Source: "fail_closed",
		}
	}
	level := strings.ToLower(strings.TrimSpace(result.Permission))
	if level != policyAllowLevel && level != policyAskLevel && level != policyDenyLevel {
		return PolicyEvaluation{
			Level:  policyAskLevel,
			Reason: "unknown_policy_result",
			Source: "fail_closed",
		}
	}
	reason := result.Reason
	if reason == "" {
		reason = level
	}
	externalPaths, ok := validatedExternalPaths(result.ExternalPaths)
	if !ok && level != policyDenyLevel {
		return PolicyEvaluation{
			Level:       policyAskLevel,
			Reason:      "malformed_policy_external_paths",
			MatchedRule: result.MatchedRule,
			Source:      "fail_closed",
		}
	}
	return PolicyEvaluation{
		Level:                  level,
		Reason:                 reason,
		MatchedRule:            result.MatchedRule,
		Source:                 "permission_engine",
		ExternalPaths:          externalPaths,
		ManualApprovalRequired: result.ManualApprovalRequired,
	}
}

// validatedExternalPaths 校验外部路径列表的三道上限。
//
// 返回 (paths, ok)；ok=false 表示格式非法（任一条非字符串/空白/超长，
// 或总数超限）。nil 输入视为合法的空列表。
//
// Python: _validated_external_paths(value)
func validatedExternalPaths(value []string) ([]string, bool) {
	if value == nil {
		return nil, true
	}
	if len(value) > maxExternalPaths {
		return nil, false
	}
	paths := make([]string, 0, len(value))
	totalLength := 0
	for _, item := range value {
		if strings.TrimSpace(item) == "" {
			return nil, false
		}
		if len(item) > maxExternalPathLength {
			return nil, false
		}
		totalLength += len(item)
		if totalLength > maxExternalPathsTotalLength {
			return nil, false
		}
		paths = append(paths, item)
	}
	return paths, true
}

// evaluationFromPathResult 把路径守卫结果转为 PolicyEvaluation。
//
// nil 或 level=="neutral"（无意见）返回 nil。
//
// Python: _evaluation_from_path_result(result)
func evaluationFromPathResult(result *SendFilePathGuardResult) *PolicyEvaluation {
	if result == nil || result.Level == "neutral" {
		return nil
	}
	return &PolicyEvaluation{
		Level:       result.Level,
		Reason:      result.Reason,
		MatchedRule: strings.Join(result.MatchedRules, "|"),
		Source:      "file_guard_adapter",
		PathResult:  result,
	}
}

// strictestEvaluation 多个评估取最严，并合并 matched_rule 与 external_paths。
//
// 三处细节（对齐 Python _strictest_evaluation）：
//  1. 取 level 最严的那条的 reason 与 source（不是拼接）
//  2. matched_rule 按 | 拆开去重后重新拼接（保持首次出现顺序）
//  3. manual_approval_required 是**任一**为真即真
//
// Python: _strictest_evaluation(*evaluations, path_result)
func strictestEvaluation(evaluations []*PolicyEvaluation, pathResult *SendFilePathGuardResult) PolicyEvaluation {
	candidates := make([]*PolicyEvaluation, 0, len(evaluations))
	for _, item := range evaluations {
		if item != nil {
			candidates = append(candidates, item)
		}
	}
	if len(candidates) == 0 {
		return PolicyEvaluation{Level: policyAskLevel, Source: "fail_closed"}
	}

	strictest := candidates[0]
	strictestRank := levelRank(strictest.Level)
	for _, item := range candidates[1:] {
		if rank := levelRank(item.Level); rank > strictestRank {
			strictest = item
			strictestRank = rank
		}
	}

	matchedRules := make([]string, 0, 4)
	externalPaths := make([]string, 0, 4)
	manualRequired := false
	for _, item := range candidates {
		for _, rule := range strings.Split(item.MatchedRule, "|") {
			if rule == "" {
				continue
			}
			if !containsStringValue(matchedRules, rule) {
				matchedRules = append(matchedRules, rule)
			}
		}
		for _, path := range item.ExternalPaths {
			if !containsStringValue(externalPaths, path) {
				externalPaths = append(externalPaths, path)
			}
		}
		if item.ManualApprovalRequired {
			manualRequired = true
		}
	}

	return PolicyEvaluation{
		Level:                  strictest.Level,
		Reason:                 strictest.Reason,
		MatchedRule:            strings.Join(matchedRules, "|"),
		Source:                 strictest.Source,
		PathResult:             pathResult,
		ExternalPaths:          externalPaths,
		ManualApprovalRequired: manualRequired,
	}
}

// levelRank 返回级别严格度排名；未知级别按 ask 处理。
func levelRank(level string) int {
	if rank, ok := policyLevelPrecedence[level]; ok {
		return rank
	}
	return policyLevelPrecedence[policyAskLevel]
}

// containsStringValue 判定切片是否含指定值。
func containsStringValue(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
