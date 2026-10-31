package utils

// payload_bridge_test.go 覆盖 PayloadToMap 的四种形态与 ParseStreamChunk 的结构体 payload 分支。
//
// 回归背景：task_loop 产出 *cschema.ControllerOutputPayload 结构体 payload，
// 消费端原先只断言 map[string]any，导致 task_failed 被整块丢弃（ACP 侧只剩 idle → end_turn）。

import (
	"testing"

	cschema "github.com/uapclaw/uapclaw-go/internal/agentcore/controller/schema"
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
