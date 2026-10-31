# 2026-10-31 48 小时逻辑审查

审查窗口：2026-10-29 07:26 ~ 2026-10-31 01:27（HEAD `7ce87f38`）
范围：161 个提交，216 个文件，+47260 / −4712 行
基线 CI：`41098993522`（HEAD）四 job 全绿；期间 `41097454959`（docs-only）Lint 失败（见 T-12）

审查依据：`IMPLEMENTATION_PLAN.md` 与 `docs/specs|plans/` 下各节设计，逐条对照 Python 参考实现
（`/home/opensource/agent-core` = openjiuwen；`/home/opensource/jiuwenswarm-develop` = jiuwenswarm）。

本轮覆盖的提交簇：

| 簇 | 章节 | 主要新代码 |
|----|------|-----------|
| ACP 网关桥接 | 11.25 | `acp_bridge.go`(899)、`protocol/acp/acp_channel.go`(1768)、`acp_alias.go`、`wswrite/guard.go`、`gateway_server.go` |
| 知识库 | 13.26/13.27/13.28 | `knowledgebase/{base,simple_kb,graph_kb,multi_kb,validate,kb_helpers}.go`、`utils/config_manager.go`、vector_store/indexer `Close()` 改造 |
| 检索器 | 13.25 | `retriever/agentic_retriever.go`(807)、`query_rewriter/*` |
| 协议适配 | 10.2.9/10.2.10 | `e2a/adapters.go`(424)、`e2a/acp/session_updates.go` |
| 适配器/CLI | 10.4.6 + project memory | `server/adapter/{code,deep}_adapter.go`、`deep_adapter_slash.go`、`cmd/uapclaw/acp_chat_cmd.go`、`dotenv` |
| harness/团队 | 修复类 | `deep_agent.go`、`sys_operation/local/*`、`memory/lite/*`、`agent_teams/runtime/team_runner.go` |

统计：**严重 5 项 / 一般 14 项 / 提示 15 项**。

---

## 一、严重（5 项）

### S-01 code 模式 rail 生命周期门控永不命中，且缺少「保持 code rails」的补充注册

- Go：`internal/swarm/server/adapter/code_adapter.go:544-549`（门控 `if mode != "code"`）、`:651`（调用点传 `config.Mode`）、`deep_adapter.go:848`（`mode := paramsString(params, "mode", "agent.plan")` 原样透传）、`deep_adapter_rails.go:1013-1048`（默认分支卸载 TaskPlanning/SkillEvolution/**SubagentRail**/SkillCreate）
- Python：`jiuwenswarm/server/runtime/agent_adapter/interface_code.py:770-825`（`_update_rails_for_mode` 完全覆写，**不判 mode**，且对缺失的 SubagentRail/ProjectMemoryRail/CodingMemoryRail 做 (re)register）

现象：请求参数 `mode` 为 `"code.plan"` / `"code.normal"` / `"code.team"`（`handle_envelope.go:604-652` 拆成 mode=`code` 只发生在 `CreateInstance` 路径；`processMessageImpl/Stream` 里 `runtimeConfig.Mode` 是**原始串**）。因此 CodeAdapter 的覆写分支在真实会话中恒不命中，每条消息都回落到 `DeepAdapter.updateAgentModeRails("code.plan")`：

1. SubagentRail 被 `UnregisterRail` 且置 nil（`:1041-1048`），后续**无任何重建**（`:1085-1087` 函数直接结束）→ code 模式 `task_tool` 派发 explore/plan 子代理失效；
2. `handleMemoryRailByConfig(ctx, "fast")` 把记忆 rail 按 agent.fast 语义处理，Python code 模式要求始终保留 ProjectMemoryRail/CodingMemoryRail；
3. 完全缺失 Python 的「缺失则补注册」分支。

影响：code 是本项目主模式，首次请求后即丢失子代理能力与项目记忆 rail，属功能性回归。现有测试（`code_adapter_test.go:73`）只用 `CreateInstance(ctx,nil,"code.plan","plan")` 断言 `d.mode`，未覆盖 `updateRailsForMode` 的真实入参形态，故未暴露。

建议：门控改为 `strings.HasPrefix(mode, "code")`（或按 `resolveMode` 归一化后再传），并补齐三条 rail 的 (re)register 分支；补一条用 `runtimeConfig{Mode:"code.plan"}` 驱动 `updateRuntimeConfig` 后断言 SubagentRail 仍在册的用例。

### S-02 沙箱根校验用字符串前缀匹配且不解析软链，可越界

- Go：`internal/agentcore/sys_operation/local/fs_operation.go:1029-1044`（`resolved, _ := filepath.Abs(path)` + `strings.HasPrefix(resolved, rootAbs)`），根目录同样只做 `filepath.Abs`
- Python：`openjiuwen/core/sys_operation/local/fs_operation.py:1122`（`Path(os.path.normpath(raw)).resolve(strict=False)`）、`:1135`（`any(self._is_within(normalized, root))`）、`:1150-1157`（`path.relative_to(root)`）

现象：
1. 根 `/ws`、目标 `/ws-evil/x` 通过 `HasPrefix` 判定为「在沙箱内」——路径边界未按分隔符对齐；
2. `filepath.Abs` 不做 `EvalSymlinks`，工作区内指向工作区外的软链（如 `ws/link -> /etc`）绕过校验；
3. 未展开 `~`（Python 侧 `Path` 相对 base 拼接前已处理），且 `SandboxRoot` 未 resolve。

影响：安全边界失效，可读/写沙箱外文件。该段正是本轮 `66687933`/`df4087a3` 为修「沙箱根从 ctx 读取」而重写的区域，ctx 读取部分已正确对齐，边界判定未同步收紧。

建议：改为 `filepath.Rel(root, resolved)` 判 `!strings.HasPrefix(rel, "..")`，两侧统一 `EvalSymlinks`（失败降级 `Clean`），并按 `os.PathSeparator` 比较。

### S-03 AcpChannel 派发失败被静默吞掉，session/prompt 永久挂起

- Go：`internal/swarm/gateway/channel_manager/protocol/acp/acp_channel.go:891-899`（`_ = ch.sendToGateway(ctx, msg)`），调用点 `:457-508`、`:512-529`
- Python：`jiuwenswarm/gateway/channel_manager/protocol/acp/acp_connect.py:1191-1205`（`await self._send_to_gateway(msg)` 不捕获），由 `handle_jsonrpc_request` 的 `except Exception` 回 `-32603`（同文件 `:320-331`）

现象：网关不可达 / 序列化失败时，Go 侧既不回 JSON-RPC error 也不清理 `requestCtx[requestID]`；IDE 侧 `session/prompt` 无响应直到空闲 finalize（3s）或永不完成，`pendingRPCCountBySession` 计数泄漏。

建议：`dispatchMessage` 返回 error，`session/prompt` 分支捕获后 `sendRawJsonrpcError(-32603)` 并清理 ctx（与 `acp_bridge.dispatchMessage` 的告警语义统一，二者当前也不一致）。

### S-04 `/evolve_rollback` 缺 4 道校验，无版本参数直接回滚最近版本

- Go：`internal/swarm/server/adapter/deep_adapter_slash.go:494-527`
- Python：`jiuwenswarm/server/runtime/agent_adapter/interface_deep.py:4162-4225`

缺失项（逐条对照）：

| Python 校验 | Go |
|---|---|
| `if not skill_name` → 列出可回滚 Skill 与版本数 | 仅 `missing skill_name` |
| `store.skill_exists()` → 「未找到 Skill + 可用列表」 | 无 |
| `list_archives()` 为空 → 「没有归档版本可回滚」 | 无 |
| 无 version → **列出归档版本清单让用户选** | **直接 `RollbackSkill(skill, nil)`，即回滚最新版** |
| `latest` 简写展开、`version not in body_versions` 校验 | 无 |

影响：用户执行 `/evolve_rollback <skill>`（本意是查看可回滚版本）会**未经确认就回滚**技能主体，属不可逆的破坏性行为；且返回 `{"status": "...", "skill_name": ...}` 与 Python 的 `{"output", "result_type"}` 契约不符，前端/通道无法展示。

建议：按 Python 五段校验逐条补齐，返回 `output/result_type` 形状；`skill_name` 缺失时的「可回滚清单」也需实现（依赖 `store.list_archives`，已具备）。

### S-05 `maybeFinalizeDeferredPrompts` 锁外读 ctx 可变字段，且未校验连接已关闭

- Go：`internal/swarm/gateway/acp_bridge.go:750-789`。`757-763` 持锁收集后，`765-788` 在**锁外**读 `c.PendingStopReason`/`c.State`（经 `shouldWaitForFinalTextBeforeEndTurn(c)` 读 `SawChatFinal`/`AssistantText`/`AssistantMessageID`），而这些字段在 `:464`、`:524`、`:488`、`:772` 均由其他 goroutine 持锁写；`775` 仅判 `ws == nil`
- Python：`acp_connect.py:540-575`（单线程事件循环，无竞态；`if ws is None or bool(getattr(ws,"closed",False)): continue`）

影响：
1. `-race` 下为真实数据竞争（`SendMessage` 与 idle timer 回调、`resolvePendingRPC` 三方并发），可能读到半写入状态导致重复 `end_turn` 或漏发；
2. 连接已关闭时仍向该 ws 写帧（Go 无 `ws.closed` 等价判定），产生写错误日志噪音。

建议：`shouldWaitForFinalTextBeforeEndTurn` 的调用移入持锁段（或用快照），并对 ws 增加关闭标记检查。

---

## 二、一般（14 项）

### M-01 JSON-RPC id 仅接受 string，数字 id 的响应被当作 unknown 丢弃
`internal/swarm/gateway/route_config.go:166-169`（`strVal` 只做 `v.(string)`）→ `acp_bridge.go:305/369`、`acp_channel.go:534/1534`。
Python 一律 `str(data.get("id") or "").strip()`（`acp_connect.py:152-163`、`:212`、`:810`、`:1665`）。
影响：IDE 侧用整数 id 回应工具请求时，`pendingRPCSessionByID` 查不到 → 回 `ignored: unknown_or_late_response`，工具结果丢失且 pending 计数不减。

### M-02 `chat.final` 未见过 idle 的分支缺少取消 idle timer
`acp_bridge.go:496-498` 只 `return true`；Python `acp_connect.py:409-412` 在该分支**无条件** `_cancel_idle_finalize_task(ctx)`。
影响：若此前已 arm 过 timer（如 `registerPendingRPC` 之前调度、或 `maybeFinalize` 的 `shouldWait` 分支），final 到达后 timer 仍会触发第二次 `end_turn`。

### M-03 ACP 会话别名在全局锁内做无超时网络 RPC
`internal/swarm/gateway/message_handler/acp_alias.go:91-111`：持 `acpSessionAliasMu`（写锁）执行 `session.create`，且用 `context.Background()`（不可取消、无超时）。
影响：agent server 卡住时，所有 ACP 会话的入站派发与出站查询（`:129-135` 只读也取写锁）全部阻塞。Python 侧同样在 lock 内 await，但其发送链路自带超时且协程模型不阻塞事件循环，Go 需显式超时。建议：锁内只做双检 + 占位，RPC 移到锁外（singleflight 语义），读写分离用 RWMutex。

### M-04 AcpChannel 的 WS 客户端无 ping/读超时
`internal/swarm/gateway/channel_manager/protocol/acp/acp_channel.go:1379-1393` 直接 `Dialer.Dial`；Python `acp_connect.py:1538-1543` 传 `ping_interval`/`ping_timeout`（≈20s）。
影响：半开连接永不发现，无重连触发，会话静默失效。

### M-05 `Stop` 先等 reader 退出再 Close，形成 1s 空等
`acp_channel.go:1416-1436`；Python `acp_connect.py:1573-1589` 先 cancel reader task。
影响：每次关闭固定多耗 1s，且若 reader 卡在读循环，超时路径才生效。

### M-06 E2A 两个适配器漏设 `protocol_version`，空 map 序列化为 null
`internal/swarm/e2a/adapters.go:114-127`（`EnvelopeFromACPJsonrpc`）、`:166-181`（`EnvelopeFromA2ASendMessage`）均未设 `ProtocolVersion`；同包其余构造器（`gateway_normalize.go:92/333/...`、`wire_codec.go:242/312/355`）全部显式设 `E2AProtocolVersion`。Python `jiuwenswarm/common/e2a/models.py:108` 默认 `"1.0"`。
影响：信封序列化出 `"protocol_version": ""`；`channel_context`/`a2a_metadata`/`acp_meta` 为 nil 时输出 `null` 而非 `{}`，下游 `range`/判空语义不同。

### M-07 `ChromaVectorStore.Close` 语义与 Python 相反
`internal/agentcore/retrieval/vector_store/chroma.go:737-741` 真关 client；Python `openjiuwen/core/retrieval/vector_store/chroma_store.py:528-537` 是空操作（显式注释「ChromaDB client usually doesn't need explicit closing」）。
影响：`KB.Close()` 后 Python 仍可检索、Go 直接失败；同时注释声称「Warn 吞由调用方 maybeClose 承担」，而 `maybeClose` 恒收到 `nil`，该注释不成立。

### M-08 KB `Close` 非幂等、不置空；setter 无锁
`internal/agentcore/retrieval/knowledgebase/base.go:317-338`（二次 Close 会对同一对象重复 Close，`vectorStore/indexManager/retriever/graphRetriever` 不置 nil，关闭后 Retrieve 复用已关对象）；`:188-203`（`SetVectorStore/SetIndexManager` 直接写字段，与并发 `Retrieve` 读竞争）。
另 `maybeClose` 只认 `interface{ Close() error }`（`:329`），自定义 Indexer 若实现 `Close()` 无返回值将被静默跳过——与 13.28 Task 0 「行为零变化」的提交说明不符。

### M-09 `indexFile` 未校验 `ReadResult.Code`/`Data`
`internal/agentcore/memory/lite/manager_impl.go:888-899`：只判 `err`，`readResult.Data == nil` 时 content 保持 ""，随后 `ChunkMarkdown("")` 得到空 chunks 并**在事务内提交**，等价于清空该文件索引；`df4087a3` 声称「收口结果码检查」但只改了 `ReadFile` 主路径。
建议：显式判 `Code != 0` 与 `Data == nil` 后 rollback。

### M-10 `user_todos` 读写无锁，且读失败当空列表 → 假成功/覆盖写
`internal/swarm/agents/harness/common/tools/user_todo_tool.go:159-163`（`os.ReadFile` 失败一律返回 nil，注释「读失败同样容错为空」）；`:312-320`（`writeTodosFile` 用 `_ = os.WriteFile` 吞错、无返回值）；`:418-566`（read-modify-write 全程无文件锁）。
影响：并发两个 update 丢更新；读瞬时失败时 create 会以单条列表覆盖整个待办文件；写盘失败仍向模型报 success（与本轮「记忆工具拒绝假成功」的修复方向不一致）。

### M-11 team 清理路径用可取消 ctx 且吞错
`internal/agent_teams/runtime/team_runner.go:132-137`、`:260-266`：`defer func(){ ...; _ = teamSession.PostRun(ctx) }()`。
影响：客户端断连/abort 后 ctx 已取消，`PostRun`/`FinalizeMember` 立即失败且错误被丢弃，会话终态不落盘。建议 defer 内使用 `context.WithoutCancel(ctx)` + 超时（Go 1.21+ 语义），并记录错误。

### M-12 同一文件内 project memory 环境变量前缀不一致
`internal/swarm/server/adapter/code_adapter.go:1213` 用 `UAPSWARM_PROJECT_MEMORY_DIR`，同文件 `:1295` 用 `UAPCLAWSWARM_ADDITIONAL_DIRECTORIES`；全仓统计 `UAPCLAW_*` 出现 174 次、`UAPCLAWSWARM_*` 4 次、`UAPSWARM_*` **仅此 1 次**。
影响：按命名规律配置的用户永远读不到 project_memory_dir，需查源码才能发现。

### M-13 Real LLM CI 回显密钥前缀
`.github/workflows/real-llm-ci.yml:66`：`echo "密钥前缀: ${LLM_API_KEY:0:8}..."`。GitHub 只遮蔽完整 secret，前缀属**部分泄露**（写入公开日志）。
建议：只输出长度或 `hash`，或 `::add-mask::` 后不打印。

### M-14 `/evolve_simplify`、`/evolve_rebuild` 返回形状与 Python 契约不符
`internal/swarm/server/adapter/deep_adapter_slash.go:399-489`：缺 `store.skill_exists` 与空名检查、异常一律 `{"status":"failed"}`，而 Python `interface_deep.py:4082-4155` 返回 `{"output": …, "result_type": "error"/"answer"/"followup"}`；rebuild 成功应为 `result_type="followup"`。两个 handler 的返回值被 `dispatchSlashCommand` 原样下发（`:45-55`），调用方无兜底转换。

---

## 三、提示（14 项）

**T-01 已完成的章节仍留着「⤵️ 待回填」注释**
`gateway_server.go:28`（「⤵️ 11.25：AcpGatewayBridge 注入点」——实际 `:197-211` 已装配）、`:53`、`:312`、`:537`；`app_gateway.go:406/458`；`route_config.go:94`；`code_adapter.go:598`（声称 `_update_tools_for_mode`/`_update_session_tools` 待回填，实际 `:665-671` 已调用）；`acp_channel.go:954`。
`team_manager_lifecycle.go:67/227/452/670/693` 的 10.6.22 待回填仍有效（未在本轮完成），无需处理。

**T-02 死代码/冗余字段**
`route_config.go` 的 `BuildACPRouteBinding` 生产无调用；`AcpGatewayRequestCtx.matchedRequestID`（`acp_bridge.go:76`）为遍历辅助字段，可用局部变量替代。

**T-03 bridge 与 channel 两套 pending/idle/timer 实现重复约 200 行**
`acp_bridge.go` 与 `acp_channel.go` 各自实现 `decreasePendingRPCCount/maybeFinalize/scheduleIdleFinalize/idleFinalizeAfterTimeout`。Python 确实也是两个类，但本轮已因 §9-4 竞态在两个文件各修一次（`e7bcb6c0`、`609b290b`），后续易再次分叉；建议抽公共 `acpPendingTracker`。

**T-04 `Close() error` 改造后错误被 `_ =` 吞**
`graph_kb.go:329`、`retriever/vector_retriever.go:168`、`sparse_retriever.go:117`、`hybrid_retriever.go:267`、`vector_store/chroma.go:739`；且四个 retriever 实现恒 `return nil`，`GraphRetriever.Close` 的 `firstErr` 实际不可达。

**T-05 KB 配置零值不做校验**
`internal/agentcore/retrieval/utils/config_manager.go:214-239` 直接取值建结构；Python `utils/config_manager.py:63` 走 pydantic `KnowledgeBaseConfig(**data)` 会抛 ValidationError。缺 `kb_id`/非法 `index_type` 时 Go 静默采用零值（collection 名退化为 `kb__chunks`，mode 推导恒 hybrid）。

**T-06 ConfigManager 无锁 + 落盘字节不齐**
`:28-31`（并发 `Load/Update/Save` 会 `concurrent map write` panic）；`:136-142`（map 键字典序 + Encoder 尾换行，与 Python `json.dump` 的定义序、无尾换行不同；注释「对齐 json.dump」不成立）。

**T-07 `LoadNonOverride` 空值语义与 python-dotenv 不同**
`internal/common/dotenv/dotenv.go:99-110`：已存在但为空字符串时仍写入。已在注释标注为有意偏差，仅提示：与 `override=False` 的「按键存在性跳过」严格语义不同，跨语言对拍时注意。

**T-08 注释与实现不同步（回填后未清理）**
`code_adapter.go:598` 的 ⤵️ 条目已全部落地（`:665-671`、`:671` refreshAcpRuntimeTools），注释仍在，易误导后续审查（参见 T-14 同类问题）。

**T-09 acp-chat 未知 flag 退出码与 argparse 不一致**
Python `acp/cli.py:40-46` 未知识别返回 2；Go 走 cobra → `exitCodeFor` → 1（`cmd/uapclaw/main.go:30-34`）。另 `client_cache.go:92-116` 对非 list `args`/非 dict `env` 报配置错，Python 侧是静默放宽。

**T-10 存量锁序（ABBA）与 spanState 落字段遗漏**
`deep_agent.go:219→230`（configMu → initMu）与 `:1833→1835`（initMu → configMu.RLock）为既有 ABBA 风险，本轮 `74c5ceb9` 未引入但仍在热路径附近。
同提交 `:1853-1858`：`ctx` 已带 SpanState 时不写入 `d.spanState`，后续调用注入不到，tracing 断链。

**T-11 AgenticRetriever 两处小偏差**
`agentic_retriever.go:450-451`（`metadata["doc_id"].(string)` 非 string 时静默置空，Python `metadata.get` 透传）；`:405-425`（每个 triple 一个 goroutine，无上限/无 errgroup 取消，triple 数由 LLM 输出决定）。

**T-12 Lint 门禁在 docs-only 提交上失败（不稳定）**
`41097454959`（仅文档改动）Build/Unit/Integration 全绿、Lint 失败，HEAD 复跑绿。属既有 SA5011/缓存类不稳定，建议记录以免误判为代码回归。

**T-13 `pyMetaFloat` 只认 `float64`**
`knowledgebase/multi_kb.go:259-274`：元数据 `raw_score` 为 int（外部/自定义 store）时返回 155509 错误中断整条多库聚合；Python `simple_knowledge_base.py:344-363` 的 `max()` 接受 int/bool。

**T-14 项目内「待实现依赖」台账未回填**
`docs/review/2026-09-21-pending-implementation.md` 仍把 `AH-M-02`（`_update_tools_for_mode`/`_update_session_tools`）、`AH-M-13`（`_refresh_acp_runtime_tools`）列为「依赖未排期」，实际本轮已实现并在 `code_adapter.go:665-671` 调用。建议按规则 2 回填，避免后续审查重复判定。

**T-15 工作区存在未跟踪的临时诊断测试文件**
`tests/integration/agentcore/harness/taskloop_stream_diag_test.go`（未跟踪，10-30 17:05）自述为「临时诊断测试：复现 Real LLM CI 40987848552 失败」。该类文件属排查脚手架，提交前应删除或移入 `.gitignore`，否则会进入集成审计的「有效测试密度」统计并带来 flaky 风险。

---

## 四、复核通过的部分（避免只报问题）

以下为本轮逐条对照后确认**已对齐**的关键点：

- `pyjson.go` `MarshalNoEscape`/`MarshalIndentNoEscape`：`SetEscapeHTML(false)` + 去尾换行，indent 模式的分隔符与 Python 一致（紧凑模式 `,` vs `, ` 差异已在注释声明）。
- `code_adapter.projectMemoryAdditionalDirectories`：三级回退（instance_overrides → config_cache.project_memory → env）与 Python `interface_code.py:556-558` 的 `raw is None` 语义一致，修正了此前「env 无条件追加」的偏差。
- explore/plan 迭代上限：`parseIntAny` 两段回退与 Python `parse_int(cfg.get(...), react_cfg.get("max_iterations", 15))` 一致，plan 默认 25→15 的订正正确。
- KnowledgeBase 三节（13.26/13.27/13.28）：5 个抽象方法、4 个模板方法、2 个聚合函数、graph 包装齐全；`multi_kb` 的去重/稳定排序/limit 真值链/负数切片与 Python 一致；`index_type` 仅在建时注入；懒创建读路径持锁，无重复建图。
- `wswrite.Guard`：per-conn 串行化写锁 + 未注册退化，`gateway_server` 的 Register/Unregister 配对（`:445/:465`、`:246`）完整。
- `dotenv.LoadNonOverride`、`real-llm-ci.yml` 的 fail-not-skip 门卫设计、`acp-chat` 退出码 typed error 分层，均按设计文档落地。

---

## 五、审查方法与覆盖度

- 事实调查分 6 簇并行（ACP/知识库/检索器+E2A/适配器+CLI/harness+团队/文档），每条结论均由人工 `Read` 完整函数体复核后再入库，未采信片段拼接。
- 严重项 5 条全部做过 Python 源码逐行对照（含行号）。
- 未覆盖：`tui_cli_route_test.go`/`e2e_acp_bridge_test.go` 等新增集成测试的断言有效性（仅抽查）；`internal/swarm/gateway/tui_route.go`(310) 与 `acp_gateway_24_test.go`(1385) 只做了结构级浏览，未逐行对拍，如需可另开一轮。
- 本文件仅记录问题，未修改任何代码。

## 复核补充（实施后自审，D1–D6）

修复落地后再对照 Python 复核实现本身，发现并处置以下 6 项偏差：

| 编号 | 问题 | 处置 |
|------|------|------|
| D1 | `/evolve_simplify` 误用 team 侧 helper `approvalResultFromEventOrItems`：文案变成「演进请求已生成，请在审批弹框中确认」，并多出 `question_count`（Python `interface_deep` 走 `_format_approval_summary`，无该字段） | 新增 deep 侧 `approvalSummaryOutput` / `approvalResultFromApprovalChunk`，文案改为「已为 Skill 'x' 生成 N 条待审批内容：」；N 用 questions 总数、明细只列 dict 项（照抄 Python 口径） |
| D1b | `/evolve` 明细行用 `text[:200]` 按字节截断，中文会截出半个 rune | 抽 `utils.TruncateRunes`（字符级），`/evolve` 与 simplify 共用 |
| D2 | rollback 空名清单用 `count > 0` 过滤，漏掉「有归档但 0 个 SKILL.v*」的 skill | 改回 Python 口径：仅判 `len(archives) > 0` |
| D3 | `config_manager` 手写 `case hybrid, bm25, vector` 与 `common.validIndexTypeKinds` 双份清单 | 导出 `common.IsValidIndexType`，单一真源 + 保留 155511 错误码 |
| D4 | S-01 重建块的 `.(*subagent.SubagentRail)` 断言失败会静默跳过注册 | `buildSubagentRail`/`buildProjectMemoryRail` 改返回具体类型，消除全部 4 处断言（接口隐式满足，调用点无需改） |
| D5 | 新增的 `containsString` 重复造轮子 | 改用 `slices.Contains` |

另：截断助手统一收敛到 `utils`，替换 4 处私有实现（agent_teams/team_agent、task_loop/handler、viking_provider、evolving/store_projection——最后一处带省略号语义，落到 `utils.TruncateRunesEllipsis`），删除对应 8 个本地测试（覆盖迁到 `utils/truncate_test.go`）。

保留的两处计划外行为（复核确认合理，已加注释）：`ExpandHome` 不再把 `~otheruser` 拼成 `home/otheruser`（对齐 Python 原样返回）；gateway ping 写失败主动 `conn.Close()` 触发重连。
