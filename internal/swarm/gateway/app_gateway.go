package gateway

import (
	"context"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/uapclaw/uapclaw-go/internal/common/config"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	cm "github.com/uapclaw/uapclaw-go/internal/swarm/gateway/channel_manager"
	web "github.com/uapclaw/uapclaw-go/internal/swarm/gateway/channel_manager/web"
	mh "github.com/uapclaw/uapclaw-go/internal/swarm/gateway/message_handler"
	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway/routing"
)

// ──────────────────────────── 结构体 ────────────────────────────

// GatewayServer Gateway 服务器，整合 HTTP 路由、WebChannel、ChannelManager、MessageHandler。
//
// 提供 WebSocket RPC 端点、静态文件服务和文件操作 HTTP API，
// 通过 AgentClient 与 AgentServer 通信。
type GatewayServer struct {
	// config 配置管理器
	config *config.Config
	// router chi 路由器
	router *chi.Mux
	// webChannel Web 通道
	webChannel *web.WebChannel
	// channelMgr 渠道管理器
	channelMgr *cm.ChannelManager
	// msgHandler 消息处理器
	msgHandler *mh.MessageHandler
	// agentClient AgentServer 客户端
	agentClient *routing.AgentClient
	// httpServer HTTP 服务器
	httpServer *http.Server
	// reloader 配置热重载监听器
	reloader *config.Reloader
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// logComponentAppGateway 本文件日志组件
const logComponentAppGateway = logger.ComponentGateway

// shutdownTimeout 优雅关闭超时时间
const shutdownTimeout = 5 * time.Second

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewGatewayServer 创建 Gateway 服务器实例。
//
// 初始化 MessageHandler、ChannelManager、WebChannel，组装 chi 路由。
// 创建顺序对齐 Python app_gateway.py：
//  1. 创建消息处理器
//  2. 创建频道管理器
//  3. 创建 Web 频道
//  4. 注册频道入站回调
func NewGatewayServer(cfg *config.Config, agentClient *routing.AgentClient) (*GatewayServer, error) {
	// 从配置读取 Web 通道参数
	webCfg := web.WebChannelConfig{
		Enabled: true,
		Host:    defaultHost(cfg),
		Port:    defaultPort(cfg),
		Path:    "/ws",
	}

	// 步骤1：创建 MessageHandler（1 参数，对齐 Python MessageHandler(agent_client)）
	msgHandler := mh.NewMessageHandler(agentClient)

	// 步骤2：创建 ChannelManager（3 参数，对齐 Python ChannelManager(message_handler, config, on_config_updated)）
	channelMgr := cm.NewChannelManager(msgHandler, nil, nil)

	// 先创建 GatewayServer（onConfigSavedImpl 需要 agentClient 和 config）
	s := &GatewayServer{
		config:      cfg,
		channelMgr:  channelMgr,
		msgHandler:  msgHandler,
		agentClient: agentClient,
	}

	// 创建配置热重载器（对齐 Python 无 fsnotify，Go 补充）
	// 监听 config.yaml 变更，触发 onConfigSavedImpl
	if cfg != nil {
		if reloader, err := config.NewReloader(cfg); err != nil {
			logger.Warn(logComponentAppGateway).
				Err(err).
				Msg("创建配置热重载器失败，fsnotify 触发路径不可用")
		} else {
			s.reloader = reloader
		}
	}

	// 步骤3：创建 WebChannel（对齐 Python WebChannel(config, on_config_saved)）
	// onMessage 通过 RegisterChannelWithInbound 设置，构造时不注入
	onConfigSavedCb := s.OnConfigSaved()
	wc := web.NewWebChannel(webCfg, channelMgr, onConfigSavedCb)

	s.webChannel = wc

	// 步骤4：注册 WebChannel（对齐 Python register_channel_with_inbound）
	// web_norm_and_forward 回调：三层路由逻辑，对齐 Python _make_norm_and_forward
	webNormAndForward := web.MakeNormAndForward(
		channelMgr,
		web.ForwardReqMethods,
		web.ForwardNoLocalHandlerMethods,
	)
	channelMgr.RegisterChannelWithInbound(wc, webNormAndForward)

	// 步骤5：注册 Web RPC handlers + onConnect 钩子（对齐 Python _register_web_handlers(bind)）
	// AgentClient 通过 BindParams 闭包捕获，不直接设到 WebChannel
	web.RegisterWebHandlers(&web.WebHandlersBindParams{
		Channel:        wc,
		AgentClient:    agentClient,
		ChannelManager: channelMgr,
		OnConfigSaved:  onConfigSavedCb,
	})

	// 组装路由
	s.setupRouter()

	return s, nil
}

// Start 启动 Gateway 服务器。
//
// 注册 WebChannel、启动 MessageHandler 转发循环、启动 HTTP 服务器。
func (s *GatewayServer) Start(ctx context.Context) error {
	// TODO(⤵️ 领域11): 初始化 IMInboundPipeline + IMOutboundPipeline，注册到 MessageHandler
	// 对齐 Python: app_gateway.py L851-856

	// TODO(⤵️ Cron): 初始化 CronJobStore + CronSchedulerService + CronController，启动 cron_scheduler
	// 对齐 Python: app_gateway.py L858-865, L1615

	// TODO(⤵️ Heartbeat): 初始化 GatewayHeartbeatService（配置 interval/timeout/active_hours），启动
	// 对齐 Python: app_gateway.py L884-913

	// WebChannel 已通过 RegisterChannelWithInbound 注册（对齐 Python register_channel_with_inbound）

	if err := s.webChannel.Start(ctx); err != nil {
		return err
	}

	// 启动 AgentClient 接收循环（对齐 Python connect 启动 _message_receiver_loop）
	// 使用 connectWithRetry 带重试连接，对齐 Python _connect_with_retry
	if s.agentClient != nil {
		maxRetries := 20
		if v := os.Getenv("AGENT_CONNECT_RETRY"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				maxRetries = n
			}
		}
		retryInterval := 3 * time.Second
		if v := os.Getenv("AGENT_CONNECT_RETRY_INTERVAL"); v != "" {
			if d, err := strconv.ParseFloat(v, 64); err == nil && d > 0 {
				retryInterval = time.Duration(d * float64(time.Second))
			}
		}
		if err := connectWithRetry(ctx, s.agentClient, maxRetries, retryInterval); err != nil {
			logger.Error(logComponentAppGateway).
				Err(err).
				Msg("连接 AgentServer 失败")
		}
	}

	// 启动 MessageHandler 转发循环
	if s.msgHandler != nil {
		if err := s.msgHandler.StartForwarding(ctx); err != nil {
			logger.Error(logComponentAppGateway).
				Err(err).
				Msg("启动 MessageHandler 转发循环失败")
		}
	}

	// 启动出站派发循环（对齐 Python channel_manager.start_dispatch()）
	if s.channelMgr != nil {
		if err := s.channelMgr.StartDispatch(ctx); err != nil {
			logger.Error(logComponentAppGateway).
				Err(err).
				Msg("启动 ChannelManager 出站派发循环失败")
		}
	}

	// 启动配置热重载监听（对齐 Python 无 fsnotify，Go 补充）
	if s.reloader != nil {
		s.reloader.OnReload(func(data map[string]any) {
			// fsnotify 检测到 config.yaml 变更，用最新配置快照调用 onConfigSaved
			configData, _ := s.config.Raw()
			if configData == nil {
				configData = make(map[string]any)
			}
			_ = s.onConfigSavedImpl(nil, BuildEnvMap(), configData)
		})
		if err := s.reloader.Start(); err != nil {
			logger.Warn(logComponentAppGateway).
				Err(err).
				Msg("启动配置热重载监听失败")
		} else {
			logger.Info(logComponentAppGateway).Msg("配置热重载监听已启动")
		}
	}

	// 启动 HTTP 服务器
	addr := s.webChannel.Addr()
	s.httpServer = &http.Server{
		Addr:    addr,
		Handler: s.router,
	}

	logger.Info(logComponentAppGateway).
		Str("addr", addr).
		Msg("GatewayServer HTTP 服务启动中")

	// 在 goroutine 中启动 HTTP 服务
	errCh := make(chan error, 1)
	go func() {
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
		close(errCh)
	}()

	// 等待启动结果或上下文取消
	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(100 * time.Millisecond):
		// HTTP 服务器已启动（无立即错误）
		logger.Info(logComponentAppGateway).
			Str("addr", addr).
			Msg("GatewayServer 已启动")
		return nil
	}
}

// Stop 优雅关闭 Gateway 服务器。
//
// 停止顺序：WebChannel → MessageHandler → Transport → HTTP Server。
func (s *GatewayServer) Stop() error {
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	// 关闭 WebChannel
	if err := s.webChannel.Stop(ctx); err != nil {
		logger.Error(logComponentAppGateway).
			Err(err).
			Msg("关闭 WebChannel 失败")
	}

	// 停止出站派发循环（对齐 Python channel_manager.stop_dispatch()）
	if s.channelMgr != nil {
		if err := s.channelMgr.StopDispatch(); err != nil {
			logger.Warn(logComponentAppGateway).
				Err(err).
				Msg("停止 ChannelManager 出站派发循环失败")
		}
	}

	// 停止 MessageHandler
	if s.msgHandler != nil {
		if err := s.msgHandler.StopForwarding(); err != nil {
			logger.Error(logComponentAppGateway).
				Err(err).
				Msg("停止 MessageHandler 失败")
		}
	}

	// 断开 AgentClient
	if s.agentClient != nil {
		s.agentClient.Disconnect()
	}

	// 停止配置热重载监听
	if s.reloader != nil {
		if err := s.reloader.Stop(); err != nil {
			logger.Warn(logComponentAppGateway).
				Err(err).
				Msg("停止配置热重载监听失败")
		}
	}

	// 关闭 HTTP 服务器
	if s.httpServer != nil {
		if err := s.httpServer.Shutdown(ctx); err != nil {
			logger.Error(logComponentAppGateway).
				Err(err).
				Msg("关闭 HTTP 服务器失败")
			return err
		}
	}

	logger.Info(logComponentAppGateway).Msg("GatewayServer 已停止")
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// setupRouter 组装 chi 路由。
//
// 路由布局：
//   - GET /ws → WebSocket 端点
//   - /file-api/* → 文件操作 API
//   - /* → 前端静态文件（SPA fallback）
func (s *GatewayServer) setupRouter() {
	r := chi.NewRouter()

	// 中间件
	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)
	r.Use(zerologMiddleware)

	// WebSocket 路由
	r.Get("/ws", s.webChannel.HandleWebSocket)

	// 文件 API 路由
	r.Route("/file-api", func(r chi.Router) {
		r.Get("/file-content", HandleFileContentGet)
		r.Post("/file-content", HandleFileContentPost)
		r.Get("/list-files", HandleListFiles)
		r.Get("/list-markdown", HandleListMarkdown)
		r.Get("/ws-debug-config", HandleWsDebugConfigGet)
		r.Post("/ws-debug-config", HandleWsDebugConfigPost)
		r.Post("/rebuild-agent-data", HandleRebuildAgentData)
	})

	// 静态文件（SPA fallback）
	spaHandler := newSPAHandler(frontendDist)
	r.HandleFunc("/*", spaHandler.ServeHTTP)

	s.router = r
}

// newSPAHandler 创建 SPA 静态文件处理器。
//
// 从嵌入的 FS 中读取文件，未匹配时回退到 index.html。
func newSPAHandler(efs fs.FS) http.Handler {
	// 去掉 "dist" 前缀目录
	sub, err := fs.Sub(efs, "channel_manager/web/frontend/dist")
	if err != nil {
		log.Fatalf("创建前端 FS 子目录失败: %v", err)
	}
	fileServer := http.FileServer(http.FS(sub))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		// 尝试读取文件
		if path != "/" && !strings.HasPrefix(path, "/assets/") {
			// 检查文件是否存在
			filePath := strings.TrimPrefix(path, "/")
			if filePath == "" {
				filePath = "index.html"
			}
			if _, err := fs.Stat(sub, filePath); err != nil {
				// 文件不存在，回退到 index.html（SPA 路由）
				r.URL.Path = "/"
			}
		}

		fileServer.ServeHTTP(w, r)
	})
}

// zerologMiddleware zerolog 请求日志中间件。
func zerologMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

		next.ServeHTTP(ww, r)

		duration := time.Since(start)
		logger.Info(logComponentAppGateway).
			Str("method", r.Method).
			Str("path", r.URL.Path).
			Int("status", ww.Status()).
			Dur("duration", duration).
			Msg("HTTP 请求")
	})
}

// connectWithRetry 带重试的连接逻辑，对齐 Python _connect_with_retry。
//
// 循环 maxRetries 次，每次调用 client.Connect(ctx)：
//   - 成功则 return nil
//   - 失败则 Warn 日志 + time.Sleep(retryInterval)
//   - 超过最大次数则 Error 日志 + return 最后一次 error
func connectWithRetry(ctx context.Context, client *routing.AgentClient, maxRetries int, retryInterval time.Duration) error {
	var lastErr error
	for attempt := 1; attempt <= maxRetries; attempt++ {
		if err := client.Connect(ctx); err != nil {
			lastErr = err
			if attempt >= maxRetries {
				logger.Error(logComponentAppGateway).
					Err(err).
					Int("attempt", attempt).
					Int("max_retries", maxRetries).
					Msg("连接 AgentServer 失败，已达最大重试次数")
				return lastErr
			}
			logger.Warn(logComponentAppGateway).
				Err(err).
				Int("attempt", attempt).
				Int("max_retries", maxRetries).
				Dur("retry_interval", retryInterval).
				Msg("连接 AgentServer 失败，即将重试")
			time.Sleep(retryInterval)
			continue
		}
		logger.Info(logComponentAppGateway).
			Int("attempt", attempt).
			Msg("已连接 AgentServer")
		return nil
	}
	return lastErr
}

// defaultHost 从配置获取默认监听地址。
// 优先级：环境变量 UAPCLAW_GATEWAY_HOST > config.yaml gateway.host > 默认 127.0.0.1
func defaultHost(cfg *config.Config) string {
	if v := os.Getenv("UAPCLAW_GATEWAY_HOST"); v != "" {
		return v
	}
	if cfg != nil {
		if v := cfg.Get("gateway.host"); v != nil {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	return "127.0.0.1"
}

// defaultPort 从配置获取默认监听端口。
// 优先级：环境变量 UAPCLAW_GATEWAY_PORT > config.yaml gateway.port > 默认 19000
func defaultPort(cfg *config.Config) int {
	if v := os.Getenv("UAPCLAW_GATEWAY_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil && p > 0 {
			return p
		}
	}
	if cfg != nil {
		if v := cfg.Get("gateway.port"); v != nil {
			switch p := v.(type) {
			case int:
				if p > 0 {
					return p
				}
			case float64:
				if p > 0 {
					return int(p)
				}
			}
		}
	}
	return 19000
}
