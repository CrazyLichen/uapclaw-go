package agent

import (
	"context"
	"fmt"
	"sync"
	"time"

	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/spawn"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	runnerspawn "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/spawn"
	streambase "github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SpawnManager 管理 teammate 进程生命周期和健康监控。
// 对齐 Python: SpawnManager (openjiuwen/agent_teams/agent/spawn_manager.py)
//
// 职责：
//   - 双模式进程生成（inprocess / subprocess）
//   - 健康检查协调
//   - 进程清理和重启
//   - 生成配置构建
type SpawnManager struct {
	// state 可变运行时状态
	state *TeamAgentState
	// configurator Agent 配置器
	configurator *AgentConfigurator
	// getTeamAgent 获取当前 TeamAgent 实例的闭包
	getTeamAgent func() *TeamAgent
	// spawnedHandles 已生成的句柄，key=memberName
	spawnedHandles map[string]spawn.SpawnHandle
	// recoveryCancel 恢复任务取消函数，key=memberName
	recoveryCancel map[string]context.CancelFunc
	// mu 保护并发访问
	mu sync.Mutex
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// spawnLogComponent 日志组件
	spawnLogComponent = logger.ComponentAgentCore
	// defaultMaxRetries 默认最大重启重试次数
	// 对齐 Python: restart_teammate(max_retries=3)
	defaultMaxRetries = 3
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSpawnManager 创建新的 SpawnManager。
// 对齐 Python: SpawnManager.__init__(state, configurator, team_agent_getter)
func NewSpawnManager(
	state *TeamAgentState,
	configurator *AgentConfigurator,
	teamAgentGetter func() *TeamAgent,
) *SpawnManager {
	return &SpawnManager{
		state:          state,
		configurator:   configurator,
		getTeamAgent:   teamAgentGetter,
		spawnedHandles: make(map[string]spawn.SpawnHandle),
		recoveryCancel: make(map[string]context.CancelFunc),
	}
}

// SpawnTeammate 生成 teammate，根据 spawn_mode 选择 inprocess 或 subprocess。
// 对齐 Python: SpawnManager.spawn_teammate(ctx, initial_message, session, spawn_config)
func (m *SpawnManager) SpawnTeammate(
	ctx context.Context,
	runtimeCtx atschema.TeamRuntimeContext,
	initialMessage string,
	sessionID string,
	spawnCfg *runnerspawn.SpawnConfig,
) error {
	memberName := runtimeCtx.MemberName
	spawnMode := "process" // 默认 subprocess
	if m.configurator != nil && m.configurator.Spec() != nil {
		spawnMode = m.configurator.Spec().SpawnMode
	}

	logger.Info(spawnLogComponent).
		Str("member_name", memberName).
		Str("spawn_mode", spawnMode).
		Msg("生成 teammate")

	var handle spawn.SpawnHandle
	var err error

	if spawnMode == "inprocess" {
		handle, err = m.spawnInprocess(ctx, runtimeCtx, initialMessage, sessionID)
	} else {
		handle, err = m.spawnSubprocess(ctx, runtimeCtx, initialMessage, sessionID, spawnCfg)
	}

	if err != nil {
		return fmt.Errorf("生成 teammate %s 失败: %w", memberName, err)
	}

	// 注册不健康回调
	// InProcessSpawnHandle 和 SpawnedProcessHandle 均有 SetOnUnhealthy 方法，
	// 但 SpawnHandle 接口不含此方法，需通过类型断言注册。
	switch h := handle.(type) {
	case *spawn.InProcessSpawnHandle:
		h.SetOnUnhealthy(func() { m.OnTeammateUnhealthy(memberName) })
	case *runnerspawn.SpawnedProcessHandle:
		h.SetOnUnhealthy(func() { m.OnTeammateUnhealthy(memberName) })
	}

	m.mu.Lock()
	m.spawnedHandles[memberName] = handle
	m.mu.Unlock()

	return nil
}

// LookupInprocessAgent 查找进程内 agent 引用。
// 对齐 Python: SpawnManager.lookup_inprocess_agent(member_name)
//
// 返回 nil 如果该成员不是 inprocess 模式或不存在。
func (m *SpawnManager) LookupInprocessAgent(memberName string) spawn.SpawnableAgent {
	m.mu.Lock()
	handle, ok := m.spawnedHandles[memberName]
	m.mu.Unlock()

	if !ok {
		return nil
	}

	inproc, ok := handle.(*spawn.InProcessSpawnHandle)
	if !ok {
		return nil
	}

	return inproc.AgentRef()
}

// CleanupTeammate 清理单个 teammate。
// 对齐 Python: SpawnManager.cleanup_teammate(member_name)
//
// 先断开 chunk_forward 观察者，再 force_kill。
func (m *SpawnManager) CleanupTeammate(ctx context.Context, memberName string) {
	m.mu.Lock()
	handle, ok := m.spawnedHandles[memberName]
	if ok {
		delete(m.spawnedHandles, memberName)
	}
	m.mu.Unlock()

	if !ok {
		return
	}

	// 断开 chunk_forward 观察者（对齐 Python: handle.chunk_forward = None）
	if inproc, ok := handle.(*spawn.InProcessSpawnHandle); ok {
		inproc.SetChunkForward(nil)
	}

	// 强制终止
	_ = handle.ForceKill()

	logger.Info(spawnLogComponent).
		Str("member_name", memberName).
		Msg("已清理 teammate")
}

// RestartTeammate 重启 teammate，指数退避重试。
// 对齐 Python: SpawnManager.restart_teammate(member_name, max_retries=3)
func (m *SpawnManager) RestartTeammate(ctx context.Context, memberName string, maxRetries int) error {
	if maxRetries <= 0 {
		maxRetries = defaultMaxRetries
	}

	// 清理旧句柄
	m.CleanupTeammate(ctx, memberName)

	// 从 DB 恢复上下文
	runtimeCtx, err := m.BuildContextFromDB(memberName)
	if err != nil {
		return fmt.Errorf("恢复 %s 上下文失败: %w", memberName, err)
	}

	// 指数退避重试
	for attempt := 1; attempt <= maxRetries; attempt++ {
		err := m.SpawnTeammate(ctx, runtimeCtx, "", "", nil)
		if err == nil {
			m.PublishRestartEvent(memberName, attempt)
			logger.Info(spawnLogComponent).
				Str("member_name", memberName).
				Int("attempt", attempt).
				Msg("重启 teammate 成功")
			return nil
		}

		logger.Warn(spawnLogComponent).
			Str("member_name", memberName).
			Int("attempt", attempt).
			Int("max_retries", maxRetries).
			Err(err).
			Msg("重启 teammate 失败")

		if attempt < maxRetries {
			backoff := time.Duration(1<<uint(attempt)) * time.Second
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
		}
	}

	return fmt.Errorf("重启 teammate %s 失败：超过最大重试次数 %d", memberName, maxRetries)
}

// OnTeammateUnhealthy 不健康回调，标记 RESTARTING 并尝试重启。
// 对齐 Python: SpawnManager.on_teammate_unhealthy(member_name)
func (m *SpawnManager) OnTeammateUnhealthy(memberName string) {
	logger.Warn(spawnLogComponent).
		Str("member_name", memberName).
		Msg("teammate 不健康，尝试重启")

	// 在独立 goroutine 中重启，避免阻塞健康检查
	recoverCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)

	m.mu.Lock()
	m.recoveryCancel[memberName] = cancel
	m.mu.Unlock()

	go func() {
		defer cancel()
		if err := m.RestartTeammate(recoverCtx, memberName, defaultMaxRetries); err != nil {
			logger.Error(spawnLogComponent).
				Str("member_name", memberName).
				Err(err).
				Msg("重启 teammate 最终失败")
			// TODO(#9.64): 更新 DB 状态为 ERROR
		}
	}()
}

// BuildContextFromDB 从 DB 恢复 TeamRuntimeContext。
// 对齐 Python: SpawnManager.build_context_from_db(member_name)
// ⤵️ 预留：TeamDatabase（9.64）实现后回填
func (m *SpawnManager) BuildContextFromDB(memberName string) (atschema.TeamRuntimeContext, error) {
	// TODO(#9.64): 从 TeamDatabase 读取 teammate 行
	// 解析 model_ref_json → resolve_member_model
	// 构建 TeamRuntimeContext (role, member_name, persona, team_spec, ...)
	logger.Debug(spawnLogComponent).
		Str("member_name", memberName).
		Msg("BuildContextFromDB 当前返回空上下文（TODO #9.64）")
	return atschema.TeamRuntimeContext{}, nil
}

// PublishRestartEvent 发布重启事件。
// 对齐 Python: SpawnManager.publish_restart_event(member_name, restart_count)
// ⤵️ 预留：Messager（9.65）实现后回填
func (m *SpawnManager) PublishRestartEvent(memberName string, restartCount int) {
	// TODO(#9.65): 通过 Messager 发布 MemberRestartedEvent
	logger.Debug(spawnLogComponent).
		Str("member_name", memberName).
		Int("restart_count", restartCount).
		Msg("PublishRestartEvent 当前为 no-op（TODO #9.65）")
}

// ShutdownAllHandles 关闭所有已生成的句柄。
// 对齐 Python: SpawnManager.shutdown_all_handles()
func (m *SpawnManager) ShutdownAllHandles(ctx context.Context) {
	m.mu.Lock()
	handles := make(map[string]spawn.SpawnHandle)
	for k, v := range m.spawnedHandles {
		handles[k] = v
	}
	m.spawnedHandles = make(map[string]spawn.SpawnHandle)
	m.mu.Unlock()

	for memberName, handle := range handles {
		// 断开 chunk_forward
		if inproc, ok := handle.(*spawn.InProcessSpawnHandle); ok {
			inproc.SetChunkForward(nil)
		}
		_ = handle.ForceKill()
		logger.Info(spawnLogComponent).
			Str("member_name", memberName).
			Msg("已关闭 teammate 句柄")
	}
}

// CancelRecoveryTasks 取消所有恢复任务。
// 对齐 Python: SpawnManager.cancel_recovery_tasks()
func (m *SpawnManager) CancelRecoveryTasks() {
	m.mu.Lock()
	cancels := make(map[string]context.CancelFunc)
	for k, v := range m.recoveryCancel {
		cancels[k] = v
	}
	m.recoveryCancel = make(map[string]context.CancelFunc)
	m.mu.Unlock()

	for _, cancel := range cancels {
		cancel()
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// spawnInprocess 以 inprocess 模式生成 teammate。
// 对齐 Python: inprocess_spawn(team_agent, ctx, initial_message, session_id)
func (m *SpawnManager) spawnInprocess(
	ctx context.Context,
	runtimeCtx atschema.TeamRuntimeContext,
	initialMessage string,
	sessionID string,
) (*spawn.InProcessSpawnHandle, error) {
	// 构建工厂函数（对齐 Python: _TeamAgent(card) + teammate.configure(spec, ctx)）
	factory := func(ctx atschema.TeamRuntimeContext) (spawn.SpawnableAgent, error) {
		// 对齐 Python: agent_spec = spec.agents.get(ctx.role.value) or spec.agents["leader"]
		spec := m.configurator.Spec()
		agentSpec := ResolveAgentSpec(*spec, ctx.Role, ctx.MemberName)

		// 对齐 Python: card = agent_spec.card or AgentCard(...)
		card := agentSpec.Card
		if card == nil {
			card = agentschema.NewAgentCard(
				agentschema.WithAgentID(fmt.Sprintf("%s_%s", spec.TeamName, ctx.MemberName)),
				agentschema.WithAgentName(ctx.MemberName),
				agentschema.WithAgentDescription(fmt.Sprintf("Teammate: %s", ctx.Persona)),
			)
		}

		// 对齐 Python: teammate = _TeamAgent(card)
		teammate := NewTeamAgent(card)

		// 对齐 Python: teammate.configure(spec, ctx)
		teammate.Configure(context.Background(), *spec, ctx)

		return teammate, nil
	}

	handle, err := spawn.InProcessSpawn(ctx, factory, runtimeCtx, initialMessage, sessionID)
	if err != nil {
		return nil, err
	}

	// 接入 chunk 转发观察者
	m.wireInprocessChunkForward(handle)

	return handle, nil
}

// spawnSubprocess 以 subprocess 模式生成 teammate。
// 对齐 Python: Runner.spawn_agent(build_spawn_config(ctx), build_spawn_payload(ctx), session, spawn_config)
func (m *SpawnManager) spawnSubprocess(
	ctx context.Context,
	runtimeCtx atschema.TeamRuntimeContext,
	initialMessage string,
	sessionID string,
	spawnCfg *runnerspawn.SpawnConfig,
) (spawn.SpawnHandle, error) {
	// 构建载荷
	// ⤵️ 待回填：payload 需要传入 SpawnAgent 调用
	payload := m.configurator.BuildSpawnPayload(runtimeCtx, initialMessage)
	if payload == nil {
		payload = make(map[string]any)
	}
	_ = payload

	// 构建配置
	agentConfig := m.configurator.BuildSpawnConfig(runtimeCtx)

	inputs := map[string]any{"query": initialMessage}
	if initialMessage == "" {
		inputs["query"] = "Join the team and wait for your first assignment."
	}

	// 调用 Runner.SpawnAgent
	// 对齐 Python: Runner.spawn_agent()
	var spawnOpts []runnerspawn.SpawnConfig
	if spawnCfg != nil {
		spawnOpts = append(spawnOpts, *spawnCfg)
	}
	handle, err := runner.SpawnAgent(ctx, agentConfig, inputs, nil, nil, spawnOpts...)
	if err != nil {
		return nil, fmt.Errorf("子进程生成失败: %w", err)
	}

	return handle, nil
}

// wireInprocessChunkForward 接入 chunk 转发观察者。
// 对齐 Python: SpawnManager._wire_inprocess_chunk_forward(handle)
func (m *SpawnManager) wireInprocessChunkForward(handle *spawn.InProcessSpawnHandle) {
	agentRef := handle.AgentRef()
	ta, ok := agentRef.(*TeamAgent)
	if !ok {
		return
	}
	teammateSC := ta.StreamController()
	if teammateSC == nil {
		return
	}
	leaderSC := m.getTeamAgent().StreamController()
	if leaderSC == nil {
		return
	}
	// 对齐 Python: 创建转发回调 teammate chunk → leader streamQueue
	forwardCb := func(ctx context.Context, chunk streambase.Schema) error {
		if leaderSC.streamQueue != nil {
			select {
			case leaderSC.streamQueue <- chunk:
			default:
			}
		}
		return nil
	}
	teammateSC.AddChunkObserver(forwardCb)
	handle.SetChunkForward(forwardCb)
}
