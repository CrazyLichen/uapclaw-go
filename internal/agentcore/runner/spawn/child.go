package spawn

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// RunSpawnedProcess 子进程主入口。
// 对齐 Python: run_spawned_process()
func RunSpawnedProcess(ctx context.Context, agentConfig map[string]any, inputs map[string]any) error {
	logger.Info(logger.ComponentAgentCore).
		Str("event_type", "SPAWN_CHILD_START").
		Msg("子进程启动")

	// 准备 Agent 配置
	spawnAgentConfig := prepareSpawnAgentConfig(agentConfig)

	// 子进程的 stdin/stdout 即 os.Stdin/os.Stdout（由 os/exec 管道连接）
	stdin := os.Stdin
	stdout := os.Stdout

	// 运行消息循环
	err := ProcessMessageLoop(ctx, stdin, stdout, spawnAgentConfig, inputs)
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
					var streamModes []string
					if sm, ok := payload["stream_modes"]; ok {
						if smSlice, ok := sm.([]string); ok {
							streamModes = smSlice
						}
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
					go runAgentTask(agentCtx, *agentConfig, inputs, stdout, msg.MessageID, streaming, streamModes, agentDoneCh)
				}
			default:
				logger.Warn(logger.ComponentAgentCore).
					Str("event_type", "SPAWN_UNKNOWN_MESSAGE").
					Str("message_type", msg.Type.String()).
					Msg("未知消息类型")
			}

		case <-agentDoneCh:
			// Agent 任务完成，退出循环
			return nil

		case err := <-errCh:
			// stdin 读取错误
			if err == io.EOF {
				logger.Info(logger.ComponentAgentCore).
					Str("event_type", "SPAWN_STDIN_CLOSED").
					Msg("stdin 关闭，退出消息循环")
				return nil
			}
			logger.Error(logger.ComponentAgentCore).
				Str("event_type", "SPAWN_STDIN_ERROR").
				Err(err).
				Msg("stdin 读取错误")
			return fmt.Errorf("stdin 读取错误: %w", err)

		case <-ctx.Done():
			// 上下文取消
			if agentCancel != nil {
				agentCancel()
			}
			return ctx.Err()
		}
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
	streamModes []string,
) (any, error) {
	switch agentConfig.AgentKind {
	case SpawnAgentKindClassAgent:
		return executeClassAgent(ctx, agentConfig, inputs, stdout, streaming, streamModes)
	case SpawnAgentKindTeamAgent:
		return nil, fmt.Errorf("TEAM_AGENT 模式尚未实现：依赖 9.x TeamAgent")
	default:
		return nil, fmt.Errorf("不支持的 Agent 启动方式: %s", agentConfig.AgentKind)
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// prepareSpawnAgentConfig 准备 Spawn Agent 配置。
// 对齐 Python: _prepare_spawn_agent_config()
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
	streamModes []string,
	doneCh chan<- struct{},
) {
	defer func() { doneCh <- struct{}{} }()

	result, err := ExecuteAgent(ctx, agentConfig, inputs, stdout, streaming, streamModes)
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

// executeClassAgent 执行类 Agent。
func executeClassAgent(
	ctx context.Context,
	agentConfig SpawnAgentConfig,
	inputs map[string]any,
	stdout io.Writer,
	streaming bool,
	streamModes []string,
) (any, error) {
	// ⤵️ 预留：ResourceMgr.GetAgent() 查注册表 + Runner.RunAgent() 执行
	// 当前返回占位结果，待 Runner 和 ResourceMgr 完整集成后回填
	logger.Info(logger.ComponentAgentCore).
		Str("event_type", "SPAWN_CLASS_AGENT").
		Str("agent_kind", string(agentConfig.AgentKind)).
		Bool("streaming", streaming).
		Msg("CLASS_AGENT 执行（预留：待 ResourceMgr 集成）")
	return map[string]any{"status": "placeholder", "agent_kind": "class_agent"}, nil
}
