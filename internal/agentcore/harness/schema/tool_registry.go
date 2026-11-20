package schema

import "sync"

// ──────────────────────────── 结构体 ────────────────────────────

// ToolProvider 把一个 BuiltinToolSpec 的 type+params 解析成工具实例列表。
//
// 返回 []any 的原因：一个 provider 可能返回单个工具、工具列表或 nil
// （对齐上游 _as_built_list："a tool, a list of tools, or None; callers flatten"）。
//
// ctx 用具体接口类型而非 any：注册表本身不碰团队类型，
// BuildContext.Extras 是唯一的团队句柄通道，因此不引入 any 债。
//
// Python: _TOOL_PROVIDER_REGISTRY 的工厂契约
// (openjiuwen/harness/schema/deep_agent_spec.py:67)
type ToolProvider func(params map[string]any, ctx BuildContextInterface) ([]any, error)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// toolRegistryMu 保护 toolRegistry。
	// 高频读（每次构造成员）+ 低频写（注册段一次性），RWMutex 优于 sync.Map。
	//
	// Go 特有：Python 单线程 asyncio 无此问题；Go 必须显式同步。
	toolRegistryMu sync.RWMutex
	// toolRegistry type 名 → provider
	toolRegistry = make(map[string]ToolProvider)
)

// ──────────────────────────── 导出函数 ────────────────────────────

// RegisterToolProvider 注册一个工具 provider（同名覆盖，幂等）。
//
// 放在 schema 包而非 tools 包，对齐上游同文件结构：
// _TOOL_PROVIDER_REGISTRY 与 BuiltinToolSpec 定义同在 deep_agent_spec.py，
// BuiltinToolSpec.build 直接查本地 dict；Go 侧 schema 包不能 import
// harness（成环），注册表必须与 RailProvider 同放 schema。
//
// Python: register_tool_provider
// (openjiuwen/harness/schema/deep_agent_spec.py:81-88)
func RegisterToolProvider(name string, f ToolProvider) {
	toolRegistryMu.Lock()
	defer toolRegistryMu.Unlock()
	toolRegistry[name] = f
}

// LookupToolProvider 查表；未注册返回 (nil, false)。
//
// 调用方（BuiltinToolSpec.Build）对未注册的处理是"warn + 跳过"，
// 不是报错——老版本持久化的 spec 可能引用已删除的工具。
func LookupToolProvider(name string) (ToolProvider, bool) {
	toolRegistryMu.RLock()
	defer toolRegistryMu.RUnlock()
	provider, ok := toolRegistry[name]
	return provider, ok
}

// RegisteredToolNames 返回已注册的全部 type 名（测试与诊断用）。
//
// 返回顺序不确定（map 遍历），调用方需自行排序。
func RegisteredToolNames() []string {
	toolRegistryMu.RLock()
	defer toolRegistryMu.RUnlock()
	names := make([]string, 0, len(toolRegistry))
	for name := range toolRegistry {
		names = append(names, name)
	}
	return names
}

// ResetToolRegistryForTest 清空注册表（仅测试用）。
//
// 生产不得调用：清空会让所有 BuiltinToolSpec 解析失败（走"未知 type"
// 分支跳过），表现为成员一个声明的工具都挂不上且只有 warn 日志。
func ResetToolRegistryForTest() {
	toolRegistryMu.Lock()
	defer toolRegistryMu.Unlock()
	toolRegistry = make(map[string]ToolProvider)
}
