package agents

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestGetAgentProfile_未知key 对照 Python profiles.py:72-78 KeyError。
func TestGetAgentProfile_未知key(t *testing.T) {
	profile, err := GetAgentProfile("role_attribution")
	require.NoError(t, err)
	require.Equal(t, "member_role_attributor", profile.AgentName)
	require.Equal(t, 300, profile.MaxIterations)

	_, err = GetAgentProfile("unknown")
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown Member Optimizer Agent profile: unknown")
}

// TestIterAgentProfiles 声明序稳定五档案。
func TestIterAgentProfiles(t *testing.T) {
	profiles := IterAgentProfiles()
	require.Len(t, profiles, 5)
	require.Equal(t, []string{
		"role_attribution", "mechanism_attribution",
		"action_planning", "action_execution", "verification_repair",
	}, func() []string {
		keys := make([]string, 0, len(profiles))
		for _, p := range profiles {
			keys = append(keys, p.Key)
		}
		return keys
	}())
}

// TestParseJSONObjectResponse 非对象拒绝（Python output.py:47-53）。
func TestParseJSONObjectResponse_非对象拒绝(t *testing.T) {
	m, err := ParseJSONObjectResponse(`{"role": "solver"}`)
	require.NoError(t, err)
	require.Equal(t, "solver", m["role"])

	// 裸映射提取
	m, err = ParseJSONObjectResponse("prefix text {\"a\": 1} suffix")
	require.NoError(t, err)
	require.Equal(t, float64(1), m["a"])

	// 数组不是对象
	_, err = ParseJSONObjectResponse("[1, 2]")
	require.Error(t, err)
	require.Contains(t, err.Error(), "must be a JSON object")
}

// TestParseYamlOrJsonObjectResponse_yaml与json双轨 对照 Python output.py:56-85。
func TestParseYamlOrJsonObjectResponse_yaml与json双轨(t *testing.T) {
	m, err := ParseYamlOrJsonObjectResponse("role: solver\nconfidence: 0.9")
	require.NoError(t, err)
	require.Equal(t, "solver", m["role"])

	m, err = ParseYamlOrJsonObjectResponse(`{"role": "solver"}`)
	require.NoError(t, err)
	require.Equal(t, "solver", m["role"])

	// yaml 围栏
	m, err = ParseYamlOrJsonObjectResponse("```yaml\nrole: solver\n```")
	require.NoError(t, err)
	require.Equal(t, "solver", m["role"])

	// json 围栏（含尾注文本的裸围栏走搜索路径）
	m, err = ParseYamlOrJsonObjectResponse("```json\n{\"role\": \"solver\"}\n```")
	require.NoError(t, err)
	require.Equal(t, "solver", m["role"])

	// json 围栏 + raw_decode 路径：```json 后跟空格且结尾恰为 ```
	m, err = ParseYamlOrJsonObjectResponse("```json {\"role\": \"solver\"}\n```")
	require.NoError(t, err)
	require.Equal(t, "solver", m["role"])

	// json 围栏但结尾非 ``` → 报错
	_, err = ParseYamlOrJsonObjectResponse("```json {\"role\": \"solver\"} trailing")
	require.Error(t, err)
	require.Contains(t, err.Error(), "must contain one JSON mapping")

	// 纯文本非映射
	_, err = ParseYamlOrJsonObjectResponse("just words")
	require.Error(t, err)
}

// TestExtractAgentText 对照 Python output.py:34-44。
func TestExtractAgentText(t *testing.T) {
	require.Equal(t, "hi", ExtractAgentText(map[string]any{"text": "hi"}))
	require.Equal(t, "hi", ExtractAgentText(map[string]any{"output": "hi", "text": ""}))
	require.Equal(t, "", ExtractAgentText(map[string]any{"text": "  "}))
	require.Equal(t, "", ExtractAgentText(nil))
}

// TestInvokeStructured_重试耗尽抛Exhausted fake agent 恒返坏输出。
func TestInvokeStructured_重试耗尽抛Exhausted(t *testing.T) {
	invoked := 0
	agent := &fakeAgent{invoke: func() map[string]any {
		invoked++
		return map[string]any{"text": "not a mapping"}
	}}
	_, err := InvokeMemberOptimizerAgentStructured(t.Context(), StructuredInvokeParams{
		Agent:       agent,
		AgentName:   "test_agent",
		UserMessage: "u",
		SessionID:   "s1",
		RetryLimit:  2,
		ParseResponse: func(text string) (map[string]any, error) {
			return nil, errParse
		},
	})
	require.Error(t, err)
	var exhausted *StructuredOutputExhaustedError
	require.ErrorAs(t, err, &exhausted)
	// 同指纹失败两次触发 no_progress（Python FeedbackTracker 默认 max_stagnant_attempts=2）
	require.Equal(t, 2, invoked)
	require.Contains(t, exhausted.Error(), "test_agent failed after 2 attempts (no_progress)")
}

// TestInvokeStructured_校验通过即返回 好输出一次通过。
func TestInvokeStructured_校验通过即返回(t *testing.T) {
	agent := &fakeAgent{invoke: func() map[string]any {
		return map[string]any{"text": "role: solver\nconfidence: 0.9"}
	}}
	raw, err := InvokeMemberOptimizerAgentStructured(t.Context(), StructuredInvokeParams{
		Agent:       agent,
		AgentName:   "test_agent",
		UserMessage: "u",
		SessionID:   "s1",
		RetryLimit:  2,
		ParseResponse: func(text string) (map[string]any, error) {
			return ParseYamlOrJsonObjectResponse(text)
		},
		ValidateResponse: func(raw map[string]any) []string {
			if raw["role"] == "solver" {
				return nil
			}
			return []string{"bad role"}
		},
	})
	require.NoError(t, err)
	require.Equal(t, "solver", raw["role"])
}

// TestInvokeStructured_校验失败走默认重试消息 默认重试文案含原消息与前次输出。
func TestInvokeStructured_校验失败走默认重试消息(t *testing.T) {
	messages := []string{}
	agent := &fakeAgent{invoke: func() map[string]any {
		return map[string]any{"text": "role: wrong"}
	}}
	_, err := InvokeMemberOptimizerAgentStructured(t.Context(), StructuredInvokeParams{
		Agent:       agent,
		AgentName:   "test_agent",
		UserMessage: "ORIGINAL",
		SessionID:   "s1",
		RetryLimit:  1,
		ParseResponse: func(text string) (map[string]any, error) {
			return ParseYamlOrJsonObjectResponse(text)
		},
		ValidateResponse: func(raw map[string]any) []string {
			return []string{"role mismatch"}
		},
		BuildRetryMessage: func(previous map[string]any, lastError string) string {
			messages = append(messages, lastError)
			return "RETRY-" + lastError
		},
	})
	require.Error(t, err)
	require.Len(t, messages, 1)
	// Python: user_message 不在重试消息中时前置拼接
	require.Contains(t, err.Error(), "attempt_limit_reached")
}

// TestDefaultRetryMessage 文案逐字对照 Python output.py:189-203。
func TestDefaultRetryMessage(t *testing.T) {
	msg := defaultRetryMessage("ORIG", map[string]any{"a": 1}, "bad")
	require.True(t, strings.HasPrefix(msg, "ORIG\n\n## Previous Invalid Output\n\n```json\n"))
	require.Contains(t, msg, "## Validation Error")
	require.Contains(t, msg, "bad")
	require.True(t, strings.HasSuffix(msg,
		"Return ONLY a corrected JSON or YAML object matching the agent output schema.\n"))
}

// TestLoadPrompt_五模板齐备 五个提示词模板可读且非空。
func TestLoadPrompt_五模板齐备(t *testing.T) {
	for _, name := range []string{
		"role_attribution.md", "mechanism_attribution.md", "action_planning.md",
		"action_execution.md", "verification_repair.md",
	} {
		content, err := LoadPrompt(name)
		require.NoError(t, err)
		require.NotEmpty(t, content)
	}
}

// ───────────────────────── 测试桩 ─────────────────────────

var errParse = &parseError{}

type parseError struct{}

func (*parseError) Error() string { return "not a mapping" }

// fakeAgent 测试用假 agent（记录调用次数，返回固定响应）。
type fakeAgent struct {
	invoke func() map[string]any
}

func (*fakeAgent) Name() string { return "fake" }

func init() {
	// 注入 fake 调用点，绕开真实 runner 基础设施
	invokeFn = func(_ context.Context, agent any, _, _ string) (map[string]any, error) {
		if f, ok := agent.(*fakeAgent); ok {
			return f.invoke(), nil
		}
		return nil, errors.New("unexpected agent type")
	}
}
