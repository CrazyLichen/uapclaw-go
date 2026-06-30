# 8.30 TeamRuntime 完整包实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现完整 team_runtime 包（消息总线 + P2P 通信 + Pub-Sub），补充 AgentTeamSession，迁移 BaseTeam 到 schema 包，实现 CommunicableAgent。

**Architecture:** TeamRuntime 是多 Agent 通信的编排入口，持有 MessageBus（底层复用 MessageQueueInMemory）、AgentCard 注册表、TeamSession 管理器。MessageBus 通过 Topic 隔离实现 P2P（InvokeQueueMessage 请求-响应）和 Pub-Sub（QueueMessage 发后即忘）。CommunicableAgent 通过嵌入结构体为 Agent 提供通信能力。BaseTeam 迁移到 schema 包解决循环依赖。

**Tech Stack:** Go 1.22+、标准库 sync/errgroup/context、`github.com/danwakefield/fnmatch`（通配符匹配）、`github.com/google/uuid`（消息 ID 生成）、已有 `runner/message_queue`（内存队列）、已有 `runner/callback`（事件系统）

---

## 文件结构

### 新建文件

| 文件 | 职责 |
|------|------|
| `session/internal/agent_team_session.go` | AgentTeam 内部会话（InnerSession + TeamIDProvider） |
| `session/internal/agent_team_session_test.go` | 内部会话测试 |
| `session/agent_team.go` | AgentTeam 公开会话（SessionFacade + 生命周期方法） |
| `session/agent_team_test.go` | 公开会话测试 |
| `multi_agent/schema/team_interface.go` | 迁移：BaseTeam + AgentTeamProvider + TeamAgentProvider |
| `multi_agent/schema/communicable.go` | Communicable 接口定义 |
| `multi_agent/team_runtime/doc.go` | 包文档 |
| `multi_agent/team_runtime/envelope.go` | MessageEnvelope 消息信封 |
| `multi_agent/team_runtime/envelope_test.go` | 消息信封测试 |
| `multi_agent/team_runtime/subscription_manager.go` | SubscriptionManager 订阅管理器 |
| `multi_agent/team_runtime/subscription_manager_test.go` | 订阅管理器测试 |
| `multi_agent/team_runtime/message_router.go` | MessageRouter 消息路由器 |
| `multi_agent/team_runtime/message_router_test.go` | 消息路由器测试 |
| `multi_agent/team_runtime/message_bus.go` | MessageBus + MessageBusConfig |
| `multi_agent/team_runtime/message_bus_test.go` | 消息总线测试 |
| `multi_agent/team_runtime/team_runtime.go` | TeamRuntime + RuntimeConfig |
| `multi_agent/team_runtime/team_runtime_test.go` | 团队运行时测试 |
| `multi_agent/team_runtime/runtime_bindable.go` | RuntimeBindable 接口 |
| `multi_agent/team_runtime/communicable_agent.go` | CommunicableAgent 具体实现 |
| `multi_agent/team_runtime/communicable_agent_test.go` | CommunicableAgent 测试 |

### 修改文件

| 文件 | 修改内容 |
|------|----------|
| `multi_agent/team.go` | 删除 BaseTeam/AgentTeamProvider/TeamAgentProvider，改为 type alias 指向 schema |
| `multi_agent/team_option.go` | Session 字段从 `any` 回填为 `*session.AgentTeamSession`，WithTeamSession 参数同步 |
| `multi_agent/team_option_test.go` | 适配类型变更 |
| `multi_agent/doc.go` | 更新文件目录，添加 team_runtime/ 条目 |
| `multi_agent/schema/doc.go` | 更新文件目录，添加 team_interface.go + communicable.go 条目 |
| `session/doc.go` | 更新文件目录，添加 agent_team.go 条目 |
| `session/internal/doc.go` | 更新文件目录，添加 agent_team_session.go 条目 |
| `runner/resources_manager/base.go` | 导入从 multi_agent 改为 multi_agent/schema |
| `runner/resources_manager/agent_team_manager.go` | 导入从 multi_agent 改为 multi_agent/schema |
| `runner/resources_manager/resource_manager.go` | 导入从 multi_agent 改为 multi_agent/schema |
| `runner/resources_manager/agent_team_manager_test.go` | 导入从 multi_agent 改为 multi_agent/schema |
| `session/controller/global_controller.go` | 注册 P2P/PubSub 回调 |

---

### Task 1: AgentTeamSession 内部层

**Files:**
- Create: `internal/agentcore/session/internal/agent_team_session.go`
- Test: `internal/agentcore/session/internal/agent_team_session_test.go`
- Modify: `internal/agentcore/session/internal/doc.go`

- [ ] **Step 1: 编写 AgentTeamSession 内部层测试**

在 `agent_team_session_test.go` 中编写测试，覆盖：
- NewAgentTeamSession 构造及默认值
- InnerSession 接口全部方法
- TeamIDProvider.TeamID() 方法
- TeamSpan() 方法
- 构造选项覆盖

- [ ] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/session/internal/... -run TestAgentTeamSession -v 2>&1 | head -20`
Expected: 编译失败（agent_team_session.go 不存在）

- [ ] **Step 3: 实现 AgentTeamSession 内部层**

在 `agent_team_session.go` 中实现：
- AgentTeamSession 结构体（sessionID, teamID, config, state, streamWriterManager, tracer, checkpointer, teamSpan）
- AgentTeamSessionOption 构造选项类型 + With* 选项函数
- NewAgentTeamSession 构造函数（对齐 AgentSession 的默认值处理模式）
- InnerSession 接口全部方法实现
- TeamID() 方法（满足 TeamIDProvider）
- TeamSpan() 方法
- 编译时检查：`var _ interfaces.InnerSession = (*AgentTeamSession)(nil)` 和 `var _ interfaces.TeamIDProvider = (*AgentTeamSession)(nil)`

对照 Python: `openjiuwen/core/session/internal/agent_team.py` AgentTeamSession，同步日志。

- [ ] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/session/internal/... -run TestAgentTeamSession -v`
Expected: PASS

- [ ] **Step 5: 更新 session/internal/doc.go 文件目录**

添加 `agent_team_session.go` 条目到文件目录树。

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/session/internal/agent_team_session.go internal/agentcore/session/internal/agent_team_session_test.go internal/agentcore/session/internal/doc.go
git commit -m "feat(session): 添加 AgentTeamSession 内部会话实现"
```

---

### Task 2: AgentTeamSession 公开层

**Files:**
- Create: `internal/agentcore/session/agent_team.go`
- Test: `internal/agentcore/session/agent_team_test.go`
- Modify: `internal/agentcore/session/doc.go`

- [ ] **Step 1: 编写 AgentTeamSession 公开层测试**

在 `agent_team_test.go` 中编写测试，覆盖：
- NewAgentTeamSession / CreateAgentTeamSession 构造
- SessionFacade 接口全部 8 个方法
- GetTeamID / GetEnvs
- PreRun / PostRun / Commit / FlushCheckpoint
- CloseStream
- CreateAgentSession 创建子 Agent 会话
- Interact 返回 error
- Inner() 返回内部 AgentTeamSession

- [ ] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/session/... -run TestAgentTeamSession -v 2>&1 | head -20`
Expected: 编译失败

- [ ] **Step 3: 实现 AgentTeamSession 公开层**

在 `agent_team.go` 中实现：
- AgentTeamSession 结构体（sessionID, teamID, inner, preRunDone, postRunDone）
- AgentTeamSessionOption 构造选项类型 + With* 选项函数
- NewAgentTeamSession 构造函数
- CreateAgentTeamSession 工厂函数
- SessionFacade 接口 8 个方法实现
- GetTeamID / GetEnvs / PreRun / PostRun / Commit / FlushCheckpoint / CloseStream / CreateAgentSession / Inner
- Interact 返回 error("team session does not support interact")
- 编译时检查：`var _ interfaces.SessionFacade = (*AgentTeamSession)(nil)`

对照 Python: `openjiuwen/core/session/agent_team.py` Session，同步日志。

- [ ] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/session/... -run TestAgentTeamSession -v`
Expected: PASS

- [ ] **Step 5: 更新 session/doc.go 文件目录**

添加 `agent_team.go` 条目到文件目录树，核心类型索引添加 AgentTeamSession。

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/session/agent_team.go internal/agentcore/session/agent_team_test.go internal/agentcore/session/doc.go
git commit -m "feat(session): 添加 AgentTeamSession 公开会话实现（满足 SessionFacade）"
```

---

### Task 3: BaseTeam 迁移到 schema 包

**Files:**
- Create: `internal/agentcore/multi_agent/schema/team_interface.go`
- Modify: `internal/agentcore/multi_agent/team.go`
- Modify: `internal/agentcore/multi_agent/schema/doc.go`
- Modify: `internal/agentcore/runner/resources_manager/base.go`
- Modify: `internal/agentcore/runner/resources_manager/agent_team_manager.go`
- Modify: `internal/agentcore/runner/resources_manager/resource_manager.go`
- Modify: `internal/agentcore/runner/resources_manager/agent_team_manager_test.go`

- [ ] **Step 1: 在 schema 包创建 team_interface.go**

将 BaseTeam 接口、AgentTeamProvider、TeamAgentProvider 从 `multi_agent/team.go` 迁移到 `multi_agent/schema/team_interface.go`。调整导入路径（schema 包内引用 schema 自身的 TeamCardInterface，引用 single_agent/interfaces 的 BaseAgent）。

- [ ] **Step 2: 修改 multi_agent/team.go**

删除 BaseTeam/AgentTeamProvider/TeamAgentProvider 的定义，改为 type alias 指向 schema 包：

```go
type BaseTeam = schema.BaseTeam
type AgentTeamProvider = schema.AgentTeamProvider
type TeamAgentProvider = schema.TeamAgentProvider
```

保持向后兼容，现有导入 `multi_agent` 的代码无需修改。

- [ ] **Step 3: 修改 runner/resources_manager/ 的 3 个非测试文件**

将 `multiagent "github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent"` 改为 `multiagentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent/schema"`，替换所有 `multiagent.BaseTeam` 为 `multiagentschema.BaseTeam`，`multiagent.AgentTeamProvider` 为 `multiagentschema.AgentTeamProvider`。

- [ ] **Step 4: 修改 runner/resources_manager/ 的测试文件**

同步修改 `agent_team_manager_test.go` 的导入和类型引用。

- [ ] **Step 5: 更新 multi_agent/schema/doc.go**

添加 `team_interface.go` 条目到文件目录树。

- [ ] **Step 6: 运行全量编译验证**

Run: `cd /home/opensource/uap-claw-go && go build ./...`
Expected: 编译通过，无错误

- [ ] **Step 7: 运行受影响的测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/multi_agent/... ./internal/agentcore/runner/resources_manager/... -v`
Expected: PASS

- [ ] **Step 8: 提交**

```bash
git add internal/agentcore/multi_agent/schema/team_interface.go internal/agentcore/multi_agent/team.go internal/agentcore/multi_agent/schema/doc.go internal/agentcore/runner/resources_manager/base.go internal/agentcore/runner/resources_manager/agent_team_manager.go internal/agentcore/runner/resources_manager/resource_manager.go internal/agentcore/runner/resources_manager/agent_team_manager_test.go
git commit -m "refactor(multi_agent): 迁移 BaseTeam 到 schema 包解决循环依赖"
```

---

### Task 4: Communicable 接口

**Files:**
- Create: `internal/agentcore/multi_agent/schema/communicable.go`
- Modify: `internal/agentcore/multi_agent/schema/doc.go`

- [ ] **Step 1: 创建 communicable.go**

定义 Communicable 接口（Send/Publish/Subscribe/Unsubscribe），引用 TeamOption 类型。注意：TeamOption 定义在 multi_agent 主包，schema 包不能导入主包。解决方案：Communicable 接口的方法签名不包含 opts ...TeamOption，简化为必要参数。

- [ ] **Step 2: 更新 schema/doc.go**

添加 `communicable.go` 条目。

- [ ] **Step 3: 运行编译验证**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/multi_agent/schema/...`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/multi_agent/schema/communicable.go internal/agentcore/multi_agent/schema/doc.go
git commit -m "feat(multi_agent/schema): 添加 Communicable 可通信接口"
```

---

### Task 5: MessageEnvelope

**Files:**
- Create: `internal/agentcore/multi_agent/team_runtime/envelope.go`
- Test: `internal/agentcore/multi_agent/team_runtime/envelope_test.go`

- [ ] **Step 1: 创建 team_runtime 包基础文件**

创建 `team_runtime/doc.go` 包文档。

- [ ] **Step 2: 编写 MessageEnvelope 测试**

测试覆盖：构造、IsP2P/IsPubSub 判断、String() 输出。

- [ ] **Step 3: 实现 MessageEnvelope**

实现 MessageEnvelope 结构体 + IsP2P/IsPubSub/String 方法。对照 Python: `envelope.py`。

- [ ] **Step 4: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/multi_agent/team_runtime/... -run TestMessageEnvelope -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/multi_agent/team_runtime/
git commit -m "feat(team_runtime): 添加 MessageEnvelope 消息信封"
```

---

### Task 6: SubscriptionManager

**Files:**
- Create: `internal/agentcore/multi_agent/team_runtime/subscription_manager.go`
- Test: `internal/agentcore/multi_agent/team_runtime/subscription_manager_test.go`

- [ ] **Step 1: 编写 SubscriptionManager 测试**

测试覆盖：
- Subscribe/Unsubscribe 基本操作
- UnsubscribeAll
- GetSubscribers 精确匹配
- GetSubscribers 通配符匹配（* 和 ?）
- 双向索引一致性
- GetSubscriptionCount/ListSubscriptions
- 空集合自动清理

- [ ] **Step 2: 实现 SubscriptionManager**

实现：
- 双向索引数据结构（subscriptions + agentTopics）
- sync.RWMutex 保护并发
- Subscribe/Unsubscribe/UnsubscribeAll
- GetSubscribers（遍历所有 pattern，fnmatch 匹配）
- GetSubscriptionCount/ListSubscriptions
- 使用 `github.com/danwakefield/fnmatch` 做通配符匹配

对照 Python: `subscription_manager.py`，同步日志。

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/multi_agent/team_runtime/... -run TestSubscriptionManager -v`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/multi_agent/team_runtime/subscription_manager.go internal/agentcore/multi_agent/team_runtime/subscription_manager_test.go
git commit -m "feat(team_runtime): 添加 SubscriptionManager 订阅管理器"
```

---

### Task 7: RuntimeBindable 接口 + CommunicableAgent

**Files:**
- Create: `internal/agentcore/multi_agent/team_runtime/runtime_bindable.go`
- Create: `internal/agentcore/multi_agent/team_runtime/communicable_agent.go`
- Test: `internal/agentcore/multi_agent/team_runtime/communicable_agent_test.go`

注意：此 Task 依赖 Task 9（TeamRuntime），因为 CommunicableAgent 持有 *TeamRuntime 引用。但可以先定义接口和结构体，方法实现待 TeamRuntime 完成后补充。

- [ ] **Step 1: 创建 runtime_bindable.go**

定义 RuntimeBindable 接口：`BindRuntime(runtime *TeamRuntime, agentID string)`。注意引用同包的 TeamRuntime 前向声明（此时 TeamRuntime 尚未实现，先定义接口，编译时需 TeamRuntime 至少有类型声明）。

- [ ] **Step 2: 创建 communicable_agent.go 骨架**

定义 CommunicableAgent 结构体（runtime *TeamRuntime, agentID string），实现 BindRuntime 方法。Send/Publish/Subscribe/Unsubscribe 方法先写好签名和委托逻辑（调用 runtime.Send/Publish/Subscribe/Unsubscribe），等 TeamRuntime 实现后即可编译。

编译时检查：`var _ schema.Communicable = (*CommunicableAgent)(nil)` 和 `var _ RuntimeBindable = (*CommunicableAgent)(nil)`。

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/multi_agent/team_runtime/runtime_bindable.go internal/agentcore/multi_agent/team_runtime/communicable_agent.go
git commit -m "feat(team_runtime): 添加 RuntimeBindable 接口和 CommunicableAgent 实现"
```

---

### Task 8: MessageRouter

**Files:**
- Create: `internal/agentcore/multi_agent/team_runtime/message_router.go`
- Test: `internal/agentcore/multi_agent/team_runtime/message_router_test.go`

注意：MessageRouter 依赖 TeamRuntime（获取 session 和 agentCard）和 Runner（RunAgent）。由于循环依赖问题，MessageRouter 通过注入接口调用 Runner.RunAgent。

- [ ] **Step 1: 编写 MessageRouter 测试**

使用 mock TeamRuntime 和 mock Runner 接口，测试覆盖：
- RouteP2PMessage 正常路由
- RouteP2PMessage 回调触发
- RouteP2PMessage Agent 不存在错误
- RoutePubsubMessage 扇出到多个订阅者
- RoutePubsubMessage 无订阅者警告
- RoutePubsubMessage 单订阅者失败不中断其他

- [ ] **Step 2: 定义 AgentExecutor 接口**

在 message_router.go 中定义 AgentExecutor 接口（解决循环依赖）：

```go
// AgentExecutor Agent 执行器接口，用于 P2P/Pub-Sub 路由时调用目标 Agent。
// 由 Runner 实现并注入，避免 team_runtime 直接导入 runner 包。
type AgentExecutor interface {
    RunAgent(ctx context.Context, agentID string, inputs any, session any) (any, error)
}
```

- [ ] **Step 3: 实现 MessageRouter**

实现：
- MessageRouter 结构体（subscriptionManager + runtime + agentExecutor）
- RouteP2PMessage：触发 AgentP2PReceived 回调 → 构建 session → agentExecutor.RunAgent → 返回响应
- RoutePubsubMessage：GetSubscribers → errgroup 并发调用各订阅者 → 单订阅者失败仅记录日志
- buildAgentSession 辅助方法

对照 Python: `message_router.py`，同步日志。

- [ ] **Step 4: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/multi_agent/team_runtime/... -run TestMessageRouter -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/multi_agent/team_runtime/message_router.go internal/agentcore/multi_agent/team_runtime/message_router_test.go
git commit -m "feat(team_runtime): 添加 MessageRouter 消息路由器"
```

---

### Task 9: MessageBus

**Files:**
- Create: `internal/agentcore/multi_agent/team_runtime/message_bus.go`
- Test: `internal/agentcore/multi_agent/team_runtime/message_bus_test.go`

- [ ] **Step 1: 编写 MessageBus 测试**

测试覆盖：
- Start/Stop 生命周期
- Send P2P 消息（请求-响应）
- Publish Pub-Sub 消息（发后即忘）
- Topic 隔离（不同 team_id/session_id 隔离）
- add_subscription/remove_subscription
- cleanup_session 清理订阅
- 双检锁 ensureSubscription
- 超时处理

- [ ] **Step 2: 实现 MessageBus + MessageBusConfig**

实现：
- MessageBusConfig（MaxQueueSize/ProcessTimeout/TeamID）
- MessageBus 结构体（config, teamID, mq, activeSubscriptions, subscriptionLock, subscriptionManager, router, running）
- Topic 命名：`{team_id}_{session_id}__p2p__` / `{team_id}_{session_id}__pubsub__`
- Start/Stop/cleanupSession 生命周期
- Send（P2P）：创建 InvokeQueueMessage → ensureSubscription → mq.Produce → WaitResponse
- Publish（Pub-Sub）：创建 QueueMessage → ensureSubscription → mq.Produce
- add_subscription/remove_subscription/remove_all_subscriptions/list_subscriptions/get_subscription_count
- ensureSubscription（双检锁 + sync.Mutex）
- handleP2pMessage/handlePubsubMessage 内部处理
- extractEnvelopeFromPayload 提取信封

底层复用 `runner/message_queue.MessageQueueInMemory`。

对照 Python: `message_bus.py`，同步日志。

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/multi_agent/team_runtime/... -run TestMessageBus -v`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/multi_agent/team_runtime/message_bus.go internal/agentcore/multi_agent/team_runtime/message_bus_test.go
git commit -m "feat(team_runtime): 添加 MessageBus 消息总线"
```

---

### Task 10: TeamRuntime

**Files:**
- Create: `internal/agentcore/multi_agent/team_runtime/team_runtime.go`
- Test: `internal/agentcore/multi_agent/team_runtime/team_runtime_test.go`

- [ ] **Step 1: 编写 TeamRuntime 测试**

测试覆盖：
- NewTeamRuntime 构造 + RuntimeConfig 默认值
- Start/Stop 生命周期
- RegisterAgent/UnregisterAgent/HasAgent/GetAgentCard/ListAgents/GetAgentCount
- Send P2P 消息
- Publish Pub-Sub 消息
- Subscribe/Unsubscribe
- BindTeamSession/UnbindTeamSession/GetTeamSession
- Provider 包装（CommunicableAgent 自动绑定）
- P2PTimeout 配置

- [ ] **Step 2: 实现 TeamRuntime + RuntimeConfig**

实现：
- RuntimeConfig（TeamID/MessageBus/P2PTimeout）
- TeamRuntime 结构体（config, teamID, agentCards, messageBus, activeTeamSessions, running, startOnce, p2pTimeout）
- Start/Stop/CleanupSession
- RegisterAgent：存储 AgentCard → wrapProvider → Runner.ResourceMgr.AddAgent
- wrapProvider：创建 Agent → 检查 RuntimeBindable → 自动 BindRuntime
- UnregisterAgent/HasAgent/GetAgentCard/ListAgents/GetAgentCount
- Send/Publish/Subscribe/Unsubscribe（校验 + ensureStarted + 委托 messageBus）
- BindTeamSession/UnbindTeamSession/GetTeamSession
- ListSubscriptions/GetSubscriptionCount
- P2PTimeout/setP2PTimeout

对照 Python: `team_runtime.py`，同步日志。

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/multi_agent/team_runtime/... -run TestTeamRuntime -v`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/multi_agent/team_runtime/team_runtime.go internal/agentcore/multi_agent/team_runtime/team_runtime_test.go
git commit -m "feat(team_runtime): 添加 TeamRuntime 团队运行时编排入口"
```

---

### Task 11: CommunicableAgent 完整测试

**Files:**
- Test: `internal/agentcore/multi_agent/team_runtime/communicable_agent_test.go`

- [ ] **Step 1: 编写 CommunicableAgent 测试**

测试覆盖：
- BindRuntime 注入引用
- Send 委托到 runtime.Send
- Publish 委托到 runtime.Publish
- Subscribe 委托到 runtime.Subscribe（使用 agentID）
- Unsubscribe 委托到 runtime.Unsubscribe（使用 agentID）
- 嵌入到自定义 Agent 结构体的使用方式
- 类型断言 agent.(schema.Communicable) 获取接口

- [ ] **Step 2: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/multi_agent/team_runtime/... -run TestCommunicableAgent -v`
Expected: PASS

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/multi_agent/team_runtime/communicable_agent_test.go
git commit -m "test(team_runtime): 添加 CommunicableAgent 完整测试"
```

---

### Task 12: 回填预留点

**Files:**
- Modify: `internal/agentcore/multi_agent/team_option.go`
- Modify: `internal/agentcore/multi_agent/team_option_test.go`
- Modify: `internal/agentcore/session/controller/global_controller.go`
- Modify: `internal/agentcore/multi_agent/doc.go`

- [ ] **Step 1: 回填 team_option.go**

将 `Session any` 改为 `Session *session.AgentTeamSession`，将 `WithTeamSession(sess any)` 改为 `WithTeamSession(sess *session.AgentTeamSession)`，删除 `⤵️ 8.30` 标记。

- [ ] **Step 2: 适配 team_option_test.go**

更新测试中的 `WithTeamSession` 调用，传入 `*session.AgentTeamSession` 实例。

- [ ] **Step 3: 回填 global_controller.go P2P/PubSub 回调注册**

取消注释 P2P/PubSub 回调注册代码，替换为具体实现：

```go
callback.GetCallbackFramework().OnAgentTeam(callback.AgentP2PReceived, onAgentP2PReceived)
callback.GetCallbackFramework().OnAgentTeam(callback.AgentPubsubReceived, onAgentPubsubReceived)
```

实现 `onAgentP2PReceived` 和 `onAgentPubsubReceived` 回调函数。

- [ ] **Step 4: 更新 multi_agent/doc.go**

添加 `team_runtime/` 子包条目到文件目录树。

- [ ] **Step 5: 运行受影响的测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/multi_agent/... ./internal/agentcore/session/controller/... -v`
Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/multi_agent/team_option.go internal/agentcore/multi_agent/team_option_test.go internal/agentcore/session/controller/global_controller.go internal/agentcore/multi_agent/doc.go
git commit -m "feat(multi_agent): 回填 TeamSession 类型和 P2P/PubSub 回调注册"
```

---

### Task 13: 全量编译与测试验证

- [ ] **Step 1: 运行全量编译**

Run: `cd /home/opensource/uap-claw-go && go build ./...`
Expected: 编译通过

- [ ] **Step 2: 运行 team_runtime 包测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/multi_agent/team_runtime/... -v -cover`
Expected: PASS，覆盖率 ≥ 85%

- [ ] **Step 3: 运行 session 包测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/session/... -v -cover`
Expected: PASS

- [ ] **Step 4: 运行 multi_agent 包测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/multi_agent/... -v -cover`
Expected: PASS

- [ ] **Step 5: 运行 resources_manager 包测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/runner/resources_manager/... -v`
Expected: PASS

- [ ] **Step 6: 更新 IMPLEMENTATION_PLAN.md**

将 8.30 状态从 ☐ 改为 ✅，8.31-8.33 状态从 ☐ 改为 ✅（已包含在 8.30 中）。

- [ ] **Step 7: 提交**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs: 更新实现计划 8.30-8.33 状态为已完成"
```
