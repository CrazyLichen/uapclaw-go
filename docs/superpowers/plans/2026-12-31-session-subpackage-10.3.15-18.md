# 10.3.15-18 Session 子包提取与 Python 对齐 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将散布于 `runtime` 和 `server` 两个包的 session 四件套统一提取到 `server/session/` 子包，恢复 Python 同包结构，补齐 5 项 Python 对齐差异 + 2 项补充功能 + 自动标题回填。

**Architecture:** 新建 `internal/swarm/server/session/` 子包，将 `session_history.go` 和 `session_manager.go` 从 `runtime/` 移入，将 metadata/rename 工具函数从 `handle_session.go` 提取到子包。handler 方法留在 `server` 包改为调用子包导出函数。`AppendHistoryRecord` 内部增加元数据联动 goroutine（失败仅 Warn）。

**Tech Stack:** Go 1.x, 标准库 `encoding/json` / `os` / `sync` / `time`, testify, 项目内部 `logger` / `workspace` / `schema`

---

## 文件结构

### 新建文件

| 文件 | 职责 |
|------|------|
| `server/session/doc.go` | 包文档 + 文件目录 |
| `server/session/session_history.go` | History JSONL 读写（从 runtime 移入 + 联动 + SerializeValue + ReadTeamHistoryRecords + TruncateHistoryRecords cutIndex） |
| `server/session/session_manager.go` | SessionManager LIFO 任务队列（从 runtime 移入，包名改为 session） |
| `server/session/session_metadata.go` | Metadata 读写 + DeliveryContext + 异步队列 + Init/Get/Update/IncrementRound（从 handle_session.go 提取） |
| `server/session/session_rename.go` | ApplySessionRename 三种语义（从 handle_session.go 提取） |
| `server/session/session_utils.go` | AutoTitle + SerializeValue + deepCopyMap + derefStr + currentTimestamp + makeSessionID + normalizeSessionID |
| `server/session/session_startup.go` | RemoveTeamModeSessionDirsAtStartup + GetAllSessionsMetadata |
| `server/session/session_history_test.go` | History 测试 |
| `server/session/session_manager_test.go` | Manager 测试 |
| `server/session/session_metadata_test.go` | Metadata 测试 |
| `server/session/session_utils_test.go` | AutoTitle + SerializeValue 测试 |
| `server/session/session_startup_test.go` | RemoveTeamMode + GetAllSessions 测试 |

### 修改文件

| 文件 | 变更 |
|------|------|
| `server/handle_session.go` | 删除所有移入子包的函数，handler 改为调用 `session.*` |
| `server/handle_session_test.go` | import session 包，测试不变（handler 行为不变） |
| `runtime/session_history.go` | 删除（移入子包） |
| `runtime/session_history_test.go` | 删除（移入子包） |
| `runtime/session_manager.go` | 删除（移入子包） |
| `runtime/session_manager_test.go` | 删除（移入子包） |
| `runtime/doc.go` | 移除 session_history/session_manager 条目 |
| `runtime/uapclaw.go` | import 从 runtime 内部调用改为 `session.*` 包调用 |
| `server/agent_server.go` | import session，run() 中调用 `session.RemoveTeamModeSessionDirsAtStartup()` |
| `server/doc.go` | 添加 session/ 子包条目 |
| `IMPLEMENTATION_PLAN.md` | 10.3.15-18 状态标记更新 |

---

### Task 1: 创建 session 子包骨架 + session_utils.go

**Files:**
- Create: `internal/swarm/server/session/doc.go`
- Create: `internal/swarm/server/session/session_utils.go`
- Create: `internal/swarm/server/session/session_utils_test.go`

- [ ] **Step 1: 创建目录**

```bash
mkdir -p internal/swarm/server/session
```

- [ ] **Step 2: 写 session_utils.go — 从 handle_session.go 提取通用辅助函数**

```go
// Package session 提供会话管理的核心功能，包括历史持久化、元数据管理、任务队列和重命名。
//
// 本包对齐 Python jiuwenswarm/server/runtime/session/ 包的同包结构，
// 使 session_history 和 session_metadata 可以互引，消除跨包依赖。
//
// 文件目录：
//
//	session/
//	├── doc.go              # 包文档
//	├── session_utils.go    # 通用辅助函数（AutoTitle / SerializeValue / deepCopyMap / derefStr / currentTimestamp / makeSessionID / normalizeSessionID）
//	├── session_history.go  # 会话历史持久化（history.json 读写 + team 过滤 + 元数据联动）
//	├── session_manager.go  # SessionManager（LIFO 会话队列）
//	├── session_metadata.go # 会话元数据管理（metadata.json 读写 + delivery context + 异步队列）
//	├── session_rename.go   # 会话重命名三种语义
//	├── session_startup.go  # 启动清理 + 分页查询
//
// 对应 Python 代码：jiuwenswarm/server/runtime/session/
package session
```

- [ ] **Step 3: 写 session_utils.go**

包含从 `handle_session.go` 提取的通用辅助函数：
- `AutoTitle(content string) string` — 截前 50 字符+换行替空格，对齐 Python `_auto_title`
- `SerializeValue(obj any) any` — 递归序列化（time.Time→RFC3339Nano, map/slice 递归），对齐 Python `_serialize_value`
- `deepCopyMap(src map[string]any) map[string]any` — 深拷贝
- `derefStr(s *string, fallback string) string` — 字符串指针解引用
- `currentTimestamp() float64` — UTC 时间戳
- `MakeSessionID() string` — 生成 sess_{hex_ts}_{6_random_hex}（从非导出 makeSessionID 升级）
- `NormalizeSessionID(sessionID string) string` — 空串→default（从非导出 normalizeSessionID 升级）
- 常量 `titleMaxLen = 50`

代码直接复制 `handle_session.go` 中对应函数，修改为 `session` 包导出/非导出。

- [ ] **Step 4: 写 session_utils_test.go**

```go
// TestAutoTitle_基本截取 验证 AutoTitle 截取前 50 字符
// TestAutoTitle_换行替换 验证换行替换为空格
// TestAutoTitle_短内容不截断 验证短内容不加省略号
// TestSerializeValue_timeTime 验证 time.Time 转 RFC3339Nano
// TestSerializeValue_map递归 验证嵌套 map 递归
// TestSerializeValue_slice递归 验证 slice 递归
// TestSerializeValue_普通值不变 验证普通类型原值返回
// TestDeepCopyMap 验证深拷贝不共享引用
// TestDerefStr 验证指针解引用和 fallback
// TestMakeSessionID 验证 sess_ 前缀和格式
// TestNormalizeSessionID 验证空串→default
```

- [ ] **Step 5: 运行测试**

```bash
cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go test ./internal/swarm/server/session/... -v -count=1
```

Expected: 所有测试 PASS

- [ ] **Step 6: Commit**

```bash
git add internal/swarm/server/session/ && git commit -m "feat(session): 创建 session 子包骨架 + session_utils 工具函数"
```

---

### Task 2: session_metadata.go — 提取元数据管理

**Files:**
- Create: `internal/swarm/server/session/session_metadata.go`
- Create: `internal/swarm/server/session/session_metadata_test.go`

- [ ] **Step 1: 写 session_metadata.go**

从 `handle_session.go` 提取以下内容，改为 `session` 包：

**结构体（全部导出或按需导出）：**
- `sessionMetadata` → 保持非导出（内部类型，handler 不直接用）
- `SessionMetadataUpdate` → 保持导出
- `metadataWriteItem` → 保持非导出

**导出函数（从非导出升级为导出）：**
- `updateSessionMetadata` → `UpdateSessionMetadata` — 回填自动标题逻辑
- `incrementSessionRoundCount` → `IncrementSessionRoundCount`
- `readSessionMetadata` → `ReadSessionMetadata`
- `readSessionMetadataWithCache` → `ReadSessionMetadataWithCache`
- `writeSessionMetadata` → `WriteSessionMetadata`

**新增导出函数（对齐 Python）：**
- `InitSessionMetadata(sessionID, channelID, userID, title, mode, teamName string)` — 对齐 Python `init_session_metadata`（同步写，确保创建后立即可读）
- `GetSessionMetadata(sessionID string) map[string]any` — 对齐 Python `get_session_metadata`
- `RemoveSessionMetadataCache(sessionID string)` — 对齐 Python `remove_session_metadata_cache`

**已有导出函数（直接移入）：**
- `SetSessionDeliveryContext`
- `GetSessionDeliveryContext`
- `BuildServerPushMessage`
- `GetSessionsDir`

**常量移入：**
- `metadataFileName`, `metadataQueueSize`, `deliveryContextKind`

**全局变量移入：**
- `deliveryContextCache`, `deliveryContextMu`, `metadataQueue`, `metadataQueueOnce`

**非导出函数移入：**
- `ensureMetadataWorker`, `metadataWriteWorker`, `enqueueMetadataWrite`

**关键变更：UpdateSessionMetadata 回填自动标题**

在 `UpdateSessionMetadata` 中回填两处 ⤵️ 标记：

1. 创建新 metadata 时：
```go
autoTitle := ""
userContentStr := derefStr(update.UserContent, "")
if update.Title == nil && userContentStr != "" {
    autoTitle = AutoTitle(userContentStr)
}
// ... metadata["title"] = derefStr(update.Title, autoTitle)
```

2. 更新现有 metadata 时：
```go
currentTitle, _ := metadata["title"].(string)
if currentTitle == "" && update.UserContent != nil && *update.UserContent != "" {
    metadata["title"] = AutoTitle(*update.UserContent)
}
```

- [ ] **Step 2: 写 session_metadata_test.go**

从 `handle_session_test.go` 提取 metadata 相关测试，改为 `session` 包：
- `TestReadSessionMetadata`
- `TestReadSessionMetadata_不存在`
- `TestWriteSessionMetadata`
- `TestMakeSessionID` → 改为 `TestMakeSessionID`（调用 `session.MakeSessionID()`）
- 新增 `TestUpdateSessionMetadata_自动标题创建` — 验证新 metadata 时自动标题
- 新增 `TestUpdateSessionMetadata_自动标题回填` — 验证更新 metadata 时自动标题
- 新增 `TestInitSessionMetadata` — 验证同步写入后立即可读
- 新增 `TestGetSessionMetadata` — 验证读取
- 新增 `TestRemoveSessionMetadataCache` — 验证缓存清除

- [ ] **Step 3: 运行测试**

```bash
cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go test ./internal/swarm/server/session/... -v -count=1
```

Expected: 所有测试 PASS

- [ ] **Step 4: Commit**

```bash
git add internal/swarm/server/session/ && git commit -m "feat(session): 提取 session_metadata.go + 回填自动标题生成"
```

---

### Task 3: session_rename.go — 提取重命名逻辑

**Files:**
- Create: `internal/swarm/server/session/session_rename.go`

- [ ] **Step 1: 写 session_rename.go**

提取 Python `apply_session_rename` 的等价逻辑为导出函数：

```go
// ApplySessionRename 实现会话重命名三种语义：查询(title=nil) / 清除(空串) / 设置(非空)。
// 对齐 Python apply_session_rename(params, connection_session_id, init_channel_id)
// 返回 (payload, previousTitle, updatedTitle, error)
func ApplySessionRename(
    target string,
    title *string,
    initChannelID string,
) (map[string]any, string, string, error) { ... }
```

内部调用 `GetSessionMetadata`、`InitSessionMetadata`、`UpdateSessionMetadata`。

常量 `renameTitleMaxLen = 200` 移入此文件。

- [ ] **Step 2: 运行编译验证**

```bash
cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go build ./internal/swarm/server/session/...
```

Expected: 编译成功

- [ ] **Step 3: Commit**

```bash
git add internal/swarm/server/session/ && git commit -m "feat(session): 提取 session_rename.go 三种语义重命名"
```

---

### Task 4: session_history.go — 移入 + 元数据联动 + SerializeValue + ReadTeamHistoryRecords + TruncateHistoryRecords cutIndex

**Files:**
- Create: `internal/swarm/server/session/session_history.go`
- Create: `internal/swarm/server/session/session_history_test.go`

- [ ] **Step 1: 写 session_history.go**

从 `runtime/session_history.go` 移入，包名改为 `session`，并做以下变更：

**变更 1：AppendHistoryRecord 增加元数据联动**

在入队成功后，启动 goroutine 联动调用 UpdateSessionMetadata + SetSessionDeliveryContext：

```go
// 对齐 Python append_history_record 内部的元数据联动（第 176-200 行）
// 联动失败仅 log.Warn，不影响主流程
go func() {
    defer func() {
        if r := recover(); r != nil {
            logger.Warn(logComponent).Any("recover", r).Msg("会话元数据联动 panic 恢复")
        }
    }()
    userContent := contentText
    if roleNorm != "user" {
        userContent = ""
    }
    UpdateSessionMetadata(SessionMetadataUpdate{
        SessionID:             sid,
        ChannelID:             ptrStr(cid),
        IncrementMessageCount: true,
        UserContent:           ptrStr(userContent),
        ChannelMetadata:       channelMetadata,
        Mode:                  ptrStr(mode),
    })
    if roleNorm == "user" {
        SetSessionDeliveryContext(sid, ptrStr(cid), ptrStr(rid), channelMetadata)
    }
}()
```

其中 `ptrStr(s string) *string` 是新增辅助函数（字符串→指针）。

**变更 2：extra 展开使用 SerializeValue**

```go
// 原代码：
// if len(extra) > 0 { for k, v := range extra { item[k] = v } }
// 改为：
serializedExtra := SerializeValue(extra)
if m, ok := serializedExtra.(map[string]any); ok && len(m) > 0 {
    for k, v := range m {
        item[k] = v
    }
}
```

**变更 3：新增 ReadTeamHistoryRecords + IsTeamRelevant**

```go
// ──────────────────────────── 常量 ────────────────────────────

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

// teamHistoryReadMaxRetries 读取重试次数，对齐 Python read_team_history_records 最多 5 次
const teamHistoryReadMaxRetries = 5

// ──────────────────────────── 导出函数 ────────────────────────────

// IsTeamRelevant 判断记录是否为 team 相关事件。
// 对齐 Python _is_team_relevant(item)
func IsTeamRelevant(item map[string]any) bool {
    et, ok := item["event_type"].(string)
    if !ok || !teamRelevantEventTypes[et] {
        return false
    }
    switch et {
    case "chat.tool_call", "chat.tracer_agent":
        mode, _ := item["mode"].(string)
        return strings.TrimSpace(strings.ToLower(mode)) == "team"
    case "chat.final", "chat.tool_result", "chat.file":
        role, _ := item["role"].(string)
        return strings.TrimSpace(strings.ToLower(role)) == "teammate"
    case "team.message":
        return true
    default:
        return false
    }
}

// ReadTeamHistoryRecords 读取指定会话的 team 相关历史记录。
// 对齐 Python read_team_history_records(session_id)
// 带 5 次递增间隔重试（0.2s × attempt），防止读到截断窗口空文件。
func ReadTeamHistoryRecords(sessionID string) ([]map[string]any, error) {
    sid := NormalizeSessionID(sessionID)
    fpath := historyFilePath(sid)

    allRecords, err := ReadHistoryRecords(sid)
    if err != nil {
        return nil, err
    }

    // 重试：空结果且文件存在时触发
    if len(allRecords) == 0 {
        if _, statErr := os.Stat(fpath); statErr == nil {
            for attempt := 1; attempt <= teamHistoryReadMaxRetries; attempt++ {
                time.Sleep(time.Duration(200*attempt) * time.Millisecond)
                allRecords, err = ReadHistoryRecords(sid)
                if err != nil {
                    return nil, err
                }
                if len(allRecords) > 0 {
                    logger.Info(logComponent).
                        Int("attempt", attempt).
                        Msg("ReadTeamHistoryRecords 重试成功")
                    break
                }
            }
            if len(allRecords) == 0 {
                logger.Warn(logComponent).Msg("ReadTeamHistoryRecords 重试耗尽，文件可能为空")
            }
        }
    }

    // 过滤 team 相关记录
    result := make([]map[string]any, 0)
    for _, item := range allRecords {
        if IsTeamRelevant(item) {
            result = append(result, item)
        }
    }
    return result, nil
}
```

**变更 4：TruncateHistoryRecords 改为 cutIndex**

```go
// TruncateResult 截断结果，对齐 Python truncate_history_records 返回 dict
type TruncateResult struct {
    // RemainingRecords 保留记录数
    RemainingRecords int
    // RemovedRecords 删除记录数
    RemovedRecords int
}

// TruncateHistoryRecords 截断 history 到指定位置索引（rewind 使用）。
// 对齐 Python truncate_history_records(session_id, cut_index: int) → dict
// 先等异步队列刷盘，再持锁截断。
func TruncateHistoryRecords(sessionID string, cutIndex int) (TruncateResult, error) {
    sid := NormalizeSessionID(sessionID)
    // 等 history 写入队列刷盘（对齐 Python _WRITE_QUEUE.join()）
    // Go chan 不支持 join()，用 sleep 短暂等待 + 持锁截断保证顺序
    fpath := historyFilePath(sid)

    historyFileMu.Lock()
    defer historyFileMu.Unlock()

    records, err := readHistoryFile(fpath)
    if err != nil {
        if os.IsNotExist(err) {
            return TruncateResult{}, nil
        }
        return TruncateResult{}, err
    }

    total := len(records)
    if cutIndex < 0 {
        cutIndex = 0
    }
    if cutIndex > total {
        cutIndex = total
    }

    truncated := records[:cutIndex]
    if err := writeHistoryFile(fpath, truncated); err != nil {
        return TruncateResult{}, err
    }
    return TruncateResult{
        RemainingRecords: len(truncated),
        RemovedRecords:   total - len(truncated),
    }, nil
}
```

- [ ] **Step 2: 写 session_history_test.go**

从 `runtime/session_history_test.go` 移入，包名改为 `session`，并做以下变更：
- `resetWorkspaceCache` → 改为调用 `path.ResetCache()`
- `TestTruncateHistoryRecords` → 改为传 cutIndex（int）而非 requestID（string），验证返回 `TruncateResult`
- 新增 `TestIsTeamRelevant_teamMessage始终保留`
- 新增 `TestIsTeamRelevant_toolCall仅TeamMode`
- 新增 `TestIsTeamRelevant_final仅TeammateRole`
- 新增 `TestReadTeamHistoryRecords_过滤team记录`
- 新增 `TestReadTeamHistoryRecords_空文件重试`

- [ ] **Step 3: 运行测试**

```bash
cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go test ./internal/swarm/server/session/... -v -count=1
```

Expected: 所有测试 PASS

- [ ] **Step 4: Commit**

```bash
git add internal/swarm/server/session/ && git commit -m "feat(session): 移入 session_history + 元数据联动 + SerializeValue + ReadTeamHistoryRecords + TruncateHistoryRecords cutIndex"
```

---

### Task 5: session_manager.go — 从 runtime 移入

**Files:**
- Create: `internal/swarm/server/session/session_manager.go`
- Create: `internal/swarm/server/session/session_manager_test.go`

- [ ] **Step 1: 写 session_manager.go**

从 `runtime/session_manager.go` 复制，包名改为 `session`。无逻辑变更，仅包名 + import 调整。

注意：`normalizeSessionID` 已在 `session_utils.go` 中导出为 `NormalizeSessionID`，session_manager.go 中调用改为 `NormalizeSessionID`。

- [ ] **Step 2: 写 session_manager_test.go**

从 `runtime/session_manager_test.go` 复制，包名改为 `session`。

- [ ] **Step 3: 运行测试**

```bash
cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go test ./internal/swarm/server/session/... -v -count=1
```

Expected: 所有测试 PASS

- [ ] **Step 4: Commit**

```bash
git add internal/swarm/server/session/ && git commit -m "feat(session): 移入 session_manager.go LIFO 任务队列"
```

---

### Task 6: session_startup.go — RemoveTeamMode + GetAllSessions

**Files:**
- Create: `internal/swarm/server/session/session_startup.go`
- Create: `internal/swarm/server/session/session_startup_test.go`

- [ ] **Step 1: 写 session_startup.go**

```go
// RemoveTeamModeSessionDirsAtStartup AgentServer 启动时删除 mode=team 的会话目录。
// 对齐 Python remove_team_mode_session_dirs_at_startup()
func RemoveTeamModeSessionDirsAtStartup() {
    sessionsDir := GetSessionsDir()
    entries, err := os.ReadDir(sessionsDir)
    if err != nil {
        if os.IsNotExist(err) {
            return
        }
        logger.Warn(logComponent).Err(err).Msg("扫描会话目录失败")
        return
    }

    removed := 0
    for _, entry := range entries {
        if !entry.IsDir() {
            continue
        }
        sessionID := entry.Name()
        meta := ReadSessionMetadata(sessionsDir, sessionID)
        if meta == nil {
            continue
        }
        mode, _ := meta["mode"].(string)
        if mode != "team" {
            continue
        }
        sessionDir := filepath.Join(sessionsDir, sessionID)
        if err := os.RemoveAll(sessionDir); err != nil {
            logger.Warn(logComponent).Err(err).Str("session_id", sessionID).Msg("删除 team 会话目录失败")
            continue
        }
        RemoveSessionMetadataCache(sessionID)
        removed++
    }
    if removed > 0 {
        logger.Info(logComponent).Int("removed", removed).Msg("启动清理：已删除 team 模式会话目录")
    }
}

// GetAllSessionsMetadata 分页获取所有会话元数据。
// 对齐 Python get_all_sessions_metadata(limit, offset) → (sessions, total)
func GetAllSessionsMetadata(limit int, offset int) ([]map[string]any, int) {
    sessionsDir := GetSessionsDir()
    entries, err := os.ReadDir(sessionsDir)
    if err != nil {
        return []map[string]any{}, 0
    }

    var sessions []map[string]any
    for _, entry := range entries {
        if !entry.IsDir() {
            continue
        }
        sessionID := entry.Name()
        // 跳过心跳会话
        if strings.HasPrefix(sessionID, heartbeatSessionPrefix) {
            continue
        }
        meta := ReadSessionMetadata(sessionsDir, sessionID)
        if meta == nil {
            // 无 metadata.json 的旧会话，构造最小信息
            info, statErr := entry.Info()
            mtime := float64(0)
            if statErr == nil {
                mtime = float64(info.ModTime().UnixMilli()) / 1000.0
            }
            meta = map[string]any{
                "session_id":      sessionID,
                "channel_id":      "",
                "user_id":         "",
                "created_at":      mtime,
                "last_message_at": mtime,
                "title":           "",
                "message_count":   0,
                "mode":            "unknown",
            }
        }
        meta["session_id"] = sessionID
        sessions = append(sessions, meta)
    }

    // 按 last_message_at 降序排列
    sort.Slice(sessions, func(i, j int) bool {
        iv, _ := sessions[i]["last_message_at"].(float64)
        jv, _ := sessions[j]["last_message_at"].(float64)
        return iv > jv
    })

    total := len(sessions)
    if offset >= total {
        return []map[string]any{}, total
    }
    end := offset + limit
    if end > total {
        end = total
    }
    return sessions[offset:end], total
}
```

常量 `heartbeatSessionPrefix` 移入此文件（从 handle_session.go 移出）。

- [ ] **Step 2: 写 session_startup_test.go**

```go
// TestRemoveTeamModeSessionDirsAtStartup_删除team会话 验证 mode=team 的目录被删除
// TestRemoveTeamModeSessionDirsAtStartup_保留非team会话 验证 mode!=team 的目录保留
// TestRemoveTeamModeSessionDirsAtStartup_空目录 验证目录不存在时不报错
// TestGetAllSessionsMetadata_分页 验证 limit/offset 分页
// TestGetAllSessionsMetadata_跳过心跳 验证 heartbeat_ 前缀被跳过
// TestGetAllSessionsMetadata_空目录 验证空目录返回空列表
```

- [ ] **Step 3: 运行测试**

```bash
cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go test ./internal/swarm/server/session/... -v -count=1
```

Expected: 所有测试 PASS

- [ ] **Step 4: Commit**

```bash
git add internal/swarm/server/session/ && git commit -m "feat(session): 添加 RemoveTeamModeSessionDirsAtStartup + GetAllSessionsMetadata 分页"
```

---

### Task 7: 删除 runtime 旧文件 + 更新 handle_session.go + 更新所有引用

**Files:**
- Delete: `internal/swarm/server/runtime/session_history.go`
- Delete: `internal/swarm/server/runtime/session_history_test.go`
- Delete: `internal/swarm/server/runtime/session_manager.go`
- Delete: `internal/swarm/server/runtime/session_manager_test.go`
- Modify: `internal/swarm/server/handle_session.go`
- Modify: `internal/swarm/server/runtime/doc.go`
- Modify: `internal/swarm/server/runtime/uapclaw.go`
- Modify: `internal/swarm/server/agent_server.go`
- Modify: `internal/swarm/server/doc.go`

- [ ] **Step 1: 删除 runtime 旧文件**

```bash
rm internal/swarm/server/runtime/session_history.go
rm internal/swarm/server/runtime/session_history_test.go
rm internal/swarm/server/runtime/session_manager.go
rm internal/swarm/server/runtime/session_manager_test.go
```

- [ ] **Step 2: 更新 handle_session.go — 删除移入子包的代码，handler 改为调用 session 包**

需要删除的内容（大约 500 行）：
- `sessionMetadata` 结构体
- `SessionMetadataUpdate` 结构体
- `metadataWriteItem` 结构体
- `sessionRenameParams` 等结构体 → 保留（handler 参数）
- `SetSessionDeliveryContext` 函数 → 删除，handler 改为调用 `session.SetSessionDeliveryContext`
- `GetSessionDeliveryContext` → 删除
- `BuildServerPushMessage` → 删除
- `GetSessionsDir` → 删除
- `readSessionMetadata` → 删除
- `writeSessionMetadata` → 删除
- `makeSessionID` → 删除
- `currentTimestamp` → 删除
- `readSessionMetadataWithCache` → 删除
- `deepCopyMap` → 删除
- `incrementSessionRoundCount` → 删除
- `ensureMetadataWorker` → 删除
- `metadataWriteWorker` → 删除
- `enqueueMetadataWrite` → 删除
- `updateSessionMetadata` → 删除
- `derefStr` → 删除
- 所有 metadata 常量和全局变量 → 删除
- `heartbeatSessionPrefix` → 删除（已移入 session_startup.go）

需要修改的 handler 方法：
- `handleSessionList` → 改为调用 `session.GetAllSessionsMetadata(0, 0)` 取所有
- `handleSessionRename` → 改为调用 `session.ApplySessionRename`
- `handleSessionDelete` → 改为调用 `session.RemoveSessionMetadataCache(params.SessionID)`
- `handleSessionCreate` → 改为调用 `session.InitSessionMetadata` + `session.MakeSessionID`

添加 import: `"github.com/uapclaw/uapclaw-go/internal/swarm/server/session"`

- [ ] **Step 3: 更新 runtime/uapclaw.go — import session 包**

将所有 `AppendHistoryRecord`、`AppendCompactHistoryFromPayload`、`AppendCompactHistoryRecords`、`ReadHistoryRecords`、`TruncateHistoryRecords` 调用改为 `session.*`。

添加 import: `"github.com/uapclaw/uapclaw-go/internal/swarm/server/session"`

`normalizeSessionID` → `session.NormalizeSessionID`
`SessionManager` → `session.SessionManager`（需确认 session_manager.go 中 SessionManager 是否需要调整）

- [ ] **Step 4: 更新 runtime/doc.go — 移除 session_history/session_manager 条目**

从文件目录树中删除：
```
├── session_history.go    # ...
├── session_manager.go    # ...
```

- [ ] **Step 5: 更新 server/agent_server.go — import session，调用 RemoveTeamMode**

在 `run()` 方法的步骤 1（重置 harness 状态）之前添加：

```go
// 0. 清理 team 模式会话目录（对齐 Python remove_team_mode_session_dirs_at_startup）
session.RemoveTeamModeSessionDirsAtStartup()
```

添加 import: `"github.com/uapclaw/uapclaw-go/internal/swarm/server/session"`

- [ ] **Step 6: 更新 server/doc.go — 添加 session/ 子包条目**

在文件目录树中添加：
```
└── session/              # 会话管理子包（history/metadata/manager/rename/startup）
```

- [ ] **Step 7: 更新 handle_session_test.go — import session 包**

如果测试中有调用 `makeSessionID`、`readSessionMetadata`、`writeSessionMetadata` 等非导出函数，改为调用 `session.MakeSessionID`、`session.ReadSessionMetadata`、`session.WriteSessionMetadata` 等。

- [ ] **Step 8: 编译验证**

```bash
cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go build ./...
```

Expected: 编译成功

- [ ] **Step 9: 运行所有相关测试**

```bash
cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go test ./internal/swarm/server/session/... ./internal/swarm/server/... ./internal/swarm/server/runtime/... -v -count=1
```

Expected: 所有测试 PASS

- [ ] **Step 10: Commit**

```bash
git add -A && git commit -m "refactor(session): 删除 runtime/server 旧文件，所有引用改为 session 子包"
```

---

### Task 8: 更新 IMPLEMENTATION_PLAN.md 状态标记

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新 10.3.15-18 状态**

将 `10.3.15-18 | 🔄 | 会话管理 | SessionManager(LIFO)✅ / SessionHistory(JSONL)☐ / SessionMetadata✅ / SessionRename✅` 改为：

`10.3.15-18 | ✅ | 会话管理 | SessionManager(LIFO)✅ / SessionHistory(JSONL)✅ / SessionMetadata✅ / SessionRename✅；✅ 子包提取(server/session/)消除跨包依赖；✅ AppendHistoryRecord 元数据联动(UpdateSessionMetadata+SetSessionDeliveryContext)；✅ ReadTeamHistoryRecords+IsTeamRelevant(team过滤+重试)；✅ TruncateHistoryRecords cutIndex对齐Python；✅ SerializeValue递归序列化；✅ AutoTitle回填⤵️11.x自动标题；✅ RemoveTeamModeSessionDirsAtStartup；✅ GetAllSessionsMetadata分页`

- [ ] **Step 2: Commit**

```bash
git add IMPLEMENTATION_PLAN.md && git commit -m "docs: 更新 10.3.15-18 实现计划状态为 ✅"
```

---

### Task 9: 最终编译 + 全量测试验证

- [ ] **Step 1: 全量编译**

```bash
cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go build ./...
```

Expected: 编译成功

- [ ] **Step 2: 全量测试**

```bash
cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go test -cover ./internal/swarm/server/... ./internal/swarm/server/session/... ./internal/swarm/server/runtime/...
```

Expected: 覆盖率 ≥ 85%，所有测试 PASS

- [ ] **Step 3: 覆盖率检查**

```bash
cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go test -coverprofile=coverage.out ./internal/swarm/server/session/... && go tool cover -func=coverage.out | tail -1
```

Expected: session 包覆盖率 ≥ 85%
