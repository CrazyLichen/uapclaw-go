package observability

// ──────────────────────────── 结构体 ────────────────────────────

// ObservabilityConfig 可观测性运行时配置。
// Python: ObservabilityConfig (pydantic BaseModel)
type ObservabilityConfig struct {
	// Enabled 总开关，为 false 时 InitObservability 为 no-op
	Enabled bool
	// ServiceName OTel resource 属性 service.name
	ServiceName string
	// Exporter 导出器类型："otlp_grpc" | "otlp_http" | "console"
	Exporter string
	// Endpoint OTLP 端点 URL（gRPC 默认 localhost:4317；HTTP 4318）
	Endpoint string
	// SampleRate 采样率（0.0 - 1.0）
	SampleRate float64
	// RedactPrompts 为 true 时哈希/截断 prompt 内容
	RedactPrompts bool
	// RedactCompletions 为 true 时哈希/截断 completion 内容
	RedactCompletions bool
	// AttributeValueMaxLength 字符串属性值硬上限
	AttributeValueMaxLength int
	// ExportTimeoutMs Span 导出器关闭超时
	ExportTimeoutMs int
}

// ──────────────────────────── 导出函数 ────────────────────────────

// DefaultObservabilityConfig 创建默认配置。
// Python: ObservabilityConfig() 无参默认值
func DefaultObservabilityConfig() *ObservabilityConfig {
	return &ObservabilityConfig{
		Enabled:                 true,
		ServiceName:             "openjiuwen-agent-teams",
		Exporter:                "otlp_grpc",
		Endpoint:                "http://localhost:4317",
		SampleRate:              1.0,
		RedactPrompts:           false,
		RedactCompletions:       false,
		AttributeValueMaxLength: 8192,
		ExportTimeoutMs:         5000,
	}
}
