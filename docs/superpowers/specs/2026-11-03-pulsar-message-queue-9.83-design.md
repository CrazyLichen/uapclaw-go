# 9.83 Pulsar Message Queue 实现设计

> 状态：Draft
> 日期：2026-11-03
> 章节：9.83 Pulsar Message Queue
> Python 源码：`openjiuwen/extensions/message_queue/message_queue_pulsar.py`（208 行）
> 前置引用源码：`openjiuwen/core/runner/drunner/dmessage_queue/{message.py, message_serializer.py}`

## 1. 概述

实现分布式 Runner 的消息队列传输层——Pulsar 后端。本节只交付**管道本身**：topic 订阅、消息生产、producer LRU 缓存、消费循环、生命周期管理，以及两个被硬依赖的前置件（Dmq 消息类型、消息编解码器）。

### 1.1 在 Agent 会话中的流程位置与作用

本节处于两个层面的交界处，自身不含任何业务语义。

**① Runner 生命周期层（进程级单例）**

```
Runner.start()   [distributed_mode == true]                    ← runner.go:152（9.83 不接线）
  ├─ 步骤3   _distribute_message_queue = MessageQueueFactory.create(...)
  │          _distribute_message_queue.start()                  ← 9.83 在这里被构造、启动
  │          system_reply_sub = ReplyTopicSubscription(mq)
  └─ 步骤4   本地 _message_queue.start()（内存版，仅分布式分支才启）
Runner.stop()
  ├─ 步骤1   _distribute_message_queue.stop()                   ← 9.83 在这里被关闭
  └─ 步骤2   本地 _message_queue.stop()
```

**② 跨进程 Agent 调用层（真正的用途）**

```
客户端 MqRemoteClient                Pulsar topic                       Runner AgentServerAdapter
  invoke(agent_id, payload) ──DmqRequestMessage(type=INPUT)──▶  openjiuwen.single_agent.{agent_id}.{version}
                                                                        ↓ 本地执行 Agent
  回调 / 流式 chunk         ◀──DmqResponseMessage(type=OUTPUT)──  openjiuwen.reply.runner.{instance_id}
                                 seq / last_chunk 标记流式分片
  中断                      ──DmqRequestMessage(type=STOP)─────▶
```

**一句话定位**：9.83 是分布式 Runner 体系的传输管道（transport），把 agent 执行能力暴露成 MQ 上的服务与订阅。谁在使用这条管道是 9.84 的事。

### 1.2 本节范围

**做**

- `MessageQueuePulsar` + `PulsarSubscription`（对齐 `message_queue_pulsar.py` 的 2 个类）
- 三个窄接口 + 真实 Pulsar SDK 适配器（可测性支撑）
- `DmqRequestMessage` / `DmqResponseMessage` / `DMessageType` / `ResultType`（提前落位 9.84）
- `SerializeMessage` / `DeserializeMessage`（提前落位 9.84）

**不做**（全部留给 9.84）

- `MessageQueueFactory`、`FakeMQ`
- `dsubscription/`（`reply_topic_subscription`、`response_collector`）
- `server_adapter/`（`mq_server_adapter`、`agent_adapter`、`mq_message_utils`）
- `remote_client/`（`mq_remote_clent`、`remote_agent`、`remote_client_config`）
- `runner.go` 步骤 3 / 步骤 1 两个预留点的回填
- 真实 broker 的连接验证（需环境）

## 2. 关键设计决策

| # | 决策点 | 选定 | 理由 |
|---|---|---|---|
| K1 | 实现范围 | 只做 Pulsar 后端 + 最小前置件（Dmq 类型 + serializer）；工厂/FakeMQ/适配器全留 9.84 | serializer 是 `produce_message` 第一行的硬依赖，无法绕过；工厂等无反向依赖 |
| K2 | 包路径 | `internal/agentcore/message_queue/`，提前建 `internal/agentcore/runner/drunner/dmessage_queue/` | 与既有 `extensions/context_evolver → agentcore/context_evolver` 扁平化惯例一致，与 Python 两级路径一一对应 |
| K3 | `PulsarConfig.MaxWorkers` | 保留字段，Go 侧不消费 | Python 的 ThreadPoolExecutor 只为把阻塞调用挪出事件循环；goroutine 本身由 runtime 调度，无 executor 等价物 |
| K4 | 反序列化返回类型 | `DeserializeMessage` 返回 `QueueMessageBase`（强类型） | 9.84 消费方需要 `reply_topic` / `request_id` 强类型访问；到 handler 边界再转 map |
| K5 | payload 内部多态 | `Payload` 固定 `map[string]any`，内部不重建类型 | Go 无 pydantic 多态；`__class__` 键原样保留，往返仍与 Python 互通 |
| K6 | SDK 引入 | `github.com/apache/pulsar-client-go` + 包内窄接口 | 真实连接无法单测；窄接口让 LRU/生命周期/消费循环/错误包装可用 fake 覆盖 |
| K7 | 并发模型 | 每 topic 一条 goroutine，无 executor | 语义与 Python 一致（Python 也是每 topic 一条 consume_loop） |

## 3. 前置件一：Dmq 消息类型

文件：`internal/agentcore/runner/drunner/dmessage_queue/message.go`
Python：`openjiuwen/core/runner/drunner/dmessage_queue/message.py`

### 3.1 枚举

```go
// DMessageType 分布式消息类型
type DMessageType string

const (
    // DMessageTypeInput 输入消息（发起 agent 调用）
    DMessageTypeInput DMessageType = "INPUT"
    // DMessageTypeStop 中断消息
    DMessageTypeStop DMessageType = "STOP"
    // DMessageTypeOutput 输出消息（返回执行结果）
    DMessageTypeOutput DMessageType = "OUTPUT"
)

// ResultType 响应结果类型
type ResultType string

const (
    // ResultTypeMessage 正常消息结果
    ResultTypeMessage ResultType = "MESSAGE"
    // ResultTypeError 错误结果
    ResultTypeError ResultType = "ERROR"
)
```

### 3.2 结构体

内嵌已交付的 `message_queue.QueueMessage`（4 字段恰好等价 Python `QueueMessage` 基类）。

```go
// DmqRequestMessage 分布式请求消息
//
// Python: DmqRequestMessage(DmqMessage)
type DmqRequestMessage struct {
    // QueueMessage 内嵌基础消息字段（message_id/payload/error_code/error_msg）
    message_queue.QueueMessage
    // Type 消息类型，默认 INPUT
    Type DMessageType
    // ReplyTopic 响应回写 topic
    ReplyTopic string
    // RequestID 请求唯一标识
    RequestID string
    // SenderID 发送方标识
    SenderID string
    // ReceiverID 接收方标识
    ReceiverID string
    // EnableStream 是否流式
    EnableStream bool
    // ExpireAt 过期时间戳，可选
    ExpireAt *float64
}

// DmqResponseMessage 分布式响应消息
//
// Python: DmqResponseMessage(DmqMessage)
type DmqResponseMessage struct {
    // QueueMessage 内嵌基础消息字段
    message_queue.QueueMessage
    // Type 消息类型，默认 OUTPUT
    Type DMessageType
    // ResultType 结果类型，默认 MESSAGE
    ResultType ResultType
    // RequestID 请求唯一标识（与请求对应）
    RequestID string
    // SenderID 发送方标识
    SenderID string
    // ReceiverID 接收方标识
    ReceiverID string
    // Seq 流式分片序号
    Seq int
    // LastChunk 是否为流式最后一片
    LastChunk bool
    // ExpireAt 过期时间戳，可选
    ExpireAt *float64
}
```

构造函数 `NewDmqRequestMessage(payload map[string]any) *DmqRequestMessage` 与 `NewDmqResponseMessage(...)`，默认值按 Python pydantic 默认填：`Type=DMessageTypeInput` / `Type=DMessageTypeOutput`、`ResultType=ResultTypeMessage`。

### 3.3 `QueueMessage` 补 json tag

`internal/agentcore/runner/message_queue/base.go` 的 `QueueMessage` 需补 4 个 tag，否则 `json.Marshal` 会输出 `MessageID` 而非 `message_id`：

```go
type QueueMessage struct {
    MessageID string         `json:"message_id"`
    Payload   map[string]any `json:"payload"`
    ErrorCode int            `json:"error_code"`
    ErrorMsg  string         `json:"error_msg"`
}
```

纯新增，不改变任何现有行为。`InvokeQueueMessage` / `StreamQueueMessage` 不加 tag（不走 wire）。

## 4. 前置件二：序列化器

文件：`internal/agentcore/runner/drunner/dmessage_queue/message_serializer.go`
Python：`openjiuwen/core/runner/drunner/dmessage_queue/message_serializer.py`

### 4.1 为什么要这一层

`Produce` 拿到的是对象，Pulsar 只认 `[]byte`，中间必须有编解码。三个必须手写而非直接 `json.Marshal` 的原因：

1. **类型信息**：裸 JSON 反序列化后是字典，分不清 INPUT/OUTPUT/STOP，取不到 `reply_topic` / `seq` / `last_chunk`。Python 在每个对象上盖 `__class__` 标签，Go 必须照写才能与 Python 互通。
2. **datetime**：`_json_serializer` 把 `datetime` 编成 `{"__type__": "datetime", "value": iso}`，不带这个特例 `json.dumps` 直接抛 `TypeError`。
3. **深度限制**：`MAX_RECURSE_DEPTH = 10`，防循环引用与恶意深嵌套。

### 4.2 函数签名

```go
// MaxRecurseDepth payload 递归深度上限（对齐 Python MAX_RECURSE_DEPTH）
const MaxRecurseDepth = 10

// SerializeMessage 将 Dmq 消息序列化为 wire 字节
//
// Python: serialize_message(msg)
func SerializeMessage(msg message_queue.QueueMessageBase) ([]byte, error)

// DeserializeMessage 将 wire 字节反序列化为强类型 Dmq 消息
//
// Python: deserialize_message(data)
func DeserializeMessage(data []byte) (message_queue.QueueMessageBase, error)
```

Go 补充的导出辅助（Python 无对应函数，因其 `_serialize_payload` 直接产出 dict；Go 侧需要它把「写侧」的 map 形态复用到 handler 边界）：

```go
// MessageToMap 将消息对象转为带 __class__ 标记的 map
//
// Go 独有辅助：写侧 SerializeMessage 复用它，读侧 consumeLoop 用同一形态
// 把强类型消息交给 handler（handler 契约为 map[string]any）。
func MessageToMap(msg message_queue.QueueMessageBase) (map[string]any, error)
```

内部辅助（非导出）：

```go
// serializePayload 递归处理 payload 值，注入 datetime 特例并做深度检查
func serializePayload(v any, depth int) (any, error)
```

### 4.3 写出规则（`MessageToMap`）

显式类型 switch，不用反射：

```go
switch m := msg.(type) {
case *DmqRequestMessage:
    return map[string]any{
        "__class__":     "DmqRequestMessage",
        "message_id":    m.MessageID,
        "payload":       payload,
        "error_code":    m.ErrorCode,
        "error_msg":     m.ErrorMsg,
        "type":          string(m.Type),
        "reply_topic":   m.ReplyTopic,
        "request_id":    m.RequestID,
        "sender_id":     m.SenderID,
        "receiver_id":   m.ReceiverID,
        "enable_stream": m.EnableStream,
        "expire_at":     m.ExpireAt,
    }, nil
case *DmqResponseMessage:
    // 同上，字段为 type/result_type/request_id/sender_id/receiver_id/seq/last_chunk/expire_at
default:
    // 见偏差 D6
    return nil, exception.BuildError(
        exception.StatusMessageQueueTopicMessageProductionError,
        exception.WithParam("reason", "unsupported message type for pulsar wire"),
        exception.WithParam("message", msg),
    )
}
```

`payload` 递归规则（对齐 `_serialize_payload`）：

| 输入类型 | 输出 |
|---|---|
| `nil` | `nil` |
| `time.Time` | `map[string]any{"__type__": "datetime", "value": RFC3339}` |
| `map[string]any` | 逐键递归 |
| `[]any` | 逐元素递归 |
| 其它（string/int/float64/bool 等） | 原样 |

深度 `> MaxRecurseDepth` 返回 error（对齐 Python `RecursionError`）。

### 4.4 读入规则（`DeserializeMessage`）

单次解析 + 顶层探类型：

```go
var probe struct {
    Class string `json:"__class__"`
}
if err := json.Unmarshal(data, &probe); err != nil {
    return nil, err
}

switch probe.Class {
case "DmqRequestMessage":
    var m DmqRequestMessage
    if err := json.Unmarshal(data, &m); err != nil {
        return nil, err
    }
    if m.Type == "" { m.Type = DMessageTypeInput }   // 补 pydantic 默认值
    return &m, nil
case "DmqResponseMessage":
    var m DmqResponseMessage
    if err := json.Unmarshal(data, &m); err != nil {
        return nil, err
    }
    if m.Type == ""       { m.Type = DMessageTypeOutput }
    if m.ResultType == "" { m.ResultType = ResultTypeMessage }
    return &m, nil
default:
    // 对齐 Python: ValueError(f"Unknown payload class: {class_name}")
    return nil, fmt.Errorf("unknown payload class: %s", probe.Class)
}
```

`__class__` 是 struct 不认识的字段，`json.Unmarshal` 自动忽略，无需处理。**payload 内部不做类型重建**（偏差 D4），`__class__` 键原样留在 `map[string]any` 里——因此 Go 收进来再发出去，wire 仍与 Python 兼容。

## 5. MessageQueuePulsar

文件：`internal/agentcore/message_queue/pulsar.go`
Python：`MessageQueuePulsar`（`message_queue_pulsar.py` L86-208）

### 5.1 结构体

```go
// MessageQueuePulsar Pulsar 消息队列封装
//
// Python: MessageQueuePulsar(MessageQueueBase)
type MessageQueuePulsar struct {
    // url Pulsar 服务地址
    url string
    // maxWorkers 工作线程数（仅保存，Go 侧不消费，见偏差 D1）
    maxWorkers int
    // maxProducers producer LRU 容量上限，默认 MaxProducers
    maxProducers int
    // client Pulsar 客户端窄接口
    client pulsarClientAPI
    // producers topic → LRU 链表节点
    producers map[string]*list.Element
    // producerOrder producer LRU 顺序（Front 最旧）
    producerOrder *list.List
    // subs topic → 订阅
    subs map[string]*PulsarSubscription
    // mu 保护 producers / producerOrder / subs（单锁，见偏差 D2）
    mu sync.Mutex
    // running 是否运行中
    running atomic.Bool
}

// MaxProducers producer LRU 容量上限
//
// Python: MessageQueuePulsar.MAX_PRODUCERS = 10000
const MaxProducers = 10000

// DefaultSubscriptionName 默认订阅名
//
// Python: MessageQueuePulsar.DEFAULT_SUBSCRIPTION_NAME = "default"
const DefaultSubscriptionName = "default"
```

`maxProducers` 做成字段而非直接用常量，是为了让同包测试能改小以覆盖 LRU 淘汰分支（10000 条无法在单测里跑）。

### 5.2 窄接口层（`pulsar_api.go`）

```go
// pulsarClientAPI Pulsar 客户端窄接口
type pulsarClientAPI interface {
    // CreateProducer 创建指定 topic 的 producer
    CreateProducer(topic string) (pulsarProducerAPI, error)
    // Subscribe 订阅 topic，返回 KeyShared 消费者
    Subscribe(topic string, subscriptionName string) (pulsarConsumerAPI, error)
    // Close 关闭客户端
    Close()
}

// pulsarConsumerAPI Pulsar 消费者窄接口
type pulsarConsumerAPI interface {
    // Receive 阻塞接收一条消息，ctx 到期返回超时错误
    Receive(ctx context.Context) (pulsarMessageAPI, error)
    // Ack 确认消息
    Ack(msg pulsarMessageAPI) error
    // Close 关闭消费者
    Close()
}

// pulsarProducerAPI Pulsar 生产者窄接口
type pulsarProducerAPI interface {
    // Send 发送消息，partitionKey 决定分区与顺序
    Send(ctx context.Context, payload []byte, partitionKey string) error
    // Close 关闭生产者
    Close()
}

// pulsarMessageAPI Pulsar 消息窄接口
type pulsarMessageAPI interface {
    // Payload 返回消息体
    Payload() []byte
}
```

真实实现 `sdkClient` / `sdkConsumer` / `sdkProducer` / `sdkMessage` 包装 `github.com/apache/pulsar-client-go/pulsar`，全部非导出。

> **实现时必须先校核真实 SDK API**，不得照抄本设计的假设签名。历史教训：Milvus SDK v2.6.5 有八项签名与文档不符（`ReadProto`、bytes schema、`IndexType()` 恒空等）。重点确认 `Consumer.Receive` 是否接受 `context.Context`、`Consumer.Ack` 签名、`Producer.Send` 的 `SendOption` 形态、`SubscriptionType`/`ConsumerOptions` 字段名。

### 5.3 Start / Stop

```go
// Start 启动消息队列
//
// Python: MessageQueuePulsar.start() L101-107
func (q *MessageQueuePulsar) Start() {
    if q.running.Load() {
        return
    }
    q.client = newSDKClient(q.url)   // 对齐 Python: pulsar.Client(self._url)
    q.running.Store(true)
    logger.Info(logComponent).Str("event_type", "mq_pulsar_started").
        Str("url", q.url).Msg("pulsar message queue started")
}
```

**注意**：`NewMessageQueuePulsar` 不建 client，`start()` 才建——严格对齐 Python `__init__` 与 `start` 的分工。

```go
// Stop 停止消息队列
//
// Python: MessageQueuePulsar.stop() L109-124
func (q *MessageQueuePulsar) Stop(ctx context.Context) error
```

顺序严格对齐 Python：`running=false` → 逐个 `Unsubscribe`（记录订阅数日志）→ 关闭全部 producer 并清空（记录 producer 数日志）→ `client.Close()`。Go 侧无 executor，跳过 `shutdown(wait=True)`（偏差 D1）。

### 5.4 Subscribe / Unsubscribe

```go
// Subscribe 创建 topic 订阅
//
// Python: MessageQueuePulsar.subscribe(topic) L126-139
func (q *MessageQueuePulsar) Subscribe(topic string) (message_queue.SubscriptionBase, error)
```

- 未运行 → `BuildError(StatusMessageQueueTopicSubscriptionError, cause=err, topic, reason)`（Python `_validate_running` 抛 `RuntimeError`，被 `except` 包成订阅错误）
- 已存在同 topic 订阅 → 直接返回（Python 行为）
- 新建 consumer 后包成 `PulsarSubscription`，存入 `subs`

```go
// Unsubscribe 取消 topic 订阅
//
// Python: MessageQueuePulsar.unsubscribe(topic) L141-145
func (q *MessageQueuePulsar) Unsubscribe(ctx context.Context, topic string) error
```

不存在该 topic → **返回 nil 不报错**（Python `pop(topic, None)` 后 `if sub:`）。存在则 `Deactivate()` 后移除，并记 info 日志。

### 5.5 Produce / getOrCreateProducer

```go
// Produce 发布消息到指定 topic
//
// Python: MessageQueuePulsar.produce_message(topic, message) L151-177
func (q *MessageQueuePulsar) Produce(ctx context.Context, topic string, msg message_queue.QueueMessageBase) error
```

流程与错误语义：

```
1. !running → BuildError(StatusMessageQueueTopicMessageProductionError, ...)   （不单独抛 RuntimeError）
2. producer := q.getOrCreateProducer(topic)
3. content := SerializeMessage(msg)
4. producer.Send(ctx, content, partitionKey=msg.GetMessageID())
5. 任一步失败 → BuildError(StatusMessageQueueTopicMessageProductionError,
                            cause=err, topic=topic, message=msg, reason=err)
```

Python 的 `except Exception` 包住了**包括 `_validate_running` 在内的全部步骤**，Go 保持同一收敛点。

```go
// getOrCreateProducer 获取或创建 topic 对应的 producer，带 LRU 淘汰
//
// Python: MessageQueuePulsar._get_or_create_producer(topic) L179-208
func (q *MessageQueuePulsar) getOrCreateProducer(topic string) (pulsarProducerAPI, error)
```

单锁内完成（**取消 Python 的双检快路径**，偏差 D2）：

```
1. lock
2. 命中 → producerOrder.MoveToBack(elem) → 返回
3. len >= maxProducers → front 淘汰：Close 旧 producer、删 map、Debug 日志
4. Info 日志 "Creating new producer for topic=..."
5. client.CreateProducer(topic)
6. 入 map + push back 链表
7. unlock → 返回
```

`partition_key=message.message_id` 保序语义依赖 producer 复用，因此 producer 不能被提前误关。

## 6. PulsarSubscription

文件：`internal/agentcore/message_queue/pulsar.go`
Python：`PulsarSubscription`（`message_queue_pulsar.py` L23-83）

### 6.1 结构体

```go
// PulsarSubscription Pulsar topic 订阅
//
// Python: PulsarSubscription(SubscriptionBase)
type PulsarSubscription struct {
    // topic 订阅的 topic 名
    topic string
    // consumer Pulsar 消费者窄接口
    consumer pulsarConsumerAPI
    // handler 消息处理回调
    handler func(ctx context.Context, payload map[string]any) (any, error)
    // handlerMu 保护 handler
    handlerMu sync.RWMutex
    // cancel 消费 goroutine 取消函数
    cancel context.CancelFunc
    // done 消费 goroutine 退出信号
    done chan struct{}
    // active 是否活跃
    active atomic.Bool
}
```

### 6.2 Activate / Deactivate

```go
// Activate 激活订阅，启动消费 goroutine（幂等）
//
// Python: PulsarSubscription.activate() L35-39
func (s *PulsarSubscription) Activate()

// Deactivate 停用订阅，同步等待消费 goroutine 退出（幂等）
//
// Python: PulsarSubscription.deactivate() L41-59
func (s *PulsarSubscription) Deactivate()
```

`Deactivate` 顺序（Go 化）：`active=false` → `cancel()` → `consumer.Close()`（解阻塞 pending Receive）→ `<-done`。

Python 是「cancel task → 在 executor 里 close consumer → await task（吞掉 CancelledError）」。Go 因接口无 ctx/error（偏差 D3），改为同步阻塞，最长等待一个 receive 超时周期（1s）。

### 6.3 consumeLoop

```go
// consumeLoop 消费循环主逻辑
//
// Python: PulsarSubscription._consume_loop() L64-83
func (s *PulsarSubscription) consumeLoop(ctx context.Context)
```

```
defer close(done)
for s.active.Load():
    recvCtx = ctx + 1s                    （对齐 receive(timeout_millis=1000)）
    msg, err := consumer.Receive(recvCtx)
    ├─ err 是超时 → continue               （对齐 pulsar.Timeout: continue）
    ├─ err 是 ctx 取消 → return
    ├─ 其它 err → Error 日志 → continue    （对齐 logger.exception 后继续）
    ├─ message, err := DeserializeMessage(msg.Payload())
    │    err → Error 日志 → continue（不 ack）
    ├─ payload, err := dmessage_queue.MessageToMap(message)
    ├─ Info 日志 "Received message, topic=, message_id=, type="
    ├─ handler != nil → handler(ctx, payload)
    │    err → Error 日志 → continue（不 ack，对齐 Python 异常路径不 acknowledge）
    └─ consumer.Ack(msg)                  err → Error 日志
```

**两个关键语义**：

- **不 ack 触发重投**。Python 的 `await self._handler(payload)` 抛异常时直接落到 `except Exception`，`acknowledge` 那行永不执行。Go 必须保持同样行为，否则消息丢失。
- **handler 收整条消息的 map**，不是 `GetPayload()`。Python `PulsarSubscription` 传给 handler 的变量虽名为 `payload`，实际是 `deserialize_message(data)` 返回的**整条消息对象**（与 `SubscriptionInMemory` 传 `message.payload` 的约定不同）。若 Go 传 `GetPayload()`，9.84 的 adapter 就拿不到 `reply_topic` / `request_id`，无法回消息。

## 7. 日志对齐表

`const logComponent = logger.ComponentAgentCore`。日志文案英文（项目惯例），注释中文。

| Python 位置 | 级别 | Go 结构化字段 |
|---|---|---|
| `[PulsarSubscription] activated topic=` | Info | `topic` |
| `[PulsarSubscription] deactivated topic=` | Info | `topic` |
| `[PulsarSubscription] Received message, topic=, message_id=, type:` | Info | `topic`, `message_id`, `message_type` |
| `[PulsarSubscription] receive error` | Error | `topic`, `.Err(err)` |
| `[MessageQueuePulsar] started with url=` | Info | `url` |
| `[MessageQueuePulsar] closing N subscriptions` | Info | `subscription_count` |
| `[MessageQueuePulsar] closing N producers` | Info | `producer_count` |
| `[MessageQueuePulsar] stopped` | Info | — |
| `[MessageQueuePulsar] Create new subscription, topic=` | Info | `topic` |
| `[MessageQueuePulsar] unsubscribed` | Info | `topic` |
| `[MessageQueuePulsar] Sending message to topic=, message_id=` | Info | `topic`, `message_id` |
| `[MessageQueuePulsar] Message sent successfully: topic=, message_id=` | Info | `topic`, `message_id` |
| `[MessageQueuePulsar] LRU producer evicted:` | Debug | `topic` |
| `[MessageQueuePulsar] Creating new producer for topic=` | Info | `topic` |

异常路径必须带 `event_type` 与 `.Err(err)`。

## 8. 文件组织

```
internal/agentcore/message_queue/           ← openjiuwen/extensions/message_queue/
├── doc.go               # 包文档（Python 路径 + 文件目录 + 核心类型索引）
├── pulsar.go            # MessageQueuePulsar + PulsarSubscription
├── pulsar_api.go        # 三个窄接口 + SDK 适配器（sdkClient/sdkConsumer/sdkProducer/sdkMessage）
├── pulsar_test.go
└── pulsar_api_test.go

internal/agentcore/runner/drunner/dmessage_queue/   ← core/runner/drunner/dmessage_queue/
├── doc.go                    # 9.83 提前落位，9.84 续填
├── message.go                # DMessageType / ResultType / DmqRequestMessage / DmqResponseMessage
├── message_serializer.go     # SerializeMessage / DeserializeMessage / MessageToMap / serializePayload
├── message_test.go
└── message_serializer_test.go
```

`internal/agentcore/runner/drunner/` 一次建到位，9.84 只往里追加文件，不修改本节产物。

**import 方向（无环）**：

```
agentcore/message_queue                    ← 9.83 Pulsar 后端
        ↓                                  ↓
agentcore/runner/drunner/dmessage_queue    ← 9.83 前置件（Dmq 类型 + serializer）
        ↓
agentcore/runner/message_queue             ← 已交付（三接口 + InMemory + Local）
```

`runner/message_queue` 不 import 上述任何新包，因此不构成循环。`dmessage_queue` 与 `agentcore/message_queue` 各自单向依赖 `runner/message_queue`。

## 9. 测试策略

### 9.1 单元测试（默认构建，计入覆盖率）

`fakePulsarClient` / `fakeConsumer` / `fakeProducer` / `fakeMessage` 实现窄接口。

**`message_queue` 包**

| 用例 | 验证点 |
|---|---|
| `TestNewMessageQueuePulsar_不建client` | 构造后 client 为 nil，Start 后才非 nil |
| `TestStart_幂等` | 连续两次 Start 只建一次 client |
| `TestStop_未启动` | 未 Start 时 Stop 直接返回，无 panic |
| `TestStop_关闭全部订阅与producer` | fake 记录 Close 调用次数 |
| `TestSubscribe_未启动报错` | 错误为 `MESSAGE_QUEUE_TOPIC_SUBSCRIPTION_ERROR` / 110210 |
| `TestSubscribe_同topic复用` | 两次 Subscribe 返回同一实例，consumer 只建一次 |
| `TestUnsubscribe_不存在topic不报错` | 返回 nil |
| `TestProduce_未启动报错` | 错误为 `MESSAGE_QUEUE_TOPIC_MESSAGE_PRODUCTION_ERROR` / 110211 |
| `TestProduce_partitionKey为messageID` | fake producer 收到的 partitionKey == msg.MessageID |
| `TestProduce_LRU命中移到最后` | 连续 Produce 后顺序符合 LRU |
| `TestProduce_LRU淘汰最旧` | `maxProducers=2`，第 3 个 topic 触发淘汰并 Close 最旧 |
| `TestActivate_幂等` | 连续两次 Activate 只起一条 goroutine |
| `TestDeactivate_等待goroutine退出` | Deactivate 返回后 active=false 且 done 已关闭 |
| `TestConsumeLoop_handler收到完整消息map` | handler 收到的 map 含 `__class__`/`reply_topic`/`payload` |
| `TestConsumeLoop_成功后ack` | fake consumer 记录 Ack |
| `TestConsumeLoop_接收超时继续` | 超时后不退出循环 |
| `TestConsumeLoop_反序列化失败不ack` | Ack 未被调用，循环继续 |
| `TestConsumeLoop_handler报错不ack` | Ack 未被调用，循环继续 |

**`dmessage_queue` 包**

| 用例 | 验证点 |
|---|---|
| `TestSerializeMessage_请求往返` | 序列化后反序列化，字段全等 |
| `TestSerializeMessage_响应往返` | 同上，含 seq/last_chunk |
| `TestSerializeMessage_注入__class__` | JSON 含 `"__class__":"DmqRequestMessage"` |
| `TestSerializeMessage_datetime` | `time.Time` 编成 `{"__type__":"datetime"}` |
| `TestSerializeMessage_深度超限报错` | 11 层嵌套返回 error |
| `TestSerializeMessage_非Dmq类型报错` | 传 `*QueueMessage` 返回 PRODUCTION_ERROR（D6） |
| `TestDeserializeMessage_未知类名报错` | `unknown payload class: X` |
| `TestDeserializeMessage_补默认值` | 缺 `type` 时填 INPUT / OUTPUT |
| `TestDeserializeMessage_嵌套payload保留map` | 内层 `__class__` 未被消除 |
| `TestDeserializeMessage_坏JSON报错` | 非法字节返回 error |

### 9.2 集成测试（build tag，不计入覆盖率）

`//go:build integration`，从 `PULSAR_URL` 读取地址，无该环境变量则 `t.Skip`。只验一条最小链路：Start → Subscribe → Produce → handler 收到 → Stop。

### 9.3 覆盖率目标

两个包各自 ≥85%，遵循项目规范 3.1。

## 10. 偏差清单

| # | 偏差 | Python | Go | 理由 |
|---|---|---|---|---|
| D1 | 无 ThreadPoolExecutor | `ThreadPoolExecutor(max_workers)` + `run_in_executor` 包裹所有阻塞调用 | 每 topic 一条 goroutine，`MaxWorkers` 仅保存不消费 | goroutine 由 runtime 调度，无事件循环可阻塞；Python 也是每 topic 一条 consume_loop，executor 只解决阻塞问题 |
| D2 | 取消双检快路径 | `_producers.get(topic)` 无锁读，命中后 `move_to_end`；未命中再进 `async with self._lock` 二次检查 | 单 `sync.Mutex` 内完成全部判断与创建 | 无锁读 map 在 Go 是数据竞争（13.27 SimpleKB 同类教训）；协程调度的双检优化在 Go 无对应收益 |
| D3 | `Deactivate()` 同步阻塞 | `async def deactivate()`，可 await | 同步函数，无 ctx / 无 error 返回 | Go `SubscriptionBase` 接口契约已定（`base.go`），不可跨节改动 |
| D4 | payload 内部不重建类型 | `_deserialize_payload` 递归查 `TYPE_REGISTRY`，还原 OutputSchema/CustomSchema/TraceSchema/InteractionOutput/WorkflowOutput | 保持 `map[string]any`，`__class__` 键原样保留 | Go 无 pydantic 多态；强行重建需为 5 个类型手写 Unmarshal，且本节无任何消费者。因 `__class__` 保留，往返 wire 仍兼容 |
| D5 | datetime 读入不收包装 | `{"__type__":"datetime"}` 由 `object_hook` 还原成 `datetime` | `ExpireAt *float64` 只收裸浮点 | 本节无 datetime 字段的实际消费方；9.84 需要时再补 `UnmarshalJSON` |
| D6 | 非 Dmq 类型 fail fast | `serialize_message` 对任意 `QueueMessage` 都能写出 `__class__`，对面 `Unknown payload class` 才炸 | `MessageToMap` 遇到非 `Dmq*` 立即返回 `PRODUCTION_ERROR` | 发送前暴露错误优于跨进程后暴露；不改变合法输入的 wire 格式 |
| D7 | handler 收 map 且为整条消息 | `PulsarSubscription` 传整条消息对象；`SubscriptionInMemory` 传 `message.payload`（两者不一致） | `SetMessageHandler(func(ctx, map[string]any))` 传 `MessageToMap(整条消息)` | Go 接口契约是 map（不可跨节改动）；整条消息对齐 `PulsarSubscription` 而非 `SubscriptionInMemory`，否则 9.84 无法回消息 |
| D8 | 容器与锁的等价物 | `OrderedDict` + `asyncio.Lock` | `container/list` + `map` + `sync.Mutex` | 语言等价物 |
| D9 | Python factory 路径不一致（记录，不处理） | `message_queue_factory.py` import `openjiuwen.extensions.runner.pulsar_mq.message_queue_pulsar`，实际文件位于 `openjiuwen.extensions.message_queue.message_queue_pulsar`；仓库内不存在 `extensions/runner/` | 不涉及（本节不含 factory） | Python 中 Pulsar 后端实际不可达（import 必失败 → `MESSAGE_QUEUE_INITIATION_ERROR`）。**留给 9.84 决定"复刻"还是"修正"** |

## 11. 回填标记

### 11.1 本节内

- `internal/agentcore/runner/message_queue/base.go`：`QueueMessage` 补 4 个 json tag
- `IMPLEMENTATION_PLAN.md` 9.83：`☐` → `🔄` → `✅`，产出列补「MessageQueuePulsar + PulsarSubscription + 3 窄接口 + Dmq 消息类型 + serializer（提前落位 9.84）」

### 11.2 交给 9.84

- ⤴️ `internal/agentcore/runner/drunner/dmessage_queue/{message.go, message_serializer.go}` 已由 9.83 落位，9.84 **不重写**
- ⤵️ `internal/agentcore/runner/runner.go` 步骤 3（启动分布式队列，对齐 Python `runner.py` L304-312）与步骤 1（停止分布式组件，L329-337）两个预留点仍待 9.84 回填；届时 `MessageQueueFactory` 才能被 Runner 使用
- D9 的 factory import 路径问题待 9.84 拍板
- D4/D5/D7 的消费侧（`server_adapter`、`response_collector`）需要按需从 `map[string]any` 取值或补类型还原 helper

### 11.3 本节明确不做

- `MessageQueueFactory`、`FakeMQ`
- `dsubscription/`、`server_adapter/`、`remote_client/`
- 真实 broker 连接验证

## 12. 验收标准

1. `go build ./...` 与 `go vet ./...` 通过
2. `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/message_queue/... ./internal/agentcore/runner/drunner/...` 全绿
3. 两个包覆盖率各自 ≥85%
4. 全量回归零 FAIL
5. 两个 `doc.go` 齐备且文件目录与磁盘一致
6. `IMPLEMENTATION_PLAN.md` 9.83 标记为 ✅ 且产出列已补全
7. 偏差 D1–D9 全部在代码注释中标注对应编号
