package routing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/e2a"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/transport"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AgentClient AgentServer 客户端，通过 AgentTransport 与 AgentServer 通信。
//
// 对齐 Python: jiuwenswarm/gateway/routing/agent_client.py (WebSocketAgentServerClient)
// 核心流程：
//   - Connect: 启动 receiverLoop，等待 connection.ack 就绪通知
//   - SendRequest: 非流式请求，等单个完整响应
//   - SendRequestStream: 流式请求，持续接收 chunk 直到 is_complete
//   - receiverLoop: 统一消费 transport.Recv()，区分事件帧/server_push/正常响应
type AgentClient struct {
	// transport 传输层（byte 管道）
	transport transport.AgentTransport
	// serverReady AgentServer 是否已发送 connection.ack
	serverReady bool
	// serverReadyMu 保护 serverReady
	serverReadyMu sync.RWMutex
	// serverReadyCh 就绪通知通道
	serverReadyCh chan struct{}
	// onServerPush Agent 主动推送回调
	onServerPush func(msg map[string]any)
	// sendMu 发送互斥锁（对齐 Python _lock）
	sendMu sync.Mutex
	// messageQueues 消息分发队列：request_id → 响应等待者
	messageQueues map[string]chan map[string]any
	// messageQueuesMu 保护 messageQueues 和 cancelledRequests
	messageQueuesMu sync.Mutex
	// cancelledRequests 已取消但可能有残余消息的 request_id
	cancelledRequests map[string]struct{}
	// running 是否正在运行
	running bool
	// runningMu 保护 running
	runningMu sync.RWMutex
	// cancelFunc 接收循环取消函数
	cancelFunc context.CancelFunc
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// unaryRequestTimeoutSeconds 非流式请求超时秒数（对齐 Python _UNARY_REQUEST_TIMEOUT_SECONDS）
	unaryRequestTimeoutSeconds = 600
	// streamTrailingGraceSeconds 流式结束后额外等待秒数（对齐 Python _STREAM_TRAILING_MESSAGE_GRACE_SECONDS）
	streamTrailingGraceSeconds = 0.7
	// connectionAckTimeoutSeconds 等待 connection.ack 超时秒数
	connectionAckTimeoutSeconds = 5
	// delayedCleanupSeconds 已取消 request_id 延迟清理秒数
	delayedCleanupSeconds = 2
	// messageQueueBufferSize 消息队列缓冲大小
	messageQueueBufferSize = 16
)

// logComponentRouting 日志组件
const logComponentRouting = logger.ComponentGateway

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewAgentClient 创建 AgentServer 客户端实例。
func NewAgentClient(transport transport.AgentTransport) *AgentClient {
	return &AgentClient{
		transport:         transport,
		messageQueues:     make(map[string]chan map[string]any),
		cancelledRequests: make(map[string]struct{}),
	}
}

// Connect 启动接收循环，等待 AgentServer 发送 connection.ack。
//
// 对齐 Python: WebSocketAgentServerClient.connect(uri)
// 超时不报错仅 warn，继续运行。
func (ac *AgentClient) Connect(ctx context.Context) error {
	ac.runningMu.RLock()
	if ac.running {
		ac.runningMu.RUnlock()
		// 对齐 Python: if self._ws is not None: await self.disconnect()
		ac.Disconnect()
	} else {
		ac.runningMu.RUnlock()
	}

	// 重置就绪状态
	ac.serverReadyMu.Lock()
	ac.serverReady = false
	ac.serverReadyCh = make(chan struct{})
	ac.serverReadyMu.Unlock()

	logger.Info(logComponentRouting).
		Str("event_type", "agent_client_connecting").
		Msg("AgentClient 正在连接")

	// 启动 receiverLoop
	loopCtx, cancel := context.WithCancel(ctx)
	ac.cancelFunc = cancel
	ac.runningMu.Lock()
	ac.running = true
	ac.runningMu.Unlock()
	go ac.receiverLoop(loopCtx)

	logger.Info(logComponentRouting).
		Str("event_type", "agent_client_loop_started").
		Msg("AgentClient 接收循环已启动，等待 connection.ack")

	// 等待 serverReady 或 5s 超时（对齐 Python timeout=5.0，超时不报错仅 warn）
	select {
	case <-ac.serverReadyCh:
		logger.Info(logComponentRouting).
			Str("event_type", "agent_client_connected").
			Msg("AgentClient 已连接，AgentServer 已就绪")
	case <-time.After(connectionAckTimeoutSeconds * time.Second):
		logger.Warn(logComponentRouting).
			Str("event_type", "agent_client_ack_timeout").
			Int("timeout_seconds", connectionAckTimeoutSeconds).
			Msg("等待 connection.ack 超时（5s），继续运行")
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}

// Disconnect 停止接收循环，清理所有队列和状态。
//
// 对齐 Python: WebSocketAgentServerClient.disconnect()
func (ac *AgentClient) Disconnect() {
	ac.runningMu.Lock()
	if !ac.running {
		ac.runningMu.Unlock()
		return
	}
	ac.running = false
	ac.runningMu.Unlock()

	// 取消接收循环
	if ac.cancelFunc != nil {
		ac.cancelFunc()
		ac.cancelFunc = nil
	}

	// 清理所有队列
	ac.messageQueuesMu.Lock()
	for rid, q := range ac.messageQueues {
		close(q)
		delete(ac.messageQueues, rid)
	}
	ac.messageQueuesMu.Unlock()

	// 关闭传输通道
	if ac.transport != nil {
		_ = ac.transport.Close()
	}

	logger.Info(logComponentRouting).
		Str("event_type", "agent_client_disconnected").
		Msg("AgentClient 已断开")
}

// ServerReady 检查 AgentServer 是否已发送 connection.ack 确认就绪。
//
// 对齐 Python: WebSocketAgentServerClient.server_ready (property)
func (ac *AgentClient) ServerReady() bool {
	ac.serverReadyMu.RLock()
	defer ac.serverReadyMu.RUnlock()
	return ac.serverReady
}

// IsConnected 检查 AgentClient 是否已连接（接收循环正在运行）。
func (ac *AgentClient) IsConnected() bool {
	ac.runningMu.RLock()
	defer ac.runningMu.RUnlock()
	return ac.running
}

// WaitServerReady 阻塞等待 AgentServer 就绪，返回是否就绪。
//
// 若 AgentClient 未运行或已断开，立即返回 false。
func (ac *AgentClient) WaitServerReady(ctx context.Context) bool {
	ac.serverReadyMu.RLock()
	if ac.serverReady {
		ac.serverReadyMu.RUnlock()
		return true
	}
	ch := ac.serverReadyCh
	ac.serverReadyMu.RUnlock()

	if ch == nil {
		return false
	}
	select {
	case <-ch:
		return ac.ServerReady()
	case <-ctx.Done():
		return false
	}
}

// SendRequest 发送非流式请求并等待完整响应。
//
// 对齐 Python: WebSocketAgentServerClient.send_request(envelope)
func (ac *AgentClient) SendRequest(ctx context.Context, envelope *e2a.E2AEnvelope) (*schema.AgentResponse, error) {
	if err := ac.ensureConnected(); err != nil {
		return nil, err
	}

	// 强制非流式（对齐 Python envelope.is_stream = False）
	envelope.IsStream = false
	rid := transport.WireRequestIDKey(envelope.RequestID)

	logger.Info(logComponentRouting).
		Str("event_type", "E2A_OUT_NOSTREAM").
		Str("request_id", rid).
		Str("channel", envelope.Channel).
		Str("method", envelope.Method).
		Bool("is_stream", envelope.IsStream).
		Msg("发送非流式请求")

	// 对齐 Python: logger.debug("发送请求(非流式) E2A: %s", _to_json(envelope.to_dict()))
	logger.Debug(logComponentRouting).
		Str("event_type", "E2A_OUT_NOSTREAM").
		Str("request_id", rid).
		Str("method", envelope.Method).
		Msg("发送请求(非流式) E2A 详情")

	// 检查 duplicate in-flight（对齐 Python rid in self._message_queues）
	ac.messageQueuesMu.Lock()
	if _, exists := ac.messageQueues[rid]; exists {
		ac.messageQueuesMu.Unlock()
		return nil, fmt.Errorf("duplicate in-flight request_id=%s; refusing to register queue", rid)
	}
	queue := make(chan map[string]any, messageQueueBufferSize)
	ac.messageQueues[rid] = queue
	ac.messageQueuesMu.Unlock()

	defer ac.drainAndRemoveQueue(rid)

	// 发送请求（对齐 Python async with _lock: ws.send(json.dumps(envelope.to_dict()))）
	data, err := json.Marshal(envelope)
	if err != nil {
		return nil, fmt.Errorf("序列化 E2AEnvelope 失败: %w", err)
	}
	ac.sendMu.Lock()
	err = ac.transport.Send(ctx, data)
	ac.sendMu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("发送请求失败: %w", err)
	}

	// 对齐 Python: logger.info("发送请求(非流式) payload: %s", _to_json(payload))
	logger.Debug(logComponentRouting).
		Str("event_type", "E2A_OUT_NOSTREAM").
		Str("request_id", rid).
		Int("payload_bytes", len(data)).
		Msg("发送请求(非流式) payload 已发送")

	// 等待响应（对齐 Python asyncio.wait_for(queue.get(), timeout=600)）
	select {
	case respData, ok := <-queue:
		if !ok || respData == nil {
			return nil, fmt.Errorf("响应通道已关闭: request_id=%s", rid)
		}
		return parseAgentServerWireUnary(respData)
	case <-time.After(unaryRequestTimeoutSeconds * time.Second):
		logger.Warn(logComponentRouting).
			Str("event_type", "LLM_CALL_ERROR").
			Str("request_id", rid).
			Int("timeout_seconds", unaryRequestTimeoutSeconds).
			Msg("AgentServer 非流式请求超时")
		return nil, fmt.Errorf("AgentServer 非流式请求超时 (request_id=%s, timeout=%ds)", rid, unaryRequestTimeoutSeconds)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// SendRequestStream 发送流式请求，返回 chunk 通道。
//
// 对齐 Python: WebSocketAgentServerClient.send_request_stream(envelope)
func (ac *AgentClient) SendRequestStream(ctx context.Context, envelope *e2a.E2AEnvelope) (<-chan *schema.AgentResponseChunk, error) {
	if err := ac.ensureConnected(); err != nil {
		return nil, err
	}

	// 强制流式（对齐 Python envelope.is_stream = True）
	envelope.IsStream = true
	rid := transport.WireRequestIDKey(envelope.RequestID)

	logger.Info(logComponentRouting).
		Str("event_type", "E2A_OUT_STREAM").
		Str("request_id", rid).
		Str("channel", envelope.Channel).
		Str("method", envelope.Method).
		Bool("is_stream", envelope.IsStream).
		Msg("发送流式请求")

	// 对齐 Python: logger.debug("发送请求(流式) E2A: %s", _to_json(envelope.to_dict()))
	logger.Debug(logComponentRouting).
		Str("event_type", "E2A_OUT_STREAM").
		Str("request_id", rid).
		Str("method", envelope.Method).
		Msg("发送请求(流式) E2A 详情")

	// 检查 duplicate in-flight
	ac.messageQueuesMu.Lock()
	if _, exists := ac.messageQueues[rid]; exists {
		ac.messageQueuesMu.Unlock()
		return nil, fmt.Errorf("duplicate in-flight request_id=%s; refusing to register queue", rid)
	}
	queue := make(chan map[string]any, messageQueueBufferSize)
	ac.messageQueues[rid] = queue
	ac.messageQueuesMu.Unlock()

	// 发送请求
	data, err := json.Marshal(envelope)
	if err != nil {
		ac.drainAndRemoveQueue(rid)
		return nil, fmt.Errorf("序列化 E2AEnvelope 失败: %w", err)
	}
	ac.sendMu.Lock()
	err = ac.transport.Send(ctx, data)
	ac.sendMu.Unlock()
	if err != nil {
		ac.drainAndRemoveQueue(rid)
		return nil, fmt.Errorf("发送请求失败: %w", err)
	}

	// 对齐 Python: logger.info("发送请求(流式) payload: %s", _to_json(payload))
	logger.Debug(logComponentRouting).
		Str("event_type", "E2A_OUT_STREAM").
		Str("request_id", rid).
		Int("payload_bytes", len(data)).
		Msg("发送请求(流式) payload 已发送")

	// 启动 goroutine 从 queue 读取并写入 chunkCh
	chunkCh := make(chan *schema.AgentResponseChunk, messageQueueBufferSize)
	go ac.streamReceiver(ctx, rid, queue, chunkCh)

	return chunkCh, nil
}

// SetServerPushHandler 注册 Agent 主动推送回调。
//
// 对齐 Python: WebSocketAgentServerClient.set_server_push_handler(handler)
func (ac *AgentClient) SetServerPushHandler(handler func(msg map[string]any)) {
	ac.onServerPush = handler
}

// SetOrUpdateServerConfig 缓存或更新服务端配置快照（当前为 no-op）。
//
// 对齐 Python: WebSocketAgentServerClient.set_or_update_server_config(config, env)
// 默认 WebSocket client 不处理服务端配置缓存，留给扩展 client 自行实现。
func (ac *AgentClient) SetOrUpdateServerConfig(config map[string]any, env map[string]string) {
	// no-op，对齐 Python 默认实现
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// receiverLoop 统一消费 transport.Recv()，区分事件帧/server_push/正常响应。
//
// 对齐 Python: WebSocketAgentServerClient._message_receiver_loop()
func (ac *AgentClient) receiverLoop(ctx context.Context) {
	recvCh, err := ac.transport.Recv()
	if err != nil {
		logger.Error(logComponentRouting).
			Str("event_type", "LLM_CALL_ERROR").
			Err(err).
			Msg("获取接收通道失败")
		return
	}

	for {
		select {
		case <-ctx.Done():
			logger.Info(logComponentRouting).
				Str("event_type", "agent_client_receiver_stopped").
				Msg("接收循环已停止")
			return
		case data, ok := <-recvCh:
			if !ok {
				logger.Info(logComponentRouting).
					Str("event_type", "agent_client_recv_channel_closed").
					Msg("接收通道已关闭")
				return
			}
			// JSON 字节 → map（对齐 Python json.loads(raw)）
			var msg map[string]any
			if err := json.Unmarshal(data, &msg); err != nil {
				logger.Warn(logComponentRouting).
					Str("event_type", "LLM_CALL_ERROR").
					Err(err).
					Int("bytes", len(data)).
					Msg("接收消息 JSON 解码失败")
				// 对齐 Python: await asyncio.sleep(0.1) 避免快速循环
				time.Sleep(100 * time.Millisecond)
				continue
			}

			// 1. 事件帧？（connection.ack 等）
			if msg["type"] == "event" {
				ac.handleEventFrame(msg)
				continue
			}

			// 2. server_push？（对齐 Python metadata[E2A_WIRE_SERVER_PUSH_KEY]）
			if meta, ok := msg["metadata"].(map[string]any); ok {
				if _, has := meta[e2a.E2AWireServerPushKey]; has {
					if ac.onServerPush != nil {
						go ac.onServerPush(msg) // 对齐 Python asyncio.create_task
					} else {
						logger.Warn(logComponentRouting).
							Str("event_type", "agent_client_server_push_no_handler").
							Str("request_id", fmt.Sprintf("%v", msg["request_id"])).
							Msg("收到 server_push 但未注册 handler，已丢弃")
					}
					continue
				}
			}

			// 3. 正常响应 → 按 request_id 路由到 messageQueues
			ac.routeToQueue(msg)
		}
	}
}

// handleEventFrame 处理事件帧（connection.ack 等）。
func (ac *AgentClient) handleEventFrame(msg map[string]any) {
	event, _ := msg["event"].(string)
	if event == "connection.ack" {
		ac.serverReadyMu.Lock()
		ac.serverReady = true
		ch := ac.serverReadyCh
		ac.serverReadyMu.Unlock()
		if ch != nil {
			select {
			case ch <- struct{}{}:
			default:
				// 已发送过就绪通知，忽略
			}
		}
		logger.Info(logComponentRouting).
			Str("event_type", "agent_client_connection_ack").
			Msg("收到 connection.ack，AgentServer 已就绪")
	} else {
		logger.Warn(logComponentRouting).
			Str("event_type", "agent_client_unknown_event").
			Str("event", event).
			Msg("收到未知事件帧")
	}
}

// routeToQueue 按 request_id 路由消息到对应的等待队列。
//
// 对齐 Python: _message_receiver_loop 中的 request_id 路由逻辑
func (ac *AgentClient) routeToQueue(msg map[string]any) {
	rid := transport.WireRequestIDKey(msg["request_id"])

	ac.messageQueuesMu.Lock()
	// 检查是否是已取消的请求，静默丢弃消息（对齐 Python）
	if _, cancelled := ac.cancelledRequests[rid]; cancelled {
		ac.messageQueuesMu.Unlock()
		logger.Debug(logComponentRouting).
			Str("event_type", "agent_client_cancelled_message_discarded").
			Str("request_id", rid).
			Msg("收到已取消请求的残余消息，已丢弃")
		return
	}

	if rid != "" {
		if q, exists := ac.messageQueues[rid]; exists {
			select {
			case q <- msg:
			default:
				logger.Warn(logComponentRouting).
					Str("event_type", "agent_client_queue_full").
					Str("request_id", rid).
					Msg("消息队列已满，丢弃消息")
			}
		} else {
			logger.Debug(logComponentRouting).
				Str("event_type", "agent_client_no_target_queue").
				Str("request_id", rid).
				Msg("收到无目标队列的消息")
		}
	}
	ac.messageQueuesMu.Unlock()
}

// drainAndRemoveQueue 清空队列中的残余消息并移除队列，同时标记 request_id 为已取消状态。
//
// 对齐 Python: WebSocketAgentServerClient._drain_and_remove_queue(rid)
func (ac *AgentClient) drainAndRemoveQueue(rid string) {
	ac.messageQueuesMu.Lock()
	q, exists := ac.messageQueues[rid]
	if !exists {
		ac.messageQueuesMu.Unlock()
		return
	}
	// 1. 先标记为已取消，阻止后续消息进入队列（对齐 Python）
	ac.cancelledRequests[rid] = struct{}{}
	// 2. 删除队列注册
	delete(ac.messageQueues, rid)
	// 3. 清空队列中的残余消息
	drainedCount := 0
	for {
		select {
		case <-q:
			drainedCount++
		default:
			goto done
		}
	}
done:
	ac.messageQueuesMu.Unlock()

	logger.Debug(logComponentRouting).
		Str("event_type", "agent_client_queue_drained").
		Str("request_id", rid).
		Int("drained_count", drainedCount).
		Msg("队列已清空并移除")

	// 4. 异步延迟清理已取消标记（对齐 Python asyncio.create_task(_delayed_cleanup_cancelled_request_id)）
	go ac.delayedCleanupCancelledRequestID(rid)
}

// delayedCleanupCancelledRequestID 延迟清理已取消的 request_id 标记。
//
// 对齐 Python: WebSocketAgentServerClient._delayed_cleanup_cancelled_request_id(rid)
func (ac *AgentClient) delayedCleanupCancelledRequestID(rid string) {
	time.Sleep(delayedCleanupSeconds * time.Second)
	ac.messageQueuesMu.Lock()
	delete(ac.cancelledRequests, rid)
	ac.messageQueuesMu.Unlock()

	logger.Debug(logComponentRouting).
		Str("event_type", "agent_client_cancelled_id_cleaned").
		Str("request_id", rid).
		Msg("已取消标记已清理")
}

// ensureConnected 检查是否已连接。
//
// 对齐 Python: WebSocketAgentServerClient._ensure_connected()
func (ac *AgentClient) ensureConnected() error {
	ac.runningMu.RLock()
	r := ac.running
	ac.runningMu.RUnlock()
	if !r {
		return errors.New("AgentClient 未连接 AgentServer，请先调用 Connect()")
	}
	return nil
}

// streamReceiver 流式响应接收 goroutine。
//
// 从 messageQueue 读取消息，解析为 AgentResponseChunk 写入 chunkCh。
// 对齐 Python: send_request_stream 中的 while True 循环。
func (ac *AgentClient) streamReceiver(ctx context.Context, rid string, queue chan map[string]any, chunkCh chan *schema.AgentResponseChunk) {
	defer close(chunkCh)
	defer ac.drainAndRemoveQueue(rid)

	chunkCount := 0
	sawComplete := false

	for {
		var msg map[string]any
		var ok bool
		shouldBreak := false

		if sawComplete {
			// 对齐 Python: saw_complete → wait queue.get(timeout=0.7s)
			select {
			case msg, ok = <-queue:
				if !ok {
					shouldBreak = true
				}
			case <-time.After(time.Duration(streamTrailingGraceSeconds * float64(time.Second))):
				shouldBreak = true
			case <-ctx.Done():
				return
			}
		} else {
			// 正常等待
			select {
			case msg, ok = <-queue:
				if !ok {
					return
				}
			case <-ctx.Done():
				return
			}
		}

		if shouldBreak {
			break
		}
		if !ok || msg == nil {
			return
		}

		chunk, err := parseAgentServerWireChunk(msg)
		if err != nil {
			logger.Warn(logComponentRouting).
				Str("event_type", "LLM_CALL_ERROR").
				Str("request_id", rid).
				Err(err).
				Msg("解析流式 chunk 失败")
			continue
		}

		chunkCount++
		select {
		case chunkCh <- chunk:
		case <-ctx.Done():
			return
		}

		if chunk.IsComplete {
			sawComplete = true
		}
	}

	logger.Info(logComponentRouting).
		Str("event_type", "agent_client_stream_complete").
		Str("request_id", rid).
		Int("chunk_count", chunkCount).
		Msg("流式响应结束")
}

// parseAgentServerWireUnary 从 wire map 解析非流式响应。
//
// 对齐 Python: parse_agent_server_wire_unary(data)
// 复用 e2a.ParseAgentServerWireUnary，包含 E2A 格式判别 + legacy fallback + deprecated 兜底。
func parseAgentServerWireUnary(data map[string]any) (*schema.AgentResponse, error) {
	return e2a.ParseAgentServerWireUnary(data)
}

// parseAgentServerWireChunk 从 wire map 解析流式 chunk。
//
// 对齐 Python: parse_agent_server_wire_chunk(data)
// 复用 e2a.ParseAgentServerWireChunk，包含 E2A 格式判别 + legacy fallback + deprecated 兜底。
func parseAgentServerWireChunk(data map[string]any) (*schema.AgentResponseChunk, error) {
	return e2a.ParseAgentServerWireChunk(data)
}
