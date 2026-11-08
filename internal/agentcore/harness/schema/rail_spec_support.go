package schema

import (
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// builtinElementsHookMu 保护 builtinElementsHook
	builtinElementsHookMu sync.RWMutex
	// builtinElementsHook 内置元素注册钩子（由上层包注入）
	//
	// 存在的原因：上游 RailSpec.build 第一步调
	// `ensure_builtin_elements_registered()`，该函数属于 harness.manifest 包。
	// Go 侧 schema 是底层包，不能 import 上层；改为上层在 init 阶段把
	// 自己的注册入口挂进来。未注入时本钩子为 no-op（团队 rail 仍由
	// agent_teams 侧显式注册，不依赖本钩子）。
	builtinElementsHook func()
)

// ──────────────────────────── 导出函数 ────────────────────────────

// SetBuiltinElementsRegisteredHook 注入内置元素注册钩子（由上层包在 init 调用）。
//
// 对齐上游 RailSpec.build 第一步的 ensure_builtin_elements_registered：
// Go 侧把"上层保证内置元素已注册"这件事变成可注入钩子，避免 schema
// 反向 import 上层。
//
// 幂等：后注入者覆盖前者；传 nil 表示清除。
func SetBuiltinElementsRegisteredHook(hook func()) {
	builtinElementsHookMu.Lock()
	defer builtinElementsHookMu.Unlock()
	builtinElementsHook = hook
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// ensureBuiltinElementsRegisteredHook 调一次内置元素注册钩子（无钩子则 no-op）。
//
// Python: ensure_builtin_elements_registered() (deep_agent_spec.py:280)
func ensureBuiltinElementsRegisteredHook() {
	builtinElementsHookMu.RLock()
	hook := builtinElementsHook
	builtinElementsHookMu.RUnlock()
	if hook != nil {
		hook()
	}
}

// logRailTypeUnknown 记录"未知 rail 类型"告警。
//
// 消息逐字对齐上游（deep_agent_spec.py:283-289），因为它专门用于诊断
// "旧版本 checkpoint 引用了已删除的 rail"。
func logRailTypeUnknown(railType string) {
	logger.Warn(logger.ComponentAgentCore).
		Str("rail_type", railType).
		Msg("Unknown rail type in RailSpec; skipping it. This usually means the rail was removed or renamed in a newer release while the spec still references the old name (e.g. a session checkpoint persisted by an older version).")
}

// newMinimalBuildContext 合成最小构建上下文（含 language 与 workspace）。
//
// Python: BuildContext(language=language, workspace=workspace)
// (deep_agent_spec.py:292)
//
func newMinimalBuildContext(language string, ws *workspace.Workspace) BuildContextInterface {
	return &BuildContext{Language: language, Workspace: ws}
}
