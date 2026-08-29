package skill_call

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
	"github.com/uapclaw/uapclaw-go/internal/agentcore/operator"
	"github.com/uapclaw/uapclaw-go/internal/evolving/checkpointing"
	"github.com/uapclaw/uapclaw-go/internal/evolving/experience"
	"github.com/uapclaw/uapclaw-go/internal/evolving/optimizer/llm_resilience"
	"github.com/uapclaw/uapclaw-go/internal/evolving/schema"
	"github.com/uapclaw/uapclaw-go/internal/evolving/signal"
	"github.com/uapclaw/uapclaw-go/internal/evolving/trajectory"
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

// TestSkillExperienceOptimizer_Bind 绑定 operators 并提取 online_contexts
func TestSkillExperienceOptimizer_Bind(t *testing.T) {
	model := newMockSkillModel(t, func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		return llmschema.NewAssistantMessage("ok"), nil
	})
	opt := NewSkillExperienceOptimizer(model, "test-model", "cn", GenerateRecordsLLMPolicy)

	skillName := "test_skill"
	evoCtx := &experience.EvolutionContext{
		SkillName:    skillName,
		SkillContent: "skill content",
		Signals:      []signal.EvolutionSignal{{SignalType: "execution_failure"}},
	}

	operators := map[string]operator.Operator{}
	config := map[string]any{
		"online_contexts": map[string]any{
			skillName: evoCtx,
		},
	}

	count := opt.Bind(operators, []string{"experiences"}, config)
	assert.Equal(t, 0, count) // 无 operator 匹配
	assert.NotNil(t, opt.onlineContexts[skillName])

	// 验证 nil config
	opt2 := NewSkillExperienceOptimizer(model, "test-model", "cn", GenerateRecordsLLMPolicy)
	count2 := opt2.Bind(operators, nil, nil)
	assert.Equal(t, 0, count2)
	assert.Empty(t, opt2.onlineContexts)
}

// TestSkillExperienceOptimizer_Bind_无config 空配置绑定
func TestSkillExperienceOptimizer_Bind_无config(t *testing.T) {
	model := newMockSkillModel(t, nil)
	opt := NewSkillExperienceOptimizer(model, "test-model", "cn", GenerateRecordsLLMPolicy)
	operators := map[string]operator.Operator{}

	count := opt.Bind(operators, nil, map[string]any{})
	assert.Equal(t, 0, count)
	assert.Empty(t, opt.onlineContexts)
}

// TestSkillExperienceOptimizer_AddTrajectory AddTrajectory 不做任何处理
func TestSkillExperienceOptimizer_AddTrajectory(t *testing.T) {
	opt := NewSkillExperienceOptimizer(nil, "test-model", "cn", GenerateRecordsLLMPolicy)
	opt.AddTrajectory(nil) // 不应 panic
}

// TestSkillExperienceOptimizer_SelectSignals 默认保留所有信号
func TestSkillExperienceOptimizer_SelectSignals(t *testing.T) {
	opt := NewSkillExperienceOptimizer(nil, "test-model", "cn", GenerateRecordsLLMPolicy)
	sigs := []*signal.EvolutionSignal{
		{SignalType: "execution_failure"},
		{SignalType: "user_correction"},
	}
	selected := opt.SelectSignals(sigs)
	assert.Equal(t, len(sigs), len(selected))
}

// TestSkillExperienceOptimizer_Backward_正常 反向传播正常流程
func TestSkillExperienceOptimizer_Backward_正常(t *testing.T) {
	// LLM 返回有效的 JSON 草稿数组
	model := newMockSkillModel(t, func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		return llmschema.NewAssistantMessage(`[{"action":"append","section":"Troubleshooting","target":"body","content":"新增排查步骤"}]`), nil
	})
	opt := NewSkillExperienceOptimizer(model, "test-model", "cn", GenerateRecordsLLMPolicy)

	skillName := "my_skill"
	skillNamePtr := skillName
	evoCtx := &experience.EvolutionContext{
		SkillName:    skillName,
		SkillContent: "# My Skill\n\n## Instructions\n\nDo something useful",
		Signals:      []signal.EvolutionSignal{{SignalType: "execution_failure", SkillName: &skillNamePtr, Excerpt: "step 3 failed"}},
	}

	// 绑定：创建 operator + online_contexts
	dummyOp := &dummyOperator{id: "skill_experience_my_skill"}
	operators := map[string]operator.Operator{"skill_experience_my_skill": dummyOp}
	config := map[string]any{
		"online_contexts": map[string]any{
			skillName: evoCtx,
		},
	}
	opt.Bind(operators, []string{"experiences"}, config)

	err := opt.Backward(context.Background(), []*signal.EvolutionSignal{
		{SignalType: "execution_failure", SkillName: &skillNamePtr, Excerpt: "step 3 failed"},
	})
	assert.NoError(t, err)

	// 验证梯度中已有记录
	param := opt.Parameters()["skill_experience_my_skill"]
	gradient := param.GetGradient("experiences")
	assert.NotNil(t, gradient)
	records, ok := gradient.([]checkpointing.EvolutionRecord)
	assert.True(t, ok)
	assert.GreaterOrEqual(t, len(records), 1)
}

// TestSkillExperienceOptimizer_Backward_无匹配信号 无信号匹配时跳过
func TestSkillExperienceOptimizer_Backward_无匹配信号(t *testing.T) {
	model := newMockSkillModel(t, nil)
	opt := NewSkillExperienceOptimizer(model, "test-model", "cn", GenerateRecordsLLMPolicy)

	dummyOp := &dummyOperator{id: "skill_experience_other_skill"}
	operators := map[string]operator.Operator{"skill_experience_other_skill": dummyOp}
	config := map[string]any{
		"online_contexts": map[string]any{},
	}
	opt.Bind(operators, []string{"experiences"}, config)

	// 信号 skill_name = "my_skill"，但 operator 是 "other_skill"
	skillNamePtr := "my_skill"
	err := opt.Backward(context.Background(), []*signal.EvolutionSignal{
		{SignalType: "execution_failure", SkillName: &skillNamePtr},
	})
	assert.NoError(t, err)
}

// TestSkillExperienceOptimizer_Backward_缺少上下文 online_contexts 缺少 skill 时跳过
func TestSkillExperienceOptimizer_Backward_缺少上下文(t *testing.T) {
	model := newMockSkillModel(t, nil)
	opt := NewSkillExperienceOptimizer(model, "test-model", "cn", GenerateRecordsLLMPolicy)

	dummyOp := &dummyOperator{id: "skill_experience_missing_skill"}
	operators := map[string]operator.Operator{"skill_experience_missing_skill": dummyOp}
	config := map[string]any{
		"online_contexts": map[string]any{},
	}
	opt.Bind(operators, []string{"experiences"}, config)

	skillNamePtr := "missing_skill"
	err := opt.Backward(context.Background(), []*signal.EvolutionSignal{
		{SignalType: "execution_failure", SkillName: &skillNamePtr},
	})
	assert.NoError(t, err)
}

// TestSkillExperienceOptimizer_Step 生成更新映射
func TestSkillExperienceOptimizer_Step(t *testing.T) {
	model := newMockSkillModel(t, func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		return llmschema.NewAssistantMessage(`[{"action":"append","section":"Troubleshooting","target":"body","content":"test"}]`), nil
	})
	opt := NewSkillExperienceOptimizer(model, "test-model", "cn", GenerateRecordsLLMPolicy)

	skillName := "my_skill"
	skillNamePtr := skillName
	evoCtx := &experience.EvolutionContext{
		SkillName:    skillName,
		SkillContent: "skill content",
		Signals:      []signal.EvolutionSignal{{SignalType: "execution_failure", SkillName: &skillNamePtr, Excerpt: "error"}},
	}
	dummyOp := &dummyOperator{id: "skill_experience_my_skill"}
	operators := map[string]operator.Operator{"skill_experience_my_skill": dummyOp}
	config := map[string]any{
		"online_contexts": map[string]any{skillName: evoCtx},
	}
	opt.Bind(operators, []string{"experiences"}, config)

	err := opt.Backward(context.Background(), []*signal.EvolutionSignal{
		{SignalType: "execution_failure", SkillName: &skillNamePtr},
	})
	require.NoError(t, err)

	updates := opt.Step()
	assert.NotEmpty(t, updates)
}

// TestSkillExperienceOptimizer_Step_无梯度 无梯度时返回空映射
func TestSkillExperienceOptimizer_Step_无梯度(t *testing.T) {
	model := newMockSkillModel(t, nil)
	opt := NewSkillExperienceOptimizer(model, "test-model", "cn", GenerateRecordsLLMPolicy)

	dummyOp := &dummyOperator{id: "skill_experience_test"}
	operators := map[string]operator.Operator{"skill_experience_test": dummyOp}
	config := map[string]any{"online_contexts": map[string]any{}}
	opt.Bind(operators, []string{"experiences"}, config)

	updates := opt.Step()
	assert.Empty(t, updates)
}

// TestSkillExperienceOptimizer_GenerateRecords_正常 正常生成记录
func TestSkillExperienceOptimizer_GenerateRecords_正常(t *testing.T) {
	model := newMockSkillModel(t, func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		return llmschema.NewAssistantMessage(`[{"action":"append","section":"Troubleshooting","target":"body","content":"排查步骤"}]`), nil
	})
	opt := NewSkillExperienceOptimizer(model, "test-model", "cn", GenerateRecordsLLMPolicy)

	skillNamePtr := "my_skill"
	evoCtx := &experience.EvolutionContext{
		SkillName:    "my_skill",
		SkillContent: "skill content",
		Signals:      []signal.EvolutionSignal{{SignalType: "execution_failure", SkillName: &skillNamePtr, Excerpt: "error"}},
		Messages:     []map[string]any{{"role": "user", "content": "test"}},
		UserQuery:    "如何使用",
	}

	records, err := opt.GenerateRecords(context.Background(), evoCtx)
	assert.NoError(t, err)
	assert.GreaterOrEqual(t, len(records), 1)
	assert.Equal(t, "execution_failure", records[0].Source)
}

// TestSkillExperienceOptimizer_GenerateRecords_空信号 空信号时返回 nil
func TestSkillExperienceOptimizer_GenerateRecords_空信号(t *testing.T) {
	model := newMockSkillModel(t, nil)
	opt := NewSkillExperienceOptimizer(model, "test-model", "cn", GenerateRecordsLLMPolicy)

	evoCtx := &experience.EvolutionContext{
		SkillName: "my_skill",
		Signals:   []signal.EvolutionSignal{},
	}

	records, err := opt.GenerateRecords(context.Background(), evoCtx)
	assert.NoError(t, err)
	assert.Nil(t, records)
}

// TestSkillExperienceOptimizer_GenerateRecords_skip LLM 返回 skip 时跳过
func TestSkillExperienceOptimizer_GenerateRecords_skip(t *testing.T) {
	model := newMockSkillModel(t, func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		return llmschema.NewAssistantMessage(`[{"action":"skip","skip_reason":"no useful info"}]`), nil
	})
	opt := NewSkillExperienceOptimizer(model, "test-model", "cn", GenerateRecordsLLMPolicy)

	skillNamePtr := "my_skill"
	evoCtx := &experience.EvolutionContext{
		SkillName: "my_skill",
		Signals:   []signal.EvolutionSignal{{SignalType: "user_correction", SkillName: &skillNamePtr, Excerpt: "wrong answer"}},
	}

	records, err := opt.GenerateRecords(context.Background(), evoCtx)
	assert.NoError(t, err)
	assert.Empty(t, records)
}

// TestSkillExperienceOptimizer_GenerateRecords_脚本 target=script 生成脚本记录
func TestSkillExperienceOptimizer_GenerateRecords_脚本(t *testing.T) {
	model := newMockSkillModel(t, func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		return llmschema.NewAssistantMessage(`[{"action":"append","section":"Scripts","target":"script","content":"脚本内容","script_filename":"fix.sh","script_language":"bash","script_purpose":"自动修复"}]`), nil
	})
	opt := NewSkillExperienceOptimizer(model, "test-model", "cn", GenerateRecordsLLMPolicy)

	skillNamePtr := "my_skill"
	evoCtx := &experience.EvolutionContext{
		SkillName: "my_skill",
		Signals:   []signal.EvolutionSignal{{SignalType: "script_artifact", SkillName: &skillNamePtr, Excerpt: "script failure"}},
	}

	records, err := opt.GenerateRecords(context.Background(), evoCtx)
	assert.NoError(t, err)
	assert.GreaterOrEqual(t, len(records), 1)
	// 脚本记录的 Target 应为 script
	hasScript := false
	for _, r := range records {
		if r.Change.Target == signal.EvolutionTargetScript {
			hasScript = true
		}
	}
	assert.True(t, hasScript)
}

// TestSkillExperienceOptimizer_GenerateRecords_空内容 内容为空时跳过
func TestSkillExperienceOptimizer_GenerateRecords_空内容(t *testing.T) {
	model := newMockSkillModel(t, func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		return llmschema.NewAssistantMessage(`[{"action":"append","section":"Troubleshooting","target":"body","content":"   "}]`), nil
	})
	opt := NewSkillExperienceOptimizer(model, "test-model", "cn", GenerateRecordsLLMPolicy)

	skillNamePtr := "my_skill"
	evoCtx := &experience.EvolutionContext{
		SkillName: "my_skill",
		Signals:   []signal.EvolutionSignal{{SignalType: "execution_failure", SkillName: &skillNamePtr, Excerpt: "error"}},
	}

	records, err := opt.GenerateRecords(context.Background(), evoCtx)
	assert.NoError(t, err)
	assert.Empty(t, records)
}

// TestSkillExperienceOptimizer_GenerateRecords_LLM失败 LLM 调用失败时返回错误
func TestSkillExperienceOptimizer_GenerateRecords_LLM失败(t *testing.T) {
	model := newMockSkillModel(t, func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		return nil, fmt.Errorf("LLM service unavailable")
	})
	// 需要短超时策略让快速失败
	policy := llm_resilience.LLMInvokePolicy{
		AttemptTimeoutSecs: 1,
		TotalBudgetSecs:    2,
		MaxAttempts:        1,
	}
	opt := NewSkillExperienceOptimizer(model, "test-model", "cn", policy)

	skillNamePtr := "my_skill"
	evoCtx := &experience.EvolutionContext{
		SkillName: "my_skill",
		Signals:   []signal.EvolutionSignal{{SignalType: "execution_failure", SkillName: &skillNamePtr, Excerpt: "error"}},
	}

	records, err := opt.GenerateRecords(context.Background(), evoCtx)
	assert.Error(t, err)
	assert.Nil(t, records)
}

// TestSkillExperienceOptimizer_buildEvolutionContext_有上下文 查到 online context
func TestSkillExperienceOptimizer_buildEvolutionContext_有上下文(t *testing.T) {
	model := newMockSkillModel(t, nil)
	opt := NewSkillExperienceOptimizer(model, "test-model", "cn", GenerateRecordsLLMPolicy)

	evoCtx := &experience.EvolutionContext{
		SkillName:    "test_skill",
		SkillContent: "content",
	}
	opt.onlineContexts = map[string]*experience.EvolutionContext{
		"test_skill": evoCtx,
	}

	result, err := opt.buildEvolutionContext("test_skill", nil, nil)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "content", result.SkillContent)
}

// TestSkillExperienceOptimizer_buildEvolutionContext_无上下文 缺少 online context 时返回错误
func TestSkillExperienceOptimizer_buildEvolutionContext_无上下文(t *testing.T) {
	model := newMockSkillModel(t, nil)
	opt := NewSkillExperienceOptimizer(model, "test-model", "cn", GenerateRecordsLLMPolicy)
	opt.onlineContexts = map[string]*experience.EvolutionContext{}

	result, err := opt.buildEvolutionContext("missing_skill", nil, nil)
	assert.Error(t, err)
	assert.Nil(t, result)
}

// TestSkillExperienceOptimizer_RetryParseDrafts_截断 截断输出→使用原始 prompt 重新生成
func TestSkillExperienceOptimizer_RetryParseDrafts_截断(t *testing.T) {
	model := newMockSkillModel(t, func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		// RetryParseDrafts 使用原始 prompt 重新调用 LLM，返回有效 JSON
		return llmschema.NewAssistantMessage(`[{"action":"append","section":"Troubleshooting","target":"body","content":"fix step"}]`), nil
	})

	opt := NewSkillExperienceOptimizer(model, "test-model", "cn", GenerateRecordsLLMPolicy)

	// truncated raw → 走 regeneration 路径
	drafts, _, err := opt.RetryParseDrafts(
		context.Background(),
		`[{"action":"append","section":"Tr`, // truncated raw
		"original prompt",
		2, // attemptNumber
		"truncated output",
	)
	assert.NoError(t, err)
	assert.NotNil(t, drafts)
}

// TestSkillExperienceOptimizer_RetryParseDrafts_截断第三次 截断第三次时放弃
func TestSkillExperienceOptimizer_RetryParseDrafts_截断第三次(t *testing.T) {
	model := newMockSkillModel(t, func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		// 重试也返回截断
		return llmschema.NewAssistantMessage(`[{"section":"Tr`), nil
	})

	opt := NewSkillExperienceOptimizer(model, "test-model", "cn", GenerateRecordsLLMPolicy)

	drafts, raw, err := opt.RetryParseDrafts(
		context.Background(),
		`[{"section":"Tr`, // truncated
		"original prompt",
		3, // attemptNumber >= 3 → 放弃
		"truncated",
	)
	assert.Error(t, err)
	assert.Nil(t, drafts)
	assert.NotEmpty(t, raw)
}

// TestSkillExperienceOptimizer_RetryParseDrafts_JSON修复 格式错误→JSON_FIX
func TestSkillExperienceOptimizer_RetryParseDrafts_JSON修复(t *testing.T) {
	model := newMockSkillModel(t, func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		// 返回修复后的有效 JSON
		return llmschema.NewAssistantMessage(`[{"action":"append","section":"Troubleshooting","target":"body","content":"fix"}]`), nil
	})

	opt := NewSkillExperienceOptimizer(model, "test-model", "cn", GenerateRecordsLLMPolicy)

	brokenRaw := `This is not valid JSON structure`
	drafts, _, err := opt.RetryParseDrafts(
		context.Background(),
		brokenRaw,
		"original prompt",
		2, // attempt < 3 → JSON_FIX
		"invalid JSON",
	)
	assert.NoError(t, err)
	assert.NotNil(t, drafts)
}

// TestSkillExperienceOptimizer_RetryParseDrafts_严格修复 attempt >= 3 → JSON_FIX_STRICT
func TestSkillExperienceOptimizer_RetryParseDrafts_严格修复(t *testing.T) {
	model := newMockSkillModel(t, func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		// 返回修复后的有效 JSON
		return llmschema.NewAssistantMessage(`[{"action":"append","section":"Troubleshooting","target":"body","content":"strict fix"}]`), nil
	})

	opt := NewSkillExperienceOptimizer(model, "test-model", "cn", GenerateRecordsLLMPolicy)

	brokenRaw := `some malformed json with { but not truncated`
	drafts, _, err := opt.RetryParseDrafts(
		context.Background(),
		brokenRaw,
		"original prompt",
		3, // attempt >= 3 → strict fix
		"parse error",
	)
	assert.NoError(t, err)
	assert.NotNil(t, drafts)
}

// TestSkillExperienceOptimizer_RetryParseDrafts_重试也失败 重试仍无法解析
func TestSkillExperienceOptimizer_RetryParseDrafts_重试也失败(t *testing.T) {
	model := newMockSkillModel(t, func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		return llmschema.NewAssistantMessage("still not valid json"), nil
	})

	opt := NewSkillExperienceOptimizer(model, "test-model", "cn", GenerateRecordsLLMPolicy)

	drafts, _, err := opt.RetryParseDrafts(
		context.Background(),
		"broken json",
		"original prompt",
		2,
		"parse error",
	)
	assert.Error(t, err)
	assert.Nil(t, drafts)
}

// TestSkillExperienceOptimizer_generateDraftsWithRetries_正常 首次成功解析
func TestSkillExperienceOptimizer_generateDraftsWithRetries_正常(t *testing.T) {
	model := newMockSkillModel(t, func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		return llmschema.NewAssistantMessage(`[{"action":"append","section":"Troubleshooting","target":"body","content":"draft"}]`), nil
	})

	opt := NewSkillExperienceOptimizer(model, "test-model", "cn", GenerateRecordsLLMPolicy)

	drafts, err := opt.generateDraftsWithRetries(context.Background(), "prompt", "retry prompt")
	assert.NoError(t, err)
	assert.GreaterOrEqual(t, len(drafts), 1)
}

// TestSkillExperienceOptimizer_generateDraftsWithRetries_需重试 首次失败后重试成功
func TestSkillExperienceOptimizer_generateDraftsWithRetries_需重试(t *testing.T) {
	callCount := 0
	model := newMockSkillModel(t, func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		callCount++
		if callCount == 1 {
			return llmschema.NewAssistantMessage("not json at all"), nil
		}
		// 重试
		return llmschema.NewAssistantMessage(`[{"action":"append","section":"Troubleshooting","target":"body","content":"retry draft"}]`), nil
	})

	policy := llm_resilience.LLMInvokePolicy{
		AttemptTimeoutSecs: 10,
		TotalBudgetSecs:    60,
		MaxAttempts:        3,
	}
	opt := NewSkillExperienceOptimizer(model, "test-model", "cn", policy)

	drafts, err := opt.generateDraftsWithRetries(context.Background(), "prompt", "retry prompt")
	assert.NoError(t, err)
	assert.GreaterOrEqual(t, len(drafts), 1)
}

// TestSkillExperienceOptimizer_generateDraftsWithRetries_全部失败 所有重试都失败
func TestSkillExperienceOptimizer_generateDraftsWithRetries_全部失败(t *testing.T) {
	model := newMockSkillModel(t, func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		return llmschema.NewAssistantMessage("invalid"), nil
	})

	policy := llm_resilience.LLMInvokePolicy{
		AttemptTimeoutSecs: 5,
		TotalBudgetSecs:    30,
		MaxAttempts:        2,
	}
	opt := NewSkillExperienceOptimizer(model, "test-model", "cn", policy)

	drafts, err := opt.generateDraftsWithRetries(context.Background(), "prompt", "retry")
	assert.Error(t, err)
	assert.Nil(t, drafts)
}

// ─── TeamSkillExperienceOptimizer LLM 模拟测试 ───

// TestTeamSkillExperienceOptimizer_Bind 绑定 operators
func TestTeamSkillExperienceOptimizer_Bind(t *testing.T) {
	model := newMockSkillModel(t, nil)
	opt := NewTeamSkillExperienceOptimizer(model, "test-model", "cn", "/tmp", TeamSkillRecordLLMPolicy, nil)

	skillName := "team_skill"
	evoCtx := &experience.EvolutionContext{
		SkillName:    skillName,
		SkillContent: "team skill content",
		Signals:      []signal.EvolutionSignal{{SignalType: "trajectory_issue"}},
	}
	operators := map[string]operator.Operator{}
	config := map[string]any{
		"online_contexts": map[string]any{
			skillName: evoCtx,
		},
	}

	count := opt.Bind(operators, []string{"experiences"}, config)
	assert.Equal(t, 0, count)
	assert.NotNil(t, opt.onlineContexts[skillName])
}

// TestTeamSkillExperienceOptimizer_SelectSignals 默认保留所有信号
func TestTeamSkillExperienceOptimizer_SelectSignals(t *testing.T) {
	opt := NewTeamSkillExperienceOptimizer(nil, "test-model", "cn", "", TeamSkillRecordLLMPolicy, nil)
	sigs := []*signal.EvolutionSignal{{SignalType: "trajectory_issue"}}
	selected := opt.SelectSignals(sigs)
	assert.Equal(t, len(sigs), len(selected))
}

// TestTeamSkillExperienceOptimizer_Backward_正常 反向传播正常流程（聚合路径）
func TestTeamSkillExperienceOptimizer_Backward_正常(t *testing.T) {
	model := newMockSkillModel(t, func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		return llmschema.NewAssistantMessage(`[{"action":"append","section":"Workflow","target":"body","content":"团队协作改进"}]`), nil
	})
	opt := NewTeamSkillExperienceOptimizer(model, "test-model", "cn", "", TeamSkillRecordLLMPolicy, nil)

	skillName := "team_skill"
	skillNamePtr := skillName
	traj := &trajectory.Trajectory{
		ExecutionID: "exec-1",
		SessionID:   "sess-1",
		Source:      "online",
		Steps: []*trajectory.TrajectoryStep{
			{Kind: trajectory.StepKindTool},
		},
	}
	evoCtx := &experience.EvolutionContext{
		SkillName:    skillName,
		SkillContent: "team skill content",
		Signals:      []signal.EvolutionSignal{{SignalType: "trajectory_issue", SkillName: &skillNamePtr, Excerpt: "agent failed"}},
		Trajectory:   traj,
	}

	dummyOp := &dummyOperator{id: "skill_experience_team_skill"}
	operators := map[string]operator.Operator{"skill_experience_team_skill": dummyOp}
	config := map[string]any{
		"online_contexts": map[string]any{skillName: evoCtx},
	}
	opt.Bind(operators, []string{"experiences"}, config)

	err := opt.Backward(context.Background(), []*signal.EvolutionSignal{
		{SignalType: "trajectory_issue", SkillName: &skillNamePtr, Excerpt: "agent failed"},
	})
	assert.NoError(t, err)

	param := opt.Parameters()["skill_experience_team_skill"]
	gradient := param.GetGradient("experiences")
	assert.NotNil(t, gradient)
}

// TestTeamSkillExperienceOptimizer_Backward_逐信号Patch 无 kind 步骤时走逐信号 patch 路径
func TestTeamSkillExperienceOptimizer_Backward_逐信号Patch(t *testing.T) {
	callCount := 0
	model := newMockSkillModel(t, func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		callCount++
		// 返回 JSON dict（不是数组），因为 patch 期望单个对象
		return llmschema.NewAssistantMessage(`{"need_patch": true, "action": "append", "section": "Instructions", "content": "改进指令"}`), nil
	})
	opt := NewTeamSkillExperienceOptimizer(model, "test-model", "cn", "", TeamSkillRecordLLMPolicy, nil)

	skillName := "team_skill"
	skillNamePtr := skillName
	// Trajectory 没有 Kind 步骤 → 走逐信号 patch 路径
	evoCtx := &experience.EvolutionContext{
		SkillName:    skillName,
		SkillContent: "team skill content",
		Signals:      []signal.EvolutionSignal{{SignalType: "trajectory_issue", SkillName: &skillNamePtr, Excerpt: "agent issue"}},
		Trajectory: &trajectory.Trajectory{
			ExecutionID: "exec-1",
			SessionID:   "sess-1",
			Source:      "online",
			Steps:       []*trajectory.TrajectoryStep{}, // 空 Steps → 走逐信号 patch
		},
	}

	dummyOp := &dummyOperator{id: "skill_experience_team_skill"}
	operators := map[string]operator.Operator{"skill_experience_team_skill": dummyOp}
	config := map[string]any{
		"online_contexts": map[string]any{skillName: evoCtx},
	}
	opt.Bind(operators, []string{"experiences"}, config)

	err := opt.Backward(context.Background(), []*signal.EvolutionSignal{
		{SignalType: "trajectory_issue", SkillName: &skillNamePtr, Excerpt: "agent issue"},
	})
	assert.NoError(t, err)
	assert.GreaterOrEqual(t, callCount, 1)
}

// TestTeamSkillExperienceOptimizer_Step 生成更新映射
func TestTeamSkillExperienceOptimizer_Step(t *testing.T) {
	model := newMockSkillModel(t, func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		return llmschema.NewAssistantMessage(`[{"action":"append","section":"Workflow","target":"body","content":"改进"}]`), nil
	})
	opt := NewTeamSkillExperienceOptimizer(model, "test-model", "cn", "", TeamSkillRecordLLMPolicy, nil)

	skillName := "team_skill"
	skillNamePtr := skillName
	evoCtx := &experience.EvolutionContext{
		SkillName:    skillName,
		SkillContent: "content",
		Signals:      []signal.EvolutionSignal{{SignalType: "trajectory_issue", SkillName: &skillNamePtr}},
		Trajectory: &trajectory.Trajectory{
			Steps: []*trajectory.TrajectoryStep{{Kind: trajectory.StepKindTool}},
		},
	}
	dummyOp := &dummyOperator{id: "skill_experience_team_skill"}
	operators := map[string]operator.Operator{"skill_experience_team_skill": dummyOp}
	config := map[string]any{"online_contexts": map[string]any{skillName: evoCtx}}
	opt.Bind(operators, []string{"experiences"}, config)

	err := opt.Backward(context.Background(), []*signal.EvolutionSignal{
		{SignalType: "trajectory_issue", SkillName: &skillNamePtr},
	})
	require.NoError(t, err)

	updates := opt.Step()
	assert.NotEmpty(t, updates)
}

// TestTeamSkillExperienceOptimizer_GenerateRecords_聚合路径 有 Kind 步骤走聚合 LLM 路径
func TestTeamSkillExperienceOptimizer_GenerateRecords_聚合路径(t *testing.T) {
	model := newMockSkillModel(t, func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		return llmschema.NewAssistantMessage(`[{"action":"append","section":"Workflow","target":"body","content":"改进协作"}]`), nil
	})
	opt := NewTeamSkillExperienceOptimizer(model, "test-model", "cn", "", TeamSkillRecordLLMPolicy, nil)

	skillNamePtr := "team_skill"
	evoCtx := &experience.EvolutionContext{
		SkillName:    "team_skill",
		SkillContent: "team skill content",
		Signals:      []signal.EvolutionSignal{{SignalType: "trajectory_issue", SkillName: &skillNamePtr, Excerpt: "failed"}},
		Trajectory: &trajectory.Trajectory{
			Steps: []*trajectory.TrajectoryStep{{Kind: trajectory.StepKindTool}},
		},
	}

	records, err := opt.GenerateRecords(context.Background(), evoCtx)
	assert.NoError(t, err)
	assert.GreaterOrEqual(t, len(records), 1)
}

// TestTeamSkillExperienceOptimizer_GenerateRecords_空信号 空信号返回 nil
func TestTeamSkillExperienceOptimizer_GenerateRecords_空信号(t *testing.T) {
	model := newMockSkillModel(t, nil)
	opt := NewTeamSkillExperienceOptimizer(model, "test-model", "cn", "", TeamSkillRecordLLMPolicy, nil)

	evoCtx := &experience.EvolutionContext{
		SkillName: "team_skill",
		Signals:   []signal.EvolutionSignal{},
	}

	records, err := opt.GenerateRecords(context.Background(), evoCtx)
	assert.NoError(t, err)
	assert.Nil(t, records)
}

// TestTeamSkillExperienceOptimizer_GenerateRecords_逐信号patch trajectory 无 steps 时走逐信号路径
func TestTeamSkillExperienceOptimizer_GenerateRecords_逐信号patch(t *testing.T) {
	model := newMockSkillModel(t, func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		return llmschema.NewAssistantMessage(`{"need_patch": true, "action": "append", "section": "Workflow", "content": "改进工作流"}`), nil
	})
	opt := NewTeamSkillExperienceOptimizer(model, "test-model", "cn", "", TeamSkillRecordLLMPolicy, nil)

	skillNamePtr := "team_skill"
	evoCtx := &experience.EvolutionContext{
		SkillName:    "team_skill",
		SkillContent: "content",
		Signals:      []signal.EvolutionSignal{{SignalType: "trajectory_issue", SkillName: &skillNamePtr, Excerpt: "issue"}},
		Trajectory: &trajectory.Trajectory{
			Steps: []*trajectory.TrajectoryStep{}, // 空 steps → 走逐信号路径
		},
	}

	_, err := opt.GenerateRecords(context.Background(), evoCtx)
	assert.NoError(t, err)
	// 逐信号路径可能返回 0 或 >0 条记录，取决于 parsePatchResponse
}

// TestTeamSkillExperienceOptimizer_GenerateUserPatch 用户意图 patch
func TestTeamSkillExperienceOptimizer_GenerateUserPatch(t *testing.T) {
	model := newMockSkillModel(t, func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		return llmschema.NewAssistantMessage(`{"need_patch": true, "action": "append", "section": "Instructions", "content": "根据用户意图新增说明", "summary": "用户需求改进"}`), nil
	})
	opt := NewTeamSkillExperienceOptimizer(model, "test-model", "cn", "", TeamSkillRecordLLMPolicy, nil)

	traj := &trajectory.Trajectory{
		ExecutionID: "exec-1",
		SessionID:   "sess-1",
		Source:      "online",
		Steps:       []*trajectory.TrajectoryStep{{Kind: trajectory.StepKindTool}},
	}

	record, err := opt.GenerateUserPatch(context.Background(), traj, "team_skill", "用户想更快速地完成分析")
	assert.NoError(t, err)
	assert.NotNil(t, record)
	assert.Equal(t, "Instructions", record.Change.Section)
	assert.Equal(t, "team_skill_user_patch", record.Source)
}

// TestTeamSkillExperienceOptimizer_GenerateUserPatch_不需要 无需 patch 时返回 nil
func TestTeamSkillExperienceOptimizer_GenerateUserPatch_不需要(t *testing.T) {
	model := newMockSkillModel(t, func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		return llmschema.NewAssistantMessage(`{"need_patch": false, "reason": "skill already covers this"}`), nil
	})
	opt := NewTeamSkillExperienceOptimizer(model, "test-model", "cn", "", TeamSkillRecordLLMPolicy, nil)

	traj := &trajectory.Trajectory{
		ExecutionID: "exec-1",
		SessionID:   "sess-1",
		Source:      "online",
		Steps:       []*trajectory.TrajectoryStep{},
	}

	record, err := opt.GenerateUserPatch(context.Background(), traj, "team_skill", "用户意图")
	assert.NoError(t, err)
	assert.Nil(t, record)
}

// TestTeamSkillExperienceOptimizer_GenerateTrajectoryPatch 轨迹 patch
func TestTeamSkillExperienceOptimizer_GenerateTrajectoryPatch(t *testing.T) {
	model := newMockSkillModel(t, func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		return llmschema.NewAssistantMessage(`{"need_patch": true, "action": "append", "section": "Workflow", "content": "改进工作流步骤", "summary": "轨迹改进"}`), nil
	})
	opt := NewTeamSkillExperienceOptimizer(model, "test-model", "cn", "", TeamSkillRecordLLMPolicy, nil)

	traj := &trajectory.Trajectory{
		ExecutionID: "exec-1",
		SessionID:   "sess-1",
		Source:      "online",
		Steps:       []*trajectory.TrajectoryStep{{Kind: trajectory.StepKindTool}},
	}
	issues := []map[string]string{{"type": "communication_failure", "description": "agent A 和 agent B 通信断开"}}

	record, err := opt.GenerateTrajectoryPatch(context.Background(), traj, "team_skill", "skill content", issues)
	assert.NoError(t, err)
	assert.NotNil(t, record)
	assert.Equal(t, "Workflow", record.Change.Section)
	assert.Equal(t, "team_skill_trajectory_patch", record.Source)
}

// TestTeamSkillExperienceOptimizer_GenerateTrajectoryPatch_不需要 无需 patch
func TestTeamSkillExperienceOptimizer_GenerateTrajectoryPatch_不需要(t *testing.T) {
	model := newMockSkillModel(t, func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		return llmschema.NewAssistantMessage(`{"need_patch": false, "reason": "trajectory is healthy"}`), nil
	})
	opt := NewTeamSkillExperienceOptimizer(model, "test-model", "cn", "", TeamSkillRecordLLMPolicy, nil)

	traj := &trajectory.Trajectory{
		ExecutionID: "exec-1",
		SessionID:   "sess-1",
		Source:      "online",
	}
	issues := []map[string]string{}

	record, err := opt.GenerateTrajectoryPatch(context.Background(), traj, "team_skill", "content", issues)
	assert.NoError(t, err)
	assert.Nil(t, record)
}

// TestTeamSkillExperienceOptimizer_RegenerateBody 重写 SKILL.md body
func TestTeamSkillExperienceOptimizer_RegenerateBody(t *testing.T) {
	model := newMockSkillModel(t, func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		// 返回足够长的重写内容
		return llmschema.NewAssistantMessage(strings.Repeat("这是重写的 skill body 内容。", 20)), nil
	})
	opt := NewTeamSkillExperienceOptimizer(model, "test-model", "cn", "", TeamSkillRecordLLMPolicy, nil)

	records := []checkpointing.EvolutionRecord{
		{ID: "ev_001", Change: checkpointing.EvolutionPatch{Section: "Instructions", Content: "新增指令"}},
	}

	body, err := opt.RegenerateBody(context.Background(), "team_skill", "当前 skill body 内容", records, "用户意图")
	assert.NoError(t, err)
	assert.NotEmpty(t, body)
}

// TestTeamSkillExperienceOptimizer_RegenerateBody_返回过短 body 过短时返回空
func TestTeamSkillExperienceOptimizer_RegenerateBody_返回过短(t *testing.T) {
	model := newMockSkillModel(t, func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		return llmschema.NewAssistantMessage("短"), nil
	})
	opt := NewTeamSkillExperienceOptimizer(model, "test-model", "cn", "", TeamSkillRecordLLMPolicy, nil)

	body, err := opt.RegenerateBody(context.Background(), "team_skill", "当前 body", nil, "")
	assert.NoError(t, err)
	assert.Empty(t, body) // len(body) < 50 → 返回空
}

// TestTeamSkillExperienceOptimizer_callLLM_有policy 有 policy 走 InvokeTextWithRetry
func TestTeamSkillExperienceOptimizer_callLLM_有policy(t *testing.T) {
	model := newMockSkillModel(t, func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		return llmschema.NewAssistantMessage("LLM response"), nil
	})
	opt := NewTeamSkillExperienceOptimizer(model, "test-model", "cn", "", TeamSkillRecordLLMPolicy, nil)

	policy := TeamSkillRecordLLMPolicy
	result, err := opt.callLLM(context.Background(), "prompt", "retry", &policy, nil)
	assert.NoError(t, err)
	assert.Equal(t, "LLM response", result)
}

// TestTeamSkillExperienceOptimizer_callLLM_无policy 无 policy 走单次调用
func TestTeamSkillExperienceOptimizer_callLLM_无policy(t *testing.T) {
	model := newMockSkillModel(t, func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		return llmschema.NewAssistantMessage("single attempt response"), nil
	})
	opt := NewTeamSkillExperienceOptimizer(model, "test-model", "cn", "", TeamSkillRecordLLMPolicy, nil)

	result, err := opt.callLLM(context.Background(), "prompt", "", nil, nil)
	assert.NoError(t, err)
	assert.Equal(t, "single attempt response", result)
}

// TestTeamSkillExperienceOptimizer_callLLM_失败 LLM 调用失败
func TestTeamSkillExperienceOptimizer_callLLM_失败(t *testing.T) {
	model := newMockSkillModel(t, func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		return nil, fmt.Errorf("service down")
	})
	singlePolicy := llm_resilience.LLMInvokePolicy{MaxAttempts: 1, AttemptTimeoutSecs: 5, TotalBudgetSecs: 5}
	opt := NewTeamSkillExperienceOptimizer(model, "test-model", "cn", "", singlePolicy, nil)

	result, err := opt.callLLM(context.Background(), "prompt", "", nil, nil)
	assert.Error(t, err)
	assert.Empty(t, result)
}

// TestTeamSkillExperienceOptimizer_RetryParseDrafts_截断 团队截断重试
func TestTeamSkillExperienceOptimizer_RetryParseDrafts_截断(t *testing.T) {
	model := newMockSkillModel(t, func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		return llmschema.NewAssistantMessage(`[{"action":"append","section":"Workflow","target":"body","content":"fix"}]`), nil
	})
	opt := NewTeamSkillExperienceOptimizer(model, "test-model", "cn", "", TeamSkillRecordLLMPolicy, nil)

	drafts, _, err := opt.RetryParseDrafts(
		context.Background(),
		`[{"section":"Tr`, // truncated
		"original prompt",
		2,
		"truncated output",
	)
	assert.NoError(t, err)
	assert.NotNil(t, drafts)
}

// TestTeamSkillExperienceOptimizer_RetryParseDrafts_严格修复 attempt >= 3
func TestTeamSkillExperienceOptimizer_RetryParseDrafts_严格修复(t *testing.T) {
	model := newMockSkillModel(t, func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		return llmschema.NewAssistantMessage(`[{"action":"append","section":"Workflow","target":"body","content":"strict"}]`), nil
	})
	opt := NewTeamSkillExperienceOptimizer(model, "test-model", "cn", "", TeamSkillRecordLLMPolicy, nil)

	drafts, _, err := opt.RetryParseDrafts(
		context.Background(),
		"broken json not truncated",
		"original",
		3, // >= 3 → strict fix
		"parse error",
	)
	assert.NoError(t, err)
	assert.NotNil(t, drafts)
}

// TestTeamSkillExperienceOptimizer_RetryParseDrafts_失败 重试也失败
func TestTeamSkillExperienceOptimizer_RetryParseDrafts_失败(t *testing.T) {
	model := newMockSkillModel(t, func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		return llmschema.NewAssistantMessage("not json"), nil
	})
	opt := NewTeamSkillExperienceOptimizer(model, "test-model", "cn", "", TeamSkillRecordLLMPolicy, nil)

	drafts, _, err := opt.RetryParseDrafts(
		context.Background(),
		"broken",
		"original",
		2,
		"error",
	)
	assert.Error(t, err)
	assert.Nil(t, drafts)
}

// TestTeamSkillExperienceOptimizer_buildEvolutionContext_有上下文 有 trajectory
func TestTeamSkillExperienceOptimizer_buildEvolutionContext_有上下文(t *testing.T) {
	model := newMockSkillModel(t, nil)
	opt := NewTeamSkillExperienceOptimizer(model, "test-model", "cn", "", TeamSkillRecordLLMPolicy, nil)

	traj := &trajectory.Trajectory{ExecutionID: "exec-1", SessionID: "sess-1"}
	evoCtx := &experience.EvolutionContext{
		SkillName:    "team_skill",
		SkillContent: "content",
		Trajectory:   traj,
	}
	opt.onlineContexts = map[string]*experience.EvolutionContext{"team_skill": evoCtx}

	defaultTraj := &trajectory.Trajectory{ExecutionID: "default"}
	result, err := opt.buildEvolutionContext("team_skill", nil, nil, defaultTraj)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, traj, result.Trajectory) // 已有 trajectory 不被替换
}

// TestTeamSkillExperienceOptimizer_buildEvolutionContext_填充trajectory trajectory nil 时填充 default_trajectory
func TestTeamSkillExperienceOptimizer_buildEvolutionContext_填充trajectory(t *testing.T) {
	model := newMockSkillModel(t, nil)
	opt := NewTeamSkillExperienceOptimizer(model, "test-model", "cn", "", TeamSkillRecordLLMPolicy, nil)

	evoCtx := &experience.EvolutionContext{
		SkillName:    "team_skill",
		SkillContent: "content",
		Trajectory:   nil, // nil → 需要填充
	}
	opt.onlineContexts = map[string]*experience.EvolutionContext{"team_skill": evoCtx}

	defaultTraj := &trajectory.Trajectory{ExecutionID: "default", SessionID: "default"}
	result, err := opt.buildEvolutionContext("team_skill", nil, nil, defaultTraj)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, defaultTraj, result.Trajectory)
}

// TestTeamSkillExperienceOptimizer_buildEvolutionContext_缺少 缺少 skill 时报错
func TestTeamSkillExperienceOptimizer_buildEvolutionContext_缺少(t *testing.T) {
	model := newMockSkillModel(t, nil)
	opt := NewTeamSkillExperienceOptimizer(model, "test-model", "cn", "", TeamSkillRecordLLMPolicy, nil)
	opt.onlineContexts = map[string]*experience.EvolutionContext{}

	result, err := opt.buildEvolutionContext("missing", nil, nil, nil)
	assert.Error(t, err)
	assert.Nil(t, result)
}

// TestTeamSkillExperienceOptimizer_generateDraftsWithRetries_正常 首次成功
func TestTeamSkillExperienceOptimizer_generateDraftsWithRetries_正常(t *testing.T) {
	model := newMockSkillModel(t, func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		return llmschema.NewAssistantMessage(`[{"action":"append","section":"Workflow","target":"body","content":"draft"}]`), nil
	})
	opt := NewTeamSkillExperienceOptimizer(model, "test-model", "cn", "", TeamSkillRecordLLMPolicy, nil)

	drafts, err := opt.generateDraftsWithRetries(context.Background(), "prompt", "retry")
	assert.NoError(t, err)
	assert.GreaterOrEqual(t, len(drafts), 1)
}

// ─── 辅助函数覆盖 ───

// TestBaseOptimizer_LLM 更新 LLM
func TestBaseOptimizer_LLM(t *testing.T) {
	model := newMockSkillModel(t, nil)
	base := SkillExperienceOptimizerBase{llm: model, model: "test"}
	result := base.LLM()
	assert.NotNil(t, result)
}

// TestBaseOptimizer_OnlineContexts 返回在线上下文
func TestBaseOptimizer_OnlineContexts(t *testing.T) {
	ctxMap := map[string]*experience.EvolutionContext{"skill1": nil}
	base := SkillExperienceOptimizerBase{onlineContexts: ctxMap}
	result := base.OnlineContexts()
	assert.Equal(t, ctxMap, result)
}

// TestBaseOptimizer_UpdateLLM_正常 更新非 nil LLM
func TestBaseOptimizer_UpdateLLM_正常(t *testing.T) {
	model1 := newMockSkillModel(t, nil)
	model2 := newMockSkillModel(t, nil)
	base := SkillExperienceOptimizerBase{llm: model1, model: "model1"}

	base.UpdateLLM(model2, "model2")
	assert.Equal(t, model2, base.llm)
	assert.Equal(t, "model2", base.model)
}

// TestSignalValuesToDicts 值类型信号转 dicts
func TestSignalValuesToDicts(t *testing.T) {
	signals := []signal.EvolutionSignal{
		{SignalType: "execution_failure", Excerpt: "error"},
	}
	dicts := signalValuesToDicts(signals)
	assert.Equal(t, 1, len(dicts))
	assert.Equal(t, "execution_failure", dicts[0]["type"])
}

// TestSignalValuesToJSONCompact 值类型信号紧凑 JSON
func TestSignalValuesToJSONCompact(t *testing.T) {
	signals := []signal.EvolutionSignal{{SignalType: "user_correction", Excerpt: "fix"}}
	data := signalValuesToJSONCompact(signals)
	assert.Contains(t, string(data), "user_correction")
}

// TestSignalListToDicts 指针类型信号转 dicts
func TestSignalListToDicts(t *testing.T) {
	signals := []*signal.EvolutionSignal{{SignalType: "low_score", Excerpt: "bad"}}
	dicts := signalListToDicts(signals)
	assert.Equal(t, 1, len(dicts))
}

// TestSignalListToJSONCompact 指针类型信号紧凑 JSON
func TestSignalListToJSONCompact(t *testing.T) {
	signals := []*signal.EvolutionSignal{{SignalType: "low_score"}}
	data := signalListToJSONCompact(signals)
	assert.Contains(t, string(data), "low_score")
}

// TestBuildContextFromValues 值类型信号构建上下文
func TestBuildContextFromValues(t *testing.T) {
	signals := []signal.EvolutionSignal{
		{SignalType: "execution_failure", Excerpt: "step 3 error"},
		{SignalType: "user_correction", Excerpt: "wrong answer"},
	}
	result := buildContextFromValues(signals)
	assert.Contains(t, result, "[execution_failure]")
	assert.Contains(t, result, "[user_correction]")
}

// TestBuildContextFromValues_空 空信号返回空
func TestBuildContextFromValues_空(t *testing.T) {
	result := buildContextFromValues(nil)
	assert.Equal(t, "", result)
}

// TestTruncateOrDefault 截断或默认值
func TestTruncateOrDefault(t *testing.T) {
	assert.Equal(t, "abc", truncateOrDefault("abcdef", 3, "无", "None"))
	assert.Equal(t, "无", truncateOrDefault("", 3, "无", "None"))
	assert.Equal(t, "hello", truncateOrDefault("hello", 10, "无", "None"))
}

// TestPtrToStr 指针转字符串
func TestPtrToStr(t *testing.T) {
	s := "value"
	assert.Equal(t, "value", ptrToStr(&s))
	assert.Equal(t, "", ptrToStr(nil))
}

// TestTimeNow 返回 UTC 时间
func TestTimeNow(t *testing.T) {
	result := timeNow()
	assert.NotEmpty(t, result)
	// 应为 RFC3339 格式
	assert.Contains(t, result, "T")
}

// TestTryParseWithError 尝试解析带错误
func TestTryParseWithError(t *testing.T) {
	result, err := tryParseWithError(`{"key": "val"}`)
	assert.NoError(t, err)
	assert.NotNil(t, result)

	result2, err2 := tryParseWithError("not json")
	assert.Error(t, err2)
	assert.Nil(t, result2)
}

// TestGetStrFromMap_完整 各种类型取值
func TestGetStrFromMap_完整(t *testing.T) {
	data := map[string]any{
		"str_key": "hello",
		"int_key": 42,
		"nil_key": nil,
		"missing": "exists",
	}
	assert.Equal(t, "hello", getStrFromMap(data, "str_key", "default"))
	assert.Equal(t, "42", getStrFromMap(data, "int_key", "default"))
	assert.Equal(t, "default", getStrFromMap(data, "nil_key", "default"))
	assert.Equal(t, "default", getStrFromMap(data, "absent", "default"))
}

// TestOrElse_defaults 多默认值
func TestOrElse_defaults(t *testing.T) {
	assert.Equal(t, "value", orDefault("value", "fallback1", "fallback2"))
	assert.Equal(t, "fallback1", orDefault("", "fallback1", "fallback2"))
	assert.Equal(t, "", orDefault(""))
}

// TestDumpRaw 调试输出
func TestDumpRaw(t *testing.T) {
	opt := NewTeamSkillExperienceOptimizer(nil, "test", "cn", "", TeamSkillRecordLLMPolicy, nil)
	opt.dumpRaw("tag", "raw") // 空 debugDir → 不做任何事
	assert.True(t, true)      // 不 panic 即通过
}

// TestDumpRaw_有目录 有 debugDir
func TestDumpRaw_有目录(t *testing.T) {
	opt := NewTeamSkillExperienceOptimizer(nil, "test", "cn", "/tmp", TeamSkillRecordLLMPolicy, nil)
	opt.dumpRaw("test", "some raw content")
}

// TestTeamSkillExperienceOptimizer_generateDraftsWithRetries_需重试 重试成功
func TestTeamSkillExperienceOptimizer_generateDraftsWithRetries_需重试(t *testing.T) {
	callCount := 0
	model := newMockSkillModel(t, func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		callCount++
		if callCount == 1 {
			return llmschema.NewAssistantMessage("not json"), nil
		}
		return llmschema.NewAssistantMessage(`[{"action":"append","section":"Workflow","target":"body","content":"retry ok"}]`), nil
	})
	policy := llm_resilience.LLMInvokePolicy{
		AttemptTimeoutSecs: 10,
		TotalBudgetSecs:    60,
		MaxAttempts:        3,
	}
	opt := NewTeamSkillExperienceOptimizer(model, "test-model", "cn", "", policy, nil)

	drafts, err := opt.generateDraftsWithRetries(context.Background(), "prompt", "retry")
	assert.NoError(t, err)
	assert.GreaterOrEqual(t, len(drafts), 1)
}

// ─── 补充覆盖率测试 ───

// TestExtractTextFromMessage_列表内容 content 为列表
func TestExtractTextFromMessage_列表内容(t *testing.T) {
	msg := map[string]any{
		"content": []any{
			map[string]any{"text": "part1"},
			map[string]any{"text": "part2"},
			"plain string",
		},
	}
	result := extractTextFromMessage(msg)
	assert.Contains(t, result, "part1")
	assert.Contains(t, result, "part2")
	assert.Contains(t, result, "plain string")
}

// TestExtractTextFromMessage_非字符串 content 为数字
func TestExtractTextFromMessage_非字符串(t *testing.T) {
	msg := map[string]any{"content": 42}
	result := extractTextFromMessage(msg)
	assert.Equal(t, "42", result)
}

// TestSummarizeSkillContentWithMax_截断 超过上限时截断
func TestSummarizeSkillContentWithMax_截断(t *testing.T) {
	long := strings.Repeat("a", 7000)
	result := summarizeSkillContentWithMax(long, 6000)
	assert.Contains(t, result, "已截断")
	assert.True(t, len(result) <= 7000)
}

// TestSummarizeSkillContentWithMax_短内容 短内容不截断
func TestSummarizeSkillContentWithMax_短内容(t *testing.T) {
	short := "short"
	result := summarizeSkillContentWithMax(short, 6000)
	assert.Equal(t, short, result)
}

// TestPreviewSection_长截断 预览截断长正文
func TestPreviewSection_长截断(t *testing.T) {
	longBody := strings.Repeat("content ", 100)
	section := "## Title\n" + longBody
	preview := previewSection(section)
	assert.Contains(t, preview, "## Title")
	assert.Contains(t, preview, "...")
}

// TestPreviewSection_无正文 只有标题时返回标题
func TestPreviewSection_无正文(t *testing.T) {
	result := previewSection("## Title")
	assert.Equal(t, "## Title", result)
}

// TestMax_function a < b 返回 b
func TestMax_function(t *testing.T) {
	assert.Equal(t, 5, max(3, 5))
	assert.Equal(t, 7, max(7, 2))
}

// TestSkillExperienceOptimizer_Bind_mapStringContext map[string]*EvolutionContext 类型
func TestSkillExperienceOptimizer_Bind_mapStringContext(t *testing.T) {
	model := newMockSkillModel(t, nil)
	opt := NewSkillExperienceOptimizer(model, "test-model", "cn", GenerateRecordsLLMPolicy)

	skillName := "test_skill"
	evoCtx := &experience.EvolutionContext{SkillName: skillName}
	operators := map[string]operator.Operator{}
	config := map[string]any{
		"online_contexts": map[string]*experience.EvolutionContext{
			skillName: evoCtx,
		},
	}

	count := opt.Bind(operators, []string{"experiences"}, config)
	assert.Equal(t, 0, count)
	assert.Equal(t, evoCtx, opt.onlineContexts[skillName])
}

// TestSkillExperienceOptimizer_Bind_default类型 online_contexts 为未知类型
func TestSkillExperienceOptimizer_Bind_default类型(t *testing.T) {
	model := newMockSkillModel(t, nil)
	opt := NewSkillExperienceOptimizer(model, "test-model", "cn", GenerateRecordsLLMPolicy)

	operators := map[string]operator.Operator{}
	config := map[string]any{
		"online_contexts": "invalid", // 非映射类型
	}

	count := opt.Bind(operators, []string{"experiences"}, config)
	assert.Equal(t, 0, count)
	assert.Empty(t, opt.onlineContexts)
}

// TestTeamSkillExperienceOptimizer_Bind_mapStringContext map[string]*EvolutionContext 类型
func TestTeamSkillExperienceOptimizer_Bind_mapStringContext(t *testing.T) {
	model := newMockSkillModel(t, nil)
	opt := NewTeamSkillExperienceOptimizer(model, "test-model", "cn", "", TeamSkillRecordLLMPolicy, nil)

	skillName := "team_skill"
	evoCtx := &experience.EvolutionContext{SkillName: skillName}
	operators := map[string]operator.Operator{}
	config := map[string]any{
		"online_contexts": map[string]*experience.EvolutionContext{
			skillName: evoCtx,
		},
	}

	count := opt.Bind(operators, []string{"experiences"}, config)
	assert.Equal(t, 0, count)
	assert.Equal(t, evoCtx, opt.onlineContexts[skillName])
}

// TestTeamSkillExperienceOptimizer_Bind_default类型 online_contexts 为未知类型
func TestTeamSkillExperienceOptimizer_Bind_default类型(t *testing.T) {
	model := newMockSkillModel(t, nil)
	opt := NewTeamSkillExperienceOptimizer(model, "test-model", "cn", "", TeamSkillRecordLLMPolicy, nil)

	operators := map[string]operator.Operator{}
	config := map[string]any{
		"online_contexts": 123, // 非映射类型
	}

	count := opt.Bind(operators, []string{"experiences"}, config)
	assert.Equal(t, 0, count)
	assert.Empty(t, opt.onlineContexts)
}

// TestLimitSummaryLines_空输入 空输入返回空
func TestLimitSummaryLines_空输入(t *testing.T) {
	result := limitSummaryLines("", 3)
	assert.Equal(t, "", result)
}

// TestLimitSummaryLines_maxLines零 maxLines <= 0 返回空
func TestLimitSummaryLines_maxLines零(t *testing.T) {
	result := limitSummaryLines("line1\nline2", 0)
	assert.Equal(t, "", result)
}

// TestTeamExtractJSONWithError_tryParseWithError 正则匹配后 tryParseWithError 路径
func TestTeamExtractJSONWithError_tryParseWithError(t *testing.T) {
	// 提供 JSON 对象但内含格式错误，触发 tryParseWithError
	raw := `some preamble {invalid json content} trailing text`
	result, errStr := teamExtractJSONWithError(raw)
	// 应尝试解析但失败
	assert.Nil(t, result)
	assert.NotEmpty(t, errStr)
}

// TestSkillExperienceOptimizer_Backward_LLM失败 GenerateRecords 返回错误
func TestSkillExperienceOptimizer_Backward_LLM失败(t *testing.T) {
	model := newMockSkillModel(t, func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		return nil, fmt.Errorf("LLM error")
	})
	policy := llm_resilience.LLMInvokePolicy{
		AttemptTimeoutSecs: 1,
		TotalBudgetSecs:    2,
		MaxAttempts:        1,
	}
	opt := NewSkillExperienceOptimizer(model, "test-model", "cn", policy)

	skillName := "fail_skill"
	skillNamePtr := skillName
	evoCtx := &experience.EvolutionContext{
		SkillName:    skillName,
		SkillContent: "content",
		Signals:      []signal.EvolutionSignal{{SignalType: "execution_failure", SkillName: &skillNamePtr}},
	}
	dummyOp := &dummyOperator{id: "skill_experience_fail_skill"}
	operators := map[string]operator.Operator{"skill_experience_fail_skill": dummyOp}
	config := map[string]any{"online_contexts": map[string]any{skillName: evoCtx}}
	opt.Bind(operators, []string{"experiences"}, config)

	err := opt.Backward(context.Background(), []*signal.EvolutionSignal{
		{SignalType: "execution_failure", SkillName: &skillNamePtr},
	})
	assert.NoError(t, err) // Backward 不返回错误，只是跳过失败的 operator
}

// TestTeamSkillExperienceOptimizer_Backward_LLM失败 GenerateRecords 返回错误
func TestTeamSkillExperienceOptimizer_Backward_LLM失败(t *testing.T) {
	model := newMockSkillModel(t, func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		return nil, fmt.Errorf("LLM error")
	})
	policy := llm_resilience.LLMInvokePolicy{
		AttemptTimeoutSecs: 1,
		TotalBudgetSecs:    2,
		MaxAttempts:        1,
	}
	opt := NewTeamSkillExperienceOptimizer(model, "test-model", "cn", "", policy, nil)

	skillName := "team_fail"
	skillNamePtr := skillName
	evoCtx := &experience.EvolutionContext{
		SkillName:    skillName,
		SkillContent: "content",
		Signals:      []signal.EvolutionSignal{{SignalType: "trajectory_issue", SkillName: &skillNamePtr}},
		Trajectory: &trajectory.Trajectory{
			Steps: []*trajectory.TrajectoryStep{{Kind: trajectory.StepKindTool}},
		},
	}
	dummyOp := &dummyOperator{id: "skill_experience_team_fail"}
	operators := map[string]operator.Operator{"skill_experience_team_fail": dummyOp}
	config := map[string]any{"online_contexts": map[string]any{skillName: evoCtx}}
	opt.Bind(operators, []string{"experiences"}, config)

	err := opt.Backward(context.Background(), []*signal.EvolutionSignal{
		{SignalType: "trajectory_issue", SkillName: &skillNamePtr},
	})
	assert.NoError(t, err)
}

// TestSkillExperienceOptimizer_GenerateRecords_英文 英文语言路径
func TestSkillExperienceOptimizer_GenerateRecords_英文(t *testing.T) {
	model := newMockSkillModel(t, func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		return llmschema.NewAssistantMessage(`[{"action":"append","section":"Troubleshooting","target":"body","content":"fix step"}]`), nil
	})
	opt := NewSkillExperienceOptimizer(model, "test-model", "en", GenerateRecordsLLMPolicy)

	skillNamePtr := "my_skill"
	evoCtx := &experience.EvolutionContext{
		SkillName:    "my_skill",
		SkillContent: "skill content",
		Signals:      []signal.EvolutionSignal{{SignalType: "execution_failure", SkillName: &skillNamePtr, Excerpt: "error"}},
		UserQuery:    "How to use",
	}

	records, err := opt.GenerateRecords(context.Background(), evoCtx)
	assert.NoError(t, err)
	assert.GreaterOrEqual(t, len(records), 1)
}

// TestSkillExperienceOptimizer_GenerateRecords_已有记录 已有演进记录时构建上下文
func TestSkillExperienceOptimizer_GenerateRecords_已有记录(t *testing.T) {
	model := newMockSkillModel(t, func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		return llmschema.NewAssistantMessage(`[{"action":"append","section":"Troubleshooting","target":"body","content":"new step"}]`), nil
	})
	opt := NewSkillExperienceOptimizer(model, "test-model", "cn", GenerateRecordsLLMPolicy)

	skillNamePtr := "my_skill"
	evoCtx := &experience.EvolutionContext{
		SkillName:    "my_skill",
		SkillContent: "skill content",
		Signals:      []signal.EvolutionSignal{{SignalType: "user_correction", SkillName: &skillNamePtr, Excerpt: "wrong answer"}},
		ExistingDescRecords: []checkpointing.EvolutionRecord{
			{ID: "ev_001", Change: checkpointing.EvolutionPatch{Section: "Description", Content: "旧描述"}},
		},
		ExistingBodyRecords: []checkpointing.EvolutionRecord{
			{ID: "ev_002", Change: checkpointing.EvolutionPatch{Section: "Instructions", Content: "旧指令"}},
		},
	}

	records, err := opt.GenerateRecords(context.Background(), evoCtx)
	assert.NoError(t, err)
	assert.GreaterOrEqual(t, len(records), 1)
}

// TestTeamSkillExperienceOptimizer_GenerateRecords_英文 英文语言聚合路径
func TestTeamSkillExperienceOptimizer_GenerateRecords_英文(t *testing.T) {
	model := newMockSkillModel(t, func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		return llmschema.NewAssistantMessage(`[{"action":"append","section":"Workflow","target":"body","content":"improve collaboration"}]`), nil
	})
	opt := NewTeamSkillExperienceOptimizer(model, "test-model", "en", "", TeamSkillRecordLLMPolicy, nil)

	skillNamePtr := "team_skill"
	evoCtx := &experience.EvolutionContext{
		SkillName:    "team_skill",
		SkillContent: "team skill content",
		Signals:      []signal.EvolutionSignal{{SignalType: "trajectory_issue", SkillName: &skillNamePtr, Excerpt: "failed"}},
		Trajectory: &trajectory.Trajectory{
			Steps: []*trajectory.TrajectoryStep{{Kind: trajectory.StepKindTool}},
		},
		UserQuery: "How to collaborate better",
	}

	records, err := opt.GenerateRecords(context.Background(), evoCtx)
	assert.NoError(t, err)
	assert.GreaterOrEqual(t, len(records), 1)
}

// TestTeamSkillExperienceOptimizer_GenerateRecords_已有记录 已有演进记录
func TestTeamSkillExperienceOptimizer_GenerateRecords_已有记录(t *testing.T) {
	model := newMockSkillModel(t, func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		return llmschema.NewAssistantMessage(`[{"action":"append","section":"Workflow","target":"body","content":"improve"}]`), nil
	})
	opt := NewTeamSkillExperienceOptimizer(model, "test-model", "cn", "", TeamSkillRecordLLMPolicy, nil)

	skillNamePtr := "team_skill"
	evoCtx := &experience.EvolutionContext{
		SkillName:    "team_skill",
		SkillContent: "team skill content",
		Signals:      []signal.EvolutionSignal{{SignalType: "trajectory_issue", SkillName: &skillNamePtr, Excerpt: "issue"}},
		Trajectory: &trajectory.Trajectory{
			Steps: []*trajectory.TrajectoryStep{{Kind: trajectory.StepKindTool}},
		},
		ExistingDescRecords:   []checkpointing.EvolutionRecord{{ID: "ev_1", Change: checkpointing.EvolutionPatch{Section: "Description"}}},
		ExistingBodyRecords:   []checkpointing.EvolutionRecord{{ID: "ev_2", Change: checkpointing.EvolutionPatch{Section: "Instructions"}}},
		ExistingScriptRecords: []checkpointing.EvolutionRecord{{ID: "ev_3", Change: checkpointing.EvolutionPatch{Section: "Scripts"}}},
	}

	records, err := opt.GenerateRecords(context.Background(), evoCtx)
	assert.NoError(t, err)
	assert.GreaterOrEqual(t, len(records), 1)
}

// TestTeamSkillExperienceOptimizer_GenerateRecords_逐信号用户意图 user_intent 信号走 GenerateUserPatch
func TestTeamSkillExperienceOptimizer_GenerateRecords_逐信号用户意图(t *testing.T) {
	model := newMockSkillModel(t, func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		return llmschema.NewAssistantMessage(`{"need_patch": true, "action": "append", "section": "Instructions", "content": "新增用户意图说明"}`), nil
	})
	opt := NewTeamSkillExperienceOptimizer(model, "test-model", "cn", "", TeamSkillRecordLLMPolicy, nil)

	skillNamePtr := "team_skill"
	evoCtx := &experience.EvolutionContext{
		SkillName:    "team_skill",
		SkillContent: "content",
		Signals:      []signal.EvolutionSignal{{SignalType: "user_intent", SkillName: &skillNamePtr, Excerpt: "更快完成分析"}},
		Trajectory: &trajectory.Trajectory{
			Steps: []*trajectory.TrajectoryStep{},
		},
		UserQuery: "快速分析",
	}

	_, err := opt.GenerateRecords(context.Background(), evoCtx)
	assert.NoError(t, err)
}

// TestTeamSkillExperienceOptimizer_GenerateUserPatch_LLM失败 LLM 失败返回错误
func TestTeamSkillExperienceOptimizer_GenerateUserPatch_LLM失败(t *testing.T) {
	model := newMockSkillModel(t, func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		return nil, fmt.Errorf("LLM unavailable")
	})
	policy := llm_resilience.LLMInvokePolicy{
		AttemptTimeoutSecs: 1,
		TotalBudgetSecs:    2,
		MaxAttempts:        1,
	}
	opt := NewTeamSkillExperienceOptimizer(model, "test-model", "cn", "", policy, nil)

	traj := &trajectory.Trajectory{ExecutionID: "exec-1", SessionID: "sess-1"}
	record, err := opt.GenerateUserPatch(context.Background(), traj, "team_skill", "用户意图")
	assert.Error(t, err)
	assert.Nil(t, record)
}

// TestTeamSkillExperienceOptimizer_GenerateUserPatch_空内容 patch 内容为空时报错
func TestTeamSkillExperienceOptimizer_GenerateUserPatch_空内容(t *testing.T) {
	model := newMockSkillModel(t, func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		return llmschema.NewAssistantMessage(`{"need_patch": true, "action": "append", "section": "Instructions", "content": ""}`), nil
	})
	opt := NewTeamSkillExperienceOptimizer(model, "test-model", "cn", "", TeamSkillRecordLLMPolicy, nil)

	traj := &trajectory.Trajectory{ExecutionID: "exec-1", SessionID: "sess-1"}
	record, err := opt.GenerateUserPatch(context.Background(), traj, "team_skill", "意图")
	assert.Error(t, err)
	assert.Nil(t, record)
}

// TestTeamSkillExperienceOptimizer_GenerateTrajectoryPatch_空内容 patch 内容为空时报错
func TestTeamSkillExperienceOptimizer_GenerateTrajectoryPatch_空内容(t *testing.T) {
	model := newMockSkillModel(t, func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		return llmschema.NewAssistantMessage(`{"need_patch": true, "action": "append", "section": "Workflow", "content": "  "}`), nil
	})
	opt := NewTeamSkillExperienceOptimizer(model, "test-model", "cn", "", TeamSkillRecordLLMPolicy, nil)

	traj := &trajectory.Trajectory{ExecutionID: "exec-1", SessionID: "sess-1"}
	record, err := opt.GenerateTrajectoryPatch(context.Background(), traj, "team_skill", "content", nil)
	assert.Error(t, err)
	assert.Nil(t, record)
}

// TestTeamSkillExperienceOptimizer_GenerateTrajectoryPatch_LLM失败 LLM 失败
func TestTeamSkillExperienceOptimizer_GenerateTrajectoryPatch_LLM失败(t *testing.T) {
	model := newMockSkillModel(t, func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		return nil, fmt.Errorf("service down")
	})
	policy := llm_resilience.LLMInvokePolicy{
		AttemptTimeoutSecs: 1,
		TotalBudgetSecs:    2,
		MaxAttempts:        1,
	}
	opt := NewTeamSkillExperienceOptimizer(model, "test-model", "cn", "", policy, nil)

	traj := &trajectory.Trajectory{ExecutionID: "exec-1"}
	record, err := opt.GenerateTrajectoryPatch(context.Background(), traj, "team_skill", "content", nil)
	assert.Error(t, err)
	assert.Nil(t, record)
}

// TestTruncateOrDefault_截断 截断超长字符串
func TestTruncateOrDefault_截断(t *testing.T) {
	result := truncateOrDefault(strings.Repeat("a", 100), 10, "无", "None")
	assert.Equal(t, "aaaaaaaaaa", result)
}

// TestBaseOptimizer_Bind_default类型 online_contexts 为未知类型
func TestBaseOptimizer_Bind_default类型(t *testing.T) {
	model := newMockSkillModel(t, nil)
	base := SkillExperienceOptimizerBase{llm: model, model: "test"}
	operators := map[string]operator.Operator{}
	config := map[string]any{
		"online_contexts": 123,
	}
	_ = base.Bind(operators, []string{"experiences"}, config)
	assert.Empty(t, base.onlineContexts)
}

// TestNewTeamSkillExperienceOptimizer_有EvolutionStore 构造函数传入 mockEvolutionStore
func TestNewTeamSkillExperienceOptimizer_有EvolutionStore(t *testing.T) {
	model := newMockSkillModel(t, nil)
	store := &mockEvolutionStore{
		readSkillContentFn: func(ctx context.Context, skillName string) (string, error) {
			return "mock skill content for " + skillName, nil
		},
		loadFullEvolutionLogFn: func(ctx context.Context, skillName string) (*checkpointing.EvolutionLog, error) {
			return &checkpointing.EvolutionLog{SkillID: skillName}, nil
		},
	}
	opt := NewTeamSkillExperienceOptimizer(model, "test-model", "cn", "", TeamSkillRecordLLMPolicy, store)
	assert.NotNil(t, opt)
	assert.NotNil(t, opt.evolutionStore)

	// 验证接口方法调用
	content, err := opt.evolutionStore.ReadSkillContent(context.Background(), "test_skill")
	assert.NoError(t, err)
	assert.Equal(t, "mock skill content for test_skill", content)

	log, err := opt.evolutionStore.LoadFullEvolutionLog(context.Background(), "test_skill")
	assert.NoError(t, err)
	assert.NotNil(t, log)
	assert.Equal(t, "test_skill", log.SkillID)
}

// TestEvolutionStore_接口对齐 验证 mockEvolutionStore 满足 EvolutionStore 接口
func TestEvolutionStore_接口对齐(t *testing.T) {
	store := &mockEvolutionStore{}
	// 编译时已通过 var _ EvolutionStore = (*mockEvolutionStore)(nil) 验证
	// 运行时验证默认行为
	content, err := store.ReadSkillContent(context.Background(), "any")
	assert.NoError(t, err)
	assert.Equal(t, "", content)

	log, err := store.LoadFullEvolutionLog(context.Background(), "any")
	assert.NoError(t, err)
	assert.Nil(t, log)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newMockSkillModel 创建一个使用 mock client 的 Model 实例（skill_call 测试专用）
func newMockSkillModel(t *testing.T, invokeFn func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error)) *llm.Model {
	t.Helper()

	mockClient := &mockBaseModelClient{invokeFn: invokeFn}
	providerName := fmt.Sprintf("mock_skill_call_%d", time.Now().UnixNano())
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

// mockEvolutionStore 用于测试的模拟 checkpointing.EvolutionStoreReader 接口实现
type mockEvolutionStore struct {
	readSkillContentFn     func(ctx context.Context, skillName string) (string, error)
	loadFullEvolutionLogFn func(ctx context.Context, skillName string) (*checkpointing.EvolutionLog, error)
}

func (m *mockEvolutionStore) ReadSkillContent(ctx context.Context, skillName string) (string, error) {
	if m.readSkillContentFn != nil {
		return m.readSkillContentFn(ctx, skillName)
	}
	return "", nil
}

func (m *mockEvolutionStore) LoadFullEvolutionLog(ctx context.Context, skillName string) (*checkpointing.EvolutionLog, error) {
	if m.loadFullEvolutionLogFn != nil {
		return m.loadFullEvolutionLogFn(ctx, skillName)
	}
	return nil, nil
}

var _ checkpointing.EvolutionStoreReader = (*mockEvolutionStore)(nil)

// dummyOperator 用于测试的最简 Operator 实现
type dummyOperator struct {
	id string
}

func (d *dummyOperator) OperatorID() string { return d.id }
func (d *dummyOperator) GetTunables() map[string]operator.TunableSpec {
	return map[string]operator.TunableSpec{
		"experiences": {Kind: operator.TunableKindText},
	}
}
func (d *dummyOperator) GetState() map[string]any              { return nil }
func (d *dummyOperator) SetParameter(target string, value any) {}
func (d *dummyOperator) ApplyUpdate(target string, update schema.UpdateValue) schema.ApplyResult {
	return schema.ApplyResult{Applied: true}
}
func (d *dummyOperator) LoadState(state map[string]any)                      {}
func (d *dummyOperator) Execute(ctx context.Context, input any) (any, error) { return nil, nil }
