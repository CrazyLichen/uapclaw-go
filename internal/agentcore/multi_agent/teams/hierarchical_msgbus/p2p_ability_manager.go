package hierarchical_msgbus

import (
	"context"
	"fmt"
	"sync"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	maschema "github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent/team_runtime"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/ability"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/rail"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	cschema "github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// P2PAbilityManager 层级团队的 P2P 能力管理器。
//
// 拦截 AgentCard 类型的 tool_call，通过 supervisor.Send() 做 P2P 派发，
// 其他能力类型转发给嵌入的 AbilityManager 执行。
// AgentCard 派发受 maxParallel 限流。
//
// 对应 Python: P2PAbilityManager(AbilityManager)
type P2PAbilityManager struct {
	// AbilityManager 嵌入：Add/Remove/Get/List/ListToolInfo 等
	ability.AbilityManager
	// supervisor 持有：用于 P2P send（Communicable 接口）
	supervisor team_runtime.Communicable
	// maxParallel 最大并行子 Agent 派发数
	maxParallel int
	// timeout P2P 超时秒数（构造时传入）
	timeout float64
	// sem 限流信号量（懒初始化）
	sem chan struct{}
	// semOnce 信号量初始化同步原语
	semOnce sync.Once
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// p2pLogComponent 日志组件标识
	p2pLogComponent = cschema.ComponentChannel
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewP2PAbilityManager 创建 P2PAbilityManager 实例。
func NewP2PAbilityManager(supervisor team_runtime.Communicable, maxParallel int, timeout float64) *P2PAbilityManager {
	if maxParallel < 1 {
		maxParallel = 1
	}
	return &P2PAbilityManager{
		AbilityManager: *ability.NewAbilityManager(nil),
		supervisor:     supervisor,
		maxParallel:    maxParallel,
		timeout:        timeout,
	}
}

// Execute 覆写：AgentCard 类型的 tool_call 通过 P2P 派发，其他委托基类执行。
//
// 对齐 Python: P2PAbilityManager.execute()
func (m *P2PAbilityManager) Execute(
	ctx context.Context,
	cbc *rail.AgentCallbackContext,
	toolCalls []*llmschema.ToolCall,
	sess sessioninterfaces.SessionFacade,
	tag string,
) []agentschema.ExecuteResult {
	if len(toolCalls) == 0 {
		return nil
	}

	// 分区：agent 调用 vs 其他调用
	agentIndices := make([]int, 0)
	otherIndices := make([]int, 0)
	for i, tc := range toolCalls {
		if m.IsAgent(tc.Name) {
			agentIndices = append(agentIndices, i)
		} else {
			otherIndices = append(otherIndices, i)
		}
	}

	// Fast path：无 Agent 调用时委托基类
	if len(agentIndices) == 0 {
		return m.AbilityManager.Execute(ctx, cbc, toolCalls, sess, tag)
	}

	// 懒初始化信号量
	m.semOnce.Do(func() {
		m.sem = make(chan struct{}, m.maxParallel)
	})

	// 预分配结果
	results := make([]agentschema.ExecuteResult, len(toolCalls))
	var wg sync.WaitGroup

	// 并行执行 Agent 调用
	for _, idx := range agentIndices {
		wg.Add(1)
		go func(i int, tc *llmschema.ToolCall) {
			defer wg.Done()
			// 获取信号量
			m.sem <- struct{}{}
			defer func() { <-m.sem }()

			r, err := m.executeSingleP2P(ctx, tc, sess)
			if err != nil {
				results[i] = errorToP2PResult(err, tc.ID)
			} else {
				results[i] = r
			}
		}(idx, toolCalls[idx])
	}

	// 其他调用委托基类
	if len(otherIndices) > 0 {
		otherToolCalls := make([]*llmschema.ToolCall, len(otherIndices))
		for j, idx := range otherIndices {
			otherToolCalls[j] = toolCalls[idx]
		}
		otherResults := m.AbilityManager.Execute(ctx, cbc, otherToolCalls, sess, tag)
		for j, idx := range otherIndices {
			results[idx] = otherResults[j]
		}
	}

	wg.Wait()

	cschema.Debug(p2pLogComponent).
		Str("action", "p2p_execute").
		Int("agent_calls", len(agentIndices)).
		Int("other_calls", len(otherIndices)).
		Int("max_parallel", m.maxParallel).
		Msg("并行派发完成")

	return results
}

// IsAgent 判断指定名称的能力是否为 Agent 类型。
//
// 通过 AbilityKind() 判断，不依赖 AbilityManager 的私有 agents map。
func (m *P2PAbilityManager) IsAgent(name string) bool {
	a := m.AbilityManager.Get(name)
	if a == nil {
		return false
	}
	return a.AbilityKind() == schema.AbilityKindAgent
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// executeSingleP2P 单个 P2P 派发。
//
// 流程：
//  1. 解析 toolCall.Arguments 为 map[string]any
//  2. 从 session 获取 sessionID
//  3. supervisor.Send(ctx, toolArgs, agentCard.ID)
//  4. 成功/失败处理
//
// 对应 Python: P2PAbilityManager._execute_single_tool_call()
func (m *P2PAbilityManager) executeSingleP2P(
	ctx context.Context,
	toolCall *llmschema.ToolCall,
	sess sessioninterfaces.SessionFacade,
) (agentschema.ExecuteResult, error) {
	toolName := toolCall.Name

	// 解析参数
	toolArgs, err := ability.ParseToolArguments(toolCall.Arguments)
	if err != nil {
		toolArgs = map[string]any{}
	}

	// 获取 sessionID
	var sessionID string
	if sess != nil {
		sessionID = sess.GetSessionID()
	}

	// 获取 AgentCard
	abilityCard := m.AbilityManager.Get(toolName)
	agentCard, ok := abilityCard.(*agentschema.AgentCard)
	if !ok || agentCard == nil {
		return agentschema.ExecuteResult{}, fmt.Errorf("未找到 Agent 能力: %s", toolName)
	}

	// 获取超时：优先使用 runtime 的 p2pTimeout，其次使用构造时传入的 timeout
	timeout := m.timeout
	if m.supervisor != nil {
		rt := m.supervisor.Runtime()
		if rt != nil {
			rtTimeout := rt.GetP2PTimeout()
			if rtTimeout > 0 {
				timeout = rtTimeout
			}
		}
	}

	cschema.Debug(p2pLogComponent).
		Str("action", "p2p_dispatch").
		Str("tool_name", toolName).
		Str("agent_id", agentCard.ID).
		Str("session_id", sessionID).
		Float64("timeout", timeout).
		Msg("P2P 派发")

	// P2P 派发
	result, err := m.supervisor.Send(ctx, toolArgs, agentCard.ID,
		maschema.WithTeamSessionID(sessionID),
		maschema.WithTeamTimeout(timeout),
	)
	if err != nil {
		cschema.Warn(p2pLogComponent).
			Str("action", "p2p_dispatch_failed").
			Str("tool_name", toolName).
			Err(err).
			Msg("P2P 派发失败")
		return agentschema.ExecuteResult{}, fmt.Errorf("P2P 派发到 '%s' 失败: %w", toolName, err)
	}

	// 构建结果
	content := fmt.Sprintf("%v", result)
	toolMsg := llmschema.NewToolMessage(toolCall.ID, content)
	return agentschema.ExecuteResult{Result: result, ToolMsg: toolMsg}, nil
}

// errorToP2PResult 将 error 转换为 ExecuteResult（P2P 派发失败）。
//
// 使用 AbilityExecutionError 包装，与 AbilityManager 的错误路径保持一致。
func errorToP2PResult(err error, toolCallID string) agentschema.ExecuteResult {
	execErr := ability.NewAbilityExecutionError(
		exception.StatusAbilityExecutionError,
		toolCallID,
		err.Error(),
		exception.WithCause(err),
	)
	return agentschema.ExecuteResult{Result: execErr, ToolMsg: execErr.ToolMessage}
}
