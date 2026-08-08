package trajectory

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/tracer"
)

func TestNewTracerTrajectoryExtractor(t *testing.T) {
	e := NewTracerTrajectoryExtractor()
	assert.NotNil(t, e)
	assert.Nil(t, e.resourceManager)
}

func TestNewTracerTrajectoryExtractor_WithResourceManager(t *testing.T) {
	rm := "mock_resource_manager"
	e := NewTracerTrajectoryExtractor(rm)
	assert.Equal(t, rm, e.resourceManager)
}

func TestTracerTrajectoryExtractor_Extract_空Session(t *testing.T) {
	e := NewTracerTrajectoryExtractor()
	traj := e.Extract(nil, "case_1")
	assert.NotNil(t, traj)
	assert.Equal(t, "case_1", traj.SessionID)
	assert.Empty(t, traj.Steps)
}

func TestTracerTrajectoryExtractor_Extract_空Tracer(t *testing.T) {
	e := NewTracerTrajectoryExtractor()
	sess := session.NewSession()
	traj := e.Extract(sess, "case_1")
	assert.NotNil(t, traj)
	assert.Empty(t, traj.Steps)
}

func TestTracerTrajectoryExtractor_Extract_有Spans(t *testing.T) {
	e := NewTracerTrajectoryExtractor()
	sess := session.NewSession()
	tr := sess.Tracer()
	if tr == nil {
		t.Skip("Session.Tracer() 返回 nil，跳过")
	}

	// 创建一个 LLM span
	agentSpan := tr.AgentSpanManager.CreateAgentSpan()
	agentSpan.InvokeType = "llm"
	agentSpan.Name = "llm_call"
	agentSpan.OnInvokeData = []map[string]any{
		{
			"llm_params": map[string]any{
				"model":    "qwen-max",
				"messages": []map[string]any{{"role": "user", "content": "hello"}},
				"usage": map[string]any{
					"prompt_tokens":     10,
					"completion_tokens": 5,
				},
			},
		},
	}
	now := time.Now()
	agentSpan.StartTime = &now
	tr.AgentSpanManager.UpdateSpan(&agentSpan.Span, map[string]any{})

	traj := e.Extract(sess, "test_case")
	assert.NotNil(t, traj)
	assert.Equal(t, "test_case", traj.SessionID)
	assert.Equal(t, "offline", traj.Source)
}

func TestClassifyKind(t *testing.T) {
	e := NewTracerTrajectoryExtractor()

	tests := []struct {
		invokeType string
		expected   StepKind
	}{
		{"plugin", StepKindTool},
		{"llm", StepKindLLM},
		{"workflow", StepKindWorkflow},
		{"memory", StepKindMemory},
		{"", StepKindAgent},
		{"unknown", StepKindAgent},
	}
	for _, tt := range tests {
		agentSpan := &tracer.TraceAgentSpan{InvokeType: tt.invokeType}
		kind := e.classifyKind(agentSpan)
		assert.Equal(t, tt.expected, kind, "invokeType=%s", tt.invokeType)
	}
}

func TestExtractInputs(t *testing.T) {
	// 对齐 Python: raw = getattr(span, "inputs", None)
	//     if isinstance(raw, dict) and "inputs" in raw: return raw["inputs"]
	span := &tracer.Span{Inputs: map[string]any{"inputs": map[string]any{"query": "test"}}}
	result := extractInputs(span)
	m, ok := result.(map[string]any)
	assert.True(t, ok)
	assert.Equal(t, "test", m["query"])

	// 非 inputs 嵌套
	span2 := &tracer.Span{Inputs: map[string]any{"query": "test"}}
	result2 := extractInputs(span2)
	m2, ok := result2.(map[string]any)
	assert.True(t, ok)
	assert.Equal(t, "test", m2["query"])
}

func TestExtractOutputs(t *testing.T) {
	span := &tracer.Span{Outputs: map[string]any{"outputs": map[string]any{"result": "ok"}}}
	result := extractOutputs(span)
	m, ok := result.(map[string]any)
	assert.True(t, ok)
	assert.Equal(t, "ok", m["result"])
}

func TestParseLLMResponse(t *testing.T) {
	// 对齐 Python: isinstance(outputs, dict) → return outputs
	resp := map[string]any{"role": "assistant", "content": "hello"}
	result := parseLLMResponse(resp)
	assert.Equal(t, "hello", result["content"])

	// nil 输入
	result2 := parseLLMResponse(nil)
	assert.Nil(t, result2)

	// 非 dict 输入
	result3 := parseLLMResponse("string")
	assert.Nil(t, result3)
}

func TestGetOperatorID(t *testing.T) {
	e := NewTracerTrajectoryExtractor()

	// 从 MetaData 中获取 operator_id
	agentSpan := &tracer.TraceAgentSpan{
		MetaData: map[string]any{"operator_id": "op_1"},
	}
	id := e.getOperatorID(agentSpan, map[string]any{})
	assert.Equal(t, "op_1", id)

	// 从 meta 中获取
	agentSpan2 := &tracer.TraceAgentSpan{Name: "fallback_name"}
	meta2 := map[string]any{"operator_id": "op_2"}
	id2 := e.getOperatorID(agentSpan2, meta2)
	assert.Equal(t, "op_2", id2)

	// 从 name 回退
	agentSpan3 := &tracer.TraceAgentSpan{Name: "tool_name"}
	id3 := e.getOperatorID(agentSpan3, map[string]any{})
	assert.Equal(t, "tool_name", id3)
}

func TestDtToMs(t *testing.T) {
	// nil 时间
	assert.Equal(t, 0, dtToMs(nil))

	// 有效时间
	now := time.Now()
	ms := dtToMs(&now)
	assert.Equal(t, int(now.UnixMilli()), ms)
}
