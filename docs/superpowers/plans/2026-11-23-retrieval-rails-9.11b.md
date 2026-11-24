# 9.11b 检索工具 Prompt Rails（批一）实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 对齐 jiuwenswarm `ToolRetrievalPromptRail` + `ToolRetrievalToolkit` + `tool_discovery_catalog`（tool 侧渐进披露），闭合 DeepAdapter 接线与 R-5 开关翻转重建判据，回填 V-6 readonly 绑定四类中的 tool/cron/progressive 三锚点（skill ⤴️16.4、heartbeat ⤴️9.11c）。

**Architecture:** 新 rail 挂 `internal/swarm/agents/harness/common/rails/`（与 Python common/rails 同层级），复用 9.11a 基座（`progressive.go` 的目录附件渲染器 `sections.RenderDeferredToolCatalogDelta/Snapshot`、`tool_discovery.BM25ToolIndex`、`RelayedToolOutput`）；DeepAdapter 层做单一所有者切换（丢弃内置 ProgressiveToolRail，build 失败兜底保留）；V-6 以具体类型/函数值相等校验扩展 `readonly_tool_bindings.go`。

**Tech Stack:** Go 1.22+、既有 `internal/openuapclaw/harness/rails`、`internal/openuapclaw/harness/tools/tool_discovery`、`internal/common/config`。禁窄接口替身（feedback-real-chain-testing）。

**Spec:** `docs/superpowers/specs/2026-11-23-retrieval-rails-9.11b-design.md`（语义 §2 六组、文件映射 §3、登记 §6）。

**Python 真相源：** `/home/opensource/jiuwenswarm-develop/jiuwenswarm/agents/harness/common/{rails/tool_retrieval_prompt_rail.py,tools/tool_retrieval_toolkits.py,tools/tool_discovery_catalog.py,tools/cron/cron_runtime.py,tools/cron/cron_tools.py}`、`server/runtime/agent_adapter/interface_deep.py`、`common/rails/permissions/_auto_permission/readonly_tool_bindings.py`。

---

## 结构总览

```
新建：
  internal/swarm/agents/harness/common/rails/tool_retrieval_prompt_rail.go      ← rail 1,086 行对齐
  internal/swarm/agents/harness/common/tools/tool_retrieval_toolkits.go         ← toolkits 1,030 行对齐
  internal/swarm/agents/harness/common/tools/tool_discovery_catalog.go          ← catalog 187 行对齐
修改：
  internal/common/config/progressive.go            ← ToolRetrievalEnabled 双开关
  internal/swarm/agents/harness/common/tools/cron/cron_runtime.go  ← restrictedCronBackend 只读变体
  internal/swarm/agents/harness/common/rails/permissions/auto_permission/readonly_tool_bindings.go ← V-6
  internal/swarm/server/adapter/deep_adapter.go    ← tool_retrieval 接线（单一所有者）
  internal/swarm/server/adapter/deep_adapter_sessions.go:240 ← R-5 重建判据
  internal/swarm/agents/swarm/providers/member_rails.go / tools.go ← 桩注释改指 16.4
  IMPLEMENTATION_PLAN.md                            ← 五处登记（Task 12）
删除清单（Task 12 验收）：
  deep_adapter_sessions.go:240 R-5 ⤵️ 注释行（回填后）
  readonly_tool_bindings.go:89 "只保留 ProgressiveToolRail 分支（D3）" 注释与 fail-closed 现状
  providers 桩内 ⤴️9.11b 标记（改 ⤴️16.4）
```

方法归属纪律（规则 9/12）：Python 类方法 → 同名 struct 方法（`snake_case → CamelCase`），模块级函数保持包级；方法内步骤按 Python 行号顺序，禁止拆 helper/合并步骤。注释中文。包注释/文件头按 doc.go 规范同步。

---

### Task 1: tool_discovery_catalog.go（双语检索投影）

**Files:**
- Create: `internal/swarm/agents/harness/common/tools/tool_discovery_catalog.go`
- Modify: `internal/swarm/agents/harness/common/tools/doc.go`（文件树补条目）
- Test: `internal/swarm/agents/harness/common/tools/tool_discovery_catalog_test.go`

Python: `tool_discovery_catalog.py`（187 行）：`ToolDiscoveryMetadata` dataclass、`PROGRESSIVE_DISCOVERY_PROPERTY = "progressive_discovery"`、`BUILTIN_TOOL_DISCOVERY` 全表（每工具 display_en + aliases_zh 空格分词）、`discovery_description(tool, language)`、`discovery_search_text(tool)`。

- [ ] **Step 1: 写失败测试**

```go
func TestDiscoveryDescription_双语回落(t *testing.T) {
    card := &tool.ToolCard{ Name: "cron_create_job", /* description map 含 zh/en */ }
    if got := DiscoveryDescription(card, "zh"); got == "" { t.Fatal("zh 描述为空") }
    en := DiscoveryDescription(card, "en")
    if en == DiscoveryDescription(card, "zh") { t.Fatal("双语未区分") }
}
func TestDiscoverySearchText_含别名(t *testing.T) {
    // cron_create_job 的投影必须含中文明细词 "提醒我"
    if s := DiscoverySearchText(cardFor("cron_create_job")); !strings.Contains(s, "提醒我") {
        t.Fatalf("search_text 缺中文别名: %s", s)
    }
}
```

- [ ] **Step 2: 跑测试确认 FAIL**（`go test ./internal/swarm/agents/harness/common/tools/ -run TestDiscovery -v`）

- [ ] **Step 3: 实现**——`ToolDiscoveryMetadata{DisplayEn string; AliasesZh []string}`、包级 `builtinToolDiscovery` map 逐条复刻 Python `_entry` 全表（**不得裁剪条目**，规则 9.2-4）；`DiscoveryDescription` 对齐 `tool_discovery_catalog.py:120-150`（zh 缺失回落 en，en 缺失回落 zh，都缺回落 ToolCard 描述）；`DiscoverySearchText` 对齐 `:155-187`（display_en + aliases_zh join + 卡片描述，多空格折叠）。

- [ ] **Step 4: 跑测试 PASS** + `go build ./...`

- [ ] **Step 5: Commit**（path-limited：`git add <上述文件> && git commit -m "feat(9.11b): tool_discovery_catalog 双语检索投影"`）

### Task 2: 配置开关 ToolRetrievalEnabled

**Files:**
- Modify: `internal/common/config/progressive.go`
- Test: `internal/common/config/progressive_test.go`（追加）

Python: `common/config.py:433-479`——`is_tool_retrieval_enabled = progressive_tool_enabled && progressive_tool_optimize`，双默认 False。

- [ ] **Step 1: 失败测试**：`ToolRetrievalEnabled(nil)==false`；仅 `progressive_tool_enabled=true` → false；双 true → true；`progressive_tool_optimize` 非布尔真值（字符串 "true"/1）对齐 Python truthiness（复用既有 falsy 助手）。
- [ ] **Step 2: FAIL → 实现 `ToolRetrievalEnabled(cfg map[string]any) bool`（复用 ProgressiveToolEnabled + 新增 ProgressiveToolOptimize）→ PASS → Commit**。

### Task 3: ToolRetrievalToolkit——静态分类表与冻结快照

**Files:**
- Create: `internal/swarm/agents/harness/common/tools/tool_retrieval_toolkits.go`
- Modify: `doc.go`
- Test: `tool_retrieval_toolkits_test.go`

Python: `tool_retrieval_toolkits.py`。对齐范围（方法清单对照写入提交信息）：

- `ToolCategory{Path, Description, Tools, DescriptionEn}` + `DescriptionFor(language)`（:41-51）
- 静态表 `_TOOL_CATEGORIES` 全部分类逐条复刻（:58-300，**成员名单不得裁剪**）
- `ToolCatalogueSnapshot{Categories, CoveredNames, UncategorizedEntries}`（构造段）
- `ToolRetrievalToolkit` struct：`frozenCatalogue`、`frozenConnectorGroups`、`toolByName`（live 注册表投影）、`language`、`mcpServersProvider func() map[string]McpServerDesc`（注入缝，nil = 无 MCP 分支）
- `RefreshSnapshot()`（:582-592：init 与首次 before_model_call 各一次）
- `McpConnectorGroups()`（:539-600：`mcp_{server}_` 最长前缀匹配、退化首段、`mcp_x` 裸名跳过、描述 zh/en 双语回落、兜底文案）
- `_build_catalogue`（静态表 ∩ live 跳过未注册名 + connector 分支 + uncovered 块）
- `ToolSearch(ctx, kwargs)`（:617-700：operation list/get_schema 分派、`search` 别名与空 operation 降级、退役字段 `call_goal/cursor/query/tools` 防御性 pop、裸 list ValueError 教学文案**逐字符复刻**）
- `_render_listing` / `_render_root_listing`（:702-800：单成员折叠 `[tool]` 行、connector 恒 `[category]` 行）
- `_rankToolsByQuery`（:751-800：BM25ToolIndex.build 语料=单分类或全量、tokenize 丢单字符 token、精确名优先、score>0）
- `_render_schema`（ToolCard → {name, description, schema} 渲染，对齐 Python 渲染函数）
- 120 字符压缩 `_compactText`（对齐 `_compact_schema_description`）

- [ ] **Step 1: 失败测试**（覆盖：静态表 ∩ live 渲染跳过未注册名；connector 最长前缀拆分（`mcp_github_ci_create` server=github_ci）；裸 list 错误文案逐字符断言；退役字段降级（旧快照 query/tools 不报错）；BM25 单字符 token 丢弃（"图片生成" vs "完成"不互中）；单成员折叠）。
- [ ] **Step 2: FAIL**（`go test ./internal/swarm/agents/harness/common/tools/ -run ToolRetrieval -v`）
- [ ] **Step 3: 实现全量**（真实 AbilityManager 投影：`toolByName` 从 `agentinterfaces.AbilityManagerInterface` 枚举构建，禁替身）
- [ ] **Step 4: PASS** + 覆盖率 ≥85%（`go test -cover ./internal/swarm/agents/harness/common/tools/`）
- [ ] **Step 5: Commit**

### Task 4: ToolRetrievalPromptRail——目录注入与过滤

**Files:**
- Create: `internal/swarm/agents/harness/common/rails/tool_retrieval_prompt_rail.go`
- Modify: `internal/swarm/agents/harness/common/rails/doc.go`
- Test: `tool_retrieval_prompt_rail_test.go`

Python: `tool_retrieval_prompt_rail.py`。方法对照：

- struct：`priority`、`SECTION_NAME="tool_retrieval_disclosure"`（对齐 Python 常量）、`frozenSnapshot`、`frozenConnectorGroups`、`toolkit`、`configBase`、`systemPromptBuilder`（`agentinterfaces.BaseAgent` 取得）
- `Init(agent)`（:176-222：enabled 判定 → toolkit.RefreshSnapshot → freeze connector groups → tool_call envelope 注册判定日志 :260-269）
- `BeforeModelCall(ctx, cbc)`（:410-446：语言读取 → 首渲染 re-freeze :424 → remove+add section（仅本 section）→ `syncDeferredToolCatalog` → `filterDeferredFromInputs`）
- `_buildDisclosureAppendix`（:453-540：intro 双语 7 条规则文案**逐字节复刻** `_DISCLOSURE_INTRO_CN/EN` :75-145、分类行、connector 行、uncovered 块、全空兜底"No available tools."）
- `_syncDeferredToolCatalog`（:840-980：指纹比对 → delta 附件；`_CATALOG_SNAPSHOT_INTERVAL=10` 兜底 snapshot——**复用 9.11a `progressive.go syncDeferredToolAttachment` 的状态键契约，键名按 Python 加 `__tool_retrieval_deferred_catalog__` 前缀区分** :64-73）
- `_filterDeferredFromInputs`（:17-18 三职责第 3 条：inputs.tools 缩为 direct ∪ {tool_search, tool_call}；复用 9.11a `filterModelCallTools` 语义，anchor=`task_tool`，meta 序 `(tool_search, tool_call)` :54-61）
- `Uninit`（:405 段：clear frozen state）
- 错误路径日志（规则 3.4）：freeze 失败/attachment 失败/refresh 失败 Warn，字段对齐 Python f-string 变量

- [ ] **Step 1: 失败测试**（真实 system_prompt_builder + AbilityManager 管线：enabled=false 不注入；注入后 section 存在且含 intro；tools 过滤后 deferred 消失 meta 保留；drift → delta 附件；10 次 delta → snapshot；语言切换重渲染）
- [ ] **Step 2: FAIL** → **Step 3: 实现** → **Step 4: PASS + cover ≥85%** → **Step 5: Commit**

### Task 5: DeepAdapter 接线——单一所有者 + R-5

**Files:**
- Modify: `internal/swarm/server/adapter/deep_adapter.go`（rails 装配段）
- Modify: `internal/swarm/server/adapter/deep_adapter_sessions.go:233-247`
- Modify: `internal/common/config/progressive.go`（如 Task 2 未覆盖 session 判定助手）
- Test: `internal/swarm/server/adapter/deep_adapter_tool_retrieval_test.go`

Python: `interface_deep.py:7270-7390`（三方法）+ `:4214-4232`（R-5）。

- [ ] **Step 1: 失败测试**
  1. enabled=true → `_pending_rails` 等价 rail 列表中 ProgressiveToolRail 被剔除、ToolRetrievalPromptRail 存在（日志字段 `dropped_bundled=true`）；
  2. toolkit 构造失败 → nil 返回 + 保留内置 rail（`keep_bundled=true`）；
  3. enabled=false → 内置 rail 保留；
  4. R-5：config 快照 tool_retrieval 开关翻转 → `getOrCreateSessionAdapter` 重建 child（对齐 Python :4214-4232 判据原序）。
- [ ] **Step 2: FAIL** → **Step 3: 实现**——`buildToolRetrievalPromptRail()`（toolkit 懒创建，失败 nil + Warn）；装配点替换 9.11a rail；R-5 在步骤 6 判据链插入（读 config 快照前后对比）。**R-5 回填后删除 :240 ⤵️ 注释行**。
- [ ] **Step 4: PASS** → **Step 5: Commit**

### Task 6: _RestrictedCronBackend 只读变体

**Files:**
- Modify: `internal/swarm/agents/harness/common/tools/cron/cron_runtime.go`
- Test: `cron_runtime_test.go` 追加

Python: `cron_runtime.py:937-1067`——`_RestrictedCronBackend` 包装 backend，仅放行只读方法（list/get/preview/status/runs），写方法（create/update/delete/toggle/run_now/wake）返回受限错误；`CronRuntimeBridge.BuildTools`（:1007-1067 等价段）在只读会话语义下注入 restricted backend 并同步 schema 收敛（:786 注释段：统一 cron 工具 schema 的写参数在受限模式下剔除）。

- [ ] **Step 1: 失败测试**：restricted 下 ListJobs/GetJob/PreviewJob/Status/GetRuns 透传成功；CreateJob 返回受限错误（文案对齐 Python）；`type(backend) is restrictedCronBackend` 判定可用（供 Task 8 用，导出判定函数 `IsRestrictedCronBackend(b agentcron.CronToolBackend) bool`——Go 无 `type(x) is` 语法，以 `reflect.TypeOf(b) == restrictedCronBackendType` 等价，登记偏差 §7）。
- [ ] **Step 2: FAIL → 实现 → PASS → Commit**

### Task 7: V-6——readonly_tool_bindings 扩 tool_retrieval + cron 两锚点

**Files:**
- Modify: `internal/swarm/agents/harness/common/rails/permissions/auto_permission/readonly_tool_bindings.go`
- Test: 追加 `readonly_tool_bindings_v6_test.go`

Python: `readonly_tool_bindings.py:148-176`（tool_retrieval：`tool_type=LocalFunction` + `rail_type=ToolRetrievalPromptRail` 归属校验 `_owned_rail`）+ `:280-310`（cron：`type(backend) is _RestrictedCronBackend` + `_methods_match(backend, _CronToolsCronBackend, ("list_jobs","_with_route"…))` 闭包/函数值一致性校验）。

- [ ] **Step 1: 失败测试**：①tool_retrieval 归属（rail 持有 toolkit 且工具名在目录内）→ allow；rail 不持有 → nil（fail-closed）；②cron：backend=restricted 且方法集一致 → allow；backend=普通 cronToolsBackend → nil；方法被替换 → nil。**fail-closed 反例必测**。
- [ ] **Step 2: FAIL → 实现**（删除 `:89 "只保留 ProgressiveToolRail 分支"` 注释现状，扩 `sessionObservationBinding` 分支；Python 闭包探测在 Go 以函数值指针/方法集相等判定替代，登记偏差）→ **PASS → Commit**

### Task 8: providers 桩注释改指 16.4 + 工厂桩语义核对

**Files:**
- Modify: `internal/swarm/agents/swarm/providers/member_rails.go:208-218`、`tools.go:24,257-280`
- Test: 既有桩测试不动（行为不变，纯注释/语义核对）

- [ ] **Step 1**：`buildSkillRetrievalPromptRail`/`BuildSkillRetrieval` 桩注释 ⤴️9.11b → ⤴️16.4（15.20② 改指）；核对 `isSkillRetrievalEnabled` 若在 tools.go Task5 落地过则保留（skill 开关属 16.4 复用）。
- [ ] **Step 2: `go build ./... && go vet ./...` → Commit**

### Task 9: 真实链集成测试

**Files:**
- Create: `tests/integration/swarm/toolretrieval/integration_test.go`（`//go:build integration`、`suite.Run`、断言 ≥2、真实跨 ≥3 子系统：DeepAdapter 装配 + harness rails + permissions 绑定）

用例（对齐 spec §5）：
- [ ] **Step 1**: ①tool_search→tool_call 闭环（真 AbilityManager 注册 deferred 工具，rail 注入 → 模型模拟调用元工具 → RelayedToolOutput 渲染）；②开关翻转 child 重建（R-5）；③readonly 快速路径 allow（restricted cron 工具调用免 LLM 审查）+ fail-closed 反例。
- [ ] **Step 2: 本地自检** `go run ./cmd/integration-audit -verify` 通过 → **Step 3: Commit**

### Task 10: doc.go 与 AGENTS.md 同步

- [ ] `internal/swarm/agents/harness/common/{rails,tools}/doc.go` 文件树补三新文件；`rails/AGENTS.md`（如存在契约变更段落）补"检索类只读 rail 单一所有者"不变量。Commit。

### Task 11: 全量验证

- [ ] `go build ./... && go vet ./...`
- [ ] `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./...`（全量回归零 FAIL；-race 对新增包 `go test -race ./internal/swarm/agents/harness/common/...`）
- [ ] 新增包覆盖率 ≥85%（`go test -cover`，逐包核对）

### Task 12: IMPLEMENTATION_PLAN.md 五处登记 + 删除清单验收

- [ ] 9.11b 行改写（批一范围 + ⤴️16.4/9.11c 标注）
- [ ] 新 9.11c 行（☐，heartbeat 子系统 + V-6 heartbeat 类 + Bridge 双 any 收紧/HEARTBEAT_TOOL_NAMES/Uninit remove）
- [ ] 16.4 行补 skill 侧回填清单（discovery 三件 + rail/toolkits + session_profile + 两工厂桩 + any 别名 + V-6 skill 类）
- [ ] 9.90d-2 行 V-6 拆分标注
- [ ] 10.3.28f 行 R-5 改挂 9.11b
- [ ] **删除清单 grep 锚点为空**：`R-5 tool_retrieval 开关翻转.*⤵️`、`只保留 ProgressiveToolRail 分支`、`⤴️9.11b`（providers 桩内）在 internal/ 0 命中
- [ ] Commit + push（规则 11：本地绿即交付，不追 CI）

---

## 已知偏差预告（实施时按规则 9.6/12.3 落 spec §7）

| Python 机制 | Go 等价 | 性质 |
|---|---|---|
| `type(backend) is _RestrictedCronBackend`（readonly_tool_bindings.py:288） | `reflect.TypeOf` 相等判定 + `IsRestrictedCronBackend` 导出函数 | 语言边界，行为等价 |
| `__closure__`/`_methods_match` 闭包身份探测（:294-310） | 函数值指针相等 / 方法集一致性校验 | 语言边界 |
| Python 动态 pop kwargs（tool_search 退役字段） | Go 显式 map[string]any 处理，保留降级语义 | 类型系统差异 |
