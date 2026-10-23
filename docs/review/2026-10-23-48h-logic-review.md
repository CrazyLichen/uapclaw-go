# 2026-10-23 48h 逻辑审查

- **审查窗口**：2026-10-22 04:01 ~ 2026-10-23 02:30（31 个提交，排除 style/gofmt/docs 后功能性提交约 22 个）
- **审查范围**：48h 内完成的功能章节——11.7 SessionMap、11.13 Gateway Hook（Handler + message_handler 集成点①~④）、13.16 JSON/TxtMd Parser、6.8a ModelBackupRail、real_llm 测试 C 类补齐（cf258b2a）、agent_teams 批次 1+2 修复（26125540，~4285 行）、CI race 修复 3 连（lite watcher / SkillManager 锁 / gateway 测试排空）
- **审查方法**：逐方法签名与步骤对照 Python 参考项目（agent-core/openjiuwen + jiuwenswarm-develop），重点核对方法签名一致性、步骤缺失、占位真实性

## 总体结论

本次 48h 移植质量整体较高：11.7 SessionMap 与 11.13 GatewayHookHandler 与 Python 逐行对齐（含防御性细节如同毫秒幂等分支、activeSessions 去重）；S-24/S-26/S-27 spawn 双参数契约修复正确落地；shutdown cleanup 538 行切片移植完整。发现 **2 项严重、5 项一般、6 项提示**。

**严重**问题集中在团队流消费链路（ControllerOutputPayload 类型域丢失导致事件丢弃）与记忆注入的头部文案复制粘贴错误；**一般**问题集中在 JSON 大整数精度、CJK 字节截断与并发残留访问点。

---

## 严重问题（S）

### S-01 chunkToMap 丢弃 ControllerOutputPayload 类型 chunk——task_failed/task_completion 事件在前端静默丢失

**Python 样例**（`jiuwenswarm/server/utils/stream_utils.py L107-170`）：

```python
def _parse_typed_chunk(chunk, _has_streamed_content):
    chunk_type = getattr(chunk, "type", "")
    payload = getattr(chunk, "payload", {})

    if chunk_type == "controller_output" and payload is not None:
        interactions = _find_interaction_payloads(payload)
        ...
        inner_t = getattr(payload, "type", None)   # ControllerOutputPayload.type
        if inner_val == "task_failed":
            data = getattr(payload, "data", None)
            error = next((item.text for item in data if hasattr(item, "text")), "任务执行失败")
            return {"event_type": "chat.error", "error": error}   # ← 前端收到错误事件
```

Python 端 task_loop 事件执行器（`openjiuwen/harness/task_loop/task_loop_event_executor.py L234-264`）产出：

```python
payload = ControllerOutputPayload(
    type=EventType.TASK_FAILED,
    data=[TextDataFrame(text=str(exc))],
    metadata={"task_id": task_id},
)
yield ControllerOutputChunk(index=0, payload=payload, last_chunk=True)
# ControllerOutputChunk.type 固定为 "controller_output"（controller_output.py L55）
```

`parse_stream_chunk` typed 分支经 `hasattr(payload, "model_dump")` → `payload.model_dump(mode="json")` 将 pydantic 对象转成 dict 后正常进入 controller_output 处理（task_failed → chat.error、task_completion → None、其余 → 内部交互判定）。

**Go 问题**（`internal/swarm/server/adapter/deep_adapter_team.go:1341-1430`）：

```go
func chunkToMap(chunk any) map[string]any {
    ...
    case *stream.OutputSchema:
        if os == nil || os.Payload == nil {
            return nil
        }
        if m, ok := os.Payload.(map[string]any); ok {   // ← 只接受 map[string]any
            ...
        }
        return nil     // ← *cschema.ControllerOutputPayload 断言失败 → 整 chunk 丢弃
    ...
    if tos, ok := chunk.(*atschema.TeamOutputSchema); ok {
        if m, ok := tos.Payload.(map[string]any); ok {  // ← 同样只接受 map
            ...
        }
        return nil     // ← TeamOutputSchema + ControllerOutputPayload 同样丢弃
    }
}
```

Go 端 task_loop 产出同型 chunk（`harness/task_loop/executor.go L274-284, L345-355`）：

```go
ch <- &stream.OutputSchema{
    Type:  string(cschema.EventTaskFailed),          // "task_failed"
    Payload: &cschema.ControllerOutputPayload{        // ← 结构体指针，非 map
        Type: string(cschema.EventTaskFailed),
        Data: []cschema.DataFrame{&cschema.TextDataFrame{Text: invokeErr.Error()}},
    },
    IsLastSchema: true,
}
```

团队 harness `EnableTaskLoop: true`（agent_teams/harness.go L448 对齐 Python 硬编码），成员 DeepAgent 的外层任务循环会真实产出 task_completion / task_failed chunk，经 `StreamController.tagChunk` 升级为 `*TeamOutputSchema`（Payload 保持 `*ControllerOutputPayload` 不变）→ 消费循环 `chunkToMap` 断言 `map[string]any` 失败 → **返回 nil → continue**。

**影响链路（流程示例）**：

```
成员 DeepAgent 外层任务循环执行失败
  → executor.go 发送 OutputSchema{Type:"task_failed", Payload:*ControllerOutputPayload}
  → StreamController.streamOneRound: detectTaskFailed 检测后 continue（不转发）
    （detectTaskFailed L813 同样断言 map[string]any 失败 → 检测不到 task_failed → 不触发重试）
  → tagChunk 升级为 *TeamOutputSchema（Payload 不变）
  → streamQueue → TeamAgent.Stream → mergedCh → 消费循环 chunkToMap
  → tos.Payload.(map[string]any) 断言失败 → return nil
  → 消费循环 L814-816: chunkMap == nil → continue
  → 前端收不到 chat.error，也收不到任何该轮输出
  → 若整轮流仅此一个 chunk：receivedChunks==0 → 兜底 team.error（错误信息是笼统的
    "possible pool/DB inconsistency or internal error" 而非真实失败原因）
```

附带影响：`detectTaskFailed`（stream_controller.go L804-843）对 `*ControllerOutputPayload` 同样断言 `map[string]any` 失败返回 nil——Python `_detect_task_failed` 读 `chunk.payload` 的 `type` 键（pydantic 属性），Go 端 task_failed 重试机制在结构体 payload 场景下同样失效。

**修复方案**：chunkToMap 三个分支（`*OutputSchema` / `*TeamOutputSchema` / outputSchemaLike）在 map 断言失败后增加 `*cschema.ControllerOutputPayload` 桥接分支：

```go
case *stream.OutputSchema:
    if os == nil || os.Payload == nil {
        return nil
    }
    if m, ok := os.Payload.(map[string]any); ok {
        // 现有 map 路径
    } else if cop, ok := os.Payload.(*cschema.ControllerOutputPayload); ok {
        // Python model_dump(mode="json") 等价桥接：JSON 往返转 map
        m = controllerOutputPayloadToMap(cop)   // {type, data:[{text|data|...}], metadata}
        if m != nil { /* 同 map 路径产出 result */ }
    }
    return nil
```

`controllerOutputPayloadToMap` 用 `json.Marshal(cop)` → `json.Unmarshal(&map)`（Data 经 dataFrameSlice 的多态 MarshalJSON 已实现），与 Python `model_dump(mode="json")` 语义一致。`detectTaskFailed` 同样需要加结构体分支读 `cop.Type` 与 `cop.Data[0].Text`。

**测试建议**：现有 `deep_adapter_team_chunktomap_test.go` 9 用例全部用 map payload，需补 `*ControllerOutputPayload` payload 用例（task_failed → chat.error 事件断言）。

---

### S-02 TeamMemoryManager.LoadAndInject 个人记忆段英文 header 复制粘贴错误——用户看到重复的 "Team shared memory" 标题

**Python 样例**（`openjiuwen/agent_teams/memory/manager.py L292-296`）：

```python
personal_content = await self._fetch_personal_memory_for_prompt(query)
if personal_content:
    header = "\n\n## 你的相关记忆\n\n" if cn else "\n\n## Your relevant memories\n\n"
    for key in section.content:
        section.content[key] += header + personal_content
```

**Go 问题**（`internal/agent_teams/memory/manager.go:307-315`）：

```go
if personalContent := m.fetchPersonalMemoryForPrompt(ctx, query); personalContent != "" {
    header := "\n\n## Team shared memory\n\n"          // ← 错误！应为 "## Your relevant memories"
    if cn {
        header = "\n\n## 你的相关记忆\n\n"              // 中文分支正确
    }
    for key := range section.Content {
        section.Content[key] += header + personalContent
    }
}
```

L328 的团队共享记忆段 header（"## Team shared memory" / "## 团队共享记忆"）是正确的；L308 是从 L328 复制后忘了改英文分支。**中文环境无影响，英文环境下最终提示词呈现**：

```
## Your relevant memories（基础段内嵌标题，build_memory_section 产出）
... 基础内容 ...
## Team shared memory          ← 个人记忆段（应为 ## Your relevant memories）
... 个人记忆内容 ...
## Team shared memory          ← 团队共享记忆段（正确）
... 团队记忆内容 ...
```

两个 "Team shared memory" 段落连排，LLM 对记忆来源的区分被破坏（个人记忆被误标为团队共享记忆，可能引发跨成员记忆归属混淆）。

**修复方案**：一行修复：

```go
header := "\n\n## Your relevant memories\n\n"
if cn {
    header = "\n\n## 你的相关记忆\n\n"
}
```

**测试建议**：现有 manager_test.go 行为矩阵补英文语言断言（`language=en` 时 LoadAndInject 后 section 内容包含 "Your relevant memories" 且 "Team shared memory" 仅出现一次）。

---

## 一般问题（M）

### M-01 JSONParser 大整数经 float64 往返精度损坏

**Python 样例**（`openjiuwen/core/retrieval/indexing/processor/parser/json_parser.py`）：

```python
json_data = json.loads(raw_content)      # 任意精度 int 保持原样
return json.dumps(json_data, indent=2, ensure_ascii=False)
```

Python `json.loads` 把整数解析为任意精度 `int`，`json.dumps` 原样输出——雪花 ID、纳秒时间戳等大整数无损往返。

**Go 问题**（`internal/agentcore/retrieval/indexing/processor/parser/json_parser.go L76-88`）：

```go
var data any
if err := json.Unmarshal(raw, &data); err != nil { ... }  // 数字 → float64
...
enc.SetEscapeHTML(false)   // HTML 转义已正确对齐（排除项）
enc.SetIndent("", "  ")
if err := enc.Encode(data); err != nil { ... }
```

`json.Unmarshal` 到 `any` 将所有数字统一为 `float64`：超过 2^53 的整数（雪花 ID 4704154934906060801、业务大 ID 等）静默损坏（`...6060801` → `...6060802`），浮点也会经最短表示法重排（`1.10` → `1.1`，`3.0` → `3`——后者 Python 同样会输出 `3.0` 保持 float 类型）。

**影响**：RAG 索引链路（Parse → chunker → embedder）摄入含大整数 ID 的 JSON 文档（trace 数据、事件日志、监控导出）时 ID 被改写，检索命中后回查数据库失败。

**修复方案**：用 `json.Decoder` + `UseNumber()` 保持 `json.Number` 原样：

```go
dec := json.NewDecoder(bytes.NewReader(raw))
dec.UseNumber()
var data any
if err := dec.Decode(&data); err != nil { ... }
// Encoder 对 json.Number 原样输出（json.Number 是 string 底层，Encode 直接写出）
```

**测试建议**：json_parser_test.go 补大整数用例（`{"id": 4704154934906060801}` 解析后原样回写）。

### M-02 fetchPersonalMemoryForPrompt 字节截断可产生非法 UTF-8

**Python 样例**（`manager.py L217-222`）：

```python
content_bytes = len(content.encode("utf-8"))
if total_bytes + content_bytes > _MAX_PERSONAL_MEMORY_BYTES:
    remaining = _MAX_PERSONAL_MEMORY_BYTES - total_bytes
    if remaining > 200:
        parts.append(f"### {r_path}\n\n{content[:remaining]}\n... (truncated)")
```

Python `content[:remaining]` 按字符切片，且注意：Python 字节数预算配字符切片（中文场景下 `remaining` 字节预算切出 `remaining` 个字符，实际截断比预算宽松，但不产生非法字符串）。

**Go 问题**（`internal/agent_teams/memory/manager.go:529-537`）：

```go
contentBytes := len([]byte(content))
if totalBytes+contentBytes > maxPersonalMemoryBytes {
    remaining := maxPersonalMemoryBytes - totalBytes
    if remaining > 200 {
        truncated := content
        if len(truncated) > remaining {
            truncated = truncated[:remaining]   // ← 字节切片
        }
        parts = append(parts, "### "+rPath+"\n\n"+truncated+"\n... (truncated)")
    }
    break
}
```

字节切片可能把多字节 UTF-8 序列（中文 3 字节、emoji 4 字节）从中间切开：`"用中文"` 截到 4 字节 → `\xe7\x94\xa8\xe4\xb8`——产生非法 UTF-8 字符串。注入系统提示词后，LLM 端（tokenizer）可能乱码或抛 tokenization 错误；本项目 CJK 截断是历史审查高频问题（2026-09-28/09-30/10-01/10-13 四轮均有 CJK 项）。

**修复方案**：rune 安全截断（对齐 Python 字符语义；更严格的是按字节预算+rune 边界回退，但 Python 语义就是字符切片）：

```go
if runes := []rune(truncated); len(runes) > remaining {
    truncated = string(runes[:remaining])
}
```

或按"字节预算、rune 边界"双向对齐：`truncated = truncateBytesRuneSafe(truncated, remaining)`（先按字节预算回退到最后一个完整 rune 边界）。后者更贴近 Python 的字节预算意图，推荐后者。

**测试建议**：纯中文超限内容用例断言 `utf8.ValidString(result)`。

### M-03 lite memoryIndexManager.GetMemoryIndexManager 缓存命中路径 closed 裸读残留

**Python 样例**（asyncio 单线程，无此问题；Go 侧本次修复引入的同步约定应全覆盖）：

**Go 问题**（`internal/agentcore/memory/lite/manager_impl.go:549-555`）：

```go
if cached, ok := indexCache.Load(cacheKey); ok {
    mgr := cached.(*memoryIndexManager)
    if !mgr.closed {          // ← 锁外裸读（Close 在 m.mu 下写 m.closed）
        return mgr, nil
    }
    _ = mgr.Close()
}
```

本次 48h 内 commit 450d0f98 修复了 watchTimer/intervalSync 两处 `m.closed` 裸读（改走 `IsClosed()` 锁读），但同文件 L551 的缓存命中路径裸读未一并修复——与已修复两处同型（Close 持锁写 vs 此处无锁读）。触发窗口：一个 goroutine 正在 GetMemoryIndexManager 判缓存存活的同时另一 goroutine Close 同一 manager（如 suite TearDown 与下一测试 InitToolkit 并发）。`-race` 下可检出。

**修复方案**：`if !mgr.IsClosed() { return mgr, nil }`。

### M-04 extractor list_memory_files 绕过 sysOp.Fs() 直读本地目录

**Python 样例**（`openjiuwen/agent_teams/memory/extractor.py L142-150`）：

```python
async def _list_files() -> dict:
    try:
        result = await sys_operation.fs().list_files(team_memory_dir, recursive=False)
        if result and hasattr(result, "data") and result.data:
            files = [f.name for f in result.data.list_items if not f.is_directory]
            return {"files": files}
        return {"files": []}
    except Exception:
        return {"files": []}
```

三个工具（read/write/list）全部走 `sys_operation.fs()` 抽象。

**Go 问题**（`internal/agent_teams/memory/extractor.go:214-227`）：

```go
listTool, listErr := tool.NewTool(func(_ context.Context, _ struct{}, opts ...tool.ToolOption) (map[string]any, error) {
    entries, err := os.ReadDir(teamMemoryDir)      // ← 绕过 sysOp.Fs().ListFiles
    ...
}, ...)
```

read/write 都走 `sysOp.Fs()`，唯独 list 直读本地文件系统。sys_operation 的 Fs 实现可挂载非本地后端（远程/沙箱 Fs）——该场景下 read/write 走后端而 list 读本地目录，提取 agent 看到的文件列表与实际可读写的文件不一致（列出本地遗留文件但 read 返回 "file not found"，或后端有文件但 list 为空导致提取 agent 误判"无已有记忆"重复提取）。Go 的 Fs 接口已有 `ListFiles`（fs.go L32），本地实现完备（fs_operation.go L637）。

**修复方案**：

```go
if sysOp != nil && sysOp.Fs() != nil {
    if res, err := sysOp.Fs().ListFiles(ctx, teamMemoryDir); err == nil &&
        res != nil && res.Data != nil {
        files := ... // 从 res.Data.ListItems 过滤 !IsDirectory
        return map[string]any{"files": files}, nil
    }
    return map[string]any{"files": []any{}}, nil
}
return map[string]any{"files": []any{}}, nil   // sysOp 缺失兜底（Python None 检查等价）
```

需确认 ListFilesResult.Data 的具体字段名（result/fs_operation_result.go L177+）。

### M-05 IMPLEMENTATION_PLAN 9.57 状态与 TeamHarness 占位实况矛盾

**Python 样例**（`openjiuwen/agent_teams/harness.py L269-301`）：

```python
async def run_streaming(self, inputs, *, session_id, team_session=None):
    if team_session is None and not self._initial_plan_mode:
        async for chunk in Runner.run_agent_streaming(self._deep_agent, inputs, session=session_id):
            yield chunk
        return
    agent_session = await self._prepare_agent_session(...)
    async for chunk in Runner.run_agent_streaming(self._deep_agent, inputs, session=agent_session):
        yield chunk
```

**Go 问题**（`internal/agent_teams/harness.go:352-359`）：

```go
func (h *TeamHarness) RunStreaming(ctx context.Context, inputs map[string]any, sessionID string, teamSession any) (<-chan stream.Schema, error) {
    // ⤵️ 待 9.57 deepAgent 类型升级为 BaseAgent 后实现分支 1
    // 分支 2: ⤵️ 待 9.57+ session 层回填
    ch := make(chan stream.Schema)
    close(ch)            // ← 空壳：立即关闭的 channel
    return ch, nil
}
```

占位本身已用 ⤵️ 标记登记（符合"占位必须登记"的项目规范），**但** IMPLEMENTATION_PLAN.md L593 将 9.57 标记为 ✅ 完成（"骨架+TODO占位"）。后果：`StreamController.streamOneRound`（stream_controller.go L681）调用此空壳——**成员流式输出链路（成员 DeepAgent → streamQueue → TeamAgent.Stream → 前端）当前整体为空流**。S-20 修复的团队流解析器在成员内容 chunk 上无可处理数据（仅 runtime_ready/team.completed 控制帧经 buildTeamRuntimeReadyChunk/buildTeamControlChunk 直接产出可达）。即：S-24 修复的 inputs 透传、S-20 的解析器实装，其大部分价值要等 9.57 回填后才能兑现，而计划勾选状态掩盖了这一断链。

**修复方案**：二选一——
1. 将 IMPLEMENTATION_PLAN 9.57 行状态改为 ⤵️/🔄 并注明"RunStreaming/Steer/FollowUp/Abort 等 6 方法待 deepAgent 类型升级后回填"；
2. 直接实现分支 1（`teamSession == nil && !initialPlanMode` → `runner.RunAgentStreaming(h.deepAgent, ...)`），分支 2 继续登记。分支 1 实现成本低（runner 已有 RunAgentStreaming），可立即打通成员内容流。

**关联说明**：本项不改变"S-20/S-24 修复正确"的结论——那两处修复在 9.57 回填后立即生效；但审查视角必须指出当前团队流仅有控制帧、无内容帧的现状，避免"解析器已实装=团队流已通"的误读。

---

## 提示问题（T）

### T-01 BuildSpawnPayload leader_member_name 缺省语义偏差（"" vs null）

**Python 样例**（`openjiuwen/agent_teams/agent/payload.py L62-63`）：

```python
"leader_member_name": team_spec.leader_member_name if team_spec else None,
```

**Go 问题**（`internal/agent_teams/agent/payload.go:58-63`）：

```go
leaderMemberName := ""
if teamSpec != nil {
    ...
    leaderMemberName = teamSpec.LeaderMemberName
}
...
"leader_member_name": leaderMemberName,   // teamSpec 为 nil 时传 "" 而非 nil
```

JSON 序列化后 `"leader_member_name": ""` vs `"leader_member_name": null`。消费端（子进程 FromSpawnPayload 侧）`string` 字段读空串与 Python `None` 的 `or` 语义在多数路径等价（Python `None or ""` → `""`），无功能影响；仅在严格区分"无团队"与"空名"的判定（如 `if leader_member_name is None`）下有语义差。**提示级**：如需严格对齐，`leaderMemberName` 用 `*string` 并在 nil 时省略赋值（map 中不写该键，反序列化后零值等价 null）。同类问题：`transport` 键为 TODO(#9.65) 登记占位（已确认非静默缺失）。

### T-02 TxtMdParser UTF-8 BOM 剥离是 Go 侧增强

**Python 样例**：`_parse` 用 `open(encoding=detected, errors="ignore")` 读文件，UTF-8 BOM（\ufeff）在 Python 端保留在 content 中（`strip()` 不剥 BOM，BOM 是 `\ufeff` 不算空白——实际上 Python `str.strip()` 默认空白集不含 \ufeff）。

**Go 实现**（txt_md_parser.go L104-113）：`strings.TrimPrefix(decoded, "\uFEFF")` 主动剥离 BOM。

这是改良性偏差：BOM 残留会污染 chunker 的首 token、embedding 首向量。设计文档已声明该决策（"BOM 剥离对齐 strip 对 \ufeff 兜底处理的最终输出"）。**提示级**：无需修改，记录在案。

### T-03 extractor read 工具"空数据"与"异常"分支合并

**Python 样例**（extractor.py L118-128）：

```python
try:
    result = await sys_operation.fs().read_file(full_path)
    if result and hasattr(result, "data") and result.data:
        return {"content": ..., "path": ...}
    return {"content": "", "path": full_path}              # ← 空数据：无 note 键
except Exception:
    return {"content": "", "path": full_path, "note": "file not found"}   # ← 异常：带 note
```

**Go 实现**（extractor.go L172-178）：两条路径统一返回带 `note: "file not found"`。LLM 消费方几乎不区分（note 只是提示字段），且 Go 的 `err == nil && result.Data != nil` 组合下"成功但空"分支实际罕见。**提示级**：如需严格对齐，把 note 移到 err != nil 分支。

### T-04 shutdown cleanup fallbackSessionID 语义与 Python 略有偏差

**Python 样例**（remote_member_bootstrap.py L1448-1450）：

```python
active_session_id = str(
    getattr(self, _SHUTDOWN_CLEANUP_SESSION_ID_ATTR, session_id) or session_id
).strip() or session_id
```

attr 缺失/为空时回退**闭包捕获的构造时 session_id**。

**Go 实现**（remote_member_shutdown_cleanup.go L305-308）：

```go
activeSessionID = strings.TrimSpace(activeSessionID)
if activeSessionID == "" {
    activeSessionID = w.fallbackSessionID()   // ← 读 rebind 后的当前值，非构造时快照
}
```

Go 的 rebind 会更新 `w.sessionID`，fallback 读到的是 rebind 后值；Python 闭包捕获的是 attach 时的原始值。实际影响极小：rebind 后 activeSessionID 通常就是 rebind 传入的新值（非空），fallback 只在"rebind 传了空串"的异常场景触发。**提示级**：如需严格对齐，构造时快照 `constructSessionID` 字段。

### T-05 GatewayHookHandler OnSessionStart 检查-执行顺序保持 Python 缺陷

**Python 样例**（handler.py L33-35）：

```python
if session_id in self._active_sessions:
    return
self._active_sessions.add(session_id)     # ← 先标记
hook_configs = self._config.match(...)     # ← 后查配置；无配置时 session 已标记，
if not hook_configs: return                #    该 session 永不再触发 SessionStart
```

**Go 实现**（handler.go L61-72）：同样的先标记后 match 顺序（对齐复刻）。语义后果：若该 session 的 SessionStart hooks 后来才注册（配置热加载），已见过的 session 不会补触发。Go 复刻了 Python 的这一行为（含 mutex 保护下的原子检查-标记，比 Python 还更正确）。**提示级**：忠实复刻，无需修改；记录以防后续"修复"时误认为 bug。

### T-06 SerializeValue date 类型未覆盖

**Python 样例**（stream_utils.py L477-482）：

```python
def _serialize_value(value):
    if isinstance(value, datetime):
        return value.isoformat()
    if isinstance(value, date):        # ← date（非 datetime）单独分支
        return value.isoformat()
    if isinstance(value, Enum):
        return value.value
    return value
```

**Go 实现**（deep_adapter_team.go L1874-1880 serializeValueAny / stream_utils.go L574-581 SerializeValue）：

```go
func serializeValueAny(v any) any {
    if t, ok := v.(time.Time); ok {
        return t.UTC().Format(time.RFC3339Nano)
    }
    return v
}
```

Go 无独立 date 类型（time.Time 覆盖两者），Enum 场景在 Go 消费域（map[string]any 产出端已字符串化）不可达。**提示级**：无实质影响。另注意 `time.Time` 序列化用 UTC（Python isoformat 保留原时区）——跨时区部署下时间字段呈现不同，仅影响前端显示格式，不影响逻辑。

---

## 已核对无误的关键点（免复查清单）

| 项 | 结论 |
|---|---|
| 11.7 SessionMap（e1f79c0f） | makeKey/makeSessionID/load/save/get_session_id 逐行对齐，含同毫秒幂等分支、rotate 语义、防御式 load/save、hex 时间戳+6位随机 hex |
| 11.13 GatewayHookHandler（7c3f5a66） | 四事件对齐：SessionStart 去重+query=source、UserPromptSubmit 无 query、SessionEnd 先 discard+query=reason、Notification query=notification_type；setdefault 超时+panic 兜底全对齐 |
| 11.13 集成点①~④ | SessionStart×2（message_handler.go L229 + newSession 路径）、UserPromptSubmit（forwardLoop L166）与 Python L176/L666/L2192 一致；SessionEnd/Notification 在 Python gateway 中本就无调用点，Go 未接线属对齐 |
| 13.16 JSONParser HTML 转义 | SetEscapeHTML(false) 正确对齐 ensure_ascii=False 的原样语义；尾换行 TrimSuffix 处理正确 |
| 13.16 TxtMdParser 编码检测 | chardet 检测 + GB-18030 手工归一 + 空输入双防御（chardet nil,nil）经探针验证；软失败语义对齐 |
| 6.8a ModelBackupRail | 此前 2026-10-22 审查已确认 set_llm/request_retry 对齐（90d98feb + 440cc8ce 测试补齐） |
| S-24 修复（team_runner.go） | 非流式+流式两处 inputsDict 透传正确（原 nil 硬编码已修），注释准确描述 Python L194/L260 对应 |
| S-26/S-27 spawn 双参数契约 | BuildSpawnConfig payload={spec,context} 与 BuildSpawnPayload inputs={coordination,query} 正确分离；FromSpawnPayload map→JSON→struct 桥接对齐 model_validate；SpawnAgentConfig 5 字段齐全（runner_config/logging_config/session_id 均在，排除初判疑点） |
| S-11 TeamToolApprovalRail / TeamPlanModeRail | configurator 步骤 13/14 实装对齐（此前会话已核对） |
| S-16 resolveMemberModelFromDB | model_ref_json 解析 + ResolveMemberModelFromPool 恢复对齐（此前会话已核对） |
| shutdown cleanup 538 行切片 | scheduleShutdownCleanup/delayedShutdownCleanup/waitFor/notice 推送/attach wrapper 全对齐（含 rebind 语义、shield 等价、goroutine+done channel 替代 asyncio.Task） |
| isLeaderOutput/isTeammateOutput/enrichTeammateEvent | team_helpers.go L538-594 对齐 Python L237-278（runtime_ready/completed 控制帧放行、role 大小写宽容、source_member→member_name） |
| utilsParseStreamChunk 主体 | typed/dict 双分支、controller_output/llm_output/answer/tool_call/tool_result 等子分支对齐；_has_streamed_content 恒 False 的团队路径注释准确；team.* 命名空间直传对齐 |
| SkillManager 锁修复（864eed39） | addToRegistry 补锁 + Get/Has/Clear/Count 读锁 + description 死存储清除，与 asyncio→Go 并发移植规则一致 |
| gateway 测试排空修复（173c5152） | 阻塞接收+2s 超时替代排空 goroutine，消除消费竞争，方案正确 |
| lite watcher race（450d0f98） | watcherInit atomic + 两处 IsClosed() 锁读正确（除 M-03 残留点） |

---

## 修复优先级建议

1. **S-01**（ControllerOutputPayload 桥接）：团队流内容链路的核心缺口，与 M-05（RunStreaming 占位）合并考虑——9.57 回填后 S-01 立即成为高频触发路径，建议在回填前先修 S-01 避免叠加回归
2. **S-02**（header 一行修复）：成本最低收益明确，随手修
3. **M-02**（CJK 截断）：历史第 5 次同类问题，建议建立统一 `truncateRuneSafe` 工具函数一次性收口
4. **M-01**（UseNumber）：索引链路数据完整性，改动小
5. **M-03/M-04**：小修
6. **M-05**：流程决策项（改计划状态 or 实现分支 1），需用户定夺
