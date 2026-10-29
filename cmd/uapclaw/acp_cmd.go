package main

// acp_cmd.go 提供 uapclaw acp 子命令（11.25 Task 10）。
// Python: acp_connect.py main() + _run()（L1764-1807）——stdout→stderr 重定向 +
// --gateway-url 参数四级回落 + AcpChannel(stdio) → Gateway → AgentServer 链路。
// 双模式（spec §4.5 D2/D3）：--gateway-url 空 → 进程内自举（模式一）；
// 非空 → 跳过自举直连远端（模式二，对齐 Python jiuwenswarm-acp 1:1）。

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/uapclaw/uapclaw-go/internal/common/config"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/version"
	"github.com/uapclaw/uapclaw-go/internal/swarm/extensions"
	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway"
	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway/channel_manager/protocol/acp"
	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway/routing"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server"
	"github.com/uapclaw/uapclaw-go/internal/swarm/transport"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// readStdinForTest stdin 读取注入口（默认 os.Stdin；测试替换为立即 EOF reader）。
	readStdinForTest = func() (io.Reader, error) { return os.Stdin, nil }
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// resolveAcpGatewayURL ACP 网关地址四级回落。
// Python: main() L1800-1805：
// gateway_url or agent_server_url or GATEWAY_URL or ws://{GATEWAY_HOST}:{GATEWAY_PORT}/acp。
// env 名字面保留（第三方 env 不改名，品牌映射决策）。
func resolveAcpGatewayURL(gatewayURL, agentServerURL string, getenv func(string) string) string {
	if gatewayURL != "" {
		return gatewayURL
	}
	if agentServerURL != "" {
		return agentServerURL
	}
	if getenv != nil {
		if v := getenv("GATEWAY_URL"); v != "" {
			return v
		}
		host := getenv("GATEWAY_HOST")
		if host == "" {
			host = "127.0.0.1"
		}
		port := getenv("GATEWAY_PORT")
		if port == "" {
			port = "19001"
		}
		return fmt.Sprintf("ws://%s:%s/acp", host, port)
	}
	return "ws://127.0.0.1:19001/acp"
}

// runAcpCmd acp 子命令执行体（D2/D3 双模式）。
// Python: main() + _run()（acp_connect.py L1764-1807）。
func runAcpCmd(cmd *cobra.Command, _ []string) error {
	ctx, cancel := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// stdout 重定向 stderr（Python sys.stdout = sys.stderr L1776）——须在
	// logger.Setup 之前，协议帧留在 acpStdout 早捕获句柄（§9-8）。
	// 杂散日志走 stderr，协议帧走包级早捕获的原始 stdout。
	os.Stdout = os.Stderr

	gatewayURL, _ := cmd.Flags().GetString("gateway-url")
	agentServerURL, _ := cmd.Flags().GetString("agent-server-url")

	// 共享自举（workspace/logger/dotenv/crypto/config/extensions，一次调用）
	cfg, extManager, err := bootstrapGatewayEnv(ctx)
	if err != nil {
		return err
	}
	if extManager != nil {
		defer func() {
			if err := extManager.ShutdownAllExtensions(context.Background()); err != nil {
				logger.Warn(logComponentGateway).Err(err).Msg("failed to shutdown extensions")
			}
		}()
	}

	var agentServer *server.AgentServer
	var gs *gateway.AppGatewayServer
	if gatewayURL == "" {
		// 模式一：进程内自举（复用 runAppCmd 服务器段，D3）
		agentServer, gs, err = bootstrapInprocServers(ctx, cfg)
		if err != nil {
			// 19001 被占等启动失败：引导 --gateway-url（偏差 §9-6）
			return fmt.Errorf(
				"自举进程内 GatewayServer 失败: %w（若已有运行中的 Gateway 实例，可改用 --gateway-url 指向其 WS 地址）", err)
		}
		gatewayURL = resolveAcpGatewayURL("", agentServerURL, os.Getenv)
	} else {
		// 模式二：远端桥进程（Python jiuwenswarm-acp 1:1）
		gatewayURL = resolveAcpGatewayURL(gatewayURL, agentServerURL, os.Getenv)
	}

	logger.Info(logComponentGateway).
		Str("gateway_url", gatewayURL).
		Msg("[ACP] started: AcpChannel(stdio) -> Gateway -> AgentServer")

	channel := acp.NewAcpChannel(acp.LoadAcpChannelConfig(func() map[string]any {
		raw, rerr := cfg.Raw()
		if rerr != nil {
			return nil
		}
		return raw
	}))
	channel.SetGatewayURL(gatewayURL)
	// stdin 注入（测试可替换为立即 EOF reader；生产 = os.Stdin）
	if stdinR, serr := readStdinForTest(); serr == nil {
		channel.SetStdinForTest(stdinR)
	}
	defer channel.Stop()

	// 阻塞读 stdin（EOF 5s 宽限后自然退出）
	if err := channel.Start(ctx); err != nil {
		return err
	}

	// 收尾：反向停止（AcpChannel defer 已停；这里停服务端）
	if agentServer != nil {
		_ = agentServer.Stop()
	}
	if gs != nil {
		_ = gs.Stop()
	}
	return nil
}

// bootstrapInprocServers 进程内 AgentServer + AppGatewayServer 自举（模式一）。
// 11.25 提取自 runAppCmd 服务器段（transport 选择→AgentClient→AgentServer→
// AppGatewayServer→双 Start），行为零变更（R6）。
func bootstrapInprocServers(ctx context.Context, cfg *config.Config) (*server.AgentServer, *gateway.AppGatewayServer, error) {
	// 创建 transport：扩展 transport 优先（对齐 Python app_gateway.py L828-833），
	// 无扩展时回落进程内 ChannelTransport
	extRegistry, _ := extensions.GetInstanceErr()
	var chTransport *transport.ChannelTransport
	var agentTransport transport.AgentTransport
	if extRegistry != nil {
		if t := extRegistry.GetAgentServerClient(); t != nil {
			agentTransport = t
			logger.Info(logComponentGateway).
				Msg("using extension AgentServerClient transport")
		}
	}
	if agentTransport == nil {
		chTransport = transport.NewChannelTransport()
		agentTransport = chTransport
	}

	// 创建 AgentClient（对齐 Python WebSocketAgentServerClient）
	agentClient := routing.NewAgentClient(agentTransport)

	// 创建 AgentServer（仅进程内 transport 时；扩展 transport = 远端场景，本地不启动）
	var agentServer *server.AgentServer
	if chTransport != nil {
		agentServer = server.NewAgentServer(cfg, chTransport)
	}

	// 创建 AppGatewayServer（AgentClient 会等待 AgentServer 就绪后发送 connection.ack）
	gs, err := gateway.NewAppGatewayServer(cfg, agentClient)
	if err != nil {
		logger.Error(logComponentGateway).Err(err).Msg("failed to create AppGatewayServer")
		return nil, nil, fmt.Errorf("创建 AppGatewayServer 失败: %w", err)
	}

	logger.Info(logComponentGateway).
		Str("version", version.Version).
		Msg("uapclaw acp bootstrapping in-process servers")

	// 启动 AgentServer（非阻塞，内部起 goroutine 运行主循环；扩展 transport 时为 nil 不启动）
	if agentServer != nil {
		if err := agentServer.Start(ctx); err != nil {
			logger.Error(logComponentAgentServer).Err(err).Msg("failed to start AgentServer")
			return nil, nil, fmt.Errorf("启动 AgentServer 失败: %w", err)
		}
	}

	// 启动 AppGatewayServer（HTTP + WebSocket；/acp 路由在 Start 内装配，11.25 Task 7）
	if err := gs.Start(ctx); err != nil {
		logger.Error(logComponentGateway).Err(err).Msg("failed to start AppGatewayServer")
		return nil, nil, fmt.Errorf("启动 AppGatewayServer 失败: %w", err)
	}
	return agentServer, gs, nil
}
