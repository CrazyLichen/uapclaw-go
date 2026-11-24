# 16.4 技能发现与索引 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 对齐 Python `symphony/discovery`（3,225 行）+ skill 侧 rail/toolkits（582+651）+ DeepAdapter 接线，交付技能渐进式使用全链（flat 三态 + skill_index 工具 + 漂移通知 + 会话恢复），闭合 9.11b/15.16c 全部回填项。

**Architecture:** 两批：16.4a = retrieval infra 前置切片（discovery 的 5 个依赖件，未来 16.5 复用）；16.4b = discovery 五文件一比一 + harness rail/toolkits + DeepAdapter 懒创建与 sidecar 持久化。SkillTaxonomyRuntime ⤴️16.5（D-16-4-5）。

**Tech Stack:** Go 1.22+，`gopkg.in/yaml.v3`（tree_index/SKILL.md frontmatter），标准库 `crypto/sha256`、`sync`。禁窄接口替身（真实链测试纪律）；注释全中文；声明顺序按 Go 编码规范。

**Spec:** `docs/superpowers/specs/2026-11-24-symphony-discovery-16.4-design.md`（运行时语义 §3 十二条是逐条验收清单）。

**Python 真相源根路径：** `PY_CORE=/home/opensource/agent-core/openjiuwen/symphony`，`PY_SWARM=/home/opensource/jiuwenswarm-develop/jiuwenswarm`。

---

## 批次 16.4a：retrieval infra 前置切片

### Task 1: retrieval/common/models.go

**Files:**
- Create: `internal/agentcore/symphony/retrieval/common/models.go`（含 doc.go）
- Test: `internal/agentcore/symphony/retrieval/common/models_test.go`

- [ ] **Step 1: 读 Python 真相源** `PY_CORE/retrieval/common/models.py`（68 行），逐字段对齐 RetrieverItem/RetrieverNode（含 `children: map[string]any`——Python 中子节点值可为 RetrieverNode 或 SkillRecord，Go 侧 `map[string]any`，discovery 层断言具体类型，勿提前收窄）。
- [ ] **Step 2: 写失败测试**：RetrieverNode 构造/字段访问/children 遍历；RetrieverItem 四字段（id/name/description/payload 类语义对照 Python dataclass）。
- [ ] **Step 3: 实现**（类型 + 构造函数，方法归属与 Python 一致）。
- [ ] **Step 4:** `go test ./internal/agentcore/symphony/retrieval/common/ -cover` PASS，覆盖率 ≥85%。
- [ ] **Step 5:** Commit（path-limited：`git commit internal/agentcore/symphony/retrieval/ -m "feat(16.4a): retrieval common models"`）。

### Task 2: build/scanners_common.go

**Files:**
- Create: `internal/agentcore/symphony/retrieval/build/scanners_common.go`
- Test: `internal/agentcore/symphony/retrieval/build/scanners_common_test.go`

- [ ] **Step 1: 读真相源** `PY_CORE/retrieval/build/scanners/common.py`（150 行）。四个函数：`FindSkillFile`（SKILL.md/skill.md/Skill.md 三变体探测，目录内大小写枚举顺序照 Python `find_skill_file` 源码）、`FirstParagraph(body, limit=500)`（splitlines 行界语义，见 python-alignment 记忆）、`ReadSkillFile`（frontmatter YAML 解析 + body + content_hash 三元组返回）、`SHA256File`。
- [ ] **Step 2: 失败测试**（t.TempDir 造目录）：三变体命中与大小写优先序；无 SKILL.md 目录返回 nil/空；首段截断（≤500 字符、跨行段落合并）；frontmatter 缺失回落；空 frontmatter；二进制文件 hash。
- [ ] **Step 3: 实现。** os.path.exists 陷阱：Go 用 `os.Stat` err!=nil 判不存在，不用 `errors.Is(IsNotExist)` 吞其他 IO 错误（记忆 python-alignment-audit）。
- [ ] **Step 4:** 测试 PASS ≥85%。
- [ ] **Step 5:** Commit。

### Task 3: build/io/ 六文件

**Files:**
- Create: `internal/agentcore/symphony/retrieval/build/io/`：`doc.go`、`tree.go`、`items_jsonl.go`、`manifest.go`、`catalog.go`、`config_loader.go`
- Test: 同目录 `*_test.go`

- [ ] **Step 1: 读真相源** `PY_CORE/retrieval/build/io/`（tree 124 / items_jsonl 138 / manifest 44 / catalog 35 / config_loader 33）。对齐入口：`LoadTreePreset(path)`（tree_index.yaml → 排序节点树）、`NormalizeItemPaths`、`ParseSimpleNodesYAML`、`SortTreeNodes`、`WriteTreePreset`、`LoadManifest/WriteManifest`、`LoadCatalogRecords`、`IsPassthroughItemURI`。
- [ ] **Step 2: 失败测试**：tree_index.yaml 往返（写→读等价）；节点排序稳定性；items_jsonl 逐行解析（空行/坏行跳过语义照 Python）；manifest 读写；passthrough URI 判定（S3 URI 只需识别返回 true——**不实现物化**，见 D-16-4-1）。
- [ ] **Step 3: 实现。** S3 分支：`is_s3_uri` 判定保留，`materialize_s3_dir`/`read_s3_text` 到 S3 URI 时返回登记错误（错误文案注明 D-16-4-1），本地路径分支全量实现。
- [ ] **Step 4:** PASS ≥85%。
- [ ] **Step 5:** Commit。

### Task 4: search/artifacts/

**Files:**
- Create: `internal/agentcore/symphony/retrieval/search/artifacts/`：`doc.go`、`loading.go`、`merge.go`
- Test: 同目录 `*_test.go`

- [ ] **Step 1: 读真相源** `PY_CORE/retrieval/search/artifacts/loading.py`（274）+ `merge.py`（40）。导出：`CatalogRecord`、`LoadedRetrieverIndex`、`BuildTreeRoot(nodes, catalogRecords)`、`LoadRetrieverIndex(dir)`（本地分支：manifest+tree+catalog 组装；S3 分支走 D-16-4-1 错误）、`LoadTreeRoot`、`HitsToSearchResult`。
- [ ] **Step 2: 失败测试**：BuildTreeRoot 用 Task 1 的 RetrieverNode 构树（payload→catalog 映射）；LoadRetrieverIndex 在 Task 3 产物目录上的端到端加载；空目录/缺 manifest 错误路径。
- [ ] **Step 3: 实现。** `CatalogRecord` 定义在 `build/models.go`（31 行，Python `retrieval/build/models.py`），本包 re-export 引用（对齐 Python from-import）。
- [ ] **Step 4:** PASS ≥85%。
- [ ] **Step 5:** Commit。

### Task 5: search/runtime/lexical.go

**Files:**
- Create: `internal/agentcore/symphony/retrieval/search/runtime/lexical.go`
- Test: `lexical_test.go`

- [ ] **Step 1: 读真相源** `PY_CORE/retrieval/search/runtime/lexical.py`（704 行，本批最大件）。核心：`LexicalDocument(path, title, body)`、`LexicalIndex`（建索引/查询/分页）、`CompileMatcher(expr)`（查询表达式编译：短语/前缀/字段限定，逐分支对照 Python `compile_matcher`）。
- [ ] **Step 2: 失败测试**：建索引→查询命中排序；compile_matcher 各表达式分支（逐个 Python 分支一条测试）；空索引/空查询/超长词；大小写折叠。
- [ ] **Step 3: 实现。** 词法分析用标准库（strings.Fields + Unicode 处理），禁引第三方分词；RE2 注意 `\s` Unicode 并集（记忆 im-pipeline 教训）。
- [ ] **Step 4:** PASS ≥85%。
- [ ] **Step 5:** Commit。

### Task 6: 16.4a 收口

- [ ] **Step 1:** `go build ./... && go vet ./internal/agentcore/symphony/...`
- [ ] **Step 2:** 全量回归 `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./...` 零新增 FAIL。
- [ ] **Step 3:** IMPLEMENTATION_PLAN.md 无 16.4a 独立行（属 16.4 切片，不单独登记；16.4 行保持 ☐）。
- [ ] **Step 4:** push。

---

## 批次 16.4b：discovery 主体 + harness 接线

### Task 7: discovery/config.go

**Files:**
- Create: `internal/agentcore/symphony/discovery/`：`doc.go`、`config.go`
- Test: `config_test.go`

- [ ] **Step 1: 读真相源** `PY_CORE/symphony/discovery/config.py`（189 行）。`DiscoverySettings` 8 字段（frozen dataclass→Go 只读结构体）、默认值五常量、`candidateBudgetTokens` 属性（max(1, int(window*ratio))）、`LoadDiscoverySettings(configBase)`（_DISCOVERY_SETTING_NAMES 白名单过滤）、ContextUtils 分支（config.py:104 延迟导入——Go 侧按调用点语义内联，登记在 spec 已知偏差不新增）。
- [ ] **Step 2: 失败测试**：默认值断言（candidate_budget_ratio=0.01 等）、白名单外键忽略、prompt_preferred_skills 三项默认。
- [ ] **Step 3: 实现。** 
- [ ] **Step 4:** PASS ≥85%。Commit。

### Task 8: discovery/models.go（扫描与缓存）

**Files:**
- Create: `internal/agentcore/symphony/discovery/models.go`
- Test: `models_test.go`

- [ ] **Step 1: 读真相源** `PY_CORE/symphony/discovery/models.py`（466 行）。对齐：`SkillRecord`（10 字段）/`SkillInventory`（items+fingerprint）/`ScanSkillDirectories(dirs, disabledSkills, sourceByName)` 主链（:191 起）：根枚举→子目录判定（FindSkillFile）→ReadSkillFile 字段回落（description 三级）→`_with_unique_worker_ids` 后缀消解→`_canonical_skill_id`（控制字符 `~uXXXX`、空名 unnamed，:414-423）→inventory fingerprint（sha256 of json sorted keys）。
- [ ] **Step 2: 五层缓存**（D-16-4-4 等价实现）：`skillFileCache`（key=path，值带 mtime+size 签名，LRU 2048）/`scannedRecordCache`/`rootLayoutCache`/inventory single-flight（同参并发只跑一次）+lastInventories/`normalizedCache`。包级 map+RWMutex。
- [ ] **Step 3: 失败测试**（全部 t.TempDir 真盘）：SKILL.md 三变体与优先序；`_` 前缀/隐藏/disabled 排除；worker_id canonical（控制字符/空名/同名后缀）；description 三级回落逐级；多根同名先扫优先；fingerprint 稳定性与内容敏感性；缓存：同文件二次扫描不重读（mtime 不变）、改文件后重读、single-flight 并发计数。
- [ ] **Step 4:** PASS ≥85%。Commit。

### Task 9: discovery/skillfs.go

**Files:**
- Create: `internal/agentcore/symphony/discovery/skillfs.go`
- Test: `skillfs_test.go`

- [ ] **Step 1: 读真相源** `PY_CORE/symphony/discovery/skillfs.py`（902 行）。对齐：`SkillDirectoryView`（path 投影 nodeByPath，:147-370）/`SkillFS` 构造（pinnedIndexSnapshot 三态参数 ：404-428）/`Refresh`（:607-636：现扫+captureIndexSnapshot+indexState 判定+_buildLiveTree+fingerprint）/`PromptSnapshot`（:470-499 四态推导）/`_promptBranches`（:501-525 下钻规则）/`_largePromptItems`（preferred 优先+截断）/`SelectionCards`/`ReadBody`（body 缓存）/`CaptureIndexSnapshot`（读 publish lock 下 taxonomy 代际，D-16-4-1 本地分支）/`_recoverIndexDirectoryUnlocked`（**偏差登记复刻**，来源 `PY_CORE/symphony/agent/retrieval_toolkit.py`，文件头注明 D-16-4-2 与回收条件）。
- [ ] **Step 2: 失败测试**：四态逐态（无索引+少量=small；无索引+大量=large-flat；磁盘预置合法 tree_index.yaml+catalog=fresh/indexed；改技能文件=stale）；分支行 ` > ` 连接下钻/混技能停层/封顶；冻结后 PromptSnapshot(refresh=false) 稳定；SelectionCards 投影；空快照兜底。
- [ ] **Step 3: 实现。** `_LEXICAL_CACHE`（skillfs.py:33）同样包级 LRU。**不实现 SkillTaxonomyRuntime，无写索引路径**（D-16-4-5）。
- [ ] **Step 4:** PASS ≥85%。Commit。

### Task 10: discovery/toolkit.go（notice 基线表）

**Files:**
- Create: `internal/agentcore/symphony/discovery/toolkit.go`
- Test: `toolkit_test.go`

- [ ] **Step 1: 读真相源** `PY_CORE/symphony/discovery/toolkit.py`（217 行）。对齐：`SkillDCICommandResult`（str 子类→Go 结构体：ModelContent + DetailedOutput + Data）、`preparedNotice`/`noticeState`/包级 `noticeStates`（D-16-4-3：map+mutex，LRU 1024，key=ToolID(sessionScope, toolName)，domain 前缀 "openjiuwen.skill-index.v1\0" 保留品牌不改——注意第三方内部域串非用户可见品牌）、`InitializeIncrementalSkillNoticeState`/`ConsumeIncrementalSkillReminder`/`ClearIncrementalSkillNoticeStates`/`IncrementalNoticeSession.Append`（:126-142：added 渲染→预算内拼接→acknowledge→返回）。
- [ ] **Step 2: 失败测试**：consume 首次全量视为 added；二次同基线无通知；新会话 scope 独立；同 scope 不同 toolName 独立（tool_id 语义）；acknowledge 推进 vs 渲染失败保留 added 重试；LRU 淘汰；Append 预算超限放弃（返回原 output, nil）。
- [ ] **Step 3: 实现。** 
- [ ] **Step 4:** PASS ≥85%。Commit。

### Task 11: discovery/directory_toolkit.go（skill_index 工具）

**Files:**
- Create: `internal/agentcore/symphony/discovery/directory_toolkit.go`
- Test: `directory_toolkit_test.go`

- [ ] **Step 1: 读真相源** `PY_CORE/symphony/discovery/directory_toolkit.py`（1,407 行，16.4b 最大件）。对齐：`InstalledSkillsDirectoryToolkit` 构造（environment/sessionScope/incrementalNoticeMaxChars，:176-196）→ `GetTools()`（LocalFunction→Go tool.Tool，暴露 `skill_index` 单工具，ToolCard 描述照 Python）→ `SkillIndex(operation, ...)` 三操作分发：list（树布局分支行+分页游标）、search（LexicalIndex：fresh 走 taxonomy、否则临时 flat）、read（单技能 SKILL.md 正文）、`[next]` 游标 footer JSON（separators 紧凑序）、`[truncated]` 注脚、_MAX_CURSORS=32 游标表、_MIN/_MAX_OUTPUT_CHARS/_MAX_LINES=5000/描述 180 字符截断、`observed_meta_paths` 记账、结果组装尾部 notice append（:420-424）+ diagnostics detailed_output。
- [ ] **Step 2: 失败测试**：list 翻页往返（cursor 取回下一页）；search 命中排序与翻页；read 全文；游标耗尽/非法游标错误码；输出预算截断链（footer/notice/正文按 Python _append_note 预算序）；METADATA.md 虚拟寻址（路径串构造无读盘——spec §80②）；三操作对无效 operation 报错。
- [ ] **Step 3: 实现。** 
- [ ] **Step 4:** PASS ≥85%。Commit。

### Task 12: harness tools skill_retrieval_toolkits.go

**Files:**
- Create: `internal/swarm/agents/harness/common/tools/skill_retrieval_toolkits.go`
- Test: `skill_retrieval_toolkits_test.go`

- [ ] **Step 1: 读真相源** `PY_SWARM/agents/harness/common/tools/skill_retrieval_toolkits.py`（651 行）。对齐：`BuildModelDiscoverySettings(configBase, model)`（context_window_tokens 解析三级：model_name→react.context_engine_config→override，:240-275）/`SkillRetrievalArtifactRoot`（配置→SYMPHONY_SKILL_RETRIEVAL_ROOT env→workspace/symphony/skill_retrieval）→`SkillRetrievalToolkit` 构造全链（:277-470：config 深拷贝、frozen_profile 防御校验、restored_settings/_snapshot/_index_snapshot 三段恢复、index_enabled/candidate_scale 判定、environment=SkillFS 装配、InstalledSkillsDirectoryToolkit 挂载）→`SessionProfile()`（:538-560 12 键）→`CurrentRecords/CurrentIndexRecords`→`SkillIndex` 转发→`GetTools`。`IsValidSkillRetrievalSessionProfile`（:126）。
- [ ] **Step 2: 失败测试**：settings 解析三级回落；artifact_root 三级；toolkit 构造（真盘目录）→ prompt_snapshot 四态到达；frozen_profile 恢复跳过重扫（对照扫描计数/指纹相等）；非法 profile 忽略回落；session_profile 12 键往返 JSON。
- [ ] **Step 3: 实现。** 目录 provider 用函数值（Python callable 语义→Go `func() []string`）。
- [ ] **Step 4:** PASS ≥85%。Commit。

### Task 13: rails skill_retrieval_prompt_rail.go

**Files:**
- Create: `internal/swarm/agents/harness/common/rails/skill_retrieval_prompt_rail.go`
- Test: `skill_retrieval_prompt_rail_test.go`

- [ ] **Step 1: 读真相源** `PY_SWARM/agents/harness/common/rails/skill_retrieval_prompt_rail.py`（582 行）。对齐（方法归属逐个照 Python）：构造（:97-127：toolkit.frozen_prompt_snapshot / prompt_skillfs.prompt_snapshot / empty 三级，构造期即冻结）/`BeforeInvoke`（:146-230：session 判定→legacy 隐藏（list_skill abilities remove+原生 SKILLS section 暂存）→冻结快照渲染 section+CANDIDATE 附录→候选 token 预算 1% 落点）/`AfterToolCall`（:265-282：工具名白名单 `_SYMPHONY_RUNTIME_TOOL_NAMES`→skill_index 跳过→现扫+consume+`_appendToolResultReminder`）/`_appendToolResultReminder`（:291-308 output→result→system_reminder 三级）/`Unregister`（:134-143：restore legacy+restore section）。
- [ ] **Step 2: 失败测试**：构造期冻结（构造后再改目录快照不变）；BeforeInvoke 注入断言（section 内容含 worker_id/description 行）；legacy remove/restore 往返；AfterToolCall 三分支（skill_index 跳过/其他工具追加 output/无 output 键落 system_reminder）；扫描异常静默（tmp 目录删除后调用不 panic、无通知、不阻断）。
- [ ] **Step 3: 实现。** rail 基座 DeepAgentRail 接口对齐 9.11b tool 侧先例（internal/swarm/agents/harness/common/rails/ 既有 rail 结构）。
- [ ] **Step 4:** PASS ≥85%。Commit。

### Task 14: DeepAdapter 接线（懒创建 + session_profile）

**Files:**
- Modify: `internal/swarm/server/adapter/deep_adapter.go`、`internal/swarm/server/adapter/deep_adapter_sessions.go`
- Test: `internal/swarm/server/adapter/deep_adapter_sessions_test.go`（追加）

- [ ] **Step 1: 读真相源** `PY_SWARM/server/runtime/agent_adapter/interface_deep.py`：`_get_or_create_skill_retrieval_toolkit`（:7165-7191 懒创建+persist）、`_load/_persist/_discard_skill_retrieval_session_profile`（:2497-2676 sidecar 原子写 0o600/≤16MB/checksum）、`persist_skill_retrieval_session_profile`/`discard_transient_...`（:2658-2676）、create_instance 恢复段（:4283-4335 五重校验→frozen_profile 传构造）、`_skill_scan_dirs`（:10200-10236 多根）、`_live_skill_retrieval_disabled_skills`（:7120-7145 reload_state+ListDisabledSkills）、`_build_skill_retrieval_prompt_rail`（:11137-11151）。Go 既有锚点：deep_adapter.go:1077（⤵️注释）与 deep_adapter_sessions.go:640（profile path 注释）——按锚点位置展开实现。
- [ ] **Step 2: 失败测试**：懒创建幂等（二次调用同实例）；persist→load 往返；五重校验逐项破坏（改一字节/换 session_id/超 16MB/错 schema_version/非 canonical JSON）→降级 enabled=false+recovery_error；_skill_scan_dirs 单根/带 MCP 两根；disabled provider 走真 SkillManager（ListDisabledSkills 已有，skill_manager.go:2317/2325）。
- [ ] **Step 3: 实现。** 真链：adapter 持有 `*SkillManager`（具体类型，禁窄接口——真实链纪律）。
- [ ] **Step 4:** PASS。Commit。

### Task 15: 装配收口（any 收窄 + V-6 skill 绑定 + 两工厂桩闭合）

**Files:**
- Modify: `internal/swarm/agents/swarm/buildctx/context.go:159-161`（any→具体类型）
- Modify: `internal/swarm/agents/swarm/providers/member_rails.go:209-272`（`swarm.skill_retrieval_prompt` 桩→真身 `_build_skill_retrieval_prompt_rail` 对齐 member_rails.py:246-272）
- Modify: `internal/swarm/agents/swarm/providers/tools.go:257-282`（`BuildSkillRetrieval` 桩→真身：白名单过滤后目录→SkillRetrievalToolkit→GetTools，V-6 skill 类装配期身份锚点）
- Test: providers 包既有测试文件追加

- [ ] **Step 1: 失败测试**：工厂产出真 rail/toolkits（非 nil、类型正确、含 skill_index 工具）；开关关闭时工 nil 且桩路径不存在。
- [ ] **Step 2: 实现 + 删除清单（规则 8）**：删桩体与 ⤴️16.4 注释；grep 验收 `grep -rn "⤴️16.4\|⤴️ 16.4" internal/ tests/ cmd/` skill 侧语义 0 命中（16.5 语义的 D-16-4-5 锚点除外，它写的是 16.5）。
- [ ] **Step 3:** build+vet PASS。Commit。

### Task 16: 集成套件 + 回归 + 台账

**Files:**
- Create: `tests/integration/agentcore/symphony/discovery_progressive/`（`//go:build integration`，suite.Run）

- [ ] **Step 1: 集成用例**（真实链，跨 adapter/harness/discovery ≥3 子系统过规则 7 审计）：
  1. 渐进式闭环：真盘技能目录（少量→small 全量注入；大量→large-flat+分支行）→agent 循环调 skill_index list/search/read 翻页→断言模型可见内容
  2. 漂移通知：会话中新增技能目录→非 skill_index 工具调用后 tool_result 尾部含 reminder→skill_index 查到新技能
  3. 恢复链：persist→破坏字节→load 降级重扫不中断；完好→frozen_profile 恢复
  4. legacy 隐藏：rail 挂载后 list_skill 不可见/卸载恢复
- [ ] **Step 2:** `go run ./cmd/integration-audit -verify` 通过。
- [ ] **Step 3:** 全量回归 `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./...` 零新增 FAIL。
- [ ] **Step 4:** IMPLEMENTATION_PLAN.md 16.4 行 ☐→✅（附偏差与回填台账摘要）；16.5 行 ⤴️ 注记核对在位。
- [ ] **Step 5:** push。

---

## 执行注意（来自既有纪律）

- **共享 checkout git 纪律**：一律 path-limited commit；commit 前 `git status --porcelain` 核对他流 staged；禁 `--amend`（先 `git log -1` 核 HEAD 归属）。
- **worktree**：实施时 EnterWorktree 后先 `git log` 分支基线核对无他人提交。
- **禁窄接口替身**（真实链纪律）：adapter 收 `*SkillManager` 具体类型；测试构造真盘目录（t.TempDir）。
- **规则 9 结构忠实**：Python 一文件→Go 一文件；方法归属/步骤顺序/命名 snake→Camel 逐条对照；偏差只走 spec §4 五条，不得注释里自行宣布新偏差。
- **16.4a 与 16.4b 可分批推送**；16.4b 内 Task 7-11（agentcore 侧）与 Task 12-15（swarm 侧）依赖单向，按序执行。
