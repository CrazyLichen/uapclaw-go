package schema

import (
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ControllerOutputPayload 输出载荷，包含输出类型、数据和元数据。
//
// 对应 Python: openjiuwen/core/controller/schema/controller_output.py (ControllerOutputPayload)
type ControllerOutputPayload struct {
	// Type 输出类型
	Type string `json:"type"`
	// Data 输出数据列表
	Data []DataFrame `json:"data"`
	// Metadata 元数据
	Metadata map[string]any `json:"metadata,omitempty"`
}

// ControllerOutputChunk 输出分片，嵌入 stream.OutputSchema 并扩展。
// 实现 stream.Schema 接口，可直接写入 Session 流。
//
// 对应 Python: openjiuwen/core/controller/schema/controller_output.py (ControllerOutputChunk)
type ControllerOutputChunk struct {
	stream.OutputSchema
	// Payload 强类型载荷（覆盖 OutputSchema.Payload）
	Payload *ControllerOutputPayload `json:"payload"`
	// LastChunk 是否最后一帧
	LastChunk bool `json:"last_chunk"`
}

// ControllerOutput 批量输出。
//
// 对应 Python: openjiuwen/core/controller/schema/controller_output.py (ControllerOutput)
type ControllerOutput struct {
	// Type 输出类型
	Type string `json:"type"`
	// Data 输出数据分片列表
	Data []*ControllerOutputChunk `json:"data"`
	// InputEventID 关联的输入事件ID
	InputEventID string `json:"input_event_id,omitempty"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// TaskProcessing 处理中类型标识
	TaskProcessing = "processing"
	// AllTasksProcessed 全部任务已处理类型标识
	AllTasksProcessed = "all_tasks_processed"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// Validate 实现 stream.Schema 接口，校验 ControllerOutputChunk。
func (c *ControllerOutputChunk) Validate() error {
	var reasons []string
	if strings.TrimSpace(c.Type) == "" {
		reasons = append(reasons, "type 不能为空")
	}
	if c.Index < 0 {
		reasons = append(reasons, "index 不能为负数")
	}
	if c.Payload == nil {
		reasons = append(reasons, "payload 不能为空")
	}
	if len(reasons) > 0 {
		return exception.NewBaseError(exception.StatusStreamWriterWriteStreamValidationError,
			exception.WithMsg(strings.Join(reasons, "; ")),
			exception.WithParam("schema_type", "ControllerOutputChunk"),
		)
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────
