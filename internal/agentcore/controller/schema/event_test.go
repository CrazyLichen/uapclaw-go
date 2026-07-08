package schema

import (
	"encoding/json"
	"testing"
)

// ──────────────────────────── 导出函数 ────────────────────────────

func TestEventType_值对齐Python(t *testing.T) {
	// 验证 5 个枚举值与 Python EventType 字符串值完全对齐
	expected := map[EventType]string{
		EventInput:           "input",
		EventTaskInteraction: "task_interaction",
		EventTaskCompletion:  "task_completion",
		EventTaskFailed:      "task_failed",
		EventFollowUp:        "follow_up",
	}
	for et, want := range expected {
		if string(et) != want {
			t.Errorf("EventType 值不对齐: got %q, want %q", string(et), want)
		}
	}
}

func TestBaseEvent_接口实现(t *testing.T) {
	// 编译期验证 BaseEvent 实现了 Event 接口
	var _ Event = (*BaseEvent)(nil)
}

func TestInputEvent_FromUserInput_字符串(t *testing.T) {
	evt, err := FromUserInput("hello")
	if err != nil {
		t.Fatalf("FromUserInput(\"hello\") 返回错误: %v", err)
	}
	if evt.GetEventType() != EventInput {
		t.Errorf("EventType = %q, want %q", evt.GetEventType(), EventInput)
	}
	if len(evt.InputData) != 1 {
		t.Fatalf("InputData 长度 = %d, want 1", len(evt.InputData))
	}
	df, ok := evt.InputData[0].(*TextDataFrame)
	if !ok {
		t.Fatal("InputData[0] 不是 *TextDataFrame")
	}
	if df.Text != "hello" {
		t.Errorf("Text = %q, want %q", df.Text, "hello")
	}
}

func TestInputEvent_FromUserInput_字典(t *testing.T) {
	input := map[string]any{"key": "value", "num": 42}
	evt, err := FromUserInput(input)
	if err != nil {
		t.Fatalf("FromUserInput(map) 返回错误: %v", err)
	}
	if evt.GetEventType() != EventInput {
		t.Errorf("EventType = %q, want %q", evt.GetEventType(), EventInput)
	}
	if len(evt.InputData) != 1 {
		t.Fatalf("InputData 长度 = %d, want 1", len(evt.InputData))
	}
	df, ok := evt.InputData[0].(*JsonDataFrame)
	if !ok {
		t.Fatal("InputData[0] 不是 *JsonDataFrame")
	}
	if df.Data["key"] != "value" {
		t.Errorf("Data[\"key\"] = %v, want %q", df.Data["key"], "value")
	}
}

func TestInputEvent_FromUserInput_已有InputEvent(t *testing.T) {
	original, _ := FromUserInput("original")
	evt, err := FromUserInput(original)
	if err != nil {
		t.Fatalf("FromUserInput(*InputEvent) 返回错误: %v", err)
	}
	if evt != original {
		t.Error("FromUserInput(*InputEvent) 应原样返回，但返回了不同实例")
	}
}

func TestInputEvent_FromUserInput_不支持的类型报错(t *testing.T) {
	_, err := FromUserInput(123)
	if err == nil {
		t.Error("FromUserInput(123) 应返回错误，但返回了 nil")
	}
}

func TestFollowUpEvent_FromText(t *testing.T) {
	evt := FromText("继续执行")
	if evt.GetEventType() != EventFollowUp {
		t.Errorf("EventType = %q, want %q", evt.GetEventType(), EventFollowUp)
	}
	if len(evt.InputData) != 1 {
		t.Fatalf("InputData 长度 = %d, want 1", len(evt.InputData))
	}
	df, ok := evt.InputData[0].(*TextDataFrame)
	if !ok {
		t.Fatal("InputData[0] 不是 *TextDataFrame")
	}
	if df.Text != "继续执行" {
		t.Errorf("Text = %q, want %q", df.Text, "继续执行")
	}
}

func TestEvent_JSON序列化_InputEvent(t *testing.T) {
	evt, _ := FromUserInput("测试输入")
	data, err := json.Marshal(evt)
	if err != nil {
		t.Fatalf("Marshal InputEvent 失败: %v", err)
	}

	var parsed struct {
		EventType string `json:"event_type"`
		InputData []struct {
			Text string `json:"text"`
		} `json:"input_data"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}
	if parsed.EventType != "input" {
		t.Errorf("event_type = %q, want %q", parsed.EventType, "input")
	}
	if len(parsed.InputData) != 1 || parsed.InputData[0].Text != "测试输入" {
		t.Errorf("input_data 不匹配: %+v", parsed.InputData)
	}
}

func TestEvent_JSON序列化_TaskFailedEvent(t *testing.T) {
	evt := &TaskFailedEvent{
		BaseEvent:    *NewBaseEvent(EventTaskFailed),
		ErrorMessage: "执行出错",
	}
	data, err := json.Marshal(evt)
	if err != nil {
		t.Fatalf("Marshal TaskFailedEvent 失败: %v", err)
	}

	var parsed struct {
		EventType    string `json:"event_type"`
		ErrorMessage string `json:"error_message"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}
	if parsed.EventType != "task_failed" {
		t.Errorf("event_type = %q, want %q", parsed.EventType, "task_failed")
	}
	if parsed.ErrorMessage != "执行出错" {
		t.Errorf("error_message = %q, want %q", parsed.ErrorMessage, "执行出错")
	}
}

func TestEvent_JSON序列化_多态切片(t *testing.T) {
	// 构造包含不同类型 Event 的切片
	inputEvt, _ := FromUserInput("多态测试")
	failedEvt := &TaskFailedEvent{
		BaseEvent:    *NewBaseEvent(EventTaskFailed),
		ErrorMessage: "多态失败",
	}
	followEvt := FromText("后续输入")

	events := []Event{inputEvt, failedEvt, followEvt}

	// MarshalEvents
	data, err := MarshalEvents(events)
	if err != nil {
		t.Fatalf("MarshalEvents 失败: %v", err)
	}

	// UnmarshalEvents round-trip
	got, err := UnmarshalEvents(data)
	if err != nil {
		t.Fatalf("UnmarshalEvents 失败: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("反序列化后长度 = %d, want 3", len(got))
	}

	// 验证每个事件的类型
	if got[0].GetEventType() != EventInput {
		t.Errorf("events[0] 类型 = %q, want %q", got[0].GetEventType(), EventInput)
	}
	if got[1].GetEventType() != EventTaskFailed {
		t.Errorf("events[1] 类型 = %q, want %q", got[1].GetEventType(), EventTaskFailed)
	}
	if got[2].GetEventType() != EventFollowUp {
		t.Errorf("events[2] 类型 = %q, want %q", got[2].GetEventType(), EventFollowUp)
	}

	// 验证 TaskFailedEvent 的字段
	failed, ok := got[1].(*TaskFailedEvent)
	if !ok {
		t.Fatal("events[1] 不是 *TaskFailedEvent")
	}
	if failed.ErrorMessage != "多态失败" {
		t.Errorf("ErrorMessage = %q, want %q", failed.ErrorMessage, "多态失败")
	}
}

func TestEvent_JSON反序列化_按EventType分发(t *testing.T) {
	// 构造不同 event_type 的 JSON 原始数据
	raws := []string{
		`{"event_type":"input","event_id":"id-1","input_data":[{"text":"hello"}]}`,
		`{"event_type":"task_interaction","event_id":"id-2","interaction":[{"text":"请确认"}]}`,
		`{"event_type":"task_completion","event_id":"id-3","task_result":[{"data":{"result":"ok"}}]}`,
		`{"event_type":"task_failed","event_id":"id-4","error_message":"出错了"}`,
		`{"event_type":"follow_up","event_id":"id-5","input_data":[{"text":"继续"}]}`,
	}

	// 组装 JSON 数组
	data := "["
	for i, raw := range raws {
		if i > 0 {
			data += ","
		}
		data += raw
	}
	data += "]"

	events, err := UnmarshalEvents([]byte(data))
	if err != nil {
		t.Fatalf("UnmarshalEvents 失败: %v", err)
	}
	if len(events) != 5 {
		t.Fatalf("反序列化后长度 = %d, want 5", len(events))
	}

	// 验证每个反序列化后的具体类型
	if _, ok := events[0].(*InputEvent); !ok {
		t.Errorf("events[0] 类型 = %T, want *InputEvent", events[0])
	}
	if _, ok := events[1].(*TaskInteractionEvent); !ok {
		t.Errorf("events[1] 类型 = %T, want *TaskInteractionEvent", events[1])
	}
	if _, ok := events[2].(*TaskCompletionEvent); !ok {
		t.Errorf("events[2] 类型 = %T, want *TaskCompletionEvent", events[2])
	}
	if _, ok := events[3].(*TaskFailedEvent); !ok {
		t.Errorf("events[3] 类型 = %T, want *TaskFailedEvent", events[3])
	}
	if _, ok := events[4].(*FollowUpEvent); !ok {
		t.Errorf("events[4] 类型 = %T, want *FollowUpEvent", events[4])
	}

	// 验证 TaskFailedEvent 字段值
	failed := events[3].(*TaskFailedEvent)
	if failed.ErrorMessage != "出错了" {
		t.Errorf("ErrorMessage = %q, want %q", failed.ErrorMessage, "出错了")
	}
	if failed.EventID != "id-4" {
		t.Errorf("EventID = %q, want %q", failed.EventID, "id-4")
	}
}

// TestBaseEvent_GetEventID 测试获取 event ID
func TestBaseEvent_GetEventID(t *testing.T) {
	e := NewBaseEvent(EventInput)
	if e.GetEventID() == "" {
		t.Error("GetEventID 返回空字符串")
	}
	if e.GetEventID() != e.EventID {
		t.Errorf("GetEventID = %q, want %q", e.GetEventID(), e.EventID)
	}
}

// TestBaseEvent_GetMetadata 测试获取 metadata
func TestBaseEvent_GetMetadata(t *testing.T) {
	// 有 metadata
	e := NewBaseEvent(EventInput)
	meta := e.GetMetadata()
	if meta == nil {
		t.Error("GetMetadata 返回 nil")
	}

	// metadata 为 nil 时返回空 map
	e2 := &BaseEvent{EventTypeField: EventInput, EventID: "test"}
	meta2 := e2.GetMetadata()
	if meta2 == nil {
		t.Error("metadata 为 nil 时 GetMetadata 应返回空 map，不应返回 nil")
	}
	if len(meta2) != 0 {
		t.Errorf("metadata 为 nil 时 GetMetadata 返回长度 = %d, want 0", len(meta2))
	}
}

// TestBaseEvent_SetMetadata 测试设置 metadata
func TestBaseEvent_SetMetadata(t *testing.T) {
	e := NewBaseEvent(EventInput)
	newMeta := map[string]any{"key": "value"}
	e.SetMetadata(newMeta)
	if e.Metadata["key"] != "value" {
		t.Errorf("SetMetadata 后 Metadata[key] = %v, want %q", e.Metadata["key"], "value")
	}
}

// TestTaskInteractionEvent_MarshalJSON 测试 TaskInteractionEvent 序列化
func TestTaskInteractionEvent_MarshalJSON(t *testing.T) {
	evt := &TaskInteractionEvent{
		BaseEvent:   *NewBaseEvent(EventTaskInteraction),
		Interaction: []DataFrame{&TextDataFrame{Text: "请确认"}},
	}
	data, err := json.Marshal(evt)
	if err != nil {
		t.Fatalf("Marshal TaskInteractionEvent 失败: %v", err)
	}

	var parsed struct {
		EventType string `json:"event_type"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}
	if parsed.EventType != "task_interaction" {
		t.Errorf("event_type = %q, want %q", parsed.EventType, "task_interaction")
	}
}

// TestTaskCompletionEvent_MarshalJSON 测试 TaskCompletionEvent 序列化
func TestTaskCompletionEvent_MarshalJSON(t *testing.T) {
	evt := &TaskCompletionEvent{
		BaseEvent:  *NewBaseEvent(EventTaskCompletion),
		TaskResult: []DataFrame{&JsonDataFrame{Data: map[string]any{"result": "ok"}}},
	}
	data, err := json.Marshal(evt)
	if err != nil {
		t.Fatalf("Marshal TaskCompletionEvent 失败: %v", err)
	}

	var parsed struct {
		EventType string `json:"event_type"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}
	if parsed.EventType != "task_completion" {
		t.Errorf("event_type = %q, want %q", parsed.EventType, "task_completion")
	}
}

// TestFollowUpEvent_MarshalJSON 测试 FollowUpEvent 序列化
func TestFollowUpEvent_MarshalJSON(t *testing.T) {
	evt := FromText("继续执行")
	data, err := json.Marshal(evt)
	if err != nil {
		t.Fatalf("Marshal FollowUpEvent 失败: %v", err)
	}

	var parsed struct {
		EventType string `json:"event_type"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}
	if parsed.EventType != "follow_up" {
		t.Errorf("event_type = %q, want %q", parsed.EventType, "follow_up")
	}
}

// TestUnmarshalEvents_成功 测试 UnmarshalEvents 正常反序列化
func TestUnmarshalEvents_成功(t *testing.T) {
	events := []Event{
		&InputEvent{BaseEvent: BaseEvent{EventID: "e1", EventTypeField: EventInput}},
		&InputEvent{BaseEvent: BaseEvent{EventID: "e2", EventTypeField: EventInput}},
	}
	data, err := MarshalEvents(events)
	if err != nil {
		t.Fatalf("MarshalEvents 失败: %v", err)
	}
	got, err := UnmarshalEvents(data)
	if err != nil {
		t.Fatalf("UnmarshalEvents 失败: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("期望 2 个事件，实际 %d", len(got))
	}
}

// TestUnmarshalEvents_无效JSON 测试 UnmarshalEvents 处理无效 JSON
func TestUnmarshalEvents_无效JSON(t *testing.T) {
	_, err := UnmarshalEvents([]byte("invalid json"))
	if err == nil {
		t.Error("期望返回错误，实际返回 nil")
	}
}
