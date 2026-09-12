package sharing

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/evolving/checkpointing"
	"github.com/uapclaw/uapclaw-go/internal/evolving/optimizer/llm_resilience"
	"github.com/uapclaw/uapclaw-go/internal/evolving/signal"
)

// ──────────────────────────── 结构体 ────────────────────────────

// mockBaseModelClient 用于测试的模拟 LLM 客户端
type mockBaseModelClient struct {
	invokeFn func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error)
}

func (m *mockBaseModelClient) Invoke(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
	if m.invokeFn != nil {
		return m.invokeFn(ctx, messages, opts...)
	}
	return nil, fmt.Errorf("mock invoke not configured")
}
func (m *mockBaseModelClient) Stream(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.StreamOption) (<-chan *llmschema.AssistantMessageChunk, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockBaseModelClient) GenerateImage(ctx context.Context, messages []*llmschema.UserMessage, opts ...model_clients.GenerateImageOption) (*llmschema.ImageGenerationResponse, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockBaseModelClient) GenerateSpeech(ctx context.Context, messages []*llmschema.UserMessage, opts ...model_clients.GenerateSpeechOption) (*llmschema.AudioGenerationResponse, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockBaseModelClient) GenerateVideo(ctx context.Context, messages []*llmschema.UserMessage, opts ...model_clients.GenerateVideoOption) (*llmschema.VideoGenerationResponse, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockBaseModelClient) TranscribeAudio(_ context.Context, _ string, _ ...llmschema.TranscribeAudioOption) (*llmschema.TranscriptionResponse, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockBaseModelClient) Release(ctx context.Context, opts ...model_clients.ReleaseOption) (bool, error) {
	return false, nil
}
func (m *mockBaseModelClient) SupportsKVCacheRelease() bool { return false }

var _ model_clients.BaseModelClient = (*mockBaseModelClient)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestParseFromOptimizerOutput_FromPatch 从 EvolutionPatch 提取
func TestParseFromOptimizerOutput_FromPatch(t *testing.T) {
	summary := "这是一个测试摘要"
	patch := checkpointing.EvolutionPatch{
		Section:   "Instructions",
		Action:    "append",
		Content:   "测试内容",
		Target:    signal.EvolutionTargetBody,
		Keywords:  []string{"IndexError", "索引错误", "Python"},
		Summary:   &summary,
	}

	keywords, summary := ParseFromOptimizerOutput(patch)
	assert.Equal(t, []string{"IndexError", "索引错误", "Python"}, keywords)
	assert.Equal(t, "这是一个测试摘要", summary)
}

// TestParseFromOptimizerOutput_FromPatchPtr 从 *EvolutionPatch 提取
func TestParseFromOptimizerOutput_FromPatchPtr(t *testing.T) {
	summary := "指针摘要"
	patch := &checkpointing.EvolutionPatch{
		Section:   "Instructions",
		Action:    "append",
		Content:   "内容",
		Target:    signal.EvolutionTargetBody,
		Keywords:  []string{"TypeError"},
		Summary:   &summary,
	}

	keywords, s := ParseFromOptimizerOutput(patch)
	assert.Equal(t, []string{"TypeError"}, keywords)
	assert.Equal(t, "指针摘要", s)
}

// TestParseFromOptimizerOutput_FromDict 从 map[string]any 提取
func TestParseFromOptimizerOutput_FromDict(t *testing.T) {
	rawPatch := map[string]any{
		"keywords": []any{"IndexError", "列表越界"},
		"summary":  "运行时索引错误",
	}

	keywords, summary := ParseFromOptimizerOutput(rawPatch)
	assert.Equal(t, []string{"IndexError", "列表越界"}, keywords)
	assert.Equal(t, "运行时索引错误", summary)
}

// TestParseFromOptimizerOutput_EmptyPatch 空 patch 返回空
func TestParseFromOptimizerOutput_EmptyPatch(t *testing.T) {
	patch := checkpointing.EvolutionPatch{
		Section: "Instructions",
		Action:  "skip",
		Content: "",
		Target:  signal.EvolutionTargetBody,
	}

	keywords, summary := ParseFromOptimizerOutput(patch)
	assert.Empty(t, keywords)
	assert.Empty(t, summary)
}

// TestParseFromOptimizerOutput_UnknownType 未知类型返回空
func TestParseFromOptimizerOutput_UnknownType(t *testing.T) {
	keywords, summary := ParseFromOptimizerOutput("invalid type")
	assert.Empty(t, keywords)
	assert.Empty(t, summary)
}

// TestParseFromOptimizerOutput_NilPatchPtr nil *EvolutionPatch 返回空
func TestParseFromOptimizerOutput_NilPatchPtr(t *testing.T) {
	var patch *checkpointing.EvolutionPatch = nil
	keywords, summary := ParseFromOptimizerOutput(patch)
	assert.Empty(t, keywords)
	assert.Empty(t, summary)
}

// TestParseFromOptimizerOutput_DictKeywordsNotList dict 中 keywords 非 list 时返回空
func TestParseFromOptimizerOutput_DictKeywordsNotList(t *testing.T) {
	rawPatch := map[string]any{
		"keywords": "not a list",
		"summary":  123, // 非字符串
	}

	keywords, summary := ParseFromOptimizerOutput(rawPatch)
	assert.Empty(t, keywords)
	assert.Empty(t, summary)
}

// TestExtractQueryKeywords_NoLLM LLM 为 nil 返回空关键词（intent=excerpt[:40]）
func TestExtractQueryKeywords_NoLLM(t *testing.T) {
	ext := NewKeywordExtractor(nil, "", "cn", llm_resilience.LLMInvokePolicy{})

	result := ext.ExtractQueryKeywords(context.Background(), "这是一段对话摘录内容，用于测试关键词提取")
	assert.Empty(t, result.Keywords)
	// 原文 20 个中文字符，不超过 40，intent 应等于原文
	assert.Equal(t, "这是一段对话摘录内容，用于测试关键词提取", result.Intent)
	assert.Equal(t, "这是一段对话摘录内容，用于测试关键词提取", result.RawExcerpt)
}

// TestExtractQueryKeywords_NoModel model 为空返回空关键词
func TestExtractQueryKeywords_NoModel(t *testing.T) {
	model := newMockModel(t, nil)
	ext := NewKeywordExtractor(model, "", "cn", llm_resilience.LLMInvokePolicy{})

	result := ext.ExtractQueryKeywords(context.Background(), "some excerpt")
	assert.Empty(t, result.Keywords)
	assert.Equal(t, "some excerpt", result.Intent)
}

// TestExtractQueryKeywords_EmptyExcerpt 空摘录返回空
func TestExtractQueryKeywords_EmptyExcerpt(t *testing.T) {
	ext := NewKeywordExtractor(nil, "", "cn", llm_resilience.LLMInvokePolicy{})

	result := ext.ExtractQueryKeywords(context.Background(), "")
	assert.Empty(t, result.Keywords)
	assert.Empty(t, result.Intent)
	assert.Empty(t, result.RawExcerpt)
}

// TestExtractQueryKeywords_正常调用 LLM 正常返回关键词
func TestExtractQueryKeywords_正常调用(t *testing.T) {
	model := newMockModel(t, func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		return llmschema.NewAssistantMessage(`{"keywords": ["IndexError", "list index out of range", "Python列表越界"], "intent": "Python列表索引越界错误"}`), nil
	})
	policy := llm_resilience.LLMInvokePolicy{
		AttemptTimeoutSecs: 5,
		TotalBudgetSecs:    10,
		MaxAttempts:        1,
	}
	ext := NewKeywordExtractor(model, "test-model", "cn", policy)

	result := ext.ExtractQueryKeywords(context.Background(), "用户遇到了 IndexError: list index out of range 错误")
	assert.Equal(t, []string{"IndexError", "list index out of range", "Python列表越界"}, result.Keywords)
	assert.Equal(t, "Python列表索引越界错误", result.Intent)
	assert.Equal(t, "用户遇到了 IndexError: list index out of range 错误", result.RawExcerpt)
}

// TestExtractQueryKeywords_LLM失败 LLM 调用失败返回空关键词
func TestExtractQueryKeywords_LLM失败(t *testing.T) {
	model := newMockModel(t, func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		return nil, fmt.Errorf("LLM service unavailable")
	})
	policy := llm_resilience.LLMInvokePolicy{
		AttemptTimeoutSecs: 1,
		TotalBudgetSecs:    2,
		MaxAttempts:        1,
	}
	ext := NewKeywordExtractor(model, "test-model", "cn", policy)

	excerpt := "测试摘录内容"
	result := ext.ExtractQueryKeywords(context.Background(), excerpt)
	assert.Empty(t, result.Keywords)
	assert.Equal(t, truncateString(excerpt, 40), result.Intent)
}

// TestExtractQueryKeywords_英文语言 英文提示词路径
func TestExtractQueryKeywords_英文语言(t *testing.T) {
	model := newMockModel(t, func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		return llmschema.NewAssistantMessage(`{"keywords": ["KeyError", "dict access"], "intent": "dict key missing"}`), nil
	})
	policy := llm_resilience.LLMInvokePolicy{
		AttemptTimeoutSecs: 5,
		TotalBudgetSecs:    10,
		MaxAttempts:        1,
	}
	ext := NewKeywordExtractor(model, "test-model", "en", policy)

	result := ext.ExtractQueryKeywords(context.Background(), "KeyError on dict access", "some hint")
	assert.Equal(t, []string{"KeyError", "dict access"}, result.Keywords)
	assert.Equal(t, "dict key missing", result.Intent)
}

// TestExtractQueryKeywords_WithSkillHint 提供 skill_hint
func TestExtractQueryKeywords_WithSkillHint(t *testing.T) {
	model := newMockModel(t, func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		return llmschema.NewAssistantMessage(`{"keywords": ["timeout"], "intent": "network timeout"}`), nil
	})
	policy := llm_resilience.LLMInvokePolicy{
		AttemptTimeoutSecs: 5,
		TotalBudgetSecs:    10,
		MaxAttempts:        1,
	}
	ext := NewKeywordExtractor(model, "test-model", "cn", policy)

	result := ext.ExtractQueryKeywords(context.Background(), "request timed out", "这是技能提示")
	assert.Equal(t, []string{"timeout"}, result.Keywords)
}

// TestExtractQueryKeywords_无SkillHint 未提供 skill_hint 时使用默认值
func TestExtractQueryKeywords_无SkillHint(t *testing.T) {
	model := newMockModel(t, func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		return llmschema.NewAssistantMessage(`{"keywords": ["error"], "intent": "some error"}`), nil
	})
	policy := llm_resilience.LLMInvokePolicy{
		AttemptTimeoutSecs: 5,
		TotalBudgetSecs:    10,
		MaxAttempts:        1,
	}
	ext := NewKeywordExtractor(model, "test-model", "cn", policy)

	result := ext.ExtractQueryKeywords(context.Background(), "some error occurred")
	assert.Equal(t, []string{"error"}, result.Keywords)
}

// TestExtractQueryKeywords_关键词超限 超过 20 个关键词时截断
func TestExtractQueryKeywords_关键词超限(t *testing.T) {
	// 构建 25 个关键词
	kws := make([]string, 25)
	for i := 0; i < 25; i++ {
		kws[i] = fmt.Sprintf("keyword_%d", i)
	}
	kwsJSON := `{"keywords": [`
	for i, k := range kws {
		if i > 0 {
			kwsJSON += ", "
		}
		kwsJSON += fmt.Sprintf(`"%s"`, k)
	}
	kwsJSON += `], "intent": "too many keywords"}`

	model := newMockModel(t, func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		return llmschema.NewAssistantMessage(kwsJSON), nil
	})
	policy := llm_resilience.LLMInvokePolicy{
		AttemptTimeoutSecs: 5,
		TotalBudgetSecs:    10,
		MaxAttempts:        1,
	}
	ext := NewKeywordExtractor(model, "test-model", "cn", policy)

	result := ext.ExtractQueryKeywords(context.Background(), "test excerpt")
	assert.Equal(t, 20, len(result.Keywords))
}

// TestExtractQueryKeywords_Intent超限 intent 超过 80 个字符（rune）时截断
func TestExtractQueryKeywords_Intent超限(t *testing.T) {
	// 构造超过 80 个 rune 的 intent
	longIntent := strings.Repeat("A", 100)

	model := newMockModel(t, func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		return llmschema.NewAssistantMessage(fmt.Sprintf(`{"keywords": ["k1"], "intent": "%s"}`, longIntent)), nil
	})
	policy := llm_resilience.LLMInvokePolicy{
		AttemptTimeoutSecs: 5,
		TotalBudgetSecs:    10,
		MaxAttempts:        1,
	}
	ext := NewKeywordExtractor(model, "test-model", "cn", policy)

	result := ext.ExtractQueryKeywords(context.Background(), "test excerpt")
	assert.True(t, len([]rune(result.Intent)) <= 80, "intent rune 长度应 <= 80")
}

// TestExtractQueryKeywords_LLM返回非JSON LLM 返回非 JSON 时回退到空关键词
func TestExtractQueryKeywords_LLM返回非JSON(t *testing.T) {
	model := newMockModel(t, func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		return llmschema.NewAssistantMessage("this is not json at all"), nil
	})
	policy := llm_resilience.LLMInvokePolicy{
		AttemptTimeoutSecs: 5,
		TotalBudgetSecs:    10,
		MaxAttempts:        1,
	}
	ext := NewKeywordExtractor(model, "test-model", "cn", policy)

	excerpt := "测试摘录"
	result := ext.ExtractQueryKeywords(context.Background(), excerpt)
	assert.Empty(t, result.Keywords)
	assert.Equal(t, truncateString(excerpt, 40), result.Intent)
}

// TestExtractQueryJSON_ValidJSON 直接 JSON 解析
func TestExtractQueryJSON_ValidJSON(t *testing.T) {
	raw := `{"keywords": ["k1", "k2"], "intent": "test"}`
	result := extractQueryJSON(raw)
	assert.NotNil(t, result)
	assert.Equal(t, "k1", result["keywords"].([]any)[0])
	assert.Equal(t, "test", result["intent"])
}

// TestExtractQueryJSON_MarkdownWrapped Markdown 代码块包裹的 JSON
func TestExtractQueryJSON_MarkdownWrapped(t *testing.T) {
	raw := "```json\n{\"keywords\": [\"k1\"], \"intent\": \"wrapped\"}\n```"
	result := extractQueryJSON(raw)
	assert.NotNil(t, result)
	assert.Equal(t, "k1", result["keywords"].([]any)[0])
	assert.Equal(t, "wrapped", result["intent"])
}

// TestExtractQueryJSON_InvalidJSON 无效 JSON 返回 nil
func TestExtractQueryJSON_InvalidJSON(t *testing.T) {
	result := extractQueryJSON("not json at all, no braces")
	assert.Nil(t, result)
}

// TestExtractQueryJSON_EmptyString 空字符串返回 nil
func TestExtractQueryJSON_EmptyString(t *testing.T) {
	result := extractQueryJSON("")
	assert.Nil(t, result)
}

// TestExtractQueryJSON_BracesButInvalidJSON 有花括号但不是有效 JSON
func TestExtractQueryJSON_BracesButInvalidJSON(t *testing.T) {
	result := extractQueryJSON("{not valid json content here}")
	assert.Nil(t, result)
}

// TestUpdateLLM 后置绑定 LLM
func TestUpdateLLM(t *testing.T) {
	model1 := newMockModel(t, nil)
	model2 := newMockModel(t, nil)
	ext := NewKeywordExtractor(model1, "model1", "cn", llm_resilience.LLMInvokePolicy{})

	assert.Equal(t, model1, ext.llm)
	assert.Equal(t, "model1", ext.model)

	ext.UpdateLLM(model2, "model2")
	assert.Equal(t, model2, ext.llm)
	assert.Equal(t, "model2", ext.model)
}

// TestNewKeywordExtractor_默认语言 非法语言默认 "cn"
func TestNewKeywordExtractor_默认语言(t *testing.T) {
	ext := NewKeywordExtractor(nil, "", "fr", llm_resilience.LLMInvokePolicy{})
	assert.Equal(t, "cn", ext.language)
}

// TestNewKeywordExtractor_默认策略 零值策略使用 QUERY_KEYWORDS_LLM_POLICY
func TestNewKeywordExtractor_默认策略(t *testing.T) {
	ext := NewKeywordExtractor(nil, "", "cn", llm_resilience.LLMInvokePolicy{})
	assert.Equal(t, QUERY_KEYWORDS_LLM_POLICY, ext.policy)
}

// TestTruncateString 截断字符串
func TestTruncateString(t *testing.T) {
	assert.Equal(t, "abc", truncateString("abc", 5))
	assert.Equal(t, "abcde", truncateString("abcdefgh", 5))
	assert.Equal(t, "", truncateString("", 5))
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newMockModel 创建一个使用 mock client 的 Model 实例
func newMockModel(t *testing.T, invokeFn func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error)) *llm.Model {
	t.Helper()

	mockClient := &mockBaseModelClient{invokeFn: invokeFn}
	providerName := fmt.Sprintf("mock_sharing_keyword_%d", time.Now().UnixNano())
	model_clients.GetClientRegistry().Register(providerName, "llm", func(modelConfig *llmschema.ModelRequestConfig, clientConfig *llmschema.ModelClientConfig) (model_clients.BaseModelClient, error) {
		return mockClient, nil
	})

	clientConfig := &llmschema.ModelClientConfig{
		ClientProvider: providerName,
		ClientID:       providerName + "_id",
	}
	modelConfig := &llmschema.ModelRequestConfig{
		ModelName: "test-model",
	}

	model, err := llm.NewModel(clientConfig, modelConfig)
	if err != nil {
		t.Fatalf("创建 mock model 失败: %v", err)
	}
	return model
}
