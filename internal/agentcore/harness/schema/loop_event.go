package schema

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 结构体 ────────────────────────────

// DeepLoopEvent 深度循环事件
type DeepLoopEvent struct {
	// Priority 优先级（数值越小优先级越高）
	Priority int `json:"priority"`
	// Seq 序号
	Seq int `json:"seq"`
	// CreatedAt 创建时间（纳秒时间戳）
	CreatedAt float64 `json:"created_at"`
	// EventID 事件唯一标识
	EventID string `json:"event_id"`
	// EventType 事件类型
	EventType DeepLoopEventType `json:"event_type"`
	// Content 事件内容
	Content string `json:"content"`
	// TaskID 任务标识（可选）
	TaskID string `json:"task_id,omitempty"`
	// Metadata 元数据（可选）
	Metadata map[string]any `json:"metadata,omitempty"`
}

// LoopEventOption 深度循环事件函数选项
type LoopEventOption func(*DeepLoopEvent)

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// DeepLoopEventType 深度循环事件类型枚举
type DeepLoopEventType int

const (
	// DeepLoopEventTypeFollowup 追问事件
	DeepLoopEventTypeFollowup DeepLoopEventType = iota
	// DeepLoopEventTypeSteer 引导事件
	DeepLoopEventTypeSteer
	// DeepLoopEventTypeAbort 中止事件
	DeepLoopEventTypeAbort
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// defaultEventPriorityMap 事件类型默认优先级映射
var defaultEventPriorityMap = map[DeepLoopEventType]int{
	DeepLoopEventTypeAbort:    0,
	DeepLoopEventTypeSteer:    1,
	DeepLoopEventTypeFollowup: 10,
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// DefaultEventPriority 返回事件类型的默认优先级
func DefaultEventPriority(eventType DeepLoopEventType) int {
	if p, ok := defaultEventPriorityMap[eventType]; ok {
		return p
	}
	return 10
}

// ParseDeepLoopEventType 从字符串解析 DeepLoopEventType
func ParseDeepLoopEventType(s string) (DeepLoopEventType, error) {
	switch strings.ToLower(s) {
	case "followup":
		return DeepLoopEventTypeFollowup, nil
	case "steer":
		return DeepLoopEventTypeSteer, nil
	case "abort":
		return DeepLoopEventTypeAbort, nil
	default:
		return DeepLoopEventTypeFollowup, fmt.Errorf("未知的 DeepLoopEventType: %q", s)
	}
}

// CreateLoopEvent 创建深度循环事件
func CreateLoopEvent(seq int, eventType DeepLoopEventType, content string, opts ...LoopEventOption) DeepLoopEvent {
	evt := DeepLoopEvent{
		Seq:       seq,
		EventType: eventType,
		Content:   content,
		Priority:  DefaultEventPriority(eventType),
		CreatedAt: float64(time.Now().UnixNano()),
		EventID:   uuid.New().String(),
	}
	for _, opt := range opts {
		opt(&evt)
	}
	return evt
}

// WithTaskID 设置任务标识选项
func WithTaskID(taskID string) LoopEventOption {
	return func(e *DeepLoopEvent) {
		e.TaskID = taskID
	}
}

// WithMetadata 设置元数据选项
func WithMetadata(metadata map[string]any) LoopEventOption {
	return func(e *DeepLoopEvent) {
		e.Metadata = metadata
	}
}

// WithPriority 设置优先级选项
func WithPriority(priority int) LoopEventOption {
	return func(e *DeepLoopEvent) {
		e.Priority = priority
	}
}

// String 返回 DeepLoopEventType 的字符串表示
func (t DeepLoopEventType) String() string {
	switch t {
	case DeepLoopEventTypeFollowup:
		return "followup"
	case DeepLoopEventTypeSteer:
		return "steer"
	case DeepLoopEventTypeAbort:
		return "abort"
	default:
		return fmt.Sprintf("unknown(%d)", t)
	}
}

// MarshalJSON 实现 json.Marshaler 接口
func (t DeepLoopEventType) MarshalJSON() ([]byte, error) {
	return json.Marshal(t.String())
}

// UnmarshalJSON 实现 json.Unmarshaler 接口
func (t *DeepLoopEventType) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("DeepLoopEventType 应为字符串，解析失败: %w", err)
	}
	parsed, err := ParseDeepLoopEventType(s)
	if err != nil {
		return err
	}
	*t = parsed
	return nil
}

// Less 判断当前事件是否优先于另一事件（优先级数值越小越优先，同优先级序号越小越优先）
func (e DeepLoopEvent) Less(other DeepLoopEvent) bool {
	if e.Priority != other.Priority {
		return e.Priority < other.Priority
	}
	return e.Seq < other.Seq
}
