package interfaces

// ──────────────────────────── 结构体 ────────────────────────────

// ExposurePolicyController 注册期曝光策略控制。
// 与 AbilityManagerInterface 分列：后者已有多个实现方，
// 曝光策略与注册版本只由 AbilityManager 提供，故独立成单方法窄接口。
// Python: AbilityManager.set_tool_exposure_policy
type ExposurePolicyController interface {
	// SetToolExposurePolicy 设置后续注册使用的默认曝光策略
	SetToolExposurePolicy(progressiveToolEnabled bool, directToolNames []string)
}

// RegistryRevisionProvider 注册表版本读取。
// Python: AbilityManager.registry_revision
type RegistryRevisionProvider interface {
	// RegistryRevision 返回注册表单调递增版本
	RegistryRevision() int
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────
