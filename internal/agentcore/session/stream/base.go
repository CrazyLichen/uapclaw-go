package stream

// ──────────────────────────── 结构体 ────────────────────────────

// Schema 流数据统一接口，三种 Schema 均实现此接口。
type Schema interface {
	// SchemaType 返回数据类型标识
	SchemaType() string
}

// OutputSchema 标准输出流数据，对应 Python OutputSchema。
// 框架标准流数据，有 Index 字段用于排序。
type OutputSchema struct {
	// Type 数据类型标识
	Type string
	// Index 序号索引
	Index int
	// Payload 实际数据载荷
	Payload any
}

// TraceSchema 追踪流数据，对应 Python TraceSchema。
// 图执行产生的追踪数据，无 Index 字段。
type TraceSchema struct {
	// Type 数据类型标识
	Type string
	// Payload 实际数据载荷
	Payload any
}

// CustomSchema 自定义流数据，对应 Python CustomSchema。
// 用户自定义流数据，Data 字段允许任意键值对。
type CustomSchema struct {
	// Type 数据类型标识
	Type string
	// Data 任意键值载荷
	Data map[string]any
}

// ──────────────────────────── 枚举 ────────────────────────────

// StreamMode 流模式枚举，对应 Python BaseStreamMode。
type StreamMode int

const (
	// StreamModeOutput 标准输出流
	StreamModeOutput StreamMode = iota
	// StreamModeTrace 追踪流
	StreamModeTrace
	// StreamModeCustom 自定义流
	StreamModeCustom
)

// ──────────────────────────── 导出函数 ────────────────────────────

// SchemaType 实现 Schema 接口
func (s OutputSchema) SchemaType() string { return s.Type }

// SchemaType 实现 Schema 接口
func (s TraceSchema) SchemaType() string { return s.Type }

// SchemaType 实现 Schema 接口
func (s CustomSchema) SchemaType() string { return s.Type }

// String 返回流模式的字符串表示
func (m StreamMode) String() string {
	switch m {
	case StreamModeOutput:
		return "output"
	case StreamModeTrace:
		return "trace"
	case StreamModeCustom:
		return "custom"
	default:
		return "unknown"
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────
