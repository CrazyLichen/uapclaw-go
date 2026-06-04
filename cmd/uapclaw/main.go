// Package main 提供 uapclaw 统一 CLI 入口。
//
// uapclaw 是 Agent 平台的命令行工具，支持以下子命令：
//   - chat: CLI REPL 直接聊天模式
//   - serve: HTTP REST API + SSE 流式服务
//   - app: 完整模式（AgentServer + Gateway）
//   - agentserver: 仅启动 AgentServer
//   - gateway: 仅启动 Gateway
//   - web: 启动 Web UI
//   - init: 初始化工作区
//   - acp: ACP stdio JSON-RPC 协议模式
//
// 所有 CLI 入口都通过 swarm 层调用 agentcore 的 Agent 能力，
// agentcore 本身不可独立运行。
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/uapclaw/uapclaw-go/internal/common/dotenv"
	"github.com/uapclaw/uapclaw-go/internal/common/version"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// Execute 执行根命令
func Execute() {
	if err := newRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newRootCmd 创建根命令
func newRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "uapclaw",
		Short: "UapClaw — Agent 平台命令行工具",
		Long: `UapClaw 是 Agent 平台的统一命令行工具。

所有用户可见的入口（chat/serve/app/acp）都在 swarm 层，
swarm 内部调用 agentcore 的 Agent 能力。

运行模式：
  uapclaw chat  → swarm/chat/repl   → 调用 agentcore
  uapclaw serve → swarm/chat/http_api → 调用 agentcore
  uapclaw app   → swarm/server + swarm/gateway → 调用 agentcore
  uapclaw acp   → swarm/chat/acp_stdio → 调用 agentcore`,
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
	)

	return rootCmd
}

// newChatCmd 创建 chat 子命令
func newChatCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "chat",
		Short: "启动 CLI REPL 直接聊天模式",
		Long: `启动 CLI REPL 交互界面，直接连接 AgentServer。
这是最简模式，无需 Gateway 依赖。
内部调用 swarm/chat/repl，REPL 再调用 agentcore 的 Agent 能力。`,
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
		Long: `启动 HTTP API 服务，提供 RESTful 接口和 SSE 流式响应。
内部调用 swarm/chat/http_api，再调用 agentcore。`,
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
		Long: `同时启动 AgentServer 和 Gateway，支持所有 IM 渠道接入。
内部调用 swarm/server + swarm/gateway，再调用 agentcore。`,
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("app 模式尚未实现（领域十 + 十一）")
		},
	}
	cmd.PreRunE = makeDotenvPreRunE()
	return cmd
}

// newAgentServerCmd 创建 agentserver 子命令
func newAgentServerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agentserver",
		Short: "仅启动 AgentServer",
		Long: `仅启动 AgentServer（WebSocket 服务端），
不启动 Gateway，适用于最简部署场景。`,
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
		Long: `仅启动 Gateway（IM 渠道网关），
需要已有运行中的 AgentServer。`,
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
		Long:  `启动 Web UI 服务，提供浏览器端交互界面。`,
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
		Long: `通过标准输入输出运行 ACP JSON-RPC 协议，
适用于 IDE 集成等场景。
内部调用 swarm/chat/acp_stdio，再调用 agentcore。`,
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("acp 模式尚未实现（领域十）")
		},
	}
	cmd.PreRunE = makeDotenvPreRunE()
	return cmd
}

func main() {
	Execute()
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
