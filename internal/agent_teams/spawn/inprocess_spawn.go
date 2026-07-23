package spawn

import (
	"context"
	"fmt"

	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// AgentFactory 创建并配置 Agent 的工厂函数。
// 对齐 Python: _TeamAgent(card) + teammate.configure(spec, ctx)
// 由 SpawnManager 注入具体实现，封装 spec 解析 / card 构建 / 配置全流程。
type AgentFactory func(runtimeCtx atschema.TeamRuntimeContext) (SpawnableAgent, error)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// defaultInitialMessage 默认初始消息
	// 对齐 Python: "Join the team and wait for your first assignment."
	defaultInitialMessage = "Join the team and wait for your first assignment."
)

// ──────────────────────────── 导出函数 ────────────────────────────

// InProcessSpawn 以进程内 goroutine 方式生成 teammate。
// 对齐 Python: inprocess_spawn(team_agent, ctx, initial_message, session_id)
//
// 核心逻辑：
//  1. 调用工厂创建并配置 teammate
//  2. 准备输入（initialMessage 为空时使用默认消息）
//  3. 启动 goroutine 运行 Agent（对齐 Python: asyncio.create_task）
//     goroutine 内部调用 Runner.RunAgentTeam，当前 TODO(#9.85) 占位
//  4. 包装 InProcessSpawnHandle 返回
func InProcessSpawn(
	ctx context.Context,
	factory AgentFactory,
	runtimeCtx atschema.TeamRuntimeContext,
	initialMessage string,
	sessionID string,
) (*InProcessSpawnHandle, error) {
	// 1. 调用工厂创建并配置 teammate
	teammate, err := factory(runtimeCtx)
	if err != nil {
		return nil, fmt.Errorf("工厂创建 Agent 失败: %w", err)
	}

	// 2. 准备输入
	query := initialMessage
	if query == "" {
		query = defaultInitialMessage
	}
	inputs := map[string]any{"query": query}

	// 3. 启动 goroutine 运行 Agent（对齐 Python: asyncio.create_task(_run())）
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		// 对齐 Python: set_session_id(session_id)
		// Go: sessionID 通过参数传递，无需 contextvars

		logger.Info(inprocessLogComponent).
			Str("member_name", runtimeCtx.MemberName).
			Msg("[inprocess] teammate started")

		// 对齐 Python: await Runner.run_agent_team(teammate, inputs, member=True, session=session_id)
		// ⤵️ 预留：TeamRunner（9.85）实现后回填
		// _, err := runner.RunAgentTeam(runCtx, teammate, inputs, true, sessionID)
		_ = runCtx    // 同上
		_ = inputs    // 避免未使用变量警告
		_ = query     // 同上
		_ = sessionID // 同上

		logger.Info(inprocessLogComponent).
			Str("member_name", runtimeCtx.MemberName).
			Msg("[inprocess] teammate exited")
	}()

	// 4. 包装句柄
	handle := &InProcessSpawnHandle{
		processID: fmt.Sprintf("inproc-%s", runtimeCtx.MemberName),
		cancelCtx: cancel,
		done:      done,
		agentRef:  teammate,
	}

	logger.Info(inprocessLogComponent).
		Str("member_name", runtimeCtx.MemberName).
		Str("process_id", handle.processID).
		Msg("[inprocess] spawned teammate")

	return handle, nil
}
