# schema/events 子包提取设计文档

## 概述

将 `schema` 包中的事件类型（BaseEventMessage、TypedEvent、TeamTopic、30+ 事件结构体及常量）提取到独立的 `schema/events/` 子包，打断 `schema ←→ team_workspace` 的循环依赖，使 `PublishEventFunc` 和 `HandleLockRequest/Response` 从 `any` 升级为具体类型。

## 问题

### 循环依赖链

```
team_workspace ──想要导入──→ schema（TypedEvent、WorkspaceArtifactEvent 等）
     ↑                              |
     |                              ↓
     └─────── schema/blueprint.go 导入 team_workspace.TeamWorkspaceConfig
```

### 受影响的 any（2 处）

| 位置 | 当前签名 | 期望签名 |
|------|---------|---------|
| `team_workspace/manager.go` PublishEventFunc | `func(eventType string, event any)` | `func(eventType string, event events.TypedEvent)` |
| `team_workspace/manager.go` HandleLockRequest | `func(request any) (any, error)` | `func(req events.WorkspaceLockRequestEvent) (events.WorkspaceLockResponseEvent, error)` |
| `team_workspace/manager.go` HandleLockResponse | `func(response any) error` | `func(resp events.WorkspaceLockResponseEvent) error` |

Python 签名（对照）：`publish_event: Callable[[str, BaseEventMessage], Awaitable[None]]`

Python 通过函数级延迟导入规避循环，Go 不支持此模式。

### 可行性验证

`events.go` **零外部导入**——仅使用 `map[string]any`（内建类型），不依赖 schema 包中的任何其他类型。提取为子包不会引入新依赖。

## 方案：提取 schema/events 子包

### 新包结构

```
agent_teams/schema/
├── doc.go                    # 更新：移除事件相关描述
├── blueprint.go              # 不变（仍导入 team_workspace）
├── events/                   # 新增子包
│   ├── doc.go                # 包文档
│   ├── base.go               # BaseEventMessage + TypedEvent 接口 + EventMessageFromEvent
│   ├── team_events.go        # TeamCreatedEvent/CleanedEvent/StandbyEvent/CompletedEvent
│   ├── member_events.go      # MemberSpawned/Restarted/StatusChanged/ExecutionChanged/Shutdown/Canceled
│   ├── task_events.go        # TaskCreated/Claimed/Completed/Cancelled/Updated/Unblocked/ListDrained/PlanRequest/PlanResponse
│   ├── message_events.go     # MessageEvent + BroadcastEvent
│   ├── workspace_events.go   # WorkspaceArtifactEvent/ConflictEvent/LockRequestEvent/LockResponseEvent
│   ├── worktree_events.go    # WorktreeCreatedEvent/WorktreeRemovedEvent
│   ├── approval_events.go    # PlanApprovalEvent + ToolApprovalResultEvent
│   ├── topic.go              # TeamTopic 类型 + 常量
│   ├── constants.go          # TeamEvent* 字符串常量
│   ├── base_test.go          # 从 schema/events_test.go 迁移
│   └── *_test.go             # 测试按分组迁移
├── i18n.go                   # 不变
├── status.go                 # 不变
└── ...
```

### 依赖方向（提取后）

```
schema/events    ← 纯事件类型，零外部导入
  ↑         ↑
  |         |
schema    team_workspace   ← 可安全导入 schema/events（循环打断）
```

- `schema/events` 不导入 `schema`（事件类型自包含）
- `schema` 可导入 `schema/events`（同模块内部，不会循环）
- `team_workspace` 可导入 `schema/events`（循环打断）
- `schema/blueprint.go` 仍导入 `team_workspace`（不变）

### 受影响的下游包

精确扫描结果（仅列出引用 `TypedEvent`/`BaseEventMessage`/`TeamEvent*` 常量的非测试文件）：

| 包 | 引用类型 | 变更 |
|---|---------|------|
| `team_workspace/manager.go` | `any`（间接） | 导入 `events`，PublishEventFunc/HandleLock 改为具体类型 |
| `team_workspace/rail.go` | `any`（间接） | 调用 publishEvent 时构造 `events.WorkspaceArtifactEvent` |
| `tools/team_backend.go` | `schema.TypedEvent` | → `events.TypedEvent` |
| `tools/task_manager.go` | `schema.TypedEvent` | → `events.TypedEvent` |
| `tools/message_manager.go` | `schema.TypedEvent` + `schema.BaseEventMessage` | → `events.TypedEvent` + `events.BaseEventMessage` |
| `tools/doc.go` | `schema.TypedEvent`（文档引用） | → `events.TypedEvent` |
| `messager/inprocess_test.go` | `schema.TeamEvent*`（测试常量） | → `events.TeamEvent*` |

**总计约 7 个文件需修改路径**，接口签名不变，仅包路径替换。

### rail.go 调用方式变更

当前（any）：
```go
r.ws.publishEvent(
    eventWorkspaceArtifactUpdated,
    map[string]any{
        "team_name":    r.ws.TeamName(),
        "member_name":  r.memberName,
        "artifact_path": realPath,
    },
)
```

提取后（TypedEvent）：
```go
r.ws.publishEvent(
    events.TeamEventWorkspaceArtifactUpdated,
    events.WorkspaceArtifactEvent{
        BaseEventMessage: events.BaseEventMessage{TeamName: r.ws.TeamName(), MemberName: r.memberName},
        ArtifactPath: realPath,
    },
)
```

对齐 Python 的强类型调用方式。

### 其他 any 不动

| any 位置 | 决策 | 原因 |
|---------|------|------|
| `WorktreeManager any` | 保留，TODO→#9.66a | 无循环依赖，等 9.66a 实现时替换 |
| `FirstIterGate any` | 保留，TODO→#9.68 | 无循环依赖，等 9.68 实现时替换 |
| `MountedRails` 5 个 any | 保留，TODO→#9.68 | 无循环依赖，等 9.68 实现时替换 |

## 影响范围

### 文件变更

| 操作 | 文件 | 说明 |
|------|------|------|
| 新建 | `schema/events/doc.go` | 包文档 |
| 新建 | `schema/events/base.go` | BaseEventMessage + TypedEvent + EventMessageFromEvent |
| 新建 | `schema/events/topic.go` | TeamTopic 类型 + 常量 |
| 新建 | `schema/events/constants.go` | TeamEvent* 字符串常量 |
| 新建 | `schema/events/team_events.go` | 团队级事件结构体 |
| 新建 | `schema/events/member_events.go` | 成员级事件结构体 |
| 新建 | `schema/events/task_events.go` | 任务级事件结构体 |
| 新建 | `schema/events/message_events.go` | 消息事件结构体 |
| 新建 | `schema/events/workspace_events.go` | 工作空间事件结构体 |
| 新建 | `schema/events/worktree_events.go` | Worktree 事件结构体 |
| 新建 | `schema/events/approval_events.go` | 审批事件结构体 |
| 迁移 | `schema/events/*_test.go` | 从 schema/events_test.go 按分组迁移 |
| 修改 | `schema/events.go` | 删除（内容已全部迁出） |
| 修改 | `schema/events_test.go` | 删除（内容已全部迁出） |
| 修改 | `schema/doc.go` | 移除事件相关描述 |
| 修改 | `team_workspace/manager.go` | PublishEventFunc + HandleLock 类型修正 |
| 修改 | `team_workspace/rail.go` | publishEvent 调用改为强类型 |
| 修改 | `messager/*.go` | schema.TypedEvent → events.TypedEvent |
| 修改 | `tools/*.go` | schema.TypedEvent → events.TypedEvent |
| 修改 | 其他引用 schema 事件类型的包 | 路径替换 |

### 风险

1. **下游引用面大**：所有引用 `schema.XxxEvent` 的包都需改为 `events.XxxEvent`
2. **接口兼容**：`TypedEvent` 接口签名不变，只是包路径变了
3. **序列化不变**：JSON tag 不变，wire 格式不受影响

### 不做的

- 不重构事件结构体本身
- 不改变 TypedEvent 接口方法
- 不合并/拆分事件类型
- 不处理非循环依赖的 any（WorktreeManager、FirstIterGate、MountedRails 等）
