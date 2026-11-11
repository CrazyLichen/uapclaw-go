package recommendation

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ProactiveEngine 主动推荐引擎。
//
// 每个 tick：
//  1. 从多会话历史构建情境报告
//  2. 单次 LLM 调用（经 proactive agent）：决定推荐类型与目标
//  3. 有决策时触发主 agent 生成话术 → 流式推到前端（话术自然进入主 agent 的上下文）
//
// 本类**不跑自有循环**，由外部触发：
//   - Gateway 的 CronScheduler 通过 proactive.tick 请求触发（含 Cron 面板「立即执行」）
//
// Python: proactive_engine.py:76 ProactiveEngine
type ProactiveEngine struct {
	mu sync.RWMutex

	// maxPerDay 每日推荐条数上限
	maxPerDay int
	// maxRounds 单次 tick 每会话最多压缩的轮数
	maxRounds int
	// enabled 是否启用（默认 false，与 config.yaml 的 proactive_recommendation.enabled 一致）
	enabled bool
	// lastTickAt 上次 tick 时间戳（Unix 秒）
	lastTickAt float64
	// closed 是否已关闭
	closed bool

	// proactiveAgent 推荐专用轻量 agent（无 tools、无 task_loop、单轮、输出 JSON）
	proactiveAgent *harness.DeepAgent
	// agentBuilder 惰性构造 proactive agent 的函数
	agentBuilder ProactiveAgentBuilder

	// triggerMainAgentCallback 触发主 agent 跑一轮生成话术的回调
	triggerMainAgentCallback ProactiveTriggerFunc
	// checkAgentAvailableCallback 检查目标 channel 是否有活跃 agent
	checkAgentAvailableCallback CheckAgentAvailableFunc
	// sendNotificationCallback 推送纯文本通知（不经过主 agent、不进 context）
	sendNotificationCallback SendNotificationFunc
}

// ProactiveAgentBuilder 惰性构造 proactive agent。
// 返回 nil 表示构造失败（无模型/初始化异常），引擎会跳过本次 tick。
//
// Python: proactive_adapter.py:85 build_proactive_agent
type ProactiveAgentBuilder func() *harness.DeepAgent

// CheckAgentAvailableFunc 检查目标 channel 是否有活跃 agent。
// agent 被 evict 后白调 LLM 是浪费 token，故 tick 前先查。
//
// Python: proactive_engine.py:283 set_check_agent_available_callback
type CheckAgentAvailableFunc func(targetChannel string) bool

// SendNotificationFunc 推送一条纯文本通知到前端（不带 LLM、不进会话历史）。
// 用于「今日推荐已达上限」等系统提醒。
//
// Python: proactive_engine.py:287 set_send_notification_callback
type SendNotificationFunc func(targetChannel, message string) error

// ──────────────────────────── 常量 ────────────────────────────

const (
	// defaultMaxPerDay 每日推荐条数默认上限
	// Python: proactive_engine.py:93 config.get("max_recommend_per_day", 5)
	defaultMaxPerDay = 5
	// defaultMaxRounds 单次 tick 默认压缩轮数
	// Python: proactive_engine.py:94 config.get("max_rounds_per_tick", 20)
	defaultMaxRounds = 20
	// maxStoredGradients 策略梯度存储上限
	// Python: proactive_engine.py:463 state.strategy_gradients = updated[-20:]
	maxStoredGradients = 20
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewProactiveEngine 创建引擎。
//
// config 为 proactive_recommendation 配置段（键：enabled / max_recommend_per_day /
// max_rounds_per_tick）。nil 或缺失键时用防御性默认值。
//
// Python: proactive_engine.py:91 __init__
func NewProactiveEngine(cfg map[string]any) *ProactiveEngine {
	if cfg == nil {
		cfg = map[string]any{}
	}
	enabled, _ := cfg["enabled"].(bool)
	return &ProactiveEngine{
		maxPerDay: SafeProactiveInt(cfg["max_recommend_per_day"], defaultMaxPerDay),
		maxRounds: SafeProactiveInt(cfg["max_rounds_per_tick"], defaultMaxRounds),
		// 默认 false：配置段缺失时引擎不应被误启用
		enabled: enabled,
	}
}

// SetProactiveAgent 直接注入已构造的 proactive agent。
// Python: proactive_engine.py:118 set_proactive_agent
func (e *ProactiveEngine) SetProactiveAgent(agent *harness.DeepAgent) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		logger.Warn(logComponent).Msg("proactive engine is closed, set agent ignored")
		return
	}
	e.agentBuilder = nil
	e.proactiveAgent = agent
}

// SetProactiveAgentBuilder 注册惰性构造器；引擎构造保持轻量。
// Python: proactive_engine.py:135 set_proactive_agent_builder
func (e *ProactiveEngine) SetProactiveAgentBuilder(builder ProactiveAgentBuilder) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		logger.Warn(logComponent).Msg("proactive engine is closed, set builder ignored")
		return
	}
	e.proactiveAgent = nil
	e.agentBuilder = builder
}

// RebuildProactiveAgent 在模型/凭据配置变更后让下次决策重建 agent。
// 进行中的 tick 保留自己的 agent 直到结束；替换后的首次使用读最新配置。
// Python: proactive_engine.py:268 rebuild_proactive_agent
func (e *ProactiveEngine) RebuildProactiveAgent(builder ProactiveAgentBuilder) {
	if builder == nil {
		logger.Warn(logComponent).Msg("proactive agent rebuild failed: nil builder")
		return
	}
	e.SetProactiveAgentBuilder(builder)
}

// SetTriggerMainAgentCallback 设置触发主 agent 跑一轮的回调。
// Python: proactive_engine.py:279 set_trigger_main_agent_callback
func (e *ProactiveEngine) SetTriggerMainAgentCallback(callback ProactiveTriggerFunc) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.triggerMainAgentCallback = callback
}

// SetCheckAgentAvailableCallback 设置「目标 channel 是否有活跃 agent」的检查回调。
// Python: proactive_engine.py:283 set_check_agent_available_callback
func (e *ProactiveEngine) SetCheckAgentAvailableCallback(callback CheckAgentAvailableFunc) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.checkAgentAvailableCallback = callback
}

// SetSendNotificationCallback 设置纯文本通知推送回调。
// Python: proactive_engine.py:287 set_send_notification_callback
func (e *ProactiveEngine) SetSendNotificationCallback(callback SendNotificationFunc) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.sendNotificationCallback = callback
}

// ReloadConfig 热重载配置，不重启进程。
//
// 当日推荐计数**不重置**——它是「今天实际推了几条」的客观事实，清掉反而会让
// 调小上限时 count 归零 < 新上限，在已超额的情况下继续推送，违反上限语义。
//
// Python: proactive_engine.py:294 reload_config
func (e *ProactiveEngine) ReloadConfig(cfg map[string]any) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if cfg == nil {
		cfg = map[string]any{}
	}
	newEnabled, _ := cfg["enabled"].(bool)
	newMaxPerDay := SafeProactiveInt(cfg["max_recommend_per_day"], e.maxPerDay)
	newMaxRounds := SafeProactiveInt(cfg["max_rounds_per_tick"], e.maxRounds)

	e.maxPerDay = newMaxPerDay
	e.maxRounds = newMaxRounds
	e.enabled = newEnabled
	logger.Info(logComponent).Bool("enabled", newEnabled).Int("max_per_day", newMaxPerDay).
		Msg("proactive config reloaded")
}

// LastTickAt 上次 tick 时间戳（Unix 秒）。供外部只读访问。
// Python: proactive_engine.py:264 last_tick_at
func (e *ProactiveEngine) LastTickAt() float64 {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.lastTickAt
}

// Close 关闭引擎：停止 tick 与 agent 的退场清理。
// Python: proactive_engine.py:234 close
func (e *ProactiveEngine) Close(ctx context.Context) {
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return
	}
	e.closed = true
	agent := e.proactiveAgent
	e.proactiveAgent = nil
	e.agentBuilder = nil
	e.mu.Unlock()

	// 用公开的生命周期方法关闭 agent（不碰 SDK Model 客户端池——
	// 池的所有权属于应用/Runner，不由引擎关闭）。
	closeProactiveAgent(ctx, agent)
}

// TickNow 触发一次推荐 tick。
//
// 这是外部调用方（CronScheduler、手动触发）的主入口。
//
// targetChannel 为空表示投递到最近活跃会话。
// 返回 true 表示确实投递了一次推荐；false 表示被跳过（引擎关闭/未启用/
// agent 不可用/无活跃会话/已达上限/无决策/session 忙等）。
//
// Python: proactive_engine.py:310 tick_now
func (e *ProactiveEngine) TickNow(ctx context.Context, targetChannel string) bool {
	e.mu.RLock()
	closed := e.closed
	enabled := e.enabled
	e.mu.RUnlock()

	if closed || !enabled {
		logger.Debug(logComponent).Msg("engine disabled, skipping tick")
		return false
	}

	logger.Info(logComponent).Str("target_channel", targetChannel).Msg("tick_now called")

	agent := e.ensureProactiveAgent()
	if agent == nil {
		e.markTickNow()
		return false
	}

	pushed, err := e.tick(ctx, agent, targetChannel)
	if err != nil {
		logger.Warn(logComponent).Err(err).Msg("tick_now failed")
		return false
	}
	return pushed
}

// RunMaintenance 清理过期数据（老日期 key 的每日计数）。
// 由 tick 内部调用，也可由 Cron 或手动调用。
// Python: proactive_engine.py:607 run_maintenance
func (e *ProactiveEngine) RunMaintenance() {
	PruneDailyCounts()
	logger.Debug(logComponent).Msg("maintenance completed")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// ensureProactiveAgent 取当前可用的 proactive agent；有 builder 时按需构造。
//
// Python: proactive_engine.py:186 _ensure_proactive_agent
func (e *ProactiveEngine) ensureProactiveAgent() *harness.DeepAgent {
	e.mu.RLock()
	existing := e.proactiveAgent
	builder := e.agentBuilder
	closed := e.closed
	e.mu.RUnlock()

	if existing != nil || builder == nil || closed {
		return existing
	}

	// 构造可能较重（建模型客户端），故在锁外执行，避免阻塞 tick_now 的其它读写
	agent := builder()
	if agent == nil {
		return nil
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	// 构造期间可能已被 Close / 换成新 builder，此时丢弃本次结果
	if e.closed || e.agentBuilder == nil {
		closeProactiveAgent(context.Background(), agent)
		return nil
	}
	e.proactiveAgent = agent
	return agent
}

// tick 单次 tick：构建情境 → 分析 → 投递。
//
// Python: proactive_engine.py:356 _tick
func (e *ProactiveEngine) tick(ctx context.Context, proactiveAgent *harness.DeepAgent, targetChannel string) (bool, error) {
	// 每次 tick 顺手清理过期数据
	e.RunMaintenance()

	// 本 tick 的语言基准：config.yaml preferred_language，
	// 决策/指令/梯度渲染共用，与正常聊天回复语言同源。
	preferredLanguage := PreferredLanguage()

	e.mu.RLock()
	checkAvailable := e.checkAgentAvailableCallback
	sendNotification := e.sendNotificationCallback
	triggerCallback := e.triggerMainAgentCallback
	maxPerDay := e.maxPerDay
	maxRounds := e.maxRounds
	e.mu.RUnlock()

	// Step 0：检查目标 channel 是否有活跃 agent——agent 被 evict 后白调 LLM 浪费 token
	if checkAvailable != nil && !safeCheckAgentAvailable(checkAvailable, targetChannel) {
		logger.Debug(logComponent).Str("channel", targetChannel).
			Msg("no active agent for channel, skipping tick")
		e.markTickNow()
		return false, nil
	}

	// Gate：每日预算
	if todayCount := TodayRecommendCount(); todayCount >= maxPerDay {
		logger.Info(logComponent).Int("count", todayCount).Int("limit", maxPerDay).
			Msg("daily limit reached, skipping tick")
		// 推送上限提醒。前端拦截 source=proactive_notification 后改走顶部弹窗
		// （不进会话历史），故每次命中上限都推一次，无需去重。
		if sendNotification != nil {
			msg := fmt.Sprintf("今日主动推荐已达每日上限（%d 条），明日恢复。", maxPerDay)
			safeSendNotification(sendNotification, targetChannel, msg)
		}
		e.markTickNow()
		return false, nil
	}

	// Step 1：构建情境报告
	_, availableSkills := DetectAllSkills()
	report := BuildSituationReport(ctx, maxRounds, availableSkills, defaultScanModePrefix, nil)
	if report.IsEmpty() {
		logger.Debug(logComponent).Msg("situation report empty, skipping tick")
		e.markTickNow()
		return false, nil
	}
	reportText := report.RenderForLLM()

	// Step 1.5：消费反馈缓冲 → 更新梯度
	state := LoadRecommendationState("")
	if len(state.FeedbackBuffer) > 0 && proactiveAgent != nil {
		e.consumeAndUpdateGradients(ctx, state, proactiveAgent)
	}

	// Step 2：LLM 分析 + 决策
	decisionRulesText := RenderDecisionRules(state.StrategyGradients, preferredLanguage)
	result := AnalyzeAndDecide(ctx, reportText, availableSkills, proactiveAgent, decisionRulesText, preferredLanguage)

	// Step 3：是否产生了推荐
	if result == nil || result.Decision == nil {
		logger.Debug(logComponent).Msg("no recommendation this tick")
		e.markTickNow()
		return false, nil
	}
	decision := result.Decision

	// Step 4：冷却检查已整段移除
	//
	// 原用 cooldown_records 做字面精确去重（同 target 24h 不推），对三类推荐语义都错配：
	// skill 同名不同事可再推（误杀）、task 未闭环该再提醒（误杀）、exploration 字面不同
	// 换皮又挡不住（漏放）。去重改由决策 LLM 承担——历史推荐记录已喂进决策 prompt 当
	// 硬约束。总量由 max_recommend_per_day 限。

	// Step 5：选投递目标
	var targetSession *SessionSummary
	if targetChannel != "" {
		targetSession = report.FindSessionForChannel(targetChannel)
		if targetSession == nil {
			logger.Debug(logComponent).Str("channel", targetChannel).
				Msg("no session found for channel")
			e.markTickNow()
			return false, nil
		}
	} else {
		targetSession = report.MostRecentActiveSession()
		if targetSession == nil {
			logger.Debug(logComponent).Msg("no active session for delivery")
			e.markTickNow()
			return false, nil
		}
	}

	// Step 6：触发主 agent 生成并投递
	if triggerCallback == nil {
		logger.Warn(logComponent).Msg("no trigger_main_agent_callback, skipping")
		e.markTickNow()
		return false, nil
	}

	styleRulesSection := RenderStyleRules(state.StrategyGradients, preferredLanguage)

	// 生成唯一 rec_id（前端反馈时用）。
	// 加随机后缀避免碰撞：纯毫秒时间戳在并发（cron 到点 + 手动「立即执行」同毫秒）
	// 或测试快速执行时可能重复，而反馈采集按 rec_id 去重，重复会让第二条反馈被丢弃。
	recID := newRecommendationID()

	// on_delivered：后台真正送达后才做的状态更新（Step 7）
	//
	// 重新 load 最新 state 再持久化——本回调在后台真正跑完时才执行，距 tick 时载入的
	// state 快照可能已隔一段时间。期间用户可能已发消息触发隐式反馈写入 buffer，
	// 或别的路径改过 state。若直接复用旧快照 save，会把中间的反馈覆盖丢失。
	onDelivered := func(generatedMessage string) {
		freshState := LoadRecommendationState("")
		IncrementDailyCount()
		freshState.AddRecommendation(map[string]any{
			"id":         recID,
			"type":       decision.Type,
			"target":     decision.Target,
			"reason":     decision.Reason,
			"urgency":    decision.Urgency,
			"tick_at":    nowUnixSeconds(),
			"session_id": targetSession.SessionID,
			"content":    generatedMessage,
		})
		freshState.Touch()
		SaveRecommendationState(freshState, "")
		logger.Info(logComponent).Str("target", decision.Target).Str("rec_id", recID).
			Str("type", decision.Type).Float64("urgency", decision.Urgency).
			Str("session", truncateRunes(targetSession.SessionID, 20)).
			Msg("recommendation delivered")
	}

	triggerRequest := &ProactiveTriggerRequest{
		SessionID:         targetSession.SessionID,
		Decision:          decision,
		ChannelID:         targetChannel,
		OnDelivered:       onDelivered,
		RecID:             recID,
		StyleRulesSection: styleRulesSection,
	}

	triggered := TriggerMainAgent(ctx, triggerCallback, triggerRequest)
	if !triggered {
		logger.Info(logComponent).Msg("not triggered (session busy, duplicate inflight, or trigger failed)")
		e.markTickNow()
		return false, nil
	}

	// 已 fire-and-forget 触发：立即返回，后台跑完会回调 onDelivered 做计数。
	// cron 端不等主 agent，秒回不超时。
	e.markTickNow()
	return true, nil
}

// consumeAndUpdateGradients 消费反馈缓冲并更新策略梯度。
// Python: proactive_engine.py:444-469 Step 1.5
func (e *ProactiveEngine) consumeAndUpdateGradients(
	ctx context.Context,
	state *RecommendationState,
	proactiveAgent *harness.DeepAgent,
) {
	feedbacks := ConsumeFeedbackBuffer(state)
	if len(feedbacks) == 0 {
		return
	}
	updated := UpdateGradients(ctx, feedbacks, state.StrategyGradients, proactiveAgent)
	// 留最新 20 条：截断方向反映「保留最近反馈学到的梯度」。
	// revise 挪位 + 留最新配合 → 被新反馈更新/新增的规则优先保留，
	// 早期未被触及的规则优先淘汰（LRU 语义）。
	// 容量 20 与喂 LLM 的决策层 10 + 话术层 10 = 20 一致。
	if len(updated) > maxStoredGradients {
		updated = updated[len(updated)-maxStoredGradients:]
	}
	state.StrategyGradients = updated
	state.Touch()
	SaveRecommendationState(state, "")
	logger.Info(logComponent).Int("gradients", len(updated)).Int("feedbacks", len(feedbacks)).
		Msg("updated gradients from feedbacks")
}

// markTickNow 更新上次 tick 时间戳。
func (e *ProactiveEngine) markTickNow() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.lastTickAt = nowUnixSeconds()
}

// closeProactiveAgent 用公开的生命周期方法关闭 agent。
//
// 只用公开的 owner 生命周期方法。SDK 的 Model 客户端池保持其既有的应用/Runner
// 所有者，不关闭私有客户端。
//
// Python: proactive_engine.py:153 _close_agent
func closeProactiveAgent(ctx context.Context, agent *harness.DeepAgent) {
	if agent == nil {
		return
	}
	if err := agent.Stop(ctx); err != nil {
		logger.Debug(logComponent).Err(err).Msg("proactive agent close failed")
	}
}

// safeCheckAgentAvailable 包装检查回调，异常时按「可用」放行（对齐 Python 吞异常）。
func safeCheckAgentAvailable(check CheckAgentAvailableFunc, targetChannel string) bool {
	defer func() {
		if recovered := recover(); recovered != nil {
			logger.Debug(logComponent).Any("panic", recovered).Msg("check_agent_available panicked")
		}
	}()
	return check(targetChannel)
}

// safeSendNotification 包装通知回调，失败只记 debug（对齐 Python 吞异常）。
func safeSendNotification(send SendNotificationFunc, targetChannel, message string) {
	defer func() {
		if recovered := recover(); recovered != nil {
			logger.Debug(logComponent).Any("panic", recovered).Msg("send_notification panicked")
		}
	}()
	if err := send(targetChannel, message); err != nil {
		logger.Debug(logComponent).Err(err).Msg("send_notification failed")
	}
}

// newRecommendationID 生成唯一推荐 ID：rec_<毫秒时间戳>_<8位随机十六进制>。
// Python: proactive_engine.py:543 f"rec_{int(time.time() * 1000)}_{uuid.uuid4().hex[:8]}"
func newRecommendationID() string {
	suffix := make([]byte, 4)
	if _, err := rand.Read(suffix); err != nil {
		// 随机源失败时退回纳秒后 8 位，仍保证唯一性足够
		return fmt.Sprintf("rec_%d_%08x", time.Now().UnixMilli(), time.Now().UnixNano()&0xffffffff)
	}
	return fmt.Sprintf("rec_%d_%s", time.Now().UnixMilli(), hex.EncodeToString(suffix))
}

// nowUnixSeconds 当前 Unix 秒（浮点，带亚秒精度）。
func nowUnixSeconds() float64 {
	return float64(time.Now().UnixNano()) / 1e9
}
