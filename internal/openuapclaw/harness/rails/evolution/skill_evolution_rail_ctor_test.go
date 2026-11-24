package evolution

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/optimizer/llm_resilience"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm"
	llmschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// newTestLLMModel 构造一个可用的 Model 实例（不实际发请求，仅满足构造依赖）。
func newTestLLMModel(t *testing.T) *llm.Model {
	t.Helper()
	m, err := llm.NewModel(&llmschema.ModelClientConfig{
		ClientProvider: "OpenAI",
		ClientID:       "test-client",
		APIKey:         "k",
		APIBase:        "http://localhost:1",
	}, nil)
	require.NoError(t, err, "构造 Model 失败")
	return m
}

// newConstructedRail 用指定选项构造完整的 SkillEvolutionRail。
func newConstructedRail(t *testing.T, opts ...SkillEvolutionRailOption) *SkillEvolutionRail {
	t.Helper()
	return NewSkillEvolutionRail([]string{t.TempDir()}, newTestLLMModel(t), "test-model", "cn", opts...)
}

// ──────────────────────────── 构造函数测试 ────────────────────────────

// TestNewSkillEvolutionRail 验证构造后的默认值与核心组件全部就绪。
func TestNewSkillEvolutionRail(t *testing.T) {
	r := newConstructedRail(t)
	require.NotNil(t, r)

	// 默认开关：signal_trigger 缺省 False（Python 新版 :242 bool(None)→False）
	assert.False(t, r.SignalTrigger(), "默认 signal_trigger 应为 false")
	assert.True(t, r.AutoSave(), "默认 autoSave 应为 true")
	assert.Equal(t, 80, r.Priority())

	// 核心组件全部非 nil
	assert.NotNil(t, r.EvolutionStore(), "EvolutionStore 不应为 nil")
	assert.NotNil(t, r.Scorer(), "Scorer 不应为 nil")
	assert.NotNil(t, r.Evolver(), "Evolver 不应为 nil")
	assert.NotNil(t, r.Manager(), "Manager 不应为 nil")
	assert.NotNil(t, r.ApprovalRuntime(), "ApprovalRuntime 不应为 nil")
	assert.Equal(t, "test-model", r.ModelName())
	assert.NotNil(t, r.ProcessedSignalKeys(), "processedSignalKeys 不应为 nil")
	assert.NotNil(t, r.EvolutionConfig(), "EvolutionConfig 不应为 nil")
}

// TestSkillEvolutionRailOptions 验证各 Functional Option 的赋值与边界钳制。
func TestSkillEvolutionRailOptions(t *testing.T) {
	t.Run("autoScan/autoSave 可关闭", func(t *testing.T) {
		r := newConstructedRail(t, WithSignalTrigger(false), WithAutoSave(false))
		assert.False(t, r.SignalTrigger())
		assert.False(t, r.AutoSave())
	})

	t.Run("evalInterval 小于 1 时钳到 1", func(t *testing.T) {
		r := newConstructedRail(t, WithEvalInterval(0))
		assert.Equal(t, 1, r.evalInterval, "evalInterval 应被钳到 1")
		r2 := newConstructedRail(t, WithEvalInterval(7))
		assert.Equal(t, 7, r2.evalInterval)
	})

	t.Run("evolutionTimeout 直接赋值", func(t *testing.T) {
		r := newConstructedRail(t, WithEvolutionTimeout(12.5))
		assert.InDelta(t, 12.5, r.evolutionTimeoutSec, 1e-9)
	})

	t.Run("三种 LLM 策略可替换", func(t *testing.T) {
		policy := llm_resilience.LLMInvokePolicy{AttemptTimeoutSecs: 5, TotalBudgetSecs: 20, MaxAttempts: 3}
		r := newConstructedRail(t,
			WithGenerateRecordsLLMPolicy(policy),
			WithEvaluateLLMPolicy(policy),
			WithSimplifyLLMPolicy(policy),
		)
		assert.Equal(t, policy, r.generateRecordsLLMPolicy)
		assert.Equal(t, policy, r.evaluateLLMPolicy)
		assert.Equal(t, policy, r.simplifyLLMPolicy)
	})

	t.Run("sharingConfig 透传", func(t *testing.T) {
		cfg := map[string]any{"enabled": false}
		r := newConstructedRail(t, WithSharingConfigMap(cfg))
		assert.Equal(t, cfg, r.sharingConfig)
		assert.False(t, r.IsSharingEnabled(), "enabled=false 时不应开启共享")
	})

	t.Run("disabledSkillsSet 同步子类与基类", func(t *testing.T) {
		r := newConstructedRail(t, WithDisabledSkillsSet([]string{"skill-a", "skill-b"}))
		assert.Equal(t, []string{"skill-a", "skill-b"}, r.disabledSkills)
		require.NotNil(t, r.EvolutionRail)
		// 基类把名单归一化为集合
		assert.Equal(t, map[string]bool{"skill-a": true, "skill-b": true}, r.EvolutionRail.disabledSkills)
	})
}

// TestSkillEvolutionRailHooks 验证各回调扩展点与触发判定。
func TestSkillEvolutionRailHooks(t *testing.T) {
	ctx := context.Background()
	r := newConstructedRail(t)
	cbc := &agentinterfaces.AgentCallbackContext{}

	assert.NoError(t, r.OnBeforeInvoke(ctx, cbc))
	assert.NoError(t, r.OnAfterModelCall(ctx, cbc))
	assert.NoError(t, r.OnAfterInvoke(ctx, cbc))
	assert.NoError(t, r.OnAfterTaskIteration(ctx, cbc))
	assert.NoError(t, r.OnAfterEvolutionTriggered(ctx, nil, cbc))

	t.Run("signalTrigger 控制触发", func(t *testing.T) {
		// Python 新版 _allow_evolution_trigger：signal_trigger 门 +
		// 后台运行排除 + skip 标记；缺省 False → 不触发。
		assert.False(t, r.AllowEvolutionTrigger(TriggerAfterInvoke, cbc))
		r.SetSignalTrigger(true)
		assert.True(t, r.AllowEvolutionTrigger(TriggerAfterInvoke, cbc))
		r.skipSignalTriggerThisInvoke = true
		assert.False(t, r.AllowEvolutionTrigger(TriggerAfterInvoke, cbc), "活动演化活动应跳过被动扫描")
		r.SetSignalTrigger(false)
	})

	t.Run("inputs 类型不符时 OnAfterToolCall 直接返回", func(t *testing.T) {
		assert.NoError(t, r.OnAfterToolCall(ctx, cbc))
	})

	t.Run("ClearProcessedSignals 清空记录", func(t *testing.T) {
		r.ProcessedSignalKeys()[[4]string{"a", "b", "c", "d"}] = true
		r.ClearProcessedSignals()
		assert.Empty(t, r.ProcessedSignalKeys())
	})
}
