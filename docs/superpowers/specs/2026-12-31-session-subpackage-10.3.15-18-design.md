# 10.3.15-18 会话管理子包提取与 Python 对齐设计

## 概述

将 Go 项目中散布于 `runtime` 和 `server` 两个包的 session 四件套（SessionManager、SessionHistory、SessionMetadata、SessionRename）统一提取到 `server/session/` 子包，恢复 Python 的同包结构，消除跨包依赖导致的元数据联动缺失问题，并补齐 5 项 Python 对齐差异 + 2 项补充功能。

## 背景：跨包根因

Python 的 `jiuwenswarm/server/runtime/session/` 包下 4 个文件同包互引：

```python
# session_history.py 内部直接调用
from jiuwenswarm.server.runtime.session.session_metadata import (
    set_session_delivery_context,
    update_session_metadata,
)
```

Go 拆分到了两个包：
- `session_history.go` + `session_manager.go` → `internal/swarm/server/runtime/`
- metadata 工具函数 + rename → `internal/swarm/server/handle_session.go`

`server` 依赖 `runtime`，反向 import 会循环依赖，导致 `AppendHistoryRecord` 无法调用 `updateSessionMetadata` + `SetSessionDeliveryContext`。

## 设计 1：session 子包提取

新建 `internal/swarm/server/session/` 子包：

```
server/session/
├── doc.go                    # 包文档
├── session_history.go        ← 从 runtime/session_history.go 移入
├── session_manager.go        ← 从 runtime/session_manager.go 移入
├── session_metadata.go       ← 从 handle_session.go 提取 metadata 工具函数
├── session_rename.go         ← 从 handle_session.go 提取 applySessionRename 工具函数
├── session_utils.go          ← SerializeValue + AutoTitle + deepCopyMap + derefStr 等辅助
├── session_history_test.go
├── session_manager_test.go
├── session_metadata_test.go
├── session_utils_test.go
```

### 从 handle_session.go 移入的函数/类型

| 类别 | 函数/类型 | 说明 |
|------|----------|------|
| 结构体 | `sessionMetadata` | 元数据结构体 |
| 结构体 | `SessionMetadataUpdate` | 增量更新参数（导出） |
| 结构体 | `metadataWriteItem` | 异步写入队列条目 |
| 导出函数 | `SetSessionDeliveryContext` | 刷新 delivery context |
| 导出函数 | `GetSessionDeliveryContext` | 读取 delivery context |
| 导出函数 | `BuildServerPushMessage` | 构造 server_push 消息 |
| 导出函数 | `GetSessionsDir` | 返回全局会话目录路径 |
| 导出函数 | `UpdateSessionMetadata` | 更新元数据（从非导出 `updateSessionMetadata` 升级为导出） |
| 导出函数 | `InitSessionMetadata` | 初始化元数据（新增，对齐 Python `init_session_metadata`） |
| 导出函数 | `GetSessionMetadata` | 获取元数据（新增，对齐 Python `get_session_metadata`） |
| 导出函数 | `IncrementSessionRoundCount` | 递增 round_id（从非导出升级为导出） |
| 导出函数 | `RemoveSessionMetadataCache` | 清除缓存（新增，对齐 Python `remove_session_metadata_cache`） |
| 导出函数 | `RemoveTeamModeSessionDirsAtStartup` | 启动清理 team 会话（新增） |
| 导出函数 | `GetAllSessionsMetadata` | 分页获取所有会话元数据（新增） |
| 导出函数 | `ApplySessionRename` | 重命名三种语义（新增，提取自 handler） |
| 非导出函数 | `readSessionMetadata` | 读取元数据文件 |
| 非导出函数 | `writeSessionMetadata` | 写入元数据文件 |
| 非导出函数 | `makeSessionID` | 生成会话 ID |
| 非导出函数 | `currentTimestamp` | UTC 时间戳 |
| 非导出函数 | `readSessionMetadataWithCache` | 缓存优先读取 |
| 非导出函数 | `deepCopyMap` | 深拷贝 |
| 非导出函数 | `derefStr` | 字符串指针解引用 |
| 非导出函数 | `ensureMetadataWorker` | 确保 worker 启动 |
| 非导出函数 | `metadataWriteWorker` | 异步写入 worker |
| 非导出函数 | `enqueueMetadataWrite` | 入队异步写入 |
| 常量 | `metadataFileName`, `metadataQueueSize`, `deliveryContextKind` | 元数据相关常量 |
| 全局变量 | `deliveryContextCache`, `deliveryContextMu`, `metadataQueue`, `metadataQueueOnce` | 缓存和队列 |

### 留在 server 包的（handler 方法）

| 类别 | 函数/类型 | 说明 |
|------|----------|------|
| 结构体 | `sessionListParams` | handler 请求参数 |
| 结构体 | `sessionRenameParams` | handler 请求参数 |
| 结构体 | `sessionDeleteParams` | handler 请求参数 |
| 结构体 | `sessionCreateParams` | handler 请求参数 |
| 结构体 | `sessionSwitchParams` | handler 请求参数 |
| 常量 | `heartbeatSessionPrefix` | handler 专用 |
| 方法 | `(s *AgentServer) handleSessionList` | 改为调用 `session.GetAllSessionsMetadata` |
| 方法 | `(s *AgentServer) handleSessionRename` | 改为调用 `session.ApplySessionRename` |
| 方法 | `(s *AgentServer) handleSessionDelete` | 改为调用 `session.RemoveSessionMetadataCache` |
| 方法 | `(s *AgentServer) handleSessionCreate` | 改为调用 `session.InitSessionMetadata` + `session.MakeSessionID` |
| 方法 | `(s *AgentServer) handleSessionSwitch` | stub 不变 |
| 方法 | `(s *AgentServer) handleSessionRewind*` | stub 不变 |
| 方法 | `(s *AgentServer) handleSessionFork` | stub 不变 |

## 设计 2：AppendHistoryRecord 元数据联动

在 `session_history.go` 的 `AppendHistoryRecord` 内部，追加 history 成功后联动调用：

```go
// 对齐 Python: append_history_record 内部的元数据联动（第 176-200 行）
func AppendHistoryRecord(...) {
    // ... 原有构建 + 入队逻辑 ...

    // 异步联动更新元数据，对齐 Python try/except：联动失败不影响主流程
    go func() {
        defer func() {
            if r := recover(); r != nil {
                logger.Warn(logComponent).Any("recover", r).Msg("会话元数据联动 panic 恢复")
            }
        }()
        // 1. 更新元数据
        userContent := contentText
        if roleNorm != "user" {
            userContent = ""
        }
        UpdateSessionMetadata(SessionMetadataUpdate{
            SessionID:             sid,
            ChannelID:             &cid,
            IncrementMessageCount: true,
            UserContent:           &userContent,
            ChannelMetadata:       channelMetadata,
            Mode:                  &mode,
        })
        // 2. 仅 user 消息刷新 delivery context
        if roleNorm == "user" {
            SetSessionDeliveryContext(sid, &cid, &rid, channelMetadata)
        }
    }()
}
```

**关键原则**：联动失败仅 log.Warn，不影响 history 写入主流程。对齐 Python 的 `try/except` 语义。

**为什么用 goroutine**：Python 用 `try/except` 包裹，联动失败不阻塞。Go 用 goroutine 确保联动不阻塞 history 入队流程（异步写入队列不应因 metadata 联动而阻塞）。

## 设计 3：ReadTeamHistoryRecords + IsTeamRelevant

新增对齐 Python `read_team_history_records` + `_is_team_relevant`：

```go
// ──────────────────────────── 常数 ────────────────────────────

// teamRelevantEventTypes team 相关事件类型集合
// 对齐 Python _TEAM_RELEVANT_EVENT_TYPES
var teamRelevantEventTypes = map[string]bool{
    "team.message":      true,
    "chat.tool_call":    true,
    "chat.tracer_agent": true,
    "chat.final":        true,
    "chat.tool_result":  true,
    "chat.file":         true,
}

// ──────────────────────────── 导出函数 ────────────────────────────

// IsTeamRelevant 判断记录是否为 team 相关事件
// 对齐 Python _is_team_relevant(item)
func IsTeamRelevant(item map[string]any) bool { ... }

// ReadTeamHistoryRecords 读取指定会话的 team 相关历史记录
// 对齐 Python read_team_history_records(session_id)
// 带 5 次递增间隔重试（0.2s × attempt），防止读到截断窗口空文件
func ReadTeamHistoryRecords(sessionID string) ([]map[string]any, error) { ... }
```

**过滤逻辑**（对齐 Python）：
- `team.message` → 始终保留
- `chat.tool_call` / `chat.tracer_agent` → 仅 `mode == "team"` 时保留
- `chat.final` / `chat.tool_result` / `chat.file` → 仅 `role == "teammate"` 时保留

**重试机制**：最多 5 次，递增间隔 0.2s × attempt，空结果且文件存在时触发重试。

## 设计 4：TruncateHistoryRecords 改为 cutIndex

```go
// TruncateResult 截断结果，对齐 Python truncate_history_records 返回 dict
type TruncateResult struct {
    // RemainingRecords 保留记录数
    RemainingRecords int
    // RemovedRecords 删除记录数
    RemovedRecords int
}

// TruncateHistoryRecords 截断 history 到指定位置索引（rewind 使用）
// 对齐 Python truncate_history_records(session_id, cut_index: int) → dict
func TruncateHistoryRecords(sessionID string, cutIndex int) (TruncateResult, error) { ... }
```

**变更**：
- 参数从 `requestID: string` 改为 `cutIndex: int`
- 返回值从 `error` 改为 `(TruncateResult, error)`，对齐 Python 返回 dict
- 内部逻辑：先等异步队列刷盘（对齐 Python `_WRITE_QUEUE.join()`），再持锁截断到 cutIndex

## 设计 5：SerializeValue 辅助函数

```go
// SerializeValue 递归序列化值，确保 JSON 可序列化
// 对齐 Python _serialize_value(obj)
func SerializeValue(obj any) any {
    switch v := obj.(type) {
    case time.Time:
        return v.Format(time.RFC3339Nano)
    case map[string]any:
        result := make(map[string]any, len(v))
        for k, val := range v {
            result[k] = SerializeValue(val)
        }
        return result
    case []any:
        result := make([]any, len(v))
        for i, val := range v {
            result[i] = SerializeValue(val)
        }
        return result
    default:
        return v
    }
}
```

**使用点**：`AppendHistoryRecord` 中 extra 展开处改为 `serializedExtra := SerializeValue(extra)`，再展开到 item。

## 设计 6：自动标题回填

```go
// ──────────────────────────── 常量 ────────────────────────────

const (
    // titleMaxLen 自动标题截取长度，对齐 Python _TITLE_MAX_LEN = 50
    titleMaxLen = 50
)

// ──────────────────────────── 导出函数 ────────────────────────────

// AutoTitle 从首条用户消息自动生成会话标题
// 对齐 Python _auto_title(content)
func AutoTitle(content string) string {
    title := strings.TrimSpace(strings.ReplaceAll(content, "\n", " "))
    if len(title) > titleMaxLen {
        title = title[:titleMaxLen] + "..."
    }
    return title
}
```

**回填点 1**：`UpdateSessionMetadata` 创建新 metadata 时（原 ⤵️ 11.x 标记）：

```go
autoTitle := ""
userContentStr := derefStr(update.UserContent, "")
if update.Title == nil && userContentStr != "" {
    autoTitle = AutoTitle(userContentStr)
}
metadata["title"] = derefStr(update.Title, autoTitle)
```

**回填点 2**：`UpdateSessionMetadata` 更新现有 metadata 时（原 ⤵️ 11.x 标记）：

```go
// 自动生成标题：当 title 为空且提供了用户消息内容时
currentTitle, _ := metadata["title"].(string)
if currentTitle == "" && update.UserContent != nil && *update.UserContent != "" {
    metadata["title"] = AutoTitle(*update.UserContent)
}
```

## 设计 7：RemoveTeamModeSessionDirsAtStartup

```go
// RemoveTeamModeSessionDirsAtStartup AgentServer 启动时删除 mode=team 的会话目录
// 对齐 Python remove_team_mode_session_dirs_at_startup()
func RemoveTeamModeSessionDirsAtStartup() { ... }
```

**实现**：
- 扫描 sessions 目录
- 读取每个子目录的 metadata.json
- 如果 `mode == "team"`，删除目录 + 清理缓存
- 统计删除数量并记录日志

**调用点**：`AgentServer.Start()` 中调用。

## 设计 8：GetAllSessionsMetadata 分页

```go
// GetAllSessionsMetadata 分页获取所有会话元数据
// 对齐 Python get_all_sessions_metadata(limit, offset) → (sessions, total)
func GetAllSessionsMetadata(limit int, offset int) ([]map[string]any, int) { ... }
```

**实现**：
- 扫描 sessions 目录，跳过 heartbeat 前缀
- 读取每个子目录的 metadata.json
- 无 metadata.json 的旧会话构造最小信息（对齐 Python）
- 按 `last_message_at` 降序排列
- 返回 `(sessions[offset:offset+limit], total)`

**调用点**：`handleSessionList` 改为调用 `session.GetAllSessionsMetadata`。

## 变更影响范围

### 受影响的文件

| 文件 | 变更类型 | 说明 |
|------|----------|------|
| `server/session/*.go` | 新建 | session 子包 |
| `server/handle_session.go` | 大幅删减 | 移出 metadata/rename 工具函数，仅保留 handler 方法 |
| `runtime/session_history.go` | 删除 | 移入 session 子包 |
| `runtime/session_manager.go` | 删除 | 移入 session 子包 |
| `runtime/doc.go` | 更新 | 移除 session_history/session_manager 条目 |
| `server/doc.go` | 更新 | 添加 session/ 子包条目 |
| `runtime/uapclaw.go` | 更新 | import 从 runtime 改为 session |
| `server/agent_server.go` | 更新 | import session，Start() 中调用 RemoveTeamModeSessionDirsAtStartup |
| `server/handle_envelope.go` | 更新 | import session |
| `server/handle_agents.go` | 更新 | import session |
| `runtime/uapclaw_test.go` | 更新 | import 调整 |
| `server/agent_server_test.go` | 更新 | import 调整 |
| `server/handle_envelope_test.go` | 更新 | import 调整 |
| `IMPLEMENTATION_PLAN.md` | 更新 | 10.3.15-18 状态标记 |

### 无外部包影响

`server` 包的外部引用者只有 `cmd/uapclaw/cmd.go` 和 `gateway_push/`，它们不使用 session 相关函数，无需调整。

## Python 对齐方法映射

| Go 函数 | Python 函数 | 说明 |
|---------|------------|------|
| `session.AppendHistoryRecord` | `append_history_record` | 内部联动 UpdateSessionMetadata + SetSessionDeliveryContext |
| `session.AppendCompactHistoryRecords` | `append_compact_history_records` | 无变更 |
| `session.AppendCompactHistoryFromPayload` | `_append_compact_history_from_payload` | 无变更 |
| `session.ReadHistoryRecords` | `read_history_records` | 无变更 |
| `session.ReadTeamHistoryRecords` | `read_team_history_records` | **新增**，带重试 + team 过滤 |
| `session.IsTeamRelevant` | `_is_team_relevant` | **新增** |
| `session.TruncateHistoryRecords` | `truncate_history_records` | 参数改为 cutIndex，返回 TruncateResult |
| `session.SerializeValue` | `_serialize_value` | **新增** |
| `session.AutoTitle` | `_auto_title` | **新增**，回填 ⤵️ 11.x |
| `session.UpdateSessionMetadata` | `update_session_metadata` | 从非导出升级为导出 |
| `session.InitSessionMetadata` | `init_session_metadata` | **新增** |
| `session.GetSessionMetadata` | `get_session_metadata` | **新增** |
| `session.SetSessionDeliveryContext` | `set_session_delivery_context` | 移入子包 |
| `session.GetSessionDeliveryContext` | `get_session_delivery_context` | 移入子包 |
| `session.BuildServerPushMessage` | `build_server_push_message` | 移入子包 |
| `session.IncrementSessionRoundCount` | `increment_session_round_count` | 从非导出升级为导出 |
| `session.RemoveSessionMetadataCache` | `remove_session_metadata_cache` | **新增** |
| `session.RemoveTeamModeSessionDirsAtStartup` | `remove_team_mode_session_dirs_at_startup` | **新增** |
| `session.GetAllSessionsMetadata` | `get_all_sessions_metadata` | **新增** |
| `session.ApplySessionRename` | `apply_session_rename` | **新增**，提取自 handler |
