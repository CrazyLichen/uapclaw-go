package extensions

// ──────────────────────────── 结构体 ────────────────────────────

// ExtensionMetadata 扩展元数据，对齐 Python ExtensionMetadata dataclass
type ExtensionMetadata struct {
	// ID 扩展唯一标识
	ID string `json:"id"`
	// Name 扩展名称
	Name string `json:"name"`
	// Version 扩展版本
	Version string `json:"version"`
	// Description 扩展描述
	Description string `json:"description"`
	// Author 扩展作者
	Author string `json:"author"`
	// MinJiuwenSwarmVersion 最小兼容版本
	MinJiuwenSwarmVersion string `json:"min_jiuwenswarm_version"`
	// Dependencies 扩展依赖 {"extension_id": ">=1.0.0"}
	Dependencies map[string]string `json:"dependencies"`
	// ConfigSchema 配置模式 (JSON Schema)，可选
	ConfigSchema map[string]any `json:"config_schema,omitempty"`
}

// ExtensionConfig 扩展配置，对齐 Python ExtensionConfig dataclass
type ExtensionConfig struct {
	// Config 全局配置字典
	Config map[string]any
	// Logger 日志实例（实际使用 logger.ComponentXxx）
	Logger any
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────
