package native

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	atharness "github.com/uapclaw/uapclaw-go/internal/agent_teams/harness"
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	taskloop "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/task_loop"
	hp "github.com/uapclaw/uapclaw-go/internal/agentcore/harness_protocol"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness_providers"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// controlChunkType 控制帧类型（协议字符串，不本地化）。
	// Python: protocol_adapter.py:58 _CONTROL_CHUNK
	controlChunkType = "native_protocol.control"
	// checkpointSchemaVersion 快照 schema 版本。
	// Python: protocol_adapter.py:146 "1"
	checkpointSchemaVersion = "1"
	// nativeV2ProviderName 工厂内置 provider 名。
	// Python: protocol_adapter.py:82 card.name / factory.py:207
	nativeV2ProviderName = "native_v2"
)

// outputStateFields 输出态 6 字段，save/restore 顺序不可换。
// Python: protocol_adapter.py:62-69 _OUTPUT_STATE_FIELDS
var outputStateFields = []string{
	"answer_parts", "reasoning_parts", "final_output",
	"result_type", "tool_blocks", "tool_messages",
}

// knownV2Keys create 允许的配置键。
// Python: protocol_adapter.py:470 _KNOWN_KEYS 等价（4 键）
var knownV2Keys = map[string]bool{
	"deep_agent": true, "agent_template": true,
	"language": true, "event_buffer_capacity": true,
}

// ──────────────────────────── 结构体 ────────────────────────────

// NativeHarnessProtocolAdapter 把 L2 NativeHarness 适配到公开 harness protocol。
// Python: protocol_adapter.py:72-426 NativeHarnessProtocolAdapter
//
// ⚠️ D-3：不嵌入 *DeepAgentHarness 后覆盖方法——15.6 的
// ExecuteTurn→runRound→consumeChunk 是基类自调用链，Go 嵌入会静态绑定失效。
// 改为：① 自己实现 providerHooks 全 11 方法并构造期注入 base；
// ② ExecuteTurn/runRound 独立重写；③ consumeFn 构造期注入；
// ④ 公开方法显式委托或自实现。
type NativeHarnessProtocolAdapter struct {
	// base 协议层串行 Turn 骨架（构造期注入 Hooks: 本 adapter）
	base *harness_providers.SerializedTurnHarness
	// spec 原生 spec（构造期深拷贝语义：Go 值拷贝）
	spec atschema.DeepAgentSpec
	// buildContext 传给 L2 的运行时依赖
	buildContext hschema.BuildContextInterface

	// mu 保护下列周期态（对齐 Python 单协程语义）
	mu sync.Mutex
	// coldRestore 从 checkpoint 装入的待续快照
	coldRestore *NativeCheckpoint
	// coldTurn 交给 runRound 按 turn_id 认领的快照
	coldTurn *NativeCheckpoint
	// currentOutputState 当前轮次输出态
	currentOutputState *turnState
	// dispatched / dispatchedTurnID 保证 pause/steer/interrupt 不在 dispatch 前摸 agent
	dispatched       chan struct{}
	dispatchedTurnID string
	// pausedObserved / resumedObserved 相位事件栅栏
	pausedObserved  chan struct{}
	resumedObserved chan struct{}
	// checkpointLock 串行化 checkpoint 捕获与 close/abort
	checkpointLock sync.Mutex

	// agent 本周期造出的 L2 实例（与团队主链那台不同）
	agent *atharness.NativeHarness
	// agentSession 本周期会话
	agentSession *session.Session
	// nativeOutputs L2 输出迭代器
	nativeOutputs *atharness.OutputIterator

	// recordStep 装配步骤记录器（仅测试注入；生产为 nil）
	recordStep func(string)
	// buildNativeFn L2 构造器（生产为 a.buildNative，测试可替换）
	buildNativeFn func(hp.HarnessContext) *atharness.NativeHarness
	// outputNext 输出帧读取器（生产绑定 nativeOutputs.Next，测试可替换）
	outputNext func(context.Context) (stream.Schema, bool)
	// consumeFn 普通帧消费器（生产绑定同包 consumeChunk，测试可替换）
	consumeFn func(context.Context, *harness_providers.PendingTurn, *turnState, stream.Schema) error
	// sinkErr checkpoint sink 故障注入（仅测试）
	sinkErr error
	// emitLifecycleFn 生命周期事件发射器（仅测试注入）
	emitLifecycleFn func(context.Context, *harness_providers.PendingTurn, hp.TurnEventKind) error
	// executeTurnErr ExecuteTurn 故障注入（仅测试）
	executeTurnErr error
	// deepAgentHarness 同包 15.6 实例，供 consumeFn 直调 consumeChunk（D-3 ③）
	deepAgentHarness *DeepAgentHarness
}

// NativeV2HarnessProvider 从可序列化配置构造未启动的 adapter。
// Python: protocol_adapter.py:458-487
type NativeV2HarnessProvider struct{}

// ──────────────────────────── 导出函数 ────────────────────────────

// nativeV2Card 构造 native_v2 能力卡。
// Python: protocol_adapter.py:81-93 card
func nativeV2Card() hp.HarnessCard {
	card, err := hp.NewHarnessCardWithOptions(nativeV2ProviderName, "0.2.0", hp.CardOptions{
		Capabilities: map[hp.HarnessCapability]bool{
			hp.HarnessCapabilitySteer:             true,
			hp.HarnessCapabilityForceAbort:        true,
			hp.HarnessCapabilityGracefulAbort:     true,
			hp.HarnessCapabilityPauseResume:       true,
			hp.HarnessCapabilityCheckpoint:        true,
			hp.HarnessCapabilityPersistentSession: true,
		},
		OptionalHostCapabilities: map[hp.HostCapability]bool{
			hp.HostCapabilityUserInput:      true,
			hp.HostCapabilityCheckpointSink: true,
		},
	})
	if err != nil {
		panic("native_v2 card 构造失败: " + err.Error())
	}
	return card
}

// NewNativeV2Adapter 构造未启动的 adapter。
// Python: protocol_adapter.py:95-120 __init__
func NewNativeV2Adapter(spec atschema.DeepAgentSpec, buildCtx hschema.BuildContextInterface, capacity int) *NativeHarnessProtocolAdapter {
	if capacity <= 0 {
		capacity = 1024
	}
	a := &NativeHarnessProtocolAdapter{
		spec:             spec,
		buildContext:     buildCtx,
		dispatched:       make(chan struct{}, 1),
		pausedObserved:   make(chan struct{}, 1),
		resumedObserved:  make(chan struct{}, 1),
		deepAgentHarness: NewDeepAgentHarness(nil),
	}
	a.buildNativeFn = a.buildNative
	a.consumeFn = func(ctx context.Context, turn *harness_providers.PendingTurn, state *turnState, chunk stream.Schema) error {
		return a.deepAgentHarness.consumeChunk(ctx, turn, state, chunk)
	}
	// D-3 关键：hooks 注入自己，不是嵌入后覆盖。
	a.base = harness_providers.NewSerializedTurnHarness(harness_providers.HarnessOptions{
		Card:                nativeV2Card(),
		Hooks:               a,
		EventBufferCapacity: capacity,
	})
	return a
}

// Card 返回静态能力卡（显式委托）。
// Python: protocol_adapter.py:461-463 / DeepAgentHarness.card
func (a *NativeHarnessProtocolAdapter) Card() hp.HarnessCard { return a.base.Card() }

// State 返回当前高层生命周期相位。
// Python: base.py:166-168 state
func (a *NativeHarnessProtocolAdapter) State() hp.HarnessState { return a.base.State() }

// ProviderSessionID 返回 provider 原生会话标识。
// Python: base.py:170-172 provider_session_id
func (a *NativeHarnessProtocolAdapter) ProviderSessionID() *string { return a.base.ProviderSessionID() }

// EventBufferConfig 返回观测缓冲背压策略。
// Python: base.py:174-176 event_buffer_config
func (a *NativeHarnessProtocolAdapter) EventBufferConfig() hp.EventBufferConfig {
	return a.base.EventBufferConfig()
}

// Events 返回周期级有序观测流。
// Python: base.py:359-363 events
func (a *NativeHarnessProtocolAdapter) Events() hp.HarnessEventCursor { return a.base.Events() }

// TurnEvents 返回单轮有限观测流。
// Python: base.py:365-369 turn_events
func (a *NativeHarnessProtocolAdapter) TurnEvents(turnID *string) hp.HarnessEventCursor {
	return a.base.TurnEvents(turnID)
}

// Start 校验宿主、开启一次周期并稳定在 idle。
// Python: base.py:271-298 start（经 SerializedTurnHarness，hooks 回调本 adapter）
func (a *NativeHarnessProtocolAdapter) Start(ctx context.Context, hc hp.HarnessContext) error {
	return a.base.Start(ctx, hc)
}

// Stop 停止周期、关闭事件并稳定在 terminated。
// Python: base.py:309-325 stop（_close_session 经 hooks 回调本 adapter）
func (a *NativeHarnessProtocolAdapter) Stop(ctx context.Context) error { return a.base.Stop(ctx) }

// Abort 按已声明能力中止在飞轮次。
// Python: base.py:429-442 abort（_interrupt_turn 经 hooks 回调本 adapter）
func (a *NativeHarnessProtocolAdapter) Abort(ctx context.Context, mode hp.AbortMode) error {
	return a.base.Abort(ctx, mode)
}

// ListModels 未声明 MODEL_DISCOVERY，返回不支持。
// Python: base.py:225-227 _list_models 默认
func (a *NativeHarnessProtocolAdapter) ListModels(context.Context) ([]hp.ModelOption, error) {
	return nil, &hp.UnsupportedHarnessCapabilityError{Msg: "native_v2 does not support model discovery"}
}

// ApplyModelSelection 未声明 MODEL_SELECTION，返回不支持。
// Python: base.py:258-265 _apply_model_selection 默认
func (a *NativeHarnessProtocolAdapter) ApplyModelSelection(context.Context, hp.ModelSelection) error {
	return &hp.UnsupportedHarnessCapabilityError{Msg: "native_v2 does not support model selection"}
}

// AuthFallbackPayload 无认证回退配置。
// Python: base.py:229-239 _auth_fallback_payload 默认
func (a *NativeHarnessProtocolAdapter) AuthFallbackPayload() (hp.JsonObject, bool) { return nil, false }

// ReconnectForAuth 未声明认证回退，返回不支持。
// Python: base.py:241-252 _reconnect_for_auth 默认
func (a *NativeHarnessProtocolAdapter) ReconnectForAuth(context.Context, bool) error {
	return &hp.UnsupportedHarnessCapabilityError{Msg: "native_v2 does not support auth fallback"}
}

// OnAuthEndpointChanged 默认 no-op。
// Python: base.py:254-256 _on_auth_endpoint_changed
func (a *NativeHarnessProtocolAdapter) OnAuthEndpointChanged(context.Context, bool) error { return nil }

// ValidateContext 校验宿主上下文与 checkpoint 作用域（12 条）。
// Python: protocol_adapter.py:138-176 _validate_context
func (a *NativeHarnessProtocolAdapter) ValidateContext(_ context.Context, hc hp.HarnessContext) error {
	// ① 基础校验（对齐 SerializedTurnHarness._validate_context(self, context)，
	//    即 card.validate_host；Go 侧不能调 base.validateContext——那是 hooks 入口会递归）
	if err := a.base.Card().ValidateHost(hc.ProtocolVersion, hc.HostCapabilities); err != nil {
		return err
	}
	// ② MCP 声明
	if len(hc.McpServers) > 0 {
		return &hp.UnsupportedHarnessCapabilityError{Msg: "declare native MCP servers in the manifest"}
	}
	cp := hc.Checkpoint
	// ③ REQUIRE_RESUME 无 checkpoint
	if hc.ResumePolicy == hp.ResumePolicyRequireResume && cp == nil {
		return &hp.HarnessProtocolError{Msg: "NativeHarness requires a checkpoint to resume"}
	}
	if cp == nil || hc.ResumePolicy == hp.ResumePolicyNew {
		return nil
	}
	// ④ schema 版本
	if cp.SchemaVersion != checkpointSchemaVersion {
		return &hp.HarnessProtocolError{Msg: "unsupported native checkpoint version"}
	}
	// ⑤ 作用域三元组
	if cp.Provider != nativeV2ProviderName || cp.AgentID != hc.AgentID || cp.HostSessionID != hc.HostSessionID {
		return &hp.HarnessProtocolError{Msg: "native checkpoint scope does not match the context"}
	}
	snap, err := decodeNativeCheckpoint(cp.Data)
	if err != nil {
		return &hp.HarnessProtocolError{Msg: "invalid native checkpoint"}
	}
	// ⑥ session_id 拼接
	if snap.SessionID != hc.HostSessionID+":"+hc.AgentID {
		return &hp.HarnessProtocolError{Msg: "invalid native checkpoint"}
	}
	// ⑦ idle 快照不得带排队
	if snap.PausedInput == nil && len(snap.Queued) > 0 {
		return &hp.HarnessProtocolError{Msg: "invalid native checkpoint"}
	}
	// ⑧ 探测组：output_state 可解析 / deepagent 可解析 / paused_query 必须 /
	//    turn_id 去重 / card_id 一致 / cwd 一致 / contexts 可解码
	probe := newTurnState("validate")
	if err := restoreOutputState(probe, snap.OutputState); err != nil {
		return &hp.HarnessProtocolError{Msg: "invalid native checkpoint"}
	}
	_ = hschema.DeepAgentState{}.FromSessionDict(snap.DeepAgent)
	ids := make([]string, 0, len(snap.Queued)+1)
	for _, q := range snap.Queued {
		ids = append(ids, q.TurnID)
	}
	if snap.PausedInput != nil {
		ids = append(ids, snap.PausedInput.TurnID)
		if snap.PausedQuery == nil {
			return &hp.HarnessProtocolError{Msg: "invalid native checkpoint"}
		}
	}
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if seen[id] {
			return &hp.HarnessProtocolError{Msg: "invalid native checkpoint"}
		}
		seen[id] = true
	}
	if a.spec.Card != nil && snap.CardID != a.spec.Card.ID {
		return &hp.HarnessProtocolError{Msg: "invalid native checkpoint"}
	}
	if snap.Cwd != a.checkpointCwd(hc) {
		return &hp.HarnessProtocolError{Msg: "invalid native checkpoint"}
	}
	_ = snap.DecodeContexts()
	return nil
}

// OpenSession 启动本周期 L2 并装配冷恢复状态。
// Python: protocol_adapter.py:178-213 _open_session
func (a *NativeHarnessProtocolAdapter) OpenSession(ctx context.Context, hc hp.HarnessContext) (*string, error) {
	a.mu.Lock()
	// ① 清周期态
	a.coldRestore, a.coldTurn, a.currentOutputState = nil, nil, nil
	a.dispatchedTurnID = ""
	drainSignal(a.dispatched)
	drainSignal(a.pausedObserved)
	drainSignal(a.resumedObserved)
	a.mu.Unlock()

	// ② 冷恢复数据
	var restored *NativeCheckpoint
	if hc.Checkpoint != nil && hc.ResumePolicy != hp.ResumePolicyNew {
		data, err := a.base.RestoredCheckpointData(hc)
		if err != nil {
			return nil, err
		}
		cp, err := decodeNativeCheckpoint(data)
		if err != nil {
			return nil, &hp.HarnessProtocolError{Msg: "invalid native checkpoint"}
		}
		restored = &cp
	}

	// ③ 造 L2；④ 先赋资源再 await（Python 原注释：失败 start 要能回滚）
	a.record("build_native")
	agent := a.buildNativeFn(hc)
	if agent == nil {
		return nil, &hp.HarnessProtocolError{Msg: "native harness build failed"}
	}
	a.agent = agent
	agent.DeepAgent().AddRail(&observationRail{}) // 15.6 同包 _ObservationRail
	if hc.SystemPrompt != "" {
		if err := appendContextPrompt(ctx, agent.DeepAgent(), hc.SystemPrompt, a.spec.Language); err != nil {
			return nil, err
		}
	}

	// ⑤ 会话
	sessionID := hc.HostSessionID + ":" + hc.AgentID
	sess := session.CreateAgentSession(sessionID, agent.DeepAgent().Card(), nil)
	a.agentSession = sess
	a.record("pre_run")
	if err := sess.PreRun(ctx, map[string]any{}); err != nil {
		return nil, err
	}
	a.record("update_state")
	sess.UpdateState(map[string]any{"context": nil, "deepagent": nil})

	// ⑥ 冷恢复装配
	if restored != nil {
		a.record("decode_contexts")
		sess.UpdateState(map[string]any{"context": restored.DecodeContexts()})
		st := hschema.DeepAgentState{}.FromSessionDict(restored.DeepAgent)
		a.record("save_state")
		agent.DeepAgent().SaveState(sess, &st)
		a.base.SetLatestCheckpoint(hc.Checkpoint)
		if restored.PausedInput != nil {
			a.mu.Lock()
			a.coldRestore = restored
			a.mu.Unlock()
		}
	}

	// ⑦ 订阅（只投帧，不转换）
	a.record("subscribe")
	if err := agent.Subscribe(ctx, a.onNativeState, a.onNativeRound); err != nil {
		return nil, err
	}
	// ⑧ start
	a.record("start")
	if err := agent.Start(ctx, sess); err != nil {
		return nil, err
	}
	// ⑨ load_state 必须在 start 之后（D-5：独立成步）
	if restored != nil {
		a.record("load_state(stop_condition_state)")
		if coord := agent.Coordinator(); coord != nil {
			coord.LoadState(coordinatorStateFromMap(restored.DeepAgent["stop_condition_state"]))
		}
	}
	// ⑩ 输出迭代器
	a.record("outputs")
	a.nativeOutputs = agent.Outputs()
	return &sessionID, nil
}

// CloseSession 在 checkpointLock 内释放周期资源。
// Python: protocol_adapter.py:215-217 _close_session
func (a *NativeHarnessProtocolAdapter) CloseSession(ctx context.Context) error {
	a.checkpointLock.Lock()
	defer a.checkpointLock.Unlock()
	if a.agent != nil {
		return a.agent.Stop(ctx)
	}
	return nil
}

// onNativeState 只把状态变化写成控制帧，绝不 transition / 不结束 Turn。
// Python: protocol_adapter.py:219-221 / :227-232
//
// 用同一条 session FIFO：回调独自触发会抢在 output forwarder 前、提前终止
// public Turn（protocol_adapter.py:228-229 原注释）。
func (a *NativeHarnessProtocolAdapter) onNativeState(ctx context.Context, ev atharness.HarnessStateEvent) {
	if a.base.ActiveTurn() == nil || a.agentSession == nil {
		return
	}
	_ = a.agentSession.WriteStream(ctx, map[string]any{
		"type": controlChunkType, "index": 0,
		"payload": map[string]any{"state": string(ev.New)},
	})
}

// onNativeRound 只把轮事件写成控制帧。
// Python: protocol_adapter.py:223-225
func (a *NativeHarnessProtocolAdapter) onNativeRound(ctx context.Context, ev atharness.HarnessRoundEvent) {
	if a.base.ActiveTurn() == nil || a.agentSession == nil || (ev.Kind != "started" && ev.Kind != "failed") {
		return
	}
	_ = a.agentSession.WriteStream(ctx, map[string]any{
		"type": controlChunkType, "index": 0,
		"payload": map[string]any{"round": ev.Kind, "result": ev.Result},
	})
}

// ExecuteTurn 把一个已接受输入跑到底；独立重写，不复用 15.6 ExecuteTurn。
// Python: protocol_adapter.py:234-294 _run_round（经 base._execute_turn 调用）
func (a *NativeHarnessProtocolAdapter) ExecuteTurn(ctx context.Context, turn *harness_providers.PendingTurn) (hp.TurnEventKind, hp.TurnResult, error) {
	if a.executeTurnErr != nil {
		return hp.TurnEventKindFailed, hp.TurnResult{}, nil
	}
	state := newTurnState(turn.TurnID)
	a.mu.Lock()
	a.currentOutputState = state
	drainSignal(a.dispatched)
	a.mu.Unlock()

	if turn.AbortRequested() {
		return hp.TurnEventKindAborted, hp.TurnResult{}, nil
	}
	a.mu.Lock()
	cold := a.coldTurn
	if cold != nil {
		a.coldTurn = nil
	}
	a.mu.Unlock()

	if cold != nil && cold.PausedInput != nil && cold.PausedInput.TurnID == turn.TurnID {
		// cold 续跑分支（:240-246）
		if err := restoreOutputState(state, cold.OutputState); err != nil {
			return "", hp.TurnResult{}, err
		}
		if err := a.base.Transition(ctx, hp.HarnessStatePaused); err != nil {
			return "", hp.TurnResult{}, err
		}
		if err := a.emitLifecycle(ctx, turn, hp.TurnEventKindPaused); err != nil {
			return "", hp.TurnResult{}, err
		}
		if a.agent != nil && cold.PausedQuery != nil {
			if err := a.agent.Resume(ctx, *cold.PausedQuery); err != nil {
				return "", hp.TurnResult{}, err
			}
		}
	} else {
		if a.agent == nil {
			return "", hp.TurnResult{}, &hp.HarnessProtocolError{Msg: "NativeHarness output stream is unavailable"}
		}
		if err := a.agent.Send(ctx, turn.Content.Content, false); err != nil {
			return "", hp.TurnResult{}, err
		}
	}

	a.mu.Lock()
	a.dispatchedTurnID = turn.TurnID
	a.mu.Unlock()
	signal(a.dispatched)

	if a.nativeOutputs == nil && a.outputNext == nil {
		return "", hp.TurnResult{}, &hp.HarnessProtocolError{Msg: "NativeHarness output stream is unavailable"}
	}
	err := a.runRound(ctx, a.agent, turn, state)
	a.mu.Lock()
	a.dispatchedTurnID = turn.TurnID
	a.mu.Unlock()
	signal(a.dispatched)
	if err != nil {
		return "", hp.TurnResult{}, err
	}
	return hp.TurnEventKindFinished, hp.TurnResult{}, nil
}

// Steer 在 RUNNING 相位向活跃轮次注入输入。
// Python: protocol_adapter.py:317-322
func (a *NativeHarnessProtocolAdapter) Steer(ctx context.Context, turn *harness_providers.PendingTurn, content hp.HarnessInput, _ string) error {
	agent, err := a.readyNative(ctx, turn)
	if err != nil {
		return err
	}
	if agent.State() != hp.HarnessStateRunning {
		return &hp.HarnessStateError{Msg: "the native turn is not running"}
	}
	return agent.Send(ctx, harness_providers.HarnessInputText(content), true)
}

// InterruptTurn 在 checkpointLock 内按 mode 分级 abort。
// Python: protocol_adapter.py:324-327
func (a *NativeHarnessProtocolAdapter) InterruptTurn(ctx context.Context, turn *harness_providers.PendingTurn, mode hp.AbortMode) error {
	agent, err := a.readyNative(ctx, turn)
	if err != nil {
		return err
	}
	a.checkpointLock.Lock()
	defer a.checkpointLock.Unlock()
	return agent.Abort(ctx, mode == hp.AbortModeForce)
}

// Pause 等待 NativeHarness 停在内部迭代边界。
// Python: protocol_adapter.py:329-338
func (a *NativeHarnessProtocolAdapter) Pause(ctx context.Context) error {
	if err := a.base.RequireAccepting(); err != nil {
		return err
	}
	turn := a.base.ActiveTurn()
	if turn == nil {
		return nil
	}
	agent, err := a.readyNative(ctx, turn)
	if err != nil {
		return err
	}
	if err := agent.Pause(ctx); err != nil {
		return err
	}
	if agent.State() == hp.HarnessStatePaused {
		select {
		case <-a.pausedObserved:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// Send 在 cold restore 未续跑前拒收新输入。
// Python: protocol_adapter.py:340-343
func (a *NativeHarnessProtocolAdapter) Send(ctx context.Context, content hp.HarnessInput, mode hp.DeliveryMode) (hp.SendReceipt, error) {
	a.mu.Lock()
	cold := a.coldRestore != nil
	a.mu.Unlock()
	if cold {
		return hp.SendReceipt{}, &hp.HarnessStateError{Msg: "resume the restored paused turn before sending new input"}
	}
	return a.base.Send(ctx, content, mode)
}

// Resume 恢复 warm 状态或由 checkpoint 恢复的暂停 Turn。
// Python: protocol_adapter.py:345-365
func (a *NativeHarnessProtocolAdapter) Resume(ctx context.Context, query *hp.HarnessInput) error {
	if err := a.base.RequireAccepting(); err != nil {
		return err
	}
	a.mu.Lock()
	restored := a.coldRestore
	if restored != nil {
		if query != nil && restored.PausedQuery != nil &&
			harness_providers.HarnessInputText(*query) != *restored.PausedQuery {
			a.mu.Unlock()
			return &hp.HarnessStateError{Msg: "resume query does not match the checkpoint"}
		}
		a.coldRestore = nil
		a.coldTurn = restored
		a.mu.Unlock()
		if restored.PausedInput != nil {
			a.base.EnqueueRestoredTurn(restored.PausedInput.Restore())
		}
		for i := range restored.Queued {
			a.base.EnqueueRestoredTurn(restored.Queued[i].Restore())
		}
		return a.base.StartSupervisorForRestore(ctx)
	}
	a.mu.Unlock()
	if query != nil {
		return &hp.HarnessStateError{Msg: "cold resume requires a paused checkpoint"}
	}
	if a.agent != nil && a.agent.State() == hp.HarnessStatePaused {
		a.checkpointLock.Lock()
		err := a.agent.Resume(ctx, nil)
		a.checkpointLock.Unlock()
		if err != nil {
			return err
		}
		select {
		case <-a.resumedObserved:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// ExportCheckpoint 捕获 paused/idle 边界；永不快照执行中的工具。
// Python: protocol_adapter.py:367-378
func (a *NativeHarnessProtocolAdapter) ExportCheckpoint(ctx context.Context) (*hp.HarnessCheckpoint, error) {
	a.mu.Lock()
	cold := a.coldRestore != nil
	a.mu.Unlock()
	if cold || !a.base.CycleStarted() {
		return a.base.LatestCheckpoint(), nil
	}
	if a.agent == nil {
		return a.base.LatestCheckpoint(), nil
	}
	state := a.base.State()
	if state != hp.HarnessStateIdle && state != hp.HarnessStatePaused {
		return nil, &hp.HarnessStateError{Msg: "pause the native turn before exporting its checkpoint"}
	}
	if state == hp.HarnessStateIdle && a.hasPending() {
		return nil, &hp.HarnessStateError{Msg: "pause the native turn before exporting its checkpoint"}
	}
	return a.captureCheckpoint(ctx, state == hp.HarnessStatePaused)
}

// captureCheckpoint 8 项守卫 + 快照构造。
// Python: protocol_adapter.py:380-409 _capture_checkpoint
func (a *NativeHarnessProtocolAdapter) captureCheckpoint(ctx context.Context, paused bool) (*hp.HarnessCheckpoint, error) {
	a.checkpointLock.Lock()
	defer a.checkpointLock.Unlock()
	agent, sess := a.agent, a.agentSession
	if agent == nil || sess == nil {
		return nil, &hp.HarnessStateError{Msg: "native session is unavailable"}
	}
	st := agent.State()
	if st != hp.HarnessStateIdle && st != hp.HarnessStatePaused {
		return nil, &hp.HarnessStateError{Msg: "native session left the checkpoint boundary"}
	}
	if agent.AsyncToolRuntime().HasAnyRunning() {
		return nil, &hp.HarnessStateError{Msg: "wait for native background tools before checkpointing"}
	}
	contexts := map[string]map[string]any{}
	if ce := agent.DeepAgent().ContextEngineForCheckpoint(); ce != nil {
		saved, err := ce.SaveContexts(ctx, sess, nil)
		if err != nil {
			return nil, err
		}
		for k, v := range saved {
			if m, ok := v.(map[string]any); ok {
				contexts[k] = m
			}
		}
	}
	deepagent := agent.DeepAgent().LoadState(sess).ToSessionDict()
	// D-5 ⑤：显式取当次 GetState()，不靠 ToSessionDict 隐式带出。
	if coord := agent.Coordinator(); coord != nil {
		deepagent["stop_condition_state"] = coordinatorStateToMap(coord.GetState())
	}
	if a.currentOutputState != nil && len(a.currentOutputState.pendingInterrupts) > 0 {
		return nil, &hp.HarnessStateError{Msg: "resolve pending user interactions before checkpointing"}
	}
	var pausedInput *SavedInput
	var pausedQuery *string
	queued := []SavedInput{}
	outState := map[string]any{}
	if paused {
		active := a.base.ActiveTurn()
		if active == nil {
			return nil, &hp.HarnessStateError{Msg: "paused native turn has no resumable query"}
		}
		pq, ok := agent.PausedQuery()
		if !ok {
			return nil, &hp.HarnessStateError{Msg: "paused native turn has no resumable query"}
		}
		captured := CaptureSavedInput(active)
		pausedInput, pausedQuery = &captured, &pq
		for _, p := range a.base.PendingTurns() {
			queued = append(queued, CaptureSavedInput(p))
		}
		outState = saveOutputState(a.currentOutputState)
	}
	snap := NativeCheckpoint{
		SessionID: sess.GetSessionID(), CardID: agent.DeepAgent().Card().ID,
		Cwd:      a.checkpointCwd(*a.base.Context()),
		Contexts: EncodeContexts(contexts), DeepAgent: deepagent,
		PausedInput: pausedInput, PausedQuery: pausedQuery,
		Queued: queued, OutputState: outState,
	}
	reason := hp.CheckpointReasonTurnCompleted
	if paused {
		reason = hp.CheckpointReasonStateChanged
	}
	data, err := structToJsonObject(snap)
	if err != nil {
		return nil, err
	}
	if a.sinkErr != nil {
		return nil, a.sinkErr
	}
	return a.base.PublishCheckpoint(ctx, data, reason, checkpointSchemaVersion)
}

// Card 返回 native_v2 能力卡。
// Python: protocol_adapter.py:461-463 NativeV2HarnessProvider.card
func (p *NativeV2HarnessProvider) Card() hp.HarnessCard { return nativeV2Card() }

// Create 校验配置并构造未启动的 adapter。
// Python: protocol_adapter.py:465-487 NativeV2HarnessProvider.create
func (p *NativeV2HarnessProvider) Create(config hp.JsonObject) (hp.HarnessProtocol, error) {
	values := hp.JsonValueToBuiltin(config)
	m, ok := values.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("native_v2 configuration must be an object")
	}
	unknown := make([]string, 0, len(m))
	for k := range m {
		if !knownV2Keys[k] {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return nil, fmt.Errorf("unknown native_v2 configuration fields: %s", strings.Join(unknown, ", "))
	}
	capacity := 1024
	if v, exists := m["event_buffer_capacity"]; exists {
		n, isInt := v.(int)
		if _, isBool := v.(bool); isBool || !isInt || n <= 0 {
			return nil, fmt.Errorf("event_buffer_capacity must be a positive integer")
		}
		capacity = n
	}
	spec, err := deepAgentSpecFromMap(m["deep_agent"])
	if err != nil {
		return nil, err
	}
	if lang, ok := m["language"].(string); ok {
		spec.Language = lang
	}
	a := NewNativeV2Adapter(spec, nil, capacity)
	if tpl, exists := m["agent_template"]; exists {
		if err := CreateNativeHarnessProtocol(a, tpl); err != nil {
			return nil, err
		}
	}
	return a, nil
}

// CreateNativeHarnessProtocol 用模板加载器装配未启动的 adapter。
// Python: protocol_adapter.py:429-455 create_native_harness_protocol
func CreateNativeHarnessProtocol(a *NativeHarnessProtocolAdapter, manifest any) error {
	tpl, err := harness_providers.LoadManifestAny(manifest)
	if err != nil {
		return err
	}
	if a.spec.Card == nil {
		card := tpl.AgentCard
		a.spec.Card = &card
	}
	if a.spec.Model == nil {
		a.spec.Model = tpl.Model
	}
	a.spec.AgentTemplateSpec = &tpl
	return nil
}

// DecodeNativeCheckpointForExport 供集成测试从 HarnessCheckpoint.Data 读回快照。
func DecodeNativeCheckpointForExport(cp *hp.HarnessCheckpoint) (NativeCheckpoint, error) {
	if cp == nil {
		return NativeCheckpoint{}, fmt.Errorf("nil checkpoint")
	}
	return decodeNativeCheckpoint(cp.Data)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// runRound 统一消费输出流：普通帧交 consumeFn，控制帧按 FIFO 触发相位。
// Python: protocol_adapter.py:254-291（控制帧处理内联在 async for 里；
// D-7 为可测抽成同文件 runRoundControl 助手，步骤顺序不变）
func (a *NativeHarnessProtocolAdapter) runRound(ctx context.Context, _ *atharness.NativeHarness, turn *harness_providers.PendingTurn, state *turnState) error {
	for {
		chunk, ok := a.nextOutput(ctx)
		if !ok {
			return &hp.HarnessProtocolError{Msg: "NativeHarness output closed before reaching IDLE"}
		}
		if chunkTypeOf(chunk) != controlChunkType {
			if err := a.consumeFn(ctx, turn, state, chunk); err != nil {
				return err
			}
			continue
		}
		if done, err := a.runRoundControl(ctx, turn, state, controlPayloadOf(chunk)); err != nil {
			return err
		} else if done {
			return nil
		}
	}
}

// runRoundControl 处理单个控制帧；返回 true 表示 IDLE 应结束本轮。
// Python: protocol_adapter.py:258-289 内联段（D-7）
func (a *NativeHarnessProtocolAdapter) runRoundControl(ctx context.Context, turn *harness_providers.PendingTurn, state *turnState, payload map[string]any) (bool, error) {
	switch payload["round"] {
	case "started":
		state.err = nil
	case "failed":
		state.err = &hp.TurnError{Message: "NativeHarness round failed", Category: strPtr("sdk_error")}
	}
	switch payload["state"] {
	case string(hp.HarnessStatePausing):
		_ = a.base.Transition(ctx, hp.HarnessStatePausing)
	case string(hp.HarnessStatePaused):
		drainSignal(a.resumedObserved)
		_ = a.base.Transition(ctx, hp.HarnessStatePaused)
		_ = a.emitLifecycle(ctx, turn, hp.TurnEventKindPaused)
		if _, err := a.ExportCheckpoint(ctx); err != nil {
			logger.Error(logComponent).Err(err).Str("phase", "paused").
				Str("event_type", "LLM_CALL_ERROR").Str("method", "exportCheckpoint").
				Str("model_provider", nativeV2ProviderName).Msg("checkpoint capture failed at pause")
		}
		signal(a.pausedObserved)
	case string(hp.HarnessStateRunning):
		wasPaused := a.base.State() == hp.HarnessStatePaused
		drainSignal(a.pausedObserved)
		_ = a.base.Transition(ctx, hp.HarnessStateRunning)
		if wasPaused {
			_ = a.emitLifecycle(ctx, turn, hp.TurnEventKindResumed)
			signal(a.resumedObserved)
		}
	case string(hp.HarnessStateIdle):
		if !a.hasPending() && !turn.StopRequested() {
			if _, err := a.captureCheckpoint(ctx, false); err != nil {
				logger.Error(logComponent).Err(err).Str("phase", "idle").
					Str("event_type", "LLM_CALL_ERROR").Str("method", "captureCheckpoint").
					Str("model_provider", nativeV2ProviderName).Msg("checkpoint capture failed at turn end")
			}
		}
		return true, nil
	}
	return false, nil
}

// emitLifecycle 发射轮次生命周期事件。
// Python: protocol_adapter.py:296-306 _emit
//
// Python 用 replace(payload, provider=self.card.name) 改写 ProviderEvent；
// Go 侧 base.Emit 已用 h.card.Name（=native_v2）填信封 provider，结构等价
// （不新增 ProviderEvent 构造，避免偏离 15.5 既有事件链）。
func (a *NativeHarnessProtocolAdapter) emitLifecycle(ctx context.Context, turn *harness_providers.PendingTurn, kind hp.TurnEventKind) error {
	if a.emitLifecycleFn != nil {
		return a.emitLifecycleFn(ctx, turn, kind)
	}
	event, err := hp.NewTurnLifecycleEvent(kind, nil)
	if err != nil {
		return err
	}
	return a.base.Emit(ctx, event, harness_providers.EmitOptions{Turn: turn})
}

// readyNative 等待轮次 dispatch 后返回 L2 句柄。
// Python: protocol_adapter.py:308-315 _ready_native
func (a *NativeHarnessProtocolAdapter) readyNative(ctx context.Context, turn *harness_providers.PendingTurn) (*atharness.NativeHarness, error) {
	a.mu.Lock()
	id := a.dispatchedTurnID
	a.mu.Unlock()
	if id != turn.TurnID {
		select {
		case <-a.dispatched:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if a.base.ActiveTurn() != turn || a.agent == nil {
		return nil, &hp.HarnessStateError{Msg: "the native turn is no longer active"}
	}
	return a.agent, nil
}

// buildNative 按 context.cwd 覆盖 spec 副本后造一台 L2。
// Python: protocol_adapter.py:122-126 _build_native
//
// ⚠️ D-8：Go 的 DeepAgentSpec 无 cwd 字段（上游 model_copy(update={"cwd":...})），
// 等价改写为 Workspace.RootPath（Go 侧 cwd 的唯一载体）。不改 schema 包。
func (a *NativeHarnessProtocolAdapter) buildNative(hc hp.HarnessContext) *atharness.NativeHarness {
	spec := a.spec
	if hc.Cwd != nil && *hc.Cwd != "" {
		if spec.Workspace == nil {
			spec.Workspace = &atschema.WorkspaceSpec{}
		} else {
			ws := *spec.Workspace
			spec.Workspace = &ws
		}
		spec.Workspace.RootPath = *hc.Cwd
	}
	h, err := atharness.NewNativeHarness(spec, a.buildContext)
	if err != nil {
		return nil
	}
	return h
}

// checkpointCwd 计算快照锚定的工作目录。
// Python: protocol_adapter.py:133-136 _checkpoint_cwd
func (a *NativeHarnessProtocolAdapter) checkpointCwd(hc hp.HarnessContext) string {
	root := ""
	if hc.Cwd != nil {
		root = *hc.Cwd
	}
	if root == "" && a.spec.Workspace != nil {
		root = a.spec.Workspace.RootPath
	}
	if root == "" {
		root = "."
	}
	abs, err := filepath.Abs(expandHome(root))
	if err != nil {
		return root
	}
	return abs
}

// expandHome 展开 ~ 前缀。
// Python: Path(root).expanduser()
func expandHome(root string) string {
	if root == "~" || strings.HasPrefix(root, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, strings.TrimPrefix(root, "~"))
		}
	}
	return root
}

// decodeNativeCheckpoint 从 JsonObject 还原 NativeCheckpoint。
// Python: protocol_adapter.py:155 NativeCheckpoint.model_validate(...)
func decodeNativeCheckpoint(data hp.JsonObject) (NativeCheckpoint, error) {
	raw, err := json.Marshal(data)
	if err != nil {
		return NativeCheckpoint{}, err
	}
	if err := ValidateNativeCheckpointJSON(raw); err != nil {
		return NativeCheckpoint{}, err
	}
	var cp NativeCheckpoint
	if err := json.Unmarshal(raw, &cp); err != nil {
		return NativeCheckpoint{}, err
	}
	if cp.Queued == nil {
		cp.Queued = []SavedInput{}
	}
	if cp.OutputState == nil {
		cp.OutputState = map[string]any{}
	}
	if cp.Contexts == nil {
		cp.Contexts = map[string]map[string]any{}
	}
	if cp.DeepAgent == nil {
		cp.DeepAgent = map[string]any{}
	}
	return cp, nil
}

// coordinatorStateFromMap 把快照里的 stop_condition_state 还原为 LoopCoordinatorState。
// D-5：Python load_state(data) 的 Go 等价（D-6 改名后为 LoadState）。
func coordinatorStateFromMap(v any) taskloop.LoopCoordinatorState {
	m, _ := v.(map[string]any)
	stopReason, _ := m["stop_reason"].(string)
	evStates := make(map[string]map[string]any)
	if raw, ok := m["evaluator_states"].(map[string]any); ok {
		for k, val := range raw {
			if mm, ok := val.(map[string]any); ok {
				evStates[k] = mm
			}
		}
	}
	return taskloop.LoopCoordinatorState{
		Iteration: intFromAny(m["iteration"]), TokenUsage: intFromAny(m["token_usage"]),
		StopReason: stopReason, EvaluatorStates: evStates,
	}
}

// coordinatorStateToMap 把 LoopCoordinatorState 转为快照 dict（D-5 ⑤ 配套）。
func coordinatorStateToMap(s taskloop.LoopCoordinatorState) map[string]any {
	ev := make(map[string]any, len(s.EvaluatorStates))
	for k, v := range s.EvaluatorStates {
		ev[k] = v
	}
	return map[string]any{
		"iteration": s.Iteration, "token_usage": s.TokenUsage,
		"stop_reason": s.StopReason, "evaluator_states": ev,
	}
}

// intFromAny 把 JSON 数字（float64/int）统一取成 int。
func intFromAny(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case float64:
		return int(n)
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	}
	return 0
}

// saveOutputState 输出态 6 字段经 ToJsonSafe 序列化。
// Python: protocol_adapter.py:411-415
func saveOutputState(state *turnState) map[string]any {
	if state == nil {
		return map[string]any{}
	}
	values := map[string]any{
		"answer_parts":    state.answerParts,
		"reasoning_parts": state.reasoningParts,
		"final_output":    state.finalOutput,
		"result_type":     state.resultType,
		"tool_blocks":     state.toolBlocks,
		"tool_messages":   state.toolMessages,
	}
	out := make(map[string]any, len(outputStateFields))
	for _, name := range outputStateFields {
		out[name] = harness_providers.ToJsonSafe(values[name])
	}
	return out
}

// restoreOutputState 从快照还原输出态 6 字段。
// Python: protocol_adapter.py:417-426
func restoreOutputState(state *turnState, values map[string]any) error {
	state.answerParts = toStringSlice(values["answer_parts"])
	state.reasoningParts = toStringSlice(values["reasoning_parts"])
	state.finalOutput = stringPtrFromAny(values["final_output"])
	state.resultType = stringPtrFromAny(values["result_type"])
	blocks, err := contentBlocksFromAny(values["tool_blocks"])
	if err != nil {
		return err
	}
	state.toolBlocks = blocks
	msgs, err := turnMessagesFromAny(values["tool_messages"])
	if err != nil {
		return err
	}
	state.toolMessages = msgs
	return nil
}

// toStringSlice 从 JSON-native 列表还原字符串切片。
func toStringSlice(v any) []string {
	list, _ := v.([]any)
	out := make([]string, 0, len(list))
	for _, item := range list {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// stringPtrFromAny 从 JSON-native 值还原 *string（nil 保持 nil）。
func stringPtrFromAny(v any) *string {
	s, ok := v.(string)
	if !ok {
		return nil
	}
	return &s
}

// contentBlocksFromAny 从 JSON-native 列表重建 []ContentBlock。
// Python: :423 [ContentBlock(**item) for item in values.get("tool_blocks", [])]
func contentBlocksFromAny(v any) ([]hp.ContentBlock, error) {
	list, _ := v.([]any)
	out := make([]hp.ContentBlock, 0, len(list))
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("invalid native checkpoint")
		}
		raw, err := json.Marshal(m)
		if err != nil {
			return nil, err
		}
		var block hp.ContentBlock
		if err := json.Unmarshal(raw, &block); err != nil {
			return nil, err
		}
		out = append(out, block)
	}
	return out, nil
}

// turnMessagesFromAny 从 JSON-native 列表重建 []TurnMessage。
// Python: :424-426 [TurnMessage(message_id=..., role=MessageRole(...),
//
//	content=tuple(ContentBlock(**block) ...), data=...) ...]
func turnMessagesFromAny(v any) ([]hp.TurnMessage, error) {
	list, _ := v.([]any)
	out := make([]hp.TurnMessage, 0, len(list))
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("invalid native checkpoint")
		}
		blocks, err := contentBlocksFromAny(m["content"])
		if err != nil {
			return nil, err
		}
		msgID, _ := m["message_id"].(string)
		role, _ := m["role"].(string)
		data, _ := m["data"].(map[string]any)
		msg, err := hp.NewTurnMessage(msgID, hp.MessageRole(role), blocks, data)
		if err != nil {
			return nil, err
		}
		out = append(out, msg)
	}
	return out, nil
}

// structToJsonObject 把快照结构体转成协议 JsonObject。
// Python: snapshot.model_dump(mode="json")
func structToJsonObject(snap NativeCheckpoint) (hp.JsonObject, error) {
	raw, err := json.Marshal(snap)
	if err != nil {
		return nil, err
	}
	var out hp.JsonObject
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// deepAgentSpecFromMap 从配置字典解析 DeepAgentSpec。
// Python: :473 DeepAgentSpec.model_validate(values.get("deep_agent") or {})
func deepAgentSpecFromMap(v any) (atschema.DeepAgentSpec, error) {
	spec := atschema.NewDeepAgentSpec()
	if v == nil {
		return spec, nil
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return spec, err
	}
	if err := json.Unmarshal(raw, &spec); err != nil {
		return spec, err
	}
	return spec, nil
}

// chunkTypeOf 读取输出帧的 type 字段。
func chunkTypeOf(chunk stream.Schema) string {
	if s, ok := chunk.(*stream.OutputSchema); ok {
		return s.Type
	}
	return ""
}

// controlPayloadOf 读取控制帧的 payload 字段。
func controlPayloadOf(chunk stream.Schema) map[string]any {
	if s, ok := chunk.(*stream.OutputSchema); ok {
		if m, ok := s.Payload.(map[string]any); ok {
			return m
		}
	}
	return map[string]any{}
}

// nextOutput 读取下一帧（生产走 nativeOutputs，测试走注入的 outputNext）。
func (a *NativeHarnessProtocolAdapter) nextOutput(ctx context.Context) (stream.Schema, bool) {
	if a.outputNext != nil {
		return a.outputNext(ctx)
	}
	if a.nativeOutputs == nil {
		return nil, false
	}
	return a.nativeOutputs.Next(ctx)
}

// hasPending 是否还有待决轮次。
// Python: protocol_adapter.py:284 self._pending
func (a *NativeHarnessProtocolAdapter) hasPending() bool { return len(a.base.PendingTurns()) > 0 }

// drainSignal 非阻塞排空容量为 1 的信号 channel。
func drainSignal(ch chan struct{}) {
	select {
	case <-ch:
	default:
	}
}

// signal 非阻塞置位容量为 1 的信号 channel。
func signal(ch chan struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}

// record 记录装配步骤（仅测试注入时生效）。
func (a *NativeHarnessProtocolAdapter) record(name string) {
	if a.recordStep != nil {
		a.recordStep(name)
	}
}
