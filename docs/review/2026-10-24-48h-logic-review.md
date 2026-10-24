# 2026-10-24 48h 逻辑审查报告（11.7/11.8/11.13/13.15/13.16/6.8a + 批次1+2 复核）

- **审查窗口**：2026-10-22 00:00 → 2026-10-24（61 个提交，HEAD=77b4948e）
- **审查方法**：5 路并行审查（SessionMap+InteractionContext+message_handler 集成点 / Gateway Hook / Parser 双侧探针实测 / ModelBackupRail+批次1+2 复核 / 远程成员关停清理），全部 S/M 级发现经 team-lead 逐条在 HEAD 交叉验证确认
- **本窗口完成章节**：11.7 SessionMap、11.8 InteractionContext、11.13 Gateway Hook、13.15 Image Parser+Captioner、13.16 JSON/TxtMd Parser、6.8a ModelBackupRail、10-14 审查批次1+2 修复（26125540+581e6b67）、并发修复批次（864eed39/450d0f98/173c5152）、远程成员关停清理（含于 26125540）

## 结论摘要

| 级别 | 数量 | 编号 |
|------|------|------|
| 严重 | 6 | S-01~S-06 |
| 一般 | 14 | M-01~M-14 |
| 提示 | 22 | T-01~T-22 |

**总体评价**：六个章节本体（SessionMap/InteractionContext/GatewayHook 四事件/Parser 三件套/ModelBackupRail/shutdown cleanup 主链路）移植质量高，主路径逐行对齐且测试完备。问题集中在三类：①团队流 chunk 类型域桥接缺口（S-01，9.57 回填后立即高频触发）；②message_handler 集成层的指令映射/文案/fallback 偏差（S-02 等）；③"结构体 payload vs map payload"与"JSON 数字 float64 化"两个 Go/Python 类型系统系统性差异的多点显形（S-04/M-01/rewind int 断言）。

---

## 严重问题（S 级）

### S-01 chunkToMap 丢弃 *ControllerOutputPayload 类型 chunk——task_failed/task_completion 事件前端静默丢失（9.57 回填后立即高频触发）

**Python 样例**（stream_utils.py L107-170 + controller_output.py L46-62）：
```python
class ControllerOutputChunk(OutputSchema):
    type: str = "controller_output"          # L60：chunk.type 固定 controller_output
    payload: ControllerOutputPayload = None  # L61：payload 是 pydantic 模型

# task_loop_event_executor.py L256-265（生产端）：
payload = ControllerOutputPayload(type=EventType.TASK_FAILED, data=[TextDataFrame(text=str(exc))], ...)
yield ControllerOutputChunk(index=0, payload=payload, last_chunk=True)

# stream_utils.py L150-170（消费端）：getattr(payload, "type") 天然命中 pydantic 属性
if chunk_type == "controller_output" and payload is not None:
    ...
    if inner_val == "task_failed":
        return {"event_type": "chat.error", "error": error}
```

**Go 问题**：
- 生产端 executor.go L274-284 / L345-355、session_spawn_executor.go L172/L230、task_scheduler.go L495/L895 产出 `OutputSchema{Type: "task_failed", Payload: &cschema.ControllerOutputPayload{...}}`（结构体指针）
- 消费端 deep_adapter_team.go L1341-1430 三个分支（`*stream.OutputSchema`/`stream.OutputSchema`/`*atschema.TeamOutputSchema`）的 payload 全部只断言 `map[string]any`，结构体断言失败 → `return nil` → 消费循环 L814 `continue` 静默丢弃
- 附带偏差：Go chunk.Type 设为 `"task_failed"` 而 Python 固定为 `"controller_output"`（内层 payload.type 才是 task_failed）——即便桥接了 payload，外层 type 也与 Python 不一致
- 连带：detectTaskFailed（agent_teams/agent/stream_controller.go L813）同样只断言 `map[string]any`，Python `_detect_task_failed`（agent-core stream_controller.py L56-75）用 `getattr(payload, "type")` 读 pydantic 属性天然命中——task_failed 重试机制在结构体 payload 场景失效

**触发条件**：团队 harness `EnableTaskLoop=true` 的 DeepAgent 任务循环，任务失败/完成时产出该类型 chunk（当前 RunStreaming 空壳故未显形；9.57 回填后立即成为高频路径）。

**修复方案**：
1. chunkToMap 三个分支各加 `*cschema.ControllerOutputPayload` 桥接：`json.Marshal(cop)` → `json.Unmarshal` 到 `map[string]any`（等价 Python `model_dump(mode="json")`），TextDataFrame→`{"text":...}`、JsonDataFrame→`{"data":...}` 由 Marshal 自然产出
2. 生产端 executor.go 等 4 处把 `Type` 改为 `"controller_output"`（对齐 Python ControllerOutputChunk.type 固定值），task_failed 语义走内层 payload.type
3. detectTaskFailed 加结构体分支：`if cop, ok := payload.(*cschema.ControllerOutputPayload); ok` 读 `cop.Type`/`cop.Data[0]` 的 Text
4. deep_adapter_team_chunktomap_test.go 现有 9 用例全是 map payload，补结构体 payload 用例

**具体流程示例**：
```
成员 DeepAgent(task_loop) 任务执行
→ executor.go:259 invokeErr != nil
→ ch <- OutputSchema{Type:"task_failed", Payload:&ControllerOutputPayload{Type:"task_failed", Data:[{Text:"..."}]}}
→ consumeStream 循环 deep_adapter_team.go:813 chunkToMap(chunk)
→ case *stream.OutputSchema: os.Payload.(map[string]any) 断言失败
→ return nil → L814 continue → chunk 丢弃
【预期(Python)】chat.error 事件达前端 + StreamController 重试判定生效
【实际(Go)】前端无任何 task_failed 提示，任务静默失败
```

### S-02 `/mode code` 一级子命令映射错误——切 code 实际切到 agent.plan

**Python 样例**（message_handler.py L715-730，MODE_OK 分支显式一级映射）：
```python
if mode_str == "agent":
    state.mode = ChannelMode.AGENT_PLAN
elif mode_str == "code":
    state.mode = ChannelMode.CODE_NORMAL    # ← 一级映射
elif mode_str == "team":
    state.mode = ChannelMode.TEAM
elif mode_str == "agent.plan":
    ...
```

**Go 问题**（slash_cmd.go L142-143 + channel_state.go L50-57/L82-88）：
```go
case command_parser.ActionModeOK:
    newMode = ParseChannelMode(parsed.ModeSubcommand)  // 只查二级六值表
```
`channelModeLookup` 只含 `agent.plan/agent.fast/code.plan/code.normal/code.team/team` 六个二级值，无裸的 `agent/code`。`/mode code` miss 后 fallback `ChannelModeAgentPlan`。`/mode agent` 碰巧正确（fallback 恰为 AgentPlan）、`/mode team` 正确（命中），**`/mode code` 行为完全反向**——而 `/mode code` 是 command_parser 白名单前三个合法一级子命令之一。

**修复方案**：modeChangeCancelAndNotice 的 ActionModeOK 分支先做一级映射：
```go
switch parsed.ModeSubcommand {
case "agent": newMode = ChannelModeAgentPlan
case "code":  newMode = ChannelModeCodeNormal
case "team":  newMode = ChannelModeTeam
default:      newMode = ParseChannelMode(parsed.ModeSubcommand)
}
```

### S-03 /rewind 通知数字恒为 0——`.(int)` 断言在 WebSocket JSON float64 上永不命中 + Python 的本地 fallback 整层缺失

**Python 样例**（message_handler.py L1090-1131）：
```python
resp = await self._agent_client.send_request(env)
if resp.ok:
    remaining = pl.get("remaining_records", 0)   # Python json int，直接取值
    removed = pl.get("removed_records", 0)
    ...
# AgentServer 返回错误或请求异常时：
result = rewind_session(session_id=target_sid, turn_index=turn_index)  # ← 本地 fallback 截断 history.json
await self._send_channel_notice(... f"，删除 {removed} 条记录，剩余 {remaining} 条。" f"（注意：上下文未同步截断）")
```

**Go 问题**（slash_cmd.go L457-466，两层）：
1. 响应链路：WebSocket 收到 JSON → agent_client.go L410 `json.Unmarshal(data, &msg)` → **数字统一解码为 float64** → wire_codec.go → gateway_normalize.go L507 `body["result"]` 直接透传为 Payload → slash_cmd.go L458/L463 `r.(int)` 断言 **永不命中** → remaining/removed 恒为 0，用户执行 /rewind 后永远看到"删除 0 条记录，剩余 0 条"
2. **本地 fallback 整层缺失**：Python 在 `!resp.ok` 或请求异常时有 `rewind_session` 本地截断分支（L1114-1131，通知带"（注意：上下文未同步截断）"）；Go L435-476 只发"回退失败"错误通知直接返回，`grep rewind_session` 在 gateway 包零命中

**修复方案**：
1. 断言改宽容转换：
```go
if n, ok := r.(float64); ok { remaining = int(n) } else if n, ok := r.(int); ok { remaining = n }
```
（或封装 toInt helper；team_completion.go L189/L218 的 `.(int)` 断言同源风险一并收口）
2. 补本地 fallback：若 Go 端 session_ops 的 rewind_session 等价物已移植则接分支+警示文案；未移植则记 TODO(⤵️) 回填点并在 doc.go 标注

### S-04 JSONParser 大整数精度损坏——>2^53 整数经 float64 往返静默改写（双侧行为探针实测）

**Python 样例**（json_parser.py L30-31）：`json.loads` int 无限精度。探针实测 `{"id": 12345678901234567890}` → Python 输出 `12345678901234567890`（精确）。

**Go 问题**（retrieval/indexing/processor/parser/json_parser.go L76-77）：
```go
var data any
if err := json.Unmarshal(raw, &data); err != nil { ... }
```
所有数字统一 float64（53 位精度）。探针实测同输入 → Go 输出 `12345678901234567000`（末 3 位失真）。19 位雪花 ID/订单号序列化文本被改变 → 下游 chunk embedding 文本与原文不符，检索命中内容错误。JSON 是结构化数据解析器，主职就是保真文本。附带：`{"x": 1.0}` Python 输出 `1.0`，Go 输出 `1`。

**修复方案**：解码侧 `json.NewDecoder + UseNumber()`，数字保留为 `json.Number`（字符串）；Encode 侧 `json.Number` 实现 TextMarshaler，Encoder 原样输出（不带引号）——大整数与 `1.0` 原始字面逐字保留。

### S-05 TeamMemoryManager.LoadAndInject 个人记忆段英文 header 复制粘贴错（一行修复）

**Python 样例**（agent-core manager.py L294）：
```python
header = "\n\n## 你的相关记忆\n\n" if cn else "\n\n## Your relevant memories\n\n"
```

**Go 问题**（agent_teams/memory/manager.go L308）：英文分支误用 `"\n\n## Team shared memory\n\n"`（从 L328 团队段复制后忘改），中文分支正确。英文环境下个人记忆被误标为"团队共享记忆"，与 L328 的真团队段两个同名 header 连排。

**修复方案**：L308 一行改 `"## Your relevant memories"`。

### S-06 DeleteSessionRuntime 错误契约失真——清理失败仍推送"已解散"成功通知，err 分支成死代码

**Python 样例**（team_manager.py L1948-1955）：
```python
except Exception as exc:
    logger.warning("[TeamManager] failed to delete team session runtime: ...")
    return False        # ← 失败返回 False
```
（消费端 remote_member_bootstrap.py L1380-1391：deleted=False → 用户收到"团队解散清理未完成，请查看后端日志确认原因。"）

**Go 问题**（team_manager_lifecycle.go L523-547）：
```go
if _, err := runner.DeleteAgentTeam(ctx, teamName, []string{sessionID}, true); err != nil {
    logger.Warn(...)    // ← 错误仅 Warn 吞掉
}
...
return true, nil         // ← 恒 (true, nil)
```
remote_member_shutdown_cleanup.go L492-499 的 `if err != nil` 分支**永远不可达**（死代码），L505 `pushShutdownCleanupNotice(..., deleted)` 的 deleted 恒 true。Runner.delete_agent_team 失败时：①用户收到成功文案"团队已清空并解散"；②日志打出 deleted=true；③WaitForPending... 返回 true。三点全部与 Python 失败路径相反。

**修复方案**：DeleteAgentTeam/Release 失败时 `return false, err`（stop 失败同样上抛，Python 中 stop 异常同样传播到 run_pending 的 except）。

---

## 一般问题（M 级）

### M-01（Parser 侧）JSON 对象 key 排序：Go map 按字母序，Python 保插入序

json_parser.go L77（`map[string]any`）探针实测 `{"b":1,"a":2}` → Go 输出 a 在前，Python 保 b 在前。JSON 语义等价但文本不同 → embedding 向量与 Python 管线不一致。**修复**：Decoder Token 流构造保序结构（orderedmap）+ 自定义 MarshalJSON；若团队接受"语义等价"可降级归档，但需注释声明。

### M-02（Parser 侧）编码检测器算法差异：chardet ≠ charset_normalizer（含 UTF-32 查表 nil 楔子）

txt_md_parser.go L127 `chardet.DetectBest` vs Python charset_normalizer.detect。探针对照：长中文 GBK 主路径一致 ✓；**UTF-32 系 chardet 可检出但 `htmlindex` 查 "UTF-32LE"→nil**（L138 `charset.Lookup` 失败无日志静默走原样路径 → 全乱码）；附带 `charset.Lookup("ISO-8859-1")` 实际返回 Windows-1252。**修复**：①查表失败补 Warn 日志；②UTF-32LE/BE 在 switch 手工映射 `unicode.UTF32.LittleEndian/BigEndian`；③短文本乱码差异属选型既定限制，注释归档。

### M-03（Parser 侧）errors="ignore" 语义：Go 解码器输出 U+FFFD，Python 丢弃非法字节

txt_md_parser.go L109 x/text Decoder 默认替换策略：探针实测 GBK 截断尾部 → Python 输出 `你好，世`（干净），Go 输出 `你好，世\ufffd`。损坏/截断文件的输出文本混入 U+FFFD（进 caption/chunk 文本）。**修复**：解码后 `strings.ReplaceAll(s, "\uFFFD", "")` 近似，或接受差异注释归档。

### M-04 /mode 切换取消加了 hasActiveStreamTaskForSession 守卫，Agent 侧工作不会被停止

slash_cmd.go L200-202：
```go
if oldSID != "" && mh.hasActiveStreamTaskForSession(oldSID) {   // ← Python 无此守卫
    go mh.CancelAgentWorkForSession(...)
}
```
Python L599-603 无条件 cancel（且注释明确点名"Agent 正在执行 shell/工具但网关无活跃流式拉取任务"场景）。Agent 侧工作继续跑。同文件 /new_session 分支（L124-126）无此守卫，自相矛盾。**修复**：去掉守卫，对齐 /new_session。

### M-05 /branch 的 new_sid 用 ChannelID 而非 channel_type

slash_cmd.go L325 `GenerateChannelSessionID(msg.ChannelID)`（如 `feishu_enterprise:app1`），Python L966-967 用 channel_type（如 `feishu`）。生成的分叉 session_id 形态不匹配 Python `_KNOWN_JIUWENSWARM_SESSION_PREFIXES` 12 前缀，为 ACP alias 判断埋雷。同文件 /new_session else 分支（L76）正确。**修复**：先 `mh.resolveControlChannelType(msg)` 再生成。

### M-06 /rewind 确认提示文案与 Python 完全不同（丢失不可逆警告）

Python L1039-1045 四行模板含不可逆警告、删除范围说明、bash/文件影响说明、`/rewind confirm {turn_index}` 格式；Go L393 单行简化版全部丢失。违反提示词一比一纪律。**修复**：逐字复制 Python 四行模板。

### M-07 /new_session 与 /mode 的 cancel→notice 顺序语义丢失

Python L575-591 docstring 明确"先完成取消再下发通知"（await 顺序保证）；Go L124-128 `go mh.CancelAgentWorkForSession(...)` fire-and-forget 后立即发通知。用户收到"已变更"后旧任务仍跑一小段。**修复**：同步完成后再通知，或注释声明偏差确认可接受。

### M-08 resolveControlChannelType fallback 语义偏差（provider 缺失时过度受控）

Python L339-345：provider 缺失时 channel_type=完整 channel_id（不在精确匹配集合 → 不受控）。Go channel_state.go L183-190+L261-282：`inferChannelTypeFromID` 前缀推断 → 误受控。provider 缺失的消息被误纳入 slash 拦截与状态注入。**修复**：fallback 直接 `ChannelType(msg.ChannelID)` 原样。

### M-09 Gateway Hook reason fallback 语义 + timeout 字符串静默默认

handler.go（11.13）：①Python 原值存在（含空串）即用，Go 只用非空串；②Python timeout 为字符串时 TypeError→NON_BLOCKING_ERROR，Go 静默用默认 30。**修复**：对齐 Python 取值语义。

### M-10 forwardLoopStep3UserPromptSubmit 缺 str() 转换

forward_loop.go L152-157：Python L2180-2215 `str(params.get("query") or ...)` truthy 值转字符串，Go 非 string 类型直接跳过。**修复**：补 fmt.Sprintf 兜底转换。

### M-11 fetchPersonalMemoryForPrompt 字节截断产生非法 UTF-8（CJK 截断历史第 6 次）

agent_teams/memory/manager.go L535 `truncated[:remaining]` 字节切片可切穿多字节 rune。Python `content[:remaining]` 是字符口径。注：extractor.go 内截断已用 rune 口径（L119/L144），同包两套标准。**修复**：按字节预算回退 rune 边界（truncateBytesRuneSafe）；**建议建统一 truncate 工具函数一次性收口全仓**（本审查窗口内已是第 6 次出现：M-07 /rewind preview + 本处 + 历史审查 4 次）。

### M-12 lite GetMemoryIndexManager 缓存命中路径 closed 裸读残留（450d0f98 漏修）

agentcore/memory/lite/manager_impl.go L551 `if !mgr.closed` ——Close() 持锁写（L462-468），此处 sync.Map Load 后无锁直接读，data race 可检出。450d0f98 修了两处 IsClosed() 锁读，此处残留；接口 MemoryIndexManager 缺 IsClosed() 方法。**修复**：接口加 IsClosed() 或断言具体类型走锁读。

### M-13 extractor list_memory_files 绕过 sysOp.Fs() 直读本地目录

agent_teams/memory/extractor.go L215 `os.ReadDir` 直读，read/write 工具走 Fs 抽象——非本地 Fs 后端下文件列表与可读写文件不一致。**修复**：走 `sysOp.Fs().ListFiles`。

### M-14 AddTool(WithRefresh) 注册表替换存在 Remove→Add 非原子窗口 + notice pusher 注入无并发保护

①remote_member_shutdown_cleanup.go L243-252 + resource_manager.go L407-413：refresh 路径先 Remove 后 Add 无整体锁，并发 executeTool→GetTool 命中"工具未找到"→ AbilityNotFound（Python monkey-patch 原子替换无此窗口；TeamToolRail.Init 同款既有窗口）。②L84-93 check-then-set + 全局 func 变量无锁写（第三个同型实例，前有 eventBroadcastInitialized/teamDepsInjected）。**修复**：①ResourceMgr 对 refresh 路径加整体互斥（或原子 ReplaceTool）；②注入改 sync.Once。

---

## 提示问题（T 级）

| # | 问题 | 位置 | 修复 |
|---|------|------|------|
| T-01 | HookMatcher.Matches 不 strip pattern（Python L103 有 `.strip()`） | common/hooks/config.go L70-74 | 对齐补 strip |
| T-02 | SessionMap.save 用 MarshalIndent 未关 HTML 转义（同包 interaction_context 特意关了） | session_map.go L218 | 参照 Encoder+SetEscapeHTML(false) |
| T-03 | SessionMap.load 非 dict JSON Go 记 Warn，Python 静默跳过 | session_map.go L199-202 | Unmarshal 到 any 断言 map |
| T-04 | LoadSessionMapScope 非字符串配置值静默用默认（Python str() 后走未知值 Warn） | session_map.go L74 | fmt.Sprintf 后进同一 switch |
| T-05 | InteractionContext now 用 Unix() 整秒 vs Python time.time() float 秒 | interaction_context.go L261/282 | 亚秒级 TTL 边界差异，可忽略 |
| T-06 | ApplyChannelState params 非对象时 Go 覆盖 Params 为 {"mode":...}，Python 原样保留 | channel_state.go L120-129 | Unmarshal 失败跳过注入 |
| T-07 | handleChannelControl 加了 ReqMethod==ChatSend 前置过滤（Python 无） | slash_cmd.go L39 | 保守收紧，注释声明即可 |
| T-08 | GetOrCreateChannelState 上提到 switch 前（Python SKILLS_OK 分支在取 state 前 return） | slash_cmd.go L64 | 影响微小，下移或声明 |
| T-09 | skills req_id 无随机后缀（Python 有 token_hex(3)） | slash_cmd.go L246 | 接 generateRandomHex(3) |
| T-10 | /branch fork_title 空串时 Go 用 branchName，Python 键存在即用 | slash_cmd.go L364-371 | 边界一致化 |
| T-11 | _KNOWN_JIUWENSWARM_SESSION_PREFIXES 等 12 前缀判断 Go 无对应（ACP 回填时别漏） | dispatch.go:30 TODO(#ACP) | 回填清单登记 |
| T-12 | /rewind preview 字节截断 CJK 乱码（Python rune 截断） | slash_cmd.go L450-455 | `[]rune(p)[:50]` |
| T-13 | /rewind confirm 指令格式偏差（Python `/rewind confirm {turn}`） | slash_cmd.go L393 | 随 M-06 一并逐字对齐 |
| T-14 | skills_slash 通知 payload 键 Go 用 content/removed/remaining（Python removed_count/remaining_count） | slash_cmd.go L278-282 | 对照 Python L895-940 键名 |
| T-15 | get_checkpoint_dir legacy 迁移无（Go 全新部署无 legacy） | paths.go L273 | 知悉即可 |
| T-16 | U+2028/U+2029 转义差异（JSON 层等价文本不同） | json_parser.go | 可不修 |
| T-17 | NaN/超界浮点：Go Unmarshal 报错走原文分支（更贴合日志意图） | json_parser.go L77-83 | 注释归档 |
| T-18 | parser 测试残留未跟踪 images/ 目录 | parser/ | t.TempDir 隔离 + .gitignore |
| T-19 | TeamHarness.RunStreaming 空壳但计划 9.57 标 ✅（流程矛盾） | harness.go L352-359 | 改 🔄/⤵️ 或实现分支1 |
| T-20 | BuildSpawnConfig 不填 RunnerConfig/LoggingConfig/SessionID；per-member 日志目录隔离缺失 | payload.go L121-143 | 登记回填 |
| T-21 | shutdown 跳过日志缺 channel 字段；InnerAgent() 生产调用违反文档契约；超时分支 shim 复刻零覆盖；ctx 取消时清理调度被跳过 | remote_member_shutdown_cleanup.go L212/L167/L120 | 随手修 |
| T-22 | OnTaskListDrained/OnTeamCompleted 的 `.(int)` 断言（inprocess 直传 int 当前安全，跨进程 JSON 后 float64 失效——与 S-03 同源的系统性风险） | team_completion.go L189/L218 | 统一 toInt 宽容转换 |

---

## 占位符核对（审查要点 3-2）

| 占位标记 | 状态 | 说明 |
|---------|------|------|
| forward_loop.go L113 TODO(#11.12) Inbound Pipeline | 真未实现 | 标记诚实 |
| team_manager_distributed.go L82-89 四个 ⤵️(#10.6.21) hooks | 真未实现 | 标记诚实；Go 生产链路走 runtime_ready 事件 attach（deep_adapter_team.go:862），CreateTeam 路径当前无生产调用方，无功能缺口 |
| TeamHarness.RunStreaming（harness.go L352-359） | 空壳 | **但 IMPLEMENTATION_PLAN 9.57 标 ✅——状态失真**，见 T-19 |
| gateway slash_cmd 的 rewind_session 本地 fallback | 无对应 | Python 有而 Go 整层缺失，见 S-03 第 2 层 |

## 批次1+2 修复复核结论（26125540+581e6b67+77b4948e）

S-20/S-21/S-18/S-24/S-26/S-27/S-11/S-12/M-17/M-18/T-18 及附录4 handlers（OnTaskPlanDecision/nudgeIdleAgent/OnPollTask/processUnreadMessagesWithSteer）全部逐行对齐验证通过；双参数契约（SpawnConfig payload vs SpawnPayload inputs）分离正确；shutdown cleanup 538 行主链路（attach→包装→invoke→全员判定→调度→延迟→删除→通知→等待）步骤完整。残留缺口即上文 S-01/S-05/M-11/M-12/M-13/M-14。并发修复批次（864eed39 SkillManager 锁/450d0f98 watcher atomic/173c5152 排空 goroutine）验证正确（450d0f98 漏修一处即 M-12）。6.8a ModelBackupRail（90d98feb+440cc8ce）无问题，免复查。

## 修复优先级建议

1. **S-01**（团队流内容链路核心缺口，9.57 回填前修避免叠加回归）
2. **S-02/S-05/S-06**（一行级修复：mode 映射/header/错误上抛）
3. **S-03+S-04**（同源系统性风险：map 数字断言 + float64 往返，建议一并建 toInt helper 与 UseNumber 模式收口）
4. **M-11**（CJK 截断第 6 次，建统一 truncate 工具收口）
5. 其余 M/T 按列表顺序
6. **T-19 需用户定夺**：RunStreaming 空壳 vs 9.57 标 ✅ 的流程矛盾——改计划状态 or 直接实现分支1（runner 已有 RunAgentStreaming，成本低可立即打通成员流式链路）
