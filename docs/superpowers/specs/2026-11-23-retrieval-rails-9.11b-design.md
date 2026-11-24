# 9.11b 检索工具 Prompt Rails 设计（批一：tool 侧 + R-5 + V-6 四类；skill 侧 ⤴️16.4；heartbeat ⤴️9.11c）

> 日期：2026-11-23。流程：brainstorming 定案 → writing-plans。
> Python 真相源：`/home/opensource/jiuwenswarm-develop/jiuwenswarm/agents/harness/common/{rails,tools}/`、
> `server/runtime/agent_adapter/interface_deep.py`（DeepAdapter 接线段）、
> `server/runtime/skill/skill_manager.py`（session_profile 持久化，⤴️16.4）。

## 1. 流程位置与作用

每轮 DeepAgent 调用：

1. **before_invoke**：SkillRetrievalPromptRail（⤴️16.4）隐藏 legacy 入口（本节不涉及）。
2. **init / 首次 before_model_call**：ToolRetrievalPromptRail 冻结目录（静态分类表 ∩ live 注册表），向系统提示词注入「Deferred 工具（渐进式披露）」section。
3. **before_model_call**：`inputs.tools` 过滤为 direct ∪ {tool_search, tool_call}，deferred 工具从模型可见 tools 数组隐藏；目录漂移检测（指纹比对）→ delta/snapshot 附件（≥10 次 delta 兜底全量快照，复用 9.11a 的 `render_deferred_tool_catalog_delta/snapshot` 渲染器）。
4. **模型按需检索**：`tool_search`（list 分类导航 / get_schema 精确 schema）→ `tool_call` 代理执行 deferred 工具。
5. **before_tool_call**：9.90d-2 readonly_tool_bindings 消费本节锚点（V-6）——命中只读绑定免 LLM 审查，fail-closed 兜底。

**deferred 判据** = 执行通道（不在 API tools 数组、须经 tool_call 中转），非可见性（提示词目录文字可见但不可直接调用）。

## 2. 核心语义（实现必须逐条对应）

### 2.1 单一所有者

ToolRetrievalPromptRail 与 9.11a ProgressiveToolRail 注册**同名元工具**（tool_search/tool_call）。启用时 DeepAdapter 从 `_pending_rails` 剔除内置 ProgressiveToolRail（日志 "dropped bundled ProgressiveToolRail; ToolRetrievalPromptRail will own tool_search"）；build 失败返回 nil 时**保留**内置 rail 兜底（spec-reload reconcile 语义：绝不让会话裸奔无披露层）。开关关闭走回内置 rail。

### 2.2 静态分类表与 live 注册表

- `_TOOL_CATEGORIES`：in-code 静态表（~15 个分类字面量，path/description/tools/description_en 四字段），新工具 = 改代码或走兜底。
- live 注册表 = AbilityManager 运行时 deferred 工具集合。渲染时以 live 为准：静态表名字不存在则跳过；live 新增未覆盖工具走 uncategorized 块（name+压缩描述 ≤120 字符）或 `mcp_{server}` 虚拟分支。
- 冻结时机：init 一次 + 首次 before_model_call 再冻结一次（注册表那时才齐）；语言切换（cn/en）重渲染 section（remove+add，仅动本 section）。
- 会话中途漂移**不回改 section**，走 delta/snapshot 附件。

### 2.3 MCP connector 虚拟分支

MCP 工具命名契约 `mcp_{server}_{tool}`；server 名最长前缀匹配（已知 server 清单来自 `mcp_servers_provider` 运行时回调），拆不出退化取第一段；`mcp_x` 裸名不构成 server 限定，跳过。描述来自 provider 配置（`{"zh","en"}`，纯串双语共用），未配置给兜底文案（"MCP 连接器 {server} 提供的工具"）。每 server 一行目录 + list 可导航（`_render_connector_listing`）。

### 2.4 tool_search 检索算法

- 操作集：`list`（category 必带，或 keyword 全局搜索；裸 list 抛 ValueError 教学文案）+ `get_schema`（tool 精确渲染 ToolCard）。`search` 别名与空 operation 降级为兼容路径；退役字段（call_goal/cursor/query/tools）防御性 pop，旧快照长会话降级不报错。
- BM25（复用 9.11a `BM25ToolIndex`，k1=1.5/b=0.75）：语料 = 单分类工具集或全量注册表，**每次调用重建**；文档文本 = 工具名 + `discovery_description`（双语）+ 中文别名（`tool_discovery_catalog.BUILTIN_TOOL_DISCOVERY`）+ `discovery_search_text`；查询 token 丢单字符（CJK unigram 噪声）；精确名命中优先第一、score>0、索引 tiebreak。
- list 整类返回无分页（token 有界由成员数有限 + 120 字符压缩保证）。

### 2.5 V-6 绑定回填（四类）

`readonly_tool_bindings` 现仅 ProgressiveToolRail 一类。本节补 4 类锚点（Python `_auto_permission/readonly_tool_bindings.py` 对应）：

1. **tool_retrieval**：tool_type=LocalFunction 等价物 + rail_type=ToolRetrievalPromptRail 归属校验；
2. **skill_retrieval**：⤴️16.4（依赖 SkillRetrievalPromptRail 存在）；
3. **cron**：backend 必须是 `_RestrictedCronBackend`（`type(backend) is` 严格判定）且方法闭包与 `_CronToolsCronBackend` 对应方法一致（`_methods_match`/`expected_owner` 校验）；**本节须新增 Go `_RestrictedCronBackend` 只读变体**（cron_runtime.go 已有 CronToolsCronBackend 基盘）；
4. **heartbeat**：⤴️9.11c（依赖 HeartbeatRuntimeBridge）。

任何一处身份存疑 → 返回 nil → fail-closed 走正常 LLM 审查（不新增绕过面）。

### 2.6 DeepAdapter 接线 + R-5

- `deep_adapter.go` 补 `_build_tool_retrieval_prompt_rail` 等价段（toolkit 懒创建 + pending rails 队列 + 内置 rail 丢弃 + 失败 nil 兜底），开关 = `isToolRetrievalEnabled`（= progressiveToolEnabled && progressiveToolOptimize，双默认 False，config.py:64/68）。
- **R-5 回填**（`deep_adapter_sessions.go:240` ⤵️ 缝，Python interface_deep.py:4214-4232）：tool_retrieval 开关翻转 → 会话 child 重建判据。

## 3. 批一范围（Python→Go 文件映射，规则 9）

| Python | 行数 | Go 落点 |
|---|---|---|
| `common/rails/tool_retrieval_prompt_rail.py` | 1,086 | `internal/swarm/agents/harness/common/rails/tool_retrieval_prompt_rail.go` |
| `common/tools/tool_retrieval_toolkits.py` | 1,030 | `internal/swarm/agents/harness/common/tools/tool_retrieval_toolkits.go` |
| `common/tools/tool_discovery_catalog.py` | 187 | `internal/swarm/agents/harness/common/tools/tool_discovery_catalog.go` |
| cron `_RestrictedCronBackend`（cron_runtime.py:937-1067 段） | ~130 | `internal/swarm/agents/harness/common/tools/cron/cron_runtime.go` 增补 |
| readonly_tool_bindings 四类扩展 | — | `internal/swarm/agents/harness/common/rails/permissions/auto_permission/readonly_tool_bindings.go` |
| interface_deep.py 接线段（:7270-7390）+ :4214-4232（R-5） | ~230 | `internal/swarm/server/adapter/deep_adapter.go` + `deep_adapter_sessions.go` |
| `common/config.py` is_tool_retrieval_enabled（:469-479） | ~15 | config 层对应助手 |

已存在复用：9.11a progressive.go（目录渲染器/元工具基座）、tool_discovery/bm25、cron_runtime.go CronToolsCronBackend、providers 两工厂桩（member_rails.go:213 / tools.go:280 注释同步回收 ⤴️9.11b 标记）。

**删除清单（规则 8/13）**：R-5 ⤵️ 缝注释、`buildSkillRetrievalPromptRail`/`BuildSkillRetrieval` 桩内 ⤴️9.11b 注释改指 16.4（桩本体保留待 16.4）、tool 侧新文件无旧壳。

## 4. 后移与新节登记

- **skill 侧三件 ⤴️16.4**：`symphony/discovery/`（models/skillfs/directory_toolkit/toolkit/config，3,225 行）+ `skill_retrieval_toolkits.py`（651）+ `skill_retrieval_prompt_rail.py`（582）+ session_profile 持久化（skill_manager 消费段）+ V-6 skill 类绑定 + `SwarmBuildContext.SkillRetrievalToolkit` any 别名收窄 + `swarm.skill_retrieval_prompt`/`swarm.skill_retrieval` 两工厂桩闭合。理由：skill 侧强依赖 16.4 的 discovery 基础件，提前实现等于把 16.4 拆半，单批 8,600+ 行超载。15.20② 登记改指向 16.4。
- **heartbeat ⤴️9.11c（新节，☐）**：HeartbeatRuntimeBridge（code/rails/heartbeat/tools.py 312 行）+ code/rails/heartbeat/ 子系统（5,145 行）。收口点：heartbeat_rail.go `HeartbeatTool{Card any, Tool any}` 双 any 收紧为 `*tool.ToolCard`/`tool.Tool`、heartbeatToolNames 按 HEARTBEAT_TOOL_NAMES 填充、Uninit AbilityManager remove 接线、V-6 heartbeat 类绑定。
- skill 侧语义差异（供 16.4 spec 撰写参考）：磁盘目录扫描（非静态表）、`skill_index` 单元工具（SkillTool 执行面保留）、隐藏 legacy `list_skill`/`list_skills` abilities（remove 暂存 + 恢复）与原生 `SectionName.SKILLS`（暂存 + 恢复）、四策略（small_full/large_flat/indexed/indexed_stale，legacy=不挂载）、候选 token 预算 = 上下文窗口 × 1%、可选后台 taxonomy 构建（SkillTaxonomyRuntime，失败回退冻结）、session_profile 12 键持久化（skill_manager 消费，恢复基线非真相源，live 扫描为准）。
- skill 侧补充实证（16.4 需逐条落）：①策略推导在 SkillFS（skillfs.py:479-486），rail 只消费冻结 snapshot，不参与推导；②large_flat 裁剪 = `prompt_preferred_skills` 优先保留 + 截 `max_list_entries`（skillfs.py:641-646），非 token 动态截断，被裁技能经分支行 + skill_index 翻页到达；③"布局是树" = 技能目录嵌套结构 + taxonomy 索引（tree_index.yaml）fresh，branches = 子目录分支行（path/label/description）；④profile 序列化者是 **DeepAdapter**（非 rail）：`_persist_skill_retrieval_session_profile` 调 toolkit.session_profile() + 会话级字段 + SHA-256 checksum，sidecar JSON 原子写（os.replace、0o600、≤16MB）；persist 调用点 interface_deep.py:4335（create_instance 恢复流程后）与 :7187（toolkit 构造完）；⑤恢复链五重校验（大小/schema_version/checksum/session_id/payload canonical），任一不过 → 降级 dict（enabled=False + recovery_error）不中断会话，toolkit 走全量重扫；通过 → frozen_profile 传构造（:7180）跳过重新推导；⑥聊天中途漂移通知走 rail 内存态（consume_incremental_skill_reminder，after_tool_call 消费），sidecar 只在构造/恢复两时机使用；⑦指纹独用场景：恢复时 pinned_index_revision 对比磁盘索引 fingerprint → 变了 → indexed_stale。

## 5. 测试要求

- 单测：rail 过滤/注入/漂移附件/tool_search 双操作/BM25 排序/退役字段降级/错误教学文案；toolkits 静态表 ∩ live 渲染/connector 分组最长前缀/uncategorized 块；绑定四类锚点（含 fail-closed 反例）；Restricted backend 严格类型判定。
- 真实链纪律（feedback-real-chain-testing）：rail 走真实 AbilityManager + system_prompt_builder 管线，禁窄接口替身。
- 集成：tool_search→tool_call 闭环 + 开关翻转 child 重建（R-5）+ readonly 快速路径 allow（规则 7 两层门禁自检 `go run ./cmd/integration-audit -verify`）。
- 本地门槛：`go build ./...`、`go vet ./...`、`CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./...`；覆盖率 ≥85%。

## 6. IMPLEMENTATION_PLAN.md 登记变更（随批提交）

1. **9.11b 行改写**：范围 = tool 侧批一（rail 1,086 + toolkits 1,030 + catalog 187 + DeepAdapter 接线 + R-5 + V-6 四类 + Restricted cron backend）；skill 类绑定与 skill 侧三件 ⤴️16.4；15.20② 指向改 16.4。
2. **新 9.11c 行（☐）**：heartbeat 子系统整件 + V-6 heartbeat 类（⤴️ 待回填登记）。
3. **16.4 行补**：skill 侧回填清单（discovery 三件+rail+toolkits+session_profile+工厂桩+any 别名+V-6 skill 类）。
4. **9.90d-2 行**：V-6 标注拆分（4 类→9.11b 批一；skill 类→16.4；heartbeat 类→9.11c）。
5. **10.3.28f 行**：R-5 回填点改挂 9.11b。

## 7. 已知偏差

实施期如遇 Go 无继承/闭包身份校验不可直译（Python 用 `__closure__` 探测，Go 需以具体类型/函数值相等判定替代）等语言边界，按规则 9.6/12.3 在本节登记：Python 符号+行号、Go 等价位置、偏差性质、理由、回收条件，经确认后实施。
