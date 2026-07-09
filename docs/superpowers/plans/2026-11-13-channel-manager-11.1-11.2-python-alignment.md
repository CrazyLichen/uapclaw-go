# 11.1+11.2 ChannelManager 对齐 Python 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将 Go 的 ChannelManager 和 MessageHandler 完整对齐 Python 源码，修复入站/出站消息流差异。

**Architecture:** ChannelManager 新增入站中转（存活检查+转发）和出站派发循环（按 channel_id 定向投递），MessageHandler 暴露 ConsumeRobotMessages/ConsumeRobotMessages 接口供 ChannelManager 消费，删除 MessageHandler 内部的 outboundLoop。通过在 channel_manager 包定义 InboundMessageHandler + RobotMessageConsumer 两个最小接口避免循环依赖。

**Tech Stack:** Go 1.22+, sync/atomic, context, testing

---

## 文件结构

| 文件 | 操作 | 职责 |
|------|------|------|
| `channel_manager/channel_manager.go` | 修改 | 新增 9 个方法、删除 3 个、结构体+构造变更 |
| `channel_manager/channel_manager_test.go` | 修改 | 更新测试适配新 API，新增测试 |
| `channel_manager/doc.go` | 修改 | 更新文件目录 |
| `message_handler/message_handler.go` | 修改 | 重命名+新增+删除方法，修改 StartForwarding |
| `message_handler/message_handler_test.go` | 修改 | 更新测试适配新 API |
| `message_handler/forward_loop.go` | 修改 | enqueueOutbound → PublishRobotMessages |
| `message_handler/forward_loop_test.go` | 修改 | 适配 |
| `message_handler/outbound.go` | 修改 | enqueueOutbound → PublishRobotMessages |
| `message_handler/cancel.go` | 修改 | enqueueOutbound → PublishRobotMessages |
| `message_handler/slash_cmd.go` | 修改 | enqueueOutbound → PublishRobotMessages |
| `message_handler/doc.go` | 修改 | 更新文件目录 |
| `gateway/app_gateway.go` | 修改 | 组装更新 |

---

### Task 1: 定义 InboundMessageHandler + RobotMessageConsumer 接口

**Files:**
- Modify: `internal/swarm/gateway/channel_manager/channel_manager.go`

- [ ] **Step 1: 在 channel_manager.go 的结构体区块前添加两个接口定义**

在 `// ──────────────────────────── 结构体 ────────────────────────────` 注释后、`OnConfigUpdatedFunc` 前插入：

```go
// InboundMessageHandler 入站消息处理接口，由 MessageHandler 实现。
//
// ChannelManager 通过此接口将入站消息转发到 MessageHandler，
// 避免 channel_manager → message_handler 循环依赖。
//
// 对齐 Python MessageHandler.handle_message
type InboundMessageHandler interface {
	// HandleMessage 处理入站消息（写入 userMessages 队列）。
	HandleMessage(msg *schema.Message)
}

// RobotMessageConsumer 出站消息消费接口，由 MessageHandler 实现。
//
// ChannelManager 通过此接口从 MessageHandler 的 robotMessages 队列消费消息，
// 按 channel_id 投递到对应 Channel。
//
// 对齐 Python MessageHandler.consume_robot_messages
type RobotMessageConsumer interface {
	// ConsumeRobotMessages 从出站队列消费一条消息，超时返回 nil。
	ConsumeRobotMessages(timeout time.Duration) *schema.Message
}
```

- [ ] **Step 2: 运行编译确认无错误**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/swarm/gateway/channel_manager/...`
Expected: 编译成功（接口定义不影响已有代码）

- [ ] **Step 3: 提交**

```bash
git add internal/swarm/gateway/channel_manager/channel_manager.go
git commit -m "feat(channel_manager): 定义 InboundMessageHandler + RobotMessageConsumer 接口避免循环依赖"
```

---

### Task 2: 重构 ChannelManager 结构体和构造函数

**Files:**
- Modify: `internal/swarm/gateway/channel_manager/channel_manager.go`

- [ ] **Step 1: 修改 ChannelManager 结构体**

将现有结构体替换为：

```go
// ChannelManager 负责 Channel 的注册、注销、查找与消息分发。
//
// 核心职责：
//  1. Channel 的注册、注销与查找
//  2. 将各 Channel 收到的消息统一转发到 MessageHandler
//  3. 运行出站派发循环：从 MessageHandler 取出 AgentServer 响应并投递到对应 Channel
//  4. 配置热更新回调
//
// 对应 Python: jiuwenswarm/gateway/channel_manager/channel_manager.py (ChannelManager)
type ChannelManager struct {
	// channels 已注册的 Channel 实例映射（channelID → BaseChannel）
	channels map[string]BaseChannel
	// config Channel 相关配置（channelID → 配置 dict）
	config map[string]map[string]any
	// onConfigUpdated 配置更新回调
	onConfigUpdated OnConfigUpdatedFunc
	// pendingChannelRestart 待强制重启的 channelID 集合
	pendingChannelRestart map[string]struct{}
	// inboundHandler 入站消息处理器（对齐 Python _message_handler）
	inboundHandler InboundMessageHandler
	// consumeProvider 出站消息消费者（对齐 Python _message_handler.consume_robot_messages）
	consumeProvider RobotMessageConsumer
	// running 出站派发循环运行状态（对齐 Python _running）
	running atomic.Bool
	// dispatchCancel 出站派发循环取消函数（对齐 Python _dispatch_task）
	dispatchCancel context.CancelFunc
	// mu 保护 channels/config/pendingChannelRestart 的并发访问
	mu sync.RWMutex
}
```

- [ ] **Step 2: 修改 NewChannelManager 构造函数**

替换现有构造函数：

```go
// NewChannelManager 创建 ChannelManager 实例。
//
// 参数：
//   - inboundHandler：入站消息处理器，可为 nil（仅配置管理场景）
//   - consumeProvider：出站消息消费者，可为 nil（仅配置管理场景）
//   - config：初始 Channel 配置（channelID → 配置 dict），可为 nil
//   - onConfigUpdated：配置更新回调，可为 nil
//
// 对齐 Python: ChannelManager(message_handler, config, on_config_updated)
func NewChannelManager(
	inboundHandler InboundMessageHandler,
	consumeProvider RobotMessageConsumer,
	config map[string]map[string]any,
	onConfigUpdated OnConfigUpdatedFunc,
) *ChannelManager {
	cfg := make(map[string]map[string]any)
	for k, v := range config {
		cfg[k] = v
	}
	return &ChannelManager{
		channels:              make(map[string]BaseChannel),
		config:                cfg,
		onConfigUpdated:       onConfigUpdated,
		pendingChannelRestart: make(map[string]struct{}),
		inboundHandler:        inboundHandler,
		consumeProvider:       consumeProvider,
	}
}
```

- [ ] **Step 3: 在 import 中添加 `sync/atomic` 和 `context`**

确保 import 包含 `"context"`, `"sync"`, `"sync/atomic"`。

- [ ] **Step 4: 运行编译确认**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/swarm/gateway/channel_manager/...`
Expected: 可能有调用方编译错误（app_gateway.go），暂忽略，下一步修

- [ ] **Step 5: 提交**

```bash
git add internal/swarm/gateway/channel_manager/channel_manager.go
git commit -m "refactor(channel_manager): 重构结构体新增 inboundHandler/consumeProvider/running/dispatchCancel 字段"
```

---

### Task 3: 替换 ChannelManager 注册/注销/查找方法

**Files:**
- Modify: `internal/swarm/gateway/channel_manager/channel_manager.go`

- [ ] **Step 1: 删除旧 Register 方法，添加新注册方法**

删除 `Register(ch BaseChannel, onMessageCallback func(*schema.Message))` 方法，替换为以下 4 个方法：

```go
// RegisterChannel 注册 Channel，并为其注册默认入站回调（存活检查+转发）。
//
// 注册后 Channel 收到的消息将通过 onChannelMessage 转发到 MessageHandler。
//
// 对齐 Python: ChannelManager.register_channel()
func (cm *ChannelManager) RegisterChannel(ch BaseChannel) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	cid := ch.ChannelID()
	cm.channels[cid] = ch
	ch.OnMessage(cm.onChannelMessage)

	logger.Info(logComponent).
		Str("channel_id", cid).
		Int("total", len(cm.channels)).
		Msg("已注册 Channel")
}

// RegisterChannelWithInbound 注册 Channel 并使用自定义入站回调。
//
// 不替换为默认 onChannelMessage，由调用方决定消息处理路径。
//
// 对齐 Python: ChannelManager.register_channel_with_inbound()
func (cm *ChannelManager) RegisterChannelWithInbound(ch BaseChannel, onMessage func(*schema.Message)) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	cm.channels[ch.ChannelID()] = ch
	ch.OnMessage(onMessage)

	logger.Info(logComponent).
		Str("channel_id", ch.ChannelID()).
		Int("total", len(cm.channels)).
		Msg("已注册 Channel（自定义入站）")
}

// RegisterExternalChannel 登记一个已由外部完成入站装配的 Channel 实例。
//
// 对齐 Python: ChannelManager.register_external_channel()
func (cm *ChannelManager) RegisterExternalChannel(channelID string, ch BaseChannel) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	cm.channels[channelID] = ch
}

// DeliverToMessageHandler 将消息直接交给 MessageHandler。
//
// 供自定义入站路径使用，不做存活检查。
//
// 对齐 Python: ChannelManager.deliver_to_message_handler()
func (cm *ChannelManager) DeliverToMessageHandler(msg *schema.Message) {
	if cm.inboundHandler == nil {
		logger.Warn(logComponent).Str("msg_id", msg.ID).Msg("inboundHandler 为空，无法转发消息")
		return
	}
	cm.inboundHandler.HandleMessage(msg)
}
```

- [ ] **Step 2: 替换 Unregister 为 UnregisterChannel**

将现有 `Unregister` 方法重命名为 `UnregisterChannel`，更新注释：

```go
// UnregisterChannel 注销指定 Channel。
//
// 对齐 Python: ChannelManager.unregister_channel()
func (cm *ChannelManager) UnregisterChannel(channelID string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	delete(cm.channels, channelID)

	logger.Info(logComponent).
		Str("channel_id", channelID).
		Msg("已注销 Channel")
}
```

- [ ] **Step 3: 删除 BroadcastToChannels 方法**

删除整个 `BroadcastToChannels` 方法。

- [ ] **Step 4: 添加 onChannelMessage 非导出方法**

在非导出函数区块添加：

```go
// onChannelMessage 默认入站回调：存活检查 + 转发到 MessageHandler。
//
// 对齐 Python: ChannelManager._on_channel_message()
func (cm *ChannelManager) onChannelMessage(msg *schema.Message) {
	logger.Info(logComponent).
		Str("msg_id", msg.ID).
		Str("channel_id", msg.ChannelID).
		Msg("Channel 消息 -> MessageHandler")

	// 存活检查：Channel 已注销则丢弃
	cm.mu.RLock()
	_, exists := cm.channels[msg.ChannelID]
	cm.mu.RUnlock()

	if !exists {
		logger.Info(logComponent).
			Str("channel_id", msg.ChannelID).
			Msg("Channel 已关闭，丢弃此消息")
		return
	}

	if cm.inboundHandler == nil {
		logger.Warn(logComponent).Str("msg_id", msg.ID).Msg("inboundHandler 为空，无法转发消息")
		return
	}
	cm.inboundHandler.HandleMessage(msg)
}
```

- [ ] **Step 5: 运行编译确认**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/swarm/gateway/channel_manager/...`
Expected: 编译成功（包内自洽）

- [ ] **Step 6: 提交**

```bash
git add internal/swarm/gateway/channel_manager/channel_manager.go
git commit -m "feat(channel_manager): 替换 Register 为 RegisterChannel/RegisterChannelWithInbound/RegisterExternalChannel/DeliverToMessageHandler，添加 onChannelMessage 存活检查"
```

---

### Task 4: 添加 ChannelManager 出站派发循环

**Files:**
- Modify: `internal/swarm/gateway/channel_manager/channel_manager.go`

- [ ] **Step 1: 添加 dispatchRobotMessages + notifyCronDeliveryError + StartDispatch + StopDispatch**

在非导出函数区块添加：

```go
// dispatchRobotMessages 出站派发循环：从 MessageHandler 消费 robot_messages，按 channel_id 投递到对应 Channel。
//
// 对齐 Python: ChannelManager._dispatch_robot_messages()
func (cm *ChannelManager) dispatchRobotMessages(ctx context.Context) {
	if cm.consumeProvider == nil {
		logger.Warn(logComponent).Msg("consumeProvider 为空，出站派发跳过")
		return
	}
	for cm.running.Load() {
		msg := cm.consumeProvider.ConsumeRobotMessages(1 * time.Second)
		if msg == nil {
			continue
		}
		cm.mu.RLock()
		channel, exists := cm.channels[msg.ChannelID]
		cm.mu.RUnlock()

		if exists {
			if err := channel.Send(ctx, msg); err != nil {
				logger.Error(logComponent).
					Str("channel_id", msg.ChannelID).
					Err(err).
					Msg("投递消息到 Channel 失败")
				// cron 投递失败通知
				if strings.HasPrefix(msg.ID, "cron-push-") {
					cm.notifyCronDeliveryError(msg, err)
				}
			}
		} else {
			logger.Warn(logComponent).
				Str("channel_id", msg.ChannelID).
				Str("msg_id", msg.ID).
				Msg("未找到 Channel，丢弃 robot_messages")
		}
	}
}

// notifyCronDeliveryError 推送失败时，通过 web channel 发送 chat.error 通知前端。
//
// 对齐 Python: ChannelManager._notify_cron_delivery_error()
func (cm *ChannelManager) notifyCronDeliveryError(originalMsg *schema.Message, deliveryErr error) {
	cronInfo, _ := originalMsg.Payload["cron"].(map[string]any)
	jobName := ""
	if cronInfo != nil {
		jobName, _ = cronInfo["job_name"].(string)
	}
	errorText := fmt.Sprintf("定时任务「%s」推送到 %s 失败：%v", jobName, originalMsg.ChannelID, deliveryErr)

	errMsg := &schema.Message{
		ID:        "cron-delivery-error-" + originalMsg.ID,
		Type:      "event",
		ChannelID: "web",
		SessionID: originalMsg.SessionID,
		Params:    map[string]any{},
		OK:        false,
		Payload: map[string]any{
			"event_type": "chat.error",
			"error":      errorText,
		},
	}

	cm.mu.RLock()
	webChannel, exists := cm.channels["web"]
	cm.mu.RUnlock()

	if exists {
		if err := webChannel.Send(context.Background(), errMsg); err != nil {
			logger.Warn(logComponent).Msg("发送 cron 推送失败通知到 web 也失败了")
		}
	}
}
```

在导出函数区块添加：

```go
// StartDispatch 启动出站派发循环（消费 MessageHandler.robot_messages 并发送到各 Channel）。
//
// 对齐 Python: ChannelManager.start_dispatch()
func (cm *ChannelManager) StartDispatch(ctx context.Context) error {
	if cm.running.Load() {
		return nil
	}
	cm.running.Store(true)
	dispatchCtx, cancel := context.WithCancel(ctx)
	cm.dispatchCancel = cancel
	go cm.dispatchRobotMessages(dispatchCtx)
	logger.Info(logComponent).Msg("出站派发循环已启动 (robot_messages -> Channel.send)")
	return nil
}

// StopDispatch 停止出站派发循环。
//
// 对齐 Python: ChannelManager.stop_dispatch()
func (cm *ChannelManager) StopDispatch() error {
	if !cm.running.Load() {
		return nil
	}
	cm.running.Store(false)
	if cm.dispatchCancel != nil {
		cm.dispatchCancel()
		cm.dispatchCancel = nil
	}
	logger.Info(logComponent).Msg("出站派发循环已停止")
	return nil
}
```

- [ ] **Step 2: 在 import 中添加 `strings` 和 `time`**

确保 import 包含 `"strings"`, `"time"`。

- [ ] **Step 3: 运行编译确认**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/swarm/gateway/channel_manager/...`
Expected: 编译成功

- [ ] **Step 4: 提交**

```bash
git add internal/swarm/gateway/channel_manager/channel_manager.go
git commit -m "feat(channel_manager): 添加 dispatchRobotMessages 定向投递 + StartDispatch/StopDispatch + notifyCronDeliveryError"
```

---

### Task 5: 重命名 MessageHandler 方法 + 暴露消费接口

**Files:**
- Modify: `internal/swarm/gateway/message_handler/message_handler.go`
- Modify: `internal/swarm/gateway/message_handler/forward_loop.go`

- [ ] **Step 1: 将 HandleInbound 重命名为 HandleMessage，修改签名**

将 `message_handler.go` 中的：

```go
func (mh *MessageHandler) HandleInbound(_ context.Context, msg *schema.Message) error {
```

替换为：

```go
// HandleMessage 处理入站消息（用户→Agent）。
//
// 将消息写入 userMessages channel，由 forwardLoop 异步消费。
// 对齐 Python handle_message：非阻塞写入，channel 满时丢弃并记录警告。
//
// 对齐 Python: MessageHandler.handle_message()
func (mh *MessageHandler) HandleMessage(msg *schema.Message) {
```

函数体也修改——去掉 error 返回，改 `return nil` 为 `return`：

```go
func (mh *MessageHandler) HandleMessage(msg *schema.Message) {
	if msg == nil {
		return
	}
	select {
	case mh.userMessages <- msg:
		logger.Debug(logComponent).
			Str("event_type", "handle_inbound").
			Str("msg_id", msg.ID).
			Str("session_id", msg.SessionID).
			Msg("入站消息已入队")
	default:
		logger.Warn(logComponent).
			Str("event_type", "handle_inbound_dropped").
			Str("msg_id", msg.ID).
			Msg("入站消息队列已满，丢弃消息")
	}
}
```

- [ ] **Step 2: 将 enqueueOutbound 重命名为 PublishRobotMessages**

在 `forward_loop.go` 中将：

```go
func (mh *MessageHandler) enqueueOutbound(msg *schema.Message) {
```

替换为：

```go
// PublishRobotMessages 将 Agent 响应写入出站 channel。
//
// 非阻塞写入，channel 满时丢弃并记录警告。
// 对齐 Python: MessageHandler.publish_robot_messages()
func (mh *MessageHandler) PublishRobotMessages(msg *schema.Message) {
```

- [ ] **Step 3: 全局替换 enqueueOutbound 调用为 PublishRobotMessages**

在以下文件中将所有 `mh.enqueueOutbound(` 替换为 `mh.PublishRobotMessages(`：
- `forward_loop.go`（2 处调用：processStream、processNonStreamRequest）
- `outbound.go`（1 处调用：HandleServerPush）
- `cancel.go`（3 处调用）
- `slash_cmd.go`（1 处调用）

- [ ] **Step 4: 添加 ConsumeRobotMessages、ConsumeUserMessages、PublishUserMessagesNowait 方法**

在 `message_handler.go` 的导出函数区块添加：

```go
// ConsumeRobotMessages 从出站队列消费一条消息，超时返回 nil。
//
// 供 ChannelManager 的出站派发循环调用。
// 对齐 Python: MessageHandler.consume_robot_messages()
func (mh *MessageHandler) ConsumeRobotMessages(timeout time.Duration) *schema.Message {
	select {
	case msg := <-mh.robotMessages:
		return msg
	case <-time.After(timeout):
		return nil
	}
}

// ConsumeUserMessages 从入站队列消费一条消息，超时返回 nil。
//
// 对齐 Python: MessageHandler.consume_user_messages()
func (mh *MessageHandler) ConsumeUserMessages(timeout time.Duration) *schema.Message {
	select {
	case msg := <-mh.userMessages:
		return msg
	case <-time.After(timeout):
		return nil
	}
}

// PublishUserMessagesNowait 将消息同步写入入站队列，满时丢弃。
//
// 对齐 Python: MessageHandler.publish_user_messages_nowait()
func (mh *MessageHandler) PublishUserMessagesNowait(msg *schema.Message) {
	if msg == nil {
		return
	}
	select {
	case mh.userMessages <- msg:
	default:
		logger.Warn(logComponent).
			Str("event_type", "inbound_queue_full").
			Str("msg_id", msg.ID).
			Msg("入站消息队列已满，丢弃消息")
	}
}
```

- [ ] **Step 5: 在 import 中添加 `time`**

确保 `message_handler.go` 的 import 包含 `"time"`。

- [ ] **Step 6: 删除 outboundLoop、dispatchOutbound、StartOutboundLoop**

从 `message_handler.go` 中删除以下三个方法：
- `outboundLoop(ctx context.Context)`
- `dispatchOutbound(ctx context.Context, msg *schema.Message)`
- `StartOutboundLoop(ctx context.Context) error`

- [ ] **Step 7: 修改 StartForwarding，删除 outboundLoop 启动**

在 `StartForwarding` 中删除 `go mh.outboundLoop(ctx)` 行。

- [ ] **Step 8: 运行编译确认**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/swarm/gateway/message_handler/...`
Expected: 可能有调用方错误（app_gateway.go），暂忽略

- [ ] **Step 9: 提交**

```bash
git add internal/swarm/gateway/message_handler/
git commit -m "refactor(message_handler): HandleInbound→HandleMessage, enqueueOutbound→PublishRobotMessages, 新增 Consume/Consume/Nowait 方法, 删除 outboundLoop"
```

---

### Task 6: 更新 app_gateway.go 组装代码

**Files:**
- Modify: `internal/swarm/gateway/app_gateway.go`

- [ ] **Step 1: 修改 NewChannelManager 调用**

将：
```go
channelMgr := cm.NewChannelManager(nil, nil)
```
替换为：
```go
channelMgr := cm.NewChannelManager(msgHandler, msgHandler, nil, nil)
```

- [ ] **Step 2: 修改 Channel 注册方式**

将：
```go
s.channelMgr.Register(s.webChannel, nil)
```
替换为：
```go
s.channelMgr.RegisterChannelWithInbound(s.webChannel, onMessageCb)
```

- [ ] **Step 3: 修改 onMessageCb 闭包**

将：
```go
onMessageCb := func(msg *schema.Message) {
    if err := msgHandler.HandleInbound(context.Background(), msg); err != nil {
        logger.Warn(logComponentAppGateway).
            Err(err).
            Str("msg_id", msg.ID).
            Msg("HandleInbound 失败")
    }
}
```
替换为：
```go
onMessageCb := func(msg *schema.Message) {
    msgHandler.HandleMessage(msg)
}
```

- [ ] **Step 4: 在 Start() 中添加 StartDispatch**

在 `msgHandler.StartForwarding(ctx)` 调用之后添加：

```go
// 启动出站派发循环（对齐 Python channel_manager.start_dispatch()）
if s.channelMgr != nil {
    if err := s.channelMgr.StartDispatch(ctx); err != nil {
        logger.Error(logComponentAppGateway).
            Err(err).
            Msg("启动 ChannelManager 出站派发循环失败")
    }
}
```

- [ ] **Step 5: 在 Stop() 中添加 StopDispatch**

在 `msgHandler.StopForwarding()` 调用之前添加：

```go
// 停止出站派发循环（对齐 Python channel_manager.stop_dispatch()）
if s.channelMgr != nil {
    if err := s.channelMgr.StopDispatch(); err != nil {
        logger.Warn(logComponentAppGateway).
            Err(err).
            Msg("停止 ChannelManager 出站派发循环失败")
    }
}
```

- [ ] **Step 6: 运行编译确认**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/swarm/gateway/...`
Expected: 编译成功

- [ ] **Step 7: 提交**

```bash
git add internal/swarm/gateway/app_gateway.go
git commit -m "refactor(gateway): 更新 app_gateway 组装，ChannelManager 双接口注入 + RegisterChannelWithInbound + StartDispatch/StopDispatch"
```

---

### Task 7: 更新 channel_manager 测试

**Files:**
- Modify: `internal/swarm/gateway/channel_manager/channel_manager_test.go`

- [ ] **Step 1: 更新 stubMessageHandler 测试桩**

添加一个实现 InboundMessageHandler + RobotMessageConsumer 的测试桩：

```go
// stubMessageHandler 用于测试的 InboundMessageHandler + RobotMessageConsumer 桩实现
type stubMessageHandler struct {
	handledMessages []*schema.Message
	consumeQueue    []*schema.Message
	mu              sync.Mutex
}

func newStubMessageHandler() *stubMessageHandler {
	return &stubMessageHandler{}
}

func (s *stubMessageHandler) HandleMessage(msg *schema.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handledMessages = append(s.handledMessages, msg)
}

func (s *stubMessageHandler) ConsumeRobotMessages(timeout time.Duration) *schema.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.consumeQueue) > 0 {
		msg := s.consumeQueue[0]
		s.consumeQueue = s.consumeQueue[1:]
		return msg
	}
	return nil
}

func (s *stubMessageHandler) getHandledMessages() []*schema.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]*schema.Message, len(s.handledMessages))
	copy(result, s.handledMessages)
	return result
}

func (s *stubMessageHandler) enqueueConsume(msg *schema.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.consumeQueue = append(s.consumeQueue, msg)
}
```

- [ ] **Step 2: 更新所有现有测试**

将所有 `NewChannelManager(nil, nil)` 替换为 `NewChannelManager(newStubMessageHandler(), newStubMessageHandler(), nil, nil)`。

将所有 `cm.Register(ch, func(_ *schema.Message) {})` 替换为 `cm.RegisterChannel(ch)`。

将所有 `cm.Unregister(...)` 替换为 `cm.UnregisterChannel(...)`。

删除 `BroadcastToChannels` 相关测试。

更新 `stubChannel` 的 `onMsgCb` 测试逻辑以适配 `RegisterChannel` 的默认回调。

- [ ] **Step 3: 新增 RegisterChannelWithInbound 测试**

```go
func TestChannelManager_RegisterChannelWithInbound_自定义回调(t *testing.T) {
	mh := newStubMessageHandler()
	cm := NewChannelManager(mh, mh, nil, nil)
	ch := newStubChannel("web-001", ChannelTypeWeb)

	callbackCalled := false
	cm.RegisterChannelWithInbound(ch, func(_ *schema.Message) { callbackCalled = true })

	if cm.GetChannel("web-001") == nil {
		t.Error("RegisterChannelWithInbound 后 GetChannel 应返回非 nil")
	}
	if ch.onMsgCb == nil {
		t.Error("OnMessage 回调应已设置")
	}
	if ch.onMsgCb != nil {
		ch.onMsgCb(&schema.Message{ID: "test"})
		if !callbackCalled {
			t.Error("自定义回调应被触发")
		}
	}
}
```

- [ ] **Step 4: 新增 RegisterExternalChannel 测试**

```go
func TestChannelManager_RegisterExternalChannel_登记成功(t *testing.T) {
	mh := newStubMessageHandler()
	cm := NewChannelManager(mh, mh, nil, nil)
	ch := newStubChannel("external-001", ChannelTypeWeb)

	cm.RegisterExternalChannel("external-001", ch)
	if cm.GetChannel("external-001") == nil {
		t.Error("RegisterExternalChannel 后 GetChannel 应返回非 nil")
	}
}
```

- [ ] **Step 5: 新增 DeliverToMessageHandler 测试**

```go
func TestChannelManager_DeliverToMessageHandler_直接转发(t *testing.T) {
	mh := newStubMessageHandler()
	cm := NewChannelManager(mh, mh, nil, nil)

	msg := &schema.Message{ID: "test-msg", ChannelID: "web"}
	cm.DeliverToMessageHandler(msg)

	handled := mh.getHandledMessages()
	if len(handled) != 1 {
		t.Errorf("DeliverToMessageHandler 应转发 1 条消息, 实际 = %d", len(handled))
	}
}
```

- [ ] **Step 6: 新增 onChannelMessage 存活检查测试**

```go
func TestChannelManager_onChannelMessage_存活检查(t *testing.T) {
	mh := newStubMessageHandler()
	cm := NewChannelManager(mh, mh, nil, nil)
	ch := newStubChannel("web-001", ChannelTypeWeb)
	cm.RegisterChannel(ch)

	// Channel 存活时应转发
	msg := &schema.Message{ID: "msg-1", ChannelID: "web-001"}
	ch.onMsgCb(msg)
	handled := mh.getHandledMessages()
	if len(handled) != 1 {
		t.Errorf("存活 Channel 应转发消息, 实际 = %d", len(handled))
	}

	// 注销后应丢弃
	cm.UnregisterChannel("web-001")
	msg2 := &schema.Message{ID: "msg-2", ChannelID: "web-001"}
	ch.onMsgCb(msg2)
	handled = mh.getHandledMessages()
	if len(handled) != 1 {
		t.Errorf("已注销 Channel 应丢弃消息, 实际 = %d", len(handled))
	}
}
```

- [ ] **Step 7: 新增 StartDispatch/StopDispatch 测试**

```go
func TestChannelManager_StartDispatch_定向投递(t *testing.T) {
	mh := newStubMessageHandler()
	cm := NewChannelManager(mh, mh, nil, nil)
	ch := newStubChannel("web-001", ChannelTypeWeb)
	cm.RegisterChannel(ch)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := cm.StartDispatch(ctx)
	if err != nil {
		t.Fatalf("StartDispatch 返回错误: %v", err)
	}

	// 向 consumeQueue 放入一条消息
	outMsg := &schema.Message{ID: "out-1", ChannelID: "web-001"}
	mh.enqueueConsume(outMsg)

	// 等待投递
	time.Sleep(100 * time.Millisecond)

	sent := ch.getSentMsgs()
	if len(sent) != 1 {
		t.Errorf("应投递 1 条消息到 Channel, 实际 = %d", len(sent))
	}
}

func TestChannelManager_StopDispatch_停止(t *testing.T) {
	mh := newStubMessageHandler()
	cm := NewChannelManager(mh, mh, nil, nil)

	ctx := context.Background()
	cm.StartDispatch(ctx)
	err := cm.StopDispatch()
	if err != nil {
		t.Fatalf("StopDispatch 返回错误: %v", err)
	}
	if cm.running.Load() {
		t.Error("StopDispatch 后 running 应为 false")
	}
}
```

- [ ] **Step 8: 运行测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/gateway/channel_manager/... -v -count=1`
Expected: PASS

- [ ] **Step 9: 提交**

```bash
git add internal/swarm/gateway/channel_manager/channel_manager_test.go
git commit -m "test(channel_manager): 更新测试适配新 API，新增 RegisterChannelWithInbound/External/Deliver/Dispatch/存活检查测试"
```

---

### Task 8: 更新 message_handler 测试

**Files:**
- Modify: `internal/swarm/gateway/message_handler/message_handler_test.go`
- Modify: `internal/swarm/gateway/message_handler/forward_loop_test.go`

- [ ] **Step 1: 更新 message_handler_test.go**

将所有 `HandleInbound` 测试改为 `HandleMessage`：
- `TestHandleInbound_正常入队` → `TestHandleMessage_正常入队`
- `TestHandleInbound_空消息` → `TestHandleMessage_空消息`
- 签名从 `mh.HandleInbound(context.Background(), msg)` 改为 `mh.HandleMessage(msg)`
- 去掉 error 返回值检查

将 `enqueueOutbound` 测试改为 `PublishRobotMessages`。

- [ ] **Step 2: 新增 ConsumeRobotMessages/ConsumeUserMessages/PublishUserMessagesNowait 测试**

```go
func TestMessageHandler_ConsumeRobotMessages_超时返回nil(t *testing.T) {
	mh := NewMessageHandler(nil, nil)
	msg := mh.ConsumeRobotMessages(10 * time.Millisecond)
	if msg != nil {
		t.Error("空队列超时应返回 nil")
	}
}

func TestMessageHandler_ConsumeRobotMessages_正常消费(t *testing.T) {
	mh := NewMessageHandler(nil, nil)
	testMsg := &schema.Message{ID: "test"}
	mh.PublishRobotMessages(testMsg)
	msg := mh.ConsumeRobotMessages(100 * time.Millisecond)
	if msg == nil || msg.ID != "test" {
		t.Error("应消费到已写入的消息")
	}
}

func TestMessageHandler_ConsumeUserMessages_超时返回nil(t *testing.T) {
	mh := NewMessageHandler(nil, nil)
	msg := mh.ConsumeUserMessages(10 * time.Millisecond)
	if msg != nil {
		t.Error("空队列超时应返回 nil")
	}
}

func TestMessageHandler_PublishUserMessagesNowait_正常写入(t *testing.T) {
	mh := NewMessageHandler(nil, nil)
	testMsg := &schema.Message{ID: "test"}
	mh.PublishUserMessagesNowait(testMsg)
	msg := mh.ConsumeUserMessages(100 * time.Millisecond)
	if msg == nil || msg.ID != "test" {
		t.Error("应消费到已写入的消息")
	}
}
```

- [ ] **Step 3: 更新 forward_loop_test.go**

适配 `enqueueOutbound` → `PublishRobotMessages` 重命名。

- [ ] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/gateway/message_handler/... -v -count=1`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/swarm/gateway/message_handler/message_handler_test.go internal/swarm/gateway/message_handler/forward_loop_test.go
git commit -m "test(message_handler): 更新测试适配 HandleMessage/PublishRobotMessages, 新增 Consume/Nowait 测试"
```

---

### Task 9: 更新 doc.go 文件

**Files:**
- Modify: `internal/swarm/gateway/channel_manager/doc.go`
- Modify: `internal/swarm/gateway/message_handler/doc.go`

- [ ] **Step 1: 更新 channel_manager/doc.go**

更新文件目录和包描述以反映新增接口和方法。

- [ ] **Step 2: 更新 message_handler/doc.go**

更新描述以反映方法重命名和新增方法。

- [ ] **Step 3: 提交**

```bash
git add internal/swarm/gateway/channel_manager/doc.go internal/swarm/gateway/message_handler/doc.go
git commit -m "docs: 更新 channel_manager/message_handler doc.go"
```

---

### Task 10: 全量编译 + 测试 + IMPLEMENTATION_PLAN 更新

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 全量编译**

Run: `cd /home/opensource/uapclaw-gateway && go build ./...`
Expected: 编译成功

- [ ] **Step 2: 全量测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/gateway/... -count=1`
Expected: PASS

- [ ] **Step 3: 更新 IMPLEMENTATION_PLAN.md 中 11.1 和 11.2 状态**

确认 11.1 和 11.2 仍为 ✅（已完成后对齐修复）。

- [ ] **Step 4: 提交**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "chore: 更新 IMPLEMENTATION_PLAN 11.1/11.2 对齐完成确认"
```
