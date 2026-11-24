# 16.4 技能发现与索引（Symphony Discovery）设计

> 2026-11-24 brainstorming 收口。分 16.4a（retrieval infra 前置切片）+ 16.4b（discovery 主体 + harness 接线）两批实施。
> Python 真相源：`agent-core/openjiuwen/symphony/discovery/`（3,225 行）+ `jiuwenswarm-develop/jiuwenswarm/agents/harness/common/{rails/skill_retrieval_prompt_rail.py,tools/skill_retrieval_toolkits.py}`（582+651）+ `interface_deep.py` 消费段 + `server/runtime/skill/skill_manager.py`（禁用名单消费段，Go 已有）。
> 语义实证基线：9.11b spec（2026-11-23-retrieval-rails-9.11b-design.md）§79-81 七项实证 + 本 spec §5 运行时语义。

## 1. 范围与批次

### 16.4a — retrieval infra 前置切片（~1,600 行）

discovery 触达的 retrieval 基础设施层，按规则 9 一比一移植；未来 16.5 直接复用，不算拆半。

| Go 落点（`internal/agentcore/symphony/retrieval/`） | Python 来源 | 行数 |
|---|---|---|
| `common/models.go` | `retrieval/common/models.py`（RetrieverItem / RetrieverNode） | 68 |
| `build/scanners_common.go` | `build/scanners/common.py`（find_skill_file / first_paragraph / read_skill_file / sha256_file） | 150 |
| `build/io/`（6 文件一比一：tree/items_jsonl/manifest/catalog/config_loader/doc） | `build/io/`（load_tree_preset 等） | 388 |
| `search/artifacts/`（loading/merge/doc） | `search/artifacts/`（CatalogRecord / build_tree_root / load_retriever_index） | 325 |
| `search/runtime/lexical.go` | `search/runtime/lexical.py`（LexicalIndex / LexicalDocument / compile_matcher） | 704 |

### 16.4b — 主体（~4,500 行）

| Go 落点 | Python 来源 | 行数 |
|---|---|---|
| `internal/agentcore/symphony/discovery/`（config/models/skillfs/toolkit/directory_toolkit 5 文件一比一 + doc.go） | `symphony/discovery/` | 3,225 |
| `internal/swarm/agents/harness/common/rails/skill_retrieval_prompt_rail.go` | jiuwenswarm `rails/skill_retrieval_prompt_rail.py` | 582 |
| `internal/swarm/agents/harness/common/tools/skill_retrieval_toolkits.go` | jiuwenswarm `tools/skill_retrieval_toolkits.py` | 651 |
| DeepAdapter 接线段（toolkit 懒创建 / session_profile load+persist+discard / rail 装配） | `interface_deep.py`（:2497-2676, :4283-4335, :7120-7191, :11137-11151） | ~300 |
| （⤵️16.5）SkillTaxonomyRuntime | jiuwenswarm `runtime/symphony/skill_retrieval/runtime.py` | 154 |

## 2. 流程位置（agent 会话中的角色）

16.4 是 symphony 的入口数据源：磁盘技能目录 → 结构化 live 目录 → 提示词注入 + `skill_index` 渐进式检索工具。16.5 检索（BM25/embedding/Agentic）的输入目录来自本节；16.6 编排候选来自检索。**依赖方向实证：16.5 不依赖 16.4；16.4 依赖 retrieval 域 infra 层（16.4a 切片解决）+ 1 个跨界函数（D-16-4-2 复刻解决）。**

## 3. 运行时语义（实施必须逐条落地）

1. **懒创建**：DeepAdapter `_get_or_create_skill_retrieval_toolkit`（interface_deep.py:7165-7191）第一次需要技能能力时构造 toolkit 并缓存；构造完立刻 persist session_profile（:7187）。
2. **多根扫描**：live 目录 = `get_agent_skills_dir()` + 本会话选中的 MCP 捆绑技能目录（仅 session-scoped 子 adapter；根 adapter 恒单根防泄漏，:10200-10236）。双目录设计：`index_skill_directories`（共享 taxonomy 构建源）恒为 agent skills dir 单根；MCP 会话技能进 live 不进共享索引。
3. **扫描判定**（spec §79-80 七项实证照落）：SKILL.md/skill.md/Skill.md 三变体；隐藏/`_` 前缀/disabled 排除；worker_id = 目录名经 canonical（控制字符转 ~uXXXX、空名回落 unnamed，models.py:414-423）；description 三级回落（frontmatter → 正文首段 ≤500 → name）；source = 调用方映射缺省 "local"；多根同名先扫优先去重 + `_with_unique_worker_ids` 后缀消解。
4. **策略推导四态**（skillfs.py:476-486）：small（估 token < context_window × 1%）/ indexed（tree+fresh）/ indexed-stale（tree+stale）/ large-flat。large-flat 裁剪 = prompt_preferred_skills 优先 + 截 max_list_entries（skillfs.py:641-646），非 token 动态截断。
5. **布局与索引**（skillfs.py:607-636）：`_refresh()` 现扫 + `capture_index_snapshot(index_root)` 读磁盘 taxonomy；index_state = missing/fresh（record_hashes 相等）/stale。layout 是冻结时刻磁盘事实：无索引恒 flat，本轮将就，下轮改善。分支行展开规则：全目录子节点继续下钻（label ` > ` 连接），混技能停层，总量受 max_list_entries（:501-525）。**indexed-stale 照样给分支行**（不要求 fresh）。
6. **冻结点**：rail 构造函数（skill_retrieval_prompt_rail.py:114-127）取 `toolkit.frozen_prompt_snapshot`（或 SkillFS prompt_snapshot，失败空快照兜底）。冻结后整轮不变；before_invoke 纯渲染消费（remove 旧 section → 渲染 → append），不做扫描/推导。
7. **工具三操作**（directory_toolkit.py）：list（树布局分支行+翻页）/ search（indexed-fresh 走 taxonomy LexicalIndex，否则对现扫临时建 flat 索引降级）/ read（单技能正文）。游标 _MAX_CURSORS=32，`[next]` footer JSON，`[truncated]` 注脚。
8. **漂移通知（拉取式，双入口）**：全局 notice 表 `_STATES`（toolkit.py:94-95，模块级 OrderedDict，LRU ≤1,024，key=`tool_id(session_scope, tool_name)`）存**基线全量**非增量。
   - 入口 A：skill_index 工具执行前 refresh 现扫 → 自有 IncrementalNoticeSession（directory_toolkit.py:193-196）比对 → reminder 拼进**自己的输出**（:420-424，超 output_budget 放弃本次投递）。
   - 入口 B：rail after_tool_call（工具名≠skill_index，rail :265-282）→ `_scan_selection_cards()`（refresh=True 现扫）→ 模块级 `consume_incremental_skill_reminder` 比对 → `_append_tool_result_reminder` 追加进**触发钩子的那个工具**结果（output/result 已知键优先 → system_reminder 键兜底，:291-308，防御异构结果形状）。
9. **基线推进判定**（toolkit.py:60-67，prepare() finally 必跑）：acknowledged=True 或无 added → 基线全量推进；否则基线扣掉 added（待下轮重试）。added=本轮差集数据；acknowledged=投递成功标记（渲染成功才置位）。通知投递失败不丢失。
10. **性能**：五层缓存（models.py:23-35）——_SKILL_FILE_CACHE（mtime 签名，2,048 LRU）/ record / root layout / inventory single-flight + last / normalized。全扫成本 = walk + 每文件 stat，命中缓存毫秒级。
11. **恢复链**（adapter 层，interface_deep.py:4283-4335）：sidecar 读 → 五重校验（大小/schema_version=1/checksum/session_id/payload canonical）→ 过则 frozen_profile 传构造跳过推导；不过则降级 dict（enabled=False + recovery_error）不中断会话，toolkit 全量重扫。toolkit 层防御性二次校验（is_valid_skill_retrieval_session_profile）。sidecar 内容与格式见 9.11b spec §79 + toolkits:538-560（12 键 + selection_cards/prompt_snapshot/index_snapshot 全量）。原子写：os.replace、0o600、≤16MB、SHA-256 checksum。**sidecar 是恢复基线非真相源**。
12. **失败兜底**：rail 扫描异常（磁盘 IO）→ log warning 静默放弃本轮通知，绝不影响工具调用；toolkit 构造失败 → rail 空快照；taxonomy 构建失败 → 会话留冻结降级（preserve_previous_index_on_failure 语义随 16.5）。

## 4. 偏差登记（已拍板）

| # | 偏差 | Python 位置 | Go 位置 | 回收条件 |
|---|---|---|---|---|
| D-16-4-1 | S3 裁剪：storage 只移植本地路径分支，S3 URI 分支不实现 | `symphony/shared/storage.py`（299 行） | 16.4a 各加载函数仅本地路径 | storage 域实施 S3 时回填 |
| D-16-4-2 | `_recover_index_directory_unlocked` 偏差登记复刻（单函数跨界，不动 16.5 领地文件） | `symphony/agent/retrieval_toolkit.py`（skillfs.py:656 引用） | 复刻进 discovery 包内 | 16.5 实施 retrieval_toolkit.go 时收口去重 |
| D-16-4-3 | notice 全局表：模块级 OrderedDict + LRU 1,024，Go 用包级 map+mutex 等价实现，key=tool_id(session_scope, toolName) | `discovery/toolkit.py:94-95` | discovery/toolkit.go | 无需回收（等价形态） |
| D-16-4-4 | 五层扫描缓存：Go 用带锁 LRU + single-flight 等价实现，逐层保留 mtime 签名校验语义 | `discovery/models.py:23-35` | discovery/models.go | 无需回收（等价形态） |
| D-16-4-5 | SkillTaxonomyRuntime ⤵️16.5：构建 SDK 本体 AgenticSkillRetrievalToolkit（symphony/agent/retrieval_toolkit.py ~1,000 行，含 LLM 聚类/build worker/发布锁）属 16.5 领地；16.4b 交付 flat 渐进式全链，indexed 两态代码路径照写（无索引恒 None 分支有单测） | `runtime/symphony/skill_retrieval/runtime.py`（154） | ⤵️16.5 | 16.5 落地后 indexed/indexed-stale 全链打通 |
| ~~D-16-4-6~~ ✅ 已回收（2026-11-24 用户拍板对齐上游）：`internal/swarm/server/runtime/mcp/state_store.go`（state.json 读取侧全量：_load BOM 容忍/版本默认/list_connected/truly/connecting/tui_enabled/registered/connected_mcp_skill_dirs/_save 原语；写侧 upsert/remove/set_state/mounts 消费方=MCP 连接章节 ⤵️10.3.31g）+ `SkillManager.McpSkillsDirs`（对齐 _mcp_skills_dirs :9341）+ adapter `_skill_scan_dirs` MCP 会话分支（selected ∪ pending 过滤，:10200-10236）+ `SetSessionSelectedMcp` 入口（reconcile 写入侧 ⤵️）。单测 state_store 六例 + skill manager 两例 + adapter 分支例；集成 TestMcpBundledSkillVisibility | `skill_manager.py:9341-9352` + `mcp/state_store.py:225-246` | 已落地 | — |
| ~~D-16-4-7~~ ✅ 已回收（2026-11-24）：`SkillManager.ReloadState()` 对齐 reload_state（skills_state.json 重读；多实例写者同步点）；adapter live provider 现查前调 reload。单测 TestReloadState_外部写者落盘后重载生效；集成 TestDisabledSkillPerception（外部写者禁用 → live 现查立即生效） | `skill_manager.py reload_state` + `interface_deep.py:7120-7145` | 已落地 | — |
| ~~D-16-4-8~~ ✅ 已回收（2026-11-24）：`workspace.RuntimeStatePath(sid)` 对齐 get_runtime_state_path（utils.py:1758-1765 per-session 派生+净化截断），sidecar 路径按 Python :2474-2483 逐字派生（runtime_path 改缀）。单测 TestSkillRetrievalProfilePath_RuntimeStateDerivation。注：runtime_state.yaml 本体仍为全进程单文件（既有 D-8，属 runtime_prompt 域，不在本批） | `utils.py:1758-1765` + `interface_deep.py:2474-2483` | 已落地 | — |
| D-16-4-9 | 集成缝三条纯属性注入：`SetSessionScopedForTest/SetSkillRetrievalSessionEnabledForTest/SetSkillRetrievalToolkitForTest`——Python 集成测试直接戳实例属性，Go 跨包不可见；均为最小注入面（生产入口=懒创建/RestoreSkillRetrievalSession）。冗余缝已修剪（ScanSelectionCardsForTest 删除，走 `toolkit.Environment().SelectionCards(true)`）；`LoadSkillRetrievalSessionProfile/SkillRetrievalProfilePath` 转正为导出 API | Python 集成用例属性触达惯例 | adapter deep_adapter_sessions.go 集成缝段 | 无需回收（等价形态） |

## 5. 回填台账（一条不丢）

| 回填项 | 来源 | 处理 |
|---|---|---|
| `SwarmBuildContext.SkillRetrievalToolkit` any 别名收窄 | 15.16c 挂账 | 16.4b：`buildctx/context.go:161` any → 具体类型 |
| skill 侧三件（discovery + rail + toolkits） | 9.11b 批一改挂 | 16.4b 本体 |
| session_profile 持久化（五重校验/恢复降级） | 9.11b | 16.4b DeepAdapter 段 |
| V-6 skill 类绑定（skill_index 工具装配期身份锚点） | 9.11b | 16.4b |
| `swarm.skill_retrieval_prompt` 工厂桩闭合 | 9.11b | 16.4b：`member_rails.go:209-212` 桩转真身 |
| `swarm.skill_retrieval` 工厂桩闭合 | 9.11b | 16.4b：`tools.go:257-282` BuildSkillRetrieval 转真身 |
| SkillTaxonomyRuntime + taxonomy 构建 SDK | 本 spec D-16-4-5 | ⤴️16.5（IMPLEMENTATION_PLAN 16.5 行已登记） |
| 15.20③ symphony 三件 rail（orchestration_rail 1,149 + retrieval_context_processor 240 + tool_stream_events 124） | 批二登记 | **不在本批**——挂 16.1，本 spec 仅登记互引 |

## 6. 删除清单（规则 8/13）

16.4b 闭合桩时同改动删除：
- `member_rails.go` 内 `swarm.skill_retrieval_prompt` 桩体与 ⤴️16.4 注释（转真身后）
- `tools.go` `BuildSkillRetrieval` 桩体与 ⤴️16.4 注释
- `buildctx/context.go` `SkillRetrievalToolkit any` 字段类型收窄后旧注释锚点
- 全仓 grep 锚点验收：`⤴️16.4` / `⤴️ 16.4`（skill 侧语义）0 命中

## 7. 测试策略（讨论过的每个点均须单测+集成覆盖）

### 16.4a 单测
- tree preset 往返（load_tree_preset 读/写、normalize_item_paths、sort_tree_nodes）
- scanners_common：find_skill_file 三变体、first_paragraph 限长、read_skill_file frontmatter 解析、sha256_file
- lexical：LexicalIndex 建/查、compile_matcher 语法、翻页
- artifacts：build_tree_root、load_retriever_index 本地路径、merge
- S3 分支缺失路径：URI 输入 → 明确错误（钉住 D-16-4-1 行为）

### 16.4b 单测（t.TempDir 真实磁盘）
- 扫描判定：SKILL.md 三变体 / 隐藏与 `_` 前缀 / disabled / 多根同名去重 / worker_id canonical（~uXXXX、unnamed）/ description 三级回落 / `_with_unique_worker_ids`
- 策略四态推导：small 阈值（context_window × 1%）、large-flat 裁剪（preferred 优先 + max_list_entries）、layout=flat 恒定（无索引）、capture_index_snapshot None 分支（D-16-4-5 钉住）
- 分支行展开：全目录下钻 ` > ` 连接、混技能停层、max_list_entries 封顶
- 冻结语义：rail 构造期冻结、冻结后 refresh 不变、空快照兜底
- skill_index 三操作：list 翻页游标（_MAX_CURSORS）/ search flat 降级 / read 正文；`[next]`/`[truncated]` footer
- notice 双入口：skill_index 内部拼进输出（output_budget 超限放弃）/ rail after_tool_call 追加（output→result→system_reminder 三级适配）/ 非 skill_index 工具触发
- 基线推进：acknowledged 推进 / 失败保留 added 重试 / 渲染异常不投递不改基线推进方向
- 五层缓存：mtime 签名命中/失效、single-flight 并发去重
- session_profile：12 键往返、五重校验逐项失败路径、降级 enabled=False+recovery_error、原子写（0o600/≤16MB）
- legacy 隐藏：list_skill abilities remove/restore、原生 SKILLS section 暂存恢复

### 集成（规则 7 门禁）
- 真链 DeepAdapter → toolkit 懒创建 → SkillFS 真扫 → rail before_invoke 注入 → agent 循环调 skill_index → after_tool_call 通知 → 恢复链 sidecar 五重校验，跨 ≥3 子系统
- 渐进式闭环断言：无索引环境下 small_full / large-flat / skill_index 翻页 / 漂移通知全链可用

### 回归
`CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./...` 零新增 FAIL。

## 8. 已知范围外

- 16.5：检索编排（BM25/embedding/Agentic）、taxonomy 构建 SDK、SkillTaxonomyRuntime
- 16.1：symphony 三件 rail（15.20③）
- 16.6+：编排/执行/经验沉淀
