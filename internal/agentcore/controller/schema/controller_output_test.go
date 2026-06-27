package schema

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
)

// ──────────────────────────── 导出函数 ────────────────────────────

func TestControllerOutputChunk_嵌入OutputSchema(t *testing.T) {
	chunk := &ControllerOutputChunk{}
	// 通过嵌入的 OutputSchema 访问 Type 和 Index
	chunk.Type = "processing"
	chunk.Index = 0
	if chunk.SchemaType() != "processing" {
		t.Errorf("SchemaType() = %q, want %q", chunk.SchemaType(), "processing")
	}
}

func TestControllerOutputChunk_实现Schema接口(t *testing.T) {
	// 编译期验证 ControllerOutputChunk 实现了 stream.Schema 接口
	var _ stream.Schema = (*ControllerOutputChunk)(nil)
}

func TestControllerOutputChunk_Validate_正常(t *testing.T) {
	chunk := &ControllerOutputChunk{
		Payload: &ControllerOutputPayload{
			Type: "processing",
			Data: []DataFrame{&TextDataFrame{Text: "hello"}},
		},
	}
	chunk.Type = "processing"
	chunk.Index = 0

	if err := chunk.Validate(); err != nil {
		t.Errorf("Validate() 返回错误: %v", err)
	}
}

func TestControllerOutputChunk_Validate_缺少type(t *testing.T) {
	chunk := &ControllerOutputChunk{
		Payload: &ControllerOutputPayload{Type: "processing"},
	}
	chunk.Type = ""
	chunk.Index = 0

	if err := chunk.Validate(); err == nil {
		t.Error("缺少 type 时 Validate() 应返回错误")
	}
}

func TestControllerOutputChunk_Validate_payload为nil(t *testing.T) {
	chunk := &ControllerOutputChunk{}
	chunk.Type = "processing"
	chunk.Index = 0

	if err := chunk.Validate(); err == nil {
		t.Error("payload 为 nil 时 Validate() 应返回错误")
	}
}

func TestControllerOutputChunk_Validate_index为负(t *testing.T) {
	chunk := &ControllerOutputChunk{
		Payload: &ControllerOutputPayload{Type: "processing"},
	}
	chunk.Type = "processing"
	chunk.Index = -1

	if err := chunk.Validate(); err == nil {
		t.Error("index 为负数时 Validate() 应返回错误")
	}
}
