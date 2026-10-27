package spawn

import (
	"context"
	"fmt"

	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/sessionctx"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AgentFactory 创建并配置 Agent 的工厂函数。
// Python: _TeamAgent(card) + teammate.configure(spec, ctx)
// 由 SpawnManager 注入具体实现，封装 spec 解析 / card 构建 / 配置全流程。
type AgentFactory func(runtimeCtx atschema.TeamRuntimeContext) (SpawnableAgent, error)

// RunTeamMemberFn 团队成员执行函数类型。
// T-14: 替代原 any 注入缝——具体类型使注入时类型错误在编译期暴露。
// spawn 不能 import agent 包（agent → spawn 已存在，反向成环），
// 参数用本包 SpawnableAgent 接口；注入侧（runtime/manager.go）负责收窄到 *agent.TeamAgent。
type RunTeamMemberFn func(ctx context.Context, agentTeam SpawnableAgent, inputs map[string]any, sessionID string) (map[string]any, error)

// inProcessResult S-38: goroutine 执行结果载体。
// 对齐 Python asyncio task 的 result/exception 存储。
type inProcessResult struct {
	// res 执行结果
	res map[string]any
	// err 执行错误（对齐 Python task.exception()）
	err error
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// defaultInitialMessage 默认初始消息
	// Python: "Join the team and wait for your first assignment."
	defaultInitialMessage = "Join the team and wait for your first assignment."
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// runTeamMemberFunc 团队成员执行函数（由 runtime 包注入，打破 spawn ↔ runtime 循环依赖）。
	// T-14: 具体函数类型（原 any + 运行时断言）。
	runTeamMemberFunc RunTeamMemberFn
)

// ──────────────────────────── 导出函数 ────────────────────────────

// SetRunTeamMemberFunc 设置团队成员执行函数（由 runtime 包注入）。
func SetRunTeamMemberFunc(fn RunTeamMemberFn) {
	runTeamMemberFunc = fn
}

// InProcessSpawn 以进程内 goroutine 方式生成 teammate。
// Python: inprocess_spawn(team_agent, ctx, initial_message, session_id)
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
	// S-39: 对齐 Python inprocess_spawn.py L70-74 set_session_id(session_id)
	// 把 sessionID 注入 runCtx，teammate goroutine 内部 GetSessionID(ctx) 可读取
	if sessionID != "" {
		state := sessionctx.InitSessionState()
		state.SetSessionID(sessionID)
		runCtx = sessionctx.WithSessionState(runCtx, state)
	}
	done := make(chan struct{})
	// S-38: 结果通道，携带 runFn 的 result/err（对齐 Python task.exception() 传播）
	resultCh := make(chan inProcessResult, 1)
	go func() {
		defer close(done)
		// Python: set_session_id(session_id) — 已通过 runCtx 注入（见上）

		logger.Info(logComponent).
			Str("member_name", runtimeCtx.MemberName).
			Msg("[inprocess] teammate started")

		// Python: await Runner.run_agent_team(teammate, inputs, member=True, session=session_id)
		// 回填（#9.85）：调用 TeamRunner 的 Member 路径（通过函数注入打破循环依赖）
		// S-38: 捕获 result/err 通过 resultCh 传播，不再静默吞掉
		// T-14: 注入缝已是具体函数类型，无需运行时类型断言（类型错误在注入时编译期暴露）
		var result inProcessResult
		if runTeamMemberFunc != nil {
			res, err := runTeamMemberFunc(runCtx, teammate, inputs, sessionID)
			// 对齐 Python: 异常存入 task，wait_for_completion 时可读出
			result = inProcessResult{res: res, err: err}
			if err != nil {
				// Python: cancelled → info；crashed → error 日志（re-raise 语义由 resultCh 承载）
				if runCtx.Err() != nil {
					logger.Info(logComponent).
						Str("member_name", runtimeCtx.MemberName).
						Err(err).
						Msg("[inprocess] teammate execution cancelled")
				} else {
					logger.Error(logComponent).
						Str("member_name", runtimeCtx.MemberName).
						Err(err).
						Msg("[inprocess] teammate execution failed")
				}
			}
		} else {
			logger.Error(logComponent).
				Str("member_name", runtimeCtx.MemberName).
				Msg("[inprocess] runTeamMemberFunc is nil, teammate cannot execute")
		}
		resultCh <- result

		logger.Info(logComponent).
			Str("member_name", runtimeCtx.MemberName).
			Msg("[inprocess] teammate exited")
	}()

	// 4. 包装句柄
	handle := &InProcessSpawnHandle{
		processID: fmt.Sprintf("inproc-%s", runtimeCtx.MemberName),
		cancelCtx: cancel,
		done:      done,
		agentRef:  teammate,
		resultCh:  resultCh,
	}

	logger.Info(logComponent).
		Str("member_name", runtimeCtx.MemberName).
		Str("process_id", handle.processID).
		Msg("[inprocess] spawned teammate")

	return handle, nil
}
