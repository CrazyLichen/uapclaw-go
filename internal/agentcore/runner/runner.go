package runner

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// RunAgent 执行单个 Agent，管理完整的会话生命周期。
//
// 对齐 Python: Runner.run_agent(agent, inputs, *, session, context, envs)
// Python 源码: openjiuwen/core/runner/runner.py L399-427
//
// 步骤对照：
//
//	Python L417: with self._root_task_group_scope()
//	Python L418: agent_instance, agent_session = await self._prepare_agent(agent, inputs, session)
//	  Python L504-512: if isinstance(session, AgentSession) → pre_run + return
//	  Python L513-514: session_id = inputs.get(conversation_id, ...)
//	  Python L515-522: if isinstance(agent, str) → get_agent + remote check
//	  Python L524-526: agent_session = _create_agent_session + pre_run
//	Python L419: if _is_remote_agent → invoke(inputs)
//	Python L421-423: elif LegacyBaseAgent → invoke(inputs, session=None)
//	Python L425: else → invoke(inputs, agent_session)
//	Python L426: await agent_session.post_run()
func RunAgent(
	ctx context.Context,
	agent interfaces.BaseAgent,
	inputs map[string]any,
	sess sessioninterfaces.SessionFacade,
) (any, error) {
	// 步骤 1：任务组作用域（对齐 Python L417: with self._root_task_group_scope()）
	// ⤵️ 预留章节回填：任务组作用域

	// 步骤 2：_prepare_agent → pre_run（对齐 Python L418 → L509/L511/L525: await session.pre_run(inputs=inputs)）
	// ⤵️ 预留章节回填：session.PreRun

	// 步骤 3：远程 Agent 判断（对齐 Python L419-420: if _is_remote_agent → invoke(inputs)）
	// ⤵️ 预留章节回填：远程 Agent 支持

	// 步骤 4：LegacyBaseAgent 判断（对齐 Python L421-423: elif LegacyBaseAgent → invoke(inputs, session=None)）
	// ⤵️ 预留章节回填：LegacyBaseAgent 兼容

	// 步骤 5：正常 Agent 调用（对齐 Python L425: res = await agent_instance.invoke(inputs, agent_session)）
	result, err := agent.Invoke(ctx, inputs, interfaces.WithSession(sess))
	if err != nil {
		return nil, err
	}

	// 步骤 6：post_run 清理（对齐 Python L426: await agent_session.post_run()）
	// ⤵️ 预留章节回填：session.PostRun

	return result, nil
}

// RunWorkflow 执行单个 Workflow，管理会话和上下文生命周期。
//
// 对齐 Python: Runner.run_workflow(workflow, inputs, *, session, context, envs)
// Python 源码: openjiuwen/core/runner/runner.py L350-369
//
// 步骤对照：
//
//	Python L367: with self._root_task_group_scope()
//	Python L368: workflow_instance, workflow_session = await self._prepare_workflow(workflow, session)
//	Python L369: workflow_instance.invoke(inputs, session=workflow_session, context=context)
func RunWorkflow(
	ctx context.Context,
	workflow interfaces.Workflow,
	inputs map[string]any,
	workflowSess *session.WorkflowSession,
	wfCtx any,
) (any, error) {
	// 步骤 1：任务组作用域（对齐 Python L367: with self._root_task_group_scope()）
	// ⤵️ 预留章节回填：任务组作用域

	// 步骤 2：_prepare_workflow（对齐 Python L368）
	// ⤵️ 预留章节回填：_prepare_workflow 完整逻辑

	// 步骤 3：调用 workflow.Invoke（对齐 Python L369: workflow_instance.invoke(inputs, session=workflow_session, context=context)）
	// ⤵️ 预留章节回填：WorkflowOptions 传 session + context
	result, err := workflow.Invoke(ctx, inputs)
	if err != nil {
		return nil, err
	}
	return result, nil
}
