package utils

// payload_bridge_test.go 覆盖 PayloadToMap 的四种形态与 ParseStreamChunk 的结构体 payload 分支。
//
// 回归背景：task_loop 产出 *cschema.ControllerOutputPayload 结构体 payload，
// 消费端原先只断言 map[string]any，导致 task_failed 被整块丢弃（ACP 侧只剩 idle → end_turn）。

import (
	"testing"

	cschema "github.com/uapclaw/uapclaw-go/internal/agentcore/controller/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interaction"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
)

// TestPayloadToMap_nil nil 返回 nil
func TestPayloadToMap_nil(t *testing.T) {
	if got := PayloadToMap(nil); got != nil {
		t.Errorf("nil 应返回 nil，实际 %v", got)
	}
}

// TestPayloadToMap_map 原样返回
func TestPayloadToMap_map(t *testing.T) {
	in := map[string]any{"type": "task_failed"}
	got := PayloadToMap(in)
	if got["type"] != "task_failed" {
		t.Errorf("map 应原样返回，实际 %v", got)
	}
}

// TestPayloadToMap_结构体指针 转为 map（type/data/metadata 三键）
func TestPayloadToMap_结构体指针(t *testing.T) {
	in := &cschema.ControllerOutputPayload{
		Type:     "task_failed",
		Data:     []cschema.DataFrame{&cschema.TextDataFrame{Text: "boom"}},
		Metadata: map[string]any{"task_id": "t1"},
	}
	got := PayloadToMap(in)
	if got["type"] != "task_failed" {
		t.Errorf("type = %v", got["type"])
	}
	data, ok := got["data"].([]any)
	if !ok || len(data) != 1 {
		t.Fatalf("data 应为长度 1 的 []any，实际 %v", got["data"])
	}
	item, _ := data[0].(map[string]any)
	if item["text"] != "boom" {
		t.Errorf("data[0].text = %v", item["text"])
	}
	meta, _ := got["metadata"].(map[string]any)
	if meta["task_id"] != "t1" {
		t.Errorf("metadata.task_id = %v", meta["task_id"])
	}
}

// TestPayloadToMap_结构体值 转为 map
func TestPayloadToMap_结构体值(t *testing.T) {
	got := PayloadToMap(cschema.ControllerOutputPayload{Type: "task_completion"})
	if got["type"] != "task_completion" {
		t.Errorf("type = %v", got["type"])
	}
}

// TestPayloadToMap_nil指针 nil 结构体指针返回 nil
func TestPayloadToMap_nil指针(t *testing.T) {
	if got := PayloadToMap((*cschema.ControllerOutputPayload)(nil)); got != nil {
		t.Errorf("nil 指针应返回 nil，实际 %v", got)
	}
}

// TestPayloadToMap_不支持类型 返回 nil（与断言失败行为一致）
func TestPayloadToMap_不支持类型(t *testing.T) {
	if got := PayloadToMap("plain-string"); got != nil {
		t.Errorf("字符串应返回 nil，实际 %v", got)
	}
}

// TestParseStreamChunk_结构体payload_taskFailed 结构体 payload 的 task_failed 也转 chat.error
func TestParseStreamChunk_结构体payload_taskFailed(t *testing.T) {
	output := &stream.OutputSchema{
		Type: "controller_output",
		Payload: &cschema.ControllerOutputPayload{
			Type: "task_failed",
			Data: []cschema.DataFrame{&cschema.TextDataFrame{Text: "上游模型失败"}},
		},
	}
	result := ParseStreamChunk(output, nil, nil, nil, false)
	if result["event_type"] != "chat.error" {
		t.Fatalf("期望 chat.error，实际 %v", result)
	}
	if result["error"] != "上游模型失败" {
		t.Errorf("error = %v", result["error"])
	}
}

// ──────────────────────────── 泛化桥接 ────────────────────────────

// jsonPayload 用于验证"任意结构体（含 json tag）都能被泛化桥接"——无需为每个类型加 case
type jsonPayload struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

// toMapPayload 验证 ToMap 快路径优先于 JSON 往返
type toMapPayload struct {
	called int
}

// ToMap 返回固定映射（快路径）
func (p *toMapPayload) ToMap() map[string]any {
	p.called++
	return map[string]any{"from": "to_map"}
}

// TestPayloadToMap_泛化自定义结构体 未注册的类型也能桥接（泛化的核心收益）
func TestPayloadToMap_泛化自定义结构体(t *testing.T) {
	got := PayloadToMap(&jsonPayload{ID: "n1", Text: "hi"})
	if got["id"] != "n1" || got["text"] != "hi" {
		t.Fatalf("期望 id/text 小写键，实际 %v", got)
	}
}

// TestPayloadToMap_ToMap快路径 实现 ToMap 的类型优先走快路径
func TestPayloadToMap_ToMap快路径(t *testing.T) {
	p := &toMapPayload{}
	got := PayloadToMap(p)
	if got["from"] != "to_map" || p.called != 1 {
		t.Fatalf("应走 ToMap 快路径，实际 %v（调用 %d 次）", got, p.called)
	}
}

// TestPayloadToMap_InteractionOutput 交互结构体带小写 json tag，桥接后键名对齐 Python
func TestPayloadToMap_InteractionOutput(t *testing.T) {
	got := PayloadToMap(interaction.InteractionOutput{ID: "req-1", Value: map[string]any{"message": "确认"}})
	if got["id"] != "req-1" {
		t.Fatalf("期望 id=req-1，实际 %v", got)
	}
	value, ok := got["value"].(map[string]any)
	if !ok || value["message"] != "确认" {
		t.Fatalf("期望 value.message=确认，实际 %v", got["value"])
	}
}

// TestPayloadsToMaps_嵌套展开与结构体桥接 对齐 Python _iter_interactions 的摊平语义
func TestPayloadsToMaps_嵌套展开与结构体桥接(t *testing.T) {
	items := []any{
		map[string]any{"id": "a"},
		[]any{
			&jsonPayload{ID: "b"},
			[]map[string]any{{"id": "c"}},
		},
		nil, // 跳过
	}
	got := PayloadsToMaps(items)
	if len(got) != 3 {
		t.Fatalf("应摊平为 3 个元素，实际 %d：%v", len(got), got)
	}
	if got[0]["id"] != "a" || got[1]["id"] != "b" || got[2]["id"] != "c" {
		t.Errorf("摊平顺序/内容不符：%v", got)
	}
}

// TestParseStreamChunk_interaction_结构体payload __interaction__ 携带结构体时也能转为交互事件
// 回归：payload 是 interaction.InteractionOutput（workflow 中断的真实形态），
// 只断言 map 时会退化成空 map，交互事件被静默丢弃。
func TestParseStreamChunk_interaction_结构体payload(t *testing.T) {
	var gotList []map[string]any
	converter := func(interactions []map[string]any) map[string]any {
		gotList = interactions
		return map[string]any{"event_type": "chat.ask_user_question"}
	}
	output := &stream.OutputSchema{
		Type:    "__interaction__",
		Payload: interaction.InteractionOutput{ID: "req-9", Value: map[string]any{"message": "确认"}},
	}
	result := ParseStreamChunk(output, nil, nil, converter, false)
	if result["event_type"] != "chat.ask_user_question" {
		t.Fatalf("期望 chat.ask_user_question，实际 %v", result)
	}
	if len(gotList) != 1 || gotList[0]["id"] != "req-9" {
		t.Fatalf("converter 应收到 1 个已桥接元素，实际 %v", gotList)
	}
}

// ──────────────────────────── 信封形态（controller_output 内嵌 interaction）────────────────────────────

// TestFindInteractionPayloads_结构体信封 对齐 Python 信封形态：
// workflow_task_executor._handle_interruption 把 interaction 塞进
// WorkflowOutput.result → ControllerOutputPayload.data，且 Go 侧是结构体。
func TestFindInteractionPayloads_结构体信封(t *testing.T) {
	// 模拟 JsonDataFrame.data → {"result": <OutputSchema 结构体>}
	envelope := map[string]any{
		"type": "task_interaction",
		"data": []any{
			map[string]any{"data": map[string]any{
				"result": &stream.OutputSchema{
					Type:    "__interaction__",
					Payload: interaction.InteractionOutput{ID: "req-env", Value: map[string]any{"message": "确认"}},
				},
			}},
		},
	}
	found := FindInteractionPayloads(envelope)
	if len(found) != 1 {
		t.Fatalf("结构体信封应命中 1 个 interaction，实际 %d：%v", len(found), found)
	}
	m := PayloadToMap(found[0])
	if m["id"] != "req-env" {
		t.Errorf("应取到 payload 并桥接出 id，实际 %v", m)
	}
}

// TestFindInteractionPayloads_值结构体信封 OutputSchema 以值形态嵌套时同样命中
func TestFindInteractionPayloads_值结构体信封(t *testing.T) {
	envelope := map[string]any{
		"data": []map[string]any{
			{"result": stream.OutputSchema{
				Type:    "__interaction__",
				Payload: map[string]any{"id": "req-val"},
			}},
		},
	}
	found := FindInteractionPayloads(envelope)
	if len(found) != 1 {
		t.Fatalf("值结构体信封应命中，实际 %v", found)
	}
	if m := PayloadToMap(found[0]); m["id"] != "req-val" {
		t.Errorf("expected id=req-val，实际 %v", m)
	}
}

// TestParseStreamChunk_controllerOutput_结构体信封 信封形态在 ParseStreamChunk 端到端可用
func TestParseStreamChunk_controllerOutput_结构体信封(t *testing.T) {
	var gotList []map[string]any
	converter := func(interactions []map[string]any) map[string]any {
		gotList = interactions
		return map[string]any{"event_type": "chat.ask_user_question"}
	}
	output := &stream.OutputSchema{
		Type: "controller_output",
		Payload: map[string]any{
			"type": "task_interaction",
			"data": []any{
				map[string]any{"data": map[string]any{
					"result": &stream.OutputSchema{
						Type:    "__interaction__",
						Payload: interaction.InteractionOutput{ID: "req-e2e"},
					},
				}},
			},
		},
	}
	result := ParseStreamChunk(output, nil, nil, converter, false)
	if result["event_type"] != "chat.ask_user_question" {
		t.Fatalf("信封形态应解析为 chat.ask_user_question，实际 %v", result)
	}
	if len(gotList) != 1 || gotList[0]["id"] != "req-e2e" {
		t.Fatalf("converter 应收到 1 个已桥接元素，实际 %v", gotList)
	}
}
