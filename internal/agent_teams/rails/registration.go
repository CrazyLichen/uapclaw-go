package rails

import "sync"

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// registrationMu 保护 registrationDone
	registrationMu sync.Mutex
	// registrationDone 是否已完成注册（对齐上游 _REGISTERED 守卫）
	registrationDone bool
)

// ──────────────────────────── 导出函数 ────────────────────────────

// EnsureHarnessElementsRegistered 注册团队 rail 与 harness 内置项。
//
// 三步，顺序不可换（对齐上游 registration.py:28-64）：
//  1. 基础内置元素注册（core.* 内置）——本仓无 manifest catalog，
//     等价物见 ensureBuiltinRailProviders 的说明
//  2. 注册 8 个团队 rail（等价于上游 import team elements 的副作用）
//  3. 补跑同步——本仓无 catalog，退化为 no-op（偏差 D-9）
//
// 第 3 步为什么在上游必须：harness 的 _REGISTERED 守卫会在第 1 步短路，
// 此时 team 描述符还没进目录，若不补跑就永远到不了 provider 注册表。
// Go 无 catalog 机制，所以第 3 步不需要；但**第一步的短路语义要保留**：
// 团队注册必须独立于基础注册的完成状态（本实现用独立守卫保证）。
//
// 幂等：只有首次调用做事，重复调用直接短路。
//
// Python: ensure_harness_elements_registered
// (openjiuwen/agent_teams/rails/registration.py:28-64)
func EnsureHarnessElementsRegistered() {
	registrationMu.Lock()
	defer registrationMu.Unlock()
	if registrationDone {
		return
	}
	// 步骤 1：基础内置
	ensureBuiltinRailProviders()
	// 步骤 2：团队 rail
	RegisterTeamRailProviders()
	// 步骤 3：补跑同步（本仓无 catalog，no-op；见函数注释与偏差 D-9）
	registerFromCatalogHook()
	registrationDone = true
}

// ResetRegistrationForTest 重置注册守卫（仅测试用）。
//
// 生产不得调用：会让下次 EnsureHarnessElementsRegistered 重跑注册。
func ResetRegistrationForTest() {
	registrationMu.Lock()
	defer registrationMu.Unlock()
	registrationDone = false
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// ensureBuiltinRailProviders 注册 core 内置 rail 的 provider。
//
// ⤵️ 偏差 D-9：上游靠 manifest catalog + register_from_catalog 自动同步；
// 本仓 core 层无 catalog 机制。当前**不注册任何内置 rail**——core 侧的
// 默认 rail（Security/TaskPlanning/SkillUse/Subagent）仍由 CreateDeepAgent
// 的 addDefaultRails 自动补齐，不走 RailSpec 声明。
//
// 将来若要把内置 rail 也改成 spec 声明，在此补注册即可（调用方不变）。
func ensureBuiltinRailProviders() {
	// 本仓暂无需要通过 RailSpec 声明的内置 rail。
	// 保留本函数是为了让 EnsureHarnessElementsRegistered 的三步结构
	// 与上游一一对应，将来补内置 rail 时不必调整调用方。
}

// registerFromCatalogHook 目录同步的等价物（本仓为 no-op）。
//
// ⤵️ 偏差 D-9：上游的 register_from_catalog 遍历 manifest catalog，
// 按 ElementKind 分派到 register_rail/tool/subagent_provider。本仓无 catalog，
// 团队 rail 已在步骤 2 直接注册，因此这里无事可做。
//
// 保留本函数使三步结构与上游保持一一对应，将来引入 catalog 时在此接入。
func registerFromCatalogHook() {
	// no-op：见上方偏差 D-9 说明。
}
