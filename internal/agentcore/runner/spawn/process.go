package spawn

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/google/uuid"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// EnvSpawnProcess 子进程标识环境变量
	EnvSpawnProcess = "UAPCLAW_SPAWN_PROCESS"
	// EnvSpawnLoggingConfig 子进程日志配置环境变量
	EnvSpawnLoggingConfig = "UAPCLAW_SPAWN_LOGGING_CONFIG"
	// SpawnChildSubCommand spawn-child 子命令名
	SpawnChildSubCommand = "spawn-child"
)

// SpawnProcess 创建子进程运行 Agent，返回 SpawnedProcessHandle。
// 对齐 Python: spawn_process() (process_manager.py)
// ──────────────────────────── 导出函数 ────────────────────────────

func SpawnProcess(
	ctx context.Context,
	agentConfig SpawnAgentConfig,
	inputs map[string]any,
	cfg ...SpawnConfig,
) (*SpawnedProcessHandle, error) {
	spawnCfg := DefaultSpawnConfig()
	if len(cfg) > 0 {
		spawnCfg = cfg[0]
	}

	processID := uuid.New().String()

	// 获取当前可执行文件路径
	exePath, err := getSelfExecutable()
	if err != nil {
		return nil, fmt.Errorf("获取可执行文件路径失败: %w", err)
	}

	cmd := exec.CommandContext(ctx, exePath, SpawnChildSubCommand)

	// 设置环境变量
	env := os.Environ()
	env = append(env, fmt.Sprintf("%s=1", EnvSpawnProcess))
	if agentConfig.LoggingConfig != nil {
		loggingJSON, err := json.Marshal(agentConfig.LoggingConfig)
		if err == nil {
			env = append(env, fmt.Sprintf("%s=%s", EnvSpawnLoggingConfig, string(loggingJSON)))
		}
	}
	cmd.Env = env

	// 创建管道
	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("创建 stdin 管道失败: %w", err)
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("创建 stdout 管道失败: %w", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("创建 stderr 管道失败: %w", err)
	}

	// 后台读取 stderr 日志
	go drainStderr(stderrPipe)

	commandStr := fmt.Sprintf("%s %s", exePath, SpawnChildSubCommand)
	logger.Info(logger.ComponentAgentCore).
		Str("event_type", "SPAWN_PROCESS_START").
		Str("process_id", processID).
		Str("command", commandStr).
		Msg("启动子进程")

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("启动子进程失败: %w", err)
	}

	handle := NewSpawnedProcessHandle(processID, cmd, stdinPipe, stdoutPipe, spawnCfg, nil, 0)

	// 发送初始 INPUT 消息
	initMsg := NewMessage(MessageTypeInput, map[string]any{
		"agent_config": agentConfig,
		"inputs":       inputs,
	})
	if err := handle.SendMessage(ctx, initMsg); err != nil {
		_ = handle.ForceKill()
		return nil, fmt.Errorf("发送初始消息失败: %w", err)
	}

	logger.Info(logger.ComponentAgentCore).
		Str("event_type", "SPAWN_PROCESS_SUCCESS").
		Str("process_id", processID).
		Int("pid", cmd.Process.Pid).
		Msg("子进程启动成功")

	return handle, nil
}

// getSelfExecutable 获取当前主二进制的路径，用于启动子进程。
// ──────────────────────────── 非导出函数 ────────────────────────────

func getSelfExecutable() (string, error) {
	return os.Executable()
}

// drainStderr 后台读取子进程 stderr 输出。
func drainStderr(stderrPipe io.Reader) {
	scanner := bufio.NewScanner(stderrPipe)
	for scanner.Scan() {
		logger.Debug(logger.ComponentAgentCore).
			Str("event_type", "SPAWN_CHILD_STDERR").
			Str("line", scanner.Text()).
			Msg("子进程 stderr")
	}
}
