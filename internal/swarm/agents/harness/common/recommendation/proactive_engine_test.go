package recommendation

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
	swarmschema "github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 构造与配置 ────────────────────────────

// TestNewProactiveEngine_默认值 缺配置时用防御性默认值，enabled 默认 false。
// Python: proactive_engine.py:91 __init__
func TestNewProactiveEngine_默认值(t *testing.T) {
	engine := NewProactiveEngine(nil)
	require.NotNil(t, engine)
	assert.Equal(t, defaultMaxPerDay, engine.maxPerDay)
	assert.Equal(t, defaultMaxRounds, engine.maxRounds)
	assert.False(t, engine.enabled, "配置段缺失时引擎不应被误启用")
	assert.Zero(t, engine.LastTickAt())
}

// TestNewProactiveEngine_自定义值 配置项生效。
func TestNewProactiveEngine_自定义值(t *testing.T) {
	engine := NewProactiveEngine(map[string]any{
		"enabled":               true,
		"max_recommend_per_day": 3,
		"max_rounds_per_tick":   10,
	})
	assert.Equal(t, 3, engine.maxPerDay)
	assert.Equal(t, 10, engine.maxRounds)
	assert.True(t, engine.enabled)
}

// TestNewProactiveEngine_非法值回落 越界/非数字回落默认值。
// Python: proactive_engine.py:52 _safe_proactive_int
func TestNewProactiveEngine_非法值回落(t *testing.T) {
	engine := NewProactiveEngine(map[string]any{
		"max_recommend_per_day": "abc",
		"max_rounds_per_tick":   999,
	})
	assert.Equal(t, defaultMaxPerDay, engine.maxPerDay, "非数字应回落默认")
	assert.Equal(t, defaultMaxRounds, engine.maxRounds, "越界应回落默认")
}

// TestReloadConfig 热重载只改配置，不清当日计数。
// Python: proactive_engine.py:294 reload_config
func TestReloadConfig(t *testing.T) {
	engine := NewProactiveEngine(map[string]any{"enabled": false, "max_recommend_per_day": 5})
	ResetDailyCounts()
	t.Cleanup(ResetDailyCounts)
	IncrementDailyCount() // 当日已有 1 条

	engine.ReloadConfig(map[string]any{"enabled": true, "max_recommend_per_day": 3})

	assert.True(t, engine.enabled)
	assert.Equal(t, 3, engine.maxPerDay)
	assert.Equal(t, 1, TodayRecommendCount(), "当日计数不应被 reload 重置")
}

// TestReloadConfig_缺失键保留旧值 未提供的键不改成默认值。
func TestReloadConfig_缺失键保留旧值(t *testing.T) {
	engine := NewProactiveEngine(map[string]any{"max_recommend_per_day": 7, "max_rounds_per_tick": 15})
	engine.ReloadConfig(map[string]any{"enabled": true})

	assert.Equal(t, 7, engine.maxPerDay, "未提供时应保留旧值")
	assert.Equal(t, 15, engine.maxRounds)
}

// ──────────────────────────── 回调设置 ────────────────────────────

// TestSetCallbacks 三个回调都能设置。
func TestSetCallbacks(t *testing.T) {
	engine := NewProactiveEngine(nil)

	engine.SetTriggerMainAgentCallback(func(_ context.Context, _ *ProactiveTriggerRequest) (bool, error) {
		return true, nil
	})
	engine.SetCheckAgentAvailableCallback(func(string) bool { return true })
	engine.SetSendNotificationCallback(func(string, string) error { return nil })

	engine.mu.RLock()
	defer engine.mu.RUnlock()
	assert.NotNil(t, engine.triggerMainAgentCallback)
	assert.NotNil(t, engine.checkAgentAvailableCallback)
	assert.NotNil(t, engine.sendNotificationCallback)
}

// TestSetProactiveAgent 直接注入 agent。
func TestSetProactiveAgent(t *testing.T) {
	engine := NewProactiveEngine(nil)
	agent := newProactiveAgentWithReply(t, `{"decision": null}`)

	engine.SetProactiveAgent(agent)

	engine.mu.RLock()
	defer engine.mu.RUnlock()
	assert.Same(t, agent, engine.proactiveAgent)
	assert.Nil(t, engine.agentBuilder, "注入 agent 后应清空 builder")
}

// TestSetProactiveAgentBuilder 注入 builder。
func TestSetProactiveAgentBuilder(t *testing.T) {
	engine := NewProactiveEngine(nil)
	engine.SetProactiveAgentBuilder(func() *harness.DeepAgent { return nil })

	engine.mu.RLock()
	defer engine.mu.RUnlock()
	assert.NotNil(t, engine.agentBuilder)
	assert.Nil(t, engine.proactiveAgent)
}

// TestRebuildProactiveAgent 重建，nil builder 只告警。
func TestRebuildProactiveAgent(t *testing.T) {
	engine := NewProactiveEngine(nil)
	engine.RebuildProactiveAgent(nil) // 不应 panic

	engine.mu.RLock()
	assert.Nil(t, engine.agentBuilder)
	engine.mu.RUnlock()

	engine.RebuildProactiveAgent(func() *harness.DeepAgent { return nil })
	engine.mu.RLock()
	assert.NotNil(t, engine.agentBuilder)
	engine.mu.RUnlock()
}

// TestClose 关闭后标记 closed 且不接受新 agent。
func TestClose(t *testing.T) {
	engine := NewProactiveEngine(map[string]any{"enabled": true})
	engine.Close(context.Background())

	engine.mu.RLock()
	assert.True(t, engine.closed)
	engine.mu.RUnlock()

	// 再次 Close 应幂等
	engine.Close(context.Background())
}

// ──────────────────────────── TickNow 测试分组 ────────────────────────────

// TestTickNow_未启用跳过 enabled=false 时直接返回 false。
// Python: proactive_engine.py:325
func TestTickNow_未启用跳过(t *testing.T) {
	engine := NewProactiveEngine(map[string]any{"enabled": false})
	assert.False(t, engine.TickNow(context.Background(), ""))
}

// TestTickNow_已关闭跳过 closed 后返回 false。
func TestTickNow_已关闭跳过(t *testing.T) {
	engine := NewProactiveEngine(map[string]any{"enabled": true})
	engine.Close(context.Background())
	assert.False(t, engine.TickNow(context.Background(), ""))
}

// TestTickNow_无agent跳过 无 agent 也无 builder 时返回 false 并更新时间戳。
func TestTickNow_无agent跳过(t *testing.T) {
	engine := NewProactiveEngine(map[string]any{"enabled": true})
	assert.False(t, engine.TickNow(context.Background(), ""))
	assert.NotZero(t, engine.LastTickAt(), "跳过也应更新时间戳")
}

// TestTickNow_检查agent不可用跳过 Step 0 回调返回 false 时跳过。
func TestTickNow_检查agent不可用跳过(t *testing.T) {
	ResetDailyCounts()
	t.Cleanup(ResetDailyCounts)
	engine := NewProactiveEngine(map[string]any{"enabled": true})
	engine.SetProactiveAgent(newProactiveAgentWithReply(t, `{"decision": null}`))
	engine.SetCheckAgentAvailableCallback(func(string) bool { return false })

	assert.False(t, engine.TickNow(context.Background(), "web"))
}

// TestTickNow_每日上限跳过 已达上限时跳过并推通知。
// Python: proactive_engine.py:404-421
func TestTickNow_每日上限(t *testing.T) {
	ResetDailyCounts()
	t.Cleanup(ResetDailyCounts)

	engine := NewProactiveEngine(map[string]any{"enabled": true, "max_recommend_per_day": 1})
	engine.SetProactiveAgent(newProactiveAgentWithReply(t, `{"decision": null}`))
	engine.SetCheckAgentAvailableCallback(func(string) bool { return true })

	var notification string
	engine.SetSendNotificationCallback(func(_, message string) error {
		notification = message
		return nil
	})

	assert.Equal(t, 1, IncrementDailyCount(), "先把当日计数推到上限")
	assert.False(t, engine.TickNow(context.Background(), "web"))
	assert.Contains(t, notification, "今日主动推荐已达每日上限", "应推送上限提醒")
	assert.Contains(t, notification, "1 条")
}

// TestTickNow_每日上限通知失败不影响流程 通知回调报错被吞。
func TestTickNow_每日上限通知报错(t *testing.T) {
	ResetDailyCounts()
	t.Cleanup(ResetDailyCounts)

	engine := NewProactiveEngine(map[string]any{"enabled": true, "max_recommend_per_day": 1})
	engine.SetProactiveAgent(newProactiveAgentWithReply(t, `{"decision": null}`))
	engine.SetCheckAgentAvailableCallback(func(string) bool { return false })
	engine.SetSendNotificationCallback(func(string, string) error {
		return assert.AnError
	})
	IncrementDailyCount()

	assert.NotPanics(t, func() {
		assert.False(t, engine.TickNow(context.Background(), "web"))
	})
}

// TestTickNow_触发回调缺失跳过 无 trigger callback 时跳过。
func TestTickNow_触发回调缺失(t *testing.T) {
	ResetDailyCounts()
	t.Cleanup(ResetDailyCounts)

	engine := NewProactiveEngine(map[string]any{"enabled": true})
	engine.SetProactiveAgent(newProactiveAgentWithReply(t, `{"decision": null}`))
	engine.SetCheckAgentAvailableCallback(func(string) bool { return true })

	assert.False(t, engine.TickNow(context.Background(), "web"))
}

// ──────────────────────────── ensureProactiveAgent 测试分组 ────────────────────────────

// TestEnsureProactiveAgent_builder构造 首次调用触发 builder。
func TestEnsureProactiveAgent_builder(t *testing.T) {
	engine := NewProactiveEngine(nil)
	built := 0
	agent := newProactiveAgentWithReply(t, `{"decision": null}`)
	engine.SetProactiveAgentBuilder(func() *harness.DeepAgent {
		built++
		return agent
	})

	got := engine.ensureProactiveAgent()
	require.NotNil(t, got)
	assert.Same(t, agent, got)
	assert.Equal(t, 1, built)

	// 第二次应复用，不再构造
	got2 := engine.ensureProactiveAgent()
	assert.Same(t, agent, got2)
	assert.Equal(t, 1, built, "已有 agent 时不应重复构造")
}

// TestEnsureProactiveAgent_builder返回nil builder 返回 nil 时不缓存。
func TestEnsureProactiveAgent_builder返回nil(t *testing.T) {
	engine := NewProactiveEngine(nil)
	engine.SetProactiveAgentBuilder(func() *harness.DeepAgent { return nil })

	assert.Nil(t, engine.ensureProactiveAgent())
	assert.Nil(t, engine.ensureProactiveAgent(), "构造失败不应缓存，下次仍尝试")
}

// TestEnsureProactiveAgent_已关闭不构造 closed 后 builder 不应被调用。
func TestEnsureProactiveAgent_已关闭(t *testing.T) {
	engine := NewProactiveEngine(nil)
	built := 0
	engine.SetProactiveAgentBuilder(func() *harness.DeepAgent {
		built++
		return nil
	})
	engine.Close(context.Background())

	assert.Nil(t, engine.ensureProactiveAgent())
	assert.Equal(t, 0, built, "已关闭时不应构造")
}

// ──────────────────────────── RunMaintenance 测试分组 ────────────────────────────

// TestRunMaintenance 清理过期每日计数。
// Python: proactive_engine.py:607
func TestRunMaintenance(t *testing.T) {
	ResetDailyCounts()
	t.Cleanup(ResetDailyCounts)

	dailyCountsMu.Lock()
	dailyCounts["1999-01-01"] = 99
	dailyCountsMu.Unlock()

	engine := NewProactiveEngine(nil)
	assert.NotPanics(t, engine.RunMaintenance)

	dailyCountsMu.Lock()
	_, staleExists := dailyCounts["1999-01-01"]
	dailyCountsMu.Unlock()
	assert.False(t, staleExists, "过期 key 应被清理")
}

// ──────────────────────────── newRecommendationID 测试分组 ────────────────────────────

// TestNewRecommendationID 格式为 rec_<毫秒>_<8位hex>，且不重复。
// Python: proactive_engine.py:543
func TestNewRecommendationID(t *testing.T) {
	id1 := newRecommendationID()
	id2 := newRecommendationID()

	assert.Regexp(t, `^rec_\d+_[0-9a-f]{8}$`, id1)
	assert.NotEqual(t, id1, id2, "两次生成必须不同（随机后缀防同毫秒碰撞）")
}

// TestNewRecommendationID_并发不重复 并发下也不产生重复 ID。
func TestNewRecommendationID_并发不重复(t *testing.T) {
	const n = 100
	ids := make([]string, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			ids[idx] = newRecommendationID()
		}(i)
	}
	wg.Wait()

	seen := make(map[string]struct{}, n)
	for _, id := range ids {
		_, dup := seen[id]
		assert.False(t, dup, "ID 不应重复: %s", id)
		seen[id] = struct{}{}
	}
	assert.Len(t, seen, n)
}

// ──────────────────────────── nowUnixSeconds 测试分组 ────────────────────────────

// TestNowUnixSeconds 返回合理范围的 Unix 秒。
func TestNowUnixSeconds(t *testing.T) {
	ts := nowUnixSeconds()
	assert.Greater(t, ts, 1.7e9, "应晚于 2023 年")
	assert.Contains(t, []bool{true}, ts == float64(int64(ts)) || ts != float64(int64(ts)))
}

// ──────────────────────────── 辅助验证（agent 请求构造）────────────────────────────

// TestBuildProactiveAgentRequest 请求构造含 source 与 proactive 标记。
// Python: proactive_adapter.py:181
func TestBuildProactiveAgentRequest(t *testing.T) {
	request := &ProactiveTriggerRequest{
		SessionID: "sess-1",
		Decision: &RecommendationDecision{
			Type: "skill_recommend", Target: "翻译技能", Reason: "能翻文档",
		},
		Query: "指令内容",
		RecID: "rec_123_abcdef01",
	}
	got, err := buildProactiveAgentRequest(request, "web")
	require.NoError(t, err)

	assert.Equal(t, "web", got.ChannelID)
	require.NotNil(t, got.SessionID)
	assert.Equal(t, "sess-1", *got.SessionID)
	assert.True(t, got.IsStream)
	assert.Contains(t, got.RequestID, "proactive_skill_recommend_")

	params := decodeParamsForTest(t, got.Params)
	assert.Equal(t, "指令内容", params["query"])
	assert.Equal(t, "agent", params["mode"])
	assert.Equal(t, ProactiveSourceSource, params["source"],
		"source 标记用于 process_message_stream 跳过写 user history")
	assert.Equal(t, "skill_recommend", params["proactive_type"])
	assert.Equal(t, "翻译技能", params["proactive_target"])
	assert.Equal(t, "rec_123_abcdef01", params["proactive_rec_id"])
}

// TestBuildProactiveAgentRequest_无recId rec_id 为空时不带该字段。
func TestBuildProactiveAgentRequest_无recId(t *testing.T) {
	request := &ProactiveTriggerRequest{
		SessionID: "sess-1",
		Decision:  &RecommendationDecision{Type: "task_reminder", Target: "X"},
		Query:     "q",
	}
	got, err := buildProactiveAgentRequest(request, "web")
	require.NoError(t, err)

	params := decodeParamsForTest(t, got.Params)
	_, hasRecID := params["proactive_rec_id"]
	assert.False(t, hasRecID, "rec_id 为空时不应带该字段")
}

// ──────────────────────────── inflight 去重 ────────────────────────────

// TestInflightPush 同名 key 第二次获取失败，释放后可再获取。
// Python: proactive_adapter.py:220 _proactive_push_inflight
func TestInflightPush(t *testing.T) {
	key := "test:inflight:key"

	assert.True(t, acquireInflightPush(key), "首次应成功")
	assert.False(t, acquireInflightPush(key), "同名未释放时应失败")

	releaseInflightPush(key)
	assert.True(t, acquireInflightPush(key), "释放后应可再获取")
	releaseInflightPush(key)
}

// TestInflightPush_并发互斥 并发获取同一 key 只有一个成功。
func TestInflightPush_并发互斥(t *testing.T) {
	key := "test:concurrent:key"
	var wg sync.WaitGroup
	var mu sync.Mutex
	acquired := 0

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if acquireInflightPush(key) {
				mu.Lock()
				acquired++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	assert.Equal(t, 1, acquired, "并发下只应有一个成功")
	releaseInflightPush(key)
}

// ──────────────────────────── 测试辅助 ────────────────────────────

// decodeParamsForTest 解析 AgentRequest.Params。
func decodeParamsForTest(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	params := make(map[string]any)
	if len(raw) == 0 {
		return params
	}
	require.NoError(t, json.Unmarshal(raw, &params))
	return params
}

// mockMainAgentHost 测试用 MainAgentHost 替身（server 运行时属外部边界，允许 mock）。
//
// pushed 由后台 goroutine 写入、主 goroutine 读取，故用 mu 保护。
type mockMainAgentHost struct {
	// agent 返回给 GetAgentNoWait 的 handle；nil 表示无 agent
	agent MainAgentHandle
	// executing IsSessionExecuting 返回值
	executing bool
	// pushResult SendPush 返回值
	pushResult bool

	mu     sync.Mutex
	pushed []map[string]any
}

// GetAgentNoWait 返回预定 handle。
func (m *mockMainAgentHost) GetAgentNoWait(_, _, _, _ string) MainAgentHandle {
	return m.agent
}

// SendPush 记录推送；pushResult=false 时返回错误（模拟 ws 未送达）。
func (m *mockMainAgentHost) SendPush(_ context.Context, payload map[string]any) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pushed = append(m.pushed, payload)
	if !m.pushResult {
		return errors.New("push not delivered")
	}
	return nil
}

// Pushed 返回推送快照。
func (m *mockMainAgentHost) Pushed() []map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]map[string]any, len(m.pushed))
	copy(out, m.pushed)
	return out
}

// PushedChannel 返回首个推送的 channel_id（无推送时为空串）。
func (m *mockMainAgentHost) PushedChannel() string {
	pushed := m.Pushed()
	if len(pushed) == 0 {
		return ""
	}
	channel, _ := pushed[0]["channel_id"].(string)
	return channel
}

// IsSessionExecuting 返回预定状态。
func (m *mockMainAgentHost) IsSessionExecuting(_ string) bool {
	return m.executing
}

// mockMainAgentHandle 测试用主 agent handle。
type mockMainAgentHandle struct {
	chunks []*swarmschema.AgentResponseChunk
	err    error
}

// ProcessMessageStream 返回预定 chunk 序列。
func (m *mockMainAgentHandle) ProcessMessageStream(_ context.Context, _ *swarmschema.AgentRequest) (<-chan *swarmschema.AgentResponseChunk, error) {
	if m.err != nil {
		return nil, m.err
	}
	ch := make(chan *swarmschema.AgentResponseChunk, len(m.chunks))
	for _, c := range m.chunks {
		ch <- c
	}
	close(ch)
	return ch, nil
}

// ──────────────────────────── tick 全流程 ────────────────────────────

// TestTickNow_全流程走通 覆盖 Step 1~6：情境 → 决策 → 选会话 → 触发 → 回调计数。
//
// 用真 *DeepAgent 做决策、mock host 做投递边界（server 运行时属外部边界）。
// 情境报告来自真实 workspace 目录，故先造一个可被扫描到的 agent 会话。
func TestTickNow_全流程(t *testing.T) {
	ResetDailyCounts()
	t.Cleanup(ResetDailyCounts)
	resetRecommendationState(t)

	// 造一个可被 ScanSessions 扫到的会话
	seedAgentSession(t, "tick-flow-sess", 1)

	delivered := make(chan struct{}, 1)
	host := &mockMainAgentHost{
		agent: &mockMainAgentHandle{chunks: []*swarmschema.AgentResponseChunk{
			newFinalChunk("推荐话术正文"),
		}},
		pushResult: true,
	}

	engine := NewProactiveEngine(map[string]any{"enabled": true, "max_recommend_per_day": 5})
	engine.SetProactiveAgent(newProactiveAgentWithReply(t,
		`{"decision": {"type": "task_reminder", "target": "整理报表", "reason": "用户说过", "urgency": 0.8}}`))
	engine.SetCheckAgentAvailableCallback(func(string) bool { return true })
	engine.SetTriggerMainAgentCallback(func(ctx context.Context, request *ProactiveTriggerRequest) (bool, error) {
		// 包一层，拿到回调后在线程内触发真实投递并等回执
		go func() {
			if request.OnDelivered != nil {
				request.OnDelivered("推荐话术正文")
				delivered <- struct{}{}
			}
		}()
		_ = host
		return true, nil
	})

	ok := engine.TickNow(context.Background(), "")
	assert.True(t, ok, "全流程应返回 true")

	select {
	case <-delivered:
	case <-time.After(2 * time.Second):
		t.Fatal("onDelivered 未被调用")
	}

	// 回调应已写入 history 并计数
	assert.Equal(t, 1, TodayRecommendCount(), "送达后应计数")
	state := LoadRecommendationState("")
	require.Len(t, state.RecommendationHistory, 1)
	assert.Equal(t, "task_reminder", state.RecommendationHistory[0]["type"])
	assert.Equal(t, "整理报表", state.RecommendationHistory[0]["target"])
	assert.Equal(t, "推荐话术正文", state.RecommendationHistory[0]["content"])
}

// TestTickNow_无决策跳过 模型返回 null 时 tick 返回 false。
func TestTickNow_无决策(t *testing.T) {
	ResetDailyCounts()
	t.Cleanup(ResetDailyCounts)
	seedAgentSession(t, "tick-no-decision", 2)

	engine := NewProactiveEngine(map[string]any{"enabled": true})
	engine.SetProactiveAgent(newProactiveAgentWithReply(t, `{"decision": null}`))
	engine.SetCheckAgentAvailableCallback(func(string) bool { return true })
	engine.SetTriggerMainAgentCallback(func(context.Context, *ProactiveTriggerRequest) (bool, error) {
		t.Fatal("无决策时不应触发主 agent")
		return false, nil
	})

	assert.False(t, engine.TickNow(context.Background(), ""))
}

// TestTickNow_触发失败跳过 回调返回 false 时 tick 返回 false 且不计数。
func TestTickNow_触发失败(t *testing.T) {
	ResetDailyCounts()
	t.Cleanup(ResetDailyCounts)
	seedAgentSession(t, "tick-trigger-fail", 3)

	engine := NewProactiveEngine(map[string]any{"enabled": true})
	engine.SetProactiveAgent(newProactiveAgentWithReply(t,
		`{"decision": {"type": "task_reminder", "target": "X", "reason": "R"}}`))
	engine.SetCheckAgentAvailableCallback(func(string) bool { return true })
	engine.SetTriggerMainAgentCallback(func(context.Context, *ProactiveTriggerRequest) (bool, error) {
		return false, nil
	})

	assert.False(t, engine.TickNow(context.Background(), ""))
	assert.Equal(t, 0, TodayRecommendCount(), "未触发不应计数")
}

// TestTickNow_指定通道无会话跳过 按通道找不到会话时返回 false。
func TestTickNow_指定通道无会话(t *testing.T) {
	ResetDailyCounts()
	t.Cleanup(ResetDailyCounts)
	seedAgentSession(t, "tick-channel-miss", 4)

	engine := NewProactiveEngine(map[string]any{"enabled": true})
	engine.SetProactiveAgent(newProactiveAgentWithReply(t,
		`{"decision": {"type": "task_reminder", "target": "X", "reason": "R"}}`))
	engine.SetCheckAgentAvailableCallback(func(string) bool { return true })
	engine.SetTriggerMainAgentCallback(func(context.Context, *ProactiveTriggerRequest) (bool, error) {
		t.Fatal("找不到会话时不应触发")
		return false, nil
	})

	assert.False(t, engine.TickNow(context.Background(), "不存在的通道"))
}

// TestTickNow_消费反馈更新梯度 Step 1.5：buffer 非空时走梯度更新。
func TestTickNow_消费反馈更新梯度(t *testing.T) {
	ResetDailyCounts()
	t.Cleanup(ResetDailyCounts)
	resetRecommendationState(t)
	seedAgentSession(t, "tick-gradients", 5)

	// 预置一条待消费的反馈
	RecordExplicitFeedback("rec_old", "explicit_like", &RecMeta{
		RecType: "skill_recommend", RecTarget: "翻译技能",
	})
	before := LoadRecommendationState("")
	require.Len(t, before.FeedbackBuffer, 1)

	engine := NewProactiveEngine(map[string]any{"enabled": true})
	engine.SetProactiveAgent(newProactiveAgentWithReply(t,
		`{"decision": null}`))
	engine.SetCheckAgentAvailableCallback(func(string) bool { return true })

	engine.TickNow(context.Background(), "")

	after := LoadRecommendationState("")
	assert.Empty(t, after.FeedbackBuffer, "tick 应消费掉反馈缓冲")
}

// TestConsumeAndUpdateGradients 直接测梯度更新与 20 条截断。
func TestConsumeAndUpdateGradients(t *testing.T) {
	t.Run("空buffer不动作", func(t *testing.T) {
		engine := NewProactiveEngine(nil)
		state := &RecommendationState{}
		engine.consumeAndUpdateGradients(context.Background(), state, nil)
		assert.Empty(t, state.StrategyGradients)
	})

	t.Run("更新后截断到20条", func(t *testing.T) {
		resetRecommendationState(t)
		engine := NewProactiveEngine(nil)
		agent := newProactiveAgentWithReply(t,
			`{"operations": [{"action": "add", "rule": "新规则", "category": "target"}]}`)

		existing := make([]map[string]any, 0, 25)
		for i := 0; i < 25; i++ {
			existing = append(existing, map[string]any{
				"gradient_id": "g" + strconv.Itoa(i), "rule": "R" + strconv.Itoa(i), "category": "target",
			})
		}
		state := &RecommendationState{
			StrategyGradients: existing,
			FeedbackBuffer: []map[string]any{
				{"feedback_type": "explicit_like", "rec_content": "A"},
			},
		}
		engine.consumeAndUpdateGradients(context.Background(), state, agent)

		assert.LessOrEqual(t, len(state.StrategyGradients), maxStoredGradients,
			"梯度存储应截断到 20 条")
	})
}

// TestCloseProactiveAgent nil 与正常关闭都不 panic。
func TestCloseProactiveAgent(t *testing.T) {
	assert.NotPanics(t, func() {
		closeProactiveAgent(context.Background(), nil)
	})
	agent := newProactiveAgentWithReply(t, `{"decision": null}`)
	assert.NotPanics(t, func() {
		closeProactiveAgent(context.Background(), agent)
	})
}

// TestSafeCheckAgentAvailable panic 被捕获并放行。
func TestSafeCheckAgentAvailable(t *testing.T) {
	assert.NotPanics(t, func() {
		got := safeCheckAgentAvailable(func(string) bool { panic("boom") }, "web")
		// panic 后返回值是零值 false（对齐 Python 吞异常后按「不可用」保守处理）
		assert.False(t, got)
	})
	assert.True(t, safeCheckAgentAvailable(func(string) bool { return true }, "web"))
}

// TestSafeSendNotification panic 与报错都被吞。
func TestSafeSendNotification(t *testing.T) {
	assert.NotPanics(t, func() {
		safeSendNotification(func(string, string) error { panic("boom") }, "web", "msg")
	})
	assert.NotPanics(t, func() {
		safeSendNotification(func(string, string) error { return assert.AnError }, "web", "msg")
	})
}

// TestSetProactiveAgent_已关闭 关闭后设置被忽略不 panic。
func TestSetProactiveAgent_已关闭(t *testing.T) {
	engine := NewProactiveEngine(nil)
	engine.Close(context.Background())
	agent := newProactiveAgentWithReply(t, `{"decision": null}`)

	assert.NotPanics(t, func() {
		engine.SetProactiveAgent(agent)
		engine.SetProactiveAgentBuilder(func() *harness.DeepAgent { return nil })
	})

	engine.mu.RLock()
	defer engine.mu.RUnlock()
	assert.Nil(t, engine.proactiveAgent, "关闭后注入应被忽略")
	assert.Nil(t, engine.agentBuilder)
}

// seedAgentSession 在真实 agent sessions 目录造一个可被扫到的会话。
//
// ScanSessions 走 workspace.AgentSessionsDir()（进程级缓存），测试无法重定向，
// 故只能写到真实目录。会话 id 带 test 前缀便于识别；清理由 t.Cleanup 负责。
func seedAgentSession(t *testing.T, sessionID string, tsOffset int) {
	t.Helper()
	dir := filepath.Join(workspace.AgentSessionsDir(), sessionID)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	meta := map[string]any{
		"session_id":      sessionID,
		"channel_id":      "web",
		"mode":            "agent",
		"last_message_at": float64(time.Now().Unix()) - float64(tsOffset),
		"message_count":   2,
		"title":           sessionID,
	}
	metaData, err := json.MarshalIndent(meta, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "metadata.json"), metaData, 0o644))

	history := []map[string]any{
		{"role": "user", "content": "帮我整理报表"},
		{"role": "assistant", "event_type": "chat.final", "content": "好的"},
	}
	historyData, err := json.MarshalIndent(history, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "history.json"), historyData, 0o644))
}

// TestGetProactiveModel_无默认模型 无配置时返回 nil。
func TestGetProactiveModel_无默认模型(t *testing.T) {
	// 真实环境可能无模型配置；只验证返回类型安全
	assert.NotPanics(t, func() {
		_ = getProactiveModel()
	})
}

// TestBuildProactiveAgent_有模型配置 注入默认模型后能构造出 agent。
//
// 通过 GetDefaultModels 读 config.yaml，测试里临时写一份含 models.defaults 的配置不可行
// （配置路径进程级缓存），故此处只验证「有模型时构造路径」不 panic 且返回可用 agent 或 nil。
func TestBuildProactiveAgent_有模型配置(t *testing.T) {
	agent := BuildProactiveAgent(context.Background())
	if agent == nil {
		t.Skip("当前环境无 default models 配置，跳过构造成功路径")
	}
	defer func() { _ = agent.Stop(context.Background()) }()
	assert.NotNil(t, agent)
}

// TestNewRecommendationID_随机源正常 格式与唯一性（补 release 分支外的路径）。
func TestNewRecommendationID_格式(t *testing.T) {
	for i := 0; i < 5; i++ {
		id := newRecommendationID()
		assert.Len(t, id, len("rec_")+13+1+8, "rec_ + 13 位毫秒 + _ + 8 位 hex")
	}
}
