package observability

import (
	"testing"
)

func TestGenAI常量值与Python一致(t *testing.T) {
	tests := []struct {
		name     string
		got      string
		expected string
	}{
		{"GenAISystem", GenAISystem, "gen_ai.system"},
		{"GenAIRequestModel", GenAIRequestModel, "gen_ai.request.model"},
		{"GenAIRequestTemperature", GenAIRequestTemperature, "gen_ai.request.temperature"},
		{"GenAIRequestTopP", GenAIRequestTopP, "gen_ai.request.top_p"},
		{"GenAIRequestMaxTokens", GenAIRequestMaxTokens, "gen_ai.request.max_tokens"},
		{"GenAIPrompt", GenAIPrompt, "gen_ai.prompt"},
		{"GenAICompletion", GenAICompletion, "gen_ai.completion"},
		{"GenAIUsagePromptTokens", GenAIUsagePromptTokens, "gen_ai.usage.prompt_tokens"},
		{"GenAIUsageCompletionTokens", GenAIUsageCompletionTokens, "gen_ai.usage.completion_tokens"},
		{"GenAIUsageTotalTokens", GenAIUsageTotalTokens, "gen_ai.usage.total_tokens"},
		{"GenAIResponseFinishReason", GenAIResponseFinishReason, "gen_ai.response.finish_reason"},
		{"GenAIResponseModel", GenAIResponseModel, "gen_ai.response.model"},
		{"GenAIResponseTTFTMs", GenAIResponseTTFTMs, "gen_ai.response.time_to_first_token_ms"},
		{"GenAIToolName", GenAIToolName, "gen_ai.tool.name"},
		{"GenAIToolInput", GenAIToolInput, "gen_ai.tool.input"},
		{"GenAIToolOutput", GenAIToolOutput, "gen_ai.tool.output"},
		{"GenAIToolID", GenAIToolID, "gen_ai.tool.id"},
	}
	for _, tt := range tests {
		if tt.got != tt.expected {
			t.Errorf("%s: 期望 %q，实际 %q", tt.name, tt.expected, tt.got)
		}
	}
}

func TestAgentTeam常量值与Python一致(t *testing.T) {
	tests := []struct {
		name     string
		got      string
		expected string
	}{
		{"ATTeamName", ATTeamName, "agentteam.team.name"},
		{"ATTeamDisplayName", ATTeamDisplayName, "agentteam.team.display_name"},
		{"ATEventType", ATEventType, "agentteam.event_type"},
		{"ATAgentID", ATAgentID, "agentteam.agent.id"},
		{"ATMemberName", ATMemberName, "agentteam.member.name"},
		{"ATTaskID", ATTaskID, "agentteam.task.id"},
		{"ATMessageID", ATMessageID, "agentteam.message.id"},
	}
	for _, tt := range tests {
		if tt.got != tt.expected {
			t.Errorf("%s: 期望 %q，实际 %q", tt.name, tt.expected, tt.got)
		}
	}
}

func TestDeepAgent常量值与Python一致(t *testing.T) {
	if DATaskIteration != "deepagent.task.iteration" {
		t.Errorf("期望 deepagent.task.iteration，实际 %s", DATaskIteration)
	}
	if DATaskIsFollowUp != "deepagent.task.is_follow_up" {
		t.Errorf("期望 deepagent.task.is_follow_up，实际 %s", DATaskIsFollowUp)
	}
}

// TestSemconvV2键齐备 验证轨迹 schema v2 语义约定键全集与 Python 一致。
// Python: extensions/observability/semconv.py + gen_ai_semconv.py + tool_outcome.py:27
func TestSemconvV2键齐备(t *testing.T) {
	if TrajectorySpanSchemaVersion != "2" {
		t.Fatalf("schema version 漂移: %q", TrajectorySpanSchemaVersion)
	}
	keys := map[string]string{
		// openjiuwen.* 轨迹与关联（semconv.py:47-125）
		OJTraceRoot:                        "openjiuwen.trace.root",
		OJSpanInput:                        "openjiuwen.span.input",
		OJSpanOutput:                       "openjiuwen.span.output",
		OJRequestID:                        "openjiuwen.request.id",
		OJRequestMessageCount:              "openjiuwen.request.message_count",
		OJRunID:                            "openjiuwen.run.id",
		OJTurnID:                           "openjiuwen.turn.id",
		OJTurnNumber:                       "openjiuwen.turn.number",
		OJStepID:                           "openjiuwen.step.id",
		OJStepNumber:                       "openjiuwen.step.number",
		OJInferenceID:                      "openjiuwen.inference.id",
		OJInferenceInputObserved:           "openjiuwen.inference.input_observed",
		OJRequestNumber:                    "openjiuwen.request.number",
		OJRequestPurpose:                   "openjiuwen.request.purpose",
		OJTrajectoryRecordKind:             "openjiuwen.trajectory.record.kind",
		OJTrajectorySchemaVersion:          "openjiuwen.trajectory.schema_version",
		OJTrajectoryEventID:                "openjiuwen.trajectory.event_id",
		OJTrajectoryEventKind:              "openjiuwen.trajectory.event_kind",
		OJTrajectorySubjectID:              "openjiuwen.trajectory.subject_id",
		OJTrajectorySequenceEpoch:          "openjiuwen.trajectory.sequence_epoch",
		OJTrajectorySubjectSequence:        "openjiuwen.trajectory.subject_sequence",
		OJTrajectoryRecordedAtUnixNano:     "openjiuwen.trajectory.recorded_at_unix_nano",
		OJTrajectoryPayload:                "openjiuwen.trajectory.payload",
		OJAgentMode:                        "openjiuwen.agent.mode",
		OJExecutionSubjectID:               "openjiuwen.execution.subject.id",
		OJExecutionSubjectDisplayName:      "openjiuwen.execution.subject.display_name",
		OJExecutionSubjectKind:             "openjiuwen.execution.subject.kind",
		OJExecutionSubjectParentID:         "openjiuwen.execution.subject.parent_id",
		OJExecutionSubjectSessionID:        "openjiuwen.execution.subject.session_id",
		OJExecutionSubjectRequestNumber:    "openjiuwen.execution.subject.request.number",
		OJGenAIUsageTotalCost:              "openjiuwen.gen_ai.usage.total_cost",
		OJGenAIInputMessageProvenance:      "openjiuwen.gen_ai.input.message_provenance",
		OJGenAIResponseTotalLatencyMs:      "openjiuwen.gen_ai.response.total_latency_ms",
		OJToolAuthoritative:                "openjiuwen.tool.authoritative",
		ErrorType:                          "error.type",
		// 标准 GenAI 键（gen_ai_semconv.py）
		GenAIConversationID:                "gen_ai.conversation.id",
		GenAIOperationName:                 "gen_ai.operation.name",
		GenAIAgentName:                     "gen_ai.agent.name",
		GenAIProviderName:                  "gen_ai.provider.name",
		GenAIRequestTopK:                   "gen_ai.request.top_k",
		GenAIRequestSeed:                   "gen_ai.request.seed",
		GenAIRequestChoiceCount:            "gen_ai.request.choice.count",
		GenAIRequestPresencePenalty:        "gen_ai.request.presence_penalty",
		GenAIRequestFrequencyPenalty:       "gen_ai.request.frequency_penalty",
		GenAIRequestReasoningLevel:         "gen_ai.request.reasoning.level",
		GenAIRequestStream:                 "gen_ai.request.stream",
		GenAIRequestStopSequences:          "gen_ai.request.stop_sequences",
		GenAIResponseFinishReasons:         "gen_ai.response.finish_reasons",
		GenAIResponseID:                    "gen_ai.response.id",
		GenAIResponseTimeToFirstChunk:      "gen_ai.response.time_to_first_chunk",
		GenAIUsageInputTokens:              "gen_ai.usage.input_tokens",
		GenAIUsageOutputTokens:             "gen_ai.usage.output_tokens",
		GenAIUsageCacheReadInputTokens:     "gen_ai.usage.cache_read.input_tokens",
		GenAIUsageReasoningOutputTokens:    "gen_ai.usage.reasoning.output_tokens",
		GenAIToolCallArguments:             "gen_ai.tool.call.arguments",
		GenAIToolCallID:                    "gen_ai.tool.call.id",
		GenAIToolCallResult:                "gen_ai.tool.call.result",
		GenAIToolDefinitions:               "gen_ai.tool.definitions",
		GenAISystemInstructions:            "gen_ai.system_instructions",
		GenAIInputMessages:                 "gen_ai.input.messages",
		GenAIOutputMessages:                "gen_ai.output.messages",
	}
	for got, want := range keys {
		if got != want {
			t.Fatalf("semconv 键漂移: got %q want %q", got, want)
		}
	}
	if ToolReportedFailure != "ToolReportedFailure" {
		t.Fatalf("tool_outcome 常量漂移: %q", ToolReportedFailure)
	}
}

// TestTrajectory闭集 验证 record kind 与 event kind 闭集成员与 Python 一致。
// Python: semconv.py:225-236
func TestTrajectory闭集(t *testing.T) {
	wantKinds := []string{"turn", "step", "inference", "reasoning", "tool", "agent", "event"}
	if len(TrajectoryRecordKinds) != len(wantKinds) {
		t.Fatalf("record kind 闭集成员数漂移: %v", TrajectoryRecordKinds)
	}
	for _, kind := range wantKinds {
		if _, ok := TrajectoryRecordKinds[kind]; !ok {
			t.Fatalf("record kind 缺失: %q", kind)
		}
	}
	wantEvents := []string{"context.window.commit", "compaction.completed", "ask_user.requested", "ask_user.resolved"}
	if len(TrajectoryEventKinds) != len(wantEvents) {
		t.Fatalf("event kind 闭集成员数漂移: %v", TrajectoryEventKinds)
	}
	for _, kind := range wantEvents {
		if _, ok := TrajectoryEventKinds[kind]; !ok {
			t.Fatalf("event kind 缺失: %q", kind)
		}
	}
}
