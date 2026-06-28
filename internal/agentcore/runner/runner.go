package runner

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/message_queue"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/resources_manager"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/spawn"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/checkpointer"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// Runner 全局运行器，编排 Agent/Workflow 的执行生命周期。
// 对齐 Python: _RunnerImpl (runner.py L62-670)
type Runner struct {
	// runnerID Runner唯一标识（对齐 Python _runner_id）
	runnerID string
	// resourceMgr 全局资源注册表（对齐 Python _resource_manager）
	resourceMgr *resources_manager.ResourceMgr
	// messageQueue 本地消息队列（对齐 Python _message_queue）
	messageQueue *message_queue.MessageQueueInMemory
	// callbackFramework 异步回调框架（对齐 Python _callback_framework）
	callbackFramework *callback.CallbackFramework
	// rootTaskGroup 根任务组（对齐 Python _root_task_group）
	// ⤵️ 预留：任务组实现后回填
	rootTaskGroup any
	// teamRuntimeManager Team运行时管理器（对齐 Python _team_runtime_manager）
	// ⤵️ 预留：TeamRunner（9.85）实现后回填
	teamRuntimeManager any
	// distributeMessageQueue 分布式消息队列（对齐 Python _distribute_message_queue）
	// ⤵️ 预留：分布式模式实现后回填
	distributeMessageQueue any
	// systemReplySub 系统回复订阅（对齐 Python system_reply_sub）
	// ⤵️ 预留：分布式模式实现后回填
	systemReplySub any
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// defaultRunnerID 默认Runner ID（对齐 Python _DEFAULT_RUNNER_ID = "global"）
	defaultRunnerID = "global"
	// defaultAgentSessionID 默认Agent会话ID（对齐 Python _DEFAULT_AGENT_SESSION_ID = "default_session"）
	defaultAgentSessionID = "default_session"
	// agentConversationIDKey 会话ID键名（对齐 Python _AGENT_CONVERSATION_ID = "conversation_id"）
	agentConversationIDKey = "conversation_id"
	// defaultMQMaxSize 默认消息队列最大容量
	defaultMQMaxSize = 1000
	// defaultMQTimeout 默认消息队列超时时间
	defaultMQTimeout = 30 * time.Second
	// logComponent 日志组件标识
	logComponent = logger.ComponentAgentCore
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// globalRunner 全局Runner实例（对齐 Python GLOBAL_RUNNER）
	globalRunner *Runner
	// runnerOnce Runner初始化同步原语
	runnerOnce sync.Once
	// runnerMu Runner读写锁
	runnerMu sync.RWMutex
)

// ──────────────────────────── 导出函数 ────────────────────────────

// SetGlobalRunner 替换全局Runner实例（用于测试注入）。
func SetGlobalRunner(r *Runner) {
	runnerMu.Lock()
	globalRunner = r
	runnerMu.Unlock()
}

// --- 生命周期 ---

// Start 启动Runner及其关联组件。
// 对齐 Python: Runner.start() (runner.py L267-322)
func Start(ctx context.Context) error {
	r := getRunner()
	logger.Info(logComponent).
		Str("event_type", "runner_start").
		Str("runner_id", r.runnerID).
		Msg("开始启动 Runner")

	// 步骤 1：确保根任务组（对齐 Python L271: await self._ensure_root_task_group()）
	// ⤵️ 预留：任务组初始化（依赖 TaskGroup 实现）

	// 步骤 2：进入任务组作用域（对齐 Python L274: with self._root_task_group_scope()）
	// ⤵️ 预留：任务组作用域（依赖 TaskGroup 实现）

	// 对齐 Python L320-322: start 失败时调用 _close_root_task_group() 清理
	started := false
	defer func() {
		if !started {
			// 步骤 1 的清理：关闭根任务组
			// ⤵️ 预留：任务组关闭（依赖 TaskGroup 实现）
			logger.Debug(logComponent).
				Str("event_type", "runner_start_failed").
				Str("runner_id", r.runnerID).
				Msg("Runner 启动失败，执行清理")
		}
	}()

	// 步骤 3：初始化 Checkpointer（对齐 Python L277-302）
	cfg := config.GetRunnerConfig()
	if cfg != nil && cfg.CheckpointerConfig != nil {
		logger.Info(logComponent).
			Str("event_type", "runner_start").
			Str("runner_id", r.runnerID).
			Str("checkpointer_type", cfg.CheckpointerConfig.Type).
			Msg("开始初始化 Checkpointer")
		cp, err := checkpointer.CreateCheckpointer(ctx, *cfg.CheckpointerConfig)
		if err != nil {
			logger.Error(logComponent).
				Str("event_type", "runner_start").
				Str("runner_id", r.runnerID).
				Str("checkpointer_type", cfg.CheckpointerConfig.Type).
				Err(err).
				Msg("Checkpointer 初始化失败")
			return fmt.Errorf("checkpointer 初始化失败: %w", err)
		}
		checkpointer.SetDefaultCheckpointer(cp)
		logger.Info(logComponent).
			Str("event_type", "runner_start").
			Str("runner_id", r.runnerID).
			Str("checkpointer_type", cfg.CheckpointerConfig.Type).
			Msg("Checkpointer 初始化成功")
	}

	// 步骤 4：启动分布式消息队列（对齐 Python L304-312）
	// ⤵️ 预留：分布式模式启动（依赖分布式消息队列实现）

	// 步骤 5：启动本地消息队列（对齐 Python L312: result = await self._message_queue.start()）
	r.messageQueue.Start()

	started = true
	logger.Info(logComponent).
		Str("event_type", "runner_start").
		Str("runner_id", r.runnerID).
		Msg("Runner 启动成功")
	return nil
}

// Stop 停止Runner并清理资源。
// 对齐 Python: Runner.stop() (runner.py L324-348)
// 返回首个遇到的错误（对齐 Python 失败返回 False），用 defer 保证后续清理仍执行。
func Stop(ctx context.Context) error {
	r := getRunner()
	logger.Info(logComponent).
		Str("event_type", "runner_stop").
		Str("runner_id", r.runnerID).
		Msg("开始停止 Runner")

	var firstErr error

	// 对齐 Python L346-348: finally 保证释放资源管理器和关闭根任务组
	defer func() {
		// 步骤 4：释放资源管理器（对齐 Python L347: await self._resource_manager.release()）
		if r.resourceMgr != nil {
			if err := r.resourceMgr.Release(ctx); err != nil {
				logger.Warn(logComponent).
					Str("event_type", "runner_stop").
					Str("runner_id", r.runnerID).
					Err(err).
					Msg("资源管理器释放失败")
				if firstErr == nil {
					firstErr = err
				}
			}
		}

		// 步骤 5：关闭根任务组（对齐 Python L348: await self._close_root_task_group()）
		// ⤵️ 预留：任务组关闭（依赖 TaskGroup 实现）
	}()

	// 步骤 1：进入任务组作用域（对齐 Python L328）
	// ⤵️ 预留：任务组作用域（依赖 TaskGroup 实现）

	// 步骤 2：停止分布式组件（对齐 Python L329-337）
	// ⤵️ 预留：分布式模式停止（依赖分布式消息队列实现）

	// 步骤 3：停止本地消息队列（对齐 Python L339: result = await self._message_queue.stop()）
	if err := r.messageQueue.Stop(ctx); err != nil {
		logger.Warn(logComponent).
			Str("event_type", "runner_stop").
			Str("runner_id", r.runnerID).
			Err(err).
			Msg("消息队列停止失败")
		firstErr = err
	}

	if firstErr != nil {
		logger.Warn(logComponent).
			Str("event_type", "runner_stop").
			Str("runner_id", r.runnerID).
			Err(firstErr).
			Msg("Runner 停止过程中遇到错误")
		return firstErr
	}

	logger.Info(logComponent).
		Str("event_type", "runner_stop").
		Str("runner_id", r.runnerID).
		Msg("Runner 停止成功")
	return nil
}

// --- Agent 执行 ---

// RunAgent 执行单个Agent，管理完整的会话生命周期。
// 对齐 Python: Runner.run_agent() (runner.py L399-427)
func RunAgent(
	ctx context.Context,
	agentRef AgentRef,
	inputs map[string]any,
	sess sessioninterfaces.SessionFacade,
	modelCtx any,
	envs map[string]any,
) (any, error) {
	r := getRunner()

	// 步骤 1：进入任务组作用域（对齐 Python L417: with self._root_task_group_scope()）
	// ⤵️ 预留：任务组作用域（依赖 TaskGroup 实现）
	_ = r

	// 步骤 2：_prepareAgent → 获取agent实例和session（对齐 Python L418）
	agentInstance, agentSession, err := r.prepareAgent(ctx, agentRef, inputs, sess)
	if err != nil {
		return nil, err
	}

	// 步骤 3：判断是否远程Agent（对齐 Python L419-420: if _is_remote_agent）
	// ⤵️ 预留：远程Agent支持（依赖 RemoteAgent 实现）
	_ = agentInstance

	// 步骤 4：判断是否LegacyBaseAgent（对齐 Python L421-423: elif isinstance(agent_instance, LegacyBaseAgent)）
	// ⤵️ 预留：LegacyBaseAgent兼容（依赖 LegacyBaseAgent 实现）

	// 步骤 5：正常Agent调用（对齐 Python L425: res = await agent_instance.invoke(inputs, agent_session)）
	result, err := agentInstance.Invoke(ctx, inputs, interfaces.WithSession(agentSession))
	if err != nil {
		return nil, err
	}

	// 步骤 6：PostRun清理（对齐 Python L426: await agent_session.post_run()）
	if agentSession != nil {
		if agentSess, ok := agentSession.(*session.Session); ok {
			if postErr := agentSess.PostRun(ctx); postErr != nil {
				logger.Warn(logComponent).
					Str("event_type", "runner_run_agent").
					Err(postErr).
					Msg("Agent PostRun 失败")
			}
		}
	}

	return result, nil
}

// RunAgentStreaming 流式执行单个Agent。
// 对齐 Python: Runner.run_agent_streaming() (runner.py L429-463)
func RunAgentStreaming(
	ctx context.Context,
	agentRef AgentRef,
	inputs map[string]any,
	sess sessioninterfaces.SessionFacade,
	modelCtx any,
	streamModes any,
	envs map[string]any,
) (<-chan stream.Schema, error) {
	r := getRunner()

	// 步骤 1：进入任务组上下文（对齐 Python L448: token = self._enter_root_task_group_context()）
	// ⤵️ 预留：任务组上下文（依赖 TaskGroup 实现）
	_ = r

	// 步骤 2：_prepareAgent → 获取agent实例和session（对齐 Python L450）
	agentInstance, agentSession, err := r.prepareAgent(ctx, agentRef, inputs, sess)
	if err != nil {
		return nil, err
	}

	// 步骤 3：判断是否远程Agent（对齐 Python L451-453）
	// ⤵️ 预留：远程Agent流式支持（依赖 RemoteAgent 实现）

	// 步骤 4：判断是否LegacyBaseAgent（对齐 Python L454-457）
	// ⤵️ 预留：LegacyBaseAgent流式兼容（依赖 LegacyBaseAgent 实现）

	// 步骤 5：正常Agent流式调用（对齐 Python L459-460）
	opts := []interfaces.AgentOption{interfaces.WithSession(agentSession)}
	ch, err := agentInstance.Stream(ctx, inputs, opts...)
	if err != nil {
		return nil, err
	}

	// 步骤 6：PostRun清理（对齐 Python L461: await agent_session.post_run()）
	// 在单独 goroutine 中等待流完成后调用 PostRun
	outCh := make(chan stream.Schema, 64)
	go func() {
		defer close(outCh)
		for chunk := range ch {
			outCh <- chunk
		}
		if agentSession != nil {
			if agentSess, ok := agentSession.(*session.Session); ok {
				if postErr := agentSess.PostRun(ctx); postErr != nil {
					logger.Warn(logComponent).
						Str("event_type", "runner_run_agent_streaming").
						Err(postErr).
						Msg("Agent PostRun 失败")
				}
			}
		}
	}()

	// 步骤 7：退出任务组上下文（对齐 Python L463: self._exit_root_task_group_context(token)）
	// ⤵️ 预留：任务组上下文退出（依赖 TaskGroup 实现）

	return outCh, nil
}

// --- Workflow 执行 ---

// RunWorkflow 执行单个Workflow。
// 对齐 Python: Runner.run_workflow() (runner.py L350-369)
func RunWorkflow(
	ctx context.Context,
	workflowRef WorkflowRef,
	inputs map[string]any,
	sess any,
	modelCtx any,
	envs map[string]any,
) (any, error) {
	r := getRunner()

	// 步骤 1：进入任务组作用域（对齐 Python L367: with self._root_task_group_scope()）
	// ⤵️ 预留：任务组作用域（依赖 TaskGroup 实现）
	_ = r

	// 步骤 2：_prepareWorkflow → 获取workflow实例和session（对齐 Python L368）
	workflowInstance, workflowSession, err := r.prepareWorkflow(ctx, workflowRef, sess)
	if err != nil {
		return nil, err
	}

	// 步骤 3：调用workflow.Invoke（对齐 Python L369: workflow_instance.invoke(inputs, session=workflow_session, context=context)）
	opts := []interfaces.WorkflowOption{}
	if workflowSession != nil {
		opts = append(opts, interfaces.WithWorkflowSession(workflowSession))
	}
	if modelCtx != nil {
		opts = append(opts, interfaces.WithWorkflowContext(modelCtx))
	}
	result, err := workflowInstance.Invoke(ctx, inputs, opts...)
	if err != nil {
		return nil, err
	}

	return result, nil
}

// RunWorkflowStreaming 流式执行单个Workflow。
// 对齐 Python: Runner.run_workflow_streaming() (runner.py L371-397)
func RunWorkflowStreaming(
	ctx context.Context,
	workflowRef WorkflowRef,
	inputs map[string]any,
	sess any,
	modelCtx any,
	streamModes any,
	envs map[string]any,
) (<-chan stream.Schema, error) {
	r := getRunner()

	// 步骤 1：进入任务组上下文（对齐 Python L390: token = self._enter_root_task_group_context()）
	// ⤵️ 预留：任务组上下文（依赖 TaskGroup 实现）
	_ = r

	// 步骤 2：_prepareWorkflow → 获取workflow实例和session（对齐 Python L392）
	workflowInstance, workflowSession, err := r.prepareWorkflow(ctx, workflowRef, sess)
	if err != nil {
		return nil, err
	}

	// 步骤 3：调用workflow.Stream（对齐 Python L393-395）
	opts := []interfaces.WorkflowOption{}
	if workflowSession != nil {
		opts = append(opts, interfaces.WithWorkflowSession(workflowSession))
	}
	if modelCtx != nil {
		opts = append(opts, interfaces.WithWorkflowContext(modelCtx))
	}
	ch, err := workflowInstance.Stream(ctx, inputs, opts...)
	if err != nil {
		return nil, err
	}

	// 步骤 4：退出任务组上下文（对齐 Python L397: self._exit_root_task_group_context(token)）
	// ⤵️ 预留：任务组上下文退出（依赖 TaskGroup 实现）

	return ch, nil
}

// --- Spawn 子进程 ---

// SpawnAgent 启动子进程运行 Agent。
// 对齐 Python: Runner.spawn_agent() (runner.py L532-576)
func SpawnAgent(
	ctx context.Context,
	agentConfig spawn.SpawnAgentConfig,
	inputs map[string]any,
	sess sessioninterfaces.SessionFacade,
	envs map[string]any,
	spawnCfg ...spawn.SpawnConfig,
) (*spawn.SpawnedProcessHandle, error) {
	cfg := spawn.DefaultSpawnConfig()
	if len(spawnCfg) > 0 {
		cfg = spawnCfg[0]
	}

	// 合并环境变量到 agentConfig
	if envs != nil {
		if agentConfig.Payload == nil {
			agentConfig.Payload = make(map[string]any)
		}
		agentConfig.Payload["envs"] = envs
	}

	handle, err := spawn.SpawnProcess(ctx, agentConfig, inputs, cfg)
	if err != nil {
		return nil, fmt.Errorf("spawn_agent 启动子进程失败: %w", err)
	}

	return handle, nil
}

// SpawnAgentStreaming 启动子进程运行 Agent（流式）。
// 对齐 Python: Runner.spawn_agent_streaming() (runner.py L578-640)
func SpawnAgentStreaming(
	ctx context.Context,
	agentConfig spawn.SpawnAgentConfig,
	inputs map[string]any,
	sess sessioninterfaces.SessionFacade,
	streamModes []string,
	envs map[string]any,
	spawnCfg ...spawn.SpawnConfig,
) (<-chan stream.Schema, error) {
	cfg := spawn.DefaultSpawnConfig()
	if len(spawnCfg) > 0 {
		cfg = spawnCfg[0]
	}

	// 在 payload 中标记流式模式和 stream_modes
	if agentConfig.Payload == nil {
		agentConfig.Payload = make(map[string]any)
	}
	agentConfig.Payload["streaming"] = true
	agentConfig.Payload["stream_modes"] = streamModes
	if envs != nil {
		agentConfig.Payload["envs"] = envs
	}

	handle, err := spawn.SpawnProcess(ctx, agentConfig, inputs, cfg)
	if err != nil {
		return nil, fmt.Errorf("spawn_agent_streaming 启动子进程失败: %w", err)
	}

	ch := make(chan stream.Schema, 64)

	go func() {
		defer close(ch)
		for {
			msg, err := handle.ReceiveMessage(ctx)
			if err != nil {
				return
			}
			switch msg.Type {
			case spawn.MessageTypeStreamChunk:
				if schema, ok := msg.Payload.(stream.Schema); ok {
					ch <- schema
				}
			case spawn.MessageTypeDone, spawn.MessageTypeError:
				return
			}
		}
	}()

	return ch, nil
}

// --- 释放 ---

// Release 释放与会话关联的资源。
// 对齐 Python: Runner.release() (runner.py L465-483)
func Release(ctx context.Context, sessionID string, force bool) error {
	// 步骤 1：尝试释放Team会话（对齐 Python L481: if await self._maybe_release_team_session(...)）
	// ⤵️ 预留：Team会话释放（依赖 9.85 TeamRunner 实现）

	// 步骤 2：获取Checkpointer并释放（对齐 Python L483）
	cp := checkpointer.GetCheckpointer()
	if cp != nil {
		return cp.Release(ctx, sessionID)
	}
	return nil
}

// --- 配置访问 ---

// SetConfig 设置Runner配置。
// 对齐 Python: Runner.set_config() (runner.py L250-257)
func SetConfig(cfg *config.RunnerConfig) {
	config.SetRunnerConfig(cfg)
}

// GetConfig 获取当前Runner配置。
// 对齐 Python: Runner.get_config() (runner.py L259-265)
func GetConfig() *config.RunnerConfig {
	return config.GetRunnerConfig()
}

// --- 资源访问 ---

// GetResourceMgr 获取全局资源管理器。
// 对齐 Python: Runner.resource_mgr 属性
func GetResourceMgr() *resources_manager.ResourceMgr {
	return getRunner().resourceMgr
}

// GetPubSub 获取本地消息队列。
// 对齐 Python: Runner.pubsub 属性
func GetPubSub() *message_queue.MessageQueueInMemory {
	return getRunner().messageQueue
}

// GetCallbackFramework 获取回调框架。
// 对齐 Python: Runner.callback_framework 属性
func GetCallbackFramework() *callback.CallbackFramework {
	return getRunner().callbackFramework
}

// IsRemoteAgent 判断Agent是否为远程Agent。
// 对齐 Python: _RunnerImpl._is_remote_agent() (runner.py L123-131)
// ⤵️ 预留：远程Agent判断（依赖 RemoteAgent 实现）
func IsRemoteAgent(agent interfaces.BaseAgent) bool {
	// ⤵️ 预留：实现远程Agent判断逻辑
	return false
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// initRunner 初始化全局Runner实例。
// 对齐 Python: GLOBAL_RUNNER = _RunnerImpl(config=DEFAULT_RUNNER_CONFIG)
// 创建 Runner 的同时设置默认配置。
func initRunner() {
	runnerOnce.Do(func() {
		// 对齐 Python __init__: set_runner_config(DEFAULT_RUNNER_CONFIG)
		config.SetRunnerConfig(config.GetRunnerConfig())

		globalRunner = &Runner{
			runnerID:          defaultRunnerID,
			resourceMgr:       resources_manager.NewResourceMgr(),
			messageQueue:      message_queue.NewMessageQueueInMemory(defaultMQMaxSize, defaultMQTimeout),
			callbackFramework: callback.NewCallbackFramework(),
		}
	})
}

// getRunner 获取全局Runner实例（懒初始化）。
func getRunner() *Runner {
	runnerMu.RLock()
	r := globalRunner
	runnerMu.RUnlock()
	if r == nil {
		runnerMu.Lock()
		if globalRunner == nil {
			initRunner()
		}
		r = globalRunner
		runnerMu.Unlock()
	}
	return r
}

// prepareAgent 准备Agent实例和会话。
// 对齐 Python: _RunnerImpl._prepare_agent() (runner.py L502-530)
func (r *Runner) prepareAgent(
	ctx context.Context,
	agentRef AgentRef,
	inputs map[string]any,
	sess sessioninterfaces.SessionFacade,
) (interfaces.BaseAgent, sessioninterfaces.SessionFacade, error) {
	// 分支 1：session 是 AgentSession（对齐 Python L504: isinstance(session, AgentSession)）
	if sess != nil {
		// 断言为具体 Session 类型以调用 PreRun
		agentSess, isAgentSess := sess.(*session.Session)

		if agentRef.IsByID() {
			// 对齐 Python L505-510: isinstance(agent, str) + isinstance(session, AgentSession)
			agents, err := r.resourceMgr.GetAgent(ctx, []string{agentRef.ID()})
			if err != nil {
				return nil, nil, fmt.Errorf("获取Agent失败: %w", err)
			}
			if len(agents) == 0 || agents[0] == nil {
				return nil, nil, fmt.Errorf("agent不存在: %s", agentRef.ID())
			}
			agentInstance := agents[0]
			// 对齐 Python L509: await session.pre_run(inputs=inputs)
			if isAgentSess {
				if preErr := agentSess.PreRun(ctx, inputs); preErr != nil {
					return nil, nil, fmt.Errorf("PreRun失败: %w", preErr)
				}
			}
			return agentInstance, sess, nil
		}
		// 对齐 Python L511-512: isinstance(session, AgentSession) + not isinstance(agent, str)
		if isAgentSess {
			if preErr := agentSess.PreRun(ctx, inputs); preErr != nil {
				return nil, nil, fmt.Errorf("PreRun失败: %w", preErr)
			}
		}
		return agentRef.Agent(), sess, nil
	}

	// 分支 2：session 不是 AgentSession（对齐 Python L513-530）
	// 解析 sessionID（对齐 Python L513-514: session_id = inputs.get(conversation_id, ...)）
	sessionID := defaultAgentSessionID
	if inputs != nil {
		if convID, ok := inputs[agentConversationIDKey]; ok {
			if id, ok := convID.(string); ok && id != "" {
				sessionID = id
			}
		}
	}

	if agentRef.IsByID() {
		// 对齐 Python L515-526: isinstance(agent, str) + not isinstance(session, AgentSession)
		agents, err := r.resourceMgr.GetAgent(ctx, []string{agentRef.ID()})
		if err != nil {
			return nil, nil, fmt.Errorf("获取Agent失败: %w", err)
		}
		if len(agents) == 0 || agents[0] == nil {
			return nil, nil, fmt.Errorf("agent不存在: %s", agentRef.ID())
		}
		agentInstance := agents[0]

		// 判断是否远程Agent（对齐 Python L519: if self._is_remote_agent）
		// ⤵️ 预留：远程Agent判断（依赖 RemoteAgent 实现）

		// 创建AgentSession（对齐 Python L524: agent_session = self._create_agent_session(...)）
		agentSession := r.createAgentSession(agentInstance, sessionID)
		// 对齐 Python L525: await agent_session.pre_run(inputs=inputs)
		if preErr := agentSession.PreRun(ctx, inputs); preErr != nil {
			return nil, nil, fmt.Errorf("PreRun失败: %w", preErr)
		}
		return agentInstance, agentSession, nil
	}

	// 对齐 Python L528-530: not isinstance(agent, str) + not isinstance(session, AgentSession)
	agentInstance := agentRef.Agent()
	agentSession := r.createAgentSession(agentInstance, sessionID)
	if preErr := agentSession.PreRun(ctx, inputs); preErr != nil {
		return nil, nil, fmt.Errorf("PreRun失败: %w", preErr)
	}
	return agentInstance, agentSession, nil
}

// prepareWorkflow 准备Workflow实例和会话。
// 对齐 Python: _RunnerImpl._prepare_workflow() (runner.py L642-655)
func (r *Runner) prepareWorkflow(
	ctx context.Context,
	workflowRef WorkflowRef,
	sess any,
) (interfaces.Workflow, *session.WorkflowSession, error) {
	var workflowKey string

	// 解析 workflow key（对齐 Python L643-648）
	if workflowRef.IsByID() {
		workflowKey = workflowRef.ID()
	} else {
		wf := workflowRef.Workflow()
		if wf.Card() != nil {
			workflowKey = generateWorkflowKey(wf.Card().ID, wf.Card().Version)
		}
	}

	// 创建 workflow session（对齐 Python L649: workflow_session = self._create_workflow_session(session)）
	workflowSession := r.createWorkflowSession(sess)

	// 获取 workflow 实例（对齐 Python L650-654）
	var workflowInstance interfaces.Workflow
	if workflowRef.IsByID() {
		workflows, err := r.resourceMgr.GetWorkflow(ctx, []string{workflowKey})
		if err != nil {
			return nil, nil, fmt.Errorf("获取Workflow失败: %w", err)
		}
		if len(workflows) == 0 || workflows[0] == nil {
			return nil, nil, fmt.Errorf("workflow不存在: %s", workflowKey)
		}
		workflowInstance = workflows[0]
	} else {
		workflowInstance = workflowRef.Workflow()
	}

	return workflowInstance, workflowSession, nil
}

// createAgentSession 创建Agent会话。
// 对齐 Python: _RunnerImpl._create_agent_session() (runner.py L657-670)
// 从 agent 提取完整 card 和 envs，传给 CreateAgentSession。
func (r *Runner) createAgentSession(agent interfaces.BaseAgent, sessionID string) *session.Session {
	// 对齐 Python L658-669: 提取 card 和 envs
	card := agent.Card()
	// ⤵️ 预留：从 AgentConfig 提取 envs
	// Python L665-666: if isinstance(config, Config): envs = getattr(config, "_env", None)
	// Go 的 AgentConfig 接口目前不含 envs，待 AgentConfig 完善（添加 GetEnvs）后回填
	var envs map[string]any
	return session.CreateAgentSession(sessionID, card, envs)
}

// createWorkflowSession 创建Workflow会话。
// 对齐 Python: _RunnerImpl._create_workflow_session() (runner.py L489-500)
func (r *Runner) createWorkflowSession(sess any) *session.WorkflowSession {
	// 对齐 Python L492-500: 根据 session 类型创建 WorkflowSession
	if sess == nil {
		return session.NewWorkflowSession()
	}
	switch s := sess.(type) {
	case string:
		return session.NewWorkflowSession(session.WithWorkflowSessionID(s))
	case *session.WorkflowSession:
		return s
	case *session.Session:
		return s.CreateWorkflowSession()
	default:
		return session.NewWorkflowSession()
	}
}

// generateWorkflowKey 生成Workflow唯一键。
// 对齐 Python: generate_workflow_key(workflow.card.id, workflow.card.version)
func generateWorkflowKey(id, version string) string {
	if version != "" {
		return id + ":" + version
	}
	return id
}
