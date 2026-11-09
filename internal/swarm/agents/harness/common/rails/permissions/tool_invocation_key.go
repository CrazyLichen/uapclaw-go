package permissions

import (
	"fmt"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ToolInvocationKeyV1 一次逻辑工具调用的宿主侧身份标识。
// 该标识为增量字段，同一次调用的所有事件共享同一份，用于把前端卡片、
// 用户答案、后端重放三者精确对应起来，避免多卡并发时串台。
// Python: @dataclass(frozen=True) ToolInvocationKeyV1
type ToolInvocationKeyV1 struct {
	// InvocationID 本次调用的全局唯一标识
	InvocationID string
	// RootSessionID 根会话标识
	RootSessionID string
	// RequestID 宿主请求标识
	RequestID string
	// ExecutorKind 执行器类型，当前仅支持 "agent"
	ExecutorKind string
	// ExecutionSessionID 执行会话标识
	ExecutionSessionID string
	// ToolCallID 工具调用标识
	ToolCallID string
	// Version wire 版本号，当前固定为 1
	Version int
}

// ──────────────────────────── 枚举 ────────────────────────────

// ExecutorKind 执行器类型取值。
// Python: ExecutorKind: TypeAlias = Literal["agent"]
const (
	// ExecutorKindAgent Agent 执行器
	ExecutorKindAgent = "agent"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// ToolInvocationKeyVersion 当前 wire 版本号
	// Python: TOOL_INVOCATION_KEY_VERSION
	ToolInvocationKeyVersion = 1

	// MaxToolInvocationIDLength invocation_id 的最大长度
	// Python: MAX_TOOL_INVOCATION_ID_LENGTH
	MaxToolInvocationIDLength = 128

	// defaultToolInvocationTextMaximum 常规身份字段的默认最大长度
	// Python: normalize_tool_invocation_text 的 maximum 默认值 512
	defaultToolInvocationTextMaximum = 512
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NormalizeToolInvocationText 归一化单个身份字段，非法时返回错误。
// non-string 一律视为空串后判空，与 Python 的 isinstance 判定一致；
// 前后空白被 trim，超长或空串直接报错（fail-closed）。
// Python: normalize_tool_invocation_text(name, value, *, maximum=512)
func NormalizeToolInvocationText(name string, value any, maximum int) (string, error) {
	normalized := ""
	if s, ok := value.(string); ok {
		normalized = trimSpace(s)
	}
	if normalized == "" || len(normalized) > maximum {
		return "", fmt.Errorf("invalid %s", name)
	}
	return normalized, nil
}

// NewToolInvocationKeyV1 构造并校验 ToolInvocationKeyV1。
// 校验顺序与 Python __post_init__ 一致：先 version，再 executor_kind，
// 然后 invocation_id（带专属长度上限），最后其余四个身份字段。
// Python: ToolInvocationKeyV1(...)
func NewToolInvocationKeyV1(
	invocationID string,
	rootSessionID string,
	requestID string,
	executorKind string,
	executionSessionID string,
	toolCallID string,
	version int,
) (*ToolInvocationKeyV1, error) {
	if version != ToolInvocationKeyVersion {
		return nil, fmt.Errorf("unsupported tool invocation key version")
	}
	if executorKind != ExecutorKindAgent {
		return nil, fmt.Errorf("invalid executor_kind")
	}

	normalizedInvocationID, err := NormalizeToolInvocationText(
		"invocation_id", invocationID, MaxToolInvocationIDLength,
	)
	if err != nil {
		return nil, err
	}

	key := &ToolInvocationKeyV1{
		ExecutorKind: executorKind,
		Version:      version,
	}
	key.InvocationID = normalizedInvocationID

	for _, field := range []struct {
		name  string
		dst   *string
		value string
	}{
		{"root_session_id", &key.RootSessionID, rootSessionID},
		{"request_id", &key.RequestID, requestID},
		{"execution_session_id", &key.ExecutionSessionID, executionSessionID},
		{"tool_call_id", &key.ToolCallID, toolCallID},
	} {
		normalized, err := NormalizeToolInvocationText(
			field.name, field.value, defaultToolInvocationTextMaximum,
		)
		if err != nil {
			return nil, err
		}
		*field.dst = normalized
	}

	return key, nil
}

// ToWire 序列化为带版本号的 wire 表示。
// Python: ToolInvocationKeyV1.to_wire()
func (k *ToolInvocationKeyV1) ToWire() map[string]any {
	return map[string]any{
		"version":              k.Version,
		"invocation_id":        k.InvocationID,
		"root_session_id":      k.RootSessionID,
		"request_id":           k.RequestID,
		"executor_kind":        k.ExecutorKind,
		"execution_session_id": k.ExecutionSessionID,
		"tool_call_id":         k.ToolCallID,
	}
}

// ToolInvocationKeyV1FromWire 从 wire 映射还原，字段集合必须完全一致（不接受部分身份）。
// Python: ToolInvocationKeyV1.from_wire(value)
func ToolInvocationKeyV1FromWire(value map[string]any) (*ToolInvocationKeyV1, error) {
	if value == nil {
		return nil, fmt.Errorf("tool invocation key must be a mapping")
	}
	required := map[string]struct{}{
		"version":              {},
		"invocation_id":        {},
		"root_session_id":      {},
		"request_id":           {},
		"executor_kind":        {},
		"execution_session_id": {},
		"tool_call_id":         {},
	}
	if len(value) != len(required) {
		return nil, fmt.Errorf("invalid tool invocation key fields")
	}
	for field := range value {
		if _, ok := required[field]; !ok {
			return nil, fmt.Errorf("invalid tool invocation key fields")
		}
	}

	version, ok := value["version"].(int)
	if !ok {
		return nil, fmt.Errorf("invalid tool invocation key fields")
	}

	// Python 的 cls(**dict(value)) 会把每个字段按其注解解析；非字符串字段
	// 在 normalize 阶段被判空后报错，此处沿用同样的兜底。
	return NewToolInvocationKeyV1(
		strField(value, "invocation_id"),
		strField(value, "root_session_id"),
		strField(value, "request_id"),
		strField(value, "executor_kind"),
		strField(value, "execution_session_id"),
		strField(value, "tool_call_id"),
		version,
	)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// strField 取映射中的字符串字段，非字符串返回空串。
// Python 侧由 dataclass 注解做类型校验，Go 侧用空串让后续 normalize 报错，
// 等价于"非 str 视为非法"。
func strField(value map[string]any, field string) string {
	if s, ok := value[field].(string); ok {
		return s
	}
	return ""
}

// trimSpace 按 Python str.strip() 语义去除首尾空白。
// 单独抽出便于测试覆盖，避免直接依赖 strings.TrimSpace 的 Unicode 集合差异。
func trimSpace(s string) string {
	return strings.TrimSpace(s)
}
