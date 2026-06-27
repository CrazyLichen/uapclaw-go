package schema

import (
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
)

// ──────────────────────────── 结构体 ────────────────────────────

// Event 事件接口，所有事件类型的公共契约。
//
// 对应 Python: openjiuwen/core/controller/schema/event.py (Event)
type Event interface {
	// GetEventType 返回事件类型
	GetEventType() EventType
	// GetEventID 返回事件唯一标识
	GetEventID() string
	// GetMetadata 返回元数据
	GetMetadata() map[string]any
	// SetMetadata 设置元数据
	SetMetadata(meta map[string]any)
}

// BaseEvent 事件基类，包含事件类型、事件 ID 和元数据。
//
// 对应 Python: openjiuwen/core/controller/schema/event.py (Event)
type BaseEvent struct {
	// EventTypeField 事件类型
	EventTypeField EventType `json:"event_type"`
	// EventID 事件唯一标识
	EventID string `json:"event_id"`
	// Metadata 元数据
	Metadata map[string]any `json:"metadata,omitempty"`
}

// InputEvent 输入事件，承载用户输入数据。
//
// 对应 Python: openjiuwen/core/controller/schema/event.py (InputEvent)
type InputEvent struct {
	// BaseEvent 嵌入事件基类
	BaseEvent
	// InputData 输入数据列表
	InputData []DataFrame `json:"input_data"`
}

// TaskInteractionEvent 任务交互事件，任务执行中需要用户交互时触发。
//
// 对应 Python: openjiuwen/core/controller/schema/event.py (TaskInteractionEvent)
type TaskInteractionEvent struct {
	// BaseEvent 嵌入事件基类
	BaseEvent
	// Interaction 交互内容列表
	Interaction []DataFrame `json:"interaction"`
	// Task 关联的任务对象
	Task *Task `json:"task,omitempty"`
}

// TaskCompletionEvent 任务完成事件，包含任务输出结果。
//
// 对应 Python: openjiuwen/core/controller/schema/event.py (TaskCompletionEvent)
type TaskCompletionEvent struct {
	// BaseEvent 嵌入事件基类
	BaseEvent
	// TaskResult 任务结果列表
	TaskResult []DataFrame `json:"task_result"`
	// Task 关联的任务对象
	Task *Task `json:"task,omitempty"`
}

// TaskFailedEvent 任务失败事件，包含错误信息。
//
// 对应 Python: openjiuwen/core/controller/schema/event.py (TaskFailedEvent)
type TaskFailedEvent struct {
	// BaseEvent 嵌入事件基类
	BaseEvent
	// ErrorMessage 错误消息
	ErrorMessage string `json:"error_message,omitempty"`
	// Task 关联的任务对象
	Task *Task `json:"task,omitempty"`
}

// FollowUpEvent 后续事件，用于继续任务循环的新输入。
//
// 对应 Python: openjiuwen/core/controller/schema/event.py (FollowUpEvent)
type FollowUpEvent struct {
	// BaseEvent 嵌入事件基类
	BaseEvent
	// InputData 后续输入数据列表
	InputData []DataFrame `json:"input_data"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// EventType 事件类型枚举，定义所有支持的事件类型。
//
// 对应 Python: openjiuwen/core/controller/schema/event.py (EventType)
type EventType string

// ──────────────────────────── 常量 ────────────────────────────

const (
	// EventInput 用户输入事件
	EventInput EventType = "input"
	// EventTaskInteraction 任务交互事件
	EventTaskInteraction EventType = "task_interaction"
	// EventTaskCompletion 任务完成事件
	EventTaskCompletion EventType = "task_completion"
	// EventTaskFailed 任务失败事件
	EventTaskFailed EventType = "task_failed"
	// EventFollowUp 后续事件
	EventFollowUp EventType = "follow_up"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// GetEventType 实现 Event 接口，返回事件类型。
func (e *BaseEvent) GetEventType() EventType { return e.EventTypeField }

// GetEventID 实现 Event 接口，返回事件唯一标识。
func (e *BaseEvent) GetEventID() string { return e.EventID }

// GetMetadata 实现 Event 接口，返回元数据。
func (e *BaseEvent) GetMetadata() map[string]any {
	if e.Metadata == nil {
		return map[string]any{}
	}
	return e.Metadata
}

// SetMetadata 实现 Event 接口，设置元数据。
func (e *BaseEvent) SetMetadata(meta map[string]any) { e.Metadata = meta }

// NewBaseEvent 创建事件基类实例，自动生成事件 ID。
//
// 对应 Python: Event.event_id = Field(default_factory=lambda: str(uuid.uuid4()))
func NewBaseEvent(eventType EventType) *BaseEvent {
	return &BaseEvent{
		EventTypeField: eventType,
		EventID:        uuid.NewString(),
		Metadata:       map[string]any{},
	}
}

// FromUserInput 从用户输入创建 InputEvent 工厂方法，支持 string→TextDataFrame, map→JsonDataFrame, *InputEvent→原样返回。
//
// 对应 Python: InputEvent.from_user_input
func FromUserInput(userInput any) (*InputEvent, error) {
	switch v := userInput.(type) {
	case *InputEvent:
		return v, nil
	case string:
		return &InputEvent{
			BaseEvent: *NewBaseEvent(EventInput),
			InputData: []DataFrame{&TextDataFrame{Text: v}},
		}, nil
	case map[string]any:
		return &InputEvent{
			BaseEvent: *NewBaseEvent(EventInput),
			InputData: []DataFrame{&JsonDataFrame{Data: v}},
		}, nil
	default:
		return nil, fmt.Errorf("不支持的用户输入类型: %T，必须是 string、map[string]any 或 *InputEvent", userInput)
	}
}

// FromText 从文本创建 FollowUpEvent 工厂方法。
//
// 对应 Python: FollowUpEvent.from_text
func FromText(text string) *FollowUpEvent {
	return &FollowUpEvent{
		BaseEvent: *NewBaseEvent(EventFollowUp),
		InputData: []DataFrame{&TextDataFrame{Text: text}},
	}
}

// MarshalEvents 将 Event 切片序列化为 JSON 字节数组（多态序列化）。
func MarshalEvents(events []Event) ([]byte, error) {
	return json.Marshal(eventSlice(events))
}

// UnmarshalEvents 从 JSON 字节数组反序列化为 Event 切片（多态反序列化）。
func UnmarshalEvents(data []byte) ([]Event, error) {
	var es eventSlice
	if err := json.Unmarshal(data, &es); err != nil {
		return nil, err
	}
	return []Event(es), nil
}

// MarshalJSON 实现 json.Marshaler，支持 InputEvent 的多态 DataFrame 序列化。
func (e *InputEvent) MarshalJSON() ([]byte, error) {
	return json.Marshal(&inputEventJSON{
		EventTypeField: e.EventTypeField,
		EventID:        e.EventID,
		Metadata:       e.Metadata,
		InputData:      dataFrameSlice(e.InputData),
	})
}

// UnmarshalJSON 实现 json.Unmarshaler，支持 InputEvent 的多态 DataFrame 反序列化。
func (e *InputEvent) UnmarshalJSON(data []byte) error {
	var j inputEventJSON
	if err := json.Unmarshal(data, &j); err != nil {
		return err
	}
	e.EventTypeField = j.EventTypeField
	e.EventID = j.EventID
	e.Metadata = j.Metadata
	e.InputData = []DataFrame(j.InputData)
	return nil
}

// MarshalJSON 实现 json.Marshaler，支持 TaskInteractionEvent 的多态 DataFrame 序列化。
func (e *TaskInteractionEvent) MarshalJSON() ([]byte, error) {
	return json.Marshal(&taskInteractionEventJSON{
		EventTypeField: e.EventTypeField,
		EventID:        e.EventID,
		Metadata:       e.Metadata,
		Interaction:    dataFrameSlice(e.Interaction),
		Task:           e.Task,
	})
}

// UnmarshalJSON 实现 json.Unmarshaler，支持 TaskInteractionEvent 的多态 DataFrame 反序列化。
func (e *TaskInteractionEvent) UnmarshalJSON(data []byte) error {
	var j taskInteractionEventJSON
	if err := json.Unmarshal(data, &j); err != nil {
		return err
	}
	e.EventTypeField = j.EventTypeField
	e.EventID = j.EventID
	e.Metadata = j.Metadata
	e.Interaction = []DataFrame(j.Interaction)
	e.Task = j.Task
	return nil
}

// MarshalJSON 实现 json.Marshaler，支持 TaskCompletionEvent 的多态 DataFrame 序列化。
func (e *TaskCompletionEvent) MarshalJSON() ([]byte, error) {
	return json.Marshal(&taskCompletionEventJSON{
		EventTypeField: e.EventTypeField,
		EventID:        e.EventID,
		Metadata:       e.Metadata,
		TaskResult:     dataFrameSlice(e.TaskResult),
		Task:           e.Task,
	})
}

// UnmarshalJSON 实现 json.Unmarshaler，支持 TaskCompletionEvent 的多态 DataFrame 反序列化。
func (e *TaskCompletionEvent) UnmarshalJSON(data []byte) error {
	var j taskCompletionEventJSON
	if err := json.Unmarshal(data, &j); err != nil {
		return err
	}
	e.EventTypeField = j.EventTypeField
	e.EventID = j.EventID
	e.Metadata = j.Metadata
	e.TaskResult = []DataFrame(j.TaskResult)
	e.Task = j.Task
	return nil
}

// MarshalJSON 实现 json.Marshaler，支持 FollowUpEvent 的多态 DataFrame 序列化。
func (e *FollowUpEvent) MarshalJSON() ([]byte, error) {
	return json.Marshal(&followUpEventJSON{
		EventTypeField: e.EventTypeField,
		EventID:        e.EventID,
		Metadata:       e.Metadata,
		InputData:      dataFrameSlice(e.InputData),
	})
}

// UnmarshalJSON 实现 json.Unmarshaler，支持 FollowUpEvent 的多态 DataFrame 反序列化。
func (e *FollowUpEvent) UnmarshalJSON(data []byte) error {
	var j followUpEventJSON
	if err := json.Unmarshal(data, &j); err != nil {
		return err
	}
	e.EventTypeField = j.EventTypeField
	e.EventID = j.EventID
	e.Metadata = j.Metadata
	e.InputData = []DataFrame(j.InputData)
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// eventSlice Event 切片的类型别名，用于实现多态 JSON 序列化/反序列化。
type eventSlice []Event

// inputEventJSON InputEvent 的 JSON 序列化中间结构。
type inputEventJSON struct {
	EventTypeField EventType       `json:"event_type"`
	EventID        string          `json:"event_id"`
	Metadata       map[string]any  `json:"metadata,omitempty"`
	InputData      dataFrameSlice  `json:"input_data"`
}

// taskInteractionEventJSON TaskInteractionEvent 的 JSON 序列化中间结构。
type taskInteractionEventJSON struct {
	EventTypeField EventType       `json:"event_type"`
	EventID        string          `json:"event_id"`
	Metadata       map[string]any  `json:"metadata,omitempty"`
	Interaction    dataFrameSlice  `json:"interaction"`
	Task           *Task           `json:"task,omitempty"`
}

// taskCompletionEventJSON TaskCompletionEvent 的 JSON 序列化中间结构。
type taskCompletionEventJSON struct {
	EventTypeField EventType       `json:"event_type"`
	EventID        string          `json:"event_id"`
	Metadata       map[string]any  `json:"metadata,omitempty"`
	TaskResult     dataFrameSlice  `json:"task_result"`
	Task           *Task           `json:"task,omitempty"`
}

// followUpEventJSON FollowUpEvent 的 JSON 序列化中间结构。
type followUpEventJSON struct {
	EventTypeField EventType       `json:"event_type"`
	EventID        string          `json:"event_id"`
	Metadata       map[string]any  `json:"metadata,omitempty"`
	InputData      dataFrameSlice  `json:"input_data"`
}

// MarshalJSON 实现 json.Marshaler，遍历每个 Event 按具体类型序列化。
func (es eventSlice) MarshalJSON() ([]byte, error) {
	items := make([]json.RawMessage, len(es))
	for i, e := range es {
		data, err := json.Marshal(e)
		if err != nil {
			return nil, fmt.Errorf("序列化事件 [%d] 失败: %w", i, err)
		}
		items[i] = data
	}
	return json.Marshal(items)
}

// UnmarshalJSON 实现 json.Unmarshaler，按 event_type 字段分发到具体类型反序列化。
func (es *eventSlice) UnmarshalJSON(data []byte) error {
	var raws []json.RawMessage
	if err := json.Unmarshal(data, &raws); err != nil {
		return err
	}

	result := make([]Event, len(raws))
	for i, raw := range raws {
		// 先解析出 event_type 字段
		var peek struct {
			EventType EventType `json:"event_type"`
		}
		if err := json.Unmarshal(raw, &peek); err != nil {
			return fmt.Errorf("反序列化事件 [%d] 失败: 无法解析 event_type: %w", i, err)
		}

		var evt Event
		var err error
		switch peek.EventType {
		case EventInput:
			var e InputEvent
			err = json.Unmarshal(raw, &e)
			evt = &e
		case EventTaskInteraction:
			var e TaskInteractionEvent
			err = json.Unmarshal(raw, &e)
			evt = &e
		case EventTaskCompletion:
			var e TaskCompletionEvent
			err = json.Unmarshal(raw, &e)
			evt = &e
		case EventTaskFailed:
			var e TaskFailedEvent
			err = json.Unmarshal(raw, &e)
			evt = &e
		case EventFollowUp:
			var e FollowUpEvent
			err = json.Unmarshal(raw, &e)
			evt = &e
		default:
			return fmt.Errorf("反序列化事件 [%d] 失败: 未知 event_type %q", i, peek.EventType)
		}
		if err != nil {
			return fmt.Errorf("反序列化事件 [%d] (type=%s) 失败: %w", i, peek.EventType, err)
		}
		result[i] = evt
	}
	*es = result
	return nil
}
