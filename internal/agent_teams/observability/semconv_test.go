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
