package runner

import (
	"context"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/spawn"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ChildRunnerImpl 子进程 Runner 实现，满足 spawn.ChildRunner 接口。
// 在子进程中通过 SetConfig→Start→RunAgent→Stop 驱动 Agent 执行。
type ChildRunnerImpl struct{}

// ──────────────────────────── 全局变量 ────────────────────────────

// 编译期校验：ChildRunnerImpl 必须满足 spawn.ChildRunner 接口
var _ spawn.ChildRunner = (*ChildRunnerImpl)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// SetConfig 设置 Runner 配置。
// 对齐 Python: Runner.set_config(config)
func (c *ChildRunnerImpl) SetConfig(runnerConfig map[string]any) error {
	cfg, err := spawn.DeserializeRunnerConfig(runnerConfig)
	if err != nil {
		return fmt.Errorf("反序列化 Runner 配置失败: %w", err)
	}
	SetConfig(cfg)
	return nil
}

// Start 启动 Runner。
// 对齐 Python: await Runner.start()
func (c *ChildRunnerImpl) Start(ctx context.Context) error {
	return Start(ctx)
}

// Stop 停止 Runner。
// 对齐 Python: await Runner.stop()
func (c *ChildRunnerImpl) Stop(ctx context.Context) error {
	return Stop(ctx)
}

// RunAgent 执行 Agent（非流式）。
// 将 BaseAgent 转为 AgentRef，sessionID 转为 SessionRef，后调用 runner.RunAgent。
// 对齐 Python: Runner.run_agent(agent=agent, inputs=inputs, session=session)
func (c *ChildRunnerImpl) RunAgent(ctx context.Context, agent interfaces.BaseAgent, inputs map[string]any, sessionID string) (map[string]any, error) {
	agentRef := ByAgent(agent)
	sessionRef := SessionRef{}
	if sessionID != "" {
		sessionRef = BySessionID(sessionID)
	}
	return RunAgent(ctx, agentRef, inputs, sessionRef, nil, nil)
}

// RunAgentStreaming 执行 Agent（流式）。
// 将 BaseAgent 转为 AgentRef，sessionID 转为 SessionRef，后调用 runner.RunAgentStreaming。
// 对齐 Python: Runner.run_agent_streaming(agent, inputs, session=session, stream_modes=stream_modes)
func (c *ChildRunnerImpl) RunAgentStreaming(ctx context.Context, agent interfaces.BaseAgent, inputs map[string]any, sessionID string, streamModes any) (<-chan stream.Schema, error) {
	agentRef := ByAgent(agent)
	sessionRef := SessionRef{}
	if sessionID != "" {
		sessionRef = BySessionID(sessionID)
	}
	return RunAgentStreaming(ctx, agentRef, inputs, sessionRef, nil, streamModes, nil)
}
