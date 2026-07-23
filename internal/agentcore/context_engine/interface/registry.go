package iface

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ProcessorFactory 处理器工厂函数类型。
//
// 根据 ProcessorConfig 创建对应的 ContextProcessor 实例。
// 对应 Python: ContextEngine._PROCESSOR_MAP 中存储的 processor_class，
// 运行时通过 processor_class(config) 创建实例。
type ProcessorFactory func(config ProcessorConfig) (ContextProcessor, error)
