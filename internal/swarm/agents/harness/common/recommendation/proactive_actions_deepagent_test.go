package recommendation

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 测试替身 ────────────────────────────

// proactiveMockModelClient 测试用 LLM 客户端，按 invokeFn 返回固定文本。
// 与 skill_call 包的 mockBaseModelClient 同款（该替身为包内私有，无法跨包复用）。
type proactiveMockModelClient struct {
	invokeFn func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error)
}

// Invoke 调用模型。
func (m *proactiveMockModelClient) Invoke(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
	if m.invokeFn != nil {
		return m.invokeFn(ctx, messages, opts...)
	}
	return nil, fmt.Errorf("mock invoke not configured")
}

// Stream 流式调用（本包测试不用）。
func (m *proactiveMockModelClient) Stream(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.StreamOption) (<-chan *llmschema.AssistantMessageChunk, error) {
	return nil, fmt.Errorf("not implemented")
}

// GenerateImage 生图（未实现）。
func (m *proactiveMockModelClient) GenerateImage(ctx context.Context, messages []*llmschema.UserMessage, opts ...model_clients.GenerateImageOption) (*llmschema.ImageGenerationResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

// GenerateSpeech 语音合成（未实现）。
func (m *proactiveMockModelClient) GenerateSpeech(ctx context.Context, messages []*llmschema.UserMessage, opts ...model_clients.GenerateSpeechOption) (*llmschema.AudioGenerationResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

// GenerateVideo 生视频（未实现）。
func (m *proactiveMockModelClient) GenerateVideo(ctx context.Context, messages []*llmschema.UserMessage, opts ...model_clients.GenerateVideoOption) (*llmschema.VideoGenerationResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

// TranscribeAudio 语音转写（未实现）。
func (m *proactiveMockModelClient) TranscribeAudio(_ context.Context, _ string, _ ...llmschema.TranscribeAudioOption) (*llmschema.TranscriptionResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

// Release KV 缓存释放（不支持）。
func (m *proactiveMockModelClient) Release(_ context.Context, _ ...model_clients.ReleaseOption) (bool, error) {
	return false, nil
}

// SupportsKVCacheRelease 是否支持 KV 缓存释放。
func (m *proactiveMockModelClient) SupportsKVCacheRelease() bool { return false }

// SupportsKVCacheAffinity 是否支持 Ascend KV cache 亲和协议。
func (m *proactiveMockModelClient) SupportsKVCacheAffinity() bool { return false }

// BuildKVCacheAffinityInvokeKwargs 构造 agent_hint 亲和参数（本包测试不涉及亲和）。
func (m *proactiveMockModelClient) BuildKVCacheAffinityInvokeKwargs(_ any, _, _ string, _ int) map[string]any {
	return nil
}

var _ model_clients.BaseModelClient = (*proactiveMockModelClient)(nil)

// ──────────────────────────── 测试辅助 ────────────────────────────

// newProactiveMockModel 构造注入了 mock 客户端的真 *llm.Model。
// 手法与 skill_call 包的 newMockSkillModel 一致：注册动态命名的 provider。
func newProactiveMockModel(t *testing.T, invokeFn func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error)) *llm.Model {
	t.Helper()

	mockClient := &proactiveMockModelClient{invokeFn: invokeFn}
	providerName := fmt.Sprintf("mock_proactive_%d", time.Now().UnixNano())
	model_clients.GetClientRegistry().Register(providerName, "llm",
		func(_ *llmschema.ModelRequestConfig, _ *llmschema.ModelClientConfig) (model_clients.BaseModelClient, error) {
			return mockClient, nil
		})

	clientConfig := &llmschema.ModelClientConfig{
		ClientProvider: providerName,
		ClientID:       providerName + "_id",
	}
	modelConfig := &llmschema.ModelRequestConfig{ModelName: "test-model"}

	model, err := llm.NewModel(clientConfig, modelConfig)
	require.NoError(t, err, "创建 mock model 失败")
	return model
}

// newProactiveAgentWithReply 构造返回固定文本的真 *DeepAgent。
//
// 严格复刻 BuildProactiveAgent 的形态：无 tools、无 rails、关闭 task loop、
// max_iterations=1、不加通用子 agent。
func newProactiveAgentWithReply(t *testing.T, reply string) *harness.DeepAgent {
	t.Helper()

	model := newProactiveMockModel(t, func(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		return llmschema.NewAssistantMessage(reply), nil
	})

	agent, err := harness.CreateDeepAgent(context.Background(), harness_config.CreateDeepAgentParams{
		Model:                  model,
		Card:                   agentschema.NewAgentCard(agentschema.WithAgentName("proactive_agent")),
		SystemPrompt:           "你是用户洞察与推荐助手。严格输出 JSON 对象。",
		EnableTaskLoop:         false,
		MaxIterations:          1,
		AddGeneralPurposeAgent: false,
	})
	require.NoError(t, err, "创建 DeepAgent 失败")
	t.Cleanup(func() { _ = agent.Stop(context.Background()) })
	return agent
}

// ──────────────────────────── AnalyzeAndDecide（真 DeepAgent）────────────────────────────

// TestAnalyzeAndDecide_正常决策 模型返回合法 JSON，解析出决策。
// Python: proactive_actions.py:287 _analyze_and_decide
func TestAnalyzeAndDecide_正常决策(t *testing.T) {
	reply := `{"decision": {"type": "task_reminder", "target": "整理Q4报表", "reason": "用户说过回头整理", "urgency": 0.8}}`
	agent := newProactiveAgentWithReply(t, reply)

	result := AnalyzeAndDecide(context.Background(), "## 情境报告", nil, agent, "（暂无）", "zh")
	require.NotNil(t, result)
	require.NotNil(t, result.Decision, "应解析出决策")
	assert.Equal(t, "task_reminder", result.Decision.Type)
	assert.Equal(t, "整理Q4报表", result.Decision.Target)
	assert.Equal(t, "用户说过回头整理", result.Decision.Reason)
	assert.Equal(t, 0.8, result.Decision.Urgency)
}

// TestAnalyzeAndDecide_决策为null 模型明确返回 null，结果无决策。
func TestAnalyzeAndDecide_决策为null(t *testing.T) {
	agent := newProactiveAgentWithReply(t, `{"decision": null}`)

	result := AnalyzeAndDecide(context.Background(), "报告", nil, agent, "（暂无）", "zh")
	require.NotNil(t, result)
	assert.Nil(t, result.Decision)
}

// TestAnalyzeAndDecide_模型返回非JSON 无法解析时无决策，不 panic。
func TestAnalyzeAndDecide_模型返回非JSON(t *testing.T) {
	agent := newProactiveAgentWithReply(t, "我不知道该怎么推荐")

	result := AnalyzeAndDecide(context.Background(), "报告", nil, agent, "（暂无）", "zh")
	require.NotNil(t, result)
	assert.Nil(t, result.Decision)
}

// TestAnalyzeAndDecide_JSON代码块包裹 模型把 JSON 包在 markdown 代码块里也能解析。
func TestAnalyzeAndDecide_JSON代码块包裹(t *testing.T) {
	reply := "好的，我的分析如下：\n```json\n{\"decision\": {\"type\": \"need_exploration\", \"target\": \"世界杯赛制\", \"reason\": \"从聊天延伸\", \"urgency\": 0.4}}\n```\n以上。"
	agent := newProactiveAgentWithReply(t, reply)

	result := AnalyzeAndDecide(context.Background(), "报告", nil, agent, "（暂无）", "zh")
	require.NotNil(t, result.Decision)
	assert.Equal(t, "need_exploration", result.Decision.Type)
	assert.Equal(t, "世界杯赛制", result.Decision.Target)
}

// TestAnalyzeAndDecide_skill推荐校验target 推荐不存在的 skill 时被拒绝。
// Python: proactive_actions.py:353-360
func TestAnalyzeAndDecide_skill推荐校验target(t *testing.T) {
	t.Run("target存在则接受", func(t *testing.T) {
		reply := `{"decision": {"type": "skill_recommend", "target": "翻译技能", "reason": "能帮上忙", "urgency": 0.6}}`
		agent := newProactiveAgentWithReply(t, reply)
		skills := []map[string]any{{"name": "翻译技能"}, {"name": "写作技能"}}

		result := AnalyzeAndDecide(context.Background(), "报告", skills, agent, "（暂无）", "zh")
		require.NotNil(t, result.Decision)
		assert.Equal(t, "翻译技能", result.Decision.Target)
	})

	t.Run("target不存在则拒绝", func(t *testing.T) {
		reply := `{"decision": {"type": "skill_recommend", "target": "已卸载的技能", "reason": "历史的", "urgency": 0.6}}`
		agent := newProactiveAgentWithReply(t, reply)
		skills := []map[string]any{{"name": "翻译技能"}}

		result := AnalyzeAndDecide(context.Background(), "报告", skills, agent, "（暂无）", "zh")
		assert.Nil(t, result.Decision, "推荐不存在的 skill 应被拒绝")
	})

	t.Run("非skill类型不校验target", func(t *testing.T) {
		reply := `{"decision": {"type": "task_reminder", "target": "不存在的待办", "reason": "原因", "urgency": 0.5}}`
		agent := newProactiveAgentWithReply(t, reply)

		result := AnalyzeAndDecide(context.Background(), "报告", nil, agent, "（暂无）", "zh")
		require.NotNil(t, result.Decision, "task_reminder 不做 skill 校验")
	})
}

// TestAnalyzeAndDecide_缺type或target 任一缺失都不构成决策。
func TestAnalyzeAndDecide_缺type或target(t *testing.T) {
	for _, reply := range []string{
		`{"decision": {"target": "只有target"}}`,
		`{"decision": {"type": "task_reminder"}}`,
		`{"decision": {}}`,
	} {
		agent := newProactiveAgentWithReply(t, reply)
		result := AnalyzeAndDecide(context.Background(), "报告", nil, agent, "（暂无）", "zh")
		assert.Nil(t, result.Decision, "reply=%s 不应产生决策", reply)
	}
}

// TestAnalyzeAndDecide_urgency异常值回落 模型给字符串或超界 urgency，回落/夹取。
func TestAnalyzeAndDecide_urgency异常值回落(t *testing.T) {
	cases := []struct {
		urgency string
		want    float64
	}{
		{`"0.9"`, 0.9},
		{`1.8`, 1.0},
		{`-0.5`, 0.5},
		{`"abc"`, 0.5},
	}
	for _, tc := range cases {
		reply := fmt.Sprintf(`{"decision": {"type": "task_reminder", "target": "X", "reason": "R", "urgency": %s}}`, tc.urgency)
		agent := newProactiveAgentWithReply(t, reply)
		result := AnalyzeAndDecide(context.Background(), "报告", nil, agent, "（暂无）", "zh")
		require.NotNil(t, result.Decision, "urgency=%s", tc.urgency)
		assert.Equal(t, tc.want, result.Decision.Urgency, "urgency=%s", tc.urgency)
	}
}

// TestAnalyzeAndDecide_每次传给模型的prompt含渲染后的占位符 真 agent 收到的是渲染后文本。
func TestAnalyzeAndDecide_prompt已渲染(t *testing.T) {
	var captured string
	model := newProactiveMockModel(t, func(_ context.Context, messages model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		captured = messagesToText(messages)
		return llmschema.NewAssistantMessage(`{"decision": null}`), nil
	})
	agent, err := harness.CreateDeepAgent(context.Background(), harness_config.CreateDeepAgentParams{
		Model:                  model,
		Card:                   agentschema.NewAgentCard(agentschema.WithAgentName("proactive_agent")),
		SystemPrompt:           "严格输出 JSON 对象。",
		EnableTaskLoop:         false,
		MaxIterations:          1,
		AddGeneralPurposeAgent: false,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = agent.Stop(context.Background()) })

	AnalyzeAndDecide(context.Background(), "SITUATION_MARKER", nil, agent, "RULES_MARKER", "zh")

	require.NotEmpty(t, captured, "模型应收到 prompt")
	assert.Contains(t, captured, "SITUATION_MARKER", "情境报告应被渲染进 prompt")
	assert.Contains(t, captured, "RULES_MARKER", "决策规则应被渲染进 prompt")
	assert.NotContains(t, captured, "{conversation_summary}", "占位符应已被替换")
	assert.NotContains(t, captured, "{decision_rules_text}")
}

// messagesToText 把 messages 参数拼成纯文本，供断言 prompt 内容。
func messagesToText(messages model_clients.MessagesParam) string {
	if messages.IsText() {
		return messages.Text()
	}
	if messages.IsMessages() {
		parts := make([]string, 0, len(messages.Messages()))
		for _, msg := range messages.Messages() {
			if msg == nil {
				continue
			}
			parts = append(parts, fmt.Sprintf("%v", msg.GetContent()))
		}
		return strings.Join(parts, "\n")
	}
	var parts []string
	for _, d := range messages.Dicts() {
		parts = append(parts, fmt.Sprintf("%v", d))
	}
	return strings.Join(parts, "\n")
}

// ──────────────────────────── TriggerMainAgent 测试分组 ────────────────────────────

// TestTriggerMainAgent_构造指令并透传 指令由 directive prompt 渲染后填入 request.Query。
// Python: proactive_actions.py:398
func TestTriggerMainAgent_构造指令并透传(t *testing.T) {
	var captured *ProactiveTriggerRequest
	callback := func(_ context.Context, req *ProactiveTriggerRequest) (bool, error) {
		captured = req
		return true, nil
	}
	request := &ProactiveTriggerRequest{
		SessionID: "sess-1",
		Decision: &RecommendationDecision{
			Type: "skill_recommend", Target: "翻译技能", Reason: "能翻文档", Urgency: 0.7,
		},
		StyleRulesSection: "【话术风格要求】简洁",
	}

	got := TriggerMainAgent(context.Background(), callback, request)
	assert.True(t, got)
	require.NotNil(t, captured)
	assert.Contains(t, captured.Query, "翻译技能", "指令应含 target")
	assert.Contains(t, captured.Query, "能翻文档", "指令应含 reason")
	assert.Contains(t, captured.Query, "skill_recommend", "指令应含推荐类型")
	assert.Contains(t, captured.Query, "【话术风格要求】简洁", "指令应含话术风格段")
	assert.NotContains(t, captured.Query, "{rec_type}", "占位符应已渲染")
}

// TestTriggerMainAgent_回调返回false 表示 session 忙，本函数返回 false。
func TestTriggerMainAgent_回调返回false(t *testing.T) {
	callback := func(_ context.Context, _ *ProactiveTriggerRequest) (bool, error) {
		return false, nil
	}
	request := &ProactiveTriggerRequest{
		Decision: &RecommendationDecision{Type: "task_reminder", Target: "X"},
	}
	assert.False(t, TriggerMainAgent(context.Background(), callback, request))
}

// TestTriggerMainAgent_回调报错 异常被吞并返回 false（Python 侧 try/except）。
func TestTriggerMainAgent_回调报错(t *testing.T) {
	callback := func(_ context.Context, _ *ProactiveTriggerRequest) (bool, error) {
		return false, fmt.Errorf("session busy")
	}
	request := &ProactiveTriggerRequest{
		Decision: &RecommendationDecision{Type: "task_reminder", Target: "X"},
	}
	assert.False(t, TriggerMainAgent(context.Background(), callback, request))
}

// TestTriggerMainAgent_边界情形 nil 回调 / nil 请求 / nil 决策一律 false。
func TestTriggerMainAgent_边界情形(t *testing.T) {
	callback := func(_ context.Context, _ *ProactiveTriggerRequest) (bool, error) { return true, nil }

	assert.False(t, TriggerMainAgent(context.Background(), nil, &ProactiveTriggerRequest{}))
	assert.False(t, TriggerMainAgent(context.Background(), callback, nil))
	assert.False(t, TriggerMainAgent(context.Background(), callback, &ProactiveTriggerRequest{Decision: nil}))
}
