package rails

import (
	"sync"

	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// RailProvider 把一个 RailSpec 的 type+params 解析成活 rail 实例。
//
// 返回 any 的原因：一个 provider 可能返回单个 rail、rail 列表或 nil
// （对齐上游："A rail, a list of rails, or None; callers flatten the result"）。
//
// ctx 用具体接口类型而非 any：注册表本身不碰团队类型，
// BuildContext.Extras 是唯一的团队句柄通道，因此不引入 any 债。
//
// Python: _RAIL_PROVIDER_REGISTRY 的工厂契约
// (openjiuwen/harness/schema/deep_agent_spec.py:60-78)
type RailProvider func(params map[string]any, ctx hschema.BuildContextInterface) (any, error)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// railRegistryMu 保护 railRegistry。
	// 高频读（每次构造成员）+ 低频写（注册在 init / 首次构建），RWMutex 优于 sync.Map。
	//
	// Go 特有：Python 单线程 asyncio 无此问题；Go 必须显式同步，
	// 因为注册可能在某个成员的首次构建里发生，而其他成员可能同时在读。
	railRegistryMu sync.RWMutex
	// railRegistry type 名 → provider
	railRegistry = make(map[string]RailProvider)
)

// ──────────────────────────── 导出函数 ────────────────────────────

// RegisterRailProvider 注册一个 rail provider（同名覆盖，幂等）。
//
// Python: register_rail_provider
// (openjiuwen/harness/schema/deep_agent_spec.py:71-78)
func RegisterRailProvider(name string, f RailProvider) {
	railRegistryMu.Lock()
	defer railRegistryMu.Unlock()
	railRegistry[name] = f
}

// LookupRailProvider 查表；未注册返回 (nil, false)。
//
// 调用方（RailSpec.Build）对未注册的处理是"warn + 跳过"，
// 不是报错——老版本持久化的 spec 可能引用已删除的 rail。
func LookupRailProvider(name string) (RailProvider, bool) {
	railRegistryMu.RLock()
	defer railRegistryMu.RUnlock()
	provider, ok := railRegistry[name]
	return provider, ok
}

// RegisteredRailNames 返回已注册的全部 type 名（测试与诊断用）。
//
// 返回顺序不确定（map 遍历），调用方需自行排序。
func RegisteredRailNames() []string {
	railRegistryMu.RLock()
	defer railRegistryMu.RUnlock()
	names := make([]string, 0, len(railRegistry))
	for name := range railRegistry {
		names = append(names, name)
	}
	return names
}

// ResetRailRegistryForTest 清空注册表（仅测试用）。
//
// 生产不得调用：清空会让所有 RailSpec 解析失败（走"未知 type"分支跳过），
// 表现为成员一个 rail 都挂不上且只有 warn 日志。
func ResetRailRegistryForTest() {
	railRegistryMu.Lock()
	defer railRegistryMu.Unlock()
	railRegistry = make(map[string]RailProvider)
}
