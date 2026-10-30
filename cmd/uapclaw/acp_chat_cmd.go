package main

// acp_chat_cmd.go 提供 uapclaw acp-chat 子命令（10.4.6）。
// Python: jiuwenswarm/acp/cli.py（90 行，一次性冒烟：connect → chat 一次 → close）。
// 定位：外部 ACP 子代理出口链的旁路冒烟入口——不进 Gateway/AgentServer/LLM/工具调度，
// 不产生 E2A 消息与会话历史（设计见 docs/superpowers/specs/2026-10-31-acp-cli-smoke-10.4.6-design.md）。

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/uapclaw/uapclaw-go/internal/common/config"
	"github.com/uapclaw/uapclaw-go/internal/common/crypto"
	"github.com/uapclaw/uapclaw-go/internal/common/dotenv"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
	"github.com/uapclaw/uapclaw-go/internal/swarm/acp"
	acpchat "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/tools/acp_chat"
)

// ──────────────────────────── 结构体 ────────────────────────────

// acpChatExitError 带进程退出码的语义错误（Execute 消费，映射为 os.Exit 码）。
type acpChatExitError struct {
	// code 进程退出码
	code int
}

// acpChatDeps 冒烟入口依赖注入点（生产用 defaultAcpChatDeps，测试替换）。
type acpChatDeps struct {
	// loadConfig 读取 config base 映射
	loadConfig func(context.Context) (map[string]any, error)
	// connect 建立 ACP 会话
	connect func(context.Context, *acp.AcpStdioClient) error
	// chat 发送一轮消息并取回文本
	chat func(context.Context, *acp.AcpStdioClient, string) (string, error)
	// stdout 响应文本输出目标
	stdout io.Writer
	// getenv 环境变量读取
	getenv func(string) string
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
	// envCloseTimeout 关闭超时环境变量（对齐 Python ACP_CLI_CLOSE_TIMEOUT_S）
	envCloseTimeout = "ACP_CLI_CLOSE_TIMEOUT_S"
	// defaultCloseTimeoutS 关闭超时默认秒数（Python 默认 30）
	defaultCloseTimeoutS = 30
	// emptyResponse 空响应占位（对齐 Python cli.py 字面，勿用工具侧文案）
	emptyResponse = "(empty response)"
	// defaultAcpChatMessage 缺省消息（Python argparse default="Hello"）
	defaultAcpChatMessage = "Hello"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// newAcpChatCmd 创建 acp-chat 子命令（一次性冒烟，对齐 Python console script jiuwenswarm-acp-chat）。
func newAcpChatCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "acp-chat <profile> [message]",
		Short: "外部 ACP 子代理冒烟入口（一次性 connect→chat→close）",
		Long: `对 config.yaml 的 acp_agents.<profile> 做一次冒烟：spawn 子进程 → ACP 握手 →
发送一条消息 → 打印回复 → 关闭。不进 Gateway/AgentServer，不产生会话历史。

profile 规格与 acp_chat 工具共用严格校验（args 必须是列表、env 必须是映射）。

退出码：0 成功 / 2 配置错 / 1 会话异常 / 130 中断。`,
		// 冒烟工具不该打 usage 与 Error: 前缀（对齐 Python argparse 的输出观感）
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			// cobra 仅在 Execute 路径注入 ctx；测试/库调用可能为 nil，回落 Background
			parent := cmd.Context()
			if parent == nil {
				parent = context.Background()
			}
			ctx, stop := signal.NotifyContext(parent, syscall.SIGINT, syscall.SIGTERM)
			defer stop()

			if len(args) == 0 || len(args) > 2 {
				fmt.Fprintln(os.Stderr, "usage: uapclaw acp-chat <profile> [message]")
				return &acpChatExitError{code: exitCodeConfig}
			}

			message := defaultAcpChatMessage
			if len(args) == 2 {
				message = args[1]
			}

			code := runAcpChat(ctx, defaultAcpChatDeps(), args[0], message)
			if code == exitCodeOK {
				return nil
			}
			return &acpChatExitError{code: code}
		},
	}
	return cmd
}

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

// defaultAcpChatDeps 生产依赖。
func defaultAcpChatDeps() acpChatDeps {
	return acpChatDeps{
		loadConfig: loadAcpChatConfig,
		connect: func(ctx context.Context, c *acp.AcpStdioClient) error {
			return c.Connect(ctx)
		},
		chat: func(ctx context.Context, c *acp.AcpStdioClient, msg string) (string, error) {
			return c.Chat(ctx, msg)
		},
		stdout: os.Stdout,
		getenv: os.Getenv,
	}
}

// acpChatCloseTimeout 关闭超时（env ACP_CLI_CLOSE_TIMEOUT_S，默认 30s；非法值回落默认）。
// 偏差：Python float() 解析失败会抛 ValueError，这里回落默认值（防御性）。
func acpChatCloseTimeout(getenv func(string) string) time.Duration {
	if getenv != nil {
		if raw := strings.TrimSpace(getenv(envCloseTimeout)); raw != "" {
			if secs, err := strconv.ParseFloat(raw, 64); err == nil {
				if secs < 0 {
					secs = 0
				}
				return time.Duration(secs * float64(time.Second))
			}
		}
	}
	return defaultCloseTimeoutS * time.Second
}

// closeAcpChatClientWithTimeout 限时等待关闭（对齐 Python wait_for(shield(close()))）。
// 超时/错误仅 Warn，不影响退出码。
func closeAcpChatClientWithTimeout(closer interface{ Close() error }, timeout time.Duration) {
	if closer == nil {
		return
	}
	done := make(chan error, 1)
	go func() { done <- closer.Close() }()
	select {
	case err := <-done:
		if err != nil {
			logger.Warn(logComponentAcpChat).Err(err).Msg("[acp-chat] failed to close ACP client")
		}
	case <-time.After(timeout):
		logger.Warn(logComponentAcpChat).Str("timeout", timeout.String()).
			Msg("[acp-chat] close ACP client timed out")
	}
}

// acpChatFailureCode 失败分支退出码：信号取消 → 130，否则 1（并记 Error 日志）。
func acpChatFailureCode(ctx context.Context, stage string, err error) int {
	if ctx.Err() != nil {
		logger.Warn(logComponentAcpChat).Str("stage", stage).Err(err).
			Msg("[acp-chat] interrupted by signal")
		return exitCodeInterrupted
	}
	logger.Error(logComponentAcpChat).Str("stage", stage).Err(err).
		Msg("[acp-chat] ACP session failed")
	return exitCodeSession
}

// runAcpChat 冒烟主体（对齐 Python _run_once）：返回进程退出码，不上抛语义错误。
func runAcpChat(ctx context.Context, deps acpChatDeps, profile, message string) int {
	raw, err := deps.loadConfig(ctx)
	if err != nil {
		logger.Error(logComponentAcpChat).Err(err).Msg("[acp-chat] load config failed")
		return exitCodeConfig
	}

	spec, err := acpchat.LoadProfile(raw, profile)
	if err != nil {
		logger.Error(logComponentAcpChat).Str("profile", profile).Err(err).
			Msg("[acp-chat] profile lookup failed")
		return exitCodeConfig
	}

	cs, err := acpchat.BuildClientSpec(spec)
	if err != nil {
		logger.Error(logComponentAcpChat).Str("profile", profile).Err(err).
			Msg("[acp-chat] invalid profile spec")
		return exitCodeConfig
	}

	client := acp.NewAcpStdioClient(cs.Command, cs.Args, cs.Cwd, cs.Env)
	// 对齐 Python finally：无论如何都尝试关闭，且不改退出码
	defer closeAcpChatClientWithTimeout(client, acpChatCloseTimeout(deps.getenv))

	if err := deps.connect(ctx, client); err != nil {
		return acpChatFailureCode(ctx, "connect", err)
	}

	out, err := deps.chat(ctx, client, message)
	if err != nil {
		return acpChatFailureCode(ctx, "chat", err)
	}

	if out == "" {
		out = emptyResponse
	}
	fmt.Fprintln(deps.stdout, out)
	return exitCodeOK
}
