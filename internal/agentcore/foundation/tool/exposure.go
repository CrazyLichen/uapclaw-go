package tool

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ToolExposure 描述工具何时可出现在模型可见的工具清单中。
// 该值属于运行时策略元数据，不写入模型可见的 ToolInfo schema。
// Python: openjiuwen/core/foundation/tool/exposure.py (ToolExposure)
type ToolExposure string

const (
	// ToolExposureDirect 直接可调用（出现在顶层 tools）
	ToolExposureDirect ToolExposure = "direct"
	// ToolExposureDeferred 延迟暴露（须先经 tool_search 发现）
	ToolExposureDeferred ToolExposure = "deferred"
)

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// EffectiveExposure 返回卡片生效的曝光策略，零值按 direct 处理。
// Python: ToolCard.exposure 默认 ToolExposure.DIRECT
func (c *ToolCard) EffectiveExposure() ToolExposure {
	if c.Exposure == "" {
		return ToolExposureDirect
	}
	return c.Exposure
}

// GetExposureDeclared 返回曝光策略是否已在注册期解析并锁定。
// 二元返回对齐 Python 的 bool|None：ok=false 表示从未声明。
func (c *ToolCard) GetExposureDeclared() (value bool, ok bool) {
	return c.exposureDeclared, c.exposureDeclaredSet
}

// SetExposureDeclared 标记曝光策略已在注册期解析并锁定。
func (c *ToolCard) SetExposureDeclared(value bool) {
	c.exposureDeclared = value
	c.exposureDeclaredSet = true
}

// ──────────────────────────── 非导出函数 ────────────────────────────
