# 2026-10-24 48h 逻辑审查（汇总报告）

- **审查窗口**：2026-10-22 04:32 ~ 2026-10-24 04:32（61 个提交）
- **审查范围**（本窗口内完成的实现计划章节及批次）：
  - **11.7 SessionMap**（集成点③ `e4a87673` 等）
  - **11.8 InteractionContext**（`0557b8d4`~`21725b5d` 六连提交）
  - **11.13 Gateway Hook**（Handler + message_handler 集成点①~④）
  - **13.15 Image Parser + Captioner**（`44968838`~`87db71c3` 五连提交）
  - **13.16 JSON/TxtMd Parser**（`0d09c320`~`a781679d` 六连提交）
  - **6.8a ModelBackupRail**（`d5e30b96`~`b258c39a`，含 10-22 增量复核）
  - real_llm C 类补齐（`cf258b2a`）、agent_teams 批次 1+2 修复（`26125540`+`581e6b67`）、CI race 修复 3 连（`864eed39`/`450d0f98`/`173c5152`）、2026-10-23 审查修复批次
- **审查方法**：五路并行审查（11.7/11.8 集成层、11.13、13.15/13.16、6.8a+批次复核、message_handler 集成点交叉验证），逐方法签名与步骤对照 Python 参考项目（`agent-core/openjiuwen` + `jiuwenswarm-develop/jiuwenswarm`），13.15/13.16 附双侧行为探针实测（Python 3.12 与 Go 1.26 同机对照）。详细批次复核见同库 `2026-10-23-48h-logic-review.md`（513 行，随 `77b4948e` 入库），本报告为全窗口汇总，重点条目均经 team-lead 亲自二次验证（标注 ✅verified）。
- **问题统计**：**6 严重 / 17 一般 / 26 提示**

## 总体结论

本窗口移植整体质量高：11.7 SessionMap 与 11.8 InteractionContext 两文件本体逐行对齐（含 dataclass 三态默认值、五层过滤链等防御性细节）；11.13 GatewayHookHandler 四事件对齐且 HookExecutor 六大功能无丢失；13.15/13.16 常量与提示词逐字一致；6.8a 与批次 1+2 的修复本身全部正确落地。问题集中在三处：**① message_handler 集成层的 slash 命令语义偏差**（`/mode code` 映射错误、rewind 通知数值断言永不命中等）；**② 团队流消费链路的类型域丢失**（ControllerOutputPayload 结构体 chunk 被整块丢弃，`2026-10-23` 报告 S-01 复核确认仍未修）；**③ JSON/CJK 文本保真类惯性问题**（大整数 float64 精度、字节截断第 5 次出现）。

---

## 严重问题（S）

### S-01 `/mode code` 一级子命令映射错误——切 code 实际切成 agent.plan ✅verified

**Python 样例**（`jiuwenswarm/gateway/message_handler/message_handler.py L715-730`，MODE_OK 分支显式映射）：

```python
if mode_str == "agent":
    state.mode = ChannelMode.AGENT_PLAN
elif mode_str == "code":
    state.mode = ChannelMode.CODE_NORMAL   # ← 一级模式映射：code → code.normal
elif mode_str == "team":
    state.mode = ChannelMode.TEAM
elif mode_str == "agent.plan":
    ...
```

**Go 问题**（`internal/swarm/gateway/message_handler/slash_cmd.go:142-143` + `channel_state.go:60,322-327`）：

```go
case command_parser.ActionModeOK:
    newMode = ParseChannelMode(parsed.ModeSubcommand)
```

`channelModeLookup` 由 `channelModeStrings` 反向构建，只含 `"agent.plan"/"agent.fast"/"code.plan"/"code.normal"/"code.team"/"team"` 六个二级值，**没有裸的 `"agent"/"code"`**。`ParseChannelMode` miss 后 fallback `ChannelModeAgentPlan`（channel_state.go:87）。

**具体流程示例**：
1. 用户在受控通道发送 `/mode code`（command_parser 白名单前三个合法一级子命令之一，Python L694-703 明确放行）；
2. Go：`ParseChannelMode("code")` → lookup miss → 返回 `ChannelModeAgentPlan`；
3. `state.Mode` 被设为 agent.plan，通知用户 "mode 已变更为 agent.plan"；
4. Python：`/mode code` → `ChannelMode.CODE_NORMAL`，通知 "code.normal"。
5. 结果：用户想进代码模式却被切进规划模式，**行为完全反向**。`/mode agent` 碰巧正确（fallback 恰为 AgentPlan）、`/mode team` 正确（lookup 命中）。

**修复方案**：modeChangeCancelAndNotice 的 ActionModeOK 分支先做一级映射再回退：

```go
switch parsed.ModeSubcommand {
case "agent":
    newMode = ChannelModeAgentPlan
case "code":
    newMode = ChannelModeCodeNormal
case "team":
    newMode = ChannelModeTeam
default:
    newMode = ParseChannelMode(parsed.ModeSubcommand)
}
```

（或者在 `channelModeLookup` 构建时补充三个裸键映射，二选一；前者改动面更小。）

---

### S-02 applyGatewayTimeout 并发写共享 hooks map——data race + panic 逃逸 recover 兜底（进程崩溃风险）

**Python 样例**（`jiuwenswarm/gateway/hooks/handler.py L43-46 + L172-177`）：

```python
for cfg in hook_configs:
    cfg.setdefault("timeout", self._gateway_timeout)   # asyncio 单线程，永不并发
...
def trigger_session_start_hook(self, session_id, source="startup"):
    if self._gateway_hook_handler:
        asyncio.create_task(...on_session_start(...))  # 并发 task，但 setdefault 循环无 await 点，不可交叉
```

Python 的 asyncio 单线程协作式调度保证 setdefault 循环原子执行，永不并发写。

**Go 问题**（`internal/swarm/gateway/hooks/handler.go:73-76` + `message_handler.go:229`）：

```go
applyGatewayTimeout(hookConfigs, h.gatewayTimeout)  // L73 写共享 map
cwd, _ := os.Getwd()                                 // L75
defer recoverHookPanic("SessionStart")               // L76 defer 在 L73 之后才注册！
```

```go
go mh.gatewayHookHandler.OnSessionStart(context.Background(), sessionID, source)  // message_handler.go L229
```

**具体流程示例**（多用户并发 /new_session）：
1. 用户 A 与用户 B 几乎同时触发 `/new_session`，forwardLoop 串行处理两条消息，各起一个 SessionStart goroutine；
2. goroutine A 进入 `OnSessionStart` → `Match` 返回 hooks map 原始引用（handler_test.go L103 注释明确"setdefault 就地修改共享 map（Python 语义）"）→ `applyGatewayTimeout` 开始写 `HooksConfig.Events["SessionStart"][i].Hooks[j]["timeout"]=10`；
3. goroutine B 同时对**同一个** map 执行 setdefault——首次写入是新 key，并发插入可破坏 bucket → `concurrent map writes` panic；
4. 更严重：panic 发生在 L73，而 `defer recoverHookPanic` 在 L76 才注册 → **panic 逃逸所有 recover → goroutine 崩溃 → 整个进程崩溃**。四个 On* 方法均为 defer 晚于 applyGatewayTimeout 的模式（L93/L95、L115/L117、L133/L135 同病）。
5. 现实触发面：同一 event 并发多次触发（多用户同时 /new_session）；跨 event 的 hooks map 是不同实例，不受影响。

**修复方案**：
- 治本：消除并发写——`Match` 返回 hooks map 浅拷贝，或 applyGatewayTimeout 构造新 slice 不就地写（会破坏 handler_test.go L22-30/L104-109 的"就地修改可观察"断言，需同步改测试；若要保留 Python 就地修改可观察语义则在 HooksConfig 上加锁）；
- 治标（必做）：把 `defer recoverHookPanic(...)` 移到各 On* 方法第一行，至少保证 panic 被兜底、进程不崩。

---

### S-03 chunkToMap 丢弃 *ControllerOutputPayload 类型 chunk——task_failed/task_completion 事件前端静默丢失 ✅verified（2026-10-23 报告 S-01 复核仍未修）

**Python 样例**（`jiuwenswarm/server/utils/stream_utils.py L107-170`）：

```python
def _parse_typed_chunk(chunk, _has_streamed_content):
    payload = getattr(chunk, "payload", {})
    ...
    if chunk_type == "controller_output" and payload is not None:
        interactions = _find_interaction_payloads(payload)
        inner_t = getattr(payload, "type", None)      # pydantic 属性访问天然命中
        if inner_val == "task_failed":
            data = getattr(payload, "data", None)
            error = next((item.text for item in data if hasattr(item, "text")), "任务执行失败")
            return {"event_type": "chat.error", "error": error}
```

Python 端 task_loop 事件执行器（`openjiuwen/harness/task_loop/task_loop_event_executor.py L234-264`）产出 pydantic 对象 `ControllerOutputPayload(type=EventType.TASK_FAILED, data=[TextDataFrame(text=str(exc))], ...)`，`parse_stream_chunk` typed 分支经 `payload.model_dump(mode="json")` 转成 dict 后正常进入 controller_output 处理。

**Go 问题**（`internal/swarm/server/adapter/deep_adapter_team.go:1341-1430`）：

```go
case *stream.OutputSchema:
    if os == nil || os.Payload == nil {
        return nil
    }
    if m, ok := os.Payload.(map[string]any); ok {   // ← 三个分支都只接受 map[string]any
        ...
    }
    return nil                                       // ← 结构体 payload 断言失败 → nil
```

生产端确实产出结构体指针（非 map）：`task_loop/executor.go:276/347`、`task_loop/session_spawn_executor.go:172/230`、`controller/modules/task_scheduler.go:495` 均为 `Payload: &cschema.ControllerOutputPayload{...}`。消费循环 `deep_adapter_team.go:813-817` 对 nil `continue` 整块丢弃。

**具体流程示例**（团队 harness EnableTaskLoop=true 的成员任务失败）：
1. 成员 DeepAgent 的 task_loop 执行工具抛异常 → `executor.go:276` 产出 `OutputSchema{Type:"task_failed", Payload:&ControllerOutputPayload{Type:"task_failed", Data:[&TextDataFrame{Text:err}], Metadata:{task_id}}}`；
2. 团队流消费循环 `consumeStreamWithQuery` 收到 chunk → `chunkToMap(chunk)` → 三个 typed 分支全部只断言 `map[string]any`，结构体指针断言失败 → **返回 nil → continue，整块事件丢弃**；
3. 前端永远收不到 `chat.error`——用户看到成员任务无声失败；
4. 连带：`detectTaskFailed`（`agent_teams/agent/stream_controller.go:804-843`）同样只断言 `payload.(map[string]any)`（L813-816），结构体 payload 场景返回 nil → **task_failed 重试机制失效**。
5. 26125540 修了 chunkToMap 的 map 域形状，但 ControllerOutputPayload 域未桥接（`git log -S` 确认无后续修复提交）。

**修复方案**：对齐 Python `model_dump(mode="json")`，在 chunkToMap 三个 typed 分支加 `*cschema.ControllerOutputPayload` 桥接（json.Marshal→Unmarshal 到 map，或手工展开 Type/Data[]DataFrame/Metadata 键：TextDataFrame→`{text}`、JsonDataFrame→`{data}`）；detectTaskFailed 加结构体分支读 `cop.Type`/`cop.Data[0].Text`。测试 `deep_adapter_team_chunktomap_test.go` 现有 9 个用例全是 map payload，需补结构体用例。

---

### S-04 TeamMemoryManager.LoadAndInject 个人记忆段英文 header 复制粘贴错 ✅verified

**Python 样例**（`openjiuwen/agent_teams/memory/manager.py:294,302`）：

```python
header = "\n\n## 你的相关记忆\n\n" if cn else "\n\n## Your relevant memories\n\n"   # L294 个人记忆段
...
header = "\n\n## 团队共享记忆\n\n" if cn else "\n\n## Team shared memory\n\n"       # L302 团队共享段
```

**Go 问题**（`internal/agent_teams/memory/manager.go:308`）：

```go
if personalContent := m.fetchPersonalMemoryForPrompt(ctx, query); personalContent != "" {
    header := "\n\n## Team shared memory\n\n"    // ← 英文分支误用团队共享标题（从 L328 复制后忘改）
    if cn {
        header = "\n\n## 你的相关记忆\n\n"        // 中文分支正确
    }
```

英文环境下个人记忆被标注为 "## Team shared memory"，与真正的团队共享段（L328）形成两个同名段连排，模型无法区分记忆归属。

**修复方案**：一行改为 `"\n\n## Your relevant memories\n\n"`。补一个英文语言分支的断言用例。

---

### S-05 JSONParser 大整数经 float64 往返精度损坏——>2^53 雪花 ID 静默改写 ✅verified（双侧行为探针实测）

**Python 样例**（`openjiuwen/core/retrieval/indexing/processor/parser/json_parser.py:30-31`）：

```python
json_data = json.loads(raw_content)       # int 无限精度
return json.dumps(json_data, indent=2, ensure_ascii=False)
```

探针实测：`{"id": 12345678901234567890}` → Python 输出 `12345678901234567890`（精确）。

**Go 问题**（`internal/agentcore/retrieval/indexing/processor/parser/json_parser.go:76-97`）：

```go
var data any
if err := json.Unmarshal(raw, &data); err != nil { ... }   // 所有数字统一 float64（53 位精度）
```

探针实测：同输入 → Go 输出 `12345678901234567000`（末 3 位失真）。

**偏差说明**：19 位雪花 ID/订单号超过 2^53，序列化文本被改变 → 下游 chunk embedding 的文本与原文不符，检索命中内容错误。JSON 是结构化数据解析器，保真文本是其主职，属功能正确性问题。附带偏差：`{"x": 1.0}` Python 输出 `1.0`，Go 输出 `1`。（注：2026-10-23 报告 M-01 与本条同源，双路审查分别评为 M/S，按"功能问题都是严重问题"口径合并为 S。）

**修复方案**：解码侧用 `json.Decoder` + `UseNumber()`，数字保留为 `json.Number`；Encode 侧 `json.Number` 实现 `encoding.TextMarshaler`，Encoder 原样输出字符串内容（不带引号），大整数与 `1.0` 字面均逐字保留：

```go
dec := json.NewDecoder(bytes.NewReader(raw))
dec.UseNumber()
if err := dec.Decode(&data); err != nil { ... }
```

---

### S-06 /rewind 通知的 `.(int)` 断言在 WebSocket JSON 域永不命中——删除/剩余条数永远显示 0 ✅verified（team-lead 新发现）

**Python 样例**（`jiuwenswarm/gateway/message_handler/message_handler.py:1091-1102`）：

```python
pl = resp.payload if isinstance(resp.payload, dict) else {}
remaining = pl.get("remaining_records", 0)    # Python json.loads 数字解析为 int，直接可用
removed = pl.get("removed_records", 0)
await self._send_channel_notice(..., f"已回退到第 {turn_index} 轮...删除 {removed} 条记录，剩余 {remaining} 条。")
```

**Go 问题**（`internal/swarm/gateway/message_handler/slash_cmd.go:457-466`）：

```go
if r, ok := resp.Payload["remaining_records"]; ok {
    if n, isNum := r.(int); isNum {    // ← 断言 int，但值是 float64
        remaining = n
    }
}
```

**具体流程示例**（完整 wire 链路验证）：
1. AgentServer 处理 `session.rewind` 返回 `{"remaining_records": 12, "removed_records": 3}`（JSON 数字）；
2. 网关 `AgentClient.SendRequest`（`agent_client.go:223-295`）从 WebSocket 收到响应 → `json.Unmarshal` 到 `map[string]any`——**Go 的 encoding/json 把所有 JSON 数字解码为 float64**；
3. `e2a.ParseAgentServerWireUnary`（`wire_codec.go:49`）→ `E2AResponseToAgentResponse`（`gateway_normalize.go:504-517`）把 `body["result"]` 原样透传为 `resp.Payload`——数值仍是 float64；
4. slash_cmd.go:458 `r.(int)` 类型断言 **永不命中**（float64 ≠ int）→ remaining/removed 保持 0；
5. 用户看到的通知永远是："已回退到第 N 轮（"..."），删除 0 条记录，剩余 0 条。"——真实回退条数信息全部丢失。

**修复方案**：改为宽容数值转换（仓库内已有 10 处 `toInt` 同构实现，建议统一收口到公共工具）：

```go
if r, ok := resp.Payload["remaining_records"]; ok {
    if f, isNum := r.(float64); isNum {
        remaining = int(f)
    }
}
```

**系统性提示**：这是"map 断言白名单"风险的又一实例（同源问题见 2026-10-23 报告 N-02：`team_completion.go:189` 的 `task_count.(int)` 当前域内安全但跨进程 JSON 化后会踩同一坑）。凡从 WebSocket/JSON 边界读出的 map 数值，一律按 float64 断言或走 `toInt` 宽容转换。

---

## 一般问题（M）

### M-01 /mode 切换取消加了 hasActiveStreamTaskForSession 守卫——Agent 侧工作不会被停止

**Python**（message_handler.py L428-429 注释 + L599-603）：

```python
# 即使网关侧已无活跃流式拉取任务（例如 Agent 正在执行 shell/工具），也必须通知 AgentServer，
# 否则仅断开 CLI WebSocket 无法停止已派发的工作。
await self._cancel_agent_work_for_session(msg, params.old_sid, publish_interrupt_result=False)
```

**Go**（slash_cmd.go:198-202）：`if oldSID != "" && mh.hasActiveStreamTaskForSession(oldSID)` ——Python 无此守卫。Agent 正在执行 shell/工具但网关无活跃流式拉取任务时（Python 注释点名的场景），Go 不会向 AgentServer 发 cancel。同文件 /new_session 分支（L124-126）无此守卫，同文件内自相矛盾。
**修复**：去掉守卫，对齐 /new_session 分支。

### M-02 /branch 的 new_sid 用 ChannelID 而非 channel_type

**Python**（message_handler.py L966-967）：`new_sid = self._generate_channel_session_id(channel_type)`（用类型如 "feishu"）。
**Go**（slash_cmd.go:325）：`GenerateChannelSessionID(msg.ChannelID)`（用 channel_id 如 "feishu_enterprise:app1"）。生成的分叉 session_id 形态不匹配 Python `_KNOWN_JIUWENSWARM_SESSION_PREFIXES` 的 12 个前缀，为将来 ACP alias 判断埋雷。同文件 /new_session 的 else 分支（L76）正确用了 `string(channelType)`，branch 分支用错。
**修复**：先 `channelType := mh.resolveControlChannelType(msg)` 再生成。

### M-03 /rewind 确认提示文案与 Python 完全不同（丢失不可逆警告）

**Python**（message_handler.py L1039-1045，四行模板）：含"此操作不可逆，将删除第 N 轮及之后的所有对话""注意：回退不影响手动编辑的文件或通过 bash 执行的命令"，confirm 格式为 `/rewind confirm {turn_index}`。
**Go**（slash_cmd.go:393）：单行简化版"确认回退到第 N 轮？发送 /rewind confirm 确认，/rewind cancel 取消。"——不可逆警告、删除范围说明、bash/文件影响说明全部丢失，违反提示词一比一纪律。
**修复**：逐字复制 Python 四行模板（含 `\n` 与 confirm 带轮次编号格式）。

### M-04 /rewind 无本地 fallback——Python E2A 失败后本地截断 history.json ✅verified

**Python**（message_handler.py L1109-1131）：AgentServer 返回错误或请求异常 → warning → fallback `rewind_session(session_id, turn_index)` 本地截断 history.json → 通知带 `（注意：上下文未同步截断）`。
**Go**（slash_cmd.go:435-476）：SendRequest 失败或 `!resp.OK` → 直接发送"回退失败"通知，无本地 fallback 路径（gateway 全目录 grep 无 `rewind_session` 调用；Go 等价物 `sessionops.RewindSession` 已存在但只在 harness 侧，受 6.5 架构规则约束不能反向 import）。
**修复**：在 gateway 侧补一个到 sessionops 的薄转发（经 server 侧已注册的 handler 或 E2A 之外的本地通道），或记 TODO(#11.x) 回填点并在 doc.go 标注；E2A 失败场景至少对齐 Python 的 warning 日志。

### M-05 /new_session 与 /mode 的 cancel→notice 顺序语义丢失

**Python**（message_handler.py L575-591 docstring）："先完成旧会话取消与 AgentServer 中断，再下发 session 已变更提示"——`await cancel` 完成后才 `_send_channel_notice`。
**Go**（slash_cmd.go:122-129 / L201）：`go mh.CancelAgentWorkForSession(...)` fire-and-forget，通知立即发出。用户收到"已变更"后旧任务仍可能跑一小段。modeChange 分支同病。
**修复**：同步调用完成后再通知，或用 entry.wg 等待；至少注释声明偏差并确认可接受。

### M-06 resolveControlChannelType 的 fallback 语义偏差（provider 缺失时过度受控）

**Python**（message_handler.py L339-345）：provider 缺失时 channel_type = 完整 channel_id（如 `"feishu_enterprise:app1"`），不在 `_control_channel_types` 精确匹配集合 → 不受控。
**Go**（channel_state.go:183-190 + L261-282）：`inferChannelTypeFromID` 做前缀推断 → `feishu_enterprise:app1` 被推断为 Feishu → 受控。provider 缺失的消息（企业通道早期消息、测试桩）会被误纳入 slash 命令拦截与状态注入。
**修复**：fallback 直接 `channel_manager.ChannelType(msg.ChannelID)` 原样返回，对齐 Python 精确匹配语义。

### M-07 /rewind preview 字节截断，CJK 乱码（CJK 截断惯性问题）

**Python**（message_handler.py L1101-1102）：`preview[:50]` 是 rune 截断。
**Go**（slash_cmd.go:450-455）：`p[:50]` 是字节截断，中文场景在第 17 个汉字处截断产生非法 UTF-8 序列。
**修复**：`r := []rune(p); if len(r) > 50 { preview = string(r[:50]) }`。与 M-14 同属"CJK 截断第 N 次"惯性问题，建议建统一 `truncateRuneSafe` 工具一次收口。

### M-08 hook reason 兜底语义——Python key 存在即用原值（含空串/非字符串），Go 仅非空 string 才用

**Python**（executor.py L154-160，prompt hook 同模式 L198-201）：`error=data.get("reason", "blocked by hook")`——key 存在（哪怕 `""`/`123`）即用原值。
**Go**（executor.go L196-199，runPromptHook L448-451 同模式）：`if v, ok := data["reason"].(string); ok && v != ""`。`{"decision":"block","reason":""}` 时 Python error=""，Go error="blocked by hook"。ShowToModel 拦截提示文案在边缘输入下不一致。
**修复**：去掉 `&& v != ""`；非字符串场景用 `fmt.Sprintf("%v", ...)` 对齐原值传递。

### M-09 forwardLoopStep3UserPromptSubmit 缺 str() 强转语义

**Python**（message_handler.py L2189-2190）：`prompt_text = str(params.get("query") or params.get("content") or "")`——数字等 truthy 值 → str() 转换。
**Go**（forward_loop.go L152-157）：只断言 `.(string)`，`params={"query":123}` 时 Python prompt="123"，Go 直接跳过取 content。hook 收到的 prompt 字段在非字符串输入下不一致。
**修复**：query/content 取值后类型 switch：string 直接用，float64/int/bool 用 `fmt.Sprintf("%v")`（仅 truthy 时），按 Python or 链回退。

### M-10 hook timeout 配置为字符串时行为分歧——Python 报错不执行，Go 静默用默认值

**Python**（executor.py L66 + L85-88）：`timeout: "30"`（YAML 引号字符串）→ `asyncio.wait_for(..., timeout="30")` TypeError → NON_BLOCKING_ERROR，hook 拒绝执行（配置错误显式暴露）。
**Go**（executor.go L274-282，runPromptHook L398-406 同模式）：switch 只命中 int/float64，字符串 → 静默保持默认 30 执行（配置错误被吞）。
**修复**：switch 增加 `case string:`——strconv.Atoi 成功则用、失败返回 NON_BLOCKING_ERROR，或至少记 Warn。

### M-11 JSON 对象 key 排序：Go 按字母序，Python 保插入序（探针实测）

**Python**（json_parser.py L30-31，dict 保序）：`{"b": 1, "a": 2}` → 输出保持 `b` 在前。
**Go**（json_parser.go:77，`map[string]any`）：输出 `a` 在前。JSON 语义等价但文本不同 → embedding 向量与 Python 管线不一致；字段较多时 diff 噪声大。
**修复**：用 `json.Decoder` Token 流手工构造保序结构（orderedmap），Encoder 侧自定义 MarshalJSON 按序输出。若团队接受"语义等价"可降级处置，但需注释归档。

### M-12 编码检测器差异：chardet ≠ charset_normalizer（含 htmlindex 楔子）

**Python**（txt_md_parser.py:27）`charset_normalizer.detect` vs **Go**（txt_md_parser.go:127）`chardet.DetectBest`。双侧行为探针对照：

| 输入 | Python detect | Go chardet | 解码结果 |
|---|---|---|---|
| 长中文 GBK(108字×3) | GB18030 conf1.0 | GB-18030 conf100 | **一致** ✓（主路径） |
| 短 GBK「你好，世界」 | CP949(Korean) | ISO-8859-7(Greek) | 两边都乱码，乱码文本不同 |
| UTF-32 系 | 可检出 | 可检出但 htmlindex 查 "UTF-32LE"→**nil** | **Go 回落 UTF-8 原样→全乱码** |

两个次生问题：① `charset.Lookup("ISO-8859-1")` 实际返回 Windows-1252（0x80-0x9F 区间解出曲引号/€，Python 纯 Latin-1 解出 C1 控制符）；② UTF-32 编码名查表 nil 直接放弃解码（txt_md_parser.go:138 `e, _ := charset.Lookup(name)` 失败**无日志**静默走原样路径）。
**修复建议**：① 查表失败补 Warn 日志（可观测性）；② UTF-32LE/BE 在 switch 里手工映射 `unicode.UTF32.LittleEndian/BigEndian`；③ 短文本乱码差异属检测器选型既定限制（长文本一致），注释归档即可。

### M-13 errors="ignore" 语义：Go 解码器输出 U+FFFD，Python 丢弃非法字节（探针实测）

**Python**（txt_md_parser.py:40 / json_parser.py:25）：`errors="ignore"` 非法字节静默丢弃。GBK 截断尾部探针：Python 输出 `你好，世`（干净截断）。
**Go**（txt_md_parser.go:109 x/text Decoder 默认替换策略）：同输入输出 `你好，世\ufffd`；中段非法字节 → `你\ufffd好`。损坏/截断文件的输出文本混入 U+FFFD（进 caption/chunk 文本）。
**修复**：解码后 `strings.ReplaceAll(s, "\uFFFD", "")` 近似（误伤合法 U+FFFD 概率极低），或接受差异注释归档。

### M-14 fetchPersonalMemoryForPrompt 字节截断产生非法 UTF-8（CJK 截断历史第 5 次）✅verified

**Python**（manager.py 个人记忆预算截断）：`content[:remaining]` 字符口径。
**Go**（`internal/agent_teams/memory/manager.go:535-537`）：`truncated = truncated[:remaining]` 字节切片可切穿多字节 rune。注：同包 extractor.go 的截断已用 rune 口径（L119/L144），说明团队知悉正确写法，此处遗漏。
**修复**：按字节预算回退到 rune 边界；与 M-07 一并建统一 `truncateBytesRuneSafe` 收口。

### M-15 lite GetMemoryIndexManager 缓存命中路径 closed 裸读残留（450d0f98 漏修）✅verified

**Go**（`internal/agentcore/memory/lite/manager_impl.go:551`）：`if !mgr.closed` ——`Close()` 持锁写（L462-468）、`Sync` 持锁读，此处在 `sync.Map Load` 后无锁直接读，构成 data race（race 检测可检出）。450d0f98 修了两处 `IsClosed()` 锁读，此处残留；接口 `MemoryIndexManager`（manager.go L15-30）缺 `IsClosed()` 方法无法直接走锁读。
**修复**：接口加 `IsClosed()` 或对 cached 断言到具体类型走 `IsClosed()`。

### M-16 extractor list_memory_files 绕过 sysOp.Fs() 直读本地目录

**Python**（extractor.py）：文件列举走 fs 抽象。
**Go**（`internal/agent_teams/memory/extractor.go:215`）：`os.ReadDir` 直读本地目录，与 read/write 走 Fs 抽象不一致——非本地 Fs 后端下文件列表与可读写文件不一致。
**修复**：走 `sysOp.Fs().ListFiles`。

### M-17 TeamHarness.RunStreaming 空壳但 IMPLEMENTATION_PLAN 9.57 标 ✅（流程矛盾）✅verified

**Go**（`internal/agent_teams/harness.go:352-359`）：`ch := make(chan stream.Schema); close(ch); return ch, nil`——立即 close 的空壳；注释标注"⤵️ 待 9.57 deepAgent 类型升级后实现分支 1"。但 `IMPLEMENTATION_PLAN.md:593` 的 9.57 AgentConfigurator 已标 ✅。
**影响**：成员流式内容链路整体空流——S-20/S-24 修复的价值要等 RunStreaming 实装才兑现。
**修复（二选一，需定夺）**：① 改计划状态为 🔄/⤵️ 如实反映；② 直接实现分支 1（`teamSession==nil && !initialPlanMode → runner.RunAgentStreaming`，runner 已有 RunAgentStreaming，成本低可立即打通）。

### M-18（N-01）BuildSpawnConfig 不填 RunnerConfig/LoggingConfig——成员日志隔离缺失

**Python**（payload.py L116-128）：填 `serialize_runner_config(Runner.get_config())` + `_build_member_logging_config(member_tag)`（per-member 日志目录隔离 `teammates/<member_tag>/` 重写文件 sink）。
**Go**（`internal/agent_teams/runtime/payload.go:121-143`）：三个字段均不填。Runner.SpawnAgent 会兜底 SessionID/LoggingConfig，但 **RunnerConfig Go 端无兜底**——子进程拿不到 runner 配置；per-member 日志目录隔离完全缺失，所有成员混写同一日志文件。
**修复**：登记回填项；实现时对齐 `_build_member_logging_config` 独立职责。

---

## 提示问题（T）

### 11.7/11.8 集成层（Agent 1）

| # | 问题 | 位置 | 处置 |
|---|---|---|---|
| T-01 | SessionMap.save 用 MarshalIndent 未关 HTML 转义，同包 interaction_context 用 Encoder+SetEscapeHTML(false)，不一致 | session_map.go:218 | 参照 interaction_context 写法 |
| T-02 | SessionMap.load 对非 dict JSON 记 Warn；Python 静默跳过 | session_map.go:199-202 | Unmarshal 到 any 后断言 map，非 map 静默 return |
| T-03 | LoadSessionMapScope 对非字符串配置值静默用默认；Python str() 后走未知值 Warn | session_map.go:74 | `fmt.Sprintf("%v", v)` 后进同一 switch |
| T-04 | TTL 用 Unix() 整秒 vs Python time.time() float 秒 | interaction_context.go:261/282 | 亚秒级精度差异，可忽略 |
| T-05 | ApplyChannelState params 非对象时 Go 覆盖 Params；Python 原样保留 | channel_state.go:120-129 | Unmarshal 失败跳过 mode 注入不覆盖 |
| T-06 | handleChannelControl 加了 ReqMethod==ChatSend 前置过滤，Python 无 | slash_cmd.go:39 | 保守收紧，注明偏差 |
| T-07 | switch 前统一 GetOrCreateChannelState；Python SKILLS_OK 分支在取 state 前 return | slash_cmd.go:64 | 多缓存一条 state，把取 state 下移到需要它的分支 |
| T-08 | skills req_id 无随机后缀；Python 有 `_{token_hex(3)}` | slash_cmd.go:246 | 接 generateRandomHex(3) |
| T-09 | branch fork_title 空串时 Go 用 branchName；Python 键存在即用（空串） | slash_cmd.go:364-371 | 边界一致化 |
| T-10 | `_KNOWN_JIUWENSWARM_SESSION_PREFIXES`（12 前缀）及两个判断函数 Go 无对应 | dispatch.go:30 | ACP 回填时别漏这组前缀判断 |
| T-11 | get_checkpoint_dir legacy 迁移 Go 无 | paths.go:273 | Go 全新部署无 legacy 可迁，知悉 |

另：team-lead 交叉验证补充——`generateRandomHex` fallback 的 `[:n*2]` 在 n≥8 时理论越界（UnixNano hex 约 15-16 字符），当前仅 n=3 使用，提示知悉。

### 11.13 Gateway Hook（Agent 2）

| # | 问题 | 位置 | 处置 |
|---|---|---|---|
| T-12 | HookMatcher.Matches 未 strip pattern（Python `self.matcher.strip()`）；`matcher: "  *  "` Python 匹配所有、Go 精确匹配失败 ✅verified | config.go:70-74 | 入口 `strings.TrimSpace(m.Matcher)`，一行修复 |
| T-13 | exit≠0 分支 stderr 判空方式微差（Go TrimSpace 更合理） | executor.go:381-384 | 可接受偏差，记录 |
| T-14 | queryLLM content 非 str/list 兜底缺失（最终 decision 等价） | executor.go:528-540 | 提示级 |
| T-15 | 超时 kill 失败日志无对应（Go CommandContext 自动 kill 合理）+ Go 新增超时 Debug | executor.go:330 | 可接受，记录 |
| T-16 | OnNotification/OnSessionEnd 无调用方——Python 同样未接线，精确对齐（连带 activeSessions 只增不减两侧一致） | — | 无需动作 |
| T-17 | handler_test 未断言 hook_input 内容（M-09 类偏差因此漏检） | handler_test.go | 随 M-09 修复补 `type: command + cat > 文件` 捕获 stdin JSON 断言 |

### 13.15/13.16（Agent 3，探针实测）

| # | 问题 | 处置 |
|---|---|---|
| T-18 | U+2028/U+2029 转义差异：Go Encoder 输出 `\u2028`（SetEscapeHTML(false) 不豁免行分隔符），Python 原样输出 | JSON 层等价，可不修 |
| T-19 | NaN/超界浮点：Python loads 接受非标 NaN/1e400→inf；Go Unmarshal 报错走 JSONDecodeError 分支返回原文——Go 更贴合"JSON format error"日志意图 | 可接受分歧，注释归档 |
| T-20 | parser 目录残留 git 未跟踪的空 `images/` 目录（测试 CpImage 在 cwd 创建） | 测试用 t.TempDir()+chdir 隔离，确认 .gitignore 覆盖 |

### 6.8a/批次/团队流（Agent 4，详细版见 2026-10-23 报告）

| # | 问题 | 处置 |
|---|---|---|
| T-21 | BuildSpawnPayload leader_member_name `""` vs Python `null`（payload.go:58） | 记录 |
| T-22 | TxtMdParser BOM 剥离为良性增强（Python 靠 UTF-8-SIG 检测） | 记录 |
| T-23 | extractor read 工具空数据/异常分支合并 note 键 | 记录 |
| T-24 | shutdown cleanup fallbackSessionID 读 rebind 后值非构造快照（remote_member_shutdown_cleanup.go:307） | 记录 |
| T-25 | GatewayHook 先标记后 match 忠实复刻 Python 缺陷 | 防误修，保留 |
| T-26 | SerializeValue 无独立 date 类型 | 无实质影响 |
| N-02 | `task_count.(int)` 断言——inprocess 直传当前安全，跨进程 JSON 化后变 float64 会失败（与 S-06 同源系统性风险） | 统一 ToInt 宽容转换 |
| N-03 | spawnSubprocess sessionID 形参未使用，靠 inputs 键隐式传递，与 Python 显式传参优先级有微妙偏差 | 低风险，登记备查 |

---

## 已核对无误项（免复查清单，摘录）

- **11.7 SessionMap**：make_key/make_session_id 格式（`::` 分隔、hex 毫秒、6 位随机 hex）、scope 枚举、rotate 语义、幂等分支、load/save 过滤、跨实例恢复、`base:` 前缀匹配、含 `:` 的 channelID glob 实测等价
- **11.8 InteractionContext**：15 字段 dataclass、UnmarshalJSON 三态默认值（必填检测/可选默认/显式 null）、FindPending 五层过滤链顺序、FindGroupPending 直达加载、CleanupExpired 宽松时间解析三态、BuildResumeContent 两套中文模板逐字一致
- **11.13 GatewayHookHandler**：四事件方法对齐；HookExecutor 六大功能完整（run_all/_run_command_hook/parse_command_output/_run_prompt_hook/_query_llm/extract_json_from_response 三级）；RegisterConfig 装配时序正确；user_hook_rail 留守文件无旧路径残留；`//go:build llm` 隔离符合规范 3.2
- **13.15**：IMAGE_CAPTION_PROMPT 与 Python 逐字一致（含 em-dash）；白名单四前缀、`.jfif→image/jpeg` 注册、CpImage 四分支软失败、多模态消息构造、captioner 7 处日志全对齐
- **13.16**：六 txt/md 扩展名注册列表一致；空文件→空串；TrimSpace 与 Python str.strip() Unicode 空白集合一致
- **6.8a**：set_llm/request_retry 修复对齐（90d98feb + 440cc8ce），增量无新偏差
- **批次 1+2**（26125540+581e6b67）：S-20 utilsParseStreamChunk ~340 行完整移植（typed/dict 双分支、八类事件、task_failed→chat.error、team.* 直传）；S-21 teamHelperEventAsMap 展平链；S-18 Evolution 三函数；S-24 inputs 透传；S-26 FromSpawnPayload 桥接；S-27 child.go TEAM_AGENT 分支；S-11 两个 Rail 实装；S-12 TeamMemoryManager 5 方法；M-17/M-18/T-18；附录 4 六个 handler 逐分支对齐；581e6b67 测试增强后断言真实
- **CI race 修复 3 连**：864eed39（registry 互斥锁）、450d0f98（watcherInit atomic，M-15 为漏修残留）、173c5152（排空 goroutine）方案正确
- **shutdown cleanup**：538 行全对齐（goroutine+done channel 替代 asyncio.Task、shield 等价、注册表替换等价）

---

## 修复优先级建议

1. **S-01**（/mode code 映射，用户直接可感知的功能反向，改动小）
2. **S-06**（rewind float64 断言，一行级修复×2，用户可见信息全丢）
3. **S-04**（英文 header 一行修复）
4. **S-03**（ControllerOutputPayload 桥接——团队流内容链路核心缺口，9.57 RunStreaming 实装后立即成为高频触发路径，建议先修避免叠加回归）
5. **S-02**（hooks 并发 panic——defer 前移是零成本治标，治本浅拷贝需同步改测试）
6. **S-05**（UseNumber，一并修浮点表示）
7. **M-17**（流程决策项：改计划状态 or 实现 RunStreaming 分支 1，需定夺）
8. **M-07+M-14**（CJK 截断收口，建统一 truncateRuneSafe）
9. **M-01/M-02/M-03/M-04/M-05/M-06**（slash 命令语义对齐批次，可合并一个 PR）
10. **M-08/M-09/M-10/M-12**（hook 边缘语义 + 编码检测小修）
11. **M-11/M-13**（需定夺：接受为归档偏差 or 实装）
12. **M-15/M-16/M-18**（小修 + 登记回填）
13. **T 级**按顺手修（T-12 一行 TrimSpace 优先）

---

## 附录：审查执行记录

- **general-purpose-1**：11.7/11.8 本体 + message_handler 集成层（1S/7M/11T）
- **general-purpose-2**：11.13 Gateway Hook（1S/3M/6T，含 HookExecutor 六功能完整性矩阵）
- **general-purpose-3**：13.15/13.16（1S/3M/3T，双侧行为探针实测：mimetypes/json.dumps/json.loads/charset_normalizer/chardet/x-text 解码）
- **general-purpose-4**：6.8a 复核 + 任务 B 四提交复核 + 批次 1+2 复核（2S/5M/6T + 3 新发现；详细报告已落盘 `2026-10-23-48h-logic-review.md`）
- **team-lead（本人）**：message_handler 集成点交叉验证——S-01/S-04/S-05/S-06 及 M-04/M-14/M-15/M-17 亲自二次核实行号与证据链；S-06 为本人新发现（wire 链路完整验证：agent_client.go → wire_codec.go → gateway_normalize.go → slash_cmd.go）
- Agent 5（远程成员关停清理专项）因会话压缩失联；该领域已由 general-purpose-4 的 shutdown cleanup 复核覆盖（538 行全对齐 + T-24 发现），无信息缺口。
