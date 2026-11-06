# 2026-11-06 48 小时上库质量审查

> 审查范围：`d4ee946f .. HEAD(a3a8a21e)`（2026-11-05 02:06 ~ 2026-11-06 02:31，82 个提交；上轮审查截止到 `d4ee946f`，本轮的增量即这 82 个提交）
> 代码规模：157 个 Go 文件 / +17,167 / −7,126（其余为 docs 计划与豁免登记）
> 审查对象：提交原因与实现正确性；对齐 Python（`jiuwenswarm` 0.2.5 产品层 / `openjiuwen` agent-core 库层）；桩与回填完整性；层级与步骤归属；稳定性、性能、可维护性、并发安全
> 审查方式：逐批读生产代码 + 对照 Python 同位置源码；关键结论均在本地读码复核，下文 `文件:行号` 以 HEAD `a3a8a21e` 为准
> **只读审查，未修改任何代码**

---

## 零、结论摘要

| 级别 | 数量 | 一句话 |
|---|---|---|
| 严重 | 8 | 9.11a 对齐了上游产品会删掉的那一层且开关无赋值点（整章实际不可用）；`session.delete` 被误删导致前端删会话直接报「未知方法」；`chat.swarmflow_reply` 只回 ack 不转发（假成功）；技能工具热刷新后永远执行旧实例；技能 rail 全字段无锁（并发写 map 会崩进程）；取消后「清洗并持久化」用已取消的 ctx 必然写库失败；通道配置保存后不重启实例；权限 RPC 的 request_id 恒空导致并发撞车 |
| 一般 | 16 | 占位 handler 一律回「成功」、install_status 字段名不符致前端误判安装中、微信解绑是空壳、微信配置无边界校验、配置文件并发写无串行化、契约矩阵「漂移 0」不可信且已退出 CI、覆盖率豁免吃掉新逻辑、禁用名单用错 API、软链技能目录被跳过、媒体识别正则漏判、目录指纹只存不用、取消后上下文清理不落库的连带问题等 |
| 提示 | 12 | 为一行注释整文件豁免覆盖率、为绕覆盖率门禁改代码结构、list_skill 少渲染、死结构体、陈旧注释、ToolExposure 显式声明被静默覆盖、日志文案与上游不一致等 |

**先说清楚这批里做得好的部分**（避免只看问题）：

1. **Web WS 层的结构归位是这轮质量最高的部分**。`RPCDispatcher`、`frame` DTO、三个 handler 文件这些上游没有的自造抽象，都在同一次改动里删干净了（符合规则 8）；`app_web_handlers.go` 4426 行 / Python 6580 行，文件级 1:1 映射对得上。分发顺序（local_only → handler-before-callback → 转发 → 本地 handler）与上游 `web_connect.py:2180-2231` 逐句可对照，且用的是精确键查找，**没有**掉进「用 map 遍历决定匹配顺序」这个坑。9 个 IM 平台集合与 18 条 `channel.*.conf` 注册和上游一一对应。
2. `9f70b00a`/`f1006578` 让子进程输出交给 `os/exec` 托管，Windows 丢字节这个连续多轮的老问题方向是对的。
3. `3cad73cd`/`6db8c478`/`075ffe4a`/`86ce459e` 这几条 ReAct 结果判定的修复，与上游分支结构基本对得上（错误值两层拆分、输出上限无答案判失败、空回答重试一次、迭代耗尽字段）。
4. `df01d112`/`1788d5fb`/`467c7cea` 的 ToolExposure 枚举 + 注册期策略 + 注册版本号，默认值（未声明 → direct）与上游一致，版本号自增有锁且额外用 fingerprint 兜底，索引不会漏重建。
5. 技能会话基线的**语义**（`skill_use` state、schema v1、基线∪新增的合并视图、`skills.runtime_changes` 附件、priority 95 且仅在对方已注册时才让位）与上游逐项对得上，typed-nil 的三次收敛也确实把 rail 侧四个入口收拢到了一处。

**这批最需要警惕的模式**：`e0ed3e69` 这种「修 A 却顺手删掉 A 确实需要的防护」最危险——它删掉的 ResourceMgr 预删除，是本批后续所有「刷新技能」「清基线」功能能生效的前提；删了之后三条链路全部失效，而单测因为用的是假的 ResourceMgr 全绿。与之同源的是 `3800239a`（改无参方法顺手删锁）和 `0912a250`（为绕覆盖率门禁把判空跨包下沉）。三条都属于「为了达成某个局部目标，把不相关但必要的约束一起去掉」。

---

## 一、严重（8 项）

### S-01 9.11a 对齐的是上游产品层「init 前就会删掉」的那一层，且 Go 侧没有任何地方能把它打开

- **归属流程**：领域九 9.11a 延迟工具目录（ProgressiveToolRail 全部落地代码）
- **相关提交**：`a1a66686`（重写 ProgressiveToolRail）、`2dfc1cb2`/`4020cf09`（BM25）、`e012dbef`/`15cfb1df`（tool_search/tool_call）、`4598234a`/`870c2c0b`（提示词）、`1788d5fb`/`467c7cea`/`cf1c8bc3`（曝光与回调）

**问题一：上游产品层不用的那套，Go 却只实现了这套。**

`jiuwenswarm/server/runtime/agent_adapter/interface_deep.py:7289-7330`：

```python
def _drop_pending_progressive_tool_rail(self) -> None:
    """Remove openjiuwen's bundled ProgressiveToolRail before rail init.
    ...
    We swap that rail for our skill_index-framework ``ToolRetrievalPromptRail``;
    dropping it from ``_pending_rails`` before ``ensure_initialized`` means its
    ``init`` never runs, so no meta tools are registered and no ``uninit``
    cleanup is needed.
    """
```

也就是说：产品运行时用的是 `jiuwenswarm/.../rails/tool_retrieval_prompt_rail.py`（priority 80，1086 行）+ `.../tools/tool_retrieval_toolkits.py`（1030 行），`openjiuwen` 的 `ProgressiveToolRail`（1448 行）被显式摘掉、`init` 不执行。Go 侧只有后者：全仓 `grep ToolRetrievalPromptRail / tool_retrieval / Deferred 工具（渐进式披露）` → **0 命中**。

**问题二：`tool_search` 的对外契约与产品版完全不同。**

- Go（`internal/agentcore/harness/prompts/tools/tool_search.go:68-93`）：`{"query": string, "limit": integer}`，`required: ["query"]`
- Python 产品版（`tool_retrieval_toolkits.py:362-402`）：`{"operation": list|get_schema, "category": ..., "keyword": ..., "tool": ...}`，`required: ["operation"]`

这不只是参数名不同：产品版的 `tool_search` 是**目录导航器**（先按 `[category]` 找类目，再 `get_schema` 拿单个工具 schema），Go 版是**一次性 BM25 检索**。模型在两种提示词下学会的调用方式互不兼容。

**问题三：Go 少了一个开关，且现有开关没有生产赋值点。**

- Python 需要**两个**开关同时为真（`jiuwenswarm/common/config.py:469-479`）：`progressive_tool_enabled` **and** `progressive_tool_optimize`
- Go 只有 `ProgressiveToolEnabled`（`internal/agentcore/harness/schema/config.go:261-262`），全仓 `grep` 结果：只在 `factory.go:134` 从 `params` 拷进来，而 `params` 的所有生产构造点（`deep_adapter.go:587-616`、`code_adapter.go:333`、`agent_tool.go:344`）**都没有给这个字段赋值**。

**影响与触发**：
1. 今天：9.11a 落地的一整章（曝光策略、BM25 索引、tool_search/tool_call、目录附件、顶层工具过滤）在真实链路里**一行都执行不到**。这与计划里 `9.11a ✅` 的完成度标注不符。
2. 一旦有人补上赋值：用户按 Python 的语义只打开 `progressive_tool_enabled` 而 `progressive_tool_optimize` 关着（Python 此时**全部工具 direct**），Go 会把 deferred 工具藏起来、要求走 tool_search——同一份 config.yaml 在两边行为相反。
3. 提示词正文用错版本：Go 复刻的是 `openjiuwen/harness/prompts/sections/progressive_tool_rail.py:40-108`（「使用用户意图和所需能力描述作为 query」），产品版是 `tool_retrieval_prompt_rail.py:75-103` 的 `_DISCLOSURE_INTRO_CN`（「按标注导航：`[category]` 行是分类，调用 tool_search（operation=list，category=该行名称）」）。模型看到的规则和可用的工具能力对不上。

**建议**：把 9.11a 的对齐对象改成产品层的 `ToolRetrievalPromptRail` + `ToolRetrievalToolkit`（静态类目表 + `mcp_{server}` 虚拟分支 + operation 形态的 tool_search + tool_call 信封注册）；`ProgressiveToolRail` 按上游做法在装配期摘掉或干脆不移植；两个开关语义补齐（`progressive_tool_optimize` 缺失）。

---

### S-02 `session.delete` 从 Web 通道被删掉且不在转发集，前端删会话直接报「未知方法」

- **归属流程**：Web WS 分发 → 会话生命周期 RPC
- **相关提交**：`385eb614`（漂移归零，删 15 个上游没有的方法）

**证据**：

- `internal/swarm/gateway/channel_manager/web/app_web_handlers.go` 共 99 个 `wc.registerMethod(...)`，**没有** `session.delete`；全 `internal/swarm/gateway/` `grep session.delete` → 0 命中。
- `internal/swarm/gateway/normalize.go:45-116` 的 `ForwardReqMethods` 里也没有 `session.delete`。
- 分发兜底在 `web_connect.go:318-330`：既没有本地 handler、又不命中转发集 → `wc.writeError(conn, reqID, "unknown method: "+method, WsErrMethodNotFound)`。

**Python 对照**：`gateway/channel_manager/web/lifecycle_handlers.py:246-250` 明确 `channel.register_method(method, handler(method))` 批量注册了 `session.delete`（同时 HTTP 侧另有 `http_routes/mapped.py`）。

**影响**：会话列表删除 / 归档页删除全部失效，前端只能拿到协议级 `METHOD_NOT_FOUND`。AgentServer 侧的 `session.delete`（`internal/swarm/server/handle_session.go:127`）还在，但 Web 通道的请求根本到不了它那里。

**为什么算严重**：删这条的唯一依据是「上游没有」；而它恰恰是上游**有**的。这直接暴露了本批验收口径的问题（见 M-06）。

---

### S-03 `chat.swarmflow_reply` 只回 ack、不转发给 Agent，人的回复永远到不了工作流

- **归属流程**：Web WS → 人机协同（SwarmFlow 人工确认/回复）
- **相关提交**：`c3b602f7`（B1b-4 杂项本地方法）

**证据**：

`app_web_handlers.go:3802-3814`：

```go
// chatSwarmflowReply 空 ack：三层路由把 reply 转发给 agent adapter（由其构造
// HumanAgentMessage 交给 team_manager），HTTP 层只需应答。
func chatSwarmflowReply(wc *WebChannel) methodHandler {
	return func(ws wsSender, reqID string, _ map[string]any, sessionID string) {
		wc.sendResponse(ws, reqID, true, map[string]any{
			"accepted":   true,
			"session_id": sessionID,
		}, "", "")
	}
}
```

注释说「三层路由把 reply 转发」，但事实是：
- 不在 `ForwardReqMethods`（`normalize.go:45-116`）
- 不在 `ForwardNoLocalHandlerMethods`
- 不在 `localOnlyMethods`（`web_connect.go:52-53`，只由 `MarkLocalOnly` 显式写入）
- 不在 `handlerBeforeCallbackMethods`（`web_connect.go:118-120`，只有 `chat.send`）

所以走的是第 ④ 段「只有本地 handler」→ 只回 ack，**从不转发**。

**Python 对照**：`_FORWARD_REQ_METHODS`（`gateway/channel_manager/web/app_web_handlers.py:407` 起）里有 `"chat.swarmflow_reply"`，且它不在 `_FORWARD_NO_LOCAL_HANDLER_METHODS` 里——即「先转发 + 本地再回 ack」两件事都做。

**影响**：典型假成功。前端拿到 `accepted: true` 就不再重试，工作流的人工确认环节静默卡死。

---

### S-04 `e0ed3e69` 删掉 ResourceMgr 预删除，热刷新后技能工具永远执行第一次构建的旧实例

- **归属流程**：技能 rail 装配 → 工具注册 → 热刷新（技能安装/卸载、MCP 同步、配置热更）
- **相关提交**：`e0ed3e69`（同时声称「defer to rails that already own read_file/code/bash」）

**证据**：

`git show e0ed3e69 -- internal/agentcore/harness/rails/skills/skill_use_rail.go` 删掉了这段：

```go
-			existing, err := resourceMgr.GetTool([]string{toolID})
-			if err == nil && len(existing) > 0 {
-				if _, removeErr := resourceMgr.RemoveTool([]string{toolID}); removeErr != nil { ... }
-			}
```

替换成了（现 `skill_use_rail.go:448-462`）：

```go
if err := resourceMgr.AddTool(t); err != nil {
    // 同 ID 可能已被占用（同 agent 重复 Init / 全局注册表残留）：
    // 记录告警但不阻断能力注册，否则工具对模型不可见。
    logger.Warn(...).Msg("failed to add tool resource to resource_mgr")
} else {
    r.ownedToolIDs[toolID] = struct{}{}
}
```

而 `AddTool` 对重复 ID 是**直接报错不覆盖**（`internal/agentcore/runner/resources_manager/tool_manager.go:112-121`：「工具已存在」）；工具 ID 是确定值 `SkillTool_<agentID>`（`prompts/tools/registry.go:85-92`）；`Uninit`（`skill_use_rail.go:484-499`）只 `am.Remove(toolName)`，**从不 RemoveTool**，`ownedToolIDs` 只是清零。

同时 `DeepAdapter.getCurrentAgentRails` 每次都会重建 skill rail（`deep_adapter.go:3063-3064`：`d.skillRail = d.buildSkillRailWithIncludeTools(...)`）。

**Python 对照**：`openjiuwen/core/single_agent/ability_manager.py:837-840`

```python
Runner.resource_mgr.add_tool(resource, refresh=True)   # 覆盖，同 id 后来者赢
```

`remove_ability`（:842-858）还会 `resource_mgr.remove_tool(card.id)`，另有 `teardown_tools()` 在轮末清掉本 agent 的 stateful 工具。Python 注释原文就写着 "so a later owner of the same id wins"。

**影响链**：AbilityManager 的卡片换成了新 rail 的，但执行期 `executeTool` 是按 ID 从 ResourceMgr 取实例（`internal/agentcore/single_agent/ability/ability_manager.go:900-912`）→ 拿到的仍是**旧 rail 的 SkillTool**，其闭包绑定旧 rail 的 `r.skills`。于是：
1. 新装的技能，`skill_tool` 看不见；
2. `RefreshSkillRails` 清掉基线后，旧实例用「旧快照」继续把已卸载技能提供给模型；
3. `ownedToolIDs` 只写不删，进程级 ResourceMgr 持续泄漏。

这正是用户关心的第 2 类问题（依赖已具备却没接上）的反向版本——**原本接好的又拆掉了**，而且 `IMPLEMENTATION_PLAN.md` 的 9.19-23 行把「删 ResourceMgr 预删除」当成成果登记，说明这个偏差是被当成"对齐"写进计划的，需要连计划一起纠正。

---

### S-05 `SkillUseRail` 全字段无锁，同一 adapter 多会话并发时会 `fatal error: concurrent map writes`，且会话间互相串味

- **归属流程**：技能 rail 生命周期（BeforeInvoke / BeforeModelCall）
- **相关提交**：`35568c1c`、`1966c60d`、`e5d79cf0`

**证据**：`internal/agentcore/harness/rails/skills/skill_use_rail.go` 全文 `grep sync. / Mutex` → **0 命中**（结构体 100-102 行也无锁字段）。而无保护的共享可变状态有：

| 位置 | 字段 | 读/写 |
|---|---|---|
| `:1094` | `r.skillCache[key]` | 写 |
| `:1086`/`:1109`/`:1215` | `r.skillCache` | 读 |
| `:1261` 附近 | `r.evolutionTexts` | 写 |
| `:525` | `r.currentQuery = extractQueryFromMessages(...)` | 写 |
| `:1289` | `applySkillBudget(skills, r.currentQuery, ...)` | 读 |

**Python 对照**：`openjiuwen/harness/rails/skills/skill_use_rail.py:486-508`、`:657-669` —— query 是**每轮现算的局部变量**，直接作为参数传给 `_build_skills_section`，rail 不保存 query。Python 单线程 asyncio，也没有「两个会话同时跑」的写竞争。

**影响**：同一 adapter 只持有一个 `d.instance`（`deep_adapter.go:618`），多个 Web 连接/团队会话会并发进入同一 rail 的回调；回调框架执行业务回调时不持锁。并发写 map 在 Go 里是**不可 recover 的进程级崩溃**。即便不崩，A 会话的技能预算排序也会用 B 会话的 query。

这是本批最该在合并前拦下来的问题：**把 Python 的「每轮局部变量」提升成 rail 字段，同时没有引入任何同步**。

---

### S-06 取消/异常后「清洗并持久化本轮上下文」用的是已取消的 ctx，生产 checkpointer 下必然写库失败

- **归属流程**：ReAct 生命周期 → 取消/异常收尾
- **相关提交**：`afd6ae0c`（取消与异常后清洗并持久化本轮上下文）、`e50926de`

**证据**：`internal/agentcore/single_agent/agents/react_invoke.go:1144-1169`

```go
func (a *ReActAgent) handleContextAbort(ctx context.Context, sess ..., marker string) bool {
	...
	a.saveContexts(ctx, sess)                       // :1155
	if as, ok := sess.(*session.Session); ok {
		if commitErr := as.Commit(ctx); commitErr != nil { ... return false }   // :1159
	}
	return true
}
```

调用点 `:508-512` 就是那条刚被取消/出错的 ctx：

```go
if utils.Canceled(ctx) {
	abortPersisted = a.handleContextAbort(ctx, sess, "[Request cancelled by user]")
}
```

`Session.Commit(ctx)`（`internal/agentcore/session/agent.go:389`）最终带 ctx 落盘；ctx 已取消 → 返回 `context canceled`，只打一条 Warn 并 `return false`，随后 finally 又用同一个 ctx 再试一次。

**Python 对照**：`openjiuwen/harness/rails/../react_agent.py:3650-3706` 的 `_persist_context_after_abort` 用了两次 `asyncio.shield(...)`（:3664、:3688-3691），取消不会阻断写库。

**影响**：内存里的清洗生效了，检查点没落盘。同进程内继续对话看起来正常，**重启 / 多实例 / 上下文重载后回到旧检查点**，本轮等于没清洗——而单测 `TestInvoke_CancelPersistsSanitizedContext` 用的是 InMemory checkpointer + 忽略 ctx 的假 context engine，永远绿。

---

### S-07 `channel.<p>.set_conf` 只写配置文件、不重启通道实例，改完凭据必须重启进程才生效

- **归属流程**：通道配置 RPC（B1b-2，18 个方法）
- **相关提交**：`968bafac`

**证据**：

- `channelConfSet`（`app_web_handlers.go:3474-3543`）只做 `channelMgr.SetConf(...)` + `updateChannelInConfig(...)` + 清 Agent 缓存。
- `ChannelManager.SetConf` 的「重建通道」挂在 `onConfigUpdated` 回调上（`channel_manager.go:282-303`），但生产装配传的是 **nil**：`app_gateway.go:136` → `cm.NewChannelManager(msgHandler, nil, nil)`。
- `SetConfigCallback`（`channel_manager.go:323`）全仓**无生产调用点**；`MarkChannelRestartPending`/`PopChannelRestartPending`（`:240-260`）也**无消费方**。

**Python 对照**：`channel_manager.py:373-394` 的 `set_conf` 会 `await self._on_config_updated(self._config)`；该回调接到 `app_gateway.py:2806-2860` 的 `_apply_channel_config`，内部 `_should_restart_channel(...)` 后 stop/start 各 IM 通道。上游 `_channel_telegram_set_conf` 的 docstring 也直接写着「并按新配置重新实例化通道」（`app_web_handlers.py:4543-4544`）。

**影响**：用户在设置页改完飞书/Telegram 凭据，接口成功、config.yaml 也对，但正在跑的通道实例仍持旧 token。这正是用户关心的第 2 类问题——**重启机制早就实现好了，就是没人接上**。

---

### S-08 `permissions.*` 转发的 E2A request_id 恒为空串，并发请求直接撞在途队列；失败还会静默回退本地写

- **归属流程**：权限配置 RPC 代理
- **相关提交**：`c510ab75`（handler 批转 native 时保留）

**证据**：`app_web_handlers.go:1823-1848`

```go
env := e2a.E2AFromAgentFields("",              // ← request_id 空
	e2a.WithFieldChannelID(""),
	e2a.WithFieldSessionID(sessionID),
	...
)
resp, err := ac.SendRequest(context.Background(), env)
if err != nil {
	logger.Error(...).Msg("permission RPC forwarding to AgentServer failed")
	// 转发失败，回退到本地 dispatch
}
```

在途队列以 request_id 为 key，空串即全局同一个槽位；第二个并发请求会因「重复的进行中请求」被拒。失败后还会落到本地 `permrpc.DispatchPermissionsConfigRequest` 把配置写到**网关本地**。

**Python 对照**：`proxy_unary_request` 明确传 `request_id=req_id`（`e2a_proxy.py:139-149`），失败只回错误帧、没有本地回退（`:188-212`）；上游 spec 对这条也有明确口径：「Gateway never falls back to local state」。

**影响**：设置页打开时 `permissions.tools.*` / `rules.*` / `approval_overrides.*`（共 10 个方法）多路并发拉取必然撞车；撞车后错误被吞，多租户形态下会把本该写到对端 AgentServer 的权限写到网关本地。

---

## 二、一般（16 项）

### M-01 `external_cli.install_status` 返回的字段名与前端契约不符，未安装的 CLI 会显示成「正在安装」

- Go：`defaultExternalCLIInstallStatus()` 只有 `{installing, installed, state, progress, error}`（`app_web_handlers.go:3962-3970`）
- Python：`_EXTERNAL_CLI_DEPENDENCY_INSTALL_IDLE_STATUS` 是 21 个字段，键名是 `status` / `phase` / `progress_kind` / `log_tail` …（`app_web_handlers.py:110-131`）
- 前端实证：`internal/swarm/channels/web/frontend/src/components/ExternalCliAgentsSection.tsx:1511-1512` → `installStatus.status !== 'idle'`。Go 没有 `status` 键 → `undefined !== 'idle'` 恒真 → 每种 CLI 首次加载都渲染「安装进行中」并开始轮询。

### M-02 11 个占位 handler 一律回「成功」，属于假成功而不是「不支持」

- `updater.check/download/get_status` 回硬编码 `{"status":"up_to_date"}`、`updater.upgrade` 回 `{"state":"idle"}`、`updater.get_conf/set_conf/reset_source` 回默认值（`app_web_handlers.go:336-344`）；`external_cli.install` 回 `{"installing":false}`、`custom_agent.remove` 回 `{"cli_agent":""}`（`:353-354`）。
- Python：这些都有真实实现；`external_cli.install` 在非 frozen runtime 下明确回 `UNSUPPORTED`（`app_web_handlers.py:2345-2356`）。
- 影响：用户点「检查更新」永远得到「已是最新」；点「一键安装」进入流程后一直等；点「移除自定义 agent」什么都没删却收到成功。
- **口径建议**：占位应统一回 `UNSUPPORTED` + 说明文案，让前端能区分「不支持」与「已完成」。

### M-03 `channel.wechat.get_login_ui` / `unbind` 是空壳却回 `ok:true`，解绑是假成功（安全语义错误）

- Go：`app_web_handlers.go:398-399` 两行 `stubHandler(wc, map[string]any{})`。
- Python：`get_login_ui` 走 `snapshot_wechat_login_ui_state()`（`:4899-4912`）；`unbind` 会 `clear_wechat_bound_session` → `reset_wechat_login_ui_state` → `mark_channel_restart_pending` → `set_conf` → 写回 config.yaml（`:4914-4956`）。
- 影响：用户以为已解绑，凭据与连接都还在，旧账号继续收消息。这条比 M-02 更严重一些，因为它涉及账号安全，仅因影响面限于微信单平台列为一般。

### M-04 `channel.wechat.set_conf` 缺少数值边界校验，非法参数直接落盘

- Go：走通用分支，只判 `params == nil`（`app_web_handlers.go:3485-3488`）；全仓无对应校验实现。
- Python：`set_conf` 之前先 `_validate_wechat_numeric_params`（`app_web_handlers.py:846-876`，调用点 `:4876-4885`）：`long_poll_timeout_sec` 必须整数、四个字段有限正数且落在区间、`backoff_max_sec >= backoff_base_sec`，非法回 `BAD_REQUEST` + 中文文案。
- 影响：负数/0/超大值会写进 `channels.wechat` 并清缓存，微信长轮询与退避定时器拿到畸形值（0 间隔重连、负数计时），坏配置会持续到下次启动。

### M-05 配置文件写回没有跨请求串行化，多连接并发保存会丢更新

- Go：所有写回都是「新建实例 → 读 → 改 → `os.WriteFile` 截断覆盖」。`SetRoundTrip` 的锁是**实例级** `c.mu`（`internal/common/config/roundtrip.go:30-31`），写文件用 `os.WriteFile`（`:93`），既无进程级文件锁也没有「临时文件 + rename」。同类写点：`updateChannelInConfig`（`app_web_handlers.go:3686-3697`）、`heartbeatSetConf`、`externalCLIPrunePathHistories`、`memoryForbiddenSet`、`permissionsOwnerScopesSet`。
- Python：全程单线程 asyncio 读-改-写天然互斥，不存在并发写面。
- 影响：网关是一连接一 goroutine，两个标签页/两个用户同时保存 → 后写覆盖先写（读到的是旧 section）；极端交错下 `os.WriteFile` 截断期还可能被另一写者读到半文件，配置直接损坏。
- 建议：改成「临时文件 + `os.Rename`」+ 进程内 `sync.Mutex` 串行化配置写。

### M-06 契约矩阵的「漂移 0」不可信，而且这道门禁已经从 CI 撤掉了

- `cmd/rpc-matrix` 的上游扫描只认三种字面量写法（`cmd/rpc-matrix/scan.go:26`、`:126-171`），**循环注册完全不可见**——`lifecycle_handlers.py:250` 的 `channel.register_method(method, handler(method))` 一个都扫不到。
- 矩阵只对「前端 ∪ 本仓 ∪ 上游」求并集（`matrix.go:36-48`），三方都没被扫到的方法**根本不生成行**，因此不可能被判「缺」。`session.delete` 就是这样消失的（`docs/sync/web-rpc-matrix.md` 里 grep 零命中，而上游确实有）。
- 判定只看名字是否注册，不看实现：`channel.wechat.unbind` 在 Go 是 `stubHandler`，矩阵仍标 `✅ 一致`。
- 反向也挡不住：`scanTree` 逐行跑正则、不剥注释（`scan.go:91-101`），注释掉的注册照样算「已注册」。
- CI 步骤已在 `36fcbc29` 整段删除（`.github/workflows/ci.yml` 现无 `rpc-matrix`），Makefile 也无入口，只剩本地手工跑。
- 影响：这批「漂移归零 / ✅ 89」是删除 15 个方法的唯一依据，而该结论的产出工具存在系统性漏扫且已退出 CI。建议要么恢复门禁前先 pin 一份可复现的上游快照并支持循环/工厂注册，要么把矩阵降级为「参考资料」别当验收口径。

### M-07 `.coverage-exempt` 把 `sessionProxyForward` 的转发主体整段豁免，新方法的唯一新逻辑没有测试

- `.coverage-exempt:347-351` 登记 `app_web_handlers.go#L4339-L4370`，理由「需真实 AgentServer 客户端与进程间往返」；实际该区间覆盖了「不可达分支 + 组 E2A + SendRequest + 失败响应」全部核心逻辑（`app_web_handlers.go:4348-4372`）。
- 本批新增的 `session.rename` / `session.plan_status` 的唯一新逻辑就是这段代理，豁免后只验证了「不可达」分支。
- 仓库里已有可注入的 `AgentClient` / 假 E2A 端点（`agent_client_test.go:116` 就在用），属于「可 mock 的代码用豁免逃避」。

### M-08 `sessionProxyForward` 的错误码映射与上游不一致（超时/传输失败都报 INTERNAL_ERROR）

- Go：任何错误统一 `WsErrInternalError`（`app_web_handlers.go:4366-4372`）。
- Python：超时 → 专用的 `AGENT_SERVER_TIMEOUT_CODE`；发送异常 → `SERVICE_UNAVAILABLE`（`e2a_proxy.py:188-212`）。
- 影响：前端按 code 决定是否可重试；「对端不可用」被报成「网关内部错误」，用户看到的是不可重试的红错。

### M-09 刷新技能时用错了 disabled 名单 API，MCP/未登记技能的禁用不生效

- Go：`internal/swarm/server/adapter/deep_adapter_skills.go:36` → `rail.SetDisabledSkills(d.skillManager.ListExecutionDisabledSkills())`；该函数先取 `GetRegisteredSkillNames` 再过滤（`internal/swarm/server/runtime/skill/state_utils.go:242-255`）。项目里已有正确的 `ListDisabledSkills`（`skill_manager.go:2270`）。
- Python：`interface_deep.py:11163-11185` 的注释原文：「use list_disabled_skills (**NOT** list_execution_disabled_skills) — the latter filters to "registered" … so a disabled MCP's skills would stay visible」，并且调用前先 `reload_state()`。
- 影响：MCP 捆绑/外部 root 的技能被禁用后仍对模型可见——正是本批「禁用即时生效」想解决的场景。另外 Go 也漏了 `reload_state()`，热切换后读到的是旧 state。

### M-10 技能发现不跟随子目录符号链接，软链共享的技能库被整目录跳过

- Go：`skill_use_rail.go:754`（以及 `:591`、`skill_layout.go` 同类处）用 `os.ReadDir` 的 `entry.IsDir()`，**不跟随** symlink。
- Python：`skill_use_rail.py:1100` 用 `Path.is_dir()`（跟随 symlink），注释明确写 "Symlinked libraries are a normal way to share one skill across teams"。
- 影响：`skills/` 下 `lark -> /shared/skills/lark` 这类团队共享布局整目录消失；本批新增的 visited 防环对子目录软链也不可达（根本不下钻），并且没有软链用例。

### M-11 裸媒体路径识别用了消费式边界，相邻两个媒体只认第一个

- Go：`internal/agentcore/harness/tools/skills/markdown_media.go:23` → `(?i)(?:^|[^\w./])([\w./-]+\.(?:png|...))(?:[^\w.]|$)`
- Python：`markdown_media.py:20-25` 用零宽断言 `(?<![\w./]) ... (?![\w.])`
- 影响：`"a.png b.mp4"` 里第一个匹配把分隔空格吃掉，第二项无法开始 → 视频引用漏判、`SKILL_TOOL_MARKDOWN_VIDEOS_HINT` 不注入。表格/连续截图场景常见。
- 附带：`mediaURLSuffix` 只替换 `%20`（`:79`），Python 用 `urlparse.unquote` 全解码，`%2Epng` 之类漏判。

### M-12 延迟工具目录不比较 schema 指纹，原地改 schema 不产生「已修改」通知

- Go：`progressive.go:1047-1049`
  ```go
  if fingerprints, ok := normalizeDeferredCatalog(mapValueOf(existing.Metadata[...])); ok {
      _ = fingerprints          // ← 恢复的指纹被直接丢弃
  }
  ```
  且全文没有 `previousFingerprints` 基线：判「已修改」只看 `existing != description`（`:1074-1076`）。`collectDeferredFingerprints()` 每轮都算、存在 state 与附件 metadata 里，但**从不参与比较**。
- Python：`tool_retrieval_prompt_rail.py:767-777` 明确「基线有指纹、当前有指纹且不相等」也算 updated。
- 影响：工具的 schema 原地变更（描述压缩到 120 字后没变）时，模型收不到「已修改」附件；下一轮它仍用旧参数调用，被 `callDiscoveredTool` 的指纹校验拒掉（`progressive.go:538-542`），却不知道自己错在哪。

### M-13 目录状态恢复用 `.(int)` 断言，序列化往返后类型变成 float64，版本号与 delta 计数会复位

- `progressive.go:1053-1058`：`existing.Metadata["catalog_version"].(int)`、`["delta_count"].(int)`；`catalogStateOf` 里的 `intOf`（`:1274-1279`）也只认 `int`。
- 影响：附件 metadata / 会话状态一旦经过 JSON 往返（重启、跨实例、检查点恢复），断言失败静默回落 → 每次恢复都回到 version=1、delta_count=0，导致「连续 10 次 delta 转快照」的兜底永远不触发、版本号在历史里来回跳。本项目此前已多次踩「float64 断言」这个坑，属系统性风险。

### M-14 无进展守卫的零值回退写成 80，上游等价场景是 20

- Go：`deep_agent.go:3299-3305` → `maxConsecutive <= 0 → 3`、`minChars <= 0 → 80`，注释自认「上游 DeepAgentConfig 构造默认是 3/20，回退分支是 3/80，这里按回退分支对齐」。
- Python：`schema/config.py:166-170` 的 dataclass 默认是 `min_answer_chars: int = 20`；`getattr(guard_cfg, "min_answer_chars", 80)` 的 80 只在 `guard_cfg` 是 dict 的鸭子类型分支生效，而全仓没有传 dict 的调用点。
- 影响：程序化构造 `TaskLoopNoProgressGuardConfig{Enabled: true}`（不填阈值）时，20~79 字的答案在 Go 被算「无进展」，3 轮即停；走默认配置的路径不受影响，因此降级为一般。

### M-15 `3800239a` 改无参方法时顺手删掉了锁，`buildTaskLoopEvaluators` 无锁读配置与 rail

- `git show 3800239a` 删除了 `d.configMu.RLock()/RUnlock()` 与其间的 `guard = d.deepConfig.TaskLoopNoProgressGuard` 拷贝；现 `deep_agent.go:3282-3290` 直接读 `d.taskCompletionRail` 与 `d.deepConfig.TaskLoopNoProgressGuard`。
- 写点有锁：`d.taskCompletionRail`（`deep_agent.go:515-516`，railsMu）、`d.deepConfig`（`:267-275`/`:1503`，configMu）。
- 影响：任务循环 invoke 进行中，Web 端改配置或运行期重新注册 rail → `-race` 可命中，并可能读到「rail 已替换、guard 还是旧配置」的混合评估器集，本轮停止条件出错。改无参方法并不需要删锁。

### M-16 桥接事件只做了 2/3，注释却承诺三条

- `deep_agent.go:180-193`：`bridgeEvents` 只新增 `CallbackAfterReactIteration`、`CallbackOnUserMessage`，而 `:190` 的注释原文写着「Python: _BRIDGE_EVENTS 的 AFTER_REACT_ITERATION / ON_USER_MESSAGE / **BEFORE_STEERING_DRAIN** 三条，各带同样理由的注释」。
- 全仓 `grep BeforeSteeringDrain / SteeringDrain` → **0 命中**；steering 在 `react_invoke.go:735-740` 是一次性全量 `DrainSteering`。
- 影响：注释承诺了对齐但事件类型和输入结构都没移植；后续 TeamPolicyRail 搬过来时永远收不到事件，单靠往 `bridgeEvents` 加一行也无法生效。当前成员忙碌期积压的 mailbox 消息是一次性全部注入的。

### M-17 流包装层 panic 兜底只投哨兵不排空内层，长流会泄漏 goroutine 与 HTTP 连接

- `internal/agentcore/foundation/llm/model.go:273-291`：recover 分支发完 `StreamError` 就返回（defer close(out)），没有像正常 ctx.Done 分支（`:324-339`）那样 `for range chunkChan` 排空；`decorator.go:175-189` 同构。
- 生产侧 chunkChan 缓冲 64（`model_clients/openai/client.go:309`），生产 goroutine 只写不 select ctx。
- 影响：panic 本身被转成了 error（没吞错），但剩余 chunk 超过缓冲时生产 goroutine 永久阻塞、`resp.Body` 不关闭。正常取消路径能退出，只有「钩子 panic 且未取消」这条漏。

### M-18 `resolveExternalChannelSession` 持全局锁跨同步 RPC，一条慢 `session.create` 卡住全部 a2a 入站

- `internal/swarm/gateway/message_handler/channel_state.go`：`externalAliasMu.Lock()` 之后才调 `allocateChannelSession`，而后者内部是一次同步 `mh.agentClient.SendRequest(ctx, env)`（`:506`）。
- 结构上确实是照抄上游（上游同样在全局 `asyncio.Lock` 内 await 分配），但 Python 是单事件循环、没有「一条慢请求阻塞其它线程」的形态；Go 里这等于把一次 RPC 的超时放大成全局停顿，且是在持锁状态做 I/O（若 RPC 路径将来回进 message handler 就是死锁）。
- 建议：改成「占位 + 单飞（singleflight）」，只对同一个 `(channel_id, external_id)` 串行，不要用全局锁。

### M-19 探活键迁移忽略传入的 `cfg`，用默认路径新建实例；单测会改到真实用户配置

- `internal/common/config/roundtrip.go:209-222`：`MigrateLegacyHeartbeatProbeConfig()` 无参，内部 `New("")`（即全局默认 `ConfigDir()/config.yaml`）；而调用点 `app_gateway.go:194-205` 手里明明有 `cfg`，迁移完再 `cfg.Reload()`。
- 影响一：若启动时用的是非默认配置路径，迁移会读到/写到另一个文件，白做。
- 影响二：`app_gateway_test.go:38-49` 的 `newTestAppGatewayServer` 没设 `UAPCLAW_DATA_DIR`，构造网关就会触发迁移——**跑单测会改开发者本机的真实 config.yaml**，测试不再隔离。

### M-20 `Uninit` 不校验所有权、也不回收 ResourceMgr，可能误删别人重新注册的工具

- `skill_use_rail.go:484-492`：对 `ownedToolNames` 无身份比对直接 `am.Remove(toolName)`。
- Python：`skill_use_rail.py:408-428` 先 `if ability_manager.get(tool_name) is not tool_card: continue`（注释写明「名字被别人重新注册后本 rail 仍是旧 card，此时拆除会注销对方的工具」）。
- 影响：热重载 / 多 rail 覆盖同名工具时会把对方的工具拆掉。当前装配顺序降低了概率，属潜伏缺陷（与 S-04 同源）。

---

## 三、提示（12 项）

### T-01 为一行注释把 1300+ 行的适配器整体移出集成覆盖统计

- `a3a8a21e` 在 `.integration-coverage-exempt` 追加 `internal/agentcore/retrieval/vector_store/{chroma,milvus}.go` 整文件豁免，起因是 `14f4df77` 撤掉的**两行行内注释**（改动落在 switch 的 `default:` 标签行，无覆盖块）。
- 逻辑上说得通（行级豁免对未插桩块无效），但代价与收益严重失衡；建议下次这种「纯注释改动」干脆别改，或改成不改动行的写法。

### T-02 为规避覆盖率豁免而改代码结构（判空跨包下沉）

- `0912a250` 删掉 `deep_adapter_skills.go` 的 `if sess := d.instance.LoopSession(); sess != nil`，理由直接写在提交信息里：「这样两行改动都能被 `TestRefreshSkillRails_未绑定会话时安全返回` 覆盖，无需行级豁免」。
- 判空下沉本身可以接受，但**由覆盖率门禁驱动代码结构**是危险信号：上游在这一层就是 `getattr(loop_session, ...)` 判空；现在的等价性依赖「rail 内部一定用 isNilSession 兜住」，跨包隐式契约没有测试守着。

### T-03 `list_skill` 缺 `render_for_llm`，模型看到的是 JSON 而非上游的一行一技能

- Go：`internal/agentcore/harness/tools/skills/list_skill.go:147-154` 只返回 `success/data`，无 `content`，最终走 `json.Marshal`（`ability_types.go:98-100`）。
- Python：`list_skill.py:88-95` 渲染 `- name: description (skill_md_path)`，可带 `message` 前缀；`routeSkills` 用 `json.dumps(..., ensure_ascii=False, indent=2)`（`:129-134`），Go 是无缩进 `json.Marshal`（`list_skill.go:207`）。
- 影响：prompt 文本与 token 数都不一样。此项非本批引入，但本批宣称「list_skill 对齐」时仍是缺口。

### T-04 技能描述不支持 `description_<language>`

- `skill_use_rail.go:1197` 只取 `yamlData["description"]`；Python `skill_use_rail.py:996-1000` 先取 `description_{language}` 再回落。
- 上游有只用 `description_en` 的 SKILL.md，英文会话下 Go 会显示中文描述或退化成 `Skill located in ...`。本批把描述渲染进基线后更显性。

### T-05 死结构体与半实现的导出 API（违反规则 8）

- `skillBaselineState` / `skillBaselineEntry`（`skill_use_rail.go:103-119`）全仓零引用，实际序列化走 `map[string]any`。
- `ClearSkills`（`:329-335`）未重置 `lastSignature`/`signatureSet`，而 Python `clear_skills` 会置 `_skills_snapshot_signature = None`（`skill_use_rail.py:157-164`）；该函数生产无调用方。

### T-06 参考版本改成常量表 + 注释与代码不符

- `app_web_handlers.go:3818-3828` 新增 `externalCLIReferenceVersions{claude:"0.2.115", codex:"0.144.4"}`；而同一文件 `:3985-3987` 的 `detectExternalCLIAgent` 注释仍写着「reference_version 取 Python 侧包元数据……本仓无此依赖，**恒回空串**」——注释已经和下面 10 行的代码矛盾。
- 语义差异也值得注意：Python 在包未安装时返回 `""`（不做比较、不告警）；Go 恒有值，一旦上游升版或用户 CLI 版本不同，就会稳定输出 `version X may be incompatible with expected version Y`。

### T-07 `ToolExposure` 的「显式声明」只认私有标记，直接给字段赋值会被注册期策略静默覆盖

- `foundation/tool/exposure.go:30-48`：零值按 direct，只有 `SetExposureDeclared(true)` 才算已声明；`ability/ability_manager.go:133-148` 对未声明者无条件 `card.Exposure = resolved`。
- Python 用 pydantic 的 `model_fields_set` 识别「显式传了 exposure」，显式值直接 return（`ability_manager.py:225-248`）。
- 影响：`card.Exposure = tool.ToolExposureDirect` 这种字段级写法不锁定，progressive 打开时会被改成 deferred。当前仓库内生产代码只有三处主动调 `SetExposureDeclared`，属潜伏缺陷，插件/第三方按字段声明就会踩。

### T-08 `tool_search` 渲染 schema 用了会做 HTML 转义的 `json.Marshal`

- `tool_discovery/tool_search.go:163-167` 与 `:167` 用 `json.Marshal(item["parameters"])`；Python 是 `json.dumps(..., ensure_ascii=False)`。
- 差异：Go 会把 `<`、`>`、`&` 转成 `\u003c` 等。JSON 语义合法但模型看到的文本不同；项目已有 `pyjson` / `marshalCompactNoEscape` 先例，此处未复用。

### T-09 无进展守卫与 50 轮上限的日志/文案没对齐上游

- `stop_condition.go:411-417` 输出结构化字段 `iteration/output_len/...` + `msg="no progress answer guard triggered"`；Python `stop_condition.py:173-184` 是一整行 `"No progress answer guard triggered: round=%s, output_len=%s, ..."`（字段名 `round` vs `iteration`）。
- `deep_agent.go:3319` 的 `const maxOuterRounds = 50` 与 `:3329` 文案里再次硬编码 `(50)`，改常量时文案会漂移。

### T-10 `GetConf` 返回浅拷贝，嵌套 map 仍与实时配置共享

- `channel_manager.go:271-280` 用 `maps.Clone`（浅拷贝），而 `channelConfGet` 对非 feishu/xiaoyi 平台直接把这个 map 塞进响应（`app_web_handlers.go:3558-3563`）。
- 影响：另一连接 `set_conf` 时对内层 map 的写与本次序列化可能并发；概率低但属于「返回内部状态」的不好习惯。

### T-11 平台清单有两份真源

- 生产注册是 18 行硬编码字面量（`app_web_handlers.go:3429-3447`），而 `channelConfPlatforms`（`:2334-2337`）只被测试引用。新增平台极易只改一处；测试里的 `Len(..., 9)` 也发现不了「变量加了、注册没加」。

### T-12 技能预算在 adapter 路径用 `v.(int)` 接 RPC map，JSON 数字（float64）会被静默忽略；且 0 的语义与上游相反

- `deep_adapter_rails.go:307-317`：两个预算都是 `if n, isInt := v.(int); isInt`；同包已有 `parseIntAny`（`deep_adapter_config.go:990-1009`，支持 int/int64/float64/string）却没用。
- 语义：Go `maxSkills <= 0` = 不限（`skill_use_rail.go:1413`）；Python `max_skills = 0` 会 `ranked[:0]` 全部丢弃（`skill_use_rail.py:599-601`）。显式配 0 时两边行为相反。Go 用 int 无法区分 None 与 0，需要 `*int` 或哨兵值。

---

## 四、按提交批次的印象

| 批次 | 提交 | 印象 |
|---|---|---|
| Web WS RPC 全量对齐（B0/B1a/B1b） | `385eb614` `b86278c9` `6e624576` `0909d27a` `c510ab75` `968bafac` `853ae4d4` `c3b602f7` `f2fc6c3f` `d430411d` | 结构归位做得干净，文件级 1:1、分发顺序逐句可对照；但**验收口径从「行为」退化成「名字」**，导致误删与假成功（S-02、S-03、M-02、M-06）。`set_conf` 只接线了一半（S-07）。 |
| 延迟工具目录 9.11a + BM25 5.32 | `a1a66686` `2dfc1cb2` `4020cf09` `e012dbef` `15cfb1df` `4598234a` `870c2c0b` `62de7a36` `1788d5fb` `df01d112` `467c7cea` `cf1c8bc3` | 单看每一块实现质量不错（提示词逐字、limit clamp、指纹、ownership），但**对齐对象选错层**且开关没接（S-01），指纹只存不用（M-12）。整章当前不可执行。 |
| 技能会话基线 + skill_tool 对齐 | `35568c1c` `bbf3db91` `1966c60d` `52d13dd5` `3b4900d6` `d1a30d53` `e0ed3e69` `91646eaf` `c66fc838` `db7a56ca` `f33e9e70` `4fa3f379` `0912a250` | 语义对齐是这批最好的；但 `e0ed3e69` 删掉必要防护把三条链路全废（S-04），无锁（S-05），disabled 名单用错 API（M-09）。typed-nil 的三次收敛没有留测试后门，这点值得肯定。 |
| ReAct / harness 循环修复 | `f5e45f90` `17590079` `3800239a` `7997cb8d` `ddcaea4a` `763e5415` `05c04830` `3cad73cd` `e50926de` `22170151` `86ce459e` `075ffe4a` `6db8c478` `7d282edc` `afd6ae0c` `8fc7a55f` `41ef0a1e` `eef80939` | 数值/取值的对齐总体靠谱；两处硬伤：取消后写库用已取消 ctx（S-06）、改无参方法顺手删锁（M-15）；桥接只做 2/3（M-16）。 |
| 网关配置与通道 | `c2600380` `968bafac` `853ae4d4` | 迁移语义（setdefault、jobs 不动、搬空写空 mapping）写得清楚，但忽略了传入 `cfg`（M-19），通道重启没接（S-07）、无并发保护（M-05）。 |
| 覆盖率与文档收尾 | `607ac358` `9684dc5a` `a0ca794b` `e272c480` `d32398be` `14f4df77` `a3a8a21e` `2bd9be1f` `1cf3b7ec` `6beb58e6` `255a0103` `ac47cd90` | 豁免理由多数写得很实（谁、为什么、能不能 mock），但出现两处「为门禁改代码/整文件豁免」（T-01、T-02）。计划勾选与计数修正（41→39，与矩阵 138→99 一致）是对的。 |

---

## 五、建议处置顺序

1. **先修会崩/会假成功的**：S-05（加锁或改每会话 rail）、S-02（恢复 `session.delete` 注册）、S-03（把 `chat.swarmflow_reply` 加入转发集）。
2. **再修「功能实际不生效」的**：S-04（恢复 ResourceMgr 覆盖语义，`AddTool` 改成 refresh 或先删后加）、S-07（接上 `SetConfigCallback` + restart-pending）、S-06（换成不被取消的 ctx / 用独立 short-timeout ctx 落库）。
3. **然后是契约与验收口径**：S-01（决定是补产品层 rail 还是明确登记「本仓只做库层」并把 9.11a 标为未启用）、M-06（恢复可复现的矩阵门禁或降级为参考资料）、M-07（把 sessionProxyForward 用假 AgentServer 测起来）。
4. **最后是边界与体验**：M-01/M-02/M-03/M-04/M-09/M-10/M-11/M-12/M-13。

> 以上均为只读审查结论，未对任何代码做修改。
