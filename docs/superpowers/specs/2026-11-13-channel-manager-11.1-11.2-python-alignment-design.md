# 11.1+11.2 ChannelManager 对齐 Python 修复设计

## 背景

11.1 BaseChannel 接口和 11.2 ChannelManager 已实现，但经逐项对比 Python 源码
(`jiuwenswarm/gateway/channel_manager/base.py` + `channel_manager.py`)，发现以下差异需修复：

1. **RobotMessageRouter 缺失** → 确认不实现（Python 死代码，所有 Channel 注入 `_DummyBus`）
2. **ChannelManager 缺少 MessageHandler 引用** → 构造需新增 `messageHandler` 参数
3. **入站缺少 ChannelManager 中转** → 缺存活检查 + 日志
4. **注册方法不对齐** → Python 有 4 种注册方式，Go 只有 1 种
5. **出站循环归属错误** → 在 MessageHandler，应在 ChannelManager
6. **投递策略错误** → 广播，应为按 channel_id 定向投递
7. **缺少 cron 投递失败通知** → `_notify_cron_delivery_error`
8. **MessageHandler 方法名不对齐** → `HandleInbound`/`enqueueOutbound` 等

## 设计决策

### 决策 1：不实现 RobotMessageRouter

Python 的 `RobotMessageRouter` 在 Gateway 实际部署中从未被实例化，
所有 Channel 注入的是 `_DummyBus()`（no-op 空壳）。
入站走 `ChannelManager._on_channel_message` → `MessageHandler.handle_message`，
出站走 `ChannelManager._dispatch_robot_messages` 消费 `MessageHandler.consume_robot_messages()`。
Go 用 `OnMessage` 回调 + ChannelManager 出站循环已等价覆盖，无需额外类型。

### 决策 2：循环依赖处理

`channel_manager` 不能直接 import `message_handler`（会形成循环依赖：
`message_handler` → `channel_manager` → `message_handler`）。

**方案：** 在 `channel_manager` 包中定义最小接口 `RobotMessageConsumer`，
由 `MessageHandler` 隐式实现。接口仅包含出站消费所需的方法。

```go
// RobotMessageConsumer 出站消息消费接口，由 MessageHandler 实现。
//
// 避免 channel_manager → message_handler 循环依赖。
type RobotMessageConsumer interface {
    // ConsumeRobotMessages 从出站队列消费一条消息，超时返回 nil。
    // 对齐 Python MessageHandler.consume_robot_messages
    ConsumeRobotMessages(timeout time.Duration) *schema.Message
}
```

## 修改清单

### 一、channel_manager/channel_manager.go

#### 结构体变更

```go
type ChannelManager struct {
    channels              map[string]BaseChannel
    config                map[string]map[string]any
    onConfigUpdated       OnConfigUpdatedFunc
    pendingChannelRestart map[string]struct{}
    // ─── 新增 ───
    messageHandler        RobotMessageConsumer   // 对齐 Python _message_handler
    running               atomic.Bool            // 对齐 Python _running
    dispatchCancel        context.CancelFunc     // 对齐 Python _dispatch_task
    mu                    sync.RWMutex
}
```

#### 构造函数变更

```go
// 旧：NewChannelManager(config, onConfigUpdated)
// 新：NewChannelManager(messageHandler, config, onConfigUpdated)
func NewChannelManager(
    messageHandler RobotMessageConsumer,
    config map[string]map[string]any,
    onConfigUpdated OnConfigUpdatedFunc,
) *ChannelManager
```

#### 删除方法

| 方法 | 原因 |
|------|------|
| `Register(ch, onMessageCallback)` | 替换为 `RegisterChannel` |
| `Unregister(channelID)` | 重命名为 `UnregisterChannel` |
| `BroadcastToChannels(ctx, msg)` | Python 无对应方法，出站走定向投递 |

#### 新增方法

| Go 方法名 | Python 对齐 | 导出 | 签名 |
|----------|-----------|------|------|
| `RegisterChannel` | `register_channel` | ✓ | `(ch BaseChannel)` |
| `RegisterChannelWithInbound` | `register_channel_with_inbound` | ✓ | `(ch BaseChannel, onMessage func(*schema.Message))` |
| `RegisterExternalChannel` | `register_external_channel` | ✓ | `(channelID string, ch BaseChannel)` |
| `DeliverToMessageHandler` | `deliver_to_message_handler` | ✓ | `(msg *schema.Message)` |
| `UnregisterChannel` | `unregister_channel` | ✓ | `(channelID string)` |
| `StartDispatch` | `start_dispatch` | ✓ | `(ctx context.Context) error` |
| `StopDispatch` | `stop_dispatch` | ✓ | `() error` |
| `onChannelMessage` | `_on_channel_message` | ✗ | `(msg *schema.Message)` |
| `dispatchRobotMessages` | `_dispatch_robot_messages` | ✗ | `(ctx context.Context)` |
| `notifyCronDeliveryError` | `_notify_cron_delivery_error` | ✗ | `(originalMsg *schema.Message, err error)` |

#### 关键方法实现逻辑

**`RegisterChannel(ch)`：**
```go
func (cm *ChannelManager) RegisterChannel(ch BaseChannel) {
    cm.mu.Lock()
    defer cm.mu.Unlock()
    cid := ch.ChannelID()
    cm.channels[cid] = ch
    ch.OnMessage(cm.onChannelMessage)  // 默认回调：存活检查+转发
    logger.Info(logComponent).Str("channel_id", cid).Int("total", len(cm.channels)).Msg("已注册 Channel")
}
```

**`onChannelMessage(msg)`：**
```go
// 对齐 Python _on_channel_message
func (cm *ChannelManager) onChannelMessage(msg *schema.Message) {
    logger.Info(logComponent).Str("msg_id", msg.ID).Str("channel_id", msg.ChannelID).
        Msg("Channel 消息 -> MessageHandler")
    // 存活检查
    cm.mu.RLock()
    _, exists := cm.channels[msg.ChannelID]
    cm.mu.RUnlock()
    if !exists {
        logger.Info(logComponent).Str("channel_id", msg.ChannelID).
            Msg("Channel 已关闭，丢弃此消息")
        return
    }
    cm.messageHandler.HandleMessage(msg)
}
```

注意：`onChannelMessage` 需要调 `messageHandler.HandleMessage(msg)`，
但 `RobotMessageConsumer` 接口只定义了 `ConsumeRobotMessages`。
需要扩展接口或另定义 `MessageHandler` 最小接口。

**接口拆分方案：**
```go
// InboundMessageHandler 入站消息处理接口，由 MessageHandler 实现。
type InboundMessageHandler interface {
    // HandleMessage 处理入站消息（写入 userMessages 队列）。
    // 对齐 Python MessageHandler.handle_message
    HandleMessage(msg *schema.Message)
}

// RobotMessageConsumer 出站消息消费接口，由 MessageHandler 实现。
type RobotMessageConsumer interface {
    // ConsumeRobotMessages 从出站队列消费一条消息，超时返回 nil。
    // 对齐 Python MessageHandler.consume_robot_messages
    ConsumeRobotMessages(timeout time.Duration) *schema.Message
}
```

ChannelManager 持有两个接口引用：
```go
type ChannelManager struct {
    // ...
    inboundHandler  InboundMessageHandler   // 入站：DeliverToMessageHandler / onChannelMessage 用
    consumeProvider RobotMessageConsumer     // 出站：dispatchRobotMessages 用
    // ...
}
```

构造函数：
```go
func NewChannelManager(
    inboundHandler InboundMessageHandler,
    consumeProvider RobotMessageConsumer,
    config map[string]map[string]any,
    onConfigUpdated OnConfigUpdatedFunc,
) *ChannelManager
```

**`DeliverToMessageHandler(msg)`：**
```go
// 对齐 Python deliver_to_message_handler：直接转发，不做存活检查
func (cm *ChannelManager) DeliverToMessageHandler(msg *schema.Message) {
    cm.inboundHandler.HandleMessage(msg)
}
```

**`dispatchRobotMessages(ctx)`：**
```go
// 对齐 Python _dispatch_robot_messages：按 channel_id 定向投递
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
                logger.Error(logComponent).Str("channel_id", msg.ChannelID).Err(err).
                    Msg("投递消息到 Channel 失败")
                // cron 投递失败通知
                if strings.HasPrefix(msg.ID, "cron-push-") {
                    cm.notifyCronDeliveryError(msg, err)
                }
            }
        } else {
            logger.Warn(logComponent).Str("channel_id", msg.ChannelID).Str("msg_id", msg.ID).
                Msg("未找到 Channel，丢弃 robot_messages")
        }
    }
}
```

**`notifyCronDeliveryError(originalMsg, err)`：**
```go
// 对齐 Python _notify_cron_delivery_error
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

**`StartDispatch(ctx)` / `StopDispatch()`：**
```go
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

### 二、channel_manager/base.go

无修改。BaseChannel 接口和 ChannelType 枚举已完全对齐。

### 三、message_handler/message_handler.go

#### 方法名重命名

| 旧名 | 新名 | Python 对齐 |
|------|------|-----------|
| `HandleInbound(ctx, msg)` | `HandleMessage(msg)` | `handle_message` |
| `enqueueOutbound(msg)` | `PublishRobotMessages(msg)` | `publish_robot_messages` |

注意：`HandleInbound` 当前签名为 `(ctx context.Context, msg *schema.Message) error`，
Python 的 `handle_message` 签名为 `(msg: Message) -> None`。
Go 改为 `HandleMessage(msg *schema.Message)` 去掉 ctx 和 error 返回值，
对齐 Python 同步回调语义（非阻塞写入，满时丢弃仅记日志）。

#### 新增方法

| Go 方法名 | Python 对齐 | 签名 |
|----------|-----------|------|
| `ConsumeRobotMessages` | `consume_robot_messages` | `(timeout time.Duration) *schema.Message` |
| `ConsumeUserMessages` | `consume_user_messages` | `(timeout time.Duration) *schema.Message` |
| `PublishUserMessagesNowait` | `publish_user_messages_nowait` | `(msg *schema.Message)` |

**`ConsumeRobotMessages` 实现：**
```go
func (mh *MessageHandler) ConsumeRobotMessages(timeout time.Duration) *schema.Message {
    select {
    case msg := <-mh.robotMessages:
        return msg
    case <-time.After(timeout):
        return nil
    }
}
```

**`ConsumeUserMessages` 实现：**
```go
func (mh *MessageHandler) ConsumeUserMessages(timeout time.Duration) *schema.Message {
    select {
    case msg := <-mh.userMessages:
        return msg
    case <-time.After(timeout):
        return nil
    }
}
```

**`PublishUserMessagesNowait` 实现：**
```go
func (mh *MessageHandler) PublishUserMessagesNowait(msg *schema.Message) {
    if msg == nil {
        return
    }
    select {
    case mh.userMessages <- msg:
    default:
        logger.Warn(logComponent).Str("msg_id", msg.ID).Msg("入站消息队列已满，丢弃消息")
    }
}
```

#### 删除方法

| 旧名 | 原因 |
|------|------|
| `outboundLoop(ctx)` | 出站循环移到 ChannelManager.dispatchRobotMessages |
| `dispatchOutbound(ctx, msg)` | 投递逻辑移到 ChannelManager |
| `StartOutboundLoop(ctx)` | 改用 ChannelManager.StartDispatch |

#### StartForwarding 修改

删除 `go mh.outboundLoop(ctx)` 调用，出站循环由 ChannelManager.StartDispatch 启动。

```go
func (mh *MessageHandler) StartForwarding(ctx context.Context) error {
    // ...
    go mh.forwardLoop(ctx)
    // 删除：go mh.outboundLoop(ctx)
    // 注册 push 回调
    // ...
}
```

### 四、message_handler/forward_loop.go

所有 `enqueueOutbound(msg)` 调用点 → `PublishRobotMessages(msg)`

### 五、app_gateway.go 组装更新

```go
// 旧：
channelMgr := cm.NewChannelManager(nil, nil)
// 新：
channelMgr := cm.NewChannelManager(msgHandler, msgHandler, nil, nil)
// msgHandler 同时实现 InboundMessageHandler 和 RobotMessageConsumer

// 旧：
s.channelMgr.Register(s.webChannel, nil)
// 新：
s.channelMgr.RegisterChannelWithInbound(s.webChannel, onMessageCb)

// Start() 中新增：
s.channelMgr.StartDispatch(ctx)

// Stop() 中新增：
s.channelMgr.StopDispatch()
```

### 六、测试更新

- `channel_manager_test.go`：更新所有测试用 RegisterChannel 替代 Register，
  新增 RegisterChannelWithInbound / RegisterExternalChannel / DeliverToMessageHandler /
  StartDispatch / StopDispatch / 定向投递 / cron 通知测试
- `message_handler_test.go`：新增 ConsumeRobotMessages / ConsumeUserMessages /
  PublishUserMessagesNowait / PublishRobotMessages 测试

## Python 方法对齐映射表

### ChannelManager

| Python 方法 | Go 方法 | 说明 |
|------------|--------|------|
| `__init__(message_handler, config, on_config_updated)` | `NewChannelManager(inboundHandler, consumeProvider, config, onConfigUpdated)` | 构造 |
| `register_channel(channel)` | `RegisterChannel(ch)` | 默认回调 |
| `register_channel_with_inbound(channel, on_message)` | `RegisterChannelWithInbound(ch, onMessage)` | 自定义入站 |
| `register_external_channel(channel_id, channel)` | `RegisterExternalChannel(channelID, ch)` | 仅登记 |
| `deliver_to_message_handler(msg)` | `DeliverToMessageHandler(msg)` | 直接转发 |
| `unregister_channel(channel_id)` | `UnregisterChannel(channelID)` | 注销 |
| `get_channel(channel_id)` | `GetChannel(channelID)` | 查找 |
| `enabled_channels` (property) | `GetEnabledChannels()` | 列表 |
| `get_conf(channel_id)` | `GetConf(channelID)` | 获取配置 |
| `set_conf(channel_id, new_conf)` | `SetConf(channelID, newConf)` | 更新单渠道配置 |
| `set_config(new_conf)` | `SetConfig(newConf)` | 整体替换配置 |
| `set_config_callback(callback)` | `SetConfigCallback(callback)` | 设置回调 |
| `mark_channel_restart_pending(channel_id)` | `MarkChannelRestartPending(channelID)` | 标记待重启 |
| `pop_channel_restart_pending()` | `PopChannelRestartPending()` | 取出待重启集合 |
| `start_dispatch()` | `StartDispatch(ctx)` | 启动出站派发 |
| `stop_dispatch()` | `StopDispatch()` | 停止出站派发 |
| `_on_channel_message(msg)` | `onChannelMessage(msg)` | 默认入站回调 |
| `_dispatch_robot_messages()` | `dispatchRobotMessages(ctx)` | 出站派发循环 |
| `_notify_cron_delivery_error(msg, err)` | `notifyCronDeliveryError(msg, err)` | cron 失败通知 |

### MessageHandler（涉及 11.2 对齐的方法）

| Python 方法 | Go 方法 | 说明 |
|------------|--------|------|
| `handle_message(msg)` | `HandleMessage(msg)` | 入站写入 userMessages |
| `publish_robot_messages(msg)` | `PublishRobotMessages(msg)` | 出站写入 robotMessages |
| `publish_robot_messages_nowait(msg)` | `PublishRobotMessagesNowait(msg)` | 同步出站写入 |
| `consume_robot_messages(timeout)` | `ConsumeRobotMessages(timeout)` | 消费出站队列 |
| `consume_user_messages(timeout)` | `ConsumeUserMessages(timeout)` | 消费入站队列 |
| `publish_user_messages_nowait(msg)` | `PublishUserMessagesNowait(msg)` | 同步入站写入 |
| `start_forwarding()` | `StartForwarding(ctx)` | 启动入站转发 |
| `stop_forwarding()` | `StopForwarding()` | 停止转发 |
