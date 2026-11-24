// ReActExecutable / ReActAgentComp——8.26 薄壳执行体与组合面。
// Python: react_executable.py（90 行一比一）+ react_component.py（26 行一比一）。
//
// Python 构造期的 lazy import（react_executable.py L18-120 注释：react_agent.py →
// base.py → ability_manager.py → workflow/__init__.py → react_executable.py 循环）
// 在 Go 侧不存在该循环，直接 import；原注释语义保留于此。
package react

import (
	"context"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine"
	ceiface "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/interface"
	ceschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/graph/executable"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/agents"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow/components"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ReActExecutable 可执行体（Python ReActAgentCompExecutable）。
type ReActExecutable struct {
	// WorkflowComponent 8.17 组件基座（四槽 + 组合面）
	components.WorkflowComponent
	// cfg 组件配置（Python self.config）
	cfg *ReActAgentCompConfig
	// reactAgent 内嵌 ReAct Agent（Python self._react_agent；固定 card）
	reactAgent *agents.ReActAgent
}

// ReActAgentComp 组合面（Python ReActAgentComp(ComponentComposable)）。
type ReActAgentComp struct {
	// BaseComponentComposable 组合面基座（Python ComponentComposable mixin 嵌入；
	// executable property 懒初始化由 ToExecutableFn 槽缓存承载，8.18 D-18-4 同款）
	components.BaseComponentComposable
	// cfg 组件配置（Python self._config）
	cfg *ReActAgentCompConfig
	// executable 缓存产物（Python self._executable）
	executable *ReActExecutable
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewReActAgentComp 构造组件组合面（Python __init__ L12-116）。
func NewReActAgentComp(cfg *ReActAgentCompConfig) *ReActAgentComp {
	c := &ReActAgentComp{cfg: cfg}
	c.ToExecutableFn = c.toExecutableCached
	return c
}

// NewReActExecutable 构造执行体（Python __init__ L20-135）。
func NewReActExecutable(cfg *ReActAgentCompConfig) *ReActExecutable {
	e := &ReActExecutable{cfg: cfg}
	// 固定 card（Python L22-128）
	e.reactAgent = agents.NewReActAgent(
		agentschema.NewAgentCard(
			agentschema.WithAgentID("react_agent_workflow_executable"),
			agentschema.WithAgentName("ReAct Agent Workflow Executable"),
			agentschema.WithAgentDescription("ReAct agent for workflow execution"),
		),
		cfg.ReActAgentConfig,
	)
	// Python L129: self._react_agent.configure(config)
	// 偏差 D-26-6：Go 无继承，ReActAgentCompConfig 嵌入类型无法通过 Configure
	// 的 *ReActAgentConfig 类型断言，传入嵌入的 ReActAgentConfig（语义等价）
	if err := e.reactAgent.Configure(context.Background(), cfg.ReActAgentConfig); err != nil {
		panic(err) // Python configure 异常上抛 → Go 构造期 panic 承载（8.18 同款）
	}
	// Python ReActAgent.__init__ 总是自建 ContextEngine（react_agent.py:
	// self.context_engine = ContextEngine(...)，即使使用默认配置）——
	// Go 由装配方注入（SetContextEngine），薄壳按同语义补齐（真链 e2e 实证
	// 缺口：无 ContextEngine 时 messages 仅空 system、query 不入历史 →
	// 智谱 400 "messages 参数非法"）
	e.reactAgent.SetContextEngine(context_engine.NewContextEngine(
		ceschema.NewContextEngineConfig()))
	// 四槽装配
	e.InvokeFn = e.invokeEntry
	e.StreamFn = e.streamEntry
	return e
}

// ReactAgent 返回内嵌 ReAct Agent（Go 侧组合面访问器；对应 Python ability_manager
// property 的内嵌对象暴露面——ability_manager 经 agent.AbilityManager() 直达）。
func (e *ReActExecutable) ReactAgent() *agents.ReActAgent {
	return e.reactAgent
}

// AbilityManager 能力管理器访问器（Python ability_manager property L40-152：
// 公开接口用于添加工具/工作流/Agent）。
func (e *ReActExecutable) AbilityManager() interfaces.AbilityManagerInterface {
	return e.reactAgent.AbilityManager()
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// toExecutableCached 可执行体供应槽（Python to_executable L21-123 + executable
// property 懒初始化合并承载：首调构造并缓存）。
func (c *ReActAgentComp) toExecutableCached() executable.GeneralExecutor {
	if c.executable == nil {
		c.executable = NewReActExecutable(c.cfg)
	}
	return c.executable
}

// invokeEntry 批入批出（Python invoke L37-147）。
func (e *ReActExecutable) invokeEntry(
	ctx context.Context,
	inputs map[string]any,
	sess *session.NodeSessionFacade,
	_ ceiface.ModelContext,
) (map[string]any, error) {
	// Python L40-143：委托 ReActAgent.invoke(inputs, session)；异常吞掉转
	// {"output": "Error in ReAct execution: <err>", "result_type": "error"}（文案逐字）
	result, err := e.reactAgent.Invoke(ctx, inputs, interfaces.WithSession(sess))
	if err != nil {
		return map[string]any{
			"output":      fmt.Sprintf("Error in ReAct execution: %v", err),
			"result_type": "error",
		}, nil
	}
	return result, nil
}

// streamEntry 批入流出（Python stream L50-189）。
func (e *ReActExecutable) streamEntry(
	ctx context.Context,
	inputs map[string]any,
	sess *session.NodeSessionFacade,
	_ ceiface.ModelContext,
) (<-chan executable.StreamChunk[any], error) {
	// Python L63-170：新建 agent 会话（session_id 复用 workflow 会话 ID）
	agentSess := session.CreateAgentSession(sess.GetSessionID(), e.reactAgent.Card(), nil)
	ch, err := e.reactAgent.Stream(ctx, inputs, interfaces.WithSession(agentSess))
	if err != nil {
		return nil, err
	}
	out := make(chan executable.StreamChunk[any])
	go func() {
		defer close(out)
		for chunk := range ch {
			// Python L170-183：OutputSchema 解包三态（llm_output+content → 换键；
			// 其余剥信封透传 payload；非 OutputSchema 原样）
			if os, ok := chunk.(*stream.OutputSchema); ok {
				out <- executable.StreamChunk[any]{Val: unpackChunk(os)}
			} else {
				out <- executable.StreamChunk[any]{Val: chunk}
			}
		}
	}()
	return out, nil
}

// unpackChunk OutputSchema 解包三态（Python react_executable.py L170-183）：
// llm_output 且含 content → {"output": content}；其余 OutputSchema → payload；
// 非 OutputSchema → 原样。
func unpackChunk(chunk *stream.OutputSchema) any {
	if chunk.Type == "llm_output" {
		if m, ok := chunk.Payload.(map[string]any); ok {
			if content, exists := m["content"]; exists {
				return map[string]any{"output": content}
			}
		}
	}
	return chunk.Payload
}
