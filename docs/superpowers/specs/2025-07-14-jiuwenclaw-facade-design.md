# JiuWenClaw 门面实现设计文档（层级 0+1）

> 目标：实现 JiuWenClaw 门面的核心逻辑（层级 0：结构体改造 + 层级 1：辅助函数），
> 层级 2-4 先 stub 并标注 ⤵️ 回填点。
> 完成后，JiuWenClaw 的方法逻辑对齐 Python，但 adapter 调用仍走 DeepAdapter 骨架，
> LLM 调用暂时不生效，需后续回填层级 3-4。

---

## 1. 当前状态

### 1.1 Go 现状

`internal/swarm/server/runtime/jiowenclaw.go`：纯 stub，空结构体，所有方法返回硬编码值。

```go
type JiuWenClaw struct{}  // 空结构体，无字段
```

### 1.2 Python 对标

`jiuwenswarm/server/runtime/agent_adapter/interface.py`：JiuWenClaw 是 Agent 统一门面，持有：
- `_adapter: AgentAdapter` — SDK 适配器（DeepAdapter/CodeAdapter），延迟初始化
- `_sdk_name: str` — SDK 名称，仅用于日志
- `_skill_manager: SkillManager` — server 层技能管理器（处理 skills.* RPC）
- `_session_manager: SessionManager` — 会话任务队列
- `_skilldev_service` — SkillDev 服务，懒初始化

### 1.3 依赖矩阵

| 依赖组件 | Go 实现状态 | 层级 |
|---------|-----------|------|
| `AgentAdapter` 接口 + 工厂 | ✅ 已实现 | 0 |
| `SessionManager` | ✅ 已实现 | 0 |
| `DeepAdapter` / `CodeAdapter` | ⚠️ 骨架（CreateInstance/ProcessMessageStreamImpl 待回填） | 3 |
| server 层 `SkillManager` | ❌ 不存在 | 2 |
| `SkillDevService` | ❌ 不存在 | 2 |
| `buildUserPrompt()` | ❌ 不存在 | 1 |
| `buildInputs()` | ❌ 不存在 | 1 |
| `appendHistoryRecord()` | ❌ 不存在 | 1 |
| Rails（15 个） | ❌ interface{} 占位 | 4 |

---

## 2. 层级 0：结构体改造

### 2.1 JiuWenClaw 结构体

```go
// JiuWenClaw Agent 统一门面。
//
// 提供：SDK 适配器路由、统一对外 API、公共编排
// （session 队列、Skills 路由、heartbeat、流式包装）。
//
// 对齐 Python: jiuwenswarm/server/runtime/agent_adapter/interface.py (JiuWenClaw)
type JiuWenClaw struct {
    // adapter SDK 适配器（延迟初始化，ensureAdapter 时创建）。
    adapter adapter.AgentAdapter

    // skillManager 技能管理器（server 层）。
    // ⤵️ 10.3.2: 替换为 swarm/server/runtime/skill.SkillManager 实例
    skillManager interface{}

    // sessionManager 会话任务队列管理器。
    sessionManager *SessionManager

    // skilldevService SkillDev 服务（懒初始化）。
    // ⤵️ 10.3.2: 替换为 SkillDevService 实例
    skilldevService interface{}

    // mu 保护 adapter 字段的并发访问（ensureAdapter 可能被多 goroutine 并发调用）。
    adapterMu sync.Mutex
}
```

**与 Python 的差异**：
- Go 无 `_sdk_name` 字段：`CreateAdapter(sdk, mode)` 传空串时内部自动调用 `ResolveSDKChoice()`，日志需要时临时调用即可
- Go 增加 `adapterMu sync.Mutex`：Python 依赖 asyncio 单线程天然安全，Go 多 goroutine 需要显式加锁
- `skillManager` / `skilldevService` 声明为 `interface{}` stub，标注 ⤵️ 回填

### 2.2 构造函数

```go
// NewJiuWenClaw 创建 JiuWenClaw 实例。
//
// 对齐 Python: JiuWenClaw.__init__()
func NewJiuWenClaw() *JiuWenClaw {
    return &JiuWenClaw{
        sessionManager: NewSessionManager(),
        // skillManager 和 skilldevService 延迟到后续回填
    }
}
```

---

## 3. 层级 1：辅助函数

### 3.1 buildUserPrompt

对齐 Python `build_user_prompt()`（interface.py:162-226）。

**职责**：将用户原始文本 query 包装为结构化 JSON 字符串，包含 source、language、content、type、files、timestamp 等字段。

```go
// BuildUserPrompt 将用户 query 包装为结构化 JSON prompt。
//
// 对齐 Python: build_user_prompt(content, files, channel, language, *, trusted_dirs, metadata)
//
// 返回格式: interaction_prefix + prompt_prefix + json.dumps(userMessageContext)
func BuildUserPrompt(content string, files map[string]any, channel string, language string,
    trustedDirs []string, metadata map[string]any) string
```

**实现步骤**：

1. **interaction_context 前缀**：从 metadata["interaction_context"] 提取，非空时加 `\n{ctx}\n\n` 前缀
2. **/skills use 斜杠命令解析**：匹配 `/skills use {skill_name}, {query}` 模式，提取 skills_to_use
3. **按语言+channel 构建 prompt 前缀**：
   - zh: `"你收到一条消息：\n"`（cron 模式额外追加查询输出要求）
   - en: `"You receive a new message:\n"`（同上）
4. **构建 userMessageContext**：
   ```go
   {
       "source":                       channel,                          // cron/heartbeat 时为 "system"
       "timezone":                     "Asia/Shanghai",
       "timestamp":                    "2025-07-14 10:30:00",           // 当前时间 UTC+8
       "preferred_response_language":  language,
       "content":                      content,
       "files_updated_by_user":        json.dumps(files),
       "type":                         "user input",                    // cron/heartbeat 时为 channel 值
       // 可选字段：
       "skills_to_use":                [...],                          // 仅 /skills use 时
       "trusted_dirs":                 json.dumps(trustedDirs),        // 仅非空时
   }
   ```
5. **返回**：`interactionPrefix + promptPrefix + json.Marshal(userMessageContext)`

**文件位置**：`internal/swarm/server/runtime/build_user_prompt.go`

### 3.2 buildInputs

对齐 Python `_build_inputs()`（interface.py:349-452）。

**职责**：从 AgentRequest 提取字段，调用 BuildUserPrompt 包装 query，组装 adapter 所需的 inputs 字典。

```go
// BuildInputs 构建 adapter 所需的 inputs 字典。
//
// 对齐 Python: JiuWenClaw._build_inputs(request) -> (inputs, memoryMode, rawQuery)
//
// 返回: inputs 字典、memoryMode 字符串、原始 query
func (jw *JiuWenClaw) BuildInputs(request *schema.AgentRequest) (inputs map[string]any, memoryMode string, rawQuery string)
```

**实现步骤**：

1. **获取配置**：`config.New().Load()` → `configBase`
2. **提取基础字段**：
   - `query` = `params["query"]`
   - `channel` = `sessionID` 的第一个 `_` 前部分，默认 `"web"`
   - `language` = `configBase["preferred_language"]`，默认 `"zh"`
3. **提取 trusted_dirs**：从 `params["trusted_dirs"]` 列表
4. **提取 project_dir / cwd**：params 优先，metadata 兜底
5. **构建 finalQuery**：
   - 若 query 是 InteractiveInput 实例 → 直接使用
   - ⤵️ 若 params 有 `answers` → 构建 InteractiveInput（当前 stub，fallback 到 BuildUserPrompt）
   - 否则 → `BuildUserPrompt(query, files, channel, language, trustedDirs, metadata)`
6. **组装 inputs 字典**：
   ```go
   {
       "conversation_id":  sessionID,
       "query":            finalQuery,
       "channel":          channel,
       "language":         language,
       "enable_memory":    true,              // 从 metadata，默认 true
       // 可选：
       "trusted_dirs":     [...],
       "project_dir":      "...",
       "cwd":              "...",
       "run":              {...},             // 从 params["run"] 或 cron 转换
   }
   ```
7. **cron 字段转换**：若 params 有 `cron`，转换为 `run = {kind: "cron", context: {extra: {cron: ...}}}`
8. **返回**：`(inputs, memoryMode, rawQuery)`

**文件位置**：`internal/swarm/server/runtime/build_inputs.go`

### 3.3 appendHistoryRecord

对齐 Python `append_history_record()`（session_history.py:132-201）。

**职责**：将消息记录异步追加到 `~/.uapclaw/agent/sessions/{session_id}/history.json`。

```go
// AppendHistoryRecord 向指定 session 的 history.json 异步追加一条记录。
//
// 对齐 Python: append_history_record(session_id, request_id, channel_id, role, content, timestamp, event_type, extra, channel_metadata, mode)
func AppendHistoryRecord(sessionID, requestID, channelID, role, content string,
    timestamp float64, eventType string, extra map[string]any,
    channelMetadata map[string]any, mode string)
```

**记录格式**：

```json
{
    "id": "request_id:role",
    "role": "user" | "assistant",
    "request_id": "abc-123",
    "channel_id": "ch1",
    "timestamp": 1720500000.123,
    "content": "消息文本",
    "event_type": "chat.final",
    "mode": "agent"
}
```

**role 归一化**：只有 `"assistant"` 和 `"user"` 两种，非 assistant 一律为 user。

**event_type**：仅在 `role == "assistant"` 且 event_type 非空时写入。

**extra 字段**：展开到记录顶层（merge）。

**写入基础设施**：

```go
// 全局写入队列（对齐 Python _WRITE_QUEUE）
var (
    historyWriteQueue chan historyWriteItem  // 容量 20000
    historyFileMu     sync.Mutex            // 文件锁（read-modify-write 期间持锁）
    historyWorkerOnce sync.Once             // 保证 worker 只启动一次
)

type historyWriteItem struct {
    sessionID string
    record    map[string]any
}
```

**写入模式**：

1. `AppendHistoryRecord()` 构建记录 → 尝试入队 `historyWriteQueue`
2. 队列满时退化为同步写（避免丢失记录）
3. Worker goroutine（单消费者）从队列取出 → `writeHistoryItem()` → 持文件锁 → 读全量 → append → 写全量
4. 首次调用时 `sync.Once` 启动 worker goroutine

**文件路径**：

```
history.json 路径 = {getAgentSessionsDir()}/{sessionID}/history.json
                  = {workspace}/agent/sessions/{sessionID}/history.json
```

`getAgentSessionsDir()` 对齐 Python `get_agent_sessions_dir()`，受 `JIUWENSWARM_DATA_DIR` 环境变量控制。

**辅助函数**：

```go
// ReadHistoryRecords 读取指定 session 的全部 history 记录。
func ReadHistoryRecords(sessionID string) ([]map[string]any, error)

// TruncateHistoryRecords 截断 history 到指定 request_id（rewind 使用）。
func TruncateHistoryRecords(sessionID string, requestID string) error
```

**文件位置**：`internal/swarm/server/runtime/session_history.go`

---

## 4. JiuWenClaw 方法实现

### 4.1 ensureAdapter

对齐 Python `_ensure_adapter(mode)`（interface.py:281-292）。

```go
// ensureAdapter 确保 SDK adapter 已初始化，幂等。
//
// 对齐 Python: JiuWenClaw._ensure_adapter(mode)
func (jw *JiuWenClaw) ensureAdapter(mode string) (adapter.AgentAdapter, error) {
    jw.adapterMu.Lock()
    defer jw.adapterMu.Unlock()

    if jw.adapter != nil {
        return jw.adapter, nil
    }

    // 创建 adapter（sdk 传空串，内部自动 ResolveSDKChoice）
    a, err := adapter.CreateAdapter("", mode)
    if err != nil {
        return nil, err
    }

    // ⤵️ 10.3.2: 若 adapter 有 SetSkillManager 方法，注入 skillManager
    // ⤵️ 10.3.2: 设置 skillManager 的 skillnet_install_complete_hook

    jw.adapter = a
    logger.Info(logComponent).
        Str("sdk", adapter.ResolveSDKChoice()).
        Str("mode", mode).
        Msg("JiuWenClaw adapter 已初始化")

    return a, nil
}
```

### 4.2 CreateInstance

对齐 Python `create_instance(config, mode, sub_mode)`（interface.py:306-322）。

```go
func (jw *JiuWenClaw) CreateInstance(config map[string]any, mode string, subMode string) error {
    a, err := jw.ensureAdapter(mode)
    if err != nil {
        return err
    }

    ctx := context.Background()
    if err := a.CreateInstance(ctx, config, mode, subMode); err != nil {
        return err
    }

    logger.Info(logComponent).
        Str("sdk", adapter.ResolveSDKChoice()).
        Str("mode", mode).
        Str("sub_mode", subMode).
        Msg("JiuWenClaw Agent 实例已创建")

    // ⤵️ 10.3.2: 启动 dreaming 后台任务（adapter.TryStartDreaming）

    return nil
}
```

### 4.3 ProcessMessage

对齐 Python `process_message(request)`（interface.py:784-870）。

```go
func (jw *JiuWenClaw) ProcessMessage(ctx context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
    // 1. CANCEL 分支 → 委托 ProcessInterrupt
    if request.ReqMethod == schema.ReqMethodChatCancel {
        return jw.ProcessInterrupt(ctx, request)
    }

    // 2. 确保 adapter
    mode := jw.adapterModeForRequest(request)
    a, err := jw.ensureAdapter(mode)
    if err != nil {
        return nil, err
    }

    // 3. ANSWER 分支 → adapter.HandleUserAnswer
    if request.ReqMethod == schema.ReqMethodChatAnswer {
        return a.HandleUserAnswer(ctx, request)
    }

    // 4. heartbeat 分支
    if resp, err := a.HandleHeartbeat(ctx, request); resp != nil {
        return resp, err
    }

    // 5. Skills 分支
    // ⤵️ 10.3.2: handleSkillsRequest(request) — 当前 stub，返回 nil 不拦截
    // ⤵️ 10.3.2: handleSkillDevRequest(request) — 当前 stub，返回 nil 不拦截
    // ⤵️ 10.3.2: handlePluginsRequest(request) — 当前 stub，返回 nil 不拦截

    // 6. 常规对话
    sessionID := GetSessionID(request.SessionIDStr())

    // 记录 user 历史
    AppendHistoryRecord(
        sessionID, request.RequestID, request.ChannelID,
        "user", jw.extractQuery(request), float64(time.Now().UnixMilli())/1000,
        "", nil, nil, "",
    )

    // 构建 inputs
    inputs, _, _ := jw.BuildInputs(request)

    // ⤵️ 10.3.2: cloud memory before-chat hook（ExtensionRegistry）

    // 提交到 session 队列并等待结果
    result, err := jw.sessionManager.SubmitAndWait(ctx, sessionID, func(taskCtx context.Context) (any, error) {
        return a.ProcessMessageImpl(taskCtx, request, inputs)
    })
    if err != nil {
        return nil, err
    }

    resp, ok := result.(*schema.AgentResponse)
    if !ok || resp == nil {
        return schema.NewAgentResponse(request.RequestID, request.ChannelID,
            schema.WithResponseOK(true),
        ), nil
    }

    // 记录 assistant 历史
    if resp.OK {
        content := jw.extractResponseContent(resp)
        AppendHistoryRecord(
            sessionID, request.RequestID, request.ChannelID,
            "assistant", content, float64(time.Now().UnixMilli())/1000,
            "chat.final", nil, nil, "",
        )
    }

    // ⤵️ 10.3.2: cloud memory after-chat hook

    return resp, nil
}
```

### 4.4 ProcessMessageStream

对齐 Python `process_message_stream(request)`（interface.py:881-1153）。

```go
func (jw *JiuWenClaw) ProcessMessageStream(ctx context.Context, request *schema.AgentRequest) (<-chan *schema.AgentResponseChunk, error) {
    // 1. SkillDev 流式分支
    // ⤵️ 10.3.2: handleSkillDevStreamRequest(request) — 当前 stub

    // 2. 确保 adapter
    mode := jw.adapterModeForRequest(request)
    a, err := jw.ensureAdapter(mode)
    if err != nil {
        return nil, err
    }

    // 3. 提取 sessionID / query
    sessionID := GetSessionID(request.SessionIDStr())

    // ⤵️ 10.3.2: Team 模式判断（isTeamMode / isAutoHarnessResume）
    // ⤵️ 10.3.2: Team 模式使用原始 query（不经过 BuildUserPrompt 包装）

    // 4. 记录 user 历史
    AppendHistoryRecord(
        sessionID, request.RequestID, request.ChannelID,
        "user", jw.extractQuery(request), float64(time.Now().UnixMilli())/1000,
        "", nil, nil, "",
    )

    // 5. 构建 inputs
    inputs, _, _ := jw.BuildInputs(request)

    // ⤵️ 10.3.2: cloud memory before-chat hook

    // 6. 创建中转 channel + done 信号
    outCh := make(chan *schema.AgentResponseChunk, 64)
    streamDone := make(chan struct{})

    // 7. 生产者 goroutine：从 adapter 流式读取 → 放入 outCh
    go func() {
        defer close(streamDone)

        chunkCh, streamErr := a.ProcessMessageStreamImpl(ctx, request, inputs)
        if streamErr != nil {
            outCh <- schema.NewAgentResponseChunk(request.RequestID, request.ChannelID,
                map[string]any{"event_type": "chat.error", "error": streamErr.Error()},
            )
            return
        }

        for chunk := range chunkCh {
            outCh <- chunk
        }
    }()

    // 8. 消费者 goroutine：从 outCh 读取 → 写入结果 channel
    resultCh := make(chan *schema.AgentResponseChunk, 64)
    go func() {
        defer close(resultCh)

        var finalAnswerContent string
        var finalAnswerChunks []string

        for {
            select {
            case chunk, ok := <-outCh:
                if !ok {
                    // 流结束
                    goto streamComplete
                }

                // 记录 assistant 历史
                if payload := chunk.Payload; payload != nil {
                    if eventType, _ := payload["event_type"].(string); eventType != "" {
                        if shouldRecordHistory(eventType) {
                            content := extractChunkContent(payload)
                            AppendHistoryRecord(
                                sessionID, request.RequestID, request.ChannelID,
                                "assistant", content, float64(time.Now().UnixMilli())/1000,
                                eventType, nil, nil, "",
                            )
                        }
                        // 收集 final_answer
                        if eventType == "chat.final" {
                            if c, ok := payload["content"].(string); ok {
                                finalAnswerContent = c
                            }
                        } else if eventType == "chat.delta" {
                            if c, ok := payload["content"].(string); ok {
                                finalAnswerChunks = append(finalAnswerChunks, c)
                            }
                        }
                    }
                }

                resultCh <- chunk

            case <-streamDone:
                // 等待 outCh 排空
                for len(outCh) > 0 {
                    chunk := <-outCh
                    resultCh <- chunk
                }
                goto streamComplete
            }
        }

    streamComplete:
        // ⤵️ 10.3.2: cloud memory after-chat hook（用 finalAnswerContent 或 join(finalAnswerChunks)）

        // 发送终止 chunk
        resultCh <- schema.NewTerminalChunk(request.RequestID, request.ChannelID)
    }()

    // 9. 提交流式任务到 session 队列
    // ⤵️ 10.3.2: Team 后续请求 / Auto-Harness resume 绕过 Session 队列（当前走正常队列）
    _ = jw.sessionManager.EnsureSessionProcessor(ctx, sessionID)

    return resultCh, nil
}
```

**shouldRecordHistory 判定**：`event_type` 以 `"chat."` 开头时记录。

### 4.5 ProcessInterrupt

对齐 Python `_process_interrupt(request)`（interface.py:621-679）。

```go
func (jw *JiuWenClaw) ProcessInterrupt(ctx context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
    // 1. 从 params 提取 intent（默认 "cancel"）
    intent := jw.extractIntent(request)  // pause / resume / cancel / supplement
    sessionID := GetSessionID(request.SessionIDStr())

    // ⤵️ 10.3.2: Team 模式分流（_processTeamInterrupt）

    // 2. 确保 adapter
    mode := jw.adapterModeForRequest(request)
    a, err := jw.ensureAdapter(mode)
    if err != nil {
        return nil, err
    }

    // 3. pause / resume：仅调 adapter.ProcessInterrupt，不取消 session task
    if intent == "pause" || intent == "resume" {
        return a.ProcessInterrupt(ctx, request)
    }

    // 4. supplement：先 adapter.ProcessInterrupt，再 cancel_session_task
    if intent == "supplement" {
        resp, err := a.ProcessInterrupt(ctx, request)
        _ = jw.sessionManager.CancelSessionTask(ctx, sessionID, "interrupt(supplement)", nil)
        return resp, err
    }

    // 5. cancel（默认）：先 adapter.ProcessInterrupt（session 还在 active），
    //    再 cancel_team_work，再 cancel_session_task。
    //    顺序不能反！
    resp, err := a.ProcessInterrupt(ctx, request)
    // ⤵️ 10.3.2: cancelTeamWorkForSession(sessionID, channelID)
    waitTimeout := 5 * time.Second
    _ = jw.sessionManager.CancelSessionTask(ctx, sessionID, "interrupt(cancel)", &waitTimeout)
    return resp, err
}
```

### 4.6 CancelInflightWork

对齐 Python `cancel_inflight_work()`（interface.py:1226-1238）。

```go
func (jw *JiuWenClaw) CancelInflightWork() error {
    // 1. 取消所有 session 的非流式任务
    _ = jw.sessionManager.CancelAllSessionTasks(context.Background(), "[gateway disconnect]")

    // 2. 调用 adapter 的 AbortOnGatewayDisconnect（若存在）
    jw.adapterMu.Lock()
    a := jw.adapter
    jw.adapterMu.Unlock()

    if a == nil {
        return nil
    }

    // ⤵️ 10.3.2: adapter.AbortOnGatewayDisconnect()（DeepAdapter 尚未实现此方法）

    return nil
}
```

### 4.7 ReloadAgentConfig

对齐 Python `reload_agent_config()`（interface.py:331-342）。

```go
func (jw *JiuWenClaw) ReloadAgentConfig(configBase map[string]any, envOverrides map[string]any) error {
    jw.adapterMu.Lock()
    a := jw.adapter
    jw.adapterMu.Unlock()

    if a == nil {
        return nil
    }

    // ⤵️ 10.3.2: adapter.TryStopDreaming()

    ctx := context.Background()
    if err := a.ReloadAgentConfig(ctx, configBase, envOverrides); err != nil {
        return err
    }

    // ⤵️ 10.3.2: adapter.TryStartDreaming()

    return nil
}
```

### 4.8 Cleanup

对齐 Python `cleanup()`（interface.py:1240-1249）。

```go
func (jw *JiuWenClaw) Cleanup() error {
    jw.adapterMu.Lock()
    a := jw.adapter
    jw.adapter = nil
    jw.adapterMu.Unlock()

    if a != nil {
        _ = a.Cleanup()
    }
    return nil
}
```

### 4.9 辅助方法（stub / 简单实现）

```go
// SwitchMode 切换运行模式。
// ⤵️ 10.3.2: 完整实现需要 DeepAdapter 支撑（session 持久化 + switch_mode + load_state）
func (jw *JiuWenClaw) SwitchMode(sessionID, mode string) error {
    return nil
}

// GetContextUsage 获取上下文使用量。
// ⤵️ 10.3.2: 需要从 adapter 获取 DeepAgent 实例后调用 GetContextUsage
func (jw *JiuWenClaw) GetContextUsage(sessionID string) (map[string]any, error) {
    return map[string]any{"usage": 0, "limit": 0}, nil
}

// CompressContext 压缩上下文。
// ⤵️ 10.3.2: 需要调用 DeepAgent 的压缩逻辑
func (jw *JiuWenClaw) CompressContext(sessionID string) (map[string]any, error) {
    return map[string]any{"ok": true, "compressed": false}, nil
}

// GenerateRecap 生成会话回顾。
// ⤵️ 10.3.2: 需要调用 DeepAgent 的回顾逻辑
func (jw *JiuWenClaw) GenerateRecap(sessionID string) (map[string]any, error) {
    return map[string]any{"recap": ""}, nil
}

// GetInstance 获取底层 Agent 实例。
// ⤵️ 10.3.2: 需要返回 adapter 内部的 DeepAgent 实例
func (jw *JiuWenClaw) GetInstance() any {
    return nil
}
```

### 4.10 内部辅助函数

```go
// adapterModeForRequest 从请求参数中提取 adapter mode。
func (jw *JiuWenClaw) adapterModeForRequest(request *schema.AgentRequest) string

// extractQuery 从请求参数中提取 query 字段。
func (jw *JiuWenClaw) extractQuery(request *schema.AgentRequest) string

// extractResponseContent 从响应中提取 content。
func (jw *JiuWenClaw) extractResponseContent(resp *schema.AgentResponse) string

// extractIntent 从请求参数中提取 intent（默认 "cancel"）。
func (jw *JiuWenClaw) extractIntent(request *schema.AgentRequest) string

// extractChunkContent 从 chunk payload 中提取 content。
func extractChunkContent(payload map[string]any) string

// shouldRecordHistory 判断 event_type 是否需要记录到 history。
func shouldRecordHistory(eventType string) bool
```

---

## 5. 层级 2-4 stub 标注

### 5.1 层级 2：Skills / SkillDev / Plugins 分流

| 函数 | stub 行为 | 回填标记 | 对齐 Python |
|------|----------|---------|------------|
| `handleSkillsRequest(request)` | 返回 nil（不拦截，继续正常流程） | `⤵️ 10.3.2` | `_handle_skills_request()` |
| `handleSkillDevRequest(request)` | 返回 nil | `⤵️ 10.3.2` | `_handle_skilldev_request()` |
| `handlePluginsRequest(request)` | 返回 nil | `⤵️ 10.3.2` | `_handle_plugins_request()` |
| `handleSkillDevStreamRequest(request)` | 返回 nil | `⤵️ 10.3.2` | SkillDev 流式分支 |

**回填前提**：需要实现 `internal/swarm/server/runtime/skill/skill_manager.go`（server 层 SkillManager）。

### 5.2 层级 3：DeepAdapter 回填

| 方法 | 当前状态 | 回填标记 | 对齐 Python |
|------|---------|---------|------------|
| `DeepAdapter.CreateInstance` | 步骤 15-25 待回填 | `⤵️ 10.3.7-11` | `create_instance()` 完整 25 步 |
| `DeepAdapter.ProcessMessageImpl` | Runner 调用链待回填 | `⤵️ 10.3.7-11` | `process_message_impl()` |
| `DeepAdapter.ProcessMessageStreamImpl` | Runner 流式调用链待回填 | `⤵️ 10.3.7-11` | `process_message_stream_impl()` |

**回填前提**：需要 Runner 流式调用链完整、Rails 构建完成。

### 5.3 层级 4：Rails 具体实现

| Rail | 当前状态 | 回填标记 | 对齐 Python |
|------|---------|---------|------------|
| `StreamEventRail` | `interface{}` | `⤵️ 10.6.3-10` | `JiuClawStreamEventRail` |
| `SysOperationRail` | `interface{}` | `⤵️ 10.6.3-10` | `SysOperationRail` |
| `SkillUseRail` | BaseRail 占位 | `⤵️ 9.8-9.24` | `SkillUseRail` |
| `PermissionInterruptRail` | `interface{}` | `⤵️ 10.6.3-10` | `PermissionInterruptRail` |
| `RuntimePromptRail` | `interface{}` | `⤵️ 10.6.3-10` | `RuntimePromptRail` |
| `ResponsePromptRail` | `interface{}` | `⤵️ 10.6.3-10` | `ResponsePromptRail` |
| `SecurityRail` | `interface{}` | `⤵️ 10.6.3-10` | `SecurityRail` |
| `MemoryRail` | `interface{}` | `⤵️ 10.6.3-10` | `MemoryRail` |
| `SkillEvolutionRail` | `interface{}` | `⤵️ 10.6.3-10` | `SkillEvolutionRail` |
| `SkillCreateRail` | `interface{}` | `⤵️ 10.6.3-10` | `SkillCreateRail` |
| `SubagentRail` | `interface{}` | `⤵️ 10.6.3-10` | `SubagentRail` |
| `AvatarRail` | `interface{}` | `⤵️ 10.6.3-10` | `AvatarRail` |
| `ContextAssembleRail` | `interface{}` | `⤵️ 10.6.3-10` | `ContextAssembleRail` |
| `ContextProcessorRail` | `interface{}` | `⤵️ 10.6.3-10` | `ContextProcessorRail` |
| `ExternalMemoryRail` | `interface{}` | `⤵️ 10.6.3-10` | `ExternalMemoryRail` |

**Code 模式专有 Rail**：

| Rail | 当前状态 | 回填标记 |
|------|---------|---------|
| `LspRail` | `interface{}` | `⤵️ 10.6.3-10` |
| `ProjectMemoryRail` | `interface{}` | `⤵️ 10.6.3-10` |
| `CodingMemoryRail` | `interface{}` | `⤵️ 10.6.3-10` |
| `WorktreeRail` | `interface{}` | `⤵️ 10.6.3-10` |
| `CodeAgentRail` | `interface{}` | `⤵️ 10.3.7-11` |

### 5.4 其他 stub 标注

| 功能 | stub 行为 | 回填标记 | 对齐 Python |
|------|----------|---------|------------|
| Team 模式分流 | 跳过 | `⤵️ 10.3.2` | `is_team_params` / `_process_team_interrupt` |
| Auto-Harness 模式 | 跳过 | `⤵️ 10.3.2` | AutoHarnessService |
| cloud memory hook | 跳过 | `⤵️ 10.3.2` | ExtensionRegistry.MEMORY_BEFORE/AFTER_CHAT |
| dreaming 后台任务 | 跳过 | `⤵️ 10.3.2` | adapter.TryStartDreaming / TryStopDreaming |
| SkillNet install complete hook | 跳过 | `⤵️ 10.3.2` | skillManager.set_skillnet_install_complete_hook |
| InteractiveInput 构建 | fallback 到 BuildUserPrompt | `⤵️ 10.3.2` | `_build_interactive_input_from_answers` |
| adapter.AbortOnGatewayDisconnect | 跳过 | `⤵️ 10.3.2` | DeepAdapter.abort_on_gateway_disconnect |

---

## 6. 文件清单

### 6.1 新建文件

| 文件 | 职责 |
|------|------|
| `internal/swarm/server/runtime/build_user_prompt.go` | BuildUserPrompt 函数 |
| `internal/swarm/server/runtime/build_user_prompt_test.go` | 测试 |
| `internal/swarm/server/runtime/build_inputs.go` | BuildInputs 方法 |
| `internal/swarm/server/runtime/build_inputs_test.go` | 测试 |
| `internal/swarm/server/runtime/session_history.go` | AppendHistoryRecord / ReadHistoryRecords / TruncateHistoryRecords + 写入队列 + worker |
| `internal/swarm/server/runtime/session_history_test.go` | 测试 |

### 6.2 修改文件

| 文件 | 修改内容 |
|------|---------|
| `internal/swarm/server/runtime/jiowenclaw.go` | 结构体改造 + 全部方法实现 |
| `internal/swarm/server/runtime/jiowenclaw_test.go` | 更新测试 |
| `internal/swarm/server/runtime/doc.go` | 更新文件目录 |

---

## 7. History 与 Checkpoint 的关系

### 7.1 两者互补，非冗余

| 维度 | History (history.json) | Checkpoint (checkpoint.db) |
|------|----------------------|---------------------------|
| **本质** | 对话记录（"说了什么"） | 运行时状态（"做到哪了"） |
| **格式** | 人类可读 JSON 文件 | SQLite 二进制 KV 存储 |
| **写入时机** | 每条消息产生时 | 生命周期节点（pre/post agent/workflow） |
| **读取场景** | 前端历史展示、rewind 截断 | 会话恢复、中断续跑 |
| **正常完成后** | 永久保留 | 清除 |
| **Rewind 时** | 截断尾部记录 | 清除 + 从 history 重建 context_engine |
| **Go 状态** | 本设计实现写入 | ✅ 已实现 |

### 7.2 为什么需要两套

1. **History 不可替代 Checkpoint**：History 只有对话文本，没有 Agent 内部状态（ReAct 循环位置、工具调用栈等），无法中断续跑
2. **Checkpoint 不可替代 History**：Checkpoint 是不透明二进制状态，不能直接展示给用户；正常完成后 checkpoint 清除，但 history 永久保留
3. **Rewind 依赖两者协作**：先截断 history.json，再从截断后的 history 重建 context_engine，最后 persist 到 checkpoint

---

## 8. IMPLEMENTATION_PLAN.md 章节对应

| 本次实现 | 章节状态变更 |
|---------|-------------|
| JiuWenClaw 结构体改造 + 方法实现 | 10.3.2 ☐→🔄（层级 0+1 完成，层级 2-4 仍 ⤵️） |
| BuildUserPrompt | 10.3.2 子项 |
| BuildInputs | 10.3.2 子项 |
| AppendHistoryRecord | 10.3.2 子项 |

---

## 9. 测试策略

### 9.1 单元测试

| 包 | 测试重点 |
|---|---------|
| `runtime` (build_user_prompt) | 中文/英文 prompt 格式、cron 模式、files 序列化、trusted_dirs、interaction_context 前缀、/skills use 解析 |
| `runtime` (build_inputs) | 基础字段提取、project_dir/cwd 优先级、cron 转换、InteractiveInput 分支（stub） |
| `runtime` (session_history) | 记录格式、role 归一化、event_type 条件、extra 展开、文件持久化读写、truncate、并发写入安全 |
| `runtime` (jiowenclaw) | ensureAdapter 幂等、ProcessMessage 分流（cancel/answer/heartbeat/normal）、ProcessMessageStream chunk 传递、ProcessInterrupt intent 分支、CancelInflightWork、Cleanup |

### 9.2 Mock 策略

- `AgentAdapter` 接口 mock：实现 `fakeAdapter` 返回固定响应
- 文件系统：使用 `t.TempDir()` 作为 workspace
- `SessionManager`：使用真实实例（已完整实现）
