package stream

import (
	"fmt"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 结构体 ────────────────────────────

// Schema 流数据统一接口，三种 Schema 均实现此接口。
// 对应 Python 中 Pydantic BaseModel 的 model_validate 验证能力，
// Go 通过 Validate() 方法实现等价的字段校验逻辑。
type Schema interface {
	// SchemaType 返回数据类型标识
	SchemaType() string
	// Validate 校验 Schema 字段合法性，对应 Python model_validate。
	// 校验失败时返回 STREAM_WRITER_WRITE_STREAM_VALIDATION_ERROR 错误。
	Validate() error
}

// OutputSchema 标准输出流数据，对应 Python OutputSchema。
// 框架标准流数据，有 Index 字段用于排序。
// IsLastSchema 标识是否为最后一帧，对齐 Python ControllerOutputChunk.last_chunk。
// Payload 为 any 类型，controller 场景下存放 *schema.ControllerOutputPayload，
// 其他场景存放任意数据（对齐 Python OutputSchema.payload: Any）。
type OutputSchema struct {
	// Type 数据类型标识
	Type string
	// Index 序号索引
	Index int
	// Payload 实际数据载荷
	Payload any
	// IsLastSchema 是否最后一帧（对齐 Python ControllerOutputChunk.last_chunk）
	IsLastSchema bool
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

// StreamMode 流模式，对应 Python StreamMode + BaseStreamMode。
// Python 中 StreamMode 是富枚举基类（携带 mode/desc/options），
// BaseStreamMode 继承 StreamMode 定义 OUTPUT/TRACE/CUSTOM 三个内置成员。
// Go 中统一为 StreamMode 结构体，三个预定义变量等价于 BaseStreamMode 成员，
// 用户可自行构造 StreamMode 扩展自定义模式（等价于继承 StreamMode 基类）。
type StreamMode struct {
	// mode 模式标识（同时也是枚举值，对应 Python StreamMode._value_）
	mode string
	// desc 人类可读描述（对应 Python StreamMode.desc）
	desc string
	// options 扩展选项（对应 Python StreamMode.options）
	options map[string]any
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// StreamModeOutput 标准输出流，对应 Python BaseStreamMode.OUTPUT
	StreamModeOutput = StreamMode{mode: "output", desc: "Standard stream data defined by the framework"}
	// StreamModeTrace 追踪流，对应 Python BaseStreamMode.TRACE
	StreamModeTrace = StreamMode{mode: "trace", desc: "Trace stream data produced by the graph"}
	// StreamModeCustom 自定义流，对应 Python BaseStreamMode.CUSTOM
	StreamModeCustom = StreamMode{mode: "custom", desc: "Custom stream data defined by the runnable"}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// SchemaType 实现 Schema 接口
func (s OutputSchema) SchemaType() string { return s.Type }

// SchemaType 实现 Schema 接口
func (s TraceSchema) SchemaType() string { return s.Type }

// SchemaType 实现 Schema 接口
func (s CustomSchema) SchemaType() string { return s.Type }

// Validate 校验 OutputSchema 字段合法性，对应 Python OutputSchema.model_validate()。
// 校验规则（对齐 Pydantic 必填约束）：type 非空、index >= 0。
func (s OutputSchema) Validate() error {
	var reasons []string
	if strings.TrimSpace(s.Type) == "" {
		reasons = append(reasons, "type 不能为空")
	}
	if s.Index < 0 {
		reasons = append(reasons, "index 不能为负数")
	}
	if len(reasons) > 0 {
		return newSchemaValidationError("OutputSchema", reasons...)
	}
	return nil
}

// Validate 校验 TraceSchema 字段合法性，对应 Python TraceSchema.model_validate()。
// 校验规则（对齐 Pydantic 必填约束）：type 非空。
func (s TraceSchema) Validate() error {
	if strings.TrimSpace(s.Type) == "" {
		return newSchemaValidationError("TraceSchema", "type 不能为空")
	}
	return nil
}

// Validate 校验 CustomSchema 字段合法性，对应 Python CustomSchema.model_validate()。
// CustomSchema 的 model_config = ConfigDict(extra="allow")，无必填字段，几乎不校验。
func (s CustomSchema) Validate() error {
	// Python CustomSchema 无显式字段定义，extra="allow" 允许任意字段，
	// Pydantic 的 model_validate 几乎不会失败。Go 端同样不做字段级校验。
	return nil
}

// NewStreamMode 创建自定义流模式，对应 Python 继承 StreamMode 定义新成员。
// mode 为模式标识（必须非空），desc 为描述，options 为扩展选项（可选）。
func NewStreamMode(mode string, desc string, options ...map[string]any) StreamMode {
	opts := map[string]any{}
	if len(options) > 0 {
		opts = options[0]
	}
	return StreamMode{mode: mode, desc: desc, options: opts}
}

// Mode 返回模式标识，对应 Python StreamMode.mode
func (m StreamMode) Mode() string { return m.mode }

// Desc 返回模式描述，对应 Python StreamMode.desc
func (m StreamMode) Desc() string { return m.desc }

// Options 返回扩展选项，对应 Python StreamMode.options
func (m StreamMode) Options() map[string]any { return m.options }

// String 实现 fmt.Stringer，对齐 Python StreamMode.__str__()
func (m StreamMode) String() string {
	return fmt.Sprintf("StreamMode(mode=%s, desc=%s, options=%v)", m.mode, m.desc, m.options)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newSchemaValidationError 创建 Schema 校验错误，对应 Python build_error(StatusCode.STREAM_WRITER_WRITE_STREAM_VALIDATION_ERROR)。
func newSchemaValidationError(schemaName string, reasons ...string) error {
	return exception.NewBaseError(exception.StatusStreamWriterWriteStreamValidationError,
		exception.WithMsg(strings.Join(reasons, "; ")),
		exception.WithParam("schema_type", schemaName),
	)
}
