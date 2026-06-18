package tracer

import "testing"

// ──────────────────────────── 导出函数 ────────────────────────────

// TestInvokeType_值对齐 验证 InvokeType 枚举值与 Python 一致
func TestInvokeType_值对齐(t *testing.T) {
	expected := map[InvokeType]string{
		InvokeTypePrompt:    "prompt",
		InvokeTypeLLM:       "llm",
		InvokeTypePlugin:    "plugin",
		InvokeTypeWorkflow:  "workflow",
		InvokeTypeChain:     "chain",
		InvokeTypeRetriever: "retriever",
		InvokeTypeEvaluator: "evaluator",
	}
	for typ, val := range expected {
		if string(typ) != val {
			t.Errorf("InvokeType 值不匹配: got %q, want %q", typ, val)
		}
	}
}

// TestNodeStatus_值对齐 验证 NodeStatus 枚举值与 Python 一致
func TestNodeStatus_值对齐(t *testing.T) {
	expected := map[NodeStatus]string{
		NodeStatusStart:       "start",
		NodeStatusFinish:      "finish",
		NodeStatusRunning:     "running",
		NodeStatusInterrupted: "interrupted",
		NodeStatusError:       "error",
	}
	for typ, val := range expected {
		if string(typ) != val {
			t.Errorf("NodeStatus 值不匹配: got %q, want %q", typ, val)
		}
	}
}

// TestTraceEvent_Agent事件完整性 验证 Agent 事件共 22 种
func TestTraceEvent_Agent事件完整性(t *testing.T) {
	agentEvents := []TraceEvent{
		TraceChainStart, TraceChainEnd, TraceChainError,
		TraceLLMStart, TraceLLMRequest, TraceLLMEnd, TraceLLMError,
		TracePromptStart, TracePromptEnd, TracePromptError,
		TracePluginStart, TracePluginEnd, TracePluginError,
		TraceRetrieverStart, TraceRetrieverEnd, TraceRetrieverError,
		TraceEvaluatorStart, TraceEvaluatorEnd, TraceEvaluatorError,
		TraceWorkflowStart, TraceWorkflowEnd, TraceWorkflowError,
	}
	if len(agentEvents) != 22 {
		t.Errorf("Agent 事件数量: got %d, want 22", len(agentEvents))
	}
}

// TestTraceEvent_Workflow事件完整性 验证 Workflow 事件共 8 种
func TestTraceEvent_Workflow事件完整性(t *testing.T) {
	wfEvents := []TraceEvent{
		TraceWFCallStart, TraceWFPreInvoke, TraceWFPreStream,
		TraceWFInvoke, TraceWFPostStream, TraceWFPostInvoke,
		TraceWFCallDone, TraceWFInteract,
	}
	if len(wfEvents) != 8 {
		t.Errorf("Workflow 事件数量: got %d, want 8", len(wfEvents))
	}
}
