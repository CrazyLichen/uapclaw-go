package observability

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// findSpanAttr 取 span 属性字符串值。
func findSpanAttr(st tracetest.SpanStub, key string) (string, bool) {
	for _, a := range st.Attributes {
		if string(a.Key) == key {
			return a.Value.Emit(), true
		}
	}
	return "", false
}

// TestRecordRequestInput_标准属性 system 归指令槽、user 归输入消息。
// Python: callback_handler.py:419-434 + :1730-1766
func TestRecordRequestInput_标准属性(t *testing.T) {
	tracer, exp := newTestTracer(t)
	h := NewOtelCallbackHandler(DefaultObservabilityConfig(), tracer)
	_, span := tracer.Start(context.Background(), "chat")
	h.RecordRequestInput(span, []map[string]any{
		{"role": "system", "content": "sys prompt"},
		{"role": "user", "content": "hello", "message_id": "m1",
			"metadata": map[string]any{llmschemaOriginKey(): llmschemaOriginExternal()}},
	})
	span.End()
	spans := exp.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("应导出 1 条 span，实际 %d", len(spans))
	}
	st := spans[0]
	if v, _ := findSpanAttr(st, OJRequestMessageCount); v != "2" {
		t.Fatalf("消息计数漂移: %q", v)
	}
	sysInstr, ok := findSpanAttr(st, GenAISystemInstructions)
	if !ok || !strings.Contains(sysInstr, "sys prompt") {
		t.Fatalf("system_instructions 缺失或内容不符: %q", sysInstr)
	}
	inputMsgs, ok := findSpanAttr(st, GenAIInputMessages)
	// Python _structured_message 不产 origin（来源走 provenance 属性），仅断言角色与内容
	if !ok || !strings.Contains(inputMsgs, `"role":"user"`) || !strings.Contains(inputMsgs, "hello") {
		t.Fatalf("input.messages 缺失或内容不符: %q", inputMsgs)
	}
}

// TestRecordResponseOutput_结构化输出 输出消息属性落盘。
// Python: callback_handler.py:436-445 + :1818-1852
func TestRecordResponseOutput_结构化输出(t *testing.T) {
	tracer, exp := newTestTracer(t)
	h := NewOtelCallbackHandler(DefaultObservabilityConfig(), tracer)
	_, span := tracer.Start(context.Background(), "chat")
	h.RecordResponseOutput(span, map[string]any{
		"role":              "assistant",
		"content":           "answer",
		"reasoning_content": "thinking",
	})
	span.End()
	spans := exp.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("应导出 1 条 span，实际 %d", len(spans))
	}
	out, ok := findSpanAttr(spans[0], GenAIOutputMessages)
	if !ok {
		t.Fatal("output.messages 缺失")
	}
	if !strings.Contains(out, `"role":"assistant"`) || !strings.Contains(out, "answer") || !strings.Contains(out, "thinking") {
		t.Fatalf("输出内容不符: %q", out)
	}
}

// TestContextWindowMessages_出现id context_message_id 与 system 槽兜底。
// Python: callback_handler.py:399-417 + :1628-1652
func TestContextWindowMessages_出现id(t *testing.T) {
	h := NewOtelCallbackHandler(DefaultObservabilityConfig(), newTestTracerOnly(t))
	msgs := h.ContextWindowMessages([]map[string]any{
		{"role": "system", "content": "s"},
		{"role": "user", "content": "hi", "metadata": map[string]any{"context_message_id": "ctx-1"}},
		{"role": "user", "content": "again"},
	})
	if msgs[0]["message_id"] != RequestSystemSlotPrefix+"0" {
		t.Fatalf("system 槽兜底 id 漂移: %v", msgs[0]["message_id"])
	}
	if msgs[1]["message_id"] != "ctx-1" {
		t.Fatalf("context_message_id 应被采用: %v", msgs[1]["message_id"])
	}
	if msgs[2]["message_id"] == "" || msgs[2]["message_id"] == RequestSystemSlotPrefix+"0" {
		t.Fatalf("无 id 消息应生成 uuid: %v", msgs[2]["message_id"])
	}
}

// TestTrajectoryMessages_脱敏 redact_prompts 开启时内容哈希。
// Python: callback_handler.py:1654-1669 + :1671-1714
func TestTrajectoryMessages_脱敏(t *testing.T) {
	cfg := DefaultObservabilityConfig()
	cfg.RedactPrompts = true
	h := NewOtelCallbackHandler(cfg, newTestTracerOnly(t))
	msgs := h.trajectoryMessages([]map[string]any{{"role": "user", "content": "secret"}}, nil, nil)
	content := msgs[0]["content"]
	if content == "secret" {
		t.Fatal("应脱敏")
	}
	if s, _ := content.(string); !strings.HasPrefix(s, "sha256:") {
		t.Fatalf("应为哈希: %v", content)
	}
}

// TestJsonCompatible_嵌套与标量 归一行为。
// Python: callback_handler.py:315-383
func TestJsonCompatible_嵌套与标量(t *testing.T) {
	got := jsonCompatible(map[string]any{"a": 1, "b": []any{"x", 2.5, true}, "c": nil}, 0, map[uintptr]bool{})
	data, err := json.Marshal(got)
	if err != nil || string(data) != `{"a":1,"b":["x",2.5,true],"c":null}` {
		t.Fatalf("归一漂移: %v %v", string(data), err)
	}
}

// llmschemaOriginKey / llmschemaOriginExternal 测试辅助（避免导入循环别名冗长）
func llmschemaOriginKey() string      { return llmschema.OpenjiuwenMessageOriginMetadata }
func llmschemaOriginExternal() string { return llmschema.OpenjiuwenMessageOriginExternalUser }

// newTestTracerOnly 仅建 tracer 不需要导出器断言的用例
func newTestTracerOnly(t *testing.T) trace.Tracer {
	t.Helper()
	tracer, _ := newTestTracer(t)
	return tracer
}
