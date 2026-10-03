# 监控模块设计文档

## 概述

Go 端监控模块（`internal/agent_teams/monitor/`）完全缺失，Python 有 3 个核心类 + 1 个日志类 + 1 个事件映射层。本文档设计完整对齐 Python 的 Go 实现，同时将 Interact 的 `userInput` 参数类型从 `any` 改为 `*InteractInput`。

## 包布局

完全对齐 Python 分层：

**核心层 `internal/agent_teams/monitor/`**（对齐 `openjiuwen/agent_teams/monitor/`）：

| 文件 | 职责 |
|------|------|
| `doc.go` | 包文档 |
| `models.go` | MonitorEventType 枚举 + TeamInfo/MemberInfo/TaskInfo/MessageInfo/MonitorEvent 模型 + FromEventMessage 转换 |
| `team_monitor.go` | TeamMonitor struct + Start/Stop/查询方法/事件迭代器 + CreateMonitor 工厂 |
| `stream_logger.go` | TeamStreamLogger（P1） |

**应用层 `internal/swarm/agents/harness/team/`**（对齐 `jiwenswarm/agents/harness/team/`）：

| 文件 | 职责 |
|------|------|
| `event_types.go` | TeamEventCategory/TeamEventType 枚举 + sdkToTeamEventMap + GetTeamEventType/GetEventCategory/IsMessageEvent |
| `monitor_handler.go` | teamMonitorHandlerImpl（封装 TeamMonitor，事件收集+channel 广播+快照查询） |

**依赖方向**：`team → monitor → database`（应用层依赖核心层，核心层依赖数据库），无循环。

## MonitorEventType 枚举

```go
type MonitorEventType string

const (
    MonitorEventTypeTeamCreated            MonitorEventType = "team_created"
    MonitorEventTypeTeamCleaned            MonitorEventType = "team_cleaned"
    MonitorEventTypeTeamStandby            MonitorEventType = "team_standby"
    MonitorEventTypeMemberSpawned          MonitorEventType = "member_spawned"
    MonitorEventTypeMemberRestarted        MonitorEventType = "member_restarted"
    MonitorEventTypeMemberStatusChanged    MonitorEventType = "member_status_changed"
    MonitorEventTypeMemberExecutionChanged MonitorEventType = "member_execution_changed"
    MonitorEventTypeMemberShutdown         MonitorEventType = "member_shutdown"
    MonitorEventTypeMemberCanceled         MonitorEventType = "member_canceled"
    MonitorEventTypeTaskCreated            MonitorEventType = "task_created"
    MonitorEventTypeTaskPlanRequest        MonitorEventType = "task_plan_request"
    MonitorEventTypeTaskPlanResponse       MonitorEventType = "task_plan_response"
    MonitorEventTypeTaskUpdated            MonitorEventType = "task_updated"
    MonitorEventTypeTaskClaimed            MonitorEventType = "task_claimed"
    MonitorEventTypeTaskCompleted          MonitorEventType = "task_completed"
    MonitorEventTypeTaskCancelled          MonitorEventType = "task_cancelled"
    MonitorEventTypeTaskUnblocked          MonitorEventType = "task_unblocked"
    MonitorEventTypeMessage                MonitorEventType = "message"
    MonitorEventTypeBroadcast              MonitorEventType = "broadcast"
)
```

`monitorEventValues` 全局变量存储 `frozenset(e.value for e in MonitorEventType)` 的等价 map，供 `FromEventMessage` 检查。

## Monitor Models

从 database 内部模型转换的"视图"模型，每个都有 `FromInternal` 方法：

### TeamInfo

| 字段 | 类型 | 来源 |
|------|------|------|
| TeamName | string | database.Team.TeamName |
| DisplayName | string | database.Team.DisplayName |
| LeaderMemberName | string | database.Team.LeaderMemberName |
| Desc | *string | database.Team.Desc（空字符串时为 nil） |
| Created | int64 | database.Team.Created |

`FromInternal(team *database.Team) *TeamInfo`

### MemberInfo

| 字段 | 类型 | 来源 |
|------|------|------|
| MemberName | string | database.TeamMember.MemberName |
| TeamName | string | database.TeamMember.TeamName |
| DisplayName | string | database.TeamMember.DisplayName |
| Desc | *string | database.TeamMember.Desc |
| Status | string | database.TeamMember.Status |
| ExecutionStatus | *string | database.TeamMember.ExecutionStatus |
| Mode | string | database.TeamMember.Mode |

`FromInternal(member *database.TeamMember) *MemberInfo`

### TaskInfo

| 字段 | 类型 | 来源 |
|------|------|------|
| TaskID | string | database.TeamTaskBase.TaskID |
| TeamName | string | database.TeamTaskBase.TeamName |
| Title | string | database.TeamTaskBase.Title |
| Content | string | database.TeamTaskBase.Content |
| Status | string | database.TeamTaskBase.Status |
| Assignee | *string | database.TeamTaskBase.Assignee |
| UpdatedAt | *int64 | database.TeamTaskBase.UpdatedAt |

`FromInternal(task *database.TeamTaskBase) *TaskInfo`

### MessageInfo

| 字段 | 类型 | 来源 |
|------|------|------|
| MessageID | string | database.TeamMessageBase.MessageID |
| TeamName | string | database.TeamMessageBase.TeamName |
| FromMemberName | string | database.TeamMessageBase.FromMemberName |
| ToMemberName | *string | database.TeamMessageBase.ToMemberName |
| Content | string | database.TeamMessageBase.Content |
| Timestamp | int64 | database.TeamMessageBase.Timestamp |
| Broadcast | bool | database.TeamMessageBase.Broadcast |
| IsRead | bool | database.TeamMessageBase.IsRead（nil 时为 false） |

`FromInternal(msg *database.TeamMessageBase) *MessageInfo`

### MonitorEvent

实时事件，所有载荷字段扁平化（对齐 Python MonitorEvent(BaseModel)）：

```go
type MonitorEvent struct {
    EventType   MonitorEventType
    TeamName    string
    MemberName  *string
    Timestamp   int64    // 毫秒，Go time.Now().UnixMilli()
    // 团队字段
    DisplayName      *string
    LeaderMemberName *string
    Created          *int64
    // 成员字段
    OldStatus    *string
    NewStatus    *string
    Reason       *string
    RestartCount *int
    Force        *bool
    // 任务字段
    TaskID        *string
    Status        *string
    PlanID        *string
    MemberPlanMD  *string
    Approved      *bool
    // 消息字段
    MessageID      *string
    FromMemberName *string
    ToMemberName   *string
}
```

`FromEventMessage(msg *events.EventMessage) *MonitorEvent`：如果事件类型不在 `monitorEventValues` 中返回 nil。合并 `event_message.Payload` 并添加 `event_type` 和 `timestamp`。

## TeamMonitor

对齐 Python `openjiuwen/agent_teams/monitor/team_monitor.py`：

```go
type TeamMonitor struct {
    teamName    string
    sessionID   string
    db          database.TeamDatabase
    teamAgent   EventListenerRegistrar  // 接口，避免 monitor → agent 循环依赖
    hideDM      bool
    eventCh     chan *MonitorEvent       // 缓冲 channel（缓冲 256）
    eventMu     sync.Mutex
    started     bool
    listenerHandle *messager.EventListenerHandle
}
```

### EventListenerRegistrar 接口

在 monitor 包内定义（与 observability/setup.go 同名接口独立，因包不同）：

```go
type EventListenerRegistrar interface {
    AddEventListener(handler messager.MessagerHandler) *messager.EventListenerHandle
    RemoveEventListener(handle *messager.EventListenerHandle)
}
```

`*agent.TeamAgent` 已实现此接口，编译期自动满足。

### 事件流

- `_on_event` 实现 `MessagerHandler` 签名，通过 `TeamAgent.AddEventListener` 注册
- 调用 `FromEventMessage` 转换，`hideDM=true` 时丢弃 `MESSAGE` 类型事件
- 转换结果入 `eventCh`
- `Stop()` 发送 nil sentinel 后关闭 channel
- `Events() <-chan *MonitorEvent` — 消费者 `for evt := range ch` 遍历

### 会话绑定

Go 端 TeamDatabase 的 DAO 方法都接受 `ctx` + `teamName`/`memberName` 参数，不需要 Python 的 contextvar `_bound_session`。直接传参即可。

### 方法清单

| 方法 | 签名 | 说明 |
|------|------|------|
| `Start` | `(ctx context.Context) error` | 注册 event listener，幂等 |
| `Stop` | `(ctx context.Context) error` | 取消注册，发 sentinel，关 channel，幂等 |
| `Events` | `() <-chan *MonitorEvent` | 返回只读事件 channel |
| `GetTeamInfo` | `(ctx context.Context) (*TeamInfo, error)` | db.Team().GetTeam → TeamInfo |
| `GetMembers` | `(ctx context.Context, status string) ([]*MemberInfo, error)` | db.Member().GetTeamMembers → []MemberInfo |
| `GetMember` | `(ctx context.Context, memberName string) (*MemberInfo, error)` | db.Member().GetMember → MemberInfo |
| `GetTasks` | `(ctx context.Context, status string) ([]*TaskInfo, error)` | db.Task().GetTeamTasks → []TaskInfo |
| `GetMessages` | `(ctx context.Context, toMemberName, fromMemberName string) ([]*MessageInfo, error)` | hideDM+toMemberName → 空列表；hideDM 无 toMemberName → GetBroadcastMessages；否则 → GetMessages |

### 工厂函数

```go
func CreateMonitor(teamAgent EventListenerRegistrar, db database.TeamDatabase,
    teamName, sessionID string, hideDM bool) (*TeamMonitor, error)
```

校验 teamAgent 非 nil，返回 `*TeamMonitor`。

## event_types.go

放 `internal/swarm/agents/harness/team/event_types.go`，对齐 Python `event_types.py`：

```go
type TeamEventCategory string
const (
    EventCategoryMember  TeamEventCategory = "team.member"
    EventCategoryTask    TeamEventCategory = "team.task"
    EventCategoryMessage TeamEventCategory = "team.message"
)

type TeamEventType string
const (
    // 成员事件
    TeamEventMemberSpawned          TeamEventType = "team.member.spawned"
    TeamEventMemberStatusChanged    TeamEventType = "team.member.status_changed"
    TeamEventMemberExecutionChanged TeamEventType = "team.member.execution_changed"
    TeamEventMemberRestarted        TeamEventType = "team.member.restarted"
    TeamEventMemberShutdown         TeamEventType = "team.member.shutdown"
    // 任务事件
    TeamEventTaskCreated   TeamEventType = "team.task.created"
    TeamEventTaskClaimed   TeamEventType = "team.task.claimed"
    TeamEventTaskCompleted TeamEventType = "team.task.completed"
    TeamEventTaskCancelled TeamEventType = "team.task.cancelled"
    TeamEventTaskUnblocked TeamEventType = "team.task.unblocked"
    // 消息事件
    TeamEventMessageP2P       TeamEventType = "team.message.p2p"
    TeamEventMessageBroadcast TeamEventType = "team.message.broadcast"
)
```

**SDK → 前端映射**（未包含的 SDK 类型在转换时返回零值，静默丢弃）：

| SDK MonitorEventType | 前端 TeamEventType |
|---------------------|-------------------|
| MEMBER_SPAWNED | team.member.spawned |
| MEMBER_STATUS_CHANGED | team.member.status_changed |
| MEMBER_EXECUTION_CHANGED | team.member.execution_changed |
| MEMBER_RESTARTED | team.member.restarted |
| MEMBER_SHUTDOWN | team.member.shutdown |
| TASK_CREATED | team.task.created |
| TASK_CLAIMED | team.task.claimed |
| TASK_COMPLETED | team.task.completed |
| TASK_CANCELLED | team.task.cancelled |
| TASK_UNBLOCKED | team.task.unblocked |
| MESSAGE | team.message.p2p |
| BROADCAST | team.message.broadcast |

**函数**：

| 函数 | 签名 | 说明 |
|------|------|------|
| `GetTeamEventType` | `(sdk monitor.MonitorEventType) TeamEventType` | SDK → 前端映射，零值表示未映射 |
| `GetEventCategory` | `(et TeamEventType) TeamEventCategory` | 返回类别，默认 MEMBER |
| `IsMessageEvent` | `(et TeamEventType) bool` | 类别是否为 MESSAGE |

## TeamMonitorHandler 实现

放 `internal/swarm/agents/harness/team/monitor_handler.go`，对齐 Python `TeamMonitorHandler`：

```go
type teamMonitorHandlerImpl struct {
    monitor    *monitor.TeamMonitor
    sessionID  string
    eventQueue chan map[string]any    // 缓冲 256，对齐 Python _event_queue
    cancel     context.CancelFunc
    running    bool
    mu         sync.Mutex
}
```

### 方法

| 方法 | 签名 | 说明 |
|------|------|------|
| `Start` | `(ctx context.Context) error` | 调 monitor.Start()，启动后台 _collectEvents goroutine |
| `Stop` | `(ctx context.Context) error` | 设 running=false，cancel goroutine，调 monitor.Stop() |
| `IsRunning` | `() bool` | |
| `Events` | `() <-chan map[string]any` | 返回 eventQueue 的只读视图 |
| `GetTeamSnapshot` | `(ctx context.Context) (map[string]any, error)` | 聚合查询：成员列表+任务列表 |

### 事件收集 goroutine

对齐 Python `_collect_events`：range 遍历 `monitor.Events()`，每个事件调 `_convertEventToDict` 转换，结果放入 `eventQueue`。channel 关闭时退出。

### 事件转换

对齐 Python `_convert_event_to_dict`：

1. `GetTeamEventType(event.EventType)` 映射 SDK → 前端类型，未映射则跳过
2. `GetEventCategory(teamEventType)` 获取类别
3. 按 event_type 分派到各 handler（handleMemberSpawned / handleTaskCreated / handleMessage 等）
4. 消息事件需异步获取内容：调 `monitor.GetMessages()` 按 messageID 查找
5. 输出格式：`{"event_type": category, "session_id": ..., "event": eventData}`

### TeamMonitorHandler 接口更新

`team_manager.go` 中的接口扩展：

```go
type TeamMonitorHandler interface {
    Stop(ctx context.Context) error
    IsRunning() bool
    Events() <-chan map[string]any
    GetTeamSnapshot(ctx context.Context) (map[string]any, error)
}
```

## TeamStreamLogger

放 `internal/agent_teams/monitor/stream_logger.go`，对齐 Python `stream_logger.py`。P1 优先级。

```go
type TeamStreamLogger struct {
    file          *os.File
    mu            sync.Mutex
    runs          map[sourceKey]*run
    llmOutputSeen map[sourceKey]bool
    chunkCount    int
}

type sourceKey struct {
    member string
    role   string
}

type run struct {
    category string
    buf      []string
}
```

### 方法

| 方法 | 签名 | 说明 |
|------|------|------|
| `NewTeamStreamLogger` | `(filePath string) (*TeamStreamLogger, error)` | 创建目录+打开文件 |
| `Feed` | `(chunk OutputSchema)` | 消费流块，绝不 panic |
| `Flush` | `()` | 刷新所有待处理 run，关闭文件 |

### 块分类 + 去重

完全对齐 Python `_classify`：
- 累积型（llm_output/llm_reasoning）：按 source 缓冲，同一 source 切换 category 时刷新
- 离散型：立即写入前先刷新该 source 的待处理 run
- `answer` chunk 被 `llmOutputSeen` 去重

### 日志格式

`[LEVEL] member=<m> role=<r> category=<c>\n  | <content>`，对齐 Python `_emit`。

## InteractInput 类型

新增 `interaction/interact_input.go`：

```go
// InteractInput 交互输入，统一 TeamManager.Interact 的参数类型。
type InteractInput struct {
    // Raw 原始输入内容。
    // 支持 *sessioninteraction.InteractiveInput / string / interaction.InteractPayload。
    Raw any
}
```

### 变更范围

| 文件 | 变更 |
|------|------|
| `interaction/interact_input.go` | 新增 InteractInput struct |
| `team_manager_interact.go` | `userInput any` → `userInput *InteractInput`，内部用 `input.Raw` 传给 runtime.Manager |

runtime.Manager.Interact 的 `payload any` 保持不变（内部仍需多态分发），上层负责从 InteractInput 提取 Raw。

## TeamManager 集成变更

| 文件 | 变更 |
|------|------|
| `team_manager.go` | TeamMonitorHandler 接口增加 Events() + GetTeamSnapshot() |
| `team_manager_monitor.go` | 新增 EnsureMonitor(ctx, sessionID, teamName, hideDM) 方法 |
| `team_manager_monitor.go` | 新增 consumeMonitorEvents 内部方法 |

### EnsureMonitor

对齐 Python `ensure_monitor_for_active_runtime`：
1. 检查是否已有 handler 且 is_running → 提前返回
2. 通过 TeamAgent 获取 db、teamName、sessionID
3. `monitor.CreateMonitor(teamAgent, db, teamName, sessionID, hideDM)` 创建 TeamMonitor
4. `newTeamMonitorHandlerImpl(monitor, sessionID)` 创建 handler
5. `handler.Start(ctx)`
6. `m.RegisterMonitor(sessionID, handler)` 注册到 teamMonitors
7. 启动后台 `consumeMonitorEvents` goroutine

### consumeMonitorEvents

对齐 Python `_consume_monitor_events`：
- range 遍历 `handler.Events()`
- 通过 channel waiters 广播事件
- context 取消或 channel 关闭时退出

## 实现优先级

1. **P0（阻塞前端监控）**：models.go → team_monitor.go → event_types.go → monitor_handler.go → TeamManager 集成
2. **P1（诊断辅助）**：stream_logger.go
3. **并行**：InteractInput 类型（独立于监控模块）
