package spawn

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ChildRunner 子进程 Runner 接口，由 runner 包实现并注入。
// 对齐 Python: Runner.set_config() / Runner.start() / Runner.stop()
//
// 与 runner.RunAgent 不同：这是子进程专用接口，只接收 BaseAgent 实例
// （子进程已有 agent 实例，不需要 AgentRef 按ID查找），
// session 通过 sessionID 传入（对齐 Python: session = agent_config.session_id），
// streamModes 透传给 RunAgentStreaming。
type ChildRunner interface {
	// SetConfig 设置 Runner 配置。
	SetConfig(runnerConfig map[string]any) error
	// Start 启动 Runner。
	Start(ctx context.Context) error
	// Stop 停止 Runner。
	Stop(ctx context.Context) error
	// RunAgent 执行 Agent（非流式）。
	// 对齐 Python: Runner.run_agent(agent=agent, inputs=inputs, session=session)
	RunAgent(ctx context.Context, agent interfaces.BaseAgent, inputs map[string]any, sessionID string) (any, error)
	// RunAgentStreaming 执行 Agent（流式），返回消息块通道。
	// 对齐 Python: Runner.run_agent_streaming(agent, inputs, session=session, stream_modes=stream_modes)
	RunAgentStreaming(ctx context.Context, agent interfaces.BaseAgent, inputs map[string]any, sessionID string, streamModes any) (<-chan stream.Schema, error)
}

// AgentCreator Agent 创建接口，由 spawn/factory 包实现并注入。
// 对齐 Python: importlib.import_module(agent_module) → getattr(module, agent_class) → cls(**init_kwargs)
type AgentCreator interface {
	// CreateByType 根据 agent_type 和 AgentCard 创建 Agent 实例。
	// 对齐 Python: agent = agent_cls(**class_config.init_kwargs)
	CreateByType(ctx context.Context, agentType string, agentCard map[string]any, initKwargs map[string]any) (interfaces.BaseAgent, error)
}

// ──────────────────────────── 导出函数 ────────────────────────────

// RunSpawnedProcess 子进程主入口。
// 对齐 Python: run_spawned_process()
//
// childRunner 参数由调用方注入（cmd 层传入 runner.ChildRunnerImpl），
// 避免 spawn 包直接依赖 runner 包导致循环导入。
// agentCreator 参数由调用方注入（cmd 层传入 factory.DefaultAgentCreator），
// 避免 spawn 包直接依赖具体 Agent 类型包导致循环导入。
func RunSpawnedProcess(
	ctx context.Context,
	agentConfig map[string]any,
	inputs map[string]any,
	childRunner ChildRunner,
	agentCreator AgentCreator,
) error {
	// 场景(1)：从环境变量读取日志配置并初始化 logger。
	// 对齐 Python: child_process.py L22-27（在 import logger 之前从 env 应用 logging_config）
	applyLoggingConfigFromEnv()

	logger.Info(logger.ComponentAgentCore).
		Str("event_type", "SPAWN_CHILD_START").
		Msg("子进程启动")

	// 准备 Agent 配置
	spawnAgentConfig := prepareSpawnAgentConfig(agentConfig)

	// Runner 生命周期管理。
	// 对齐 Python: run_spawned_process() L456-468
	//   Runner.set_config(deserialize_runner_config(...))
	//   Runner.start()
	//   process_message_loop(...)
	//   Runner.stop()
	if childRunner != nil && spawnAgentConfig != nil && spawnAgentConfig.RunnerConfig != nil {
		if err := childRunner.SetConfig(spawnAgentConfig.RunnerConfig); err != nil {
			logger.Error(logger.ComponentAgentCore).
				Str("event_type", "SPAWN_RUNNER_CONFIG_ERROR").
				Err(err).
				Msg("设置 Runner 配置失败")
		}
	}

	if childRunner != nil {
		if err := childRunner.Start(ctx); err != nil {
			logger.Error(logger.ComponentAgentCore).
				Str("event_type", "SPAWN_RUNNER_START_ERROR").
				Err(err).
				Msg("runner 启动失败")
			return fmt.Errorf("runner 启动失败: %w", err)
		}
		defer func() {
			if stopErr := childRunner.Stop(ctx); stopErr != nil {
				logger.Error(logger.ComponentAgentCore).
					Str("event_type", "SPAWN_RUNNER_STOP_ERROR").
					Err(stopErr).
					Msg("Runner 停止失败")
			}
		}()
	}

	// 子进程的 stdin/stdout 即 os.Stdin/os.Stdout（由 os/exec 管道连接）
	stdin := os.Stdin
	stdout := os.Stdout

	// 运行消息循环
	err := ProcessMessageLoop(ctx, stdin, stdout, spawnAgentConfig, inputs, childRunner, agentCreator)
	if err != nil {
		logger.Error(logger.ComponentAgentCore).
			Str("event_type", "SPAWN_CHILD_ERROR").
			Err(err).
			Msg("子进程消息循环错误")
		return err
	}

	logger.Info(logger.ComponentAgentCore).
		Str("event_type", "SPAWN_CHILD_EXIT").
		Msg("子进程退出")
	return nil
}

// ProcessMessageLoop 子进程消息循环。
// 对齐 Python: process_message_loop()
func ProcessMessageLoop(
	ctx context.Context,
	stdin io.Reader,
	stdout io.Writer,
	agentConfig *SpawnAgentConfig,
	inputs map[string]any,
	childRunner ChildRunner,
	agentCreator AgentCreator,
) error {
	shutdownRequested := false
	var agentCancel context.CancelFunc
	agentDoneCh := make(chan struct{}, 1)

	for !shutdownRequested {
		// 使用 goroutine + channel 桥接 stdin 读取
		msgCh := make(chan Message, 1)
		errCh := make(chan error, 1)
		go func() {
			msg, err := ReadMessage(stdin)
			if err != nil {
				errCh <- err
				return
			}
			msgCh <- msg
		}()

		select {
		case msg := <-msgCh:
			// 处理消息
			switch msg.Type {
			case MessageTypeHealthCheck:
				if err := HandleHealthCheck(ctx, msg, stdout); err != nil {
					logger.Error(logger.ComponentAgentCore).
						Str("event_type", "SPAWN_HEALTH_CHECK_ERROR").
						Err(err).
						Msg("健康检查处理错误")
				}
			case MessageTypeShutdown:
				if agentCancel != nil {
					agentCancel()
				}
				if err := HandleShutdown(ctx, msg, stdout); err != nil {
					logger.Error(logger.ComponentAgentCore).
						Str("event_type", "SPAWN_SHUTDOWN_ERROR").
						Err(err).
						Msg("关闭处理错误")
				}
				shutdownRequested = true
			case MessageTypeInput:
				payload, ok := msg.Payload.(map[string]any)
				if !ok {
					continue
				}
				if ac, ok := payload["agent_config"]; ok {
					if acMap, ok := ac.(map[string]any); ok {
						parsedCfg := prepareSpawnAgentConfig(acMap)
						agentConfig = parsedCfg
					}
				}
				if newInputs, ok := payload["inputs"]; ok {
					if inputMap, ok := newInputs.(map[string]any); ok {
						for k, v := range inputMap {
							inputs[k] = v
						}
					}
				}
				if agentCancel == nil {
					streaming := false
					if s, ok := payload["streaming"]; ok {
						if sb, ok := s.(bool); ok {
							streaming = sb
						}
					}
					var streamModes any
					if sm, ok := payload["stream_modes"]; ok {
						streamModes = sm
					}
					if agentConfig == nil {
						errMsg := NewMessage(MessageTypeError, map[string]any{
							"error":      "缺少 agent_config",
							"error_type": "ValueError",
						})
						_ = WriteMessage(stdout, errMsg)
						return fmt.Errorf("缺少 agent_config")
					}
					var agentCtx context.Context
					agentCtx, agentCancel = context.WithCancel(ctx)
					go runAgentTask(agentCtx, *agentConfig, inputs, stdout, msg.MessageID, streaming, streamModes, agentDoneCh, childRunner, agentCreator)
				}
			default:
				logger.Warn(logger.ComponentAgentCore).
					Str("event_type", "SPAWN_UNKNOWN_MESSAGE").
					Str("message_type", msg.Type.String()).
					Msg("未知消息类型")
			}

		case <-agentDoneCh:
			// Agent 任务完成，退出循环
			if agentCancel != nil {
				agentCancel()
			}
			return nil

		case err := <-errCh:
			// stdin 读取错误
			if err == io.EOF {
				logger.Info(logger.ComponentAgentCore).
					Str("event_type", "SPAWN_STDIN_CLOSED").
					Msg("stdin 关闭，退出消息循环")
				if agentCancel != nil {
					agentCancel()
				}
				return nil
			}
			logger.Error(logger.ComponentAgentCore).
				Str("event_type", "SPAWN_STDIN_ERROR").
				Err(err).
				Msg("stdin 读取错误")
			if agentCancel != nil {
				agentCancel()
			}
			return fmt.Errorf("stdin 读取错误: %w", err)

		case <-ctx.Done():
			// 上下文取消
			if agentCancel != nil {
				agentCancel()
			}
			return ctx.Err()
		}
	}

	if agentCancel != nil {
		agentCancel()
	}
	return nil
}

// HandleHealthCheck 处理健康检查请求。
// 对齐 Python: handle_health_check()
func HandleHealthCheck(ctx context.Context, msg Message, stdout io.Writer) error {
	response := NewMessage(MessageTypeHealthCheckResponse, map[string]any{
		"status": "healthy",
	})
	if err := WriteMessage(stdout, response); err != nil {
		return fmt.Errorf("写入健康检查响应失败: %w", err)
	}
	logger.Debug(logger.ComponentAgentCore).
		Str("event_type", "SPAWN_HEALTH_CHECK_RESPONSE").
		Msg("回复健康检查")
	return nil
}

// HandleShutdown 处理关闭请求。
// 对齐 Python: handle_shutdown()
func HandleShutdown(ctx context.Context, msg Message, stdout io.Writer) error {
	logger.Info(logger.ComponentAgentCore).
		Str("event_type", "SPAWN_SHUTDOWN_RECEIVED").
		Msg("收到关闭请求")
	ack := NewMessage(MessageTypeShutdownAck, map[string]any{
		"status": "acknowledged",
	})
	if err := WriteMessage(stdout, ack); err != nil {
		return fmt.Errorf("写入关闭确认失败: %w", err)
	}
	return nil
}

// ExecuteAgent 在子进程中执行 Agent。
// 对齐 Python: execute_agent()
func ExecuteAgent(
	ctx context.Context,
	agentConfig SpawnAgentConfig,
	inputs map[string]any,
	stdout io.Writer,
	streaming bool,
	streamModes any,
	childRunner ChildRunner,
	agentCreator AgentCreator,
) (any, error) {
	switch agentConfig.AgentKind {
	case SpawnAgentKindClassAgent:
		return executeChildAgent(ctx, agentConfig, inputs, stdout, streaming, streamModes, childRunner, agentCreator)
	case SpawnAgentKindTeamAgent:
		return nil, fmt.Errorf("team_agent 模式尚未实现：依赖 9.x TeamAgent")
	default:
		return nil, fmt.Errorf("不支持的 Agent 启动方式: %s", agentConfig.AgentKind)
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// prepareSpawnAgentConfig 准备 Spawn Agent 配置。
// 对齐 Python: _prepare_spawn_agent_config()
// 额外行为：解析到 logging_config 非空时，调用 logger.Reconfigure() 动态更新日志配置。
func prepareSpawnAgentConfig(agentConfig map[string]any) *SpawnAgentConfig {
	if agentConfig == nil {
		return nil
	}
	cfg, err := ParseSpawnAgentConfig(agentConfig)
	if err != nil {
		logger.Error(logger.ComponentAgentCore).
			Str("event_type", "SPAWN_CONFIG_PARSE_ERROR").
			Err(err).
			Msg("解析 Agent 配置失败")
		return nil
	}

	// 场景(2)：从 SpawnAgentConfig.logging_config 动态更新日志配置。
	// 对齐 Python: _prepare_spawn_agent_config() L119-122
	//   if spawn_agent_config.logging_config is not None:
	//       configure_log_config(spawn_agent_config.logging_config)
	if cfg.LoggingConfig != nil {
		applyLoggingConfigMap(cfg.LoggingConfig)
	}

	return &cfg
}

// runAgentTask 包装 Agent 执行，成功发送 DONE，失败发送 ERROR。
// 对齐 Python: _run_agent_task()
func runAgentTask(
	ctx context.Context,
	agentConfig SpawnAgentConfig,
	inputs map[string]any,
	stdout io.Writer,
	messageID string,
	streaming bool,
	streamModes any,
	doneCh chan<- struct{},
	childRunner ChildRunner,
	agentCreator AgentCreator,
) {
	defer func() { doneCh <- struct{}{} }()

	result, err := ExecuteAgent(ctx, agentConfig, inputs, stdout, streaming, streamModes, childRunner, agentCreator)
	if err != nil {
		logger.Error(logger.ComponentAgentCore).
			Str("event_type", "LLM_CALL_ERROR").
			Err(err).
			Msg("Agent 执行错误")
		errMsg := NewMessage(MessageTypeError, map[string]any{
			"error":      err.Error(),
			"error_type": fmt.Sprintf("%T", err),
		})
		_ = WriteMessage(stdout, errMsg)
		return
	}

	doneMsg := NewMessage(MessageTypeDone, map[string]any{
		"result": result,
	})
	_ = WriteMessage(stdout, doneMsg)
	logger.Info(logger.ComponentAgentCore).
		Str("event_type", "SPAWN_AGENT_DONE").
		Msg("Agent 执行完成")
}

// executeChildAgent 在子进程中执行 Agent。
// 对齐 Python: execute_agent() L148-169
func executeChildAgent(
	ctx context.Context,
	agentConfig SpawnAgentConfig,
	inputs map[string]any,
	stdout io.Writer,
	streaming bool,
	streamModes any,
	childRunner ChildRunner,
	agentCreator AgentCreator,
) (any, error) {
	// 解析 ClassAgentSpawnConfig
	classConfig := ClassAgentSpawnConfig{}
	data, err := json.Marshal(agentConfig)
	if err != nil {
		return nil, fmt.Errorf("序列化 Agent 配置失败: %w", err)
	}
	if err := json.Unmarshal(data, &classConfig); err != nil {
		return nil, fmt.Errorf("解析 ClassAgentSpawnConfig 失败: %w", err)
	}

	// 从 AgentCard 中取 agent_type
	var agentType string
	if classConfig.AgentCard != nil {
		if at, ok := classConfig.AgentCard["agent_type"].(string); ok {
			agentType = at
		}
	}
	if agentType == "" {
		agentType = "react_agent" // 默认
	}

	// 创建 Agent 实例。
	// 对齐 Python:
	//   module = importlib.import_module(class_config.agent_module)
	//   agent_cls = getattr(module, class_config.agent_class)
	//   agent = agent_cls(**class_config.init_kwargs)
	if agentCreator == nil {
		return nil, fmt.Errorf("未注入 AgentCreator，无法创建 Agent 实例")
	}
	if classConfig.AgentCard == nil {
		return nil, fmt.Errorf("缺少 agent_card，无法创建 Agent 实例")
	}

	agent, err := agentCreator.CreateByType(ctx, agentType, classConfig.AgentCard, classConfig.InitKwargs)
	if err != nil {
		return nil, fmt.Errorf("创建 Agent 实例失败: %w", err)
	}

	// 从 agentConfig 取 session_id。
	// 对齐 Python: session = agent_config.session_id
	sessionID := agentConfig.SessionID

	logger.Info(logger.ComponentAgentCore).
		Str("event_type", "SPAWN_CHILD_AGENT").
		Str("agent_type", agentType).
		Bool("streaming", streaming).
		Msg("CHILD_AGENT 已创建实例，开始执行")

	// 执行 Agent
	if childRunner == nil {
		return nil, fmt.Errorf("未注入 ChildRunner，无法执行 Agent")
	}

	if streaming {
		// streaming 路径。
		// 对齐 Python:
		//   async for chunk in Runner.run_agent_streaming(agent, inputs, session=session, stream_modes=stream_modes):
		//       stream_message = Message(type=MessageType.STREAM_CHUNK, payload=chunk)
		//       await write_output_to_stdout(stream_message, writer)
		//       result_chunks.append(chunk)
		//   return result_chunks
		streamCh, err := childRunner.RunAgentStreaming(ctx, agent, inputs, sessionID, streamModes)
		if err != nil {
			return nil, fmt.Errorf("启动 Agent 流式执行失败: %w", err)
		}

		var resultChunks []any
		for chunk := range streamCh {
			chunkMsg := NewMessage(MessageTypeStreamChunk, chunk)
			if writeErr := WriteMessage(stdout, chunkMsg); writeErr != nil {
				logger.Error(logger.ComponentAgentCore).
					Str("event_type", "SPAWN_STREAM_WRITE_ERROR").
					Err(writeErr).
					Msg("写入 STREAM_CHUNK 失败")
			}
			resultChunks = append(resultChunks, chunk)
		}
		return resultChunks, nil
	}

	// 非 streaming 路径。
	// 对齐 Python: return await Runner.run_agent(agent=agent, inputs=inputs, session=session)
	return childRunner.RunAgent(ctx, agent, inputs, sessionID)
}

// applyLoggingConfigFromEnv 从环境变量 UAPCLAW_SPAWN_LOGGING_CONFIG 读取日志配置并应用。
// 对齐 Python: child_process.py L22-27
//
//	_logging_config_json = os.environ.pop("OPENJIUWEN_SPAWN_LOGGING_CONFIG", None)
//	if _logging_config_json:
//	    configure_log_config(_json.loads(_logging_config_json))
//
// 子进程首次初始化时调用 logger.Setup()，后续调用 logger.Reconfigure()。
func applyLoggingConfigFromEnv() {
	loggingConfigJSON := os.Getenv(EnvSpawnLoggingConfig)
	if loggingConfigJSON == "" {
		// 没有环境变量配置，确保 logger 已初始化
		if !logger.IsSetup() {
			_ = logger.Setup()
		}
		return
	}

	var loggingConfig map[string]any
	if err := json.Unmarshal([]byte(loggingConfigJSON), &loggingConfig); err != nil {
		// 解析失败，确保 logger 已初始化（用默认配置）
		if !logger.IsSetup() {
			_ = logger.Setup()
		}
		logger.Warn(logger.ComponentAgentCore).
			Str("event_type", "SPAWN_LOGGING_CONFIG_PARSE_ERROR").
			Err(err).
			Msg("解析日志配置环境变量失败，使用默认配置")
		return
	}

	// 根据 logger 是否已初始化，选择 Setup 或 Reconfigure
	if !logger.IsSetup() {
		_ = logger.Setup(logger.WithLogLevel(resolveLogLevelFromConfig(loggingConfig)))
	} else {
		_ = logger.Reconfigure(logger.WithLogLevel(resolveLogLevelFromConfig(loggingConfig)))
	}
}

// applyLoggingConfigMap 从 map[string]any 类型的日志配置动态更新 logger。
// 对齐 Python: configure_log_config(logging_config)
func applyLoggingConfigMap(loggingConfig map[string]any) {
	if loggingConfig == nil {
		return
	}

	levelStr := resolveLogLevelFromConfig(loggingConfig)

	if !logger.IsSetup() {
		_ = logger.Setup(logger.WithLogLevel(levelStr))
	} else {
		_ = logger.Reconfigure(logger.WithLogLevel(levelStr))
	}

	logger.Info(logger.ComponentAgentCore).
		Str("event_type", "SPAWN_LOGGING_CONFIG_APPLIED").
		Str("level", levelStr).
		Msg("已应用日志配置")
}

// resolveLogLevelFromConfig 从日志配置 map 中提取日志级别字符串。
func resolveLogLevelFromConfig(loggingConfig map[string]any) string {
	if level, ok := loggingConfig["level"]; ok {
		if levelStr, ok := level.(string); ok && levelStr != "" {
			return levelStr
		}
	}
	return ""
}
