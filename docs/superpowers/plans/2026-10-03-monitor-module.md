# 监控模块 + InteractInput 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现完整对齐 Python 的监控模块（Models + TeamMonitor + EventTypes + MonitorHandler + StreamLogger）并将 Interact 的 userInput 从 `any` 改为 `*InteractInput`。

**Architecture:** 核心层 `internal/agent_teams/monitor/` 提供 MonitorEventType、视图模型、TeamMonitor、StreamLogger；应用层 `internal/swarm/agents/harness/team/` 提供 event_types 映射和 TeamMonitorHandler 实现。TeamMonitor 通过 EventListenerRegistrar 接口注册事件监听，用 channel 替代 Python asyncio.Queue 实现事件流。InteractInput 统一 Interact 参数类型。

**Tech Stack:** Go 1.23+, channel-based event streaming, database.TeamDatabase DAO 接口

---

## 文件结构

### 新增文件

| 文件 | 职责 |
|------|------|
| `internal/agent_teams/monitor/doc.go` | 包文档 |
| `internal/agent_teams/monitor/models.go` | MonitorEventType 枚举 + TeamInfo/MemberInfo/TaskInfo/MessageInfo/MonitorEvent + FromEventMessage |
| `internal/agent_teams/monitor/models_test.go` | 模型单元测试 |
| `internal/agent_teams/monitor/team_monitor.go` | TeamMonitor struct + Start/Stop/查询/Events + CreateMonitor 工厂 |
| `internal/agent_teams/monitor/team_monitor_test.go` | TeamMonitor 单元测试 |
| `internal/agent_teams/monitor/stream_logger.go` | TeamStreamLogger（P1） |
| `internal/agent_teams/monitor/stream_logger_test.go` | StreamLogger 单元测试 |
| `internal/swarm/agents/harness/team/event_types.go` | TeamEventCategory/TeamEventType 枚举 + SDK 映射 + 查询函数 |
| `internal/swarm/agents/harness/team/event_types_test.go` | event_types 单元测试 |
| `internal/swarm/agents/harness/team/monitor_handler.go` | teamMonitorHandlerImpl 实现 |
| `internal/swarm/agents/harness/team/monitor_handler_test.go` | MonitorHandler 单元测试 |
| `internal/agent_teams/interaction/interact_input.go` | InteractInput struct |

### 修改文件

| 文件 | 变更 |
|------|------|
| `internal/swarm/agents/harness/team/team_manager.go` | TeamMonitorHandler 接口增加 Events() + GetTeamSnapshot() |
| `internal/swarm/agents/harness/team/team_manager_monitor.go` | 新增 EnsureMonitor + consumeMonitorEvents |
| `internal/swarm/agents/harness/team/team_manager_interact.go` | `userInput any` → `userInput *InteractInput` |
| `internal/swarm/agents/harness/team/team_manager_test.go` | 更新 fakeMonitorHandler 实现 + InteractInput 测试 |
| `internal/agent_teams/interaction/doc.go` | 文件目录添加 interact_input.go |

---

## Task 1: InteractInput 类型

**Files:**
- Create: `internal/agent_teams/interaction/interact_input.go`
- Create: `internal/agent_teams/interaction/interact_input_test.go`
- Modify: `internal/agent_teams/interaction/doc.go`

- [ ] **Step 1: 创建 interact_input.go**

```go
package interaction

// ──────────────────────────── 结构体 ────────────────────────────

// InteractInput 交互输入，统一 TeamManager.Interact 的参数类型。
// 对齐 Python: user_input 参数（实际运行时可以是 string/dict/payload）。
// 外层调用者需将 string/InteractiveInput 等类型自行包装为 InteractInput。
type InteractInput struct {
	// Raw 原始输入内容。
	// TeamRuntimeManager.Interact 内部根据类型分发：
	//   - *sessioninteraction.InteractiveInput → 恢复中断
	//   - string → ParseInteractStr → payloads
	//   - InteractPayload → 直接分发
	Raw any
}
```

- [ ] **Step 2: 创建 interact_input_test.go**

```go
package interaction

import "testing"

func TestInteractInput(t *testing.T) {
	input := &InteractInput{Raw: "hello"}
	if input.Raw != "hello" {
		t.Errorf("Raw = %v, want hello", input.Raw)
	}

	mapInput := &InteractInput{Raw: map[string]any{"key": "value"}}
	m, ok := mapInput.Raw.(map[string]any)
	if !ok || m["key"] != "value" {
		t.Errorf("Raw map = %v, want map[key]=value", mapInput.Raw)
	}
}
```

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agent_teams/interaction/ -run TestInteractInput -v`
Expected: PASS

- [ ] **Step 4: 更新 doc.go 文件目录**

在 `internal/agent_teams/interaction/doc.go` 的文件目录中添加：
```
//	├── interact_input.go     # InteractInput 统一交互输入类型
```

- [ ] **Step 5: 修改 team_manager_interact.go**

将 `userInput any` 改为 `userInput *InteractInput`，内部用 `input.Raw`：

```go
// 导入增加
import (
    "github.com/uapclaw/uapclaw-go/internal/agent_teams/interaction"
)

// Interact 签名变更
func (m *TeamManager) Interact(ctx context.Context, sessionID string, userInput *interaction.InteractInput) (bool, error) {
    // ... 内部逻辑不变，但传给 mgr.Interact 的参数改为 userInput.Raw
    result, err := mgr.Interact(ctx, userInput.Raw, teamName, sessionID)
    // ...
}
```

- [ ] **Step 6: 运行编译**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/swarm/agents/harness/team/`
Expected: 编译通过

- [ ] **Step 7: 提交**

```bash
git add internal/agent_teams/interaction/interact_input.go internal/agent_teams/interaction/interact_input_test.go internal/agent_teams/interaction/doc.go internal/swarm/agents/harness/team/team_manager_interact.go
git commit -m "feat(interaction): 新增 InteractInput 类型，TeamManager.Interact 参数 any→InteractInput"
```

---

## Task 2: Monitor Models（models.go）

**Files:**
- Create: `internal/agent_teams/monitor/doc.go`
- Create: `internal/agent_teams/monitor/models.go`
- Create: `internal/agent_teams/monitor/models_test.go`

- [ ] **Step 1: 创建 monitor/doc.go**

```go
// Package monitor 提供团队监控能力：事件观察、状态查询和实时事件流。
//
// TeamMonitor 注册到 TeamAgent 的事件监听器上，将内部 EventMessage 转换为
// 面向前端的 MonitorEvent，并通过 channel 供消费者实时获取。
// 同时提供团队/成员/任务/消息的只读查询接口。
//
// 文件目录：
//
//	monitor/
//	├── doc.go              # 包文档
//	├── models.go           # MonitorEventType 枚举 + TeamInfo/MemberInfo/TaskInfo/MessageInfo/MonitorEvent 模型
//	├── team_monitor.go     # TeamMonitor 事件观察器 + 查询 API
//	└── stream_logger.go    # TeamStreamLogger 诊断日志
//
// 对应 Python 代码：openjiuwen/agent_teams/monitor/
package monitor
```

- [ ] **Step 2: 创建 monitor/models.go**

包含：
1. MonitorEventType 枚举（19 个值，string 类型）
2. monitorEventValues 全局 map（用于 FromEventMessage 检查）
3. TeamInfo struct + FromInternal(*database.Team) *TeamInfo
4. MemberInfo struct + FromInternal(*database.TeamMember) *MemberInfo
5. TaskInfo struct + FromInternal(*database.TeamTaskBase) *TaskInfo
6. MessageInfo struct + FromInternal(*database.TeamMessageBase) *MessageInfo
7. MonitorEvent struct + FromEventMessage(*events.EventMessage) *MonitorEvent

关键实现细节：
- FromEventMessage: 合并 payload 字段到 MonitorEvent 扁平字段，EventType 不在 monitorEventValues 中返回 nil
- Timestamp 用 time.Now().UnixMilli()
- 可选字段（*string/*int64/*int/*bool）从 payload 提取，不存在时为 nil
- strPtrVal 辅助函数从 map[string]any 提取 *string

- [ ] **Step 3: 创建 monitor/models_test.go**

测试覆盖：
- TestMonitorEventType_值对齐：验证几个关键枚举值的字符串
- TestTeamInfo_FromInternal：构造 database.Team → FromInternal → 验证字段
- TestMemberInfo_FromInternal：构造 database.TeamMember → 验证
- TestTaskInfo_FromInternal：构造 database.TeamTaskBase → 验证 Assignee *string
- TestMessageInfo_FromInternal：构造 database.TeamMessageBase → 验证 IsRead *bool 处理
- TestMonitorEvent_FromEventMessage_有效事件：构造 EventMessage → 验证 MonitorEvent 字段映射
- TestMonitorEvent_FromEventMessage_无效事件：不在 monitorEventValues 中的事件类型 → 返回 nil
- TestMonitorEvent_FromEventMessage_成员事件：member_status_changed → 验证 OldStatus/NewStatus

- [ ] **Step 4: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agent_teams/monitor/ -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agent_teams/monitor/
git commit -m "feat(monitor): MonitorEventType 枚举 + TeamInfo/MemberInfo/TaskInfo/MessageInfo/MonitorEvent 视图模型"
```

---

## Task 3: TeamMonitor

**Files:**
- Create: `internal/agent_teams/monitor/team_monitor.go`
- Create: `internal/agent_teams/monitor/team_monitor_test.go`

- [ ] **Step 1: 创建 monitor/team_monitor.go**

关键类型和接口：

```go
// EventListenerRegistrar 事件监听器注册接口。
// 避免 monitor → agent 循环依赖。*agent.TeamAgent 已实现此接口。
type EventListenerRegistrar interface {
    AddEventListener(handler messager.MessagerHandler) *messager.EventListenerHandle
    RemoveEventListener(handle *messager.EventListenerHandle)
}

type TeamMonitor struct {
    teamName       string
    sessionID      string
    db             database.TeamDatabase
    teamAgent      EventListenerRegistrar
    hideDM         bool
    eventCh        chan *MonitorEvent  // 缓冲 256
    eventMu        sync.Mutex
    started        bool
    listenerHandle *messager.EventListenerHandle
}
```

方法实现：
1. `NewTeamMonitor(teamName, sessionID string, db database.TeamDatabase, teamAgent EventListenerRegistrar, hideDM bool) *TeamMonitor`
2. `CreateMonitor(teamAgent EventListenerRegistrar, db database.TeamDatabase, teamName, sessionID string, hideDM bool) (*TeamMonitor, error)` — 校验 teamAgent 非 nil
3. `TeamName() string` / `SessionID() string`
4. `Start(ctx context.Context) error` — 幂等，注册 _onEvent 回调
5. `Stop(ctx context.Context) error` — 幂等，取消注册，发送 nil sentinel，关闭 channel
6. `Events() <-chan *MonitorEvent`
7. `GetTeamInfo(ctx context.Context) (*TeamInfo, error)` — db.Team().GetTeam → FromInternal
8. `GetMembers(ctx context.Context, status string) ([]*MemberInfo, error)` — db.Member().GetTeamMembers → 批量 FromInternal
9. `GetMember(ctx context.Context, memberName string) (*MemberInfo, error)` — db.Member().GetMember → FromInternal
10. `GetTasks(ctx context.Context, status string) ([]*TaskInfo, error)` — db.Task().GetTeamTasks → 批量 FromInternal
11. `GetMessages(ctx context.Context, toMemberName, fromMemberName string) ([]*MessageInfo, error)` — 按 hideDM+toMemberName 分支：hideDM && toMemberName != "" → 空列表；hideDM → GetBroadcastMessages；否则 → GetMessages

_onEvent 实现 MessagerHandler 签名：
- 调用 FromEventMessage 转换
- nil 或 hideDM=true 且 event_type=message 时跳过
- 非nil 结果入 eventCh
- channel 关闭时跳过（default 分支丢弃，避免阻塞）

- [ ] **Step 2: 创建 monitor/team_monitor_test.go**

需要 mock：
- fakeTeamAgent 实现 EventListenerRegistrar：AddEventListener 记录 handler，RemoveEventListener 清除
- 使用 database.NewInMemoryTeamDatabase() 做真实查询测试

测试覆盖：
- TestCreateMonitor_NilTeamAgent：teamAgent 为 nil → 返回 error
- TestTeamMonitor_StartStop：Start 注册监听器 → Stop 取消注册 → channel 关闭
- TestTeamMonitor_Start_幂等：连续 Start 两次 → 只注册一次
- TestTeamMonitor_Stop_幂等：未 Start 时 Stop → 无 panic
- TestTeamMonitor_Events：Start → 手动调用 onEvent 回调 → Events channel 收到 MonitorEvent
- TestTeamMonitor_Events_HideDM：hideDM=true 时 message 类型事件被丢弃
- TestTeamMonitor_GetTeamInfo：内存 DB 中创建 team → GetTeamInfo 返回 TeamInfo
- TestTeamMonitor_GetMembers：内存 DB 中创建 member → GetMembers 返回列表
- TestTeamMonitor_GetTasks：内存 DB 中创建 task → GetTasks 返回列表
- TestTeamMonitor_GetMessages_HideDM：hideDM=true + toMemberName → 返回空列表
- TestTeamMonitor_GetMessages_正常：获取直发消息

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agent_teams/monitor/ -v`
Expected: PASS

- [ ] **Step 4: 更新 monitor/doc.go 文件目录**

确认 team_monitor.go 已在目录中（Step 1 已包含）

- [ ] **Step 5: 提交**

```bash
git add internal/agent_teams/monitor/team_monitor.go internal/agent_teams/monitor/team_monitor_test.go
git commit -m "feat(monitor): TeamMonitor 事件观察器 + 查询 API + CreateMonitor 工厂"
```

---

## Task 4: event_types 映射层

**Files:**
- Create: `internal/swarm/agents/harness/team/event_types.go`
- Create: `internal/swarm/agents/harness/team/event_types_test.go`

- [ ] **Step 1: 创建 team/event_types.go**

```go
package team

import "github.com/uapclaw/uapclaw-go/internal/agent_teams/monitor"

// ──────────────────────────── 枚举 ────────────────────────────

// TeamEventCategory 前端事件类别。
// 对齐 Python: TeamEventCategory (event_types.py)
type TeamEventCategory string

const (
    // EventCategoryMember 成员事件区域
    EventCategoryMember TeamEventCategory = "team.member"
    // EventCategoryTask 任务事件区域
    EventCategoryTask TeamEventCategory = "team.task"
    // EventCategoryMessage 消息事件区域
    EventCategoryMessage TeamEventCategory = "team.message"
)

// TeamEventType 前端事件类型，命名约定 team.{category}.{action}。
// 对齐 Python: TeamEventType (event_types.py)
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

加上：
- `eventTypeToCategory` map：TeamEventType → TeamEventCategory
- `sdkToTeamEventMap` map：monitor.MonitorEventType → TeamEventType（11 个映射条目，对齐 Python SDK_TO_TEAM_EVENT_MAP）
- `GetTeamEventType(sdk monitor.MonitorEventType) TeamEventType`
- `GetEventCategory(et TeamEventType) TeamEventCategory` — 默认 EventCategoryMember
- `IsMessageEvent(et TeamEventType) bool`

- [ ] **Step 2: 创建 team/event_types_test.go**

测试覆盖：
- TestGetTeamEventType_已映射：MEMBER_SPAWNED → team.member.spawned
- TestGetTeamEventType_未映射：TEAM_CREATED → 零值（空字符串）
- TestGetTeamEventType_消息映射：MESSAGE → team.message.p2p, BROADCAST → team.message.broadcast
- TestGetEventCategory_成员：TeamEventMemberSpawned → EventCategoryMember
- TestGetEventCategory_任务：TeamEventTaskCreated → EventCategoryTask
- TestGetEventCategory_消息：TeamEventMessageP2P → EventCategoryMessage
- TestGetEventCategory_默认：无效类型 → EventCategoryMember
- TestIsMessageEvent：TeamEventMessageP2P → true, TeamEventTaskCreated → false

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/swarm/agents/harness/team/ -run TestGet -v`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/swarm/agents/harness/team/event_types.go internal/swarm/agents/harness/team/event_types_test.go
git commit -m "feat(team): event_types 映射层，SDK MonitorEventType→前端 TeamEventType"
```

---

## Task 5: TeamMonitorHandler 实现

**Files:**
- Create: `internal/swarm/agents/harness/team/monitor_handler.go`
- Create: `internal/swarm/agents/harness/team/monitor_handler_test.go`
- Modify: `internal/swarm/agents/harness/team/team_manager.go` — 接口扩展

- [ ] **Step 1: 扩展 TeamMonitorHandler 接口（team_manager.go）**

在现有接口增加两个方法：

```go
type TeamMonitorHandler interface {
    Stop(ctx context.Context) error
    IsRunning() bool
    // Events 返回前端事件流 channel，供消费者 range 遍历。
    // 对齐 Python: TeamMonitorHandler.events()
    Events() <-chan map[string]any
    // GetTeamSnapshot 获取团队快照（成员+任务聚合视图）。
    // 对齐 Python: TeamMonitorHandler.get_team_snapshot()
    GetTeamSnapshot(ctx context.Context) (map[string]any, error)
}
```

- [ ] **Step 2: 创建 team/monitor_handler.go**

```go
type teamMonitorHandlerImpl struct {
    monitor   *monitor.TeamMonitor
    sessionID string
    eventQueue chan map[string]any  // 缓冲 256
    cancel     context.CancelFunc
    running    bool
    mu         sync.Mutex
}
```

方法实现：
1. `newTeamMonitorHandlerImpl(monitor *monitor.TeamMonitor, sessionID string) *teamMonitorHandlerImpl` — 创建实例，初始化 eventQueue
2. `Start(ctx context.Context) error` — 调 monitor.Start()，设 running=true，启动 collectEvents goroutine
3. `Stop(ctx context.Context) error` — 设 running=false，cancel goroutine（等待退出），调 monitor.Stop()，置 monitor=nil
4. `IsRunning() bool`
5. `Events() <-chan map[string]any` — 返回 eventQueue
6. `GetTeamSnapshot(ctx context.Context) (map[string]any, error)` — 聚合查询：调 monitor.GetMembers + monitor.GetTasks，构建成员列表（过滤 leader）和任务列表
7. `collectEvents(ctx context.Context)` — range 遍历 monitor.Events()，每个事件调 convertEventToDict，结果放入 eventQueue
8. `convertEventToDict(event *monitor.MonitorEvent) map[string]any` — SDK→前端映射 + 分类 + 分派 handler
9. 各 handler 方法：handleMemberSpawned / handleMemberStatusChanged / handleMemberExecutionChanged / handleMemberRestarted / handleMemberShutdown / handleTaskCreated / handleTaskClaimed / handleTaskCompleted / handleTaskCancelled / handleTaskUnblocked / handleMessage / handleBroadcast

convertEventToDict 逻辑：
- `GetTeamEventType(event.EventType)` → 未映射返回 nil → 跳过
- `GetEventCategory(teamEventType)` → 类别
- 构建基础 dict：`{"event_type": category, "session_id": sessionID, "event": baseEvent}`
- 按 event.EventType 分派到各 handler，handler 修改 event dict 并返回

消息 handler（handleMessage/handleBroadcast）需要异步获取内容：
- 调 monitor.GetMessages(ctx, "", fromMemberName) 或 GetMessages(ctx, toMemberName, "") 按 messageID 查找内容
- 查找失败返回空字符串

- [ ] **Step 3: 更新 team_manager_test.go 中的 fakeMonitorHandler**

现有 fakeMonitorHandler 需要实现新增的 Events() 和 GetTeamSnapshot() 方法：

```go
type fakeMonitorHandler struct {
    stopped  bool
    running  bool
    eventsCh chan map[string]any
}

func newFakeMonitorHandler() *fakeMonitorHandler {
    return &fakeMonitorHandler{
        running:  true,
        eventsCh: make(chan map[string]any, 16),
    }
}

func (f *fakeMonitorHandler) Stop(ctx context.Context) error {
    f.running = false
    f.stopped = true
    close(f.eventsCh)
    return nil
}

func (f *fakeMonitorHandler) IsRunning() bool { return f.running }
func (f *fakeMonitorHandler) Events() <-chan map[string]any { return f.eventsCh }
func (f *fakeMonitorHandler) GetTeamSnapshot(ctx context.Context) (map[string]any, error) {
    return map[string]any{"team_id": "test-team"}, nil
}
```

- [ ] **Step 4: 创建 team/monitor_handler_test.go**

测试覆盖：
- TestTeamMonitorHandlerImpl_StartStop：Start → IsRunning=true → Stop → IsRunning=false
- TestTeamMonitorHandlerImpl_Events：构造 fake TeamMonitor 注入事件 → Events channel 收到转换后的 dict
- TestTeamMonitorHandlerImpl_GetTeamSnapshot：mock monitor 返回成员+任务 → 验证快照结构
- TestConvertEventToDict_成员事件：MEMBER_SPAWNED → 验证 event_type="team.member", event 包含 member_id
- TestConvertEventToDict_任务事件：TASK_CREATED → 验证 event_type="team.task", event 包含 task_id
- TestConvertEventToDict_未映射事件：TEAM_CREATED → 返回 nil
- TestConvertEventToDict_消息事件：MESSAGE → 验证 event_type="team.message", event 包含 message_id/from_member

- [ ] **Step 5: 运行编译和测试**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/swarm/agents/harness/team/ && go test ./internal/swarm/agents/harness/team/ -v`
Expected: 编译通过 + 测试 PASS

- [ ] **Step 6: 提交**

```bash
git add internal/swarm/agents/harness/team/monitor_handler.go internal/swarm/agents/harness/team/monitor_handler_test.go internal/swarm/agents/harness/team/team_manager.go internal/swarm/agents/harness/team/team_manager_test.go
git commit -m "feat(team): teamMonitorHandlerImpl 实现，封装 TeamMonitor 事件收集+channel 广播+快照查询"
```

---

## Task 6: TeamManager 集成

**Files:**
- Modify: `internal/swarm/agents/harness/team/team_manager_monitor.go`

- [ ] **Step 1: 在 team_manager_monitor.go 添加 EnsureMonitor 方法**

对齐 Python `ensure_monitor_for_active_runtime`：

```go
// EnsureMonitor 为指定 session 创建并启动监控 handler。
// 对齐 Python: ensure_monitor_for_active_runtime(channel_id, session_id, team_name, hide_dm)
//
// Python 步骤：
//  1. 检查是否已有 handler 且 is_running → 提前返回
//  2. 获取 TeamMonitor
//  3. 创建 TeamMonitorHandler
//  4. handler.start()
//  5. tm.register_monitor(session_id, monitor_handler)
//  6. 启动 _consume_monitor_events 后台任务
func (m *TeamManager) EnsureMonitor(ctx context.Context, sessionID string, teamAgent *agent.TeamAgent, hideDM bool) error {
    m.mu.Lock()
    if handler, ok := m.teamMonitors[sessionID]; ok && handler.IsRunning() {
        m.mu.Unlock()
        return nil
    }
    m.mu.Unlock()

    // 从 TeamAgent 获取 TeamBackend → db
    backend := teamAgent.TeamBackend()
    if backend == nil {
        return fmt.Errorf("team_agent has no team_backend")
    }
    db := backend.DB()
    teamName := backend.TeamName()

    // 创建 TeamMonitor
    mon, err := monitor.CreateMonitor(teamAgent, db, teamName, sessionID, hideDM)
    if err != nil {
        return fmt.Errorf("create monitor: %w", err)
    }

    // 创建 handler
    handler := newTeamMonitorHandlerImpl(mon, sessionID)
    if err := handler.Start(ctx); err != nil {
        return fmt.Errorf("start handler: %w", err)
    }

    // 注册到 teamMonitors
    m.RegisterMonitor(sessionID, handler)

    // 启动事件消费 goroutine
    ctx, cancel := context.WithCancel(ctx)
    m.streamTasks[sessionID] = cancel // 复用 streamTasks 管理 cancel
    go m.consumeMonitorEvents(ctx, sessionID, handler)

    return nil
}
```

- [ ] **Step 2: 在 team_manager_monitor.go 添加 consumeMonitorEvents 方法**

对齐 Python `_consume_monitor_events`：

```go
// consumeMonitorEvents 消费监控事件并广播到 channel waiters。
// 对齐 Python: _consume_monitor_events(channel_id, session_id, monitor_handler)
func (m *TeamManager) consumeMonitorEvents(ctx context.Context, sessionID string, handler TeamMonitorHandler) {
    defer logger.Info(logComponent).Str("session_id", sessionID).Msg("监控事件消费 goroutine 退出")
    logger.Info(logComponent).Str("session_id", sessionID).Msg("监控事件消费 goroutine 启动")

    for evt := range handler.Events() {
        select {
        case <-ctx.Done():
            return
        default:
        }
        // TODO(#9.85): 广播到 channel waiters（需要 _pending_waiters 机制）
        // 当前仅记录日志，待前端通道层实现后回填
        logger.Debug(logComponent).Any("event_type", evt["event_type"]).Str("session_id", sessionID).
            Msg("监控事件")
    }
}
```

- [ ] **Step 3: 更新 cleanupRuntimeLocals 确认 Stop 调用**

验证 team_manager_monitor.go 中 cleanupRuntimeLocals 已正确调 handler.Stop()（上一轮已实现）。

- [ ] **Step 4: 运行编译**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/swarm/agents/harness/team/`
Expected: 编译通过

- [ ] **Step 5: 提交**

```bash
git add internal/swarm/agents/harness/team/team_manager_monitor.go
git commit -m "feat(team): EnsureMonitor + consumeMonitorEvents，集成 TeamMonitor 到 TeamManager 生命周期"
```

---

## Task 7: TeamStreamLogger（P1）

**Files:**
- Create: `internal/agent_teams/monitor/stream_logger.go`
- Create: `internal/agent_teams/monitor/stream_logger_test.go`

- [ ] **Step 1: 创建 monitor/stream_logger.go**

对齐 Python `stream_logger.py`：

```go
type sourceKey struct {
    member string
    role   string
}

type run struct {
    category string
    buf      []string
}

type TeamStreamLogger struct {
    file          *os.File
    mu            sync.Mutex
    runs          map[sourceKey]*run
    llmOutputSeen map[sourceKey]bool
    chunkCount    int
}
```

方法：
1. `NewTeamStreamLogger(filePath string) (*TeamStreamLogger, error)` — mkdirall + open file O_APPEND|O_CREATE|O_WRONLY
2. `Feed(chunk map[string]any)` — 分类（_classify），累积型缓冲/离散型写入，去重 answer，绝不 panic（recover 兜底）
3. `Flush()` — 刷新所有 pending run，关闭文件

块分类常量：
- `_chunkLLMOutput = "llm_output"`, `_chunkLLMReasoning = "llm_reasoning"`, `_chunkAnswer = "answer"`
- `_chunkInteraction = "__interaction__"`, `_chunkMessage = "message"`, `_chunkToolCall = "tool_call"`
- `_chunkToolResult = "tool_result"`, `_chunkToolUpdate = "tool_update"`, `_chunkTodoUpdated = "todo.updated"`
- `_chunkControllerOutput = "controller_output"`

类别 → 日志级别映射：
- text/INFO, reasoning/DEBUG, tool_call/DEBUG, tool_result/DEBUG, tool_update/DEBUG
- interaction/WARN, controller_output/WARN, runtime_ready/INFO, message/INFO, todo/INFO, other/INFO

日志格式：`[LEVEL] member=<m> role=<r> category=<c>\n  | <content line>`

写入前缀：`YYYY-MM-DD HH:MM:SS.mmm `

truncate 常量：toolResultCap=2000, toolArgsCap=500, genericCap=2000

辅助函数：toolCallSummary / toolResultSummary / toolUpdateSummary / controllerOutputSummary / runtimeReadySummary / interactionSummary / genericSummary

- [ ] **Step 2: 创建 monitor/stream_logger_test.go**

测试覆盖：
- TestNewTeamStreamLogger_创建文件：指定路径 → 文件创建成功
- TestNewTeamStreamLogger_创建目录：含子目录的路径 → 目录和文件都创建
- TestTeamStreamLogger_Feed_离散型：tool_call chunk → 立即写入文件
- TestTeamStreamLogger_Feed_累积型：llm_output chunk → 缓冲不写入，新 category → 刷新旧 run
- TestTeamStreamLogger_Feed_去重answer：先 llm_output 再 answer → answer 被丢弃
- TestTeamStreamLogger_Flush：缓冲中有数据 → Flush 后写入
- TestTeamStreamLogger_Feed_绝不panic：nil chunk / 无效字段 → recover 兜底

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agent_teams/monitor/ -run TestTeamStreamLogger -v`
Expected: PASS

- [ ] **Step 4: 更新 monitor/doc.go 文件目录**

确认 stream_logger.go 已在目录中（Step 1 已包含）

- [ ] **Step 5: 提交**

```bash
git add internal/agent_teams/monitor/stream_logger.go internal/agent_teams/monitor/stream_logger_test.go
git commit -m "feat(monitor): TeamStreamLogger 诊断日志，累积型/离散型分类+answer 去重"
```

---

## Task 8: 最终验证 + 全量编译

- [ ] **Step 1: 全量编译**

Run: `cd /home/opensource/uap-claw-go && go build ./...`
Expected: 编译通过

- [ ] **Step 2: 全量测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agent_teams/monitor/ ./internal/swarm/agents/harness/team/ ./internal/agent_teams/interaction/ -v`
Expected: 全部 PASS

- [ ] **Step 3: 推送**

```bash
git push
```
