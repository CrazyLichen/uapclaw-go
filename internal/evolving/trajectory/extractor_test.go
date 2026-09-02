package trajectory

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

// ──────────────────────────── buildStep 测试 ────────────────────────────

// TestBuildStep_LLM步骤 验证 LLM span 转换为 TrajectoryStep
func TestBuildStep_LLM步骤(t *testing.T) {
	e := NewTracerTrajectoryExtractor()
	now := time.Now()

	span := &tracer.TraceAgentSpan{
		Span: tracer.Span{
			OnInvokeData: []map[string]any{
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
			},
			Outputs:    map[string]any{"role": "assistant", "content": "hi"},
			StartTime:  &now,
			Error:      map[string]any{"code": "timeout"},
			InvokeID:   "inv-1",
		},
		InvokeType: "llm",
		Name:       "llm_call",
		MetaData:   map[string]any{"agent_id": "agent-1"},
	}

	step := e.buildStep(span)
	assert.Equal(t, StepKindLLM, step.Kind)
	assert.NotNil(t, step.Detail)
	assert.Equal(t, "timeout", step.Error["code"])
	assert.NotEqual(t, 0, step.StartTimeMs)
	// Meta 包含 operator_id 和 agent_id
	assert.NotEmpty(t, step.Meta["operator_id"])
	assert.Equal(t, "agent-1", step.Meta["agent_id"])
	assert.Equal(t, "inv-1", step.Meta["invoke_id"])
}

// TestBuildStep_Tool步骤 验证 tool span 转换
func TestBuildStep_Tool步骤(t *testing.T) {
	e := NewTracerTrajectoryExtractor()

	span := &tracer.TraceAgentSpan{
		Span: tracer.Span{
			Inputs:  map[string]any{"inputs": map[string]any{"query": "test"}},
			Outputs: map[string]any{"outputs": map[string]any{"result": "ok"}},
		},
		InvokeType: "plugin",
		Name:       "search_tool",
		MetaData:   map[string]any{},
	}

	step := e.buildStep(span)
	assert.Equal(t, StepKindTool, step.Kind)
	toolDetail, ok := step.Detail.(*ToolCallDetail)
	require.True(t, ok)
	assert.Equal(t, "search_tool", toolDetail.ToolName)
	assert.Equal(t, "test", toolDetail.CallArgs["query"])
	assert.Equal(t, "ok", toolDetail.CallResult["result"])
}

// TestBuildStep_Workflow步骤 验证非 LLM/Tool 步骤将 I/O 放入 meta
func TestBuildStep_Workflow步骤(t *testing.T) {
	e := NewTracerTrajectoryExtractor()

	span := &tracer.TraceAgentSpan{
		Span: tracer.Span{
			Inputs:  map[string]any{"inputs": "workflow_input"},
			Outputs: map[string]any{"outputs": "workflow_output"},
		},
		InvokeType: "workflow",
		Name:       "workflow_step",
		MetaData:   map[string]any{},
	}

	step := e.buildStep(span)
	assert.Equal(t, StepKindWorkflow, step.Kind)
	assert.Nil(t, step.Detail)
	// 非 LLM/Tool 步骤，I/O 放入 meta
	assert.Equal(t, "workflow_input", step.Meta["inputs"])
	assert.Equal(t, "workflow_output", step.Meta["outputs"])
}

// TestBuildStep_TokenLevel字段提升 验证 prompt_token_ids 等从 response 提升到步骤级别
func TestBuildStep_TokenLevel字段提升(t *testing.T) {
	e := NewTracerTrajectoryExtractor()

	span := &tracer.TraceAgentSpan{
		Span: tracer.Span{
			OnInvokeData: []map[string]any{
				{
					"llm_params": map[string]any{
						"model":    "qwen-max",
						"messages": []map[string]any{{"role": "user", "content": "hello"}},
					},
				},
			},
			Outputs: map[string]any{
				"role":                "assistant",
				"content":             "hi",
				"prompt_token_ids":    []int{1, 2, 3},
				"completion_token_ids": []int{4, 5},
				"logprobs":            []any{0.1, 0.2},
			},
		},
		InvokeType: "llm",
		Name:       "llm_call",
		MetaData:   map[string]any{},
	}

	step := e.buildStep(span)
	assert.Equal(t, []int{1, 2, 3}, step.PromptTokenIDs)
	assert.Equal(t, []int{4, 5}, step.CompletionTokenIDs)
	assert.NotNil(t, step.Logprobs)
	// 提升后从 response 中移除
	llmDetail, ok := step.Detail.(*LLMCallDetail)
	require.True(t, ok)
	assert.Nil(t, llmDetail.Response["prompt_token_ids"])
	assert.Nil(t, llmDetail.Response["completion_token_ids"])
	assert.Nil(t, llmDetail.Response["logprobs"])
}

// ──────────────────────────── buildDetail 测试 ────────────────────────────

// TestBuildDetail_LLM 验证 LLM 类型构建 LLMCallDetail
func TestBuildDetail_LLM(t *testing.T) {
	e := NewTracerTrajectoryExtractor()

	span := &tracer.TraceAgentSpan{
		Span: tracer.Span{
			OnInvokeData: []map[string]any{
				{
					"llm_params": map[string]any{
						"model":    "qwen-max",
						"messages": []map[string]any{{"role": "user", "content": "hello"}},
						"tools":    []map[string]any{{"type": "function"}},
						"usage":    map[string]any{"prompt_tokens": 10},
					},
				},
			},
			Outputs: map[string]any{"role": "assistant", "content": "hi"},
		},
		InvokeType: "llm",
	}

	detail := e.buildDetail(span, StepKindLLM)
	llmDetail, ok := detail.(*LLMCallDetail)
	require.True(t, ok)
	assert.Equal(t, "qwen-max", llmDetail.Model)
	assert.Len(t, llmDetail.Messages, 1)
	assert.NotNil(t, llmDetail.Response)
	assert.NotNil(t, llmDetail.Tools)
	assert.NotNil(t, llmDetail.Usage)
}

// TestBuildDetail_Tool 验证 Tool 类型构建 ToolCallDetail
func TestBuildDetail_Tool(t *testing.T) {
	e := NewTracerTrajectoryExtractor()

	span := &tracer.TraceAgentSpan{
		Span: tracer.Span{
			Inputs:  map[string]any{"inputs": map[string]any{"query": "test"}},
			Outputs: map[string]any{"outputs": map[string]any{"result": "ok"}},
		},
		InvokeType: "plugin",
		Name:       "search",
	}

	detail := e.buildDetail(span, StepKindTool)
	toolDetail, ok := detail.(*ToolCallDetail)
	require.True(t, ok)
	assert.Equal(t, "search", toolDetail.ToolName)
}

// TestBuildDetail_其他类型 验证非 LLM/Tool 返回 nil
func TestBuildDetail_其他类型(t *testing.T) {
	e := NewTracerTrajectoryExtractor()
	span := &tracer.TraceAgentSpan{}

	detail := e.buildDetail(span, StepKindWorkflow)
	assert.Nil(t, detail)

	detail = e.buildDetail(span, StepKindAgent)
	assert.Nil(t, detail)
}

// ──────────────────────────── buildLLMDetail 测试 ────────────────────────────

// TestBuildLLMDetail_完整数据 完整 LLM 调用数据
func TestBuildLLMDetail_完整数据(t *testing.T) {
	e := NewTracerTrajectoryExtractor()

	span := &tracer.Span{
		OnInvokeData: []map[string]any{
			{
				"llm_params": map[string]any{
					"model":    "qwen-max",
					"messages": []map[string]any{{"role": "user", "content": "hello"}},
					"tools":    []map[string]any{{"type": "function"}},
					"usage":    map[string]any{"prompt_tokens": 10, "completion_tokens": 5},
				},
			},
		},
		Outputs: map[string]any{"role": "assistant", "content": "hi"},
	}

	detail := e.buildLLMDetail(span)
	require.NotNil(t, detail)
	assert.Equal(t, "qwen-max", detail.Model)
	assert.Len(t, detail.Messages, 1)
	assert.NotNil(t, detail.Tools)
	assert.NotNil(t, detail.Usage)
	assert.Equal(t, 10, detail.Usage["prompt_tokens"])
}

// TestBuildLLMDetail_空OnInvokeData 空 OnInvokeData 返回 nil
func TestBuildLLMDetail_空OnInvokeData(t *testing.T) {
	e := NewTracerTrajectoryExtractor()
	span := &tracer.Span{OnInvokeData: nil}
	detail := e.buildLLMDetail(span)
	assert.Nil(t, detail)
}

// TestBuildLLMDetail_无LLMParams 无 llm_params 返回 nil
func TestBuildLLMDetail_无LLMParams(t *testing.T) {
	e := NewTracerTrajectoryExtractor()
	span := &tracer.Span{
		OnInvokeData: []map[string]any{{"other_key": "value"}},
	}
	detail := e.buildLLMDetail(span)
	assert.Nil(t, detail)
}

// TestBuildLLMDetail_Usage从LLMParams回退 当 response 无 usage 时从 llm_params 回退
func TestBuildLLMDetail_Usage从LLMParams回退(t *testing.T) {
	e := NewTracerTrajectoryExtractor()

	span := &tracer.Span{
		OnInvokeData: []map[string]any{
			{
				"llm_params": map[string]any{
					"model":    "qwen-max",
					"messages": []map[string]any{{"role": "user", "content": "hello"}},
					"usage":    map[string]any{"prompt_tokens": 20, "completion_tokens": 10},
				},
			},
		},
		Outputs: map[string]any{"role": "assistant", "content": "hi"},
	}

	detail := e.buildLLMDetail(span)
	require.NotNil(t, detail)
	// response 没有 usage，从 llm_params 回退
	assert.NotNil(t, detail.Usage)
	assert.Equal(t, 20, detail.Usage["prompt_tokens"])
}

// ──────────────────────────── buildToolDetail 测试 ────────────────────────────

// TestBuildToolDetail_基本 验证工具步骤构建
func TestBuildToolDetail_基本(t *testing.T) {
	e := NewTracerTrajectoryExtractor()

	span := &tracer.TraceAgentSpan{
		Span: tracer.Span{
			Inputs:  map[string]any{"inputs": map[string]any{"query": "test"}},
			Outputs: map[string]any{"outputs": map[string]any{"result": "ok"}},
		},
		Name: "search",
	}

	detail := e.buildToolDetail(span)
	require.NotNil(t, detail)
	assert.Equal(t, "search", detail.ToolName)
	assert.Equal(t, "test", detail.CallArgs["query"])
	assert.Equal(t, "ok", detail.CallResult["result"])
	assert.Empty(t, detail.ToolDescription)
	assert.Nil(t, detail.ToolSchema)
}

// TestBuildToolDetail_空名称 工具名称为空
func TestBuildToolDetail_空名称(t *testing.T) {
	e := NewTracerTrajectoryExtractor()

	span := &tracer.TraceAgentSpan{
		Span: tracer.Span{},
		Name: "",
	}

	detail := e.buildToolDetail(span)
	require.NotNil(t, detail)
	assert.Empty(t, detail.ToolName)
}

// ──────────────────────────── buildMeta 测试 ────────────────────────────

// TestBuildMeta_完整 验证完整 meta 构建
func TestBuildMeta_完整(t *testing.T) {
	e := NewTracerTrajectoryExtractor()

	span := &tracer.TraceAgentSpan{
		Span: tracer.Span{
			Inputs:        "raw_input",
			Outputs:       "raw_output",
			InvokeID:      "inv-1",
			ParentInvokeID: "parent-1",
			ChildInvokesID: []string{"child-1"},
		},
		Name:       "test_step",
		MetaData:   map[string]any{"custom_key": "custom_val"},
	}

	meta := e.buildMeta(span, map[string]any{"custom_key": "custom_val"}, StepKindWorkflow, nil)
	assert.Equal(t, "custom_val", meta["custom_key"])
	assert.NotEmpty(t, meta["operator_id"])
	assert.Equal(t, "test_step", meta["span_name"])
	assert.Equal(t, "inv-1", meta["invoke_id"])
	assert.Equal(t, "parent-1", meta["parent_invoke_id"])
	assert.Equal(t, []string{"child-1"}, meta["child_invokes"])
	// 非 LLM/Tool 步骤包含 I/O
	assert.Equal(t, "raw_input", meta["inputs"])
	assert.Equal(t, "raw_output", meta["outputs"])
}

// TestBuildMeta_LLM步骤不包含IO LLM/Tool 步骤不包含 I/O
func TestBuildMeta_LLM步骤不包含IO(t *testing.T) {
	e := NewTracerTrajectoryExtractor()

	span := &tracer.TraceAgentSpan{
		Span: tracer.Span{
			Inputs:  "raw_input",
			Outputs: "raw_output",
		},
		Name:     "llm_call",
		MetaData: map[string]any{},
	}

	meta := e.buildMeta(span, map[string]any{}, StepKindLLM, &LLMCallDetail{Model: "qwen-max"})
	assert.NotContains(t, meta, "inputs")
	assert.NotContains(t, meta, "outputs")
}

// TestBuildMeta_AgentID从BaseMeta agent_id 从 baseMeta 回退
func TestBuildMeta_AgentID从BaseMeta(t *testing.T) {
	e := NewTracerTrajectoryExtractor()

	span := &tracer.TraceAgentSpan{
		Span:   tracer.Span{},
		Name:   "step",
		MetaData: map[string]any{},
	}

	meta := e.buildMeta(span, map[string]any{"agent_id": "agent-1"}, StepKindLLM, nil)
	assert.Equal(t, "agent-1", meta["agent_id"])
}

// ──────────────────────────── getOperatorID 测试 ────────────────────────────

// TestGetOperatorID_LLMCallID 从 llm_call_id 获取
func TestGetOperatorID_LLMCallID(t *testing.T) {
	e := NewTracerTrajectoryExtractor()

	span := &tracer.TraceAgentSpan{
		Name:     "fallback_name",
		MetaData: map[string]any{},
	}
	meta := map[string]any{"llm_call_id": "call-1"}

	id := e.getOperatorID(span, meta)
	assert.Equal(t, "call-1", id)
}

// TestGetOperatorID_全部为空 全部为空时返回空字符串
func TestGetOperatorID_全部为空(t *testing.T) {
	e := NewTracerTrajectoryExtractor()

	span := &tracer.TraceAgentSpan{
		MetaData: map[string]any{},
	}
	meta := map[string]any{}

	id := e.getOperatorID(span, meta)
	assert.Equal(t, "", id)
}

// ──────────────────────────── extractInputsAsMap / extractOutputsAsMap 测试 ────────────────────────────

// TestExtractInputsAsMap_嵌套 inputs 嵌套解包
func TestExtractInputsAsMap_嵌套(t *testing.T) {
	span := &tracer.Span{Inputs: map[string]any{"inputs": map[string]any{"query": "test"}}}
	result := extractInputsAsMap(span)
	assert.Equal(t, "test", result["query"])
}

// TestExtractInputsAsMap_非Map 非 map 输入返回 nil
func TestExtractInputsAsMap_非Map(t *testing.T) {
	span := &tracer.Span{Inputs: "raw_string"}
	result := extractInputsAsMap(span)
	assert.Nil(t, result)
}

// TestExtractOutputsAsMap_嵌套 outputs 嵌套解包
func TestExtractOutputsAsMap_嵌套(t *testing.T) {
	span := &tracer.Span{Outputs: map[string]any{"outputs": map[string]any{"result": "ok"}}}
	result := extractOutputsAsMap(span)
	assert.Equal(t, "ok", result["result"])
}

// TestExtractOutputsAsMap_非Map 非 map 输出返回 nil
func TestExtractOutputsAsMap_非Map(t *testing.T) {
	span := &tracer.Span{Outputs: "raw_string"}
	result := extractOutputsAsMap(span)
	assert.Nil(t, result)
}
