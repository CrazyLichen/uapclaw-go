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
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/gateway_push"
)

// ──────────────────────────── 结构体 ────────────────────────────

// GatewayServer Gateway 服务器，整合 HTTP 路由、WebChannel、ChannelManager、MessageHandler。
//
// 提供 WebSocket RPC 端点、静态文件服务和文件操作 HTTP API，
// 通过 Transport 抽象与 AgentServer 通信。
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
	// transport Agent 传输层
	transport gateway_push.AgentTransport
	// httpServer HTTP 服务器
	httpServer *http.Server
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
// 初始化 WebChannel、ChannelManager、MessageHandler，组装 chi 路由。
func NewGatewayServer(cfg *config.Config, transport gateway_push.AgentTransport, pushTransport gateway_push.GatewayPushTransport) (*GatewayServer, error) {
	// 从配置读取 Web 通道参数
	webCfg := web.WebChannelConfig{
		Enabled: true,
		Host:    defaultHost(cfg),
		Port:    defaultPort(cfg),
		Path:    "/ws",
	}

	// 创建 ChannelManager
	channelMgr := cm.NewChannelManager(nil, nil)

	// 创建 MessageHandler
	msgHandler := mh.NewMessageHandler(transport, pushTransport, channelMgr)

	// 创建 onMessage 回调：WebChannel → MessageHandler.HandleInbound
	onMessageCb := func(msg *schema.Message) {
		if err := msgHandler.HandleInbound(context.Background(), msg); err != nil {
			logger.Warn(logComponentAppGateway).
				Err(err).
				Str("msg_id", msg.ID).
				Msg("HandleInbound 失败")
		}
	}

	wc := web.NewWebChannel(webCfg, onMessageCb)

	s := &GatewayServer{
		config:     cfg,
		webChannel: wc,
		channelMgr: channelMgr,
		msgHandler: msgHandler,
		transport:  transport,
	}

	// 组装路由
	s.setupRouter()

	return s, nil
}

// Start 启动 Gateway 服务器。
//
// 注册 WebChannel、启动 MessageHandler 转发循环、启动 HTTP 服务器。
func (s *GatewayServer) Start(ctx context.Context) error {
	// 注册并启动 WebChannel（OnMessage 回调已通过 NewWebChannel 注入）
	s.channelMgr.Register(s.webChannel, nil)

	if err := s.webChannel.Start(ctx); err != nil {
		return err
	}

	// 启动 MessageHandler 转发循环
	if s.msgHandler != nil {
		if err := s.msgHandler.StartForwarding(ctx); err != nil {
			logger.Error(logComponentAppGateway).
				Err(err).
				Msg("启动 MessageHandler 转发循环失败")
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

	// 停止 MessageHandler
	if s.msgHandler != nil {
		if err := s.msgHandler.StopForwarding(); err != nil {
			logger.Error(logComponentAppGateway).
				Err(err).
				Msg("停止 MessageHandler 失败")
		}
	}

	// 关闭 Transport
	if s.transport != nil {
		if err := s.transport.Close(); err != nil {
			logger.Error(logComponentAppGateway).
				Err(err).
				Msg("关闭 Transport 失败")
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
