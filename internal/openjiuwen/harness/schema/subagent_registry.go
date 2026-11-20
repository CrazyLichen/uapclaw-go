package schema

import "sync"

// ──────────────────────────── 结构体 ────────────────────────────

// SubagentProvider 按 factory_name 把 factory_kwargs 解析成完整 SubAgentConfig。
//
// 命中时 provider 直接产出完整子代理配置（跳过正常路径的字段解析）；
// 未命中由调用方落到正常路径。factory_name 形态参考：
// "swarm.browser_agent" / "swarm.cua_agent" / "custom_<name>"。
//
// Python: _SUBAGENT_PROVIDER_REGISTRY 的工厂契约；查表分支
// (openjiuwen/harness/schema/deep_agent_spec.py:383-384)，注册函数
// openjiuwen/harness/manifest/registration.py:47
type SubagentProvider func(factoryKwargs map[string]any, ctx BuildContextInterface) (*SubAgentConfig, error)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// subagentRegistryMu 保护 subagentRegistry。
	subagentRegistryMu sync.RWMutex
	// subagentRegistry factory_name → provider
	subagentRegistry = make(map[string]SubagentProvider)
)

// ──────────────────────────── 导出函数 ────────────────────────────

// RegisterSubagentProvider 注册一个子代理 provider（同名覆盖，幂等）。
//
// Python: register_subagent_provider
// (openjiuwen/harness/manifest/registration.py:47)，注册表本体与
// SubAgentSpec 同在 deep_agent_spec.py（同 ToolProvider 的归属理由）。
func RegisterSubagentProvider(name string, f SubagentProvider) {
	subagentRegistryMu.Lock()
	defer subagentRegistryMu.Unlock()
	subagentRegistry[name] = f
}

// LookupSubagentProvider 查表；未注册返回 (nil, false)。
func LookupSubagentProvider(name string) (SubagentProvider, bool) {
	subagentRegistryMu.RLock()
	defer subagentRegistryMu.RUnlock()
	provider, ok := subagentRegistry[name]
	return provider, ok
}

// RegisteredSubagentNames 返回已注册的全部 factory_name（测试与诊断用）。
//
// 返回顺序不确定（map 遍历），调用方需自行排序。
func RegisteredSubagentNames() []string {
	subagentRegistryMu.RLock()
	defer subagentRegistryMu.RUnlock()
	names := make([]string, 0, len(subagentRegistry))
	for name := range subagentRegistry {
		names = append(names, name)
	}
	return names
}

// ResetSubagentRegistryForTest 清空注册表（仅测试用）。
//
// 生产不得调用：清空后所有 factory_name 分支不命中，subagent 全部
// 落到正常路径（对齐 Python if 条件不成立的语义）。
func ResetSubagentRegistryForTest() {
	subagentRegistryMu.Lock()
	defer subagentRegistryMu.Unlock()
	subagentRegistry = make(map[string]SubagentProvider)
}
