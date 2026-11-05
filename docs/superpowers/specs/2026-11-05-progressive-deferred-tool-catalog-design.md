# 9.11a 延迟工具目录（deferred tool catalog）设计

- 对应 Python：`openjiuwen/harness/rails/progressive_tool_rail.py`（1448 行，HEAD `d6a81548`）
- 关联 Python：`harness/prompts/sections/progressive_tool_rail.py`（357）、`harness/tools/tool_discovery/{tool_search.py 109, tool_call.py 119, bm25.py 236}`、`harness/prompts/tools/{tool_search.py 90, tool_call.py 77}`、`core/foundation/tool/exposure.py`（26）、`core/single_agent/ability_manager.py`（曝光策略/registry_revision/execute）、`core/context_engine/processor/forked/compressor/recall/bm25.py`（123）、`harness/deep_agent.py`（`_DEFAULT_DIRECT_TOOL_NAMES` L199-242、`set_tool_exposure_policy` L507/L529、装配 L898、收尾 L1518-1528）、`harness/manifest/harness_elements.py`（`PROGRESSIVE_TOOL` 元素）
- 上游关键提交：`27a2e99b`（引入 tool_search + deferred）、`dce36fc8`（tool_call 统一执行）、`79159731`（参数变更重建 BM25）、`0af325eb`/`b95148b2`（保留 deferred 中断）、`d6a81548`（discovery-only 元数据）
- 缺陷台账：`docs/sync/defect-plan-2026-11-04.md` **D-18**（P1, 🔍）
- 日期：2026-11-05

## 1. 背景与定位

### 1.1 计划缺口

`IMPLEMENTATION_PLAN.md` 领域九只有一行 `9.11 ✅ ProgressiveToolRail | 渐进式工具权限`，**没有任何 deferred catalog 章节**；`grep -i deferred/tool_search` 在计划中 0 命中。而 9.11 的实现是上游**已废弃的旧语义**：

| 证据 | 内容 |
|---|---|
| `internal/agentcore/harness/rails/progressive.go:303` | 「动态的「延迟工具目录」才走附件（`_sync_deferred_tool_attachment`，**Go 侧未移植该子特性**）」 |
| `harness/tools/tool_discovery/search_tools.go:41` | `Search results are discovery-only. Tools shown here are not callable until load_tools is called.`（旧语义） |
| 全仓 `grep Exposure` | 0 命中（无 `ToolExposure` 概念） |

### 1.2 现状是死代码

`ProgressiveToolEnabled` 默认 `false`，且 Go 侧除 `deep_adapter_rails.go:269` 的注释外**无任何开启点**（无配置文件、无 adapter 赋值）。因此本项改造对现网行为零影响，符合"大幅重构但低风险"的前提；集成测试需显式开启开关。

### 1.3 作用

把「上下文预算」与「工具数量」解耦：顶层 `tools` 只保留少量 direct 工具；deferred 工具按 BM25 按需发现、经固定 `tool_call` 执行（搜索结果不污染 `tools` 列表、schema 精确下发）；目录变更以 system attachment 增量推送，保证模型对"当前哪些工具可用"的认知不过期，且**旧搜索结果不能复活已变更/已删除的工具**。

## 2. 流程位置

```
① 配置期   DeepAgentConfig.progressive_tool_enabled(默认 False) + tool_search_limit(默认 5, clamp 1..20)
              ↓
② 注册期   AbilityManager.set_tool_exposure_policy(enabled, direct_tool_names)
           │  卡片显式 exposure 优先 → 命中 direct 名单 → enabled?DEFERRED:DIRECT
           │  决策写回卡片 + declared=True；registry_revision 单调 +1
              ↓
③ Rail 装配 DeepAgent._queue_pending_rails：enabled 时追加 ProgressiveToolRail(priority=90)
              ↓
④ rail.init 注册 tool_search / tool_call（显式 DIRECT+declared），只存 registry 引用，不建索引
              ↓
⑤ 启动收尾  finalize_startup_async → list_tool_info 物化惰性(MCP) schema → _rebuild_tool_search_index
              ↓
⑥ 每轮 invoke  before_invoke：缓存全量 ToolInfo + ensure_initial_deferred_catalog
              ↓
⑦ 每次模型调用 before_model_call：① 注入 rules 节（规则正文 + session 初始目录）
           │                     ② sync_deferred_tool_attachment（delta/snapshot）
           │                     ③ inputs.Tools = meta ∪ direct
              ↓
⑧ tool_search  ensure_index（revision + fingerprint 双判据）→ BM25 → 精确名边界优先
           │  → 写 session 授权(names+fingerprints) + trace → 返回完整 parameters schema
              ↓
⑨ tool_call    五道校验 → 嵌套 ToolCall → ability_manager.execute（目标工具 rail/权限/超时/中断照旧）
           │  → RelayedToolOutput（转发目标渲染文本，multimodal 提升到外层 data）
              ↓
⑩ before_tool_call  非 direct 一律拒绝（meta 与「nested 标记 + 已授权」除外）
              ↓
⑪ uninit      按 ownership 比对移除自有元工具；清空索引与缓存
```

## 3. 拍板决策

| # | 决策 | 内容 |
|---|---|---|
| D1 | 实施范围 | **一次做全**：exposure 策略 + BM25 + tool_search/tool_call + 目录附件同步 + 嵌套执行通道 + 旧机制删除，单节闭环 |
| D2 | BM25 依赖边界 | **只落 `recall/bm25.py`**（123 行）；`archive`/`retriever`/`compression_recall` 工具另开章节（服务"压缩后召回原文"，与本项无调用关系，B-15 在那章收口） |
| D3 | 计划登记 | `9.11` 原文保留 + 追加「⚠️ 旧语义已由 9.11a 取代」；新增 `9.11a`；领域五新增 `5.32`（recall/bm25） |
| D4 | cbc 传递 | **方案 A**：`railedExecuteSingleToolCall/executeSingleToolCall/executeTool` 线程化 cbc 形参 + `foundation/tool` 窄接口 `ToolCallbackContext` + `WithToolCallbackContext()` 选项；`tool_call` 内断言回 `*interfaces.AgentCallbackContext` |
| D5 | 旧实现处置 | 旧 `search_tools`/`load_tools`/可见工具白名单/导航节/三配置字段 **同改动内删除**（规则 8），不留兼容壳 |
| D6 | 测试口径 | 单测覆盖改动行 ≥85%；集成用 MockLLM 断确定性契约（请求载荷 + 执行副作用），不依赖模型"想了什么" |
| D7 | 本项不改工具名 | Go 与上游的工具名差异（`sessions_*` vs `subagent_*` 等）不在本项修正，仅在 direct 名单中做映射并注释（见 §10 D-2） |

## 4. 结构映射

### 4.1 新增

| Go 目标 | Python 来源 | 说明 |
|---|---|---|
| `internal/agentcore/context_engine/processor/compressor/recall/bm25.go`（+`doc.go`） | `core/.../forked/compressor/recall/bm25.py` | `normalizeMarkdown`/`tokenize`/`BM25Index.build/scores`/`rankBM25`（`k1=1.5, b=0.75`） |
| `internal/agentcore/foundation/tool/exposure.go` | `core/foundation/tool/exposure.py` | `ToolExposure`（`direct`/`deferred`） |
| `internal/agentcore/harness/tools/tool_discovery/bm25.go` | `harness/tools/tool_discovery/bm25.py` | `BM25ToolIndex`：`build`/`fingerprint`/`toolFingerprint`/`search`（精确名边界优先） |
| `internal/agentcore/harness/prompts/tools/tool_search.go`、`tool_call.go` | `harness/prompts/tools/*.py` | 两卡文案（cn/en），一比一 |

### 4.2 改写（一对一，不拆文件）

| Go | Python | 要点 |
|---|---|---|
| `foundation/tool/base.go` | `base.py` | `ToolCard` 增 `Exposure` + 私有 `exposureDeclared`（三态）+ `Get/SetExposureDeclared` |
| `single_agent/ability/ability_manager.go` | `ability_manager.py` | `registryRevision`/`SetToolExposurePolicy`/`applyToolExposurePolicy`/`RegistryRevision()`；`executeTool` 按 acceptor 注入 cbc |
| `harness/tools/tool_discovery/tool_search.go` | `tool_search.py` | 名 `tool_search`；入参 `query`/`limit`；默认 5、clamp 1..20；`RenderForLLM` 输出精确 JSON schema |
| `harness/tools/tool_discovery/tool_call.go` | `tool_call.py` | 名 `tool_call`；入参 `name`/`args`；`RelayedToolOutput`（渲染文本走私有字段）；中断透传 |
| `harness/prompts/sections/progressive_tool_rail.go` | 同名 357 行 | 规则正文**全文替换**；新增 `RenderDeferredToolCatalogSnapshot/Delta`；摘要压缩 `compactToolDescription` |
| `harness/rails/progressive.go` | `progressive_tool_rail.py` | 全量重写（§5） |
| `harness/schema/config.go` | `config.py` | 增 `ToolSearchLimit`（默认 5）；删三旧字段 |
| `harness/deep_agent.go` | `deep_agent.py` | `defaultDirectToolNames` 常量；创建/热重配时 `SetToolExposurePolicy`；startup 收尾调 `finalizeStartupAsync` |
| `harness/prompts/attachment_delivery.go` | `prompt_attachment_manager.py` | 导出 `Manager.HasHistorySnapshot(hc, sessionID) bool`（复用 `readHistoryState`） |
| `harness/rails/doc.go`、`harness/tools/tool_discovery/doc.go`、`prompts/sections/doc.go`、`context_engine/processor/compressor/doc.go` | — | 文件树同步 |

### 4.3 删除清单（同一改动内完成，规则 8）

1. `tools/tool_discovery/search_tools.go`、`load_tools.go` + 各自 `_test.go` + `doc.go` 条目
2. `rails/progressive.go`：`visibleToolsKey`、`navigationBaselineTools`、`toolGroupOrder`、`toolGroupCN`、`score*` 五常量、`buildNavigationSection(s)`、`buildNavigationEntries`、`getVisibleTools`/`setVisibleTools`/`initVisibleTools`、`maxLoadedTools`、`defaultVisible`/`alwaysVisible`
3. `prompts/sections/progressive_tool_rail.go`：navigation 文案常量 + `BuildNavigationSection`/`BuildNavigationEntry`（上游同名函数已无调用方）；`SectionToolNavigation` 常量**保留**（上游 `SectionName.TOOL_NAVIGATION` 同样保留，仅枚举对齐，无消费者）；`SectionProgressiveToolRules` 已存在，无需新增
4. `schema/config.go`：`ProgressiveToolAlwaysVisibleTools`、`ProgressiveToolDefaultVisibleTools`、`ProgressiveToolMaxLoadedTools`、`DefaultProgressiveToolMax`、`EffectiveProgressiveToolMaxLoadedTools`
5. `rails/progressive_test.go`（706 行）按新语义重写（不整删）
6. `deep_agent.go:1729` 的 `⤴️ 9.11 回填` 占位注释删除（改为真实装配）

## 5. 数据流

### 5.1 曝光策略（注册期一次定死）

```
SetToolExposurePolicy(enabled, directNames)
applyToolExposurePolicy(card):
    declared 已声明 → 原值（不改写）
    否则 name ∈ directNames → DIRECT
    否则 enabled ? DEFERRED : DIRECT
    card.Exposure = resolved; declared = true
```

- `registryRevision` 递增点对齐上游 `_mark_registry_changed` 7 处：`add` 内 tools(3)/workflows/agents(5 处) + `remove` 的 MCP 两分支。
- **上游 `remove` 移除普通工具不涨 revision**（既有事实）→ 索引正确性依赖 fingerprint 双判据（§5.5）。
- direct 名单：上游 36 项（`deep_agent.py:199-242`，已机器核对条数）一比一；Go 侧按实际注册名映射（§10 D-2）：

```
tool_search tool_call read_file write_file edit_file glob grep bash task_tool
subagent_spawn subagent_wait subagent_list subagent_send_input subagent_close subagent_resume
ask_user todo_create todo_list todo_get todo_modify skill_tool
memory_search memory_get free_search paid_search fetch_webpage
write_memory edit_memory read_memory ltm_search ltm_search_summary send_file_to_user
enter_plan_mode exit_plan_mode skill_branch_explore skill_index_build
```

### 5.2 Rail 生命周期

| 阶段 | 动作 |
|---|---|
| `Init` | 注册 `tool_search`/`tool_call`，卡片显式 `Exposure=DIRECT` + `declared=true`；保存 registry 引用；**不建索引** |
| `FinalizeStartup` / `FinalizeStartupAsync` | 先 `ListToolInfo(ctx, nil)` 物化惰性（MCP）schema，再 `rebuildToolSearchIndex` |
| `BeforeInvoke` | `cachedAllToolInfos = ListToolInfo`；`ensureInitialDeferredCatalog` |
| `BeforeModelCall` | `AddSection(rules)` → `syncDeferredToolAttachment` → 过滤 `inputs.Tools` |
| `BeforeToolCall` | direct 放行；deferred 仅放行 `nested 标记 ∧ 已授权`；未知工具拒绝 |
| `Uninit` | ownership 比对后 `Remove`；清空索引/缓存/授权映射 |

### 5.3 会话状态键（4 个，与上游同名）

| Key | 类型 | 用途 |
|---|---|---|
| `__progressive_discovered_tool_names__` | `[]string` | 已授权可 `tool_call` 的工具 |
| `__progressive_discovered_tool_fingerprints__` | `map[string]string` | 授权时 schema 指纹 |
| `__progressive_tool_discovery_trace__` | `[]map[string]any` | 搜索/调用轨迹 |
| `__progressive_deferred_tool_catalog__` | `map[string]any` | `initialized/version/delta_count/language/initial_tools/tools/initial_fingerprints/fingerprints/registry_revision` |

### 5.4 目录附件（动态目录唯一载体）

- `section=progressive_deferred_tools`、`source=progressive_tool_rail`、`kind=TOOL`、`priority=75`、`content_kind=text/markdown`
- metadata：`catalog_version` / `catalog_mode`(`snapshot`|`delta`) / `catalog_language` / `delta_count` / `catalog_tools` / `catalog_fingerprints` / `registry_revision`
- 判定顺序：会话 state → **从已有附件 metadata 恢复**（热重载/新 Session 防重复发同一 diff）→ `HasHistorySnapshot` → `forceSnapshot = 无快照 ∨ 语言变 ∨ delta_count≥10` → delta → 无变化直接返回
- 有变更时同步 `invalidateDiscoveredTools(updated ∪ removed)`
- 摘要文本：卡片 `properties["progressive_discovery"]["display"][语言]` 优先，回落 `description`；压缩到 120 字符（句末标点优先，否则 119+`…`）；按名排序渲染 `- **name**: 摘要`

### 5.5 索引（内存，per-agent）

- 存放：Rail 字段 `toolSearchIndex` / `toolSearchIndexRevision` / `toolSearchRegistry`；**不落盘、不进 session state**
- 文档集合：`Exposure == DEFERRED` 且非元工具的卡片；文本 = `name + description + parameters(JSON)` + `extraSearchText`（`display` 全语言值 + `aliases`）
- 双判据复用：`registryRevision == 记录值` **且** 全量 `fingerprint == sourceFingerprint` 才复用，否则重建
- `toolFingerprint = sha256(type,name,description,parametersText)`；索引 `fingerprint = sha256(sorted[(toolFingerprint, extraSearchText)])`
- 检索：`score>0 ∨ 精确名边界命中` → 排序 `(非精确名, -score, name, 原始序)` → 截 `limit`
- 建立时机：① startup 收尾 ② 每次 `tool_search` 前 `ensureIndex` ③ `uninit` 清空

### 5.6 执行通道

```
tool_call.Invoke(name,args):
    cbc = opts 携带的回调上下文（缺失 → error）
    result = rail.callDiscoveredTool(name, args, session, cbc)
callDiscoveredTool:
    ① 非空 ② 已授权 ③ 仍注册 ④ 仍是 DEFERRED ⑤ 指纹一致（无指纹→需重搜；指纹变→失效重搜）
    → trace(tool_call)
    → cbc.Extra[__progressive_nested_tool_call__] = true（defer 清理）
    → abilityManager.Execute(ctx, cbc, []*ToolCall{{ID:"<outer>:target", name, args}}, sess, tag)
    → 解析 ExecuteResult：失败→RelayedToolOutput(success=false, error)；无结果→error；成功→data{name, result[, multimodal]}
```

## 6. 三处 Go 适配点（与 Python 对照）

| # | Python（文件:行） | Go 落点 | 性质 |
|---|---|---|---|
| a | `tool_call.py:66 accepts_tool_callback_context=True`；`ability_manager.py:1597-1606` 按属性注入 `_tool_callback_context` kwargs；`tool_call.py:81` 读取 | 可选接口 `CallbackContextAcceptor{AcceptsToolCallbackContext() bool}` 门卫 + `tool.WithToolCallbackContext(cb ToolCallbackContext)` 选项；`tool_call` 断言回具体类型 | 语言差异（Go 无 kwargs），注入时机与 opt-in 语义一致 |
| b | `prompt_attachment_manager.py:522-536 has_history_snapshot` | `Manager.HasHistorySnapshot(hc, sessionID)` 包装现成 `readHistoryState` | 仅导出 |
| c | rail 内 `session.get_state/update_state` | `SessionFacade.GetState(stateKey)` / `UpdateState(map)`（已存在） | 无差异 |

`ToolCallbackContext` 窄接口取 `AgentCallbackContext` 已有的 `Extra() map[string]any` 与 `Session() interfaces.SessionFacade`，不新增人造方法、不引入 `any`。

## 7. 提示词与错误处理

### 7.1 提示词

- 规则正文：一比一复刻 `prompts/sections/progressive_tool_rail.py:44-108`（cn/en 两套，含「动态工具状态优先级」整段与 `tool_search` 使用规则），`priority=75`、`category=tools`
- 初始目录：`## 当前可通过 tool_search 搜索的 deferred 工具（session 初始目录）` + 摘要列表；空目录用固定文案
- snapshot：`## 当前可通过 tool_search 搜索的 deferred 工具` + `目录版本：N`
- delta：`## deferred 工具目录更新（立即生效）` + 版本 + 「仅列出变化」声明 + 新增/修改/删除三段（无变化写 `本次没有变化。`）
- **删除**：旧 navigation 节文案与构造器（上游已无调用方）

### 7.2 面向模型的错误文案（英文原文，不本地化——协议/模型契约字符串）

```
tool_call requires a non-empty tool name.
Deferred tool '<x>' must be returned by tool_search before it can be called.
Deferred tool '<x>' is no longer registered.
Tool '<x>' is not a deferred search result.
Deferred tool '<x>' must be searched again before it can be called.
Deferred tool '<x>' has changed; search it again before calling it.
Deferred tool '<x>' returned no execution result.
Unknown tool '<x>'.
Deferred tool '<x>' must be found with tool_search before it can be called.
tool_call requires an active agent callback context
```

### 7.3 日志

对齐上游：`init`/`uninit` 注册与移除失败 warn、`list_tool_info` 失败 warn、卡片转 ToolInfo 失败 warn、附件写入失败 warn、`tool_search` 调用 info（query/limit/match_count/matched）、`tool_call` 失败 warn。上游 `before_model_call` 里 3 处带 `[DEBUG]` 前缀的 `logger.info` 在 Go 侧降为 `Debug`（理由与回收条件见 §10 D-3）。

## 8. 测试方案

### 8.1 单元测试（改动行 ≥85%）

| 包 | 用例重点 |
|---|---|
| `foundation/tool` | 枚举值与零值默认；`Exposure` 默认；`exposureDeclared` 三态 |
| `single_agent/ability` | 策略四分支矩阵；declared 后共享卡二次注册不被改写；`registryRevision` 7 处递增点；**移除普通工具不涨 revision**；acceptor 门卫只在 true 时注入选项 |
| `context_engine/.../recall` | `tokenize`（中英混排、下划线拆分、CJK 单字+双字）；`normalizeMarkdown`（frontmatter/fence/link/heading/list）；打分（k1/b、空查询、空索引、`averageLength<=0`、单文档） |
| `tool_discovery`（bm25） | `fingerprint` 与参数 key 顺序无关；参数变更必变；`extraSearchText` 参与；精确名边界（`search_skill` 不命中 `search_skill_extra`、中文包夹命中）；排序与 limit |
| `tool_discovery`（tool_search） | limit 归一化（非法→5、0→1、100→20）；空 query；渲染精确 JSON；失败 `success=false` |
| `tool_discovery`（tool_call） | 缺参报错；acceptor 为 true；`RelayedToolOutput` 只转发渲染文本；成功/目标失败/无结果三分支；中断透传 |
| `prompts/sections` | 摘要压缩三档；排序；空目录文案；snapshot 版本；delta 三段与「无变化」 |
| `prompts` | `HasHistorySnapshot` 首轮 false → 快照后 true → 会话隔离 |
| `rails`（重写） | 目录收集（仅 deferred、排除元工具、display 回退）；diff 四类；首轮未变不发、首轮变了 delta、三类 forceSnapshot；附件 metadata 恢复；变更失效授权；授权表合并/裁剪指纹；trace；五道校验文案；`beforeToolCall` 四分支；顶层过滤；uninit ownership |
| `harness/schema` | `ToolSearchLimit` 默认 5；旧配置键被忽略 |

### 8.2 集成测试（`tests/integration/agentcore/harness/progressive_tool/`，`//go:build integration` + `suite.Run`）

跨子系统：`harness(rails/deep_agent)` + `single_agent(ability/interfaces)` + `session(state)` + `context_engine(recall.bm25)` + `foundation/tool`（≥3，满足 audit 一层）。

| 编号 | 用例 | 断言（可观测契约） |
|---|---|---|
| IT-1 | 曝光映射到提示词 | 卡片 `Exposure` 逐项正确；模型实际收到的 `tools` 只含 meta∪direct；系统提示词含规则正文 + 初始目录 3 行且摘要已压缩 |
| IT-2 | 目录变更推送 | 新增/改描述/移除 → 附件 delta（三段齐全、版本 +1）；切语言 → snapshot；连续 10 次 delta → 第 11 次 snapshot |
| IT-3 | `tool_search` 真检索 | 经 `AbilityManager.Execute` 调用；只命中 deferred；改参数后命中新 schema；精确名优先于高分；授权名单+指纹落 session state |
| IT-4 | `tool_call` 真执行 | 目标 LocalFunction 执行计数 1；目标工具挂拒绝型 rail 验证嵌套仍走 rail 链；渲染文本一致；multimodal 提升到外层 |
| IT-5 | 负例四连 | 未搜索即调 / 搜索后移除 / 搜索后改 schema / 用 direct 名走 `tool_call` → 各自拒绝文案 |
| IT-6 | MockLLM 全轮次闭环 | 请求#1 发 `tool_search`、#2 发 `tool_call`、#3 final；断言 #1 的 `tools` 无 deferred、#2 的 messages 含完整 schema、目标工具执行 1 次、最终答案正常 |
| IT-7 | 同会话复用 | 新 rail 实例 + 同 session：不重复推送同一 delta；授权仍在 → 仍可 `tool_call` |

### 8.3 门禁

- `go test -race`；改动行覆盖率 ≥85%（逐包）
- `go run ./cmd/integration-audit -verify` 通过（新目录预计 5 个子系统，无需豁免）
- mock 只在边界：LLM 用 MockLLM，工具用真实 `LocalFunction`

### 8.4 回填锚点断言

```
grep -rn "search_tools\|load_tools" internal/ | grep -v mcp_search      → 空
grep -rn "ProgressiveTool\(AlwaysVisible\|DefaultVisible\|MaxLoaded\)"  → 空
grep -rn "BuildNavigationSection\|BuildNavigationEntry"                → 空
grep -rn "延迟工具目录.*未移植"                                          → 空
IMPLEMENTATION_PLAN.md 含 9.11a、5.32 行；9.11 行有弃用标注；D-18 状态推进
```

## 9. 计划同步

| 位置 | 变更 |
|---|---|
| 领域九 `9.11` | 原文保留 + 状态列追加 `⚠️ 旧语义已由 9.11a 取代` |
| 领域九 `9.11a` | 新增：ProgressiveToolRail 重写（deferred 目录）——exposure / BM25 / tool_search / tool_call / 目录附件 / 嵌套执行 / 删除清单 |
| 领域五 `5.32` | 新增：`recall/bm25` 落地（BM25Index + tokenize），供 tool_discovery 与后续压缩召回共用 |
| `docs/sync/defect-plan-2026-11-04.md` D-18 | 🔍 → 处置说明（同批提交） |

## 10. 已知偏差

| # | 偏差 | 理由 | 回收条件 |
|---|---|---|---|
| D-1 | `recall/bm25.go` 落在 `processor/compressor/recall/`，上游在 `processor/forked/compressor/recall/` | Go 未建 `forked` 层（5.22–5.26 已按非 forked 家族对齐，forked 侧为 65 行薄壳）；为单一模块新建 `forked/` 会让目录结构误导 | 若后续补 forked 层（session_memory_compressor 等），同步迁移 |
| D-2 | direct 名单按 Go 实际注册名映射：上游 `subagent_spawn/wait/list/send_input/close/resume` → Go `sessions_spawn/sessions_list/sessions_cancel`；上游未实现的 `skill_branch_explore`/`skill_index_build`/`send_file_to_user`/`ltm_search*` 保留原字符串 | 工具改名属独立对齐议题，本项不越界；未注册名字符串对策略无副作用 | 工具名对齐章节落地后，映射表收敛为纯上游名单 |
| D-3 | 上游 `before_model_call` 的 3 处 `[DEBUG]` 前缀 `logger.info` 在 Go 降级为 `Debug` | 生产噪音，内容与上游一致 | 用户要求逐字对齐日志级别时改回 `Info` |
| D-4 | `ToolCallbackContext` 用窄接口 + 一次具体类型断言，替代 Python kwargs 注入 | Go 无 `**kwargs`，且 `foundation` 不得反向依赖 `single_agent` | Go 提供官方 tool 上下文通道时替换 |
| D-5 | Go 独有 harness 工具（`code`/`list_files`/`cron`/`switch_mode`/`coding_memory_*`/多模态）不在上游 direct 名单，开启渐进后会被判 deferred | 保持与上游策略一致；渐进开关默认关闭，且当前无开启点 | 若某 adapter 开启渐进后发现关键工具不可直调，按需补入名单并注明来源 |

## 11. 风险

| 风险 | 影响 | 对策 |
|---|---|---|
| `remove` 不涨 revision（上游既有） | 索引残留已删工具 | fingerprint 双判据兜底；IT-5 覆盖"搜索后移除" |
| 新 Session/热重载丢 state | 同一 diff 重复推送 | 从附件 metadata 恢复（IT-7） |
| `tool_call` 嵌套执行绕过目标工具 rail | 权限/超时失效 | 一律走 `ability_manager.Execute`；IT-4 用拒绝型 rail 验证 |
| 渐进开关被误开且名单不全 | 关键工具不可直调 | D-5；IT-1 把"哪些是 direct"变成显式断言 |
