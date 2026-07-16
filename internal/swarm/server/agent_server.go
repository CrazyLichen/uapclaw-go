package server

import (
	"context"
	"encoding/json"
	"strings"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/common/config"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
	"github.com/uapclaw/uapclaw-go/internal/swarm/e2a"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/adapter"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime"
	"github.com/uapclaw/uapclaw-go/internal/swarm/transport"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AgentServer Agent 核心服务，对齐 Python AgentWebSocketServer，通过 AgentTransport 接口通信。
//
// Start() 非阻塞（内部起 goroutine 运行主循环），对齐 Python server.start() 风格。
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
	// cancel 停止 AgentServer 的取消函数
	cancel context.CancelFunc
	// stopCh run() 退出时关闭的信号通道
	stopCh chan struct{}
	// runErr run() 的返回错误
	runErr error
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// logComponent 日志组件
const logComponent = logger.ComponentAgentServer

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// agentServerInstance AgentServer 单例实例
	agentServerInstance *AgentServer
	// agentServerOnce 保证单例只设置一次
	agentServerOnce sync.Once
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewAgentServer 创建 AgentServer 实例。
func NewAgentServer(cfg *config.Config, transport transport.AgentTransport) *AgentServer {
	return &AgentServer{
		config:             cfg,
		transport:          transport,
		sessionStreamTasks: make(map[string]context.CancelFunc),
		sessionsDir:        workspace.AgentSessionsDir(),
		stopCh:             make(chan struct{}),
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

// Start 启动 AgentServer（非阻塞，内部起 goroutine 运行主循环）。
// 对齐 Python: AgentWebSocketServer.start() 风格——调用方无需 go 包一层。
func (s *AgentServer) Start(ctx context.Context) error {
	s.runningMu.Lock()
	if s.running {
		s.runningMu.Unlock()
		logger.Warn(logComponent).Msg("AgentServer 已在运行中，跳过重复启动")
		return nil
	}
	s.running = true
	s.runningMu.Unlock()

	ctx, s.cancel = context.WithCancel(ctx)
	go func() {
		err := s.run(ctx)
		s.runningMu.Lock()
		s.runErr = err
		s.running = false
		s.runningMu.Unlock()
	}()

	return nil
}

// Stop 停止 AgentServer：取消所有任务 → 清理 AgentManager → 等待主循环退出。
func (s *AgentServer) Stop() error {
	// 1. 取消所有流式任务
	s.cancelAllStreamTasks()
	logger.Info(logComponent).Msg("所有流式任务已取消")

	// 2. 取消所有进行中的任务（对齐 Python agent_ws_server.py L726）
	s.cancelAllInflightWork()

	// 3. 停止调度器（对齐 Python agent_ws_server.py L732）
	s.stopScheduler()

	// 4. 取消所有 team 流式任务（对齐 Python agent_ws_server.py L737）
	s.cancelAllTeamStreamTasks()

	// 5. 清理 AgentManager
	if s.agentManager != nil {
		if err := s.agentManager.Cleanup(); err != nil {
			logger.Error(logComponent).Err(err).Msg("AgentManager 清理失败")
		} else {
			logger.Info(logComponent).Msg("AgentManager 已清理")
		}
	}

	// 6. 取消主循环
	if s.cancel != nil {
		s.cancel()
	}

	// 7. 等待主循环退出
	<-s.stopCh

	logger.Info(logComponent).Msg("AgentServer 已停止")
	s.runningMu.RLock()
	err := s.runErr
	s.runningMu.RUnlock()
	return err
}

// AgentManager 返回 AgentManager 实例，供 handler 使用。
func (s *AgentServer) AgentManager() *runtime.AgentManager {
	return s.agentManager
}

// Transport 返回传输通道实例，供 handler 写入响应。
func (s *AgentServer) Transport() transport.AgentTransport {
	return s.transport
}

// GetInstance 返回 AgentServer 单例实例。
// 对齐 Python: AgentWebSocketServer.get_instance()
func GetInstance() *AgentServer { return agentServerInstance }

// ResetInstance 重置单例（仅用于测试）。
// 对齐 Python: AgentWebSocketServer.reset_instance()
func ResetInstance() {
	agentServerInstance = nil
	agentServerOnce = sync.Once{}
}

// SendPush AgentServer 主动向 Gateway 推送消息（高层方法）。
//
// 对齐 Python: AgentWebSocketServer.send_push(msg)
// 内部流程：BuildServerPushWire(msg) → json.Marshal → sendToGateway(data)
// 这是所有 server_push 场景的统一入口。
// Python 中所有异常仅 warn 不上抛（返回 None），Go 对齐此行为统一返回 nil。
func (s *AgentServer) SendPush(ctx context.Context, msg map[string]any) error {
	// 对齐 Python: if self._current_ws is None or self._current_send_lock is None
	if s.transport == nil {
		logger.Warn(logComponent).Msg("SendPush 失败: 无活跃 Gateway 连接")
		return nil
	}
	// 对齐 Python: try/except Exception — 捕获 panic 防止整个 goroutine 崩溃
	defer func() {
		if r := recover(); r != nil {
			logger.Warn(logComponent).Any("error", r).Msg("SendPush 失败")
		}
	}()

	wire := transport.BuildServerPushWire(msg)
	data, err := json.Marshal(wire)
	if err != nil {
		// 对齐 Python: except Exception → warn + 静默返回
		logger.Warn(logComponent).Err(err).Msg("SendPush: wire 编码失败")
		return nil
	}
	s.sendToGateway(data)

	responseKind := ""
	if rk, ok := msg["response_kind"].(string); ok {
		responseKind = strings.TrimSpace(rk)
	}
	if responseKind != "" {
		channelID, _ := msg["channel_id"].(string)
		logger.Info(logComponent).
			Str("channel_id", channelID).
			Str("response_kind", responseKind).
			Msg("SendPush response_kind wire 已发送")
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// run 执行 AgentServer 主循环（阻塞直到 ctx 取消）。
// 按 Python AgentWebSocketServer.start() + app_agentserver.py _run() 顺序初始化。
func (s *AgentServer) run(ctx context.Context) error {
	defer close(s.stopCh)

	// 设置单例
	agentServerOnce.Do(func() {
		agentServerInstance = s
	})

	// 1. 重置 harness 包状态到 native（对齐 Python agent_ws_server.py L440）
	s.resetHarnessPackagesState()

	// 2. 确保持久化检查点器就绪（对齐 Python agent_ws_server.py L443）
	if err := adapter.EnsurePersistentCheckpointer(); err != nil {
		logger.Error(logComponent).Err(err).Msg("持久化检查点器初始化失败")
		// 对齐 Python：raise RuntimeError，Go 侧记录错误但继续启动（best-effort）
	}

	// 3. 初始化 AgentManager
	s.agentManager = runtime.NewAgentManager()
	logger.Info(logComponent).Msg("AgentManager 已初始化")

	// 4. 发送 connection.ack 事件帧（对齐 Python AgentWebSocketServer._connection_handler 首帧）
	ackFrame := transport.BuildConnectionAckFrame()
	ackData, err := json.Marshal(ackFrame)
	if err != nil {
		logger.Error(logComponent).Err(err).Msg("编码 connection.ack 失败")
	} else {
		s.sendToGateway(ackData)
		logger.Info(logComponent).Msg("AgentServer 已就绪（connection.ack 已发送）")
	}

	// 5. 沙箱自动启动（对齐 Python agent_ws_server.py L475）
	s.bootstrapInternalJiuwenbox()

	// 6. 队友启动守护进程（对齐 Python app_agentserver.py L156）
	s.startTeammateBootstrapDaemon(ctx)

	// 7. 进入消费循环（阻塞直到 ctx 取消）
	s.startConsumeLoop(ctx)

	return nil
}

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

// ──────────────────────────── TODO stub 方法（对齐 Python） ────────────────────────────

// resetHarnessPackagesState 重置 harness 包状态到 native。
// 对齐 Python: jiuwenswarm/agents/harness/common/auto_harness/service.py reset_harness_packages_state()
// TODO(⤵️ AutoHarness): 清空 harness-packages.json 中的 active_package_ids
func (s *AgentServer) resetHarnessPackagesState() {
	// 未实现：等 AutoHarness 包管理系统实现后回填
}

// bootstrapInternalJiuwenbox 沙箱自动启动。
// 对齐 Python: jiuwenswarm/server/agent_ws_server.py _bootstrap_internal_jiuwenbox()
// TODO(⤵️ JiuwenBox): 按 config.yaml::sandbox.startup_mode 决定是否自动拉起 jiuwenbox
func (s *AgentServer) bootstrapInternalJiuwenbox() {
	// 未实现：等 JiuwenBox 沙箱系统实现后回填
}

// startTeammateBootstrapDaemon 启动队友 bootstrap 守护进程。
// 对齐 Python: jiuwenswarm/agents/harness/team/remote_member_bootstrap.py run_teammate_bootstrap_daemon()
// TODO(⤵️ Team): 启动守护 goroutine 消费远程队友 bootstrap
func (s *AgentServer) startTeammateBootstrapDaemon(ctx context.Context) {
	// 未实现：等 Team 功能实现后回填
}

// cancelAllInflightWork 取消所有进行中的任务。
// 对齐 Python: jiuwenswarm/server/runtime/agent_manager.py cancel_all_inflight_work()
// TODO(⤵️ AgentManager): 等 AgentManager inflight work 追踪实现后回填
func (s *AgentServer) cancelAllInflightWork() {
	// 未实现：等 AgentManager 完整实现后回填
}

// stopScheduler 停止调度器。
// 对齐 Python: jiuwenswarm/server/agent_ws_server.py _stop_scheduler()
// TODO(⤵️ Scheduler): 等调度器实现后回填
func (s *AgentServer) stopScheduler() {
	// 未实现：等调度器实现后回填
}

// cancelAllTeamStreamTasks 取消所有 team 流式任务。
// 对齐 Python: jiuwenswarm/agents/harness/team/ cancel_all_team_stream_tasks_across_managers()
// TODO(⤵️ Team): 等 Team 流式任务管理实现后回填
func (s *AgentServer) cancelAllTeamStreamTasks() {
	// 未实现：等 Team 功能实现后回填
}
