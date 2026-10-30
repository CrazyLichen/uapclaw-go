package main

// acp_chat_cmd.go 提供 uapclaw acp-chat 子命令（10.4.6）。
// Python: jiuwenswarm/acp/cli.py（90 行，一次性冒烟：connect → chat 一次 → close）。
// 定位：外部 ACP 子代理出口链的旁路冒烟入口——不进 Gateway/AgentServer/LLM/工具调度，
// 不产生 E2A 消息与会话历史（设计见 docs/superpowers/specs/2026-10-31-acp-cli-smoke-10.4.6-design.md）。

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/config"
	"github.com/uapclaw/uapclaw-go/internal/common/crypto"
	"github.com/uapclaw/uapclaw-go/internal/common/dotenv"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
)

// ──────────────────────────── 结构体 ────────────────────────────

// acpChatExitError 带进程退出码的语义错误（Execute 消费，映射为 os.Exit 码）。
type acpChatExitError struct {
	// code 进程退出码
	code int
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// exitCodeOK 成功
	exitCodeOK = 0
	// exitCodeSession 会话异常（connect/chat 失败）
	exitCodeSession = 1
	// exitCodeConfig 配置错误（profile 缺失/规格非法/参数个数错）
	exitCodeConfig = 2
	// exitCodeInterrupted 被信号中断（对齐 Python KeyboardInterrupt → 130）
	exitCodeInterrupted = 130
	// envSkipDotenv 跳过 .env 加载的开关（品牌映射自 Python JIUWENCLAW_SKIP_DOTENV）
	envSkipDotenv = "UAPCLAW_SKIP_DOTENV"
	// logComponentAcpChat 冒烟入口日志组件（工具层归属 AgentServer，对齐 acpchat 包）
	logComponentAcpChat = logger.ComponentAgentServer
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// Error 实现 error 接口。
func (e *acpChatExitError) Error() string {
	return fmt.Sprintf("acp-chat exit code %d", e.code)
}

// exitCodeFor 从 error 解析进程退出码（nil → 0，未知 → 1）。
func exitCodeFor(err error) int {
	if err == nil {
		return exitCodeOK
	}
	var exitErr *acpChatExitError
	if errors.As(err, &exitErr) && exitErr != nil {
		return exitErr.code
	}
	return exitCodeSession
}

// acpChatSkipDotenv 是否跳过 .env 加载（对齐 Python：strip() 后等于 "1"）。
func acpChatSkipDotenv(getenv func(string) string) bool {
	if getenv == nil {
		return false
	}
	return strings.TrimSpace(getenv(envSkipDotenv)) == "1"
}

// loadAcpChatConfig 生产配置加载：logger.Setup → 可选 dotenv → config.Load。
// Python: main() 的 load_dotenv 段 + _get_spec 里的 get_config()。
func loadAcpChatConfig(_ context.Context) (map[string]any, error) {
	// Go 的 logger 未 Setup 时是 Nop（静默），冒烟工具必须初始化才有诊断输出
	if err := logger.Setup(logger.WithConfigFile()); err != nil {
		fmt.Fprintf(os.Stderr, "初始化日志系统失败: %v\n", err)
	}

	envFile := ""
	if !acpChatSkipDotenv(os.Getenv) {
		envFile = workspace.EnvFile()
	}
	return loadAcpChatConfigFrom("", envFile)
}

// loadAcpChatConfigFrom 从指定路径加载配置（测试注入点）。
// cfgPath 空 → 默认 config.yaml；envFile 空 → 不加载 .env。
// Python: get_config() = yaml.safe_load + resolve_env_vars + _normalize_config。
func loadAcpChatConfigFrom(cfgPath, envFile string) (map[string]any, error) {
	if envFile != "" {
		// 非覆盖加载：shell 已导出的变量优先（Python override=False）
		if err := dotenv.LoadNonOverride(envFile); err != nil {
			logger.Debug(logComponentAcpChat).Err(err).Str("env_file", envFile).
				Msg("[acp-chat] skipping dotenv load")
		}
	}

	cfg, err := config.New(cfgPath,
		config.WithNormalize(config.NormalizeConfig),
		config.WithDecrypt(crypto.NewDecryptFunc()))
	if err != nil {
		return nil, err
	}
	return cfg.Load()
}
