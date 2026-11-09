// PermissionInterruptRail 的可注入派发点与导出访问器。
//
// 背景（9.90d-1 Task 13 前置基础设施）：
// Python 的 JiuwenSwarmPermissionInterruptRail **继承** PermissionInterruptRail，
// 覆写 _persist_allow_always / _persist_merged_allow /
// _collect_file_guard_persist_accesses / _get_user_input 等方法；父类方法体内部
// 的 self.xxx() 调用会**动态派发**到子类覆写版本（Python MRO）。
//
// Go 的嵌入不做虚函数派发——父类方法体内 r.persistAllowAlways(...) 静态绑定
// 父类实现。为在跨包场景复刻 Python 语义，本文件给父类加一组可选覆写钩子：
// 父类内部调用前先查 override，覆写方返回 handled=true 则采用其结果，
// handled=false 则回落父类默认实现（对齐 Python 的 `super().xxx()` 分支）。
//
// 同时导出子类必需的访问器（原为私有字段/方法/包级函数）。

package security

import (
	"context"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/interrupt"
	harnesssecurity "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/security"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
)

// ──────────────────────────── 结构体 ────────────────────────────

// PermissionInterruptRailOverride 子类对父类内部调用点的派发覆写集合。
//
// 每个函数返回 (结果, handled)：handled=true 表示覆写方已处理并采用该结果；
// handled=false 表示回落父类默认实现。
//
// Python 对应：JiuwenSwarmPermissionInterruptRail 覆写的 5 个父类方法
// （permission_interrupt_rail.py:140/201/355/377/384）。
type PermissionInterruptRailOverride struct {
	// PersistAllowAlways 覆写 _persist_allow_always
	PersistAllowAlways func(ctx context.Context, normalizedName string, toolArgs map[string]any) (bool, bool)
	// PersistSessionAllow 覆写 _persist_session_allow
	PersistSessionAllow func(
		ctx context.Context, normalizedName string, toolArgs map[string]any, sessionID string,
	) (bool, bool)
	// PersistMergedAllow 覆写 _persist_merged_allow
	PersistMergedAllow func(
		ctx context.Context,
		normalizedName string,
		toolArgs map[string]any,
		persistHook func(map[string]any) bool,
		writeYAMLFallback bool,
	) (bool, bool)
	// CollectFileGuardPersistAccesses 覆写 _collect_file_guard_persist_accesses
	CollectFileGuardPersistAccesses func(
		normalizedName string,
		toolArgs map[string]any,
		permissionsCfg map[string]any,
	) ([]harnesssecurity.PathAction, bool)
	// GetUserInput 覆写 _get_user_input
	GetUserInput func(cbc *agentinterfaces.AgentCallbackContext, toolCallID string) (any, bool)
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// SetOverride 注入子类的派发覆写集合。
//
// 传 nil 清除覆写，恢复父类默认行为。
func (r *PermissionInterruptRail) SetOverride(override *PermissionInterruptRailOverride) {
	r.override = override
}

// Host 返回权限宿主回调（Python self._host）。
func (r *PermissionInterruptRail) Host() *harnesssecurity.ToolPermissionHost {
	return r.host
}

// StaticConfig 返回静态权限配置（Python self._static_config）。
func (r *PermissionInterruptRail) StaticConfig() map[string]any {
	return r.staticConfig
}

// NormalizeToolName 归一化工具名（Python self._normalize_tool_name）。
func (r *PermissionInterruptRail) NormalizeToolName(toolName string) string {
	return r.normalizeToolName(toolName)
}

// AutoConfirmKey 计算自动确认键（Python self._get_auto_confirm_key）。
func (r *PermissionInterruptRail) AutoConfirmKey(toolCall *llmschema.ToolCall) string {
	return r.getAutoConfirmKey(toolCall)
}

// CollectFileGuardPersistAccesses 收集 file_guard 持久化访问项。
//
// Python: self._collect_file_guard_persist_accesses(...)
// 内部先查 override（子类覆写），未处理则走父类默认。
func (r *PermissionInterruptRail) CollectFileGuardPersistAccesses(
	normalizedName string,
	toolArgs map[string]any,
	permissionsCfg map[string]any,
) []harnesssecurity.PathAction {
	return r.collectFileGuardPersistAccesses(normalizedName, toolArgs, permissionsCfg)
}

// GetUserInput 取用户输入（Python self._get_user_input）。
func (r *PermissionInterruptRail) GetUserInput(
	cbc *agentinterfaces.AgentCallbackContext,
	toolCallID string,
) any {
	return r.getUserInput(cbc, toolCallID)
}

// PersistAllowAlways 执行「总是允许」持久化（Python self._persist_allow_always）。
func (r *PermissionInterruptRail) PersistAllowAlways(
	ctx context.Context,
	normalizedName string,
	toolArgs map[string]any,
) bool {
	return r.persistAllowAlways(ctx, normalizedName, toolArgs)
}

// PersistSessionAllow 执行「会话内记住」持久化（Python self._persist_session_allow）。
func (r *PermissionInterruptRail) PersistSessionAllow(
	ctx context.Context,
	normalizedName string,
	toolArgs map[string]any,
	sessionID string,
) bool {
	return r.persistSessionAllow(ctx, normalizedName, toolArgs, sessionID)
}

// PersistMergedAllow 执行合并后持久化（Python self._persist_merged_allow）。
func (r *PermissionInterruptRail) PersistMergedAllow(
	ctx context.Context,
	normalizedName string,
	toolArgs map[string]any,
	persistHook func(map[string]any) bool,
	writeYAMLFallback bool,
) bool {
	return r.persistMergedAllow(ctx, normalizedName, toolArgs, persistHook, writeYAMLFallback)
}

// ParseToolArgs 解析工具调用参数（Python self.parse_tool_args）。
func ParseToolArgs(toolCall *llmschema.ToolCall) map[string]any {
	return parseToolArgs(toolCall)
}

// ParseConfirmPayload 解析用户确认载荷（Python self.parse_confirm_payload）。
func ParseConfirmPayload(userInput any) *harnesssecurity.PermissionConfirmResponse {
	return parseConfirmPayload(userInput)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// overridePersistAllowAlways 查询 PersistAllowAlways 覆写。
func (r *PermissionInterruptRail) overridePersistAllowAlways(
	ctx context.Context,
	normalizedName string,
	toolArgs map[string]any,
) (bool, bool) {
	if r.override == nil || r.override.PersistAllowAlways == nil {
		return false, false
	}
	return r.override.PersistAllowAlways(ctx, normalizedName, toolArgs)
}

// overridePersistSessionAllow 查询 PersistSessionAllow 覆写。
func (r *PermissionInterruptRail) overridePersistSessionAllow(
	ctx context.Context,
	normalizedName string,
	toolArgs map[string]any,
	sessionID string,
) (bool, bool) {
	if r.override == nil || r.override.PersistSessionAllow == nil {
		return false, false
	}
	return r.override.PersistSessionAllow(ctx, normalizedName, toolArgs, sessionID)
}

// overridePersistMergedAllow 查询 PersistMergedAllow 覆写。
func (r *PermissionInterruptRail) overridePersistMergedAllow(
	ctx context.Context,
	normalizedName string,
	toolArgs map[string]any,
	persistHook func(map[string]any) bool,
	writeYAMLFallback bool,
) (bool, bool) {
	if r.override == nil || r.override.PersistMergedAllow == nil {
		return false, false
	}
	return r.override.PersistMergedAllow(ctx, normalizedName, toolArgs, persistHook, writeYAMLFallback)
}

// overrideCollectFileGuardPersistAccesses 查询 CollectFileGuardPersistAccesses 覆写。
func (r *PermissionInterruptRail) overrideCollectFileGuardPersistAccesses(
	normalizedName string,
	toolArgs map[string]any,
	permissionsCfg map[string]any,
) ([]harnesssecurity.PathAction, bool) {
	if r.override == nil || r.override.CollectFileGuardPersistAccesses == nil {
		return nil, false
	}
	return r.override.CollectFileGuardPersistAccesses(normalizedName, toolArgs, permissionsCfg)
}

// overrideGetUserInput 查询 GetUserInput 覆写。
func (r *PermissionInterruptRail) overrideGetUserInput(
	cbc *agentinterfaces.AgentCallbackContext,
	toolCallID string,
) (any, bool) {
	if r.override == nil || r.override.GetUserInput == nil {
		return nil, false
	}
	return r.override.GetUserInput(cbc, toolCallID)
}

// ResolvePermissionInterrupt 导出父类的中断解析（Python super().resolve_interrupt）。
//
// 子类在请求级策略包裹下调用本方法，语义等同 Python 的
// `await super().resolve_interrupt(...)`。
func (r *PermissionInterruptRail) ResolvePermissionInterrupt(
	ctx context.Context,
	cbc *agentinterfaces.AgentCallbackContext,
	toolCall *llmschema.ToolCall,
	userInput any,
	autoConfirmConfig map[string]any,
) interrupt.InterruptDecision {
	return r.resolvePermissionInterrupt(ctx, cbc, toolCall, userInput, autoConfirmConfig)
}

// CollectFileGuardPersistAccessesDefault 父类默认实现（跳过 override）。
//
// 子类在 override 内需要「回落父类默认」时必须调本方法而非
// CollectFileGuardPersistAccesses，否则会形成
// 子类 → 父类导出方法 → override → 子类 的无限递归。
func (r *PermissionInterruptRail) CollectFileGuardPersistAccessesDefault(
	normalizedName string,
	toolArgs map[string]any,
	permissionsCfg map[string]any,
) []harnesssecurity.PathAction {
	return r.collectFileGuardPersistAccessesDefault(normalizedName, toolArgs, permissionsCfg)
}
