package recommendation

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/kv_cache"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/agents"
	saconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/config"
	sainterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// lineageTestSession 满足 SessionFacade 与 kv_cache 的超集，供血缘与轮次解析。
type lineageTestSession struct {
	id, parentID string
	cacheID      string
	parentCache  string
	env          map[string]any
	stateMap     map[string]any
}

func (s *lineageTestSession) GetSessionID() string { return s.id }
func (s *lineageTestSession) GetCacheIdentity() kv_cache.KVCacheIdentity {
	return kv_cache.KVCacheIdentity{CacheID: s.cacheID, ParentCacheID: s.parentCache}
}
func (s *lineageTestSession) GetParentSessionID() string { return s.parentID }
func (s *lineageTestSession) GetEnv(k string, _ ...any) any {
	return s.env[k]
}
func (s *lineageTestSession) UpdateState(d map[string]any) {
	for k, v := range d {
		s.stateMap[k] = v
	}
}
func (s *lineageTestSession) GetState(_ state.StateKey) (any, error) { return nil, nil }
func (s *lineageTestSession) DumpState() map[string]any              { return s.stateMap }
func (s *lineageTestSession) WriteStream(_ context.Context, _ any) error {
	return nil
}
func (s *lineageTestSession) WriteCustomStream(_ context.Context, _ any) error { return nil }
func (s *lineageTestSession) Interact(_ context.Context, _ any) error          { return nil }
func (s *lineageTestSession) ClearSession(_ context.Context)                   {}

var _ sessioninterfaces.SessionFacade = (*lineageTestSession)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestForkRail_Priority_极小值最后读
// ★ Go 的 callback.go:142「数值越大越先执行」，故 -1_000_000 才是最后读。
// Python: prompt_suggestion.py:552 priority = -1_000_000
func TestForkRail_Priority_极小值最后读(t *testing.T) {
	assert.Equal(t, -1_000_000, NewPromptSuggestionForkRail().Priority())
	// 与默认 rail priority（50）相比必须更小，才能排在其后
	assert.Less(t, NewPromptSuggestionForkRail().Priority(), 50)
}

// TestForkRail_AfterModelCall_恒返回nil 退化上下文不许报错。
// Python: prompt_suggestion.py:555「An error here would fail the answer」
func TestForkRail_AfterModelCall_恒返回nil(t *testing.T) {
	rail := NewPromptSuggestionForkRail()
	assert.NoError(t, rail.AfterModelCall(context.Background(), nil))
	assert.NoError(t, rail.AfterModelCall(context.Background(), &sainterfaces.AgentCallbackContext{}))
}

// TestMainCallParameters_与父调用逐字一致 缓存命中的唯一保障。
func TestMainCallParameters_与父调用逐字一致(t *testing.T) {
	hook := kv_cache.NewKVCacheModelCallHook()
	llmModel := newAffinityTestModel(true)
	cfg := &kv_cache.KVCacheAffinityConfig{EnableKVCacheAffinity: true}
	sess := &lineageTestSession{
		id: "s1", cacheID: "cid", parentCache: "pid",
		env: map[string]any{}, stateMap: map[string]any{},
	}

	// 父侧（react_model_call 的等价步骤）
	runtime := hook.ResolveRuntime(llmModel, cfg)
	kvSess := newRecommendationLineageSession(sess)
	parentSID, parentParentSID := hook.ResolveLineage(runtime, kvSess, "fb")
	// 两条路径都吃宿主的 _turn_number，避免 state 自增导致假红
	hostExtra := map[string]any{"_turn_number": 3}
	parentTurn := hook.ResolveTurnNum(hostExtra, kvSess)
	parentKwargs := hook.BuildInvokeKwargs(runtime, llmModel, kvSess,
		parentSID, parentParentSID, parentTurn)

	// 重放侧
	replayParams := MainCallParameters(hook, llmModel, cfg, sess, "fb",
		map[string]any{"_turn_number": 3}, sainterfaces.GenerationOptions{})

	require.NotEmpty(t, parentKwargs, "开启亲和时父请求必须真带 kwargs")
	assert.Equal(t, parentKwargs, replayParams,
		"父子 kwargs 不一致 → cache key 失配，重放退化为全价调用")
}

// TestMainCallParameters_关闭时为空 未开亲和不得注入任何字段。
func TestMainCallParameters_关闭时为空(t *testing.T) {
	hook := kv_cache.NewKVCacheModelCallHook()
	llmModel := newAffinityTestModel(true)
	sess := &lineageTestSession{id: "s1", env: map[string]any{}, stateMap: map[string]any{}}
	assert.Empty(t, MainCallParameters(hook, llmModel, nil, sess, "fb", map[string]any{},
		sainterfaces.GenerationOptions{}))
	assert.Empty(t, MainCallParameters(hook, llmModel,
		&kv_cache.KVCacheAffinityConfig{EnableKVCacheAffinity: false},
		sess, "fb", map[string]any{}, sainterfaces.GenerationOptions{}))
}

// TestMainCallParameters_过度降级不panic nil 依赖直接返回空 map。
// Python: prompt_suggestion.py:307 except Exception: logger.debug(...)
func TestMainCallParameters_过度降级不panic(t *testing.T) {
	assert.Empty(t, MainCallParameters(nil, nil, nil, nil, "", nil, sainterfaces.GenerationOptions{}))
	assert.NotPanics(t, func() {
		MainCallParameters(nil, nil, nil, nil, "", nil, sainterfaces.GenerationOptions{})
	})
}

// affinityFakeLLM 满足 kv_cache.AffinityLLM 的最小实现（测试用）。
type affinityFakeLLM struct{ affinity bool }

// SupportsKVCacheAffinity 返回构造时指定的能力。
func (f *affinityFakeLLM) SupportsKVCacheAffinity() bool { return f.affinity }

// BuildKVCacheAffinityInvokeKwargs 返回四字段亲和参数或空 map。
func (f *affinityFakeLLM) BuildKVCacheAffinityInvokeKwargs(
	_ any, sessionID, parentSessionID string, turnNum int,
) map[string]any {
	if !f.affinity {
		return map[string]any{}
	}
	return map[string]any{
		"session_id":               sessionID,
		"parent_session_id":        parentSessionID,
		"turn_num":                 turnNum,
		"enable_kv_cache_affinity": true,
	}
}

// newAffinityTestModel 构造支持亲和探测的假客户端。
func newAffinityTestModel(affinity bool) *affinityFakeLLM {
	return &affinityFakeLLM{affinity: affinity}
}

// planStateStub 满足 planStateLoader，返回预置状态。
type planStateStub struct {
	sainterfaces.BaseAgent
	state *hschema.DeepAgentState
}

// LoadState 返回预置的会话状态。
func (s *planStateStub) LoadState(_ sessioninterfaces.SessionFacade) *hschema.DeepAgentState {
	return s.state
}

// TestResolvePlanning_三态 对齐 prompt_suggestion.py:561。
func TestResolvePlanning_三态(t *testing.T) {
	sess := &lineageTestSession{id: "s1", env: map[string]any{}, stateMap: map[string]any{}}

	// ① plan 模式 → true
	planAgent := &planStateStub{state: &hschema.DeepAgentState{
		PlanMode: hschema.PlanModeState{Mode: "plan"},
	}}
	assert.True(t, resolvePlanning(planAgent, sess), "plan_mode.mode == plan 应为 true")

	// ② 非 plan 模式 → false
	normalAgent := &planStateStub{state: &hschema.DeepAgentState{
		PlanMode: hschema.PlanModeState{Mode: "normal"},
	}}
	assert.False(t, resolvePlanning(normalAgent, sess))

	// ③ 不实现 planStateLoader 的 agent → false（Python: _load_state is None）
	assert.False(t, resolvePlanning(&plainAgentStub{}, sess),
		"取不到 load_state 时必须为 false")
	// ④ state 为 nil → false
	assert.False(t, resolvePlanning(&planStateStub{}, sess))
}

// plainAgentStub 只实现 BaseAgent，不含 LoadState。
type plainAgentStub struct {
	sainterfaces.BaseAgent
}

// TestResolveGenerationOptions_真实ReActAgent能取到 防止两个同构类型导致断言静默失败。
func TestResolveGenerationOptions_真实ReActAgent能取到(t *testing.T) {
	agent := newReActAgentWithGenerationOptionsForTest()
	got := resolveGenerationOptions(agent)
	assert.True(t, got.ReturnTokenIDs, "llm_return_token_ids 必须能读到")
	assert.True(t, got.Logprobs, "llm_logprobs 必须能读到")
	assert.Equal(t, 7, got.TopLogprobs, "llm_top_logprobs 必须能读到")
}

// TestResolveMaxIterations_真实ReActAgent能取到 同上。
func TestResolveMaxIterations_真实ReActAgent能取到(t *testing.T) {
	agent := newReActAgentWithGenerationOptionsForTest()
	assert.Equal(t, 9, resolveMaxIterations(agent))
}

// TestMainCallParameters_带生成选项 三项必须在 affinity 之前写入。
func TestMainCallParameters_带生成选项(t *testing.T) {
	hook := kv_cache.NewKVCacheModelCallHook()
	sess := &lineageTestSession{id: "s1", env: map[string]any{}, stateMap: map[string]any{}}
	got := MainCallParameters(hook, newAffinityTestModel(false), nil, sess, "fb",
		map[string]any{},
		sainterfaces.GenerationOptions{ReturnTokenIDs: true, Logprobs: true, TopLogprobs: 5})
	assert.Equal(t, true, got["return_token_ids"])
	assert.Equal(t, true, got["logprobs"])
	assert.Equal(t, 5, got["top_logprobs"])
	assert.NotContains(t, got, "session_id", "亲和关闭时不应有亲和字段")
}

// newReActAgentWithGenerationOptionsForTest 造一个带生成选项配置的真实 ReActAgent。
// 用真实构造器，确保测的是真实类型而不是手写替身——两个同构类型导致的
// 类型断言静默失败只有用真实类型才能暴露。
func newReActAgentWithGenerationOptionsForTest() sainterfaces.BaseAgent {
	cfg := &saconfig.ReActAgentConfig{
		LLMReturnTokenIDs: true,
		LLMLogprobs:       true,
		LLMTopLogprobs:    7,
		MaxIterations:     9,
	}
	return agents.NewReActAgent(agentschema.NewAgentCard(
		agentschema.WithAgentID("test"), agentschema.WithAgentName("test"),
	), cfg)
}
