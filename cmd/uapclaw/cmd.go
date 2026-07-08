// Package main 提供 uapclaw 命令定义。
//
// 可测试的命令构建逻辑放在此文件，
// main.go 中的 Execute() + main() 用 //go:build !test 隔离，不参与单元测试覆盖率统计。
package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/spawn"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/spawn/factory"
	"github.com/uapclaw/uapclaw-go/internal/common/config"
	"github.com/uapclaw/uapclaw-go/internal/common/dotenv"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/version"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/gateway_push"
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// newRootCmd 创建根命令
func newRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "uapclaw",
		Short: "UapClaw — Agent 平台命令行工具",
		Long: `UapClaw 是 Agent 平台的统一命令行工具。

所有外部入口统一经过 Gateway，Gateway 与 AgentServer 之间始终走 E2A 协议。
进程内通过 ChannelTransport（Go channel）通信，跨进程通过 WebSocketTransport 通信。

运行模式：
  uapclaw chat       → REPL Channel   → Gateway → E2A → ChannelTransport → AgentServer
  uapclaw serve      → HTTP Channel   → Gateway → E2A → ChannelTransport → AgentServer
  uapclaw acp        → stdio Channel  → Gateway → E2A → ChannelTransport → AgentServer
  uapclaw app        → IM/Web Channel → Gateway → E2A → ChannelTransport → AgentServer
  uapclaw agentserver → 独立进程，监听 WS，等待外部 Gateway 连入
  uapclaw gateway    → 独立进程，通过 WS + E2A 连接外部 AgentServer`,
		Version: version.Version,
	}

	// 全局参数：多实例隔离
	// --dotenv/--name 仅在运行服务的子命令中有意义（选择使用哪个实例），
	// init 子命令有自己的 --name（语义是"创建实例"），不走这个回调。
	rootCmd.PersistentFlags().String("dotenv", "", "指定 .env 文件路径（用于多实例隔离）")
	rootCmd.PersistentFlags().String("name", "", "指定命名实例（用于多实例隔离）")

	// 子命令注册
	rootCmd.AddCommand(
		newChatCmd(),
		newServeCmd(),
		newAppCmd(),
		newAgentServerCmd(),
		newGatewayCmd(),
		newWebCmd(),
		newInitCmd(),
		newAcpCmd(),
		newSpawnChildCmd(),
	)

	return rootCmd
}

// newChatCmd 创建 chat 子命令
func newChatCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "chat",
		Short: "启动 CLI REPL 聊天模式",
		Long: `启动 CLI REPL 交互界面，REPL 作为 Channel 接入 Gateway。
进程内 Gateway 通过 ChannelTransport 与 AgentServer 通信。
内部流程：REPL → Gateway → E2A编码 → Go channel → E2A解码 → AgentServer → agentcore`,
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("chat 模式尚未实现（领域十）")
		},
	}
	// 预解析 --dotenv/--name，确保 UAPCLAW_DATA_DIR 在 workspace 路径函数首次调用前就位
	cmd.PreRunE = makeDotenvPreRunE()
	return cmd
}

// newServeCmd 创建 serve 子命令
func newServeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "启动 HTTP REST API + SSE 流式服务",
		Long: `启动 HTTP API 服务，HTTP 作为 Channel 接入 Gateway。
提供 RESTful 接口和 SSE 流式响应。
进程内 Gateway 通过 ChannelTransport 与 AgentServer 通信。`,
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("serve 模式尚未实现（领域十）")
		},
	}
	cmd.PreRunE = makeDotenvPreRunE()
	return cmd
}

// newAppCmd 创建 app 子命令
func newAppCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "app",
		Short: "启动完整模式（AgentServer + Gateway）",
		Long: `同时启动 Gateway 和 AgentServer（同进程），支持所有 IM 渠道接入。
Gateway 通过 ChannelTransport 与 AgentServer 通信。`,
		RunE: runAppCmd,
	}
	cmd.PreRunE = makeDotenvPreRunE()
	cmd.Flags().String("host", "", "监听地址（默认 127.0.0.1）")
	cmd.Flags().Int("port", 0, "监听端口（默认 19000）")
	return cmd
}

// runAppCmd 执行 app 子命令。
//
// 启动流程对齐 Python app.py / app_gateway.py：
//
//	1. ParseEarly        → 早期 dotenv（--dotenv/--name，在 PreRunE 中）
//	2. workspace.Prepare → workspace 自动初始化
//	3. logger.Setup      → 日志初始化（尽早，后续步骤的日志可写文件）
//	4. dotenv.Load       → 加载主 .env（~/.uapclaw/config/.env）
//	5. config.New+Load   → 完整配置加载
//	6. ResetFlags        → 重置免费搜索运行时标志
func runAppCmd(cmd *cobra.Command, _ []string) error {
	ctx, cancel := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// CLI flags 覆写环境变量（优先于配置文件）
	host, _ := cmd.Flags().GetString("host")
	port, _ := cmd.Flags().GetInt("port")
	if host != "" {
		os.Setenv("UAPCLAW_GATEWAY_HOST", host)
	}
	if port > 0 {
		os.Setenv("UAPCLAW_GATEWAY_PORT", fmt.Sprintf("%d", port))
	}

	// 2. workspace 自动初始化（等价 Python: prepare_workspace(overwrite=False)）
	if !workspace.IsInitialized() {
		if _, err := workspace.Prepare(workspace.InitOption{
			Overwrite: false,
			Language:  "zh",
		}); err != nil {
			return fmt.Errorf("初始化工作区失败: %w", err)
		}
	}

	// 3. 日志初始化（等价 Python: configure_log() / setup_logger()）
	//    尽早初始化，让后续步骤的日志能正确输出到文件。
	//    WithConfigFile() 自己读 config.yaml 的 logging 段，不依赖 Config 对象。
	if err := logger.Setup(logger.WithConfigFile()); err != nil {
		return fmt.Errorf("初始化日志系统失败: %w", err)
	}

	// 4. 加载主 .env 文件（等价 Python: load_dotenv(dotenv_path=get_env_file(), override=True)）
	envFile := workspace.EnvFile()
	if _, err := os.Stat(envFile); err == nil {
		if err := dotenv.Load(envFile); err != nil {
			return fmt.Errorf("加载 .env 文件失败: %w", err)
		}
	}

	// 5. 完整配置加载（等价 Python: get_config()）
	cfg, err := config.New("", config.WithNormalize(config.NormalizeConfig))
	if err != nil {
		return fmt.Errorf("创建配置失败: %w", err)
	}
	if _, err := cfg.Load(); err != nil {
		return fmt.Errorf("加载配置文件失败: %w", err)
	}

	// 6. 重置免费搜索运行时标志（等价 Python: reset_free_search_runtime_flags()）
	harness.ResetFreeSearchRuntimeFlags()

	// 创建 ChannelTransport（进程内传输）
	transport := gateway_push.NewChannelTransport()
	pushTransport := transport // ChannelTransport 同时实现 AgentTransport + GatewayPushTransport

	// 创建 GatewayServer
	gs, err := gateway.NewGatewayServer(cfg, transport, pushTransport)
	if err != nil {
		return fmt.Errorf("创建 GatewayServer 失败: %w", err)
	}

	logger.Info(logger.ComponentGateway).
		Str("version", version.Version).
		Msg("uapclaw app 启动中")

	// 启动服务器
	if err := gs.Start(ctx); err != nil {
		return fmt.Errorf("启动 GatewayServer 失败: %w", err)
	}

	// 等待退出信号
	<-ctx.Done()
	logger.Info(logger.ComponentGateway).Msg("收到退出信号，正在关闭...")

	return gs.Stop()
}

// newAgentServerCmd 创建 agentserver 子命令
func newAgentServerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agentserver",
		Short: "仅启动 AgentServer",
		Long: `仅启动 AgentServer（WebSocket 服务端），独立进程部署。
监听 WS 端口，等待外部 Gateway 通过 WebSocketTransport + E2A 协议连入。`,
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("agentserver 模式尚未实现（领域十）")
		},
	}
	cmd.PreRunE = makeDotenvPreRunE()
	return cmd
}

// newGatewayCmd 创建 gateway 子命令
func newGatewayCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "gateway",
		Short: "仅启动 Gateway",
		Long: `仅启动 Gateway（IM 渠道网关），独立进程部署。
通过 WebSocketTransport + E2A 协议连接外部 AgentServer。`,
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("gateway 模式尚未实现（领域十一）")
		},
	}
	cmd.PreRunE = makeDotenvPreRunE()
	return cmd
}

// newWebCmd 创建 web 子命令
func newWebCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "web",
		Short: "启动 Web UI",
		Long: `启动 Web UI 服务，Web 作为 Channel 接入 Gateway。
进程内 Gateway 通过 ChannelTransport 与 AgentServer 通信。`,
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("web 模式尚未实现（领域十）")
		},
	}
	cmd.PreRunE = makeDotenvPreRunE()
	return cmd
}

// newInitCmd 创建 init 子命令
func newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "初始化工作区",
		Long:  `初始化 ~/.uapclaw 工作区，创建默认配置和目录结构。`,
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("init 命令尚未实现（领域一 1.7）")
		},
	}
}

// newAcpCmd 创建 acp 子命令
func newAcpCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "acp",
		Short: "启动 ACP stdio JSON-RPC 协议模式",
		Long: `通过标准输入输出运行 ACP JSON-RPC 协议，stdio 作为 Channel 接入 Gateway。
适用于 IDE 集成等场景。
进程内 Gateway 通过 ChannelTransport 与 AgentServer 通信。`,
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("acp 模式尚未实现（领域十）")
		},
	}
	cmd.PreRunE = makeDotenvPreRunE()
	return cmd
}

// makeDotenvPreRunE 创建 --dotenv/--name 预解析钩子。
//
// 仅在运行服务的子命令（chat/serve/app/agentserver/gateway/web/acp）中使用，
// init 子命令不走此钩子（init 的 --name 语义是"创建实例"，不是"选择实例"）。
//
// 对应 Python: 各 app_*.py 入口中的 parse_dotenv_early() 调用
func makeDotenvPreRunE() func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, args []string) error {
		dotenvPath, _ := cmd.Flags().GetString("dotenv")
		instanceName, _ := cmd.Flags().GetString("name")
		if dotenvPath == "" && instanceName == "" {
			return nil
		}
		_, err := dotenv.ParseEarly(dotenvPath, instanceName)
		return err
	}
}

// newSpawnChildCmd 创建 spawn-child 子命令
func newSpawnChildCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "spawn-child",
		Short:  "作为子进程运行 Agent（内部命令，不应直接调用）",
		Hidden: true,
		RunE:   runSpawnChild,
	}
}

// runSpawnChild 执行 spawn-child 子命令
func runSpawnChild(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	agentConfig := map[string]any{}
	inputs := map[string]any{}
	childRunner := &runner.ChildRunnerImpl{}
	agentCreator := factory.NewDefaultAgentCreator()
	return spawn.RunSpawnedProcess(ctx, agentConfig, inputs, childRunner, agentCreator)
}
