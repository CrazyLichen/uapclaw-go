package server

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/common/config"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
	"github.com/uapclaw/uapclaw-go/internal/swarm/e2a"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime"
	"github.com/uapclaw/uapclaw-go/internal/swarm/transport"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AgentServer Agent 核心服务，对齐 Python AgentWebSocketServer，通过 AgentTransport 接口通信。
//
// 从 transport.Recv() 消费 E2AEnvelope，每个请求独立 goroutine 处理。
// 流式任务通过 sessionStreamTasks 追踪，支持按会话取消。
// 支持 ChannelTransport（进程内）和将来的 WebSocketTransport（跨进程）。
//
// 对齐 Python: jiuwenswarm/server/agent_server.py (AgentWebSocketServer)
type AgentServer struct {
	// config 配置实例
	config *config.Config
	// transport 传输通道（AgentTransport 接口，支持 ChannelTransport 和将来的 WebSocketTransport）
	transport transport.AgentTransport
	// agentManager Agent 实例管理器
	agentManager *runtime.AgentManager
	// sessionStreamTasks 流式任务的取消函数映射（sessionID → CancelFunc）
	sessionStreamTasks map[string]context.CancelFunc
	// sessionStreamTasksMu 保护 sessionStreamTasks 的读写锁
	sessionStreamTasksMu sync.RWMutex
	// sessionsDir Agent 会话目录路径（默认从 workspace 获取，测试时可注入）
	sessionsDir string
	// running 是否正在运行
	running bool
	// runningMu 保护 running 的读写锁
	runningMu sync.RWMutex
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// logComponent 日志组件
const logComponent = logger.ComponentAgentServer

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewAgentServer 创建 AgentServer 实例。
func NewAgentServer(cfg *config.Config, transport transport.AgentTransport) *AgentServer {
	return &AgentServer{
		config:             cfg,
		transport:          transport,
		sessionStreamTasks: make(map[string]context.CancelFunc),
		sessionsDir:        workspace.AgentSessionsDir(),
	}
}

// SetSessionsDir 设置会话目录路径（供测试注入）。
func (s *AgentServer) SetSessionsDir(dir string) {
	s.sessionsDir = dir
}

// SessionsDir 返回会话目录路径。
func (s *AgentServer) SessionsDir() string {
	return s.sessionsDir
}

// Start 启动 AgentServer：初始化 AgentManager → 发送 connection.ack → 进入消费循环 → 阻塞直到 ctx 取消。
func (s *AgentServer) Start(ctx context.Context) error {
	s.runningMu.Lock()
	if s.running {
		s.runningMu.Unlock()
		logger.Warn(logComponent).Msg("AgentServer 已在运行中，跳过重复启动")
		return nil
	}
	s.running = true
	s.runningMu.Unlock()

	// 初始化 AgentManager
	s.agentManager = runtime.NewAgentManager()
	logger.Info(logComponent).Msg("AgentManager 已初始化")

	// 发送 connection.ack 事件帧（对齐 Python AgentWebSocketServer._connection_handler 首帧）
	ackFrame := transport.BuildConnectionAckFrame()
	ackData, err := json.Marshal(ackFrame)
	if err != nil {
		logger.Error(logComponent).Err(err).Msg("编码 connection.ack 失败")
	} else {
		s.sendToGateway(ackData)
		logger.Info(logComponent).Msg("AgentServer 已就绪（connection.ack 已发送）")
	}

	// 进入消费循环（阻塞直到 ctx 取消）
	s.startConsumeLoop(ctx)

	return nil
}

// Stop 停止 AgentServer：取消所有流式任务 → 清理 AgentManager → 标记停止。
func (s *AgentServer) Stop() error {
	s.cancelAllStreamTasks()
	logger.Info(logComponent).Msg("所有流式任务已取消")

	if s.agentManager != nil {
		if err := s.agentManager.Cleanup(); err != nil {
			logger.Error(logComponent).Err(err).Msg("AgentManager 清理失败")
			return err
		}
		logger.Info(logComponent).Msg("AgentManager 已清理")
	}

	s.runningMu.Lock()
	s.running = false
	s.runningMu.Unlock()
	logger.Info(logComponent).Msg("AgentServer 已停止")

	return nil
}

// AgentManager 返回 AgentManager 实例，供 handler 使用。
func (s *AgentServer) AgentManager() *runtime.AgentManager {
	return s.agentManager
}

// Transport 返回传输通道实例，供 handler 写入响应。
func (s *AgentServer) Transport() transport.AgentTransport {
	return s.transport
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// startConsumeLoop 从传输通道持续读取 JSON 字节并反序列化为 E2AEnvelope 分发处理。
// 阻塞直到 ctx 取消或通道关闭。
// ChannelTransport 模式下通过 SendCh() 读取（Gateway→AgentServer 方向），
// 将来 WebSocketTransport 模式下通过 Recv() 读取。
func (s *AgentServer) startConsumeLoop(ctx context.Context) {
	var recvCh <-chan []byte
	if ct, ok := s.transport.(*transport.ChannelTransport); ok {
		recvCh = ct.SendCh()
	} else {
		var err error
		recvCh, err = s.transport.Recv()
		if err != nil {
			logger.Error(logComponent).Err(err).Msg("获取接收通道失败")
			return
		}
	}
	for {
		select {
		case <-ctx.Done():
			logger.Info(logComponent).Msg("AgentServer 消费循环退出（上下文取消）")
			return
		case data, ok := <-recvCh:
			if !ok {
				logger.Info(logComponent).Msg("AgentServer 消费循环退出（通道已关闭）")
				return
			}
			// JSON 字节 → map → E2AEnvelope（对齐 Python json.loads → E2AEnvelope.from_dict）
			var m map[string]any
			if err := json.Unmarshal(data, &m); err != nil {
				logger.Warn(logComponent).Err(err).Msg("消费循环：请求 JSON 解码失败")
				continue
			}
			envelope := e2a.EnvelopeFromMap(m)
			if envelope == nil {
				logger.Warn(logComponent).Msg("消费循环：E2AEnvelope 反序列化失败")
				continue
			}
			logger.Debug(logComponent).
				Str("request_id", envelope.RequestID).
				Str("method", envelope.Method).
				Msg("收到 E2A 请求信封")
			go s.handleEnvelope(ctx, envelope)
		}
	}
}

// registerStreamTask 注册流式任务的取消函数。
func (s *AgentServer) registerStreamTask(sessionID string, cancel context.CancelFunc) {
	s.sessionStreamTasksMu.Lock()
	defer s.sessionStreamTasksMu.Unlock()
	s.sessionStreamTasks[sessionID] = cancel
	logger.Debug(logComponent).
		Str("session_id", sessionID).
		Msg("流式任务已注册")
}

// cancelStreamTask 取消并移除指定会话的流式任务。
func (s *AgentServer) cancelStreamTask(sessionID string) {
	s.sessionStreamTasksMu.Lock()
	defer s.sessionStreamTasksMu.Unlock()
	if cancel, ok := s.sessionStreamTasks[sessionID]; ok {
		cancel()
		delete(s.sessionStreamTasks, sessionID)
		logger.Debug(logComponent).
			Str("session_id", sessionID).
			Msg("流式任务已取消")
	}
}

// cancelAllStreamTasks 取消所有流式任务并清空映射。
func (s *AgentServer) cancelAllStreamTasks() {
	s.sessionStreamTasksMu.Lock()
	defer s.sessionStreamTasksMu.Unlock()
	for sessionID, cancel := range s.sessionStreamTasks {
		cancel()
		logger.Debug(logComponent).
			Str("session_id", sessionID).
			Msg("流式任务已取消")
	}
	s.sessionStreamTasks = make(map[string]context.CancelFunc)
}

// sendToGateway 通过传输通道发送数据到 Gateway 侧。
// 对齐 Python AgentWebSocketServer 中 ws.send(json_str) 写响应。
// ChannelTransport 模式下通过 RecvCh() 写入（AgentServer→Gateway 方向），
// 将来 WebSocketTransport 模式下直接调用 Send()。
func (s *AgentServer) sendToGateway(data []byte) {
	if ct, ok := s.transport.(*transport.ChannelTransport); ok {
		select {
		case ct.RecvCh() <- data:
		default:
			logger.Warn(logComponent).Msg("RecvCh 已满，丢弃数据")
		}
		return
	}
	// 将来 WebSocketTransport 走 Send 路径
	if err := s.transport.Send(context.Background(), data); err != nil {
		logger.Warn(logComponent).Err(err).Msg("发送到 Gateway 失败")
	}
}
